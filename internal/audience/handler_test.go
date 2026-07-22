package audience

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateRejectsInvalidEmailBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/contacts", NewHandler(nil).CreateContact)
	request := httptest.NewRequest(http.MethodPost, "/contacts", strings.NewReader(`{"firstName":"Test","email":"not-an-email","consent":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}

func TestCSVHeaderIndexesAcceptsCroatianColumns(t *testing.T) {
	indexes := csvHeaderIndexes([]string{"Ime", "Prezime", "Email", "Privola"})
	if indexes["first_name"] != 0 || indexes["last_name"] != 1 || indexes["email"] != 2 || indexes["consent"] != 3 {
		t.Fatalf("unexpected indexes: %#v", indexes)
	}
}

func TestUpdateListRejectsInvalidNameBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/lists/:listID", NewHandler(nil).UpdateList)
	request := httptest.NewRequest(http.MethodPut, "/lists/00000000-0000-0000-0000-000000000000", strings.NewReader(`{"name":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}
