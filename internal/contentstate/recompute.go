// Package contentstate keeps the denormalized content_items state aligned
// with its publishable variants and newsletter delivery queue.
package contentstate

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is implemented by pgx.Tx and keeps recomputation inside the caller's
// transaction.
type Execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Recompute applies one canonical state precedence everywhere:
// scheduled, failed, in_review, draft, approved, published, then draft when
// no actionable child state exists. Newsletter deliveries participate in the
// same state and schedule calculation as content variants.
func Recompute(ctx context.Context, execer Execer, projectID, itemID string) error {
	_, err := execer.Exec(ctx, `
		UPDATE content_items AS item
		SET channels = ARRAY(
				SELECT channel
				FROM (
					SELECT variant.channel
					FROM content_variants AS variant
					WHERE variant.content_item_id = item.id
					UNION
					SELECT 'newsletter'
					WHERE EXISTS (
						SELECT 1
						FROM newsletter_deliveries AS delivery
						WHERE delivery.content_item_id = item.id
					)
				) AS child_channels
				ORDER BY channel
			),
			status = CASE
				WHEN EXISTS (
					SELECT 1 FROM content_variants
					WHERE content_item_id = item.id AND status = 'scheduled'
				) OR EXISTS (
					SELECT 1 FROM newsletter_deliveries
					WHERE content_item_id = item.id AND status = 'scheduled'
				) THEN 'scheduled'
				WHEN EXISTS (
					SELECT 1 FROM content_variants
					WHERE content_item_id = item.id AND status = 'failed'
				) OR EXISTS (
					SELECT 1 FROM newsletter_deliveries
					WHERE content_item_id = item.id AND status = 'failed'
				) THEN 'failed'
				WHEN EXISTS (
					SELECT 1 FROM content_variants
					WHERE content_item_id = item.id AND status = 'in_review'
				) THEN 'in_review'
				WHEN EXISTS (
					SELECT 1 FROM content_variants
					WHERE content_item_id = item.id AND status = 'draft'
				) THEN 'draft'
				WHEN EXISTS (
					SELECT 1 FROM content_variants
					WHERE content_item_id = item.id AND status = 'approved'
				) THEN 'approved'
				WHEN EXISTS (
					SELECT 1 FROM content_variants
					WHERE content_item_id = item.id AND status = 'published'
				) OR EXISTS (
					SELECT 1 FROM newsletter_deliveries
					WHERE content_item_id = item.id AND status = 'sent'
				) THEN 'published'
				ELSE 'draft'
			END,
			scheduled_for = (
				SELECT min(due_at)
				FROM (
					SELECT scheduled_for AS due_at
					FROM content_variants
					WHERE content_item_id = item.id AND status = 'scheduled'
					UNION ALL
					SELECT scheduled_for AS due_at
					FROM newsletter_deliveries
					WHERE content_item_id = item.id AND status = 'scheduled'
				) AS due
			),
			updated_at = now()
		WHERE item.id = $2::uuid
		  AND (NULLIF($1, '')::uuid IS NULL OR item.project_id = NULLIF($1, '')::uuid)`, projectID, itemID)
	return err
}
