package actions

import "time"

type Event struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"projectId"`
	ActorID    *string        `json:"actorId"`
	Action     string         `json:"action"`
	EntityType string         `json:"entityType"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type RecordInput struct {
	Action   string         `json:"action"`
	Label    string         `json:"label"`
	Screen   string         `json:"screen"`
	Metadata map[string]any `json:"metadata"`
}
