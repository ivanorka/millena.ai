-- Public signup offers three clear choices. The legacy `unlimited` code is
-- retained for existing entitlements and presented as Enterprise.
UPDATE plan_catalog
SET monthly_publication_limit = 30,
    description = 'Početni sadržajni workflow s do 30 objava mjesečno.',
    updated_at = now()
WHERE code = 'starter' AND is_system;

INSERT INTO plan_catalog (
    code, name, description, price_cents, currency, billing_interval,
    seat_limit, monthly_publication_limit, storage_limit_bytes, features,
    is_active, is_system
)
VALUES (
    'optimum', 'Optimum', 'Napredni sadržajni workflow s do 100 objava mjesečno.',
    7900, 'EUR', 'month', 10, 100, 53687091200,
    '{"aiAgents":true,"analytics":true,"api":true,"auditLog":true,"automations":true,"prioritySupport":false,"socialChannels":"all","whiteLabel":false}'::jsonb,
    true, true
)
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    price_cents = EXCLUDED.price_cents,
    currency = EXCLUDED.currency,
    billing_interval = EXCLUDED.billing_interval,
    seat_limit = EXCLUDED.seat_limit,
    monthly_publication_limit = EXCLUDED.monthly_publication_limit,
    storage_limit_bytes = EXCLUDED.storage_limit_bytes,
    features = EXCLUDED.features,
    is_active = true,
    is_system = true,
    updated_at = now();

UPDATE plan_catalog
SET name = 'Enterprise',
    description = 'Svi moduli, kanali i neograničen broj objava.',
    updated_at = now()
WHERE code = 'unlimited' AND is_system;

UPDATE plan_catalog
SET is_active = false, updated_at = now()
WHERE code = 'growth' AND is_system;
