package projects

import (
	"encoding/json"
	"time"
)

type Project struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Slug          string         `json:"slug"`
	DefaultLocale string         `json:"defaultLocale"`
	Status        string         `json:"status"`
	Settings      map[string]any `json:"settings"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type CreateProjectInput struct {
	Name           string `json:"name" binding:"required,min=2,max=120"`
	Slug           string `json:"slug" binding:"required,min=2,max=80"`
	DefaultLocale  string `json:"defaultLocale" binding:"omitempty,oneof=hr en"`
	AdminProjectID string `json:"adminProjectId"`
}

type AppState struct {
	ProjectID string          `json:"projectId"`
	State     json.RawMessage `json:"state"`
	Revision  int64           `json:"revision"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type BootstrapResult struct {
	Project Project  `json:"project"`
	App     AppState `json:"app"`
}

type SaveAppStateInput struct {
	State json.RawMessage `json:"state" binding:"required"`
}
