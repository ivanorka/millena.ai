package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/automationengine"
	"github.com/ivanorka/millena-ai/internal/automationschedule"
	"github.com/ivanorka/millena-ai/internal/contentstate"
)

var ErrDatabaseUnavailable = errors.New("operations database is unavailable")

var socialProviders = map[string]struct{}{
	"linkedin": {}, "facebook": {}, "instagram": {}, "youtube": {},
	"x": {}, "reddit": {}, "pinterest": {}, "threads": {},
}

type BatchResult struct {
	AutomationsRun        int `json:"automationsRun"`
	PublicationsSucceeded int `json:"publicationsSucceeded"`
	PublicationsFailed    int `json:"publicationsFailed"`
	NewslettersSent       int `json:"newslettersSent"`
	NewslettersFailed     int `json:"newslettersFailed"`
}

func (result BatchResult) Total() int {
	return result.AutomationsRun + result.PublicationsSucceeded + result.PublicationsFailed +
		result.NewslettersSent + result.NewslettersFailed
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

// ProcessDue drains at most limit records from each independent queue. Every
// record is claimed with FOR UPDATE SKIP LOCKED and completed in the same
// transaction, making multiple API processes safe and avoiding partial state.
func (r *Repository) ProcessDue(ctx context.Context, limit int) (BatchResult, error) {
	if r == nil || r.pool == nil {
		return BatchResult{}, ErrDatabaseUnavailable
	}
	if limit < 1 {
		return BatchResult{}, nil
	}

	var result BatchResult
	for range limit {
		processed, err := r.processNextAutomation(ctx)
		if err != nil {
			return result, fmt.Errorf("process scheduled automation: %w", err)
		}
		if !processed {
			break
		}
		result.AutomationsRun++
	}
	for range limit {
		processed, succeeded, err := r.processNextPublication(ctx)
		if err != nil {
			return result, fmt.Errorf("process publication job: %w", err)
		}
		if !processed {
			break
		}
		if succeeded {
			result.PublicationsSucceeded++
		} else {
			result.PublicationsFailed++
		}
	}
	for range limit {
		processed, succeeded, err := r.processNextNewsletter(ctx)
		if err != nil {
			return result, fmt.Errorf("process newsletter delivery: %w", err)
		}
		if !processed {
			break
		}
		if succeeded {
			result.NewslettersSent++
		} else {
			result.NewslettersFailed++
		}
	}
	return result, nil
}

type dueAutomation struct {
	ID                string
	ProjectID         string
	Name              string
	Description       string
	Kind              string
	Channel           string
	ReviewPolicy      string
	ScheduleRule      string
	Configuration     map[string]any
	NextRunAt         time.Time
	Timezone          string
	NewsletterCadence string
}

func (r *Repository) processNextAutomation(ctx context.Context) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rule dueAutomation
	err = tx.QueryRow(ctx, `
		SELECT rule.id::text, rule.project_id::text, rule.name, rule.description,
		       rule.kind, rule.channel, rule.review_policy, rule.schedule_rule, rule.configuration,
		       rule.next_run_at, COALESCE(profile.timezone, 'Europe/Zagreb'),
		       COALESCE(profile.newsletter_cadence, 'weekly')
		FROM automation_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id AND project.status = 'active'
		LEFT JOIN project_profiles AS profile ON profile.project_id = rule.project_id
		JOIN project_entitlements AS entitlement
		  ON entitlement.project_id = rule.project_id
		 AND entitlement.status IN ('trial', 'active')
		 AND CASE
		       WHEN jsonb_typeof(entitlement.features->'automations') = 'boolean'
		         THEN (entitlement.features->>'automations')::boolean
		       ELSE false
		     END
		WHERE rule.enabled AND rule.next_run_at <= now()
		ORDER BY rule.next_run_at, rule.id
		FOR UPDATE OF rule SKIP LOCKED
		LIMIT 1`).Scan(
		&rule.ID, &rule.ProjectID, &rule.Name, &rule.Description, &rule.Kind,
		&rule.Channel, &rule.ReviewPolicy, &rule.ScheduleRule, &rule.Configuration,
		&rule.NextRunAt, &rule.Timezone, &rule.NewsletterCadence,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}

	runAt := time.Now().UTC()
	effect, err := automationengine.Execute(ctx, tx, automationengine.Rule{
		ID: rule.ID, ProjectID: rule.ProjectID, Name: rule.Name,
		Description: rule.Description, Kind: rule.Kind, Channel: rule.Channel,
		ReviewPolicy: rule.ReviewPolicy, ScheduleRule: rule.ScheduleRule,
		Configuration: rule.Configuration,
	}, nil, automationengine.ScheduledTrigger, runAt)
	if err != nil {
		return false, err
	}
	effectIDsJSON, err := json.Marshal(effect.IDs)
	if err != nil {
		return false, err
	}
	forbiddenMatchesJSON, err := json.Marshal(effect.ForbiddenTopicMatches)
	if err != nil {
		return false, err
	}
	nextRunAt := nextAutomationRun(rule, runAt)
	if _, err := tx.Exec(ctx, `
		UPDATE automation_rules
		SET run_count = run_count + 1, last_run_at = now(), next_run_at = $2,
		    updated_at = now()
		WHERE id = $1::uuid`, rule.ID, nextRunAt); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			project_id, actor_id, action, entity_type, entity_id, metadata
		)
		VALUES ($1::uuid, NULL, 'automation_rule.scheduled_run', 'automation_rule', $2::uuid,
		        jsonb_build_object(
		        	'systemActor', true, 'sandbox', true, 'effectType', $3::text,
		        	'effectId', $4::text, 'effectIds', $5::jsonb,
		        	'effectCreated', $6::boolean, 'packageId', NULLIF($7::text, ''),
		        	'previousNextRunAt', $8::timestamptz,
		        	'nextRunAt', $9::timestamptz, 'reviewPolicy', $10::text,
		        	'status', $11::text, 'factCheckRequired', $12::boolean,
		        	'gapDays', $13::integer, 'scheduledFor', $14::timestamptz,
		        	'respectForbiddenTopics', $15::boolean,
		        	'forbiddenTopicMatches', $16::jsonb
		        ))`, rule.ProjectID, rule.ID, effect.Type, effect.ID, string(effectIDsJSON),
		effect.Created, effect.PackageID, rule.NextRunAt, nextRunAt, rule.ReviewPolicy,
		effect.Status, effect.FactCheck, effect.GapDays, effect.ScheduledFor,
		effect.RespectForbiddenTopics, string(forbiddenMatchesJSON)); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func nextAutomationRun(rule dueAutomation, now time.Time) *time.Time {
	location := time.UTC
	if configured, err := time.LoadLocation(rule.Timezone); err == nil {
		location = configured
	}
	hour := automationConfigurationInt(rule.Configuration, "hour", 10, 0, 23)
	minute := automationConfigurationInt(rule.Configuration, "minute", 0, 0, 59)

	if schedule := strings.TrimSpace(rule.ScheduleRule); schedule != "" {
		spec, err := automationschedule.Parse(schedule)
		if err != nil {
			return nil
		}
		var next time.Time
		if spec.IsGap() {
			next = nextDailyWallClock(now, location, hour, minute)
		} else {
			// Recalculate from the declaration instead of adding a duration to the
			// previous timestamp. A nonexistent DST wall time may be normalized for
			// that occurrence, but the following run returns to the declared clock.
			next = spec.Next(now, location)
		}
		return &next
	}

	recurrence := automationRecurrence(rule)
	if recurrence == "" {
		return nil
	}
	return nextCadenceFromAnchor(rule.NextRunAt, now, location, recurrence, hour, minute)
}

func nextDailyWallClock(now time.Time, location *time.Location, hour, minute int) time.Time {
	localNow := now.In(location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if !next.After(localNow) {
		next = time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, hour, minute, 0, 0, location)
	}
	return next.UTC()
}

func nextCadenceFromAnchor(anchor, now time.Time, location *time.Location, recurrence string, hour, minute int) *time.Time {
	if location == nil {
		location = time.UTC
	}
	localAnchor := anchor.In(location)
	localNow := now.In(location)
	// Keep a noon date cursor separate from the scheduled clock. Noon avoids a
	// spring-forward normalization becoming the anchor for every later run.
	dateCursor := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 12, 0, 0, 0, location)
	advanceDate := func(value time.Time) time.Time {
		switch recurrence {
		case "daily":
			return value.AddDate(0, 0, 1)
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
	if recurrence != "daily" && recurrence != "weekly" && recurrence != "biweekly" && recurrence != "monthly" {
		return nil
	}
	dateCursor = advanceDate(dateCursor)
	for {
		next := time.Date(dateCursor.Year(), dateCursor.Month(), dateCursor.Day(), hour, minute, 0, 0, location)
		if next.After(localNow) {
			next = next.UTC()
			return &next
		}
		dateCursor = advanceDate(dateCursor)
	}
}

func automationConfigurationInt(configuration map[string]any, key string, fallback, minimum, maximum int) int {
	raw, ok := configuration[key]
	if !ok {
		return fallback
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
			return fallback
		}
	default:
		return fallback
	}
	if value < minimum || value > maximum {
		return fallback
	}
	return value
}

func automationRecurrence(rule dueAutomation) string {
	if schedule := strings.TrimSpace(rule.ScheduleRule); schedule != "" {
		spec, err := automationschedule.Parse(schedule)
		if err != nil {
			return ""
		}
		return string(spec.Frequency)
	}
	if configured, ok := rule.Configuration["cadence"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(configured)) {
		case "off":
			return ""
		case "weekly", "biweekly", "monthly":
			return strings.ToLower(strings.TrimSpace(configured))
		}
	}
	if rule.Kind == "calendar_gap" {
		return "daily"
	}
	if rule.Kind == "newsletter" || rule.Channel == "newsletter" {
		switch strings.ToLower(strings.TrimSpace(rule.NewsletterCadence)) {
		case "weekly", "biweekly", "monthly":
			return strings.ToLower(strings.TrimSpace(rule.NewsletterCadence))
		}
	}
	return ""
}

type duePublication struct {
	ID            string
	ProjectID     string
	VariantID     string
	ContentItemID string
	Channel       string
	Body          string
	ScheduledFor  time.Time
}

func (r *Repository) processNextPublication(ctx context.Context) (bool, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var job duePublication
	err = tx.QueryRow(ctx, `
		SELECT job.id::text, job.project_id::text, variant.id::text,
		       item.id::text, variant.channel, variant.body, job.scheduled_for
		FROM publication_jobs AS job
		JOIN content_variants AS variant ON variant.id = job.content_variant_id
		JOIN content_items AS item
		  ON item.id = variant.content_item_id AND item.project_id = job.project_id
		JOIN projects AS project ON project.id = job.project_id AND project.status = 'active'
		JOIN project_entitlements AS entitlement
		  ON entitlement.project_id = job.project_id
		 AND entitlement.status IN ('trial', 'active')
		WHERE job.status = 'pending' AND job.scheduled_for <= now()
		  AND NOT EXISTS (
			SELECT 1
			FROM newsletter_deliveries AS delivery
			WHERE delivery.content_variant_id = variant.id
			  AND delivery.status = 'scheduled'
		  )
		ORDER BY job.scheduled_for, job.id
		FOR UPDATE OF job, variant, item SKIP LOCKED
		LIMIT 1`).Scan(
		&job.ID, &job.ProjectID, &job.VariantID, &job.ContentItemID,
		&job.Channel, &job.Body, &job.ScheduledFor,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, tx.Commit(ctx)
	}
	if err != nil {
		return false, false, err
	}

	var externalReference, failure string
	if _, social := socialProviders[job.Channel]; social {
		externalReference, failure, err = publishSocialSandbox(ctx, tx, job)
	} else {
		externalReference, failure, err = publishLocalSandbox(ctx, tx, job)
	}
	if err != nil {
		return false, false, err
	}
	succeeded := failure == ""
	if err := finishPublication(ctx, tx, job, succeeded, externalReference, failure); err != nil {
		return false, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, false, err
	}
	return true, succeeded, nil
}

func publishSocialSandbox(ctx context.Context, tx pgx.Tx, job duePublication) (string, string, error) {
	bodyLength := utf8.RuneCountInString(job.Body)
	if bodyLength < 1 || bodyLength > 10000 {
		return "", "Social variant body must contain between 1 and 10,000 characters.", nil
	}

	var connectionID, mode string
	err := tx.QueryRow(ctx, `
		SELECT id::text, mode
		FROM social_connections
		WHERE project_id = $1::uuid AND provider = $2 AND status = 'connected'
		FOR SHARE`, job.ProjectID, job.Channel).Scan(&connectionID, &mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "A connected social sandbox account is required for " + job.Channel + ".", nil
	}
	if err != nil {
		return "", "", err
	}
	if mode != "sandbox" {
		return "", "External social publishing is disabled; connect a sandbox account.", nil
	}

	var postID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM social_posts
		WHERE project_id = $1::uuid AND content_variant_id = $2::uuid
		ORDER BY created_at
		FOR UPDATE
		LIMIT 1`, job.ProjectID, job.VariantID).Scan(&postID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO social_posts (
				project_id, content_item_id, content_variant_id, body, status, scheduled_for
			)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'published', $5)
			RETURNING id::text`, job.ProjectID, job.ContentItemID, job.VariantID,
			job.Body, job.ScheduledFor).Scan(&postID)
	} else if err == nil {
		_, err = tx.Exec(ctx, `
			UPDATE social_posts
			SET content_item_id = $2::uuid, body = $3, status = 'published',
			    scheduled_for = $4, updated_at = now()
			WHERE id = $1::uuid`, postID, job.ContentItemID, job.Body, job.ScheduledFor)
	}
	if err != nil {
		return "", "", err
	}

	externalReference := fmt.Sprintf("sandbox://%s/%s", job.Channel, postID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO social_publications (
			social_post_id, social_connection_id, provider, status,
			external_reference, published_at, last_error
		)
		VALUES ($1::uuid, $2::uuid, $3, 'published', $4, now(), NULL)
		ON CONFLICT (social_post_id, social_connection_id) DO UPDATE SET
			provider = EXCLUDED.provider, status = 'published',
			external_reference = EXCLUDED.external_reference,
			published_at = now(), last_error = NULL, updated_at = now()`,
		postID, connectionID, job.Channel, externalReference); err != nil {
		return "", "", err
	}
	return externalReference, "", nil
}

