package actions

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
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

func (r *Repository) Record(ctx context.Context, projectID, actorID string, input RecordInput) (Event, error) {
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["label"] = input.Label
	metadata["screen"] = input.Screen
	var event Event
	err := r.pool.QueryRow(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, metadata)
		VALUES ($1::uuid, $2::uuid, $3, 'ui_action', $4)
		RETURNING id::text, project_id::text, actor_id::text, action, entity_type, metadata, created_at`,
		projectID, actorID, input.Action, metadata).Scan(
		&event.ID, &event.ProjectID, &event.ActorID, &event.Action,
		&event.EntityType, &event.Metadata, &event.CreatedAt)
	return event, err
}

func (r *Repository) List(ctx context.Context, projectID string) ([]Event, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, project_id::text, actor_id::text, action, entity_type, metadata, created_at
		FROM audit_events
		WHERE project_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 100`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.ProjectID, &event.ActorID, &event.Action, &event.EntityType, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
