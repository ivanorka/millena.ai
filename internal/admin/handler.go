package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/auth"
)

var (
	planCodePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	teamRoles       = map[string]struct{}{"owner": {}, "lead": {}, "editor": {}, "contributor": {}, "viewer": {}}
	memberStatuses  = map[string]struct{}{"active": {}, "suspended": {}}
	billingPeriods  = map[string]struct{}{"month": {}, "year": {}, "custom": {}}
)

type Store interface {
	ListTeam(context.Context, string) ([]TeamMember, error)
	CreateMember(context.Context, string, string, CreateMemberInput) (TeamMember, error)
	UpdateMember(context.Context, string, string, string, UpdateMemberInput) (TeamMember, error)
	DeleteMember(context.Context, string, string, string) error
	ListPlans(context.Context, string) ([]Plan, error)
	CreateCustomPlan(context.Context, string, string, CreatePlanInput) (Plan, error)
	GetEntitlement(context.Context, string) (Entitlement, error)
	UpdateEntitlement(context.Context, string, string, string) (Entitlement, error)
}

type Handler struct {
	repository Store
}

func NewHandler(repository Store) *Handler {
	return &Handler{repository: repository}
}

func (h *Handler) ListTeam(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	members, err := h.repository.ListTeam(c.Request.Context(), c.Param("projectID"))
	if h.writeRepositoryError(c, err, "Project team could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": members})
}

func (h *Handler) CreateMember(c *gin.Context) {
	var input CreateMemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Display name, email, role and temporary password are required.")
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	if !validDisplayName(input.DisplayName) || !validEmail(input.Email) {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Display name or email is invalid.")
		return
	}
	if _, ok := teamRoles[input.Role]; !ok {
		writeError(c, http.StatusUnprocessableEntity, "unsupported_role", "Role must be owner, lead, editor, contributor or viewer.")
		return
	}
	if len(input.TempPassword) < 10 || len(input.TempPassword) > 128 {
		writeError(c, http.StatusUnprocessableEntity, "weak_password", "Temporary password must contain between 10 and 128 characters.")
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	member, err := h.repository.CreateMember(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if h.writeRepositoryError(c, err, "Project member could not be created.") {
		return
	}
	c.Header("Location", "/api/v1/projects/"+c.Param("projectID")+"/team/"+member.UserID)
	c.JSON(http.StatusCreated, gin.H{"data": member})
}

func (h *Handler) UpdateMember(c *gin.Context) {
	var input UpdateMemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Role and status must use valid JSON values.")
		return
	}
	if input.Role == nil && input.Status == nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "At least one of role or status is required.")
		return
	}
	if input.Role != nil {
		value := strings.ToLower(strings.TrimSpace(*input.Role))
		if _, ok := teamRoles[value]; !ok {
			writeError(c, http.StatusUnprocessableEntity, "unsupported_role", "Role must be owner, lead, editor, contributor or viewer.")
			return
		}
		input.Role = &value
	}
	if input.Status != nil {
		value := strings.ToLower(strings.TrimSpace(*input.Status))
		if _, ok := memberStatuses[value]; !ok {
			writeError(c, http.StatusUnprocessableEntity, "unsupported_status", "Member status must be active or suspended.")
			return
		}
		input.Status = &value
	}
	if !h.databaseAvailable(c) {
		return
	}
	member, err := h.repository.UpdateMember(c.Request.Context(), c.Param("projectID"), auth.UserID(c), memberID(c), input)
	if h.writeRepositoryError(c, err, "Project member could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": member})
}

func (h *Handler) DeleteMember(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	err := h.repository.DeleteMember(c.Request.Context(), c.Param("projectID"), auth.UserID(c), memberID(c))
	if h.writeRepositoryError(c, err, "Project member could not be removed.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListPlans(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	plans, err := h.repository.ListPlans(c.Request.Context(), c.Param("projectID"))
	if h.writeRepositoryError(c, err, "Plans could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": plans})
}

