# Overview

`ktsu` runs AI agent pipelines from YAML. It's built around knowing what your agents did, controlling what they can access, and keeping operations simple.

---

## Why ktsu

### Built for auditability

Every agent step declares what it produces. ktsu validates the output matches that declaration before passing it to the next step — a gate called the [Air-Lock](architecture/air-lock.md). Agents can also surface structured signals in their output: confidence scores, rationale, flags, and skip reasons. These show up in the run record and can be used for alerting, filtering, or human review.

Run state is persisted and inspectable. `ktsu runs get <id>` shows you exactly what each step produced.

### Built for safety

Secrets never touch agent prompts directly. You declare them in your environment config, pass them through the parameter chain, and ktsu enforces at startup that every secret is accounted for at every layer. Agents receive resolved values — they never see variable names or env references.

You control exactly which tools each agent can call via a per-agent allowlist. Sensitive tool calls can require human approval before execution; the pipeline pauses and waits.

### Easy to operate

Pipelines are plain YAML. `ktsu validate` checks all your config files before you run anything. The same workflow runs locally with `ktsu start --all` and in production with Docker Compose — no code changes, just environment config.

---

## How it works

A ktsu project is a directory of YAML files:

| File type | What it defines |
|---|---|
| `*.workflow.yaml` | An ordered pipeline of steps |
| `*.agent.yaml` | An LLM agent: its prompt, tool servers, and output schema |
| `*.server.yaml` | A connection to an MCP tool server |
| `gateway.yaml` | LLM providers and model groups |
| `*.env.yaml` | Environment-specific variables and secrets |

When you invoke a workflow, the **Orchestrator** resolves the step dependency graph, injects secrets, and dispatches each step. The **Agent Runtime** runs the LLM loop for agent steps. The **Gateway** normalizes LLM provider APIs so you can swap models without changing your agents.

---

## Get started

**1. [Install ktsu](installation.md)** — binary, Docker, or Docker Compose. Takes about 2 minutes.

**2. [Run the minimal hello-world](examples/hello-world-minimal.md)** — one agent, one workflow, no setup beyond a gateway config. Get something running before reading anything else.

**3. [Work through the quickstart](quickstart.md)** — a multi-step pipeline with tool servers, variable passing, and run inspection. This is the full picture of how ktsu works in practice.

---

## Core concepts

- [Pipeline primitives](concepts/pipeline-primitives.md) — the four step types: agent, transform, webhook, workflow
- [Variables & secrets](concepts/variables.md) — how data and secrets flow through a pipeline
- [Reserved outputs](concepts/reserved-outputs.md) — structured signals agents use to communicate confidence, flags, and human review requests
- [Fanout](concepts/fanout.md) — running a step in parallel over a list of inputs
