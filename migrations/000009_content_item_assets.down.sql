DROP TABLE IF EXISTS content_item_assets;

ALTER TABLE content_items
    DROP CONSTRAINT IF EXISTS content_items_project_id_id_key;
