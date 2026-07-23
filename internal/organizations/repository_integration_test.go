package organizations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOrganizationMemberLifecycleAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("ORGANIZATIONS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ORGANIZATIONS_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UTC().UnixNano()
	var organizationID, projectID, ownerID, memberID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO organizations (name,slug) VALUES ('Tenant integration',$1)
		RETURNING id::text`, fmt.Sprintf("tenant-integration-%d", suffix)).Scan(&organizationID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM projects WHERE id=$1::uuid`, projectID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id=$1::uuid`, organizationID)
		ids := []string{ownerID}
		if memberID != "" {
			ids = append(ids, memberID)
		}
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=ANY($1::uuid[])`, ids)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email,display_name,status) VALUES ($1,'Tenant owner','active')
		RETURNING id::text`, fmt.Sprintf("tenant-owner-%d@example.test", suffix)).Scan(&ownerID); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members (organization_id,user_id,role,status) VALUES ($1::uuid,$2::uuid,'owner','active')`, organizationID, ownerID); err != nil {
		t.Fatalf("seed tenant owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_entitlements (organization_id,plan_code,status,monthly_publication_limit,features) VALUES ($1::uuid,'starter','active',30,'{}'::jsonb)`, organizationID); err != nil {
		t.Fatalf("seed tenant entitlement: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (organization_id,name,slug) VALUES ($1::uuid,'Tenant project',$2)
		RETURNING id::text`, organizationID, fmt.Sprintf("tenant-project-%d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role,status) VALUES ($1::uuid,$2::uuid,'owner','active')`, projectID, ownerID); err != nil {
		t.Fatalf("seed project owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_entitlements (project_id,plan_code,status,seat_limit,monthly_publication_limit,features) VALUES ($1::uuid,'starter','active',NULL,30,'{}'::jsonb)`, projectID); err != nil {
		t.Fatalf("seed project entitlement: %v", err)
	}

	repository := NewRepository(pool)
	created, err := repository.CreateMember(ctx, projectID, ownerID, CreateMemberInput{
		DisplayName: "Tenant member", Email: fmt.Sprintf("tenant-member-%d@example.test", suffix),
		TempPassword: "temporary-pass", Role: "member", ProjectRole: "contributor", GrantProjectAccess: true,
	})
	if err != nil {
		t.Fatalf("create organization member: %v", err)
	}
	memberID = created.UserID
	if created.ProjectCount != 1 || created.CurrentProjectRole == nil || *created.CurrentProjectRole != "contributor" {
		t.Fatalf("created member has wrong project access: %+v", created)
	}
	suspended := "suspended"
	updated, err := repository.UpdateMember(ctx, projectID, ownerID, memberID, UpdateMemberInput{Status: &suspended})
	if err != nil || updated.Status != "suspended" {
		t.Fatalf("suspend organization member: member=%+v err=%v", updated, err)
	}
	var effectiveAccess bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM project_members AS access
			JOIN projects AS project ON project.id=access.project_id
			JOIN organization_members AS member
			  ON member.organization_id=project.organization_id AND member.user_id=access.user_id
			WHERE access.project_id=$1::uuid AND access.user_id=$2::uuid
			  AND access.status='active' AND member.status='active'
		)`, projectID, memberID).Scan(&effectiveAccess); err != nil || effectiveAccess {
		t.Fatalf("suspended organization member still has effective project access: access=%v err=%v", effectiveAccess, err)
	}
	if err := repository.DeleteMember(ctx, projectID, ownerID, memberID); err != nil {
		t.Fatalf("delete organization member: %v", err)
	}
	var membershipCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM organization_members WHERE organization_id=$1::uuid AND user_id=$2::uuid`, organizationID, memberID).Scan(&membershipCount); err != nil || membershipCount != 0 {
		t.Fatalf("organization membership still exists: count=%d err=%v", membershipCount, err)
	}
}
