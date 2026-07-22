package workspace

import (
	"testing"
	"time"
)

func TestCalculateAutomationNextRunSchedulingPrecedence(t *testing.T) {
	location, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)

	explicit, managed, err := calculateAutomationNextRun(
		"custom", "", "FREQ=DAILY;BYHOUR=7;BYMINUTE=30",
		map[string]any{"cadence": "off", "hour": float64(7), "minute": float64(30)},
		4, "weekly", now, location,
	)
	if err != nil || !managed || explicit == nil {
		t.Fatalf("explicit schedule result=%v managed=%v err=%v", explicit, managed, err)
	}
	if local := explicit.In(location); local.Hour() != 7 || local.Minute() != 30 {
		t.Fatalf("explicit next run = %s, want 07:30 project time", local)
	}

	configured, managed, err := calculateAutomationNextRun(
		"bot_event", "", "",
		map[string]any{"cadence": "weekly", "hour": float64(8), "minute": float64(15)},
		4, "monthly", now, location,
	)
	if err != nil || !managed || configured == nil {
		t.Fatalf("configured schedule result=%v managed=%v err=%v", configured, managed, err)
	}
	if local := configured.In(location); local.Weekday() != time.Friday || local.Hour() != 8 || local.Minute() != 15 {
		t.Fatalf("configured next run = %s, want Friday 08:15 project time", local)
	}

	disabled, managed, err := calculateAutomationNextRun(
		"bot_event", "", "", map[string]any{"cadence": "off"},
		4, "weekly", now, location,
	)
	if err != nil || !managed || disabled != nil {
		t.Fatalf("off cadence result=%v managed=%v err=%v, want managed nil", disabled, managed, err)
	}
}

func TestCalculateGapRunUsesConfiguredProjectClock(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	next, managed, err := calculateAutomationNextRun(
		"calendar_gap", "linkedin", "gap:5d",
		map[string]any{"hour": float64(11), "minute": float64(45)},
		4, "weekly", now, location,
	)
	if err != nil || !managed || next == nil {
		t.Fatalf("gap schedule result=%v managed=%v err=%v", next, managed, err)
	}
	if local := next.In(location); local.Hour() != 11 || local.Minute() != 45 {
		t.Fatalf("gap next run = %s, want 11:45 project time", local)
	}
}

func TestCalculateMonthlyCadenceUsesRemainingFirstDaySlot(t *testing.T) {
	location, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	now := time.Date(2026, time.August, 1, 8, 0, 0, 0, location)
	next, managed, err := calculateAutomationNextRun(
		"newsletter", "newsletter", "", map[string]any{"cadence": "monthly", "hour": float64(10)},
		4, "weekly", now, location,
	)
	if err != nil || !managed || next == nil {
		t.Fatalf("monthly result=%v managed=%v err=%v", next, managed, err)
	}
	local := next.In(location)
	if local.Year() != 2026 || local.Month() != time.August || local.Day() != 1 || local.Hour() != 10 {
		t.Fatalf("monthly next run = %s, want same-day August 1 at 10:00", local)
	}
}

func TestReanchorCadencePreservesBiweeklyPhase(t *testing.T) {
	oldLocation, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load old timezone: %v", err)
	}
	newLocation, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load new timezone: %v", err)
	}
	anchor := time.Date(2026, time.August, 7, 8, 15, 0, 0, oldLocation)
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	next := reanchorCadenceFromExisting(anchor, now, oldLocation, newLocation, "biweekly", 8, 15)
	if next == nil {
		t.Fatal("reanchor returned nil")
	}
	local := next.In(newLocation)
	if local.Year() != 2026 || local.Month() != time.August || local.Day() != 7 ||
		local.Weekday() != time.Friday || local.Hour() != 8 || local.Minute() != 15 {
		t.Fatalf("reanchored run = %s, want Friday August 7 at 08:15", local)
	}
}
