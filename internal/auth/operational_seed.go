package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const assistantWelcomeMessage = "Bok! Koristim spremljenu strategiju, sadržaj, kalendar, publiku i pravila projekta. Mogu pripremiti nacrt, sažeti stanje ili pokrenuti lokalnu automatizaciju bez API ključa."

const defaultProjectTimezone = "Europe/Zagreb"

type operationalSeedExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type operationalWorkspaceSeed struct {
	companyName    string
	websiteURL     string
	industry       string
	personaName    string
	timezone       string
	setupCompleted bool
	channels       []operationalChannelSeed
}

type operationalChannelSeed struct {
	provider      string
	displayName   string
	accountHandle string
	endpointURL   string
}

func mprOperationalWorkspaceSeed() operationalWorkspaceSeed {
	return operationalWorkspaceSeed{
		companyName:    "MPR Grupa",
		websiteURL:     "https://mpr.hr",
		industry:       "Komunikacije i odnosi s javnošću",
		personaName:    "Voditelji marketinga i komunikacija",
		timezone:       defaultProjectTimezone,
		setupCompleted: true,
		channels: []operationalChannelSeed{
			{provider: "whatsapp", displayName: "MPR WhatsApp Business", accountHandle: "+385 91 555 0123"},
			{provider: "telegram", displayName: "Millena AI bot", accountHandle: "@millena_ai_bot"},
			{provider: "website", displayName: "MPR web", accountHandle: "mpr.hr", endpointURL: "https://mpr.hr"},
			{provider: "newsletter", displayName: "MPR Newsletter", accountHandle: "Aktivni pretplatnici"},
		},
	}
}

func newTenantOperationalWorkspaceSeed(organizationName string) operationalWorkspaceSeed {
	return operationalWorkspaceSeed{
		companyName: organizationName,
		timezone:    defaultProjectTimezone,
		channels: []operationalChannelSeed{
			{
				provider:      "newsletter",
				displayName:   organizationName + " Newsletter",
				accountHandle: "Aktivni pretplatnici",
			},
		},
	}
}

// SeedNewProjectOperationalWorkspace gives projects created by an already
// authenticated owner the same functional baseline as a project created at
// registration time. It is exported so the projects package does not need to
// duplicate the operational schema contract.
func SeedNewProjectOperationalWorkspace(
	ctx context.Context,
	tx operationalSeedExecutor,
	projectID string,
	ownerID string,
	projectName string,
) error {
	return seedOperationalWorkspace(ctx, tx, projectID, ownerID, newTenantOperationalWorkspaceSeed(projectName))
}

func seedOperationalWorkspace(
	ctx context.Context,
	tx operationalSeedExecutor,
	projectID string,
	userID string,
	seed operationalWorkspaceSeed,
) error {
	timezone := strings.TrimSpace(seed.timezone)
	if timezone == "" {
		timezone = defaultProjectTimezone
	}
	calendarGapRun, weeklyRun, err := operationalSeedRunAnchors(time.Now(), timezone)
	if err != nil {
		return fmt.Errorf("seed automation schedule: %w", err)
	}
	if _, err := tx.Exec(ctx, seedProjectProfileSQL,
		projectID, seed.companyName, seed.websiteURL, seed.industry, seed.setupCompleted, userID, timezone); err != nil {
		return fmt.Errorf("seed project profile: %w", err)
	}
	if seed.personaName != "" {
		if _, err := tx.Exec(ctx, seedProjectPersonaSQL, projectID, seed.personaName, userID); err != nil {
			return fmt.Errorf("seed project persona: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, seedAutomationRulesSQL, projectID, userID, calendarGapRun, weeklyRun); err != nil {
		return fmt.Errorf("seed automation rules: %w", err)
	}
	if _, err := tx.Exec(ctx, seedAudienceListSQL, projectID, userID); err != nil {
		return fmt.Errorf("seed audience list: %w", err)
	}
	for _, channel := range seed.channels {
		if _, err := tx.Exec(ctx, seedChannelConnectionSQL,
			projectID, channel.provider, channel.displayName, channel.accountHandle, channel.endpointURL, userID); err != nil {
			return fmt.Errorf("seed %s channel connection: %w", channel.provider, err)
		}
	}
	if _, err := tx.Exec(ctx, seedAssistantThreadSQL, projectID, userID); err != nil {
		return fmt.Errorf("seed assistant thread: %w", err)
	}
	if _, err := tx.Exec(ctx, seedAssistantWelcomeSQL, projectID, assistantWelcomeMessage); err != nil {
		return fmt.Errorf("seed assistant welcome: %w", err)
	}
	return nil
}

func operationalSeedRunAnchors(now time.Time, timezone string) (time.Time, time.Time, error) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" || timezone == "Local" {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid IANA timezone %q", timezone)
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("load IANA timezone %q: %w", timezone, err)
	}

	localNow := now.In(location)
	calendarGapRun := time.Date(
		localNow.Year(), localNow.Month(), localNow.Day(), 10, 0, 0, 0, location,
	)
	if !calendarGapRun.After(localNow) {
		calendarGapRun = calendarGapRun.AddDate(0, 0, 1)
	}

	daysUntilFriday := (int(time.Friday) - int(localNow.Weekday()) + 7) % 7
	weeklyRun := time.Date(
		localNow.Year(), localNow.Month(), localNow.Day()+daysUntilFriday, 10, 0, 0, 0, location,
	)
	if !weeklyRun.After(localNow) {
		weeklyRun = weeklyRun.AddDate(0, 0, 7)
	}

	return calendarGapRun.UTC(), weeklyRun.UTC(), nil
}

