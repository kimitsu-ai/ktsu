# Kimitsu — Runtime Architecture
## Architecture & Design Reference — v4

---

## Runtime Architecture

An Kimitsu deployment consists of four container tiers running on a shared internal network. All inter-container communication is HTTP. No container except the LLM Gateway has outbound internet access by default.

### The Four Tiers

```
┌─────────────────────────────────────────────────────────────┐
│                    Internal Network                          │
│                                                             │
│  ┌──────────────┐    ┌──────────────┐                       │
│  │ Orchestrator  │───▶│ Agent Runtime│                       │
│  │              │◀───│ (worker pool)│                       │
│  │  - DAG       │    │              │                       │
│  │  - Air-Lock  │    │  - event loop│                       │
│  │  - Scheduler │    │  - MCP client│                       │
│  │  - State     │    │  - access    │                       │
│  │    store     │    │    policy    │                       │
│  └──────┬───────┘    └──────┬───────┘                       │
│         │                   │                               │
│         │            ┌──────▼───────┐                       │
│         └───────────▶│ LLM Gateway  │──▶ internet           │
│                      │              │   (LLM providers)     │
│                      └──────────────┘                       │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Built-in Tool Servers  (first-party, Kimitsu-managed) │   │
│  │                                                      │   │
│  │  ktsu/kv   ktsu/blob   ktsu/log   ktsu/envelope          │   │
│  │  ktsu/memory   ktsu/format   ktsu/validate              │   │
│  │  ktsu/cli   ktsu/transform                             │   │
│  │                                                      │   │
│  │  Stateful servers call back to orchestrator HTTP API │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
         │
         │ MCP (HTTP/SSE) — internal or external
         ▼
┌─────────────────────────────────────────────────────────────┐
│  User-Provided Tool Servers  (operator-managed)             │
│                                                             │
│  wiki-search   crm-lookup   sentiment-scorer   cli-custom   │
│  (local servers and marketplace servers)                    │
└─────────────────────────────────────────────────────────────┘
```

### Orchestrator

A long-running container that owns the pipeline lifecycle. It is the only component that understands the DAG. It makes zero LLM calls. It is the single writer to the state store database.

Responsibilities:
- Parse and validate workflow, agent, and tool server YAML files
- Resolve the dependency graph and determine execution order
- Validate workflow input schema on `POST /invoke/{workflow}` — reject invalid input with 422
- Execute transform op chains directly (no Agent Runtime involvement)
- Dispatch webhook steps via HTTP POST, check 2xx, record result
- Dispatch agent invocations to the Agent Runtime via HTTP
- Run the Air-Lock validator on all step output
- Maintain all run and step state in the state store database
- Serve the envelope as a live query over the state store
- Enforce declared tool lists — agent invocations include the list of permitted tool server endpoints
- Enforce `cost_budget_usd` circuit breaker via the LLM Gateway
- Monitor Agent Runtime heartbeats and fail steps that go silent
- Act on reserved output fields (`ktsu_*`) from agent output before passing results downstream

### Agent Runtime

A long-running container (or horizontally scaled pool) that executes agent logic. It runs an async event loop (Node.js, Python asyncio, or similar) and handles many concurrent agent invocations simultaneously. Since agents are I/O-bound — they spend most of their time awaiting HTTP responses from tool servers and the LLM Gateway — a single runtime instance can serve dozens to hundreds of concurrent agent runs.

The Agent Runtime is a **generic, reusable image**. It has no workflow-specific code. What makes an invocation a specific agent is the payload the orchestrator sends: the prompt, input data, tool server endpoint URLs, sub-agent definitions, LLM Gateway URL, and output schema. The runtime executes the agent reasoning loop and returns the result.

The runtime is stateless between invocations. It holds no data about previous runs. All persistence is handled by tool calls (ktsu/kv, ktsu/blob, ktsu/memory) which write to the orchestrator's state store via their respective tool servers.

Transform steps and webhook steps do **not** execute on the Agent Runtime. They are executed directly by the orchestrator.

### Agent Runtime Heartbeat

