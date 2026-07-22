package content

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/limits"
)

func TestManualVariantSurvivesMasterUpdateAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("CONTENT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CONTENT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := time.Now().UnixNano()
	var userID, projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Content integration', 'active')
		RETURNING id::text`, fmt.Sprintf("content-%d@example.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, status)
		VALUES ('Content integration', $1, 'en', 'active')
		RETURNING id::text`, fmt.Sprintf("content-integration-%d", suffix)).Scan(&projectID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'unlimited', 'active', '{"auditLog":true}'::jsonb)`, projectID); err != nil {
		t.Fatalf("create entitlement: %v", err)
	}

	repository := NewRepository(pool)
	item, err := repository.Create(ctx, projectID, userID, SaveInput{
		Kind: "social", Status: "draft", Title: "Master title", Body: "Master body",
		Channels: []string{"linkedin"}, Source: "manual", Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("create content: %v", err)
	}
	variants, err := repository.ListVariants(ctx, projectID, item.ID)
	if err != nil || len(variants) != 1 || variants[0].Locale != "en" || variants[0].Metadata["syncedFromItem"] != true {
		t.Fatalf("initial default variant: variants=%+v err=%v", variants, err)
	}
	manual, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "linkedin", Locale: "en", Title: "LinkedIn title", Body: "Manual LinkedIn copy",
		Status: "draft", Metadata: map[string]any{"syncedFromItem": false, "editor": "social-studio"},
	})
	if err != nil {
		t.Fatalf("save manual variant: %v", err)
	}
	item, err = repository.Update(ctx, projectID, item.ID, userID, SaveInput{
		Kind: "social", Status: "draft", Title: "Changed master", Body: "Changed master body",
		Channels: []string{"linkedin"}, Source: "manual", Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("update master content: %v", err)
	}
	variants, err = repository.ListVariants(ctx, projectID, item.ID)
	if err != nil || len(variants) != 1 || variants[0].ID != manual.ID || variants[0].Body != "Manual LinkedIn copy" {
		t.Fatalf("manual variant was overwritten: variants=%+v err=%v", variants, err)
	}

	firstSchedule := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	secondSchedule := firstSchedule.Add(3 * time.Hour)
	manual, err = repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "linkedin", Locale: "en", Title: "Scheduled LinkedIn", Body: "Scheduled copy",
		Status: "scheduled", ScheduledFor: &firstSchedule, Metadata: map[string]any{"syncedFromItem": false},
	})
	if err != nil {
		t.Fatalf("schedule first variant: %v", err)
	}
	facebook, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "facebook", Locale: "en", Title: "Scheduled Facebook", Body: "Scheduled copy",
		Status: "scheduled", ScheduledFor: &secondSchedule, Metadata: map[string]any{"syncedFromItem": false},
	})
	if err != nil {
		t.Fatalf("schedule second variant: %v", err)
	}
	assertMasterSchedule(t, ctx, pool, projectID, item.ID, "scheduled", &firstSchedule)

	if _, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "linkedin", Locale: "en", Title: manual.Title, Body: manual.Body,
		Status: "draft", Metadata: map[string]any{"syncedFromItem": false},
	}); err != nil {
		t.Fatalf("unschedule first variant: %v", err)
	}
	assertMasterSchedule(t, ctx, pool, projectID, item.ID, "scheduled", &secondSchedule)

	if err := repository.DeleteVariant(ctx, projectID, item.ID, facebook.ID, userID); err != nil {
		t.Fatalf("delete final scheduled variant: %v", err)
	}
	assertMasterSchedule(t, ctx, pool, projectID, item.ID, "draft", nil)

	item, err = repository.Update(ctx, projectID, item.ID, userID, SaveInput{
		Kind: "social", Status: "draft", Title: "Master with auto channel", Body: "Master body",
		Channels: []string{"linkedin", "facebook"}, Source: "manual", Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("add auto-synced channel: %v", err)
	}
	variants, err = repository.ListVariants(ctx, projectID, item.ID)
	if err != nil || len(variants) != 2 {
		t.Fatalf("expected manual and auto variants: variants=%+v err=%v", variants, err)
	}
	item, err = repository.Update(ctx, projectID, item.ID, userID, SaveInput{
		Kind: "social", Status: "draft", Title: "Master without channels", Body: "Master body",
		Channels: []string{}, Source: "manual", Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("remove auto-synced channel: %v", err)
	}
	variants, err = repository.ListVariants(ctx, projectID, item.ID)
	if err != nil || len(variants) != 1 || variants[0].ID != manual.ID || variants[0].Channel != "linkedin" {
		t.Fatalf("channel pruning removed a manual variant or retained an auto variant: variants=%+v err=%v", variants, err)
	}

	if _, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "linkedin", Locale: "en", Title: "Published LinkedIn", Body: "Published copy",
		Status: "published", Metadata: map[string]any{"syncedFromItem": false},
	}); err != nil {
		t.Fatalf("publish linkedin variant: %v", err)
	}
	if _, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "facebook", Locale: "en", Title: "Facebook draft", Body: "Draft copy",
		Status: "draft", Metadata: map[string]any{"syncedFromItem": false},
	}); err != nil {
		t.Fatalf("create facebook draft: %v", err)
	}
	assertMasterSchedule(t, ctx, pool, projectID, item.ID, "draft", nil)

	if _, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "facebook", Locale: "en", Title: "Facebook review", Body: "Review copy",
		Status: "in_review", Metadata: map[string]any{"syncedFromItem": false},
	}); err != nil {
		t.Fatalf("move facebook to review: %v", err)
	}
	assertMasterSchedule(t, ctx, pool, projectID, item.ID, "in_review", nil)

	if _, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "facebook", Locale: "en", Title: "Facebook failed", Body: "Failed copy",
		Status: "failed", Metadata: map[string]any{"syncedFromItem": false},
	}); err != nil {
		t.Fatalf("fail facebook variant: %v", err)
	}
	assertMasterSchedule(t, ctx, pool, projectID, item.ID, "failed", nil)

	thirdSchedule := secondSchedule.Add(2 * time.Hour)
	if _, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "instagram", Locale: "en", Title: "Instagram scheduled", Body: "Scheduled copy",
		Status: "scheduled", ScheduledFor: &thirdSchedule, Metadata: map[string]any{"syncedFromItem": false},
	}); err != nil {
		t.Fatalf("schedule instagram variant: %v", err)
	}
	assertMasterSchedule(t, ctx, pool, projectID, item.ID, "scheduled", &thirdSchedule)
	if _, err := repository.SaveVariant(ctx, projectID, item.ID, userID, VariantInput{
		Channel: "instagram", Locale: "en", Title: "Instagram draft", Body: "Draft copy",
		Status: "draft", Metadata: map[string]any{"syncedFromItem": false},
	}); err != nil {
		t.Fatalf("unschedule instagram variant: %v", err)
	}
	assertMasterSchedule(t, ctx, pool, projectID, item.ID, "failed", nil)

	var newsletterItemID, newsletterVariantID string
	newsletterSchedule := thirdSchedule.Add(2 * time.Hour)
	if err := pool.QueryRow(ctx, `
		INSERT INTO content_items (
			project_id, author_id, kind, status, title, body, channels, scheduled_for
		)
		VALUES ($1::uuid, $2::uuid, 'newsletter', 'scheduled', 'Locale-owned newsletter',
		        'Newsletter body', ARRAY['newsletter'], $3)
		RETURNING id::text`, projectID, userID, newsletterSchedule).Scan(&newsletterItemID); err != nil {
		t.Fatalf("create newsletter conflict item: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO content_variants (
			content_item_id, channel, locale, title, body, status, scheduled_for
		)
		VALUES ($1::uuid, 'newsletter', 'hr', 'Hrvatski newsletter',
		        'Newsletter body', 'scheduled', $2)
		RETURNING id::text`, newsletterItemID, newsletterSchedule).Scan(&newsletterVariantID); err != nil {
		t.Fatalf("create newsletter conflict variant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO newsletter_deliveries (
			project_id, content_item_id, content_variant_id, mode, status, subject, scheduled_for
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'sandbox', 'scheduled',
		        'Hrvatski newsletter', $4)`, projectID, newsletterItemID,
		newsletterVariantID, newsletterSchedule); err != nil {
		t.Fatalf("create newsletter conflict delivery: %v", err)
	}
	_, err = repository.SaveVariant(ctx, projectID, newsletterItemID, userID, VariantInput{
		Channel: "newsletter", Locale: "en", Title: "English newsletter", Body: "English body",
		Status: "scheduled", ScheduledFor: &newsletterSchedule,
		Metadata: map[string]any{"syncedFromItem": false},
	})
	if !errors.Is(err, ErrNewsletterDeliveryVariantConflict) {
		t.Fatalf("alternate newsletter variant schedule error = %v", err)
	}
	var alternateVariants int
	var linkedVariantID string
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM content_variants
		        WHERE content_item_id = $1::uuid AND channel = 'newsletter' AND locale = 'en'),
		       content_variant_id::text
		FROM newsletter_deliveries
		WHERE content_item_id = $1::uuid AND status = 'scheduled'`, newsletterItemID).Scan(
		&alternateVariants, &linkedVariantID,
	); err != nil || alternateVariants != 0 || linkedVariantID != newsletterVariantID {
		t.Fatalf("newsletter conflict rollback variants=%d linked=%q expected=%q err=%v",
			alternateVariants, linkedVariantID, newsletterVariantID, err)
	}
}

