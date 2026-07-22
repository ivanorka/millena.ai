package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type testStore struct {
	createdMemberInput *CreateMemberInput
	createdPlanInput   *CreatePlanInput
	createMemberError  error
	updateMemberError  error
	deleteMemberError  error
	updatedPlanCode    string
}

func (s *testStore) ListTeam(context.Context, string) ([]TeamMember, error) {
	return []TeamMember{}, nil
}

func (s *testStore) CreateMember(_ context.Context, _ string, _ string, input CreateMemberInput) (TeamMember, error) {
	s.createdMemberInput = &input
	if s.createMemberError != nil {
		return TeamMember{}, s.createMemberError
	}
	return TeamMember{UserID: "00000000-0000-0000-0000-000000000002", Email: input.Email, DisplayName: input.DisplayName, Role: input.Role, MembershipStatus: "active"}, nil
}

func (s *testStore) UpdateMember(context.Context, string, string, string, UpdateMemberInput) (TeamMember, error) {
	return TeamMember{}, s.updateMemberError
}

func (s *testStore) DeleteMember(context.Context, string, string, string) error {
	return s.deleteMemberError
}

func (s *testStore) ListPlans(context.Context, string) ([]Plan, error) {
	return []Plan{}, nil
}

func (s *testStore) CreateCustomPlan(_ context.Context, _ string, _ string, input CreatePlanInput) (Plan, error) {
	s.createdPlanInput = &input
	return Plan{Code: input.Code, Name: input.Name, Currency: input.Currency, BillingInterval: input.BillingInterval, Features: input.Features}, nil
}

func (s *testStore) GetEntitlement(context.Context, string) (Entitlement, error) {
	return Entitlement{}, nil
}

func (s *testStore) UpdateEntitlement(_ context.Context, _ string, _ string, planCode string) (Entitlement, error) {
	s.updatedPlanCode = planCode
	return Entitlement{PlanCode: planCode}, nil
}

