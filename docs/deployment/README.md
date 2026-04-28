# Production Deployment

This guide covers running ktsu in production: service topology, state persistence, configuration, scaling, health checks, and observability.

## Deployment guides

- [Deploy with Docker](deployment/docker.md)
- [Deploy on Railway](deployment/railway.md)

---

## Service Topology

A ktsu deployment has three services:

1. **Orchestrator** — The kernel. Manages the workflow DAG, validates step outputs through the Air-Lock, tracks heartbeats, and persists all run state. Exposes the public HTTP API for workflow invocation.
2. **Agent Runtime** — The worker. Executes stateless agent reasoning loops. Receives invocation payloads from the Orchestrator, connects to tool servers, and calls the Gateway for LLM inference.
3. **LLM Gateway** — The security boundary. Normalizes LLM providers, holds credentials, enforces cost budgets, and is the only service with outbound internet access to provider APIs.

All inter-service communication is plain HTTP. There are no message queues or shared memory between services.

**Communication flow:**

```
CLI / external → Orchestrator → Runtime → Gateway → LLM provider
                                        → Tool servers (MCP/SSE)
```

The Runtime is registered with the Orchestrator via `KTSU_RUNTIME_URL`. The Gateway address is set on both the Orchestrator (`KTSU_GATEWAY_URL`) and the Runtime (`KTSU_GATEWAY_URL`).

### Minimum Viable Deployment

For single-host or development use, run all three services on one machine:

```bash
ktsu start gateway --config gateway.yaml   # :5052 by default
ktsu start orchestrator                    # :5050
ktsu start runtime                         # :5051
```

For a scaled deployment, each service runs on separate hosts or containers. The Orchestrator and Gateway ports are configurable; point services at each other via environment variables.

---

## State Persistence

The Orchestrator is stateless — all run and step state lives in the Store, which is the only component that must be backed up.

**Store backends:**

| Backend | `KTSU_STORE_TYPE` | Notes |
|---|---|---|
| In-memory | `memory` (default) | No persistence; state is lost on restart. For development only. |
| SQLite | `sqlite` | Persists to a file. Suitable for single-host or low-volume production. |
| Postgres | `postgres` | Defined in the interface but not yet implemented. |

**SQLite in production:**

Set `KTSU_STORE_TYPE=sqlite` and `KTSU_DB_PATH` to an absolute path on a persistent volume:

```bash
KTSU_STORE_TYPE=sqlite
KTSU_DB_PATH=/data/ktsu.db
```

The SQLite store does not enable WAL mode automatically. For workloads with concurrent readers, enable WAL mode manually on the database file before starting the Orchestrator:

```bash
sqlite3 /data/ktsu.db "PRAGMA journal_mode=WAL;"
```

Back up `ktsu.db` regularly. Because the Orchestrator is stateless, recovery is straightforward: restore the database file and restart.

---

## Environment Configuration

### Orchestrator

| Variable | Default | Description |
|---|---|---|
| `KTSU_ORCHESTRATOR_HOST` | `` (all interfaces) | Bind host |
| `KTSU_ORCHESTRATOR_PORT` | `5050` | Listen port |
| `KTSU_RUNTIME_URL` | — | URL of the Agent Runtime |
| `KTSU_GATEWAY_URL` | — | URL of the LLM Gateway |
| `KTSU_OWN_URL` | — | The Orchestrator's own URL (used for Runtime callbacks) |
| `KTSU_STORE_TYPE` | `memory` | State backend: `memory` or `sqlite` |
| `KTSU_DB_PATH` | `ktsu.db` | SQLite file path |
| `KTSU_PROJECT_DIR` | `.` | Project root for resolving agent and server paths |

### Agent Runtime

