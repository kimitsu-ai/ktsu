# ktsu CLI Reference

`ktsu` is the command-line interface for the Kimitsu agentic pipeline framework. Use it to start services, invoke workflows, validate configuration, and scaffold new projects.

---

## Global Service Ports

| Service | Default Port | Flag |
|---|---|---|
| Orchestrator | 5050 | `--orchestrator-port` |
| Agent Runtime | 5051 | `--runtime-port` |
| LLM Gateway | 5052 | `--gateway-port` |

---

## Commands

### `ktsu start --all`
Starts the full stack in a single process.

```bash
ktsu start --all --env environments/dev.env.yaml
```

| Flag | Default | Description |
|---|---|---|
| `--env` | `""` | Path to environment config (e.g., `environments/dev.env.yaml`) |
| `--project-dir` | `.` | Root directory for resolving relative paths |

### `ktsu invoke <workflow>`
Invokes a workflow on a running orchestrator.

```bash
ktsu invoke support-triage --input '{"message": "help", "user_id": "123"}' --wait
```

### `ktsu validate`
Validates the project's dependency graph, cycle detection, and I/O types.

```bash
ktsu validate . --env environments/dev.env.yaml
```

---

## Variable Substitution in Workflows

When defining webhooks or agent parameters in a workflow:

- **`{{ env.NAME }}`**: References a declared environment variable (Root Workflows only).
- **`{{ params.NAME }}`**: References a workflow parameter.
- **`{{ step.ID.FIELD }}`**: References an upstream step's output.

### Webhook Example

```yaml
- id: notify
  webhook:
    url: "{{ env.SLACK_WEBHOOK_URL }}"
    method: POST
    body:
      text: "Triage result: {{ step.triage.summary }}"
```

---

*Revised April 2026*
