CREATE TABLE social_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('linkedin', 'facebook', 'instagram', 'youtube', 'x', 'reddit', 'pinterest', 'threads')),
    mode TEXT NOT NULL DEFAULT 'sandbox' CHECK (mode IN ('sandbox', 'oauth')),
    account_handle TEXT NOT NULL CHECK (char_length(account_handle) BETWEEN 2 AND 120),
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 2 AND 120),
    status TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'error', 'disconnected')),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_checked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, provider)
);

CREATE INDEX social_connections_project_status_idx
    ON social_connections (project_id, status, provider);

CREATE TABLE social_posts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    body TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 10000),
    status TEXT NOT NULL CHECK (status IN ('scheduled', 'published', 'failed')),
    scheduled_for TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX social_posts_project_time_idx
    ON social_posts (project_id, created_at DESC);

CREATE TABLE social_publications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    social_post_id UUID NOT NULL REFERENCES social_posts(id) ON DELETE CASCADE,
    social_connection_id UUID NOT NULL REFERENCES social_connections(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('scheduled', 'published', 'failed')),
    external_reference TEXT,
    published_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (social_post_id, social_connection_id)
);

CREATE INDEX social_publications_post_idx
    ON social_publications (social_post_id, provider);

