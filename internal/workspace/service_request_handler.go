package workspace

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ivanorka/millena-ai/internal/auth"
)

var supportedServiceRequestStatuses = map[string]struct{}{
	"open": {}, "in_progress": {}, "completed": {}, "cancelled": {},
}

func (h *Handler) UpdateServiceRequest(c *gin.Context) {
	var input ServiceRequestUpdateInput
	if !decodeWorkspaceJSON(c, &input) {
		return
	}
	if message := normalizeServiceRequestUpdateInput(&input); message != "" {
		writeWorkspaceError(c, http.StatusUnprocessableEntity, "validation_error", message)
		return
	}
	if !h.databaseAvailable(c) {
		return
	}
	request, err := h.repository.UpdateServiceRequest(
		c.Request.Context(), c.Param("projectID"), c.Param("requestID"), auth.UserID(c), input,
	)
	if writeRepositoryError(c, err, "Service request could not be updated.") {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": request})
}

func normalizeServiceRequestUpdateInput(input *ServiceRequestUpdateInput) string {
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if _, ok := supportedServiceRequestStatuses[input.Status]; !ok {
		return "Service request status is not supported."
	}
	if input.Summary != nil {
		value := strings.TrimSpace(*input.Summary)
		input.Summary = &value
		if runeLength(value) > 4000 {
			return "Service request summary must be at most 4000 characters."
		}
	}
	if input.Metadata != nil {
		if *input.Metadata == nil {
			value := map[string]any{}
			input.Metadata = &value
		}
		if !validJSONMapSize(*input.Metadata, 32<<10) {
			return "Service request metadata must be smaller than 32 KB."
		}
	}
	return ""
}
