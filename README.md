# ktsu

Go monorepo for the Kimitsu agentic pipeline framework. Kimitsu treats the **tool** as the atomic unit — agents are compositions of tool servers with a prompt. All component boundaries are HTTP contracts.

## Prerequisites

- Go 1.24+

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

```sh
# Orchestrator (control plane)
go run ./cmd/kimitsu start orchestrator

# With an environment config
go run ./cmd/kimitsu start orchestrator --env environments/dev.env.yaml

# LLM Gateway
go run ./cmd/kimitsu start gateway --config gateway.yaml

# Agent Runtime
go run ./cmd/kimitsu start runtime --orchestrator http://localhost:8080 --gateway http://localhost:8081
```

Shortcut for the orchestrator:

```sh
make run-orchestrator
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

```sh
go run ./cmd/kimitsu start kv --port 9100 --orchestrator http://localhost:8080
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
