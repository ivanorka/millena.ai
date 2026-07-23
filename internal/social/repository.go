package social

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/assets"
	"github.com/ivanorka/millena-ai/internal/limits"
)

var ErrNotFound = errors.New("social resource not found")
var ErrInvalidConnections = errors.New("one or more social connections are invalid")
var ErrSocialChannelLimitReached = errors.New("social channel limit has been reached")
var ErrSocialChannelsUnavailable = errors.New("social channels are not available in the project entitlement")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

func (r *Repository) ListConnections(ctx context.Context, projectID string) ([]Connection, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, project_id::text, provider, mode, account_handle, display_name,
		       status, metadata, last_checked_at, created_at, updated_at
		FROM social_connections
		WHERE project_id = $1::uuid AND status <> 'disconnected'
		ORDER BY provider`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	connections := make([]Connection, 0)
	for rows.Next() {
		connection, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func (r *Repository) UpsertConnection(ctx context.Context, projectID string, input ConnectInput) (Connection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Connection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var entitlementStatus string
	var rawChannelLimit *string
	err = tx.QueryRow(ctx, `
		SELECT status, features->>'socialChannels'
		FROM project_entitlements
		WHERE project_id = $1::uuid
		FOR UPDATE`, projectID).Scan(&entitlementStatus, &rawChannelLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, limits.ErrEntitlementInactive
	}
	if err != nil {
		return Connection{}, err
	}
	if entitlementStatus != "active" && entitlementStatus != "trial" {
		return Connection{}, limits.ErrEntitlementInactive
	}
	channelLimit, unlimited, err := parseSocialChannelLimit(rawChannelLimit)
	if err != nil {
		return Connection{}, err
	}
	if !unlimited {
		var otherActiveConnections int
		var replacingActiveProvider bool
		err = tx.QueryRow(ctx, `
			SELECT count(*) FILTER (
			         WHERE provider <> $2 AND status <> 'disconnected'
			       )::int,
			       COALESCE(bool_or(provider = $2 AND status <> 'disconnected'), false)
			FROM social_connections
			WHERE project_id = $1::uuid`, projectID, input.Provider).Scan(
			&otherActiveConnections, &replacingActiveProvider,
		)
		if err != nil {
			return Connection{}, err
		}
		// Updating an already-active provider does not consume another slot and
		// remains possible if an administrator subsequently lowers the plan.
		if !replacingActiveProvider && otherActiveConnections >= channelLimit {
			return Connection{}, ErrSocialChannelLimitReached
		}
	}

	connection, err := scanConnection(tx.QueryRow(ctx, `
		INSERT INTO social_connections (project_id, provider, mode, account_handle, display_name, status, last_checked_at)
		VALUES ($1::uuid, $2, $3, $4, $5, 'connected', now())
		ON CONFLICT (project_id, provider) DO UPDATE
		SET mode = EXCLUDED.mode,
		    account_handle = EXCLUDED.account_handle,
		    display_name = EXCLUDED.display_name,
		    status = 'connected',
		    last_checked_at = now(),
		    updated_at = now()
		RETURNING id::text, project_id::text, provider, mode, account_handle, display_name,
		          status, metadata, last_checked_at, created_at, updated_at`,
		projectID, input.Provider, input.Mode, input.AccountHandle, input.DisplayName))
	if err != nil {
		return Connection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Connection{}, err
	}
	return connection, nil
}

func parseSocialChannelLimit(value *string) (limit int, unlimited bool, err error) {
	if value == nil {
		return 0, false, ErrSocialChannelsUnavailable
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	if normalized == "all" {
		return 0, true, nil
	}
	limit, err = strconv.Atoi(normalized)
	if err != nil || limit < 0 {
		return 0, false, ErrSocialChannelsUnavailable
	}
	return limit, false, nil
}

func (r *Repository) TestConnection(ctx context.Context, projectID, connectionID string) (Connection, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE social_connections
		SET status = 'connected', last_checked_at = now(), updated_at = now()
		WHERE id = $1::uuid AND project_id = $2::uuid AND status <> 'disconnected'
		RETURNING id::text, project_id::text, provider, mode, account_handle, display_name,
		          status, metadata, last_checked_at, created_at, updated_at`, connectionID, projectID)
	connection, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return connection, err
}

