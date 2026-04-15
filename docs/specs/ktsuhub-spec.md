# ktsu CLI Reference

`ktsu` is the command-line interface for the Kimitsu agentic pipeline framework. Use it to start services, invoke workflows, validate configuration, scaffold new projects, and interact with the ktsuhub workflow registry.

See also: [YAML Spec](yaml-spec/index.md) · [Overview](kimitsu-overview.md) · [Configuration](kimitsu-configuration.md) · [ktsuhub](ktsuhub.md)

---

## Quick Reference

| Command | Purpose | When to use |
|---|---|---|
| `ktsu start orchestrator` | Start the control plane | Running the full stack or orchestrator only |
| `ktsu start runtime` | Start the agent executor | Running the full stack or runtime only |
| `ktsu start gateway` | Start the LLM gateway | Running the full stack or gateway only |
| `ktsu start envelope` | Start the shipped envelope tool server | Adding the envelope server to a running stack |
| `ktsu invoke <workflow>` | Invoke a workflow | Development and testing |
| `ktsu validate [project-dir]` | Validate config files | CI, pre-deploy checks, local debugging |
| `ktsu new project <name>` | Scaffold a new project | Starting a new Kimitsu project |
| `ktsu workflow tree <file>` | Emit dependency tree for a workflow | Auditing dependencies, bundling for publish |
| `ktsu hub install <target>` | Install a workflow from ktsuhub or a git repo | Adding hub workflows to a project |
| `ktsu hub update` | Re-resolve all entries in ktsuhub.lock.yaml | Updating installed workflows |
| `ktsu hub publish` | Publish workflows to ktsuhub | Sharing your workflows |
| `ktsu hub login` | Authenticate with GitHub | Required before publish |
| `ktsu hub search <query>` | Search ktsuhub from the CLI | Discovering workflows |
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
| `KTSU_ORCHESTRATOR_URL` | `http://localhost:8080` | `start runtime`, `start envelope`, `invoke` | Orchestrator URL |
| `KTSU_OWN_URL` | `""` | `start orchestrator` | Orchestrator's own URL for callbacks |
| `KTSU_PROJECT_DIR` | `.` | `start orchestrator` | Project root for resolving agent/server paths |
| `KTSU_GATEWAY_HOST` | `""` (all interfaces) | `start gateway` | Host interface to bind |
| `KTSU_GATEWAY_PORT` | `8081` | `start gateway` | Port to listen on |
| `KTSU_GATEWAY_URL` | `http://localhost:8081` | `start runtime` | LLM gateway URL |
| `KTSU_RUNTIME_HOST` | `""` (all interfaces) | `start runtime` | Host interface to bind |
| `KTSU_RUNTIME_PORT` | `8082` | `start runtime` | Port to listen on |
| `KTSU_RUNTIME_URL` | `""` | `start orchestrator` | Agent runtime URL |
| `KTSU_CACHE_DIR` | `~/.ktsu/cache` | `hub install`, `hub update` | Local cache directory for installed workflows |
| `NO_COLOR` | *(unset)* | `validate` | Set to any value to disable colored output |

---

## ktsu start

Start a Kimitsu service or shipped tool server. Every service exposes `GET /health` returning `{"status":"ok"}`.

### Core Services

#### ktsu start orchestrator

Start the control plane. Loads workflow definitions from all configured workspaces and coordinates pipeline execution.

If `ktsuhub.lock.yaml` is present at the project root, the orchestrator automatically mounts all cached workflow installs as additional workspaces. Pass `--no-hub-lock` to suppress this behavior.

```
ktsu start orchestrator [flags]
```

