ALTER TABLE newsletter_deliveries
    ADD COLUMN content_variant_id UUID REFERENCES content_variants(id) ON DELETE SET NULL;

-- A content item can have only one live recipient delivery. Keep the oldest
-- existing schedule and make later legacy duplicates explicit cancellations
-- before installing the concurrency-safe unique index.
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY content_item_id
               ORDER BY scheduled_for, created_at, id
           ) AS position
    FROM newsletter_deliveries
    WHERE status = 'scheduled' AND test_recipient IS NULL
)
UPDATE newsletter_deliveries AS delivery
SET status = 'cancelled',
    last_error = COALESCE(delivery.last_error, 'Cancelled while consolidating duplicate newsletter schedules.'),
    updated_at = now()
FROM ranked
WHERE ranked.id = delivery.id AND ranked.position > 1;

UPDATE newsletter_deliveries AS delivery
SET content_variant_id = (
    SELECT variant.id
    FROM content_variants AS variant
    JOIN content_items AS item ON item.id = variant.content_item_id
    JOIN projects AS project ON project.id = item.project_id
    WHERE variant.content_item_id = delivery.content_item_id
      AND variant.channel = 'newsletter'
    ORDER BY (variant.locale = project.default_locale) DESC,
             variant.created_at,
             variant.id
    LIMIT 1
)
WHERE delivery.content_variant_id IS NULL;

CREATE INDEX newsletter_deliveries_variant_idx
    ON newsletter_deliveries (content_variant_id)
    WHERE content_variant_id IS NOT NULL;

CREATE UNIQUE INDEX newsletter_deliveries_active_content_idx
    ON newsletter_deliveries (content_item_id)
    WHERE status = 'scheduled' AND test_recipient IS NULL;

-- A linked recipient delivery is the execution queue for newsletters. Retire
-- generic publication jobs that would otherwise send the same variant twice.
UPDATE publication_jobs AS job
SET status = 'cancelled', updated_at = now()
FROM newsletter_deliveries AS delivery
WHERE delivery.content_variant_id = job.content_variant_id
  AND delivery.status = 'scheduled'
  AND job.status IN ('pending', 'running');

UPDATE calendar_items AS calendar
SET publication_job_id = NULL,
    metadata = calendar.metadata || jsonb_build_object(
        'newsletterDeliveryId', delivery.id::text,
        'newsletterQueue', true
    ),
    updated_at = now()
FROM newsletter_deliveries AS delivery
WHERE delivery.content_variant_id = calendar.content_variant_id
  AND delivery.status = 'scheduled';