func TestMasterAndDefaultVariantsAreAtomicAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("CONTENT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CONTENT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID, userID := createContentAssetTestTenant(t, ctx, pool, "atomic")
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(ctx, `
		UPDATE project_entitlements
		SET monthly_publication_limit = 1
		WHERE project_id = $1::uuid`, projectID); err != nil {
		t.Fatalf("set publication limit: %v", err)
	}

	repository := NewRepository(pool)
	item, err := repository.Create(ctx, projectID, userID, SaveInput{
		Kind: "social", Status: "draft", Title: "Atomic draft", Body: "Original body",
		Channels: []string{"linkedin", "facebook"}, Source: "manual", Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("create atomic draft: %v", err)
	}
	scheduledFor := time.Now().UTC().Add(4 * time.Hour).Truncate(time.Second)
	_, err = repository.Update(ctx, projectID, item.ID, userID, SaveInput{
		Kind: "social", Status: "scheduled", Title: "Must roll back", Body: "Changed body",
		Channels: []string{"linkedin", "facebook"}, ScheduledFor: &scheduledFor,
		Source: "manual", Metadata: map[string]any{},
	})
	if !errors.Is(err, limits.ErrPublicationLimitReached) {
		t.Fatalf("expected publication limit rollback, got %v", err)
	}
	loaded, err := repository.Get(ctx, projectID, item.ID)
	if err != nil || loaded.Title != "Atomic draft" || loaded.Status != "draft" || loaded.ScheduledFor != nil {
		t.Fatalf("master update was partially committed: item=%+v err=%v", loaded, err)
	}
	variants, err := repository.ListVariants(ctx, projectID, item.ID)
	if err != nil || len(variants) != 2 {
		t.Fatalf("load variants after rollback: variants=%+v err=%v", variants, err)
	}
	for _, variant := range variants {
		if variant.Status != "draft" || variant.ScheduledFor != nil {
			t.Fatalf("variant was partially scheduled: %+v", variant)
		}
	}
	assertPublicationConsumptionCount(t, ctx, pool, projectID, 0)

	_, err = repository.Create(ctx, projectID, userID, SaveInput{
		Kind: "social", Status: "scheduled", Title: "Atomic create rollback", Body: "Body",
		Channels: []string{"linkedin", "facebook"}, ScheduledFor: &scheduledFor,
		Source: "manual", Metadata: map[string]any{},
	})
	if !errors.Is(err, limits.ErrPublicationLimitReached) {
		t.Fatalf("expected atomic create publication limit error, got %v", err)
	}
	var partialItemCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM content_items
		WHERE project_id = $1::uuid AND title = 'Atomic create rollback'`, projectID).Scan(&partialItemCount); err != nil {
		t.Fatalf("count partial content items: %v", err)
	}
	if partialItemCount != 0 {
		t.Fatalf("failed create left %d partial content items", partialItemCount)
	}
	assertPublicationConsumptionCount(t, ctx, pool, projectID, 0)
}

func TestContentAssetLinksAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("CONTENT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CONTENT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID, userID := createContentAssetTestTenant(t, ctx, pool, "primary")
	otherProjectID, otherUserID := createContentAssetTestTenant(t, ctx, pool, "other")
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM projects WHERE id = ANY($1::uuid[])`, []string{projectID, otherProjectID})
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = ANY($1::uuid[])`, []string{userID, otherUserID})
	})

	firstAssetID := createContentAssetTestFile(t, ctx, pool, projectID, userID, "first.png", "content_media")
	secondAssetID := createContentAssetTestFile(t, ctx, pool, projectID, userID, "second.png", "content_media")
	wrongPurposeID := createContentAssetTestFile(t, ctx, pool, projectID, userID, "notes.txt", "assistant_attachment")
	crossTenantID := createContentAssetTestFile(t, ctx, pool, otherProjectID, otherUserID, "other.png", "content_media")

	repository := NewRepository(pool)
	item, err := repository.Create(ctx, projectID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Asset-backed article", Body: "Initial body",
		Channels: []string{}, Source: "manual", Metadata: map[string]any{
			"assetIds":       []string{firstAssetID, secondAssetID, firstAssetID},
			"coverAssetId":   firstAssetID,
			"seoDescription": "Metadata compatibility is preserved.",
		},
	})
	if err != nil {
		t.Fatalf("create content with assets: %v", err)
	}
	if got := contentAssetMetadataIDs(t, item.Metadata, "assetIds"); len(got) != 2 || got[0] != firstAssetID || got[1] != secondAssetID {
		t.Fatalf("assetIds were not normalized in metadata: %#v", item.Metadata)
	}
	if item.Metadata["coverAssetId"] != firstAssetID || item.Metadata["seoDescription"] == nil {
		t.Fatalf("cover/custom metadata was not preserved: %#v", item.Metadata)
	}
	assertContentAssetLinks(t, ctx, pool, item.ID, map[string][]string{
		"attachment": {firstAssetID, secondAssetID},
		"cover":      {firstAssetID},
	})

	if _, err := repository.Create(ctx, projectID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Wrong asset", Body: "Body",
		Channels: []string{}, Source: "manual", Metadata: map[string]any{"assetIds": []string{wrongPurposeID}},
	}); !errors.Is(err, ErrInvalidAssetReferences) {
		t.Fatalf("create accepted wrong-purpose asset: %v", err)
	}

	invalidUpdates := []struct {
		name     string
		metadata map[string]any
	}{
		{name: "cross tenant", metadata: map[string]any{"assetIds": []string{crossTenantID}}},
		{name: "dangling", metadata: map[string]any{"coverAssetId": "00000000-0000-4000-8000-000000000001"}},
		{name: "wrong purpose", metadata: map[string]any{"assetIds": []string{wrongPurposeID}}},
		{name: "malformed list", metadata: map[string]any{"assetIds": []any{firstAssetID, 42}}},
	}
	for _, testCase := range invalidUpdates {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := repository.Update(ctx, projectID, item.ID, userID, SaveInput{
				Kind: "blog", Status: "draft", Title: "Must roll back", Body: "Changed body",
				Channels: []string{}, Source: "manual", Metadata: testCase.metadata,
			})
			if !errors.Is(err, ErrInvalidAssetReferences) {
				t.Fatalf("expected invalid asset reference, got %v", err)
			}
			loaded, loadErr := repository.Get(ctx, projectID, item.ID)
			if loadErr != nil || loaded.Title != "Asset-backed article" || loaded.Body != "Initial body" {
				t.Fatalf("invalid update was not rolled back: item=%+v err=%v", loaded, loadErr)
			}
			assertContentAssetLinks(t, ctx, pool, item.ID, map[string][]string{
				"attachment": {firstAssetID, secondAssetID},
				"cover":      {firstAssetID},
			})
		})
	}

	item, err = repository.Update(ctx, projectID, item.ID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Updated asset article", Body: "Updated body",
		Channels: []string{}, Source: "manual", Metadata: map[string]any{
			"assetIds": []string{secondAssetID}, "coverAssetId": secondAssetID,
		},
	})
	if err != nil {
		t.Fatalf("replace linked assets: %v", err)
	}
	assertContentAssetLinks(t, ctx, pool, item.ID, map[string][]string{
		"attachment": {secondAssetID},
		"cover":      {secondAssetID},
	})

	item, err = repository.Update(ctx, projectID, item.ID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Article without assets", Body: "Updated body",
		Channels: []string{}, Source: "manual", Metadata: map[string]any{
			"assetIds": []string{}, "coverAssetId": nil,
		},
	})
	if err != nil {
		t.Fatalf("remove linked assets: %v", err)
	}
	assertContentAssetLinks(t, ctx, pool, item.ID, map[string][]string{})
	if got := contentAssetMetadataIDs(t, item.Metadata, "assetIds"); len(got) != 0 {
		t.Fatalf("removed assetIds remained in metadata: %#v", item.Metadata)
	}

	item, err = repository.Update(ctx, projectID, item.ID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Article before deletion", Body: "Updated body",
		Channels: []string{}, Source: "manual", Metadata: map[string]any{"assetIds": []string{firstAssetID}},
	})
	if err != nil {
		t.Fatalf("restore link before deletion: %v", err)
	}
	if err := repository.Delete(ctx, projectID, item.ID); err != nil {
		t.Fatalf("delete content item: %v", err)
	}
	assertContentAssetLinks(t, ctx, pool, item.ID, map[string][]string{})
}

func TestBlogNewsletterRelationIsAtomicAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("CONTENT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CONTENT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID, userID := createContentAssetTestTenant(t, ctx, pool, "blog-newsletter")
	otherProjectID, otherUserID := createContentAssetTestTenant(t, ctx, pool, "blog-newsletter-other")
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM projects WHERE id = ANY($1::uuid[])`, []string{projectID, otherProjectID})
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = ANY($1::uuid[])`, []string{userID, otherUserID})
	})

	repository := NewRepository(pool)
	firstCampaign, err := repository.Create(ctx, projectID, userID, SaveInput{
		Kind: "newsletter", Status: "draft", Title: "First campaign", Body: "First body",
		Channels: []string{"newsletter"}, Source: "manual", Metadata: map[string]any{"blocks": []string{}},
	})
	if err != nil {
		t.Fatalf("create first campaign: %v", err)
	}
	secondCampaign, err := repository.Create(ctx, projectID, userID, SaveInput{
		Kind: "newsletter", Status: "draft", Title: "Second campaign", Body: "Second body",
		Channels: []string{"newsletter"}, Source: "manual", Metadata: map[string]any{"blocks": []string{}},
	})
	if err != nil {
		t.Fatalf("create second campaign: %v", err)
	}
	foreignCampaign, err := repository.Create(ctx, otherProjectID, otherUserID, SaveInput{
		Kind: "newsletter", Status: "draft", Title: "Foreign campaign", Body: "Foreign body",
		Channels: []string{"newsletter"}, Source: "manual", Metadata: map[string]any{"blocks": []string{}},
	})
	if err != nil {
		t.Fatalf("create foreign campaign: %v", err)
	}

	blog, err := repository.Create(ctx, projectID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Linked article", Body: "Article body",
		Channels: []string{"newsletter"}, Source: "manual", Metadata: map[string]any{
			"addNewsletter": true, "newsletterTargetId": firstCampaign.ID, "customKey": "preserved",
		},
	})
	if err != nil {
		t.Fatalf("create linked blog: %v", err)
	}
	assertNewsletterHasBlock(t, ctx, pool, firstCampaign.ID, blog.ID, true)

	blog, err = repository.Update(ctx, projectID, blog.ID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Relinked article", Body: "Article body",
		Channels: []string{"newsletter"}, Source: "manual", Metadata: map[string]any{
			"addNewsletter": true, "newsletterTargetId": secondCampaign.ID, "customKey": "preserved",
		},
	})
	if err != nil {
		t.Fatalf("move blog to second campaign: %v", err)
	}
	assertNewsletterHasBlock(t, ctx, pool, firstCampaign.ID, blog.ID, false)
	assertNewsletterHasBlock(t, ctx, pool, secondCampaign.ID, blog.ID, true)

	_, err = repository.Update(ctx, projectID, blog.ID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Must roll back", Body: "Changed body",
		Channels: []string{"newsletter"}, Source: "manual", Metadata: map[string]any{
			"addNewsletter": true, "newsletterTargetId": foreignCampaign.ID,
		},
	})
	if !errors.Is(err, ErrInvalidNewsletterTarget) {
		t.Fatalf("cross-project campaign error = %v", err)
	}
	loaded, loadErr := repository.Get(ctx, projectID, blog.ID)
	if loadErr != nil || loaded.Title != "Relinked article" || loaded.Metadata["newsletterTargetId"] != secondCampaign.ID {
		t.Fatalf("failed relink partially updated blog: item=%+v err=%v", loaded, loadErr)
	}
	assertNewsletterHasBlock(t, ctx, pool, secondCampaign.ID, blog.ID, true)

	blog, err = repository.Update(ctx, projectID, blog.ID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Unlinked article", Body: "Article body",
		Channels: []string{}, Source: "manual", Metadata: map[string]any{
			"addNewsletter": false, "newsletterTargetId": nil, "customKey": "preserved",
		},
	})
	if err != nil {
		t.Fatalf("unlink blog: %v", err)
	}
	assertNewsletterHasBlock(t, ctx, pool, secondCampaign.ID, blog.ID, false)

	blog, err = repository.Update(ctx, projectID, blog.ID, userID, SaveInput{
		Kind: "blog", Status: "draft", Title: "Linked before campaign delete", Body: "Article body",
		Channels: []string{"newsletter"}, Source: "manual", Metadata: map[string]any{
			"addNewsletter": true, "newsletterTargetId": secondCampaign.ID, "customKey": "preserved",
		},
	})
	if err != nil {
		t.Fatalf("relink before campaign deletion: %v", err)
	}
	if err := repository.Delete(ctx, projectID, secondCampaign.ID); err != nil {
		t.Fatalf("delete linked newsletter: %v", err)
	}
	loaded, err = repository.Get(ctx, projectID, blog.ID)
	if err != nil || loaded.Metadata["addNewsletter"] != false || loaded.Metadata["newsletterTargetId"] != nil || len(loaded.Channels) != 0 {
		t.Fatalf("deleting newsletter did not clear blog relation: item=%+v err=%v", loaded, err)
	}
	if loaded.Metadata["customKey"] != "preserved" {
		t.Fatalf("unrelated blog metadata was removed: %#v", loaded.Metadata)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE content_items
		SET metadata = jsonb_set(metadata, '{blocks}', jsonb_build_array($2::text), true)
		WHERE id = $1::uuid`, firstCampaign.ID, blog.ID); err != nil {
		t.Fatalf("create manual block selection: %v", err)
	}
	if err := repository.Delete(ctx, projectID, blog.ID); err != nil {
		t.Fatalf("delete blog: %v", err)
	}
	assertNewsletterHasBlock(t, ctx, pool, firstCampaign.ID, blog.ID, false)
}

