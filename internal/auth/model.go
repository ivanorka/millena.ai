package auth

import "time"

type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	DisplayName  string     `json:"displayName"`
	Status       string     `json:"status"`
	SystemRole   string     `json:"systemRole"`
	LastLoginAt  *time.Time `json:"lastLoginAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	PasswordHash string     `json:"-"`
}

type Entitlement struct {
	PlanCode                string         `json:"planCode"`
	Status                  string         `json:"status"`
	SeatLimit               *int           `json:"seatLimit"`
	MonthlyPublicationLimit *int           `json:"monthlyPublicationLimit"`
	StorageLimitBytes       *int64         `json:"storageLimitBytes"`
	Features                map[string]any `json:"features"`
	RenewsAt                *time.Time     `json:"renewsAt"`
}

type ProjectAccess struct {
	OrganizationID   string         `json:"organizationId"`
	OrganizationName string         `json:"organizationName"`
	OrganizationRole string         `json:"organizationRole"`
	ProjectID        string         `json:"projectId"`
	ProjectName      string         `json:"projectName"`
	ProjectSlug      string         `json:"projectSlug"`
	DefaultLocale    string         `json:"defaultLocale"`
	Role             string         `json:"role"`
	Permissions      map[string]any `json:"permissions"`
	Entitlement      Entitlement    `json:"entitlement"`
}

type SessionUser struct {
	User     User            `json:"user"`
	Projects []ProjectAccess `json:"projects"`
}

type RegisterInput struct {
	DisplayName      string `json:"displayName"`
	Email            string `json:"email"`
	Password         string `json:"password"`
	OrganizationName string `json:"organizationName"`
	PlanCode         string `json:"planCode"`
	ProjectSlug      string `json:"-"`
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type PasswordResetRequestInput struct {
	Email string `json:"email"`
}
type PasswordResetConfirmInput struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type AccountInput struct {
	DisplayName     string `json:"displayName"`
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type RegistrationPlan struct {
	Code                    string `json:"code"`
	Name                    string `json:"name"`
	Description             string `json:"description"`
	PriceCents              int    `json:"priceCents"`
	Currency                string `json:"currency"`
	MonthlyPublicationLimit *int   `json:"monthlyPublicationLimit"`
}
