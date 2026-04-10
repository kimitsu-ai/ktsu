# ktsuhub Feature Spec

`ktsuhub` is the workflow registry and distribution layer for Kimitsu. It lets you install, publish, and share workflow packages backed by git repositories.

See also: [CLI Reference](../cli-reference.md) · [YAML Spec](../yaml-spec/index.md)

---

## Implementation Status

| Feature | Status | Notes |
|---------|--------|-------|
| Feature flag `KTSU_HUB_ENABLED=true` | ✅ Implemented | Default off |
| `ktsu hub install` (git sources: `github.com/`, `https://`) | ✅ Implemented | |
| `ktsu hub install` (registry short form `owner/name`) | 🚧 Stub | Requires registry API |
| `ktsu hub update` | ✅ Implemented | |
| `ktsu hub login` | 🚧 Stub | Not yet implemented |
| `ktsu hub search` | 🚧 Stub | Not yet implemented |
| `ktsu hub publish` | 🚧 Stub | Not yet implemented |
| Multi-workspace orchestrator (`--workspace`, lock auto-load) | ✅ Implemented | |
| `ktsu validate --workspace` | ✅ Implemented | |
| `ktsu workflow tree` | ✅ Implemented | |
| `ktsuhub.yaml` scaffold in `ktsu new project` | ✅ Implemented | |

---

## Overview

All `ktsu hub` commands are gated behind the `KTSU_HUB_ENABLED=true` environment variable. Set it to enable hub commands:

```bash
KTSU_HUB_ENABLED=true ktsu hub install github.com/kyle/workflows
```

---

## ktsuhub.yaml

Declares which workflows in a repository are published to the registry. Lives at the repo root.

```yaml
# ktsuhub.yaml
workflows:
  - name: support-triage
    version: 1.2.0
    description: "Triages inbound support tickets"
    tags: [support, triage]
    entrypoint: workflows/support-triage.workflow.yaml
```

| Field | Required | Description |
|---|---|---|
| `workflows[].name` | Yes | Registry name (must be unique per GitHub user/org) |
| `workflows[].version` | Yes | SemVer string |
| `workflows[].description` | No | Short description shown in search results |
| `workflows[].tags` | No | List of tags for search filtering |
| `workflows[].entrypoint` | Yes | Path to the root workflow file relative to repo root |

An empty `ktsuhub.yaml` with `workflows: []` is valid and is scaffolded by `ktsu new project`.

---

## ktsuhub.lock.yaml

Tracks installed workflow versions and their cache locations. Lives at the project root and is managed by `ktsu hub install` / `ktsu hub update`. Commit this file to pin installed versions.

```yaml
# ktsuhub.lock.yaml
entries:
  - name: support-triage
    version: 1.2.0
    source: github.com/kyle/workflows
    ref: v1.2.0
    sha: a3f9c12b8e2d47c1f3a9b0d5e8c2f7a4b6d1e3f5
    cache: ~/.ktsu/cache/github.com/kyle/workflows
    mutable: false
```

| Field | Description |
|---|---|
| `name` | Workflow name from `ktsuhub.yaml` |
| `version` | Workflow version from `ktsuhub.yaml` |
| `source` | Original install target (normalized) |
| `ref` | Git ref used at install time (tag, branch, or SHA) |
| `sha` | Resolved commit SHA at install time |
| `cache` | Absolute path to the cached clone |
| `mutable` | `true` if the ref is a branch (can change on update) |

---

## Install Mechanics

`ktsu hub install` supports three source formats:

| Format | Example | Notes |
|---|---|---|
| `github.com/owner/repo[@ref]` | `github.com/kyle/workflows@v1.2.0` | Expanded to `https://github.com/kyle/workflows.git` |
| `https://...[@ref]` | `https://gitlab.com/org/repo@main` | Any git-accessible HTTPS URL |
| Local path (`/` or `./`) | `./local-workflows` | For development and testing only |
| `owner/name` (short form) | `kyle/support-triage` | **Not yet implemented** — returns error |

The short-form `owner/name` registry lookup requires a registry API that is not yet implemented. Attempting it returns:

```
registry install (short form "kyle/support-triage") not yet implemented — use github.com/owner/repo or https://...
```

### Cache Layout

Installed packages are cloned into `KTSU_CACHE_DIR` (default `~/.ktsu/cache`), organized by source path:

```
~/.ktsu/cache/
└── github.com/
    └── kyle/
        └── workflows/    ← git clone of github.com/kyle/workflows
```

---

## Multi-Workspace Integration

When `ktsuhub.lock.yaml` is present at the project root, `ktsu start orchestrator` and `ktsu validate` automatically mount each `cache` path as an additional workspace. This makes installed workflows available without any extra flags.

To suppress this behavior, pass `--no-hub-lock`:

```bash
ktsu start orchestrator --no-hub-lock
ktsu validate --no-hub-lock
```

When the same workflow name appears in multiple workspaces, the primary workspace takes precedence, then additional workspaces in the order they are declared (first-match-wins, analogous to PATH resolution). The local project workspace always has highest priority over hub-installed packages.

---

## Stubs (Not Yet Implemented)

The following commands are registered and visible in `ktsu hub --help` but return an error when invoked:

- `ktsu hub login` — returns `hub login: not yet implemented`
- `ktsu hub publish` — returns `hub publish: not yet implemented`
- `ktsu hub search` — returns `hub search: not yet implemented`

These will be implemented when the registry API is available.