func (r *Repository) Disconnect(ctx context.Context, projectID, connectionID string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE social_connections
		SET status = 'disconnected', updated_at = now()
		WHERE id = $1::uuid AND project_id = $2::uuid AND status <> 'disconnected'`, connectionID, projectID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) CreatePost(ctx context.Context, projectID string, input CreatePostInput) (Post, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	publicationCapacity, err := limits.LockPublicationCapacity(ctx, tx, projectID)
	if err != nil {
		return Post{}, err
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, provider
		FROM social_connections
		WHERE project_id = $1::uuid AND status = 'connected' AND id::text = ANY($2::text[])
		ORDER BY provider`, projectID, input.ConnectionIDs)
	if err != nil {
		return Post{}, err
	}
	type selectedConnection struct{ id, provider string }
	selected := make([]selectedConnection, 0, len(input.ConnectionIDs))
	for rows.Next() {
		var item selectedConnection
		if err := rows.Scan(&item.id, &item.provider); err != nil {
			rows.Close()
			return Post{}, err
		}
		selected = append(selected, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Post{}, err
	}
	rows.Close()
	if len(selected) != len(input.ConnectionIDs) {
		return Post{}, ErrInvalidConnections
	}
	assetReferences, err := assets.ResolveReferences(ctx, tx, projectID, input.AssetIDs, assets.PurposeSocialMedia)
	if err != nil {
		return Post{}, err
	}

	status := "published"
	if input.ScheduledFor != nil {
		status = "scheduled"
	}
	var post Post
	err = tx.QueryRow(ctx, `
		INSERT INTO social_posts (project_id, body, status, scheduled_for)
		VALUES ($1::uuid, $2, $3, $4)
		RETURNING id::text, project_id::text, content_item_id::text,
		          content_variant_id::text, body, status, scheduled_for, created_at, updated_at`,
		projectID, input.Body, status, input.ScheduledFor).Scan(
		&post.ID, &post.ProjectID, &post.ContentItemID, &post.ContentVariantID,
		&post.Body, &post.Status, &post.ScheduledFor, &post.CreatedAt, &post.UpdatedAt)
	if err != nil {
		return Post{}, err
	}
	if err := publicationCapacity.Consume(ctx, tx, projectID, limits.PublicationSourceSocialPost, post.ID); err != nil {
		return Post{}, err
	}
	if err := assets.LinkSocialPost(ctx, tx, projectID, post.ID, assetReferences); err != nil {
		return Post{}, err
	}
	post.Assets = assetReferences

	post.Publications = make([]Publication, 0, len(selected))
	for _, connection := range selected {
		externalReference := fmt.Sprintf("sandbox://%s/%s", connection.provider, post.ID)
		var publication Publication
		err = tx.QueryRow(ctx, `
			INSERT INTO social_publications (
				social_post_id, social_connection_id, provider, status, external_reference, published_at
			)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, CASE WHEN $4 = 'published' THEN now() ELSE NULL END)
			RETURNING id::text, social_post_id::text, social_connection_id::text, provider,
			          status, external_reference, published_at, last_error, created_at, updated_at`,
			post.ID, connection.id, connection.provider, status, externalReference).Scan(
			&publication.ID,
			&publication.SocialPostID,
			&publication.SocialConnectionID,
			&publication.Provider,
			&publication.Status,
			&publication.ExternalReference,
			&publication.PublishedAt,
			&publication.LastError,
			&publication.CreatedAt,
			&publication.UpdatedAt,
		)
		if err != nil {
			return Post{}, err
		}
		post.Publications = append(post.Publications, publication)
	}

	if err := tx.Commit(ctx); err != nil {
		return Post{}, err
	}
	return post, nil
}

