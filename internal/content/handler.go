package content

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/auth"
	"github.com/ivanorka/millena-ai/internal/limits"
)

const maxStrategyFileSize = 25 << 20

type Handler struct {
	repository *Repository
	ai         *AIService
}

func NewHandler(repository *Repository, ai *AIService) *Handler {
	if ai == nil {
		ai = NewAIService(AIOptions{})
	}
	return &Handler{repository: repository, ai: ai}
}

func (h *Handler) List(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	filter := ListFilter{
		Kind:   strings.ToLower(strings.TrimSpace(c.Query("kind"))),
		Status: strings.ToLower(strings.TrimSpace(c.Query("status"))),
		Search: strings.TrimSpace(c.Query("search")),
	}
	if filter.Kind != "" {
		if _, ok := supportedKinds[filter.Kind]; !ok {
			writeContentError(c, http.StatusBadRequest, "unsupported_kind", "The selected content category is not supported.")
			return
		}
	}
	if filter.Status != "" {
		if _, ok := supportedStatuses[filter.Status]; !ok {
			writeContentError(c, http.StatusBadRequest, "unsupported_status", "The selected content status is not supported.")
			return
		}
	}
	items, err := h.repository.List(c.Request.Context(), c.Param("projectID"), filter)
	if writeContentDatabaseError(c, err, "Content could not be loaded.") {
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
		writeContentError(c, http.StatusNotFound, "not_found", "Content entry was not found.")
		return
	}
	if writeContentDatabaseError(c, err, "Content could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) Create(c *gin.Context) {
	input, ok := bindContentInput(c)
	if !ok || !h.databaseAvailable(c) {
		return
	}
	item, err := h.repository.Create(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeContentDatabaseError(c, err, "Content could not be created.") {
		return
	}
	_ = h.repository.RecordAudit(c.Request.Context(), c.Param("projectID"), auth.UserID(c), "content.created", "content_item", &item.ID, map[string]any{"kind": item.Kind, "source": item.Source})
	c.Header("Location", "/api/v1/projects/"+c.Param("projectID")+"/content/items/"+item.ID)
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

func (h *Handler) Update(c *gin.Context) {
	input, ok := bindContentInput(c)
	if !ok || !h.databaseAvailable(c) {
		return
	}
	item, err := h.repository.Update(c.Request.Context(), c.Param("projectID"), c.Param("itemID"), auth.UserID(c), input)
	if errors.Is(err, ErrNotFound) {
		writeContentError(c, http.StatusNotFound, "not_found", "Content entry was not found.")
		return
	}
	if writeContentDatabaseError(c, err, "Content could not be updated.") {
		return
	}
	_ = h.repository.RecordAudit(c.Request.Context(), c.Param("projectID"), auth.UserID(c), "content.updated", "content_item", &item.ID, map[string]any{"kind": item.Kind, "revision": item.Revision})
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) Delete(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	itemID := c.Param("itemID")
	err := h.repository.Delete(c.Request.Context(), c.Param("projectID"), itemID)
	if errors.Is(err, ErrNotFound) {
		writeContentError(c, http.StatusNotFound, "not_found", "Content entry was not found.")
		return
	}
	if writeContentDatabaseError(c, err, "Content could not be deleted.") {
		return
	}
	_ = h.repository.RecordAudit(c.Request.Context(), c.Param("projectID"), auth.UserID(c), "content.deleted", "content_item", &itemID, map[string]any{})
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListVariants(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	items, err := h.repository.ListVariants(c.Request.Context(), c.Param("projectID"), c.Param("itemID"))
	if writeContentDatabaseError(c, err, "Content variants could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) SaveVariant(c *gin.Context) {
	var input VariantInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeContentError(c, http.StatusUnprocessableEntity, "validation_error", "Variant fields must use valid JSON values.")
		return
	}
	input.Channel = strings.ToLower(strings.TrimSpace(input.Channel))
	input.Locale = strings.ToLower(strings.TrimSpace(input.Locale))
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Body = strings.TrimSpace(input.Body)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Locale == "" {
		input.Locale = "hr"
	}
	if !supportedVariantChannels[input.Channel] || (input.Locale != "hr" && input.Locale != "en") {
		writeContentError(c, http.StatusUnprocessableEntity, "validation_error", "Variant channel or locale is not supported.")
		return
	}
	if _, ok := supportedStatuses[input.Status]; !ok {
		writeContentError(c, http.StatusUnprocessableEntity, "unsupported_status", "The selected variant status is not supported.")
		return
	}
	if utf8.RuneCountInString(input.Title) < 2 || utf8.RuneCountInString(input.Title) > 180 || utf8.RuneCountInString(input.Summary) > 500 || utf8.RuneCountInString(input.Body) > 100000 {
		writeContentError(c, http.StatusUnprocessableEntity, "validation_error", "Variant title, summary or body length is invalid.")
		return
	}
	if input.Status == "scheduled" && input.ScheduledFor == nil {
		writeContentError(c, http.StatusUnprocessableEntity, "schedule_required", "A scheduled variant needs a date and time.")
		return
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	if !h.databaseAvailable(c) {
		return
	}
	item, err := h.repository.SaveVariant(c.Request.Context(), c.Param("projectID"), c.Param("itemID"), auth.UserID(c), input)
	if errors.Is(err, ErrNotFound) {
		writeContentError(c, http.StatusNotFound, "not_found", "Content entry was not found.")
		return
	}
	if writeContentDatabaseError(c, err, "Content variant could not be saved.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) DeleteVariant(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	err := h.repository.DeleteVariant(c.Request.Context(), c.Param("projectID"), c.Param("itemID"), c.Param("variantID"), auth.UserID(c))
	if errors.Is(err, ErrNotFound) {
		writeContentError(c, http.StatusNotFound, "not_found", "Content variant was not found.")
		return
	}
	if writeContentDatabaseError(c, err, "Content variant could not be deleted.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) GetStrategy(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	strategy, err := h.repository.GetStrategy(c.Request.Context(), c.Param("projectID"))
	if writeContentDatabaseError(c, err, "Project strategy could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": strategy})
}

func (h *Handler) SaveStrategy(c *gin.Context) {
	var input StrategyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeContentError(c, http.StatusUnprocessableEntity, "validation_error", "Strategy fields must use valid JSON values.")
		return
	}
	normalizeStrategyInput(&input)
	if input.Mode == "" {
		input.Mode = "questions"
	}
	if input.Mode != "questions" && input.Mode != "upload" {
		writeContentError(c, http.StatusUnprocessableEntity, "unsupported_mode", "Strategy mode must be questions or upload.")
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	strategy, err := h.repository.SaveStrategy(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if writeContentDatabaseError(c, err, "Project strategy could not be saved.") {
		return
	}
	_ = h.repository.RecordAudit(c.Request.Context(), c.Param("projectID"), auth.UserID(c), "strategy.updated", "project_strategy", nil, map[string]any{"mode": strategy.Mode, "revision": strategy.Revision})
	c.JSON(http.StatusOK, gin.H{"data": strategy})
}

func (h *Handler) UploadStrategy(c *gin.Context) {
	if !h.databaseAvailable(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxStrategyFileSize+(1<<20))
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		writeContentError(c, http.StatusUnprocessableEntity, "file_required", "Choose a PDF, DOCX, PPTX, TXT or MD strategy file up to 25 MB.")
		return
	}
	defer file.Close()
	if header.Size > maxStrategyFileSize {
		writeContentError(c, http.StatusRequestEntityTooLarge, "file_too_large", "Strategy files are limited to 25 MB.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxStrategyFileSize+1))
	if err != nil {
		writeContentError(c, http.StatusUnprocessableEntity, "file_read_failed", "The strategy file could not be read.")
		return
	}
	if len(data) > maxStrategyFileSize {
		writeContentError(c, http.StatusRequestEntityTooLarge, "file_too_large", "Strategy files are limited to 25 MB.")
		return
	}
	extracted, err := ExtractDocument(header.Filename, data)
	if errors.Is(err, ErrUnsupportedDocument) {
		writeContentError(c, http.StatusUnsupportedMediaType, "unsupported_file", "Supported strategy formats are PDF, DOCX, PPTX, TXT and MD.")
		return
	}
	if errors.Is(err, ErrNoDocumentText) {
		writeContentError(c, http.StatusUnprocessableEntity, "no_extractable_text", "No readable text was found. Scanned documents need OCR before upload.")
		return
	}
	if err != nil {
		writeContentError(c, http.StatusUnprocessableEntity, "extraction_failed", "Text could not be extracted from the strategy file.")
		return
	}
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(header.Filename)))
	}
	strategy, err := h.repository.SaveStrategyFile(c.Request.Context(), c.Param("projectID"), auth.UserID(c), filepath.Base(header.Filename), mimeType, extracted)
	if writeContentDatabaseError(c, err, "Strategy file could not be stored.") {
		return
	}
	_ = h.repository.RecordAudit(c.Request.Context(), c.Param("projectID"), auth.UserID(c), "strategy.file_uploaded", "project_strategy", nil, map[string]any{"filename": strategy.SourceFilename, "characters": utf8.RuneCountInString(extracted), "revision": strategy.Revision})
	c.JSON(http.StatusOK, gin.H{"data": strategy, "meta": gin.H{"extractedCharacters": utf8.RuneCountInString(extracted)}})
}

func (h *Handler) AIStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.ai.Status()})
}

func (h *Handler) RunAI(c *gin.Context) {
	var input AIInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeContentError(c, http.StatusUnprocessableEntity, "validation_error", "AI operation, category and text values are required.")
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	strategy, err := h.repository.GetStrategy(c.Request.Context(), c.Param("projectID"))
	if writeContentDatabaseError(c, err, "Project strategy could not be loaded for AI.") {
		return
	}
	result, err := h.ai.Run(c.Request.Context(), strategy, input)
	if err != nil {
		writeContentError(c, http.StatusUnprocessableEntity, "ai_validation_error", err.Error())
		return
	}
	_ = h.repository.RecordAudit(c.Request.Context(), c.Param("projectID"), auth.UserID(c), "content.ai_"+result.Operation, "content_ai", nil, map[string]any{
		"kind": input.Kind, "provider": result.Provider, "strategyRevision": result.StrategyRevision, "contextUsed": result.ContextUsed,
	})
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func bindContentInput(c *gin.Context) (SaveInput, bool) {
	var input SaveInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeContentError(c, http.StatusUnprocessableEntity, "validation_error", "Category, status, title and body must use valid values.")
		return SaveInput{}, false
	}
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Body = strings.TrimSpace(input.Body)
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	if input.Source == "" {
		input.Source = "manual"
	}
	input.Channels = cleanStringSlice(input.Channels, 12, 50)
	if _, ok := supportedKinds[input.Kind]; !ok {
		writeContentError(c, http.StatusUnprocessableEntity, "unsupported_kind", "The selected content category is not supported.")
		return SaveInput{}, false
	}
	if _, ok := supportedStatuses[input.Status]; !ok {
		writeContentError(c, http.StatusUnprocessableEntity, "unsupported_status", "The selected content status is not supported.")
		return SaveInput{}, false
	}
	if _, ok := supportedSources[input.Source]; !ok {
		writeContentError(c, http.StatusUnprocessableEntity, "unsupported_source", "The selected content source is not supported.")
		return SaveInput{}, false
	}
	if utf8.RuneCountInString(input.Title) < 2 || utf8.RuneCountInString(input.Title) > 180 || utf8.RuneCountInString(input.Summary) > 500 || utf8.RuneCountInString(input.Body) > 100000 {
		writeContentError(c, http.StatusUnprocessableEntity, "validation_error", "Title, summary or body length is invalid.")
		return SaveInput{}, false
	}
	if input.Status == "scheduled" && input.ScheduledFor == nil {
		writeContentError(c, http.StatusUnprocessableEntity, "schedule_required", "Scheduled content needs a date and time.")
		return SaveInput{}, false
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	return input, true
}

func normalizeStrategyInput(input *StrategyInput) {
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	input.SixMonthGoal = strings.TrimSpace(input.SixMonthGoal)
	input.PrimaryGoals = cleanStringSlice(input.PrimaryGoals, 20, 120)
	input.PriorityTopics = cleanStringSlice(input.PriorityTopics, 30, 120)
	input.Audience = strings.TrimSpace(input.Audience)
	input.AudienceProblem = strings.TrimSpace(input.AudienceProblem)
	input.BrandMessage = strings.TrimSpace(input.BrandMessage)
	input.ProofPoints = strings.TrimSpace(input.ProofPoints)
	input.ForbiddenTopics = strings.TrimSpace(input.ForbiddenTopics)
	input.SuccessMetrics = strings.TrimSpace(input.SuccessMetrics)
	input.Tone = strings.TrimSpace(input.Tone)
}

func cleanStringSlice(values []string, maxItems, maxLength int) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || utf8.RuneCountInString(value) > maxLength {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == maxItems {
			break
		}
	}
	if result == nil {
		return []string{}
	}
	return result
}

func (h *Handler) databaseAvailable(c *gin.Context) bool {
	if h.repository != nil {
		return true
	}
	writeContentError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
	return false
}

func writeContentDatabaseError(c *gin.Context, err error, message string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, limits.ErrEntitlementInactive) {
		writeContentError(c, http.StatusForbidden, "entitlement_inactive", "The project needs an active or trial plan for this operation.")
		return true
	}
	if errors.Is(err, limits.ErrPublicationLimitReached) {
		writeContentError(c, http.StatusConflict, "publication_limit_reached", "The active plan's monthly publication limit has been reached.")
		return true
	}
	if errors.Is(err, ErrInvalidAssetReferences) {
		writeContentError(c, http.StatusUnprocessableEntity, "invalid_asset_references", "Content media must use valid asset IDs from this project.")
		return true
	}
	if errors.Is(err, ErrInvalidNewsletterTarget) {
		writeContentError(c, http.StatusUnprocessableEntity, "invalid_newsletter_target", "The selected newsletter campaign must exist in this project.")
		return true
	}
	if errors.Is(err, ErrNewsletterDeliveryVariantConflict) {
		writeContentError(c, http.StatusConflict, "newsletter_delivery_variant_conflict", "The active newsletter delivery is linked to another locale variant.")
		return true
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "22P02" {
		writeContentError(c, http.StatusBadRequest, "invalid_id", "Resource IDs must be UUID values.")
		return true
	}
	writeContentError(c, http.StatusInternalServerError, "internal_error", message)
	return true
}

func writeContentError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
