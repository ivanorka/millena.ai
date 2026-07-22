package content

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/contentstate"
	"github.com/ivanorka/millena-ai/internal/limits"
)

var (
	ErrNotFound                          = errors.New("content record not found")
	ErrReviewNotPending                  = errors.New("content record is not awaiting review")
	ErrInvalidAssetReferences            = errors.New("content asset references are invalid")
	ErrInvalidNewsletterTarget           = errors.New("newsletter target is invalid")
	ErrNewsletterDeliveryVariantConflict = errors.New("another newsletter variant owns the active delivery")
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

func (r *Repository) List(ctx context.Context, projectID string, filter ListFilter) ([]Item, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, project_id::text, author_id::text, kind, status, title, summary,
		       body, channels, scheduled_for, source, revision, metadata, created_at, updated_at
		FROM content_items
		WHERE project_id = $1::uuid
		  AND ($2 = '' OR kind = $2)
		  AND ($3 = '' OR status = $3)
		  AND ($4 = '' OR title ILIKE '%' || $4 || '%' OR summary ILIKE '%' || $4 || '%' OR body ILIKE '%' || $4 || '%')
		ORDER BY updated_at DESC
		LIMIT 200`, projectID, filter.Kind, filter.Status, filter.Search)
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
		SELECT id::text, project_id::text, author_id::text, kind, status, title, summary,
		       body, channels, scheduled_for, source, revision, metadata, created_at, updated_at
		FROM content_items
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) Create(ctx context.Context, projectID, userID string, input SaveInput) (Item, error) {
	selection, metadata, err := normalizeContentAssetMetadata(input.Metadata)
	if err != nil {
		return Item{}, err
	}
	input.Metadata = metadata
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if defaultVariantsCanConsume(input) {
		if _, err := limits.LockPublicationCapacity(ctx, tx, projectID); err != nil {
			return Item{}, err
		}
	}
	if err := validateContentAssetReferences(ctx, tx, projectID, selection); err != nil {
		return Item{}, err
	}
	item, err := scanItem(tx.QueryRow(ctx, `
		INSERT INTO content_items (
			project_id, author_id, kind, status, title, summary, body, channels,
			scheduled_for, source, metadata
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id::text, project_id::text, author_id::text, kind, status, title, summary,
		          body, channels, scheduled_for, source, revision, metadata, created_at, updated_at`,
		projectID, userID, input.Kind, input.Status, input.Title, input.Summary,
		input.Body, input.Channels, input.ScheduledFor, input.Source, input.Metadata))
	if err != nil {
		return Item{}, err
	}
	if err := syncContentAssetLinks(ctx, tx, projectID, item.ID, selection); err != nil {
		return Item{}, err
	}
	if err := r.syncDefaultVariantsTx(ctx, tx, projectID, userID, item); err != nil {
		return Item{}, err
	}
	if err := syncBlogNewsletterRelation(ctx, tx, projectID, item.ID, "", nil, item.Kind, item.Metadata); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, err
	}
	return item, nil
}

func (r *Repository) Update(ctx context.Context, projectID, itemID, userID string, input SaveInput) (Item, error) {
	selection, metadata, err := normalizeContentAssetMetadata(input.Metadata)
	if err != nil {
		return Item{}, err
	}
	input.Metadata = metadata
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if defaultVariantsCanConsume(input) {
		if _, err := limits.LockPublicationCapacity(ctx, tx, projectID); err != nil {
			return Item{}, err
		}
	}
	// Asset rows are locked before the content row. Asset deletion uses the
	// same asset -> content lock order while cleaning compatibility metadata,
	// preventing an update/delete deadlock around a linked file.
	if err := validateContentAssetReferences(ctx, tx, projectID, selection); err != nil {
		return Item{}, err
	}
	var lockedItemID, previousKind string
	var previousMetadata map[string]any
	if err := tx.QueryRow(ctx, `
		SELECT id::text, kind, metadata
		FROM content_items
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, itemID).Scan(&lockedItemID, &previousKind, &previousMetadata); errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	} else if err != nil {
		return Item{}, err
	}
	item, err := scanItem(tx.QueryRow(ctx, `
		UPDATE content_items
		SET kind = $3, status = $4, title = $5, summary = $6, body = $7,
		    channels = $8, scheduled_for = $9, source = $10, metadata = $11,
		    revision = revision + 1, updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING id::text, project_id::text, author_id::text, kind, status, title, summary,
		          body, channels, scheduled_for, source, revision, metadata, created_at, updated_at`,
		projectID, itemID, input.Kind, input.Status, input.Title, input.Summary,
		input.Body, input.Channels, input.ScheduledFor, input.Source, input.Metadata))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	if err := syncContentAssetLinks(ctx, tx, projectID, item.ID, selection); err != nil {
		return Item{}, err
	}
	if err := r.syncDefaultVariantsTx(ctx, tx, projectID, userID, item); err != nil {
		return Item{}, err
	}
	if err := syncBlogNewsletterRelation(ctx, tx, projectID, item.ID, previousKind, previousMetadata, item.Kind, item.Metadata); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, err
	}
	return item, nil
}

