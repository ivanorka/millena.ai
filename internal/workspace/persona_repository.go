package workspace

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const personaColumns = `
	id::text, project_id::text, name, description, demographics, is_primary,
	metadata, created_by::text, created_at, updated_at`

func (r *Repository) ListProjectPersonas(ctx context.Context, projectID string) ([]ProjectPersona, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+personaColumns+`
		FROM project_personas
		WHERE project_id = $1::uuid
		ORDER BY is_primary DESC, created_at, lower(name)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProjectPersona, 0)
	for rows.Next() {
		item, err := scanProjectPersona(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateProjectPersona(ctx context.Context, projectID, actorID string, input ProjectPersonaInput) (ProjectPersona, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ProjectPersona{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var hasPrimary bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project_personas WHERE project_id = $1::uuid AND is_primary)`, projectID).Scan(&hasPrimary); err != nil {
		return ProjectPersona{}, err
	}
	input.IsPrimary = input.IsPrimary || !hasPrimary
	if input.IsPrimary {
		if _, err := tx.Exec(ctx, `UPDATE project_personas SET is_primary = false, updated_at = now() WHERE project_id = $1::uuid AND is_primary`, projectID); err != nil {
			return ProjectPersona{}, err
		}
	}
	item, err := scanProjectPersona(tx.QueryRow(ctx, `
		INSERT INTO project_personas (
			project_id, name, description, demographics, is_primary, metadata, created_by
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::jsonb, $7::uuid)
		RETURNING `+personaColumns,
		projectID, input.Name, input.Description, input.Demographics,
		input.IsPrimary, input.Metadata, actorID))
	if err != nil {
		return ProjectPersona{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "project_persona.created", "project_persona", &item.ID, map[string]any{"name": item.Name, "isPrimary": item.IsPrimary}); err != nil {
		return ProjectPersona{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) UpdateProjectPersona(ctx context.Context, projectID, personaID, actorID string, input ProjectPersonaInput) (ProjectPersona, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ProjectPersona{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var wasPrimary bool
	err = tx.QueryRow(ctx, `
		SELECT is_primary FROM project_personas
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, personaID).Scan(&wasPrimary)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectPersona{}, ErrNotFound
	}
	if err != nil {
		return ProjectPersona{}, err
	}
	if wasPrimary && !input.IsPrimary {
		var otherPrimary bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project_personas WHERE project_id = $1::uuid AND id <> $2::uuid AND is_primary)`, projectID, personaID).Scan(&otherPrimary); err != nil {
			return ProjectPersona{}, err
		}
		input.IsPrimary = !otherPrimary
	}
	if input.IsPrimary {
		if _, err := tx.Exec(ctx, `UPDATE project_personas SET is_primary = false, updated_at = now() WHERE project_id = $1::uuid AND id <> $2::uuid AND is_primary`, projectID, personaID); err != nil {
			return ProjectPersona{}, err
		}
	}
	item, err := scanProjectPersona(tx.QueryRow(ctx, `
		UPDATE project_personas
		SET name = $3, description = $4, demographics = $5, is_primary = $6,
		    metadata = $7::jsonb, updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING `+personaColumns,
		projectID, personaID, input.Name, input.Description, input.Demographics,
		input.IsPrimary, input.Metadata))
	if err != nil {
		return ProjectPersona{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "project_persona.updated", "project_persona", &item.ID, map[string]any{"name": item.Name, "isPrimary": item.IsPrimary}); err != nil {
		return ProjectPersona{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) DeleteProjectPersona(ctx context.Context, projectID, personaID, actorID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var wasPrimary bool
	err = tx.QueryRow(ctx, `
		DELETE FROM project_personas
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING is_primary`, projectID, personaID).Scan(&wasPrimary)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if wasPrimary {
		if _, err := tx.Exec(ctx, `
			UPDATE project_personas
			SET is_primary = true, updated_at = now()
			WHERE id = (
				SELECT id FROM project_personas
				WHERE project_id = $1::uuid
				ORDER BY created_at, id
				LIMIT 1
			)`, projectID); err != nil {
			return err
		}
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "project_persona.deleted", "project_persona", &personaID, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanProjectPersona(scanner scanner) (ProjectPersona, error) {
	var item ProjectPersona
	err := scanner.Scan(
		&item.ID, &item.ProjectID, &item.Name, &item.Description,
		&item.Demographics, &item.IsPrimary, &item.Metadata, &item.CreatedBy,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}
