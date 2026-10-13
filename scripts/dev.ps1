$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath '.env')) {
    Copy-Item -LiteralPath '.env.example' -Destination '.env'
    Write-Host 'Created .env from .env.example. Replace development secrets before public deployment.' -ForegroundColor Yellow
}

& "$PSScriptRoot\doctor.ps1"
docker compose up --build