The Agent Runtime maintains an internal set of active invocations (run_id + step_id pairs). A single background timer fires every 5 seconds and sends one batched HTTP POST to the orchestrator containing all currently active invocation IDs.

```
POST http://orchestrator:8080/heartbeat
{
  "runtime_id": "rt-1",
  "active": [
    { "run_id": "run_abc123", "step_id": "triage" },
    { "run_id": "run_abc123", "step_id": "legal-review" },
    { "run_id": "run_def456", "step_id": "inbound" }
  ]
}
```

The orchestrator bulk-updates `last_heartbeat_at` for all reported steps in a single DB write. If a step the orchestrator believes is running on a given runtime is absent from that runtime's heartbeat for longer than 15 seconds (3 missed beats), the orchestrator marks the step as `failed` with error `heartbeat_timeout` and proceeds with DAG resolution.

If a runtime stops heartbeating entirely, all steps the orchestrator dispatched to that runtime are failed after the 15-second threshold. This handles silent runtime crashes without waiting for per-step timeouts to expire.

The heartbeat is lightweight — even with 10 runtime instances each running 100 concurrent invocations, the orchestrator receives 10 HTTP requests every 5 seconds. The DB write is a single bulk update.

### Built-in Tool Servers

Built-in tool servers are first-party MCP servers shipped as standalone Docker images by Kimitsu. They are a distinct tier from user-provided tool servers — Kimitsu manages their lifecycle, they run on the internal network with well-known service names, and stateful built-in servers have a back-channel dependency on the orchestrator. They write to the orchestrator's state store via the orchestrator's internal HTTP API — the orchestrator remains the single writer to the database.

This back-channel is a meaningful architectural distinction. `ktsu/kv` and `ktsu/blob` are not independent MCP servers that happen to run nearby — they are part of the Kimitsu state surface. Each stateful built-in server requires `ORCHESTRATOR_URL` at startup and calls back in to write state. Stateless built-in servers (`ktsu/cli`, `ktsu/format`, `ktsu/validate`, `ktsu/transform`) have no orchestrator dependency and can be run without one.

### User-Provided Tool Servers

User-provided tool servers are any MCP server the operator builds or operates. They can be on the internal network or reachable over the internet. They have no dependency on the orchestrator — they are plain MCP servers. The tool server file declares the endpoint and auth; Kimitsu routes agent tool calls to it directly.

### LLM Gateway

A long-running container that is the sole outbound path to LLM providers. No other container holds LLM API keys or makes direct calls to LLM providers. All LLM calls from agents route through the LLM Gateway via HTTP. (Note: These are standard REST calls, not MCP).

The LLM Gateway is a **first-party Kimitsu implementation** — it is not a wrapper around any third-party proxy library. This is a deliberate decision: the gateway is a critical security and cost boundary, its contract with the orchestrator is narrow and well-defined, and external dependencies at this layer would compromise the auditability and independence that the rest of the architecture is built on. Third-party gateway projects (LiteLLM and others) informed the design, but no runtime dependency on them exists.

The LLM Gateway:
- Loads `gateway.yaml` at startup and owns all group and provider configuration
- Looks up the requested group and selects a model via the group's routing strategy
- Checks `restrictions.allow_from` on the group — rejects calls from unauthorized `step_id` values
- Enforces the `cost_budget_usd` circuit breaker per `run_id` — rejects calls when the run's budget is exhausted
- Translates the request to the provider's wire format via a per-provider adapter and makes the outbound call
- Returns token usage (`tokens_in`, `tokens_out`), cost, and the resolved `provider/model` string synchronously in every response
- Holds all LLM provider credentials — no other container has them

#### Internal HTTP Contract

The Agent Runtime calls the LLM Gateway using Kimitsu's internal invoke format. This is not an OpenAI-compatible API — it is a minimal, purpose-built contract between two first-party components.

```
POST /invoke
Content-Type: application/json

{
  "run_id":     "run_abc123",
  "step_id":    "triage",
  "group":      "standard",
  "max_tokens": 1024,
  "messages": [
    { "role": "system", "content": "..." },
    { "role": "user",   "content": "..." }
  ]
}
```

Response:

