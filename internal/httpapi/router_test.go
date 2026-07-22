package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLiveness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(RouterOptions{})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, response.Code)
	}
}

func TestReadinessWithoutDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(RouterOptions{})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
}