func (h *Handler) CreatePlan(c *gin.Context) {
	var input CreatePlanInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Custom plan fields must use valid JSON values.")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.BillingInterval = strings.ToLower(strings.TrimSpace(input.BillingInterval))
	if input.Currency == "" {
		input.Currency = "EUR"
	}
	if input.BillingInterval == "" {
		input.BillingInterval = "month"
	}
	if utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 80 || utf8.RuneCountInString(input.Description) > 1000 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Plan name or description length is invalid.")
		return
	}
	if input.PriceCents < 0 || !currencyPattern.MatchString(input.Currency) {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Plan price or currency is invalid.")
		return
	}
	if _, ok := billingPeriods[input.BillingInterval]; !ok {
		writeError(c, http.StatusUnprocessableEntity, "unsupported_billing_interval", "Billing interval must be month, year or custom.")
		return
	}
	if !validPositiveLimits(input.SeatLimit, input.MonthlyPublicationLimit, input.StorageLimitBytes) {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Plan limits must be positive when provided.")
		return
	}
	if input.Features == nil {
		input.Features = map[string]any{}
	}
	if err := validatePlanFeatures(input.Features); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_plan_features", err.Error())
		return
	}
	encodedFeatures, err := json.Marshal(input.Features)
	if err != nil || len(encodedFeatures) > 64<<10 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Plan features must be a JSON object smaller than 64 KB.")
		return
	}
	input.Code, err = makeCustomPlanCode(input.Code, input.Name)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_plan_code", err.Error())
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	plan, err := h.repository.CreateCustomPlan(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if h.writeRepositoryError(c, err, "Custom plan could not be created.") {
		return
	}
	c.Header("Location", "/api/v1/projects/"+c.Param("projectID")+"/plans/"+plan.Code)
	c.JSON(http.StatusCreated, gin.H{"data": plan})
}

func (h *Handler) GetEntitlement(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	entitlement, err := h.repository.GetEntitlement(c.Request.Context(), c.Param("projectID"))
	if h.writeRepositoryError(c, err, "Project entitlement could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": entitlement})
}

func (h *Handler) UpdateEntitlement(c *gin.Context) {
	var input UpdateEntitlementInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "A plan code is required.")
		return
	}
	input.PlanCode = strings.ToLower(strings.TrimSpace(input.PlanCode))
	if len(input.PlanCode) < 2 || len(input.PlanCode) > 80 || !planCodePattern.MatchString(input.PlanCode) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_plan_code", "Plan code may contain lowercase letters, numbers and single hyphens.")
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	entitlement, err := h.repository.UpdateEntitlement(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input.PlanCode)
	if h.writeRepositoryError(c, err, "Project entitlement could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": entitlement})
}

func validDisplayName(value string) bool {
	length := utf8.RuneCountInString(value)
	return length >= 2 && length <= 120
}

