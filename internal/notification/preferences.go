package notification

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var ErrUnknownPreferenceEvent = errors.New("unknown notification event")

type PreferenceEvent struct {
	EventType     string `json:"eventType"`
	Group         string `json:"group"`
	LabelHR       string `json:"labelHr"`
	LabelEN       string `json:"labelEn"`
	DescriptionHR string `json:"descriptionHr"`
	DescriptionEN string `json:"descriptionEn"`
	Enabled       bool   `json:"enabled"`
	Configurable  bool   `json:"configurable"`
}

type Preferences struct {
	EmailEnabled bool              `json:"emailEnabled"`
	Events       []PreferenceEvent `json:"events"`
}

type UpdatePreferencesInput struct {
	EmailEnabled bool            `json:"emailEnabled"`
	Events       map[string]bool `json:"events"`
}

var preferenceCatalog = []PreferenceEvent{
	{EventType: "content.created", Group: "content", LabelHR: "Novi sadržaj", LabelEN: "New content", DescriptionHR: "Kada drugi član doda novi sadržaj u projekt.", DescriptionEN: "When another member adds new project content.", Configurable: true},
	{EventType: "content.updated", Group: "content", LabelHR: "Izmjena sadržaja", LabelEN: "Content updated", DescriptionHR: "Kada se promijene tekst, naslov ili podaci sadržaja.", DescriptionEN: "When content copy, title or details change.", Configurable: true},
	{EventType: "content.reviewed", Group: "content", LabelHR: "Sadržaj je odobren", LabelEN: "Content approved", DescriptionHR: "Kada sadržaj prođe urednički pregled.", DescriptionEN: "When content passes editorial review.", Configurable: true},
	{EventType: "content.revision_requested", Group: "content", LabelHR: "Sadržaj je vraćen na doradu", LabelEN: "Revision requested", DescriptionHR: "Kada pregled uključuje komentar i zahtjev za doradu.", DescriptionEN: "When a review includes feedback and a revision request.", Configurable: true},
	{EventType: "calendar_item.created", Group: "calendar", LabelHR: "Nova stavka kalendara", LabelEN: "New calendar item", DescriptionHR: "Kada se u raspored doda novi termin.", DescriptionEN: "When a new item is added to the schedule.", Configurable: true},
	{EventType: "calendar_item.updated", Group: "calendar", LabelHR: "Promjena kalendara", LabelEN: "Calendar item updated", DescriptionHR: "Kada se promijene termin, kanal ili status stavke.", DescriptionEN: "When an item's date, channel or status changes.", Configurable: true},
	{EventType: "publication_job.succeeded", Group: "publishing", LabelHR: "Objava je uspjela", LabelEN: "Publication succeeded", DescriptionHR: "Kada sadržaj uspješno prođe objavu na kanalu.", DescriptionEN: "When content is successfully published to a channel.", Configurable: true},
	{EventType: "publication_job.failed", Group: "publishing", LabelHR: "Objava nije uspjela", LabelEN: "Publication failed", DescriptionHR: "Kada objava na kanalu vrati grešku.", DescriptionEN: "When channel publishing returns an error.", Configurable: true},
	{EventType: "newsletter_delivery.sent", Group: "publishing", LabelHR: "Newsletter je poslan", LabelEN: "Newsletter delivered", DescriptionHR: "Kada zakazana newsletter dostava završi uspješno.", DescriptionEN: "When a scheduled newsletter delivery succeeds.", Configurable: true},
	{EventType: "newsletter_delivery.failed", Group: "publishing", LabelHR: "Newsletter nije poslan", LabelEN: "Newsletter failed", DescriptionHR: "Kada newsletter dostava ne uspije.", DescriptionEN: "When a newsletter delivery fails.", Configurable: true},
	{EventType: "strategy.updated", Group: "workspace", LabelHR: "Strategija je izmijenjena", LabelEN: "Strategy updated", DescriptionHR: "Kada se ručno promijeni strateški kontekst projekta.", DescriptionEN: "When project strategy context is edited manually.", Configurable: true},
	{EventType: "strategy.file_uploaded", Group: "workspace", LabelHR: "Učitana je nova strategija", LabelEN: "Strategy file uploaded", DescriptionHR: "Kada se učita nova datoteka strateškog konteksta.", DescriptionEN: "When a new strategy context file is uploaded.", Configurable: true},
	{EventType: "service_request.updated", Group: "workspace", LabelHR: "Zahtjev projekta je ažuriran", LabelEN: "Project request updated", DescriptionHR: "Kada se promijeni status servisnog zahtjeva.", DescriptionEN: "When a service request status changes.", Configurable: true},
	{EventType: "account.registered", Group: "security", LabelHR: "Otvaranje računa", LabelEN: "Account created", DescriptionHR: "Jednokratna potvrda nakon registracije računa.", DescriptionEN: "One-time confirmation after account registration.", Enabled: true},
	{EventType: "password.reset_requested", Group: "security", LabelHR: "Promjena lozinke", LabelEN: "Password reset", DescriptionHR: "Sigurnosni e-mail s jednokratnim linkom; uvijek je uključen.", DescriptionEN: "Security email with a single-use link; always enabled.", Enabled: true},
}

