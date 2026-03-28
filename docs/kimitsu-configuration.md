# Kimitsu — Configuration Reference
## Architecture & Design Reference — v4

---

## Project Structure

All Kimitsu files use a `kind` field to declare what they are.

```
kind: agent       → agents/*.agent.yaml
kind: workflow    → workflows/*.workflow.yaml
kind: env         → environments/*.env.yaml
kind: tool-server → servers/*.server.yaml (MCP HTTP/SSE)
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
    crm-lookup.server.yaml

  agents/
    triage.agent.yaml
    legal.agent.yaml
    consolidator.agent.yaml
    summarize.agent.yaml            # sub-agent

  environments/
    dev.env.yaml
    staging.env.yaml
    production.env.yaml

  fixtures/
    sample-request.json

  ktsu.lock.yaml                     # auto-generated — fully resolved dependency tree
```

---

## Workflow Compose Reference

The workflow file is the deployment manifest. It declares the pipeline of steps and references shared resources from the project. Model group definitions live in `gateway.yaml` — the workflow only declares a cost budget and optional group remapping.

```yaml
kind: workflow
name: "support-triage"
version: "1.2.0"

input:
  schema:
    type: object
    required: [message, user_id]
    properties:
      message:    { type: string }
      user_id:    { type: string }
      channel_id: { type: string }

pipeline:
  - id: parse
    agent: ktsu/secure-parser@1.0.0
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

  - id: notify-billing
    webhook:
      url: "env:SLACK_WEBHOOK_URL"
      method: POST
      body:
        text:    "consolidate.recommendation"
        channel: "input.channel_id"
      timeout_s: 10
    condition: "triage.category == 'billing'"
    depends_on: [consolidate]

model_policy:
  cost_budget_usd: 0.50
```

### Pipeline Step Fields

| Field | Description |
|---|---|
| `id` | Unique step identifier |
| `agent` | Path or built-in reference with pinned version |
| `transform` | Inline transform declaration |
| `webhook` | Inline webhook declaration — URL, method, body mapping, timeout |
| `for_each` | Fanout spec — see Pipeline Primitives. Fields: `from` (JMESPath to an array), `max_items` (optional cap), `concurrency` (optional parallel limit). Agent-only. |
| `condition` | JMESPath expression evaluated against step outputs at runtime. If falsy, the step is marked `skipped`. Valid on webhook steps. |
| `depends_on` | Array of step IDs this step waits for (not needed for transform steps — derived from inputs) |
| `consolidation` | Fan-in strategy: `array` \| `merge` \| `first` |
| `confidence_threshold` | Minimum `ktsu_confidence` value required for the step to proceed |
| `params` | Parameters for built-in agents (e.g. `ktsu/secure-parser`) |

### Step Failure Behaviour

If any step fails, the run fails immediately. There is no `on_fail`, `allow_failed`, or continue-on-failure option on pipeline steps. All downstream steps are skipped when a step fails.

### Consolidation

The `consolidation` field is accepted on pipeline steps but is not yet enforced at runtime. Do not rely on it for current behaviour.

---

## Environment Config Reference

Environment configs are selected at startup with `--env environments/dev.env.yaml`. They declare the state store backend and any environment-specific variable overrides.

### Format

```yaml
name: <string>         # environment name — dev, staging, production, etc.
variables:             # key-value pairs injected as environment variables
  KEY: value
state:
  driver: sqlite       # sqlite | postgres
  dsn: <string>        # SQLite: file path — Postgres: connection string or env:VAR
```

### Dev Environment

```yaml
name: dev
variables:
  OUTPUT_WEBHOOK_URL: http://localhost:9999/receive
state:
  driver: sqlite
  dsn: /tmp/myproject/kimitsu.db
```

### Production Environment

```yaml
name: production
variables: {}
state:
  driver: sqlite
  dsn: /var/data/kimitsu.db
```

---

## Gateway Config Reference

`gateway.yaml` is the project-level registry for all LLM providers and model groups. Agents reference groups by name — they have no knowledge of what providers or models back them.

### Format

```yaml
providers:
  - name: <string>     # logical name — referenced by models as "name/model-id"
    type: <string>     # anthropic | openai | openai-compat
    config:
      api_key: "env:<VAR_NAME>"    # API key — env: prefix resolves from environment

model_groups:
  - name: <string>     # group name agents declare in their model: field
    models:            # list of "provider-name/model-id" strings
      - <provider-name>/<model-id>
    strategy: <string> # round_robin | cost_optimized (default: round_robin)
    default_temperature: <float>   # optional
    pricing:           # optional — for cost tracking
      - model: <model-id>
        input_per_million: <float>
        output_per_million: <float>
```

