package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTeamCapacityRequiresUsableEntitlementAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("ADMIN_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ADMIN_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UTC().UnixNano()
	var projectID, ownerID, suspendedID, createdID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug)
		VALUES ('Admin entitlement integration', $1)
		RETURNING id::text`, fmt.Sprintf("admin-entitlement-%d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, target := range []*string{&ownerID, &suspendedID} {
		label := "owner"
		if target == &suspendedID {
			label = "suspended"
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO users (email, display_name, status)
			VALUES ($1, $2, 'active')
			RETURNING id::text`, fmt.Sprintf("admin-%s-%d@example.test", label, suffix), "Admin "+label).Scan(target); err != nil {
			t.Fatalf("create %s user: %v", label, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		userIDs := []string{ownerID, suspendedID}
		if createdID != "" {
			userIDs = append(userIDs, createdID)
		}
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = ANY($1::uuid[])`, userIDs)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role, status)
		VALUES ($1::uuid, $2::uuid, 'owner', 'active'),
		       ($1::uuid, $3::uuid, 'editor', 'suspended')`, projectID, ownerID, suspendedID); err != nil {
		t.Fatalf("create memberships: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, seat_limit, features)
		VALUES ($1::uuid, 'unlimited', 'past_due', 5, '{}'::jsonb)`, projectID); err != nil {
		t.Fatalf("create inactive entitlement: %v", err)
	}

	repository := NewRepository(pool)
	memberInput := CreateMemberInput{
		DisplayName: "Blocked member", Email: fmt.Sprintf("admin-new-%d@example.test", suffix),
		Role: "editor", TempPassword: "temporary-pass",
	}
	if _, err := repository.CreateMember(ctx, projectID, ownerID, memberInput); !errors.Is(err, ErrEntitlementInactive) {
		t.Fatalf("expected inactive entitlement on member creation, got %v", err)
	}
	active := "active"
	if _, err := repository.UpdateMember(ctx, projectID, ownerID, suspendedID, UpdateMemberInput{Status: &active}); !errors.Is(err, ErrEntitlementInactive) {
		t.Fatalf("expected inactive entitlement on member reactivation, got %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE project_entitlements SET status = 'trial' WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("activate trial entitlement: %v", err)
	}
	created, err := repository.CreateMember(ctx, projectID, ownerID, memberInput)
	if err != nil || created.Email != memberInput.Email || created.MembershipStatus != "active" {
		t.Fatalf("trial entitlement should allow member creation: member=%+v err=%v", created, err)
	}
	createdID = created.UserID
	if _, err := repository.UpdateMember(ctx, projectID, ownerID, suspendedID, UpdateMemberInput{Status: &active}); err != nil {
		t.Fatalf("trial entitlement should allow member reactivation: %v", err)
	}
}