const seedProjectProfileSQL = `
	INSERT INTO project_profiles (
		project_id, company_name, website_url, industry, primary_language, timezone,
		social_posts_per_week, newsletter_cadence, signup_headline, signup_copy,
		setup_completed, setup_completed_at, updated_by
	)
	VALUES (
		$1::uuid, $2, $3, $4, 'hr', $7, 4, 'weekly',
		'Budite u toku.', 'Najvažnije priče jednom tjedno.', $5,
		CASE WHEN $5 THEN now() ELSE NULL END, $6::uuid
	)
	ON CONFLICT (project_id) DO NOTHING`

const seedProjectPersonaSQL = `
	INSERT INTO project_personas (
		project_id, name, description, demographics, is_primary, metadata, created_by
	)
	VALUES (
		$1::uuid, $2,
		'Osobe odgovorne za reputaciju, sadržaj i odnose s javnošću.',
		'B2B', true, '{"seeded":true}'::jsonb, $3::uuid
	)
	ON CONFLICT DO NOTHING`

const seedAutomationRulesSQL = `
	INSERT INTO automation_rules (
		project_id, rule_key, name, description, kind, channel, enabled,
		review_policy, schedule_rule, configuration, next_run_at, created_by
	)
	SELECT $1::uuid, seed.rule_key, seed.name, seed.description, seed.kind,
	       seed.channel, seed.enabled, seed.review_policy, seed.schedule_rule,
	       seed.configuration, seed.next_run_at, $2::uuid
	FROM (
		VALUES
		  ('master', 'Glavni tijek sadržaja', 'Prikupljanje, provjera, izrada i objava prema pravilima projekta.', 'master', '', true, 'conditional', '', '{"factCheck":true,"respectForbiddenTopics":true}'::jsonb, NULL::timestamptz),
		  ('bot_event', 'Događaj iz bota', 'Iz poruke izradi social, blog i newsletter nacrt.', 'bot_event', 'whatsapp', true, 'always', '', '{"formats":["social","blog","newsletter"]}'::jsonb, NULL::timestamptz),
		  ('calendar_gap', 'Popuni praznine u kalendaru', 'Ako pet dana nema LinkedIn sadržaja, pripremi prijedlog.', 'calendar_gap', 'linkedin', true, 'always', 'gap:5d', '{"gapDays":5}'::jsonb, $3::timestamptz),
		  ('weekly_newsletter', 'Tjedni newsletter', 'Petkom složi pregled najboljeg sadržaja.', 'newsletter', 'newsletter', true, 'always', 'FREQ=WEEKLY;BYDAY=FR;BYHOUR=10', '{"weekday":"friday","hour":10}'::jsonb, $4::timestamptz),
		  ('linkedin', 'LinkedIn pravilo', 'Objavi nakon provjere činjenica.', 'channel', 'linkedin', true, 'conditional', '', '{}'::jsonb, NULL::timestamptz),
		  ('instagram', 'Instagram pravilo', 'Traži pregled kada nedostaje fotografija.', 'channel', 'instagram', true, 'conditional', '', '{}'::jsonb, NULL::timestamptz),
		  ('facebook', 'Facebook pravilo', 'Pripremi prilagođenu verziju.', 'channel', 'facebook', true, 'conditional', '', '{}'::jsonb, NULL::timestamptz),
		  ('youtube', 'YouTube pravilo', 'Pripremi naslov, opis i Shorts tekst.', 'channel', 'youtube', true, 'always', '', '{}'::jsonb, NULL::timestamptz),
		  ('x', 'X pravilo', 'Pripremi kratku verziju ili niz.', 'channel', 'x', true, 'always', '', '{}'::jsonb, NULL::timestamptz),
		  ('reddit', 'Reddit pravilo', 'Uvijek pošalji osobi na pregled.', 'channel', 'reddit', false, 'always', '', '{}'::jsonb, NULL::timestamptz),
		  ('pinterest', 'Pinterest pravilo', 'Izradi pin iz vizuala i članka.', 'channel', 'pinterest', false, 'always', '', '{}'::jsonb, NULL::timestamptz),
		  ('threads', 'Threads pravilo', 'Pripremi razgovornu verziju.', 'channel', 'threads', false, 'always', '', '{}'::jsonb, NULL::timestamptz),
		  ('telegram', 'Telegram pravilo', 'Objavi sažetak u odabrani kanal.', 'channel', 'telegram', true, 'conditional', '', '{}'::jsonb, NULL::timestamptz),
		  ('blog', 'Blog pravilo', 'Izradi puni članak i pripremi objavu na web.', 'channel', 'blog', true, 'always', '', '{}'::jsonb, NULL::timestamptz),
		  ('newsletter', 'Newsletter pravilo', 'Pripremi tjedni pregled aktivnoj listi.', 'channel', 'newsletter', true, 'always', 'FREQ=WEEKLY;BYDAY=FR;BYHOUR=10', '{}'::jsonb, $4::timestamptz)
	) AS seed(rule_key, name, description, kind, channel, enabled, review_policy, schedule_rule, configuration, next_run_at)
	ON CONFLICT (project_id, rule_key) DO NOTHING`

