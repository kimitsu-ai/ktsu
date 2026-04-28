---
description: "Deploy the full ktsu stack (orchestrator, gateway, runtime) to Railway using GitHub integration or the Railway CLI."
---

# Deploy on Railway

This guide covers deploying the Kimitsu stack to [Railway](https://railway.app). We recommend using the GitHub integration for automatic deployments, but you can also use the Railway CLI.

## Prerequisites

1. A Railway account.
2. An `ANTHROPIC_API_KEY` (required for the default gateway configuration).
3. The Kimitsu repository forked or cloned to your GitHub account.

## Deployment via Railway Dashboard

This is the recommended method for production-like environments.

### 1. Create a New Project
- Go to the [Railway Dashboard](https://railway.app/dashboard).
- Click **New Project** > **Deploy from GitHub repo**.
- Select your `ktsu` repository.

### 2. Configure Service Settings
Once the service is created, go to the **Settings** tab:

- **Build**:
    - **Builder**: Select `Dockerfile`.
    - **Dockerfile Path**: Set to `deploy/railway/Dockerfile`.
- **Deploy**:
    - **Start Command**: `sh -c 'ktsu start --all --gateway-config /app/gateway.yaml'`

### 3. Set Environment Variables
Go to the **Variables** tab and add the following:

| Variable | Value | Description |
|---|---|---|
| `ANTHROPIC_API_KEY` | `your_api_key` | Required for LLM inference. |
| `KTSU_API_KEY` | `generate_a_random_string` | Secure key for orchestrator authentication. |
| `KTSU_ORCHESTRATOR_PORT` | `${{PORT}}` | Maps the internal orchestrator to Railway's public port. |
| `KTSU_STORE_TYPE` | `sqlite` | Enables persistent state. |
| `KTSU_DB_PATH` | `/app/data/ktsu.db` | Path to the SQLite database. |

### 4. Add Persistent Storage
To ensure your workflow run state persists across deployments, you must add a Volume:

- Go to the **Volumes** tab.
- Click **Add Volume**.
- Set the **Mount Path** to `/app/data`.

## Deployment via Railway CLI

If you prefer the command line, you can deploy directly from your local machine:

```bash
# Install Railway CLI
npm i -g @railway/cli

# Login to your account
railway login

# Initialize project
railway init

# Set the configuration
railway variables --set ANTHROPIC_API_KEY=your_key KTSU_API_KEY=your_secret KTSU_ORCHESTRATOR_PORT='${{PORT}}' KTSU_STORE_TYPE=sqlite KTSU_DB_PATH=/app/data/ktsu.db

# Deploy
railway up --dockerfile deploy/railway/Dockerfile
```

## Health Checks

Railway will automatically monitor the health of your service. Kimitsu exposes a health endpoint at `/health`.

- **Healthcheck Path**: `/health`
- **Healthcheck Timeout**: `100`

Once the deployment is green, you can access your orchestrator at the generated Railway domain.

> [!TIP]
> You can verify the deployment by running `curl https://your-project.up.railway.app/health`.
