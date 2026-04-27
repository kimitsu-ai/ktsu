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
curl -s -X POST http://localhost:5050/invoke/hello \
  -H "Content-Type: application/json" \
  -d '{"name": "World"}'
```

```json
{ "run_id": "01HXYZ1234ABCDEF" }
```

Poll until the run completes:

```bash
curl -s http://localhost:5050/runs/01HXYZ1234ABCDEF
```

```json
{
  "run_id": "01HXYZ1234ABCDEF",
  "workflow": "hello",
  "status": "complete"
}
```

If you have the `ktsu` CLI installed, it wraps the same HTTP calls and adds `--wait` polling:

```bash
ktsu invoke hello --input '{"name": "World"}' --wait
```

---

## Inspect your runs

**List recent runs** (supports `?workflow=`, `?status=`, `?limit=` query params):

```bash
curl -s http://localhost:5050/runs
curl -s "http://localhost:5050/runs?workflow=hello&status=complete&limit=10"
```

Or with the CLI:

```bash
ktsu runs
ktsu runs --workflow hello --status complete --limit 10
```

**Get the full run envelope** (all step inputs and outputs):

```bash
curl -s http://localhost:5050/runs/01HXYZ1234ABCDEF/envelope
```

Or with the CLI:

```bash
ktsu runs get 01HXYZ1234ABCDEF
```

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

## What just happened

1. The invoke call sent `{"name": "World"}` to the orchestrator and received a `run_id`
2. The orchestrator ran the **greet** step: the runtime sent the envelope to the greeter agent, the LLM returned `{"greeting": "..."}`, and the Air-Lock validator confirmed the output matched the schema
3. The orchestrator ran the **send** step: it POST'd `{"greeting": "...", "name": "World"}` to the configured webhook URL
4. The run reached `complete` status and the final envelope was stored

---

## Next steps

- [Core Concepts](./concepts/pipeline-primitives.md) — understand steps, agents, and variable substitution
- [YAML Spec](./reference/yaml-spec/index.md) — full reference for workflow, agent, and server files
- [CLI Reference](./reference/cli.md) — all commands and flags
