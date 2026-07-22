package workspace

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ivanorka/millena-ai/internal/automationengine"
	"github.com/ivanorka/millena-ai/internal/automationschedule"
)

const automationRuleColumns = `
	id::text, project_id::text, rule_key, name, description, kind, channel,
	enabled, review_policy, schedule_rule, configuration, run_count,
	last_run_at, next_run_at, created_by::text, created_at, updated_at`

func (r *Repository) ListAutomationRules(ctx context.Context, projectID string) ([]AutomationRule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+automationRuleColumns+`
		FROM automation_rules
		WHERE project_id = $1::uuid
		ORDER BY CASE kind WHEN 'master' THEN 0 WHEN 'bot_event' THEN 1 WHEN 'calendar_gap' THEN 2 WHEN 'newsletter' THEN 3 ELSE 4 END,
		         name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := make([]AutomationRule, 0)
	for rows.Next() {
		rule, err := scanAutomationRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *Repository) CreateAutomationRule(ctx context.Context, projectID, actorID string, input AutomationRuleInput) (AutomationRule, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AutomationRule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAutomationScheduleProject(ctx, tx, projectID); err != nil {
		return AutomationRule{}, err
	}
	if err := applyProfileScheduleDefault(ctx, tx, projectID, &input, time.Now().UTC()); err != nil {
		return AutomationRule{}, err
	}
	rule, err := scanAutomationRule(tx.QueryRow(ctx, `
		INSERT INTO automation_rules (
			project_id, rule_key, name, description, kind, channel, enabled,
			review_policy, schedule_rule, configuration, next_run_at, created_by
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::uuid)
		RETURNING `+automationRuleColumns,
		projectID, input.RuleKey, input.Name, input.Description, input.Kind,
		input.Channel, input.Enabled, input.ReviewPolicy, input.ScheduleRule,
		input.Configuration, input.NextRunAt, actorID))
	if err != nil {
		if postgresCode(err) == "23505" {
			return AutomationRule{}, ErrConflict
		}
		return AutomationRule{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "automation_rule.created", "automation_rule", &rule.ID, map[string]any{
		"kind": rule.Kind, "ruleKey": rule.RuleKey,
	}); err != nil {
		return AutomationRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AutomationRule{}, err
	}
	return rule, nil
}

func (r *Repository) UpdateAutomationRule(ctx context.Context, projectID, ruleID, actorID string, input AutomationRuleInput) (AutomationRule, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AutomationRule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAutomationScheduleProject(ctx, tx, projectID); err != nil {
		return AutomationRule{}, err
	}
	if err := applyProfileScheduleDefault(ctx, tx, projectID, &input, time.Now().UTC()); err != nil {
		return AutomationRule{}, err
	}
	rule, err := scanAutomationRule(tx.QueryRow(ctx, `
		UPDATE automation_rules
		SET rule_key = $3, name = $4, description = $5, kind = $6, channel = $7,
		    enabled = $8, review_policy = $9, schedule_rule = $10,
		    configuration = $11, next_run_at = $12, updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING `+automationRuleColumns,
		projectID, ruleID, input.RuleKey, input.Name, input.Description, input.Kind,
		input.Channel, input.Enabled, input.ReviewPolicy, input.ScheduleRule,
		input.Configuration, input.NextRunAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return AutomationRule{}, ErrNotFound
	}
	if postgresCode(err) == "23505" {
		return AutomationRule{}, ErrConflict
	}
	if err != nil {
		return AutomationRule{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "automation_rule.updated", "automation_rule", &rule.ID, map[string]any{
		"enabled": rule.Enabled, "kind": rule.Kind,
	}); err != nil {
		return AutomationRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AutomationRule{}, err
	}
	return rule, nil
}

func (r *Repository) DeleteAutomationRule(ctx context.Context, projectID, ruleID, actorID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `DELETE FROM automation_rules WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, ruleID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "automation_rule.deleted", "automation_rule", &ruleID, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) RunAutomationRule(ctx context.Context, projectID, ruleID, actorID string) (AutomationRunResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AutomationRunResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rule, err := scanAutomationRule(tx.QueryRow(ctx, `
		SELECT `+automationRuleColumns+`
		FROM automation_rules
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, ruleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return AutomationRunResult{}, ErrNotFound
	}
	if err != nil {
		return AutomationRunResult{}, err
	}
	if !rule.Enabled {
		return AutomationRunResult{}, ErrRuleDisabled
	}

	runAt := time.Now().UTC()
	actor := actorID
	effect, err := automationengine.Execute(ctx, tx, automationengine.Rule{
		ID: rule.ID, ProjectID: rule.ProjectID, Name: rule.Name,
		Description: rule.Description, Kind: rule.Kind, Channel: rule.Channel,
		ReviewPolicy: rule.ReviewPolicy, ScheduleRule: rule.ScheduleRule,
		Configuration: rule.Configuration,
	}, &actor, automationengine.ManualTrigger, runAt)
	if err != nil {
		return AutomationRunResult{}, err
	}
	result := AutomationRunResult{
		RunAt: runAt, EffectType: effect.Type, EffectID: effect.ID,
		EffectTitle: effect.Title,
	}

	rule, err = scanAutomationRule(tx.QueryRow(ctx, `
		UPDATE automation_rules
		SET run_count = run_count + 1, last_run_at = now(), updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING `+automationRuleColumns, projectID, ruleID))
	if err != nil {
		return AutomationRunResult{}, err
	}
	result.Rule = rule
	if rule.LastRunAt != nil {
		result.RunAt = *rule.LastRunAt
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "automation_rule.ran", "automation_rule", &rule.ID, map[string]any{
		"effectType": result.EffectType, "effectId": result.EffectID,
		"effectIds": effect.IDs, "effectCreated": effect.Created,
		"packageId": effect.PackageID, "reviewPolicy": rule.ReviewPolicy,
		"status": effect.Status, "factCheckRequired": effect.FactCheck,
		"respectForbiddenTopics": effect.RespectForbiddenTopics,
		"forbiddenTopicMatches":  effect.ForbiddenTopicMatches,
		"gapDays":                effect.GapDays, "scheduledFor": effect.ScheduledFor,
	}); err != nil {
		return AutomationRunResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AutomationRunResult{}, err
	}
	return result, nil
}

func scanAutomationRule(row scanner) (AutomationRule, error) {
	var rule AutomationRule
	err := row.Scan(
		&rule.ID, &rule.ProjectID, &rule.RuleKey, &rule.Name, &rule.Description,
		&rule.Kind, &rule.Channel, &rule.Enabled, &rule.ReviewPolicy,
		&rule.ScheduleRule, &rule.Configuration, &rule.RunCount,
		&rule.LastRunAt, &rule.NextRunAt, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if rule.Configuration == nil {
		rule.Configuration = map[string]any{}
	}
	return rule, err
}

func lockAutomationScheduleProject(ctx context.Context, tx pgx.Tx, projectID string) error {
	lockKey := "millena:automation-schedule:" + projectID
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey)
	return err
}

func applyProfileScheduleDefault(ctx context.Context, tx pgx.Tx, projectID string, input *AutomationRuleInput, now time.Time) error {
	scheduleRule := strings.TrimSpace(input.ScheduleRule)
	_, configuredCadence := automationConfiguredCadence(input.Configuration)
	profileScheduled := input.Kind == "calendar_gap" || input.Kind == "newsletter" || input.Channel == "newsletter"
	if scheduleRule == "" && !configuredCadence && !profileScheduled {
		return nil
	}
	var timezone, newsletterCadence string
	var socialPostsPerWeek int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(profile.timezone, 'Europe/Zagreb'),
		       COALESCE(profile.social_posts_per_week, 4),
		       COALESCE(profile.newsletter_cadence, 'weekly')
		FROM projects AS project
		LEFT JOIN project_profiles AS profile ON profile.project_id = project.id
		WHERE project.id = $1::uuid`, projectID).Scan(
		&timezone, &socialPostsPerWeek, &newsletterCadence,
	)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.UTC
	}
	next, _, err := calculateAutomationNextRun(
		input.Kind, input.Channel, scheduleRule, input.Configuration,
		socialPostsPerWeek, newsletterCadence, now, location,
	)
	if err != nil {
		return err
	}
	input.NextRunAt = next
	return nil
}