func publishLocalSandbox(ctx context.Context, tx pgx.Tx, job duePublication) (string, string, error) {
	provider := ""
	switch job.Channel {
	case "telegram":
		provider = "telegram"
	case "newsletter":
		provider = "newsletter"
	case "blog", "website":
		provider = "website"
	case "media":
		return fmt.Sprintf("sandbox://media/%s", job.ID), "", nil
	default:
		return "", "The publication channel is not supported by the sandbox worker.", nil
	}

	var mode string
	err := tx.QueryRow(ctx, `
		SELECT mode
		FROM channel_connections
		WHERE project_id = $1::uuid AND provider = $2 AND status = 'connected'
		FOR SHARE`, job.ProjectID, provider).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "A connected channel sandbox is required for " + provider + ".", nil
	}
	if err != nil {
		return "", "", err
	}
	if mode != "sandbox" {
		return "", "External channel delivery is disabled; connect a sandbox destination.", nil
	}
	return fmt.Sprintf("sandbox://%s/%s", job.Channel, job.ID), "", nil
}

func finishPublication(ctx context.Context, tx pgx.Tx, job duePublication, succeeded bool, externalReference, failure string) error {
	status := "failed"
	variantStatus := "failed"
	calendarStatus := "failed"
	action := "publication_job.failed"
	if succeeded {
		status = "succeeded"
		variantStatus = "published"
		calendarStatus = "published"
		action = "publication_job.succeeded"
	}
	if len(failure) > 2000 {
		failure = failure[:2000]
	}

	if _, err := tx.Exec(ctx, `
		UPDATE publication_jobs
		SET status = $2, attempt_count = attempt_count + 1,
		    last_error = NULLIF($3, ''), external_reference = NULLIF($4, ''),
		    updated_at = now()
		WHERE id = $1::uuid AND status = 'pending'`,
		job.ID, status, failure, externalReference); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE content_variants
		SET status = $2, revision = revision + 1, updated_at = now()
		WHERE id = $1::uuid`, job.VariantID, variantStatus); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE calendar_items
		SET status = $3,
		    metadata = metadata || jsonb_build_object(
		    	'sandbox', true, 'publicationJobId', $1::text,
		    	'externalReference', NULLIF($4, ''), 'lastError', NULLIF($5, ''),
		    	'processedAt', now()
		    ),
		    updated_at = now()
		WHERE project_id = $2::uuid
		  AND (publication_job_id = $1::uuid OR content_variant_id = $6::uuid)
		  AND status = 'scheduled'`, job.ID, job.ProjectID, calendarStatus,
		externalReference, failure, job.VariantID); err != nil {
		return err
	}
	if err := contentstate.Recompute(ctx, tx, job.ProjectID, job.ContentItemID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			project_id, actor_id, action, entity_type, entity_id, metadata
		)
		VALUES ($1::uuid, NULL, $2, 'publication_job', $3::uuid,
		        jsonb_build_object(
		        	'systemActor', true, 'sandbox', true, 'channel', $4::text,
		        	'contentItemId', $5::text, 'contentVariantId', $6::text,
		        	'externalReference', NULLIF($7, ''), 'lastError', NULLIF($8, '')
		        ))`, job.ProjectID, action, job.ID, job.Channel,
		job.ContentItemID, job.VariantID, externalReference, failure)
	return err
}

type dueNewsletter struct {
	ID            string
	ProjectID     string
	ContentItemID string
	VariantID     *string
	ListID        *string
	Mode          string
}

func (r *Repository) processNextNewsletter(ctx context.Context) (bool, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var delivery dueNewsletter
	err = tx.QueryRow(ctx, `
		SELECT delivery.id::text, delivery.project_id::text,
		       delivery.content_item_id::text, delivery.content_variant_id::text,
		       delivery.list_id::text, delivery.mode
		FROM newsletter_deliveries AS delivery
		JOIN content_items AS item
		  ON item.id = delivery.content_item_id AND item.project_id = delivery.project_id
		JOIN projects AS project ON project.id = delivery.project_id AND project.status = 'active'
		JOIN project_entitlements AS entitlement
		  ON entitlement.project_id = delivery.project_id
		 AND entitlement.status IN ('trial', 'active')
		WHERE delivery.status = 'scheduled' AND delivery.scheduled_for <= now()
		ORDER BY delivery.scheduled_for, delivery.id
		FOR UPDATE OF delivery, item SKIP LOCKED
		LIMIT 1`).Scan(
		&delivery.ID, &delivery.ProjectID, &delivery.ContentItemID,
		&delivery.VariantID, &delivery.ListID, &delivery.Mode,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, tx.Commit(ctx)
	}
	if err != nil {
		return false, false, err
	}

	recipientCount := 0
	failure := ""
	if delivery.Mode != "sandbox" {
		failure = "External newsletter delivery is disabled; scheduled delivery must use sandbox mode."
	} else {
		err = tx.QueryRow(ctx, `
			SELECT count(*)
			FROM audience_contacts
			WHERE project_id = $1::uuid AND status = 'active' AND consent
			  AND ($2::uuid IS NULL OR list_id = $2::uuid)`,
			delivery.ProjectID, delivery.ListID).Scan(&recipientCount)
		if err != nil {
			return false, false, err
		}
		if recipientCount == 0 {
			failure = "No active consented recipients are available for this newsletter delivery."
		}
	}

	succeeded := failure == ""
	if err := finishNewsletter(ctx, tx, delivery, recipientCount, succeeded, failure); err != nil {
		return false, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, false, err
	}
	return true, succeeded, nil
}

func finishNewsletter(ctx context.Context, tx pgx.Tx, delivery dueNewsletter, recipientCount int, succeeded bool, failure string) error {
	status := "failed"
	action := "newsletter_delivery.failed"
	externalReference := ""
	if succeeded {
		status = "sent"
		action = "newsletter_delivery.sent"
		externalReference = "sandbox://newsletter/" + delivery.ID
	}
	if _, err := tx.Exec(ctx, `
		UPDATE newsletter_deliveries
		SET status = $2, recipient_count = $3,
		    sent_at = CASE WHEN $2 = 'sent' THEN now() ELSE NULL END,
		    external_reference = NULLIF($4, ''), last_error = NULLIF($5, ''),
		    updated_at = now()
		WHERE id = $1::uuid AND status = 'scheduled'`,
		delivery.ID, status, recipientCount, externalReference, failure); err != nil {
		return err
	}
	if delivery.VariantID != nil {
		variantStatus := "failed"
		calendarStatus := "failed"
		if succeeded {
			variantStatus = "published"
			calendarStatus = "published"
		}
		if _, err := tx.Exec(ctx, `
			UPDATE content_variants
			SET status = $2, revision = revision + 1,
			    metadata = metadata || jsonb_build_object(
			      'newsletterQueue', true, 'newsletterDeliveryId', $3::text,
			      'externalReference', NULLIF($4, ''), 'lastError', NULLIF($5, ''),
			      'processedAt', now()
			    ),
			    updated_at = now()
			WHERE id = $1::uuid`, *delivery.VariantID, variantStatus, delivery.ID,
			externalReference, failure); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE calendar_items
			SET status = $3, publication_job_id = NULL,
			    metadata = metadata || jsonb_build_object(
			      'sandbox', true, 'newsletterQueue', true,
			      'newsletterDeliveryId', $1::text,
			      'externalReference', NULLIF($4, ''), 'lastError', NULLIF($5, ''),
			      'processedAt', now()
			    ),
			    updated_at = now()
			WHERE project_id = $2::uuid AND content_variant_id = $6::uuid
			  AND status = 'scheduled'`, delivery.ID, delivery.ProjectID, calendarStatus,
			externalReference, failure, *delivery.VariantID); err != nil {
			return err
		}
	}
	if err := contentstate.Recompute(ctx, tx, delivery.ProjectID, delivery.ContentItemID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			project_id, actor_id, action, entity_type, entity_id, metadata
		)
		VALUES ($1::uuid, NULL, $2, 'newsletter_delivery', $3::uuid,
		        jsonb_build_object(
		        	'systemActor', true, 'sandbox', true,
		        	'contentItemId', $4::text, 'recipientCount', $5::integer,
		        	'externalReference', NULLIF($6, ''), 'lastError', NULLIF($7, '')
		        ))`, delivery.ProjectID, action, delivery.ID, delivery.ContentItemID,
		recipientCount, externalReference, failure)
	return err
}
