DROP TABLE IF EXISTS calendar_items;
DROP TABLE IF EXISTS project_entitlements;
DROP TABLE IF EXISTS user_sessions;
ALTER TABLE project_members DROP COLUMN IF EXISTS status, DROP COLUMN IF EXISTS permissions;
ALTER TABLE users DROP COLUMN IF EXISTS last_login_at, DROP COLUMN IF EXISTS password_hash;

