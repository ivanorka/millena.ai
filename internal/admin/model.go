package admin

import "time"

type TeamMember struct {
	UserID           string         `json:"userId"`
	Email            string         `json:"email"`
	DisplayName      string         `json:"displayName"`
	UserStatus       string         `json:"userStatus"`
	Role             string         `json:"role"`
	Permissions      map[string]any `json:"permissions"`
	MembershipStatus string         `json:"status"`
	CreatedAt        time.Time      `json:"createdAt"`
}

type CreateMemberInput struct {
	DisplayName  string `json:"displayName"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	TempPassword string `json:"tempPassword"`
}

type UpdateMemberInput struct {
	Role   *string `json:"role"`
	Status *string `json:"status"`
}

type Plan struct {
	Code                    string         `json:"code"`
	OwnerProjectID          *string        `json:"ownerProjectId"`
	Name                    string         `json:"name"`
	Description             string         `json:"description"`
	PriceCents              int            `json:"priceCents"`
	Currency                string         `json:"currency"`
	BillingInterval         string         `json:"billingInterval"`
	SeatLimit               *int           `json:"seatLimit"`
	MonthlyPublicationLimit *int           `json:"monthlyPublicationLimit"`
	StorageLimitBytes       *int64         `json:"storageLimitBytes"`
	Features                map[string]any `json:"features"`
	IsActive                bool           `json:"isActive"`
	IsSystem                bool           `json:"isSystem"`
	CreatedAt               time.Time      `json:"createdAt"`
	UpdatedAt               time.Time      `json:"updatedAt"`
}

type CreatePlanInput struct {
	Code                    string         `json:"code"`
	Name                    string         `json:"name"`
	Description             string         `json:"description"`
	PriceCents              int            `json:"priceCents"`
	Currency                string         `json:"currency"`
	BillingInterval         string         `json:"billingInterval"`
	SeatLimit               *int           `json:"seatLimit"`
	MonthlyPublicationLimit *int           `json:"monthlyPublicationLimit"`
	StorageLimitBytes       *int64         `json:"storageLimitBytes"`
	Features                map[string]any `json:"features"`
}

type Entitlement struct {
	ProjectID               string         `json:"projectId"`
	PlanCode                string         `json:"planCode"`
	PlanName                string         `json:"planName"`
	Status                  string         `json:"status"`
	SeatLimit               *int           `json:"seatLimit"`
	MonthlyPublicationLimit *int           `json:"monthlyPublicationLimit"`
	StorageLimitBytes       *int64         `json:"storageLimitBytes"`
	Features                map[string]any `json:"features"`
	RenewsAt                *time.Time     `json:"renewsAt"`
	UpdatedAt               time.Time      `json:"updatedAt"`
}

type UpdateEntitlementInput struct {
	PlanCode string `json:"planCode"`
}
