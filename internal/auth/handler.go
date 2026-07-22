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

func (h *Handler) Register(c *gin.Context) {
	var input RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Name, organization, email and password are required.")
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.OrganizationName = strings.TrimSpace(input.OrganizationName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if len(input.DisplayName) < 2 || len(input.DisplayName) > 120 || len(input.OrganizationName) < 2 || len(input.OrganizationName) > 120 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Name and organization must contain between 2 and 120 characters.")
		return
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || strings.ToLower(address.Address) != input.Email {
		writeError(c, http.StatusUnprocessableEntity, "invalid_email", "A valid email address is required.")
		return
	}
	if len(input.Password) < 10 || len(input.Password) > 128 {
		writeError(c, http.StatusUnprocessableEntity, "weak_password", "Password must contain between 10 and 128 characters.")
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
