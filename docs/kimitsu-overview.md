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
| LLM Gateway | Provider-normalizing proxy (first-party) |
| Gateway Config | Provider registry & model group definitions |
| Environment config | Environment-specific override |

The orchestrator is the kernel — deterministic, cheap, zero LLM calls. All intelligence lives in agent processes and tool servers where it creates value and can be metered precisely.

### Tool-Driven Development

Tools are the first-class development primitive. An agent has no identity beyond what tools it carries. Two agents with identical tool sets *are* the same agent. The prompt is not the agent — it is the routing logic that decides how tools get invoked. Version the tool server. The agent version follows automatically.

### Modular by Design

Every component in Kimitsu — the orchestrator, Agent Runtime, LLM Gateway, and all tool servers — communicates exclusively over documented HTTP APIs. No component assumes it is talking to the Kimitsu implementation of any peer. The Agent Runtime does not care whether the orchestrator is the Kimitsu orchestrator or a conforming replacement. The LLM Gateway is swappable for any proxy that speaks the same contract.

This is a deliberate architectural commitment, not a side effect of the HTTP choice:

- The orchestrator's HTTP API (invocation dispatch, heartbeat endpoint, Air-Lock, state writes) is a first-class documented contract.
- The Agent Runtime invocation payload format is a spec. What the orchestrator sends defines what any conforming orchestrator must send.
- The LLM Gateway HTTP contract is a replaceable surface — a team that needs to swap in a different proxy can do so. The Kimitsu LLM Gateway is a first-party implementation of that contract, not a wrapper around any external library. It was designed with third-party gateway projects as reference points, but carries no runtime dependency on them.
- Tool servers are already fully independent — MCP over HTTP, no Kimitsu coupling whatsoever.

The Kimitsu implementations are **reference implementations**. They are the defaults, they are supported, and they are what most teams will run. But a team that needs to replace the orchestrator with a Temporal-backed implementation, or the LLM Gateway with an internal proxy, or the Agent Runtime with a serverless function executor, should be able to do so without forking Kimitsu or losing compatibility with the YAML pipeline definitions.

Well-documented boundaries make this possible. When in doubt, document the contract before writing the implementation.

---

## Core Concepts

### The Three Pipeline Primitives

Every step in a Kimitsu pipeline is exactly one of three things:

| Primitive | LLM | Tools | Role |
|---|---|---|---|
| **Transform** | Never | Never | Reshapes data between steps via deterministic ops |
| **Agent** | Always | Optional | Reasons, classifies, synthesizes — the only LLM-bearing step type |
| **Webhook** | Never | Never | POSTs pipeline data to an external URL; expects 200 for success |

Nothing else. If logic requires reasoning about content, it is an agent. If it is pure data shaping, it is a transform. If it needs to call out to an external system, it is a webhook. There is no fourth option and no escape hatch.

### Everything is HTTP

The Kimitsu invoke API is a standard HTTP endpoint. Any service, script, cron job, or language that can make an HTTP POST can trigger a workflow. The orchestrator does not own or prescribe how triggers are built — it only validates the shape of the data that arrives and the shape of the data that leaves.

- **To start a run:** `POST /invoke/{workflow}` with a JSON body
- **To receive pipeline output:** declare a `webhook` step that POSTs to your endpoint

This is the entirety of the integration surface.

### Tools are Pure Functions

- No persistent state between calls
- Same input always produces the same output
- Safe to call from multiple agents simultaneously
- The tool server starts once, serves many calls, remembers nothing
- Run as long-lived MCP servers hosted externally — Kimitsu does not manage their runtime

### Tool Servers are External MCP Servers

Every tool server is an MCP server deployed and operated independently of Kimitsu. The tool server file is a pointer: it declares where the server lives, how to authenticate, and what the interface contract is. Kimitsu does not care what language the server is written in, how it is packaged, or how it is deployed. A single MCP server can expose multiple tools — grouping related tools into one server is a packaging decision made by the server author.

### Agents are Stateful Processes

- Executed as functions on the Agent Runtime worker pool
- Orchestrate tool calls via reasoning prompt
- Produce typed output returned to the orchestrator via HTTP
- Are the only callers permitted to use storage and context tools
- The output is validated by the Air-Lock before downstream agents can read it

### Sub-Agents

A sub-agent is a full agent invoked by a parent agent as a tool call rather than as a pipeline step. Sub-agents appear in the parent agent's `agents` field and are available to the parent as a built-in `agent-invoke` tool over MCP. They have their own prompt, tools, model declaration, and typed output schema validated by the Air-Lock. Sub-agents are the replacement for what was previously called "prompt skills" — any LLM-backed reasoning task that the parent agent needs to delegate is modelled as a sub-agent.

Sub-agents do not appear in the pipeline DAG. They are private implementation details of the agent that declares them. Their cost and token usage roll up to the parent step's metrics.

### The Air-Lock

A middleware validator running inside the orchestrator. It validates every step's output against the declared `output.schema` before making the output available to downstream steps. Bad data never reaches the next step.

