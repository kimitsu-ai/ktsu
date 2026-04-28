# Hello World (Minimal)

The absolute minimum ktsu project: one workflow, one agent, no tool servers, no webhooks.

Ready for a more complete example? See [Quickstart](../quickstart.md), which adds a webhook step and shows run inspection.

**Prerequisites:** ktsu services must be running. See [Installation](../installation.md).

---

## Project structure

```
my-project/
├── gateway.yaml
├── workflows/
│   └── hello.workflow.yaml
└── agents/
    └── greeter.agent.yaml
```

That's it. No `servers/`, no `environments/`.

---

## gateway.yaml

Tells ktsu which LLM provider to use.

```yaml
env:
  - name: ANTHROPIC_API_KEY
    secret: true

providers:
  - name: anthropic
    type: anthropic
    config:
      api_key: "{{ env.ANTHROPIC_API_KEY }}"

model_groups:
  - name: standard
    models:
      - anthropic/claude-sonnet-4-5
    strategy: round_robin
```

---

## workflows/hello.workflow.yaml

A single-step pipeline that calls the greeter agent.

```yaml
kind: workflow
name: hello
version: "1.0.0"
visibility: root

params:
  schema:
    type: object
    required: [name]
    properties:
      name: { type: string, description: "Name to greet" }

pipeline:
  - id: greet
    agent: "./agents/greeter.agent.yaml"
    params:
      name: "{{ params.name }}"
```

---

## agents/greeter.agent.yaml

A minimal agent: a prompt and an output schema. No tool servers.

```yaml
name: greeter
model: standard

params:
  schema:
    type: object
    required: [name]
    properties:
      name: { type: string }

prompt:
  system: |
    You are a friendly greeter. Reply with exactly one sentence.
  user: |
    Greet {{ params.name }}.

output:
  schema:
    type: object
    required: [greeting]
    properties:
      greeting: { type: string }
```

---

## Run it

Start services pointing at your project directory:

```bash
ktsu start --all --project-dir my-project
```

Invoke the workflow:

```bash
ktsu invoke hello --input '{"name": "World"}' --wait
```

Example output:

```
run_id: 01HXYZ1234ABCDEF
status: complete
greeting: Hello, World! Great to meet you.
```

---

## What happened

1. The orchestrator received `{"name": "World"}` and started a run.
2. It dispatched the `greet` step to the greeter agent.
3. The agent sent the rendered prompt to the LLM and received `{"greeting": "..."}`.
4. The Air-Lock validator confirmed the output matched the schema.
5. The run reached `complete` and the result was stored.

---

Ready for more? [Quickstart](../quickstart.md) adds a webhook step, shows run inspection, and demonstrates the `ktsu runs` command.
