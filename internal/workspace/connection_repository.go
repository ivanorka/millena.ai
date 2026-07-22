package workspace

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ivanorka/millena-ai/internal/limits"
)

const channelConnectionColumns = `
	id::text, project_id::text, provider, mode, display_name, account_handle,
	endpoint_url, status, credential_fingerprint, metadata, last_checked_at,
	created_by::text, created_at, updated_at`

func (r *Repository) ListChannelConnections(ctx context.Context, projectID string) ([]ChannelConnection, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+channelConnectionColumns+`
		FROM channel_connections
		WHERE project_id = $1::uuid AND status <> 'disconnected'
		ORDER BY provider`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connections := make([]ChannelConnection, 0)
	for rows.Next() {
		connection, err := scanChannelConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func (r *Repository) CreateChannelConnection(ctx context.Context, projectID, actorID string, input ChannelConnectionInput) (ChannelConnection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ChannelConnection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.Mode != "sandbox" {
		if err := requireConnectionAPIFeature(ctx, tx, projectID); err != nil {
			return ChannelConnection{}, err
		}
	}
	connection, err := scanChannelConnection(tx.QueryRow(ctx, `
		INSERT INTO channel_connections (
			project_id, provider, mode, display_name, account_handle, endpoint_url,
			status, credential_fingerprint, metadata, last_checked_at, created_by
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, 'connected', $7, $8, now(), $9::uuid)
		ON CONFLICT (project_id, provider) DO UPDATE SET
			mode = EXCLUDED.mode,
			display_name = EXCLUDED.display_name,
			account_handle = EXCLUDED.account_handle,
			endpoint_url = EXCLUDED.endpoint_url,
			status = 'connected',
			credential_fingerprint = EXCLUDED.credential_fingerprint,
			metadata = EXCLUDED.metadata,
			last_checked_at = now(),
			created_by = EXCLUDED.created_by,
			updated_at = now()
		WHERE channel_connections.status = 'disconnected'
		RETURNING `+channelConnectionColumns,
		projectID, input.Provider, input.Mode, input.DisplayName, input.AccountHandle,
		input.EndpointURL, input.credentialFingerprint, input.Metadata, actorID))
	if errors.Is(err, pgx.ErrNoRows) || postgresCode(err) == "23505" {
		return ChannelConnection{}, ErrConflict
	}
	if err != nil {
		return ChannelConnection{}, err
	}
	if connection.Mode != "sandbox" && !connection.CredentialConfigured {
		return ChannelConnection{}, ErrCredentialRequired
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "channel_connection.created", "channel_connection", &connection.ID, map[string]any{
		"provider": connection.Provider, "mode": connection.Mode,
	}); err != nil {
		return ChannelConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChannelConnection{}, err
	}
	return connection, nil
}

func (r *Repository) UpdateChannelConnection(ctx context.Context, projectID, connectionID, actorID string, input ChannelConnectionInput) (ChannelConnection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ChannelConnection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.Mode != "sandbox" {
		if err := requireConnectionAPIFeature(ctx, tx, projectID); err != nil {
			return ChannelConnection{}, err
		}
	}
	connection, err := scanChannelConnection(tx.QueryRow(ctx, `
		UPDATE channel_connections
		SET provider = $3, mode = $4, display_name = $5, account_handle = $6,
		    endpoint_url = $7, status = 'connected',
		    credential_fingerprint = CASE WHEN $8 = '' THEN credential_fingerprint ELSE $8 END,
		    metadata = $9, updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING `+channelConnectionColumns,
		projectID, connectionID, input.Provider, input.Mode, input.DisplayName,
		input.AccountHandle, input.EndpointURL, input.credentialFingerprint, input.Metadata))
	if errors.Is(err, pgx.ErrNoRows) {
		return ChannelConnection{}, ErrNotFound
	}
	if postgresCode(err) == "23505" {
		return ChannelConnection{}, ErrConflict
	}
	if err != nil {
		return ChannelConnection{}, err
	}
	if connection.Mode != "sandbox" && !connection.CredentialConfigured {
		return ChannelConnection{}, ErrCredentialRequired
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "channel_connection.updated", "channel_connection", &connection.ID, map[string]any{
		"provider": connection.Provider, "mode": connection.Mode,
	}); err != nil {
		return ChannelConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChannelConnection{}, err
	}
	return connection, nil
}

func requireConnectionAPIFeature(ctx context.Context, tx pgx.Tx, projectID string) error {
	enabled, err := connectionAPIEntitlement(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrFeatureUnavailable
	}
	return nil
}

func connectionAPIEntitlement(ctx context.Context, tx pgx.Tx, projectID string) (bool, error) {
	var status string
	var enabled bool
	err := tx.QueryRow(ctx, `
		SELECT status, COALESCE(features->>'api', '') = 'true'
		FROM project_entitlements
		WHERE project_id = $1::uuid
		FOR UPDATE`, projectID).Scan(&status, &enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, limits.ErrEntitlementInactive
	}
	if err != nil {
		return false, err
	}
	if status != "active" && status != "trial" {
		return false, limits.ErrEntitlementInactive
	}
	return enabled, nil
}

func (r *Repository) DeleteChannelConnection(ctx context.Context, projectID, connectionID, actorID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `
		UPDATE channel_connections
		SET status = 'disconnected', credential_fingerprint = '', updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid AND status <> 'disconnected'`, projectID, connectionID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "channel_connection.deleted", "channel_connection", &connectionID, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) TestChannelConnection(ctx context.Context, projectID, connectionID, actorID string) (ChannelConnection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ChannelConnection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	apiEnabled, err := connectionAPIEntitlement(ctx, tx, projectID)
	if err != nil {
		return ChannelConnection{}, err
	}
	connection, err := scanChannelConnection(tx.QueryRow(ctx, `
		UPDATE channel_connections
		SET status = 'connected', last_checked_at = now(), updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid AND status <> 'disconnected'
		RETURNING `+channelConnectionColumns, projectID, connectionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ChannelConnection{}, ErrNotFound
	}
	if err != nil {
		return ChannelConnection{}, err
	}
	if connection.Mode != "sandbox" && !connection.CredentialConfigured {
		return ChannelConnection{}, ErrCredentialRequired
	}
	if connection.Mode != "sandbox" && !apiEnabled {
		return ChannelConnection{}, ErrFeatureUnavailable
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "channel_connection.tested", "channel_connection", &connection.ID, map[string]any{
		"provider": connection.Provider, "status": connection.Status,
	}); err != nil {
		return ChannelConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChannelConnection{}, err
	}
	return connection, nil
}

func scanChannelConnection(row scanner) (ChannelConnection, error) {
	var connection ChannelConnection
	var fingerprint string
	err := row.Scan(
		&connection.ID, &connection.ProjectID, &connection.Provider, &connection.Mode,
		&connection.DisplayName, &connection.AccountHandle, &connection.EndpointURL,
		&connection.Status, &fingerprint, &connection.Metadata, &connection.LastCheckedAt,
		&connection.CreatedBy, &connection.CreatedAt, &connection.UpdatedAt,
	)
	connection.CredentialConfigured = fingerprint != ""
	if connection.Metadata == nil {
		connection.Metadata = map[string]any{}
	}
	return connection, err
}
