# ktsu CLI Reference

`ktsu` is the command-line interface for the Kimitsu agentic pipeline framework. Use it to start services, invoke workflows, validate configuration, and scaffold new projects.

See also: [YAML Spec](yaml-spec/index.md) · [Overview](kimitsu-overview.md) · [Configuration](kimitsu-configuration.md)

---

## Quick Reference

| Command | Purpose | When to use |
|---|---|---|
| `ktsu start --all` | Start all core services in one process | Local dev / single-host deployments |
| `ktsu start orchestrator` | Start the control plane | Running the full stack or orchestrator only |
| `ktsu start runtime` | Start the agent executor | Running the full stack or runtime only |
| `ktsu start gateway` | Start the LLM gateway | Running the full stack or gateway only |
| `ktsu start envelope` | Start the shipped envelope tool server | Adding the envelope server to a running stack |
| `ktsu invoke <workflow>` | Invoke a workflow | Development and testing |
| `ktsu runs [--workflow <name>]` | List workflow runs | Discover and filter past runs |
| `ktsu runs get <run_id>` | Print the full envelope for a run | Inspect step outputs and metrics |
| `ktsu workflow tree <workflow-file>` | Print full dependency tree | Auditing sub-workflow, agent, and server references |
| `ktsu validate [project-dir]` | Validate config files | CI, pre-deploy checks, local debugging |
| `ktsu new project <name>` | Scaffold a new project | Starting a new Kimitsu project |
| `ktsu workflow tree <file>` | Emit dependency tree for a workflow | Auditing dependencies, bundling for publish |
| `ktsu hub install <target>` | Install a workflow from ktsuhub or a git repo | Adding hub workflows to a project. Requires `KTSU_HUB_ENABLED=true` |
| `ktsu hub update` | Re-resolve all entries in ktsuhub.lock.yaml | Updating installed workflows. Requires `KTSU_HUB_ENABLED=true` |
| `ktsu hub publish` | Publish workflows to ktsuhub | Sharing your workflows. Requires `KTSU_HUB_ENABLED=true` |
| `ktsu hub login` | Authenticate with GitHub | Required before publish. Requires `KTSU_HUB_ENABLED=true` |
| `ktsu hub search <query>` | Search ktsuhub from the CLI | Discovering workflows. Requires `KTSU_HUB_ENABLED=true` |
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
| `KTSU_ORCHESTRATOR_PORT` | `5050` | `start orchestrator` | Port to listen on |
| `KTSU_ORCHESTRATOR_URL` | `http://localhost:5050` | `start runtime`, `start envelope`, `invoke` | Orchestrator URL |
| `KTSU_OWN_URL` | `""` | `start orchestrator` | Orchestrator's own URL for callbacks |
| `KTSU_PROJECT_DIR` | `.` | `start orchestrator` | Project root for resolving agent/server paths |
| `KTSU_GATEWAY_HOST` | `""` (all interfaces) | `start gateway` | Host interface to bind |
| `KTSU_GATEWAY_PORT` | `5052` | `start gateway` | Port to listen on |
| `KTSU_GATEWAY_URL` | `http://localhost:5052` | `start runtime` | LLM gateway URL |
| `KTSU_RUNTIME_HOST` | `""` (all interfaces) | `start runtime` | Host interface to bind |
| `KTSU_RUNTIME_PORT` | `5051` | `start runtime` | Port to listen on |
| `KTSU_RUNTIME_URL` | `""` | `start orchestrator` | Agent runtime URL |
| `KTSU_STORE_TYPE` | `memory` | `start orchestrator` | Orchestrator store type: `memory`, `sqlite` |
| `KTSU_DB_PATH` | `ktsu.db` | `start orchestrator` | Database path for SQLite |
| `KTSU_HUB_ENABLED` | `false` | all | Enable hub commands (`ktsu hub *`) |
| `KTSU_CACHE_DIR` | `~/.ktsu/cache` | `hub install`, `hub update` | Local cache directory for installed workflows |
| `NO_COLOR` | *(unset)* | `validate` | Set to any value to disable colored output |

---

## ktsu start

Start a Kimitsu service or shipped tool server. Every service exposes `GET /health` returning `{"status":"ok"}`.

### ktsu start --all

Run orchestrator, gateway, and runtime together in a single process. Service URLs are derived automatically from the configured ports. Log output is prefixed per service (`[orchestrator]`, `[gateway]`, `[runtime]`).

```
ktsu start --all [flags]
```

