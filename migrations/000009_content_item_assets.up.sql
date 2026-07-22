ALTER TABLE content_items
    ADD CONSTRAINT content_items_project_id_id_key UNIQUE (project_id, id);

CREATE TABLE content_item_assets (
    project_id UUID NOT NULL,
    content_item_id UUID NOT NULL,
    asset_id UUID NOT NULL,
    use_type TEXT NOT NULL CHECK (use_type IN ('attachment', 'cover')),
    position SMALLINT NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 49),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (content_item_id, asset_id, use_type),
    FOREIGN KEY (project_id, content_item_id)
        REFERENCES content_items(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, asset_id)
        REFERENCES project_assets(project_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX content_item_assets_one_cover_idx
    ON content_item_assets (content_item_id)
    WHERE use_type = 'cover';

CREATE UNIQUE INDEX content_item_assets_attachment_position_idx
    ON content_item_assets (content_item_id, position)
    WHERE use_type = 'attachment';

CREATE INDEX content_item_assets_asset_idx
    ON content_item_assets (asset_id);

-- Preserve the existing metadata API contract while adding database-enforced
-- relationships. Invalid legacy IDs are deliberately ignored during backfill;
-- the next content update validates and normalizes the complete reference set.
INSERT INTO content_item_assets (
    project_id, content_item_id, asset_id, use_type, position, metadata
)
SELECT item.project_id,
       item.id,
       asset.id,
       'attachment',
       reference.ordinality - 1,
       '{"metadataKey":"assetIds"}'::jsonb
FROM content_items AS item
CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE
        WHEN jsonb_typeof(item.metadata -> 'assetIds') = 'array'
            THEN item.metadata -> 'assetIds'
        ELSE '[]'::jsonb
    END
) WITH ORDINALITY AS reference(asset_id, ordinality)
JOIN project_assets AS asset
  ON asset.project_id = item.project_id
 AND asset.purpose = 'content_media'
 AND asset.id::text = lower(btrim(reference.asset_id))
WHERE reference.ordinality <= 50
ON CONFLICT DO NOTHING;

INSERT INTO content_item_assets (
    project_id, content_item_id, asset_id, use_type, position, metadata
)
SELECT item.project_id,
       item.id,
       asset.id,
       'cover',
       0,
       '{"metadataKey":"coverAssetId"}'::jsonb
FROM content_items AS item
JOIN project_assets AS asset
  ON asset.project_id = item.project_id
 AND asset.purpose = 'content_media'
 AND asset.id::text = lower(btrim(item.metadata ->> 'coverAssetId'))
WHERE jsonb_typeof(item.metadata -> 'coverAssetId') = 'string'
ON CONFLICT DO NOTHING;
