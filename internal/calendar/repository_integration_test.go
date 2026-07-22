package calendar

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

func TestLinkedCalendarUpdateConsistencyAndQuotaAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("CALENDAR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CALENDAR_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UnixNano()
	var userID, projectID, itemID, variantID, calendarID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Calendar integration', 'active')
		RETURNING id::text`, fmt.Sprintf("calendar-%d@example.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, status)
		VALUES ('Calendar integration', $1, 'hr', 'active')
		RETURNING id::text`, fmt.Sprintf("calendar-integration-%d", suffix)).Scan(&projectID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (
			project_id, plan_code, status, monthly_publication_limit, features
		)
		VALUES ($1::uuid, 'unlimited', 'active', 1, '{}'::jsonb)`, projectID); err != nil {
		t.Fatalf("create entitlement: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO content_items (
			project_id, author_id, kind, status, title, summary, body, channels
		)
		VALUES ($1::uuid, $2::uuid, 'social', 'draft', 'Linked calendar content',
		        'Initial summary', 'Initial body', ARRAY['linkedin'])
		RETURNING id::text`, projectID, userID).Scan(&itemID); err != nil {
		t.Fatalf("create content item: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO content_variants (
			content_item_id, channel, locale, title, summary, body, status
		)
		VALUES ($1::uuid, 'linkedin', 'hr', 'Linked calendar content',
		        'Initial summary', 'Initial body', 'draft')
		RETURNING id::text`, itemID).Scan(&variantID); err != nil {
		t.Fatalf("create content variant: %v", err)
	}
	initialSchedule := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	if err := pool.QueryRow(ctx, `
		INSERT INTO calendar_items (
			project_id, created_by, title, summary, channel, status, scheduled_for,
			content_item_id, content_variant_id, metadata
		)
		VALUES ($1::uuid, $2::uuid, 'Linked calendar content', 'Initial summary',
		        'linkedin', 'draft', $3, $4::uuid, $5::uuid, '{"synced":true}'::jsonb)
		RETURNING id::text`, projectID, userID, initialSchedule, itemID, variantID).Scan(&calendarID); err != nil {
		t.Fatalf("create calendar item: %v", err)
	}

	repository := NewRepository(pool)
	_, err = repository.Update(ctx, projectID, calendarID, userID, SaveInput{
		Title: "Wrong channel", Summary: "Must roll back", Channel: "facebook",
		Status: "draft", ScheduledFor: initialSchedule, Metadata: map[string]any{},
	})
	if !errors.Is(err, ErrLinkedVariantChannelChange) {
		t.Fatalf("linked channel change error = %v", err)
	}
	assertLinkedCalendarState(t, ctx, pool, calendarID, variantID, "linkedin", "draft", initialSchedule)

	if _, err := pool.Exec(ctx, `
		INSERT INTO publication_consumptions (
			project_id, source_type, source_id, billing_month
		)
		VALUES ($1::uuid, 'social_post', gen_random_uuid(),
		        date_trunc('month', now() AT TIME ZONE 'UTC')::date)`, projectID); err != nil {
		t.Fatalf("fill publication quota: %v", err)
	}
	_, err = repository.Update(ctx, projectID, calendarID, userID, SaveInput{
		Title: "Quota guarded", Summary: "Must roll back", Channel: "linkedin",
		Status: "scheduled", ScheduledFor: initialSchedule, Metadata: map[string]any{},
	})
	if !errors.Is(err, limits.ErrPublicationLimitReached) {
		t.Fatalf("calendar schedule quota error = %v", err)
	}
	assertLinkedCalendarState(t, ctx, pool, calendarID, variantID, "linkedin", "draft", initialSchedule)

	if _, err := pool.Exec(ctx, `DELETE FROM publication_consumptions WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("clear quota fixture: %v", err)
	}
	scheduled, err := repository.Update(ctx, projectID, calendarID, userID, SaveInput{
		Title: "Scheduled through calendar", Summary: "One source of truth", Channel: "linkedin",
		Status: "scheduled", ScheduledFor: initialSchedule, Metadata: map[string]any{"editedIn": "calendar"},
	})
	if err != nil || scheduled.PublicationJobID == nil {
		t.Fatalf("schedule linked calendar: item=%+v err=%v", scheduled, err)
	}
	assertLinkedCalendarState(t, ctx, pool, calendarID, variantID, "linkedin", "scheduled", initialSchedule)
	assertCalendarConsumptionCount(t, ctx, pool, projectID, variantID, 1)

	rescheduledFor := initialSchedule.Add(3 * time.Hour)
	rescheduled, err := repository.Update(ctx, projectID, calendarID, userID, SaveInput{
		Title: "Rescheduled through calendar", Summary: "Same publication unit", Channel: "linkedin",
		Status: "scheduled", ScheduledFor: rescheduledFor, Metadata: map[string]any{"editedIn": "calendar"},
	})
	if err != nil || rescheduled.PublicationJobID == nil {
		t.Fatalf("reschedule linked calendar: item=%+v err=%v", rescheduled, err)
	}
	assertLinkedCalendarState(t, ctx, pool, calendarID, variantID, "linkedin", "scheduled", rescheduledFor)
	assertCalendarConsumptionCount(t, ctx, pool, projectID, variantID, 1)

	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements SET status = 'past_due'
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("disable entitlement: %v", err)
	}
	_, err = repository.Update(ctx, projectID, calendarID, userID, SaveInput{
		Title: "Blocked reschedule", Summary: "Inactive plan", Channel: "linkedin",
		Status: "scheduled", ScheduledFor: rescheduledFor.Add(time.Hour), Metadata: map[string]any{},
	})
	if !errors.Is(err, limits.ErrEntitlementInactive) {
		t.Fatalf("inactive entitlement reschedule error = %v", err)
	}
	assertLinkedCalendarState(t, ctx, pool, calendarID, variantID, "linkedin", "scheduled", rescheduledFor)
}

