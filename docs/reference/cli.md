---
description: "Complete CLI reference: start, validate, invoke, runs, runs get, workflow tree, lock, new project, and hub subcommands with all flags and defaults."
---

# ktsu CLI Reference

`ktsu` is the command-line interface for the Kimitsu agentic pipeline framework. Use it to start services, invoke workflows, validate configuration, inspect runs, and scaffold new projects.

---

## Global Service Ports

| Service | Default Port | Env Override |
|---|---|---|
| Orchestrator | 5050 | `KTSU_ORCHESTRATOR_PORT` |
| Agent Runtime | 5051 | `KTSU_RUNTIME_PORT` |
| LLM Gateway | 5052 | `KTSU_GATEWAY_PORT` |

Most commands that contact a running service accept `--orchestrator` (default: `http://localhost:5050`) and respect `KTSU_ORCHESTRATOR_URL`.

---

## Commands

- [`ktsu start`](#ktsu-start) — Start one or all services
- [`ktsu validate`](#ktsu-validate) — Validate configuration files
- [`ktsu invoke`](#ktsu-invoke) — Trigger a workflow run
- [`ktsu runs`](#ktsu-runs) — List workflow runs
- [`ktsu runs get`](#ktsu-runs-get) — Inspect a specific run
- [`ktsu workflow tree`](#ktsu-workflow-tree) — Visualize workflow dependencies
- [`ktsu lock`](#ktsu-lock) — Generate a lockfile
- [`ktsu new project`](#ktsu-new-project) — Scaffold a new project
- [`ktsu hub`](#ktsu-hub) — Interact with the Kimitsu Hub *(requires `KTSU_HUB_ENABLED=true`)*

---

## `ktsu start`

Start one or all Kimitsu services.

Running `ktsu start` with `--all` launches the Orchestrator, Agent Runtime, and LLM Gateway in a single process. Alternatively, start each service individually using subcommands.

### `ktsu start --all`

Starts all three services in a single process.

```bash
ktsu start --all --env environments/dev.env.yaml
```

| Flag | Default | Description |
|---|---|---|
| `--all` | `false` | Start orchestrator, gateway, and runtime together |
| `--env` | `""` | Path to environment config (e.g. `environments/dev.env.yaml`) |
| `--workflow-dir` | `./workflows` | Path to workflow directory |
| `--project-dir` | `.` | Project root for resolving agent/server paths (env: `KTSU_PROJECT_DIR`) |
| `--own-url` | `""` | Orchestrator's own URL for callbacks (env: `KTSU_OWN_URL`) |
| `--orchestrator-host` | `""` | Orchestrator bind host (env: `KTSU_ORCHESTRATOR_HOST`) |
| `--orchestrator-port` | `5050` | Orchestrator port (env: `KTSU_ORCHESTRATOR_PORT`) |
| `--store-type` | `memory` | State store: `memory` or `sqlite` (env: `KTSU_STORE_TYPE`) |
| `--db-path` | `ktsu.db` | Database path when using sqlite (env: `KTSU_DB_PATH`) |
| `--gateway-config` | `gateway.yaml` | Path to gateway config |
| `--gateway-host` | `""` | Gateway bind host (env: `KTSU_GATEWAY_HOST`) |
| `--gateway-port` | `5052` | Gateway port (env: `KTSU_GATEWAY_PORT`) |
| `--runtime-host` | `""` | Runtime bind host (env: `KTSU_RUNTIME_HOST`) |
| `--runtime-port` | `5051` | Runtime port (env: `KTSU_RUNTIME_PORT`) |

---

### `ktsu start orchestrator`

Starts only the Orchestrator service.

```bash
ktsu start orchestrator --env environments/dev.env.yaml --port 5050
```

| Flag | Default | Description |
|---|---|---|
| `--env` | `""` | Path to environment config |
| `--workflow-dir` | `./workflows` | Path to workflow directory |
| `--host` | `""` | Bind host (env: `KTSU_ORCHESTRATOR_HOST`) |
| `--port` | `5050` | Listen port (env: `KTSU_ORCHESTRATOR_PORT`) |
| `--runtime-url` | `""` | Agent runtime URL (env: `KTSU_RUNTIME_URL`) |
| `--gateway-url` | `""` | LLM gateway URL (env: `KTSU_GATEWAY_URL`) |
| `--own-url` | `""` | Orchestrator's public URL for callbacks (env: `KTSU_OWN_URL`) |
| `--project-dir` | `.` | Project root (env: `KTSU_PROJECT_DIR`) |
| `--store-type` | `memory` | State store: `memory` or `sqlite` (env: `KTSU_STORE_TYPE`) |
| `--db-path` | `ktsu.db` | Database path for sqlite (env: `KTSU_DB_PATH`) |
| `--workspace` | — | Additional workspace root; repeatable |
| `--no-hub-lock` | `false` | Ignore `ktsuhub.lock.yaml` even if present |

---

### `ktsu start runtime`

Starts only the Agent Runtime service.

```bash
ktsu start runtime --orchestrator http://localhost:5050
```

| Flag | Default | Description |
|---|---|---|
| `--orchestrator` | `http://localhost:5050` | Orchestrator URL (env: `KTSU_ORCHESTRATOR_URL`) |
| `--gateway` | `http://localhost:5052` | LLM gateway URL (env: `KTSU_GATEWAY_URL`) |
| `--host` | `""` | Bind host (env: `KTSU_RUNTIME_HOST`) |
| `--port` | `5051` | Listen port (env: `KTSU_RUNTIME_PORT`) |

---

### `ktsu start gateway`

Starts only the LLM Gateway service.

```bash
ktsu start gateway --config gateway.yaml --port 5052
```

| Flag | Default | Description |
|---|---|---|
| `--config` | `gateway.yaml` | Path to gateway config |
| `--host` | `""` | Bind host (env: `KTSU_GATEWAY_HOST`) |
| `--port` | `5052` | Listen port (env: `KTSU_GATEWAY_PORT`) |

---

## `ktsu validate`

Validates all workflow, agent, server, and gateway YAML files in the project. Checks schema correctness, DAG cycle detection, `depends_on` references, and environment variable scoping rules.

```bash
ktsu validate
ktsu validate . --env environments/dev.env.yaml
ktsu validate --graph   # output a Mermaid dependency graph
```

| Flag | Default | Description |
|---|---|---|
| `--env` | `""` | Path to environment config to validate |
| `--workflow-dir` | `""` | Directory of `*.workflow.yaml` files (defaults to `<project-dir>/workflows`) |
| `--project-dir` | `.` | Project root for resolving agent/server paths (env: `KTSU_PROJECT_DIR`) |
| `--workspace` | — | Additional workspace root to validate; repeatable |
| `--no-hub-lock` | `false` | Ignore `ktsuhub.lock.yaml` even if present |
| `--graph` | `false` | Output a Mermaid graph of the workflow DAG instead of a status report |

The command prints a grouped summary — Workflows, Agents, Servers, Systems — with `OKAY` / `FAIL` per file and exits non-zero if any errors are found.

---

## `ktsu invoke`

Triggers a workflow run on a running Orchestrator and prints the `run_id`. Optionally polls until the run completes and prints the full result envelope.

```bash
ktsu invoke support-triage --input '{"message": "help", "user_id": "123"}'
ktsu invoke support-triage --input '{"message": "help", "user_id": "123"}' --wait
```

| Flag | Default | Description |
|---|---|---|
| `--input` | `{}` | JSON input for the workflow |
| `--wait` | `false` | Poll until the run completes, then print the result |
| `--orchestrator` | `http://localhost:5050` | Orchestrator URL (env: `KTSU_ORCHESTRATOR_URL`) |

---

## `ktsu runs`

Lists recent workflow runs from the Orchestrator, with optional filters.

```bash
ktsu runs
ktsu runs --workflow support-triage --status failed --limit 20
```

| Flag | Default | Description |
|---|---|---|
| `--orchestrator` | `http://localhost:5050` | Orchestrator URL (env: `KTSU_ORCHESTRATOR_URL`) |
| `--workflow` | `""` | Filter by workflow name |
| `--status` | `""` | Filter by status: `pending`, `running`, `complete`, `failed` |
| `--limit` | `0` | Max results to return (orchestrator default: 50) |

Output columns: `RUN ID`, `WORKFLOW`, `STATUS`, `STARTED`, `DURATION`.

---

## `ktsu runs get`

Fetches and prints the full run envelope for a specific run ID.

```bash
ktsu runs get <run_id>
```

| Flag | Default | Description |
|---|---|---|
| `--orchestrator` | `http://localhost:5050` | Orchestrator URL (env: `KTSU_ORCHESTRATOR_URL`) |

Output is pretty-printed JSON containing the run status, step outputs, and any errors.

---

## `ktsu workflow tree`

Prints the full dependency tree of a workflow — sub-workflows, agents, and tool servers — as an indented tree. Useful for understanding what a workflow pulls in before running it.

```bash
ktsu workflow tree workflows/support-triage.workflow.yaml
ktsu workflow tree workflows/support-triage.workflow.yaml --json
```

| Flag | Default | Description |
|---|---|---|
| `--json` | `false` | Output the tree as JSON instead of a text tree |

---

## `ktsu lock`

Generates or updates `ktsu.lock.yaml` with pinned versions of all project dependencies.

```bash
ktsu lock
```

> **Note:** This command is not yet implemented and currently prints a placeholder message.

---

## `ktsu new project`

Scaffolds a new Kimitsu project with the standard directory structure and starter files.

```bash
ktsu new project my-project
```

Creates the following files under `./my-project/`:

| File | Purpose |
|---|---|
| `workflows/<name>.workflow.yaml` | Starter workflow |
| `agents/placeholder.agent.yaml` | Placeholder agent |
| `environments/dev.env.yaml` | Development environment config |
| `gateway.yaml` | LLM gateway config |
| `servers.yaml` | Tool server manifest |
| `ktsuhub.yaml` | Hub publishing config |

---

## `ktsu hub`

Interact with the Kimitsu Hub workflow registry. This command group is only available when `KTSU_HUB_ENABLED=true` is set in the environment.

```bash
KTSU_HUB_ENABLED=true ktsu hub <subcommand>
```

### `ktsu hub login`

Authenticate with GitHub.

```bash
ktsu hub login
```

> **Note:** Not yet implemented.

---

### `ktsu hub install`

Install a workflow from the Kimitsu Hub or a git repository.

```bash
ktsu hub install owner/repo-name
ktsu hub install owner/repo-name@v1.2.0
ktsu hub install https://github.com/owner/repo.git
```

| Flag | Default | Description |
|---|---|---|
| `--cache-dir` | `~/.ktsu/cache` | Local cache directory (env: `KTSU_CACHE_DIR`) |
| `--dry-run` | `false` | Preview changes without installing |

Installed entries are recorded in `ktsuhub.lock.yaml`.

---

### `ktsu hub update`

Re-resolves all entries in `ktsuhub.lock.yaml` to their latest matching commits.

```bash
ktsu hub update
ktsu hub update --latest
```

| Flag | Default | Description |
|---|---|---|
| `--latest` | `false` | Also update pinned version entries to their latest releases |
| `--dry-run` | `false` | Preview changes without writing |

---

### `ktsu hub publish`

Publish workflows to the Kimitsu Hub.

```bash
ktsu hub publish
```

> **Note:** Not yet implemented.

---

### `ktsu hub search`

Search available workflows on the Kimitsu Hub.

```bash
ktsu hub search summarization
```

| Flag | Default | Description |
|---|---|---|
| `--tag` | `""` | Filter results by tag |
| `--limit` | `10` | Number of results to return |

> **Note:** Not yet implemented.

---

## Variable Substitution in Workflows

Workflow YAML files support `{{ expr }}` interpolation in string values. See [workflow.yaml](yaml-spec/workflow.md) for full details.

| Syntax | Scope | Description |
|---|---|---|
| `{{ params.NAME }}` | All workflows | Workflow input parameter |
| `{{ env.NAME }}` | Root workflows only | Declared environment variable |
| `{{ step.ID.FIELD }}` | All workflows | Output field from a completed step |

JMESPath expressions (in `condition:` or transform `expr:`) use bare syntax without `{{ }}`.

---

*Revised April 2026*
