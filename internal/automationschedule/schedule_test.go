package automationschedule

import (
	"testing"
	"time"
)

func TestParseAcceptsOnlyDocumentedSubset(t *testing.T) {
	accepted := []string{
		"gap:1d",
		"gap:365d",
		"FREQ=DAILY",
		"FREQ=DAILY;BYHOUR=7;BYMINUTE=30",
		"FREQ=WEEKLY;BYDAY=FR;BYHOUR=10",
		"FREQ=MONTHLY;BYMONTHDAY=22;BYHOUR=9;BYMINUTE=5",
	}
	for _, rule := range accepted {
		t.Run(rule, func(t *testing.T) {
			if _, err := Parse(rule); err != nil {
				t.Fatalf("Parse(%q): %v", rule, err)
			}
		})
	}
}

func TestParseRejectsUnsupportedOrAmbiguousSemantics(t *testing.T) {
	rejected := []string{
		"FREQ=WEEKLY;BYDAY=MO,FR",
		"FREQ=WEEKLY;BYDAY=FR;INTERVAL=2",
		"FREQ=DAILY;COUNT=3",
		"FREQ=DAILY;UNTIL=20261231T000000Z",
		"FREQ=DAILY;FREQ=WEEKLY;BYDAY=FR",
		"FREQ=DAILY;BYDAY=FR",
		"FREQ=WEEKLY",
		"FREQ=MONTHLY;BYMONTHDAY=31",
		"FREQ=MONTHLY;BYMONTHDAY=10;BYDAY=MO",
		"gap:0d",
		"gap:5",
	}
	for _, rule := range rejected {
		t.Run(rule, func(t *testing.T) {
			if _, err := Parse(rule); err == nil {
				t.Fatalf("Parse(%q) unexpectedly succeeded", rule)
			}
		})
	}
}

func TestNextUsesProjectWallClock(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	spec, err := Parse("FREQ=WEEKLY;BYDAY=FR;BYHOUR=7;BYMINUTE=30")
	if err != nil {
		t.Fatalf("parse schedule: %v", err)
	}
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	next := spec.Next(now, location).In(location)
	if next.Weekday() != time.Friday || next.Hour() != 7 || next.Minute() != 30 {
		t.Fatalf("next local run = %s, want Friday 07:30", next)
	}
}

func TestNextDoesNotCarrySpringGapNormalizationIntoLaterWeek(t *testing.T) {
	location, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	spec, err := Parse("FREQ=WEEKLY;BYDAY=SU;BYHOUR=2;BYMINUTE=30")
	if err != nil {
		t.Fatalf("parse schedule: %v", err)
	}
	now := time.Date(2026, time.March, 29, 4, 0, 0, 0, location)
	next := spec.Next(now, location).In(location)
	if next.Year() != 2026 || next.Month() != time.April || next.Day() != 5 ||
		next.Hour() != 2 || next.Minute() != 30 {
		t.Fatalf("next local run = %s, want 2026-04-05 02:30", next)
	}
}