```json
{
  "content":        "...",
  "model_resolved": "anthropic/claude-sonnet-4-6",
  "tokens_in":      2104,
  "tokens_out":     512,
  "cost_usd":       0.0341
}
```

Error responses use standard HTTP status codes with a structured body:

```json
{ "error": "budget_exceeded",  "message": "Run run_abc123 has exhausted its cost budget." }
{ "error": "group_restricted", "message": "Step 'summarize' is not permitted to call group 'legal'." }
{ "error": "provider_error",   "message": "All models in group 'frontier' failed. Last error: ..." }
```

`run_id` and `step_id` are passed on every call. They are used for budget tracking and `restrictions.allow_from` enforcement respectively. Neither is optional.

#### Provider Adapters

Each provider type declared in `gateway.yaml` is backed by a small, self-contained adapter inside the gateway. The adapter's only job is to translate the internal invoke request into the provider's wire format and translate the response back. No adapter has knowledge of routing, budgets, or group configuration — those concerns live in the router layer above.

| Provider type | Wire format |
|---|---|
| `anthropic` | Anthropic Messages API |
| `openai` | OpenAI Chat Completions API |
| `openai-compat` | OpenAI Chat Completions API (arbitrary base URL) |
| `ollama` | Ollama `/api/chat` |
| `gemini` | Google Generative Language API |
| `cohere` | Cohere Chat API |

Adding a new provider requires writing one new adapter. No other gateway code changes.

#### Group Router

The group router sits between the invoke endpoint and the provider adapters. It receives a group name and a normalized request, and is responsible for:

1. Looking up the group in the loaded `gateway.yaml`
2. Applying the group's routing strategy to select a model
3. Dispatching to the correct provider adapter
4. Handling retries and fallback ordering on provider error or timeout
5. Returning the normalized response with usage metadata

Routing strategies map directly to the `gateway.yaml` declarations:

| Strategy | Behaviour |
|---|---|
| `single` | One model. Default when `models` has one entry. |
| `fallback` | Try in order. Move to next on timeout, error, or rate limit. |
| `round-robin` | Distribute calls evenly across all models in the group. |
| `least-latency` | Route to the model with the lowest rolling p50 latency. |

#### Budget Circuit Breaker

The gateway maintains a per-run cost accumulator in memory, updated after every successful invoke call. Before dispatching any call, the gateway checks the accumulator against the run's declared `cost_budget_usd`. If the budget is exhausted, the call is rejected with `budget_exceeded` before any outbound request is made.

The orchestrator communicates the budget for a run when it first dispatches an invocation for that run. The gateway holds no database connection — budget state is in-process memory, scoped to the lifetime of the run. When a run completes, the orchestrator notifies the gateway to release the accumulator.

This keeps the circuit breaker fast (no DB read per call) and the gateway stateless with respect to persistent storage.

### Network Zones

**Internal network.** A private container network connecting the orchestrator, Agent Runtime, LLM Gateway, and any built-in tool servers. No container on this network has outbound internet access by default. In Docker Compose this is a private bridge network. In ECS it is a task group with no public IP. In K8s it is a namespace with a default-deny egress NetworkPolicy.

**Egress.** Tool servers that need to reach external services are deployed with outbound access at the operator's discretion. The tool server file declares `egress: true` as a signal to operators. The LLM Gateway always has egress — it is the only Kimitsu container that talks to LLM providers.

### Scaling Model

| Tier | Scaling | Reason |
|---|---|---|
| Orchestrator | Single instance (HA pair for production) | Deterministic, low CPU — manages DAG state |
| Agent Runtime | Horizontal — 1 to N instances | I/O-bound event loop; add instances for concurrency |
| LLM Gateway | Horizontal — 1 to N instances | Proxies concurrent LLM calls; stateless |
| Built-in tool servers | Kimitsu-managed — one instance per server type | Stateless servers scale horizontally; stateful servers are single-instance by default |
| User-provided tool servers | Operator-managed | Kimitsu does not manage user tool server scaling |

---

## State Store

The orchestrator persists all run state in a relational database. The state store is the single source of truth for run history, step status, cost tracking, and built-in tool server data. The orchestrator is the only writer — no other container has database credentials.

