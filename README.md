# ktsu

Go monorepo for the Kimitsu agentic pipeline framework. Kimitsu treats the **tool** as the atomic unit — agents are compositions of tool servers with a prompt. All component boundaries are HTTP contracts.

[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/template/deploy?repo=https://github.com/kimitsu-ai/ktsu)

## Quick start (Railway)

The easiest way to get started is to deploy the full stack to Railway with a single click. This includes the Orchestrator, Gateway, and Runtime, pre-configured with the `hello-world` example.

> [!NOTE]
> If this is a **private repository**, ensure the [Railway GitHub App](https://railway.app/dashboard/settings/github) has access to this repo before clicking the button.

1. Click the **Deploy on Railway** button above.

2. Provide your `ANTHROPIC_API_KEY`.
3. Railway will generate a secure `KTSU_API_KEY` for you.
4. Once deployed, you can invoke the demo workflow immediately.


## Quick start (Docker)

Run the full stack with the included hello-world example:

```sh
# Set your Anthropic API key
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> .env

# Start all services
make docker-up
```

This launches the gateway, orchestrator, and runtime with the `workflows/` project mounted. Verify all services are healthy:

```sh
# Check each service (should return {"status":"ok"})
curl -s http://localhost:5052/health   # gateway
curl -s http://localhost:5050/health   # orchestrator
curl -s http://localhost:5051/health   # runtime
```

Once healthy, invoke the workflow with the ktsu CLI:

```sh
# Wait for the workflow to complete and print the result
./ktsu invoke hello --input "{\"name\": \"Kyle\"}" --wait
```

Or with curl:

```sh
curl -s -X POST http://localhost:${KTSU_PORT:-5050}/invoke/hello \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${KTSU_API_KEY}" \
  -d '{"name": "World"}'
# => {"run_id":"run_a1b2c3...","status":"accepted"}
```

The invoke endpoint returns immediately with a `run_id`. Poll the run to check progress:

```sh
```sh
curl -s -H "Authorization: Bearer ${KTSU_API_KEY}" http://localhost:${KTSU_PORT:-5050}/runs/<run_id>
```

The host port is configurable via `KTSU_PORT` in `.env` (default: `5050`).

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

- Go 1.25+ (for local development)
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
| Orchestrator | `ktsu start orchestrator` | 5050 |
| LLM Gateway | `ktsu start gateway` | 5052 |
| Agent Runtime | `ktsu start runtime` | 5051 |

Run all three together in a single process:

```sh
go run ./cmd/ktsu start --all
go run ./cmd/ktsu start --all --env environments/dev.env.yaml
```

Or start each service individually. Every service accepts `--host` (bind interface, default `""` = all interfaces) and `--port`:

```sh
# Orchestrator (control plane)
go run ./cmd/ktsu start orchestrator

# With an environment config and custom address
go run ./cmd/ktsu start orchestrator --env environments/dev.env.yaml --host 0.0.0.0 --port 5050

# LLM Gateway
go run ./cmd/ktsu start gateway --config gateway.yaml --port 5052

# Agent Runtime — point at the orchestrator and gateway
go run ./cmd/ktsu start runtime \
  --orchestrator http://orchestrator.internal:5050 \
  --gateway http://llm-gateway.internal:5052
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
| `KTSU_ORCHESTRATOR_PORT` | `--port` | orchestrator | `5050` |
| `KTSU_GATEWAY_HOST` | `--host` | gateway | `""` |
| `KTSU_GATEWAY_PORT` | `--port` | gateway | `5052` |
| `KTSU_RUNTIME_HOST` | `--host` | runtime | `""` |
| `KTSU_RUNTIME_PORT` | `--port` | runtime | `5051` |
| `KTSU_ORCHESTRATOR_URL` | `--orchestrator` | runtime, builtins, invoke | `http://localhost:5050` |
| `KTSU_GATEWAY_URL` | `--gateway` | runtime | `http://localhost:5052` |
| `KTSU_API_KEY` | `--api-key` | orchestrator, invoke | `""` (auth disabled) |
| `KTSU_STORE_TYPE` | `--store-type` | orchestrator | `memory` |
| `KTSU_DB_PATH` | `--db-path` | orchestrator | `ktsu.db` |

```sh
# Container / multi-host example
KTSU_ORCHESTRATOR_URL=http://orchestrator.internal:5050 \
KTSU_GATEWAY_URL=http://llm-gateway.internal:5052 \
  go run ./cmd/ktsu start runtime
```

### Built-in tool servers

| Server | Command | Default port |
|---|---|---|
| envelope | `ktsu start envelope` | 9104 |

The envelope server accepts `--host`, `--port`, and `--orchestrator` (reads `KTSU_ORCHESTRATOR_URL`):

```sh
go run ./cmd/ktsu start envelope --host 0.0.0.0 --port 9104 --orchestrator http://orchestrator.internal:5050
```

The envelope server registers with the orchestrator on startup and requires `--orchestrator` to be set.

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