func (r *Repository) ApproveReview(ctx context.Context, projectID, itemID, reviewerID, reviewerName string) (Item, error) {
	item, err := scanItem(r.pool.QueryRow(ctx, `
		UPDATE content_items
		SET status = 'approved',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'reviewedBy', $3,
		      'reviewedByName', $4,
		      'reviewedAt', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
		    ),
		    revision = revision + 1,
		    updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid AND status = 'in_review'
		RETURNING id::text, project_id::text, author_id::text, kind, status, title, summary,
		          body, channels, scheduled_for, source, revision, metadata, created_at, updated_at`,
		projectID, itemID, reviewerID, reviewerName))
	if !errors.Is(err, pgx.ErrNoRows) {
		return item, err
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM content_items WHERE project_id = $1::uuid AND id = $2::uuid)`, projectID, itemID).Scan(&exists); err != nil {
		return Item{}, err
	}
	if !exists {
		return Item{}, ErrNotFound
	}
	return Item{}, ErrReviewNotPending
}

func (r *Repository) ReturnForRevision(ctx context.Context, projectID, itemID, reviewerID, reviewerName, comment string) (Item, error) {
	item, err := scanItem(r.pool.QueryRow(ctx, `
		UPDATE content_items
		SET status = 'draft',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'reviewDecision', 'revision_requested',
		      'reviewedBy', $3,
		      'reviewedByName', $4,
		      'reviewedAt', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),
		      'reviewComment', $5
		    ),
		    revision = revision + 1,
		    updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid AND status = 'in_review'
		RETURNING id::text, project_id::text, author_id::text, kind, status, title, summary,
		          body, channels, scheduled_for, source, revision, metadata, created_at, updated_at`,
		projectID, itemID, reviewerID, reviewerName, comment))
	if !errors.Is(err, pgx.ErrNoRows) {
		return item, err
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM content_items WHERE project_id = $1::uuid AND id = $2::uuid)`, projectID, itemID).Scan(&exists); err != nil {
		return Item{}, err
	}
	if !exists {
		return Item{}, ErrNotFound
	}
	return Item{}, ErrReviewNotPending
}

func defaultVariantsCanConsume(input SaveInput) bool {
	if input.Status != "scheduled" && input.Status != "published" {
		return false
	}
	for _, channel := range input.Channels {
		if supportedVariantChannels[channel] {
			return true
		}
	}
	return false
}

const maxContentAssetReferences = 50

var canonicalAssetUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type contentAssetSelection struct {
	AssetIDs     []string
	CoverAssetID string
	AllIDs       []string
}

func normalizeContentAssetMetadata(metadata map[string]any) (contentAssetSelection, map[string]any, error) {
	normalized := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		normalized[key] = value
	}

	rawAssetIDs, hasAssetIDs := normalized["assetIds"]
	assetIDValues := make([]string, 0)
	if hasAssetIDs && rawAssetIDs != nil {
		switch values := rawAssetIDs.(type) {
		case []string:
			assetIDValues = append(assetIDValues, values...)
		case []any:
			for _, value := range values {
				id, ok := value.(string)
				if !ok {
					return contentAssetSelection{}, nil, ErrInvalidAssetReferences
				}
				assetIDValues = append(assetIDValues, id)
			}
		default:
			return contentAssetSelection{}, nil, ErrInvalidAssetReferences
		}
	}
	assetIDs, err := normalizeContentAssetIDs(assetIDValues, maxContentAssetReferences)
	if err != nil {
		return contentAssetSelection{}, nil, ErrInvalidAssetReferences
	}
	normalized["assetIds"] = assetIDs

	coverAssetID := ""
	if rawCoverAssetID, ok := normalized["coverAssetId"]; ok && rawCoverAssetID != nil {
		value, ok := rawCoverAssetID.(string)
		if !ok {
			return contentAssetSelection{}, nil, ErrInvalidAssetReferences
		}
		value = strings.TrimSpace(value)
		if value != "" {
			coverIDs, err := normalizeContentAssetIDs([]string{value}, 1)
			if err != nil {
				return contentAssetSelection{}, nil, ErrInvalidAssetReferences
			}
			coverAssetID = coverIDs[0]
			normalized["coverAssetId"] = coverAssetID
		} else {
			normalized["coverAssetId"] = nil
		}
	}

	allIDs := append([]string(nil), assetIDs...)
	if coverAssetID != "" {
		found := false
		for _, assetID := range allIDs {
			if assetID == coverAssetID {
				found = true
				break
			}
		}
		if !found {
			allIDs = append(allIDs, coverAssetID)
		}
	}
	if len(allIDs) > maxContentAssetReferences {
		return contentAssetSelection{}, nil, ErrInvalidAssetReferences
	}
	return contentAssetSelection{AssetIDs: assetIDs, CoverAssetID: coverAssetID, AllIDs: allIDs}, normalized, nil
}

