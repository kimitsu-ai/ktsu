# RSS — Configuration Reference
## Architecture & Design Reference — v3

---

## Project Structure

All RSS files use a `kind` field to declare what they are.

```
kind: agent       → agents/*.agent.yaml
kind: inlet       → inlets/*.inlet.yaml
kind: outlet      → outlets/*.outlet.yaml
kind: workflow    → workflows/*.workflow.yaml
kind: env         → environments/*.env.yaml
kind: tool-server → servers/*.server.yaml
```

### Standard Directory Layout

```
my-project/
  servers.yaml                      # marketplace dependency manifest
  gateway.yaml                      # provider registry and model group definitions

  workflows/
    support-triage.workflow.yaml
    onboarding.workflow.yaml

  servers/                          # local tool server files
    wiki-search.server.yaml
    slack-reply.server.yaml
    email-send.server.yaml

  agents/
    triage.agent.yaml
    legal.agent.yaml
    consolidator.agent.yaml
    summarize.agent.yaml            # sub-agent

  inlets/
    slack-webhook.inlet.yaml
    email-inbound.inlet.yaml
    daily-digest.inlet.yaml

  outlets/
    slack-responder.outlet.yaml
    email-reply.outlet.yaml

  environments/
    dev.env.yaml
    staging.env.yaml
    production.env.yaml

  fixtures/
    sample-request.json

  rss.lock.yaml                     # auto-generated — fully resolved dependency tree
```

---

## Workflow Compose Reference

The workflow file is the deployment manifest. It declares the pipeline of steps and references shared resources from the project. Model group definitions live in `gateway.yaml` — the workflow only declares a cost budget and optional group remapping.

```yaml
kind: workflow
name: "support-triage"
version: "1.2.0"

pipeline:
  - id: inbound
    inlets:
      - "./inlets/slack-webhook.inlet.yaml@1.0.0"
      - "./inlets/email-inbound.inlet.yaml@1.0.0"
      - "./inlets/workflow-inbound.inlet.yaml@1.0.0"

  - id: parse
    agent: rss/secure-parser@1.0.0
    depends_on: [inbound]
    params:
      source_field: message
      extract:
        intent: { type: string, enum: [billing, technical, legal, other] }
    model:
      group:      economy
      max_tokens: 512

  - id: triage
    agent: "./agents/triage.agent.yaml@1.3.0"
    depends_on: [parse]
    confidence_threshold: 0.7

  - id: legal-review
    agent: "./agents/legal.agent.yaml@2.0.0"
    depends_on: [triage]

  - id: risk-review
    agent: "./agents/risk.agent.yaml@1.1.0"
    depends_on: [triage]

  - id: merge-reviews
    transform:
      inputs:
        - from: legal-review
        - from: risk-review
      ops:
        - merge:       [legal-review, risk-review]
        - deduplicate: { field: ticket_id }
        - sort:        { field: confidence, order: desc }
    output:
      schema:
        type: array
        items:
          type: object

  - id: consolidate
    agent: "./agents/consolidator.agent.yaml@1.0.0"
    depends_on: [merge-reviews]

  - id: reply-slack
    outlet: "./outlets/slack-responder.outlet.yaml@1.0.0"
    condition: "envelope.inlet.context.source == 'slack'"
    depends_on: [consolidate]

  - id: reply-email
    outlet: "./outlets/email-reply.outlet.yaml@1.0.0"
    condition: "envelope.inlet.context.source == 'email'"
    depends_on: [consolidate]

  - id: reply-workflow
    outlet: "./outlets/workflow-callback.outlet.yaml@1.0.0"
    condition: "envelope.inlet.context.source == 'workflow'"
    depends_on: [consolidate]

model_policy:
  cost_budget_usd: 0.50
```

### Pipeline Step Fields

| Field | Description |
|---|---|
| `id` | Unique step identifier |
| `agent` | Path or built-in reference with pinned version |
| `inlet` | Path to a single inlet definition (first step only, mutually exclusive with `inlets`) |
| `inlets` | List of inlet paths — all must produce identical output schemas (first step only, mutually exclusive with `inlet`) |
| `outlet` | Path to an outlet definition (terminal step only) |
| `condition` | JMESPath expression evaluated against the envelope at runtime. If falsy, the outlet step is marked `skipped`. Declared on the pipeline step, not in the outlet file. |
| `transform` | Inline transform declaration |
| `depends_on` | Array of step IDs this step waits for (not needed for transform steps — derived from inputs) |
| `consolidation` | Fan-in strategy: `array` \| `merge` \| `first` |
| `confidence_threshold` | Minimum `rss_confidence` value required for the step to proceed |
| `params` | Parameters for built-in agents (e.g. `rss/secure-parser`) |

