# DeployHub

DeployHub is a self hosted deployment platform for GitHub repositories. It accepts a repository and branch, builds a Docker image, starts an isolated container, creates a Traefik route, streams logs, collects metrics, and keeps deployment history for rollback.

This repository contains a working MVP control plane, not only a dashboard mockup.

## What is included

* Go REST API with session authentication
* GitHub OAuth and repository discovery
* signed GitHub push webhooks
* PostgreSQL projects, deployments, logs, sessions, and environment variables
* recoverable Redis job queue
* concurrent deployment worker
* Git clone by exact commit
* Dockerfile support
* automatic Node.js, Python, and Java buildpacks
* candidate container health checks before traffic switching
* Traefik routing by project subdomain
* CPU, RAM, process, capability, and filesystem limits
* live build and runtime logs through Server Sent Events
* container metrics
* restart and rollback actions
* responsive web dashboard
* Docker Compose development environment
* GitHub Actions CI

## Architecture

```text
GitHub OAuth and webhook
           |
           v
Frontend -> Go API -> PostgreSQL
              |
              v
         Redis Queue
              |
              v
       Deployment Worker -> Docker Engine
                                |
                                v
Browser -> Traefik -> Application container
```

The full flow is described in [docs/architecture.md](docs/architecture.md).

## Requirements

* Docker Desktop or Docker Engine
* Docker Compose
* GitHub OAuth App only when authentication and private repositories are needed

Go and Node.js do not need to be installed locally because all services build inside Docker.

## Quick start

1. Create the local environment file.

```powershell
Copy-Item .env.example .env
```

2. Start Docker Desktop.

3. Build and run DeployHub.

```powershell
docker compose up --build
```

You can also run:

```powershell
.\scripts\dev.ps1
```

4. Open these addresses.

| Service | Address |
| --- | --- |
| DeployHub | http://deployhub.localhost |
| Direct API | http://localhost:8080/api/health |
| Traefik dashboard | http://localhost:8081 |
| PostgreSQL | localhost:5432 |
| Redis | localhost:6379 |

Local development uses `DEV_MODE=true`. This skips login but keeps every other part of the deployment pipeline active.

## First real deployment

1. Push this project to GitHub.
2. Open DeployHub and press `Project`.
3. Enter the new repository as `owner/repository`.
4. To deploy the included Node.js example, set the project folder to `examples/node-hello`.
5. Set the port to `3000` and the health check to `/health`.
6. Create the project and press `Deploy`.
7. Open `http://your-subdomain.localhost`.

The worker will clone the repository, generate a Node.js Dockerfile, build the image, start a candidate container, call `/health`, add the Traefik route, and begin streaming runtime logs.

## GitHub authentication

For private repositories and real users, create a GitHub OAuth App and fill these values in `.env`:

```dotenv
DEV_MODE=false
GITHUB_CLIENT_ID=your-client-id
GITHUB_CLIENT_SECRET=your-client-secret
GITHUB_WEBHOOK_SECRET=a-long-random-value
EXTERNAL_URL=https://deployhub.example.com
BASE_DOMAIN=deployhub.example.com
```

Detailed instructions are in [docs/github-setup.md](docs/github-setup.md).

## Production HTTPS

Point the dashboard hostname and a wildcard application domain to the Docker host. Set `EXTERNAL_URL`, `BASE_DOMAIN`, `DASHBOARD_HOST`, and `ACME_EMAIL` in `.env`, then start the production override:

```powershell
docker compose -f compose.yaml -f compose.production.yaml up -d --build
```

Traefik redirects HTTP to HTTPS, requests Let’s Encrypt certificates, and adds TLS routing to every application container created by the worker.

## Supported projects

DeployHub uses a repository Dockerfile when one exists. Without a Dockerfile it detects:

| Runtime | Detection | Default command |
| --- | --- | --- |
| Node.js | `package.json` | `npm start` |
| Python | `requirements.txt` or `pyproject.toml` | `python main.py` or `uvicorn app:app` |
| Java | `pom.xml` or `gradlew` | generated runnable JAR image |

Monorepositories are supported through the project folder setting.

## Useful commands

```powershell
# Validate the environment
.\scripts\doctor.ps1

# Follow platform logs
docker compose logs -f api worker traefik

# Stop the platform
docker compose down

# Remove local database and Redis data
docker compose down --volumes
```

The last command permanently deletes local DeployHub state.

## Tests

With Go installed:

```powershell
go test ./...
go vet ./...
node --check app.js
docker compose config --quiet
```

CI runs the same checks for every push and pull request.

## Contributing and security

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Report suspected vulnerabilities privately using the process in [SECURITY.md](SECURITY.md).

## Production direction

The MVP is designed for one trusted Docker host. Before offering it as a public multi tenant service, move image builds to rootless BuildKit, run applications on separate worker nodes or microVMs, add image registry storage, enforce organization quotas, rotate OAuth tokens, add audit logs, and configure Traefik ACME certificates.

## Project structure

```text
cmd/api                 Go API entrypoint
cmd/worker              deployment worker entrypoint
internal/api            HTTP, OAuth, webhook, projects, deploys
internal/database       PostgreSQL schema and connection
internal/queue          Redis queue
internal/worker         Git, buildpacks, Docker, logs, metrics
docker                  service images and Nginx proxy
examples/node-hello     deployable example application
docs                    architecture and GitHub setup
index.html              dashboard
styles.css              dashboard styles
app.js                  API and SSE client
compose.yaml            complete local platform
```

## Important security note

The worker needs access to the Docker socket to create workloads. Anyone who controls the worker container effectively controls the Docker host. Keep the dashboard private, protect secrets, and do not deploy untrusted third party repositories on a personal machine.
