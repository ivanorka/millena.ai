package assets

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCleanFilenameRemovesClientPath(t *testing.T) {
	if got := cleanFilename(`C:\\Users\\test\\strategy.pdf`); got != "strategy.pdf" {
		t.Fatalf("cleanFilename returned %q", got)
	}
	if got := cleanFilename("../campaign.png"); got != "campaign.png" {
		t.Fatalf("cleanFilename returned %q", got)
	}
	if got := cleanFilename("bad\nname.txt"); got != "" {
		t.Fatalf("expected invalid filename, got %q", got)
	}
}

func TestNormalizeIDsValidatesLimitAndDeduplicates(t *testing.T) {
	first := "9df11135-b6eb-46d6-8fc5-e35cb4704456"
	second := "2ac8e0eb-fecd-4f68-9bc1-d2882ab935e9"
	items, err := NormalizeIDs([]string{" " + first + " ", first, second}, 3)
	if err != nil {
		t.Fatalf("NormalizeIDs failed: %v", err)
	}
	if len(items) != 2 || items[0] != first || items[1] != second {
		t.Fatalf("unexpected normalized IDs: %#v", items)
	}
	if _, err := NormalizeIDs([]string{first, second}, 1); !errors.Is(err, ErrInvalidReferences) {
		t.Fatalf("expected reference limit error, got %v", err)
	}
	if _, err := NormalizeIDs([]string{"not-an-id"}, 1); !errors.Is(err, ErrInvalidReferences) {
		t.Fatalf("expected UUID validation error, got %v", err)
	}
}

func TestUploadRejectsUnsupportedPurposeBeforeDatabase(t *testing.T) {
	response := performUpload(t, "notes.txt", "text/plain", []byte("hello"), "other")
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "unsupported_purpose") {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestUploadRejectsTextAsSocialMediaBeforeDatabase(t *testing.T) {
	response := performUpload(t, "notes.txt", "text/plain", []byte("hello"), PurposeSocialMedia)
	if response.Code != http.StatusUnsupportedMediaType || !strings.Contains(response.Body.String(), "social_media_required") {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestWriteErrorMapsInactiveEntitlementToForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	if !NewHandler(nil).writeError(context, ErrEntitlementInactive, "fallback") {
		t.Fatal("expected error response")
	}
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "entitlement_inactive") {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func performUpload(t *testing.T, filename, mimeType string, data []byte, purpose string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("purpose", purpose); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects/:projectID/assets", NewHandler(nil).Upload)
	request := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-4000-8000-000000000000/assets", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
