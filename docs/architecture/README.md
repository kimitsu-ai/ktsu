# Architecture

This section describes the four components that make up a running Kimitsu system and explains how they relate to one another.

---

## The Four-Component Model

**Orchestrator** — Resolves the step DAG, evaluates `depends_on` conditions, manages secret propagation, and dispatches steps to the runtime. The orchestrator does not execute agent loops or call LLMs directly.

**Runtime** — Executes agent loops: calling the LLM through the gateway, invoking MCP tools, enforcing `max_turns`, and validating step output against the declared schema via the Air-Lock. The runtime does not make routing or scheduling decisions.

**Gateway** — Normalizes LLM provider APIs (Anthropic, OpenAI, and others) into a single internal interface. Agents declare a `model.group` name; the gateway resolves that name to a concrete provider and model based on the active gateway configuration. Provider credentials never leave the gateway.

**Tool Servers** — Independent processes that expose tools to agents via the Model Context Protocol (MCP) over HTTP/SSE. Tool servers are not managed by Kimitsu; they are started separately and registered in `.server.yaml` configuration files. Each tool server runs in its own process with its own lifecycle.

---

## Reading Guide

- **[runtime.md](./runtime.md)** — How execution works end-to-end: the agent loop, turn management, Air-Lock validation, and how the orchestrator and runtime handoff.
- **[tool-servers.md](./tool-servers.md)** — How MCP integration works: server discovery, the SSE handshake, tool invocation, and secret propagation to server params.
- **[configuration.md](./configuration.md)** — The files you write to configure the system: gateway config, environment config, server declarations, and how they compose.

---

## Key Design Principle

Each component has a single, well-defined responsibility and communicates with the others over HTTP contracts. No component holds hidden state that another depends on implicitly:

- The orchestrator is stateless; it reads the workflow definition and the current run envelope on every dispatch.
- The runtime is stateless across agent invocations; turn state lives in the message history passed per call.
- The gateway is a proxy; it holds provider credentials but no per-run state.
- Tool servers are independent processes; they manage their own state and expose it only through declared tool interfaces.

This separation means each component can be replaced or scaled independently. The Kimitsu implementations are reference implementations, not the only valid ones.

---

*Revised April 2026*
