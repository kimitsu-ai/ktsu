# RSS — Overview & Philosophy
## Architecture & Design Reference — v3

> **Tools are pure functions. Agents are stateful processes.**
> **The pipeline is a declaration. The orchestrator is the kernel. The tool server is the atom.**
> **Every component boundary is an HTTP contract. The RSS implementations are reference implementations, not the only valid ones.**

---

## Philosophy

RSS is built on a single inversion of the conventional agentic model:

**Conventional model:** The agent is the atom. Tools and tool servers are accessories bolted on.

**RSS model:** The tool is the atom. The agent is an emergent artifact of its tool composition.

This maps directly to how engineers already think about software:

| RSS Concept | Software Analogy |
|---|---|
| Tool | Function / method |
| Tool server | Library / service |
| Agent | Application |
| Sub-agent | Internal service / module |
| Workflow Compose | Deployment manifest |
| Orchestrator | Kernel / control plane |
| Agent Runtime | Worker pool / function runtime |
| LLM Gateway | Provider-normalizing proxy (first-party) |
| Gateway Config | Provider registry & model group definitions |
| Environment config | Environment-specific override |

The orchestrator is the kernel — deterministic, cheap, zero LLM calls. All intelligence lives in agent processes and tool servers where it creates value and can be metered precisely.

### Tool-Driven Development

Tools are the first-class development primitive. An agent has no identity beyond what tools it carries. Two agents with identical tool sets *are* the same agent. The prompt is not the agent — it is the routing logic that decides how tools get invoked. Version the tool server. The agent version follows automatically.

### Modular by Design

Every component in RSS — the orchestrator, Agent Runtime, LLM Gateway, and all tool servers — communicates exclusively over documented HTTP APIs. No component assumes it is talking to the RSS implementation of any peer. The Agent Runtime does not care whether the orchestrator is the RSS orchestrator or a conforming replacement. The LLM Gateway is swappable for any proxy that speaks the same contract.

This is a deliberate architectural commitment, not a side effect of the HTTP choice:

- The orchestrator's HTTP API (invocation dispatch, heartbeat endpoint, Air-Lock, state writes) is a first-class documented contract.
- The Agent Runtime invocation payload format is a spec. What the orchestrator sends defines what any conforming orchestrator must send.
- The LLM Gateway HTTP contract is a replaceable surface — a team that needs to swap in a different proxy can do so. The RSS LLM Gateway is a first-party implementation of that contract, not a wrapper around any external library. It was designed with third-party gateway projects as reference points, but carries no runtime dependency on them.
- Tool servers are already fully independent — MCP over HTTP, no RSS coupling whatsoever.

The RSS implementations are **reference implementations**. They are the defaults, they are supported, and they are what most teams will run. But a team that needs to replace the orchestrator with a Temporal-backed implementation, or the LLM Gateway with an internal proxy, or the Agent Runtime with a serverless function executor, should be able to do so without forking RSS or losing compatibility with the YAML pipeline definitions.

Well-documented boundaries make this possible. When in doubt, document the contract before writing the implementation.

---

## Core Concepts

### The Four Pipeline Primitives

Every step in an RSS pipeline is exactly one of four things:

| Primitive | LLM | Tools | Role |
|---|---|---|---|
| **Inlet** | Never | Never | Receives external trigger, extracts structured data via declarative mapping. Multiple inlets per step supported — all must produce identical output schemas. |
| **Transform** | Never | Never | Reshapes data between steps via deterministic ops |
| **Agent** | Always | Optional | Reasons, classifies, synthesizes — the only LLM-bearing step type |
| **Outlet** | Never | Never | Emits pipeline result to an external system via declarative mapping |

Nothing else. If logic requires reasoning about content, it is an agent. If it is pure data shaping, it is a transform. If it sits at the boundary, it is an inlet or outlet. There is no fourth option and no escape hatch.

### Tools are Pure Functions

- No persistent state between calls
- Same input always produces the same output
- Safe to call from multiple agents simultaneously
- The tool server starts once, serves many calls, remembers nothing
- Run as long-lived MCP servers hosted externally — RSS does not manage their runtime

