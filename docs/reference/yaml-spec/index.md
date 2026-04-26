# Kimitsu YAML Spec

Reference for all Kimitsu YAML file kinds. One file per kind — annotated example + field table. No conceptual explanations.

| File | Kind | Filename Convention | Description |
|---|---|---|---|
| [workflow.md](workflow.md) | `workflow` | `workflows/*.workflow.yaml` | Pipeline definition — steps, input schema, model policy |
| [agent.md](agent.md) | *(none)* | `agents/*.agent.yaml` | LLM agent — model, system prompt, tool servers, output schema |
| [server.md](server.md) | `tool-server` | `servers/*.server.yaml` | Local MCP tool server pointer — URL, auth, trust flags |
| [servers.md](servers.md) | `servers` | `servers.yaml` | Marketplace dependency manifest — names, sources, versions |
| [gateway.md](gateway.md) | *(none)* | `gateway.yaml` | LLM provider registry and named model group definitions |
| [env.md](env.md) | `env` | `environments/*.env.yaml` | Environment config — variables and state store backend |

For the full project directory layout, see [Architecture: Configuration](../../architecture/configuration.md).