| Variable | Default | Description |
|---|---|---|
| `KTSU_RUNTIME_HOST` | `` (all interfaces) | Bind host |
| `KTSU_RUNTIME_PORT` | `5051` | Listen port |
| `KTSU_ORCHESTRATOR_URL` | `http://localhost:5050` | Orchestrator to register with and send heartbeats to |
| `KTSU_GATEWAY_URL` | `http://localhost:5052` | Gateway for LLM calls |

### LLM Gateway

| Variable | Default | Description |
|---|---|---|
| `KTSU_GATEWAY_HOST` | `` (all interfaces) | Bind host |
| `KTSU_GATEWAY_PORT` | `5052` | Listen port |
| `ANTHROPIC_API_KEY` | — | Injected into the gateway container; referenced by `env:` in gateway config |

### Provider Credentials

Do not hardcode provider API keys in workflow or gateway YAML files. The gateway config supports `env:` references:

```yaml
# gateway.yaml
providers:
  - name: anthropic
    type: anthropic
    config:
      api_key: "env:ANTHROPIC_API_KEY"
```

The `env:ANTHROPIC_API_KEY` value is resolved at runtime from the gateway process's environment. Inject secrets via your orchestration platform's secret management (Docker secrets, Kubernetes secrets, AWS Secrets Manager, etc.) rather than committing them to files.

---

## Scaling

**Orchestrator:** Stateless. Can be horizontally scaled behind a load balancer as long as all instances share the same SQLite file (single host only) or a future Postgres backend. For multi-host scale-out, Postgres support is required (not yet implemented).

**Agent Runtime:** Handles one agent reasoning loop per request. Scale out Runtime instances to increase concurrent workflow throughput. Each Runtime registers with the Orchestrator independently via `KTSU_ORCHESTRATOR_URL`.

**LLM Gateway:** Stateless. Can be horizontally scaled behind a load balancer. All instances read credentials from environment variables.

**Tool Servers:** Independent processes. Scale custom tool servers independently based on observed load. Each tool server is referenced by URL in agent definitions, so multiple instances can sit behind a load balancer without changes to workflow config.

---

## Health Checks

All three core services expose a `GET /health` endpoint that returns `{"status":"ok"}` when the service is ready.

| Service | Default URL |
|---|---|
| Orchestrator | `http://localhost:5050/health` |
| Agent Runtime | `http://localhost:5051/health` |
| LLM Gateway | `http://localhost:5052/health` |

Verify all three after startup:

```bash
curl -s http://localhost:5050/health   # orchestrator
curl -s http://localhost:5051/health   # agent runtime
curl -s http://localhost:5052/health   # LLM gateway
```

A healthy startup sequence: Gateway starts first (it has no dependencies), then the Orchestrator (depends on Gateway being healthy), then the Runtime (registers with both). The Docker Compose setup enforces this order via `depends_on` with `condition: service_healthy`.

The Agent Runtime sends a heartbeat POST to `<KTSU_ORCHESTRATOR_URL>/heartbeat` every 5 seconds while agent steps are active. The Orchestrator uses these heartbeats to detect stuck agents and fail steps that go silent.

---

## Observability

**Reserved output fields:**

Agents may emit two reserved fields in step output that the Orchestrator surfaces for logging and monitoring — they have no effect on downstream pipeline data:

- `ktsu_flags` — An array of string labels (e.g. `["low_risk", "requires_review"]`). Use these to drive alerting rules on step output logs.
- `ktsu_rationale` — A free-text string explaining the agent's decision. Useful for audit trails and debugging.

**Inspecting run state:**

```bash
ktsu runs list
ktsu runs get <run-id>
```

`ktsu runs get` returns the full run record including step statuses, outputs, and error messages. This is the primary tool for diagnosing failed or stuck runs.

**Heartbeat monitoring:**

The Runtime posts active step IDs to the Orchestrator every 5 seconds. If a Runtime crashes or becomes unresponsive, the Orchestrator will detect the missing heartbeat and mark the affected step as failed. Monitor for steps that transition to `failed` with a timeout error as a signal of Runtime instability.

---

*Last updated April 2026*
