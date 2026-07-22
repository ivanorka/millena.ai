package calendar

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/auth"
	"github.com/ivanorka/millena-ai/internal/limits"
)

type Handler struct {
	repository *Repository
}

func NewHandler(repository *Repository) *Handler {
	return &Handler{repository: repository}
}

func (h *Handler) List(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	from, to, ok := calendarRange(c)
	if !ok {
		return
	}
	items, err := h.repository.List(c.Request.Context(), c.Param("projectID"), from, to)
	if writeDatabaseError(c, err, "Calendar could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) Get(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	item, err := h.repository.Get(c.Request.Context(), c.Param("projectID"), c.Param("itemID"))
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Calendar item was not found.")
		return
	}
	if writeDatabaseError(c, err, "Calendar item could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) Create(c *gin.Context) {
	input, ok := bindInput(c)
	if !ok || !h.databaseAvailable(c) {
		return
	}
	item, err := h.repository.Create(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeDatabaseError(c, err, "Calendar item could not be created.") {
		return
	}
	c.Header("Location", "/api/v1/projects/"+c.Param("projectID")+"/calendar/items/"+item.ID)
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

func (h *Handler) Update(c *gin.Context) {
	input, ok := bindInput(c)
	if !ok || !h.databaseAvailable(c) {
		return
	}
	item, err := h.repository.Update(c.Request.Context(), c.Param("projectID"), c.Param("itemID"), auth.UserID(c), input)
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Calendar item was not found.")
		return
	}
	if errors.Is(err, ErrLinkedVariantChannelChange) {
		writeError(c, http.StatusConflict, "linked_channel_immutable", "Change the content variant channel instead of changing its linked calendar entry.")
		return
	}
	if errors.Is(err, limits.ErrEntitlementInactive) {
		writeError(c, http.StatusForbidden, "entitlement_inactive", "The project needs an active or trial plan for this operation.")
		return
	}
	if errors.Is(err, limits.ErrPublicationLimitReached) {
		writeError(c, http.StatusConflict, "publication_limit_reached", "The active plan's monthly publication limit has been reached.")
		return
	}
	if writeDatabaseError(c, err, "Calendar item could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) Delete(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	err := h.repository.Delete(c.Request.Context(), c.Param("projectID"), c.Param("itemID"), auth.UserID(c))
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Calendar item was not found.")
		return
	}
	if writeDatabaseError(c, err, "Calendar item could not be deleted.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func bindInput(c *gin.Context) (SaveInput, bool) {
	var input SaveInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Title, channel, status and schedule are required.")
		return SaveInput{}, false
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Channel = strings.ToLower(strings.TrimSpace(input.Channel))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if len(input.Title) < 2 || len(input.Title) > 180 || len(input.Summary) > 5000 || input.ScheduledFor.IsZero() {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Calendar item contains invalid title, summary or schedule values.")
		return SaveInput{}, false
	}
	if _, ok := supportedChannels[input.Channel]; !ok {
		writeError(c, http.StatusUnprocessableEntity, "unsupported_channel", "The selected calendar channel is not supported.")
		return SaveInput{}, false
	}
	if _, ok := supportedStatuses[input.Status]; !ok {
		writeError(c, http.StatusUnprocessableEntity, "unsupported_status", "The selected calendar status is not supported.")
		return SaveInput{}, false
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	return input, true
}

func calendarRange(c *gin.Context) (time.Time, time.Time, bool) {
	now := time.Now().UTC()
	weekday := (int(now.Weekday()) + 6) % 7
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -weekday)
	to := from.AddDate(0, 0, 7)
	var err error
	if value := strings.TrimSpace(c.Query("from")); value != "" {
		from, err = time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_range", "Calendar range must use RFC3339 timestamps.")
			return time.Time{}, time.Time{}, false
		}
	}
	if value := strings.TrimSpace(c.Query("to")); value != "" {
		to, err = time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_range", "Calendar range must use RFC3339 timestamps.")
			return time.Time{}, time.Time{}, false
		}
	}
	if !to.After(from) || to.Sub(from) > 370*24*time.Hour {
		writeError(c, http.StatusBadRequest, "invalid_range", "Calendar range is invalid or too large.")
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func (h *Handler) databaseAvailable(c *gin.Context) bool {
	if h.repository != nil {
		return true
	}
	writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
	return false
}

func writeDatabaseError(c *gin.Context, err error, message string) bool {
	if err == nil {
		return false
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "22P02" {
		writeError(c, http.StatusBadRequest, "invalid_id", "Resource IDs must be UUID values.")
		return true
	}
	writeError(c, http.StatusInternalServerError, "internal_error", message)
	return true
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
