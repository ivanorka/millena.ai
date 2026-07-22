package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type AIOptions struct {
	Provider      string
	OllamaBaseURL string
	OllamaModel   string
	Timeout       time.Duration
}

type AIService struct {
	provider string
	ollama   *ollamaProvider
}

func NewAIService(options AIOptions) *AIService {
	provider := strings.ToLower(strings.TrimSpace(options.Provider))
	if provider == "" {
		provider = "local"
	}
	service := &AIService{provider: provider}
	if provider == "ollama" && strings.TrimSpace(options.OllamaModel) != "" {
		timeout := options.Timeout
		if timeout <= 0 {
			timeout = 90 * time.Second
		}
		service.ollama = &ollamaProvider{
			baseURL: strings.TrimRight(options.OllamaBaseURL, "/"),
			model:   strings.TrimSpace(options.OllamaModel),
			client:  &http.Client{Timeout: timeout},
		}
	}
	return service
}

func (s *AIService) Status() AIStatus {
	if s != nil && s.ollama != nil {
		return AIStatus{
			Provider: "ollama", Model: s.ollama.model, AccountNeeded: false, Local: true,
			Description: "Lokalni Ollama model koristi projektni strateški kontekst i radi bez cloud računa.",
		}
	}
	return AIStatus{
		Provider: "millena-local", AccountNeeded: false, Local: true,
		Description: "Ugrađeni besplatni lokalni engine radi bez računa; Ollama se može uključiti za puni LLM.",
	}
}

func (s *AIService) Run(ctx context.Context, strategy Strategy, input AIInput) (AIResult, error) {
	input.Operation = strings.ToLower(strings.TrimSpace(input.Operation))
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Language = strings.ToLower(strings.TrimSpace(input.Language))
	if input.Language == "" {
		input.Language = strings.ToLower(strings.TrimSpace(strategy.DefaultLocale))
		if input.Language == "" {
			input.Language = "hr"
		}
	}
	if input.Language != "hr" && input.Language != "en" {
		return AIResult{}, errors.New("unsupported content language")
	}
	if input.Operation != "generate" && input.Operation != "refine" {
		return AIResult{}, errors.New("unsupported AI operation")
	}
	if _, ok := supportedKinds[input.Kind]; !ok {
		return AIResult{}, errors.New("unsupported content kind")
	}
	if input.Operation == "generate" && len(strings.TrimSpace(input.Brief)) < 3 {
		return AIResult{}, errors.New("brief is required for generation")
	}
	if input.Operation == "refine" && len(strings.TrimSpace(input.Body)) < 3 {
		return AIResult{}, errors.New("body is required for refinement")
	}

	if s != nil && s.ollama != nil {
		result, err := s.ollama.generate(ctx, strategy, input)
		if err == nil {
			return finishAIResult(result, strategy, input, "ollama:"+s.ollama.model, ""), nil
		}
		fallback := localAI(strategy, input)
		return finishAIResult(fallback, strategy, input, "millena-local", "Lokalni LLM nije bio dostupan pa je korišten ugrađeni engine."), nil
	}
	return finishAIResult(localAI(strategy, input), strategy, input, "millena-local", ""), nil
}

func finishAIResult(result AIResult, strategy Strategy, input AIInput, provider, warning string) AIResult {
	result.Title = strings.TrimSpace(result.Title)
	result.Body = strings.TrimSpace(result.Body)
	result.Provider = provider
	result.Operation = input.Operation
	result.Language = input.Language
	result.StrategyRevision = strategy.Revision
	result.ContextUsed = strategyHasContext(strategy)
	result.ContextSummary = strategySummary(strategy)
	result.Warning = warning
	return result
}

func strategyHasContext(strategy Strategy) bool {
	context := strings.TrimSpace(strings.Join([]string{
		strategy.OrganizationName, strategy.SixMonthGoal, strategy.Audience, strategy.BrandMessage, strategy.SourceText,
		strings.Join(strategy.PrimaryGoals, " "), strings.Join(strategy.PriorityTopics, " "),
		personaContextSummary(strategy.Personas, 1200),
	}, " "))
	return context != "" && (strategy.Revision > 0 || len(strategy.Personas) > 0 || strategy.OrganizationName != "")
}

