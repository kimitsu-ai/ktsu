# Design: Unified params, env declarations, and workflow outputs

**Date:** 2026-04-15
**Status:** Approved

## Context

The current design has overlapping mechanisms for passing data into workflows — `input.schema`, `params.schema`, `env:VAR` and `param:NAME` prefix strings — with no clear rule for which to use. Subworkflow steps require two call-site blocks (`input:` + `params:`). Env vars are scattered opaque strings rather than declared explicitly. Step outputs share the envelope namespace with reserved keys, creating collision risk. Transforms have a special `inputs:` block that differs from all other step types. Workflows have no defined output mechanism when used as subworkflow steps.

The fix unifies everything: one schema (`params.schema`), explicit env declarations, a structured envelope (`env` / `params` / `step`), `{{ expr }}` expression syntax, and workflow output declarations. All steps use `depends_on` for ordering — no special source-declaration blocks.

---

## Design

### 1. Single schema: `params.schema` everywhere

`input.schema` is removed. All workflows — root and sub — declare only `params.schema`.

For **root workflows** (`visibility: root`), `params.schema` IS the API schema: the HTTP request body is validated against it on invoke (422 on failure).

For **sub-workflows** (`visibility: sub-workflow`), `params.schema` declares what the parent step must supply. `POST /invoke` returns 404.

No more distinction between "input" and "params" at the workflow level. A workflow has one interface.

### 2. Explicit `env` declaration in workflow files

Workflows declare the environment variables they use in a top-level `env:` array:

```yaml
env:
  - name: SLACK_WEBHOOK_URL
    secret: true
    description: "Slack incoming webhook URL"   # optional
  - name: DATABASE_URL
    secret: false
```

- `secret: true` — value is masked as `[hidden]` in logs, envelope inspection, and run output. Default: `false`.
- The runtime resolves declared env vars at run start and injects them into the envelope under the `env` key.
- Referencing an undeclared env var anywhere in the workflow is a boot error.

### 3. Pipeline envelope structure

The envelope is restructured into three namespaced top-level keys:

```json
{
  "env": {
    "SLACK_WEBHOOK_URL": "[hidden]",
    "DATABASE_URL": "postgres://..."
  },
  "params": {
    "message": "hello",
    "user_id": "u-123"
  },
  "step": {
    "parse":  { "intent": "billing" },
    "triage": { "category": "billing", "priority": "high" }
  }
}
```

| Key | Contents |
|---|---|
| `env` | Resolved env var values; secrets shown as `[hidden]` |
| `params` | Resolved workflow params (HTTP body for root, parent step for sub) |
| `step` | All step outputs keyed by step ID |

A step ID that collides with `env`, `params`, or `step` is a boot error.

### 4. `{{ expr }}` expression syntax

All value references use `{{ expr }}` where the expression is evaluated as a path into the envelope. Plain strings are always literals — no backtick syntax needed.

| Value | Resolves to |
|---|---|
| `"support-bot"` | Literal string `support-bot` |
| `"{{ env.SLACK_WEBHOOK_URL }}"` | Env var value |
| `"{{ params.user_id }}"` | Workflow param value |
| `"{{ step.parse.channel_id }}"` | Step output field |
| `"{{ step.triage.category }}"` | Step output field |

**Type preservation:** When the entire YAML string value is a single `{{ expr }}`, the result is the resolved type (object, array, number, bool). When `{{ expr }}` appears within a larger string (e.g. `"Hello {{ params.name }}"`) the result is always a string.

This syntax applies to:
- Step `params:` block values
- Webhook `body:` values
- Workflow `output.map` values
- Agent `prompt.system` and `reflect` text (interpolation into prose)

`expr:` fields inside transform ops and `condition:` on steps remain bare JMESPath — they are always expression contexts and do not use `{{ }}`.

**Workflow step example (before → after):**
```yaml
# Before
- id: notify
  workflow: ktsu/slack-reply
  params:
    webhook_url: "env:SLACK_WEBHOOK_URL"
    username:   "`support-bot`"
  input:
    channel_id: "parse.channel_id"
    text:       "agent.reply"

# After
- id: notify
  workflow: ktsu/slack-reply
  params:
    webhook_url: "{{ env.SLACK_WEBHOOK_URL }}"
    username:   "support-bot"
    channel_id: "{{ step.parse.channel_id }}"
    text:       "{{ step.agent.reply }}"
```

### 5. Transforms use `depends_on` like every other step

