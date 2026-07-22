-- Keep Optimum so projects created under it retain a valid foreign key, but
-- restore the catalogue that existed after migration 015.
UPDATE plan_catalog
SET monthly_publication_limit = 10,
    description = 'Početni sadržajni workflow s do 10 objava mjesečno.',
    updated_at = now()
WHERE code = 'starter' AND is_system;

UPDATE plan_catalog
SET name = 'Unlimited',
    description = 'Svi moduli, kanali i neograničen rad.',
    updated_at = now()
WHERE code = 'unlimited' AND is_system;

UPDATE plan_catalog
SET is_active = true, updated_at = now()
WHERE code = 'growth' AND is_system;

UPDATE plan_catalog
SET is_active = false, updated_at = now()
WHERE code = 'optimum' AND is_system;