When validation fails on an agent step, the Air-Lock returns the validation error to the Agent Runtime, which reinjects the error into the agent's reasoning loop. The agent may retry up to the configured `retry.max` limit (default: 0). If all retries are exhausted, the step fails. Transform and webhook steps do not retry — they fail immediately on bad output.

### MCP as the Singular Interface

Every tool server presents as an MCP server to agents over HTTP. Agents call tools by sending MCP tool call requests to the server's endpoint. Built-in tool servers (provided by Kimitsu) follow the same interface — they are just well-known MCP servers with stable URLs on the internal network. There is no translation layer and no proprietary protocol. Everything is debuggable with curl.

### Reserved Output Fields

Any agent may include reserved `ktsu_` prefixed fields in its output schema. The orchestrator has hardcoded behavior for each one — they are a contract between the agent and the orchestrator that bypasses normal data flow. See `kimitsu-reserved-outputs.md` for the full reference.

---

## Landscape & Differentiation

### Where Kimitsu is Differentiated

**Reference implementations, not a closed system.** Every component boundary is a documented HTTP contract. The orchestrator, Agent Runtime, and LLM Gateway are reference implementations — conforming replacements are first-class citizens. Teams that need to swap a component for operational or compliance reasons can do so without forking.

**Tool as the atomic unit.** Every other framework treats the agent as the atom. Kimitsu treats the tool as the atom — agents are compositions of tool servers with a prompt.

**MCP as the only interface.** Tool servers are MCP servers. Kimitsu does not wrap, host, or manage user-provided tool servers. Agents call them directly over HTTP. There is one protocol and one mental model.

**Three and only three pipeline primitives.** Transform, agent, webhook. No special cases, no escape hatches. If it needs reasoning, it is an agent. Everything else is deterministic.

**The invoke endpoint is the integration contract.** Any HTTP client can start a workflow. Any HTTP server can receive pipeline output. Kimitsu does not prescribe trigger infrastructure or output destination — only the data shapes at the boundary.

**Four container tiers, clearly separated.** Orchestrator, Agent Runtime, LLM Gateway, and built-in tool servers are distinct tiers with distinct responsibilities. Built-in tool servers are first-party Docker images managed by Kimitsu — not generic external MCP servers. Stateful built-ins (kv, blob, log, memory) have a back-channel to the orchestrator and are part of the Kimitsu state surface. User-provided tool servers have no orchestrator dependency and are operator-managed. The architecture diagram makes this split explicit.

**Tool-level access control enforced at the Agent Runtime.** The `access.allowlist` field in any tool server file — built-in, local, or marketplace — is enforced by the Agent Runtime's MCP client, not the server. The agent's context is pruned to the permitted tool set before reasoning begins. This applies uniformly regardless of server type, including third-party marketplace servers that implement no restrictions themselves. Allowlist only, no blocklist. Explicit permit is the only mode.

**Sub-agent server access is statically auditable.** A sub-agent cannot access any server endpoint not granted to its parent, and cannot have a wider allowlist than the parent for shared servers. This is validated at boot — not resolved silently at runtime. Version mismatches (parent on v1, sub-agent on v2) are caught as boot errors, not discovered in production.

**`ktsu/cli` — CLI tools as typed MCP tools.** The standard CLI tool server wraps Unix utilities as named MCP tools with typed inputs. Agents call `jq`, `date`, `wc`, and others the same way they call any tool — over MCP, with the same access policy enforcement, the same audit trail in `skill_calls`, and the same container isolation. Custom images extend `ktsu/cli` as a base with a single Dockerfile and a local tool server file. No new concepts.

**Sub-agents replace prompt skills.** LLM-backed reasoning tasks are modelled as full agents invoked by a parent, not as a special skill type. They get the full agent contract: typed output, Air-Lock validation, model declaration.

**Declarative pipeline with typed IO contracts.** The full pipeline is declared in files with no code required. The Air-Lock validates every boundary.

**Gateway-driven model routing.** A `gateway.yaml` defines providers and named model groups. Agents declare a group name — the gateway owns provider selection, fallback chains, and routing strategy.

**Shared resources across workflows.** Tool servers and agents live in shared directories. Multiple workflows compose the same building blocks.

**Event-loop agent runtime.** Agents execute as lightweight functions on a shared worker pool. 1000 concurrent triggers do not require 1000 containers.

**LLM Gateway as infrastructure.** Model resolution, cost tracking, and budget enforcement are centralized. No agent or tool server holds LLM provider credentials.

### Related Projects

| Project | Relation to Kimitsu |
|---|---|
| NanoClaw | Philosophical origin — container isolation, Claude-native, minimal codebase. |
| kagent | Closest structural cousin — K8s CRDs for agents. Requires an actual cluster. |
| LangGraph | Graph orchestration, stateful, production-ready. Code-first — no declarative file format. |
| CrewAI | YAML agent roles. No container isolation, no IO schema, no Air-Lock. |
| Orkes Conductor / Camunda | Durable workflow engines. Agents are embedded tasks, not first-class citizens. |

---

*Revised from design session — March 2026*