func strategySummary(strategy Strategy) string {
	parts := make([]string, 0, 4)
	if strategy.OrganizationName != "" {
		parts = append(parts, "organizacija: "+shorten(strategy.OrganizationName, 90))
	}
	if strategy.SourceFilename != nil && *strategy.SourceFilename != "" {
		parts = append(parts, "datoteka: "+*strategy.SourceFilename)
	}
	if strategy.Audience != "" {
		parts = append(parts, "publika: "+shorten(strategy.Audience, 90))
	}
	if persona := primaryPersona(strategy.Personas); persona != nil {
		parts = append(parts, "persona: "+shorten(personaAudience(*persona), 120))
	}
	if len(strategy.PriorityTopics) > 0 {
		parts = append(parts, "teme: "+strings.Join(strategy.PriorityTopics, ", "))
	}
	if len(parts) == 0 && strategy.SixMonthGoal != "" {
		parts = append(parts, "cilj: "+shorten(strategy.SixMonthGoal, 120))
	}
	if len(parts) == 0 {
		return "Nema spremljenog strateškog konteksta."
	}
	return strings.Join(parts, " · ")
}

func localAI(strategy Strategy, input AIInput) AIResult {
	if input.Operation == "refine" {
		return localRefine(strategy, input)
	}
	return localGenerate(strategy, input)
}

func localGenerate(strategy Strategy, input AIInput) AIResult {
	if input.Language == "en" {
		return localGenerateEnglish(strategy, input)
	}
	topic := cleanTitle(input.Brief)
	audience := generationAudience(strategy)
	organization := fallbackPhrase(strategy.OrganizationName, "Projektni tim")
	message := cleanSentence(fallbackPhrase(strategy.BrandMessage, "Jasna komunikacija pretvara složene teme u korisne odluke"))
	proof := fallbackPhrase(strategy.ProofPoints, "provjerene činjenice i konkretni primjeri")
	tone := fallbackPhrase(strategy.Tone, "stručno, jasno i pristupačno")
	title := titleForKind(input.Kind, topic)
	if input.Kind == "press_release" {
		title = organization + " predstavlja: " + lowerFirst(topic)
	}
	var body string
	switch input.Kind {
	case "social":
		body = fmt.Sprintf("%s\n\nZa %s najvažnije je razumjeti što ova tema mijenja u svakodnevnom radu. %s\n\nNaš pristup ostaje %s i oslanja se na %s.\n\nKoji dio ove teme danas stvara najviše pitanja u vašem timu?\n\n#komunikacije #strategija #povjerenje", title, audience, message, tone, proof)
	case "blog":
		body = fmt.Sprintf("%s\n\n%s nije samo aktualna tema, nego praktično pitanje za %s. Dobar odgovor počinje jasnim ciljem, provjerenim činjenicama i razumijevanjem publike.\n\n1. Što se stvarno mijenja\nIzdvojite odluke i ponašanja na koja tema utječe, bez generičkih tvrdnji.\n\n2. Što publika treba znati\nObjasnite posljedice jednostavnim jezikom i potkrijepite ih kroz %s.\n\n3. Kako prijeći iz ideje u praksu\nDefinirajte vlasnika, sljedeći korak i mjerilo uspjeha.\n\nZaključak\n%s", title, topic, audience, proof, message)
	case "newsletter":
		body = fmt.Sprintf("Predmet: %s\n\nPozdrav,\n\nOvoga tjedna izdvajamo temu: %s. Za %s pripremili smo tri stvari koje vrijedi ponijeti u idući tjedan:\n\n• što se promijenilo i zašto je važno\n• koji dokaz treba provjeriti prije odluke\n• koji je najmanji korak koji tim može napraviti odmah\n\n%s\n\nDo sljedećeg izdanja,\n%s", title, topic, audience, message, organization)
	case "press_release":
		body = fmt.Sprintf("Zagreb — %s danas predstavlja %s. Inicijativa je usmjerena na %s i odgovara na potrebu da se složene teme pretvore u jasne, provjerljive poruke.\n\nKljuč pristupa čine %s. %s\n\nDodatne informacije i izjave bit će dostupne medijima na zahtjev.", organization, strings.ToLower(topic), audience, proof, message)
	case "case_study":
		body = fmt.Sprintf("Izazov\nTema „%s” otvorila je konkretan komunikacijski izazov za %s.\n\nPristup\nDefinirali smo cilj, mapirali pitanja publike i pripremili sadržaj oslonjen na %s.\n\nRješenje\nSadržaj je prilagođen kanalima, ali je zadržao zajedničku poruku: %s\n\nRezultat\nTim je dobio jasan proces, materijale spremne za provjeru i mjerljivu osnovu za sljedeći ciklus. Brojke se dodaju tek nakon potvrde iz projekta.", topic, audience, proof, message)
	case "event":
		body = fmt.Sprintf("Pozivamo vas na događaj „%s”.\n\nSusret je namijenjen %s, a fokus je na konkretnim primjerima, pitanjima iz prakse i koracima koji se mogu primijeniti odmah.\n\nŠto donosimo:\n• jasan okvir za temu\n• provjerene primjere i materijale\n• vrijeme za pitanja i razmjenu iskustava\n\nDetalji termina i lokacije dodaju se nakon potvrde organizatora.", title, audience)
	default:
		body = fmt.Sprintf("Tema: %s\nPublika: %s\nGlavna poruka: %s\nDokazi koje treba uključiti: %s\nTon: %s\n\nSljedeći korak: potvrditi činjenice, vlasnika sadržaja i željeni kanal.", topic, audience, message, proof, tone)
	}
	return AIResult{Title: title, Body: body}
}

