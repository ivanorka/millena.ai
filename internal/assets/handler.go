package assets

import (
	"crypto/sha256"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/auth"
	"github.com/ivanorka/millena-ai/internal/content"
)

const assetRequestAllowance = 1 << 20

type Handler struct {
	repository *Repository
}

func NewHandler(repository *Repository) *Handler { return &Handler{repository: repository} }

func (h *Handler) List(c *gin.Context) {
	purpose := strings.ToLower(strings.TrimSpace(c.Query("purpose")))
	if purpose != "" && !ValidPurpose(purpose) {
		writeAssetError(c, http.StatusBadRequest, "unsupported_purpose", "Asset purpose is not supported.")
		return
	}
	if !h.available(c) {
		return
	}
	items, err := h.repository.List(c.Request.Context(), c.Param("projectID"), purpose)
	if h.writeError(c, err, "Assets could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) Get(c *gin.Context) {
	if !h.available(c) {
		return
	}
	item, err := h.repository.Get(c.Request.Context(), c.Param("projectID"), c.Param("assetID"))
	if h.writeError(c, err, "Asset could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) Upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxAssetSize+assetRequestAllowance)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		writeAssetError(c, http.StatusUnprocessableEntity, "file_required", "Choose a file up to 4 MB.")
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > MaxAssetSize {
		writeAssetError(c, http.StatusRequestEntityTooLarge, "file_too_large", "Files are limited to 4 MB.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxAssetSize+1))
	if err != nil {
		writeAssetError(c, http.StatusUnprocessableEntity, "file_read_failed", "The selected file could not be read.")
		return
	}
	if len(data) < 1 || int64(len(data)) > MaxAssetSize {
		writeAssetError(c, http.StatusRequestEntityTooLarge, "file_too_large", "Files are limited to 4 MB.")
		return
	}
	purpose := strings.ToLower(strings.TrimSpace(c.PostForm("purpose")))
	if !ValidPurpose(purpose) {
		writeAssetError(c, http.StatusUnprocessableEntity, "unsupported_purpose", "Purpose must be assistant_attachment, social_media or content_media.")
		return
	}
	filename := cleanFilename(header.Filename)
	if filename == "" {
		writeAssetError(c, http.StatusUnprocessableEntity, "invalid_filename", "A valid filename is required.")
		return
	}
	mimeType := assetMIMEType(filename, header.Header.Get("Content-Type"), data)
	if purpose == PurposeSocialMedia && !isSocialMediaType(mimeType) {
		writeAssetError(c, http.StatusUnsupportedMediaType, "social_media_required", "Social media assets must be an image or video.")
		return
	}
	var extractedText *string
	if extracted, extractErr := content.ExtractDocument(filename, data); extractErr == nil {
		extractedText = &extracted
	}
	if !h.available(c) {
		return
	}
	digest := sha256.Sum256(data)
	item, err := h.repository.Create(c.Request.Context(), c.Param("projectID"), auth.UserID(c), UploadInput{
		Purpose: purpose, Filename: filename, MIMEType: mimeType, Data: data,
		SHA256: digest, ExtractedText: extractedText,
	})
	if h.writeError(c, err, "Asset could not be uploaded.") {
		return
	}
	c.Header("Location", "/api/v1/projects/"+c.Param("projectID")+"/assets/"+item.ID)
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

func (h *Handler) Update(c *gin.Context) {
	var input UpdateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeAssetError(c, http.StatusUnprocessableEntity, "validation_error", "Filename and purpose must use valid JSON values.")
		return
	}
	input.Filename = cleanFilename(input.Filename)
	input.Purpose = strings.ToLower(strings.TrimSpace(input.Purpose))
	if input.Filename == "" || !ValidPurpose(input.Purpose) {
		writeAssetError(c, http.StatusUnprocessableEntity, "validation_error", "A valid filename and purpose are required.")
		return
	}
	if !h.available(c) {
		return
	}
	item, err := h.repository.Update(c.Request.Context(), c.Param("projectID"), c.Param("assetID"), auth.UserID(c), input)
	if h.writeError(c, err, "Asset could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) Download(c *gin.Context) {
	if !h.available(c) {
		return
	}
	blob, err := h.repository.Download(c.Request.Context(), c.Param("projectID"), c.Param("assetID"), auth.UserID(c))
	if h.writeError(c, err, "Asset could not be downloaded.") {
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": blob.Filename})
	c.Header("Content-Disposition", disposition)
	c.Header("Content-Length", strconv.FormatInt(blob.SizeBytes, 10))
	c.Header("Cache-Control", "private, no-store")
	c.Header("ETag", `"sha256-`+blob.SHA256+`"`)
	c.Data(http.StatusOK, blob.MIMEType, blob.Data)
}

func (h *Handler) Delete(c *gin.Context) {
	if !h.available(c) {
		return
	}
	err := h.repository.Delete(c.Request.Context(), c.Param("projectID"), c.Param("assetID"), auth.UserID(c))
	if h.writeError(c, err, "Asset could not be deleted.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func cleanFilename(value string) string {
	value = strings.TrimSpace(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
	if value == "." || value == ".." || utf8.RuneCountInString(value) > 255 || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}

func assetMIMEType(filename, declared string, data []byte) string {
	if parsed, _, err := mime.ParseMediaType(declared); err == nil && parsed != "" && parsed != "application/octet-stream" {
		return strings.ToLower(parsed)
	}
	extensionTypes := map[string]string{
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".pdf":  "application/pdf", ".md": "text/markdown", ".csv": "text/csv",
	}
	if value := extensionTypes[strings.ToLower(filepath.Ext(filename))]; value != "" {
		return value
	}
	return http.DetectContentType(data)
}

func (h *Handler) available(c *gin.Context) bool {
	if h != nil && h.repository != nil {
		return true
	}
	writeAssetError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
	return false
}

func (h *Handler) writeError(c *gin.Context, err error, message string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrNotFound):
		writeAssetError(c, http.StatusNotFound, "not_found", "Asset was not found.")
	case errors.Is(err, ErrEntitlementInactive):
		writeAssetError(c, http.StatusForbidden, "entitlement_inactive", "An active or trial plan is required to upload assets.")
	case errors.Is(err, ErrStorageLimitReached):
		writeAssetError(c, http.StatusConflict, "storage_limit_reached", "The active plan's storage limit would be exceeded.")
	case errors.Is(err, ErrAssetInUse):
		writeAssetError(c, http.StatusConflict, "asset_in_use", "A linked asset cannot change purpose.")
	case errors.Is(err, ErrInvalidMediaPurpose):
		writeAssetError(c, http.StatusUnsupportedMediaType, "social_media_required", "Social media assets must be an image or video.")
	default:
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "22P02" {
			writeAssetError(c, http.StatusBadRequest, "invalid_id", "Project and asset IDs must be UUID values.")
		} else {
			writeAssetError(c, http.StatusInternalServerError, "internal_error", message)
		}
	}
	return true
}

func writeAssetError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
