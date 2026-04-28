# Core Invariants

---

These invariants are the inviolable contracts between ktsu components that make the system predictable, secure, and composable. They define what every component can assume about every other component — and what it can never do.

Violations cause silent failures, security holes, or unpredictable behavior. The system enforces as many of these invariants as possible at boot time or validation time, returning explicit errors rather than allowing runtime surprises. Where enforcement is deferred to runtime, the invariant is still binding — enforcement absence does not mean permission.

Experienced users can treat this as a reference. New users should read these invariants after understanding the four pipeline primitives (agent, transform, webhook, workflow) and how variable scoping works across root and sub-workflows.

---

1. **Tools are pure functions.** They have no persistent state between calls. External side effects are declared via `stateful` but not prevented by Kimitsu.

2. **Tool servers are external.** Kimitsu does not build, package, host, or manage user-provided tool servers. A local tool server file is a pointer — URL, auth, and interface contract. Nothing more.

3. **Marketplace tool servers are declared centrally.** `servers.yaml` is the single source of truth for external dependencies. Agents cannot call a marketplace server that is not in `servers.yaml`. This is enforced at boot.

4. **Only pipeline agents can cause internal side effects.** Restricted tool servers (storage, context) are only available to pipeline agents, never to sub-agents.

5. **Tool-level access is enforced by the Agent Runtime, not the server.** The Agent Runtime's MCP client enforces the allowlist declared in each tool server file. Enforcement applies uniformly to all server types — built-in, local, and marketplace — regardless of whether the server implements its own restrictions.

6. **Allowlist only, no blocklist.** Tool access policy is explicit permit. The only valid allowlist entries are exact tool names, prefix wildcards (`prefix-*`), and the global wildcard (`*`). Mid-string wildcards are a boot error. Omitting the access block is equivalent to `allowlist: ["*"]`.

7. **Sub-agent server access cannot exceed the parent's.** A sub-agent's effective server set is the intersection of its declared servers and the parent's granted servers, matched by endpoint URL. A sub-agent referencing an endpoint the parent was not granted — including a different version of the same server — is a boot error, not a runtime resolution.

8. **The orchestrator makes zero LLM calls.** It is a deterministic kernel. All intelligence lives in the Agent Runtime.

9. **Every tool interface is MCP over HTTP/SSE.** There is one protocol. Everything is debuggable with `ktsu` or curl.

10. **Every component boundary is a documented HTTP contract.** The orchestrator, Agent Runtime, and LLM Gateway expose their interfaces as first-class documented APIs, not implementation details. No component assumes it is talking to the Kimitsu implementation of any peer. The Kimitsu implementations are reference implementations — conforming replacements are valid. When in doubt, document the contract before writing the implementation.

11. **Sub-agents are full agents.** They have typed output, Air-Lock validation, and a model declaration. There is no special "prompt skill" type.

12. **Every boundary is validated.** The Air-Lock checks output schema compliance at every step handoff. No step receives unvalidated data.

13. **Secrets flow via declared env vars or params, not ad-hoc strings.** Workflows declare environment variables in the `env:` array; they are resolved at run start and injected into the envelope under `env`. Agent and server files may not use `env:` references directly — they receive env var values through params passed down from the workflow. Violation is a boot error.

14. **Failure is explicit.** A step either completes or it fails the run. There is no continue-on-failure, no `optional` dependency, and no partial-failure semantics. If a step fails, the run fails immediately and all downstream steps are skipped.

15. **Version everything.** Tool servers, agents, and workflows are all independently semver-versioned. The lockfile freezes the full resolved tree.

16. **Environment config never touches workflow files.** Dev/staging/prod differences live entirely in `environments/*.env.yaml`.

17. **Workflow params are validated before the run is created.** For root workflows, the orchestrator validates the `POST /invoke` request body against `params.schema`. Missing required params return HTTP 422 and no run is created.

18. **The envelope is orchestrator-owned.** The orchestrator assembles the envelope from step outputs persisted in the state store. Inter-step data flows exclusively through declared step outputs and params — no agent may write to or mutate run context directly.

19. **All inter-container communication is HTTP.** This includes MCP over HTTP/SSE. No Unix sockets, no shared volumes for IPC, no proprietary protocols.

20. **The LLM Gateway is the sole outbound path to LLM providers.** No other container holds LLM credentials or makes direct LLM calls.

