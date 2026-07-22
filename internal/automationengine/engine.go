// Package automationengine creates the concrete, database-backed effects of
// automation rules. Both manual API runs and the scheduled worker use this
// package so configuration has identical semantics in both execution paths.
package automationengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ivanorka/millena-ai/internal/automationpolicy"
)

var ErrProjectNotFound = errors.New("automation project was not found")

type Rule struct {
	ID            string
	ProjectID     string
	Name          string
	Description   string
	Kind          string
	Channel       string
	ReviewPolicy  string
	ScheduleRule  string
	Configuration map[string]any
}

type Trigger string

const (
	ManualTrigger    Trigger = "manual"
	ScheduledTrigger Trigger = "scheduled"
)

type Effect struct {
	Type                   string
	ID                     string
	Title                  string
	Status                 string
	Created                bool
	IDs                    []string
	PackageID              string
	FactCheck              bool
	RespectForbiddenTopics bool
	ForbiddenTopicMatches  []string
	GapDays                int
	ScheduledFor           *time.Time
}

type projectPreferences struct {
	Timezone           string
	PrimaryLanguage    string
	SocialPostsPerWeek int
	NewsletterCadence  string
	ForbiddenTopics    string
}

type automationPolicy struct {
	FactCheck              bool
	RespectForbiddenTopics bool
}

// Execute applies a rule inside the caller's transaction. Every write remains
// scoped to Rule.ProjectID; no effect can survive if the rule update or audit
// write performed by the caller fails.
func Execute(ctx context.Context, tx pgx.Tx, rule Rule, actorID *string, trigger Trigger, now time.Time) (Effect, error) {
	preferences, err := loadProjectPreferences(ctx, tx, rule.ProjectID)
	if err != nil {
		return Effect{}, err
	}
	policy, err := effectivePolicy(ctx, tx, rule)
	if err != nil {
		return Effect{}, err
	}
	if rule.Kind == "calendar_gap" {
		return createCalendarGap(ctx, tx, rule, preferences, actorID, trigger, policy, now)
	}
	return createContentEffects(ctx, tx, rule, preferences, actorID, trigger, policy)
}