func assertNewsletterHasBlock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, newsletterID, itemID string, want bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, `
		SELECT CASE WHEN jsonb_typeof(metadata->'blocks') = 'array'
		            THEN metadata->'blocks' @> jsonb_build_array($2::text)
		            ELSE false END
		FROM content_items
		WHERE id = $1::uuid AND kind = 'newsletter'`, newsletterID, itemID).Scan(&got); err != nil {
		t.Fatalf("read newsletter block relation: %v", err)
	}
	if got != want {
		t.Fatalf("newsletter %s contains block %s = %t, want %t", newsletterID, itemID, got, want)
	}
}

func createContentAssetTestTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) (string, string) {
	t.Helper()
	suffix := time.Now().UnixNano()
	var userID, projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, 'Content asset integration', 'active')
		RETURNING id::text`, fmt.Sprintf("content-assets-%s-%d@example.test", label, suffix)).Scan(&userID); err != nil {
		t.Fatalf("create content asset user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, status)
		VALUES ('Content asset integration', $1, 'active')
		RETURNING id::text`, fmt.Sprintf("content-assets-%s-%d", label, suffix)).Scan(&projectID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create content asset project: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, 'owner')`, projectID, userID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create content asset membership: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'unlimited', 'active', '{"auditLog":true}'::jsonb)`, projectID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create content asset tenant state: %v", err)
	}
	return projectID, userID
}

