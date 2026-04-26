# Troubleshooting

A debugging guide for common problems when running ktsu workflows.

---

## Bootstrap failures

### Missing env vars

Secret agent params must use an `env:` source. If a secret param is wired to a plain string or the referenced env var is absent at startup, the orchestrator fails the run before the first step executes:

```
agent param "api_key" is secret and must use an env: source
```

```
agent param "api_key": env var "OPENAI_API_KEY" is not set
```

Check the chain: the workflow step's `params` block must pass `env:MY_VAR` for every param declared `secret: true` in the agent schema.

### Schema validation errors

Run `ktsu validate` against your project directory before invoking:

```bash
ktsu validate ./my-project
```

Common errors:

- **YAML parse failure** — a tab instead of spaces, or an unquoted colon in a string value. The error message includes the file path and line number from the YAML parser.
- **Unknown field** — a misspelled top-level key (`pipline` instead of `pipeline`). The YAML library surfaces the offending field name.
- **depends_on references an unknown step** — `ktsu validate` checks the DAG and reports `step "X" depends on unknown step "Y"`.
- **Missing required field in agent/server schema** — an agent ref in the workflow supplies a param that the agent's `params.schema` does not declare, or omits one that is required.

Fix all reported errors before invoking. A failed `ktsu validate` will produce a non-zero exit code and a summary grouped by workflows, agents, and servers.

### Service not reachable

`ktsu invoke` communicates with the orchestrator over HTTP. If the orchestrator is not running, the CLI will fail with a connection error. Ensure all three services are up:

```bash
ktsu start --all --project-dir ./my-project
```

Verify the orchestrator is listening (default port 5050):

```bash
curl -s http://localhost:5050/runs
```

A `connection refused` response means the orchestrator process is not running. Check the terminal where `ktsu start` was launched for startup errors.

---

## Step failures

### Inspecting a failed run

```bash
ktsu runs get <run-id>
```

The JSON envelope shows each step's `status`, `output`, and `error`:

```json
{
  "run_id": "01HXYZ...",
  "status": "failed",
  "steps": {
    "triage": {
      "status": "failed",
      "error": "output missing required field \"severity\""
    }
  }
}
```

Key fields to check:

- `status` at the run level — `failed` means at least one step failed.
- `status` at the step level — `failed`, `complete`, or `skipped`.
- `error` at the step level — the orchestrator's error string for that step.
- `output` at the step level — the raw output the agent returned before failure.

### Reading `ktsu_flags` and `ktsu_rationale`

Agents may include soft diagnostic fields in their output:

- `ktsu_rationale` — the agent's prose explanation of its reasoning. No pipeline effect; useful for debugging unexpected outputs.
- `ktsu_flags` — string labels the agent applied for observability. No pipeline effect.

These appear in the step's `output` block in the run envelope. If a step produced unexpected results, check `ktsu_rationale` first — it often explains what the agent considered.

### Orchestrator errors vs. agent errors

- **Orchestrator errors** appear at the run level (`run.error`) or at the step level with an error string that does not come from the LLM — for example, a failed env var resolution, an Air-Lock schema violation, or a tool call routing failure.
- **Agent errors** surface in the step `output` via reserved fields such as `ktsu_low_quality: true` or `ktsu_needs_human: true`, and in `ktsu_rationale`. The step will be marked `failed` by the orchestrator once it interprets these signals.

---

## Secret propagation errors

### Secret param has no `env:` source

```
agent param "api_key" is secret and must use an env: source
```

The workflow step is passing a literal string or a `{{ step.X.value }}` expression for a param declared `secret: true`. Secret params must be sourced exclusively from environment variables using the `env:VAR_NAME` syntax.

### Server param references a non-secret agent param

```
server param "token" is secret but agent param "auth_token" is not marked secret
```

A server's `params` block declares a param as `secret: true`, but the agent param supplying it via `{{ params.auth_token }}` is not itself marked secret. Mark the agent param `secret: true` in its schema.

### Tracing the full chain

Follow this path when debugging secret resolution:

1. **env.yaml** — declares which env vars are available to the workflow.
2. **workflow params** — the `params` block in the step wires `env:VAR` to the agent param.
3. **agent params** — the agent's `params.schema` declares the param and whether it is `secret`.
4. **server params** — the agent's `servers[].params` block wires `{{ params.agent_param }}` to the server param.

Each link must be consistent: if the server param is secret, the agent param must be secret, and the workflow step must source it from `env:`.

---

## Tool access denials

When an agent requests a tool that is not in its allowlist — either because the tool was not listed or the LLM hallucinated a tool name — the runtime fails the step immediately:

```
tool_not_permitted: delete-record
```

The step status will be `failed` and this string will appear in the step's `error` field.

To add the tool, find the relevant `servers` entry in the agent file and add the tool name to the `access.allowlist`:

```yaml
servers:
  - name: my-db
    path: ./servers/db.server.yaml
    access:
      allowlist:
        - delete-record
```

Glob patterns are supported — `delete-*` will match any tool whose name starts with `delete-`.

---

## Air-Lock validation failures

Air-Lock runs after every agent step. It checks that the agent's output contains all fields declared as `required` in the agent's `output.schema`. If a field is missing:

```
output missing required field "severity"
```

If the agent sets a `ktsu_` prefixed field that is not a recognized reserved field, Air-Lock rejects it:

```
output contains reserved field "ktsu_custom"
```

To fix a schema mismatch:

1. Confirm the agent's system prompt instructs it to return all required fields.
2. Check the `output.schema.required` list in the agent YAML — remove fields the agent genuinely cannot always produce, or update the prompt to ensure they are always present.
3. Re-run `ktsu validate` after editing the agent file to catch any schema syntax errors.

---

## `ktsu_needs_human` pauses

When an agent sets `ktsu_needs_human: true` in its output, the orchestrator fails the run with the error `needs_human_review`. The run reaches a terminal `failed` state.

This is distinct from tool-level human approval (`require_approval` in the allowlist). `ktsu_needs_human` is a signal from the agent that the entire case exceeds its confidence or authorization — it is not a pause-and-resume mechanism.

To check whether a run stopped for this reason, inspect the step output:

```bash
ktsu runs get <run-id>
```

Look for `"ktsu_needs_human": true` in the failing step's `output` block.

**Tool approval pauses** (a step in `pending_approval` status) are separate. When a tool call requires human approval, the step status is `pending_approval`. Resume or reject via the HTTP API:

```bash
# Approve
curl -s -X POST http://localhost:5050/runs/<run-id>/steps/<step-id>/approval/decide \
  -H "Content-Type: application/json" \
  -d '{"decision": "approved"}'

# Reject
curl -s -X POST http://localhost:5050/runs/<run-id>/steps/<step-id>/approval/decide \
  -H "Content-Type: application/json" \
  -d '{"decision": "rejected"}'
```

Check the current approval state:

```bash
curl -s http://localhost:5050/runs/<run-id>/steps/<step-id>/approval
```

---

*Revised April 2026*
