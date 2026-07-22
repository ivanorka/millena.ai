package audience

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("audience record not found")
var ErrListNotDeletable = errors.New("audience list cannot be deleted")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

func (r *Repository) Lists(ctx context.Context, projectID string) ([]List, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT list.id::text, list.project_id::text, list.name, list.description,
		       list.is_default, count(contact.id)::int,
		       count(contact.id) FILTER (WHERE contact.status = 'active')::int,
		       list.created_at, list.updated_at
		FROM audience_lists AS list
		LEFT JOIN audience_contacts AS contact ON contact.list_id = list.id
		WHERE list.project_id = $1::uuid
		GROUP BY list.id
		ORDER BY list.is_default DESC, lower(list.name)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]List, 0)
	for rows.Next() {
		var item List
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Name, &item.Description,
			&item.IsDefault, &item.ContactCount, &item.ActiveCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateList(ctx context.Context, projectID, userID string, input ListInput) (List, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return List{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE audience_lists SET is_default = false, updated_at = now() WHERE project_id = $1::uuid`, projectID); err != nil {
			return List{}, err
		}
	}
	var item List
	err = tx.QueryRow(ctx, `
		INSERT INTO audience_lists (project_id, name, description, is_default, created_by)
		VALUES ($1::uuid, $2, $3, $4, $5::uuid)
		RETURNING id::text, project_id::text, name, description, is_default, 0, 0, created_at, updated_at`,
		projectID, input.Name, input.Description, input.IsDefault, userID).Scan(
		&item.ID, &item.ProjectID, &item.Name, &item.Description, &item.IsDefault,
		&item.ContactCount, &item.ActiveCount, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return List{}, err
	}
	if err := recordAudit(ctx, tx, projectID, userID, "audience.list_created", "audience_list", &item.ID, map[string]any{"name": item.Name}); err != nil {
		return List{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) UpdateList(ctx context.Context, projectID, listID, userID string, input ListInput) (List, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return List{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var wasDefault bool
	err = tx.QueryRow(ctx, `
		SELECT is_default FROM audience_lists
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, listID).Scan(&wasDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return List{}, ErrNotFound
	}
	if err != nil {
		return List{}, err
	}
	if wasDefault && !input.IsDefault {
		var otherDefault bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audience_lists WHERE project_id = $1::uuid AND id <> $2::uuid AND is_default)`, projectID, listID).Scan(&otherDefault); err != nil {
			return List{}, err
		}
		input.IsDefault = !otherDefault
	}
	if input.IsDefault {
		if _, err := tx.Exec(ctx, `
			UPDATE audience_lists
			SET is_default = false, updated_at = now()
			WHERE project_id = $1::uuid AND id <> $2::uuid`, projectID, listID); err != nil {
			return List{}, err
		}
	}
	command, err := tx.Exec(ctx, `
		UPDATE audience_lists
		SET name = $3, description = $4, is_default = $5, updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid`,
		projectID, listID, input.Name, input.Description, input.IsDefault)
	if err != nil {
		return List{}, err
	}
	if command.RowsAffected() == 0 {
		return List{}, ErrNotFound
	}
	if err := recordAudit(ctx, tx, projectID, userID, "audience.list_updated", "audience_list", &listID, map[string]any{
		"name": input.Name, "isDefault": input.IsDefault,
	}); err != nil {
		return List{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return List{}, err
	}
	return r.GetList(ctx, projectID, listID)
}

func (r *Repository) GetList(ctx context.Context, projectID, listID string) (List, error) {
	var item List
	err := r.pool.QueryRow(ctx, `
		SELECT list.id::text, list.project_id::text, list.name, list.description,
		       list.is_default, count(contact.id)::int,
		       count(contact.id) FILTER (WHERE contact.status = 'active')::int,
		       list.created_at, list.updated_at
		FROM audience_lists AS list
		LEFT JOIN audience_contacts AS contact ON contact.list_id = list.id
		WHERE list.project_id = $1::uuid AND list.id = $2::uuid
		GROUP BY list.id`, projectID, listID).Scan(
		&item.ID, &item.ProjectID, &item.Name, &item.Description, &item.IsDefault,
		&item.ContactCount, &item.ActiveCount, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return List{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) DeleteList(ctx context.Context, projectID, listID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var isDefault bool
	err = tx.QueryRow(ctx, `
		SELECT is_default
		FROM audience_lists
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, listID).Scan(&isDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var contactCount int
	if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM audience_contacts WHERE list_id = $1::uuid`, listID).Scan(&contactCount); err != nil {
		return err
	}
	if isDefault || contactCount > 0 {
		return ErrListNotDeletable
	}
	if _, err := tx.Exec(ctx, `DELETE FROM audience_lists WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, listID); err != nil {
		return err
	}
	if err := recordAudit(ctx, tx, projectID, userID, "audience.list_deleted", "audience_list", &listID, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) Contacts(ctx context.Context, projectID, search, status, listID string) (ContactCollection, error) {
	args := []any{projectID}
	conditions := []string{"contact.project_id = $1::uuid"}
	if search != "" {
		args = append(args, "%"+search+"%")
		conditions = append(conditions, "(contact.email ILIKE $2 OR concat_ws(' ', contact.first_name, contact.last_name) ILIKE $2)")
	}
	if status != "" {
		args = append(args, status)
		conditions = append(conditions, "contact.status = $"+itoa(len(args)))
	}
	if listID != "" {
		args = append(args, listID)
		conditions = append(conditions, "contact.list_id = $"+itoa(len(args))+"::uuid")
	}
	query := `
		SELECT contact.id::text, contact.project_id::text, contact.list_id::text,
		       COALESCE(list.name, ''), contact.first_name, contact.last_name,
		       contact.email, contact.source, contact.status, contact.consent,
		       contact.subscribed_at, contact.unsubscribed_at, contact.metadata,
		       contact.created_at, contact.updated_at
		FROM audience_contacts AS contact
		LEFT JOIN audience_lists AS list ON list.id = contact.list_id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY contact.created_at DESC
		LIMIT 500`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ContactCollection{}, err
	}
	defer rows.Close()
	items := make([]Contact, 0)
	for rows.Next() {
		item, err := scanContact(rows)
		if err != nil {
			return ContactCollection{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ContactCollection{}, err
	}
	stats, err := r.Stats(ctx, projectID)
	return ContactCollection{Items: items, Stats: stats}, err
}

func (r *Repository) Stats(ctx context.Context, projectID string) (Stats, error) {
	var stats Stats
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE status = 'active')::int,
		       count(*) FILTER (WHERE status = 'pending')::int,
		       count(*) FILTER (WHERE status = 'unsubscribed')::int,
		       count(*) FILTER (WHERE source = 'website')::int
		FROM audience_contacts WHERE project_id = $1::uuid`, projectID).Scan(
		&stats.Total, &stats.Active, &stats.Pending, &stats.Unsubscribed, &stats.Website)
	if stats.Total > 0 {
		stats.ActiveRate = float64(stats.Active) * 100 / float64(stats.Total)
	}
	return stats, err
}

func (r *Repository) Get(ctx context.Context, projectID, contactID string) (Contact, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT contact.id::text, contact.project_id::text, contact.list_id::text,
		       COALESCE(list.name, ''), contact.first_name, contact.last_name,
		       contact.email, contact.source, contact.status, contact.consent,
		       contact.subscribed_at, contact.unsubscribed_at, contact.metadata,
		       contact.created_at, contact.updated_at
		FROM audience_contacts AS contact
		LEFT JOIN audience_lists AS list ON list.id = contact.list_id
		WHERE contact.project_id = $1::uuid AND contact.id = $2::uuid`, projectID, contactID)
	item, err := scanContact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contact{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) Create(ctx context.Context, projectID, userID string, input ContactInput) (Contact, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Contact{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	listID, err := resolveListID(ctx, tx, projectID, input.ListID)
	if err != nil {
		return Contact{}, err
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO audience_contacts (
			project_id, list_id, first_name, last_name, email, source, status,
			consent, subscribed_at, unsubscribed_at, metadata, created_by
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, lower($5), $6, $7, $8,
			CASE WHEN $8 OR $7 = 'active' THEN now() ELSE NULL END,
			CASE WHEN $7 = 'unsubscribed' THEN now() ELSE NULL END, $9::jsonb, $10::uuid)
		RETURNING id::text`, projectID, listID, input.FirstName, input.LastName, input.Email,
		input.Source, input.Status, input.Consent, input.Metadata, userID).Scan(&id)
	if err != nil {
		return Contact{}, err
	}
	if err := recordAudit(ctx, tx, projectID, userID, "audience.contact_created", "audience_contact", &id, map[string]any{"source": input.Source}); err != nil {
		return Contact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Contact{}, err
	}
	return r.Get(ctx, projectID, id)
}

func (r *Repository) Update(ctx context.Context, projectID, contactID, userID string, input ContactInput) (Contact, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Contact{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	listID, err := resolveListID(ctx, tx, projectID, input.ListID)
	if err != nil {
		return Contact{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE audience_contacts
		SET list_id = $3::uuid, first_name = $4, last_name = $5, email = lower($6),
		    source = $7, status = $8, consent = $9, metadata = $10::jsonb,
		    subscribed_at = CASE WHEN $9 OR $8 = 'active' THEN COALESCE(subscribed_at, now()) ELSE subscribed_at END,
		    unsubscribed_at = CASE WHEN $8 = 'unsubscribed' THEN COALESCE(unsubscribed_at, now()) ELSE NULL END,
		    updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, contactID, listID,
		input.FirstName, input.LastName, input.Email, input.Source, input.Status, input.Consent, input.Metadata)
	if err != nil {
		return Contact{}, err
	}
	if command.RowsAffected() == 0 {
		return Contact{}, ErrNotFound
	}
	if err := recordAudit(ctx, tx, projectID, userID, "audience.contact_updated", "audience_contact", &contactID, map[string]any{"status": input.Status}); err != nil {
		return Contact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Contact{}, err
	}
	return r.Get(ctx, projectID, contactID)
}

func (r *Repository) Delete(ctx context.Context, projectID, contactID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `DELETE FROM audience_contacts WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, contactID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := recordAudit(ctx, tx, projectID, userID, "audience.contact_deleted", "audience_contact", &contactID, map[string]any{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) UpsertImported(ctx context.Context, projectID, userID string, input ContactInput) (bool, error) {
	listID, err := resolveListID(ctx, r.pool, projectID, input.ListID)
	if err != nil {
		return false, err
	}
	var inserted bool
	err = r.pool.QueryRow(ctx, `
		INSERT INTO audience_contacts (
			project_id, list_id, first_name, last_name, email, source, status,
			consent, subscribed_at, metadata, created_by
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, lower($5), 'csv', $6, $7,
			CASE WHEN $7 OR $6 = 'active' THEN now() ELSE NULL END,
			jsonb_build_object('imported', true), $8::uuid)
		ON CONFLICT (project_id, lower(email)) DO UPDATE
		SET list_id = EXCLUDED.list_id,
		    first_name = CASE WHEN EXCLUDED.first_name <> '' THEN EXCLUDED.first_name ELSE audience_contacts.first_name END,
		    last_name = CASE WHEN EXCLUDED.last_name <> '' THEN EXCLUDED.last_name ELSE audience_contacts.last_name END,
		    consent = EXCLUDED.consent,
		    status = EXCLUDED.status,
		    updated_at = now()
		RETURNING xmax = 0`, projectID, listID, input.FirstName, input.LastName, input.Email,
		input.Status, input.Consent, userID).Scan(&inserted)
	return inserted, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanContact(row rowScanner) (Contact, error) {
	var item Contact
	err := row.Scan(&item.ID, &item.ProjectID, &item.ListID, &item.ListName,
		&item.FirstName, &item.LastName, &item.Email, &item.Source, &item.Status,
		&item.Consent, &item.SubscribedAt, &item.UnsubscribedAt, &item.Metadata,
		&item.CreatedAt, &item.UpdatedAt)
	return item, err
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func resolveListID(ctx context.Context, query queryRower, projectID string, requested *string) (string, error) {
	var id string
	if requested != nil && *requested != "" {
		err := query.QueryRow(ctx, `SELECT id::text FROM audience_lists WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, *requested).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return id, err
	}
	err := query.QueryRow(ctx, `
		SELECT id::text FROM audience_lists
		WHERE project_id = $1::uuid
		ORDER BY is_default DESC, created_at
		LIMIT 1`, projectID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func recordAudit(ctx context.Context, tx pgx.Tx, projectID, userID, action, entityType string, entityID *string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6::jsonb)`,
		projectID, userID, action, entityType, entityID, metadata)
	return err
}

func itoa(value int) string {
	const digits = "0123456789"
	if value < 10 {
		return string(digits[value])
	}
	return string(digits[value/10]) + string(digits[value%10])
}
