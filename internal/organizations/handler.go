package organizations

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/ivanorka/millena-ai/internal/auth"
)

type Handler struct{ repository *Repository }

func NewHandler(repository *Repository) *Handler { return &Handler{repository: repository} }

func (h *Handler) Detail(c *gin.Context) {
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	detail, err := h.repository.Detail(c.Request.Context(), c.Param("projectID"), auth.UserID(c))
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail})
}

func (h *Handler) CreateMember(c *gin.Context) {
	var input CreateMemberInput
	if c.ShouldBindJSON(&input) != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Organization member fields are invalid.")
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	input.ProjectRole = strings.ToLower(strings.TrimSpace(input.ProjectRole))
	if utf8.RuneCountInString(input.DisplayName) < 2 || utf8.RuneCountInString(input.DisplayName) > 120 || !validEmail(input.Email) {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Display name or email is invalid.")
		return
	}
	if input.Role != "admin" && input.Role != "member" {
		writeError(c, http.StatusUnprocessableEntity, "unsupported_role", "A new organization member must be an administrator or member.")
		return
	}
	if input.GrantProjectAccess && !validProjectRole(input.ProjectRole) {
		writeError(c, http.StatusUnprocessableEntity, "unsupported_project_role", "Choose a valid project role.")
		return
	}
	if len(input.TempPassword) < 10 || len(input.TempPassword) > 128 {
		writeError(c, http.StatusUnprocessableEntity, "weak_password", "Temporary password must contain between 10 and 128 characters.")
		return
	}
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	member, err := h.repository.CreateMember(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": member})
}

func (h *Handler) UpdateMember(c *gin.Context) {
	var input UpdateMemberInput
	if c.ShouldBindJSON(&input) != nil || (input.Role == nil && input.Status == nil) {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "At least one organization member change is required.")
		return
	}
	if input.Role != nil {
		role := strings.ToLower(strings.TrimSpace(*input.Role))
		if role != "owner" && role != "admin" && role != "member" {
			writeError(c, http.StatusUnprocessableEntity, "unsupported_role", "Organization role is invalid.")
			return
		}
		input.Role = &role
	}
	if input.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*input.Status))
		if status != "active" && status != "suspended" {
			writeError(c, http.StatusUnprocessableEntity, "unsupported_status", "Organization member status is invalid.")
			return
		}
		input.Status = &status
	}
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	member, err := h.repository.UpdateMember(c.Request.Context(), c.Param("projectID"), auth.UserID(c), c.Param("memberID"), input)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": member})
}

func (h *Handler) DeleteMember(c *gin.Context) {
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	err := h.repository.DeleteMember(c.Request.Context(), c.Param("projectID"), auth.UserID(c), c.Param("memberID"))
	if h.writeError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrForbidden):
		writeError(c, http.StatusForbidden, "organization_admin_required", "Only organization administrators can manage company accounts.")
	case errors.Is(err, ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", "Organization member was not found.")
	case errors.Is(err, ErrMemberExists):
		writeError(c, http.StatusConflict, "member_exists", "This account already belongs to the organization.")
	case errors.Is(err, ErrUserUnavailable):
		writeError(c, http.StatusConflict, "user_unavailable", "This account is not active.")
	case errors.Is(err, ErrSelfRemoval):
		writeError(c, http.StatusConflict, "self_removal", "You cannot suspend or remove your own organization account.")
	case errors.Is(err, ErrLastOwner):
		writeError(c, http.StatusConflict, "last_owner", "The organization must retain at least one active owner.")
	default:
		writeError(c, http.StatusInternalServerError, "internal_error", "Organization accounts could not be updated.")
	}
	return true
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func validProjectRole(value string) bool {
	return value == "owner" || value == "lead" || value == "editor" || value == "contributor" || value == "viewer"
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