### Full Example

```yaml
providers:
  - name: anthropic
    type: anthropic
    config:
      api_key: "env:ANTHROPIC_API_KEY"

  - name: openai
    type: openai
    config:
      api_key: "env:OPENAI_API_KEY"

model_groups:
  - name: economy
    models:
      - anthropic/claude-haiku-4-5-20251001
    strategy: round_robin

  - name: standard
    models:
      - anthropic/claude-sonnet-4-6
    strategy: round_robin

  - name: frontier
    models:
      - anthropic/claude-opus-4-6
    strategy: round_robin

  - name: vision
    models:
      - openai/gpt-4o
    strategy: round_robin
```

### Agent Model Declaration

In an agent file, `model:` is a string referencing a group name:

```yaml
model: standard
```

In a workflow pipeline step, `model:` is an optional override with group and max_tokens:

```yaml
model:
  group: economy
  max_tokens: 512
```

The agent file's `model:` field names the default group. The pipeline step's `model:` block overrides it. The gateway owns everything else — provider selection, routing strategy, and credentials.

### `group_map` — Environment Remapping

```yaml
# In model_policy on a workflow or via environment:
model_policy:
  cost_budget_usd: 0.25
  group_map:
    frontier: standard    # frontier agents use standard in this context
  force_group: local      # overrides ALL group declarations (useful for dev)
```

`force_group` overrides all group declarations. `group_map` selectively remaps named groups. Both appear in `model_policy` — never directly in agent files.

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
4.  lock            Write ktsu.lock.yaml with full resolved dependency tree
5.  check-dag       Topological sort on pipeline depends_on — fail on cycle
6.  check-agents    DFS across all agent sub-agent references — fail on cycle;
                    resolve server grants for each sub-agent against its parent;
                    validate allowlist intersections; fail on ungranted server
                    access, version mismatches, empty intersections, or near-match
                    URL warnings
7.  check-tools     Verify marketplace tool names exist in servers.yaml;
                    verify no sub-agent declares restricted built-in tool servers;
                    verify every tool name in each access.allowlist exists in the
                    server's declared tools interface — typos are boot errors;
                    verify allowlist entries are valid (exact name, prefix-*, or *
                    only — mid-string wildcards are boot errors);
                    verify access.allowlist and access.allowlist_env are not both
                    absent on servers marked sensitive (warning only)
8.  validate-io     Type-check input/output schemas at every boundary;
                    check reserved field types and confidence_threshold consistency;
                    verify workflow input.schema is a valid JSON Schema;
                    verify webhook condition expressions are valid JMESPath;
                    verify webhook body value expressions are valid JMESPath
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
  if built-in (ktsu/*):    → always valid
  if restricted built-in in a sub-agent: → fail

for each tool server file with access.allowlist:
  for each entry in allowlist:
    if not "*", not "prefix-*", not exact tool name: → fail (invalid wildcard)
    if exact tool name: → must exist in server's declared tools interface
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

- Any `ktsu_` prefixed output field not in the known reserved vocabulary is a boot error.
- Reserved field types are checked (`ktsu_confidence` must be `number`, `ktsu_flags` must be `array of string`, etc.).
- `confidence_threshold` on a pipeline step is only valid if the agent's output schema declares `ktsu_confidence`.

### Example Error Output

```
ERROR  Graph validation failed. Run aborted. No containers started.

  [1]  Pipeline cycle in workflows/support-triage.workflow.yaml
       Cycle path: triage -> legal-review -> consolidate -> triage

  [2]  Sub-agent cycle
       Cycle path: research-brief -> summarize -> research-brief
       Declared in: agents/summarize.agent.yaml@1.0.0

  [3]  Unknown reserved output field: "ktsu_custom_signal"
       Referenced in: agents/triage.agent.yaml

  [4]  confidence_threshold declared on step "triage" but agent output schema
       does not include ktsu_confidence.
       Fix: add ktsu_confidence to the agent's output schema.

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

  [10] Invalid webhook body expression
       Workflow: workflows/support-triage.workflow.yaml
       Step: notify-billing
       Body key "text" — expression "consolidate.recommendation" is not valid JMESPath.

WARNING  Possible server URL mismatch
         Sub-agent:   agents/summarize.agent.yaml
         Declared in: agents/triage.agent.yaml
         Sub-agent:   http://api.crm.internal/mcp
         Parent:      http://api.crm.internal/mcp/
         These may be the same server. Normalize URLs to confirm.
```

### Upgrading a Marketplace Tool Server

Update the version in `servers.yaml` and run `ktsu lock` to regenerate the lockfile. The validate-io check catches interface incompatibilities before the new version is adopted.

---

*Revised from design session — March 2026*
