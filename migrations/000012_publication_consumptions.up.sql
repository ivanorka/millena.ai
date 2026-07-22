CREATE TABLE publication_consumptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_type TEXT NOT NULL CHECK (source_type IN (
        'content_variant', 'social_post', 'newsletter_delivery'
    )),
    source_id UUID NOT NULL,
    billing_month DATE NOT NULL CHECK (billing_month = date_trunc('month', billing_month)::date),
    consumed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    UNIQUE (project_id, source_type, source_id, billing_month)
);

CREATE INDEX publication_consumptions_project_month_idx
    ON publication_consumptions (project_id, billing_month, consumed_at);

-- Preserve the usage represented by the old status/updated_at accounting so
-- deploying the ledger cannot silently reset the current or historical month.
INSERT INTO publication_consumptions (
    project_id, source_type, source_id, billing_month, consumed_at, metadata
)
SELECT item.project_id,
       'content_variant',
       variant.id,
       date_trunc('month', variant.updated_at AT TIME ZONE 'UTC')::date,
       variant.updated_at,
       '{"migrated":true}'::jsonb
FROM content_variants AS variant
JOIN content_items AS item ON item.id = variant.content_item_id
WHERE variant.status IN ('scheduled', 'published')
ON CONFLICT DO NOTHING;

INSERT INTO publication_consumptions (
    project_id, source_type, source_id, billing_month, consumed_at, metadata
)
SELECT post.project_id,
       'social_post',
       post.id,
       date_trunc('month', post.updated_at AT TIME ZONE 'UTC')::date,
       post.updated_at,
       '{"migrated":true}'::jsonb
FROM social_posts AS post
WHERE post.content_variant_id IS NULL
  AND post.status IN ('scheduled', 'published')
ON CONFLICT DO NOTHING;

INSERT INTO publication_consumptions (
    project_id, source_type, source_id, billing_month, consumed_at, metadata
)
SELECT delivery.project_id,
       CASE WHEN delivery.content_variant_id IS NULL
            THEN 'newsletter_delivery'
            ELSE 'content_variant'
       END,
       COALESCE(delivery.content_variant_id, delivery.id),
       date_trunc('month', delivery.updated_at AT TIME ZONE 'UTC')::date,
       delivery.updated_at,
       '{"migrated":true}'::jsonb
FROM newsletter_deliveries AS delivery
WHERE delivery.test_recipient IS NULL
  AND delivery.status IN ('scheduled', 'sent')
ON CONFLICT DO NOTHING;