func loadProjectPreferences(ctx context.Context, tx pgx.Tx, projectID string) (projectPreferences, error) {
	var preferences projectPreferences
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(profile.timezone, 'Europe/Zagreb'),
		       COALESCE(profile.primary_language, project.default_locale, 'hr'),
		       COALESCE(profile.social_posts_per_week, 4),
		       COALESCE(profile.newsletter_cadence, 'weekly'),
		       COALESCE(strategy.forbidden_topics, '')
		FROM projects AS project
		LEFT JOIN project_profiles AS profile ON profile.project_id = project.id
		LEFT JOIN project_strategies AS strategy ON strategy.project_id = project.id
		WHERE project.id = $1::uuid`, projectID).Scan(
		&preferences.Timezone, &preferences.PrimaryLanguage,
		&preferences.SocialPostsPerWeek, &preferences.NewsletterCadence,
		&preferences.ForbiddenTopics,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectPreferences{}, ErrProjectNotFound
	}
	if err != nil {
		return projectPreferences{}, err
	}
	preferences.PrimaryLanguage = strings.TrimSpace(preferences.PrimaryLanguage)
	if preferences.PrimaryLanguage != "en" {
		preferences.PrimaryLanguage = "hr"
	}
	return preferences, nil
}

func effectivePolicy(ctx context.Context, tx pgx.Tx, rule Rule) (automationPolicy, error) {
	policy := automationPolicy{}
	factCheck, factCheckConfigured := configuredBool(rule.Configuration, "factCheck")
	respectForbiddenTopics, forbiddenTopicsConfigured := configuredBool(rule.Configuration, "respectForbiddenTopics")
	if factCheckConfigured {
		policy.FactCheck = factCheck
	}
	if forbiddenTopicsConfigured {
		policy.RespectForbiddenTopics = respectForbiddenTopics
	}
	if rule.Kind == "master" || (factCheckConfigured && forbiddenTopicsConfigured) {
		return policy, nil
	}
	var inheritedFactCheck, inheritedForbiddenTopics bool
	err := tx.QueryRow(ctx, `
		SELECT CASE
		         WHEN jsonb_typeof(configuration->'factCheck') = 'boolean'
		           THEN (configuration->>'factCheck')::boolean
		         ELSE false
		       END,
		       CASE
		         WHEN jsonb_typeof(configuration->'respectForbiddenTopics') = 'boolean'
		           THEN (configuration->>'respectForbiddenTopics')::boolean
		         ELSE false
		       END
		FROM automation_rules
		WHERE project_id = $1::uuid AND rule_key = 'master' AND enabled
		LIMIT 1`, rule.ProjectID).Scan(&inheritedFactCheck, &inheritedForbiddenTopics)
	if errors.Is(err, pgx.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return automationPolicy{}, err
	}
	if !factCheckConfigured {
		policy.FactCheck = inheritedFactCheck
	}
	if !forbiddenTopicsConfigured {
		policy.RespectForbiddenTopics = inheritedForbiddenTopics
	}
	return policy, nil
}

func createContentEffects(
	ctx context.Context,
	tx pgx.Tx,
	rule Rule,
	preferences projectPreferences,
	actorID *string,
	trigger Trigger,
	policy automationPolicy,
) (Effect, error) {
	formats := contentFormats(rule)
	drafts := make(map[string]string, len(formats))
	material := strings.Builder{}
	material.WriteString(rule.Name)
	material.WriteByte('\n')
	material.WriteString(rule.Description)
	for _, format := range formats {
		body := draftBody(rule, format, preferences.PrimaryLanguage, trigger, policy.FactCheck)
		drafts[format] = body
		material.WriteByte('\n')
		material.WriteString(body)
	}
	matches := []string(nil)
	if policy.RespectForbiddenTopics {
		matches = matchForbiddenTopics(preferences.ForbiddenTopics, material.String())
	}
	packageID := ""
	if len(formats) > 1 {
		if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&packageID); err != nil {
			return Effect{}, err
		}
	}

	status := automationpolicy.ContentStatus(rule.ReviewPolicy)
	if policy.FactCheck || len(matches) > 0 {
		// The local engine can prepare a verification brief but cannot prove
		// external claims or override forbidden-topic policy. Either guardrail
		// therefore always enters review.
		status = "in_review"
	}
	effect := Effect{
		Type: "content_item", Status: status, Created: true,
		PackageID: packageID, FactCheck: policy.FactCheck,
		RespectForbiddenTopics: policy.RespectForbiddenTopics,
		ForbiddenTopicMatches:  matches,
	}
	for index, format := range formats {
		channels := contentChannels(rule, format)
		title := DraftTitle(rule.Name, format, len(formats) > 1)
		body := drafts[format]
		metadata := baseMetadata(rule, trigger, status, policy.FactCheck)
		applyForbiddenTopicMetadata(metadata, policy.RespectForbiddenTopics, matches)
		metadata["format"] = format
		metadata["configuredChannels"] = channels
		if packageID != "" {
			metadata["automationPackageId"] = packageID
			metadata["packageFormats"] = formats
			metadata["packageIndex"] = index
		}
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			return Effect{}, err
		}

		var contentID string
		err = tx.QueryRow(ctx, `
			INSERT INTO content_items (
				project_id, author_id, kind, status, title, summary, body,
				channels, source, metadata
			)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, 'bot', $9::jsonb)
			RETURNING id::text`, rule.ProjectID, actorID, format, status, title,
			contentSummary(rule, format, preferences.PrimaryLanguage), body, channels,
			string(metadataJSON)).Scan(&contentID)
		if err != nil {
			return Effect{}, err
		}
		for _, channel := range channels {
			if _, err := tx.Exec(ctx, `
				INSERT INTO content_variants (
					content_item_id, channel, locale, title, summary, body, status, metadata
				)
				VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
				contentID, channel, preferences.PrimaryLanguage, title,
				contentSummary(rule, format, preferences.PrimaryLanguage), body, status,
				string(metadataJSON)); err != nil {
				return Effect{}, err
			}
		}
		effect.IDs = append(effect.IDs, contentID)
		if effect.ID == "" {
			effect.ID = contentID
			effect.Title = title
		}
	}
	return effect, nil
}

