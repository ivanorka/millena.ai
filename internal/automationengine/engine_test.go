package automationengine

import (
	"strings"
	"testing"
	"time"
)

func TestBotEventFormatsAndChannelsUseExplicitConfiguration(t *testing.T) {
	rule := Rule{
		Kind: "bot_event", Channel: "whatsapp",
		Configuration: map[string]any{
			"formats":  []any{"social", "blog", "newsletter", "social", "invalid"},
			"channels": []any{"instagram", "linkedin", "instagram"},
		},
	}
	formats := contentFormats(rule)
	if strings.Join(formats, ",") != "social,blog,newsletter" {
		t.Fatalf("contentFormats() = %#v", formats)
	}
	channels := contentChannels(rule, "social")
	if strings.Join(channels, ",") != "instagram,linkedin" {
		t.Fatalf("contentChannels() = %#v", channels)
	}
	if channels := contentChannels(rule, "blog"); len(channels) != 1 || channels[0] != "blog" {
		t.Fatalf("blog channels = %#v", channels)
	}
}

func TestBotEventExplicitTargetsPreserveTelegramAndTriggerFallbacks(t *testing.T) {
	explicitTargets := Rule{
		Kind:          "bot_event",
		Channel:       "whatsapp",
		Configuration: map[string]any{"channels": []any{"telegram", "linkedin", "instagram"}},
	}
	if channels := contentChannels(explicitTargets, "social"); strings.Join(channels, ",") != "telegram,linkedin,instagram" {
		t.Fatalf("explicit target channels = %#v", channels)
	}
	explicitTargets.Configuration = map[string]any{"channels": []any{"whatsapp", "telegram", "not-a-channel"}}
	if channels := contentChannels(explicitTargets, "social"); len(channels) != 1 || channels[0] != "telegram" {
		t.Fatalf("invalid value must not hide telegram output: %#v", channels)
	}

	explicitSingleTarget := Rule{
		Kind: "bot_event", Channel: "whatsapp",
		Configuration: map[string]any{"channel": "telegram"},
	}
	if channels := contentChannels(explicitSingleTarget, "social"); len(channels) != 1 || channels[0] != "telegram" {
		t.Fatalf("explicit single target = %#v", channels)
	}

	for _, trigger := range []string{"whatsapp", "telegram"} {
		inputOnly := Rule{Kind: "bot_event", Channel: trigger, Configuration: map[string]any{}}
		if channels := contentChannels(inputOnly, "social"); len(channels) != 1 || channels[0] != "linkedin" {
			t.Fatalf("%s input fallback channels = %#v", trigger, channels)
		}
	}

	validRuleFallback := Rule{Kind: "bot_event", Channel: "facebook", Configuration: map[string]any{}}
	if channels := contentChannels(validRuleFallback, "social"); len(channels) != 1 || channels[0] != "facebook" {
		t.Fatalf("valid bot rule fallback channels = %#v", channels)
	}
}

func TestCalendarChannelUsesFirstValidConfiguredChannelAndFallbacks(t *testing.T) {
	rule := Rule{
		Channel: "instagram",
		Configuration: map[string]any{
			"channels": []any{"telegram", " FACEbook ", "linkedin"},
			"channel":  "reddit",
		},
	}
	if got := calendarChannel(rule); got != "facebook" {
		t.Fatalf("calendarChannel() = %q, expected first valid configured channel", got)
	}

	rule.Configuration = map[string]any{"channels": []any{"telegram", "website"}, "channel": "reddit"}
	if got := calendarChannel(rule); got != "reddit" {
		t.Fatalf("calendarChannel() configured-channel fallback = %q", got)
	}

	rule.Configuration = map[string]any{"channels": []any{"telegram"}}
	if got := calendarChannel(rule); got != "instagram" {
		t.Fatalf("calendarChannel() rule-channel fallback = %q", got)
	}
}

func TestConfiguredContentKindAndGapDaysHavePrecedence(t *testing.T) {
	rule := Rule{
		Kind: "newsletter", Channel: "newsletter", ScheduleRule: "gap:9d",
		Configuration: map[string]any{"contentKind": "case_study", "gapDays": float64(3)},
	}
	if got := automationContentKind(rule); got != "case_study" {
		t.Fatalf("automationContentKind() = %q", got)
	}
	if got := configuredGapDays(rule, 1); got != 3 {
		t.Fatalf("configuredGapDays() = %d", got)
	}
	if got := configuredGapDays(Rule{ScheduleRule: "gap:5d"}, 100); got != 5 {
		t.Fatalf("schedule gap days = %d", got)
	}
	if got := configuredGapDays(Rule{}, 4); got != 2 {
		t.Fatalf("profile-derived gap days = %d", got)
	}
	if got := configuredGapDays(Rule{}, 0); got != 0 {
		t.Fatalf("disabled profile cadence gap days = %d", got)
	}
}

func TestFactCheckDraftCannotClaimAutomaticApproval(t *testing.T) {
	body := draftBody(Rule{Name: "Claims"}, "social", "en", ScheduledTrigger, true)
	if !strings.Contains(body, "Fact checking is required") || !strings.Contains(body, "scheduled automation run") {
		t.Fatalf("fact-check body = %q", body)
	}
	longName := string(make([]rune, 200))
	if got := len([]rune(DraftTitle(longName, "social", true))); got > 180 {
		t.Fatalf("DraftTitle() has %d runes", got)
	}
}

func TestForbiddenTopicMatcherIsPhraseBasedAndCaseInsensitive(t *testing.T) {
	matches := matchForbiddenTopics(
		"Tajni projekt; osobni podaci, politički stavovi",
		"Nacrt spominje TAJNI-projekt, ali ne obrađuje ostale kategorije.",
	)
	if len(matches) != 1 || matches[0] != "Tajni projekt" {
		t.Fatalf("matchForbiddenTopics() = %#v", matches)
	}
	metadata := map[string]any{}
	applyForbiddenTopicMetadata(metadata, true, matches)
	if metadata["forbiddenTopicReviewRequired"] != true || metadata["forbiddenTopicsChecked"] != true {
		t.Fatalf("forbidden-topic metadata = %#v", metadata)
	}
}

func TestForbiddenTopicMatcherRequiresWordBoundariesAndPreservesCroatianLetters(t *testing.T) {
	matches := matchForbiddenTopics(
		"rat; čista energija; žar; šuma",
		"Strategiji je cilj ČISTA-ENERGIJA, dok se požari i šumarstvo spominju samo kao kontekst.",
	)
	if len(matches) != 1 || matches[0] != "čista energija" {
		t.Fatalf("boundary-aware Croatian matches = %#v", matches)
	}

	matches = matchForbiddenTopics("rat; osobni podaci", "Rat i osobni podaci ostaju zabranjeni.")
	if strings.Join(matches, ",") != "rat,osobni podaci" {
		t.Fatalf("whole-word and whole-phrase matches = %#v", matches)
	}
}

func TestNextLocalSlotUsesProjectTimezoneAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	now := time.Date(2026, time.March, 28, 11, 0, 0, 0, location)
	next := nextLocalSlot(now, location, 10, 0).In(location)
	if next.Day() != 29 || next.Hour() != 10 {
		t.Fatalf("next local slot = %s", next)
	}
}
