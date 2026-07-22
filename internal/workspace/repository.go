package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("workspace resource not found")
var ErrConflict = errors.New("workspace resource already exists")
var ErrRuleDisabled = errors.New("automation rule is disabled")
var ErrInvalidReference = errors.New("workspace reference is invalid")
var ErrCredentialRequired = errors.New("connection credential is required")
var ErrFeatureUnavailable = errors.New("workspace feature is unavailable in the active plan")

type Repository struct {
	pool *pgxpool.Pool
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

func (r *Repository) GetProfile(ctx context.Context, projectID string) (Profile, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO project_profiles (project_id, company_name, primary_language)
		SELECT id, COALESCE(settings->>'brand', name), default_locale
		FROM projects
		WHERE id = $1::uuid
		ON CONFLICT (project_id) DO NOTHING`, projectID)
	if err != nil {
		return Profile{}, err
	}
	profile, err := scanProfile(r.pool.QueryRow(ctx, profileSelect+` WHERE project.id = $1::uuid`, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	return profile, err
}

func (r *Repository) SaveProfile(ctx context.Context, projectID, actorID string, input ProfileInput) (Profile, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAutomationScheduleProject(ctx, tx, projectID); err != nil {
		return Profile{}, err
	}

	result, err := tx.Exec(ctx, `
		UPDATE projects
		SET name = $2, default_locale = $3,
		    settings = jsonb_set(settings, '{brand}', to_jsonb($4::text), true),
		    updated_at = now()
		WHERE id = $1::uuid`, projectID, input.ProjectName, input.PrimaryLanguage, input.CompanyName)
	if err != nil {
		return Profile{}, err
	}
	if result.RowsAffected() == 0 {
		return Profile{}, ErrNotFound
	}
	var previousTimezone, previousNewsletterCadence string
	var previousSocialPostsPerWeek int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(profile.timezone, 'Europe/Zagreb'),
		       COALESCE(profile.social_posts_per_week, 4),
		       COALESCE(profile.newsletter_cadence, 'weekly')
		FROM projects AS project
		LEFT JOIN project_profiles AS profile ON profile.project_id = project.id
		WHERE project.id = $1::uuid`, projectID).Scan(
		&previousTimezone, &previousSocialPostsPerWeek, &previousNewsletterCadence,
	); err != nil {
		return Profile{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO project_profiles (
			project_id, company_name, company_description, website_url, industry, primary_language, timezone,
			social_posts_per_week, newsletter_cadence, signup_headline, signup_copy,
			setup_completed, setup_completed_at, updated_by
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
		        CASE WHEN $12 THEN now() ELSE NULL END, $13::uuid)
		ON CONFLICT (project_id) DO UPDATE SET
			company_name = EXCLUDED.company_name,
			company_description = EXCLUDED.company_description,
			website_url = EXCLUDED.website_url,
			industry = EXCLUDED.industry,
			primary_language = EXCLUDED.primary_language,
			timezone = EXCLUDED.timezone,
			social_posts_per_week = EXCLUDED.social_posts_per_week,
			newsletter_cadence = EXCLUDED.newsletter_cadence,
			signup_headline = EXCLUDED.signup_headline,
			signup_copy = EXCLUDED.signup_copy,
			setup_completed = EXCLUDED.setup_completed,
			setup_completed_at = CASE
				WHEN EXCLUDED.setup_completed THEN COALESCE(project_profiles.setup_completed_at, now())
				ELSE NULL
			END,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()`,
		projectID, input.CompanyName, input.CompanyDescription, input.WebsiteURL, input.Industry,
		input.PrimaryLanguage, input.Timezone, input.SocialPostsPerWeek,
		input.NewsletterCadence, input.SignupHeadline, input.SignupCopy,
		input.SetupCompleted, actorID)
	if err != nil {
		return Profile{}, err
	}
	newLocation, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return Profile{}, err
	}
	previousLocation, err := time.LoadLocation(previousTimezone)
	if err != nil {
		previousLocation = time.UTC
	}
	timezoneChanged := previousTimezone != input.Timezone
	socialFrequencyChanged := previousSocialPostsPerWeek != input.SocialPostsPerWeek
	newsletterCadenceChanged := previousNewsletterCadence != input.NewsletterCadence
	reanchoredRules := 0
	if timezoneChanged || socialFrequencyChanged || newsletterCadenceChanged {
		reanchoredRules, err = reanchorAutomationSchedules(
			ctx, tx, projectID, input.SocialPostsPerWeek, input.NewsletterCadence,
			newLocation, previousLocation, timezoneChanged, socialFrequencyChanged, newsletterCadenceChanged,
			time.Now().UTC(),
		)
		if err != nil {
			return Profile{}, err
		}
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "project.profile_updated", "project", &projectID, map[string]any{
		"setupCompleted": input.SetupCompleted, "previousTimezone": previousTimezone,
		"newTimezone": input.Timezone, "automationSchedulesReanchored": reanchoredRules,
	}); err != nil {
		return Profile{}, err
	}
	profile, err := scanProfile(tx.QueryRow(ctx, profileSelect+` WHERE project.id = $1::uuid`, projectID))
	if err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

const profileSelect = `
	SELECT project.id::text, project.name, profile.company_name, profile.company_description, profile.website_url,
	       profile.industry, profile.primary_language, profile.timezone,
	       profile.social_posts_per_week, profile.newsletter_cadence,
	       profile.signup_headline, profile.signup_copy, profile.setup_completed,
	       profile.setup_completed_at, profile.updated_by::text,
	       profile.created_at, profile.updated_at
	FROM projects AS project
	JOIN project_profiles AS profile ON profile.project_id = project.id`

func (r *Repository) GetDashboard(ctx context.Context, projectID string) (Dashboard, error) {
	dashboard := Dashboard{ProjectID: projectID, Today: []DashboardCalendarItem{}, Channels: []DashboardChannel{}}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1::uuid)`, projectID).Scan(&exists); err != nil {
		return Dashboard{}, err
	}
	if !exists {
		return Dashboard{}, ErrNotFound
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT CASE
			         WHEN entitlement.status IN ('trial', 'active')
			          AND jsonb_typeof(entitlement.features->'analytics') = 'boolean'
			           THEN (entitlement.features->>'analytics')::boolean
			         ELSE false
			       END
			FROM project_entitlements AS entitlement
			WHERE entitlement.project_id = $1::uuid
		), false)`, projectID).Scan(&dashboard.AnalyticsAvailable); err != nil {
		return Dashboard{}, err
	}

	if dashboard.AnalyticsAvailable {
		if err := r.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'published' AND updated_at >= date_trunc('month', now())),
			count(*) FILTER (WHERE status = 'scheduled' AND scheduled_for >= now() AND scheduled_for < now() + interval '14 days'),
			count(*) FILTER (WHERE status = 'in_review'),
			count(*) FILTER (WHERE kind = 'source'),
			count(*) FILTER (WHERE status = 'draft' AND kind <> 'source'),
			count(*) FILTER (WHERE status = 'in_review'),
			count(*) FILTER (WHERE status = 'scheduled')
		FROM content_items
		WHERE project_id = $1::uuid`, projectID).Scan(
			&dashboard.Stats.PublishedThisMonth,
			&dashboard.Stats.ScheduledNext14Days,
			&dashboard.Stats.WaitingReview,
			&dashboard.Pipeline.Collected,
			&dashboard.Pipeline.InProgress,
			&dashboard.Pipeline.InReview,
			&dashboard.Pipeline.Scheduled,
		); err != nil {
			return Dashboard{}, err
		}
		if err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM audience_contacts
		WHERE project_id = $1::uuid AND status = 'active' AND consent`, projectID).Scan(&dashboard.Stats.NewsletterAudience); err != nil {
			return Dashboard{}, err
		}
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE enabled), count(*), COALESCE(sum(run_count), 0), max(last_run_at)
		FROM automation_rules
		WHERE project_id = $1::uuid`, projectID).Scan(
		&dashboard.Automation.EnabledRules,
		&dashboard.Automation.TotalRules,
		&dashboard.Automation.RunCount,
		&dashboard.Automation.LastRunAt,
	); err != nil {
		return Dashboard{}, err
	}

	todayRows, err := r.pool.Query(ctx, `
		WITH project_zone AS (
			SELECT COALESCE((SELECT timezone FROM project_profiles WHERE project_id = $1::uuid), 'Europe/Zagreb') AS timezone
		)
		SELECT item.id::text, item.title, item.channel, item.status, item.scheduled_for
		FROM calendar_items AS item, project_zone
		WHERE item.project_id = $1::uuid
		  AND item.scheduled_for >= date_trunc('day', now() AT TIME ZONE project_zone.timezone) AT TIME ZONE project_zone.timezone
		  AND item.scheduled_for < (date_trunc('day', now() AT TIME ZONE project_zone.timezone) + interval '1 day') AT TIME ZONE project_zone.timezone
		ORDER BY item.scheduled_for
		LIMIT 20`, projectID)
	if err != nil {
		return Dashboard{}, err
	}
	for todayRows.Next() {
		var item DashboardCalendarItem
		if err := todayRows.Scan(&item.ID, &item.Title, &item.Channel, &item.Status, &item.ScheduledFor); err != nil {
			todayRows.Close()
			return Dashboard{}, err
		}
		dashboard.Today = append(dashboard.Today, item)
	}
	if err := todayRows.Err(); err != nil {
		todayRows.Close()
		return Dashboard{}, err
	}
	todayRows.Close()

	channelRows, err := r.pool.Query(ctx, `
		SELECT id::text, provider, display_name, account_handle, status, 'channel', last_checked_at
		FROM channel_connections
		WHERE project_id = $1::uuid AND status <> 'disconnected'
		UNION ALL
		SELECT id::text, provider, display_name, account_handle, status, 'social', last_checked_at
		FROM social_connections
		WHERE project_id = $1::uuid AND status <> 'disconnected'
		ORDER BY provider`, projectID)
	if err != nil {
		return Dashboard{}, err
	}
	defer channelRows.Close()
	for channelRows.Next() {
		var channel DashboardChannel
		if err := channelRows.Scan(
			&channel.ID, &channel.Provider, &channel.DisplayName, &channel.AccountHandle,
			&channel.Status, &channel.Source, &channel.LastCheckedAt,
		); err != nil {
			return Dashboard{}, err
		}
		dashboard.Channels = append(dashboard.Channels, channel)
	}
	return dashboard, channelRows.Err()
}

