package calendar

import "time"

var supportedChannels = map[string]struct{}{
	"linkedin": {}, "facebook": {}, "instagram": {}, "blog": {}, "newsletter": {},
	"youtube": {}, "x": {}, "reddit": {}, "pinterest": {}, "threads": {},
}

var supportedStatuses = map[string]struct{}{
	"suggestion": {}, "draft": {}, "in_review": {}, "scheduled": {}, "published": {}, "failed": {},
}

type Item struct {
	ID               string         `json:"id"`
	ProjectID        string         `json:"projectId"`
	CreatedBy        *string        `json:"createdBy"`
	Title            string         `json:"title"`
	Summary          string         `json:"summary"`
	Channel          string         `json:"channel"`
	Status           string         `json:"status"`
	ScheduledFor     time.Time      `json:"scheduledFor"`
	Metadata         map[string]any `json:"metadata"`
	ContentItemID    *string        `json:"contentItemId"`
	ContentVariantID *string        `json:"contentVariantId"`
	PublicationJobID *string        `json:"publicationJobId"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

type SaveInput struct {
	Title        string         `json:"title"`
	Summary      string         `json:"summary"`
	Channel      string         `json:"channel"`
	Status       string         `json:"status"`
	ScheduledFor time.Time      `json:"scheduledFor"`
	Metadata     map[string]any `json:"metadata"`
}
