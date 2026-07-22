package workspace

import "time"

var supportedLanguages = map[string]struct{}{"hr": {}, "en": {}}

var supportedCadences = map[string]struct{}{
	"off": {}, "weekly": {}, "biweekly": {}, "monthly": {},
}

var supportedAutomationKinds = map[string]struct{}{
	"master": {}, "channel": {}, "bot_event": {}, "calendar_gap": {}, "newsletter": {}, "custom": {},
}

var supportedReviewPolicies = map[string]struct{}{
	"always": {}, "conditional": {}, "automatic": {},
}

var supportedChannelProviders = map[string]struct{}{
	"whatsapp": {}, "telegram": {}, "website": {}, "newsletter": {}, "webhook": {}, "custom_api": {},
}

var supportedConnectionModes = map[string]struct{}{
	"sandbox": {}, "api": {}, "webhook": {},
}

var supportedRequestTypes = map[string]struct{}{
	"website_proposal": {}, "integration_help": {}, "support": {},
}

type Profile struct {
	ProjectID          string     `json:"projectId"`
	ProjectName        string     `json:"projectName"`
	CompanyName        string     `json:"companyName"`
	CompanyDescription string     `json:"companyDescription"`
	WebsiteURL         string     `json:"websiteUrl"`
	Industry           string     `json:"industry"`
	PrimaryLanguage    string     `json:"primaryLanguage"`
	Timezone           string     `json:"timezone"`
	SocialPostsPerWeek int        `json:"socialPostsPerWeek"`
	NewsletterCadence  string     `json:"newsletterCadence"`
	SignupHeadline     string     `json:"signupHeadline"`
	SignupCopy         string     `json:"signupCopy"`
	SetupCompleted     bool       `json:"setupCompleted"`
	SetupCompletedAt   *time.Time `json:"setupCompletedAt"`
	UpdatedBy          *string    `json:"updatedBy"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type ProfileInput struct {
	ProjectName        string `json:"projectName"`
	CompanyName        string `json:"companyName"`
	CompanyDescription string `json:"companyDescription"`
	WebsiteURL         string `json:"websiteUrl"`
	Industry           string `json:"industry"`
	PrimaryLanguage    string `json:"primaryLanguage"`
	Timezone           string `json:"timezone"`
	SocialPostsPerWeek int    `json:"socialPostsPerWeek"`
	NewsletterCadence  string `json:"newsletterCadence"`
	SignupHeadline     string `json:"signupHeadline"`
	SignupCopy         string `json:"signupCopy"`
	SetupCompleted     bool   `json:"setupCompleted"`
}

type Dashboard struct {
	ProjectID          string                  `json:"projectId"`
	AnalyticsAvailable bool                    `json:"analyticsAvailable"`
	Stats              DashboardStats          `json:"stats"`
	Pipeline           DashboardPipeline       `json:"pipeline"`
	Automation         DashboardAutomation     `json:"automation"`
	Today              []DashboardCalendarItem `json:"today"`
	Channels           []DashboardChannel      `json:"channels"`
}

type DashboardStats struct {
	PublishedThisMonth  int64 `json:"publishedThisMonth"`
	ScheduledNext14Days int64 `json:"scheduledNext14Days"`
	WaitingReview       int64 `json:"waitingReview"`
	NewsletterAudience  int64 `json:"newsletterAudience"`
}

type DashboardPipeline struct {
	Collected  int64 `json:"collected"`
	InProgress int64 `json:"inProgress"`
	InReview   int64 `json:"inReview"`
	Scheduled  int64 `json:"scheduled"`
}

type DashboardAutomation struct {
	EnabledRules int64      `json:"enabledRules"`
	TotalRules   int64      `json:"totalRules"`
	RunCount     int64      `json:"runCount"`
	LastRunAt    *time.Time `json:"lastRunAt"`
}

type DashboardCalendarItem struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Channel      string    `json:"channel"`
	Status       string    `json:"status"`
	ScheduledFor time.Time `json:"scheduledFor"`
}

type DashboardChannel struct {
	ID            string     `json:"id"`
	Provider      string     `json:"provider"`
	DisplayName   string     `json:"displayName"`
	AccountHandle string     `json:"accountHandle"`
	Status        string     `json:"status"`
	Source        string     `json:"source"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
}

