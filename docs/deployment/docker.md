# Deploy with Docker

## Quick start

```sh
# Set your Anthropic API key
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> .env

# Start all services
make docker-up
```

Verify all services are healthy (the orchestrator aggregates all service statuses):

```sh
curl -s http://localhost:5050/health
# {"status":"ok","services":{"gateway":"ok","orchestrator":"ok","runtime":"ok"}}
```

Invoke the hello-world workflow:

```sh
curl -s -X POST http://localhost:5050/invoke/hello \
  -H "Content-Type: application/json" \
  -d '{"name": "World"}'
```

This returns a `run_id`. Poll for the result:

```sh
curl -s http://localhost:5050/runs/<run_id>
```

If you have the `ktsu` CLI installed, these are just wrappers around the same HTTP calls:

```sh
ktsu invoke hello --input '{"name": "World"}' --wait
```

## Docker Compose

The repository ships with a Docker Compose setup under `deploy/`:

- `deploy/docker-compose.yaml` — Standard deployment using the `ghcr.io/kimitsu-ai/ktsu:latest` image with Anthropic as the LLM provider.
- `deploy/docker-compose.local.yaml` — Local LLM variant using Ollama; no API key required.

Start with Anthropic:

```bash
echo "ANTHROPIC_API_KEY=sk-ant-..." > .env
make docker-up
```

Start with a local LLM:

```bash
make docker-up-local
```

Override the exposed orchestrator port with `KTSU_PORT`:

```bash
KTSU_PORT=8080 make docker-up
```

Environment variables are injected from a `.env` file in the repository root. Do not commit this file. Add it to `.gitignore`.
