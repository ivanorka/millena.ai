package auth

import (
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const sessionCookieName = "millena_session"

var slugCharacters = regexp.MustCompile(`[^a-z0-9]+`)

type Handler struct {
	repository   *Repository
	sessionTTL   time.Duration
	cookieSecure bool
}

func NewHandler(repository *Repository, sessionTTL time.Duration, cookieSecure bool) *Handler {
	if sessionTTL <= 0 {
		sessionTTL = 30 * 24 * time.Hour
	}
	return &Handler{repository: repository, sessionTTL: sessionTTL, cookieSecure: cookieSecure}
}

func (h *Handler) RegistrationPlans(c *gin.Context) {
	if h.repository == nil {
		writeError(c, 503, "database_unavailable", "Database is not configured.")
		return
	}
	plans, err := h.repository.ListRegistrationPlans(c.Request.Context())
	if err != nil {
		writeError(c, 500, "internal_error", "Plans could not be loaded.")
		return
	}
	c.JSON(200, gin.H{"data": plans})
}

func (h *Handler) Register(c *gin.Context) {
	var input RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Name, organization, email and password are required.")
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.OrganizationName = strings.TrimSpace(input.OrganizationName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.PlanCode = strings.ToLower(strings.TrimSpace(input.PlanCode))
	if input.PlanCode == "" {
		input.PlanCode = "starter"
	}
	if len(input.DisplayName) < 2 || len(input.DisplayName) > 120 || len(input.OrganizationName) < 2 || len(input.OrganizationName) > 120 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Name and organization must contain between 2 and 120 characters.")
		return
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || strings.ToLower(address.Address) != input.Email {
		writeError(c, http.StatusUnprocessableEntity, "invalid_email", "A valid email address is required.")
		return
	}
	if len(input.Password) < 8 || len(input.Password) > 128 {
		writeError(c, http.StatusUnprocessableEntity, "weak_password", "Password must contain between 8 and 128 characters.")
		return
	}
	if !isRegistrationPlan(input.PlanCode) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_plan", "Choose Starter, Optimum or Enterprise.")
		return
	}
	input.ProjectSlug = projectSlug(input.OrganizationName)
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	user, access, err := h.repository.Register(c.Request.Context(), input)
	if errors.Is(err, ErrEmailConflict) {
		writeError(c, http.StatusConflict, "email_conflict", "An account with this email already exists.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Account could not be created.")
		return
	}
	token, expiresAt, err := h.repository.CreateSession(c.Request.Context(), user.ID, h.sessionTTL)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Session could not be created.")
		return
	}
	h.setSessionCookie(c, token, expiresAt)
	c.JSON(http.StatusCreated, gin.H{"data": SessionUser{User: user, Projects: []ProjectAccess{access}}})
}

func (h *Handler) Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Email and password are required.")
		return
	}
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	user, err := h.repository.Authenticate(c.Request.Context(), strings.TrimSpace(input.Email), input.Password)
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Sign-in could not be completed.")
		return
	}
	token, expiresAt, err := h.repository.CreateSession(c.Request.Context(), user.ID, h.sessionTTL)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Session could not be created.")
		return
	}
	projects, err := h.repository.ListProjectAccess(c.Request.Context(), user.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Project access could not be loaded.")
		return
	}
	h.setSessionCookie(c, token, expiresAt)
	c.JSON(http.StatusOK, gin.H{"data": SessionUser{User: user, Projects: projects}})
}

func (h *Handler) RequestPasswordReset(c *gin.Context) {
	var input PasswordResetRequestInput
	if c.ShouldBindJSON(&input) != nil || h.repository == nil {
		c.Status(http.StatusAccepted)
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if address, err := mail.ParseAddress(email); err == nil && strings.ToLower(address.Address) == email {
		_ = h.repository.RequestPasswordReset(c.Request.Context(), email)
	}
	// Deliberately identical for existing and unknown accounts.
	c.Status(http.StatusAccepted)
}

func (h *Handler) ConfirmPasswordReset(c *gin.Context) {
	var input PasswordResetConfirmInput
	if c.ShouldBindJSON(&input) != nil || len(input.Password) < 8 || len(input.Password) > 128 || strings.TrimSpace(input.Token) == "" {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Reset link or new password is invalid.")
		return
	}
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	err := h.repository.ResetPassword(c.Request.Context(), strings.TrimSpace(input.Token), input.Password)
	if errors.Is(err, ErrPasswordResetInvalid) {
		writeError(c, http.StatusBadRequest, "reset_link_invalid", "This reset link has expired or was already used.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Password could not be reset.")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Me(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "authentication_required", "Sign-in is required.")
		return
	}
	projects, err := h.repository.ListProjectAccess(c.Request.Context(), user.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Project access could not be loaded.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": SessionUser{User: user, Projects: projects}})
}

func (h *Handler) UpdateAccount(c *gin.Context) {
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	var input AccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Profile fields must use valid JSON values.")
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if len(input.DisplayName) < 2 || len(input.DisplayName) > 120 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Name must contain between 2 and 120 characters.")
		return
	}
	if input.NewPassword != "" && (len(input.NewPassword) < 8 || len(input.NewPassword) > 128) {
		writeError(c, http.StatusUnprocessableEntity, "weak_password", "New password must contain between 8 and 128 characters.")
		return
	}
	if input.NewPassword != "" && input.CurrentPassword == "" {
		writeError(c, http.StatusUnprocessableEntity, "current_password_required", "Current password is required to set a new password.")
		return
	}
	user, ok := CurrentUser(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "authentication_required", "Sign-in is required.")
		return
	}
	updated, err := h.repository.UpdateAccount(c.Request.Context(), user.ID, input.DisplayName, input.CurrentPassword, input.NewPassword)
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Account was not found.")
		return
	}
	if errors.Is(err, ErrCurrentPasswordInvalid) {
		writeError(c, http.StatusUnauthorized, "current_password_invalid", "Current password is incorrect.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Account could not be updated.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": updated})
}

func (h *Handler) Logout(c *gin.Context) {
	if token, err := c.Cookie(sessionCookieName); err == nil && h.repository != nil {
		_ = h.repository.DeleteSession(c.Request.Context(), token)
	}
	h.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *Handler) setSessionCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: h.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: expiresAt, MaxAge: int(time.Until(expiresAt).Seconds()),
	})
}

func (h *Handler) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: h.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func projectSlug(name string) string {
	base := slugCharacters.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	base = strings.Trim(base, "-")
	if len(base) > 60 {
		base = strings.Trim(base[:60], "-")
	}
	if base == "" {
		base = "projekt"
	}
	return base + "-" + time.Now().UTC().Format("060102150405")
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
