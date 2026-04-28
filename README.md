# ktsu

Run AI agent pipelines from YAML — built for auditability, safety, and simple operations.

- **Auditability** — every step's output is schema-validated and persisted; inspect exactly what each agent produced
- **Safety** — secrets never touch agent prompts; tool access is controlled via per-agent allowlists
- **Easy to operate** — plain YAML config, `ktsu validate` before you run, same setup locally and in production

## Platform support

macOS and Linux (amd64/arm64). Windows users can run ktsu via [WSL2](https://learn.microsoft.com/en-us/windows/wsl/).

## Quick start

See [Deploy with Docker](docs/deployment/docker.md) to run the full stack locally in minutes.

## Installation

### Binary (macOS & Linux)

```sh
curl -fsSL https://raw.githubusercontent.com/kimitsu-ai/ktsu/main/install.sh | sh
```

### Docker

```sh
docker pull ghcr.io/kimitsu-ai/ktsu:latest
```

## Build & test

```sh
make build           # go build -o ktsu ./cmd/ktsu
make test            # go test ./...
make lint            # go vet ./...
make docker-up       # start full stack (API key required)
make docker-down     # stop all containers
```

## Docs

- [kimitsu.ai/ktsu](https://kimitsu.ai/ktsu) — full documentation
- [./docs](docs/) — local reference: architecture, YAML spec, CLI reference
