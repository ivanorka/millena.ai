-- SSO requires an identity-provider configuration, metadata exchange and a
-- separate authentication callback flow. Remove the inert catalog flag until
-- that complete security boundary exists; unsupported configuration must not
-- be presented as a working capability.
UPDATE plan_catalog
SET features = features - 'sso', updated_at = now()
WHERE features ? 'sso';

UPDATE project_entitlements
SET features = features - 'sso', updated_at = now()
WHERE features ? 'sso';