| Flag | Default | Env | Description |
|---|---|---|---|
| `--all` | `false` | — | Enable all-in-one mode (required) |
| `--env` | `""` | — | Path to environment config (e.g. `environments/dev.env.yaml`) |
| `--workflow-dir` | `./workflows` | — | Directory of `*.workflow.yaml` files |
| `--own-url` | `""` | `KTSU_OWN_URL` | Orchestrator's own URL for callbacks |
| `--project-dir` | `.` | `KTSU_PROJECT_DIR` | Project root for resolving agent/server paths |
| `--orchestrator-host` | `""` | `KTSU_ORCHESTRATOR_HOST` | Orchestrator bind host |
| `--orchestrator-port` | `5050` | `KTSU_ORCHESTRATOR_PORT` | Orchestrator port |
| `--gateway-config` | `gateway.yaml` | — | Path to gateway config file |
| `--gateway-host` | `""` | `KTSU_GATEWAY_HOST` | Gateway bind host |
| `--gateway-port` | `5052` | `KTSU_GATEWAY_PORT` | Gateway port |
| `--runtime-host` | `""` | `KTSU_RUNTIME_HOST` | Runtime bind host |
| `--runtime-port` | `5051` | `KTSU_RUNTIME_PORT` | Runtime port |
| `--store-type` | `memory` | `KTSU_STORE_TYPE` | Orchestrator store type: `memory`, `sqlite` |
| `--db-path` | `ktsu.db` | `KTSU_DB_PATH` | Database path for SQLite |

```bash
ktsu start --all
ktsu start --all --env environments/dev.env.yaml --gateway-config gateway.yaml
```

---

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
| `--port` | `5050` | `KTSU_ORCHESTRATOR_PORT` | Port to listen on |
| `--runtime-url` | `""` | `KTSU_RUNTIME_URL` | Agent runtime URL |
| `--own-url` | `""` | `KTSU_OWN_URL` | Orchestrator's own URL for callbacks |
| `--project-dir` | `.` | `KTSU_PROJECT_DIR` | Project root for resolving agent/server paths |
| `--store-type` | `memory` | `KTSU_STORE_TYPE` | Orchestrator store type: `memory`, `sqlite` |
| `--db-path` | `ktsu.db` | `KTSU_DB_PATH` | Database path for SQLite |
| `--workspace` | *(none)* | — | Additional workspace root. Repeatable. |
| `--no-hub-lock` | `false` | — | Ignore `ktsuhub.lock.yaml` even if present |

```bash
ktsu start orchestrator
ktsu start orchestrator --env environments/dev.env.yaml --port 5050
```

---

#### ktsu start runtime

Start the agent executor. Pulls work from the orchestrator and runs agent steps using the LLM gateway.

```
ktsu start runtime [flags]
```

| Flag | Default | Env | Description |
|---|---|---|---|
| `--orchestrator` | `http://localhost:5050` | `KTSU_ORCHESTRATOR_URL` | Orchestrator URL |
| `--gateway` | `http://localhost:5052` | `KTSU_GATEWAY_URL` | LLM gateway URL |
| `--host` | `""` | `KTSU_RUNTIME_HOST` | Host interface to bind |
| `--port` | `5051` | `KTSU_RUNTIME_PORT` | Port to listen on |

```bash
ktsu start runtime
ktsu start runtime --orchestrator http://orchestrator:5050 --gateway http://gateway:5052
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
| `--port` | `5052` | `KTSU_GATEWAY_PORT` | Port to listen on |

```bash
ktsu start gateway
ktsu start gateway --config gateway.yaml --port 5052
```

---

### Shipped Tool Servers

The envelope server is the sole shipped MCP-compatible tool provider. It registers with the orchestrator on startup and requires `--orchestrator`.

#### `ktsu start envelope` (requires `--orchestrator`)

| Command | Default Port | Description |
|---|---|---|
| `ktsu start envelope` | `9104` | Envelope/context management |

**Flags:**

| Flag | Default | Env | Description |
|---|---|---|---|
| `--host` | `""` | — | Host interface to bind |
| `--port` | `9104` | — | Port to listen on |
| `--orchestrator` | `http://localhost:5050` | `KTSU_ORCHESTRATOR_URL` | Orchestrator URL for registration |

```bash
ktsu start envelope
ktsu start envelope --port 9104 --orchestrator http://orchestrator:5050
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
| `--orchestrator` | `http://localhost:5050` | `KTSU_ORCHESTRATOR_URL` | Orchestrator URL |

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
ktsu invoke hello --orchestrator http://example.com:5050 --wait
```

---

## `ktsu runs`

List runs from a running orchestrator, filtered by workflow name or status.

```
ktsu runs [--workflow <name>] [--status <status>] [--limit <n>] [--orchestrator <url>]
```

Output columns: run ID, workflow name, status, started timestamp, duration.

| Flag | Default | Description |
|---|---|---|
| `--workflow` | — | Filter by workflow name (exact match) |
| `--status` | — | Filter by status: `pending`, `running`, `complete`, `failed` |
| `--limit` | 50 | Maximum results to return |
| `--orchestrator` | `http://localhost:5050` | Orchestrator URL (env: `KTSU_ORCHESTRATOR_URL`) |

## `ktsu runs get <run_id>`

Print the full envelope for a run as JSON. Includes step outputs, per-step metrics, and aggregated totals. On failure, shows all steps that completed before the failure.