### Step Failure Behaviour

Failure tolerance is declared on the consuming agent via `inputs[].optional`, not on the pipeline step. There is no `on_fail` or `allow_failed` field on pipeline steps.

### Consolidation Strategies

| Strategy | Behaviour |
|---|---|
| `array` | Wrap all upstream outputs as a list under their step labels. **Default. Always safe.** |
| `merge` | Deep-merge all upstream outputs into a single object. Use only when outputs are additive and keys cannot conflict. |
| `first` | Take the first step to complete. For race/fallback patterns. |

---

## Environment Config Reference

Environment configs overlay workflow defaults without touching workflow or agent files. The active env is selected at invocation time: `rss run --env environments/staging.env.yaml`.

### Override Resolution Hierarchy (highest to lowest priority)

1. CLI flags (`--force-group`, `--budget`)
2. `environments/*.env.yaml`
3. `workflows/*.workflow.yaml`
4. `agents/*.agent.yaml`

### Dev Environment

```yaml
kind: env
environment: dev

model_policy:
  force_group:     local
  cost_budget_usd: 0.00

inlets:
  overrides:
    - id: inbound
      mock: "./fixtures/sample-request.json"

runtime:
  agent_runtime_instances: 1
  heartbeat_interval_s: 5
  heartbeat_timeout_s:  30

state_store:
  backend: sqlite
  path:    "./data/rss.db"

secrets:
  source: dotenv
  file:   ".env.dev"
```

### Staging Environment

```yaml
kind: env
environment: staging

model_policy:
  cost_budget_usd: 0.25
  group_map:
    frontier: standard

runtime:
  agent_runtime_instances: 2
  heartbeat_interval_s: 5
  heartbeat_timeout_s:  15

state_store:
  backend: postgres
  connection: "env:DATABASE_URL"
  pool_size: 5

secrets:
  source: aws_secrets_manager
  prefix: "rss/staging/"
```

### Production Environment

```yaml
kind: env
environment: production

model_policy:
  cost_budget_usd: 5.00

runtime:
  agent_runtime_instances: 5
  llm_gateway_instances:   2
  heartbeat_interval_s: 5
  heartbeat_timeout_s:  15

state_store:
  backend: postgres
  connection: "env:DATABASE_URL"
  pool_size: 20

secrets:
  source: aws_secrets_manager
  prefix: "rss/production/"

logging:
  level:            info
  drain:            "https://logs.internal/rss"
  include_envelope: true
```

---

## Gateway Config Reference

`gateway.yaml` is the project-level registry for all LLM providers and model groups. Agents reference groups by name — they have no knowledge of what providers or models back them.

### Provider Registry

```yaml
providers:
  anthropic:
    type:      anthropic
    base_url:  "https://api.anthropic.com/v1"
    auth:      "env:ANTHROPIC_API_KEY"
    timeout_s: 30

  openai:
    type:      openai
    base_url:  "https://api.openai.com/v1"
    auth:      "env:OPENAI_API_KEY"
    timeout_s: 30

  gemini:
    type:      gemini
    base_url:  "https://generativelanguage.googleapis.com"
    auth:      "env:GEMINI_API_KEY"
    timeout_s: 30

  local-ollama:
    type:      ollama
    base_url:  "http://host.docker.internal:11434"
    auth:      none
    timeout_s: 120

  local-vllm:
    type:      openai-compat
    base_url:  "http://vllm.internal:8000/v1"
    auth:      "env:VLLM_TOKEN"
    timeout_s: 60
```

Supported provider types: `anthropic`, `openai`, `openai-compat`, `ollama`, `gemini`, `cohere`.

### Model Groups

```yaml
groups:
  economy:
    models:
      - provider: anthropic
        model:    "claude-haiku-4-5"

  standard:
    models:
      - provider: anthropic
        model:    "claude-sonnet-4-6"

  frontier:
    models:
      - provider: anthropic
        model:    "claude-opus-4-6"

  local:
    models:
      - provider: local-ollama
        model:    "llama3.2:8b"

  local-with-fallback:
    routing: fallback
    models:
      - provider: local-ollama
        model:    "llama3.2:8b"
      - provider: anthropic
        model:    "claude-haiku-4-5"

  legal:
    models:
      - provider: anthropic
        model:    "claude-opus-4-6"
    restrictions:
      allow_from: [legal-review]

  vision:
    routing: round-robin
    models:
      - provider: openai
        model:    "gpt-4o"
      - provider: gemini
        model:    "gemini-2.0-flash"
```

### Routing Strategies

