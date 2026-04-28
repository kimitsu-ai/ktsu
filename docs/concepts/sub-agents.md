---
description: "The sub_agents field: currently validation-only (no runtime dispatch), planned runtime behavior (issue #14), and when to use sub_agents vs. workflow steps."
---

# Sub-Agents

---

## What `sub_agents` declares

`sub_agents` is a field on an agent config that lists other agent files this agent depends on. It is a dependency graph declaration — it names the agent files that must exist and be valid for this agent to be considered well-formed.

```yaml
# agents/coordinator.agent.yaml
name: coordinator
model: claude-opus-4
sub_agents:
  - ./agents/researcher.agent.yaml
  - ./agents/writer.agent.yaml
prompt:
  system: |
    You coordinate research and writing tasks.
  user: "{{ params.task }}"
servers: []
```

Each entry is a file path, relative to the declaring agent file or absolute within the project.

---

## Current behavior: validation only

`ktsu validate` resolves each path in `sub_agents`, checks that the file exists, and validates its schema. If a declared sub-agent is missing or malformed, `ktsu validate` reports an error:

```
agents/coordinator.agent.yaml: sub-agent "./agents/researcher.agent.yaml" not found
```

This is the extent of current `sub_agents` support. The field creates no runtime behavior — the orchestrator does not dispatch calls from the coordinator agent to the sub-agents, inject sub-agent tools into the coordinator's tool list, or manage any sub-agent invocation lifecycle.

Declaring `sub_agents` is safe and encouraged as a form of documentation and static dependency checking. It does not affect how a workflow executes today.

---

## What it will become

[GitHub issue #14](https://github.com/kimitsu-ai/ktsu/issues/14) tracks full runtime implementation of `sub_agents`.

When implemented, `sub_agents` will become a runtime dispatch mechanism: the coordinator agent will be able to invoke sub-agents as tools within its reasoning loop. Each declared sub-agent will be exposed to the LLM as a callable tool, with the sub-agent's prompt and schema defining the tool's contract. The orchestrator will manage the dispatch, collect the sub-agent's output, and return it as a tool result to the coordinating agent's message history.

This will enable agent-to-agent composition within a single workflow step — one agent orchestrating several specialist agents without needing to model each as a separate pipeline step.

---

## `sub_agents` vs. `type: workflow` steps

These two mechanisms serve different composition needs:

| | `sub_agents` | `workflow` step |
|---|---|---|
| Scope | Within a single agent step | Pipeline-level |
| Control | Agent's reasoning loop (planned) | Orchestrator DAG |
| Visibility | Agent decides when to invoke | Explicit pipeline step with `depends_on` |
| Status | Validation-only today | Fully implemented |

Use a `workflow` step when you want the orchestrator to sequence sub-pipelines explicitly, with each sub-workflow appearing as a distinct step in the run envelope. Use `sub_agents` (once implemented) when you want an LLM to decide dynamically which specialist agents to call and in what order.

---

*Revised April 2026*
