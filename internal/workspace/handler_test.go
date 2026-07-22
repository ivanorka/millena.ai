package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ivanorka/millena-ai/internal/limits"
)

func TestSaveProfileRejectsInvalidTimezoneBeforeDatabase(t *testing.T) {
	context, recorder := workspaceTestContext(http.MethodPut, `{
		"projectName":"MPR Grupa",
		"companyName":"MPR",
		"primaryLanguage":"hr",
		"timezone":"Mars/Olympus",
		"socialPostsPerWeek":4,
		"newsletterCadence":"weekly"
	}`)

	NewHandler(nil).SaveProfile(context)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d", http.StatusUnprocessableEntity, recorder.Code)
	}
}

func TestSaveProfileRejectsLongCompanyDescriptionBeforeDatabase(t *testing.T) {
	input := ProfileInput{
		ProjectName: "MPR Grupa", CompanyName: "MPR",
		CompanyDescription: string(make([]rune, 1001)),
		PrimaryLanguage:    "hr", Timezone: "Europe/Zagreb",
		NewsletterCadence: "weekly",
	}
	if message := normalizeProfileInput(&input); message == "" {
		t.Fatal("expected company description length validation")
	}
}

func TestSaveProfileRejectsProcessLocalTimezone(t *testing.T) {
	input := ProfileInput{
		ProjectName: "MPR Grupa", CompanyName: "MPR", PrimaryLanguage: "hr",
		Timezone: "Local", NewsletterCadence: "weekly",
	}
	if message := normalizeProfileInput(&input); message == "" {
		t.Fatal("expected the host-dependent Local timezone to be rejected")
	}
}

func TestCreateAutomationRuleRejectsUnsupportedKindBeforeDatabase(t *testing.T) {
	context, recorder := workspaceTestContext(http.MethodPost, `{
		"ruleKey":"bad_rule",
		"name":"Bad rule",
		"kind":"delete_everything",
		"enabled":true,
		"reviewPolicy":"always"
	}`)

	NewHandler(nil).CreateAutomationRule(context)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d", http.StatusUnprocessableEntity, recorder.Code)
	}
}

func TestConnectionCredentialBecomesFingerprintAndPlaintextIsCleared(t *testing.T) {
	credential := "local-test-token-123"
	input := ChannelConnectionInput{
		Provider:    "custom_api",
		Mode:        "api",
		DisplayName: "Local test API",
		EndpointURL: "https://example.test/webhook",
		Credential:  credential,
	}

	if message := normalizeChannelConnectionInput(&input, false); message != "" {
		t.Fatalf("unexpected validation error: %s", message)
	}
	if input.Credential != "" {
		t.Fatal("plaintext credential was retained")
	}
	expected := sha256.Sum256([]byte(credential))
	if input.credentialFingerprint != hex.EncodeToString(expected[:]) {
		t.Fatal("credential fingerprint does not match SHA-256")
	}
}

func TestInactiveEntitlementMapsToForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	if !writeRepositoryError(context, limits.ErrEntitlementInactive, "fallback") {
		t.Fatal("expected error to be handled")
	}
	if response.Code != http.StatusForbidden || !bytes.Contains(response.Body.Bytes(), []byte("entitlement_inactive")) {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestUnavailableConnectionFeatureMapsToForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	if !writeRepositoryError(context, ErrFeatureUnavailable, "fallback") {
		t.Fatal("expected error to be handled")
	}
	if response.Code != http.StatusForbidden || !bytes.Contains(response.Body.Bytes(), []byte("feature_not_available")) {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestNewsletterDeliveryRequiresFutureScheduleOrTestRecipient(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	input := NewsletterDeliveryInput{
		ContentItemID: "9df11135-b6eb-46d6-8fc5-e35cb4704456",
		Subject:       "Tjedni pregled",
		ScheduledFor:  &past,
	}

	if message := normalizeNewsletterDeliveryInput(&input); message == "" {
		t.Fatal("expected a validation error for a past delivery")
	}
}

func TestNewsletterTestDeliveryClearsSchedule(t *testing.T) {
	future := time.Now().Add(time.Hour)
	recipient := "test@example.com"
	input := NewsletterDeliveryInput{
		ContentItemID: "9df11135-b6eb-46d6-8fc5-e35cb4704456",
		Subject:       "Probna poruka",
		TestRecipient: &recipient,
		ScheduledFor:  &future,
	}

	if message := normalizeNewsletterDeliveryInput(&input); message != "" {
		t.Fatalf("unexpected validation error: %s", message)
	}
	if input.ScheduledFor != nil {
		t.Fatal("test delivery must not retain a schedule")
	}
}

func TestWeeklyAutomationScheduleUsesProjectTimezone(t *testing.T) {
	now := time.Date(2026, time.July, 22, 8, 0, 0, 0, time.UTC)
	next, err := nextAutomationRun("FREQ=WEEKLY;BYDAY=FR;BYHOUR=10", now)
	if err != nil {
		t.Fatalf("unexpected schedule error: %v", err)
	}
	location, _ := time.LoadLocation("Europe/Zagreb")
	local := next.In(location)
	if local.Weekday() != time.Friday || local.Hour() != 10 || !next.After(now) {
		t.Fatalf("unexpected next run: %s", next)
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load alternate timezone: %v", err)
	}
	alternate, err := nextAutomationRun("FREQ=DAILY;BYHOUR=7;BYMINUTE=30", now, newYork)
	if err != nil {
		t.Fatalf("unexpected alternate schedule error: %v", err)
	}
	alternateLocal := alternate.In(newYork)
	if alternateLocal.Hour() != 7 || alternateLocal.Minute() != 30 || !alternate.After(now) {
		t.Fatalf("unexpected alternate project-time run: %s", alternateLocal)
	}
}

func TestAutomationScheduleRejectsUnsupportedExpression(t *testing.T) {
	unsupported := []string{
		"whenever possible",
		"FREQ=DAILY;INTERVAL=2",
		"FREQ=DAILY;COUNT=3",
		"FREQ=DAILY;UNTIL=20261231T000000Z",
		"FREQ=WEEKLY;BYDAY=MO,FR",
		"FREQ=DAILY;FREQ=WEEKLY;BYDAY=FR",
	}
	for _, schedule := range unsupported {
		if _, err := nextAutomationRun(schedule, time.Now()); err == nil {
			t.Fatalf("expected unsupported schedule %q to be rejected", schedule)
		}
	}
}

func TestCreateAutomationRuleReturnsValidationErrorForUnsupportedRRULEFields(t *testing.T) {
	context, recorder := workspaceTestContext(http.MethodPost, `{
		"ruleKey":"unsupported_rrule",
		"name":"Unsupported RRULE",
		"kind":"custom",
		"enabled":true,
		"reviewPolicy":"always",
		"scheduleRule":"FREQ=DAILY;INTERVAL=2;COUNT=3",
		"configuration":{}
	}`)

	NewHandler(nil).CreateAutomationRule(context)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	}
}

func TestAutomationScheduleRejectsMisleadingConfigurationClock(t *testing.T) {
	input := AutomationRuleInput{
		RuleKey: "mismatched_clock", Name: "Mismatched clock", Kind: "custom",
		ReviewPolicy: "always", ScheduleRule: "FREQ=DAILY;BYHOUR=7;BYMINUTE=30",
		Configuration: map[string]any{"hour": float64(10), "minute": float64(30)},
	}
	if message := normalizeAutomationRuleInput(&input); message == "" {
		t.Fatal("expected mismatched configuration hour to be rejected")
	}
	input.Configuration = map[string]any{"hour": float64(7), "minute": float64(30)}
	if message := normalizeAutomationRuleInput(&input); message != "" {
		t.Fatalf("matching configuration clock was rejected: %s", message)
	}
}

func TestAutomationClockRequiresSchedulingSource(t *testing.T) {
	input := AutomationRuleInput{
		RuleKey: "unused_clock", Name: "Unused clock", Kind: "custom",
		ReviewPolicy: "always", Configuration: map[string]any{"hour": float64(10)},
	}
	if message := normalizeAutomationRuleInput(&input); message == "" {
		t.Fatal("expected an unused configuration clock to be rejected")
	}
	input.Configuration["cadence"] = "weekly"
	if message := normalizeAutomationRuleInput(&input); message != "" {
		t.Fatalf("configured cadence and clock were rejected: %s", message)
	}
}

func TestAutomationGapDaysMustBeWholeNumberInSupportedRange(t *testing.T) {
	for _, value := range []any{float64(0), float64(366), 1.5, "5"} {
		input := AutomationRuleInput{
			RuleKey: "invalid_gap", Name: "Invalid gap", Kind: "calendar_gap",
			ReviewPolicy: "always", Configuration: map[string]any{"gapDays": value},
		}
		if message := normalizeAutomationRuleInput(&input); message == "" {
			t.Fatalf("expected gapDays=%v (%T) to be rejected", value, value)
		}
	}
	valid := AutomationRuleInput{
		RuleKey: "valid_gap", Name: "Valid gap", Kind: "calendar_gap",
		ReviewPolicy: "always", Configuration: map[string]any{"gapDays": float64(5)},
	}
	if message := normalizeAutomationRuleInput(&valid); message != "" {
		t.Fatalf("valid gapDays was rejected: %s", message)
	}
	if value, ok := valid.Configuration["gapDays"].(int); !ok || value != 5 {
		t.Fatalf("gapDays was not normalized to an integer: %#v", valid.Configuration["gapDays"])
	}
}

func TestListServiceRequestsRejectsUnsupportedFilterBeforeDatabase(t *testing.T) {
	context, recorder := workspaceTestContext(http.MethodGet, "")
	context.Request.URL.RawQuery = "requestType=not_supported"

	NewHandler(nil).ListServiceRequests(context)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestCreateProjectPersonaRejectsShortNameBeforeDatabase(t *testing.T) {
	context, recorder := workspaceTestContext(http.MethodPost, `{"name":"x","metadata":{}}`)

	NewHandler(nil).CreateProjectPersona(context)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d", http.StatusUnprocessableEntity, recorder.Code)
	}
}

func workspaceTestContext(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(method, "/", bytes.NewBufferString(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context, recorder
}
