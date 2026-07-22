// Package automationpolicy translates persisted review policies into the
// initial workflow state of an automation effect.
package automationpolicy

// ContentStatus returns the canonical initial content status. An always-review
// rule enters the review queue, a conditional rule remains a draft, and a
// fully automatic rule is approved but still requires a separate publish or
// schedule action.
func ContentStatus(reviewPolicy string) string {
	switch reviewPolicy {
	case "always":
		return "in_review"
	case "automatic":
		return "approved"
	default:
		return "draft"
	}
}

// CalendarStatus uses suggestion for mandatory review. Calendar rows do not
// have an approved state, so conditional and automatic rules create editable
// drafts; the policy remains explicit in metadata.
func CalendarStatus(reviewPolicy string) string {
	if reviewPolicy == "always" {
		return "suggestion"
	}
	return "draft"
}
