package assets

import "time"

// Netlify encodes binary function requests, leaving roughly 4.5 MB for the
// original file inside its 6 MB request limit. Keep a safety margin.
const MaxAssetSize int64 = 4 << 20

const (
	PurposeAssistantAttachment = "assistant_attachment"
	PurposeSocialMedia         = "social_media"
	PurposeContentMedia        = "content_media"
)

type Asset struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"projectId"`
	UploadedBy       *string   `json:"uploadedBy"`
	Purpose          string    `json:"purpose"`
	Filename         string    `json:"filename"`
	MIMEType         string    `json:"mimeType"`
	SizeBytes        int64     `json:"sizeBytes"`
	SHA256           string    `json:"sha256"`
	HasExtractedText bool      `json:"hasExtractedText"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Reference struct {
	ID               string `json:"id"`
	Purpose          string `json:"purpose"`
	Filename         string `json:"filename"`
	MIMEType         string `json:"mimeType"`
	SizeBytes        int64  `json:"sizeBytes"`
	SHA256           string `json:"sha256"`
	HasExtractedText bool   `json:"hasExtractedText"`
}

type ContextAsset struct {
	Reference
	ExtractedText *string
}

type Blob struct {
	Asset
	Data []byte
}

type UploadInput struct {
	Purpose       string
	Filename      string
	MIMEType      string
	Data          []byte
	SHA256        [32]byte
	ExtractedText *string
}

type UpdateInput struct {
	Purpose  string `json:"purpose"`
	Filename string `json:"filename"`
}

func ValidPurpose(value string) bool {
	switch value {
	case PurposeAssistantAttachment, PurposeSocialMedia, PurposeContentMedia:
		return true
	default:
		return false
	}
}
