-- Keep the original uploaded strategy document alongside its editable plain
-- text. The asset remains private to its project and can be downloaded only
-- through the authenticated project-assets endpoint.
ALTER TABLE project_assets
    DROP CONSTRAINT IF EXISTS project_assets_purpose_check,
    ADD CONSTRAINT project_assets_purpose_check CHECK (
        purpose IN ('assistant_attachment', 'social_media', 'content_media', 'strategy_document')
    );

ALTER TABLE project_assets
    DROP CONSTRAINT IF EXISTS project_assets_size_bytes_check,
    ADD CONSTRAINT project_assets_size_bytes_check CHECK (size_bytes BETWEEN 1 AND 26214400);

ALTER TABLE project_strategies
    ADD COLUMN IF NOT EXISTS source_asset_id UUID;

ALTER TABLE project_strategies
    DROP CONSTRAINT IF EXISTS project_strategies_source_asset_fk,
    ADD CONSTRAINT project_strategies_source_asset_fk
        FOREIGN KEY (project_id, source_asset_id)
        REFERENCES project_assets(project_id, id)
        ON DELETE SET NULL (source_asset_id);
