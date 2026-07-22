package workspace

import (
	"net/http"
	"testing"
)

func TestUpdateServiceRequestRejectsUnsupportedStatusBeforeDatabase(t *testing.T) {
	context, recorder := workspaceTestContext(http.MethodPut, `{"status":"waiting_forever"}`)

	NewHandler(nil).UpdateServiceRequest(context)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d", http.StatusUnprocessableEntity, recorder.Code)
	}
}

func TestUpdateServiceRequestRequiresStatusBeforeDatabase(t *testing.T) {
	context, recorder := workspaceTestContext(http.MethodPut, `{"summary":"Only a summary"}`)

	NewHandler(nil).UpdateServiceRequest(context)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d", http.StatusUnprocessableEntity, recorder.Code)
	}
}

func TestNormalizeServiceRequestUpdateInputTrimsOptionalFields(t *testing.T) {
	summary := "  Updated service request  "
	metadata := map[string]any{"source": "test"}
	input := ServiceRequestUpdateInput{Status: " IN_PROGRESS ", Summary: &summary, Metadata: &metadata}

	if message := normalizeServiceRequestUpdateInput(&input); message != "" {
		t.Fatalf("unexpected validation error: %s", message)
	}
	if input.Status != "in_progress" || input.Summary == nil || *input.Summary != "Updated service request" {
		t.Fatalf("input was not normalized: %+v", input)
	}
}

func TestNormalizeServiceRequestUpdateInputRejectsOversizedMetadata(t *testing.T) {
	metadata := map[string]any{"payload": string(make([]byte, 33<<10))}
	input := ServiceRequestUpdateInput{Status: "open", Metadata: &metadata}

	if message := normalizeServiceRequestUpdateInput(&input); message == "" {
		t.Fatal("expected oversized metadata to be rejected")
	}
}
