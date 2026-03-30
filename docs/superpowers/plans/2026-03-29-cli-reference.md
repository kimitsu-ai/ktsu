# CLI Reference Doc Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create `docs/cli-reference.md` — a single comprehensive CLI reference for developers and AI agents.

**Architecture:** Single flat markdown file covering all commands, flags, env vars, examples, and port mappings. Cross-links to existing yaml-spec docs. No duplication of config file format details.

**Tech Stack:** Markdown only. Source of truth: `cmd/ktsu/main.go`.

---

### Task 1: Write `docs/cli-reference.md`

**Files:**
- Create: `docs/cli-reference.md`

- [ ] **Step 1: Write the file**

Create `docs/cli-reference.md` with the following content exactly:

```markdown
# ktsu CLI Reference

`ktsu` is the command-line interface for the Kimitsu agentic pipeline framework. Use it to start services, invoke workflows, validate configuration, and scaffold new projects.

See also: [YAML Spec](yaml-spec/index.md) · [Overview](kimitsu-overview.md) · [Configuration](kimitsu-configuration.md)

---

## Quick Reference

| Command | Purpose | When to use |
|---|---|---|
| `ktsu start orchestrator` | Start the control plane | Running the full stack or orchestrator only |
| `ktsu start runtime` | Start the agent executor | Running the full stack or runtime only |
| `ktsu start gateway` | Start the LLM gateway | Running the full stack or gateway only |
| `ktsu start kv\|blob\|log\|memory\|envelope` | Start a stateful built-in tool server | Adding built-in state/storage tools to a running stack |
| `ktsu start format\|validate\|transform\|cli` | Start a stateless built-in tool server | Adding built-in utility tools to a running stack |
| `ktsu invoke <workflow>` | Invoke a workflow | Development and testing |
| `ktsu validate [project-dir]` | Validate config files | CI, pre-deploy checks, local debugging |
| `ktsu new project <name>` | Scaffold a new project | Starting a new Kimitsu project |
| `ktsu lock` | Generate ktsu.lock.yaml | *(not yet implemented)* |
| `ktsu completion <shell>` | Generate shell completion script | One-time shell setup |

---

## Global Flags

| Flag | Description |
|---|---|
| `-h, --help` | Print help for any command |

---

## Environment Variables

CLI flags take precedence over environment variables. All env vars are optional.

| Variable | Default | Affects | Description |
|---|---|---|---|
| `KTSU_ORCHESTRATOR_HOST` | `""` (all interfaces) | `start orchestrator` | Host interface to bind |
| `KTSU_ORCHESTRATOR_PORT` | `8080` | `start orchestrator` | Port to listen on |
| `KTSU_ORCHESTRATOR_URL` | `http://localhost:8080` | `start runtime`, `start kv/blob/log/memory/envelope`, `invoke` | Orchestrator URL |
| `KTSU_OWN_URL` | `""` | `start orchestrator` | Orchestrator's own URL for callbacks |
| `KTSU_PROJECT_DIR` | `.` | `start orchestrator` | Project root for resolving agent/server paths |
| `KTSU_GATEWAY_HOST` | `""` (all interfaces) | `start gateway` | Host interface to bind |
| `KTSU_GATEWAY_PORT` | `8081` | `start gateway` | Port to listen on |
| `KTSU_GATEWAY_URL` | `http://localhost:8081` | `start runtime` | LLM gateway URL |
| `KTSU_RUNTIME_HOST` | `""` (all interfaces) | `start runtime` | Host interface to bind |
| `KTSU_RUNTIME_PORT` | `8082` | `start runtime` | Port to listen on |
| `KTSU_RUNTIME_URL` | `""` | `start orchestrator` | Agent runtime URL |
| `NO_COLOR` | *(unset)* | `validate` | Set to any value to disable colored output |

---

## ktsu start

Start a Kimitsu service or built-in tool server. Every service exposes `GET /health` returning `{"status":"ok"}`.

### Core Services

#### ktsu start orchestrator

Start the control plane. Loads workflow definitions and coordinates pipeline execution.

```
ktsu start orchestrator [flags]
```

| Flag | Default | Env | Description |
|---|---|---|---|
| `--env` | `""` | — | Path to environment config (e.g. `environments/dev.env.yaml`) |
| `--workflow-dir` | `./workflows` | — | Directory of `*.workflow.yaml` files |
| `--host` | `""` | `KTSU_ORCHESTRATOR_HOST` | Host interface to bind |
| `--port` | `8080` | `KTSU_ORCHESTRATOR_PORT` | Port to listen on |
| `--runtime-url` | `""` | `KTSU_RUNTIME_URL` | Agent runtime URL |
| `--own-url` | `""` | `KTSU_OWN_URL` | Orchestrator's own URL for callbacks |
| `--project-dir` | `.` | `KTSU_PROJECT_DIR` | Project root for resolving agent/server paths |