func localGenerateEnglish(strategy Strategy, input AIInput) AIResult {
	topic := cleanTitle(input.Brief)
	audience := generationAudienceWithFallback(strategy, "people affected by this topic")
	message := cleanSentence(fallbackPhrase(strategy.BrandMessage, "Clear communication turns complex topics into useful decisions"))
	proof := fallbackPhrase(strategy.ProofPoints, "verified facts and concrete examples")
	tone := fallbackPhrase(strategy.Tone, "professional, clear and approachable")
	organization := fallbackPhrase(strategy.OrganizationName, "Project team")
	title := englishTitleForKind(input.Kind, topic, organization)
	var body string
	switch input.Kind {
	case "social":
		body = fmt.Sprintf("%s\n\nFor %s, the key is understanding what this topic changes in day-to-day work. %s\n\nOur approach remains %s and is grounded in %s.\n\nWhich part of this topic creates the most questions for your team today?\n\n#communications #strategy #trust", title, audience, message, tone, proof)
	case "blog":
		body = fmt.Sprintf("%s\n\n%s is more than a timely topic; it is a practical question for %s. A useful answer starts with a clear objective, verified facts and an understanding of the audience.\n\n1. What is actually changing\nIdentify the decisions and behaviours affected by the topic without relying on generic claims.\n\n2. What the audience needs to know\nExplain the consequences in plain language and support them with %s.\n\n3. How to move from idea to practice\nDefine an owner, the next action and a measure of success.\n\nConclusion\n%s", title, topic, audience, proof, message)
	case "newsletter":
		body = fmt.Sprintf("Subject: %s\n\nHello,\n\nThis week we are focusing on %s. For %s, we prepared three points worth carrying into the week ahead:\n\n• what changed and why it matters\n• which evidence should be checked before a decision\n• the smallest action a team can take now\n\n%s\n\nUntil the next edition,\n%s", title, topic, audience, message, organization)
	case "press_release":
		body = fmt.Sprintf("Zagreb — %s today announces %s. The initiative is designed for %s and responds to the need to turn complex topics into clear, verifiable messages.\n\nThe approach is grounded in %s. %s\n\nFurther information and statements are available to the media on request.", organization, strings.ToLower(topic), audience, proof, message)
	case "case_study":
		body = fmt.Sprintf("Challenge\nThe topic “%s” created a concrete communications challenge for %s.\n\nApproach\nWe defined the objective, mapped audience questions and prepared content grounded in %s.\n\nSolution\nThe content was adapted for each channel while retaining one shared message: %s\n\nResult\nThe team gained a clear process, review-ready materials and a measurable baseline for the next cycle. Figures are added only after they are confirmed in the project.", topic, audience, proof, message)
	case "event":
		body = fmt.Sprintf("You are invited to “%s”.\n\nThe event is intended for %s and focuses on concrete examples, practical questions and actions that can be applied immediately.\n\nWhat to expect:\n• a clear framework for the topic\n• verified examples and materials\n• time for questions and peer exchange\n\nThe date and location are added after confirmation by the organiser.", title, audience)
	default:
		body = fmt.Sprintf("Topic: %s\nAudience: %s\nCore message: %s\nEvidence to include: %s\nTone: %s\n\nNext step: confirm the facts, content owner and intended channel.", topic, audience, message, proof, tone)
	}
	return AIResult{Title: title, Body: body}
}

