package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestProjectFeatureEnabledRequiresBooleanTrue(t *testing.T) {
	tests := []struct {
		name     string
		features map[string]any
		want     bool
	}{
		{name: "enabled", features: map[string]any{"automations": true}, want: true},
		{name: "disabled", features: map[string]any{"automations": false}},
		{name: "missing", features: map[string]any{}},
		{name: "wrong type", features: map[string]any{"automations": "true"}},
		{name: "nil", features: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := projectFeatureEnabled(test.features, "automations"); got != test.want {
				t.Fatalf("projectFeatureEnabled() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFeatureGuardWithoutDatabaseStopsRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	called := false
	router.GET("/projects/:projectID", requireProjectFeature(nil, "automations"), func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/projects/a047b1c3-a997-4b70-a128-bb4764b826cf", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if called {
		t.Fatal("feature guard called the protected handler")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
	if !strings.Contains(response.Body.String(), `"code":"database_unavailable"`) {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}
