package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/contentstate"
	"github.com/ivanorka/millena-ai/internal/limits"
)

var (
	ErrNotFound                   = errors.New("calendar item not found")
	ErrLinkedVariantChannelChange = errors.New("linked calendar item channel cannot differ from its content variant")
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context, projectID string, from, to time.Time) ([]Item, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, project_id::text, created_by::text, title, summary, channel, status,
		       scheduled_for, metadata, content_item_id::text, content_variant_id::text,
		       publication_job_id::text, created_at, updated_at
		FROM calendar_items
		WHERE project_id = $1::uuid AND scheduled_for >= $2 AND scheduled_for < $3
		ORDER BY scheduled_for, created_at`, projectID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, projectID, itemID string) (Item, error) {
	item, err := scanItem(r.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, created_by::text, title, summary, channel, status,
		       scheduled_for, metadata, content_item_id::text, content_variant_id::text,
		       publication_job_id::text, created_at, updated_at
		FROM calendar_items
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) Create(ctx context.Context, projectID, userID string, input SaveInput) (Item, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanItem(tx.QueryRow(ctx, `
		INSERT INTO calendar_items (project_id, created_by, title, summary, channel, status, scheduled_for, metadata)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, project_id::text, created_by::text, title, summary, channel, status,
		          scheduled_for, metadata, content_item_id::text, content_variant_id::text,
		          publication_job_id::text, created_at, updated_at`,
		projectID, userID, input.Title, input.Summary, input.Channel, input.Status, input.ScheduledFor, input.Metadata))
	if err != nil {
		return Item{}, err
	}
	if err := recordCalendarAudit(ctx, tx, projectID, userID, "calendar.item_created", item.ID, item); err != nil {
		return Item{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) Update(ctx context.Context, projectID, itemID, userID string, input SaveInput) (Item, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var linkedVariantID *string
	err = tx.QueryRow(ctx, `
		SELECT content_variant_id::text
		FROM calendar_items
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, itemID).Scan(&linkedVariantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}

	var capacity limits.PublicationCapacity
	capacityLocked := false
	if linkedVariantID != nil {
		if input.Status == "scheduled" || input.Status == "published" {
			capacity, err = limits.LockPublicationCapacity(ctx, tx, projectID)
			if err != nil {
				return Item{}, err
			}
			capacityLocked = true
		}

		var contentItemID string
		err = tx.QueryRow(ctx, `
			SELECT variant.content_item_id::text
			FROM content_variants AS variant
			JOIN content_items AS item ON item.id = variant.content_item_id
			WHERE item.project_id = $1::uuid AND variant.id = $2::uuid
			`, projectID, *linkedVariantID).Scan(&contentItemID)
		if err != nil {
			return Item{}, err
		}
		var lockedContentItemID string
		if err := tx.QueryRow(ctx, `
			SELECT id::text FROM content_items
			WHERE project_id = $1::uuid AND id = $2::uuid
			FOR UPDATE`, projectID, contentItemID).Scan(&lockedContentItemID); err != nil {
			return Item{}, err
		}
		var variantChannel, variantStatus string
		if err := tx.QueryRow(ctx, `
			SELECT channel, status
			FROM content_variants
			WHERE id = $1::uuid AND content_item_id = $2::uuid
			FOR UPDATE`, *linkedVariantID, contentItemID).Scan(&variantChannel, &variantStatus); err != nil {
			return Item{}, err
		}
		if input.Channel != variantChannel {
			return Item{}, ErrLinkedVariantChannelChange
		}
		if (input.Status == "scheduled" || input.Status == "published") &&
			variantStatus != "scheduled" && variantStatus != "published" {
			if variantChannel == "newsletter" {
				err = capacity.RequireAvailableForNewsletterItem(ctx, tx, projectID, contentItemID)
			} else {
				err = capacity.RequireAvailable(ctx, tx, projectID)
			}
			if err != nil {
				return Item{}, err
			}
		}
	}

	item, err := scanItem(tx.QueryRow(ctx, `
		UPDATE calendar_items
		SET title = $3, summary = $4, channel = $5, status = $6,
		    scheduled_for = $7, metadata = $8, updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING id::text, project_id::text, created_by::text, title, summary, channel, status,
		          scheduled_for, metadata, content_item_id::text, content_variant_id::text,
		          publication_job_id::text, created_at, updated_at`,
		projectID, itemID, input.Title, input.Summary, input.Channel, input.Status, input.ScheduledFor, input.Metadata))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	if item.ContentVariantID != nil {
		jobID, err := syncLinkedVariant(ctx, tx, item)
		if err != nil {
			return Item{}, err
		}
		item.PublicationJobID = jobID
		if capacityLocked {
			if err := capacity.Consume(ctx, tx, projectID, limits.PublicationSourceContentVariant, *item.ContentVariantID); err != nil {
				return Item{}, err
			}
		}
	}
	if err := recordCalendarAudit(ctx, tx, projectID, userID, "calendar.item_updated", item.ID, item); err != nil {
		return Item{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) Delete(ctx context.Context, projectID, itemID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanItem(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, created_by::text, title, summary, channel, status,
		       scheduled_for, metadata, content_item_id::text, content_variant_id::text,
		       publication_job_id::text, created_at, updated_at
		FROM calendar_items
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM calendar_items WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, itemID); err != nil {
		return err
	}
	if item.ContentVariantID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE newsletter_deliveries
			SET status = 'cancelled',
			    last_error = COALESCE(last_error, 'Cancelled because the linked calendar entry was deleted.'),
			    updated_at = now()
			WHERE project_id = $1::uuid AND content_variant_id = $2::uuid
			  AND status = 'scheduled'`, projectID, *item.ContentVariantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE publication_jobs SET status = 'cancelled', updated_at = now()
			WHERE content_variant_id = $1::uuid AND status IN ('pending', 'running')`, *item.ContentVariantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE content_variants
			SET status = 'draft', scheduled_for = NULL, revision = revision + 1, updated_at = now()
			WHERE id = $1::uuid`, *item.ContentVariantID); err != nil {
			return err
		}
		if item.ContentItemID != nil {
			if err := contentstate.Recompute(ctx, tx, projectID, *item.ContentItemID); err != nil {
				return err
			}
		}
	}
	if err := recordCalendarAudit(ctx, tx, projectID, userID, "calendar.item_deleted", item.ID, item); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func syncLinkedVariant(ctx context.Context, tx pgx.Tx, item Item) (*string, error) {
	variantStatus := item.Status
	if variantStatus == "suggestion" {
		variantStatus = "draft"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE content_variants
		SET title = $2, summary = $3, status = $4,
		    scheduled_for = CASE WHEN $4 = 'scheduled' THEN $5::timestamptz ELSE NULL END,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1::uuid`, *item.ContentVariantID, item.Title, item.Summary, variantStatus, item.ScheduledFor); err != nil {
		return nil, err
	}
	if item.Channel == "newsletter" {
		if item.Status == "scheduled" {
			var deliveryID string
			err := tx.QueryRow(ctx, `
				SELECT id::text
				FROM newsletter_deliveries
				WHERE project_id = $1::uuid AND content_variant_id = $2::uuid
				  AND status = 'scheduled' AND test_recipient IS NULL
				FOR UPDATE`, item.ProjectID, *item.ContentVariantID).Scan(&deliveryID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			if err == nil {
				if _, err := tx.Exec(ctx, `
					UPDATE newsletter_deliveries
					SET subject = $2, scheduled_for = $3, updated_at = now()
					WHERE id = $1::uuid`, deliveryID, item.Title, item.ScheduledFor); err != nil {
					return nil, err
				}
				if _, err := tx.Exec(ctx, `
					UPDATE publication_jobs
					SET status = 'cancelled', updated_at = now()
					WHERE content_variant_id = $1::uuid AND status IN ('pending', 'running')`, *item.ContentVariantID); err != nil {
					return nil, err
				}
				if _, err := tx.Exec(ctx, `
					UPDATE calendar_items
					SET publication_job_id = NULL,
					    metadata = metadata || jsonb_build_object(
					      'newsletterQueue', true, 'newsletterDeliveryId', $2::text
					    ),
					    updated_at = now()
					WHERE id = $1::uuid`, item.ID, deliveryID); err != nil {
					return nil, err
				}
				if item.ContentItemID != nil {
					if err := contentstate.Recompute(ctx, tx, item.ProjectID, *item.ContentItemID); err != nil {
						return nil, err
					}
				}
				return nil, nil
			}
		} else {
			if _, err := tx.Exec(ctx, `
				UPDATE newsletter_deliveries
				SET status = 'cancelled',
				    last_error = COALESCE(last_error, 'Cancelled because the linked calendar entry was unscheduled.'),
				    updated_at = now()
				WHERE project_id = $1::uuid AND content_variant_id = $2::uuid
				  AND status = 'scheduled'`, item.ProjectID, *item.ContentVariantID); err != nil {
				return nil, err
			}
		}
	}

	var jobID *string
	if item.Status == "scheduled" {
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO publication_jobs (project_id, content_variant_id, status, scheduled_for)
			VALUES ($1::uuid, $2::uuid, 'pending', $3)
			ON CONFLICT (content_variant_id) WHERE status IN ('pending', 'running') DO UPDATE
			SET scheduled_for = EXCLUDED.scheduled_for, status = 'pending', updated_at = now()
			RETURNING id::text`, item.ProjectID, *item.ContentVariantID, item.ScheduledFor).Scan(&id); err != nil {
			return nil, err
		}
		jobID = &id
		if _, err := tx.Exec(ctx, `UPDATE calendar_items SET publication_job_id = $2::uuid WHERE id = $1::uuid`, item.ID, id); err != nil {
			return nil, err
		}
	} else {
		jobStatus := "cancelled"
		if item.Status == "published" {
			jobStatus = "succeeded"
		} else if item.Status == "failed" {
			jobStatus = "failed"
		}
		if _, err := tx.Exec(ctx, `
			UPDATE publication_jobs SET status = $2, updated_at = now()
			WHERE content_variant_id = $1::uuid AND status IN ('pending', 'running')`, *item.ContentVariantID, jobStatus); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE calendar_items SET publication_job_id = NULL WHERE id = $1::uuid`, item.ID); err != nil {
			return nil, err
		}
	}
	if item.ContentItemID != nil {
		if err := contentstate.Recompute(ctx, tx, item.ProjectID, *item.ContentItemID); err != nil {
			return nil, err
		}
	}
	return jobID, nil
}

func recordCalendarAudit(ctx context.Context, tx pgx.Tx, projectID, userID, action, itemID string, item Item) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, NULLIF($2, '')::uuid, $3, 'calendar_item', $4::uuid,
		        jsonb_build_object('channel', $5::text, 'status', $6::text, 'scheduledFor', $7::timestamptz,
		                           'contentItemId', $8::text, 'contentVariantId', $9::text))`,
		projectID, userID, action, itemID, item.Channel, item.Status, item.ScheduledFor,
		item.ContentItemID, item.ContentVariantID)
	return err
}

type itemScanner interface {
	Scan(dest ...any) error
}

func scanItem(scanner itemScanner) (Item, error) {
	var item Item
	err := scanner.Scan(
		&item.ID, &item.ProjectID, &item.CreatedBy, &item.Title, &item.Summary,
		&item.Channel, &item.Status, &item.ScheduledFor, &item.Metadata,
		&item.ContentItemID, &item.ContentVariantID, &item.PublicationJobID,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}
