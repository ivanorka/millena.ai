package operations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryProcessesDueWorkAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPERATIONS_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer pool.Close()

	suffix := time.Now().UTC().UnixNano()
	var projectID string
	err = pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug)
		VALUES ('Operations integration', $1)
		RETURNING id::text`, fmt.Sprintf("operations-integration-%d", suffix)).Scan(&projectID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
	}()
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'unlimited', 'active',
		        '{"automations":true,"aiAgents":true,"auditLog":true}'::jsonb)`, projectID); err != nil {
		t.Fatalf("create project entitlement: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO social_connections (
			project_id, provider, mode, account_handle, display_name, status
		)
		VALUES
			($1::uuid, 'linkedin', 'sandbox', '@operations', 'Operations sandbox', 'connected'),
			($1::uuid, 'facebook', 'oauth', '@external', 'External disabled', 'connected')`, projectID); err != nil {
		t.Fatalf("create social connections: %v", err)
	}

	successJob := createDuePublicationFixture(t, ctx, pool, projectID, "linkedin")
	missingConnectionJob := createDuePublicationFixture(t, ctx, pool, projectID, "instagram")
	externalModeJob := createDuePublicationFixture(t, ctx, pool, projectID, "facebook")
	sentDelivery := createDueNewsletterFixture(t, ctx, pool, projectID, true)
	failedDelivery := createDueNewsletterFixture(t, ctx, pool, projectID, false)

	var automationRuleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, description, kind, channel, enabled,
			review_policy, schedule_rule, configuration, next_run_at
		)
		VALUES ($1::uuid, 'integration_gap', 'Integration gap', 'Create a calendar suggestion.',
		        'calendar_gap', 'linkedin', true, 'always', 'gap:1d', '{}'::jsonb,
		        now() - interval '2 days')
		RETURNING id::text`, projectID).Scan(&automationRuleID)
	if err != nil {
		t.Fatalf("create automation rule: %v", err)
	}
	var automaticRuleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, description, kind, channel, enabled,
			review_policy, schedule_rule, configuration, next_run_at
		)
		VALUES ($1::uuid, 'integration_automatic', 'Integration automatic',
		        'Create an approved content effect.', 'channel', 'linkedin', true,
		        'automatic', 'FREQ=DAILY', '{}'::jsonb, now() - interval '1 day')
		RETURNING id::text`, projectID).Scan(&automaticRuleID)
	if err != nil {
		t.Fatalf("create automatic automation rule: %v", err)
	}

	repository := NewRepository(pool)
	result, err := repository.ProcessDue(ctx, 20)
	if err != nil {
		t.Fatalf("process due work: %v", err)
	}
	if result.AutomationsRun != 2 || result.PublicationsSucceeded != 1 || result.PublicationsFailed != 2 ||
		result.NewslettersSent != 1 || result.NewslettersFailed != 1 {
		t.Fatalf("unexpected batch result: %+v", result)
	}

	assertPublicationState(t, ctx, pool, successJob, "succeeded", "published", "published")
	assertPublicationState(t, ctx, pool, missingConnectionJob, "failed", "failed", "failed")
	assertPublicationState(t, ctx, pool, externalModeJob, "failed", "failed", "failed")

	var socialPostCount, socialPublicationCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*), (
			SELECT count(*)
			FROM social_publications AS publication
			JOIN social_posts AS post ON post.id = publication.social_post_id
			WHERE post.content_variant_id = $1::uuid
		)
		FROM social_posts
		WHERE content_variant_id = $1::uuid`, successJob.variantID).Scan(&socialPostCount, &socialPublicationCount)
	if err != nil || socialPostCount != 1 || socialPublicationCount != 1 {
		t.Fatalf("linked social records: posts=%d publications=%d err=%v", socialPostCount, socialPublicationCount, err)
	}

	assertNewsletterState(t, ctx, pool, sentDelivery, "sent", "published", 1, "sandbox://newsletter/")
	assertNewsletterState(t, ctx, pool, failedDelivery, "failed", "failed", 0, "")

	var runCount int
	var nextRunAt time.Time
	var scheduledEffects int
	err = pool.QueryRow(ctx, `
		SELECT run_count, next_run_at,
		       (SELECT count(*) FROM calendar_items
		        WHERE project_id = $1::uuid
		          AND metadata->>'automationRuleId' = $2
		          AND metadata->>'scheduledRun' = 'true'
		          AND metadata->>'reviewPolicy' = 'always'
		          AND metadata->>'status' = 'suggestion'
		          AND status = 'suggestion')
		FROM automation_rules
		WHERE id = $2::uuid`, projectID, automationRuleID).Scan(&runCount, &nextRunAt, &scheduledEffects)
	if err != nil || runCount != 1 || !nextRunAt.After(time.Now()) || scheduledEffects != 1 {
		t.Fatalf("automation state: runs=%d next=%v effects=%d err=%v", runCount, nextRunAt, scheduledEffects, err)
	}
	var approvedEffects int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM content_items
		WHERE project_id = $1::uuid
		  AND metadata->>'automationRuleId' = $2
		  AND metadata->>'scheduledRun' = 'true'
		  AND metadata->>'reviewPolicy' = 'automatic'
		  AND metadata->>'status' = 'approved'
		  AND status = 'approved'`, projectID, automaticRuleID).Scan(&approvedEffects)
	if err != nil || approvedEffects != 1 {
		t.Fatalf("automatic review effect count=%d err=%v", approvedEffects, err)
	}

	second, err := repository.ProcessDue(ctx, 20)
	if err != nil || second.Total() != 0 {
		t.Fatalf("idempotent second pass: result=%+v err=%v", second, err)
	}
	var secondSocialPostCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM social_posts WHERE content_variant_id = $1::uuid`, successJob.variantID).Scan(&secondSocialPostCount); err != nil || secondSocialPostCount != 1 {
		t.Fatalf("idempotent social post count=%d err=%v", secondSocialPostCount, err)
	}

	var gatedRuleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, kind, enabled, review_policy,
			schedule_rule, configuration, next_run_at
		)
		VALUES ($1::uuid, 'plan_gated', 'Plan gated automation', 'custom', true,
		        'always', 'FREQ=DAILY', '{}'::jsonb, now() - interval '1 minute')
		RETURNING id::text`, projectID).Scan(&gatedRuleID)
	if err != nil {
		t.Fatalf("create plan-gated automation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET features = jsonb_set(features, '{automations}', 'false'::jsonb)
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("disable automation entitlement: %v", err)
	}
	gatedPass, err := repository.ProcessDue(ctx, 20)
	if err != nil || gatedPass.Total() != 0 {
		t.Fatalf("disabled plan feature processed work: result=%+v err=%v", gatedPass, err)
	}
	var gatedRuns int
	if err := pool.QueryRow(ctx, `SELECT run_count FROM automation_rules WHERE id = $1::uuid`, gatedRuleID).Scan(&gatedRuns); err != nil || gatedRuns != 0 {
		t.Fatalf("plan-gated automation run_count=%d err=%v", gatedRuns, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET features = jsonb_set(features, '{automations}', '"invalid"'::jsonb)
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("set malformed legacy feature value: %v", err)
	}
	legacyPass, err := repository.ProcessDue(ctx, 20)
	if err != nil || legacyPass.Total() != 0 {
		t.Fatalf("malformed legacy feature must be safely treated as disabled: result=%+v err=%v", legacyPass, err)
	}

	var systemAuditCount int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM audit_events
		WHERE project_id = $1::uuid AND actor_id IS NULL
		  AND action IN (
			'automation_rule.scheduled_run', 'publication_job.succeeded',
			'publication_job.failed', 'newsletter_delivery.sent', 'newsletter_delivery.failed'
		  )`, projectID).Scan(&systemAuditCount)
	if err != nil || systemAuditCount != 7 {
		t.Fatalf("system audit count=%d err=%v", systemAuditCount, err)
	}
}

