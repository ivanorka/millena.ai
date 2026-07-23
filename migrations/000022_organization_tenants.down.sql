UPDATE plan_catalog
SET seat_limit = CASE code WHEN 'starter' THEN 3 WHEN 'optimum' THEN 10 ELSE seat_limit END,
    updated_at = now()
WHERE is_system AND code IN ('starter', 'optimum');

DROP TABLE IF EXISTS organization_entitlements;
DROP TABLE IF EXISTS organization_members;
DROP INDEX IF EXISTS projects_organization_idx;
ALTER TABLE projects DROP COLUMN IF EXISTS organization_id;
DROP TABLE IF EXISTS organizations;
