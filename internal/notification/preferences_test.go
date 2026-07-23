package notification

import "testing"

func TestPreferenceCatalogHasUniqueEvents(t *testing.T) {
	seen := make(map[string]bool, len(preferenceCatalog))
	for _, event := range preferenceCatalog {
		if event.EventType == "" || event.Group == "" || event.LabelHR == "" || event.LabelEN == "" {
			t.Fatalf("notification preference is incomplete: %#v", event)
		}
		if seen[event.EventType] {
			t.Fatalf("notification event %q is listed more than once", event.EventType)
		}
		seen[event.EventType] = true
	}
}

func TestSecurityEventsCannotBeDisabled(t *testing.T) {
	for _, eventType := range []string{"account.registered", "password.reset_requested"} {
		if preferenceEventKnown(eventType) {
			t.Fatalf("security event %q must not be configurable", eventType)
		}
		for _, event := range preferenceCatalog {
			if event.EventType == eventType && (!event.Enabled || event.Configurable) {
				t.Fatalf("security event %q must be enabled and locked", eventType)
			}
		}
	}
}

func TestHandledProjectEventsAreConfigurable(t *testing.T) {
	events := []string{
		"content.created", "content.updated", "content.reviewed", "content.revision_requested",
		"calendar_item.created", "calendar_item.updated",
		"publication_job.succeeded", "publication_job.failed",
		"newsletter_delivery.sent", "newsletter_delivery.failed",
		"strategy.updated", "strategy.file_uploaded", "service_request.updated",
	}
	for _, eventType := range events {
		if !preferenceEventKnown(eventType) {
			t.Fatalf("handled event %q is missing a configurable preference", eventType)
		}
	}
}
