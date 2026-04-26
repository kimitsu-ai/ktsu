# Glossary

A concise reference for terms used throughout the Kimitsu docs. Many of these words carry specific meanings in this system that differ from their common usage.

---

**Agent** — A workflow step that makes one or more LLM calls. Agents are defined in `.agent.yaml` files; they declare a prompt, a model policy, tool servers, and an output schema. The agent is the only primitive type allowed to call an LLM.

**Step** — A single unit in a workflow pipeline. Every step has an `id` and resolves to exactly one primitive type: agent, transform, webhook, or workflow.

**Primitive** — One of the four step types available in a pipeline. The four primitives are agent, transform, webhook, and workflow. Each has distinct semantics: only agents are intelligent, only transforms are deterministic data reshaping, only webhooks send outbound HTTP, and only workflow steps invoke sub-pipelines.

**Workflow** — A YAML file defining an ordered pipeline of steps. Workflows are the top-level unit of composition in Kimitsu. A workflow can be a root workflow (invoked directly) or a sub-workflow (invoked as a step inside another workflow).

**Root workflow** — A workflow invoked directly via the CLI (`ktsu run`) or the orchestrator HTTP API. Root workflows have access to `env.*` variable substitution, which is resolved from the host environment at invocation time.

**Sub-workflow** — A workflow invoked as a `workflow:` step inside another workflow. Sub-workflows do not have direct access to `env.*` variables; any environment values must be explicitly threaded in via `input:` params from the parent.

**Orchestrator** — The component responsible for resolving the step DAG, evaluating `depends_on` conditions, managing secrets throughout the pipeline, and dispatching steps to the runtime. The orchestrator is stateless across invocations.

**Runtime** — The component that executes agent loops: calling the LLM, invoking MCP tools, enforcing `max_turns`, and validating output against the declared schema via the Air-Lock. The runtime does not make routing decisions; it only executes what the orchestrator dispatches.

**Gateway** — The component that normalizes LLM provider APIs (Anthropic, OpenAI, and others) into a single internal interface. Agents declare a `model.group`; the gateway resolves that group name to a concrete provider and model according to the active gateway config.

**Tool server** — An MCP-compatible process that exposes a set of tools to agents. Tool servers run as independent processes and communicate with the runtime over HTTP/SSE. Kimitsu does not manage tool server lifecycles; it connects to already-running servers based on `.server.yaml` configuration.

**Pipeline** — The ordered list of steps defined inside a workflow file. Steps in a pipeline can declare `depends_on` to express data dependencies; the orchestrator uses these declarations to build a DAG and determine execution order and parallelism.

**Param** — A named input value threaded through the invocation chain. Params are the mechanism for injecting configuration, user input, and secrets into agents and sub-workflows without relying on global environment state. Params marked `secret: true` are scrubbed from logs and the run envelope.

---

*Revised April 2026*
