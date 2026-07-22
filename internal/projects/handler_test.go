package projects

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateRejectsInvalidSlugBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects", NewHandler(nil).Create)

	request := httptest.NewRequest(
		http.MethodPost,
		"/projects",
		strings.NewReader(`{"name":"MPR Grupa","slug":"Not a slug"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}

func TestCreateRejectsWhitespaceOnlyName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects", NewHandler(nil).Create)

	request := httptest.NewRequest(
		http.MethodPost,
		"/projects",
		strings.NewReader(`{"name":"  ","slug":"mpr-grupa"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}
