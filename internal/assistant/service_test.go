package assistant

import (
	"strings"
	"testing"

	"github.com/ivanorka/millena-ai/internal/assets"
)

func TestInferKindUsesCategoryWords(t *testing.T) {
	cases := map[string]string{
		"pripremi LinkedIn objavu": "social",
		"napiši blog članak":       "blog",
		"kreiraj newsletter":       "newsletter",
		"napravi priopćenje":       "press_release",
		"pripremi studiju slučaja": "case_study",
	}
	for input, expected := range cases {
		if actual := inferKind(input); actual != expected {
			t.Fatalf("inferKind(%q) = %q, expected %q", input, actual, expected)
		}
	}
}

func TestAutomationIntentRequiresExplicitVerb(t *testing.T) {
	if _, _, ok := automationIntent("što je s linkedin pravilom"); ok {
		t.Fatal("read-only question must not mutate an automation")
	}
	enabled, channel, ok := automationIntent("isključi instagram automatizaciju")
	if !ok || enabled || channel != "instagram" {
		t.Fatalf("unexpected intent: enabled=%v channel=%q ok=%v", enabled, channel, ok)
	}
}

func TestAutomationFeatureRequiresActiveBooleanEntitlement(t *testing.T) {
	cases := []struct {
		name     string
		status   string
		features map[string]any
		want     bool
	}{
		{name: "active and enabled", status: "active", features: map[string]any{"automations": true}, want: true},
		{name: "trial and enabled", status: "trial", features: map[string]any{"automations": true}, want: true},
		{name: "disabled", status: "active", features: map[string]any{"automations": false}},
		{name: "wrong JSON type", status: "active", features: map[string]any{"automations": "true"}},
		{name: "inactive", status: "past_due", features: map[string]any{"automations": true}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := automationFeatureEnabled(test.status, test.features); got != test.want {
				t.Fatalf("automationFeatureEnabled() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBuildAttachmentContextUsesRuneLimitAndSkipsBinary(t *testing.T) {
	text := "Četiri ključne poruke"
	items := []assets.ContextAsset{
		{Reference: assets.Reference{Filename: "photo.png"}},
		{Reference: assets.Reference{Filename: "strategy.txt"}, ExtractedText: &text},
	}
	context := buildAttachmentContext(items, 20)
	if len([]rune(context)) != 20 {
		t.Fatalf("expected 20 runes, got %d in %q", len([]rune(context)), context)
	}
	if strings.Contains(context, "photo.png") {
		t.Fatalf("binary attachment leaked into text context: %q", context)
	}
}

func TestLocalAttachmentResponseExplainsTextAndBinaryHandling(t *testing.T) {
	text := "Strategija traži fokus na stručnost i mjerljive rezultate."
	response := localAttachmentResponse([]assets.ContextAsset{
		{Reference: assets.Reference{Filename: "strategy.txt"}, ExtractedText: &text},
		{Reference: assets.Reference{Filename: "visual.png"}},
	})
	for _, expected := range []string{"2 privitka", "strategy.txt", "visual.png", "ne radi vizualnu analizu"} {
		if !strings.Contains(response, expected) {
			t.Fatalf("response %q does not contain %q", response, expected)
		}
	}
}
