ALTER TABLE project_profiles
    ADD COLUMN company_description TEXT NOT NULL DEFAULT ''
        CHECK (char_length(company_description) <= 1000);

CREATE TABLE project_assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL,
    purpose TEXT NOT NULL CHECK (
        purpose IN ('assistant_attachment', 'social_media', 'content_media')
    ),
    filename TEXT NOT NULL CHECK (char_length(filename) BETWEEN 1 AND 255),
    mime_type TEXT NOT NULL CHECK (char_length(mime_type) BETWEEN 1 AND 255),
    size_bytes BIGINT NOT NULL CHECK (size_bytes BETWEEN 1 AND 10485760),
    sha256 BYTEA NOT NULL CHECK (octet_length(sha256) = 32),
    data BYTEA NOT NULL,
    extracted_text TEXT CHECK (
        extracted_text IS NULL OR char_length(extracted_text) <= 120000
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_assets_data_size_check
        CHECK (octet_length(data) = size_bytes),
    CONSTRAINT project_assets_social_media_type_check
        CHECK (
            purpose <> 'social_media'
            OR mime_type LIKE 'image/%'
            OR mime_type LIKE 'video/%'
        ),
    CONSTRAINT project_assets_project_id_id_key UNIQUE (project_id, id)
);

CREATE INDEX project_assets_project_time_idx
    ON project_assets (project_id, created_at DESC);
CREATE INDEX project_assets_project_purpose_idx
    ON project_assets (project_id, purpose, created_at DESC);
CREATE INDEX project_assets_project_sha_idx
    ON project_assets (project_id, sha256);

ALTER TABLE assistant_messages
    ADD CONSTRAINT assistant_messages_project_id_id_key UNIQUE (project_id, id);

ALTER TABLE social_posts
    ADD CONSTRAINT social_posts_project_id_id_key UNIQUE (project_id, id);

CREATE TABLE assistant_message_assets (
    project_id UUID NOT NULL,
    message_id UUID NOT NULL,
    asset_id UUID NOT NULL,
    position SMALLINT NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 9),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, asset_id),
    UNIQUE (message_id, position),
    FOREIGN KEY (project_id, message_id)
        REFERENCES assistant_messages(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, asset_id)
        REFERENCES project_assets(project_id, id) ON DELETE CASCADE
);

CREATE INDEX assistant_message_assets_asset_idx
    ON assistant_message_assets (asset_id);

CREATE TABLE social_post_assets (
    project_id UUID NOT NULL,
    social_post_id UUID NOT NULL,
    asset_id UUID NOT NULL,
    position SMALLINT NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 9),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (social_post_id, asset_id),
    UNIQUE (social_post_id, position),
    FOREIGN KEY (project_id, social_post_id)
        REFERENCES social_posts(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, asset_id)
        REFERENCES project_assets(project_id, id) ON DELETE CASCADE
);

CREATE INDEX social_post_assets_asset_idx
    ON social_post_assets (asset_id);
