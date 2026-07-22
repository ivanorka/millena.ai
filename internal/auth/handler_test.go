package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterRejectsWeakPasswordBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/register", NewHandler(nil, 0, false).Register)
	request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{
		"displayName":"Ana Marić","organizationName":"Nova Grupa","email":"ana@example.com","password":"short"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}

func TestRegisterRejectsUnsupportedPlanBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/register", NewHandler(nil, 0, false).Register)
	request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{
		"displayName":"Ana Marić","organizationName":"Nova Grupa","email":"ana@example.com","password":"sigurna-lozinka","planCode":"unlimited"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
}

func TestProjectSlugIsNormalized(t *testing.T) {
	slug := projectSlug("  MPR Nova Grupa! ")
	if !strings.HasPrefix(slug, "mpr-nova-grupa-") {
		t.Fatalf("unexpected slug %q", slug)
	}
}
