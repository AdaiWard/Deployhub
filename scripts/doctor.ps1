$ErrorActionPreference = 'Stop'

Write-Host 'DeployHub environment check' -ForegroundColor Cyan

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw 'Docker is not installed or not available in PATH.'
}

docker info *> $null
if ($LASTEXITCODE -ne 0) {
    throw 'Docker daemon is not running. Start Docker Desktop and retry.'
}

docker compose version
docker compose config --quiet

if (-not (Test-Path -LiteralPath '.env')) {
    Write-Host 'Missing .env. Copy .env.example to .env before production use.' -ForegroundColor Yellow
}

Write-Host 'Environment is ready.' -ForegroundColor Green