func localRefine(strategy Strategy, input AIInput) AIResult {
	if input.Language == "en" {
		return localRefineEnglish(strategy, input)
	}
	title := cleanTitle(input.Title)
	body := normalizeParagraphs(input.Body)
	if title == "" {
		title = titleForKind(input.Kind, firstSentence(body))
		if input.Kind == "press_release" {
			title = fallbackPhrase(strategy.OrganizationName, "Projektni tim") + " predstavlja: " + lowerFirst(cleanTitle(firstSentence(body)))
		}
	}
	message := strings.TrimSpace(strategy.BrandMessage)
	forbidden := strings.TrimSpace(strategy.ForbiddenTopics)

	if input.Kind == "social" && !strings.Contains(body, "?") {
		if persona := primaryPersona(strategy.Personas); persona != nil {
			body += "\n\nKoji dio ove teme danas najviše utječe na " + personaAudience(*persona) + "?"
		} else {
			body += "\n\nKoje iskustvo iz svoje prakse biste dodali ovom razgovoru?"
		}
	}
	if input.Kind == "newsletter" && !strings.Contains(strings.ToLower(body), "predmet:") {
		body = "Predmet: " + title + "\n\n" + body
	}
	if input.Kind == "case_study" && !strings.Contains(strings.ToLower(body), "izazov") {
		body = "Izazov\n" + body + "\n\nPristup\nOpišite odluke i provedene korake.\n\nRezultat\nDodajte samo potvrđene rezultate."
	}
	if message != "" && !strings.Contains(strings.ToLower(body), strings.ToLower(shorten(message, 34))) {
		body += "\n\n" + cleanSentence(message)
	}
	if forbidden != "" {
		body += "\n\n[Urednička provjera: prije objave potvrditi da tekst ne uključuje " + strings.ToLower(cleanSentence(forbidden)) + "]"
	}
	return AIResult{Title: title, Body: body}
}

