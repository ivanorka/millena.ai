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

func TestRepositoryLifecycleAgainstPostgres(t *testing.T) {
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

	fixtureSuffix := time.Now().UnixNano()
	var userID string
	err = pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Workspace integration', 'active')
		RETURNING id::text`, fmt.Sprintf("workspace-%d@example.test", fixtureSuffix)).Scan(&userID)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	var projectID string
	err = pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, status)
		VALUES ('Workspace integration', $1, 'hr', 'active')
		RETURNING id::text`, fmt.Sprintf("workspace-agent-test-%d", fixtureSuffix)).Scan(&projectID)
	if err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create test project: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role, status)
		VALUES ($1::uuid, $2::uuid, 'owner', 'active')`, projectID, userID); err != nil {
		t.Fatalf("seed test member: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (
			project_id, plan_code, status, features
		) VALUES (
			$1::uuid, 'unlimited', 'active',
			'{"aiAgents":true,"analytics":true,"automations":true,"auditLog":true}'::jsonb
		)`, projectID); err != nil {
		t.Fatalf("seed test entitlement: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, description, kind, channel, enabled,
			review_policy, schedule_rule, configuration, next_run_at, created_by
		) VALUES (
			$1::uuid, 'calendar_gap', 'Integration calendar gap',
			'Create an integration calendar suggestion.', 'calendar_gap', 'linkedin',
			true, 'always', 'gap:5d', '{"gapDays":5}'::jsonb, now() + interval '1 day', $2::uuid
		)`, projectID, userID); err != nil {
		t.Fatalf("seed test automation: %v", err)
	}

	repository := NewRepository(pool)
	profile, err := repository.GetProfile(ctx, projectID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profileInput := ProfileInput{
		ProjectName: "Workspace test", CompanyName: "Workspace company",
		CompanyDescription: "Integrated communications company used by the repository test.",
		WebsiteURL:         "https://example.test", PrimaryLanguage: "hr",
		Timezone: "Europe/Zagreb", SocialPostsPerWeek: 4,
		NewsletterCadence: "weekly", SignupHeadline: "Novosti",
	}
	profile, err = repository.SaveProfile(ctx, projectID, userID, profileInput)
	if err != nil || profile.CompanyName != "Workspace company" || profile.CompanyDescription == "" {
		t.Fatalf("save profile: profile=%+v err=%v", profile, err)
	}

	rules, err := repository.ListAutomationRules(ctx, projectID)
	if err != nil || len(rules) == 0 {
		t.Fatalf("list automation rules: count=%d err=%v", len(rules), err)
	}
	var runnable AutomationRule
	for _, rule := range rules {
		if rule.Kind == "calendar_gap" {
			runnable = rule
			break
		}
	}
	if runnable.ID == "" {
		t.Fatal("calendar gap seed rule was not found")
	}
	run, err := repository.RunAutomationRule(ctx, projectID, runnable.ID, userID)
	if err != nil || run.EffectType != "calendar_item" || run.Rule.RunCount != runnable.RunCount+1 {
		t.Fatalf("run automation: result=%+v err=%v", run, err)
	}
	var calendarReviewStatus, calendarReviewPolicy, calendarMetadataStatus string
	if err := pool.QueryRow(ctx, `
		SELECT status, metadata->>'reviewPolicy', metadata->>'status'
		FROM calendar_items
		WHERE id = $1::uuid`, run.EffectID).Scan(
		&calendarReviewStatus, &calendarReviewPolicy, &calendarMetadataStatus,
	); err != nil || calendarReviewStatus != "suggestion" || calendarReviewPolicy != "always" || calendarMetadataStatus != "suggestion" {
		t.Fatalf("manual calendar review policy status=%q policy=%q metadataStatus=%q err=%v",
			calendarReviewStatus, calendarReviewPolicy, calendarMetadataStatus, err)
	}
	automaticRule, err := repository.CreateAutomationRule(ctx, projectID, userID, AutomationRuleInput{
		RuleKey: "automatic_review_test", Name: "Automatic review test",
		Description: "Create an approved content effect.", Kind: "channel", Channel: "linkedin",
		Enabled: true, ReviewPolicy: "automatic", Configuration: map[string]any{},
	})
	if err != nil {
		t.Fatalf("create automatic review rule: %v", err)
	}
	automaticRun, err := repository.RunAutomationRule(ctx, projectID, automaticRule.ID, userID)
	if err != nil || automaticRun.EffectType != "content_item" {
		t.Fatalf("run automatic review rule: result=%+v err=%v", automaticRun, err)
	}
	var automaticStatus, automaticPolicy, automaticMetadataStatus string
	if err := pool.QueryRow(ctx, `
		SELECT status, metadata->>'reviewPolicy', metadata->>'status'
		FROM content_items
		WHERE id = $1::uuid`, automaticRun.EffectID).Scan(
		&automaticStatus, &automaticPolicy, &automaticMetadataStatus,
	); err != nil || automaticStatus != "approved" || automaticPolicy != "automatic" || automaticMetadataStatus != "approved" {
		t.Fatalf("manual content review policy status=%q policy=%q metadataStatus=%q err=%v",
			automaticStatus, automaticPolicy, automaticMetadataStatus, err)
	}
	profileNewsletterRule, err := repository.CreateAutomationRule(ctx, projectID, userID, AutomationRuleInput{
		RuleKey: "profile_cadence_newsletter", Name: "Profile cadence newsletter",
		Kind: "newsletter", Channel: "newsletter", Enabled: true,
		ReviewPolicy: "always", Configuration: map[string]any{},
	})
	if err != nil || profileNewsletterRule.NextRunAt == nil {
		t.Fatalf("create profile-cadence newsletter: rule=%+v err=%v", profileNewsletterRule, err)
	}
	zagreb, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load profile timezone: %v", err)
	}
	newsletterLocal := profileNewsletterRule.NextRunAt.In(zagreb)
	if newsletterLocal.Weekday() != time.Friday || newsletterLocal.Hour() != 10 || newsletterLocal.Minute() != 0 {
		t.Fatalf("profile newsletter next run = %s, want Friday at 10:00 project time", newsletterLocal)
	}
	profileGapRule, err := repository.CreateAutomationRule(ctx, projectID, userID, AutomationRuleInput{
		RuleKey: "profile_cadence_gap", Name: "Profile cadence gap",
		Kind: "calendar_gap", Channel: "instagram", Enabled: true,
		ReviewPolicy: "always", Configuration: map[string]any{"gapDays": float64(2), "hour": float64(11)},
	})
	if err != nil || profileGapRule.NextRunAt == nil || profileGapRule.NextRunAt.In(zagreb).Hour() != 11 {
		t.Fatalf("create profile-cadence gap: rule=%+v err=%v", profileGapRule, err)
	}
	configuredCadenceRule, err := repository.CreateAutomationRule(ctx, projectID, userID, AutomationRuleInput{
		RuleKey: "configured_cadence_any_kind", Name: "Configured cadence any kind",
		Kind: "bot_event", Enabled: true, ReviewPolicy: "always",
		Configuration: map[string]any{"cadence": "biweekly", "hour": float64(8), "minute": float64(15)},
	})
	if err != nil || configuredCadenceRule.NextRunAt == nil {
		t.Fatalf("create configured-cadence bot rule: rule=%+v err=%v", configuredCadenceRule, err)
	}
	configuredLocal := configuredCadenceRule.NextRunAt.In(zagreb)
	if configuredLocal.Weekday() != time.Friday || configuredLocal.Hour() != 8 || configuredLocal.Minute() != 15 {
		t.Fatalf("configured-cadence next run = %s, want Friday 08:15 project time", configuredLocal)
	}
	explicitTimezoneRule, err := repository.CreateAutomationRule(ctx, projectID, userID, AutomationRuleInput{
		RuleKey: "profile_timezone_schedule", Name: "Profile timezone schedule",
		Kind: "custom", Channel: "linkedin", Enabled: true, ReviewPolicy: "always",
		ScheduleRule: "FREQ=DAILY;BYHOUR=7;BYMINUTE=30", Configuration: map[string]any{},
	})
	if err != nil || explicitTimezoneRule.NextRunAt == nil {
		t.Fatalf("create timezone-aware schedule: rule=%+v err=%v", explicitTimezoneRule, err)
	}
	if explicitLocal := explicitTimezoneRule.NextRunAt.In(zagreb); explicitLocal.Hour() != 7 || explicitLocal.Minute() != 30 {
		t.Fatalf("initial explicit schedule = %s, want 07:30 Zagreb", explicitLocal)
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load alternate project timezone: %v", err)
	}
	profileInput.Timezone = "America/New_York"
	if _, err := repository.SaveProfile(ctx, projectID, userID, profileInput); err != nil {
		t.Fatalf("save alternate project timezone: %v", err)
	}
	rules, err = repository.ListAutomationRules(ctx, projectID)
	if err != nil {
		t.Fatalf("list timezone-reanchored rules: %v", err)
	}
	byID := make(map[string]AutomationRule, len(rules))
	for _, rule := range rules {
		byID[rule.ID] = rule
	}
	for _, expectation := range []struct {
		id     string
		hour   int
		minute int
		label  string
	}{
		{id: explicitTimezoneRule.ID, hour: 7, minute: 30, label: "RRULE"},
		{id: configuredCadenceRule.ID, hour: 8, minute: 15, label: "configured cadence"},
		{id: profileGapRule.ID, hour: 11, minute: 0, label: "calendar gap"},
		{id: profileNewsletterRule.ID, hour: 10, minute: 0, label: "profile newsletter"},
	} {
		reanchored := byID[expectation.id]
		if reanchored.NextRunAt == nil {
			t.Fatalf("%s schedule was cleared during timezone change", expectation.label)
		}
		local := reanchored.NextRunAt.In(newYork)
		if local.Hour() != expectation.hour || local.Minute() != expectation.minute {
			t.Fatalf("%s reanchored run = %s, want %02d:%02d New York wall clock",
				expectation.label, local, expectation.hour, expectation.minute)
		}
	}
	reanchoredConfiguredLocal := byID[configuredCadenceRule.ID].NextRunAt.In(newYork)
	if reanchoredConfiguredLocal.Year() != configuredLocal.Year() ||
		reanchoredConfiguredLocal.Month() != configuredLocal.Month() ||
		reanchoredConfiguredLocal.Day() != configuredLocal.Day() {
		t.Fatalf("biweekly phase changed during timezone reanchor: before=%s after=%s",
			configuredLocal, reanchoredConfiguredLocal)
	}

	connectionInput := ChannelConnectionInput{
		Provider: "custom_api", Mode: "api", DisplayName: "Local API",
		EndpointURL: "https://example.test/hook", Credential: "temporary-test-token",
	}
	if message := normalizeChannelConnectionInput(&connectionInput, false); message != "" {
		t.Fatalf("normalize connection: %s", message)
	}
	_, err = repository.CreateChannelConnection(ctx, projectID, userID, connectionInput)
	if !errors.Is(err, ErrFeatureUnavailable) {
		t.Fatalf("expected API feature guard, got %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET features = jsonb_set(features, '{api}', 'true'::jsonb)
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("enable API feature: %v", err)
	}
	connection, err := repository.CreateChannelConnection(ctx, projectID, userID, connectionInput)
	if err != nil || !connection.CredentialConfigured {
		t.Fatalf("create connection: connection=%+v err=%v", connection, err)
	}
	if _, err := repository.TestChannelConnection(ctx, projectID, connection.ID, userID); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if err := repository.DeleteChannelConnection(ctx, projectID, connection.ID, userID); err != nil {
		t.Fatalf("delete connection: %v", err)
	}

	var contentID string
	err = pool.QueryRow(ctx, `
		INSERT INTO content_items (project_id, author_id, kind, status, title, body, channels)
		VALUES ($1::uuid, $2::uuid, 'newsletter', 'draft', 'Integration newsletter', 'Body', ARRAY['newsletter'])
		RETURNING id::text`, projectID, userID).Scan(&contentID)
	if err != nil {
		t.Fatalf("create newsletter content: %v", err)
	}
	recipient := "test@example.com"
	delivery, err := repository.CreateNewsletterDelivery(ctx, projectID, userID, NewsletterDeliveryInput{
		ContentItemID: contentID, Subject: "Integration delivery", TestRecipient: &recipient, Mode: "sandbox",
	})
	if err != nil || delivery.Status != "test_sent" || delivery.RecipientCount != 1 {
		t.Fatalf("create newsletter delivery: delivery=%+v err=%v", delivery, err)
	}
	var newsletterListID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO audience_lists (project_id, name, created_by)
		VALUES ($1::uuid, $2, $3::uuid)
		RETURNING id::text`, projectID,
		fmt.Sprintf("Newsletter integration %d", fixtureSuffix), userID).Scan(&newsletterListID); err != nil {
		t.Fatalf("create newsletter list: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO audience_contacts (
			project_id, list_id, email, source, status, consent, subscribed_at, created_by
		)
		VALUES ($1::uuid, $2::uuid, $3, 'manual', 'active', true, now(), $4::uuid)`,
		projectID, newsletterListID, fmt.Sprintf("newsletter-%d@example.test", fixtureSuffix), userID); err != nil {
		t.Fatalf("create newsletter recipient: %v", err)
	}
	scheduledFor := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	var preexistingVariantID, preexistingJobID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO content_variants (
			content_item_id, channel, locale, title, summary, body, status, scheduled_for,
			metadata
		)
		VALUES ($1::uuid, 'newsletter', 'en', 'Existing newsletter queue', '',
		        'Existing body', 'scheduled', $2, '{"syncedFromItem":false}'::jsonb)
		RETURNING id::text`, contentID, scheduledFor).Scan(&preexistingVariantID); err != nil {
		t.Fatalf("create existing newsletter variant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO publication_jobs (project_id, content_variant_id, status, scheduled_for)
		VALUES ($1::uuid, $2::uuid, 'pending', $3)
		RETURNING id::text`, projectID, preexistingVariantID, scheduledFor).Scan(&preexistingJobID); err != nil {
		t.Fatalf("create existing newsletter publication job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO calendar_items (
			project_id, created_by, title, channel, status, scheduled_for,
			content_item_id, content_variant_id, publication_job_id
		)
		VALUES ($1::uuid, $2::uuid, 'Existing newsletter queue', 'newsletter',
		        'scheduled', $3, $4::uuid, $5::uuid, $6::uuid)`, projectID, userID,
		scheduledFor, contentID, preexistingVariantID, preexistingJobID); err != nil {
		t.Fatalf("create existing newsletter calendar item: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO publication_consumptions (
			project_id, source_type, source_id, billing_month
		)
		VALUES ($1::uuid, 'content_variant', $2::uuid,
		        date_trunc('month', now() AT TIME ZONE 'UTC')::date)`, projectID, preexistingVariantID); err != nil {
		t.Fatalf("consume existing newsletter variant: %v", err)
	}
	delivery, err = repository.CreateNewsletterDelivery(ctx, projectID, userID, NewsletterDeliveryInput{
		ContentItemID: contentID, ListID: &newsletterListID, Subject: "Scheduled integration delivery",
		ScheduledFor: &scheduledFor, Mode: "sandbox",
	})
	if err != nil || delivery.Status != "scheduled" || delivery.ContentVariantID == nil ||
		*delivery.ContentVariantID != preexistingVariantID ||
		delivery.ScheduledFor == nil || !delivery.ScheduledFor.Equal(scheduledFor) {
		t.Fatalf("schedule newsletter delivery: delivery=%+v err=%v", delivery, err)
	}
	var variantStatus, calendarStatus, masterStatus string
	var activePublicationJobs, activeDeliveries, consumptionCount int
	err = pool.QueryRow(ctx, `
		SELECT variant.status, calendar.status, item.status,
		       (SELECT count(*) FROM publication_jobs
		        WHERE content_variant_id = variant.id AND status IN ('pending', 'running')),
		       (SELECT count(*) FROM newsletter_deliveries
		        WHERE content_item_id = item.id AND status = 'scheduled'),
		       (SELECT count(*) FROM publication_consumptions
		        WHERE project_id = $1::uuid AND source_type = 'content_variant'
		          AND source_id = variant.id)
		FROM content_items AS item
		JOIN content_variants AS variant
		  ON variant.content_item_id = item.id AND variant.channel = 'newsletter'
		JOIN calendar_items AS calendar ON calendar.content_variant_id = variant.id
		WHERE item.id = $2::uuid AND variant.id = $3::uuid`, projectID, contentID,
		*delivery.ContentVariantID).Scan(
		&variantStatus, &calendarStatus, &masterStatus,
		&activePublicationJobs, &activeDeliveries, &consumptionCount,
	)
	if err != nil || variantStatus != "scheduled" || calendarStatus != "scheduled" ||
		masterStatus != "scheduled" || activePublicationJobs != 0 || activeDeliveries != 1 || consumptionCount != 1 {
		t.Fatalf("newsletter queue state variant=%q calendar=%q master=%q jobs=%d deliveries=%d consumptions=%d err=%v",
			variantStatus, calendarStatus, masterStatus, activePublicationJobs, activeDeliveries, consumptionCount, err)
	}
	if _, err := repository.CreateNewsletterDelivery(ctx, projectID, userID, NewsletterDeliveryInput{
		ContentItemID: contentID, ListID: &newsletterListID, Subject: "Duplicate schedule",
		ScheduledFor: &scheduledFor, Mode: "sandbox",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate newsletter schedule error = %v", err)
	}

	if _, err := repository.CreateServiceRequest(ctx, projectID, userID, ServiceRequestInput{
		RequestType: "support", Summary: "Integration request", Metadata: map[string]any{},
	}); err != nil {
		t.Fatalf("create service request: %v", err)
	}
	requests, err := repository.ListServiceRequests(ctx, projectID, "support")
	if err != nil || len(requests) == 0 || requests[0].RequestType != "support" {
		t.Fatalf("list service requests: requests=%+v err=%v", requests, err)
	}
	persona, err := repository.CreateProjectPersona(ctx, projectID, userID, ProjectPersonaInput{
		Name:        "Integration persona " + time.Now().Format("150405.000000000"),
		Description: "Temporary repository integration persona.", Demographics: "B2B",
		Metadata: map[string]any{"integrationTest": true},
	})
	if err != nil {
		t.Fatalf("create project persona: %v", err)
	}
	persona, err = repository.UpdateProjectPersona(ctx, projectID, persona.ID, userID, ProjectPersonaInput{
		Name: persona.Name, Description: persona.Description, Demographics: "B2B · test",
		IsPrimary: true, Metadata: map[string]any{"integrationTest": true},
	})
	if err != nil || !persona.IsPrimary || persona.Demographics != "B2B · test" {
		t.Fatalf("update project persona: persona=%+v err=%v", persona, err)
	}
	personas, err := repository.ListProjectPersonas(ctx, projectID)
	if err != nil || len(personas) == 0 || personas[0].ID != persona.ID {
		t.Fatalf("list project personas: personas=%+v err=%v", personas, err)
	}
	if err := repository.DeleteProjectPersona(ctx, projectID, persona.ID, userID); err != nil {
		t.Fatalf("delete project persona: %v", err)
	}
	dashboard, err := repository.GetDashboard(ctx, projectID)
	if err != nil || !dashboard.AnalyticsAvailable {
		t.Fatalf("get dashboard: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET features = jsonb_set(features, '{analytics}', 'false'::jsonb)
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("disable analytics: %v", err)
	}
	dashboard, err = repository.GetDashboard(ctx, projectID)
	if err != nil || dashboard.AnalyticsAvailable || dashboard.Stats != (DashboardStats{}) || dashboard.Pipeline != (DashboardPipeline{}) {
		t.Fatalf("disabled analytics must be redacted: dashboard=%+v err=%v", dashboard, err)
	}
}

func TestAutomationScheduleLockSerializesProfileAndRuleWrites(t *testing.T) {
	databaseURL := os.Getenv("WORKSPACE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("WORKSPACE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer pool.Close()

	suffix := time.Now().UnixNano()
	var userID, projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Schedule lock test', 'active') RETURNING id::text`,
		fmt.Sprintf("workspace-lock-%d@example.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, status)
		VALUES ('Schedule lock test', $1, 'hr', 'active') RETURNING id::text`,
		fmt.Sprintf("workspace-lock-%d", suffix)).Scan(&projectID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create project: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1::uuid`, userID)
	}()
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_profiles (project_id, company_name, primary_language, timezone)
		VALUES ($1::uuid, 'Schedule lock test', 'hr', 'Europe/Zagreb')`, projectID); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if err := lockAutomationScheduleProject(ctx, blocker, projectID); err != nil {
		t.Fatalf("lock project schedule: %v", err)
	}
	if _, err := blocker.Exec(ctx, `
		UPDATE project_profiles SET timezone = 'America/New_York' WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("stage timezone update: %v", err)
	}

	type createResult struct {
		rule AutomationRule
		err  error
	}
	started := make(chan struct{})
	done := make(chan createResult, 1)
	repository := NewRepository(pool)
	go func() {
		close(started)
		rule, createErr := repository.CreateAutomationRule(ctx, projectID, userID, AutomationRuleInput{
			RuleKey: "serialized_daily", Name: "Serialized daily", Kind: "custom", Channel: "linkedin",
			Enabled: true, ReviewPolicy: "always", ScheduleRule: "FREQ=DAILY;BYHOUR=7;BYMINUTE=30",
			Configuration: map[string]any{},
		})
		done <- createResult{rule: rule, err: createErr}
	}()
	<-started
	select {
	case result := <-done:
		t.Fatalf("rule write bypassed project schedule lock: rule=%+v err=%v", result.rule, result.err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("commit timezone update: %v", err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.rule.NextRunAt == nil {
			t.Fatalf("create serialized rule: rule=%+v err=%v", result.rule, result.err)
		}
		location, _ := time.LoadLocation("America/New_York")
		local := result.rule.NextRunAt.In(location)
		if local.Hour() != 7 || local.Minute() != 30 {
			t.Fatalf("serialized rule used stale timezone: %s", local)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for serialized rule write")
	}
}
