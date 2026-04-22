# Kimitsu — Future Plans

## Roadmap & Design Notes — April 2026

---

## Current State (v4 — Refined)

The following components are implemented and tested:

**Orchestrator**
- `POST /invoke/{workflow}` — validates `params.schema`, generates `run_id`, starts DAG execution
- `GET /runs/{run_id}` — returns current run status and step summary
- `GET /envelope/{run_id}` — returns full run envelope with per-step outputs and aggregated metrics
- Variable substitution: `{{ env.NAME }}`, `{{ params.NAME }}`, `{{ step.ID.FIELD }}`
- DAG resolution and step dispatch (agent, transform, webhook, workflow)
- Air-Lock output validation with agent retry
- Reserved output field processing (`ktsu_*`)
- Webhook step execution with JMESPath body mapping and condition evaluation
- Transform step execution (merge, sort, filter, map, flatten, deduplicate)
- Agent fanout (`for_each`) — bounded concurrency, `max_items` cap, metrics aggregation

**Agent Runtime**
- Reasoning loop: LLM → tool calls → output
- MCP client with access policy enforcement (tool allowlist)
- Heartbeat reporting (batched, every 5s)
- Air-Lock error reinjection for retry
- Static system prompt enforcement for caching

**LLM Gateway**
- Provider adapters: Anthropic, OpenAI, openai-compat, Ollama, Gemini, Cohere
- Group router with fallback, round-robin, least-latency strategies
- Budget circuit breaker per `run_id`
- `restrictions.allow_from` enforcement per group

---

## Near-term (v4.x)

### Persistent State Store Migration
Finalize PostgreSQL driver and migration tooling (`ktsu db migrate`).

### `ktsu invoke` CLI Command
Add `ktsu invoke <workflow> --input '{"key":"value"}'` as a development convenience.

---

## Medium-term (v5)

### Sub-Agent Invocations
Implement `agent-invoke` built-in MCP tool for nested agent reasoning.

### Request/Response Audit Logging
Log every LLM Gateway request and response to a dedicated `audit_log` table.

---

*Revised April 2026*