The `transform.inputs` block is removed entirely. Transforms now use `depends_on` for execution ordering — the same as agent, webhook, and workflow steps. Since the full envelope is always available, a transform can reference any prior step output in its ops without declaring it as a source.

Transform ops reference step outputs as `step.id` or `step.id.field`:

```yaml
- id: merge-reviews
  depends_on: [legal-review, risk-review]
  transform:
    ops:
      - merge: [step.legal-review, step.risk-review]
      - filter: { expr: "confidence > 0.7" }
      - sort:   { field: confidence, order: desc }
  output:
    schema:
      type: array
      items: { type: object }
```

`depends_on` may be auto-derived from `step.id` references in ops, but explicit `depends_on` is required when the dependency isn't referenced in ops.

### 6. Workflow output declaration

Workflows declare `output:` to specify what they return to the parent pipeline as a subworkflow step. Without `output:`, the step produces no usable output in the parent.

Two mutually exclusive forms:

**`from` (step reference):** The named step's full output becomes the workflow's return value.
```yaml
output:
  from: triage
  schema:
    type: object
    required: [category, priority]
    properties:
      category: { type: string }
      priority: { type: string }
```

**`map` (field mapping):** JMESPath projections from any internal steps compose the return value.
```yaml
output:
  schema:
    type: object
    properties:
      summary: { type: string }
      tickets: { type: array }
  map:
    summary: "{{ step.triage.summary }}"
    tickets: "{{ step.enrich.results }}"
```

- `from` and `map` are mutually exclusive — both present is a boot error.
- `output.schema` is required when `output:` is declared; Air-Lock validated before the parent reads the result.
- All steps in the subworkflow — including webhook steps — must complete before output is assembled and returned. Webhooks are normal steps and their completion is required before Air-Lock runs.
- Referenced in parent as `step.<workflow-step-id>.<field>` (e.g. `step.notify.category`).

---

## Breaking Changes Summary

| Old | New |
|---|---|
| `input.schema` on workflow | Removed — use `params.schema` for all workflows |
| `input:` block on workflow step | Removed — everything in `params:` |
| `env:VAR_NAME` in value strings | `"{{ env.VAR_NAME }}"` |
| `param:name` in value strings | `"{{ params.name }}"` |
| `` `literal` `` backtick strings | Plain string `"literal"` |
| `steps.parse.field` / `parse.field` | `"{{ step.parse.field }}"` |
| `input.field` in JMESPath / prompts | `"{{ params.field }}"` |
| `{{param_name}}` in prompts | `{{ params.name }}` |
| `transform.inputs: [{from: x}]` | Removed — use `depends_on: [x]` |
| Step output ref in transform ops (bare `step-id`) | `step.id` / `step.id.field` |

---

## Files to Change

| File | Change |
|---|---|
| `docs/yaml-spec/workflow.md` | Remove `input.schema`; `params.schema` as API schema for root; add `env:` section; update all value syntax to `{{ expr }}`; remove `input:` from workflow step; remove `transform.inputs`; add `output:` section; restructure envelope docs |
| `docs/yaml-spec/agent.md` | Update interpolation to `{{ params.x }}`, `{{ step.x.y }}`, `{{ env.X }}`; update input envelope example |
| `docs/yaml-spec/server.md` | Remove `param:name` reference; verify `auth` field alignment |
| `docs/kimitsu-pipeline-primitives.md` | Update all descriptions; new envelope structure; workflow output section |
| `docs/kimitsu-invariants.md` | Update for: single `params.schema`, `env` declaration, `step.*` namespace, no `transform.inputs` |
| `docs/kimitsu-runtime.md` | Update: env injection, params-as-API-schema for root, envelope construction, workflow output resolution |

---

## Verification

- Root workflow: `params.schema` validated against HTTP body (422 on failure); `visibility: root`
- Sub-workflow: no `input.schema`; `POST /invoke` returns 404
- Workflow step with `input:` block → boot error
- Workflow step with `env:VAR` or `param:name` string → boot error
- Step named `env`, `params`, or `step` → boot error
- Reference to undeclared env var → boot error
- `output.from` and `output.map` both present → boot error
- `output.schema` Air-Lock validated after all subworkflow steps complete
- All steps including webhooks complete before output is returned to parent
- `{{ expr }}` whole-value expression preserves type; mixed string coerces to string
- Transform `depends_on` auto-derived from `step.x` refs in ops when not explicit
- Secret env vars appear as `[hidden]` in envelope, logs, and run output
