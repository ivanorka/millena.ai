-- New accounts start on the Starter plan with ten publication units per month.
-- Existing tenant entitlements are left untouched.
UPDATE plan_catalog
SET monthly_publication_limit = 10,
    description = 'Početni sadržajni workflow s do 10 objava mjesečno.',
    updated_at = now()
WHERE code = 'starter' AND is_system;