```bash
ktsu start orchestrator
ktsu start orchestrator --env environments/dev.env.yaml --port 8080
```

---

#### ktsu start runtime

Start the agent executor. Pulls work from the orchestrator and runs agent steps using the LLM gateway.

```
ktsu start runtime [flags]
```

| Flag | Default | Env | Description |
|---|---|---|---|
| `--orchestrator` | `http://localhost:8080` | `KTSU_ORCHESTRATOR_URL` | Orchestrator URL |
| `--gateway` | `http://localhost:8081` | `KTSU_GATEWAY_URL` | LLM gateway URL |
| `--host` | `""` | `KTSU_RUNTIME_HOST` | Host interface to bind |
| `--port` | `8082` | `KTSU_RUNTIME_PORT` | Port to listen on |

```bash
ktsu start runtime
ktsu start runtime --orchestrator http://orchestrator:8080 --gateway http://gateway:8081
```

---

#### ktsu start gateway

Start the LLM gateway. Routes model calls to configured providers based on `gateway.yaml`. See [gateway.md](yaml-spec/gateway.md) for config format.

```
ktsu start gateway [flags]
```

| Flag | Default | Env | Description |
|---|---|---|---|
| `--config` | `gateway.yaml` | — | Path to gateway config file |
| `--host` | `""` | `KTSU_GATEWAY_HOST` | Host interface to bind |
| `--port` | `8081` | `KTSU_GATEWAY_PORT` | Port to listen on |

```bash
ktsu start gateway
ktsu start gateway --config gateway.yaml --port 8081
```

---

### Built-in Tool Servers

Built-in servers are MCP-compatible tool providers. Stateful servers register with the orchestrator on startup; stateless servers do not.

#### Stateful (require `--orchestrator`)

| Command | Default Port | Description |
|---|---|---|
| `ktsu start kv` | `9100` | Key-value store |
| `ktsu start blob` | `9101` | Blob storage |
| `ktsu start log` | `9102` | Structured logging |
| `ktsu start memory` | `9103` | In-memory state |
| `ktsu start envelope` | `9104` | Envelope/context management |

**Flags (all stateful builtins):**

| Flag | Default | Env | Description |
|---|---|---|---|
| `--host` | `""` | — | Host interface to bind |
| `--port` | *(see table above)* | — | Port to listen on |
| `--orchestrator` | `http://localhost:8080` | `KTSU_ORCHESTRATOR_URL` | Orchestrator URL for registration |

```bash
ktsu start kv
ktsu start kv --port 9100 --orchestrator http://orchestrator:8080
```

#### Stateless (no orchestrator needed)

| Command | Default Port | Description |
|---|---|---|
| `ktsu start format` | `9105` | Data formatting |
| `ktsu start validate` | `9106` | Data validation |
| `ktsu start transform` | `9107` | Data transformation |
| `ktsu start cli` | `9108` | CLI command execution |

**Flags (all stateless builtins):**

| Flag | Default | Description |
|---|---|---|
| `--host` | `""` | Host interface to bind |
| `--port` | *(see table above)* | Port to listen on |

```bash
ktsu start format
ktsu start transform --port 9107
```

---

## ktsu invoke

Invoke a named workflow on a running orchestrator. Primarily used for development and testing. Posts to `POST /invoke/<workflow>` and optionally polls until completion.

```
ktsu invoke <workflow> [flags]
```

| Flag | Default | Env | Description |
|---|---|---|---|
| `--input` | `{}` | — | JSON input for the workflow |
| `--wait` | `false` | — | Poll until the run completes and print result |
| `--orchestrator` | `http://localhost:8080` | `KTSU_ORCHESTRATOR_URL` | Orchestrator URL |

**Without `--wait`:** prints `run_id: <id>` immediately and exits.

**With `--wait`:** polls `GET /runs/<run_id>` every second until the run reaches a terminal status, then prints the full result as JSON.

```bash
# Fire and forget
ktsu invoke hello

# With input
ktsu invoke hello --input '{"name": "World"}'

# Wait for result
ktsu invoke hello --input '{"name": "World"}' --wait

# Remote orchestrator
ktsu invoke hello --orchestrator http://example.com:8080 --wait
```

---

## ktsu validate

Validate Kimitsu configuration files without starting any services. Checks DAG cycles, `depends_on` references, agent and server file existence, and parses all referenced YAML. Useful for CI and pre-deploy checks.

```
ktsu validate [project-dir] [flags]
```

