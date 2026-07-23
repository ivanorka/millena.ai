package notification

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDeliveryDisabled = errors.New("email delivery is not configured")

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

type Delivery struct {
	ID string
	Message
}

func (r *Repository) ClaimPending(ctx context.Context, limit int) ([]Delivery, error) {
	if r == nil || limit < 1 {
		return nil, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		WITH claimed AS (
			SELECT id FROM email_notifications
			WHERE status IN ('pending', 'failed') AND next_attempt_at <= now() AND attempts < 5
			ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT $1
		), updated AS (
			UPDATE email_notifications AS notification
			SET status = 'processing', attempts = notification.attempts + 1, updated_at = now()
			FROM claimed
			WHERE notification.id = claimed.id
			RETURNING notification.id, notification.recipient_user_id, notification.project_id,
			          notification.subject, notification.summary, notification.action_path
		)
		SELECT updated.id::text, recipient.display_name, recipient.email,
		       COALESCE(project.name, 'Millena AI'), updated.subject, updated.summary, updated.action_path
		FROM updated
		JOIN users AS recipient ON recipient.id = updated.recipient_user_id
		LEFT JOIN projects AS project ON project.id = updated.project_id`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Delivery, 0, limit)
	for rows.Next() {
		var item Delivery
		if err := rows.Scan(&item.ID, &item.RecipientName, &item.RecipientEmail, &item.ProjectName, &item.Subject, &item.Summary, &item.ActionPath); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}

func (r *Repository) MarkSent(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE email_notifications SET status='sent', sent_at=now(), last_error=NULL, updated_at=now() WHERE id=$1::uuid`, id)
	return err
}

func (r *Repository) MarkFailed(ctx context.Context, id string, attempt int, reason error) error {
	message := strings.TrimSpace(reason.Error())
	if len(message) > 1000 {
		message = message[:1000]
	}
	delay := time.Duration(1<<min(attempt, 5)) * time.Minute
	_, err := r.pool.Exec(ctx, `UPDATE email_notifications SET status='failed', next_attempt_at=now()+$2::interval, last_error=$3, updated_at=now() WHERE id=$1::uuid`, id, interval(delay), message)
	return err
}

func interval(value time.Duration) string { return strings.TrimSuffix(value.String(), "0s") }
func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
