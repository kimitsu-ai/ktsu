---
name: YAML Spec Docs
description: Design for docs/yaml-spec/ — per-kind YAML reference files for coding agents
type: project
---

# YAML Spec Docs — Design

## Goal

Create `docs/yaml-spec/` as a self-contained, token-efficient YAML reference for coding agents that already understand Kimitsu conceptually. No prose explanations of concepts — only field specs.

## Audience

Coding agents (AI). They know what Kimitsu is. They need to know field names, types, required vs optional, constraints, and valid values to write correct YAML.

## File Structure

```
docs/yaml-spec/
  index.md       — one paragraph per kind, filename convention, links
  workflow.md    — kind: workflow
  agent.md       — agent YAML files (no kind field)
  server.md      — kind: tool-server (local server files)
  servers.md     — kind: servers (marketplace manifest)
  gateway.md     — gateway.yaml (providers + model groups)
  env.md         — kind: env (environment config)
```

## Per-File Format (Option B)

Each file follows this structure:

1. **One-line description** — what this file does in the system
2. **Filename convention** — e.g. `agents/*.agent.yaml`
3. **Annotated YAML block** — complete example with every field present, inline `#` comments explaining each field
4. **Field reference table** — columns: Field | Type | Required | Description

No introductory prose, no conceptual explanations, no links to external docs.

## workflow.md Special Case

`workflow.md` covers the top-level workflow fields plus all three step types. Each step type (agent step, transform step, webhook step) gets its own annotated YAML block and field table within the same file — not split into separate files.

## Index Format

`index.md` contains:
- One-paragraph description of each kind
- Its filename convention
- Link to the corresponding spec file

No field details in the index — it is navigation only.

## What Is NOT Included

- Conceptual explanations of how the orchestrator works
- Runtime semantics (boot sequence, Air-Lock behavior, retry logic)
- CLI commands
- Deployment or Docker configuration
