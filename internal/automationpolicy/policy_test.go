package automationpolicy

import "testing"

func TestReviewPolicyStatuses(t *testing.T) {
	tests := []struct {
		policy         string
		contentStatus  string
		calendarStatus string
	}{
		{policy: "always", contentStatus: "in_review", calendarStatus: "suggestion"},
		{policy: "conditional", contentStatus: "draft", calendarStatus: "draft"},
		{policy: "automatic", contentStatus: "approved", calendarStatus: "draft"},
	}
	for _, test := range tests {
		t.Run(test.policy, func(t *testing.T) {
			if got := ContentStatus(test.policy); got != test.contentStatus {
				t.Fatalf("ContentStatus(%q) = %q, expected %q", test.policy, got, test.contentStatus)
			}
			if got := CalendarStatus(test.policy); got != test.calendarStatus {
				t.Fatalf("CalendarStatus(%q) = %q, expected %q", test.policy, got, test.calendarStatus)
			}
		})
	}
}
