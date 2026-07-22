package social

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/assets"
	"github.com/ivanorka/millena-ai/internal/limits"
)

type Handler struct {
	repository *Repository
}

func NewHandler(repository *Repository) *Handler {
	return &Handler{repository: repository}
}

func (h *Handler) ListConnections(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	connections, err := h.repository.ListConnections(c.Request.Context(), c.Param("projectID"))
	if writeDatabaseError(c, err, "Social connections could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": connections})
}

func (h *Handler) Connect(c *gin.Context) {
	var input ConnectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Provider, account handle and display name are required.")
		return
	}
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.AccountHandle = strings.TrimSpace(input.AccountHandle)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode == "" {
		input.Mode = "sandbox"
	}
	if _, ok := supportedProviders[input.Provider]; !ok {
		writeError(c, http.StatusUnprocessableEntity, "unsupported_provider", "The selected social provider is not supported.")
		return
	}
	if input.Mode != "sandbox" {
		writeError(c, http.StatusUnprocessableEntity, "oauth_not_configured", "Only sandbox connections are available until OAuth credentials are configured.")
		return
	}
	if len(input.AccountHandle) < 2 || len(input.AccountHandle) > 120 || len(input.DisplayName) < 2 || len(input.DisplayName) > 120 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Account handle and display name must contain between 2 and 120 characters.")
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	connection, err := h.repository.UpsertConnection(c.Request.Context(), c.Param("projectID"), input)
	if writeDatabaseError(c, err, "Social connection could not be saved.") {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": connection})
}

func (h *Handler) TestConnection(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	connection, err := h.repository.TestConnection(c.Request.Context(), c.Param("projectID"), c.Param("connectionID"))
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Social connection was not found.")
		return
	}
	if writeDatabaseError(c, err, "Social connection could not be tested.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": connection})
}

func (h *Handler) Disconnect(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	err := h.repository.Disconnect(c.Request.Context(), c.Param("projectID"), c.Param("connectionID"))
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Social connection was not found.")
		return
	}
	if writeDatabaseError(c, err, "Social connection could not be disconnected.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) CreatePost(c *gin.Context) {
	var input CreatePostInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Post body and at least one social connection are required.")
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	normalizedAssetIDs, err := assets.NormalizeIDs(input.AssetIDs, 10)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_assets", "Use up to ten valid social media asset IDs.")
		return
	}
	input.AssetIDs = normalizedAssetIDs
	if len(input.Body) == 0 || len(input.Body) > 10000 || len(input.ConnectionIDs) == 0 || len(input.ConnectionIDs) > 8 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Post body and between one and eight social connections are required.")
		return
	}
	if input.ScheduledFor != nil && input.ScheduledFor.Before(time.Now().Add(-time.Minute)) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_schedule", "Scheduled time must be in the future.")
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	post, err := h.repository.CreatePost(c.Request.Context(), c.Param("projectID"), input)
	if errors.Is(err, ErrInvalidConnections) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_connections", "Every selected social connection must belong to the project and be connected.")
		return
	}
	if writeDatabaseError(c, err, "Social post could not be created.") {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": post})
}

func (h *Handler) ListPosts(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	posts, err := h.repository.ListPosts(c.Request.Context(), c.Param("projectID"))
	if writeDatabaseError(c, err, "Social posts could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": posts})
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
	if errors.Is(err, limits.ErrEntitlementInactive) {
		writeError(c, http.StatusForbidden, "entitlement_inactive", "The project needs an active or trial plan for this operation.")
		return true
	}
	if errors.Is(err, ErrSocialChannelLimitReached) {
		writeError(c, http.StatusConflict, "social_channel_limit_reached", "The current plan's social channel limit has been reached.")
		return true
	}
	if errors.Is(err, ErrSocialChannelsUnavailable) {
		writeError(c, http.StatusForbidden, "social_channels_unavailable", "Social channels are not included in the current plan.")
		return true
	}
	if errors.Is(err, limits.ErrPublicationLimitReached) {
		writeError(c, http.StatusConflict, "publication_limit_reached", "The active plan's monthly publication limit has been reached.")
		return true
	}
	if errors.Is(err, assets.ErrInvalidReferences) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_assets", "Every asset must belong to this project and use social_media purpose.")
		return true
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "22P02":
			writeError(c, http.StatusBadRequest, "invalid_id", "Resource IDs must be UUID values.")
		case "23503":
			writeError(c, http.StatusNotFound, "project_not_found", "Project was not found.")
		default:
			writeError(c, http.StatusInternalServerError, "internal_error", message)
		}
		return true
	}
	writeError(c, http.StatusInternalServerError, "internal_error", message)
	return true
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