func createCalendarGap(
	ctx context.Context,
	tx pgx.Tx,
	rule Rule,
	preferences projectPreferences,
	actorID *string,
	trigger Trigger,
	policy automationPolicy,
	now time.Time,
) (Effect, error) {
	channel := calendarChannel(rule)
	gapDays := configuredGapDays(rule, preferences.SocialPostsPerWeek)
	title := DraftTitle(rule.Name, automationContentKind(rule), false)
	effect := Effect{
		Type: "calendar_item", Title: title, Status: "skipped",
		Created: false, FactCheck: policy.FactCheck,
		RespectForbiddenTopics: policy.RespectForbiddenTopics, GapDays: gapDays,
	}
	if gapDays < 1 {
		effect.Title = noGapTitle(rule.Name, preferences.PrimaryLanguage)
		return effect, nil
	}

	location, err := time.LoadLocation(preferences.Timezone)
	if err != nil {
		location = time.UTC
	}
	windowStart := now.UTC()
	windowEnd := windowStart.In(location).AddDate(0, 0, gapDays).UTC()
	// The gap check and the linked writes must be one serialized operation for
	// each project/channel pair. Without a transaction-scoped lock, two workers
	// can both observe the same empty window and create duplicate suggestions.
	if err := lockCalendarGap(ctx, tx, rule.ProjectID, channel); err != nil {
		return Effect{}, err
	}
	var occupied bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM calendar_items
			WHERE project_id = $1::uuid AND channel = $2 AND status <> 'failed'
			  AND scheduled_for >= $3 AND scheduled_for < $4
		) OR EXISTS (
			SELECT 1
			FROM content_items
			WHERE project_id = $1::uuid AND $2 = ANY(channels) AND status <> 'failed'
			  AND scheduled_for >= $3 AND scheduled_for < $4
		)`, rule.ProjectID, channel, windowStart, windowEnd).Scan(&occupied)
	if err != nil {
		return Effect{}, err
	}
	if occupied {
		effect.Title = noGapTitle(rule.Name, preferences.PrimaryLanguage)
		return effect, nil
	}

	hour := configuredInt(rule.Configuration, "hour", 10, 0, 23)
	minute := configuredInt(rule.Configuration, "minute", 0, 0, 59)
	scheduledFor := nextLocalSlot(windowStart, location, hour, minute)
	kind := automationContentKind(rule)
	channels := []string{channel}
	body := draftBody(rule, kind, preferences.PrimaryLanguage, trigger, policy.FactCheck)
	matches := []string(nil)
	if policy.RespectForbiddenTopics {
		matches = matchForbiddenTopics(preferences.ForbiddenTopics, rule.Name+"\n"+rule.Description+"\n"+body)
	}
	effect.ForbiddenTopicMatches = matches
	contentStatus := automationpolicy.ContentStatus(rule.ReviewPolicy)
	calendarStatus := automationpolicy.CalendarStatus(rule.ReviewPolicy)
	if policy.FactCheck || len(matches) > 0 {
		contentStatus = "in_review"
		calendarStatus = "suggestion"
	}
	contentMetadata := baseMetadata(rule, trigger, contentStatus, policy.FactCheck)
	calendarMetadata := baseMetadata(rule, trigger, calendarStatus, policy.FactCheck)
	for _, metadata := range []map[string]any{contentMetadata, calendarMetadata} {
		applyForbiddenTopicMetadata(metadata, policy.RespectForbiddenTopics, matches)
		metadata["gapDays"] = gapDays
		metadata["gapWindowStart"] = windowStart
		metadata["gapWindowEnd"] = windowEnd
		metadata["timezone"] = location.String()
		metadata["format"] = kind
	}
	contentMetadataJSON, err := json.Marshal(contentMetadata)
	if err != nil {
		return Effect{}, err
	}
	calendarMetadataJSON, err := json.Marshal(calendarMetadata)
	if err != nil {
		return Effect{}, err
	}
	summary := contentSummary(rule, kind, preferences.PrimaryLanguage)

	var contentID string
	err = tx.QueryRow(ctx, `
		INSERT INTO content_items (
			project_id, author_id, kind, status, title, summary, body, channels,
			scheduled_for, source, metadata
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, 'bot', $10::jsonb)
		RETURNING id::text`, rule.ProjectID, actorID, kind, contentStatus, title,
		summary, body, channels, scheduledFor, string(contentMetadataJSON)).Scan(&contentID)
	if err != nil {
		return Effect{}, err
	}
	var variantID string
	err = tx.QueryRow(ctx, `
		INSERT INTO content_variants (
			content_item_id, channel, locale, title, summary, body, status,
			scheduled_for, metadata
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		RETURNING id::text`, contentID, channel, preferences.PrimaryLanguage, title,
		summary, body, contentStatus, scheduledFor, string(contentMetadataJSON)).Scan(&variantID)
	if err != nil {
		return Effect{}, err
	}
	var calendarID string
	err = tx.QueryRow(ctx, `
		INSERT INTO calendar_items (
			project_id, created_by, title, summary, channel, status, scheduled_for,
			content_item_id, content_variant_id, metadata
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8::uuid, $9::uuid, $10::jsonb)
		RETURNING id::text`, rule.ProjectID, actorID, title, summary, channel,
		calendarStatus, scheduledFor, contentID, variantID, string(calendarMetadataJSON)).Scan(&calendarID)
	if err != nil {
		return Effect{}, err
	}
	effect.ID = calendarID
	effect.IDs = []string{calendarID, contentID, variantID}
	effect.Status = calendarStatus
	effect.Created = true
	effect.ScheduledFor = &scheduledFor
	return effect, nil
}

func contentFormats(rule Rule) []string {
	if rule.Kind != "bot_event" {
		return []string{automationContentKind(rule)}
	}
	if configured := configuredStringSlice(rule.Configuration, "formats"); len(configured) > 0 {
		formats := make([]string, 0, len(configured))
		seen := map[string]bool{}
		for _, format := range configured {
			format = strings.ToLower(strings.TrimSpace(format))
			if validContentKind(format) && !seen[format] {
				seen[format] = true
				formats = append(formats, format)
			}
		}
		if len(formats) > 0 {
			return formats
		}
	}
	if configured, ok := configuredString(rule.Configuration, "contentKind"); ok && validContentKind(configured) {
		return []string{configured}
	}
	return []string{"social", "blog", "newsletter"}
}

func automationContentKind(rule Rule) string {
	if configured, ok := configuredString(rule.Configuration, "contentKind"); ok && validContentKind(configured) {
		return configured
	}
	if rule.Kind == "newsletter" || rule.Channel == "newsletter" {
		return "newsletter"
	}
	if rule.Channel == "blog" {
		return "blog"
	}
	return "social"
}

func validContentKind(value string) bool {
	switch value {
	case "source", "social", "blog", "newsletter", "press_release", "case_study", "event":
		return true
	default:
		return false
	}
}

func contentChannels(rule Rule, kind string) []string {
	if kind == "source" {
		return []string{}
	}
	if kind == "blog" || kind == "newsletter" {
		return []string{kind}
	}
	if configured := configuredStringSlice(rule.Configuration, "channels"); len(configured) > 0 {
		channels := normalizedChannels(configured)
		if len(channels) > 0 {
			return channels
		}
	}
	if configured, ok := configuredString(rule.Configuration, "channel"); ok {
		if channels := normalizedChannels([]string{configured}); len(channels) > 0 {
			return channels
		}
	}
	ruleChannel := strings.ToLower(strings.TrimSpace(rule.Channel))
	if rule.Kind != "bot_event" || (ruleChannel != "whatsapp" && ruleChannel != "telegram") {
		if channels := normalizedChannels([]string{ruleChannel}); len(channels) > 0 {
			return channels
		}
	}
	if kind == "press_release" {
		return []string{"media"}
	}
	return []string{"linkedin"}
}

func normalizedChannels(values []string) []string {
	channels := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || len([]rune(value)) > 50 || !validContentChannel(value) || seen[value] {
			continue
		}
		seen[value] = true
		channels = append(channels, value)
	}
	return channels
}

func calendarChannel(rule Rule) string {
	for _, channel := range configuredStringSlice(rule.Configuration, "channels") {
		channel = strings.ToLower(strings.TrimSpace(channel))
		if validCalendarChannel(channel) {
			return channel
		}
	}
	if configured, ok := configuredString(rule.Configuration, "channel"); ok && validCalendarChannel(configured) {
		return configured
	}
	ruleChannel := strings.ToLower(strings.TrimSpace(rule.Channel))
	if validCalendarChannel(ruleChannel) {
		return ruleChannel
	}
	return "linkedin"
}

func validContentChannel(value string) bool {
	switch value {
	case "linkedin", "facebook", "instagram", "blog", "newsletter", "website", "media",
		"youtube", "x", "reddit", "pinterest", "threads", "telegram":
		return true
	default:
		return false
	}
}

func validCalendarChannel(value string) bool {
	switch value {
	case "linkedin", "facebook", "instagram", "blog", "newsletter", "youtube", "x", "reddit", "pinterest", "threads":
		return true
	default:
		return false
	}
}

func lockCalendarGap(ctx context.Context, tx pgx.Tx, projectID, channel string) error {
	lockKey := "millena:calendar-gap:" + projectID + ":" + channel
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey)
	return err
}

func configuredGapDays(rule Rule, socialPostsPerWeek int) int {
	if days, ok := configuredNumber(rule.Configuration, "gapDays"); ok {
		if days >= 1 && days <= 365 {
			return days
		}
	}
	if days, ok := gapDaysFromSchedule(rule.ScheduleRule); ok {
		return days
	}
	if socialPostsPerWeek < 1 {
		return 0
	}
	return max(1, int(math.Ceil(7/float64(socialPostsPerWeek))))
}

func gapDaysFromSchedule(schedule string) (int, bool) {
	schedule = strings.ToLower(strings.TrimSpace(schedule))
	if !strings.HasPrefix(schedule, "gap:") {
		return 0, false
	}
	days, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(schedule, "gap:")), "d"))
	return days, err == nil && days >= 1 && days <= 365
}

func nextLocalSlot(now time.Time, location *time.Location, hour, minute int) time.Time {
	localNow := now.In(location)
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if !candidate.After(localNow) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC()
}

func DraftTitle(name, format string, includeFormat bool) string {
	title := "Automatizacija: " + strings.TrimSpace(name)
	if includeFormat {
		title += " · " + formatLabel(format)
	}
	if runes := []rune(title); len(runes) > 180 {
		title = string(runes[:180])
	}
	return title
}

func formatLabel(format string) string {
	switch format {
	case "social":
		return "Social"
	case "blog":
		return "Blog"
	case "newsletter":
		return "Newsletter"
	case "press_release":
		return "Priopćenje"
	case "case_study":
		return "Studija slučaja"
	case "event":
		return "Događaj"
	default:
		return strings.ReplaceAll(format, "_", " ")
	}
}

func contentSummary(rule Rule, format, language string) string {
	description := strings.TrimSpace(rule.Description)
	if description != "" {
		return description
	}
	if language == "en" {
		return "A structured " + strings.ReplaceAll(format, "_", " ") + " draft prepared by the project automation."
	}
	return "Strukturirani nacrt formata „" + formatLabel(format) + "” pripremljen projektnom automatizacijom."
}

func draftBody(rule Rule, format, language string, trigger Trigger, factCheck bool) string {
	description := strings.TrimSpace(rule.Description)
	if description == "" {
		if language == "en" {
			description = "Prepare the content from the active project strategy and available context."
		} else {
			description = "Pripremiti sadržaj prema aktivnoj strategiji i dostupnom kontekstu projekta."
		}
	}
	var structure string
	if language == "en" {
		switch format {
		case "blog":
			structure = "Working structure: headline, opening insight, supporting arguments, examples and a clear call to action."
		case "newsletter":
			structure = "Working structure: subject, preheader, editorial introduction, key item and call to action."
		case "social":
			structure = "Working structure: hook, one useful insight, supporting proof and a channel-appropriate call to action."
		default:
			structure = "Working structure: key message, supporting evidence, required details and next action."
		}
		if factCheck {
			structure += " Fact checking is required before approval; verify every name, date, figure and external claim."
		}
		return fmt.Sprintf("%s\n\n%s\n\nCreated by the %s automation run of rule %q.", description, structure, trigger, rule.Name)
	}
	switch format {
	case "blog":
		structure = "Radna struktura: naslov, uvodni uvid, argumenti, primjeri i jasan poziv na akciju."
	case "newsletter":
		structure = "Radna struktura: predmet, preheader, urednički uvod, glavna stavka i poziv na akciju."
	case "social":
		structure = "Radna struktura: udica, jedan koristan uvid, dokaz i poziv na akciju prilagođen kanalu."
	default:
		structure = "Radna struktura: ključna poruka, dokazi, obavezni detalji i sljedeći korak."
	}
	if factCheck {
		structure += " Provjera činjenica obavezna je prije odobrenja; provjerite svako ime, datum, broj i vanjsku tvrdnju."
	}
	return fmt.Sprintf("%s\n\n%s\n\nNastalo %s pokretanjem pravila „%s”.", description, structure, triggerLabel(trigger), rule.Name)
}

func triggerLabel(trigger Trigger) string {
	if trigger == ScheduledTrigger {
		return "zakazanim"
	}
	return "ručnim"
}

func noGapTitle(name, language string) string {
	if language == "en" {
		return "No calendar gap: " + strings.TrimSpace(name)
	}
	return "Nema praznine u kalendaru: " + strings.TrimSpace(name)
}

func baseMetadata(rule Rule, trigger Trigger, status string, factCheck bool) map[string]any {
	metadata := map[string]any{
		"automationRuleId":  rule.ID,
		"reviewPolicy":      rule.ReviewPolicy,
		"status":            status,
		"factCheckRequired": factCheck,
	}
	if trigger == ScheduledTrigger {
		metadata["scheduledRun"] = true
		metadata["sandbox"] = true
	} else {
		metadata["manualRun"] = true
	}
	return metadata
}

func applyForbiddenTopicMetadata(metadata map[string]any, enabled bool, matches []string) {
	if matches == nil {
		matches = []string{}
	}
	metadata["respectForbiddenTopics"] = enabled
	metadata["forbiddenTopicsChecked"] = enabled
	metadata["forbiddenTopicMatches"] = matches
	metadata["forbiddenTopicReviewRequired"] = len(matches) > 0
}

func matchForbiddenTopics(forbiddenTopics, material string) []string {
	normalizedMaterial := normalizePolicyText(material)
	if normalizedMaterial == "" {
		return []string{}
	}
	parts := strings.FieldsFunc(forbiddenTopics, func(character rune) bool {
		switch character {
		case ',', ';', '\n', '\r', '|', '•':
			return true
		default:
			return false
		}
	})
	matches := make([]string, 0)
	seen := map[string]bool{}
	for _, part := range parts {
		label := strings.Trim(strings.TrimSpace(part), ".:!?–—-\"'„“”")
		normalized := normalizePolicyText(label)
		if len([]rune(normalized)) < 3 || seen[normalized] || !containsNormalizedPhrase(normalizedMaterial, normalized) {
			continue
		}
		seen[normalized] = true
		matches = append(matches, label)
	}
	return matches
}

func containsNormalizedPhrase(material, phrase string) bool {
	if material == "" || phrase == "" {
		return false
	}
	return strings.Contains(" "+material+" ", " "+phrase+" ")
}

func normalizePolicyText(value string) string {
	value = strings.ToLower(value)
	var normalized strings.Builder
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9',
			character == 'č', character == 'ć', character == 'đ', character == 'š', character == 'ž':
			normalized.WriteRune(character)
		default:
			normalized.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(normalized.String()), " ")
}

func configuredString(configuration map[string]any, key string) (string, bool) {
	value, ok := configuration[key].(string)
	value = strings.ToLower(strings.TrimSpace(value))
	return value, ok && value != ""
}

func configuredStringSlice(configuration map[string]any, key string) []string {
	value, ok := configuration[key]
	if !ok {
		return nil
	}
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func configuredBool(configuration map[string]any, key string) (bool, bool) {
	value, ok := configuration[key]
	if !ok {
		return false, false
	}
	configured, valid := value.(bool)
	return configured, valid
}

func configuredNumber(configuration map[string]any, key string) (int, bool) {
	value, ok := configuration[key]
	if !ok {
		return 0, false
	}
	switch number := value.(type) {
	case int:
		return number, true
	case int32:
		return int(number), true
	case int64:
		return int(number), true
	case float64:
		if number != math.Trunc(number) {
			return 0, false
		}
		return int(number), true
	case json.Number:
		parsed, err := strconv.Atoi(string(number))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func configuredInt(configuration map[string]any, key string, fallback, minimum, maximum int) int {
	value, ok := configuredNumber(configuration, key)
	if !ok || value < minimum || value > maximum {
		return fallback
	}
	return value
}