| Flag | Default | Env | Description |
|---|---|---|---|
| `--env` | `""` | — | Path to environment config (e.g. `environments/dev.env.yaml`) |
| `--workflow-dir` | `./workflows` | — | Directory of `*.workflow.yaml` files in the local workspace |
| `--host` | `""` | `KTSU_ORCHESTRATOR_HOST` | Host interface to bind |
| `--port` | `8080` | `KTSU_ORCHESTRATOR_PORT` | Port to listen on |
| `--runtime-url` | `""` | `KTSU_RUNTIME_URL` | Agent runtime URL |
| `--own-url` | `""` | `KTSU_OWN_URL` | Orchestrator's own URL for callbacks |
| `--project-dir` | `.` | `KTSU_PROJECT_DIR` | Local workspace root |
| `--workspace` | *(none)* | — | Additional workspace root. Repeatable. |
| `--no-hub-lock` | `false` | — | Ignore `ktsuhub.lock.yaml` even if present |

```bash
# Standard — reads ktsuhub.lock.yaml automatically if present
ktsu start orchestrator

# With environment config
ktsu start orchestrator --env environments/dev.env.yaml

# Explicit additional workspace
ktsu start orchestrator --workspace ~/shared-workflows

# Ignore hub-installed workflows for this run
ktsu start orchestrator --no-hub-lock
```

#### Multi-Workspace Boot Behavior

When multiple workspaces are configured, the orchestrator:

1. Loads the local workspace from `--project-dir`
2. Reads `ktsuhub.lock.yaml` from the project root if present (unless `--no-hub-lock`), adds each `cache` path as a workspace
3. Adds any paths passed via `--workspace` flags
4. Walks `workflows/` in every workspace root and merges all `*.workflow.yaml` files into a single flat namespace
5. Enforces that workflow names are unique across all workspaces — duplicates are a boot error
6. Resolves all agent and server file paths relative to their own workspace root

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

Start the LLM gateway. Routes model calls to configured providers based on `gateway.yaml`.

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

### Shipped Tool Servers

#### `ktsu start envelope` (requires `--orchestrator`)

| Flag | Default | Env | Description |
|---|---|---|---|
| `--host` | `""` | — | Host interface to bind |
| `--port` | `9104` | — | Port to listen on |
| `--orchestrator` | `http://localhost:8080` | `KTSU_ORCHESTRATOR_URL` | Orchestrator URL for registration |

```bash
ktsu start envelope
ktsu start envelope --port 9104 --orchestrator http://orchestrator:8080
```

---

## ktsu invoke

Invoke a named workflow on a running orchestrator. Posts to `POST /invoke/<workflow>` and optionally polls until completion.

```
ktsu invoke <workflow> [flags]
```

| Flag | Default | Env | Description |
|---|---|---|---|
| `--input` | `{}` | — | JSON input for the workflow |
| `--wait` | `false` | — | Poll until the run completes and print result |
| `--orchestrator` | `http://localhost:8080` | `KTSU_ORCHESTRATOR_URL` | Orchestrator URL |

```bash
ktsu invoke hello
ktsu invoke hello --input '{"name": "World"}'
ktsu invoke hello --input '{"name": "World"}' --wait
ktsu invoke hello --orchestrator http://example.com:8080 --wait
```

---

## ktsu validate

Validate Kimitsu configuration files without starting any services. Runs the full boot-time graph validation — DAG cycle check, sub-agent cycle check, allowlist validation, IO type-checking — across all workspaces.

```
ktsu validate [project-dir] [flags]
```

| Argument/Flag | Default | Description |
|---|---|---|
| `[project-dir]` | `.` | Local workspace root |
| `--env` | `""` | Path to environment config to validate |
| `--workflow-dir` | `""` | Directory of `*.workflow.yaml` files (defaults to `<project-dir>/workflows`) |
| `--workspace` | *(none)* | Additional workspace root to include in validation. Repeatable. |
| `--no-hub-lock` | `false` | Ignore `ktsuhub.lock.yaml` even if present |
| `--graph` | `false` | Output Mermaid graph of workflows instead of text summary |

```bash
ktsu validate
ktsu validate /path/to/project
ktsu validate --env environments/dev.env.yaml
ktsu validate --graph
ktsu validate --no-hub-lock
```

---

## ktsu workflow

Commands for inspecting and working with workflow files.

### ktsu workflow tree

Walk a workflow file and emit the full dependency tree: the workflow file, all referenced agent files, all referenced local server files, and `gateway.yaml`. Paths are deduplicated and relative to `--project-dir`.

