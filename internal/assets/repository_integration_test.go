package assets_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/assets"
	"github.com/ivanorka/millena-ai/internal/assistant"
	"github.com/ivanorka/millena-ai/internal/content"
	"github.com/ivanorka/millena-ai/internal/social"
	"github.com/ivanorka/millena-ai/internal/workspace"
)

func TestAssetAssistantAndSocialLifecycleAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("ASSETS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ASSETS_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer pool.Close()

	projectID, userID := createAssetTestTenant(t, ctx, pool)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1::uuid`, userID)
	}()

	repository := assets.NewRepository(pool)
	profile, err := workspace.NewRepository(pool).SaveProfile(ctx, projectID, userID, workspace.ProfileInput{
		ProjectName: "Asset integration", CompanyName: "MPR integration",
		CompanyDescription: "Opis tvrtke spremljen u zasebno polje profila projekta.",
		PrimaryLanguage:    "hr", Timezone: "Europe/Zagreb", SocialPostsPerWeek: 4,
		NewsletterCadence: "weekly",
	})
	if err != nil || profile.CompanyDescription == "" {
		t.Fatalf("persist company description: profile=%+v err=%v", profile, err)
	}
	extracted := "Strateški kontekst: MPR treba isticati stručnost, povjerenje i mjerljive poslovne rezultate."
	textData := []byte(extracted)
	textDigest := sha256.Sum256(textData)
	textAsset, err := repository.Create(ctx, projectID, userID, assets.UploadInput{
		Purpose: assets.PurposeAssistantAttachment, Filename: "strategy.txt",
		MIMEType: "text/plain; charset=utf-8", Data: textData, SHA256: textDigest,
		ExtractedText: &extracted,
	})
	if err != nil {
		t.Fatalf("create assistant asset: %v", err)
	}
	if !textAsset.HasExtractedText || textAsset.SizeBytes != int64(len(textData)) {
		t.Fatalf("unexpected asset metadata: %+v", textAsset)
	}

	listed, err := repository.List(ctx, projectID, assets.PurposeAssistantAttachment)
	if err != nil || len(listed) != 1 || listed[0].ID != textAsset.ID {
		t.Fatalf("list assets: items=%+v err=%v", listed, err)
	}
	loaded, err := repository.Get(ctx, projectID, textAsset.ID)
	if err != nil || loaded.SHA256 != fmt.Sprintf("%x", textDigest) {
		t.Fatalf("get asset: asset=%+v err=%v", loaded, err)
	}
	blob, err := repository.Download(ctx, projectID, textAsset.ID, userID)
	if err != nil || string(blob.Data) != extracted {
		t.Fatalf("download asset: data=%q err=%v", blob.Data, err)
	}
	textAsset, err = repository.Update(ctx, projectID, textAsset.ID, userID, assets.UpdateInput{
		Purpose: assets.PurposeAssistantAttachment, Filename: "mpr-strategy.txt",
	})
	if err != nil || textAsset.Filename != "mpr-strategy.txt" {
		t.Fatalf("update asset: asset=%+v err=%v", textAsset, err)
	}
	if _, err := repository.Update(ctx, projectID, textAsset.ID, userID, assets.UpdateInput{
		Purpose: assets.PurposeSocialMedia, Filename: textAsset.Filename,
	}); !errors.Is(err, assets.ErrInvalidMediaPurpose) {
		t.Fatalf("expected social MIME validation, got %v", err)
	}

	assistantRepository := assistant.NewRepository(pool)
	thread, err := assistantRepository.CreateThread(ctx, projectID, userID, assistant.CreateThreadInput{
		Title: "Asset integration", Channel: "app",
	})
	if err != nil {
		t.Fatalf("create assistant thread: %v", err)
	}
	result, err := assistant.NewService(assistantRepository, nil).Send(
		ctx, projectID, thread.ID, userID, "Sažmi priloženi kontekst", []string{textAsset.ID},
	)
	if err != nil {
		t.Fatalf("send assistant message: %v", err)
	}
	if result.AssistantMessage.ActionType != "attachments.reviewed" ||
		len(result.UserMessage.Attachments) != 1 ||
		!strings.Contains(result.AssistantMessage.Body, "MPR treba isticati stručnost") {
		t.Fatalf("assistant did not use asset context: %+v", result)
	}
	messages, err := assistantRepository.Messages(ctx, projectID, thread.ID)
	if err != nil {
		t.Fatalf("reload assistant messages: %v", err)
	}
	var linkedAssistantAsset bool
	for _, message := range messages {
		if message.ID == result.UserMessage.ID && len(message.Attachments) == 1 && message.Attachments[0].ID == textAsset.ID {
			linkedAssistantAsset = true
		}
	}
	if !linkedAssistantAsset {
		t.Fatalf("assistant attachment link was not persisted: %+v", messages)
	}
	if _, err := repository.Update(ctx, projectID, textAsset.ID, userID, assets.UpdateInput{
		Purpose: assets.PurposeContentMedia, Filename: textAsset.Filename,
	}); !errors.Is(err, assets.ErrAssetInUse) {
		t.Fatalf("expected linked asset protection, got %v", err)
	}

	blogData := []byte("\x89PNG\r\n\x1a\ncontent-media-integration")
	blogDigest := sha256.Sum256(blogData)
	blogAsset, err := repository.Create(ctx, projectID, userID, assets.UploadInput{
		Purpose: assets.PurposeContentMedia, Filename: "article-cover.png",
		MIMEType: "image/png", Data: blogData, SHA256: blogDigest,
	})
	if err != nil {
		t.Fatalf("create content asset: %v", err)
	}
	blogItem, err := content.NewRepository(pool).Create(ctx, projectID, userID, content.SaveInput{
		Kind: "blog", Status: "draft", Title: "Article with cover", Body: "Article body",
		Channels: []string{}, Source: "manual", Metadata: map[string]any{
			"assetIds": []string{blogAsset.ID}, "coverAssetId": blogAsset.ID,
		},
	})
	if err != nil || blogItem.Metadata["coverAssetId"] != blogAsset.ID {
		t.Fatalf("persist content asset links: item=%+v err=%v", blogItem, err)
	}
	if _, err := repository.Update(ctx, projectID, blogAsset.ID, userID, assets.UpdateInput{
		Purpose: assets.PurposeSocialMedia, Filename: blogAsset.Filename,
	}); !errors.Is(err, assets.ErrAssetInUse) {
		t.Fatalf("expected content-linked asset purpose protection, got %v", err)
	}
	if err := repository.Delete(ctx, projectID, blogAsset.ID, userID); err != nil {
		t.Fatalf("delete content asset: %v", err)
	}
	cleanedBlogItem, err := content.NewRepository(pool).Get(ctx, projectID, blogItem.ID)
	if err != nil {
		t.Fatalf("reload content after asset deletion: %v", err)
	}
	if _, exists := cleanedBlogItem.Metadata["coverAssetId"]; exists {
		t.Fatalf("deleted cover remained in content metadata: %#v", cleanedBlogItem.Metadata)
	}
	assetIDs, ok := cleanedBlogItem.Metadata["assetIds"].([]any)
	if !ok || len(assetIDs) != 0 {
		t.Fatalf("deleted attachment remained in content metadata: %#v", cleanedBlogItem.Metadata)
	}
	var contentLinkCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM content_item_assets WHERE content_item_id = $1::uuid`, blogItem.ID).Scan(&contentLinkCount); err != nil {
		t.Fatalf("count content links after asset deletion: %v", err)
	}
	if contentLinkCount != 0 {
		t.Fatalf("deleted asset left %d normalized content links", contentLinkCount)
	}

	pngData := []byte("\x89PNG\r\n\x1a\nasset-integration")
	pngDigest := sha256.Sum256(pngData)
	mediaAsset, err := repository.Create(ctx, projectID, userID, assets.UploadInput{
		Purpose: assets.PurposeSocialMedia, Filename: "campaign.png",
		MIMEType: "image/png", Data: pngData, SHA256: pngDigest,
	})
	if err != nil {
		t.Fatalf("create social asset: %v", err)
	}
	socialRepository := social.NewRepository(pool)
	connection, err := socialRepository.UpsertConnection(ctx, projectID, social.ConnectInput{
		Provider: "linkedin", Mode: "sandbox", AccountHandle: "@asset-test", DisplayName: "Asset test",
	})
	if err != nil {
		t.Fatalf("create social connection: %v", err)
	}
	post, err := socialRepository.CreatePost(ctx, projectID, social.CreatePostInput{
		Body: "Objava s pravim lokalnim medijem.", ConnectionIDs: []string{connection.ID},
		AssetIDs: []string{mediaAsset.ID},
	})
	if err != nil || len(post.Assets) != 1 || post.Assets[0].ID != mediaAsset.ID {
		t.Fatalf("create post with asset: post=%+v err=%v", post, err)
	}
	posts, err := socialRepository.ListPosts(ctx, projectID)
	if err != nil || len(posts) != 1 || len(posts[0].Assets) != 1 || posts[0].Assets[0].ID != mediaAsset.ID {
		t.Fatalf("reload post assets: posts=%+v err=%v", posts, err)
	}

	otherProjectID := createAssetTestProject(t, ctx, pool, "other")
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1::uuid`, otherProjectID)
	}()
	if _, err := assets.ResolveContext(ctx, pool, otherProjectID, []string{textAsset.ID}); !errors.Is(err, assets.ErrInvalidReferences) {
		t.Fatalf("cross-tenant asset reference was accepted: %v", err)
	}

	if err := repository.Delete(ctx, projectID, textAsset.ID, userID); err != nil {
		t.Fatalf("delete assistant asset: %v", err)
	}
	messages, err = assistantRepository.Messages(ctx, projectID, thread.ID)
	if err != nil {
		t.Fatalf("reload messages after asset deletion: %v", err)
	}
	for _, message := range messages {
		if message.ID == result.UserMessage.ID && len(message.Attachments) != 0 {
			t.Fatalf("deleted asset link remained on message: %+v", message.Attachments)
		}
	}

	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET storage_limit_bytes = $2
		WHERE project_id = $1::uuid`, projectID, mediaAsset.SizeBytes); err != nil {
		t.Fatalf("set storage limit: %v", err)
	}
	extraData := []byte("x")
	extraDigest := sha256.Sum256(extraData)
	if _, err := repository.Create(ctx, projectID, userID, assets.UploadInput{
		Purpose: assets.PurposeContentMedia, Filename: "over-limit.txt",
		MIMEType: "text/plain", Data: extraData, SHA256: extraDigest,
	}); !errors.Is(err, assets.ErrStorageLimitReached) {
		t.Fatalf("expected storage limit error, got %v", err)
	}

	var assetAuditEvents int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events
		WHERE project_id = $1::uuid AND entity_type = 'project_asset'`, projectID).Scan(&assetAuditEvents); err != nil {
		t.Fatalf("count asset audit events: %v", err)
	}
	if assetAuditEvents < 5 {
		t.Fatalf("expected asset audit trail, got %d events", assetAuditEvents)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements SET status = 'past_due' WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("deactivate entitlement: %v", err)
	}
	if _, err := repository.Create(ctx, projectID, userID, assets.UploadInput{
		Purpose: assets.PurposeContentMedia, Filename: "inactive.txt",
		MIMEType: "text/plain", Data: extraData, SHA256: extraDigest,
	}); !errors.Is(err, assets.ErrEntitlementInactive) {
		t.Fatalf("inactive entitlement upload error = %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM project_entitlements WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("remove entitlement: %v", err)
	}
	if _, err := repository.Create(ctx, projectID, userID, assets.UploadInput{
		Purpose: assets.PurposeContentMedia, Filename: "missing.txt",
		MIMEType: "text/plain", Data: extraData, SHA256: extraDigest,
	}); !errors.Is(err, assets.ErrEntitlementInactive) {
		t.Fatalf("missing entitlement upload error = %v", err)
	}
}

func createAssetTestTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	suffix := time.Now().UTC().UnixNano()
	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Asset integration', 'active')
		RETURNING id::text`, fmt.Sprintf("asset-integration-%d@example.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createAssetTestProject(t, ctx, pool, fmt.Sprintf("primary-%d", suffix))
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, 'owner')`, projectID, userID); err != nil {
		t.Fatalf("create membership: %v", err)
	}
	return projectID, userID
}

func createAssetTestProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) string {
	t.Helper()
	var projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug)
		VALUES ('Asset integration', $1)
		RETURNING id::text`, fmt.Sprintf("asset-integration-%s-%d", suffix, time.Now().UTC().UnixNano())).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'unlimited', 'active', '{"aiAgents":true,"auditLog":true,"socialChannels":"all"}'::jsonb)`, projectID); err != nil {
		t.Fatalf("create entitlement: %v", err)
	}
	return projectID
}
