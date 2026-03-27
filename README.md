# ktsu

Go monorepo for the Kimitsu agentic pipeline framework. Kimitsu treats the **tool** as the atomic unit — agents are compositions of tool servers with a prompt. All component boundaries are HTTP contracts.

## Quick start (Docker)

Run the full stack with the included hello-world example:

```sh
# Set your Anthropic API key
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> .env

# Start all services
docker compose up --build
```

This launches the gateway, orchestrator, and runtime with the `examples/hello/` project mounted. Once healthy, invoke the workflow:

```sh
curl -s -X POST http://localhost:${KTSU_PORT:-8080}/invoke/hello \
  -H 'Content-Type: application/json' \
  -d '{"name": "World"}'
```

The host port is configurable via `KTSU_PORT` in `.env` (default: `8080`).

### Self-contained (no API key)

Run the full stack with a local LLM — no external accounts or API keys needed:

```sh
docker compose -f docker-compose.local.yaml up --build
```

This uses [Ollama](https://ollama.com) with `qwen2.5:0.5b` (~397MB). The model is pulled automatically on first run.

## Prerequisites

- Go 1.24+ (for local development)
- Docker & Docker Compose (for containerized usage)

## Build

```sh
make build       # go build ./...
make test        # go test ./...
make lint        # go vet ./...
```

## Running services

Each service is a subcommand of the `kimitsu` binary.

### Core services

| Service | Command | Default port |
|---|---|---|
| Orchestrator | `kimitsu start orchestrator` | 8080 |
| LLM Gateway | `kimitsu start gateway` | 8081 |
| Agent Runtime | `kimitsu start runtime` | 8082 |

Every service accepts `--host` (bind interface, default `""` = all interfaces) and `--port`:

```sh
# Orchestrator (control plane)
go run ./cmd/kimitsu start orchestrator

# With an environment config and custom address
go run ./cmd/kimitsu start orchestrator --env environments/dev.env.yaml --host 0.0.0.0 --port 8080

# LLM Gateway
go run ./cmd/kimitsu start gateway --config gateway.yaml --port 8081

# Agent Runtime — point at the orchestrator and gateway
go run ./cmd/kimitsu start runtime \
  --orchestrator http://orchestrator.internal:8080 \
  --gateway http://llm-gateway.internal:8081
```

Shortcut for the orchestrator:

```sh
make run-orchestrator
```

### Environment variables

All service addresses and peer URLs can be set via `KTSU_*` environment variables. CLI flags take precedence over env vars.

| Variable | Flag | Service | Default |
|---|---|---|---|
| `KTSU_ORCHESTRATOR_HOST` | `--host` | orchestrator | `""` (all interfaces) |
| `KTSU_ORCHESTRATOR_PORT` | `--port` | orchestrator | `8080` |
| `KTSU_GATEWAY_HOST` | `--host` | gateway | `""` |
| `KTSU_GATEWAY_PORT` | `--port` | gateway | `8081` |
| `KTSU_RUNTIME_HOST` | `--host` | runtime | `""` |
| `KTSU_RUNTIME_PORT` | `--port` | runtime | `8082` |
| `KTSU_ORCHESTRATOR_URL` | `--orchestrator` | runtime, builtins | `http://localhost:8080` |
| `KTSU_GATEWAY_URL` | `--gateway` | runtime | `http://localhost:8081` |

```sh
# Container / multi-host example
KTSU_ORCHESTRATOR_URL=http://orchestrator.internal:8080 \
KTSU_GATEWAY_URL=http://llm-gateway.internal:8081 \
  go run ./cmd/kimitsu start runtime
```

### Built-in tool servers

| Server | Command | Default port |
|---|---|---|
| kv | `kimitsu start kv` | 9100 |
| blob | `kimitsu start blob` | 9101 |
| log | `kimitsu start log` | 9102 |
| memory | `kimitsu start memory` | 9103 |
| envelope | `kimitsu start envelope` | 9104 |
| format | `kimitsu start format` | 9105 |
| validate | `kimitsu start validate` | 9106 |
| transform | `kimitsu start transform` | 9107 |
| cli | `kimitsu start cli` | 9108 |

All built-in servers accept `--host` and `--port`. Stateful built-ins also accept `--orchestrator` (reads `KTSU_ORCHESTRATOR_URL`):

```sh
go run ./cmd/kimitsu start kv --host 0.0.0.0 --port 9100 --orchestrator http://orchestrator.internal:8080
```

Stateful built-ins (`kv`, `blob`, `log`, `memory`, `envelope`) register with the orchestrator on startup and require `--orchestrator` to be set. Stateless built-ins (`format`, `validate`, `transform`, `cli`) do not.

## Other commands

```sh
# Validate configuration files
kimitsu validate --env environments/dev.env.yaml

# Generate kimitsu.lock.yaml (not yet implemented)
kimitsu lock
```

## Health checks

Every service exposes `GET /health` returning `{"status":"ok"}`.

## Docs

Architecture and configuration reference lives in [`docs/`](docs/).
