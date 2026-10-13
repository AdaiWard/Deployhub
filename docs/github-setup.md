# GitHub setup

## Local development

`DEV_MODE=true` allows the dashboard and API to work without authentication. Public GitHub repositories can be deployed by entering `owner/repository` in the project dialog.

## OAuth

Create a GitHub OAuth App and configure:

* Homepage URL: your `EXTERNAL_URL`
* Authorization callback URL: `${EXTERNAL_URL}/auth/github/callback`

Copy the Client ID and Client Secret into `.env`, then set `DEV_MODE=false`.

DeployHub requests repository and repository webhook scopes. Tokens are encrypted with AES GCM before they are written to PostgreSQL.

## Automatic deploy after push

Set `GITHUB_WEBHOOK_SECRET` to a long random value. When a project is created and `EXTERNAL_URL` is public HTTPS, DeployHub creates a `push` webhook on the repository.

The callback is:

```text
${EXTERNAL_URL}/webhooks/github
```

Webhook bodies are verified with `X-Hub-Signature-256`. Delivery IDs are stored so the same event cannot enqueue the same deploy twice.

GitHub cannot call `deployhub.localhost`. For local webhook testing, expose port 80 with an HTTPS tunnel and use that address as `EXTERNAL_URL`.
