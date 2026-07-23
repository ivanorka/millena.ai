package organizations

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound        = errors.New("organization member not found")
	ErrForbidden       = errors.New("organization administrator required")
	ErrMemberExists    = errors.New("organization member already exists")
	ErrUserUnavailable = errors.New("user account is unavailable")
	ErrSelfRemoval     = errors.New("administrator cannot remove own organization account")
	ErrLastOwner       = errors.New("organization must retain an active owner")
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

func (r *Repository) Detail(ctx context.Context, projectID, actorID string) (Detail, error) {
	if r == nil {
		return Detail{}, errors.New("organization repository is unavailable")
	}
	var detail Detail
	err := r.pool.QueryRow(ctx, `
		SELECT organization.id::text, organization.name, organization.slug, organization.status,
		       actor.role, entitlement.plan_code, plan.name,
		       entitlement.monthly_publication_limit
		FROM projects AS project
		JOIN organizations AS organization ON organization.id=project.organization_id
		JOIN organization_members AS actor
		  ON actor.organization_id=organization.id AND actor.user_id=$2::uuid AND actor.status='active'
		JOIN organization_entitlements AS entitlement ON entitlement.organization_id=organization.id
		JOIN plan_catalog AS plan ON plan.code=entitlement.plan_code
		WHERE project.id=$1::uuid`, projectID, actorID).Scan(
		&detail.ID, &detail.Name, &detail.Slug, &detail.Status, &detail.CurrentUserRole,
		&detail.PlanCode, &detail.PlanName, &detail.MonthlyPublicationLimit,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrForbidden
	}
	if err != nil {
		return Detail{}, err
	}
	if detail.CurrentUserRole != "owner" && detail.CurrentUserRole != "admin" {
		return Detail{}, ErrForbidden
	}
	rows, err := r.pool.Query(ctx, `
		SELECT account.id::text, account.email, account.display_name, account.status,
		       member.role, member.status,
		       (SELECT count(*)::int
		        FROM project_members AS access
		        JOIN projects AS accessible_project ON accessible_project.id=access.project_id
		        WHERE accessible_project.organization_id=member.organization_id
		          AND access.user_id=member.user_id AND access.status='active'),
		       current_access.role, current_access.status, member.created_at
		FROM organization_members AS member
		JOIN users AS account ON account.id=member.user_id
		LEFT JOIN project_members AS current_access
		  ON current_access.project_id=$2::uuid AND current_access.user_id=member.user_id
		WHERE member.organization_id=$1::uuid
		ORDER BY CASE member.role WHEN 'owner' THEN 1 WHEN 'admin' THEN 2 ELSE 3 END,
		         CASE member.status WHEN 'active' THEN 1 ELSE 2 END,
		         lower(account.display_name), lower(account.email)`, detail.ID, projectID)
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()
	detail.Members = make([]Member, 0)
	for rows.Next() {
		var member Member
		if err := rows.Scan(
			&member.UserID, &member.Email, &member.DisplayName, &member.UserStatus,
			&member.Role, &member.Status, &member.ProjectCount,
			&member.CurrentProjectRole, &member.CurrentProjectStatus, &member.CreatedAt,
		); err != nil {
			return Detail{}, err
		}
		detail.Members = append(detail.Members, member)
	}
	return detail, rows.Err()
}