type AutomationRule struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"projectId"`
	RuleKey       string         `json:"ruleKey"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Kind          string         `json:"kind"`
	Channel       string         `json:"channel"`
	Enabled       bool           `json:"enabled"`
	ReviewPolicy  string         `json:"reviewPolicy"`
	ScheduleRule  string         `json:"scheduleRule"`
	Configuration map[string]any `json:"configuration"`
	RunCount      int            `json:"runCount"`
	LastRunAt     *time.Time     `json:"lastRunAt"`
	NextRunAt     *time.Time     `json:"nextRunAt"`
	CreatedBy     *string        `json:"createdBy"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type AutomationRuleInput struct {
	RuleKey       string         `json:"ruleKey"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Kind          string         `json:"kind"`
	Channel       string         `json:"channel"`
	Enabled       bool           `json:"enabled"`
	ReviewPolicy  string         `json:"reviewPolicy"`
	ScheduleRule  string         `json:"scheduleRule"`
	Configuration map[string]any `json:"configuration"`
	NextRunAt     *time.Time     `json:"nextRunAt"`
}

type AutomationRunResult struct {
	Rule        AutomationRule `json:"rule"`
	EffectType  string         `json:"effectType"`
	EffectID    string         `json:"effectId"`
	EffectTitle string         `json:"effectTitle"`
	RunAt       time.Time      `json:"runAt"`
}

type ChannelConnection struct {
	ID                   string         `json:"id"`
	ProjectID            string         `json:"projectId"`
	Provider             string         `json:"provider"`
	Mode                 string         `json:"mode"`
	DisplayName          string         `json:"displayName"`
	AccountHandle        string         `json:"accountHandle"`
	EndpointURL          string         `json:"endpointUrl"`
	Status               string         `json:"status"`
	CredentialConfigured bool           `json:"credentialConfigured"`
	Metadata             map[string]any `json:"metadata"`
	LastCheckedAt        *time.Time     `json:"lastCheckedAt"`
	CreatedBy            *string        `json:"createdBy"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

type ChannelConnectionInput struct {
	Provider              string         `json:"provider"`
	Mode                  string         `json:"mode"`
	DisplayName           string         `json:"displayName"`
	AccountHandle         string         `json:"accountHandle"`
	EndpointURL           string         `json:"endpointUrl"`
	Credential            string         `json:"credential"`
	Metadata              map[string]any `json:"metadata"`
	credentialFingerprint string
}

type ServiceRequest struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"projectId"`
	RequestType string         `json:"requestType"`
	Status      string         `json:"status"`
	Summary     string         `json:"summary"`
	Metadata    map[string]any `json:"metadata"`
	CreatedBy   *string        `json:"createdBy"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type ServiceRequestInput struct {
	RequestType string         `json:"requestType"`
	Summary     string         `json:"summary"`
	Metadata    map[string]any `json:"metadata"`
}

type ServiceRequestUpdateInput struct {
	Status   string          `json:"status"`
	Summary  *string         `json:"summary"`
	Metadata *map[string]any `json:"metadata"`
}

type ProjectPersona struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"projectId"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Demographics string         `json:"demographics"`
	IsPrimary    bool           `json:"isPrimary"`
	Metadata     map[string]any `json:"metadata"`
	CreatedBy    *string        `json:"createdBy"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type ProjectPersonaInput struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Demographics string         `json:"demographics"`
	IsPrimary    bool           `json:"isPrimary"`
	Metadata     map[string]any `json:"metadata"`
}

type NewsletterDelivery struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"projectId"`
	ContentItemID     string     `json:"contentItemId"`
	ContentVariantID  *string    `json:"contentVariantId"`
	ListID            *string    `json:"listId"`
	Mode              string     `json:"mode"`
	Status            string     `json:"status"`
	Subject           string     `json:"subject"`
	TestRecipient     *string    `json:"testRecipient"`
	RecipientCount    int        `json:"recipientCount"`
	ScheduledFor      *time.Time `json:"scheduledFor"`
	SentAt            *time.Time `json:"sentAt"`
	ExternalReference *string    `json:"externalReference"`
	LastError         *string    `json:"lastError"`
	CreatedBy         *string    `json:"createdBy"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type NewsletterDeliveryInput struct {
	ContentItemID string     `json:"contentItemId"`
	ListID        *string    `json:"listId"`
	Mode          string     `json:"mode"`
	Subject       string     `json:"subject"`
	TestRecipient *string    `json:"testRecipient"`
	ScheduledFor  *time.Time `json:"scheduledFor"`
}
