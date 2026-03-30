# CLI Reference Doc — Design Spec

**Date:** 2026-03-29
**Status:** Approved

## Problem

No CLI reference doc exists for ktsu. Developers using ktsu and AI agents consuming project docs have no single place to look up commands, flags, environment variables, or examples.

## Decision

Single flat file: `docs/cli-reference.md`

YAML spec docs already exist at `docs/yaml-spec/` and cover config file formats. The CLI reference will cross-link to them rather than duplicate content.

## Audience

- **Developers** using ktsu day-to-day (running services, invoking workflows, validating config)
- **AI agents** that need to reason about which CLI commands to run and when

## Structure

```
docs/cli-reference.md
├── Header — one-line description, link to overview
├── Quick reference table — all commands, purpose, when to use
├── Global flags
├── Environment variables — full table with defaults and affected commands
├── ktsu start — core services + built-in servers
├── ktsu invoke — run a workflow
├── ktsu validate — validate config
├── ktsu new — scaffold new resources
├── ktsu lock — lockfile (not yet implemented)
├── ktsu completion — shell completion
├── Port reference table
└── Cross-references
```

## Style

- Slightly more context than yaml-spec docs: 1–2 sentence purpose per command explaining *when* to use it
- Terse — no conceptual prose beyond what's needed to understand usage
- Machine-parseable: consistent heading levels, tables for all flags and env vars
- Cross-link to yaml-spec docs; don't duplicate config file format details

## Source of Truth

All command, flag, and env var definitions are sourced from `cmd/ktsu/main.go`.
