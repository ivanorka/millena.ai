package actions

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ivanorka/millena-ai/internal/auth"
)

var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,79}$`)

type Handler struct {
	repository *Repository
}

func NewHandler(repository *Repository) *Handler {
	return &Handler{repository: repository}
}

func (h *Handler) Record(c *gin.Context) {
	var input RecordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "A valid action is required.")
		return
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.Label = strings.TrimSpace(input.Label)
	input.Screen = strings.TrimSpace(input.Screen)
	if !actionPattern.MatchString(input.Action) || len(input.Label) > 200 || len(input.Screen) > 80 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Action fields are invalid.")
		return
	}
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	event, err := h.repository.Record(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Action could not be recorded.")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": event})
}

func (h *Handler) List(c *gin.Context) {
	if h.repository == nil {
		writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return
	}
	events, err := h.repository.List(c.Request.Context(), c.Param("projectID"))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Audit events could not be loaded.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": events})
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