21. **Agents declare a group, the gateway decides everything else.** Provider selection, model resolution, fallback chains, and routing strategy all live in `gateway.yaml`. Agents have no knowledge of providers, model strings, or what happens when a group is unavailable. If a group cannot be served, the step fails.

22. **The orchestrator is the single writer to the state store.** No other container has database credentials. All state mutations go through the orchestrator's HTTP API.

23. **The Agent Runtime heartbeats, the orchestrator decides.** Runtime containers report liveness; the orchestrator detects failures and takes action. No step runs without supervision.

24. **There are exactly four pipeline primitives.** Transform, agent, webhook, workflow. No other step types exist. If logic requires LLM reasoning, it is an agent. If it is deterministic data shaping, it is a transform. If it needs to call an external HTTP endpoint, it is a webhook. If it needs to execute another workflow's full pipeline inline, it is a workflow step.

25. **Reserved output fields are an orchestrator contract, not a data convention.** Fields prefixed `ktsu_` are evaluated by the orchestrator before Air-Lock runs. Their behavior is fixed and cannot be overridden by agents or workflow configuration. Unknown `ktsu_` fields are a boot error.

26. **Transform steps are zero-cost by definition.** They burn no LLM tokens. If logic requires reasoning about content — not just reshaping structure — it belongs in an agent.

27. **Transform op vocabulary is fixed.** There is no escape hatch to arbitrary code or expressions beyond JMESPath. Complexity that exceeds the op vocabulary belongs in a tool server or agent.

28. **Built-in agents follow the same rules as user-defined agents.** They appear in the DAG, consume model budget, go through Air-Lock, and are independently versioned. The `ktsu/` namespace signals first-party origin, not special treatment by the runtime.

29. **Webhook conditions are evaluated by the orchestrator, never by agents.** A `condition` expression on a webhook step is JMESPath evaluated against step outputs. If it evaluates falsy, the webhook is marked `skipped` — never `failed`. A run where all webhooks either complete or are conditionally skipped is a successful run.

30. **Fanout output is always `{"results": [...]}`.** A `for_each` agent step collects all item invocation outputs in original array order and wraps them in a `results` array. Downstream steps reference individual results via JMESPath against this array. If any item invocation fails, the entire step fails.

31. **Fanout metrics are additive.** Token usage and cost across all fanout invocations are summed and recorded on the step as a single aggregate, exactly as if a single agent had run.

32. **Each workflow run owns its own cost budget and envelope.** Child workflows triggered via webhook get their own `run_id`, their own `cost_budget_usd`, and their own envelope. Cost does not roll up between runs.

33. **A workflow step (`workflow:`) executes another workflow's full pipeline inline under the parent run_id.** The sub-workflow runs in the same process, shares state storage, and its steps are recorded under a namespaced run_id: `parentRunID/stepID`.

34. **Sub-workflows are identified by `visibility: sub-workflow` in their workflow YAML.** They cannot be invoked directly via `POST /invoke` — attempting to do so returns 404.

35. **Webhook execution in a sub-workflow requires dual opt-in:** the sub-workflow must declare `webhooks: execute` AND the parent pipeline step must also declare `webhooks: execute`. If either side omits this, webhooks inside the sub-workflow are suppressed (skipped, not failed).

36. **`params.schema` is the single interface declaration for all workflows.** For root workflows it is the API schema validated against the HTTP request body. For sub-workflows it declares named inputs the parent step must supply. Required params have no default; optional params have a default. Missing required params fail the invocation at validation time.

37. **Value resolution in a workflow step's `params:` block uses `{{ expr }}` syntax.** Plain strings are literals. `{{ expr }}` is evaluated as a path into the pipeline envelope: `{{ env.NAME }}` resolves an env var, `{{ params.name }}` resolves a workflow param, `{{ step.id.field }}` resolves a step output field. Type is preserved when the entire string is a single expression; mixed strings coerce to string.

---

## Reflect Is a Single Pass

When `reflect` is declared on an agent, the Agent Runtime performs one additional LLM call after the initial output draft. There is no loop, no reflection-on-reflection, and no further reflection on retried outputs beyond what the trigger logic dictates. The reflected output is a complete replacement of the draft — there is no merging.

*Revised from design session — March 2026*
