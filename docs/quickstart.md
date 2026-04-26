# Quickstart

New to ktsu? Start with the [minimal hello-world](./hello-world-minimal.md) first.

This guide walks through the built-in hello-world example to show a workflow running end to end.

**Prerequisites:** ktsu services must be running. See [Installation](./installation.md).

---

## The hello-world project

The hello-world example defines a two-step pipeline:

1. **greet** — an agent that receives `{name}` and returns a one-sentence greeting
2. **send** — a webhook step that posts the greeting to an external URL

---

## Project structure

```
examples/hello/
├── gateway.yaml                     # LLM provider registry (Anthropic)
├── gateway.local.yaml               # LLM provider registry (local Ollama)
├── workflows/
│   └── hello.workflow.yaml          # Pipeline definition
├── agents/
│   └── greeter.agent.yaml           # Greeter agent
└── servers/
    └── envelope.server.yaml         # Tool server for reading run state
```

---

## Start services with the example project

```bash
ktsu start --all --project-dir examples/hello
```

To use the local LLM variant instead:

```bash
ktsu start --all --project-dir examples/hello \
  --gateway-config examples/hello/gateway.local.yaml
```

---

## Invoke the workflow

```bash
ktsu invoke hello --input '{"name": "World"}' --wait
```

Example output:

```
run_id: 01HXYZ1234ABCDEF
status: complete
greeting: Hello, World! It's wonderful to meet you.
```

The `--wait` flag polls until the run reaches a terminal state and prints the result. Without it, `ktsu invoke` returns the `run_id` immediately so you can inspect the run separately.

---

## Inspect your runs

**List recent runs:**

```bash
ktsu runs
```

```
RUN ID                    WORKFLOW  STATUS    STARTED              DURATION
01HXYZ1234ABCDEF          hello     complete  2026-04-24 10:01:32  2s
```

Filter by workflow or status:

```bash
ktsu runs --workflow hello --status complete --limit 10
```

**Inspect a specific run:**

```bash
ktsu runs get 01HXYZ1234ABCDEF
```

This prints the full run envelope as JSON, showing every step's inputs and outputs:

```json
{
  "run_id": "01HXYZ1234ABCDEF",
  "workflow": "hello",
  "status": "complete",
  "params": { "name": "World" },
  "steps": {
    "greet": {
      "status": "complete",
      "output": { "greeting": "Hello, World! It's wonderful to meet you." }
    },
    "send": {
      "status": "complete",
      "output": {}
    }
  }
}
```

---

## Calling the orchestrator directly

The `ktsu` CLI is a thin wrapper around the orchestrator's HTTP API. You can use `curl` (or any HTTP client) to do the same things.

**Invoke a workflow:**

```bash
curl -s -X POST http://localhost:5050/invoke/hello \
  -H "Content-Type: application/json" \
  -d '{"name": "World"}'
```

```json
{ "run_id": "01HXYZ1234ABCDEF" }
```

**List runs** (supports `?workflow=`, `?status=`, `?limit=` query params):

```bash
curl -s http://localhost:5050/runs
curl -s "http://localhost:5050/runs?workflow=hello&status=complete&limit=10"
```

**Get run status:**

```bash
curl -s http://localhost:5050/runs/01HXYZ1234ABCDEF
```

**Get the full run envelope** (all step inputs and outputs):

```bash
curl -s http://localhost:5050/runs/01HXYZ1234ABCDEF/envelope
```

---

## What just happened

1. `ktsu invoke` sent `{"name": "World"}` to the orchestrator and received a `run_id`
2. The orchestrator ran the **greet** step: the runtime sent the envelope to the greeter agent, the LLM returned `{"greeting": "..."}`, and the Air-Lock validator confirmed the output matched the schema
3. The orchestrator ran the **send** step: it POST'd `{"greeting": "...", "name": "World"}` to the configured webhook URL
4. The run reached `complete` status and the final envelope was stored

---

## Next steps

- [Core Concepts](./concepts/pipeline-primitives.md) — understand steps, agents, and variable substitution
- [YAML Spec](./reference/yaml-spec/index.md) — full reference for workflow, agent, and server files
- [CLI Reference](./reference/cli.md) — all commands and flags