### Tool Servers are External MCP Servers

Every tool server is an MCP server deployed and operated independently of RSS. The tool server file is a pointer: it declares where the server lives, how to authenticate, and what the interface contract is. RSS does not care what language the server is written in, how it is packaged, or how it is deployed. A single MCP server can expose multiple tools — grouping related tools into one server is a packaging decision made by the server author.

### Agents are Stateful Processes

- Executed as functions on the Agent Runtime worker pool
- Orchestrate tool calls via reasoning prompt
- Produce typed output returned to the orchestrator via HTTP
- Are the only callers permitted to use storage and context tools
- The output is validated by the Air-Lock before downstream agents can read it

### Sub-Agents

A sub-agent is a full agent invoked by a parent agent as a tool call rather than as a pipeline step. Sub-agents appear in the parent agent's `agents` field and are available to the parent as a built-in `agent-invoke` tool over MCP. They have their own prompt, tools, model declaration, and typed output schema validated by the Air-Lock. Sub-agents are the replacement for what was previously called "prompt skills" — any LLM-backed reasoning task that the parent agent needs to delegate is modelled as a sub-agent.

Sub-agents do not appear in the pipeline DAG. They are private implementation details of the agent that declares them. Their cost and token usage roll up to the parent step's metrics.

### Inlets and Outlets are Declarative Boundaries

Inlets and outlets are pure mapping steps — they never invoke an LLM. An inlet receives a raw external trigger and uses a declarative field mapping to produce a typed, Air-Lock validated output and write envelope context. An outlet receives pipeline output and maps it to an external action. They appear as named steps in the pipeline DAG and follow the same output schema and Air-Lock rules as every other step. They do not execute on the Agent Runtime.

**Why no LLM at the boundary:** Untrusted data enters the system at the inlet. If that data flows into an agent reasoning loop before being structurally extracted, an attacker can craft a payload that hijacks the agent's behavior. JMESPath field extraction has no reasoning loop to hijack — `body.event.text` is always `body.event.text` regardless of what the text says. The inlet is a sanitization boundary, not a reasoning step.

### The Air-Lock

A middleware validator running inside the orchestrator. It validates every step's output against the declared `output.schema` before making the output available to downstream steps. Bad data never reaches the next step.

When validation fails on an agent step, the Air-Lock returns the validation error to the Agent Runtime, which reinjects the error into the agent's reasoning loop. The agent may retry up to the configured `retry.max` limit (default: 0). If all retries are exhausted, the step fails. Transform steps, inlets, and outlets do not retry — they fail immediately on bad output.

### MCP as the Singular Interface

Every tool server presents as an MCP server to agents over HTTP. Agents call tools by sending MCP tool call requests to the server's endpoint. Built-in tool servers (provided by RSS) follow the same interface — they are just well-known MCP servers with stable URLs on the internal network. There is no translation layer and no proprietary protocol. Everything is debuggable with curl.

### Reserved Output Fields

Any agent may include reserved `rss_` prefixed fields in its output schema. The orchestrator has hardcoded behavior for each one — they are a contract between the agent and the orchestrator that bypasses normal data flow. See `rss-reserved-outputs.md` for the full reference.

---

## Landscape & Differentiation

### Where RSS is Differentiated

**Reference implementations, not a closed system.** Every component boundary is a documented HTTP contract. The orchestrator, Agent Runtime, and LLM Gateway are reference implementations — conforming replacements are first-class citizens. Teams that need to swap a component for operational or compliance reasons can do so without forking.

**Tool as the atomic unit.** Every other framework treats the agent as the atom. RSS treats the tool as the atom — agents are compositions of tool servers with a prompt.

**MCP as the only interface.** Tool servers are MCP servers. RSS does not wrap, host, or manage user-provided tool servers. Agents call them directly over HTTP. There is one protocol and one mental model.

**Four and only four pipeline primitives.** Inlet, transform, agent, outlet. No special cases, no escape hatches. If it needs reasoning, it is an agent. Everything else is deterministic.

