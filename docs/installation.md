# Installation

## Prerequisites

- **Docker and Docker Compose** — required for the Docker path
- **An Anthropic API key** — set as `ANTHROPIC_API_KEY` in your environment, or use the local LLM option below (no API key needed)

---

## Option A: Binary

The install script auto-detects your OS and architecture (macOS and Linux, amd64/arm64).

**Public repo / anonymous:**
```bash
curl -fsSL https://raw.githubusercontent.com/kimitsu-ai/ktsu/main/install.sh | sh
```

**Private repo — using `gh` CLI (recommended if authenticated):**
```bash
gh release download --repo kimitsu-ai/ktsu -p install.sh -O - | sh
```

**Private repo — using a GitHub token:**
```bash
curl -H "Authorization: token $GITHUB_TOKEN" \
  -fsSL https://raw.githubusercontent.com/kimitsu-ai/ktsu/main/install.sh | sh
```

The script installs the `ktsu` binary to `/usr/local/bin` by default. Override with `INSTALL_DIR=/your/path`.

---

## Option B: Docker Compose

Runs the full stack (orchestrator, runtime, gateway) as containers.

**With Anthropic API (requires `ANTHROPIC_API_KEY`):**
```bash
echo "ANTHROPIC_API_KEY=sk-ant-..." > .env
make docker-up
```

**With a local LLM (no API key needed):**
```bash
make docker-up-local
```

This variant starts Ollama and pulls `qwen2.5:0.5b` (~397 MB) automatically.

---

## Verify

Once services are running, all three health endpoints should return `{"status":"ok"}`:

```bash
curl -s http://localhost:5050/health   # orchestrator
curl -s http://localhost:5051/health   # agent runtime
curl -s http://localhost:5052/health   # LLM gateway
```

---

## Configuration Reference

| Variable | Default | Description |
|---|---|---|
| `ANTHROPIC_API_KEY` | — | Anthropic API key (required unless using local LLM) |
| `KTSU_ORCHESTRATOR_URL` | `http://localhost:5050` | Orchestrator address used by the CLI |
| `KTSU_ORCHESTRATOR_PORT` | `5050` | Port the orchestrator listens on |
| `KTSU_RUNTIME_PORT` | `5051` | Port the agent runtime listens on |
| `KTSU_GATEWAY_PORT` | `5052` | Port the LLM gateway listens on |
| `KTSU_API_KEY` | — | Optional auth token for the orchestrator API |
| `KTSU_STORE_TYPE` | `memory` | State store backend: `memory` or `sqlite` |
| `KTSU_DB_PATH` | `ktsu.db` | SQLite database path (when `KTSU_STORE_TYPE=sqlite`) |
