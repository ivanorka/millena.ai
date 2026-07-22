package assistant

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/assets"
	"github.com/ivanorka/millena-ai/internal/content"
)

var ErrNotFound = errors.New("assistant record not found")
var ErrAutomationUnavailable = errors.New("automations are not available for the project plan")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

func (r *Repository) Threads(ctx context.Context, projectID string) ([]Thread, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT thread.id::text, thread.project_id::text, thread.title, thread.channel,
		       count(message.id)::int,
		       COALESCE((array_agg(message.body ORDER BY message.created_at DESC) FILTER (WHERE message.id IS NOT NULL))[1], ''),
		       COALESCE(max(message.created_at), thread.created_at), thread.created_at, thread.updated_at
		FROM assistant_threads AS thread
		LEFT JOIN assistant_messages AS message ON message.thread_id = thread.id
		WHERE thread.project_id = $1::uuid
		GROUP BY thread.id
		ORDER BY COALESCE(max(message.created_at), thread.created_at) DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Thread, 0)
	for rows.Next() {
		var item Thread
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Title, &item.Channel,
			&item.MessageCount, &item.LastMessage, &item.LastMessageAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateThread(ctx context.Context, projectID, userID string, input CreateThreadInput) (Thread, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Thread{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item Thread
	err = tx.QueryRow(ctx, `
		INSERT INTO assistant_threads (project_id, title, channel, created_by)
		VALUES ($1::uuid, $2, $3, $4::uuid)
		RETURNING id::text, project_id::text, title, channel, 0, '', created_at, created_at, updated_at`,
		projectID, input.Title, input.Channel, userID).Scan(
		&item.ID, &item.ProjectID, &item.Title, &item.Channel, &item.MessageCount,
		&item.LastMessage, &item.LastMessageAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return Thread{}, err
	}
	welcome := "Novi razgovor je spreman. Koristim strategiju, sadržaj, kalendar, publiku i pravila ovog projekta."
	if _, err := insertMessage(ctx, tx, item.ID, projectID, "assistant", welcome, "", nil, map[string]any{"provider": "millena-local"}, nil); err != nil {
		return Thread{}, err
	}
	if err := insertAudit(ctx, tx, projectID, userID, "assistant.thread_created", "assistant_thread", &item.ID, map[string]any{"channel": item.Channel}); err != nil {
		return Thread{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) Messages(ctx context.Context, projectID, threadID string) ([]Message, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT message.id::text, message.thread_id::text, message.project_id::text,
		       message.role, message.body, message.action_type,
		       message.action_entity_id::text, message.metadata,
		       message.created_by::text, message.created_at
		FROM assistant_messages AS message
		JOIN assistant_threads AS thread ON thread.id = message.thread_id
		WHERE message.project_id = $1::uuid AND thread.project_id = $1::uuid AND message.thread_id = $2::uuid
		ORDER BY message.created_at`, projectID, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Message, 0)
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM assistant_threads WHERE project_id = $1::uuid AND id = $2::uuid)`, projectID, threadID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	for index := range items {
		attachments, err := assets.ListAssistantMessageReferences(ctx, r.pool, projectID, items[index].ID)
		if err != nil {
			return nil, err
		}
		items[index].Attachments = attachments
	}
	return items, nil
}

func (r *Repository) SaveExchange(ctx context.Context, projectID, threadID, userID, userBody, assistantBody, actionType string, entityID *string, metadata map[string]any, attachmentIDs []string) (Message, Message, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Message{}, Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM assistant_threads WHERE project_id = $1::uuid AND id = $2::uuid)`, projectID, threadID).Scan(&exists); err != nil {
		return Message{}, Message{}, err
	}
	if !exists {
		return Message{}, Message{}, ErrNotFound
	}
	attachmentReferences, err := assets.ResolveReferences(ctx, tx, projectID, attachmentIDs, assets.PurposeAssistantAttachment)
	if err != nil {
		return Message{}, Message{}, err
	}
	userMetadata := map[string]any{"attachmentIds": attachmentIDs, "attachments": attachmentReferences}
	userMessage, err := insertMessage(ctx, tx, threadID, projectID, "user", userBody, "", nil, userMetadata, &userID)
	if err != nil {
		return Message{}, Message{}, err
	}
	if err := assets.LinkAssistantMessage(ctx, tx, projectID, userMessage.ID, attachmentReferences); err != nil {
		return Message{}, Message{}, err
	}
	userMessage.Attachments = attachmentReferences
	assistantMessage, err := insertMessage(ctx, tx, threadID, projectID, "assistant", assistantBody, actionType, entityID, metadata, nil)
	if err != nil {
		return Message{}, Message{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE assistant_threads SET updated_at = now() WHERE id = $1::uuid`, threadID); err != nil {
		return Message{}, Message{}, err
	}
	if err := insertAudit(ctx, tx, projectID, userID, "assistant.message_processed", "assistant_thread", &threadID, map[string]any{"action": actionType, "attachmentCount": len(attachmentReferences)}); err != nil {
		return Message{}, Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, Message{}, err
	}
	return userMessage, assistantMessage, nil
}

func (r *Repository) Attachments(ctx context.Context, projectID string, attachmentIDs []string) ([]assets.ContextAsset, error) {
	return assets.ResolveContext(ctx, r.pool, projectID, attachmentIDs)
}

func (r *Repository) Strategy(ctx context.Context, projectID string) (content.Strategy, error) {
	return content.NewRepository(r.pool).GetStrategy(ctx, projectID)
}

func (r *Repository) Context(ctx context.Context, projectID string) (WorkspaceContext, error) {
	var result WorkspaceContext
	err := r.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM content_items WHERE project_id = $1::uuid),
		  (SELECT count(*) FROM content_items WHERE project_id = $1::uuid AND status = 'draft'),
		  (SELECT count(*) FROM content_items WHERE project_id = $1::uuid AND status = 'in_review'),
		  (SELECT count(*) FROM content_items WHERE project_id = $1::uuid AND status = 'scheduled'),
		  (SELECT count(*) FROM calendar_items WHERE project_id = $1::uuid AND scheduled_for BETWEEN now() AND now() + interval '14 days'),
		  (SELECT count(*) FROM audience_contacts WHERE project_id = $1::uuid),
		  (SELECT count(*) FROM audience_contacts WHERE project_id = $1::uuid AND status = 'active'),
		  (SELECT count(*) FROM automation_rules WHERE project_id = $1::uuid AND enabled)`, projectID).Scan(
		&result.ContentTotal, &result.Drafts, &result.InReview, &result.Scheduled,
		&result.CalendarNext14, &result.Contacts, &result.ActiveContacts, &result.EnabledRules)
	return result, err
}

func (r *Repository) UpcomingCalendar(ctx context.Context, projectID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		WITH project_zone AS (
			SELECT COALESCE(
				(SELECT timezone FROM project_profiles WHERE project_id = $1::uuid),
				'Europe/Zagreb'
			) AS timezone
		)
		SELECT item.title || ' · ' || item.channel || ' · ' ||
		       to_char(item.scheduled_for AT TIME ZONE project_zone.timezone, 'DD.MM. HH24:MI')
		FROM calendar_items AS item
		CROSS JOIN project_zone
		WHERE item.project_id = $1::uuid AND item.scheduled_for >= now()
		ORDER BY item.scheduled_for LIMIT 6`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func (r *Repository) CreateDraft(ctx context.Context, projectID, userID, kind, title, body string, strategyRevision int) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var defaultLocale string
	if err := tx.QueryRow(ctx, `SELECT default_locale FROM projects WHERE id = $1::uuid`, projectID).Scan(&defaultLocale); err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO content_items (project_id, author_id, kind, status, title, summary, body, channels, source, metadata)
		VALUES ($1::uuid, $2::uuid, $3, 'draft', $4, $5, $6,
			CASE $3 WHEN 'social' THEN ARRAY['linkedin'] WHEN 'blog' THEN ARRAY['blog'] WHEN 'newsletter' THEN ARRAY['newsletter'] ELSE ARRAY[]::text[] END,
			'ai', jsonb_build_object('assistantCreated', true, 'strategyRevision', $7::int))
		RETURNING id::text`, projectID, userID, kind, title, shorten(body, 240), body, strategyRevision).Scan(&id)
	if err != nil {
		return "", err
	}
	channel := ""
	switch kind {
	case "social":
		channel = "linkedin"
	case "blog", "newsletter":
		channel = kind
	}
	if channel != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO content_variants (
				content_item_id, channel, locale, title, summary, body, status, metadata
			)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, 'draft',
			        jsonb_build_object('assistantCreated', true, 'strategyRevision', $7::int))
			ON CONFLICT (content_item_id, channel, locale) DO NOTHING`,
			id, channel, defaultLocale, title, shorten(body, 240), body, strategyRevision); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (r *Repository) ToggleRule(ctx context.Context, projectID, ruleKey string, enabled bool) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	var features map[string]any
	err = tx.QueryRow(ctx, `
		SELECT status, features
		FROM project_entitlements
		WHERE project_id = $1::uuid
		FOR UPDATE`, projectID).Scan(&status, &features)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAutomationUnavailable
	}
	if err != nil {
		return "", err
	}
	if !automationFeatureEnabled(status, features) {
		return "", ErrAutomationUnavailable
	}
	var id string
	err = tx.QueryRow(ctx, `
		UPDATE automation_rules
		SET enabled = $3, updated_at = now()
		WHERE project_id = $1::uuid AND rule_key = $2
		RETURNING id::text`, projectID, ruleKey, enabled).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (r *Repository) AutomationFeatureEnabled(ctx context.Context, projectID string) (bool, error) {
	var status string
	var features map[string]any
	err := r.pool.QueryRow(ctx, `
		SELECT status, features
		FROM project_entitlements
		WHERE project_id = $1::uuid`, projectID).Scan(&status, &features)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return automationFeatureEnabled(status, features), nil
}

