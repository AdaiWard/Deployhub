CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    github_id BIGINT UNIQUE,
    login TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    avatar_url TEXT NOT NULL DEFAULT '',
    access_token_encrypted TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    repo_full_name TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    branch TEXT NOT NULL DEFAULT 'main',
    root_directory TEXT NOT NULL DEFAULT '',
    runtime TEXT NOT NULL DEFAULT 'auto',
    dockerfile_path TEXT NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 3000 CHECK (port BETWEEN 1 AND 65535),
    healthcheck_path TEXT NOT NULL DEFAULT '/',
    subdomain TEXT NOT NULL,
    auto_deploy BOOLEAN NOT NULL DEFAULT TRUE,
    github_webhook_id BIGINT,
    memory_mb INTEGER NOT NULL DEFAULT 512 CHECK (memory_mb BETWEEN 64 AND 8192),
    cpu DOUBLE PRECISION NOT NULL DEFAULT 0.5 CHECK (cpu > 0 AND cpu <= 8),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, slug),
    UNIQUE (subdomain)
);

ALTER TABLE projects ADD COLUMN IF NOT EXISTS github_webhook_id BIGINT;

CREATE TABLE IF NOT EXISTS environment_variables (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value_encrypted TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, key)
);

CREATE TABLE IF NOT EXISTS deployments (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    commit_sha TEXT NOT NULL DEFAULT '',
    commit_message TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    image_tag TEXT NOT NULL DEFAULT '',
    container_name TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    trigger TEXT NOT NULL DEFAULT 'manual',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS deployments_project_created_idx
    ON deployments(project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS deployment_logs (
    id BIGSERIAL PRIMARY KEY,
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stream TEXT NOT NULL DEFAULT 'system',
    message TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS deployment_logs_deployment_id_idx
    ON deployment_logs(deployment_id, id);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id TEXT PRIMARY KEY,
    event TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
