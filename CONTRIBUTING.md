# Contributing to DeployHub

Thanks for helping improve DeployHub. Small, focused pull requests are the easiest to review.

## Local setup

1. Copy `.env.example` to `.env`.
2. Start Docker Desktop or Docker Engine.
3. Run `docker compose up --build`.
4. Open `http://deployhub.localhost`.

Development mode skips GitHub login while keeping the deployment pipeline active. Do not use development mode on a public host.

## Before opening a pull request

Run the project checks:

```powershell
go test ./...
go vet ./...
node --check app.js
docker compose config --quiet
```

Keep secrets out of commits, logs, screenshots, fixtures, and issue reports. Use placeholder values in examples.

## Commit and pull request guidance

* Use a short, descriptive commit subject.
* Explain the reason for a behavior change.
* Add or update tests when changing Go code.
* Update README or files in `docs` when configuration changes.
* Keep unrelated refactoring out of feature and bug fix pull requests.

## Reporting bugs

Use the bug report template and include reproducible steps. Redact OAuth tokens, webhook secrets, environment variables, repository credentials, and private URLs from all output.