**Four container tiers, clearly separated.** Orchestrator, Agent Runtime, LLM Gateway, and built-in tool servers are distinct tiers with distinct responsibilities. Built-in tool servers are first-party Docker images managed by RSS — not generic external MCP servers. Stateful built-ins (kv, blob, log, memory) have a back-channel to the orchestrator and are part of the RSS state surface. User-provided tool servers have no orchestrator dependency and are operator-managed. The architecture diagram makes this split explicit.

**Tool-level access control enforced at the Agent Runtime.** The `access.allowlist` field in any tool server file — built-in, local, or marketplace — is enforced by the Agent Runtime's MCP client, not the server. The agent's context is pruned to the permitted tool set before reasoning begins. This applies uniformly regardless of server type, including third-party marketplace servers that implement no restrictions themselves. Allowlist only, no blocklist. Explicit permit is the only mode.

**Sub-agent server access is statically auditable.** A sub-agent cannot access any server endpoint not granted to its parent, and cannot have a wider allowlist than the parent for shared servers. This is validated at boot — not resolved silently at runtime. Version mismatches (parent on v1, sub-agent on v2) are caught as boot errors, not discovered in production.

**`rss/cli` — CLI tools as typed MCP tools.** The standard CLI tool server wraps Unix utilities as named MCP tools with typed inputs. Agents call `jq`, `date`, `wc`, and others the same way they call any tool — over MCP, with the same access policy enforcement, the same audit trail in `skill_calls`, and the same container isolation. Custom images extend `rss/cli` as a base with a single Dockerfile and a local tool server file. No new concepts.

**Inlets are pure mappings, never agents.** The boundary between the untrusted world and the pipeline is a JMESPath extraction layer with no reasoning loop. Prompt injection in external input cannot hijack an inlet.

**Multiple inlets per step for reusable workflows.** A single pipeline step can declare multiple inlets — Slack, email, and workflow trigger — each normalising to an identical output schema. The downstream DAG is identical regardless of which inlet fired. Workflows are reusable across trigger sources without duplication.

**Conditional outlets for multi-inlet workflows.** Outlet steps declare a `condition` JMESPath expression evaluated against the envelope at runtime. If the condition is false, the outlet is skipped cleanly. Each outlet is responsible for knowing whether it applies to the current trigger origin.

**Workflow-to-workflow chaining via outlet and inlet.** Workflows chain via a standard outlet posting to a `workflow` inlet. The causal link — `parent_run_id` — is explicit and opt-in. Each run owns its own envelope and cost budget.

**Built-in agents for hardened common patterns.** `rss/` namespaced agents ship with RSS for patterns like secure parsing that are easy to get wrong. Drop them into a pipeline the same way you reference a built-in tool server.

**Sub-agents replace prompt skills.** LLM-backed reasoning tasks are modelled as full agents invoked by a parent, not as a special skill type. They get the full agent contract: typed output, Air-Lock validation, model declaration.

**Declarative pipeline with typed IO contracts.** The full pipeline is declared in files with no code required. The Air-Lock validates every boundary.

**Gateway-driven model routing.** A `gateway.yaml` defines providers and named model groups. Agents declare a group name — the gateway owns provider selection, fallback chains, and routing strategy.

**Shared resources across workflows.** Tool servers, agents, inlets, and outlets live in shared directories. Multiple workflows compose the same building blocks.

**Event-loop agent runtime.** Agents execute as lightweight functions on a shared worker pool. 1000 concurrent triggers do not require 1000 containers.

**LLM Gateway as infrastructure.** Model resolution, cost tracking, and budget enforcement are centralized. No agent or tool server holds LLM provider credentials.

### Related Projects

| Project | Relation to RSS |
|---|---|
| NanoClaw | Philosophical origin — container isolation, Claude-native, minimal codebase. |
| kagent | Closest structural cousin — K8s CRDs for agents. Requires an actual cluster. |
| LangGraph | Graph orchestration, stateful, production-ready. Code-first — no declarative file format. |
| CrewAI | YAML agent roles. No container isolation, no IO schema, no Air-Lock. |
| Orkes Conductor / Camunda | Durable workflow engines. Agents are embedded tasks, not first-class citizens. |

---

*Revised from design session — March 2026*