func automationFeatureEnabled(status string, features map[string]any) bool {
	if status != "active" && status != "trial" {
		return false
	}
	enabled, ok := features["automations"].(bool)
	return ok && enabled
}

func insertMessage(ctx context.Context, tx pgx.Tx, threadID, projectID, role, body, actionType string, entityID *string, metadata map[string]any, createdBy *string) (Message, error) {
	var item Message
	err := tx.QueryRow(ctx, `
		INSERT INTO assistant_messages (
			thread_id, project_id, role, body, action_type, action_entity_id, metadata, created_by
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::uuid, $7::jsonb, $8::uuid)
		RETURNING id::text, thread_id::text, project_id::text, role, body,
		          action_type, action_entity_id::text, metadata, created_by::text, created_at`,
		threadID, projectID, role, body, actionType, entityID, metadata, createdBy).Scan(
		&item.ID, &item.ThreadID, &item.ProjectID, &item.Role, &item.Body,
		&item.ActionType, &item.ActionEntityID, &item.Metadata, &item.CreatedBy, &item.CreatedAt)
	return item, err
}

func insertAudit(ctx context.Context, tx pgx.Tx, projectID, userID, action, entityType string, entityID *string, metadata map[string]any) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6::jsonb)`,
		projectID, userID, action, entityType, entityID, metadata)
	return err
}

type rowScanner interface{ Scan(...any) error }

func scanMessage(row rowScanner) (Message, error) {
	var item Message
	err := row.Scan(&item.ID, &item.ThreadID, &item.ProjectID, &item.Role, &item.Body,
		&item.ActionType, &item.ActionEntityID, &item.Metadata, &item.CreatedBy, &item.CreatedAt)
	item.Attachments = []assets.Reference{}
	return item, err
}

func shorten(value string, limit int) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