func localRefineEnglish(strategy Strategy, input AIInput) AIResult {
	title := cleanTitle(input.Title)
	body := normalizeParagraphs(input.Body)
	if title == "" {
		title = englishTitleForKind(input.Kind, firstSentence(body), fallbackPhrase(strategy.OrganizationName, "Project team"))
	}
	message := strings.TrimSpace(strategy.BrandMessage)
	forbidden := strings.TrimSpace(strategy.ForbiddenTopics)

	if input.Kind == "social" && !strings.Contains(body, "?") {
		if persona := primaryPersona(strategy.Personas); persona != nil {
			body += "\n\nWhich part of this topic has the greatest impact on " + personaAudience(*persona) + "?"
		} else {
			body += "\n\nWhich experience from your own work would you add to this discussion?"
		}
	}
	if input.Kind == "newsletter" && !strings.Contains(strings.ToLower(body), "subject:") {
		body = "Subject: " + title + "\n\n" + body
	}
	if input.Kind == "case_study" && !strings.Contains(strings.ToLower(body), "challenge") {
		body = "Challenge\n" + body + "\n\nApproach\nDescribe the decisions and actions taken.\n\nResult\nAdd confirmed results only."
	}
	if message != "" && !strings.Contains(strings.ToLower(body), strings.ToLower(shorten(message, 34))) {
		body += "\n\n" + cleanSentence(message)
	}
	if forbidden != "" {
		body += "\n\n[Editorial check: before publishing, confirm that the text does not include " + strings.ToLower(cleanSentence(forbidden)) + "]"
	}
	return AIResult{Title: title, Body: body}
}

func englishTitleForKind(kind, topic, organization string) string {
	topic = cleanTitle(topic)
	if topic == "" {
		topic = "New content"
	}
	if utf8.RuneCountInString(topic) > 86 {
		topic = shorten(topic, 86)
	}
	switch kind {
	case "press_release":
		return organization + " announces: " + lowerFirst(topic)
	case "case_study":
		return "Case study: " + lowerFirst(topic)
	case "event":
		return "A conversation about: " + lowerFirst(topic)
	case "newsletter":
		return "Weekly focus: " + lowerFirst(topic)
	default:
		return topic
	}
}

func titleForKind(kind, topic string) string {
	topic = cleanTitle(topic)
	if topic == "" {
		topic = "Novi sadržaj"
	}
	if utf8.RuneCountInString(topic) > 86 {
		topic = shorten(topic, 86)
	}
	switch kind {
	case "press_release":
		return "Priopćenje: " + lowerFirst(topic)
	case "case_study":
		return "Studija slučaja: " + lowerFirst(topic)
	case "event":
		return "Razgovor o temi: " + lowerFirst(topic)
	case "newsletter":
		return "Tjedni fokus: " + lowerFirst(topic)
	default:
		return topic
	}
}

func cleanTitle(value string) string {
	value = strings.Trim(strings.Join(strings.Fields(value), " "), " .,!?:;-—")
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func cleanSentence(value string) string {
	value = cleanTitle(value)
	if value == "" {
		return value
	}
	last, _ := utf8.DecodeLastRuneInString(value)
	if !strings.ContainsRune(".!?", last) {
		value += "."
	}
	return value
}

func normalizeParagraphs(value string) string {
	paragraphs := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(strings.Join(strings.Fields(paragraph), " "))
		if paragraph == "" {
			continue
		}
		result = append(result, cleanSentence(paragraph))
	}
	return strings.Join(result, "\n\n")
}

func firstSentence(value string) string {
	for _, separator := range []string{". ", "! ", "? ", "\n"} {
		if index := strings.Index(value, separator); index > 0 {
			return value[:index]
		}
	}
	return shorten(value, 80)
}

func lowerFirst(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func fallbackPhrase(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		value = fallbackValue
	}
	return strings.Trim(strings.Join(strings.Fields(value), " "), " .,!?:;-—")
}

func generationAudience(strategy Strategy) string {
	return generationAudienceWithFallback(strategy, "ljudima kojima je ova tema važna")
}

func generationAudienceWithFallback(strategy Strategy, fallback string) string {
	parts := make([]string, 0, 2)
	if audience := strings.TrimSpace(strategy.Audience); audience != "" {
		parts = append(parts, fallbackPhrase(audience, ""))
	}
	if persona := primaryPersona(strategy.Personas); persona != nil {
		context := personaAudience(*persona)
		if context != "" && !strings.Contains(strings.ToLower(strings.Join(parts, " ")), strings.ToLower(persona.Name)) {
			parts = append(parts, context)
		}
	}
	if len(parts) == 0 {
		return fallback
	}
	return shorten(strings.Join(parts, ", posebno "), 360)
}

