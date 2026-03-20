# RSS — Core Invariants
## Architecture & Design Reference — v3

> These are the rules the entire system is built on. If a design decision violates one of these, the decision is wrong.

---

1. **Tools are pure functions.** They have no persistent state between calls. External side effects are declared via `stateful` but not prevented by RSS.

2. **Tool servers are external.** RSS does not build, package, host, or manage user-provided tool servers. A local tool server file is a pointer — URL, auth, and interface contract. Nothing more.

3. **Built-in tool servers are a distinct tier.** `rss/` namespaced tool servers are first-party Docker images managed by RSS. Stateful built-in servers have a back-channel dependency on the orchestrator and write to the state store via the orchestrator's HTTP API. They are not generic external MCP servers — they are part of the RSS state surface.

4. **Marketplace tool servers are declared centrally.** `servers.yaml` is the single source of truth for external dependencies. Agents cannot call a marketplace server that is not in `servers.yaml`. This is enforced at boot.

5. **Only pipeline agents can cause internal side effects.** Restricted built-in tool servers (storage, context) are only available to pipeline agents, never to sub-agents.

6. **Tool-level access is enforced by the Agent Runtime, not the server.** The Agent Runtime's MCP client enforces the allowlist declared in each tool server file. Enforcement applies uniformly to all server types — built-in, local, and marketplace — regardless of whether the server implements its own restrictions.

7. **Allowlist only, no blocklist.** Tool access policy is explicit permit. The only valid allowlist entries are exact tool names, prefix wildcards (`prefix-*`), and the global wildcard (`*`). Mid-string wildcards are a boot error. Omitting the access block is equivalent to `allowlist: ["*"]`.

8. **Sub-agent server access cannot exceed the parent's.** A sub-agent's effective server set is the intersection of its declared servers and the parent's granted servers, matched by endpoint URL. A sub-agent referencing an endpoint the parent was not granted — including a different version of the same server — is a boot error, not a runtime resolution.

9. **The orchestrator makes zero LLM calls.** It is a deterministic kernel. All intelligence lives in the Agent Runtime.

10. **Every tool interface is MCP over HTTP.** There is one protocol. Everything is debuggable with curl.

11. **Every component boundary is a documented HTTP contract.** The orchestrator, Agent Runtime, and LLM Gateway expose their interfaces as first-class documented APIs, not implementation details. No component assumes it is talking to the RSS implementation of any peer. The RSS implementations are reference implementations — conforming replacements are valid. When in doubt, document the contract before writing the implementation.

12. **Sub-agents are full agents.** They have typed output, Air-Lock validation, and a model declaration. There is no special "prompt skill" type.

13. **Every boundary is validated.** The Air-Lock checks output schema compliance at every step handoff. No step receives unvalidated data.

14. **All secrets are indirected.** Credentials are always `env:VAR_NAME`. Secrets never appear in YAML files. LLM provider keys live only in the LLM Gateway.

15. **Failure is explicit.** A step either runs or it does not. Failure tolerance is declared on the consumer via `optional`, never implicit.

16. **Version everything.** Tool servers, agents, inlets, outlets, and workflows are all independently semver-versioned. The lockfile freezes the full resolved tree.

17. **Environment config never touches workflow files.** Dev/staging/prod differences live entirely in `environments/*.env.yaml`.

18. **Inlets and outlets are declarative mappings, never agents.** Boundary steps use JMESPath field extraction with no reasoning loop. There is no LLM invocation at the pipeline boundary.

19. **The envelope is orchestrator-written, agent-readable.** The orchestrator assembles the envelope from the state store. Agents read it via `rss/envelope`. No agent can modify run context.

20. **All inter-container communication is HTTP.** No Unix sockets, no shared volumes for IPC, no proprietary protocols.

21. **The LLM Gateway is the sole outbound path to LLM providers.** No other container holds LLM credentials or makes direct LLM calls.

22. **Agents declare a group, the gateway decides everything else.** Provider selection, model resolution, fallback chains, and routing strategy all live in `gateway.yaml`. Agents have no knowledge of providers, model strings, or what happens when a group is unavailable. If a group cannot be served, the step fails.

23. **The orchestrator is the single writer to the state store.** No other container has database credentials. All state mutations go through the orchestrator's HTTP API.

24. **The Agent Runtime heartbeats, the orchestrator decides.** Runtime containers report liveness; the orchestrator detects failures and takes action. No step runs without supervision.

25. **There are exactly four pipeline primitives.** Inlet, transform, agent, outlet. No other step types exist. If logic requires LLM reasoning, it is an agent. If it is deterministic data shaping, it is a transform. If it sits at the boundary, it is an inlet or outlet.

26. **Reserved output fields are an orchestrator contract, not a data convention.** Fields prefixed `rss_` are evaluated by the orchestrator before Air-Lock runs. Their behavior is fixed and cannot be overridden by agents or workflow configuration. Unknown `rss_` fields are a boot error.

27. **Transform steps are zero-cost by definition.** They burn no LLM tokens. If logic requires reasoning about content — not just reshaping structure — it belongs in an agent.

28. **Transform op vocabulary is fixed.** There is no escape hatch to arbitrary code or expressions beyond JMESPath. Complexity that exceeds the op vocabulary belongs in a tool server or agent.

29. **Built-in agents follow the same rules as user-defined agents.** They appear in the DAG, consume model budget, go through Air-Lock, and are independently versioned. The `rss/` namespace signals first-party origin, not special treatment by the runtime.

30. **All inlets on a multi-inlet step must produce identical output schemas.** The downstream DAG sees one typed payload regardless of which inlet fired. Schema mismatches across inlets on the same step are a boot error.

31. **Outlet conditions are evaluated by the orchestrator against the envelope, never by agents.** A `condition` expression on an outlet step is JMESPath. If it evaluates falsy, the outlet is marked `skipped` — never `failed`. A run where all outlets either complete or are conditionally skipped is a successful run.

32. **Workflow-to-workflow causal links are explicit and opt-in.** `parent_run_id` is only written to the `runs` table when the receiving inlet declares `trigger.type: workflow` and the payload contains a valid `parent_run_id`. No other inlet type establishes this link. The sending outlet must explicitly map `envelope.run_id` into the payload — the link is never automatic.

33. **Each workflow run owns its own cost budget and envelope.** A child workflow triggered by a parent outlet gets its own `run_id`, its own `cost_budget_usd`, and its own envelope. Cost does not roll up from child to parent. The `parent_run_id` column is the only cross-run link.

---

*Revised from design session — March 2026*
