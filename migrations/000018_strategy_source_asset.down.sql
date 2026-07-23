ALTER TABLE project_strategies DROP CONSTRAINT IF EXISTS project_strategies_source_asset_fk;
ALTER TABLE project_strategies DROP COLUMN IF EXISTS source_asset_id;
ALTER TABLE project_assets DROP CONSTRAINT IF EXISTS project_assets_size_bytes_check;
ALTER TABLE project_assets ADD CONSTRAINT project_assets_size_bytes_check CHECK (size_bytes BETWEEN 1 AND 10485760);
ALTER TABLE project_assets DROP CONSTRAINT IF EXISTS project_assets_purpose_check;
ALTER TABLE project_assets ADD CONSTRAINT project_assets_purpose_check CHECK (purpose IN ('assistant_attachment', 'social_media', 'content_media'));
