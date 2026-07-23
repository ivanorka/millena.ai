CREATE TABLE notification_event_preferences (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    email_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, event_type)
);

CREATE OR REPLACE FUNCTION millena_filter_email_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    global_enabled BOOLEAN := TRUE;
    event_enabled BOOLEAN := TRUE;
BEGIN
    IF NEW.event_type IN ('account.registered', 'password.reset_requested') THEN
        RETURN NEW;
    END IF;

    SELECT COALESCE(preference.email_enabled, TRUE)
    INTO global_enabled
    FROM notification_preferences AS preference
    WHERE preference.user_id = NEW.recipient_user_id;

    IF NOT FOUND THEN
        global_enabled := TRUE;
    END IF;

    IF NOT global_enabled THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(preference.email_enabled, TRUE)
    INTO event_enabled
    FROM notification_event_preferences AS preference
    WHERE preference.user_id = NEW.recipient_user_id
      AND preference.event_type = NEW.event_type;

    IF NOT FOUND THEN
        event_enabled := TRUE;
    END IF;

    IF NOT event_enabled THEN
        RETURN NULL;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER email_notifications_apply_preferences
BEFORE INSERT ON email_notifications
FOR EACH ROW EXECUTE FUNCTION millena_filter_email_notification();

-- Migration 19 used the provisional action name newsletter_delivery.succeeded.
-- The application records successful deliveries as newsletter_delivery.sent,
-- so existing databases need this companion trigger as well as fresh installs.
CREATE OR REPLACE FUNCTION millena_queue_newsletter_sent_email()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO email_notifications (
        audit_event_id, recipient_user_id, project_id, event_type, subject, summary, action_path
    )
    SELECT NEW.id, member.user_id, NEW.project_id, NEW.action,
           'Newsletter je poslan',
           'Zakazani newsletter uspješno je poslan odabranoj publici.',
           '/app.html#newsletter'
    FROM project_members AS member
    JOIN users AS recipient ON recipient.id = member.user_id AND recipient.status = 'active'
    WHERE member.status = 'active'
      AND member.user_id IS DISTINCT FROM NEW.actor_id
    ON CONFLICT (audit_event_id, recipient_user_id) DO NOTHING;

    RETURN NEW;
END;
$$;

CREATE TRIGGER audit_events_queue_newsletter_sent_email
AFTER INSERT ON audit_events
FOR EACH ROW
WHEN (NEW.action = 'newsletter_delivery.sent')
EXECUTE FUNCTION millena_queue_newsletter_sent_email();