func createContentAssetTestFile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID, userID, filename, purpose string) string {
	t.Helper()
	data := []byte("content-asset:" + filename)
	digest := sha256.Sum256(data)
	var assetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_assets (
			project_id, uploaded_by, purpose, filename, mime_type,
			size_bytes, sha256, data
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)
		RETURNING id::text`, projectID, userID, purpose, filename,
		"image/png", len(data), digest[:], data).Scan(&assetID); err != nil {
		t.Fatalf("create %s asset: %v", purpose, err)
	}
	return assetID
}

func contentAssetMetadataIDs(t *testing.T, metadata map[string]any, key string) []string {
	t.Helper()
	switch values := metadata[key].(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			id, ok := value.(string)
			if !ok {
				t.Fatalf("metadata %s contains non-string value: %#v", key, metadata[key])
			}
			result = append(result, id)
		}
		return result
	default:
		t.Fatalf("metadata %s is not an array: %#v", key, metadata[key])
		return nil
	}
}

func assertContentAssetLinks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string, expected map[string][]string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT use_type, asset_id::text
		FROM content_item_assets
		WHERE content_item_id = $1::uuid
		ORDER BY use_type, position`, itemID)
	if err != nil {
		t.Fatalf("load content asset links: %v", err)
	}
	defer rows.Close()
	actual := make(map[string][]string)
	for rows.Next() {
		var useType, assetID string
		if err := rows.Scan(&useType, &assetID); err != nil {
			t.Fatalf("scan content asset link: %v", err)
		}
		actual[useType] = append(actual[useType], assetID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate content asset links: %v", err)
	}
	if fmt.Sprint(actual) != fmt.Sprint(expected) {
		t.Fatalf("content asset links mismatch: got %#v want %#v", actual, expected)
	}
}