func TestCreateMemberRejectsWeakTemporaryPassword(t *testing.T) {
	response := serveAdminRequest(t, nil, http.MethodPost, "/projects/project/team", NewHandler(nil).CreateMember, `{
		"displayName":"Ana Marić","email":"ana@example.com","role":"editor","tempPassword":"short"
	}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
	if !strings.Contains(response.Body.String(), "weak_password") {
		t.Fatalf("expected weak_password response, got %s", response.Body.String())
	}
}

func TestCreateMemberNormalizesFields(t *testing.T) {
	store := &testStore{}
	response := serveAdminRequest(t, store, http.MethodPost, "/projects/project/team", NewHandler(store).CreateMember, `{
		"displayName":"  Ana Marić  ","email":"  ANA@EXAMPLE.COM ","role":" EDITOR ","tempPassword":"temporary-pass"
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if store.createdMemberInput == nil {
		t.Fatal("expected repository call")
	}
	if store.createdMemberInput.DisplayName != "Ana Marić" || store.createdMemberInput.Email != "ana@example.com" || store.createdMemberInput.Role != "editor" {
		t.Fatalf("unexpected normalized input: %#v", store.createdMemberInput)
	}
}

func TestCreateMemberMapsInactiveEntitlement(t *testing.T) {
	store := &testStore{createMemberError: ErrEntitlementInactive}
	response := serveAdminRequest(t, store, http.MethodPost, "/projects/project/team", NewHandler(store).CreateMember, `{
		"displayName":"Ana Marić","email":"ana@example.com","role":"editor","tempPassword":"temporary-pass"
	}`)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "entitlement_inactive") {
		t.Fatalf("expected entitlement_inactive forbidden response, got %d: %s", response.Code, response.Body.String())
	}
}

func TestUpdateMemberRequiresAChange(t *testing.T) {
	response := serveAdminRequest(t, nil, http.MethodPut, "/projects/project/team/member", NewHandler(nil).UpdateMember, `{}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
}

func TestDeleteMemberMapsLastOwnerConflict(t *testing.T) {
	store := &testStore{deleteMemberError: ErrLastOwner}
	response := serveAdminRequest(t, store, http.MethodDelete, "/projects/project/team/member", NewHandler(store).DeleteMember, "")
	if response.Code != http.StatusConflict {
		t.Fatalf("expected %d, got %d: %s", http.StatusConflict, response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "last_owner") {
		t.Fatalf("expected last_owner response, got %s", response.Body.String())
	}
}

func TestCreatePlanGeneratesSafeProjectPlanCode(t *testing.T) {
	store := &testStore{}
	response := serveAdminRequest(t, store, http.MethodPost, "/projects/project/plans", NewHandler(store).CreatePlan, `{
		"name":"Prilagođeni plan","description":"Plan za poseban tim","priceCents":4900,
		"seatLimit":5,"monthlyPublicationLimit":100,"features":{"automations":true}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if store.createdPlanInput == nil {
		t.Fatal("expected repository call")
	}
	if !strings.HasPrefix(store.createdPlanInput.Code, "custom-prilagodeni-plan-") || !planCodePattern.MatchString(store.createdPlanInput.Code) {
		t.Fatalf("unsafe generated plan code %q", store.createdPlanInput.Code)
	}
	if store.createdPlanInput.Currency != "EUR" || store.createdPlanInput.BillingInterval != "month" {
		t.Fatalf("expected plan defaults, got %#v", store.createdPlanInput)
	}
}

func TestCreatePlanRejectsUnsafeRequestedCode(t *testing.T) {
	response := serveAdminRequest(t, nil, http.MethodPost, "/projects/project/plans", NewHandler(nil).CreatePlan, `{
		"code":"../../bad","name":"Custom plan","priceCents":0,"features":{}
	}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d", http.StatusUnprocessableEntity, response.Code)
	}
	if !strings.Contains(response.Body.String(), "invalid_plan_code") {
		t.Fatalf("expected invalid_plan_code response, got %s", response.Body.String())
	}
}

func TestCreatePlanRejectsInvalidFeatureSchema(t *testing.T) {
	tests := []struct {
		name     string
		features string
	}{
		{name: "boolean as string", features: `{"automations":"yes"}`},
		{name: "unknown key", features: `{"futureFlag":true}`},
		{name: "too many social channels", features: `{"socialChannels":9}`},
		{name: "fractional social channels", features: `{"socialChannels":2.5}`},
		{name: "invalid social channel label", features: `{"socialChannels":"three"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveAdminRequest(t, nil, http.MethodPost, "/projects/project/plans", NewHandler(nil).CreatePlan,
				`{"name":"Custom plan","priceCents":0,"features":`+test.features+`}`)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "invalid_plan_features") {
				t.Fatalf("expected invalid_plan_features, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCreatePlanAcceptsSupportedFeatureSchema(t *testing.T) {
	store := &testStore{}
	response := serveAdminRequest(t, store, http.MethodPost, "/projects/project/plans", NewHandler(store).CreatePlan, `{
		"name":"Custom plan","priceCents":0,
		"features":{"aiAgents":true,"analytics":false,"api":true,"auditLog":true,"automations":true,"prioritySupport":false,"whiteLabel":true,"socialChannels":8}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected supported feature schema, got %d: %s", response.Code, response.Body.String())
	}
	if value, ok := store.createdPlanInput.Features["socialChannels"].(int); !ok || value != 8 {
		t.Fatalf("expected normalized integer social channel limit, got %#v", store.createdPlanInput.Features["socialChannels"])
	}
}

func TestUpdateEntitlementNormalizesPlanCode(t *testing.T) {
	store := &testStore{}
	response := serveAdminRequest(t, store, http.MethodPut, "/projects/project/entitlement", NewHandler(store).UpdateEntitlement, `{"planCode":" GROWTH "}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if store.updatedPlanCode != "growth" {
		t.Fatalf("expected normalized plan code, got %q", store.updatedPlanCode)
	}
}

func serveAdminRequest(t *testing.T, _ Store, method, path string, handler gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, "/projects/:projectID/team", handler)
	router.Handle(method, "/projects/:projectID/team/:memberID", handler)
	router.Handle(method, "/projects/:projectID/plans", handler)
	router.Handle(method, "/projects/:projectID/entitlement", handler)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