### Supported Backends

- **SQLite** — default for development and single-node deployments. Zero configuration, file-based.
- **PostgreSQL** — recommended for production. Supports HA orchestrator pairs, external querying, and larger retention.

### Schema

#### `runs`

| Column | Type | Description |
|---|---|---|
| `run_id` | text PK | Unique run identifier |
| `workflow_name` | text | Workflow that owns this run |
| `workflow_version` | text | Semver of the workflow |
| `environment` | text | dev / staging / production |
| `status` | text | `running` \| `completed` \| `failed` |
| `started_at` | timestamp | When the run began |
| `completed_at` | timestamp | When the run finished (null if running) |
| `input` | jsonb | Validated workflow input from the invoke request body |
| `cost_usd` | decimal | Accumulated cost across all steps |
| `tokens_in` | integer | Accumulated input tokens |
| `tokens_out` | integer | Accumulated output tokens |

#### `steps`

| Column | Type | Description |
|---|---|---|
| `run_id` | text FK | References `runs.run_id` |
| `step_id` | text | Step identifier from the pipeline |
| `step_type` | text | `transform` \| `agent` \| `webhook` |
| `agent_name` | text | Agent name (null for transform and webhook steps) |
| `agent_version` | text | Agent semver |
| `runtime_id` | text | Which Agent Runtime instance executed this step (null for non-agent steps) |
| `status` | text | `pending` \| `running` \| `ok` \| `failed` \| `skipped` |
| `started_at` | timestamp | When the step began |
| `completed_at` | timestamp | When the step finished |
| `last_heartbeat_at` | timestamp | Last heartbeat received (agent steps only) |
| `output` | jsonb | Step output (null until complete) |
| `error` | text | Error message if failed |
| `model_group` | text | Group declared by the agent (null for non-agent steps) |
| `model_resolved` | text | Concrete `provider/model` string selected by the gateway (null for non-agent steps) |
| `tokens_in` | integer | Total input tokens (null for non-agent steps) |
| `tokens_out` | integer | Total output tokens (null for non-agent steps) |
| `cost_usd` | decimal | Total cost (null for non-agent steps) |
| `duration_ms` | integer | Wall-clock duration |
| `tool_calls` | integer | Number of MCP tool calls made (0 for non-agent steps) |
| `retries` | integer | Number of Air-Lock retries (0 for non-agent steps) |
| `ktsu_flags` | jsonb | Reserved output flags set by the agent (null for non-agent steps) |
| PK | | `(run_id, step_id)` |

#### `skill_calls`

One row per MCP tool call.

| Column | Type | Description |
|---|---|---|
| `call_id` | text PK | Unique call identifier |
| `run_id` | text FK | References `runs.run_id` |
| `step_id` | text | Which step initiated this call |
| `skill_name` | text | Skill that was called |
| `started_at` | timestamp | When the call began |
| `duration_ms` | integer | Call latency |
| `tokens_in` | integer | Input tokens (LLM calls only) |
| `tokens_out` | integer | Output tokens (LLM calls only) |
| `cost_usd` | decimal | Cost of this call |
| `error` | text | Error message if the call failed |

#### `kv_store`

| Column | Type | Description |
|---|---|---|
| `namespace` | text | Agent identity (step_id scoping) |
| `key` | text | User-defined key |
| `value` | jsonb | Stored value |
| `updated_at` | timestamp | Last write time |
| PK | | `(namespace, key)` |

#### `blob_store`

| Column | Type | Description |
|---|---|---|
| `namespace` | text | Agent identity (step_id scoping) |
| `key` | text | User-defined key |
| `content_type` | text | MIME type |
| `size_bytes` | integer | Content size |
| `value` | bytea | Inline content (small values) |
| `storage_path` | text | External storage path (large values, null if inline) |
| `updated_at` | timestamp | Last write time |
| PK | | `(namespace, key)` |

#### `log_entries`

