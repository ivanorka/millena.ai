package auth

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOperationalSeedAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("AUTH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AUTH_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	suffix := time.Now().UTC().UnixNano()
	var userID string
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Operational seed test', 'active')
		RETURNING id::text`, fmt.Sprintf("operational-seed-%d@example.test", suffix)).Scan(&userID)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	var projectID string
	err = tx.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale)
		VALUES ('Operational seed test', $1, 'hr')
		RETURNING id::text`, fmt.Sprintf("operational-seed-%d", suffix)).Scan(&projectID)
	if err != nil {
		t.Fatalf("create test project: %v", err)
	}

	seed := newTenantOperationalWorkspaceSeed("Operational seed test")
	if err := seedOperationalWorkspace(ctx, tx, projectID, userID, seed); err != nil {
		t.Fatalf("first operational seed: %v", err)
	}
	if err := seedOperationalWorkspace(ctx, tx, projectID, userID, seed); err != nil {
		t.Fatalf("repeat operational seed: %v", err)
	}

	var profiles, rules, defaultLists, channels, threads, welcomes int
	err = tx.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM project_profiles WHERE project_id = $1::uuid),
		  (SELECT count(*) FROM automation_rules WHERE project_id = $1::uuid),
		  (SELECT count(*) FROM audience_lists WHERE project_id = $1::uuid AND is_default),
		  (SELECT count(*) FROM channel_connections WHERE project_id = $1::uuid),
		  (SELECT count(*) FROM assistant_threads WHERE project_id = $1::uuid),
		  (SELECT count(*) FROM assistant_messages WHERE project_id = $1::uuid AND body = $2)`,
		projectID, assistantWelcomeMessage).Scan(
		&profiles, &rules, &defaultLists, &channels, &threads, &welcomes,
	)
	if err != nil {
		t.Fatalf("inspect operational seed: %v", err)
	}
	if profiles != 1 || rules != 15 || defaultLists != 1 || channels != 1 || threads != 1 || welcomes != 1 {
		t.Fatalf(
			"unexpected seed counts: profiles=%d rules=%d defaultLists=%d channels=%d threads=%d welcomes=%d",
			profiles, rules, defaultLists, channels, threads, welcomes,
		)
	}

	var timezone string
	var gapRun, weeklyRun, newsletterRun time.Time
	err = tx.QueryRow(ctx, `
		SELECT profile.timezone,
		       gap_rule.next_run_at,
		       weekly_rule.next_run_at,
		       newsletter_rule.next_run_at
		FROM project_profiles AS profile
		JOIN automation_rules AS gap_rule
		  ON gap_rule.project_id = profile.project_id AND gap_rule.rule_key = 'calendar_gap'
		JOIN automation_rules AS weekly_rule
		  ON weekly_rule.project_id = profile.project_id AND weekly_rule.rule_key = 'weekly_newsletter'
		JOIN automation_rules AS newsletter_rule
		  ON newsletter_rule.project_id = profile.project_id AND newsletter_rule.rule_key = 'newsletter'
		WHERE profile.project_id = $1::uuid`, projectID).Scan(
		&timezone, &gapRun, &weeklyRun, &newsletterRun,
	)
	if err != nil {
		t.Fatalf("inspect operational schedules: %v", err)
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		t.Fatalf("load seeded timezone %q: %v", timezone, err)
	}
	if local := gapRun.In(location); local.Hour() != 10 || local.Minute() != 0 || !gapRun.After(time.Now().Add(-time.Minute)) {
		t.Fatalf("calendar gap run = %s, want a future 10:00 in %s", local, timezone)
	}
	if local := weeklyRun.In(location); local.Weekday() != time.Friday || local.Hour() != 10 || local.Minute() != 0 || !weeklyRun.After(time.Now().Add(-time.Minute)) {
		t.Fatalf("weekly run = %s, want a future Friday 10:00 in %s", local, timezone)
	}
	if !newsletterRun.Equal(weeklyRun) {
		t.Fatalf("newsletter run = %s, want shared weekly anchor %s", newsletterRun, weeklyRun)
	}
}
