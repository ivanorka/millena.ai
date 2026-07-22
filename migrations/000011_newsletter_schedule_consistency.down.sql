DROP INDEX IF EXISTS newsletter_deliveries_active_content_idx;
DROP INDEX IF EXISTS newsletter_deliveries_variant_idx;

ALTER TABLE newsletter_deliveries
    DROP COLUMN IF EXISTS content_variant_id;