Used by ktsuhub to determine what to bundle into a zip download. Also useful locally for auditing what a workflow depends on.

```
ktsu workflow tree <workflow-file> [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--json` | `false` | Output a flat JSON array of relative paths |
| `--project-dir` | `.` | Project root for resolving relative paths |

**Default output (human-readable tree):**

```bash
ktsu workflow tree workflows/support-triage.workflow.yaml
```

```
workflows/support-triage.workflow.yaml
├── agents/triage.agent.yaml
│   ├── servers/wiki-search.server.yaml
│   └── servers/text-classifier.server.yaml
├── agents/legal.agent.yaml
│   └── servers/crm.server.yaml
├── agents/risk.agent.yaml
│   └── servers/crm.server.yaml
├── agents/consolidator.agent.yaml
└── gateway.yaml
```

**JSON output (for tooling and scripts):**

```bash
ktsu workflow tree workflows/support-triage.workflow.yaml --json
```

```json
[
  "workflows/support-triage.workflow.yaml",
  "agents/triage.agent.yaml",
  "servers/wiki-search.server.yaml",
  "servers/text-classifier.server.yaml",
  "agents/legal.agent.yaml",
  "agents/risk.agent.yaml",
  "servers/crm.server.yaml",
  "agents/consolidator.agent.yaml",
  "gateway.yaml"
]
```

---

## ktsu hub

Commands for interacting with the ktsuhub workflow registry. All publish operations require GitHub authentication via `ktsu hub login`.

### ktsu hub login

Authenticate with GitHub. Opens a browser to complete OAuth. Stores the token in `~/.ktsu/credentials`. Required before `ktsu hub publish`.

```
ktsu hub login
```

```bash
ktsu hub login
# Opens browser → GitHub OAuth → stores token
# Logged in as kyle
```

---

### ktsu hub install

Install a workflow from ktsuhub or any git repository. Clones or fetches the source into `~/.ktsu/cache/` and writes an entry to `ktsuhub.lock.yaml` in the current directory. The installed workflow is available to `ktsu start orchestrator` on next boot.

```
ktsu hub install <target>[@ref] [flags]
```

**Install from ktsuhub registry:**

```bash
ktsu hub install kyle/support-triage           # latest published version
ktsu hub install kyle/support-triage@1.2.0     # specific version
```

**Install from a GitHub repo directly:**

```bash
ktsu hub install github.com/kyle/workflows          # default branch, latest commit
ktsu hub install github.com/kyle/workflows@v1.2.0   # tag
ktsu hub install github.com/kyle/workflows@main     # branch (mutable — flagged in lock)
ktsu hub install github.com/kyle/workflows@a3f9c12  # commit SHA (fully pinned)
```

**Install from any git repo:**

```bash
ktsu hub install https://gitlab.com/org/repo
ktsu hub install https://git.internal.corp/team/workflows@v2.0.0
```

All install sources require a valid `ktsuhub.yaml` at the repo root. If absent, the command fails with a clear error.

| Flag | Default | Description |
|---|---|---|
| `--dry-run` | `false` | Show what would be installed and written to the lock file without making changes |
| `--cache-dir` | `~/.ktsu/cache` | Override the local cache directory |

```bash
# Preview before installing
ktsu hub install kyle/support-triage --dry-run

# Install and check lock file
ktsu hub install kyle/support-triage@1.2.0
cat ktsuhub.lock.yaml
```

**After install**, run `ktsu start orchestrator` — the new workflow is available immediately. No additional flags needed if `ktsuhub.lock.yaml` is at the project root.

---

### ktsu hub update

Re-resolve all entries in `ktsuhub.lock.yaml`. For pinned versions and SHAs, this is a no-op unless a newer version is available and `--latest` is passed. For mutable branch installs, this fetches the latest commit on the branch.

```
ktsu hub update [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--latest` | `false` | Also update pinned version entries to the latest published version |
| `--dry-run` | `false` | Show what would change without writing |

```bash
ktsu hub update            # re-fetch mutable entries, leave pinned versions alone
ktsu hub update --latest   # update everything to latest
ktsu hub update --dry-run  # preview changes
```

