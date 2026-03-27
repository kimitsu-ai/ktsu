# Kimitsu — Future Plans
## Roadmap & Design Notes — March 2026

---

## Current State (v1 — Implemented)

The following components are implemented and tested:

**Orchestrator**
- `POST /invoke/{workflow}` — validates required input fields (HTTP 422 on missing), generates run_id, starts DAG execution
- `GET /runs/{run_id}` — returns current run status and step summary
- `GET /envelope/{run_id}` — returns full run envelope with per-step outputs and aggregated metrics
- DAG resolution and step dispatch (agent, transform, webhook)
- Air-Lock output validation with agent retry
- Reserved output field processing (`ktsu_*`)
- Webhook step execution with JMESPath body mapping, `env:` URL and body value resolution, and condition evaluation
- Transform step execution (merge, sort, filter, map, flatten, deduplicate)
- Agent fanout (`for_each`) — bounded concurrency, `max_items` cap, `item`/`item_index` injection, metrics aggregation
- Per-run timeout via `model_policy.timeout_s`
- Agent Runtime dispatch (HTTP POST with callback)
- Token and cost metrics collected per step and aggregated into run totals
- Heartbeat monitoring with stale step detection
- State store (runs, steps, kv_store, blob_store, log_entries, skill_calls)

**Agent Runtime**
- Reasoning loop: LLM → tool calls → output
- MCP client with access policy enforcement (tool allowlist)
- Heartbeat reporting (batched, every 5s)
- Air-Lock error reinjection for retry

**LLM Gateway**
- Provider adapters: Anthropic, OpenAI, openai-compat, Ollama, Gemini, Cohere
- Group router with fallback, round-robin, least-latency strategies
- Budget circuit breaker per run_id
- `restrictions.allow_from` enforcement per group

**Built-in Tool Servers** (standalone, started via `kimitsu start <name>`)
- `ktsu/kv`, `ktsu/blob`, `ktsu/log`, `ktsu/memory`, `ktsu/envelope`
- `ktsu/format`, `ktsu/validate`, `ktsu/transform`, `ktsu/cli`

**Config & Boot**
- YAML loaders for workflow, agent, env, gateway, server manifest
- Graph validation at boot: DAG cycle check, sub-agent cycle check, allowlist validation, IO type-checking

---

## Near-term (v1.x)

These are small, targeted additions to the existing system. Each is self-contained.

### Persistent State Store Migration (SQLite → PostgreSQL)
The state store driver is already abstracted. Wire up the PostgreSQL driver and write migration tooling (`kimitsu db migrate`). SQLite remains the default for dev.

### `kimitsu invoke` CLI Command
Add `kimitsu invoke <workflow> --input '{"key":"value"}'` as a development convenience. Wraps `curl -X POST /invoke/{workflow}`. Optional `--wait` flag that polls `GET /runs/{run_id}` until completion and prints the final envelope.

### `kimitsu validate` Full Graph Check
The current `kimitsu validate` only checks env config. Extend it to run the full boot-time graph validation (DAG cycle check, IO type-checking, allowlist validation) without starting any services. Useful in CI.

---

## Medium-term (v2)

These require more significant design work or introduce new system-level concepts.

### Sub-Agent Invocations
Sub-agents are declared in the agent file but the runtime dispatch is not yet implemented. The Agent Runtime needs to support the `agent-invoke` built-in MCP tool, which creates a nested invocation payload and dispatches it synchronously (with its own heartbeat) within the parent agent's turn. Cost and token metrics roll up to the parent step.

### Streaming LLM Responses
The LLM Gateway currently buffers the full response before returning. Add streaming support: the gateway forwards chunks to the Agent Runtime as they arrive. The Agent Runtime assembles the stream for tool call detection but can begin yielding partial content earlier. This reduces time-to-first-token for agents with long outputs.

### `ktsu lock` Marketplace Resolution
`kimitsu lock` is currently a stub. Implement marketplace resolution: read `servers.yaml`, fetch server manifests from the registry, pin resolved versions to `ktsu.lock.yaml`. The lockfile becomes the authoritative source for all server endpoints during boot.

### Budget Enforcement at Gateway Level (Per-Group)
Add optional `cost_budget_usd` to individual model groups in `gateway.yaml`. This allows a shared gateway to enforce spending caps per group regardless of per-run budgets. Useful for protecting expensive groups (frontier, legal) from runaway workflows.

### Request/Response Audit Logging
Log every LLM Gateway request and response (minus API keys) to the state store's `audit_log` table. Include prompt, response, model_resolved, tokens, cost, and timestamp. Off by default — enabled via env config. Required for compliance in regulated environments.

### Scheduled Triggers (Cron)
Add a `schedules.yaml` to the project. The orchestrator reads it at boot and registers internal cron jobs. On tick, the orchestrator calls its own `POST /invoke/{workflow}` with a synthetic input (scheduled_at, run_number). No new primitive — scheduled runs are regular invocations with a system-generated input.

```yaml
# schedules.yaml
schedules:
  - workflow: daily-digest
    cron: "0 9 * * *"
    input:
      report_type: summary
```

### `ktsu/secure-parser` Built-in Agent
Implement the `ktsu/secure-parser` built-in agent described in the pipeline primitives doc. Parameterized by an `extract` schema, toolless, prompt-hardened. Reduces boilerplate for the common "first agent receives raw user input" pattern.

---

## Long-term (v3+)

These are directional. Design work has not started.

### Kubernetes Operator
A Kimitsu CRD operator for K8s. Workflows and agents become custom resources. The operator manages the orchestrator, runtime, and gateway deployments and handles scaling, upgrades, and configuration reloads. Targets teams running Kimitsu at scale with existing K8s infrastructure.

### Multi-tenant Isolation
Support multiple isolated projects on a single Kimitsu deployment. Each project has its own namespace in the state store, its own budget tracking, and its own set of permitted tool servers. The orchestrator enforces namespace boundaries. Targets SaaS use cases where multiple customer workflows share infrastructure.

### Advanced Routing Strategies
Extend the LLM Gateway group router with:
- **Cost-optimized routing**: route to cheapest model within latency budget
- **Quality-gated fallback**: route to a stronger model when the primary returns low `ktsu_confidence`
- **Shadow mode**: route to primary and secondary simultaneously, compare outputs, return primary — log divergence for analysis

### Multi-step Webhook Retry
Add declarative retry policy to webhook steps for cases where the destination is known to be flaky. The orchestrator retries with exponential backoff up to a configured limit. Requires idempotency key support on the webhook destination.

---

*March 2026*
