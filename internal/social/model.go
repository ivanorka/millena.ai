package social

import (
	"time"

	"github.com/ivanorka/millena-ai/internal/assets"
)

var supportedProviders = map[string]struct{}{
	"linkedin":  {},
	"facebook":  {},
	"instagram": {},
	"youtube":   {},
	"x":         {},
	"reddit":    {},
	"pinterest": {},
	"threads":   {},
}

type Connection struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"projectId"`
	Provider      string         `json:"provider"`
	Mode          string         `json:"mode"`
	AccountHandle string         `json:"accountHandle"`
	DisplayName   string         `json:"displayName"`
	Status        string         `json:"status"`
	Metadata      map[string]any `json:"metadata"`
	LastCheckedAt *time.Time     `json:"lastCheckedAt"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type ConnectInput struct {
	Provider      string `json:"provider"`
	AccountHandle string `json:"accountHandle"`
	DisplayName   string `json:"displayName"`
	Mode          string `json:"mode"`
}

type Publication struct {
	ID                 string     `json:"id"`
	SocialPostID       string     `json:"socialPostId"`
	SocialConnectionID string     `json:"socialConnectionId"`
	Provider           string     `json:"provider"`
	Status             string     `json:"status"`
	ExternalReference  *string    `json:"externalReference"`
	PublishedAt        *time.Time `json:"publishedAt"`
	LastError          *string    `json:"lastError"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type Post struct {
	ID               string             `json:"id"`
	ProjectID        string             `json:"projectId"`
	ContentItemID    *string            `json:"contentItemId"`
	ContentVariantID *string            `json:"contentVariantId"`
	Body             string             `json:"body"`
	Status           string             `json:"status"`
	ScheduledFor     *time.Time         `json:"scheduledFor"`
	Publications     []Publication      `json:"publications"`
	Assets           []assets.Reference `json:"assets"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
}

type CreatePostInput struct {
	Body          string     `json:"body"`
	ConnectionIDs []string   `json:"connectionIds"`
	AssetIDs      []string   `json:"assetIds"`
	ScheduledFor  *time.Time `json:"scheduledFor"`
}