func (r *Repository) CreateServiceRequest(ctx context.Context, projectID, actorID string, input ServiceRequestInput) (ServiceRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ServiceRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	priority, err := serviceRequestPriority(ctx, tx, projectID)
	if err != nil {
		return ServiceRequest{}, err
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	input.Metadata["priority"] = priority
	var request ServiceRequest
	err = tx.QueryRow(ctx, `
		INSERT INTO service_requests (project_id, request_type, summary, metadata, created_by)
		VALUES ($1::uuid, $2, $3, $4, $5::uuid)
		RETURNING id::text, project_id::text, request_type, status, summary, metadata,
		          created_by::text, created_at, updated_at`,
		projectID, input.RequestType, input.Summary, input.Metadata, actorID).Scan(
		&request.ID, &request.ProjectID, &request.RequestType, &request.Status,
		&request.Summary, &request.Metadata, &request.CreatedBy,
		&request.CreatedAt, &request.UpdatedAt,
	)
	if err != nil {
		return ServiceRequest{}, err
	}
	if err := recordAudit(ctx, tx, projectID, actorID, "service_request.created", "service_request", &request.ID, map[string]any{
		"requestType": request.RequestType, "priority": priority,
	}); err != nil {
		return ServiceRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ServiceRequest{}, err
	}
	return request, nil
}

func serviceRequestPriority(ctx context.Context, query queryRower, projectID string) (string, error) {
	var prioritySupport bool
	if err := query.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT CASE
			         WHEN entitlement.status IN ('trial', 'active')
			          AND jsonb_typeof(entitlement.features->'prioritySupport') = 'boolean'
			           THEN (entitlement.features->>'prioritySupport')::boolean
			         ELSE false
			       END
			FROM project_entitlements AS entitlement
			WHERE entitlement.project_id = $1::uuid
		), false)`, projectID).Scan(&prioritySupport); err != nil {
		return "", err
	}
	if prioritySupport {
		return "priority", nil
	}
	return "standard", nil
}

func (r *Repository) ListServiceRequests(ctx context.Context, projectID, requestType string) ([]ServiceRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, project_id::text, request_type, status, summary, metadata,
		       created_by::text, created_at, updated_at
		FROM service_requests
		WHERE project_id = $1::uuid
		  AND ($2 = '' OR request_type = $2)
		ORDER BY (metadata->>'priority' = 'priority') DESC, created_at DESC
		LIMIT 100`, projectID, requestType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := make([]ServiceRequest, 0)
	for rows.Next() {
		var request ServiceRequest
		if err := rows.Scan(
			&request.ID, &request.ProjectID, &request.RequestType, &request.Status,
			&request.Summary, &request.Metadata, &request.CreatedBy,
			&request.CreatedAt, &request.UpdatedAt,
		); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func recordAudit(ctx context.Context, tx pgx.Tx, projectID, actorID, action, entityType string, entityID *string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (project_id, actor_id, action, entity_type, entity_id, metadata)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6)`,
		projectID, actorID, action, entityType, entityID, metadata)
	return err
}

func scanProfile(row scanner) (Profile, error) {
	var profile Profile
	err := row.Scan(
		&profile.ProjectID, &profile.ProjectName, &profile.CompanyName, &profile.CompanyDescription, &profile.WebsiteURL,
		&profile.Industry, &profile.PrimaryLanguage, &profile.Timezone,
		&profile.SocialPostsPerWeek, &profile.NewsletterCadence,
		&profile.SignupHeadline, &profile.SignupCopy, &profile.SetupCompleted,
		&profile.SetupCompletedAt, &profile.UpdatedBy, &profile.CreatedAt, &profile.UpdatedAt,
	)
	return profile, err
}
