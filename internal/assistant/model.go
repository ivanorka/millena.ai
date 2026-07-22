package assistant

import (
	"time"

	"github.com/ivanorka/millena-ai/internal/assets"
)

type Thread struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"projectId"`
	Title         string    `json:"title"`
	Channel       string    `json:"channel"`
	MessageCount  int       `json:"messageCount"`
	LastMessage   string    `json:"lastMessage"`
	LastMessageAt time.Time `json:"lastMessageAt"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Message struct {
	ID             string             `json:"id"`
	ThreadID       string             `json:"threadId"`
	ProjectID      string             `json:"projectId"`
	Role           string             `json:"role"`
	Body           string             `json:"body"`
	ActionType     string             `json:"actionType"`
	ActionEntityID *string            `json:"actionEntityId"`
	Metadata       map[string]any     `json:"metadata"`
	Attachments    []assets.Reference `json:"attachments"`
	CreatedBy      *string            `json:"createdBy"`
	CreatedAt      time.Time          `json:"createdAt"`
}

type CreateThreadInput struct {
	Title   string `json:"title"`
	Channel string `json:"channel"`
}

type SendInput struct {
	Body          string   `json:"body"`
	AttachmentIDs []string `json:"attachmentIds"`
}

type SendResult struct {
	UserMessage      Message `json:"userMessage"`
	AssistantMessage Message `json:"assistantMessage"`
	CreatedContentID *string `json:"createdContentId,omitempty"`
	AffectedRuleID   *string `json:"affectedRuleId,omitempty"`
}

type WorkspaceContext struct {
	ContentTotal   int
	Drafts         int
	InReview       int
	Scheduled      int
	CalendarNext14 int
	Contacts       int
	ActiveContacts int
	EnabledRules   int
}
