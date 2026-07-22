package audience

import "time"

type List struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"projectId"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	IsDefault    bool      `json:"isDefault"`
	ContactCount int       `json:"contactCount"`
	ActiveCount  int       `json:"activeCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Contact struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"projectId"`
	ListID         *string        `json:"listId"`
	ListName       string         `json:"listName"`
	FirstName      string         `json:"firstName"`
	LastName       string         `json:"lastName"`
	Email          string         `json:"email"`
	Source         string         `json:"source"`
	Status         string         `json:"status"`
	Consent        bool           `json:"consent"`
	SubscribedAt   *time.Time     `json:"subscribedAt"`
	UnsubscribedAt *time.Time     `json:"unsubscribedAt"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type Stats struct {
	Total        int     `json:"total"`
	Active       int     `json:"active"`
	Pending      int     `json:"pending"`
	Unsubscribed int     `json:"unsubscribed"`
	Website      int     `json:"website"`
	ActiveRate   float64 `json:"activeRate"`
}

type ContactCollection struct {
	Items []Contact `json:"items"`
	Stats Stats     `json:"stats"`
}

type ContactInput struct {
	ListID    *string        `json:"listId"`
	FirstName string         `json:"firstName"`
	LastName  string         `json:"lastName"`
	Email     string         `json:"email"`
	Source    string         `json:"source"`
	Status    string         `json:"status"`
	Consent   bool           `json:"consent"`
	Metadata  map[string]any `json:"metadata"`
}

type ListInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   bool   `json:"isDefault"`
}

type ImportResult struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}
