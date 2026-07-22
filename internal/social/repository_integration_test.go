package social

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/limits"
)

func TestConnectionEntitlementAgainstPostgres(t *testing.T) {
	pool, ctx := socialIntegrationPool(t)
	projectID := createSocialTestProject(t, ctx, pool, "active", `{"socialChannels":1}`)

	repository := NewRepository(pool)
	linkedin, err := repository.UpsertConnection(ctx, projectID, ConnectInput{
		Provider: "linkedin", Mode: "sandbox", AccountHandle: "@first", DisplayName: "First account",
	})
	if err != nil {
		t.Fatalf("create first connection: %v", err)
	}
	replacement, err := repository.UpsertConnection(ctx, projectID, ConnectInput{
		Provider: "linkedin", Mode: "sandbox", AccountHandle: "@replacement", DisplayName: "Replacement account",
	})
	if err != nil || replacement.ID != linkedin.ID || replacement.AccountHandle != "@replacement" {
		t.Fatalf("replace same provider: connection=%+v err=%v", replacement, err)
	}
	_, err = repository.UpsertConnection(ctx, projectID, ConnectInput{
		Provider: "facebook", Mode: "sandbox", AccountHandle: "@second", DisplayName: "Second account",
	})
	if !errors.Is(err, ErrSocialChannelLimitReached) {
		t.Fatalf("expected ErrSocialChannelLimitReached, got %v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET features = jsonb_set(features, '{socialChannels}', '"all"'::jsonb)
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("enable all channels: %v", err)
	}
	facebook, err := repository.UpsertConnection(ctx, projectID, ConnectInput{
		Provider: "facebook", Mode: "sandbox", AccountHandle: "@second", DisplayName: "Second account",
	})
	if err != nil {
		t.Fatalf("create connection with unlimited entitlement: %v", err)
	}
	if err := repository.Disconnect(ctx, projectID, linkedin.ID); err != nil {
		t.Fatalf("disconnect first provider: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET features = jsonb_set(features, '{socialChannels}', '1'::jsonb)
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("set finite channel limit: %v", err)
	}
	_, err = repository.UpsertConnection(ctx, projectID, ConnectInput{
		Provider: "linkedin", Mode: "sandbox", AccountHandle: "@again", DisplayName: "Reconnect account",
	})
	if !errors.Is(err, ErrSocialChannelLimitReached) {
		t.Fatalf("reconnecting must consume a slot while %s is active: %v", facebook.Provider, err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements SET status = 'past_due'
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("make entitlement inactive: %v", err)
	}
	_, err = repository.UpsertConnection(ctx, projectID, ConnectInput{
		Provider: "facebook", Mode: "sandbox", AccountHandle: "@updated", DisplayName: "Inactive update",
	})
	if !errors.Is(err, limits.ErrEntitlementInactive) {
		t.Fatalf("expected ErrEntitlementInactive, got %v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements SET status = 'active', features = '{}'::jsonb
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("remove channel entitlement: %v", err)
	}
	_, err = repository.UpsertConnection(ctx, projectID, ConnectInput{
		Provider: "facebook", Mode: "sandbox", AccountHandle: "@updated", DisplayName: "Unavailable update",
	})
	if !errors.Is(err, ErrSocialChannelsUnavailable) {
		t.Fatalf("expected ErrSocialChannelsUnavailable, got %v", err)
	}
}

func TestPublishDueSandboxSkipsInactiveEntitlementsAgainstPostgres(t *testing.T) {
	pool, ctx := socialIntegrationPool(t)
	inactiveProjectID := createSocialTestProject(t, ctx, pool, "past_due", `{"socialChannels":"all"}`)
	trialProjectID := createSocialTestProject(t, ctx, pool, "trial", `{"socialChannels":"all"}`)
	inactivePostID := createScheduledSocialPost(t, ctx, pool, inactiveProjectID, "1900-01-01T00:00:00Z")
	trialPostID := createScheduledSocialPost(t, ctx, pool, trialProjectID, "1901-01-01T00:00:00Z")

	count, err := NewRepository(pool).PublishDueSandbox(ctx, 1)
	if err != nil || count != 1 {
		t.Fatalf("PublishDueSandbox() count=%d err=%v", count, err)
	}
	assertSocialPostStatus(t, ctx, pool, inactivePostID, "scheduled")
	assertSocialPostStatus(t, ctx, pool, trialPostID, "published")
}

func socialIntegrationPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("SOCIAL_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SOCIAL_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, ctx
}

func createSocialTestProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status, features string) string {
	t.Helper()
	var projectID string
	suffix := time.Now().UTC().UnixNano()
	err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, status)
		VALUES ('Social integration', $1, 'active')
		RETURNING id::text`, fmt.Sprintf("social-integration-%d", suffix)).Scan(&projectID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		command, err := pool.Exec(cleanupCtx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		if err != nil {
			t.Errorf("clean up social integration project: %v", err)
			return
		}
		if command.RowsAffected() != 1 {
			t.Errorf("clean up social integration project: deleted %d projects", command.RowsAffected())
		}
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'unlimited', $2, $3::jsonb)`, projectID, status, features); err != nil {
		t.Fatalf("create entitlement: %v", err)
	}
	return projectID
}

func createScheduledSocialPost(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID, scheduledFor string) string {
	t.Helper()
	var connectionID string
	err := pool.QueryRow(ctx, `
		INSERT INTO social_connections (
			project_id, provider, mode, account_handle, display_name, status
		)
		VALUES ($1::uuid, 'linkedin', 'sandbox', '@worker-test', 'Worker test', 'connected')
		RETURNING id::text`, projectID).Scan(&connectionID)
	if err != nil {
		t.Fatalf("create social connection: %v", err)
	}
	var postID string
	err = pool.QueryRow(ctx, `
		INSERT INTO social_posts (project_id, body, status, scheduled_for)
		VALUES ($1::uuid, 'Scheduled integration post', 'scheduled', $2::timestamptz)
		RETURNING id::text`, projectID, scheduledFor).Scan(&postID)
	if err != nil {
		t.Fatalf("create social post: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO social_publications (
			social_post_id, social_connection_id, provider, status
		)
		VALUES ($1::uuid, $2::uuid, 'linkedin', 'scheduled')`, postID, connectionID); err != nil {
		t.Fatalf("create social publication: %v", err)
	}
	return postID
}

func assertSocialPostStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, postID, want string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM social_posts WHERE id = $1::uuid`, postID).Scan(&status); err != nil {
		t.Fatalf("load social post status: %v", err)
	}
	if status != want {
		t.Fatalf("post %s status=%q, want %q", postID, status, want)
	}
}
