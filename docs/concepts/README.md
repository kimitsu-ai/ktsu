---
description: "Index and reading guide for the Concepts section with recommended reading order and a worked pipeline scenario tying together all core ideas."
---

# Concepts

This section explains how Kimitsu works at the level you need to write and debug workflows. Each page covers one idea; together they form a complete picture of how data flows through a pipeline.

---

## Recommended Reading Order

1. [Pipeline Primitives](./pipeline-primitives.md)
2. [Variables](./variables.md)
3. [Reserved Outputs](./reserved-outputs.md)
4. [Fanout](./fanout.md)
5. [Invariants](./invariants.md)

**Why this order:** Primitives establish the four building blocks — you cannot reason about the rest without knowing what a step is. Variables show how data moves between steps, which is the mechanism every subsequent concept depends on. Reserved outputs show how agents signal internal state (errors, reflections, confidence) that downstream steps can read. Fanout extends a single agent step into parallel iterations over a collection, which builds directly on how variables and reserved outputs work. Invariants close the loop by explaining the hard constraints the orchestrator enforces on every pipeline, which only make sense once you have seen all the moving parts.

---

## A Worked Scenario

Consider a three-step pipeline: an agent that fetches and parses a news feed, a transform that filters the results, and a second agent that writes a summary.

The first agent (`fetch`) calls an MCP tool to retrieve articles and emits a structured list. This step relies on **pipeline primitives** — specifically the agent type — and on **variables** to receive its API key as a param. If the tool call fails, the agent writes to `_error`, a **reserved output**, so the orchestrator can surface the failure rather than pass corrupt data forward.

The transform step (`filter`) takes `{{ step.fetch.articles }}` and applies a JMESPath expression to drop low-relevance items. This is pure **variables** and **primitives** — no LLM involved.

The second agent (`summarize`) receives the filtered list via `{{ step.filter.output }}`. If this step were applied to each article independently, you would use **fanout** (`for_each`) to run it in parallel across the array. The **invariants** — particularly the rule that sub-workflows cannot access `env.*` directly — determine how secrets must be passed if `summarize` is extracted into its own workflow file.

---

## Quick Links

| Page | What it covers |
|---|---|
| [Pipeline Primitives](./pipeline-primitives.md) | The four step types: agent, transform, webhook, workflow |
| [Variables](./variables.md) | Template syntax for env vars, params, and step outputs |
| [Reserved Outputs](./reserved-outputs.md) | Special output keys agents use to signal errors and reflection |
| [Fanout](./fanout.md) | Running an agent step once per array element, in parallel |
| [Transforms](./transforms.md) | The six deterministic data operations available in transform steps |
| [Invariants](./invariants.md) | Hard constraints the orchestrator enforces on every pipeline |

---

*Revised April 2026*
