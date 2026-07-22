package social

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ivanorka/millena-ai/internal/limits"
)

func TestConnectRejectsOAuthUntilConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects/:projectID/social/connections", NewHandler(nil).Connect)

	request := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-0000-0000-000000000000/social/connections", strings.NewReader(`{
		"provider":"linkedin",
		"accountHandle":"millena-test",
		"displayName":"Millena Test",
		"mode":"oauth"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
	if !strings.Contains(response.Body.String(), "oauth_not_configured") {
		t.Fatalf("expected oauth_not_configured response, got %s", response.Body.String())
	}
}

func TestCreatePostRequiresConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects/:projectID/social/posts", NewHandler(nil).CreatePost)

	request := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-0000-0000-000000000000/social/posts", strings.NewReader(`{"body":"Test post","connectionIds":[]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}

func TestCreatePostRejectsInvalidAssetIDBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects/:projectID/social/posts", NewHandler(nil).CreatePost)

	request := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-4000-8000-000000000000/social/posts", strings.NewReader(`{
		"body":"Test post",
		"connectionIds":["9df11135-b6eb-46d6-8fc5-e35cb4704456"],
		"assetIds":["not-a-uuid"]
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "invalid_assets") {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestDatabaseErrorsMapPlanFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "inactive", err: limits.ErrEntitlementInactive, status: http.StatusForbidden, code: "entitlement_inactive"},
		{name: "channel limit", err: ErrSocialChannelLimitReached, status: http.StatusConflict, code: "social_channel_limit_reached"},
		{name: "channels unavailable", err: ErrSocialChannelsUnavailable, status: http.StatusForbidden, code: "social_channels_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			if !writeDatabaseError(context, test.err, "fallback") {
				t.Fatal("expected error to be handled")
			}
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.code) {
				t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
			}
		})
	}
}
