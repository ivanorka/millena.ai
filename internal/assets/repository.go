package assets

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound            = errors.New("project asset not found")
	ErrInvalidReferences   = errors.New("one or more project assets are invalid")
	ErrEntitlementInactive = errors.New("project entitlement is missing or inactive")
	ErrStorageLimitReached = errors.New("project storage limit has been reached")
	ErrAssetInUse          = errors.New("asset purpose cannot change while the asset is linked")
	ErrInvalidMediaPurpose = errors.New("social media assets must be images or videos")
)

var canonicalUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context, projectID, purpose string) ([]Asset, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, project_id::text, uploaded_by::text, purpose, filename,
		       mime_type, size_bytes, sha256, extracted_text IS NOT NULL,
		       created_at, updated_at
		FROM project_assets
		WHERE project_id = $1::uuid AND ($2 = '' OR purpose = $2)
		ORDER BY created_at DESC
		LIMIT 200`, projectID, purpose)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Asset, 0)
	for rows.Next() {
		item, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, projectID, assetID string) (Asset, error) {
	item, err := scanAsset(r.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, uploaded_by::text, purpose, filename,
		       mime_type, size_bytes, sha256, extracted_text IS NOT NULL,
		       created_at, updated_at
		FROM project_assets
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, assetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) Create(ctx context.Context, projectID, userID string, input UploadInput) (Asset, error) {
	if input.Purpose == PurposeSocialMedia && !isSocialMediaType(input.MIMEType) {
		return Asset{}, ErrInvalidMediaPurpose
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requireStorageCapacity(ctx, tx, projectID, int64(len(input.Data))); err != nil {
		return Asset{}, err
	}
	item, err := scanAsset(tx.QueryRow(ctx, `
		INSERT INTO project_assets (
			project_id, uploaded_by, purpose, filename, mime_type, size_bytes,
			sha256, data, extracted_text
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, project_id::text, uploaded_by::text, purpose, filename,
		          mime_type, size_bytes, sha256, extracted_text IS NOT NULL,
		          created_at, updated_at`, projectID, userID, input.Purpose,
		input.Filename, input.MIMEType, len(input.Data), input.SHA256[:], input.Data,
		input.ExtractedText))
	if err != nil {
		return Asset{}, err
	}
	if err := recordAudit(ctx, tx, projectID, userID, "asset.created", item.ID, map[string]any{
		"purpose": item.Purpose, "filename": item.Filename,
		"mimeType": item.MIMEType, "sizeBytes": item.SizeBytes, "sha256": item.SHA256,
	}); err != nil {
		return Asset{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) Update(ctx context.Context, projectID, assetID, userID string, input UpdateInput) (Asset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var previousPurpose, mimeType string
	if err := tx.QueryRow(ctx, `
		SELECT purpose, mime_type FROM project_assets
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, assetID).Scan(&previousPurpose, &mimeType); errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	} else if err != nil {
		return Asset{}, err
	}
	if input.Purpose == PurposeSocialMedia && !isSocialMediaType(mimeType) {
		return Asset{}, ErrInvalidMediaPurpose
	}
	if previousPurpose != input.Purpose {
		var linked bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM assistant_message_assets WHERE project_id = $1::uuid AND asset_id = $2::uuid)
			    OR EXISTS (SELECT 1 FROM social_post_assets WHERE project_id = $1::uuid AND asset_id = $2::uuid)
			    OR EXISTS (SELECT 1 FROM content_item_assets WHERE project_id = $1::uuid AND asset_id = $2::uuid)`,
			projectID, assetID).Scan(&linked); err != nil {
			return Asset{}, err
		}
		if linked {
			return Asset{}, ErrAssetInUse
		}
	}
	item, err := scanAsset(tx.QueryRow(ctx, `
		UPDATE project_assets
		SET purpose = $3, filename = $4, updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING id::text, project_id::text, uploaded_by::text, purpose, filename,
		          mime_type, size_bytes, sha256, extracted_text IS NOT NULL,
		          created_at, updated_at`, projectID, assetID, input.Purpose, input.Filename))
	if err != nil {
		return Asset{}, err
	}
	if err := recordAudit(ctx, tx, projectID, userID, "asset.updated", item.ID, map[string]any{
		"previousPurpose": previousPurpose, "purpose": item.Purpose, "filename": item.Filename,
	}); err != nil {
		return Asset{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) Delete(ctx context.Context, projectID, assetID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var filename, purpose string
	if err := tx.QueryRow(ctx, `
		SELECT filename, purpose
		FROM project_assets
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, assetID).Scan(&filename, &purpose); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	cleanedContentItems, err := cleanContentAssetMetadata(ctx, tx, projectID, assetID)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
		DELETE FROM project_assets
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, assetID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	// The deleted row is intentionally not retained; audit metadata contains
	// only identifiers and never the binary payload.
	if err := recordAudit(ctx, tx, projectID, userID, "asset.deleted", assetID, map[string]any{
		"filename": filename, "purpose": purpose, "contentItemsCleaned": cleanedContentItems,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// cleanContentAssetMetadata keeps the compatibility fields in
// content_items.metadata aligned with content_item_assets before the asset FK
// cascades its normalized links. The asset row is already locked by Delete;
// content reference validation takes a conflicting share lock, so a concurrent
// content update cannot recreate a dangling reference around this cleanup.
func cleanContentAssetMetadata(ctx context.Context, tx pgx.Tx, projectID, assetID string) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT item.id::text
		FROM content_items AS item
		JOIN content_item_assets AS link
		  ON link.project_id = item.project_id AND link.content_item_id = item.id
		WHERE item.project_id = $1::uuid AND link.asset_id = $2::uuid
		FOR UPDATE OF item`, projectID, assetID)
	if err != nil {
		return 0, err
	}
	itemIDSet := make(map[string]struct{})
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			rows.Close()
			return 0, err
		}
		itemIDSet[itemID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(itemIDSet) == 0 {
		return 0, nil
	}
	itemIDs := make([]string, 0, len(itemIDSet))
	for itemID := range itemIDSet {
		itemIDs = append(itemIDs, itemID)
	}
	command, err := tx.Exec(ctx, `
		WITH cleaned AS (
			SELECT item.id,
			       CASE
			         WHEN jsonb_typeof(item.metadata -> 'assetIds') = 'array' THEN
			           jsonb_set(
			             item.metadata,
			             '{assetIds}',
			             COALESCE((
			               SELECT jsonb_agg(entry.value ORDER BY entry.position)
			               FROM jsonb_array_elements(item.metadata -> 'assetIds')
			                    WITH ORDINALITY AS entry(value, position)
			               WHERE lower(btrim(entry.value #>> '{}')) <> lower($2)
			             ), '[]'::jsonb),
			             true
			           )
			         ELSE item.metadata
			       END AS metadata
			FROM content_items AS item
			WHERE item.project_id = $1::uuid AND item.id = ANY($3::uuid[])
		)
		UPDATE content_items AS item
		SET metadata = CASE
		                 WHEN lower(btrim(cleaned.metadata ->> 'coverAssetId')) = lower($2)
		                   THEN cleaned.metadata - 'coverAssetId'
		                 ELSE cleaned.metadata
		               END,
		    revision = revision + 1,
		    updated_at = now()
		FROM cleaned
		WHERE item.id = cleaned.id`, projectID, assetID, itemIDs)
	if err != nil {
		return 0, err
	}
	return int(command.RowsAffected()), nil
}

func (r *Repository) Download(ctx context.Context, projectID, assetID, userID string) (Blob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Blob{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var blob Blob
	var digest []byte
	err = tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, uploaded_by::text, purpose, filename,
		       mime_type, size_bytes, sha256, extracted_text IS NOT NULL,
		       created_at, updated_at, data
		FROM project_assets
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, assetID).Scan(
		&blob.ID, &blob.ProjectID, &blob.UploadedBy, &blob.Purpose, &blob.Filename,
		&blob.MIMEType, &blob.SizeBytes, &digest, &blob.HasExtractedText,
		&blob.CreatedAt, &blob.UpdatedAt, &blob.Data,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Blob{}, ErrNotFound
	}
	if err != nil {
		return Blob{}, err
	}
	blob.SHA256 = hex.EncodeToString(digest)
	if err := recordAudit(ctx, tx, projectID, userID, "asset.downloaded", assetID, map[string]any{
		"filename": blob.Filename, "purpose": blob.Purpose,
	}); err != nil {
		return Blob{}, err
	}
	return blob, tx.Commit(ctx)
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func ResolveReferences(ctx context.Context, query queryer, projectID string, assetIDs []string, purpose string) ([]Reference, error) {
	if len(assetIDs) == 0 {
		return []Reference{}, nil
	}
	rows, err := query.Query(ctx, `
		SELECT id::text, purpose, filename, mime_type, size_bytes, sha256,
		       extracted_text IS NOT NULL
		FROM project_assets
		WHERE project_id = $1::uuid AND purpose = $3
		  AND id::text = ANY($2::text[])`, projectID, assetIDs, purpose)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make(map[string]Reference, len(assetIDs))
	for rows.Next() {
		var item Reference
		var digest []byte
		if err := rows.Scan(&item.ID, &item.Purpose, &item.Filename, &item.MIMEType,
			&item.SizeBytes, &digest, &item.HasExtractedText); err != nil {
			return nil, err
		}
		item.SHA256 = hex.EncodeToString(digest)
		found[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]Reference, 0, len(assetIDs))
	for _, id := range assetIDs {
		item, ok := found[id]
		if !ok {
			return nil, ErrInvalidReferences
		}
		items = append(items, item)
	}
	return items, nil
}

func ResolveContext(ctx context.Context, query queryer, projectID string, assetIDs []string) ([]ContextAsset, error) {
	if len(assetIDs) == 0 {
		return []ContextAsset{}, nil
	}
	rows, err := query.Query(ctx, `
		SELECT id::text, purpose, filename, mime_type, size_bytes, sha256,
		       extracted_text IS NOT NULL, extracted_text
		FROM project_assets
		WHERE project_id = $1::uuid AND purpose = 'assistant_attachment'
		  AND id::text = ANY($2::text[])`, projectID, assetIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make(map[string]ContextAsset, len(assetIDs))
	for rows.Next() {
		var item ContextAsset
		var digest []byte
		if err := rows.Scan(&item.ID, &item.Purpose, &item.Filename, &item.MIMEType,
			&item.SizeBytes, &digest, &item.HasExtractedText, &item.ExtractedText); err != nil {
			return nil, err
		}
		item.SHA256 = hex.EncodeToString(digest)
		found[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]ContextAsset, 0, len(assetIDs))
	for _, id := range assetIDs {
		item, ok := found[id]
		if !ok {
			return nil, ErrInvalidReferences
		}
		items = append(items, item)
	}
	return items, nil
}

func LinkAssistantMessage(ctx context.Context, tx pgx.Tx, projectID, messageID string, items []Reference) error {
	for position, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO assistant_message_assets (project_id, message_id, asset_id, position)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4)
			ON CONFLICT (message_id, asset_id) DO NOTHING`,
			projectID, messageID, item.ID, position); err != nil {
			return err
		}
	}
	return nil
}

func LinkSocialPost(ctx context.Context, tx pgx.Tx, projectID, postID string, items []Reference) error {
	for position, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO social_post_assets (project_id, social_post_id, asset_id, position)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4)
			ON CONFLICT (social_post_id, asset_id) DO NOTHING`,
			projectID, postID, item.ID, position); err != nil {
			return err
		}
	}
	return nil
}

func ListAssistantMessageReferences(ctx context.Context, query queryer, projectID, messageID string) ([]Reference, error) {
	return listLinkedReferences(ctx, query, `
		SELECT asset.id::text, asset.purpose, asset.filename, asset.mime_type,
		       asset.size_bytes, asset.sha256, asset.extracted_text IS NOT NULL
		FROM assistant_message_assets AS link
		JOIN project_assets AS asset
		  ON asset.project_id = link.project_id AND asset.id = link.asset_id
		WHERE link.project_id = $1::uuid AND link.message_id = $2::uuid
		ORDER BY link.position`, projectID, messageID)
}

func ListSocialPostReferences(ctx context.Context, query queryer, projectID, postID string) ([]Reference, error) {
	return listLinkedReferences(ctx, query, `
		SELECT asset.id::text, asset.purpose, asset.filename, asset.mime_type,
		       asset.size_bytes, asset.sha256, asset.extracted_text IS NOT NULL
		FROM social_post_assets AS link
		JOIN project_assets AS asset
		  ON asset.project_id = link.project_id AND asset.id = link.asset_id
		WHERE link.project_id = $1::uuid AND link.social_post_id = $2::uuid
		ORDER BY link.position`, projectID, postID)
}

func listLinkedReferences(ctx context.Context, query queryer, statement, projectID, entityID string) ([]Reference, error) {
	rows, err := query.Query(ctx, statement, projectID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Reference, 0)
	for rows.Next() {
		var item Reference
		var digest []byte
		if err := rows.Scan(&item.ID, &item.Purpose, &item.Filename, &item.MIMEType,
			&item.SizeBytes, &digest, &item.HasExtractedText); err != nil {
			return nil, err
		}
		item.SHA256 = hex.EncodeToString(digest)
		items = append(items, item)
	}
	return items, rows.Err()
}

func NormalizeIDs(values []string, limit int) ([]string, error) {
	if len(values) > limit {
		return nil, ErrInvalidReferences
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !canonicalUUID.MatchString(value) {
			return nil, ErrInvalidReferences
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func isSocialMediaType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "image/") || strings.HasPrefix(value, "video/")
}

func requireStorageCapacity(ctx context.Context, tx pgx.Tx, projectID string, addedBytes int64) error {
	var limit *int64
	var status string
	err := tx.QueryRow(ctx, `
		SELECT entitlement.storage_limit_bytes, entitlement.status
		FROM projects AS project
		JOIN organization_entitlements AS entitlement
		  ON entitlement.organization_id=project.organization_id
		WHERE project.id=$1::uuid
		FOR UPDATE OF entitlement`, projectID).Scan(&limit, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEntitlementInactive
	}
	if err != nil {
		return err
	}
	if status != "active" && status != "trial" {
		return ErrEntitlementInactive
	}
	// Read usage in a second statement after the entitlement row lock is held.
	// At READ COMMITTED this gives waiters a fresh snapshot, so concurrent
	// uploads cannot both spend the same final bytes of a limited plan.
	var used int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(sum(asset.size_bytes),0)
		FROM project_assets AS asset
		JOIN projects AS stored_project ON stored_project.id=asset.project_id
		JOIN projects AS active_project
		  ON active_project.id=$1::uuid
		 AND active_project.organization_id=stored_project.organization_id`, projectID).Scan(&used); err != nil {
		return err
	}
	if limit != nil && used+addedBytes > *limit {
		return ErrStorageLimitReached
	}
	return nil
}

func recordAudit(ctx context.Context, tx pgx.Tx, projectID, userID, action, assetID string, metadata map[string]any) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, $2::uuid, $3, 'project_asset', $4::uuid, $5::jsonb)`,
		projectID, userID, action, assetID, metadata)
	return err
}

type scanner interface{ Scan(...any) error }

func scanAsset(row scanner) (Asset, error) {
	var item Asset
	var digest []byte
	err := row.Scan(&item.ID, &item.ProjectID, &item.UploadedBy, &item.Purpose,
		&item.Filename, &item.MIMEType, &item.SizeBytes, &digest,
		&item.HasExtractedText, &item.CreatedAt, &item.UpdatedAt)
	item.SHA256 = hex.EncodeToString(digest)
	return item, err
}