// calculateAutomationNextRun applies the scheduling precedence shared by rule
// writes and profile timezone re-anchoring:
// explicit schedule > configured cadence > kind/profile defaults.
func calculateAutomationNextRun(
	kind, channel, scheduleRule string,
	configuration map[string]any,
	socialPostsPerWeek int,
	newsletterCadence string,
	now time.Time,
	location *time.Location,
) (*time.Time, bool, error) {
	if location == nil {
		location = time.UTC
	}
	hour := automationConfigurationInt(configuration, "hour", 10, 0, 23)
	minute := automationConfigurationInt(configuration, "minute", 0, 0, 59)
	scheduleRule = strings.TrimSpace(scheduleRule)
	if scheduleRule != "" {
		spec, err := automationschedule.Parse(scheduleRule)
		if err != nil {
			return nil, true, err
		}
		var next time.Time
		if spec.IsGap() {
			next = nextAutomationDailyClock(now, location, hour, minute)
		} else {
			next = spec.Next(now, location)
		}
		return &next, true, nil
	}
	if cadence, configured := automationConfiguredCadence(configuration); configured {
		return nextAutomationCadenceRun(cadence, now, location, hour, minute), true, nil
	}
	if kind == "calendar_gap" {
		_, configuredGap := automationConfigurationIntPresent(configuration, "gapDays", 1, 365)
		if socialPostsPerWeek < 1 && !configuredGap {
			return nil, true, nil
		}
		next := nextAutomationDailyClock(now, location, hour, minute)
		return &next, true, nil
	}
	if kind == "newsletter" || channel == "newsletter" {
		return nextAutomationCadenceRun(newsletterCadence, now, location, hour, minute), true, nil
	}
	return nil, false, nil
}

