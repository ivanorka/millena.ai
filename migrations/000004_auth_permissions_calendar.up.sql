ALTER TABLE users
    ADD COLUMN password_hash TEXT,
    ADD COLUMN last_login_at TIMESTAMPTZ;

ALTER TABLE project_members
    ADD COLUMN permissions JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended'));

CREATE TABLE user_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX user_sessions_user_expiry_idx ON user_sessions (user_id, expires_at DESC);

CREATE TABLE project_entitlements (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    plan_code TEXT NOT NULL CHECK (plan_code IN ('starter', 'growth', 'unlimited')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('trial', 'active', 'past_due', 'cancelled')),
    seat_limit INTEGER CHECK (seat_limit IS NULL OR seat_limit > 0),
    monthly_publication_limit INTEGER CHECK (monthly_publication_limit IS NULL OR monthly_publication_limit > 0),
    storage_limit_bytes BIGINT CHECK (storage_limit_bytes IS NULL OR storage_limit_bytes > 0),
    features JSONB NOT NULL DEFAULT '{}'::jsonb,
    renews_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE calendar_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 2 AND 180),
    summary TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL CHECK (channel IN ('linkedin', 'facebook', 'instagram', 'blog', 'newsletter', 'youtube', 'x', 'reddit', 'pinterest', 'threads')),
    status TEXT NOT NULL CHECK (status IN ('suggestion', 'draft', 'in_review', 'scheduled', 'published', 'failed')),
    scheduled_for TIMESTAMPTZ NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    seed_key TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX calendar_items_project_schedule_idx ON calendar_items (project_id, scheduled_for);
CREATE UNIQUE INDEX calendar_items_project_seed_idx ON calendar_items (project_id, seed_key) WHERE seed_key IS NOT NULL;