| Column | Type | Description |
|---|---|---|
| `id` | serial PK | Auto-incrementing ID |
| `run_id` | text FK | References `runs.run_id` |
| `step_id` | text | Which step wrote this entry |
| `level` | text | `info` \| `warn` \| `error` |
| `message` | text | Log message |
| `data` | jsonb | Structured metadata |
| `created_at` | timestamp | When the entry was written |

### The Envelope is a Query

The envelope is not a static JSON file. When an agent calls `envelope-get-run`, the orchestrator queries the `runs` and `steps` tables for the current run_id and assembles the envelope JSON response on the fly. The envelope always reflects the latest state.

### Orchestrator Recovery

If the orchestrator crashes and restarts, it reads the state store to recover:

1. Find all runs with `status = 'running'`.
2. For each run, find steps with `status = 'running'`. These were in-flight when the crash occurred.
3. Check `last_heartbeat_at` on each running step. If stale (older than 15 seconds), mark as `failed` with error `orchestrator_recovery`.
4. For steps with `status = 'pending'`, re-evaluate DAG readiness and dispatch if upstream dependencies are satisfied.
5. Resume normal operation.

### Database Configuration

```yaml
# Dev — SQLite (default, zero config)
state_store:
  backend: sqlite
  path: "./data/ktsu.db"

# Production — PostgreSQL
state_store:
  backend: postgres
  connection: "env:DATABASE_URL"
  pool_size: 10
```

---

## Execution Model

### How a Pipeline Run Executes

```
1. Invoke arrives         Orchestrator receives POST /invoke/{workflow} with JSON body
2. Input validation       Orchestrator validates request body against workflow input.schema.
                          Failure → 422 with error detail, no run created.
3. Run created            Orchestrator creates runs row, generates run_id, pre-populates
                          stepOutputs["input"] with the validated workflow input.
4. DAG resolves           Orchestrator determines which steps are now unblocked
5. Fan-out                Agent steps dispatched to Agent Runtime;
                          transform steps executed directly by orchestrator;
                          webhook steps executed directly by orchestrator (HTTP POST)
6. Steps complete         Each step returns result → Air-Lock → reserved field processing → resolve next steps
7. Repeat                 Until all steps complete, fail, or skip
8. Envelope finalized     Orchestrator marks run complete in state store, fires log drain
```

### Agent Runtime Invocation Lifecycle

1. Orchestrator creates a `steps` row with `status = 'running'`, records `runtime_id`.
2. Orchestrator POSTs the invocation payload to an Agent Runtime instance. The payload includes permitted tool server endpoints and the access policy (allowlist) for each server.
3. Agent Runtime calls `tools/list` on each tool server and prunes the result against the server's allowlist. The agent's context is built from the pruned list only — the agent never sees tools it is not permitted to call.
4. Agent Runtime adds the invocation to its active set and starts the reasoning cycle.
5. Agent calls tool servers over MCP (HTTP/SSE) on the internal network. The Agent Runtime enforces the allowlist at call time as a second layer — a call to a tool not on the pruned list is blocked before it reaches the server and returns a structured `tool_not_permitted` error to the agent's reasoning loop.
6. Agent calls the LLM Gateway for reasoning. Gateway resolves the model, makes the outbound call, returns response with token metrics.
7. Agent Runtime heartbeat timer (every 5s) reports this invocation as active. Orchestrator updates `last_heartbeat_at`.
8. Agent assembles output. Agent Runtime POSTs result back to orchestrator, removes invocation from active set.
9. Orchestrator processes reserved output fields (`ktsu_*`). If any reserved field triggers a fatal condition, the run or step fails immediately.
10. Orchestrator runs Air-Lock. If valid, updates step to `status = 'ok'`, stores output, resolves next steps. If invalid and retries remain, re-dispatches to Agent Runtime with error appended.

### Step Status Values

| Status | Meaning |
|---|---|
| `pending` | Waiting for upstream dependencies |
| `running` | Dispatched and actively heartbeating (agent steps) or executing (orchestrator-run steps) |
| `ok` | Completed successfully, output validated by Air-Lock |
| `failed` | Failed — skill error, Air-Lock rejection after retries, timeout, heartbeat timeout, or reserved field condition |
| `skipped` | Never ran — upstream step was skipped via `ktsu_skip_reason`, or webhook condition evaluated false |