| Strategy | Behaviour |
|---|---|
| `single` | One model. Default when `models` has one entry. |
| `fallback` | Try in order. Move to next on timeout, error, or rate limit. |
| `round-robin` | Distribute calls evenly across all models in the group. |
| `least-latency` | Route to the fastest based on rolling p50 latency. |

### Group Restrictions

`restrictions.allow_from` locks a group to specific `step_id` values. Any other step attempting to use the group is rejected and the step fails.

```yaml
  legal:
    models:
      - provider: anthropic
        model:    "claude-opus-4-6"
    restrictions:
      allow_from: [legal-review, compliance-check]
```

### Agent Model Declaration

```yaml
model:
  group:      standard
  max_tokens: 1024
```

That is the complete agent model declaration. The agent declares intent — which group it needs and how many tokens — and the gateway owns everything else.

### `group_map` — Environment Remapping

```yaml
# staging.env.yaml
model_policy:
  cost_budget_usd: 0.25
  group_map:
    frontier: standard    # frontier agents use standard in staging

# dev.env.yaml
model_policy:
  force_group:     local  # all agents use local regardless of declaration
  cost_budget_usd: 0.00
```

`force_group` overrides all group declarations. `group_map` selectively remaps named groups. Both are env-file-only — they never appear in workflow or agent files.

---

## Model Policy & Cost

### Resolution Logic

1. Orchestrator reads the agent's `model.group`
2. Applies env `model_policy.force_group` if set
3. Applies env `model_policy.group_map` if the group name has a remapping entry
4. Includes resolved group name in the invocation payload
5. Agent Runtime passes group name and `step_id` to the LLM Gateway with each call
6. LLM Gateway looks up the group, checks `restrictions.allow_from`, selects a model, makes the provider call
7. If all models in the group fail, the step fails
8. LLM Gateway checks `cost_budget_usd` circuit breaker — rejects calls when budget is exhausted
9. LLM Gateway returns response with token metrics and resolved `provider/model` string
10. Orchestrator writes `model_group` and `model_resolved` to step metrics

`model_group` vs `model_resolved` in the envelope is your regression detector — if output quality drops after a deploy, check whether these fields diverged due to a `group_map` remapping.

### Cost Attribution

Every LLM call carries `run_id` and `step_id`. The LLM Gateway returns token usage and cost in every response. The Agent Runtime aggregates these across all LLM calls in an invocation — including sub-agents — and includes them in the result payload.

---

## Graph Validation & Boot Sequence

The orchestrator validates the full dependency graph at startup before any container is started. All validation errors are collected in a single pass and reported together.

### Boot Sequence

```
1.  parse           Load and validate all YAML files (kind + schema check)
2.  db-init         Connect to state store, run migrations
3.  resolve         Fetch marketplace tool servers declared in servers.yaml
4.  lock            Write rss.lock.yaml with full resolved dependency tree
5.  check-dag       Topological sort on pipeline depends_on — fail on cycle
6.  check-agents    DFS across all agent sub-agent references — fail on cycle;
                    resolve server grants for each sub-agent against its parent;
                    validate allowlist intersections; fail on ungranted server
                    access, version mismatches, empty intersections, or near-match
                    URL warnings
7.  check-tools     Verify marketplace tool names exist in servers.yaml;
                    verify no sub-agent declares restricted built-in tool servers;
                    verify inlets and outlets have no model/prompt/tools fields;
                    verify every tool name in each access.allowlist exists in the
                    server's declared tools interface — typos are boot errors;
                    verify allowlist entries are valid (exact name, prefix-*, or *
                    only — mid-string wildcards are boot errors);
                    verify access.allowlist and access.allowlist_env are not both
                    absent on servers marked sensitive (warning only)
8.  validate-io     Type-check input/output schemas at every boundary;
                    check reserved field types and confidence_threshold consistency;
                    if a step declares multiple inlets, verify all produce identical
                    output schemas — mismatched schemas are a boot error;
                    verify outlet condition expressions are valid JMESPath
9.  check-inputs    Verify agent inputs match depends_on — no undeclared dependencies
10. policy          Apply env model_policy, verify all declared groups exist in gateway.yaml
11. recover         Check for in-flight runs from a previous crash, resolve stale steps
12. start-builtins  Start built-in tool server containers, wait for health checks;
                    verify stateful servers can reach orchestrator back-channel
13. start-runtime   Start Agent Runtime instances, wait for health checks
14. start-gateway   Start LLM Gateway, verify provider connectivity
15. ready           All checks passed — begin accepting triggers
```

### Tool Server Validation (Step 7)

```
for each agent tool reference:
  if marketplace name:    → must exist in servers.yaml
  if local path:          → file must exist on disk
  if built-in (rss/*):    → always valid
  if restricted built-in in a sub-agent: → fail

for each tool server file with access.allowlist:
  for each entry in allowlist:
    if not "*", not "prefix-*", not exact tool name: → fail (invalid wildcard)
    if exact tool name: → must exist in server's declared tools interface

for each inlet/outlet:
  if model, prompt, or tools field present: → fail
```

