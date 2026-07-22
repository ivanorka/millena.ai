DROP INDEX IF EXISTS social_posts_content_variant_idx;

ALTER TABLE social_posts
    DROP COLUMN IF EXISTS content_variant_id,
    DROP COLUMN IF EXISTS content_item_id;

DROP INDEX IF EXISTS calendar_items_variant_idx;

ALTER TABLE calendar_items
    DROP COLUMN IF EXISTS publication_job_id,
    DROP COLUMN IF EXISTS content_variant_id,
    DROP COLUMN IF EXISTS content_item_id;

DROP INDEX IF EXISTS publication_jobs_variant_active_idx;

ALTER TABLE content_variants
    DROP COLUMN IF EXISTS revision,
    DROP COLUMN IF EXISTS scheduled_for,
    DROP COLUMN IF EXISTS summary,
    DROP COLUMN IF EXISTS title;

DROP TABLE IF EXISTS service_requests;
DROP INDEX IF EXISTS newsletter_deliveries_due_idx;
DROP TABLE IF EXISTS newsletter_deliveries;
DROP TABLE IF EXISTS assistant_messages;
DROP TABLE IF EXISTS assistant_threads;
DROP TABLE IF EXISTS channel_connections;
DROP TABLE IF EXISTS audience_contacts;
DROP TABLE IF EXISTS audience_lists;
DROP INDEX IF EXISTS automation_rules_due_idx;
DROP TABLE IF EXISTS automation_rules;
DROP TABLE IF EXISTS project_profiles;

ALTER TABLE project_entitlements
    DROP CONSTRAINT IF EXISTS project_entitlements_plan_code_fkey,
    ADD CONSTRAINT project_entitlements_plan_code_check
        CHECK (plan_code IN ('starter', 'growth', 'unlimited'));

DROP TABLE IF EXISTS plan_catalog;
