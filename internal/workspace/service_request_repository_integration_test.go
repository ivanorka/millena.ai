package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServiceRequestUpdateLifecycleAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("WORKSPACE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("WORKSPACE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UnixNano()
	userID := createServiceRequestTestUser(t, ctx, pool, fmt.Sprintf("service-request-%d@example.test", suffix))
	projectID := createServiceRequestTestProject(t, ctx, pool, fmt.Sprintf("service-request-%d", suffix))
	otherProjectID := createServiceRequestTestProject(t, ctx, pool, fmt.Sprintf("service-request-other-%d", suffix))
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM projects WHERE id IN ($1::uuid, $2::uuid)`, projectID, otherProjectID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = $1::uuid`, userID)
	})

	repository := NewRepository(pool)
	created, err := repository.CreateServiceRequest(ctx, projectID, userID, ServiceRequestInput{
		RequestType: "support",
		Summary:     "Initial summary",
		Metadata:    map[string]any{"source": "integration"},
	})
	if err != nil {
		t.Fatalf("create service request: %v", err)
	}
	if created.Metadata["priority"] != "standard" {
		t.Fatalf("project without priority entitlement must create a standard request: %+v", created.Metadata)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'unlimited', 'active', '{"prioritySupport":true}'::jsonb)`, projectID); err != nil {
		t.Fatalf("create priority entitlement: %v", err)
	}
	priorityRequest, err := repository.CreateServiceRequest(ctx, projectID, userID, ServiceRequestInput{
		RequestType: "website_proposal", Summary: "Priority request", Metadata: map[string]any{"priority": "standard"},
	})
	if err != nil || priorityRequest.Metadata["priority"] != "priority" {
		t.Fatalf("priority entitlement should mark the request: request=%+v err=%v", priorityRequest, err)
	}
	requests, err := repository.ListServiceRequests(ctx, projectID, "")
	if err != nil || len(requests) < 2 || requests[0].ID != priorityRequest.ID {
		t.Fatalf("priority request should lead the service queue: requests=%+v err=%v", requests, err)
	}

	updatedSummary := "Updated summary"
	updatedMetadata := map[string]any{"source": "integration", "priority": "high"}
	updated, err := repository.UpdateServiceRequest(ctx, projectID, created.ID, userID, ServiceRequestUpdateInput{
		Status: "in_progress", Summary: &updatedSummary, Metadata: &updatedMetadata,
	})
	if err != nil {
		t.Fatalf("update service request: %v", err)
	}
	if updated.ProjectID != projectID || updated.Status != "in_progress" || updated.Summary != updatedSummary || updated.Metadata["priority"] != "priority" {
		t.Fatalf("unexpected updated request: %+v", updated)
	}

	completed, err := repository.UpdateServiceRequest(ctx, projectID, created.ID, userID, ServiceRequestUpdateInput{Status: "completed"})
	if err != nil {
		t.Fatalf("complete service request: %v", err)
	}
	if completed.Status != "completed" || completed.Summary != updatedSummary || completed.Metadata["priority"] != "priority" {
		t.Fatalf("omitted fields were not preserved: %+v", completed)
	}

	_, err = repository.UpdateServiceRequest(ctx, otherProjectID, created.ID, userID, ServiceRequestUpdateInput{Status: "cancelled"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-project update to return ErrNotFound, got %v", err)
	}

	var auditCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM audit_events
		WHERE project_id = $1::uuid AND actor_id = $2::uuid
		  AND action = 'service_request.updated' AND entity_type = 'service_request'
		  AND entity_id = $3::uuid
		  AND metadata->>'previousStatus' = 'in_progress'
		  AND metadata->>'status' = 'completed'`, projectID, userID, created.ID).Scan(&auditCount)
	if err != nil || auditCount != 1 {
		t.Fatalf("expected lifecycle audit event, count=%d err=%v", auditCount, err)
	}
}

func createServiceRequestTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Service request integration', 'active')
		RETURNING id::text`, email).Scan(&id); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return id
}

func createServiceRequestTestProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, status)
		VALUES ('Service request integration', $1, 'hr', 'active')
		RETURNING id::text`, slug).Scan(&id); err != nil {
		t.Fatalf("create test project: %v", err)
	}
	return id
}
