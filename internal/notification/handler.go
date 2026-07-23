package notification

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ivanorka/millena-ai/internal/auth"
)

type Handler struct{ repository *Repository }

func NewHandler(repository *Repository) *Handler { return &Handler{repository: repository} }

func (h *Handler) GetPreferences(c *gin.Context) {
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	preferences, err := h.repository.GetPreferences(c.Request.Context(), auth.UserID(c))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Notification preferences could not be loaded.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": preferences})
}

func (h *Handler) SavePreferences(c *gin.Context) {
	var input UpdatePreferencesInput
	if err := c.ShouldBindJSON(&input); err != nil || input.Events == nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Notification preferences are invalid.")
		return
	}
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	preferences, err := h.repository.SavePreferences(c.Request.Context(), auth.UserID(c), input)
	if err != nil {
		if errors.Is(err, ErrUnknownPreferenceEvent) {
			writeError(c, http.StatusUnprocessableEntity, "notification_preference_invalid", "Notification preferences contain an unsupported event.")
			return
		}
		writeError(c, http.StatusInternalServerError, "internal_error", "Notification preferences could not be saved.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": preferences})
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