type publicationFixture struct {
	jobID, variantID, contentItemID, calendarItemID string
}

func createDuePublicationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID, channel string) publicationFixture {
	t.Helper()
	var fixture publicationFixture
	err := pool.QueryRow(ctx, `
		INSERT INTO content_items (
			project_id, kind, status, title, body, channels, scheduled_for
		)
		VALUES ($1::uuid, 'social', 'scheduled', $2, $3, ARRAY[$4], now() - interval '1 minute')
		RETURNING id::text`, projectID, "Integration "+channel,
		"Sandbox body for "+channel, channel).Scan(&fixture.contentItemID)
	if err != nil {
		t.Fatalf("create %s content: %v", channel, err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO content_variants (
			content_item_id, channel, locale, title, body, status, scheduled_for
		)
		VALUES ($1::uuid, $2, 'hr', $3, $4, 'scheduled', now() - interval '1 minute')
		RETURNING id::text`, fixture.contentItemID, channel, "Integration "+channel,
		"Sandbox body for "+channel).Scan(&fixture.variantID)
	if err != nil {
		t.Fatalf("create %s variant: %v", channel, err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO publication_jobs (
			project_id, content_variant_id, status, scheduled_for
		)
		VALUES ($1::uuid, $2::uuid, 'pending', now() - interval '1 minute')
		RETURNING id::text`, projectID, fixture.variantID).Scan(&fixture.jobID)
	if err != nil {
		t.Fatalf("create %s publication job: %v", channel, err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO calendar_items (
			project_id, title, channel, status, scheduled_for,
			content_item_id, content_variant_id, publication_job_id
		)
		VALUES ($1::uuid, $2, $3, 'scheduled', now() - interval '1 minute',
		        $4::uuid, $5::uuid, $6::uuid)
		RETURNING id::text`, projectID, "Integration "+channel, channel,
		fixture.contentItemID, fixture.variantID, fixture.jobID).Scan(&fixture.calendarItemID)
	if err != nil {
		t.Fatalf("create %s calendar item: %v", channel, err)
	}
	return fixture
}

type newsletterFixture struct {
	deliveryID, variantID, contentItemID, calendarItemID string
}

func createDueNewsletterFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID string, withRecipient bool) newsletterFixture {
	t.Helper()
	var fixture newsletterFixture
	var listID string
	err := pool.QueryRow(ctx, `
		INSERT INTO audience_lists (project_id, name)
		VALUES ($1::uuid, $2)
		RETURNING id::text`, projectID, fmt.Sprintf("Integration list %d", time.Now().UTC().UnixNano())).Scan(&listID)
	if err != nil {
		t.Fatalf("create audience list: %v", err)
	}
	if withRecipient {
		_, err = pool.Exec(ctx, `
			INSERT INTO audience_contacts (
				project_id, list_id, email, source, status, consent, subscribed_at
			)
			VALUES ($1::uuid, $2::uuid, $3, 'manual', 'active', true, now())`,
			projectID, listID, fmt.Sprintf("operations-%d@example.test", time.Now().UTC().UnixNano()))
		if err != nil {
			t.Fatalf("create audience contact: %v", err)
		}
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO content_items (
			project_id, kind, status, title, body, channels, scheduled_for
		)
		VALUES ($1::uuid, 'newsletter', 'scheduled', 'Integration newsletter',
		        'Newsletter body', ARRAY['newsletter'], now() - interval '1 minute')
		RETURNING id::text`, projectID).Scan(&fixture.contentItemID)
	if err != nil {
		t.Fatalf("create newsletter content: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO content_variants (
			content_item_id, channel, locale, title, summary, body, status, scheduled_for,
			metadata
		)
		VALUES ($1::uuid, 'newsletter', 'hr', 'Integration subject', '',
		        'Newsletter body', 'scheduled', now() - interval '1 minute',
		        '{"newsletterQueue":true}'::jsonb)
		RETURNING id::text`, fixture.contentItemID).Scan(&fixture.variantID)
	if err != nil {
		t.Fatalf("create newsletter variant: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO newsletter_deliveries (
			project_id, content_item_id, content_variant_id, list_id, mode, status,
			subject, scheduled_for
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'sandbox', 'scheduled',
		        'Integration subject', now() - interval '1 minute')
		RETURNING id::text`, projectID, fixture.contentItemID, fixture.variantID, listID).Scan(&fixture.deliveryID)
	if err != nil {
		t.Fatalf("create newsletter delivery: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO calendar_items (
			project_id, title, channel, status, scheduled_for,
			content_item_id, content_variant_id, metadata
		)
		VALUES ($1::uuid, 'Integration subject', 'newsletter', 'scheduled',
		        now() - interval '1 minute', $2::uuid, $3::uuid,
		        jsonb_build_object('newsletterQueue', true, 'newsletterDeliveryId', $4::text))
		RETURNING id::text`, projectID, fixture.contentItemID, fixture.variantID,
		fixture.deliveryID).Scan(&fixture.calendarItemID)
	if err != nil {
		t.Fatalf("create newsletter calendar item: %v", err)
	}
	return fixture
}

func assertPublicationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture publicationFixture, jobStatus, variantStatus, calendarStatus string) {
	t.Helper()
	var gotJob, gotVariant, gotCalendar, gotContent string
	var attempts int
	err := pool.QueryRow(ctx, `
		SELECT job.status, job.attempt_count, variant.status, calendar.status, item.status
		FROM publication_jobs AS job
		JOIN content_variants AS variant ON variant.id = job.content_variant_id
		JOIN content_items AS item ON item.id = variant.content_item_id
		JOIN calendar_items AS calendar ON calendar.publication_job_id = job.id
		WHERE job.id = $1::uuid`, fixture.jobID).Scan(
		&gotJob, &attempts, &gotVariant, &gotCalendar, &gotContent,
	)
	if err != nil || gotJob != jobStatus || attempts != 1 || gotVariant != variantStatus ||
		gotCalendar != calendarStatus || gotContent != variantStatus {
		t.Fatalf("publication state job=%q attempts=%d variant=%q calendar=%q content=%q err=%v",
			gotJob, attempts, gotVariant, gotCalendar, gotContent, err)
	}
}

func assertNewsletterState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture newsletterFixture, wantStatus, wantChildStatus string, wantRecipients int, referencePrefix string) {
	t.Helper()
	var status, contentStatus, variantStatus, calendarStatus string
	var recipients int
	var reference *string
	err := pool.QueryRow(ctx, `
		SELECT delivery.status, delivery.recipient_count,
		       delivery.external_reference, item.status, variant.status, calendar.status
		FROM newsletter_deliveries AS delivery
		JOIN content_items AS item ON item.id = delivery.content_item_id
		JOIN content_variants AS variant ON variant.id = delivery.content_variant_id
		JOIN calendar_items AS calendar ON calendar.content_variant_id = variant.id
		WHERE delivery.id = $1::uuid`, fixture.deliveryID).Scan(
		&status, &recipients, &reference, &contentStatus, &variantStatus, &calendarStatus,
	)
	if err != nil || status != wantStatus || recipients != wantRecipients ||
		contentStatus != wantChildStatus || variantStatus != wantChildStatus || calendarStatus != wantChildStatus {
		t.Fatalf("newsletter state delivery=%q recipients=%d reference=%v content=%q variant=%q calendar=%q err=%v",
			status, recipients, reference, contentStatus, variantStatus, calendarStatus, err)
	}
	if referencePrefix == "" {
		if reference != nil {
			t.Fatalf("unexpected external reference: %q", *reference)
		}
	} else if reference == nil || len(*reference) < len(referencePrefix) || (*reference)[:len(referencePrefix)] != referencePrefix {
		t.Fatalf("external reference %v does not start with %q", reference, referencePrefix)
	}
}
