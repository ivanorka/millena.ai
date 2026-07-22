package content

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ivanorka/millena-ai/internal/limits"
)

func TestCreateRejectsUnsupportedKind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects/:projectID/content/items", NewHandler(nil, nil).Create)
	request := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-0000-0000-000000000000/content/items", strings.NewReader(`{
		"kind":"unknown","status":"draft","title":"Test","body":"Body"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}

func TestScheduledContentRequiresDate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects/:projectID/content/items", NewHandler(nil, nil).Create)
	request := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-0000-0000-000000000000/content/items", strings.NewReader(`{
		"kind":"blog","status":"scheduled","title":"Test article","body":"Body"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}

func TestInactiveEntitlementMapsToForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	if !writeContentDatabaseError(context, limits.ErrEntitlementInactive, "fallback") {
		t.Fatal("expected error to be handled")
	}
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "entitlement_inactive") {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestInvalidAssetReferencesMapToUnprocessableEntity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	if !writeContentDatabaseError(context, ErrInvalidAssetReferences, "fallback") {
		t.Fatal("expected error to be handled")
	}
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "invalid_asset_references") {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}
