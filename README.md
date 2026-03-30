# ktsu

Go monorepo for the Kimitsu agentic pipeline framework. Kimitsu treats the **tool** as the atomic unit — agents are compositions of tool servers with a prompt. All component boundaries are HTTP contracts.

## Quick start (Docker)

Run the full stack with the included hello-world example:

```sh
# Set your Anthropic API key
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> .env

# Start all services
make docker-up
```

This launches the gateway, orchestrator, and runtime with the `examples/hello/` project mounted. Verify all services are healthy:

```sh
# Check each service (should return {"status":"ok"})
curl -s http://localhost:8081/health   # gateway
curl -s http://localhost:8080/health   # orchestrator
curl -s http://localhost:8082/health   # runtime
```

Once healthy, invoke the workflow with the ktsu CLI:

```sh
# Wait for the workflow to complete and print the result
./ktsu invoke hello --input "{\"name\": \"Kyle\"}" --wait
```

Or with curl:

```sh
curl -s -X POST http://localhost:${KTSU_PORT:-8080}/invoke/hello \
  -H 'Content-Type: application/json' \
  -d '{"name": "World"}'
# => {"run_id":"run_a1b2c3...","status":"accepted"}
```

The invoke endpoint returns immediately with a `run_id`. Poll the run to check progress:

```sh
curl -s http://localhost:${KTSU_PORT:-8080}/runs/<run_id>
```

The host port is configurable via `KTSU_PORT` in `.env` (default: `8080`).

### Self-contained (no API key)

Run the full stack with a local LLM — no external accounts or API keys needed:

```sh
make docker-up-local
```

This uses [Ollama](https://ollama.com) with `qwen2.5:0.5b` (~397MB). The model is pulled automatically on first run.

## Installation

### Shell (macOS & Linux)

**Public Installation:**
```sh
curl -fsSL https://raw.githubusercontent.com/kimitsu-ai/ktsu/main/install.sh | sh
```

**Stealth Mode (Private Repo):**
If the repository is private, you can install using the GitHub CLI:
```sh
gh release download --repo kimitsu-ai/ktsu -p install.sh -O - | sh
```
Or with a `GITHUB_TOKEN`:
```sh
export GITHUB_TOKEN=your_token
curl -H "Authorization: token $GITHUB_TOKEN" -fsSL https://raw.githubusercontent.com/kimitsu-ai/ktsu/main/install.sh | sh
```

### Homebrew (Coming Soon)

Once the tap is official, you can install via:
`brew install kimitsu-ai/tap/ktsu`

### Docker

The official image is available on GitHub Container Registry:
`docker pull ghcr.io/kimitsu-ai/ktsu:latest`

## Prerequisites

- Go 1.24+ (for local development)
- Docker & Docker Compose (for containerized usage)

## Build

```sh
make build          # go build ./...
make test           # go test ./...
make lint           # go vet ./...
make docker-up      # docker compose (API key required)
make docker-up-local # docker compose with local LLM
make docker-down    # stop all containers
```

## Running services

Each service is a subcommand of the `ktsu` binary.

### Core services

| Service | Command | Default port |
|---|---|---|
| Orchestrator | `ktsu start orchestrator` | 8080 |
| LLM Gateway | `ktsu start gateway` | 8081 |
| Agent Runtime | `ktsu start runtime` | 8082 |

Every service accepts `--host` (bind interface, default `""` = all interfaces) and `--port`:

```sh
# Orchestrator (control plane)
go run ./cmd/ktsu start orchestrator

# With an environment config and custom address
go run ./cmd/ktsu start orchestrator --env environments/dev.env.yaml --host 0.0.0.0 --port 8080

# LLM Gateway
go run ./cmd/ktsu start gateway --config gateway.yaml --port 8081

# Agent Runtime — point at the orchestrator and gateway
go run ./cmd/ktsu start runtime \
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
  go run ./cmd/ktsu start runtime
```

### Built-in tool servers

| Server | Command | Default port |
|---|---|---|
| kv | `ktsu start kv` | 9100 |
| blob | `ktsu start blob` | 9101 |
| log | `ktsu start log` | 9102 |
| memory | `ktsu start memory` | 9103 |
| envelope | `ktsu start envelope` | 9104 |
| format | `ktsu start format` | 9105 |
| validate | `ktsu start validate` | 9106 |
| transform | `ktsu start transform` | 9107 |

All built-in servers accept `--host` and `--port`. Stateful built-ins also accept `--orchestrator` (reads `KTSU_ORCHESTRATOR_URL`):

```sh
go run ./cmd/ktsu start kv --host 0.0.0.0 --port 9100 --orchestrator http://orchestrator.internal:8080
```

Stateful built-ins (`kv`, `blob`, `log`, `memory`, `envelope`) register with the orchestrator on startup and require `--orchestrator` to be set. Stateless built-ins (`format`, `validate`, `transform`) do not.

## Other commands

```sh
# Validate configuration files
ktsu validate --env environments/dev.env.yaml

# Generate ktsu.lock.yaml (not yet implemented)
ktsu lock
```

## Health checks

Every service exposes `GET /health` returning `{"status":"ok"}`.

## Docs

Architecture and configuration reference lives in [`docs/`](docs/).
