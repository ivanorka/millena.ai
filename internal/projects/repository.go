package projects

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/auth"
)

var (
	ErrNotFound         = errors.New("project not found")
	ErrLastProject      = errors.New("cannot delete last project")
	ErrProtectedProject = errors.New("cannot delete protected project")
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

func (r *Repository) List(ctx context.Context, userID string) ([]Project, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT project.id::text, project.name, project.slug, project.default_locale,
		       project.status, project.settings, project.created_at, project.updated_at
		FROM projects AS project
		JOIN project_members AS member ON member.project_id = project.id
		WHERE member.user_id = $1::uuid AND member.status = 'active'
		ORDER BY project.created_at DESC
		LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		var project Project
		if err := rows.Scan(
			&project.ID,
			&project.Name,
			&project.Slug,
			&project.DefaultLocale,
			&project.Status,
			&project.Settings,
			&project.CreatedAt,
			&project.UpdatedAt,
		); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return projects, rows.Err()
}

func (r *Repository) GetByID(ctx context.Context, id string) (Project, error) {
	var project Project
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, name, slug, default_locale, status, settings, created_at, updated_at
		FROM projects
		WHERE id = $1::uuid`, id).Scan(
		&project.ID,
		&project.Name,
		&project.Slug,
		&project.DefaultLocale,
		&project.Status,
		&project.Settings,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return project, err
}

func (r *Repository) Create(ctx context.Context, input CreateProjectInput, ownerID string) (Project, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Project{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var project Project
	err = tx.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale)
		VALUES ($1, $2, $3)
		RETURNING id::text, name, slug, default_locale, status, settings, created_at, updated_at`,
		input.Name,
		input.Slug,
		input.DefaultLocale,
	).Scan(
		&project.ID,
		&project.Name,
		&project.Slug,
		&project.DefaultLocale,
		&project.Status,
		&project.Settings,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if err != nil {
		return Project{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role, permissions, status)
		VALUES ($1::uuid, $2::uuid, 'owner', '{"*":true}'::jsonb, 'active')`, project.ID, ownerID); err != nil {
		return Project{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'unlimited', 'active',
		'{"aiAgents":true,"analytics":true,"api":true,"auditLog":true,"automations":true,"prioritySupport":true,"socialChannels":"all","whiteLabel":true}'::jsonb)`, project.ID); err != nil {
		return Project{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_app_states (project_id) VALUES ($1::uuid)`, project.ID); err != nil {
		return Project{}, err
	}
	if err := auth.SeedNewProjectOperationalWorkspace(ctx, tx, project.ID, ownerID, project.Name); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (r *Repository) Delete(ctx context.Context, projectID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var slug string
	if err := tx.QueryRow(ctx, `SELECT slug FROM projects WHERE id = $1::uuid`, projectID).Scan(&slug); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if slug == "millena-demo" {
		return ErrProtectedProject
	}

	var projectCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM project_members
		WHERE user_id = $1::uuid AND status = 'active'`, userID).Scan(&projectCount); err != nil {
		return err
	}
	if projectCount <= 1 {
		return ErrLastProject
	}

	command, err := tx.Exec(ctx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (r *Repository) BootstrapDemo(ctx context.Context) (BootstrapResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return BootstrapResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO projects (name, slug, default_locale)
		VALUES ('MPR Grupa', 'millena-demo', 'hr')
		ON CONFLICT (slug) DO NOTHING`)
	if err != nil {
		return BootstrapResult{}, err
	}

	var result BootstrapResult
	err = tx.QueryRow(ctx, `
		SELECT id::text, name, slug, default_locale, status, settings, created_at, updated_at
		FROM projects
		WHERE slug = 'millena-demo'`).Scan(
		&result.Project.ID,
		&result.Project.Name,
		&result.Project.Slug,
		&result.Project.DefaultLocale,
		&result.Project.Status,
		&result.Project.Settings,
		&result.Project.CreatedAt,
		&result.Project.UpdatedAt,
	)
	if err != nil {
		return BootstrapResult{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO project_app_states (project_id)
		VALUES ($1::uuid)
		ON CONFLICT (project_id) DO NOTHING`, result.Project.ID)
	if err != nil {
		return BootstrapResult{}, err
	}

	err = tx.QueryRow(ctx, `
		SELECT project_id::text, state, revision, updated_at
		FROM project_app_states
		WHERE project_id = $1::uuid`, result.Project.ID).Scan(
		&result.App.ProjectID,
		&result.App.State,
		&result.App.Revision,
		&result.App.UpdatedAt,
	)
	if err != nil {
		return BootstrapResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return BootstrapResult{}, err
	}
	return result, nil
}

func (r *Repository) GetAppState(ctx context.Context, projectID string) (AppState, error) {
	var appState AppState
	err := r.pool.QueryRow(ctx, `
		SELECT project_id::text, state, revision, updated_at
		FROM project_app_states
		WHERE project_id = $1::uuid`, projectID).Scan(
		&appState.ProjectID,
		&appState.State,
		&appState.Revision,
		&appState.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AppState{}, ErrNotFound
	}
	return appState, err
}

func (r *Repository) SaveAppState(ctx context.Context, projectID string, state []byte) (AppState, error) {
	var appState AppState
	err := r.pool.QueryRow(ctx, `
		INSERT INTO project_app_states (project_id, state)
		VALUES ($1::uuid, $2::jsonb)
		ON CONFLICT (project_id) DO UPDATE
		SET state = EXCLUDED.state,
			revision = project_app_states.revision + 1,
			updated_at = now()
		RETURNING project_id::text, state, revision, updated_at`, projectID, state).Scan(
		&appState.ProjectID,
		&appState.State,
		&appState.Revision,
		&appState.UpdatedAt,
	)
	if postgresErrorCode(err) == "23503" {
		return AppState{}, ErrNotFound
	}
	return appState, err
}
