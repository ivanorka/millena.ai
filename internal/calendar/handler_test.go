package calendar

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateRejectsUnsupportedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/projects/:projectID/calendar/items", NewHandler(nil).Create)
	request := httptest.NewRequest(http.MethodPost, "/projects/00000000-0000-0000-0000-000000000000/calendar/items", strings.NewReader(`{
		"title":"Test item","channel":"unknown","status":"draft","scheduledFor":"2026-08-01T10:00:00Z"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}
