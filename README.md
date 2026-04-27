# ktsu

Run AI agent pipelines from YAML — built for auditability, safety, and simple operations.

- **Auditability** — every step's output is schema-validated and persisted; inspect exactly what each agent produced
- **Safety** — secrets never touch agent prompts; tool access is controlled via per-agent allowlists
- **Easy to operate** — plain YAML config, `ktsu validate` before you run, same setup locally and in production

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

```sh
# Set your Anthropic API key
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> .env

# Start all services
make docker-up
```

Verify all services are healthy:

```sh
curl -s http://localhost:5052/health   # gateway
curl -s http://localhost:5050/health   # orchestrator
curl -s http://localhost:5051/health   # runtime
```

Invoke the hello-world workflow:

```sh
ktsu invoke hello --input '{"name": "World"}' --wait
```

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
make build           # go build ./...
make test            # go test ./...
make lint            # go vet ./...
make docker-up       # start full stack (API key required)
make docker-down     # stop all containers
```

## Docs

- [kimitsu.ai/docs](https://kimitsu.ai/docs) — full documentation
- [./docs](docs/) — local reference: architecture, YAML spec, CLI reference
