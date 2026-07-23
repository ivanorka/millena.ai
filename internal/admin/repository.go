package admin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound            = errors.New("admin resource not found")
	ErrMemberExists        = errors.New("project member already exists")
	ErrUserUnavailable     = errors.New("existing user is not active")
	ErrSelfRemoval         = errors.New("a member cannot remove or suspend themselves")
	ErrLastOwner           = errors.New("the last active project owner cannot be removed")
	ErrPlanNotAvailable    = errors.New("plan is not available to this project")
	ErrSeatLimitReached    = errors.New("project seat limit has been reached")
	ErrEntitlementInactive = errors.New("project entitlement is not active")
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

func (r *Repository) ListTeam(ctx context.Context, projectID string) ([]TeamMember, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT users.id::text, users.email, users.display_name, users.status,
		       member.role, member.permissions, member.status, member.created_at
		FROM project_members AS member
		JOIN users ON users.id = member.user_id
		WHERE member.project_id = $1::uuid
		ORDER BY CASE member.role
		           WHEN 'owner' THEN 1 WHEN 'lead' THEN 2 WHEN 'editor' THEN 3
		           WHEN 'contributor' THEN 4 ELSE 5 END,
		         lower(users.display_name), lower(users.email)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]TeamMember, 0)
	for rows.Next() {
		member, err := scanTeamMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (r *Repository) CreateMember(ctx context.Context, projectID, actorID string, input CreateMemberInput) (TeamMember, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.TempPassword), 12)
	if err != nil {
		return TeamMember{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return TeamMember{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockProject(ctx, tx, projectID); err != nil {
		return TeamMember{}, err
	}
	var organizationID string
	if err := tx.QueryRow(ctx, `SELECT organization_id::text FROM projects WHERE id=$1::uuid`, projectID).Scan(&organizationID); err != nil {
		return TeamMember{}, err
	}
	var seatLimit *int
	var entitlementStatus string
	var activeSeats int
	if err := tx.QueryRow(ctx, `
		SELECT entitlement.seat_limit, entitlement.status,
		       (SELECT count(*)::int FROM project_members WHERE project_id = $1::uuid AND status = 'active')
		FROM project_entitlements AS entitlement
		WHERE entitlement.project_id = $1::uuid
		FOR UPDATE`, projectID).Scan(&seatLimit, &entitlementStatus, &activeSeats); errors.Is(err, pgx.ErrNoRows) {
		return TeamMember{}, ErrEntitlementInactive
	} else if err != nil {
		return TeamMember{}, err
	}
	if entitlementStatus != "active" && entitlementStatus != "trial" {
		return TeamMember{}, ErrEntitlementInactive
	}
	if seatLimit != nil && activeSeats >= *seatLimit {
		return TeamMember{}, ErrSeatLimitReached
	}
	// Serialize registrations for the same normalized email without taking a
	// broad table lock. This also makes membership conflict handling stable.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(lower($1), 0))`, input.Email); err != nil {
		return TeamMember{}, err
	}

	var userID, userStatus string
	createdUser := false
	err = tx.QueryRow(ctx, `
		SELECT id::text, status
		FROM users
		WHERE lower(email) = lower($1)
		FOR UPDATE`, input.Email).Scan(&userID, &userStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO users (email, display_name, status, password_hash)
			VALUES (lower($1), $2, 'active', $3)
			RETURNING id::text, status`, input.Email, input.DisplayName, string(passwordHash)).Scan(&userID, &userStatus)
		createdUser = err == nil
	}
	if err != nil {
		return TeamMember{}, err
	}
	if userStatus != "active" {
		return TeamMember{}, ErrUserUnavailable
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members (organization_id,user_id,role,status)
		VALUES ($1::uuid,$2::uuid,'member','active')
		ON CONFLICT (organization_id,user_id) DO UPDATE
		SET status='active',updated_at=now()`, organizationID, userID); err != nil {
		return TeamMember{}, err
	}

	var memberID string
	err = tx.QueryRow(ctx, `
		INSERT INTO project_members (project_id, user_id, role, permissions, status)
		VALUES ($1::uuid, $2::uuid, $3, '{}'::jsonb, 'active')
		ON CONFLICT (project_id, user_id) DO NOTHING
		RETURNING user_id::text`, projectID, userID, input.Role).Scan(&memberID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TeamMember{}, ErrMemberExists
	}
	if err != nil {
		return TeamMember{}, err
	}

	member, err := getTeamMember(ctx, tx, projectID, userID)
	if err != nil {
		return TeamMember{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "team.member_created", "project_member", &userID, map[string]any{
		"email": input.Email, "role": input.Role, "createdUser": createdUser,
	}); err != nil {
		return TeamMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TeamMember{}, err
	}
	return member, nil
}

func (r *Repository) UpdateMember(ctx context.Context, projectID, actorID, memberID string, input UpdateMemberInput) (TeamMember, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return TeamMember{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockProject(ctx, tx, projectID); err != nil {
		return TeamMember{}, err
	}

	var currentRole, currentStatus string
	err = tx.QueryRow(ctx, `
		SELECT role, status
		FROM project_members
		WHERE project_id = $1::uuid AND user_id = $2::uuid
		FOR UPDATE`, projectID, memberID).Scan(&currentRole, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return TeamMember{}, ErrNotFound
	}
	if err != nil {
		return TeamMember{}, err
	}

	nextRole, nextStatus := currentRole, currentStatus
	if input.Role != nil {
		nextRole = *input.Role
	}
	if input.Status != nil {
		nextStatus = *input.Status
	}
	if currentStatus != "active" && nextStatus == "active" {
		var seatLimit *int
		var entitlementStatus string
		var activeSeats int
		if err := tx.QueryRow(ctx, `
			SELECT entitlement.seat_limit, entitlement.status,
			       (SELECT count(*)::int FROM project_members WHERE project_id = $1::uuid AND status = 'active')
			FROM project_entitlements AS entitlement
			WHERE entitlement.project_id = $1::uuid
			FOR UPDATE`, projectID).Scan(&seatLimit, &entitlementStatus, &activeSeats); errors.Is(err, pgx.ErrNoRows) {
			return TeamMember{}, ErrEntitlementInactive
		} else if err != nil {
			return TeamMember{}, err
		}
		if entitlementStatus != "active" && entitlementStatus != "trial" {
			return TeamMember{}, ErrEntitlementInactive
		}
		if seatLimit != nil && activeSeats >= *seatLimit {
			return TeamMember{}, ErrSeatLimitReached
		}
	}
	if actorID == memberID && nextStatus != "active" {
		return TeamMember{}, ErrSelfRemoval
	}
	if currentRole == "owner" && currentStatus == "active" && (nextRole != "owner" || nextStatus != "active") {
		if err := requireAnotherOwner(ctx, tx, projectID, memberID); err != nil {
			return TeamMember{}, err
		}
	}

	result, err := tx.Exec(ctx, `
		UPDATE project_members
		SET role = $3, status = $4
		WHERE project_id = $1::uuid AND user_id = $2::uuid`, projectID, memberID, nextRole, nextStatus)
	if err != nil {
		return TeamMember{}, err
	}
	if result.RowsAffected() == 0 {
		return TeamMember{}, ErrNotFound
	}

	member, err := getTeamMember(ctx, tx, projectID, memberID)
	if err != nil {
		return TeamMember{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "team.member_updated", "project_member", &memberID, map[string]any{
		"previousRole": currentRole, "role": nextRole,
		"previousStatus": currentStatus, "status": nextStatus,
	}); err != nil {
		return TeamMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TeamMember{}, err
	}
	return member, nil
}

func (r *Repository) DeleteMember(ctx context.Context, projectID, actorID, memberID string) error {
	if actorID == memberID {
		return ErrSelfRemoval
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockProject(ctx, tx, projectID); err != nil {
		return err
	}

	var role, status string
	err = tx.QueryRow(ctx, `
		SELECT role, status
		FROM project_members
		WHERE project_id = $1::uuid AND user_id = $2::uuid
		FOR UPDATE`, projectID, memberID).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role == "owner" && status == "active" {
		if err := requireAnotherOwner(ctx, tx, projectID, memberID); err != nil {
			return err
		}
	}
	result, err := tx.Exec(ctx, `DELETE FROM project_members WHERE project_id = $1::uuid AND user_id = $2::uuid`, projectID, memberID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "team.member_deleted", "project_member", &memberID, map[string]any{
		"role": role, "status": status,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListPlans(ctx context.Context, projectID string) ([]Plan, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT code, owner_project_id::text, name, description, price_cents, currency,
		       billing_interval, seat_limit, monthly_publication_limit, storage_limit_bytes,
		       features, is_active, is_system, created_at, updated_at
		FROM plan_catalog
		WHERE is_active AND (is_system OR owner_project_id = $1::uuid)
		ORDER BY is_system DESC, price_cents, lower(name)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := make([]Plan, 0)
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (r *Repository) CreateCustomPlan(ctx context.Context, projectID, actorID string, input CreatePlanInput) (Plan, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockProject(ctx, tx, projectID); err != nil {
		return Plan{}, err
	}
	plan, err := scanPlan(tx.QueryRow(ctx, `
		INSERT INTO plan_catalog (
			code, owner_project_id, name, description, price_cents, currency,
			billing_interval, seat_limit, monthly_publication_limit,
			storage_limit_bytes, features, is_active, is_system
		)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, true, false)
		RETURNING code, owner_project_id::text, name, description, price_cents, currency,
		          billing_interval, seat_limit, monthly_publication_limit, storage_limit_bytes,
		          features, is_active, is_system, created_at, updated_at`,
		input.Code, projectID, input.Name, input.Description, input.PriceCents,
		input.Currency, input.BillingInterval, input.SeatLimit,
		input.MonthlyPublicationLimit, input.StorageLimitBytes, input.Features))
	if err != nil {
		return Plan{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "plan.created", "plan", nil, map[string]any{
		"code": plan.Code, "name": plan.Name,
	}); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (r *Repository) GetEntitlement(ctx context.Context, projectID string) (Entitlement, error) {
	entitlement, err := scanEntitlement(r.pool.QueryRow(ctx, entitlementSelectSQL, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entitlement{}, ErrNotFound
	}
	return entitlement, err
}

func (r *Repository) UpdateEntitlement(ctx context.Context, projectID, actorID, planCode string) (Entitlement, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Entitlement{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockProject(ctx, tx, projectID); err != nil {
		return Entitlement{}, err
	}

	var organizationID, previousPlan string
	if err := tx.QueryRow(ctx, `
		SELECT project.organization_id::text,entitlement.plan_code
		FROM projects AS project
		JOIN organization_entitlements AS entitlement ON entitlement.organization_id=project.organization_id
		WHERE project.id=$1::uuid`, projectID).Scan(&organizationID, &previousPlan); err != nil {
		return Entitlement{}, err
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO organization_entitlements (
			organization_id,plan_code,status,monthly_publication_limit,
			storage_limit_bytes,features,renews_at
		)
		SELECT $1::uuid,plan.code,'active',plan.monthly_publication_limit,
		       plan.storage_limit_bytes,plan.features,NULL
		FROM plan_catalog AS plan
		WHERE plan.code = $2 AND plan.is_active
		  AND (plan.is_system OR EXISTS (
		      SELECT 1 FROM projects AS owner_project
		      WHERE owner_project.id=plan.owner_project_id AND owner_project.organization_id=$1::uuid
		  ))
		ON CONFLICT (organization_id) DO UPDATE SET
			plan_code = EXCLUDED.plan_code,
			status = 'active',
			monthly_publication_limit = EXCLUDED.monthly_publication_limit,
			storage_limit_bytes = EXCLUDED.storage_limit_bytes,
			features = EXCLUDED.features,
			renews_at = NULL,
			updated_at = now()`, organizationID, planCode)
	if err != nil {
		return Entitlement{}, err
	}
	if result.RowsAffected() == 0 {
		return Entitlement{}, ErrPlanNotAvailable
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_entitlements (
			project_id,plan_code,status,seat_limit,monthly_publication_limit,
			storage_limit_bytes,features,renews_at
		)
		SELECT project.id,entitlement.plan_code,entitlement.status,NULL,
		       entitlement.monthly_publication_limit,entitlement.storage_limit_bytes,
		       entitlement.features,entitlement.renews_at
		FROM projects AS project
		JOIN organization_entitlements AS entitlement ON entitlement.organization_id=project.organization_id
		WHERE project.organization_id=$1::uuid
		ON CONFLICT (project_id) DO UPDATE SET
			plan_code=EXCLUDED.plan_code,status=EXCLUDED.status,seat_limit=NULL,
			monthly_publication_limit=EXCLUDED.monthly_publication_limit,
			storage_limit_bytes=EXCLUDED.storage_limit_bytes,features=EXCLUDED.features,
			renews_at=EXCLUDED.renews_at,updated_at=now()`, organizationID); err != nil {
		return Entitlement{}, err
	}
	entitlement, err := scanEntitlement(tx.QueryRow(ctx, entitlementSelectSQL, projectID))
	if err != nil {
		return Entitlement{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "entitlement.updated", "project_entitlement", &projectID, map[string]any{
		"previousPlanCode": previousPlan, "planCode": planCode,
	}); err != nil {
		return Entitlement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Entitlement{}, err
	}
	return entitlement, nil
}

const entitlementSelectSQL = `
	SELECT entitlement.project_id::text, entitlement.plan_code, plan.name,
	       entitlement.status, entitlement.seat_limit,
	       entitlement.monthly_publication_limit, entitlement.storage_limit_bytes,
	       entitlement.features, entitlement.renews_at, entitlement.updated_at
	FROM project_entitlements AS entitlement
	JOIN plan_catalog AS plan ON plan.code = entitlement.plan_code
	WHERE entitlement.project_id = $1::uuid`

func lockProject(ctx context.Context, tx pgx.Tx, projectID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM projects WHERE id = $1::uuid FOR UPDATE`, projectID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func requireAnotherOwner(ctx context.Context, tx pgx.Tx, projectID, excludingUserID string) error {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM project_members
			WHERE project_id = $1::uuid AND user_id <> $2::uuid
			  AND role = 'owner' AND status = 'active'
		)`, projectID, excludingUserID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrLastOwner
	}
	return nil
}

func getTeamMember(ctx context.Context, tx pgx.Tx, projectID, userID string) (TeamMember, error) {
	member, err := scanTeamMember(tx.QueryRow(ctx, `
		SELECT users.id::text, users.email, users.display_name, users.status,
		       member.role, member.permissions, member.status, member.created_at
		FROM project_members AS member
		JOIN users ON users.id = member.user_id
		WHERE member.project_id = $1::uuid AND member.user_id = $2::uuid`, projectID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TeamMember{}, ErrNotFound
	}
	return member, err
}

func recordAudit(ctx context.Context, tx pgx.Tx, projectID, actorID, action, entityType string, entityID *string, metadata map[string]any) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6)`,
		projectID, actorID, action, entityType, entityID, metadata)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTeamMember(scanner rowScanner) (TeamMember, error) {
	var member TeamMember
	err := scanner.Scan(
		&member.UserID, &member.Email, &member.DisplayName, &member.UserStatus,
		&member.Role, &member.Permissions, &member.MembershipStatus, &member.CreatedAt,
	)
	if member.Permissions == nil {
		member.Permissions = map[string]any{}
	}
	return member, err
}

func scanPlan(scanner rowScanner) (Plan, error) {
	var plan Plan
	err := scanner.Scan(
		&plan.Code, &plan.OwnerProjectID, &plan.Name, &plan.Description,
		&plan.PriceCents, &plan.Currency, &plan.BillingInterval, &plan.SeatLimit,
		&plan.MonthlyPublicationLimit, &plan.StorageLimitBytes, &plan.Features,
		&plan.IsActive, &plan.IsSystem, &plan.CreatedAt, &plan.UpdatedAt,
	)
	if plan.Features == nil {
		plan.Features = map[string]any{}
	}
	return plan, err
}

func scanEntitlement(scanner rowScanner) (Entitlement, error) {
	var entitlement Entitlement
	err := scanner.Scan(
		&entitlement.ProjectID, &entitlement.PlanCode, &entitlement.PlanName,
		&entitlement.Status, &entitlement.SeatLimit,
		&entitlement.MonthlyPublicationLimit, &entitlement.StorageLimitBytes,
		&entitlement.Features, &entitlement.RenewsAt, &entitlement.UpdatedAt,
	)
	if entitlement.Features == nil {
		entitlement.Features = map[string]any{}
	}
	return entitlement, err
}
