# Kimitsu — Overview & Philosophy

## Architecture & Design Reference — v4

> **Tools are pure functions. Agents are stateful processes.**
> **The pipeline is a declaration. The orchestrator is the kernel. The tool server is the atom.**
> **Every component boundary is an HTTP contract. The Kimitsu implementations are reference implementations, not the only valid ones.**

---

## Philosophy

Kimitsu is built on a single inversion of the conventional agentic model:

**Conventional model:** The agent is the atom. Tools and tool servers are accessories bolted on.

**Kimitsu model:** The tool is the atom. The agent is an emergent artifact of its tool composition.

This maps directly to how engineers already think about software:

| Kimitsu Concept | Software Analogy |
|---|---|
| Tool | Function / method |
| Tool server | Library / service |
| Agent | Application |
| Sub-agent | Internal service / module |
| Workflow | Deployment manifest |
| Orchestrator | Kernel / control plane |
| Agent Runtime | Worker pool / function runtime |
| LLM Gateway | Provider-normalizing proxy |
| Gateway Config | Provider registry & model group definitions |
| Environment config | Environment-specific overrides |

---

## Technical Foundations

### Variable Substitution & Templates

Kimitsu uses a consistent `{{ expr }}` syntax for dynamic values across all workflows and agents.

- **`{{ env.NAME }}`**: Reference environment variables (available in **Root Workflows** only).
- **`{{ params.NAME }}`**: Reference parameters passed into the workflow or agent.
- **`{{ step.ID.FIELD }}`**: Reference output from an upstream pipeline step.

> [!IMPORTANT]
> The old `env:VAR` and `param:NAME` syntax is no longer supported.

### Static Prompts

To optimize for **prompt caching**, Kimitsu enforces that `prompt.system` must be entirely static. All dynamic content, including parameter interpolation, must be placed in `prompt.user`.

### The Air-Lock

The **Air-Lock** is a middleware validator that runs inside the orchestrator. It ensures every step's output matches its declared `output.schema` before passing it to subsequent steps. If validation fails, the error is fed back into the agent's reasoning loop for potential retry.

---

## Core Concepts

### Pipeline Primitives

Every step in a Kimitsu pipeline is one of four types:

1.  **Agent**: Reasons, classifies, and synthesizer using an LLM. The only intelligent step type.
2.  **Transform**: Deterministically reshapes data using JMESPath operations.
3.  **Webhook**: POSTs data to external systems.
4.  **Workflow**: Invokes another workflow inline.

### Tool Servers as MCP

Every tool server is an independent process communicating via the **Model Context Protocol (MCP)** over HTTP/SSE. Kimitsu does not manage tool server lifecycles; it only connects to them based on the configuration in `.server.yaml` files.

### Secret Propagation

Kimitsu enforces strict end-to-end marking of secrets. For a credential to reach a tool server securely, it must be marked `secret: true` at every stage: from the workflow `env` declaration to the agent `params` and finally the server `params`. This ensures metadata is scrubbed from logs and the run envelope.

---

*Revised April 2026*
