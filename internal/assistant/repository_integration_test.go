package assistant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersonaContextAndAutomationEntitlementAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("ASSISTANT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ASSISTANT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UTC().UnixNano()
	var userID, projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Assistant integration', 'active')
		RETURNING id::text`, fmt.Sprintf("assistant-%d@example.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, status)
		VALUES ('Assistant integration', $1, 'en', 'active')
		RETURNING id::text`, fmt.Sprintf("assistant-integration-%d", suffix)).Scan(&projectID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'starter', 'active', '{"aiAgents":true,"automations":false}'::jsonb)`, projectID); err != nil {
		t.Fatalf("seed assistant entitlement: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_personas (
			project_id, name, description, demographics, is_primary, created_by
		) VALUES (
			$1::uuid, 'Voditeljice operacija',
			'Vode promjene procesa i trebaju izvedive korake',
			'B2B organizacije sa 100–500 zaposlenih', true, $2::uuid
		)`, projectID, userID); err != nil {
		t.Fatalf("seed assistant persona: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, kind, channel, enabled, review_policy, created_by
		) VALUES (
			$1::uuid, 'linkedin', 'LinkedIn pravilo', 'channel', 'linkedin', true,
			'conditional', $2::uuid
		)`, projectID, userID); err != nil {
		t.Fatalf("seed assistant automation: %v", err)
	}

	repository := NewRepository(pool)
	strategy, err := repository.Strategy(ctx, projectID)
	if err != nil || len(strategy.Personas) != 1 || !strategy.Personas[0].IsPrimary {
		t.Fatalf("load persona strategy context: strategy=%+v err=%v", strategy, err)
	}
	thread, err := repository.CreateThread(ctx, projectID, userID, CreateThreadInput{
		Title: "Assistant integration", Channel: "app",
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	service := NewService(repository, nil)
	created, err := service.Send(ctx, projectID, thread.ID, userID, "Napravi newsletter about a new process", nil)
	if err != nil || created.CreatedContentID == nil {
		t.Fatalf("create persona-aware draft: result=%+v err=%v", created, err)
	}
	var draftBody string
	if err := pool.QueryRow(ctx, `
		SELECT body FROM content_items
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, *created.CreatedContentID).Scan(&draftBody); err != nil {
		t.Fatalf("load generated draft: %v", err)
	}
	for _, expected := range []string{"Subject:", "Assistant integration", "Voditeljice operacija", "promjene procesa", "100–500 zaposlenih"} {
		if !strings.Contains(draftBody, expected) {
			t.Fatalf("generated assistant draft does not contain persona detail %q: %s", expected, draftBody)
		}
	}
	var variantLocale string
	if err := pool.QueryRow(ctx, `
		SELECT locale FROM content_variants
		WHERE content_item_id = $1::uuid`, *created.CreatedContentID).Scan(&variantLocale); err != nil {
		t.Fatalf("load assistant content variant: %v", err)
	}
	if variantLocale != "en" {
		t.Fatalf("assistant variant locale = %q, expected en", variantLocale)
	}

	available, err := repository.AutomationFeatureEnabled(ctx, projectID)
	if err != nil || available {
		t.Fatalf("expected automation capability to be unavailable: available=%v err=%v", available, err)
	}
	_, err = service.Send(ctx, projectID, thread.ID, userID, "Isključi LinkedIn automatizaciju", nil)
	if !errors.Is(err, ErrAutomationUnavailable) {
		t.Fatalf("expected entitlement rejection, got %v", err)
	}
	var ruleEnabled bool
	if err := pool.QueryRow(ctx, `
		SELECT enabled FROM automation_rules
		WHERE project_id = $1::uuid AND rule_key = 'linkedin'`, projectID).Scan(&ruleEnabled); err != nil {
		t.Fatalf("load automation rule: %v", err)
	}
	if !ruleEnabled {
		t.Fatal("assistant mutated an automation while the feature was disabled")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET features = jsonb_set(features, '{automations}', 'true'::jsonb)
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("enable automation entitlement: %v", err)
	}
	updated, err := service.Send(ctx, projectID, thread.ID, userID, "Isključi LinkedIn automatizaciju", nil)
	if err != nil || updated.AffectedRuleID == nil {
		t.Fatalf("toggle entitled automation: result=%+v err=%v", updated, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT enabled FROM automation_rules
		WHERE project_id = $1::uuid AND rule_key = 'linkedin'`, projectID).Scan(&ruleEnabled); err != nil {
		t.Fatalf("reload automation rule: %v", err)
	}
	if ruleEnabled {
		t.Fatal("entitled assistant automation toggle was not persisted")
	}
}
