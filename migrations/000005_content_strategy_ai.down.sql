DROP TABLE IF EXISTS project_strategies;

DROP INDEX IF EXISTS content_items_project_kind_idx;
DROP INDEX IF EXISTS content_items_project_seed_idx;

UPDATE content_items
SET kind = 'source'
WHERE kind NOT IN ('source', 'social', 'blog', 'newsletter');

ALTER TABLE content_items
    DROP COLUMN IF EXISTS seed_key,
    DROP COLUMN IF EXISTS revision,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS scheduled_for,
    DROP COLUMN IF EXISTS channels,
    DROP COLUMN IF EXISTS summary,
    DROP CONSTRAINT content_items_kind_check,
    ADD CONSTRAINT content_items_kind_check CHECK (kind IN ('source', 'social', 'blog', 'newsletter'));
