-- Restore the former Starter catalogue value. Existing tenant entitlements are
-- deliberately not changed by either direction of this migration.
UPDATE plan_catalog
SET monthly_publication_limit = 30,
    description = 'Osnovni sadržajni workflow za mali tim.',
    updated_at = now()
WHERE code = 'starter' AND is_system;