When a step is skipped, downstream steps that depend on it are also skipped. If a step fails, the run fails immediately and all remaining steps are skipped.

### The Envelope

```json
{
  "run_id":     "run_abc123",
  "workflow":   "support-triage",
  "workflow_v": "1.0.0",
  "started_at": "2026-03-14T09:11:44Z",

  "input": {
    "message": "I was charged twice for my subscription",
    "user_id": "U8821AB",
    "channel_id": "C04XZ99"
  },

  "steps": {
    "triage": {
      "status":       "ok",
      "completed_at": "2026-03-14T09:11:48Z",
      "ktsu_flags":    [],
      "metrics": {
        "model_group":    "standard",
        "model_resolved": "anthropic/claude-sonnet-4-6",
        "tokens_in":      2104,
        "tokens_out":     512,
        "cost_usd":       0.0341,
        "duration_ms":    3200,
        "tool_calls":     5,
        "retries":        0
      }
    },
    "legal-review": {
      "status": "failed",
      "error":  "Timeout after 30s",
      "metrics": {
        "model_group":    "standard",
        "model_resolved": "anthropic/claude-sonnet-4-6",
        "tokens_in":      null,
        "tokens_out":     null,
        "cost_usd":       null,
        "duration_ms":    30004,
        "tool_calls":     2,
        "retries":        0
      }
    }
  },

  "run_totals": {
    "cost_usd":      0.1062,
    "tokens_in":     5925,
    "tokens_out":    1024,
    "duration_ms":   30104,
    "steps_ok":      2,
    "steps_failed":  1,
    "steps_skipped": 0
  }
}
```

### Docker Compose Example (Dev)

```yaml
version: "3.8"

services:
  orchestrator:
    image: ktsu/orchestrator:latest
    ports:
      - "8080:8080"
    volumes:
      - ./workflows:/app/workflows
      - ./servers:/app/servers
      - ./agents:/app/agents
      - ./environments:/app/environments
      - ./gateway.yaml:/app/gateway.yaml
      - ktsu-data:/app/data
    environment:
      - KTSU_ENV=environments/dev.env.yaml

  agent-runtime:
    image: ktsu/agent-runtime:latest
    environment:
      - KTSU_ORCHESTRATOR_URL=http://orchestrator:8080
      - KTSU_GATEWAY_URL=http://llm-gateway:8081
      - HEARTBEAT_INTERVAL_S=5

  llm-gateway:
    image: ktsu/llm-gateway:latest
    environment:
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}

  ktsu-kv:
    image: ktsu/kv:latest
    environment:
      - KTSU_ORCHESTRATOR_URL=http://orchestrator:8080

  ktsu-log:
    image: ktsu/log:latest
    environment:
      - KTSU_ORCHESTRATOR_URL=http://orchestrator:8080

  ktsu-envelope:
    image: ktsu/envelope:latest
    environment:
      - KTSU_ORCHESTRATOR_URL=http://orchestrator:8080

volumes:
  ktsu-data:

networks:
  default:
    driver: bridge
```

---

## Failure Semantics

### Rules

- **Failure halts the run immediately.** If any step fails, the run is marked `failed` and all remaining steps are skipped. There is no `optional`, `allow_failed`, or continue-on-failure.
- **Skipped steps propagate.** A step whose only upstream dependency is skipped is also skipped. A skipped run is not a failed run.
- **Parallel branches run independently.** A sibling branch failure halts the run, but any branch already executing completes its current step before the run is torn down.
- **`cost_budget_usd` fails forward, not backward.** When the budget is hit, pending steps are marked `skipped: budget_exceeded`. Running work completes — nothing new starts.
- **Air-Lock failures are retryable on agent steps.** The orchestrator sends the error back to the Agent Runtime for correction, up to `retry.max` times.
- **Tool failures are fatal to the agent.** If any tool call fails within an agent's execution, the agent step fails.
- **Reserved output field conditions are evaluated before Air-Lock.** If `ktsu_injection_attempt: true` is set, the run fails immediately — Air-Lock is not reached.

---

*Revised from design session — March 2026*