func validateContentAssetReferences(ctx context.Context, tx pgx.Tx, projectID string, selection contentAssetSelection) error {
	if len(selection.AllIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM project_assets
		WHERE project_id = $1::uuid
		  AND purpose = 'content_media'
		  AND id::text = ANY($2::text[])
		ORDER BY id
		FOR SHARE`, projectID, selection.AllIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(selection.AllIDs) {
		return ErrInvalidAssetReferences
	}
	return nil
}

func normalizeContentAssetIDs(values []string, limit int) ([]string, error) {
	if len(values) > limit {
		return nil, ErrInvalidAssetReferences
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !canonicalAssetUUID.MatchString(value) {
			return nil, ErrInvalidAssetReferences
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func syncContentAssetLinks(ctx context.Context, tx pgx.Tx, projectID, itemID string, selection contentAssetSelection) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM content_item_assets
		WHERE project_id = $1::uuid AND content_item_id = $2::uuid`, projectID, itemID); err != nil {
		return err
	}
	for position, assetID := range selection.AssetIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO content_item_assets (
				project_id, content_item_id, asset_id, use_type, position, metadata
			)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'attachment', $4,
				'{"metadataKey":"assetIds"}'::jsonb)`, projectID, itemID, assetID, position); err != nil {
			return err
		}
	}
	if selection.CoverAssetID != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO content_item_assets (
				project_id, content_item_id, asset_id, use_type, position, metadata
			)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'cover', 0,
				'{"metadataKey":"coverAssetId"}'::jsonb)`, projectID, itemID, selection.CoverAssetID); err != nil {
			return err
		}
	}
	return nil
}