### Sub-Agent Server Resolution (Step 6)

The DFS walk of the sub-agent graph does server grant resolution in the same pass. For each sub-agent encountered, the orchestrator resolves its effective server access against its parent's granted set:

```
for each sub-agent declared by a parent agent:
  for each server in sub-agent's declared tools:
    resolve endpoint URL (normalize trailing slashes, scheme)
    find matching endpoint in parent's granted server set
    
    if no match found:
      → ERROR: ungranted server access
    
    if match found but URLs differ after normalization:
      if URLs are near-matches (differ only by trailing slash, case):
        → WARNING: possible URL mismatch — normalize to confirm
      else:
        → ERROR: server version mismatch (different endpoint = different server)
    
    if match found:
      effective_allowlist = intersection(sub-agent allowlist, parent allowlist)
      if effective_allowlist is empty:
        → ERROR: empty intersection — sub-agent has no callable tools on this server
```

All errors are collected across the full DFS walk and reported together. The sub-agent never sees a narrowed allowlist at runtime — if the boot check passes, the resolved effective allowlist is what gets built into the invocation payload.

### Two Distinct Cycle Checks

**Check 1 — Pipeline DAG:** Topological sort on `depends_on` references.

**Check 2 — Sub-agent graph:** DFS across all agent `agents` references, walked recursively to detect circular sub-agent chains.

Both checks are fatal.

### Reserved Field Validation (Step 8)

- Any `rss_` prefixed output field not in the known reserved vocabulary is a boot error.
- Reserved field types are checked (`rss_confidence` must be `number`, `rss_flags` must be `array of string`, etc.).
- `confidence_threshold` on a pipeline step is only valid if the agent's output schema declares `rss_confidence`.

### Example Error Output

```
ERROR  Graph validation failed. Run aborted. No containers started.

  [1]  Pipeline cycle in workflows/support-triage.workflow.yaml
       Cycle path: triage -> legal-review -> consolidate -> triage

  [2]  Sub-agent cycle
       Cycle path: research-brief -> summarize -> research-brief
       Declared in: agents/summarize.agent.yaml@1.0.0

  [3]  Unknown reserved output field: "rss_custom_signal"
       Referenced in: agents/triage.agent.yaml

  [4]  confidence_threshold declared on step "triage" but agent output schema
       does not include rss_confidence.
       Fix: add rss_confidence to the agent's output schema.

  [5]  Sub-agent server access violation
       Sub-agent:   agents/summarize.agent.yaml
       Declared in: agents/triage.agent.yaml
       Server "crm" (http://api.crm.internal/mcp/v2) is not in the parent
       agent's granted server set. Add it to the parent's tool list or
       remove it from the sub-agent.

  [6]  Sub-agent server version mismatch
       Sub-agent:   agents/summarize.agent.yaml
       Declared in: agents/triage.agent.yaml
       Sub-agent references "crm" at http://api.crm.internal/mcp/v2
       Parent references "crm" at http://api.crm.internal/mcp/v1
       Sub-agents cannot access a server version not granted to the parent.
       Update the parent to use v2, or pin the sub-agent to v1.

  [7]  Sub-agent effective allowlist is empty
       Sub-agent:   agents/summarize.agent.yaml
       Declared in: agents/triage.agent.yaml
       Server "crm" — sub-agent allowlist ["crm-write-*"] does not intersect
       with parent allowlist ["crm-read-*"]. The sub-agent would have no
       callable tools on this server.

  [8]  Unknown tool name in access.allowlist
       Server file: servers/crm.server.yaml
       Entry: "crm-lokup"
       No tool named "crm-lokup" exists in this server's declared interface.
       Did you mean "crm-lookup"?

  [9]  Invalid allowlist entry
       Server file: servers/cli.server.yaml
       Entry: "jq-*-filter"
       Mid-string wildcards are not permitted. Use an exact name, a prefix
       wildcard (e.g. "jq-*"), or "*" to permit all tools.

WARNING  Possible server URL mismatch
         Sub-agent:   agents/summarize.agent.yaml
         Declared in: agents/triage.agent.yaml
         Sub-agent:   http://api.crm.internal/mcp
         Parent:      http://api.crm.internal/mcp/
         These may be the same server. Normalize URLs to confirm.
```

### Upgrading a Marketplace Tool Server

Update the version in `servers.yaml` and run `rss lock` to regenerate the lockfile. The validate-io check catches interface incompatibilities before the new version is adopted.

---

*Revised from design session — March 2026*
