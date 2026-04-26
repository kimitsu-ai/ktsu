# Versioning

---

## Why versioning matters

Tool servers evolve. A workflow pinned to a specific version of an agent or server should not break silently when that dependency is updated. Versioning gives pipelines a stable contract: you can upgrade dependencies deliberately and verify compatibility before promoting a change.

---

## Declaring a version

The `version` field is available on workflow configs:

```yaml
# workflows/triage.workflow.yaml
kind: workflow
name: triage
version: "1.2.0"
description: Customer support triage pipeline
pipeline:
  - ...
```

The field accepts any string. Semver (`MAJOR.MINOR.PATCH`) is the recommended convention.

**Agents and servers** do not have a top-level `version` field in their config schemas. Versioning for agents and servers is expressed as a suffix on the file path reference when installing from the ktsu hub:

```
agents/triage.agent.yaml@1.0.0
```

The `@version` suffix is stripped by `config.StripVersion` at load time — it is used for hub resolution only, not runtime behavior.

---

## The lockfile concept

`ktsu.lock.yaml` is intended to be a machine-generated file that pins every dependency — workflow, agent, server — to a resolved version and content hash. This gives reproducible pipeline execution: two developers with the same lockfile will run identical configurations.

When implemented, the lockfile will record:

- The resolved version for each hub-installed workflow package.
- The git SHA used at install time.
- A flag indicating whether the ref is mutable (a branch) or pinned (a tag or SHA).

The hub lockfile (`ktsuhub.lock.yaml`) is already implemented and tracks installed workflow packages. `ktsu.lock.yaml` for local agent and server pinning is the planned extension.

---

## Current status

**Version declarations are parsed but not enforced at runtime.**

- The `version` field on `WorkflowConfig` is read and stored, but the orchestrator does not use it for constraint checking.
- `ktsu validate` does not warn on missing or mismatched version fields.
- Running `ktsu lock` prints `lock: not implemented` and exits. Lockfile generation for local configs is tracked in [GitHub issue #15](https://github.com/kimitsu-ai/ktsu/issues/15).

`ktsuhub.lock.yaml` (installed hub packages) is functional — `ktsu hub install` writes it and `ktsu validate` reads it. The gap is the equivalent lock mechanism for local project files.

Until issue #15 is resolved, treat version strings as documentation for humans, not as enforced constraints.

---

*Revised April 2026*