func preferenceEventKnown(eventType string) bool {
	for _, event := range preferenceCatalog {
		if event.EventType == eventType && event.Configurable {
			return true
		}
	}
	return false
}

func (r *Repository) GetPreferences(ctx context.Context, userID string) (Preferences, error) {
	preferences := Preferences{EmailEnabled: true, Events: make([]PreferenceEvent, len(preferenceCatalog))}
	copy(preferences.Events, preferenceCatalog)
	for index := range preferences.Events {
		if preferences.Events[index].Configurable {
			preferences.Events[index].Enabled = true
		}
	}
	if r == nil {
		return Preferences{}, errors.New("notification repository is unavailable")
	}
	err := r.pool.QueryRow(ctx, `SELECT email_enabled FROM notification_preferences WHERE user_id=$1::uuid`, userID).Scan(&preferences.EmailEnabled)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Preferences{}, err
	}
	rows, err := r.pool.Query(ctx, `SELECT event_type, email_enabled FROM notification_event_preferences WHERE user_id=$1::uuid`, userID)
	if err != nil {
		return Preferences{}, err
	}
	defer rows.Close()
	stored := map[string]bool{}
	for rows.Next() {
		var eventType string
		var enabled bool
		if err := rows.Scan(&eventType, &enabled); err != nil {
			return Preferences{}, err
		}
		stored[eventType] = enabled
	}
	if err := rows.Err(); err != nil {
		return Preferences{}, err
	}
	for index := range preferences.Events {
		if enabled, ok := stored[preferences.Events[index].EventType]; ok && preferences.Events[index].Configurable {
			preferences.Events[index].Enabled = enabled
		}
	}
	return preferences, nil
}

func (r *Repository) SavePreferences(ctx context.Context, userID string, input UpdatePreferencesInput) (Preferences, error) {
	if r == nil {
		return Preferences{}, errors.New("notification repository is unavailable")
	}
	for eventType := range input.Events {
		if !preferenceEventKnown(eventType) {
			return Preferences{}, fmt.Errorf("%w: %s", ErrUnknownPreferenceEvent, eventType)
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Preferences{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO notification_preferences (user_id, email_enabled, content_updates, publication_updates, workspace_updates)
		VALUES ($1::uuid, $2, TRUE, TRUE, TRUE)
		ON CONFLICT (user_id) DO UPDATE
		SET email_enabled=EXCLUDED.email_enabled, content_updates=TRUE,
		    publication_updates=TRUE, workspace_updates=TRUE, updated_at=now()`, userID, input.EmailEnabled); err != nil {
		return Preferences{}, err
	}
	for eventType, enabled := range input.Events {
		if _, err := tx.Exec(ctx, `
			INSERT INTO notification_event_preferences (user_id, event_type, email_enabled)
			VALUES ($1::uuid, $2, $3)
			ON CONFLICT (user_id, event_type) DO UPDATE
			SET email_enabled=EXCLUDED.email_enabled, updated_at=now()`, userID, eventType, enabled); err != nil {
			return Preferences{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Preferences{}, err
	}
	return r.GetPreferences(ctx, userID)
}