func primaryPersona(personas []PersonaContext) *PersonaContext {
	for index := range personas {
		if personas[index].IsPrimary {
			return &personas[index]
		}
	}
	if len(personas) > 0 {
		return &personas[0]
	}
	return nil
}

func personaAudience(persona PersonaContext) string {
	parts := make([]string, 0, 3)
	for _, value := range []string{persona.Name, persona.Description, persona.Demographics} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, fallbackPhrase(value, ""))
		}
	}
	return shorten(strings.Join(parts, " · "), 280)
}

func personaContextSummary(personas []PersonaContext, limit int) string {
	parts := make([]string, 0, len(personas))
	for _, persona := range personas {
		label := personaAudience(persona)
		if label == "" {
			continue
		}
		if persona.IsPrimary {
			label = "primarna: " + label
		}
		parts = append(parts, label)
	}
	return shorten(strings.Join(parts, " | "), limit)
}

func shorten(value string, limit int) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

type ollamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	System string `json:"system"`
	Format string `json:"format"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

func (o *ollamaProvider) generate(ctx context.Context, strategy Strategy, input AIInput) (AIResult, error) {
	endpoint, err := url.JoinPath(o.baseURL, "api", "generate")
	if err != nil {
		return AIResult{}, err
	}
	systemPrompt := "Ti si hrvatski komunikacijski urednik. Piši na hrvatskom jeziku. Koristi isključivo dani projektni kontekst. Ne izmišljaj brojke, citate ni činjenice. Vrati JSON objekt s poljima title i body."
	if input.Language == "en" {
		systemPrompt = "You are an English-language communications editor. Write in English. Use only the supplied project context. Do not invent figures, quotes or facts. Return a JSON object with title and body fields."
	}
	requestBody := ollamaRequest{
		Model: o.model, Stream: false, Format: "json",
		System: systemPrompt,
		Prompt: buildOllamaPrompt(strategy, input),
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return AIResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return AIResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := o.client.Do(request)
	if err != nil {
		return AIResult{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return AIResult{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return AIResult{}, fmt.Errorf("ollama returned HTTP %d", response.StatusCode)
	}
	var generated ollamaResponse
	if err := json.Unmarshal(data, &generated); err != nil {
		return AIResult{}, err
	}
	if generated.Error != "" {
		return AIResult{}, errors.New(generated.Error)
	}
	var result AIResult
	if err := json.Unmarshal([]byte(generated.Response), &result); err != nil {
		return AIResult{Title: cleanTitle(input.Title), Body: generated.Response}, nil
	}
	return result, nil
}

func buildOllamaPrompt(strategy Strategy, input AIInput) string {
	sourceText := shorten(strategy.SourceText, 12000)
	return fmt.Sprintf(`Operacija: %s
Vrsta sadržaja: %s
Jezik izlaza: %s
Kratki zadatak: %s
Postojeći naslov: %s
Postojeći tekst: %s

PROJEKTNA STRATEGIJA
Organizacija: %s
Cilj: %s
Primarni ciljevi: %s
Prioritetne teme: %s
Publika: %s
Spremljene personae: %s
Problem publike: %s
Glavna poruka: %s
Dokazi: %s
Zabranjene teme: %s
Mjerenje uspjeha: %s
Ton: %s
Tekst učitane strategije: %s

Za generate napiši gotov naslov i tekst prilagođen vrsti. Za refine sačuvaj činjenice i smisao autora, ali poboljšaj strukturu, jasnoću i ton.`,
		input.Operation, input.Kind, input.Language, input.Brief, input.Title, input.Body,
		strategy.OrganizationName, strategy.SixMonthGoal, strings.Join(strategy.PrimaryGoals, ", "),
		strings.Join(strategy.PriorityTopics, ", "), strategy.Audience,
		personaContextSummary(strategy.Personas, 4000),
		strategy.AudienceProblem, strategy.BrandMessage, strategy.ProofPoints,
		strategy.ForbiddenTopics, strategy.SuccessMetrics, strategy.Tone, sourceText)
}
