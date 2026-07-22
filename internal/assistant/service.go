package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/ivanorka/millena-ai/internal/assets"
	"github.com/ivanorka/millena-ai/internal/content"
)

type Service struct {
	repository *Repository
	ai         *content.AIService
}

func NewService(repository *Repository, ai *content.AIService) *Service {
	if ai == nil {
		ai = content.NewAIService(content.AIOptions{})
	}
	return &Service{repository: repository, ai: ai}
}

func (s *Service) Send(ctx context.Context, projectID, threadID, userID, body string, attachmentIDs []string) (SendResult, error) {
	attachments, err := s.repository.Attachments(ctx, projectID, attachmentIDs)
	if err != nil {
		return SendResult{}, err
	}
	strategy, err := s.repository.Strategy(ctx, projectID)
	if err != nil {
		return SendResult{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(body))
	response := ""
	actionType := "assistant.answer"
	attachmentContext := buildAttachmentContext(attachments, 16000)
	if attachmentContext != "" {
		strategy.SourceText = strings.TrimSpace(strategy.SourceText + "\n\nKontekst privitaka:\n" + attachmentContext)
		strategy.ProofPoints = strings.TrimSpace(strategy.ProofPoints + "\n" + shorten(attachmentContext, 1200))
	}
	references := attachmentReferences(attachments)
	metadata := map[string]any{
		"provider": s.ai.Status().Provider, "strategyRevision": strategy.Revision,
		"contextUsed": true, "attachmentIds": attachmentIDs, "attachments": references,
		"attachmentContextUsed": attachmentContext != "", "personaCount": len(strategy.Personas),
	}
	var createdContentID *string
	var affectedRuleID *string

	if isCreateIntent(normalized) {
		kind := inferKind(normalized)
		generated, err := s.ai.Run(ctx, strategy, content.AIInput{Operation: "generate", Kind: kind, Brief: body, Language: strategy.DefaultLocale})
		if err != nil {
			return SendResult{}, err
		}
		id, err := s.repository.CreateDraft(ctx, projectID, userID, kind, generated.Title, generated.Body, strategy.Revision)
		if err != nil {
			return SendResult{}, err
		}
		createdContentID = &id
		actionType = "content.created"
		metadata["kind"] = kind
		metadata["provider"] = generated.Provider
		metadata["language"] = generated.Language
		if generated.Language == "en" {
			response = fmt.Sprintf("I prepared “%s” and saved it as a draft in Content. I used strategy revision %d and the persisted project context; please verify facts and dates before publishing.", generated.Title, strategy.Revision)
		} else {
			response = fmt.Sprintf("Pripremila sam %s „%s” i spremila ga kao skicu u Sadržaj. Koristila sam strategiju revizije %d; prije objave još provjerite činjenice i termine.", kindLabel(kind), generated.Title, strategy.Revision)
		}
	} else if enabled, channel, ok := automationIntent(normalized); ok {
		id, err := s.repository.ToggleRule(ctx, projectID, channel, enabled)
		if err != nil {
			return SendResult{}, err
		}
		affectedRuleID = &id
		actionType = "automation.updated"
		metadata["channel"] = channel
		metadata["enabled"] = enabled
		state := "uključeno"
		if !enabled {
			state = "isključeno"
		}
		response = fmt.Sprintf("Pravilo za %s sada je %s. Promjena je spremljena i odmah utječe na automatizacije projekta.", channel, state)
	} else if len(attachments) > 0 {
		response = localAttachmentResponse(attachments)
		actionType = "attachments.reviewed"
	} else if strings.Contains(normalized, "kalendar") || strings.Contains(normalized, "zakazan") || strings.Contains(normalized, "raspored") {
		items, err := s.repository.UpcomingCalendar(ctx, projectID)
		if err != nil {
			return SendResult{}, err
		}
		if len(items) == 0 {
			response = "U kalendaru nema budućih stavki. Mogu pripremiti nacrt iz strategije, a termin ćete zatim potvrditi."
		} else {
			response = "Sljedeće u kalendaru:\n• " + strings.Join(items, "\n• ")
		}
		actionType = "calendar.read"
	} else {
		workspace, err := s.repository.Context(ctx, projectID)
		if err != nil {
			return SendResult{}, err
		}
		response = fmt.Sprintf("Projekt trenutno ima %d sadržajnih zapisa: %d skica, %d za pregled i %d zakazanih. U idućih 14 dana kalendar ima %d stavki, aktivnih kontakata je %d, a uključeno je %d automatizacijskih pravila. Glavni strateški cilj je: %s Mogu odmah pripremiti nacrt ili uključiti/isključiti pravilo za kanal.", workspace.ContentTotal, workspace.Drafts, workspace.InReview, workspace.Scheduled, workspace.CalendarNext14, workspace.ActiveContacts, workspace.EnabledRules, strategy.SixMonthGoal)
		actionType = "workspace.summary"
	}

	userMessage, assistantMessage, err := s.repository.SaveExchange(ctx, projectID, threadID, userID, body, response, actionType, firstNonNil(createdContentID, affectedRuleID), metadata, attachmentIDs)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{UserMessage: userMessage, AssistantMessage: assistantMessage, CreatedContentID: createdContentID, AffectedRuleID: affectedRuleID}, nil
}

func buildAttachmentContext(items []assets.ContextAsset, limit int) string {
	if limit < 1 {
		return ""
	}
	parts := make([]string, 0, len(items))
	used := 0
	for _, item := range items {
		if item.ExtractedText == nil || strings.TrimSpace(*item.ExtractedText) == "" {
			continue
		}
		part := "Datoteka " + item.Filename + ":\n" + strings.TrimSpace(*item.ExtractedText)
		separatorLength := 0
		if len(parts) > 0 {
			separatorLength = 2
		}
		remaining := limit - used - separatorLength
		if remaining <= 0 {
			break
		}
		runes := []rune(part)
		if len(runes) > remaining {
			part = string(runes[:remaining])
			used += remaining + separatorLength
			parts = append(parts, part)
			break
		}
		used += len(runes) + separatorLength
		parts = append(parts, part)
	}
	return strings.Join(parts, "\n\n")
}

func attachmentReferences(items []assets.ContextAsset) []assets.Reference {
	result := make([]assets.Reference, 0, len(items))
	for _, item := range items {
		result = append(result, item.Reference)
	}
	return result
}

func localAttachmentResponse(items []assets.ContextAsset) string {
	textItems := make([]string, 0, len(items))
	binaryNames := make([]string, 0, len(items))
	for _, item := range items {
		if item.ExtractedText == nil || strings.TrimSpace(*item.ExtractedText) == "" {
			binaryNames = append(binaryNames, item.Filename)
			continue
		}
		textItems = append(textItems, item.Filename+": "+shorten(*item.ExtractedText, 700))
	}
	parts := []string{fmt.Sprintf("Učitala sam %d privitka i povezala ih s ovim razgovorom.", len(items))}
	if len(textItems) > 0 {
		parts = append(parts, "Izvučeni lokalni kontekst:\n• "+strings.Join(textItems, "\n• "))
	}
	if len(binaryNames) > 0 {
		parts = append(parts, "Binarne datoteke bez tekstualnog sloja: "+strings.Join(binaryNames, ", ")+". Spremljene su i dostupne za preuzimanje, ali lokalni tekstualni engine ne radi vizualnu analizu slike.")
	}
	return strings.Join(parts, "\n\n")
}

func isCreateIntent(value string) bool {
	verb := strings.Contains(value, "napravi") || strings.Contains(value, "pripremi") || strings.Contains(value, "kreiraj") || strings.Contains(value, "napiši") || strings.Contains(value, "generiraj")
	object := strings.Contains(value, "objav") || strings.Contains(value, "blog") || strings.Contains(value, "newsletter") || strings.Contains(value, "priopćen") || strings.Contains(value, "studij") || strings.Contains(value, "događaj") || strings.Contains(value, "sadržaj")
	return verb && object
}

func inferKind(value string) string {
	switch {
	case strings.Contains(value, "newsletter") || strings.Contains(value, "email"):
		return "newsletter"
	case strings.Contains(value, "priopćen") || strings.Contains(value, "medij"):
		return "press_release"
	case strings.Contains(value, "studij") || strings.Contains(value, "case"):
		return "case_study"
	case strings.Contains(value, "događaj") || strings.Contains(value, "poziv"):
		return "event"
	case strings.Contains(value, "blog") || strings.Contains(value, "članak"):
		return "blog"
	default:
		return "social"
	}
}

func kindLabel(kind string) string {
	return map[string]string{
		"social": "društvenu objavu", "blog": "blog članak", "newsletter": "newsletter",
		"press_release": "priopćenje", "case_study": "studiju slučaja", "event": "najavu događaja",
	}[kind]
}

func automationIntent(value string) (bool, string, bool) {
	enabled := true
	if strings.Contains(value, "isključi") || strings.Contains(value, "pauziraj") || strings.Contains(value, "zaustavi") {
		enabled = false
	} else if !strings.Contains(value, "uključi") && !strings.Contains(value, "aktiviraj") {
		return false, "", false
	}
	for _, channel := range []string{"linkedin", "instagram", "facebook", "youtube", "reddit", "pinterest", "threads", "telegram", "newsletter", "blog", "x"} {
		if strings.Contains(value, channel) {
			return enabled, channel, true
		}
	}
	return false, "", false
}

func firstNonNil(values ...*string) *string {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