const seedAudienceListSQL = `
	INSERT INTO audience_lists (project_id, name, description, is_default, created_by)
	SELECT $1::uuid, 'Aktivni pretplatnici', 'Glavna newsletter lista projekta.',
	       NOT EXISTS (
	           SELECT 1 FROM audience_lists
	           WHERE project_id = $1::uuid AND is_default
	       ),
	       $2::uuid
	ON CONFLICT (project_id, name) DO NOTHING`

const seedChannelConnectionSQL = `
	INSERT INTO channel_connections (
		project_id, provider, mode, display_name, account_handle, endpoint_url,
		status, metadata, last_checked_at, created_by
	)
	VALUES (
		$1::uuid, $2, 'sandbox', left($3, 120), $4, $5, 'connected',
		'{"seeded":true,"localOnly":true}'::jsonb, now(), $6::uuid
	)
	ON CONFLICT (project_id, provider) DO NOTHING`

const seedAssistantThreadSQL = `
	INSERT INTO assistant_threads (project_id, title, channel, created_by)
	SELECT $1::uuid, 'Glavni razgovor', 'app', $2::uuid
	WHERE NOT EXISTS (
		SELECT 1 FROM assistant_threads
		WHERE project_id = $1::uuid AND title = 'Glavni razgovor' AND channel = 'app'
	)`

const seedAssistantWelcomeSQL = `
	INSERT INTO assistant_messages (
		thread_id, project_id, role, body, metadata
	)
	SELECT thread.id, thread.project_id, 'assistant', $2,
	       '{"seeded":true,"provider":"millena-local","kind":"welcome"}'::jsonb
	FROM assistant_threads AS thread
	WHERE thread.id = (
		SELECT id FROM assistant_threads
		WHERE project_id = $1::uuid AND title = 'Glavni razgovor' AND channel = 'app'
		ORDER BY created_at
		LIMIT 1
	)
	AND NOT EXISTS (
		SELECT 1 FROM assistant_messages AS message
		WHERE message.thread_id = thread.id
		  AND message.role = 'assistant'
		  AND message.body = $2
	)`