func validEmail(value string) bool {
	if len(value) < 3 || len(value) > 254 {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func validPositiveLimits(seatLimit, publicationLimit *int, storageLimit *int64) bool {
	return (seatLimit == nil || *seatLimit > 0) &&
		(publicationLimit == nil || *publicationLimit > 0) &&
		(storageLimit == nil || *storageLimit > 0)
}

// validatePlanFeatures keeps feature flags safe for every downstream SQL and
// UI consumer. Boolean capabilities must really be booleans; socialChannels is
// either the literal "all" or an integer between zero and the eight supported
// providers. Unknown keys are rejected instead of becoming inert plan data.
func validatePlanFeatures(features map[string]any) error {
	booleanFeatures := map[string]struct{}{
		"aiAgents": {}, "analytics": {}, "api": {}, "auditLog": {},
		"automations": {}, "prioritySupport": {}, "whiteLabel": {},
	}
	for key, value := range features {
		if key == "socialChannels" {
			switch typed := value.(type) {
			case string:
				if typed != "all" {
					return errors.New("socialChannels must be \"all\" or an integer from 0 to 8")
				}
			case float64:
				if typed < 0 || typed > 8 || typed != float64(int(typed)) {
					return errors.New("socialChannels must be \"all\" or an integer from 0 to 8")
				}
				features[key] = int(typed)
			case int:
				if typed < 0 || typed > 8 {
					return errors.New("socialChannels must be \"all\" or an integer from 0 to 8")
				}
			default:
				return errors.New("socialChannels must be \"all\" or an integer from 0 to 8")
			}
			continue
		}
		if _, ok := booleanFeatures[key]; !ok {
			return errors.New("unsupported plan feature: " + key)
		}
		if _, ok := value.(bool); !ok {
			return errors.New(key + " must be a boolean")
		}
	}
	return nil
}

func makeCustomPlanCode(requested, name string) (string, error) {
	base := strings.ToLower(strings.TrimSpace(requested))
	base = strings.TrimPrefix(base, "custom-")
	if base == "" {
		base = planSlug(name)
	}
	if len(base) < 2 || len(base) > 48 || !planCodePattern.MatchString(base) {
		return "", errors.New("custom plan code may contain 2-48 lowercase letters, numbers and single hyphens")
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", errors.New("custom plan code could not be generated")
	}
	return "custom-" + base + "-" + hex.EncodeToString(random), nil
}

func planSlug(value string) string {
	var builder strings.Builder
	previousHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch r {
		case 'č', 'ć':
			r = 'c'
		case 'š':
			r = 's'
		case 'ž':
			r = 'z'
		case 'đ':
			r = 'd'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			previousHyphen = false
			continue
		}
		if builder.Len() > 0 && !previousHyphen {
			builder.WriteByte('-')
			previousHyphen = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func memberID(c *gin.Context) string {
	if value := c.Param("memberID"); value != "" {
		return value
	}
	return c.Param("userID")
}

func (h *Handler) databaseAvailable(c *gin.Context) bool {
	if h.repository != nil {
		return true
	}
	writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
	return false
}

func (h *Handler) writeRepositoryError(c *gin.Context, err error, message string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", "The requested admin resource was not found.")
	case errors.Is(err, ErrMemberExists):
		writeError(c, http.StatusConflict, "member_exists", "This user is already a project member.")
	case errors.Is(err, ErrUserUnavailable):
		writeError(c, http.StatusConflict, "user_unavailable", "The existing user account is not active.")
	case errors.Is(err, ErrSelfRemoval):
		writeError(c, http.StatusConflict, "self_removal_denied", "You cannot remove or suspend your own project membership.")
	case errors.Is(err, ErrLastOwner):
		writeError(c, http.StatusConflict, "last_owner", "Assign another active owner before changing this member.")
	case errors.Is(err, ErrPlanNotAvailable):
		writeError(c, http.StatusUnprocessableEntity, "plan_not_available", "The selected plan is not available to this project.")
	case errors.Is(err, ErrSeatLimitReached):
		writeError(c, http.StatusConflict, "seat_limit_reached", "The active plan has no available team seats.")
	case errors.Is(err, ErrEntitlementInactive):
		writeError(c, http.StatusForbidden, "entitlement_inactive", "An active or trial plan is required to add or reactivate team members.")
	default:
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) {
			switch pgError.Code {
			case "22P02":
				writeError(c, http.StatusBadRequest, "invalid_id", "Project and member IDs must be UUID values.")
			case "23503":
				writeError(c, http.StatusNotFound, "project_not_found", "Project was not found.")
			case "23505":
				writeError(c, http.StatusConflict, "conflict", "A member or plan with the same identifier already exists.")
			default:
				writeError(c, http.StatusInternalServerError, "internal_error", message)
			}
		} else {
			writeError(c, http.StatusInternalServerError, "internal_error", message)
		}
	}
	return true
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
