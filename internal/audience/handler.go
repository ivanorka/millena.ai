package audience

import (
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ivanorka/millena-ai/internal/auth"
)

const maxCSVSize = 5 << 20

var validStatuses = map[string]bool{"pending": true, "active": true, "unsubscribed": true, "bounced": true}
var validSources = map[string]bool{"manual": true, "csv": true, "website": true, "api": true}

type Handler struct {
	repository *Repository
}

func NewHandler(repository *Repository) *Handler { return &Handler{repository: repository} }

func (h *Handler) Lists(c *gin.Context) {
	if !h.available(c) {
		return
	}
	items, err := h.repository.Lists(c.Request.Context(), c.Param("projectID"))
	if audienceDatabaseError(c, err, "Audience lists could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) CreateList(c *gin.Context) {
	var input ListInput
	if err := c.ShouldBindJSON(&input); err != nil {
		audienceError(c, http.StatusUnprocessableEntity, "validation_error", "A list name is required.")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 120 || utf8.RuneCountInString(input.Description) > 500 {
		audienceError(c, http.StatusUnprocessableEntity, "validation_error", "List name or description length is invalid.")
		return
	}
	if !h.available(c) {
		return
	}
	item, err := h.repository.CreateList(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if postgresCode(err) == "23505" {
		audienceError(c, http.StatusConflict, "list_conflict", "A list with this name already exists.")
		return
	}
	if audienceDatabaseError(c, err, "Audience list could not be created.") {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

func (h *Handler) UpdateList(c *gin.Context) {
	input, ok := bindList(c)
	if !ok || !h.available(c) {
		return
	}
	item, err := h.repository.UpdateList(c.Request.Context(), c.Param("projectID"), c.Param("listID"), auth.UserID(c), input)
	if postgresCode(err) == "23505" {
		audienceError(c, http.StatusConflict, "list_conflict", "A list with this name already exists.")
		return
	}
	if errors.Is(err, ErrNotFound) {
		audienceError(c, http.StatusNotFound, "not_found", "Audience list was not found.")
		return
	}
	if audienceDatabaseError(c, err, "Audience list could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) DeleteList(c *gin.Context) {
	if !h.available(c) {
		return
	}
	err := h.repository.DeleteList(c.Request.Context(), c.Param("projectID"), c.Param("listID"), auth.UserID(c))
	if errors.Is(err, ErrListNotDeletable) {
		audienceError(c, http.StatusConflict, "list_not_empty", "Default lists and lists containing contacts cannot be deleted.")
		return
	}
	if errors.Is(err, ErrNotFound) {
		audienceError(c, http.StatusNotFound, "not_found", "Audience list was not found.")
		return
	}
	if audienceDatabaseError(c, err, "Audience list could not be deleted.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Contacts(c *gin.Context) {
	if !h.available(c) {
		return
	}
	status := strings.ToLower(strings.TrimSpace(c.Query("status")))
	if status != "" && !validStatuses[status] {
		audienceError(c, http.StatusBadRequest, "unsupported_status", "The selected contact status is not supported.")
		return
	}
	items, err := h.repository.Contacts(c.Request.Context(), c.Param("projectID"), strings.TrimSpace(c.Query("search")), status, strings.TrimSpace(c.Query("listId")))
	if audienceDatabaseError(c, err, "Audience contacts could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) GetContact(c *gin.Context) {
	if !h.available(c) {
		return
	}
	item, err := h.repository.Get(c.Request.Context(), c.Param("projectID"), c.Param("contactID"))
	if errors.Is(err, ErrNotFound) {
		audienceError(c, http.StatusNotFound, "not_found", "Contact was not found.")
		return
	}
	if audienceDatabaseError(c, err, "Contact could not be loaded.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) CreateContact(c *gin.Context) {
	input, ok := bindContact(c)
	if !ok || !h.available(c) {
		return
	}
	item, err := h.repository.Create(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
	if postgresCode(err) == "23505" {
		audienceError(c, http.StatusConflict, "email_conflict", "A contact with this email already exists.")
		return
	}
	if errors.Is(err, ErrNotFound) {
		audienceError(c, http.StatusUnprocessableEntity, "list_not_found", "The selected audience list was not found.")
		return
	}
	if audienceDatabaseError(c, err, "Contact could not be created.") {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

func (h *Handler) UpdateContact(c *gin.Context) {
	input, ok := bindContact(c)
	if !ok || !h.available(c) {
		return
	}
	item, err := h.repository.Update(c.Request.Context(), c.Param("projectID"), c.Param("contactID"), auth.UserID(c), input)
	if postgresCode(err) == "23505" {
		audienceError(c, http.StatusConflict, "email_conflict", "A contact with this email already exists.")
		return
	}
	if errors.Is(err, ErrNotFound) {
		audienceError(c, http.StatusNotFound, "not_found", "Contact or audience list was not found.")
		return
	}
	if audienceDatabaseError(c, err, "Contact could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *Handler) DeleteContact(c *gin.Context) {
	if !h.available(c) {
		return
	}
	err := h.repository.Delete(c.Request.Context(), c.Param("projectID"), c.Param("contactID"), auth.UserID(c))
	if errors.Is(err, ErrNotFound) {
		audienceError(c, http.StatusNotFound, "not_found", "Contact was not found.")
		return
	}
	if audienceDatabaseError(c, err, "Contact could not be deleted.") {
		return
	}
	c.Status(http.StatusNoContent)
}

func bindList(c *gin.Context) (ListInput, bool) {
	var input ListInput
	if err := c.ShouldBindJSON(&input); err != nil {
		audienceError(c, http.StatusUnprocessableEntity, "validation_error", "A valid audience list is required.")
		return ListInput{}, false
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 120 || utf8.RuneCountInString(input.Description) > 500 {
		audienceError(c, http.StatusUnprocessableEntity, "validation_error", "List name or description length is invalid.")
		return ListInput{}, false
	}
	return input, true
}

func (h *Handler) ImportCSV(c *gin.Context) {
	if !h.available(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCSVSize+(1<<20))
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		audienceError(c, http.StatusUnprocessableEntity, "file_required", "Choose a CSV file up to 5 MB.")
		return
	}
	defer file.Close()
	reader := csv.NewReader(io.LimitReader(file, maxCSVSize+1))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil || len(rows) < 2 {
		audienceError(c, http.StatusUnprocessableEntity, "invalid_csv", "CSV must contain a header and at least one contact.")
		return
	}
	indexes := csvHeaderIndexes(rows[0])
	if indexes["email"] < 0 {
		audienceError(c, http.StatusUnprocessableEntity, "email_column_required", "CSV needs an email column.")
		return
	}
	result := ImportResult{Errors: make([]string, 0)}
	listID := strings.TrimSpace(c.PostForm("listId"))
	for index, row := range rows[1:] {
		if index >= 5000 {
			result.Skipped += len(rows[1:]) - index
			result.Errors = append(result.Errors, "Import is limited to 5,000 contacts per file.")
			break
		}
		input := ContactInput{
			FirstName: csvValue(row, indexes["first_name"]), LastName: csvValue(row, indexes["last_name"]),
			Email: strings.ToLower(strings.TrimSpace(csvValue(row, indexes["email"]))), Source: "csv", Status: "active", Consent: true,
		}
		if listID != "" {
			input.ListID = &listID
		}
		if value := strings.ToLower(csvValue(row, indexes["status"])); validStatuses[value] {
			input.Status = value
		}
		if value := strings.ToLower(csvValue(row, indexes["consent"])); value != "" {
			input.Consent, _ = strconv.ParseBool(value)
		}
		if !validEmail(input.Email) {
			result.Skipped++
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, "Row "+strconv.Itoa(index+2)+" has an invalid email.")
			}
			continue
		}
		inserted, err := h.repository.UpsertImported(c.Request.Context(), c.Param("projectID"), auth.UserID(c), input)
		if err != nil {
			result.Skipped++
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, "Row "+strconv.Itoa(index+2)+" could not be imported.")
			}
			continue
		}
		if inserted {
			result.Imported++
		} else {
			result.Updated++
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func bindContact(c *gin.Context) (ContactInput, bool) {
	var input ContactInput
	if err := c.ShouldBindJSON(&input); err != nil {
		audienceError(c, http.StatusUnprocessableEntity, "validation_error", "Contact fields must use valid JSON values.")
		return ContactInput{}, false
	}
	input.FirstName = strings.TrimSpace(input.FirstName)
	input.LastName = strings.TrimSpace(input.LastName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Source == "" {
		input.Source = "manual"
	}
	if input.Status == "" {
		if input.Consent {
			input.Status = "active"
		} else {
			input.Status = "pending"
		}
	}
	if !validEmail(input.Email) || !validSources[input.Source] || !validStatuses[input.Status] || utf8.RuneCountInString(input.FirstName) > 120 || utf8.RuneCountInString(input.LastName) > 120 {
		audienceError(c, http.StatusUnprocessableEntity, "validation_error", "Name, email, source or status is invalid.")
		return ContactInput{}, false
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	return input, true
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func csvHeaderIndexes(header []string) map[string]int {
	indexes := map[string]int{"email": -1, "first_name": -1, "last_name": -1, "status": -1, "consent": -1}
	for index, value := range header {
		normalized := strings.ToLower(strings.TrimSpace(value))
		normalized = strings.ReplaceAll(normalized, " ", "_")
		switch normalized {
		case "email", "e-mail":
			indexes["email"] = index
		case "first_name", "ime":
			indexes["first_name"] = index
		case "last_name", "prezime":
			indexes["last_name"] = index
		case "status":
			indexes["status"] = index
		case "consent", "privola":
			indexes["consent"] = index
		}
	}
	return indexes
}

func csvValue(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func (h *Handler) available(c *gin.Context) bool {
	if h != nil && h.repository != nil {
		return true
	}
	audienceError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
	return false
}

func audienceDatabaseError(c *gin.Context, err error, message string) bool {
	if err == nil {
		return false
	}
	if postgresCode(err) == "22P02" {
		audienceError(c, http.StatusBadRequest, "invalid_id", "ID must be a UUID.")
		return true
	}
	audienceError(c, http.StatusInternalServerError, "internal_error", message)
	return true
}

func postgresCode(err error) string {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return pgError.Code
	}
	return ""
}

func audienceError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
