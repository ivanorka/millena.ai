package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOperationalWorkspaceSeedDefaults(t *testing.T) {
	tenant := newTenantOperationalWorkspaceSeed("Nova Grupa")
	if tenant.companyName != "Nova Grupa" || tenant.setupCompleted {
		t.Fatalf("unexpected tenant seed: %#v", tenant)
	}
	if tenant.timezone != defaultProjectTimezone {
		t.Fatalf("tenant timezone = %q, want %q", tenant.timezone, defaultProjectTimezone)
	}
	if len(tenant.channels) != 1 || tenant.channels[0].provider != "newsletter" {
		t.Fatalf("new tenant should only receive a local newsletter connection: %#v", tenant.channels)
	}

	mpr := mprOperationalWorkspaceSeed()
	if !mpr.setupCompleted || mpr.websiteURL != "https://mpr.hr" {
		t.Fatalf("unexpected MPR seed: %#v", mpr)
	}
	wantProviders := []string{"whatsapp", "telegram", "website", "newsletter"}
	if len(mpr.channels) != len(wantProviders) {
		t.Fatalf("expected %d MPR channels, got %d", len(wantProviders), len(mpr.channels))
	}
	for index, provider := range wantProviders {
		if mpr.channels[index].provider != provider {
			t.Fatalf("channel %d: expected %q, got %q", index, provider, mpr.channels[index].provider)
		}
	}
}

func TestRegistrationPlansMapToCatalogPlans(t *testing.T) {
	for planCode, wantCatalogCode := range map[string]string{
		"starter": "starter", "optimum": "optimum", "enterprise": "unlimited",
	} {
		if !isRegistrationPlan(planCode) {
			t.Errorf("expected %q to be available during registration", planCode)
		}
		if got := registrationPlanCatalogCode(planCode); got != wantCatalogCode {
			t.Errorf("catalog code for %q = %q, want %q", planCode, got, wantCatalogCode)
		}
	}
	if isRegistrationPlan("unlimited") || isRegistrationPlan("custom") {
		t.Fatal("unsupported plans must not be selectable during registration")
	}
}

func TestOperationalSeedRunAnchorsUseProjectWallClock(t *testing.T) {
	location, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load Zagreb timezone: %v", err)
	}

	t.Run("Friday after ten advances weekly run", func(t *testing.T) {
		now := time.Date(2026, time.July, 24, 10, 1, 0, 0, location)
		gapRun, weeklyRun, err := operationalSeedRunAnchors(now, "Europe/Zagreb")
		if err != nil {
			t.Fatalf("calculate seed anchors: %v", err)
		}
		if got := gapRun.In(location); got.Weekday() != time.Saturday || got.Hour() != 10 || got.Minute() != 0 {
			t.Fatalf("gap run = %s, want Saturday 10:00 Zagreb", got)
		}
		if got := weeklyRun.In(location); got.Weekday() != time.Friday || got.Day() != 31 || got.Hour() != 10 || got.Minute() != 0 {
			t.Fatalf("weekly run = %s, want next Friday 10:00 Zagreb", got)
		}
	})

	t.Run("weekend schedules the following Friday", func(t *testing.T) {
		now := time.Date(2026, time.July, 25, 18, 0, 0, 0, location)
		_, weeklyRun, err := operationalSeedRunAnchors(now, "Europe/Zagreb")
		if err != nil {
			t.Fatalf("calculate seed anchors: %v", err)
		}
		if got := weeklyRun.In(location); got.Weekday() != time.Friday || got.Day() != 31 || got.Hour() != 10 {
			t.Fatalf("weekly run = %s, want following Friday 10:00 Zagreb", got)
		}
	})

	t.Run("DST boundary preserves ten o'clock", func(t *testing.T) {
		now := time.Date(2026, time.March, 28, 11, 0, 0, 0, location)
		gapRun, weeklyRun, err := operationalSeedRunAnchors(now, "Europe/Zagreb")
		if err != nil {
			t.Fatalf("calculate seed anchors: %v", err)
		}
		if got := gapRun.In(location); got.Hour() != 10 || got.Day() != 29 {
			t.Fatalf("DST gap run = %s, want March 29 at 10:00 Zagreb", got)
		}
		if got := weeklyRun.In(location); got.Hour() != 10 || got.Weekday() != time.Friday {
			t.Fatalf("DST weekly run = %s, want Friday at 10:00 Zagreb", got)
		}
	})
}

func TestOperationalSeedRunAnchorsRejectNonIANATimezone(t *testing.T) {
	if _, _, err := operationalSeedRunAnchors(time.Now(), "Local"); err == nil {
		t.Fatal("expected Local timezone to be rejected")
	}
}

func TestSeedOperationalWorkspaceUsesIdempotentStatements(t *testing.T) {
	executor := &recordingSeedExecutor{}
	err := seedOperationalWorkspace(
		context.Background(), executor, "00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002", newTenantOperationalWorkspaceSeed("Nova Grupa"),
	)
	if err != nil {
		t.Fatalf("seed operational workspace: %v", err)
	}
	if len(executor.queries) != 6 {
		t.Fatalf("expected 6 seed statements, got %d", len(executor.queries))
	}
	for index, query := range executor.queries {
		if !strings.Contains(query, "ON CONFLICT") && !strings.Contains(query, "NOT EXISTS") {
			t.Fatalf("statement %d is not idempotent: %s", index, query)
		}
	}

	for _, ruleKey := range []string{
		"master", "bot_event", "calendar_gap", "weekly_newsletter", "linkedin",
		"instagram", "facebook", "youtube", "x", "reddit", "pinterest",
		"threads", "telegram", "blog", "newsletter",
	} {
		if !strings.Contains(seedAutomationRulesSQL, "('"+ruleKey+"',") {
			t.Errorf("automation seed is missing %q", ruleKey)
		}
	}
}

func TestSeedOperationalWorkspaceStopsAtFirstFailure(t *testing.T) {
	wantErr := errors.New("database unavailable")
	executor := &recordingSeedExecutor{failAt: 2, err: wantErr}
	err := seedOperationalWorkspace(
		context.Background(), executor, "00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002", newTenantOperationalWorkspaceSeed("Nova Grupa"),
	)
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "automation rules") {
		t.Fatalf("expected wrapped automation error, got %v", err)
	}
	if len(executor.queries) != 2 {
		t.Fatalf("expected execution to stop after statement 2, got %d statements", len(executor.queries))
	}
}

type recordingSeedExecutor struct {
	queries []string
	failAt  int
	err     error
}

func (executor *recordingSeedExecutor) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	executor.queries = append(executor.queries, query)
	if executor.failAt == len(executor.queries) {
		return pgconn.CommandTag{}, executor.err
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
