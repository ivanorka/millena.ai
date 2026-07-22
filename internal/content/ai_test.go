package content

import (
	"context"
	"strings"
	"testing"
)

func TestLocalAIGeneratesWithStrategyContext(t *testing.T) {
	service := NewAIService(AIOptions{Provider: "local"})
	strategy := Strategy{
		Revision:       3,
		Audience:       "direktorice marketinga",
		BrandMessage:   "Povjerenje nastaje kroz jasne i provjerene poruke.",
		ProofPoints:    "studije slučaja i potvrđeni rezultati",
		PriorityTopics: []string{"Korporativne komunikacije"},
	}
	result, err := service.Run(context.Background(), strategy, AIInput{
		Operation: "generate", Kind: "social", Brief: "Kako komunicirati promjenu vodstva", Language: "hr",
	})
	if err != nil {
		t.Fatalf("Run() returned an error: %v", err)
	}
	if !result.ContextUsed || result.StrategyRevision != 3 {
		t.Fatalf("expected strategy context revision 3, got %#v", result)
	}
	if !strings.Contains(strings.ToLower(result.Body), "direktorice marketinga") {
		t.Fatalf("expected audience in generated body, got %q", result.Body)
	}
}

func TestLocalAIUsesPersistedPersonaWithoutStrategyRevision(t *testing.T) {
	service := NewAIService(AIOptions{Provider: "local"})
	strategy := Strategy{Personas: []PersonaContext{{
		Name:         "Operativne direktorice",
		Description:  "Vode digitalnu transformaciju i trebaju praktične korake",
		Demographics: "B2B tvrtke s 50–250 zaposlenih",
		IsPrimary:    true,
	}}}
	result, err := service.Run(context.Background(), strategy, AIInput{
		Operation: "generate", Kind: "social", Brief: "Uvođenje novog procesa", Language: "hr",
	})
	if err != nil {
		t.Fatalf("Run() returned an error: %v", err)
	}
	if !result.ContextUsed || !strings.Contains(result.ContextSummary, "Operativne direktorice") {
		t.Fatalf("expected persisted persona context, got %#v", result)
	}
	for _, expected := range []string{"operativne direktorice", "digitalnu transformaciju", "50–250 zaposlenih"} {
		if !strings.Contains(strings.ToLower(result.Body), strings.ToLower(expected)) {
			t.Fatalf("expected generated body to use persona detail %q, got %q", expected, result.Body)
		}
	}
}

func TestLocalAIUsesEnglishProjectLocaleAndOrganization(t *testing.T) {
	service := NewAIService(AIOptions{Provider: "local"})
	strategy := Strategy{
		OrganizationName: "Northstar Studio",
		DefaultLocale:    "en",
		Audience:         "operations leaders",
		BrandMessage:     "Useful change starts with a clear next step.",
	}
	for _, kind := range []string{"newsletter", "press_release"} {
		result, err := service.Run(context.Background(), strategy, AIInput{
			Operation: "generate", Kind: kind, Brief: "A practical guide to process change",
		})
		if err != nil {
			t.Fatalf("Run(%s) returned an error: %v", kind, err)
		}
		if result.Language != "en" || !strings.Contains(result.Body, "Northstar Studio") {
			t.Fatalf("expected English organization-aware %s, got %#v", kind, result)
		}
		for _, unexpected := range []string{"MPR Grupa", "Predmet:", "Dodatne informacije"} {
			if strings.Contains(result.Body, unexpected) {
				t.Fatalf("English %s contains hardcoded/Croatian text %q: %s", kind, unexpected, result.Body)
			}
		}
	}
}

func TestLocalAIRefinesExistingCopy(t *testing.T) {
	service := NewAIService(AIOptions{})
	result, err := service.Run(context.Background(), Strategy{}, AIInput{
		Operation: "refine", Kind: "social", Title: "kratka objava", Body: "ovo je prvi nacrt teksta",
	})
	if err != nil {
		t.Fatalf("Run() returned an error: %v", err)
	}
	if !strings.Contains(result.Body, "?") || !strings.Contains(result.Body, "Ovo je prvi") {
		t.Fatalf("expected refined structure and CTA, got %q", result.Body)
	}
}

func TestLocalAICaseStudyUsesNaturalPunctuation(t *testing.T) {
	service := NewAIService(AIOptions{})
	result, err := service.Run(context.Background(), Strategy{
		Revision:     1,
		Audience:     "uprave i komunikacijski timovi.",
		BrandMessage: "Komunikacija treba biti jasna i dokaziva.",
		ProofPoints:  "studije slučaja i potvrđeni rezultati.",
	}, AIInput{
		Operation: "generate", Kind: "case_study", Brief: "pretvaranje događaja u niz korisnih sadržaja.",
	})
	if err != nil {
		t.Fatalf("Run() returned an error: %v", err)
	}
	if strings.Contains(result.Body, "..") || strings.Contains(result.Body, ". je otvorio") {
		t.Fatalf("expected natural punctuation, got %q", result.Body)
	}
}

func TestOllamaPromptMapsEveryStrategyField(t *testing.T) {
	prompt := buildOllamaPrompt(Strategy{
		OrganizationName: "organization",
		SixMonthGoal:     "goal",
		PrimaryGoals:     []string{"primary"},
		PriorityTopics:   []string{"topic"},
		Audience:         "audience",
		AudienceProblem:  "problem",
		BrandMessage:     "message",
		ProofPoints:      "proof",
		ForbiddenTopics:  "forbidden",
		SuccessMetrics:   "metrics",
		Tone:             "tone",
		SourceText:       "source",
		Personas: []PersonaContext{{
			Name: "persona-name", Description: "persona-description",
			Demographics: "persona-demographics", IsPrimary: true,
		}},
	}, AIInput{Operation: "generate", Kind: "blog", Brief: "brief", Language: "en"})
	for _, value := range []string{"organization", "goal", "primary", "topic", "audience", "problem", "message", "proof", "forbidden", "metrics", "tone", "source", "persona-name", "persona-description", "persona-demographics", "en"} {
		if !strings.Contains(prompt, value) {
			t.Fatalf("expected prompt to contain %q: %s", value, prompt)
		}
	}
	if strings.Contains(prompt, "%!") {
		t.Fatalf("prompt contains a formatting error: %s", prompt)
	}
}
