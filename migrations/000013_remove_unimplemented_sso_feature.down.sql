UPDATE plan_catalog
SET features = jsonb_set(
      features,
      '{sso}',
      CASE WHEN code = 'unlimited' THEN 'true'::jsonb ELSE 'false'::jsonb END,
      true
    ),
    updated_at = now()
WHERE NOT (features ? 'sso');

UPDATE project_entitlements
SET features = jsonb_set(
      features,
      '{sso}',
      CASE WHEN plan_code = 'unlimited' THEN 'true'::jsonb ELSE 'false'::jsonb END,
      true
    ),
    updated_at = now()
WHERE NOT (features ? 'sso');