```
ktsu runs get <run_id> [--orchestrator <url>]
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
| `--workflow-dir` | `""` | Directory of `*.workflow.yaml` files (defaults to `<project-dir>/workflows`) |
| `--graph` | `false` | Output Mermaid graph of workflows instead of text summary |
| `--workspace` | *(none)* | Additional workspace root to include in validation. Repeatable. |
| `--no-hub-lock` | `false` | Ignore `ktsuhub.lock.yaml` even if present |

Note: `--workflow-dir` defaults to `""` and derives the path from `[project-dir]` at runtime — unlike `ktsu start orchestrator`, which defaults to `./workflows` literally.

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

# Include an additional workspace in validation
ktsu validate --workspace ~/shared-workflows

# Ignore hub lock file
ktsu validate --no-hub-lock
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
├── servers.yaml                     # empty server manifest
└── ktsuhub.yaml                     # empty hub manifest, ready to fill in
```

```bash
ktsu new project my-app
cd my-app
ktsu validate
```

---

## ktsu workflow

Commands for inspecting and working with workflow files.

### ktsu workflow tree

Walk a workflow file and emit the full dependency tree: the workflow file, all referenced agent files, all referenced local server files, and `gateway.yaml`. Paths are deduplicated and relative to `--project-dir`.

```
ktsu workflow tree <workflow-file> [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--json` | `false` | Output a flat JSON array of relative paths |
| `--project-dir` | `.` | Project root for resolving relative paths |

```bash
# Human-readable tree
ktsu workflow tree workflows/my-workflow.workflow.yaml

# JSON output for tooling
ktsu workflow tree workflows/my-workflow.workflow.yaml --json
```

---

## ktsu hub

Commands for interacting with the ktsuhub workflow registry.

> **Requires `KTSU_HUB_ENABLED=true`.** Hub commands are hidden and unavailable unless this environment variable is set.

```bash
KTSU_HUB_ENABLED=true ktsu hub install github.com/kyle/workflows
```

### ktsu hub install

Install a workflow from a git repository into the local project. Clones the source into `KTSU_CACHE_DIR` (default `~/.ktsu/cache`) and writes an entry to `ktsuhub.lock.yaml`.

```
ktsu hub install <target>[@ref] [flags]
```

**Supported target formats:**

| Format | Example |
|---|---|
| `github.com/owner/repo[@ref]` | `github.com/kyle/workflows@v1.2.0` |
| `https://...[@ref]` | `https://gitlab.com/org/repo@main` |
| Local path | `./local-workflows` |
| `owner/name` (short form) | `kyle/support-triage` — **not yet implemented** |

Short-form `owner/name` targets return an error: `registry install (short form "owner/name") not yet implemented — use github.com/owner/repo or https://...`

| Flag | Default | Description |
|---|---|---|
| `--dry-run` | `false` | Show what would be installed without making changes |
| `--cache-dir` | `~/.ktsu/cache` | Override the local cache directory |

```bash
ktsu hub install github.com/kyle/workflows
ktsu hub install github.com/kyle/workflows@v1.2.0
ktsu hub install https://gitlab.com/org/repo@main
ktsu hub install github.com/kyle/workflows --dry-run
```

After install, `ktsu start orchestrator` automatically picks up the new workflow from `ktsuhub.lock.yaml`.

---

### ktsu hub update

Re-resolve all entries in `ktsuhub.lock.yaml`. For mutable branch refs, fetches the latest commit. For pinned version/SHA entries, this is a no-op unless `--latest` is passed.

```
ktsu hub update [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--latest` | `false` | Also update pinned version entries to the latest published version |
| `--dry-run` | `false` | Show what would change without writing |

```bash
ktsu hub update
ktsu hub update --latest
ktsu hub update --dry-run
```

---

### ktsu hub login

> **Not yet implemented.** Returns `hub login: not yet implemented`.

Authenticate with GitHub for publishing workflows to the registry.

```
ktsu hub login
```

---

### ktsu hub publish

> **Not yet implemented.** Returns `hub publish: not yet implemented`.

Publish workflows declared in `ktsuhub.yaml` to the ktsuhub registry.

```
ktsu hub publish [flags]
```

---

### ktsu hub search

> **Not yet implemented.** Returns `hub search: not yet implemented`.

Search the ktsuhub registry from the CLI.

```
ktsu hub search <query> [flags]
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
| Orchestrator | 5050 | `ktsu start orchestrator` |
| LLM Gateway | 5052 | `ktsu start gateway` |
| Agent Runtime | 5051 | `ktsu start runtime` |
| envelope | 9104 | `ktsu start envelope` |

All ports are configurable via `--port`.

---

## See Also

- [YAML Spec Index](yaml-spec/index.md) — config file formats (workflow, agent, gateway, env, servers)
- [Overview](kimitsu-overview.md) — architecture and concepts
- [Configuration](kimitsu-configuration.md) — configuration reference
- [Tool Servers](kimitsu-tool-servers.md) — shipped tool server details
- [Pipeline Primitives](kimitsu-pipeline-primitives.md) — step types and pipeline mechanics