| Argument/Flag | Default | Description |
|---|---|---|
| `[project-dir]` | `.` | Project root directory |
| `--env` | `""` | Path to environment config to validate |
| `--workflow-dir` | `<project-dir>/workflows` | Directory of `*.workflow.yaml` files |
| `--graph` | `false` | Output Mermaid graph of workflows instead of text summary |

**Default output** (without `--graph`): colored text grouped by Workflows / Agents / Servers / Systems, with a summary line.

**`--graph` output**: Mermaid `graph TD` diagram per workflow showing steps, dependencies, external file references, and error indicators.

Color output is auto-detected on TTY. Set `NO_COLOR` to disable.

```bash
# Validate current directory
ktsu validate

# Validate a specific project
ktsu validate /path/to/project

# With environment config
ktsu validate --env environments/dev.env.yaml

# Generate Mermaid graph
ktsu validate --graph

# Custom workflow directory
ktsu validate --workflow-dir ./my-workflows
```

---

## ktsu new

Scaffold new Kimitsu resources.

### ktsu new project

Bootstrap a new project directory with a minimal working scaffold. Errors if the directory already exists.

```
ktsu new project <name>
```

Creates:

```
<name>/
├── workflows/<name>.workflow.yaml   # workflow definition
├── agents/placeholder.agent.yaml   # placeholder agent
├── environments/dev.env.yaml        # dev environment config (SQLite state)
├── gateway.yaml                     # empty gateway config
└── servers.yaml                     # empty server manifest
```

```bash
ktsu new project my-app
cd my-app
ktsu validate
```

---

## ktsu lock

Generate `ktsu.lock.yaml` to pin dependency versions and validate IO compatibility.

> **Not yet implemented.** Prints `lock: not implemented` and exits successfully.

```bash
ktsu lock
```

---

## ktsu completion

Generate a shell autocompletion script.

```
ktsu completion <shell>
```

Supported shells: `bash`, `zsh`, `fish`, `powershell`.

```bash
# Bash
ktsu completion bash | sudo tee /etc/bash_completion.d/ktsu

# Zsh
ktsu completion zsh | sudo tee /usr/share/zsh/site-functions/_ktsu

# Fish
ktsu completion fish | sudo tee /usr/share/fish/vendor_completions.d/ktsu.fish

# PowerShell
ktsu completion powershell | Out-String | Out-File -FilePath $profile
```

---

## Port Reference

| Service | Default Port | Command |
|---|---|---|
| Orchestrator | 8080 | `ktsu start orchestrator` |
| LLM Gateway | 8081 | `ktsu start gateway` |
| Agent Runtime | 8082 | `ktsu start runtime` |
| kv | 9100 | `ktsu start kv` |
| blob | 9101 | `ktsu start blob` |
| log | 9102 | `ktsu start log` |
| memory | 9103 | `ktsu start memory` |
| envelope | 9104 | `ktsu start envelope` |
| format | 9105 | `ktsu start format` |
| validate | 9106 | `ktsu start validate` |
| transform | 9107 | `ktsu start transform` |
| cli | 9108 | `ktsu start cli` |

All ports are configurable via `--port`.

---

## See Also

- [YAML Spec Index](yaml-spec/index.md) — config file formats (workflow, agent, gateway, env, servers)
- [Overview](kimitsu-overview.md) — architecture and concepts
- [Configuration](kimitsu-configuration.md) — configuration reference
- [Tool Servers](kimitsu-tool-servers.md) — built-in tool server details
- [Pipeline Primitives](kimitsu-pipeline-primitives.md) — step types and pipeline mechanics
```

- [ ] **Step 2: Verify flag names and defaults against source**

Open `cmd/ktsu/main.go` and confirm:
- `startOrchestratorCmd` flags match the table (lines 136-148)
- `startRuntimeCmd` flags match (lines 169-177)
- `startGatewayCmd` flags match (lines 208-211)
- `startBuiltinCmd` stateful vs stateless split matches (`hasOrchestrator` param, lines 95-103)
- `invokeCmd` flags match (lines 303-308)
- `validateCmd` flags match (lines 363-367)
- All default ports match (lines 95-103)

- [ ] **Step 3: Verify cross-links exist**

Confirm these files exist:
- `docs/yaml-spec/index.md` ✓
- `docs/kimitsu-overview.md` ✓
- `docs/kimitsu-configuration.md` ✓
- `docs/kimitsu-tool-servers.md` ✓
- `docs/kimitsu-pipeline-primitives.md` ✓
- `docs/yaml-spec/gateway.md` ✓

- [ ] **Step 4: Commit**

```bash
git add docs/cli-reference.md docs/superpowers/specs/2026-03-29-cli-reference-design.md docs/superpowers/plans/2026-03-29-cli-reference.md
git commit -m "docs: add CLI reference doc"
```
