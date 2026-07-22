package workspace

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ivanorka/millena-ai/internal/contentstate"
	"github.com/ivanorka/millena-ai/internal/limits"
)

const newsletterDeliveryColumns = `
	id::text, project_id::text, content_item_id::text, content_variant_id::text, list_id::text, mode, status,
	subject, test_recipient, recipient_count, scheduled_for, sent_at,
	external_reference, last_error, created_by::text, created_at, updated_at`

func (r *Repository) ListNewsletterDeliveries(ctx context.Context, projectID string) ([]NewsletterDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+newsletterDeliveryColumns+`
		FROM newsletter_deliveries
		WHERE project_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 100`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deliveries := make([]NewsletterDelivery, 0)
	for rows.Next() {
		delivery, err := scanNewsletterDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (r *Repository) CreateNewsletterDelivery(ctx context.Context, projectID, actorID string, input NewsletterDeliveryInput) (NewsletterDelivery, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return NewsletterDelivery{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var capacity limits.PublicationCapacity
	if input.TestRecipient == nil {
		capacity, err = limits.LockPublicationCapacity(ctx, tx, projectID)
		if err != nil {
			return NewsletterDelivery{}, err
		}
	}

	var contentSummary, contentBody, defaultLocale string
	err = tx.QueryRow(ctx, `
		SELECT content.summary, content.body, project.default_locale
		FROM content_items AS content
		JOIN projects AS project ON project.id = content.project_id
		WHERE content.project_id = $1::uuid AND content.id = $2::uuid
		  AND content.kind = 'newsletter'
		FOR UPDATE OF content`, projectID, input.ContentItemID).Scan(
		&contentSummary, &contentBody, &defaultLocale,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return NewsletterDelivery{}, ErrInvalidReference
	}
	if err != nil {
		return NewsletterDelivery{}, err
	}
	if input.ListID != nil {
		var listExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM audience_lists WHERE project_id = $1::uuid AND id = $2::uuid
			)`, projectID, *input.ListID).Scan(&listExists); err != nil {
			return NewsletterDelivery{}, err
		}
		if !listExists {
			return NewsletterDelivery{}, ErrInvalidReference
		}
	}

	status := "scheduled"
	recipientCount := 0
	var contentVariantID *string
	if input.TestRecipient != nil {
		status = "test_sent"
		recipientCount = 1
	} else {
		var existingDeliveryID string
		err = tx.QueryRow(ctx, `
			SELECT id::text
			FROM newsletter_deliveries
			WHERE project_id = $1::uuid AND content_item_id = $2::uuid
			  AND status = 'scheduled' AND test_recipient IS NULL
			FOR UPDATE`, projectID, input.ContentItemID).Scan(&existingDeliveryID)
		if err == nil {
			return NewsletterDelivery{}, ErrConflict
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return NewsletterDelivery{}, err
		}
		variantLocale := defaultLocale
		rows, err := tx.Query(ctx, `
			SELECT id::text, locale
			FROM content_variants
			WHERE content_item_id = $1::uuid AND channel = 'newsletter'
			  AND status = 'scheduled'
			ORDER BY locale
			FOR UPDATE`, input.ContentItemID)
		if err != nil {
			return NewsletterDelivery{}, err
		}
		scheduledVariantCount := 0
		for rows.Next() {
			var scheduledVariantID string
			if err := rows.Scan(&scheduledVariantID, &variantLocale); err != nil {
				rows.Close()
				return NewsletterDelivery{}, err
			}
			scheduledVariantCount++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return NewsletterDelivery{}, err
		}
		rows.Close()
		if scheduledVariantCount > 1 {
			return NewsletterDelivery{}, ErrConflict
		}
		if err := capacity.RequireAvailableForNewsletterItem(ctx, tx, projectID, input.ContentItemID); err != nil {
			return NewsletterDelivery{}, err
		}

		err = tx.QueryRow(ctx, `
			SELECT count(*)
			FROM audience_contacts
			WHERE project_id = $1::uuid
			  AND status = 'active' AND consent
			  AND ($2::uuid IS NULL OR list_id = $2::uuid)`, projectID, input.ListID).Scan(&recipientCount)
		if err != nil {
			return NewsletterDelivery{}, err
		}

		var variantID string
		err = tx.QueryRow(ctx, `
			INSERT INTO content_variants (
				content_item_id, channel, locale, title, summary, body, status,
				scheduled_for, metadata
			)
			VALUES ($1::uuid, 'newsletter', $2, $3, $4, $5, 'scheduled', $6,
				'{"newsletterQueue":true}'::jsonb)
			ON CONFLICT (content_item_id, channel, locale) DO UPDATE SET
				title = EXCLUDED.title,
				summary = CASE
					WHEN content_variants.summary = '' THEN EXCLUDED.summary
					ELSE content_variants.summary
				END,
				body = CASE
					WHEN content_variants.body = '' THEN EXCLUDED.body
					ELSE content_variants.body
				END,
				status = 'scheduled', scheduled_for = EXCLUDED.scheduled_for,
				metadata = content_variants.metadata || EXCLUDED.metadata,
				revision = content_variants.revision + 1, updated_at = now()
			RETURNING id::text`, input.ContentItemID, variantLocale, input.Subject,
			contentSummary, contentBody, input.ScheduledFor).Scan(&variantID)
		if err != nil {
			return NewsletterDelivery{}, err
		}
		contentVariantID = &variantID
		if _, err := tx.Exec(ctx, `
			UPDATE publication_jobs
			SET status = 'cancelled', updated_at = now()
			WHERE content_variant_id = $1::uuid AND status IN ('pending', 'running')`, variantID); err != nil {
			return NewsletterDelivery{}, err
		}
	}

	var delivery NewsletterDelivery
	err = tx.QueryRow(ctx, `
		INSERT INTO newsletter_deliveries (
			project_id, content_item_id, content_variant_id, list_id, mode, status, subject,
			test_recipient, recipient_count, scheduled_for, sent_at,
			external_reference, created_by
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'sandbox', $5, $6, $7, $8,
		        CASE WHEN $5 = 'scheduled' THEN $9::timestamptz ELSE NULL END,
		        CASE WHEN $5 = 'test_sent' THEN now() ELSE NULL END,
		        CASE WHEN $5 = 'test_sent' THEN 'sandbox://newsletter/test/' || encode(gen_random_bytes(12), 'hex') ELSE NULL END,
		        $10::uuid)
		RETURNING `+newsletterDeliveryColumns,
		projectID, input.ContentItemID, contentVariantID, input.ListID, status, input.Subject,
		input.TestRecipient, recipientCount, input.ScheduledFor, actorID).Scan(
		&delivery.ID, &delivery.ProjectID, &delivery.ContentItemID, &delivery.ContentVariantID, &delivery.ListID,
		&delivery.Mode, &delivery.Status, &delivery.Subject, &delivery.TestRecipient,
		&delivery.RecipientCount, &delivery.ScheduledFor, &delivery.SentAt,
		&delivery.ExternalReference, &delivery.LastError, &delivery.CreatedBy,
		&delivery.CreatedAt, &delivery.UpdatedAt,
	)
	if err != nil {
		return NewsletterDelivery{}, err
	}
	if status == "scheduled" {
		if err := capacity.Consume(ctx, tx, projectID, limits.PublicationSourceContentVariant, *contentVariantID); err != nil {
			return NewsletterDelivery{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE content_variants
			SET metadata = metadata || jsonb_build_object(
			      'newsletterQueue', true, 'newsletterDeliveryId', $2::text
			    ),
			    updated_at = now()
			WHERE id = $1::uuid`, *contentVariantID, delivery.ID); err != nil {
			return NewsletterDelivery{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO calendar_items (
				project_id, created_by, title, summary, channel, status, scheduled_for,
				content_item_id, content_variant_id, publication_job_id, metadata
			)
			VALUES ($1::uuid, $2::uuid, $3, $4, 'newsletter', 'scheduled', $5,
				$6::uuid, $7::uuid, NULL,
				jsonb_build_object('synced', true, 'newsletterQueue', true, 'newsletterDeliveryId', $8::text))
			ON CONFLICT (content_variant_id) WHERE content_variant_id IS NOT NULL DO UPDATE
			SET title = EXCLUDED.title, summary = EXCLUDED.summary, channel = 'newsletter',
			    status = 'scheduled', scheduled_for = EXCLUDED.scheduled_for,
			    publication_job_id = NULL,
			    metadata = calendar_items.metadata || EXCLUDED.metadata,
			    updated_at = now()`, projectID, actorID, input.Subject,
			contentSummary, input.ScheduledFor, input.ContentItemID, *contentVariantID, delivery.ID); err != nil {
			return NewsletterDelivery{}, err
		}
		if err := contentstate.Recompute(ctx, tx, projectID, input.ContentItemID); err != nil {
			return NewsletterDelivery{}, err
		}
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "newsletter_delivery.created", "newsletter_delivery", &delivery.ID, map[string]any{
		"status": delivery.Status, "recipientCount": delivery.RecipientCount,
		"contentItemId": delivery.ContentItemID, "contentVariantId": delivery.ContentVariantID,
	}); err != nil {
		return NewsletterDelivery{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return NewsletterDelivery{}, err
	}
	return delivery, nil
}

func scanNewsletterDelivery(row scanner) (NewsletterDelivery, error) {
	var delivery NewsletterDelivery
	err := row.Scan(
		&delivery.ID, &delivery.ProjectID, &delivery.ContentItemID, &delivery.ContentVariantID, &delivery.ListID,
		&delivery.Mode, &delivery.Status, &delivery.Subject, &delivery.TestRecipient,
		&delivery.RecipientCount, &delivery.ScheduledFor, &delivery.SentAt,
		&delivery.ExternalReference, &delivery.LastError, &delivery.CreatedBy,
		&delivery.CreatedAt, &delivery.UpdatedAt,
	)
	return delivery, err
}