func assertLinkedCalendarState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	calendarID, variantID, channel, status string,
	scheduledFor time.Time,
) {
	t.Helper()
	var calendarChannel, calendarStatus, variantChannel, variantStatus string
	var calendarSchedule, variantSchedule *time.Time
	err := pool.QueryRow(ctx, `
		SELECT calendar.channel, calendar.status, calendar.scheduled_for,
		       variant.channel, variant.status, variant.scheduled_for
		FROM calendar_items AS calendar
		JOIN content_variants AS variant ON variant.id = calendar.content_variant_id
		WHERE calendar.id = $1::uuid AND variant.id = $2::uuid`, calendarID, variantID).Scan(
		&calendarChannel, &calendarStatus, &calendarSchedule,
		&variantChannel, &variantStatus, &variantSchedule,
	)
	if err != nil || calendarChannel != channel || variantChannel != channel ||
		calendarStatus != status || variantStatus != status {
		t.Fatalf("linked state calendar=%s/%s variant=%s/%s err=%v",
			calendarChannel, calendarStatus, variantChannel, variantStatus, err)
	}
	if calendarSchedule == nil || !calendarSchedule.Equal(scheduledFor) {
		t.Fatalf("calendar schedule = %v, expected %v", calendarSchedule, scheduledFor)
	}
	if status == "scheduled" {
		if variantSchedule == nil || !variantSchedule.Equal(scheduledFor) {
			t.Fatalf("variant schedule = %v, expected %v", variantSchedule, scheduledFor)
		}
	} else if variantSchedule != nil {
		t.Fatalf("draft variant schedule = %v, expected nil", variantSchedule)
	}
}

func assertCalendarConsumptionCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID, variantID string, expected int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM publication_consumptions
		WHERE project_id = $1::uuid AND source_type = 'content_variant'
		  AND source_id = $2::uuid`, projectID, variantID).Scan(&count); err != nil || count != expected {
		t.Fatalf("publication consumption count = %d, expected %d, err=%v", count, expected, err)
	}
}
