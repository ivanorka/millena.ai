package organizations

import "time"

type Member struct {
	UserID               string    `json:"userId"`
	Email                string    `json:"email"`
	DisplayName          string    `json:"displayName"`
	UserStatus           string    `json:"userStatus"`
	Role                 string    `json:"role"`
	Status               string    `json:"status"`
	ProjectCount         int       `json:"projectCount"`
	CurrentProjectRole   *string   `json:"currentProjectRole"`
	CurrentProjectStatus *string   `json:"currentProjectStatus"`
	CreatedAt            time.Time `json:"createdAt"`
}

type Detail struct {
	ID                      string   `json:"id"`
	Name                    string   `json:"name"`
	Slug                    string   `json:"slug"`
	Status                  string   `json:"status"`
	CurrentUserRole         string   `json:"currentUserRole"`
	PlanCode                string   `json:"planCode"`
	PlanName                string   `json:"planName"`
	MonthlyPublicationLimit *int     `json:"monthlyPublicationLimit"`
	Members                 []Member `json:"members"`
}

type CreateMemberInput struct {
	DisplayName        string `json:"displayName"`
	Email              string `json:"email"`
	TempPassword       string `json:"tempPassword"`
	Role               string `json:"role"`
	ProjectRole        string `json:"projectRole"`
	GrantProjectAccess bool   `json:"grantProjectAccess"`
}

type UpdateMemberInput struct {
	Role   *string `json:"role"`
	Status *string `json:"status"`
}
