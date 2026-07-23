package content

import "time"

var supportedKinds = map[string]struct{}{
	"source": {}, "social": {}, "blog": {}, "newsletter": {},
	"press_release": {}, "case_study": {}, "event": {},
}

var supportedStatuses = map[string]struct{}{
	"draft": {}, "in_review": {}, "approved": {}, "scheduled": {}, "published": {}, "failed": {},
}

var supportedSources = map[string]struct{}{
	"manual": {}, "ai": {}, "bot": {}, "import": {},
}

type Item struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"projectId"`
	AuthorID     *string        `json:"authorId"`
	Kind         string         `json:"kind"`
	Status       string         `json:"status"`
	Title        string         `json:"title"`
	Summary      string         `json:"summary"`
	Body         string         `json:"body"`
	Channels     []string       `json:"channels"`
	ScheduledFor *time.Time     `json:"scheduledFor"`
	Source       string         `json:"source"`
	Revision     int            `json:"revision"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type SaveInput struct {
	Kind         string         `json:"kind"`
	Status       string         `json:"status"`
	Title        string         `json:"title"`
	Summary      string         `json:"summary"`
	Body         string         `json:"body"`
	Channels     []string       `json:"channels"`
	ScheduledFor *time.Time     `json:"scheduledFor"`
	Source       string         `json:"source"`
	Metadata     map[string]any `json:"metadata"`
}

type Variant struct {
	ID            string         `json:"id"`
	ContentItemID string         `json:"contentItemId"`
	Channel       string         `json:"channel"`
	Locale        string         `json:"locale"`
	Title         string         `json:"title"`
	Summary       string         `json:"summary"`
	Body          string         `json:"body"`
	Status        string         `json:"status"`
	ScheduledFor  *time.Time     `json:"scheduledFor"`
	Revision      int            `json:"revision"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type VariantInput struct {
	Channel      string         `json:"channel"`
	Locale       string         `json:"locale"`
	Title        string         `json:"title"`
	Summary      string         `json:"summary"`
	Body         string         `json:"body"`
	Status       string         `json:"status"`
	ScheduledFor *time.Time     `json:"scheduledFor"`
	Metadata     map[string]any `json:"metadata"`
}

type ListFilter struct {
	Kind   string
	Status string
	Search string
}

type Strategy struct {
	ProjectID        string           `json:"projectId"`
	OrganizationName string           `json:"organizationName"`
	DefaultLocale    string           `json:"defaultLocale"`
	Mode             string           `json:"mode"`
	SixMonthGoal     string           `json:"sixMonthGoal"`
	PrimaryGoals     []string         `json:"primaryGoals"`
	PriorityTopics   []string         `json:"priorityTopics"`
	Audience         string           `json:"audience"`
	AudienceProblem  string           `json:"audienceProblem"`
	BrandMessage     string           `json:"brandMessage"`
	ProofPoints      string           `json:"proofPoints"`
	ForbiddenTopics  string           `json:"forbiddenTopics"`
	SuccessMetrics   string           `json:"successMetrics"`
	Tone             string           `json:"tone"`
	SourceFilename   *string          `json:"sourceFilename"`
	SourceMIMEType   *string          `json:"sourceMimeType"`
	SourceAssetID    *string          `json:"sourceAssetId"`
	SourceText       string           `json:"sourceText,omitempty"`
	Revision         int              `json:"revision"`
	UpdatedBy        *string          `json:"updatedBy"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	Personas         []PersonaContext `json:"personas"`
}

// PersonaContext is the persisted audience context exposed to both the
// built-in generator and optional local LLM provider. It intentionally omits
// tenant/user metadata that is irrelevant to content generation.
type PersonaContext struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Demographics string `json:"demographics"`
	IsPrimary    bool   `json:"isPrimary"`
}

type StrategyInput struct {
	Mode            string   `json:"mode"`
	SixMonthGoal    string   `json:"sixMonthGoal"`
	PrimaryGoals    []string `json:"primaryGoals"`
	PriorityTopics  []string `json:"priorityTopics"`
	Audience        string   `json:"audience"`
	AudienceProblem string   `json:"audienceProblem"`
	BrandMessage    string   `json:"brandMessage"`
	ProofPoints     string   `json:"proofPoints"`
	ForbiddenTopics string   `json:"forbiddenTopics"`
	SuccessMetrics  string   `json:"successMetrics"`
	Tone            string   `json:"tone"`
	// SourceText is optional so ordinary strategy-field saves never overwrite
	// text extracted from an uploaded document.
	SourceText *string `json:"sourceText,omitempty"`
}

type AIInput struct {
	Operation string `json:"operation"`
	Kind      string `json:"kind"`
	Brief     string `json:"brief"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Language  string `json:"language"`
}

type AIResult struct {
	Title            string `json:"title"`
	Body             string `json:"body"`
	Provider         string `json:"provider"`
	Operation        string `json:"operation"`
	Language         string `json:"language"`
	StrategyRevision int    `json:"strategyRevision"`
	ContextUsed      bool   `json:"contextUsed"`
	ContextSummary   string `json:"contextSummary"`
	Warning          string `json:"warning,omitempty"`
}

type AIStatus struct {
	Provider      string `json:"provider"`
	Model         string `json:"model,omitempty"`
	AccountNeeded bool   `json:"accountNeeded"`
	Local         bool   `json:"local"`
	Description   string `json:"description"`
}