func (r *Repository) CreateMember(ctx context.Context, projectID, actorID string, input CreateMemberInput) (Member, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.TempPassword), 12)
	if err != nil {
		return Member{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Member{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	organizationID, err := requireManager(ctx, tx, projectID, actorID)
	if err != nil {
		return Member{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(lower($1), 0))`, input.Email); err != nil {
		return Member{}, err
	}
	var userID, userStatus string
	err = tx.QueryRow(ctx, `SELECT id::text,status FROM users WHERE lower(email)=lower($1) FOR UPDATE`, input.Email).Scan(&userID, &userStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO users (email,display_name,status,password_hash)
			VALUES (lower($1),$2,'active',$3)
			RETURNING id::text,status`, input.Email, input.DisplayName, string(passwordHash)).Scan(&userID, &userStatus)
	}
	if err != nil {
		return Member{}, err
	}
	if userStatus != "active" {
		return Member{}, ErrUserUnavailable
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO organization_members (organization_id,user_id,role,status)
		VALUES ($1::uuid,$2::uuid,$3,'active')
		ON CONFLICT (organization_id,user_id) DO NOTHING`, organizationID, userID, input.Role)
	if err != nil {
		return Member{}, err
	}
	if result.RowsAffected() == 0 {
		return Member{}, ErrMemberExists
	}
	if input.GrantProjectAccess {
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_members (project_id,user_id,role,permissions,status)
			VALUES ($1::uuid,$2::uuid,$3,'{}'::jsonb,'active')
			ON CONFLICT (project_id,user_id) DO UPDATE
			SET role=EXCLUDED.role,status='active'`, projectID, userID, input.ProjectRole); err != nil {
			return Member{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id,actor_id,action,entity_type,entity_id,metadata)
		VALUES ($1::uuid,$2::uuid,'organization.member_created','organization_member',$3::uuid,
		        jsonb_build_object('email',$4::text,'organizationRole',$5::text,'projectRole',$6::text))`,
		projectID, actorID, userID, input.Email, input.Role, input.ProjectRole); err != nil {
		return Member{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Member{}, err
	}
	detail, err := r.Detail(ctx, projectID, actorID)
	if err != nil {
		return Member{}, err
	}
	for _, member := range detail.Members {
		if member.UserID == userID {
			return member, nil
		}
	}
	return Member{}, ErrNotFound
}

func (r *Repository) UpdateMember(ctx context.Context, projectID, actorID, memberID string, input UpdateMemberInput) (Member, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Member{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	organizationID, err := requireManager(ctx, tx, projectID, actorID)
	if err != nil {
		return Member{}, err
	}
	var currentRole, currentStatus string
	if err := tx.QueryRow(ctx, `
		SELECT role,status FROM organization_members
		WHERE organization_id=$1::uuid AND user_id=$2::uuid FOR UPDATE`, organizationID, memberID).Scan(&currentRole, &currentStatus); errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	} else if err != nil {
		return Member{}, err
	}
	nextRole, nextStatus := currentRole, currentStatus
	if input.Role != nil {
		nextRole = *input.Role
	}
	if input.Status != nil {
		nextStatus = *input.Status
	}
	if actorID == memberID && nextStatus != "active" {
		return Member{}, ErrSelfRemoval
	}
	var actorRole string
	if err := tx.QueryRow(ctx, `SELECT role FROM organization_members WHERE organization_id=$1::uuid AND user_id=$2::uuid`, organizationID, actorID).Scan(&actorRole); err != nil {
		return Member{}, err
	}
	if actorRole != "owner" && (currentRole == "owner" || nextRole == "owner") {
		return Member{}, ErrForbidden
	}
	if currentRole == "owner" && currentStatus == "active" && (nextRole != "owner" || nextStatus != "active") {
		if err := requireAnotherOwner(ctx, tx, organizationID, memberID); err != nil {
			return Member{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE organization_members SET role=$3,status=$4,updated_at=now()
		WHERE organization_id=$1::uuid AND user_id=$2::uuid`, organizationID, memberID, nextRole, nextStatus); err != nil {
		return Member{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id,actor_id,action,entity_type,entity_id,metadata)
		VALUES ($1::uuid,$2::uuid,'organization.member_updated','organization_member',$3::uuid,
		        jsonb_build_object('previousRole',$4::text,'role',$5::text,'previousStatus',$6::text,'status',$7::text))`,
		projectID, actorID, memberID, currentRole, nextRole, currentStatus, nextStatus); err != nil {
		return Member{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Member{}, err
	}
	detail, err := r.Detail(ctx, projectID, actorID)
	if err != nil {
		return Member{}, err
	}
	for _, member := range detail.Members {
		if member.UserID == memberID {
			return member, nil
		}
	}
	return Member{}, ErrNotFound
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
	organizationID, err := requireManager(ctx, tx, projectID, actorID)
	if err != nil {
		return err
	}
	var role, status string
	if err := tx.QueryRow(ctx, `
		SELECT role,status FROM organization_members
		WHERE organization_id=$1::uuid AND user_id=$2::uuid FOR UPDATE`, organizationID, memberID).Scan(&role, &status); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if role == "owner" && status == "active" {
		var actorRole string
		if err := tx.QueryRow(ctx, `SELECT role FROM organization_members WHERE organization_id=$1::uuid AND user_id=$2::uuid`, organizationID, actorID).Scan(&actorRole); err != nil {
			return err
		}
		if actorRole != "owner" {
			return ErrForbidden
		}
		if err := requireAnotherOwner(ctx, tx, organizationID, memberID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM project_members AS access USING projects AS project
		WHERE access.project_id=project.id AND project.organization_id=$1::uuid
		  AND access.user_id=$2::uuid`, organizationID, memberID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM organization_members WHERE organization_id=$1::uuid AND user_id=$2::uuid`, organizationID, memberID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id,actor_id,action,entity_type,entity_id,metadata)
		VALUES ($1::uuid,$2::uuid,'organization.member_deleted','organization_member',$3::uuid,
		        jsonb_build_object('role',$4::text,'status',$5::text))`, projectID, actorID, memberID, role, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func requireManager(ctx context.Context, tx pgx.Tx, projectID, userID string) (string, error) {
	var organizationID, role string
	err := tx.QueryRow(ctx, `
		SELECT project.organization_id::text, member.role
		FROM projects AS project
		JOIN organization_members AS member
		  ON member.organization_id=project.organization_id
		 AND member.user_id=$2::uuid AND member.status='active'
		WHERE project.id=$1::uuid`, projectID, userID).Scan(&organizationID, &role)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != "owner" && role != "admin") {
		return "", ErrForbidden
	}
	return organizationID, err
}

func requireAnotherOwner(ctx context.Context, tx pgx.Tx, organizationID, excludingUserID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM organization_members
		WHERE organization_id=$1::uuid AND user_id<>$2::uuid AND role='owner' AND status='active')`,
		organizationID, excludingUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrLastOwner
	}
	return nil
}
