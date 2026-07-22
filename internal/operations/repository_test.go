package operations

import (
	"context"
	"testing"
	"time"
)

func TestNextAutomationRun(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		rule dueAutomation
		want *time.Time
	}{
		{
			name: "weekly profile cadence advances in seven day steps beyond now",
			rule: dueAutomation{Kind: "newsletter", NewsletterCadence: "weekly", Timezone: "UTC",
				NextRunAt: time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)},
			want: timePointer(time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)),
		},
		{
			name: "weekly schedule rule",
			rule: dueAutomation{Kind: "custom", ScheduleRule: "FREQ=WEEKLY;BYDAY=FR", Timezone: "UTC", NextRunAt: now.Add(-time.Hour)},
			want: timePointer(time.Date(2026, time.July, 24, 9, 0, 0, 0, time.UTC)),
		},
		{
			name: "calendar gap advances daily beyond now",
			rule: dueAutomation{Kind: "calendar_gap", Timezone: "UTC", NextRunAt: now.Add(-49 * time.Hour)},
			want: timePointer(time.Date(2026, time.July, 23, 10, 0, 0, 0, time.UTC)),
		},
		{
			name: "daily rrule",
			rule: dueAutomation{Kind: "custom", ScheduleRule: "FREQ=DAILY", Timezone: "UTC", NextRunAt: now.Add(-time.Hour)},
			want: timePointer(time.Date(2026, time.July, 23, 9, 0, 0, 0, time.UTC)),
		},
		{
			name: "monthly rrule",
			rule: dueAutomation{Kind: "custom", ScheduleRule: "FREQ=MONTHLY;BYMONTHDAY=22", Timezone: "UTC", NextRunAt: time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)},
			want: timePointer(time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)),
		},
		{
			name: "configured cadence overrides project cadence",
			rule: dueAutomation{
				Kind: "newsletter", NewsletterCadence: "monthly",
				Configuration: map[string]any{"cadence": "biweekly"},
				Timezone:      "UTC", NextRunAt: time.Date(2026, time.July, 10, 10, 0, 0, 0, time.UTC),
			},
			want: timePointer(time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)),
		},
		{
			name: "configured cadence schedules every automation kind",
			rule: dueAutomation{
				Kind:          "bot_event",
				Configuration: map[string]any{"cadence": "weekly", "hour": float64(8)},
				Timezone:      "UTC", NextRunAt: time.Date(2026, time.July, 17, 8, 0, 0, 0, time.UTC),
			},
			want: timePointer(time.Date(2026, time.July, 24, 8, 0, 0, 0, time.UTC)),
		},
		{
			name: "explicit schedule has priority over configured off",
			rule: dueAutomation{
				Kind: "custom", ScheduleRule: "FREQ=DAILY",
				Configuration: map[string]any{"cadence": "off"},
				Timezone:      "UTC", NextRunAt: now.Add(-time.Hour),
			},
			want: timePointer(time.Date(2026, time.July, 23, 9, 0, 0, 0, time.UTC)),
		},
		{
			name: "project monthly newsletter cadence",
			rule: dueAutomation{
				Kind: "newsletter", NewsletterCadence: "monthly",
				Timezone: "UTC", NextRunAt: time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC),
			},
			want: timePointer(time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)),
		},
		{
			name: "project cadence can disable an unscheduled newsletter",
			rule: dueAutomation{Kind: "newsletter", NewsletterCadence: "off", NextRunAt: now.Add(-time.Minute)},
		},
		{
			name: "gap rule checks daily",
			rule: dueAutomation{Kind: "custom", ScheduleRule: "gap:5d", Timezone: "UTC",
				Configuration: map[string]any{"hour": float64(11), "minute": float64(30)}, NextRunAt: now.Add(-time.Hour)},
			want: timePointer(time.Date(2026, time.July, 23, 11, 30, 0, 0, time.UTC)),
		},
		{
			name: "unsupported stored RRULE is not approximated",
			rule: dueAutomation{
				Kind: "custom", ScheduleRule: "FREQ=DAILY;COUNT=3",
				NextRunAt: now.Add(-time.Minute),
			},
		},
		{name: "one shot is cleared", rule: dueAutomation{Kind: "custom", NextRunAt: now.Add(-time.Minute)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := nextAutomationRun(test.rule, now)
			if test.want == nil {
				if got != nil {
					t.Fatalf("nextAutomationRun() = %v, want nil", got)
				}
				return
			}
			if got == nil || !got.Equal(*test.want) {
				t.Fatalf("nextAutomationRun() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestNextAutomationRunPreservesLocalHourAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	previous := time.Date(2026, time.March, 28, 9, 0, 0, 0, location)
	now := time.Date(2026, time.March, 28, 10, 0, 0, 0, location)
	next := nextAutomationRun(dueAutomation{
		Kind: "custom", ScheduleRule: "FREQ=DAILY", NextRunAt: previous,
		Timezone: "Europe/Zagreb",
	}, now)
	if next == nil {
		t.Fatal("nextAutomationRun() returned nil")
	}
	localNext := next.In(location)
	if localNext.Day() != 29 || localNext.Hour() != 9 {
		t.Fatalf("next local run = %s, want March 29 at 09:00", localNext)
	}
}

func TestNextAutomationRunReturnsToDeclaredClockAfterDSTGap(t *testing.T) {
	location, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	// Europe/Zagreb has no 02:30 on 2026-03-29. The current occurrence is
	// normalized by the timezone database, but the following Sunday must not
	// inherit that normalized clock permanently.
	now := time.Date(2026, time.March, 29, 4, 0, 0, 0, location)
	next := nextAutomationRun(dueAutomation{
		Kind: "custom", ScheduleRule: "FREQ=WEEKLY;BYDAY=SU;BYHOUR=2;BYMINUTE=30",
		NextRunAt: time.Date(2026, time.March, 29, 2, 30, 0, 0, location),
		Timezone:  "Europe/Zagreb",
	}, now)
	if next == nil {
		t.Fatal("nextAutomationRun() returned nil")
	}
	localNext := next.In(location)
	if localNext.Year() != 2026 || localNext.Month() != time.April || localNext.Day() != 5 ||
		localNext.Hour() != 2 || localNext.Minute() != 30 {
		t.Fatalf("next local run = %s, want 2026-04-05 02:30", localNext)
	}
}

func TestBatchResultTotal(t *testing.T) {
	result := BatchResult{
		AutomationsRun: 1, PublicationsSucceeded: 2, PublicationsFailed: 3,
		NewslettersSent: 4, NewslettersFailed: 5,
	}
	if got := result.Total(); got != 15 {
		t.Fatalf("Total() = %d, want 15", got)
	}
}

type signalingProcessor struct {
	called chan struct{}
}

func (processor *signalingProcessor) ProcessDue(context.Context, int) (BatchResult, error) {
	select {
	case <-processor.called:
	default:
		close(processor.called)
	}
	return BatchResult{}, nil
}

func TestWorkerProcessesImmediately(t *testing.T) {
	processor := &signalingProcessor{called: make(chan struct{})}
	worker := newWorker(processor, time.Hour, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()
	select {
	case <-processor.called:
	case <-time.After(time.Second):
		t.Fatal("worker did not process immediately")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func timePointer(value time.Time) *time.Time { return &value }