func assertMasterSchedule(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	projectID, itemID, expectedStatus string,
	expectedSchedule *time.Time,
) {
	t.Helper()
	var status string
	var scheduledFor *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT status, scheduled_for
		FROM content_items
		WHERE project_id = $1::uuid AND id = $2::uuid`, projectID, itemID).Scan(&status, &scheduledFor); err != nil {
		t.Fatalf("load master state: %v", err)
	}
	if status != expectedStatus {
		t.Fatalf("master status = %q, expected %q", status, expectedStatus)
	}
	if expectedSchedule == nil {
		if scheduledFor != nil {
			t.Fatalf("master scheduledFor = %v, expected nil", scheduledFor)
		}
		return
	}
	if scheduledFor == nil || !scheduledFor.Equal(*expectedSchedule) {
		t.Fatalf("master scheduledFor = %v, expected %v", scheduledFor, expectedSchedule)
	}
}

func assertPublicationConsumptionCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID string, expected int) {
	t.Helper()
	var actual int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM publication_consumptions
		WHERE project_id = $1::uuid`, projectID).Scan(&actual); err != nil {
		t.Fatalf("count publication consumptions: %v", err)
	}
	if actual != expected {
		t.Fatalf("publication consumptions = %d, expected %d", actual, expected)
	}
}
