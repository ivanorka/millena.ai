package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/auth"
	"github.com/ivanorka/millena-ai/internal/automationschedule"
	"github.com/ivanorka/millena-ai/internal/limits"
)

const maxWorkspaceRequestSize = 256 << 10

var automationRuleKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)

type Handler struct {
	repository *Repository
}

func NewHandler(repository *Repository) *Handler {
	return &Handler{repository: repository}
}

func (h *Handler) GetProfile(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	profile, err := h.repository.GetProfile(c.Request.Context(), c.Param("projectID"))
	if writeRepositoryError(c, err, "Project profile could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": profile})
}

func (h *Handler) SaveProfile(c *gin.Context) {
	var input ProfileInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeProfileInput(&input); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	profile, err := h.repository.SaveProfile(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeRepositoryError(c, err, "Project profile could not be saved.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": profile})
}

func (h *Handler) GetDashboard(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	dashboard, err := h.repository.GetDashboard(c.Request.Context(), c.Param("projectID"))
	if writeRepositoryError(c, err, "Dashboard could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": dashboard})
}

func (h *Handler) ListAutomationRules(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	rules, err := h.repository.ListAutomationRules(c.Request.Context(), c.Param("projectID"))
	if writeRepositoryError(c, err, "Automation rules could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rules})
}

func (h *Handler) CreateAutomationRule(c *gin.Context) {
	var input AutomationRuleInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeAutomationRuleInput(&input); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	rule, err := h.repository.CreateAutomationRule(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeRepositoryError(c, err, "Automation rule could not be created.") {
		return
	}
	c.Header("Location", "/api/v1/projects/"+c.Param("projectID")+"/automations/"+rule.ID)
	c.JSON(http.StatusCreated, gin.H{"data": rule})
}

func (h *Handler) UpdateAutomationRule(c *gin.Context) {
	var input AutomationRuleInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeAutomationRuleInput(&input); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	rule, err := h.repository.UpdateAutomationRule(
		c.Request.Context(), c.Param("projectID"), c.Param("ruleID"), auth.UserID(c), input,
	)
	if writeRepositoryError(c, err, "Automation rule could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rule})
}

func (h *Handler) DeleteAutomationRule(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	err := h.repository.DeleteAutomationRule(c.Request.Context(), c.Param("projectID"), c.Param("ruleID"), auth.UserID(c))
	if writeRepositoryError(c, err, "Automation rule could not be deleted.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) RunAutomationRule(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	result, err := h.repository.RunAutomationRule(c.Request.Context(), c.Param("projectID"), c.Param("ruleID"), auth.UserID(c))
	if writeRepositoryError(c, err, "Automation rule could not be run.") {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *Handler) ListChannelConnections(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	connections, err := h.repository.ListChannelConnections(c.Request.Context(), c.Param("projectID"))
	if writeRepositoryError(c, err, "Channel connections could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": connections})
}

func (h *Handler) CreateChannelConnection(c *gin.Context) {
	var input ChannelConnectionInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeChannelConnectionInput(&input, false); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	connection, err := h.repository.CreateChannelConnection(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeRepositoryError(c, err, "Channel connection could not be created.") {
		return
	}
	c.Header("Location", "/api/v1/projects/"+c.Param("projectID")+"/channel-connections/"+connection.ID)
	c.JSON(http.StatusCreated, gin.H{"data": connection})
}

func (h *Handler) UpdateChannelConnection(c *gin.Context) {
	var input ChannelConnectionInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeChannelConnectionInput(&input, true); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	connection, err := h.repository.UpdateChannelConnection(
		c.Request.Context(), c.Param("projectID"), c.Param("connectionID"), auth.UserID(c), input,
	)
	if writeRepositoryError(c, err, "Channel connection could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": connection})
}

func (h *Handler) DeleteChannelConnection(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	err := h.repository.DeleteChannelConnection(
		c.Request.Context(), c.Param("projectID"), c.Param("connectionID"), auth.UserID(c),
	)
	if writeRepositoryError(c, err, "Channel connection could not be deleted.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) TestChannelConnection(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	connection, err := h.repository.TestChannelConnection(
		c.Request.Context(), c.Param("projectID"), c.Param("connectionID"), auth.UserID(c),
	)
	if writeRepositoryError(c, err, "Channel connection could not be tested.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": connection})
}

func (h *Handler) CreateServiceRequest(c *gin.Context) {
	var input ServiceRequestInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeServiceRequestInput(&input); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	request, err := h.repository.CreateServiceRequest(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeRepositoryError(c, err, "Service request could not be created.") {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": request})
}

func (h *Handler) ListServiceRequests(c *gin.Context) {
	requestType := strings.ToLower(strings.TrimSpace(c.Query("requestType")))
	if requestType != "" {
		if _, ok := supportedRequestTypes[requestType]; !ok {
			writeWorkspaceError(c, http.StatusBadRequest, "unsupported_request_type", "Service request type is not supported.")
			return
		}
	}
	if !h.databaseAvailable(c) {
		return
	}
	requests, err := h.repository.ListServiceRequests(c.Request.Context(), c.Param("projectID"), requestType)
	if writeRepositoryError(c, err, "Service requests could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": requests})
}

func (h *Handler) ListProjectPersonas(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	items, err := h.repository.ListProjectPersonas(c.Request.Context(), c.Param("projectID"))
	if writeRepositoryError(c, err, "Project personas could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) CreateProjectPersona(c *gin.Context) {
	var input ProjectPersonaInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeProjectPersonaInput(&input); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	item, err := h.repository.CreateProjectPersona(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeRepositoryError(c, err, "Project persona could not be created.") {
		return
	}
	c.Header("Location", "/api/v1/projects/"+c.Param("projectID")+"/personas/"+item.ID)
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

func (h *Handler) UpdateProjectPersona(c *gin.Context) {
	var input ProjectPersonaInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeProjectPersonaInput(&input); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	item, err := h.repository.UpdateProjectPersona(c.Request.Context(), c.Param("projectID"), c.Param("personaID"), auth.UserID(c), input)
	if writeRepositoryError(c, err, "Project persona could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) DeleteProjectPersona(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	err := h.repository.DeleteProjectPersona(c.Request.Context(), c.Param("projectID"), c.Param("personaID"), auth.UserID(c))
	if writeRepositoryError(c, err, "Project persona could not be deleted.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListNewsletterDeliveries(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	deliveries, err := h.repository.ListNewsletterDeliveries(c.Request.Context(), c.Param("projectID"))
	if writeRepositoryError(c, err, "Newsletter deliveries could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": deliveries})
}

func (h *Handler) CreateNewsletterDelivery(c *gin.Context) {
	var input NewsletterDeliveryInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeNewsletterDeliveryInput(&input); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	delivery, err := h.repository.CreateNewsletterDelivery(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeRepositoryError(c, err, "Newsletter delivery could not be created.") {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": delivery})
}

func normalizeProfileInput(input *ProfileInput) string {
	input.ProjectName = strings.TrimSpace(input.ProjectName)
	input.CompanyName = strings.TrimSpace(input.CompanyName)
	input.CompanyDescription = strings.TrimSpace(input.CompanyDescription)
	input.WebsiteURL = strings.TrimSpace(input.WebsiteURL)
	input.Industry = strings.TrimSpace(input.Industry)
	input.PrimaryLanguage = strings.ToLower(strings.TrimSpace(input.PrimaryLanguage))
	input.Timezone = strings.TrimSpace(input.Timezone)
	input.NewsletterCadence = strings.ToLower(strings.TrimSpace(input.NewsletterCadence))
	input.SignupHeadline = strings.TrimSpace(input.SignupHeadline)
	input.SignupCopy = strings.TrimSpace(input.SignupCopy)
	if input.Timezone == "" {
		input.Timezone = "Europe/Zagreb"
	}
	if input.NewsletterCadence == "" {
		input.NewsletterCadence = "weekly"
	}
	if runeLength(input.ProjectName) < 2 || runeLength(input.ProjectName) > 120 || runeLength(input.CompanyName) > 120 {
		return "Project and company names contain invalid values."
	}
	if runeLength(input.CompanyDescription) > 1000 || runeLength(input.Industry) > 120 || runeLength(input.SignupHeadline) > 180 || runeLength(input.SignupCopy) > 1000 {
		return "Company description, industry or signup copy is too long."
	}
	if _, ok := supportedLanguages[input.PrimaryLanguage]; !ok {
		return "Primary language must be hr or en."
	}
	if _, ok := supportedCadences[input.NewsletterCadence]; !ok {
		return "Newsletter cadence is not supported."
	}
	if input.SocialPostsPerWeek < 0 || input.SocialPostsPerWeek > 100 {
		return "Social posts per week must be between 0 and 100."
	}
	if strings.EqualFold(input.Timezone, "Local") || runeLength(input.Timezone) > 100 {
		return "A valid IANA timezone is required."
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return "A valid IANA timezone is required."
	}
	if input.WebsiteURL != "" && !validHTTPURL(input.WebsiteURL) {
		return "Website URL must be a valid HTTP or HTTPS URL."
	}
	return ""
}

func normalizeAutomationRuleInput(input *AutomationRuleInput) string {
	input.RuleKey = strings.ToLower(strings.TrimSpace(input.RuleKey))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Channel = strings.ToLower(strings.TrimSpace(input.Channel))
	input.ReviewPolicy = strings.ToLower(strings.TrimSpace(input.ReviewPolicy))
	input.ScheduleRule = strings.TrimSpace(input.ScheduleRule)
	if input.RuleKey == "" {
		input.RuleKey = ruleKeyFromName(input.Name)
	}
	if input.ReviewPolicy == "" {
		input.ReviewPolicy = "always"
	}
	if input.Configuration == nil {
		input.Configuration = map[string]any{}
	}
	if !automationRuleKeyPattern.MatchString(input.RuleKey) || runeLength(input.RuleKey) > 80 {
		return "Rule key must use lowercase letters, numbers and underscores."
	}
	if runeLength(input.Name) < 2 || runeLength(input.Name) > 120 || runeLength(input.Description) > 2000 {
		return "Automation name or description length is invalid."
	}
	if _, ok := supportedAutomationKinds[input.Kind]; !ok {
		return "Automation kind is not supported."
	}
	if _, ok := supportedReviewPolicies[input.ReviewPolicy]; !ok {
		return "Review policy is not supported."
	}
	if runeLength(input.Channel) > 50 || runeLength(input.ScheduleRule) > 500 {
		return "Channel or schedule rule is too long."
	}
	if !validJSONMapSize(input.Configuration, 64<<10) {
		return "Automation configuration must be a JSON object smaller than 64 KB."
	}
	cadencePresent, configurationMessage := normalizeAutomationSchedulingConfiguration(input.Configuration)
	if configurationMessage != "" {
		return configurationMessage
	}
	var scheduleSpec automationschedule.Spec
	if input.ScheduleRule != "" {
		var err error
		scheduleSpec, err = automationschedule.Parse(input.ScheduleRule)
		if err != nil {
			return "Schedule supports gap:Nd, DAILY, WEEKLY with one BYDAY, or MONTHLY with BYMONTHDAY 1-28; INTERVAL, COUNT, UNTIL and other RRULE fields are not supported."
		}
		if !scheduleSpec.IsGap() {
			if hour, present := automationConfigurationIntPresent(input.Configuration, "hour", 0, 23); present && hour != scheduleSpec.Hour {
				return "Configuration hour must match the explicit RRULE BYHOUR (default 9)."
			}
			if minute, present := automationConfigurationIntPresent(input.Configuration, "minute", 0, 59); present && minute != scheduleSpec.Minute {
				return "Configuration minute must match the explicit RRULE BYMINUTE (default 0)."
			}
		}
	}
	_, hourPresent := input.Configuration["hour"]
	_, minutePresent := input.Configuration["minute"]
	usesProfileCadence := input.Kind == "newsletter" || input.Channel == "newsletter"
	usesDailyGap := input.Kind == "calendar_gap"
	if input.ScheduleRule == "" && !cadencePresent && !usesProfileCadence && !usesDailyGap && (hourPresent || minutePresent) {
		return "Configuration hour and minute require an explicit schedule or cadence."
	}
	// The repository calculates the actual first run with the project's
	// persisted timezone after this syntax-only validation.
	input.NextRunAt = nil
	return ""
}

func nextAutomationRun(rule string, now time.Time, configuredLocation ...*time.Location) (time.Time, error) {
	location, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		return time.Time{}, err
	}
	if len(configuredLocation) > 0 && configuredLocation[0] != nil {
		location = configuredLocation[0]
	}
	spec, err := automationschedule.Parse(rule)
	if err != nil {
		return time.Time{}, err
	}
	return spec.Next(now, location), nil
}

func normalizeAutomationSchedulingConfiguration(configuration map[string]any) (bool, string) {
	cadencePresent := false
	if raw, exists := configuration["cadence"]; exists {
		cadence, ok := raw.(string)
		if !ok {
			return false, "Automation cadence must be off, weekly, biweekly or monthly."
		}
		cadence = strings.ToLower(strings.TrimSpace(cadence))
		if _, ok := supportedCadences[cadence]; !ok {
			return false, "Automation cadence must be off, weekly, biweekly or monthly."
		}
		configuration["cadence"] = cadence
		cadencePresent = true
	}
	for _, field := range []struct {
		key     string
		minimum int
		maximum int
		label   string
	}{
		{key: "hour", minimum: 0, maximum: 23, label: "hour"},
		{key: "minute", minimum: 0, maximum: 59, label: "minute"},
		{key: "gapDays", minimum: 1, maximum: 365, label: "gapDays"},
	} {
		if _, exists := configuration[field.key]; !exists {
			continue
		}
		value, valid := automationConfigurationIntPresent(configuration, field.key, field.minimum, field.maximum)
		if !valid {
			return cadencePresent, "Automation " + field.label + " must be a whole number in its valid range."
		}
		configuration[field.key] = value
	}
	return cadencePresent, ""
}

func normalizeChannelConnectionInput(input *ChannelConnectionInput, update bool) string {
	credential := input.Credential
	input.Credential = ""
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.AccountHandle = strings.TrimSpace(input.AccountHandle)
	input.EndpointURL = strings.TrimSpace(input.EndpointURL)
	if input.Mode == "" {
		input.Mode = "sandbox"
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if credential != "" {
		if len(credential) < 8 || len(credential) > 8192 {
			return "Credentials must contain between 8 and 8192 characters."
		}
		sum := sha256.Sum256([]byte(credential))
		input.credentialFingerprint = hex.EncodeToString(sum[:])
	}
	if _, ok := supportedChannelProviders[input.Provider]; !ok {
		return "Channel provider is not supported."
	}
	if _, ok := supportedConnectionModes[input.Mode]; !ok {
		return "Connection mode is not supported."
	}
	if runeLength(input.DisplayName) < 2 || runeLength(input.DisplayName) > 120 || runeLength(input.AccountHandle) > 120 {
		return "Connection name or account handle length is invalid."
	}
	if input.EndpointURL != "" && !validHTTPURL(input.EndpointURL) {
		return "Endpoint must be a valid HTTP or HTTPS URL without embedded credentials."
	}
	if (input.Provider == "webhook" || input.Provider == "custom_api") && input.EndpointURL == "" {
		return "Webhook and custom API connections require an endpoint URL."
	}
	if input.Mode != "sandbox" && input.credentialFingerprint == "" && !update {
		return "API and webhook connections require a credential."
	}
	if !validJSONMapSize(input.Metadata, 32<<10) {
		return "Connection metadata must be smaller than 32 KB."
	}
	return ""
}

func normalizeServiceRequestInput(input *ServiceRequestInput) string {
	input.RequestType = strings.ToLower(strings.TrimSpace(input.RequestType))
	input.Summary = strings.TrimSpace(input.Summary)
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if _, ok := supportedRequestTypes[input.RequestType]; !ok {
		return "Service request type is not supported."
	}
	if runeLength(input.Summary) > 4000 || !validJSONMapSize(input.Metadata, 32<<10) {
		return "Service request summary or metadata is too large."
	}
	return ""
}

func normalizeProjectPersonaInput(input *ProjectPersonaInput) string {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Demographics = strings.TrimSpace(input.Demographics)
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if runeLength(input.Name) < 2 || runeLength(input.Name) > 120 || runeLength(input.Description) > 1000 || runeLength(input.Demographics) > 500 {
		return "Persona name, description or demographics length is invalid."
	}
	if !validJSONMapSize(input.Metadata, 32<<10) {
		return "Persona metadata must be smaller than 32 KB."
	}
	return ""
}

func normalizeNewsletterDeliveryInput(input *NewsletterDeliveryInput) string {
	input.ContentItemID = strings.TrimSpace(input.ContentItemID)
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	input.Subject = strings.TrimSpace(input.Subject)
	if input.Mode == "" {
		input.Mode = "sandbox"
	}
	if input.ListID != nil {
		value := strings.TrimSpace(*input.ListID)
		if value == "" {
			input.ListID = nil
		} else {
			input.ListID = &value
		}
	}
	if input.TestRecipient != nil {
		value := strings.ToLower(strings.TrimSpace(*input.TestRecipient))
		if value == "" {
			input.TestRecipient = nil
		} else {
			input.TestRecipient = &value
		}
	}
	if input.ContentItemID == "" || runeLength(input.Subject) < 2 || runeLength(input.Subject) > 180 {
		return "Newsletter content item and a subject between 2 and 180 characters are required."
	}
	if input.Mode != "sandbox" {
		return "Only sandbox newsletter delivery is currently available."
	}
	if input.TestRecipient != nil {
		if !validEmail(*input.TestRecipient) {
			return "A valid test recipient email is required."
		}
		input.ScheduledFor = nil
		return ""
	}
	if input.ScheduledFor == nil || input.ScheduledFor.Before(time.Now().Add(-time.Minute)) {
		return "Scheduled newsletter delivery requires a future date and time."
	}
	return ""
}

func decodeWorkspaceJSON(c *gin.Context, destination any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkspaceRequestSize)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeWorkspaceError(c, http.StatusRequestEntityTooLarge, "request_too_large", "Request body must be smaller than 256 KB.")
			return false
		}
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", "A valid JSON request body is required.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", "Only one JSON object is allowed.")
		return false
	}
	return true
}

func validHTTPURL(value string) bool {
	if len(value) > 2048 {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func validJSONMapSize(value map[string]any, limit int) bool {
	data, err := json.Marshal(value)
	return err == nil && len(data) <= limit
}

func ruleKeyFromName(value string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			if character <= unicode.MaxASCII {
				builder.WriteRune(character)
				lastUnderscore = false
			}
		} else if builder.Len() > 0 && !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

func runeLength(value string) int {
	return utf8.RuneCountInString(value)
}

func (h *Handler) databaseAvailable(c *gin.Context) bool {
	if h.repository != nil {
		return true
	}
	writeWorkspaceError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
	return false
}

func writeRepositoryError(c *gin.Context, err error, message string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrNotFound):
		writeWorkspaceError(c, http.StatusNotFound, "not_found", "Workspace resource was not found.")
		return true
	case errors.Is(err, ErrConflict):
		writeWorkspaceError(c, http.StatusConflict, "conflict", "A workspace resource with the same key already exists.")
		return true
	case errors.Is(err, ErrRuleDisabled):
		writeWorkspaceError(c, http.StatusConflict, "automation_disabled", "Enable the automation rule before running it.")
		return true
	case errors.Is(err, ErrInvalidReference):
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "invalid_reference", "The selected project resource does not exist or has the wrong type.")
		return true
	case errors.Is(err, ErrCredentialRequired):
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "credential_required", "This connection requires a credential before it can be tested.")
		return true
	case errors.Is(err, ErrFeatureUnavailable):
		writeWorkspaceError(c, http.StatusForbidden, "feature_not_available", "API and webhook connections are not included in the active plan.")
		return true
	case errors.Is(err, limits.ErrEntitlementInactive):
		writeWorkspaceError(c, http.StatusForbidden, "entitlement_inactive", "The project needs an active or trial plan for this operation.")
		return true
	case errors.Is(err, limits.ErrPublicationLimitReached):
		writeWorkspaceError(c, http.StatusConflict, "publication_limit_reached", "The active plan's monthly publication limit has been reached.")
		return true
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "22P02":
			writeWorkspaceError(c, http.StatusBadRequest, "invalid_id", "Resource IDs must be UUID values.")
		case "23503":
			writeWorkspaceError(c, http.StatusUnprocessableEntity, "invalid_reference", "A referenced project resource was not found.")
		case "23505":
			writeWorkspaceError(c, http.StatusConflict, "conflict", "A workspace resource with the same key already exists.")
		default:
			writeWorkspaceError(c, http.StatusInternalServerError, "internal_error", message)
		}
		return true
	}
	writeWorkspaceError(c, http.StatusInternalServerError, "internal_error", message)
	return true
}

func writeWorkspaceError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
