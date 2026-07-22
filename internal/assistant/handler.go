package assistant

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/assets"
	"github.com/ivanorka/millena-ai/internal/auth"
)

type Handler struct {
	repository *Repository
	service    *Service
}

func NewHandler(repository *Repository, service *Service) *Handler {
	if service == nil {
		service = NewService(repository, nil)
	}
	return &Handler{repository: repository, service: service}
}

func (h *Handler) Status(c *gin.Context) {
	if !h.available(c) {
		return
	}
	automationsAvailable, err := h.repository.AutomationFeatureEnabled(c.Request.Context(), c.Param("projectID"))
	if assistantDatabaseError(c, err, "Assistant capabilities could not be loaded.") {
		return
	}
	status := h.service.ai.Status()
	capabilities := []string{"workspace_summary", "calendar_read", "content_create", "attachment_context"}
	if automationsAvailable {
		capabilities = append(capabilities, "automation_toggle")
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"provider": status.Provider, "model": status.Model, "local": status.Local,
		"accountNeeded": status.AccountNeeded, "description": status.Description,
		"capabilities": capabilities, "automationsAvailable": automationsAvailable,
	}})
}

func (h *Handler) Threads(c *gin.Context) {
	if !h.available(c) {
		return
	}
	items, err := h.repository.Threads(c.Request.Context(), c.Param("projectID"))
	if assistantDatabaseError(c, err, "Assistant threads could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) CreateThread(c *gin.Context) {
	var input CreateThreadInput
	if err := c.ShouldBindJSON(&input); err != nil {
		assistantError(c, http.StatusUnprocessableEntity, "validation_error", "Thread title and channel must use valid values.")
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Channel = strings.ToLower(strings.TrimSpace(input.Channel))
	if input.Title == "" {
		input.Title = "Razgovor s Millenom"
	}
	if input.Channel == "" {
		input.Channel = "app"
	}
	if utf8.RuneCountInString(input.Title) > 120 || (input.Channel != "app" && input.Channel != "telegram" && input.Channel != "whatsapp") {
		assistantError(c, http.StatusUnprocessableEntity, "validation_error", "Thread title or channel is invalid.")
		return
	}
	if !h.available(c) {
		return
	}
	item, err := h.repository.CreateThread(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if assistantDatabaseError(c, err, "Assistant thread could not be created.") {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

func (h *Handler) Messages(c *gin.Context) {
	if !h.available(c) {
		return
	}
	items, err := h.repository.Messages(c.Request.Context(), c.Param("projectID"), c.Param("threadID"))
	if errors.Is(err, ErrNotFound) {
		assistantError(c, http.StatusNotFound, "not_found", "Assistant thread was not found.")
		return
	}
	if assistantDatabaseError(c, err, "Assistant messages could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) Send(c *gin.Context) {
	var input SendInput
	if err := c.ShouldBindJSON(&input); err != nil {
		assistantError(c, http.StatusUnprocessableEntity, "validation_error", "A message is required.")
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	normalizedAttachmentIDs, err := assets.NormalizeIDs(input.AttachmentIDs, 5)
	if err != nil {
		assistantError(c, http.StatusUnprocessableEntity, "invalid_attachments", "Use up to five valid assistant attachment IDs.")
		return
	}
	input.AttachmentIDs = normalizedAttachmentIDs
	if utf8.RuneCountInString(input.Body) < 2 || utf8.RuneCountInString(input.Body) > 5000 {
		assistantError(c, http.StatusUnprocessableEntity, "validation_error", "Message must contain between 2 and 5,000 characters.")
		return
	}
	if !h.available(c) {
		return
	}
	result, err := h.service.Send(c.Request.Context(), c.Param("projectID"), c.Param("threadID"), auth.UserID(c), input.Body, input.AttachmentIDs)
	if errors.Is(err, ErrAutomationUnavailable) {
		assistantError(c, http.StatusForbidden, "feature_not_available", "Automations are not included in the current project plan.")
		return
	}
	if errors.Is(err, ErrNotFound) {
		assistantError(c, http.StatusNotFound, "not_found", "Assistant thread or automation rule was not found.")
		return
	}
	if assistantDatabaseError(c, err, "Assistant could not process the message.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *Handler) available(c *gin.Context) bool {
	if h != nil && h.repository != nil && h.service != nil {
		return true
	}
	assistantError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
	return false
}

func assistantDatabaseError(c *gin.Context, err error, message string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, assets.ErrInvalidReferences) {
		assistantError(c, http.StatusUnprocessableEntity, "invalid_attachments", "Every attachment must belong to this project and use assistant_attachment purpose.")
		return true
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "22P02" {
		assistantError(c, http.StatusBadRequest, "invalid_id", "ID must be a UUID.")
		return true
	}
	assistantError(c, http.StatusInternalServerError, "internal_error", message)
	return true
}

func assistantError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
