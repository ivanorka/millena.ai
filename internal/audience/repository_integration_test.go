package audience

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAudienceListLifecycleAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("AUDIENCE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AUDIENCE_TEST_DATABASE_URL is not set")
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
		VALUES ($1, 'Audience integration', 'active')
		RETURNING id::text`, fmt.Sprintf("audience-%d@example.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, status)
		VALUES ('Audience integration', $1, 'active')
		RETURNING id::text`, fmt.Sprintf("audience-integration-%d", suffix)).Scan(&projectID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM projects WHERE id = $1::uuid`, projectID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM users WHERE id = $1::uuid`, userID)
	})

	repository := NewRepository(pool)
	primary, err := repository.CreateList(ctx, projectID, userID, ListInput{Name: "Primary", IsDefault: true})
	if err != nil || !primary.IsDefault {
		t.Fatalf("create primary list: item=%+v err=%v", primary, err)
	}
	secondary, err := repository.CreateList(ctx, projectID, userID, ListInput{Name: "Secondary"})
	if err != nil || secondary.IsDefault {
		t.Fatalf("create secondary list: item=%+v err=%v", secondary, err)
	}
	primary, err = repository.UpdateList(ctx, projectID, primary.ID, userID, ListInput{Name: "Primary", IsDefault: false})
	if err != nil || !primary.IsDefault {
		t.Fatalf("last default must remain default: item=%+v err=%v", primary, err)
	}
	secondary, err = repository.UpdateList(ctx, projectID, secondary.ID, userID, ListInput{Name: "Secondary", IsDefault: true})
	if err != nil || !secondary.IsDefault {
		t.Fatalf("promote secondary list: item=%+v err=%v", secondary, err)
	}
	primary, err = repository.GetList(ctx, projectID, primary.ID)
	if err != nil || primary.IsDefault {
		t.Fatalf("old primary must be demoted: item=%+v err=%v", primary, err)
	}
	if err := repository.DeleteList(ctx, projectID, primary.ID, userID); err != nil {
		t.Fatalf("delete empty non-default list: %v", err)
	}
	if err := repository.DeleteList(ctx, projectID, secondary.ID, userID); !errors.Is(err, ErrListNotDeletable) {
		t.Fatalf("expected default list protection, got %v", err)
	}
}