func (r *Repository) ListPosts(ctx context.Context, projectID string) ([]Post, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, project_id::text, content_item_id::text,
		       content_variant_id::text, body, status, scheduled_for, created_at, updated_at
		FROM social_posts
		WHERE project_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 20`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	posts := make([]Post, 0)
	for rows.Next() {
		var post Post
		if err := rows.Scan(
			&post.ID, &post.ProjectID, &post.ContentItemID, &post.ContentVariantID,
			&post.Body, &post.Status, &post.ScheduledFor, &post.CreatedAt, &post.UpdatedAt,
		); err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for index := range posts {
		publications, err := r.listPublications(ctx, posts[index].ID)
		if err != nil {
			return nil, err
		}
		posts[index].Publications = publications
		assetReferences, err := assets.ListSocialPostReferences(ctx, r.pool, projectID, posts[index].ID)
		if err != nil {
			return nil, err
		}
		posts[index].Assets = assetReferences
	}
	return posts, nil
}

// RecordPostNotification keeps the transactional publication data independent
// from the notification outbox. An audit event is enough for the database
// trigger to fan out a delivery to the relevant project members.
func (r *Repository) RecordPostNotification(ctx context.Context, projectID, userID string, post Post) error {
	if post.Status != "published" && post.Status != "scheduled" {
		return nil
	}
	action := "social_post.scheduled"
	summary := "Objava je zakazana za odabrane kanale."
	if post.Status == "published" {
		action = "publication_job.succeeded"
		summary = "Objava je odmah uspješno poslana na odabrane kanale."
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, NULLIF($2, '')::uuid, $3, 'social_post', $4::uuid,
		        jsonb_build_object('summary', $5::text, 'status', $6::text))`,
		projectID, userID, action, post.ID, summary, post.Status)
	return err
}

func (r *Repository) PublishDueSandbox(ctx context.Context, limit int) (int64, error) {
	if limit < 1 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
	SELECT post.id::text, post.project_id::text
		FROM social_posts AS post
		JOIN projects AS project
		  ON project.id = post.project_id AND project.status = 'active'
		JOIN project_entitlements AS entitlement
		  ON entitlement.project_id = post.project_id
		 AND entitlement.status IN ('trial', 'active')
		WHERE post.status = 'scheduled' AND post.scheduled_for <= now()
		ORDER BY post.scheduled_for
		FOR UPDATE OF post SKIP LOCKED
		LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	postIDs := make([]string, 0, limit)
	projectIDs := make(map[string]string, limit)
	for rows.Next() {
		var postID, projectID string
		if err := rows.Scan(&postID, &projectID); err != nil {
			rows.Close()
			return 0, err
		}
		postIDs = append(postIDs, postID)
		projectIDs[postID] = projectID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	if len(postIDs) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}
	_, err = tx.Exec(ctx, `
		UPDATE social_publications
		SET status = 'published', published_at = now(), updated_at = now()
		WHERE social_post_id::text = ANY($1::text[]) AND status = 'scheduled'`, postIDs)
	if err != nil {
		return 0, err
	}
	result, err := tx.Exec(ctx, `
		UPDATE social_posts
		SET status = 'published', updated_at = now()
		WHERE id::text = ANY($1::text[]) AND status = 'scheduled'`, postIDs)
	if err != nil {
		return 0, err
	}
	for _, postID := range postIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
			VALUES ($1::uuid, NULL, 'publication_job.succeeded', 'social_post', $2::uuid,
			        jsonb_build_object('summary', 'Zakazana objava uspješno je poslana na odabrane kanale.', 'status', 'published'))`,
			projectIDs[postID], postID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) listPublications(ctx context.Context, postID string) ([]Publication, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, social_post_id::text, social_connection_id::text, provider,
		       status, external_reference, published_at, last_error, created_at, updated_at
		FROM social_publications
		WHERE social_post_id = $1::uuid
		ORDER BY provider`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	publications := make([]Publication, 0)
	for rows.Next() {
		var publication Publication
		if err := rows.Scan(
			&publication.ID,
			&publication.SocialPostID,
			&publication.SocialConnectionID,
			&publication.Provider,
			&publication.Status,
			&publication.ExternalReference,
			&publication.PublishedAt,
			&publication.LastError,
			&publication.CreatedAt,
			&publication.UpdatedAt,
		); err != nil {
			return nil, err
		}
		publications = append(publications, publication)
	}
	return publications, rows.Err()
}

type connectionScanner interface {
	Scan(dest ...any) error
}

func scanConnection(scanner connectionScanner) (Connection, error) {
	var connection Connection
	err := scanner.Scan(
		&connection.ID,
		&connection.ProjectID,
		&connection.Provider,
		&connection.Mode,
		&connection.AccountHandle,
		&connection.DisplayName,
		&connection.Status,
		&connection.Metadata,
		&connection.LastCheckedAt,
		&connection.CreatedAt,
		&connection.UpdatedAt,
	)
	return connection, err
}