func nextAutomationDailyClock(now time.Time, location *time.Location, hour, minute int) time.Time {
	localNow := now.In(location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next.UTC()
}

func nextAutomationCadenceRun(cadence string, now time.Time, location *time.Location, hour, minute int) *time.Time {
	localNow := now.In(location)
	var next time.Time
	switch strings.ToLower(strings.TrimSpace(cadence)) {
	case "off":
		return nil
	case "weekly", "biweekly":
		daysUntilFriday := (int(time.Friday) - int(localNow.Weekday()) + 7) % 7
		next = time.Date(localNow.Year(), localNow.Month(), localNow.Day()+daysUntilFriday, hour, minute, 0, 0, location)
		if !next.After(localNow) {
			next = next.AddDate(0, 0, 7)
		}
	case "monthly":
		next = time.Date(localNow.Year(), localNow.Month(), 1, hour, minute, 0, 0, location)
		if !next.After(localNow) {
			next = time.Date(localNow.Year(), localNow.Month()+1, 1, hour, minute, 0, 0, location)
		}
	default:
		return nil
	}
	next = next.UTC()
	return &next
}

func reanchorCadenceFromExisting(
	anchor, now time.Time,
	previousLocation, location *time.Location,
	cadence string,
	hour, minute int,
) *time.Time {
	if previousLocation == nil {
		previousLocation = time.UTC
	}
	if location == nil {
		location = time.UTC
	}
	oldLocal := anchor.In(previousLocation)
	localNow := now.In(location)
	dateCursor := time.Date(oldLocal.Year(), oldLocal.Month(), oldLocal.Day(), 12, 0, 0, 0, location)
	advanceDate := func(value time.Time) time.Time {
		switch cadence {
		case "weekly":
			return value.AddDate(0, 0, 7)
		case "biweekly":
			return value.AddDate(0, 0, 14)
		case "monthly":
			return value.AddDate(0, 1, 0)
		default:
			return value
		}
	}
	if cadence != "weekly" && cadence != "biweekly" && cadence != "monthly" {
		return nil
	}
	for {
		candidate := time.Date(dateCursor.Year(), dateCursor.Month(), dateCursor.Day(), hour, minute, 0, 0, location)
		if candidate.After(localNow) {
			candidate = candidate.UTC()
			return &candidate
		}
		dateCursor = advanceDate(dateCursor)
	}
}

func automationConfiguredCadence(configuration map[string]any) (string, bool) {
	configured, ok := configuration["cadence"].(string)
	if !ok {
		return "", false
	}
	configured = strings.ToLower(strings.TrimSpace(configured))
	if _, ok := supportedCadences[configured]; !ok {
		return "", false
	}
	return configured, true
}

func reanchorAutomationSchedules(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
	socialPostsPerWeek int,
	newsletterCadence string,
	location, previousLocation *time.Location,
	timezoneChanged, socialFrequencyChanged, newsletterCadenceChanged bool,
	now time.Time,
) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, kind, channel, schedule_rule, configuration, next_run_at
		FROM automation_rules
		WHERE project_id = $1::uuid
		FOR UPDATE`, projectID)
	if err != nil {
		return 0, err
	}
	type storedRule struct {
		id            string
		kind          string
		channel       string
		scheduleRule  string
		configuration map[string]any
		nextRunAt     *time.Time
	}
	rules := make([]storedRule, 0)
	for rows.Next() {
		var rule storedRule
		if err := rows.Scan(
			&rule.id, &rule.kind, &rule.channel, &rule.scheduleRule, &rule.configuration, &rule.nextRunAt,
		); err != nil {
			rows.Close()
			return 0, err
		}
		if rule.configuration == nil {
			rule.configuration = map[string]any{}
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	updated := 0
	for _, rule := range rules {
		configuredCadence, hasConfiguredCadence := automationConfiguredCadence(rule.configuration)
		reliesOnNewsletterProfile := strings.TrimSpace(rule.scheduleRule) == "" && !hasConfiguredCadence &&
			(rule.kind == "newsletter" || rule.channel == "newsletter")
		reliesOnSocialProfile := strings.TrimSpace(rule.scheduleRule) == "" && !hasConfiguredCadence && rule.kind == "calendar_gap"
		if !timezoneChanged && !(newsletterCadenceChanged && reliesOnNewsletterProfile) && !(socialFrequencyChanged && reliesOnSocialProfile) {
			continue
		}
		next, managed, err := calculateAutomationNextRun(
			rule.kind, rule.channel, rule.scheduleRule, rule.configuration,
			socialPostsPerWeek, newsletterCadence, now, location,
		)
		if err != nil {
			// Legacy rows may contain schedules from before strict validation.
			// Leave them unchanged rather than making an unrelated profile save fail.
			continue
		}
		if !managed {
			continue
		}
		// A timezone-only change must not reset the phase of a biweekly (or any
		// other cadence-based) rule. Rebuild the same anchored calendar date in
		// the new zone and then advance whole declared cadence steps as needed.
		cadenceForAnchor := ""
		if hasConfiguredCadence {
			cadenceForAnchor = configuredCadence
		} else if reliesOnNewsletterProfile && !newsletterCadenceChanged {
			cadenceForAnchor = newsletterCadence
		}
		if timezoneChanged && rule.nextRunAt != nil && cadenceForAnchor != "" && cadenceForAnchor != "off" {
			hour := automationConfigurationInt(rule.configuration, "hour", 10, 0, 23)
			minute := automationConfigurationInt(rule.configuration, "minute", 0, 0, 59)
			next = reanchorCadenceFromExisting(
				*rule.nextRunAt, now, previousLocation, location, cadenceForAnchor, hour, minute,
			)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE automation_rules
			SET next_run_at = $2, updated_at = now()
			WHERE project_id = $1::uuid AND id = $3::uuid`, projectID, next, rule.id); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func automationConfigurationInt(configuration map[string]any, key string, fallback, minimum, maximum int) int {
	value, ok := automationConfigurationIntPresent(configuration, key, minimum, maximum)
	if !ok {
		return fallback
	}
	return value
}

func automationConfigurationIntPresent(configuration map[string]any, key string, minimum, maximum int) (int, bool) {
	raw, ok := configuration[key]
	if !ok {
		return 0, false
	}
	value := 0
	switch number := raw.(type) {
	case int:
		value = number
	case int32:
		value = int(number)
	case int64:
		value = int(number)
	case float64:
		value = int(number)
		if number != float64(value) {
			return 0, false
		}
	default:
		return 0, false
	}
	return value, value >= minimum && value <= maximum
}

func postgresCode(err error) string {
	var pgError interface{ SQLState() string }
	if errors.As(err, &pgError) {
		return pgError.SQLState()
	}
	return ""
}
