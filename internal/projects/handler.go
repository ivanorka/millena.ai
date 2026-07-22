package projects

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/auth"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

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

	items, err := h.repository.List(c.Request.Context(), auth.UserID(c))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Projects could not be loaded.")
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) Get(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}

	project, err := h.repository.GetByID(c.Request.Context(), c.Param("projectID"))
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Project was not found.")
		return
	}
	if postgresErrorCode(err) == "22P02" {
		writeError(c, http.StatusBadRequest, "invalid_id", "Project ID must be a UUID.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Project could not be loaded.")
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": project})
}

func (h *Handler) Create(c *gin.Context) {
	var input CreateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Name, slug and a supported locale are required.")
		return
	}

	input.Name = strings.TrimSpace(input.Name)
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	if input.DefaultLocale == "" {
		input.DefaultLocale = "hr"
	}
	if len(input.Name) < 2 {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Name must contain at least two characters.")
		return
	}
	if !slugPattern.MatchString(input.Slug) {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Slug may contain lowercase letters, numbers and single hyphens.")
		return
	}
	if !h.databaseAvailable(c) {
		return
	}

	project, err := h.repository.Create(c.Request.Context(), input, auth.UserID(c))
	if postgresErrorCode(err) == "23505" {
		writeError(c, http.StatusConflict, "slug_conflict", "A project with this slug already exists.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Project could not be created.")
		return
	}

	c.Header("Location", "/api/v1/projects/"+project.ID)
	c.JSON(http.StatusCreated, gin.H{"data": project})
}

func (h *Handler) Delete(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}

	if err := h.repository.Delete(c.Request.Context(), c.Param("projectID"), auth.UserID(c)); errors.Is(err, ErrLastProject) {
		writeError(c, http.StatusConflict, "last_project", "Create or join another project before deleting your last project.")
		return
	} else if errors.Is(err, ErrProtectedProject) {
		writeError(c, http.StatusConflict, "protected_project", "The preconfigured MPR Grupa project cannot be deleted.")
		return
	} else if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Project was not found.")
		return
	} else if postgresErrorCode(err) == "22P02" {
		writeError(c, http.StatusBadRequest, "invalid_id", "Project ID must be a UUID.")
		return
	} else if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Project could not be deleted.")
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) BootstrapDemo(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}

	result, err := h.repository.BootstrapDemo(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Workspace could not be initialized.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *Handler) GetAppState(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}

	appState, err := h.repository.GetAppState(c.Request.Context(), c.Param("projectID"))
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Workspace state was not found.")
		return
	}
	if postgresErrorCode(err) == "22P02" {
		writeError(c, http.StatusBadRequest, "invalid_id", "Project ID must be a UUID.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Workspace state could not be loaded.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": appState})
}

func (h *Handler) SaveAppState(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var input SaveAppStateInput
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(c, http.StatusRequestEntityTooLarge, "state_too_large", "Workspace state must be smaller than 1 MB.")
			return
		}
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "A valid JSON workspace state is required.")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Only one JSON object is allowed.")
		return
	}
	if len(input.State) == 0 || string(input.State) == "null" || !json.Valid(input.State) {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "A valid JSON workspace state is required.")
		return
	}
	var stateObject map[string]json.RawMessage
	if err := json.Unmarshal(input.State, &stateObject); err != nil || stateObject == nil {
		writeError(c, http.StatusUnprocessableEntity, "validation_error", "Workspace state must be a JSON object.")
		return
	}

	appState, err := h.repository.SaveAppState(c.Request.Context(), c.Param("projectID"), input.State)
	if errors.Is(err, ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "Project was not found.")
		return
	}
	if postgresErrorCode(err) == "22P02" {
		writeError(c, http.StatusBadRequest, "invalid_id", "Project ID must be a UUID.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Workspace state could not be saved.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": appState})
}

func (h *Handler) databaseAvailable(c *gin.Context) bool {
	if h.repository != nil {
		return true
	}
	writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
	return false
}

func postgresErrorCode(err error) string {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return pgError.Code
	}
	return ""
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}
