DROP TABLE IF EXISTS social_post_assets;
DROP TABLE IF EXISTS assistant_message_assets;

ALTER TABLE social_posts
    DROP CONSTRAINT IF EXISTS social_posts_project_id_id_key;

ALTER TABLE assistant_messages
    DROP CONSTRAINT IF EXISTS assistant_messages_project_id_id_key;

DROP TABLE IF EXISTS project_assets;

ALTER TABLE project_profiles
    DROP COLUMN IF EXISTS company_description;
