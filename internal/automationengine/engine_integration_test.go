package automationengine

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConfiguredEffectsAreTransactionalAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("AUTOMATION_ENGINE_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("WORKSPACE_TEST_DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("AUTOMATION_ENGINE_TEST_DATABASE_URL or WORKSPACE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UTC().UnixNano()
	var userID string
	err = pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Automation engine integration', 'active')
		RETURNING id::text`, fmt.Sprintf("automation-engine-%d@example.test", suffix)).Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	var projectID string
	err = pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, status)
		VALUES ('Automation engine integration', $1, 'en', 'active')
		RETURNING id::text`, fmt.Sprintf("automation-engine-%d", suffix)).Scan(&projectID)
	if err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_profiles (
			project_id, company_name, primary_language, timezone,
			social_posts_per_week, newsletter_cadence, updated_by
		)
		VALUES ($1::uuid, 'Automation Inc.', 'en', 'America/New_York', 2, 'monthly', $2::uuid)`,
		projectID, userID); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_strategies (project_id, forbidden_topics, tone, updated_by)
		VALUES ($1::uuid, 'secret acquisition; personal data', 'clear', $2::uuid)`,
		projectID, userID); err != nil {
		t.Fatalf("create project strategy: %v", err)
	}
	var masterRuleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, kind, enabled, review_policy,
			configuration, created_by
		)
		VALUES ($1::uuid, 'master', 'Master verification', 'master', true,
		        'conditional', '{"factCheck":true,"respectForbiddenTopics":true}'::jsonb, $2::uuid)
		RETURNING id::text`, projectID, userID).Scan(&masterRuleID)
	if err != nil {
		t.Fatalf("create master rule: %v", err)
	}
	var botRuleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, description, kind, channel, enabled,
			review_policy, configuration, created_by
		)
		VALUES ($1::uuid, 'bot_package', 'Coordinated launch', 'Prepare the secret acquisition package.',
		        'bot_event', 'whatsapp', true, 'automatic',
		        '{"formats":["social","blog","newsletter"],"channels":["telegram","linkedin","instagram"]}'::jsonb,
		        $2::uuid)
		RETURNING id::text`, projectID, userID).Scan(&botRuleID)
	if err != nil {
		t.Fatalf("create bot rule: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin bot transaction: %v", err)
	}
	effect, err := Execute(ctx, tx, Rule{
		ID: botRuleID, ProjectID: projectID, Name: "Coordinated launch",
		Description: "Prepare the secret acquisition package.", Kind: "bot_event", Channel: "whatsapp",
		ReviewPolicy: "automatic",
		Configuration: map[string]any{
			"formats":  []any{"social", "blog", "newsletter"},
			"channels": []any{"telegram", "linkedin", "instagram"},
		},
	}, &userID, ScheduledTrigger, time.Now().UTC())
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("execute bot package: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit bot package: %v", err)
	}
	if !effect.Created || effect.PackageID == "" || len(effect.IDs) != 3 || !effect.FactCheck ||
		!effect.RespectForbiddenTopics || len(effect.ForbiddenTopicMatches) != 1 || effect.Status != "in_review" {
		t.Fatalf("bot package effect = %+v", effect)
	}

	var itemCount, socialCount, blogCount, newsletterCount, packageCount int
	var allInReview, allFactChecked, allForbiddenChecked, allForbiddenMatched bool
	err = pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE kind = 'social'),
		       count(*) FILTER (WHERE kind = 'blog'),
		       count(*) FILTER (WHERE kind = 'newsletter'),
		       count(DISTINCT metadata->>'automationPackageId'),
		       bool_and(status = 'in_review'),
		       bool_and((metadata->>'factCheckRequired')::boolean),
		       bool_and((metadata->>'forbiddenTopicsChecked')::boolean),
		       bool_and((metadata->>'forbiddenTopicReviewRequired')::boolean)
		FROM content_items
		WHERE project_id = $1::uuid AND metadata->>'automationRuleId' = $2`,
		projectID, botRuleID).Scan(&itemCount, &socialCount, &blogCount,
		&newsletterCount, &packageCount, &allInReview, &allFactChecked,
		&allForbiddenChecked, &allForbiddenMatched)
	if err != nil || itemCount != 3 || socialCount != 1 || blogCount != 1 ||
		newsletterCount != 1 || packageCount != 1 || !allInReview || !allFactChecked ||
		!allForbiddenChecked || !allForbiddenMatched {
		t.Fatalf("package rows: total=%d social=%d blog=%d newsletter=%d packages=%d review=%v factCheck=%v forbiddenChecked=%v forbiddenMatched=%v err=%v",
			itemCount, socialCount, blogCount, newsletterCount, packageCount, allInReview,
			allFactChecked, allForbiddenChecked, allForbiddenMatched, err)
	}
	var variantCount, englishVariants, telegramVariants int
	err = pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE locale = 'en'),
		       count(*) FILTER (WHERE channel = 'telegram')
		FROM content_variants AS variant
		JOIN content_items AS item ON item.id = variant.content_item_id
		WHERE item.project_id = $1::uuid AND item.metadata->>'automationRuleId' = $2`,
		projectID, botRuleID).Scan(&variantCount, &englishVariants, &telegramVariants)
	if err != nil || variantCount != 5 || englishVariants != 5 || telegramVariants != 1 {
		t.Fatalf("package variants: total=%d english=%d telegram=%d err=%v",
			variantCount, englishVariants, telegramVariants, err)
	}

	var gapRuleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, description, kind, channel, enabled,
			review_policy, schedule_rule, configuration, created_by
		)
		VALUES ($1::uuid, 'real_gap', 'Real Instagram gap', 'Prepare a gap draft.',
		        'calendar_gap', 'instagram', true, 'automatic', 'gap:9d',
		        '{"gapDays":3,"hour":9}'::jsonb, $2::uuid)
		RETURNING id::text`, projectID, userID).Scan(&gapRuleID)
	if err != nil {
		t.Fatalf("create gap rule: %v", err)
	}
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	var occupiedCalendarID string
	err = pool.QueryRow(ctx, `
		INSERT INTO calendar_items (
			project_id, created_by, title, channel, status, scheduled_for
		)
		VALUES ($1::uuid, $2::uuid, 'Existing Instagram item', 'instagram', 'scheduled', $3)
		RETURNING id::text`, projectID, userID, now.Add(48*time.Hour)).Scan(&occupiedCalendarID)
	if err != nil {
		t.Fatalf("create occupied calendar slot: %v", err)
	}

	gapRule := Rule{
		ID: gapRuleID, ProjectID: projectID, Name: "Real Instagram gap",
		Description: "Prepare a gap draft.", Kind: "calendar_gap", Channel: "instagram",
		ReviewPolicy: "automatic", ScheduleRule: "gap:9d",
		Configuration: map[string]any{"gapDays": float64(3), "hour": float64(9)},
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin occupied gap transaction: %v", err)
	}
	occupiedEffect, err := Execute(ctx, tx, gapRule, &userID, ManualTrigger, now)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("execute occupied gap: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit occupied gap check: %v", err)
	}
	if occupiedEffect.Created || occupiedEffect.ID != "" || occupiedEffect.GapDays != 3 {
		t.Fatalf("occupied gap effect = %+v", occupiedEffect)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM calendar_items WHERE id = $1::uuid`, occupiedCalendarID); err != nil {
		t.Fatalf("clear occupied calendar slot: %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin open gap transaction: %v", err)
	}
	gapEffect, err := Execute(ctx, tx, gapRule, &userID, ManualTrigger, now)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("execute open gap: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit open gap: %v", err)
	}
	if !gapEffect.Created || gapEffect.ID == "" || gapEffect.Status != "suggestion" ||
		gapEffect.ScheduledFor == nil || gapEffect.GapDays != 3 {
		t.Fatalf("open gap effect = %+v", gapEffect)
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load profile timezone: %v", err)
	}
	if local := gapEffect.ScheduledFor.In(newYork); local.Hour() != 9 {
		t.Fatalf("gap scheduled for %s, expected 09:00 project time", local)
	}
	var linkedRows int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM calendar_items AS calendar
		JOIN content_items AS item
		  ON item.id = calendar.content_item_id AND item.project_id = calendar.project_id
		JOIN content_variants AS variant
		  ON variant.id = calendar.content_variant_id AND variant.content_item_id = item.id
		WHERE calendar.id = $1::uuid AND calendar.project_id = $2::uuid
		  AND calendar.status = 'suggestion' AND item.status = 'in_review'
		  AND variant.status = 'in_review'
		  AND calendar.metadata->>'status' = 'suggestion'
		  AND item.metadata->>'status' = 'in_review'
		  AND variant.metadata->>'status' = 'in_review'
		  AND (calendar.metadata->>'gapDays')::integer = 3
		  AND (calendar.metadata->>'factCheckRequired')::boolean`,
		gapEffect.ID, projectID).Scan(&linkedRows)
	if err != nil || linkedRows != 1 {
		t.Fatalf("linked calendar/content/variant rows=%d err=%v", linkedRows, err)
	}

	// Two workers racing on the same project/channel must not both observe an
	// empty window. The configured channel list also takes precedence over the
	// legacy single-channel fields and deterministically selects its first
	// calendar-supported value (Telegram is an output channel, but not a
	// calendar channel).
	lockedGapRule := gapRule
	lockedGapRule.Name = "Serialized Facebook gap"
	lockedGapRule.Channel = "instagram"
	lockedGapRule.Configuration = map[string]any{
		"channels": []any{"telegram", "facebook", "linkedin"},
		"channel":  "reddit",
		"gapDays":  float64(3),
		"hour":     float64(11),
	}
	type concurrentGapResult struct {
		effect Effect
		err    error
	}
	startGapRuns := make(chan struct{})
	gapResults := make(chan concurrentGapResult, 2)
	for range 2 {
		go func() {
			runTx, beginErr := pool.Begin(ctx)
			if beginErr != nil {
				gapResults <- concurrentGapResult{err: beginErr}
				return
			}
			<-startGapRuns
			runEffect, runErr := Execute(ctx, runTx, lockedGapRule, &userID, ScheduledTrigger, now)
			if runErr != nil {
				_ = runTx.Rollback(ctx)
				gapResults <- concurrentGapResult{err: runErr}
				return
			}
			if runErr = runTx.Commit(ctx); runErr != nil {
				gapResults <- concurrentGapResult{err: runErr}
				return
			}
			gapResults <- concurrentGapResult{effect: runEffect}
		}()
	}
	close(startGapRuns)
	createdGapRuns := 0
	skippedGapRuns := 0
	for range 2 {
		result := <-gapResults
		if result.err != nil {
			t.Fatalf("concurrent gap run: %v", result.err)
		}
		if result.effect.Created {
			createdGapRuns++
		} else {
			skippedGapRuns++
		}
	}
	if createdGapRuns != 1 || skippedGapRuns != 1 {
		t.Fatalf("serialized gap effects: created=%d skipped=%d", createdGapRuns, skippedGapRuns)
	}
	var lockedCalendarRows int
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM calendar_items
		WHERE project_id = $1::uuid AND channel = 'facebook'
		  AND title = 'Automatizacija: Serialized Facebook gap'`, projectID).Scan(&lockedCalendarRows)
	if err != nil || lockedCalendarRows != 1 {
		t.Fatalf("serialized configured-channel rows=%d err=%v", lockedCalendarRows, err)
	}

	rollbackRule := gapRule
	rollbackRule.ID = masterRuleID
	rollbackRule.Kind = "custom"
	rollbackRule.Channel = "linkedin"
	rollbackRule.Description = "Prepare the secret acquisition brief."
	rollbackRule.Configuration = map[string]any{"contentKind": "social", "factCheck": false}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback transaction: %v", err)
	}
	rollbackEffect, err := Execute(ctx, tx, rollbackRule, &userID, ManualTrigger, now)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("execute rollback effect: %v", err)
	}
	if rollbackEffect.FactCheck || !rollbackEffect.RespectForbiddenTopics ||
		len(rollbackEffect.ForbiddenTopicMatches) != 1 || rollbackEffect.Status != "in_review" {
		_ = tx.Rollback(ctx)
		t.Fatalf("forbidden-topic-only escalation effect = %+v", rollbackEffect)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback effect: %v", err)
	}
	var rolledBackCount int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM content_items WHERE id = $1::uuid`, rollbackEffect.ID).Scan(&rolledBackCount)
	if err != nil || rolledBackCount != 0 {
		t.Fatalf("rolled-back effect count=%d err=%v", rolledBackCount, err)
	}
}