Mutable entries (`mutable: true` in the lock file) print a warning on update:

```
WARN  kyle/workflows@main is a mutable branch ref.
      SHA updated: a3f9c12 → b8e2d47
      Re-run ktsu validate to check for breaking changes.
```

---

### ktsu hub publish

Publish workflows declared in `ktsuhub.yaml` to the ktsuhub registry. Requires `ktsu hub login`. Verifies that the authenticated user has push access to the repository before publishing.

Typically this is triggered automatically via the GitHub webhook registered when you first connect a repo. Use `ktsu hub publish` to trigger a manual re-publish (e.g. after updating `ktsuhub.yaml` metadata without a version bump).

```
ktsu hub publish [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--repo` | *(auto-detected from git remote)* | GitHub repo URL to publish from |

```bash
ktsu hub publish
ktsu hub publish --repo https://github.com/kyle/kimitsu-workflows
```

---

### ktsu hub search

Search the ktsuhub registry from the CLI.

```
ktsu hub search <query> [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--tag` | `""` | Filter by tag |
| `--limit` | `10` | Number of results to return |

```bash
ktsu hub search "support triage"
ktsu hub search nlp --tag support
```

Output:

```
kyle/support-triage         v1.2.0   ★ 284   Triages inbound support tickets by category and priority
openclaw/github-issue-triage v2.0.1  ★ 201   Labels and assigns inbound GitHub issues
nanoclaw/slack-digest        v1.0.4  ★ 178   Daily Slack channel digest with action item extraction
```

---

## ktsu new

Scaffold new Kimitsu resources.

### ktsu new project

Bootstrap a new project directory with a minimal working scaffold.

```
ktsu new project <name>
```

Creates:

```
<name>/
├── workflows/<name>.workflow.yaml
├── agents/placeholder.agent.yaml
├── environments/dev.env.yaml
├── gateway.yaml
├── servers.yaml
└── ktsuhub.yaml              ← empty manifest, ready to fill in
```

```bash
ktsu new project my-app
cd my-app
ktsu validate
```

---

## ktsu lock

Generate `ktsu.lock.yaml` to pin marketplace tool server dependency versions.

> **Not yet implemented.** Prints `lock: not implemented` and exits successfully.

Note: `ktsu.lock.yaml` covers marketplace tool server pins. It is distinct from `ktsuhub.lock.yaml`, which covers installed workflows.

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
ktsu completion bash | sudo tee /etc/bash_completion.d/ktsu
ktsu completion zsh | sudo tee /usr/share/zsh/site-functions/_ktsu
ktsu completion fish | sudo tee /usr/share/fish/vendor_completions.d/ktsu.fish
ktsu completion powershell | Out-String | Out-File -FilePath $profile
```

---

## Port Reference

| Service | Default Port | Command |
|---|---|---|
| Orchestrator | 8080 | `ktsu start orchestrator` |
| LLM Gateway | 8081 | `ktsu start gateway` |
| Agent Runtime | 8082 | `ktsu start runtime` |
| envelope | 9104 | `ktsu start envelope` |

All ports are configurable via `--port`.

---

## File Reference

| File | Location | Managed by | Purpose |
|---|---|---|---|
| `ktsuhub.yaml` | repo root | author | Declares which workflows in a repo are published to the hub |
| `ktsuhub.lock.yaml` | project root | `ktsu hub` | Tracks installed workflow versions and cache locations |
| `ktsu.lock.yaml` | project root | `ktsu lock` | Pins marketplace tool server versions *(not yet implemented)* |

---

## See Also

- [ktsuhub](ktsuhub.md) — registry spec, `ktsuhub.yaml` format, install mechanics
- [YAML Spec Index](yaml-spec/index.md) — config file formats
- [Overview](kimitsu-overview.md) — architecture and concepts
- [Configuration](kimitsu-configuration.md) — configuration reference
- [Tool Servers](kimitsu-tool-servers.md) — shipped tool server details
- [Pipeline Primitives](kimitsu-pipeline-primitives.md) — step types and pipeline mechanics
