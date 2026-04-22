# Kimitsu — Runtime Architecture

## Architecture & Design Reference — v4

---

## Runtime Architecture

A Kimitsu deployment consists of four container tiers running on a shared internal network. All inter-container communication is HTTP. No container except the LLM Gateway has outbound internet access by default.

### The Four Tiers

1.  **Orchestrator**: The kernel. Manages the DAG, session state, and Air-Lock.
2.  **Agent Runtime**: The worker pool. Executes stateless agent reasoning loops via an async event loop.
3.  **LLM Gateway**: The security boundary. Normalizes providers, enforces budgets, and holds credentials.
4.  **Tool Servers**: The capabilities. Long-lived MCP servers providing tools via HTTP/SSE.

---

## Orchestrator Responsibilities

- **DAG Resolution**: Determines execution order and unblocks steps as dependencies complete.
- **Param Validation**: Validates `POST /invoke` request bodies against `params.schema` for root workflows.
- **Variable Injection**: Resolves environment variables and parameters, populating the initial run envelope.
- **Air-Lock Enforcement**: Validates step outputs against JSON Schema before passing them downstream.
- **Heartbeat Monitoring**: Tracks Agent Runtime instances and fails steps that go silent.
- **Secret Scrubbing**: Ensures any parameter marked `secret: true` is masked in logs and the envelope.

---

## Variable Substitution Syntax

The orchestrator and runtime support the following `{{ expr }}` namespaces:

- **`{{ env.NAME }}`**: Injected environment variables. Available **only in root workflows**.
- **`{{ params.NAME }}`**: Local parameters passed to the current workflow or agent.
- **`{{ step.ID.FIELD }}`**: Outputs from upstream pipeline steps.

> [!IMPORTANT]
> The old `env:VAR` and `param:NAME` syntax has been removed. All dynamic resolution must use the `{{ }}` template syntax or bare JMESPath in conditions/transforms.

---

## Agent Runtime Invocation

1.  **Preparation**: The orchestrator POSTs an invocation payload to the runtime.
2.  **Tool Setup**: The runtime connects to the declared tool servers and filters their tools against the agent's `access.allowlist`.
3.  **Reasoning Loop**: The agent reasons via the LLM Gateway, calling tools as needed.
4.  **Heartbeat**: The runtime reports active status every 5 seconds.
5.  **Result**: The runtime returns the final typed output to the orchestrator for Air-Lock verification.

---

## Failure Semantics

- **Fail-Fast**: If any step fails, the entire run halts immediately.
- **Clean Skip**: Skipped steps (via conditions or `ktsu_skip_reason`) propagate downstream without failing the run.
- **Budget Circuit Breaker**: If a run exceeds its `cost_budget_usd`, all subsequent LLM calls are rejected.

---

*Revised April 2026*