// syncBlogNewsletterRelation owns the bidirectional blog -> newsletter link.
// Keeping the blog metadata and the newsletter block list in the same
// transaction prevents the UI from ever persisting only half of a relation.
func syncBlogNewsletterRelation(
	ctx context.Context,
	tx pgx.Tx,
	projectID, itemID, previousKind string,
	previousMetadata map[string]any,
	newKind string,
	newMetadata map[string]any,
) error {
	previousTarget := ""
	if previousKind == "blog" {
		previousTarget = validNewsletterTargetID(previousMetadata)
	}
	newTarget := ""
	if newKind == "blog" {
		addNewsletter, _ := newMetadata["addNewsletter"].(bool)
		if addNewsletter {
			rawTarget, ok := newMetadata["newsletterTargetId"].(string)
			if !ok || !canonicalAssetUUID.MatchString(strings.ToLower(strings.TrimSpace(rawTarget))) {
				return ErrInvalidNewsletterTarget
			}
			newTarget = strings.ToLower(strings.TrimSpace(rawTarget))
		}
	}

	targets := make([]string, 0, 2)
	if previousTarget != "" {
		targets = append(targets, previousTarget)
	}
	if newTarget != "" && newTarget != previousTarget {
		targets = append(targets, newTarget)
	}
	if len(targets) > 0 {
		rows, err := tx.Query(ctx, `
			SELECT id::text, kind
			FROM content_items
			WHERE project_id = $1::uuid AND id::text = ANY($2::text[])
			ORDER BY id
			FOR UPDATE`, projectID, targets)
		if err != nil {
			return err
		}
		foundNewTarget := false
		for rows.Next() {
			var targetID, targetKind string
			if err := rows.Scan(&targetID, &targetKind); err != nil {
				rows.Close()
				return err
			}
			if targetID == newTarget && targetKind == "newsletter" {
				foundNewTarget = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if newTarget != "" && !foundNewTarget {
			return ErrInvalidNewsletterTarget
		}
	}

	if previousTarget != "" && previousTarget != newTarget {
		if err := removeNewsletterBlock(ctx, tx, projectID, previousTarget, itemID); err != nil {
			return err
		}
	}
	if newTarget != "" {
		if err := addNewsletterBlock(ctx, tx, projectID, newTarget, itemID); err != nil {
			return err
		}
	}
	return nil
}

func validNewsletterTargetID(metadata map[string]any) string {
	rawTarget, _ := metadata["newsletterTargetId"].(string)
	target := strings.ToLower(strings.TrimSpace(rawTarget))
	if !canonicalAssetUUID.MatchString(target) {
		return ""
	}
	return target
}

func newsletterBlocksExpression() string {
	return `CASE WHEN jsonb_typeof(metadata->'blocks') = 'array'
	             THEN metadata->'blocks' ELSE '[]'::jsonb END`
}

func addNewsletterBlock(ctx context.Context, tx pgx.Tx, projectID, newsletterID, itemID string) error {
	blocks := newsletterBlocksExpression()
	command, err := tx.Exec(ctx, `
		UPDATE content_items
		SET metadata = jsonb_set(
		      metadata,
		      '{blocks}',
		      CASE WHEN `+blocks+` @> jsonb_build_array($3::text)
		           THEN `+blocks+`
		           ELSE `+blocks+` || jsonb_build_array($3::text) END,
		      true
		    ),
		    revision = revision + 1,
		    updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid AND kind = 'newsletter'`,
		projectID, newsletterID, itemID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrInvalidNewsletterTarget
	}
	return nil
}

func removeNewsletterBlock(ctx context.Context, tx pgx.Tx, projectID, newsletterID, itemID string) error {
	blocks := newsletterBlocksExpression()
	_, err := tx.Exec(ctx, `
		UPDATE content_items
		SET metadata = jsonb_set(
		      metadata,
		      '{blocks}',
		      COALESCE((
		        SELECT jsonb_agg(entry.value)
		        FROM jsonb_array_elements(`+blocks+`) AS entry(value)
		        WHERE entry.value <> to_jsonb($3::text)
		      ), '[]'::jsonb),
		      true
		    ),
		    revision = revision + 1,
		    updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid AND kind = 'newsletter'
		  AND `+blocks+` @> jsonb_build_array($3::text)`,
		projectID, newsletterID, itemID)
	return err
}

func (r *Repository) Delete(ctx context.Context, projectID, itemID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var kind string
	if err := tx.QueryRow(ctx, `
		SELECT kind
		FROM content_items
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, itemID).Scan(&kind); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}

	// Any content type may be selected as a newsletter block. Remove dangling
	// IDs before the row is deleted.
	blocks := newsletterBlocksExpression()
	if _, err := tx.Exec(ctx, `
		UPDATE content_items
		SET metadata = jsonb_set(
		      metadata,
		      '{blocks}',
		      COALESCE((
		        SELECT jsonb_agg(entry.value)
		        FROM jsonb_array_elements(`+blocks+`) AS entry(value)
		        WHERE entry.value <> to_jsonb($2::text)
		      ), '[]'::jsonb),
		      true
		    ),
		    revision = revision + 1,
		    updated_at = now()
		WHERE project_id = $1::uuid AND kind = 'newsletter'
		  AND id <> $2::uuid
		  AND `+blocks+` @> jsonb_build_array($2::text)`, projectID, itemID); err != nil {
		return err
	}

	if kind == "newsletter" {
		if _, err := tx.Exec(ctx, `
			UPDATE content_items
			SET channels = array_remove(channels, 'newsletter'),
			    metadata = jsonb_set(metadata - 'newsletterTargetId', '{addNewsletter}', 'false'::jsonb, true),
			    revision = revision + 1,
			    updated_at = now()
			WHERE project_id = $1::uuid AND kind = 'blog'
			  AND metadata->>'newsletterTargetId' = $2`, projectID, itemID); err != nil {
			return err
		}
	}

	result, err := tx.Exec(ctx, `DELETE FROM content_items WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, itemID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListVariants(ctx context.Context, projectID, itemID string) ([]Variant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT variant.id::text, variant.content_item_id::text, variant.channel, variant.locale,
		       variant.title, variant.summary, variant.body, variant.status,
		       variant.scheduled_for, variant.revision, variant.metadata,
		       variant.created_at, variant.updated_at
		FROM content_variants AS variant
		JOIN content_items AS item ON item.id = variant.content_item_id
		WHERE item.project_id = $1::uuid AND item.id = $2::uuid
		ORDER BY variant.channel, variant.locale`, projectID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Variant, 0)
	for rows.Next() {
		item, err := scanVariant(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) SaveVariant(ctx context.Context, projectID, itemID, userID string, input VariantInput) (Variant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Variant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	variant, err := r.saveVariantTx(ctx, tx, projectID, itemID, userID, input, true)
	if err != nil {
		return Variant{}, err
	}
	return variant, tx.Commit(ctx)
}

// saveVariantTx is shared by the standalone variant endpoint and the atomic
// master-content save. syncMaster is false when the master row is the source
// of truth for this transaction; manual variant edits set it to true and use
// the canonical reducer after scheduling side effects are synchronized.
func (r *Repository) saveVariantTx(
	ctx context.Context,
	tx pgx.Tx,
	projectID, itemID, userID string,
	input VariantInput,
	syncMaster bool,
) (Variant, error) {
	quotaStatus := input.Status == "scheduled" || input.Status == "published"
	var capacity limits.PublicationCapacity
	if quotaStatus {
		var err error
		capacity, err = limits.LockPublicationCapacity(ctx, tx, projectID)
		if err != nil {
			return Variant{}, err
		}
	}
	var lockedItemID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM content_items
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, itemID).Scan(&lockedItemID); errors.Is(err, pgx.ErrNoRows) {
		return Variant{}, ErrNotFound
	} else if err != nil {
		return Variant{}, err
	}
	if quotaStatus {
		var currentStatus string
		err := tx.QueryRow(ctx, `
			SELECT variant.status
			FROM content_variants AS variant
			JOIN content_items AS item ON item.id = variant.content_item_id
			WHERE item.project_id = $1::uuid AND item.id = $2::uuid
			  AND variant.channel = $3 AND variant.locale = $4
			FOR UPDATE OF variant`,
			projectID, itemID, input.Channel, input.Locale).Scan(&currentStatus)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Variant{}, err
		}
		if currentStatus != "scheduled" && currentStatus != "published" {
			if input.Channel == "newsletter" {
				err = capacity.RequireAvailableForNewsletterItem(ctx, tx, projectID, itemID)
			} else {
				err = capacity.RequireAvailable(ctx, tx, projectID)
			}
			if err != nil {
				return Variant{}, err
			}
		}
	}
	variant, err := scanVariant(tx.QueryRow(ctx, `
		INSERT INTO content_variants (
			content_item_id, channel, locale, title, summary, body, status,
			scheduled_for, metadata
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		ON CONFLICT (content_item_id, channel, locale) DO UPDATE SET
			title = EXCLUDED.title, summary = EXCLUDED.summary, body = EXCLUDED.body,
			status = EXCLUDED.status, scheduled_for = EXCLUDED.scheduled_for,
			metadata = EXCLUDED.metadata, revision = content_variants.revision + 1,
			updated_at = now()
		RETURNING id::text, content_item_id::text, channel, locale, title, summary,
		          body, status, scheduled_for, revision, metadata, created_at, updated_at`,
		itemID, input.Channel, input.Locale, input.Title, input.Summary, input.Body,
		input.Status, input.ScheduledFor, input.Metadata))
	if err != nil {
		return Variant{}, err
	}
	if quotaStatus {
		if err := capacity.Consume(ctx, tx, projectID, limits.PublicationSourceContentVariant, variant.ID); err != nil {
			return Variant{}, err
		}
	}
	if err := syncVariantSchedule(ctx, tx, projectID, userID, variant); err != nil {
		return Variant{}, err
	}
	if syncMaster {
		if err := syncItemFromVariants(ctx, tx, projectID, itemID); err != nil {
			return Variant{}, err
		}
	}
	if userID != "" {
		if err := recordContentAudit(ctx, tx, projectID, userID, "content.variant_saved", "content_variant", &variant.ID, map[string]any{"channel": variant.Channel, "status": variant.Status}); err != nil {
			return Variant{}, err
		}
	}
	return variant, nil
}

func (r *Repository) DeleteVariant(ctx context.Context, projectID, itemID, variantID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedItemID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM content_items
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, itemID).Scan(&lockedItemID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM calendar_items WHERE project_id = $1::uuid AND content_variant_id = $2::uuid`, projectID, variantID); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		DELETE FROM content_variants AS variant
		USING content_items AS item
		WHERE variant.id = $3::uuid AND variant.content_item_id = item.id
		  AND item.project_id = $1::uuid AND item.id = $2::uuid`, projectID, itemID, variantID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := syncItemFromVariants(ctx, tx, projectID, itemID); err != nil {
		return err
	}
	if err := recordContentAudit(ctx, tx, projectID, userID, "content.variant_deleted", "content_variant", &variantID, map[string]any{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// syncItemFromVariants uses the shared canonical master-state reducer so
// content, calendar, newsletter and worker mutations cannot disagree on
// status precedence or the next schedule.
func syncItemFromVariants(ctx context.Context, tx pgx.Tx, projectID, itemID string) error {
	return contentstate.Recompute(ctx, tx, projectID, itemID)
}

func (r *Repository) SyncDefaultVariants(ctx context.Context, projectID, userID string, item Item) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanItem(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, author_id::text, kind, status, title, summary,
		       body, channels, scheduled_for, source, revision, metadata, created_at, updated_at
		FROM content_items
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, item.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := r.syncDefaultVariantsTx(ctx, tx, projectID, userID, current); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type defaultVariantState struct {
	ID       string
	Channel  string
	Locale   string
	Status   string
	Metadata map[string]any
}

func (r *Repository) syncDefaultVariantsTx(ctx context.Context, tx pgx.Tx, projectID, userID string, item Item) error {
	var defaultLocale string
	if err := tx.QueryRow(ctx, `
		SELECT default_locale
		FROM projects
		WHERE id = $1::uuid`, projectID).Scan(&defaultLocale); err != nil {
		return err
	}
	desiredChannels := make([]string, 0, len(item.Channels))
	desiredSet := make(map[string]bool, len(item.Channels))
	for _, channel := range item.Channels {
		if !supportedVariantChannels[channel] || desiredSet[channel] {
			continue
		}
		desiredSet[channel] = true
		desiredChannels = append(desiredChannels, channel)
	}

	rows, err := tx.Query(ctx, `
		SELECT variant.id::text, variant.channel, variant.locale, variant.status, variant.metadata
		FROM content_variants AS variant
		JOIN content_items AS content ON content.id = variant.content_item_id
		WHERE content.project_id = $1::uuid AND variant.content_item_id = $2::uuid
		FOR UPDATE OF variant`, projectID, item.ID)
	if err != nil {
		return err
	}
	existingDefault := make(map[string]defaultVariantState)
	staleIDs := make([]string, 0)
	staleChannels := make([]string, 0)
	for rows.Next() {
		var state defaultVariantState
		if err := rows.Scan(&state.ID, &state.Channel, &state.Locale, &state.Status, &state.Metadata); err != nil {
			rows.Close()
			return err
		}
		syncedFromItem, _ := state.Metadata["syncedFromItem"].(bool)
		if syncedFromItem && (!desiredSet[state.Channel] || state.Locale != defaultLocale) {
			staleIDs = append(staleIDs, state.ID)
			staleChannels = append(staleChannels, state.Channel)
			continue
		}
		if state.Locale == defaultLocale {
			existingDefault[state.Channel] = state
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(staleIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE newsletter_deliveries
			SET status = 'cancelled',
			    last_error = COALESCE(last_error, 'Cancelled because the auto-synced channel was removed.'),
			    updated_at = now()
			WHERE project_id = $1::uuid AND content_variant_id = ANY($2::uuid[])
			  AND status = 'scheduled'`, projectID, staleIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM calendar_items
			WHERE project_id = $1::uuid AND content_variant_id = ANY($2::uuid[])`, projectID, staleIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM content_variants AS variant
			USING content_items AS content
			WHERE variant.id = ANY($3::uuid[])
			  AND variant.content_item_id = content.id
			  AND content.project_id = $1::uuid AND content.id = $2::uuid`, projectID, item.ID, staleIDs); err != nil {
			return err
		}
		if userID != "" {
			if err := recordContentAudit(ctx, tx, projectID, userID, "content.auto_variants_pruned", "content_item", &item.ID, map[string]any{
				"channels": staleChannels,
			}); err != nil {
				return err
			}
		}
	}

	for _, channel := range desiredChannels {
		if existing, ok := existingDefault[channel]; ok {
			syncedFromItem, _ := existing.Metadata["syncedFromItem"].(bool)
			if !syncedFromItem || existing.Status == "scheduled" || existing.Status == "published" {
				continue
			}
		}
		_, err = r.saveVariantTx(ctx, tx, projectID, item.ID, userID, VariantInput{
			Channel: channel, Locale: defaultLocale, Title: item.Title, Summary: item.Summary,
			Body: item.Body, Status: item.Status, ScheduledFor: item.ScheduledFor,
			Metadata: map[string]any{"syncedFromItem": true, "itemRevision": item.Revision},
		}, false)
		if err != nil {
			return err
		}
	}
	return nil
}

var supportedVariantChannels = map[string]bool{
	"linkedin": true, "instagram": true, "facebook": true, "youtube": true,
	"x": true, "reddit": true, "pinterest": true, "threads": true,
	"telegram": true, "blog": true, "newsletter": true, "website": true, "media": true,
}

var calendarVariantChannels = map[string]bool{
	"linkedin": true, "instagram": true, "facebook": true, "youtube": true,
	"x": true, "reddit": true, "pinterest": true, "threads": true,
	"blog": true, "newsletter": true,
}

func syncVariantSchedule(ctx context.Context, tx pgx.Tx, projectID, userID string, variant Variant) error {
	if variant.Status != "scheduled" || variant.ScheduledFor == nil {
		if variant.Channel == "newsletter" {
			if _, err := tx.Exec(ctx, `
				UPDATE newsletter_deliveries
				SET status = 'cancelled',
				    last_error = COALESCE(last_error, 'Cancelled because the linked newsletter variant was unscheduled.'),
				    updated_at = now()
				WHERE project_id = $1::uuid AND content_variant_id = $2::uuid
				  AND status = 'scheduled'`, projectID, variant.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE publication_jobs SET status = 'cancelled', updated_at = now() WHERE content_variant_id = $1::uuid AND status IN ('pending', 'running')`, variant.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM calendar_items WHERE project_id = $1::uuid AND content_variant_id = $2::uuid`, projectID, variant.ID)
		return err
	}
	if variant.Channel == "newsletter" {
		var deliveryID string
		var deliveryVariantID *string
		err := tx.QueryRow(ctx, `
			SELECT id::text, content_variant_id::text
			FROM newsletter_deliveries
			WHERE project_id = $1::uuid AND content_item_id = $2::uuid
			  AND status = 'scheduled' AND test_recipient IS NULL
			FOR UPDATE`, projectID, variant.ContentItemID).Scan(&deliveryID, &deliveryVariantID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			if deliveryVariantID != nil && *deliveryVariantID != variant.ID {
				return ErrNewsletterDeliveryVariantConflict
			}
			if _, err := tx.Exec(ctx, `
				UPDATE newsletter_deliveries
				SET content_variant_id = $2::uuid, subject = $3, scheduled_for = $4,
				    updated_at = now()
				WHERE id = $1::uuid`, deliveryID, variant.ID, variant.Title, variant.ScheduledFor); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE publication_jobs
				SET status = 'cancelled', updated_at = now()
				WHERE content_variant_id = $1::uuid AND status IN ('pending', 'running')`, variant.ID); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO calendar_items (
					project_id, created_by, title, summary, channel, status, scheduled_for,
					content_item_id, content_variant_id, publication_job_id, metadata
				)
				VALUES ($1::uuid, NULLIF($2, '')::uuid, $3, $4, 'newsletter', 'scheduled', $5,
					$6::uuid, $7::uuid, NULL,
					jsonb_build_object('synced', true, 'newsletterQueue', true, 'newsletterDeliveryId', $8::text))
				ON CONFLICT (content_variant_id) WHERE content_variant_id IS NOT NULL DO UPDATE
				SET title = EXCLUDED.title, summary = EXCLUDED.summary, channel = 'newsletter',
				    status = 'scheduled', scheduled_for = EXCLUDED.scheduled_for,
				    publication_job_id = NULL,
				    metadata = calendar_items.metadata || EXCLUDED.metadata,
				    updated_at = now()`, projectID, userID, variant.Title, variant.Summary,
				variant.ScheduledFor, variant.ContentItemID, variant.ID, deliveryID)
			return err
		}
	}
	var jobID string
	err := tx.QueryRow(ctx, `
		INSERT INTO publication_jobs (project_id, content_variant_id, status, scheduled_for)
		VALUES ($1::uuid, $2::uuid, 'pending', $3)
		ON CONFLICT (content_variant_id) WHERE status IN ('pending', 'running') DO UPDATE
		SET scheduled_for = EXCLUDED.scheduled_for, status = 'pending', updated_at = now()
		RETURNING id::text`, projectID, variant.ID, variant.ScheduledFor).Scan(&jobID)
	if err != nil {
		return err
	}
	if !calendarVariantChannels[variant.Channel] {
		return nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO calendar_items (
			project_id, created_by, title, summary, channel, status, scheduled_for,
			content_item_id, content_variant_id, publication_job_id, metadata
		)
		VALUES ($1::uuid, NULLIF($2, '')::uuid, $3, $4, $5, 'scheduled', $6,
			$7::uuid, $8::uuid, $9::uuid, '{"synced":true}'::jsonb)
		ON CONFLICT (content_variant_id) WHERE content_variant_id IS NOT NULL DO UPDATE
		SET title = EXCLUDED.title, summary = EXCLUDED.summary, channel = EXCLUDED.channel,
		    status = 'scheduled', scheduled_for = EXCLUDED.scheduled_for,
		    publication_job_id = EXCLUDED.publication_job_id, updated_at = now()`,
		projectID, userID, variant.Title, variant.Summary, variant.Channel,
		variant.ScheduledFor, variant.ContentItemID, variant.ID, jobID)
	return err
}

func recordContentAudit(ctx context.Context, tx pgx.Tx, projectID, userID, action, entityType string, entityID *string, metadata map[string]any) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, NULLIF($2, '')::uuid, $3, $4, $5::uuid, $6::jsonb)`,
		projectID, userID, action, entityType, entityID, metadata)
	return err
}

func (r *Repository) GetStrategy(ctx context.Context, projectID string) (Strategy, error) {
	strategy, err := scanStrategy(r.pool.QueryRow(ctx, `
		SELECT project_id::text, mode, six_month_goal, primary_goals, priority_topics,
		       audience, audience_problem, brand_message, proof_points, forbidden_topics,
		       success_metrics, tone, source_filename, source_mime_type, source_text,
		       revision, updated_by::text, created_at, updated_at
		FROM project_strategies
		WHERE project_id = $1::uuid`, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		strategy = Strategy{ProjectID: projectID, Mode: "questions", Revision: 0, PrimaryGoals: []string{}, PriorityTopics: []string{}}
		err = nil
	}
	if err != nil {
		return Strategy{}, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(profile.company_name, ''), NULLIF(project.settings->>'brand', ''), project.name),
		       project.default_locale
		FROM projects AS project
		LEFT JOIN project_profiles AS profile ON profile.project_id = project.id
		WHERE project.id = $1::uuid`, projectID).Scan(&strategy.OrganizationName, &strategy.DefaultLocale); err != nil {
		return Strategy{}, err
	}
	strategy.Personas, err = r.strategyPersonas(ctx, projectID)
	return strategy, err
}

func (r *Repository) strategyPersonas(ctx context.Context, projectID string) ([]PersonaContext, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT name, description, demographics, is_primary
		FROM project_personas
		WHERE project_id = $1::uuid
		ORDER BY is_primary DESC, created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	personas := make([]PersonaContext, 0)
	for rows.Next() {
		var persona PersonaContext
		if err := rows.Scan(&persona.Name, &persona.Description, &persona.Demographics, &persona.IsPrimary); err != nil {
			return nil, err
		}
		personas = append(personas, persona)
	}
	return personas, rows.Err()
}

func (r *Repository) SaveStrategy(ctx context.Context, projectID, userID string, input StrategyInput) (Strategy, error) {
	return scanStrategy(r.pool.QueryRow(ctx, `
		INSERT INTO project_strategies (
			project_id, mode, six_month_goal, primary_goals, priority_topics, audience,
			audience_problem, brand_message, proof_points, forbidden_topics,
			success_metrics, tone, updated_by
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::uuid)
		ON CONFLICT (project_id) DO UPDATE SET
			mode = EXCLUDED.mode, six_month_goal = EXCLUDED.six_month_goal,
			primary_goals = EXCLUDED.primary_goals, priority_topics = EXCLUDED.priority_topics,
			audience = EXCLUDED.audience, audience_problem = EXCLUDED.audience_problem,
			brand_message = EXCLUDED.brand_message, proof_points = EXCLUDED.proof_points,
			forbidden_topics = EXCLUDED.forbidden_topics, success_metrics = EXCLUDED.success_metrics,
			tone = EXCLUDED.tone, revision = project_strategies.revision + 1,
			updated_by = EXCLUDED.updated_by, updated_at = now()
		RETURNING project_id::text, mode, six_month_goal, primary_goals, priority_topics,
		          audience, audience_problem, brand_message, proof_points, forbidden_topics,
		          success_metrics, tone, source_filename, source_mime_type, source_text,
		          revision, updated_by::text, created_at, updated_at`,
		projectID, input.Mode, input.SixMonthGoal, input.PrimaryGoals, input.PriorityTopics,
		input.Audience, input.AudienceProblem, input.BrandMessage, input.ProofPoints,
		input.ForbiddenTopics, input.SuccessMetrics, input.Tone, userID))
}

func (r *Repository) SaveStrategyFile(ctx context.Context, projectID, userID, filename, mimeType, extractedText string) (Strategy, error) {
	return scanStrategy(r.pool.QueryRow(ctx, `
		INSERT INTO project_strategies (
			project_id, mode, source_filename, source_mime_type, source_text, updated_by
		)
		VALUES ($1::uuid, 'upload', $2, $3, $4, $5::uuid)
		ON CONFLICT (project_id) DO UPDATE SET
			mode = 'upload', source_filename = EXCLUDED.source_filename,
			source_mime_type = EXCLUDED.source_mime_type, source_text = EXCLUDED.source_text,
			revision = project_strategies.revision + 1, updated_by = EXCLUDED.updated_by,
			updated_at = now()
		RETURNING project_id::text, mode, six_month_goal, primary_goals, priority_topics,
		          audience, audience_problem, brand_message, proof_points, forbidden_topics,
		          success_metrics, tone, source_filename, source_mime_type, source_text,
		          revision, updated_by::text, created_at, updated_at`,
		projectID, filename, mimeType, extractedText, userID))
}

func (r *Repository) RecordAudit(ctx context.Context, projectID, userID, action, entityType string, entityID *string, metadata map[string]any) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6)`, projectID, userID, action, entityType, entityID, metadata)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanItem(scanner rowScanner) (Item, error) {
	var item Item
	err := scanner.Scan(
		&item.ID, &item.ProjectID, &item.AuthorID, &item.Kind, &item.Status,
		&item.Title, &item.Summary, &item.Body, &item.Channels, &item.ScheduledFor,
		&item.Source, &item.Revision, &item.Metadata, &item.CreatedAt, &item.UpdatedAt,
	)
	if item.Channels == nil {
		item.Channels = []string{}
	}
	return item, err
}

func scanStrategy(scanner rowScanner) (Strategy, error) {
	var strategy Strategy
	err := scanner.Scan(
		&strategy.ProjectID, &strategy.Mode, &strategy.SixMonthGoal, &strategy.PrimaryGoals,
		&strategy.PriorityTopics, &strategy.Audience, &strategy.AudienceProblem,
		&strategy.BrandMessage, &strategy.ProofPoints, &strategy.ForbiddenTopics,
		&strategy.SuccessMetrics, &strategy.Tone, &strategy.SourceFilename,
		&strategy.SourceMIMEType, &strategy.SourceText, &strategy.Revision,
		&strategy.UpdatedBy, &strategy.CreatedAt, &strategy.UpdatedAt,
	)
	if strategy.PrimaryGoals == nil {
		strategy.PrimaryGoals = []string{}
	}
	if strategy.PriorityTopics == nil {
		strategy.PriorityTopics = []string{}
	}
	return strategy, err
}

func scanVariant(scanner rowScanner) (Variant, error) {
	var item Variant
	err := scanner.Scan(&item.ID, &item.ContentItemID, &item.Channel, &item.Locale,
		&item.Title, &item.Summary, &item.Body, &item.Status, &item.ScheduledFor,
		&item.Revision, &item.Metadata, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}
