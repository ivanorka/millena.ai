CREATE TABLE notification_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    email_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    content_updates BOOLEAN NOT NULL DEFAULT TRUE,
    publication_updates BOOLEAN NOT NULL DEFAULT TRUE,
    workspace_updates BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE email_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audit_event_id UUID REFERENCES audit_events(id) ON DELETE CASCADE,
    recipient_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    subject TEXT NOT NULL,
    summary TEXT NOT NULL,
    action_path TEXT NOT NULL DEFAULT '/app.html#overview',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (audit_event_id, recipient_user_id)
);

CREATE INDEX email_notifications_pending_idx
    ON email_notifications (status, next_attempt_at, created_at)
    WHERE status IN ('pending', 'failed');

CREATE OR REPLACE FUNCTION millena_queue_audit_email()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    notification_subject TEXT;
    notification_summary TEXT;
    notification_path TEXT := '/app.html#overview';
    notification_group TEXT;
BEGIN
    IF NEW.project_id IS NULL THEN
        RETURN NEW;
    END IF;

    CASE NEW.action
        WHEN 'account.registered' THEN
            notification_subject := 'Dobro došli u Millena AI';
            notification_summary := 'Vaš račun i projekt su spremni. Možete nastaviti s postavljanjem strategije i prvog sadržaja.';
            notification_path := '/app.html#setup';
            notification_group := 'workspace';
        WHEN 'content.created' THEN
            notification_subject := 'Novi sadržaj je dodan';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'title', ''), 'Novi zapis sadržaja spreman je za tim.');
            notification_path := '/app.html#content';
            notification_group := 'content';
        WHEN 'content.updated' THEN
            notification_subject := 'Sadržaj je izmijenjen';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'title', ''), 'Sadržaj je ažuriran i spreman za daljnji rad.');
            notification_path := '/app.html#content';
            notification_group := 'content';
        WHEN 'content.reviewed' THEN
            notification_subject := 'Sadržaj je pregledan i odobren';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'title', ''), 'Sadržaj je prošao pregled i može ići u sljedeću fazu.');
            notification_path := '/app.html#content';
            notification_group := 'content';
        WHEN 'content.revision_requested' THEN
            notification_subject := 'Sadržaj vraćen je na doradu';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'comment', ''), 'Pogledajte komentar pregleda i pripremite novu verziju.');
            notification_path := '/app.html#content';
            notification_group := 'content';
        WHEN 'calendar_item.created' THEN
            notification_subject := 'Nova stavka dodana je u kalendar';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'title', ''), 'Provjerite termin i status stavke u rasporedu.');
            notification_path := '/app.html#calendar';
            notification_group := 'content';
        WHEN 'calendar_item.updated' THEN
            notification_subject := 'Promijenjen je status ili termin kalendara';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'title', ''), 'Kalendar je ažuriran.');
            notification_path := '/app.html#calendar';
            notification_group := 'content';
        WHEN 'publication_job.succeeded' THEN
            notification_subject := 'Objava je uspješno objavljena';
            notification_summary := 'Zakazana objava prošla je kroz kanal bez greške.';
            notification_path := '/app.html#social';
            notification_group := 'publication';
        WHEN 'publication_job.failed' THEN
            notification_subject := 'Objava nije uspjela';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'failure', ''), 'Provjerite kanal i pokušajte objavu ponovno.');
            notification_path := '/app.html#social';
            notification_group := 'publication';
        WHEN 'newsletter_delivery.succeeded' THEN
            notification_subject := 'Newsletter je poslan';
            notification_summary := 'Zakazani newsletter uspješno je poslan odabranoj publici.';
            notification_path := '/app.html#newsletter';
            notification_group := 'publication';
        WHEN 'newsletter_delivery.failed' THEN
            notification_subject := 'Newsletter nije poslan';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'failure', ''), 'Provjerite postavke kampanje i pokušajte ponovno.');
            notification_path := '/app.html#newsletter';
            notification_group := 'publication';
        WHEN 'strategy.updated', 'strategy.file_uploaded' THEN
            notification_subject := 'Strateški kontekst je ažuriran';
            notification_summary := 'AI kontekst projekta sada koristi najnoviju verziju strategije.';
            notification_path := '/app.html#setup';
            notification_group := 'workspace';
        WHEN 'service_request.updated' THEN
            notification_subject := 'Ažuriran je zahtjev projekta';
            notification_summary := COALESCE(NULLIF(NEW.metadata->>'status', ''), 'Status zahtjeva je promijenjen.');
            notification_path := '/app.html#requests';
            notification_group := 'workspace';
        ELSE
            RETURN NEW;
    END CASE;

    INSERT INTO email_notifications (
        audit_event_id, recipient_user_id, project_id, event_type, subject, summary, action_path
    )
    SELECT NEW.id, member.user_id, NEW.project_id, NEW.action,
           notification_subject, notification_summary, notification_path
    FROM project_members AS member
    JOIN users AS recipient ON recipient.id = member.user_id AND recipient.status = 'active'
    LEFT JOIN notification_preferences AS preference ON preference.user_id = member.user_id
    WHERE member.status = 'active'
      AND COALESCE(preference.email_enabled, TRUE)
      AND (
          (notification_group = 'content' AND COALESCE(preference.content_updates, TRUE))
          OR (notification_group = 'publication' AND COALESCE(preference.publication_updates, TRUE))
          OR (notification_group = 'workspace' AND COALESCE(preference.workspace_updates, TRUE))
      )
      AND (NEW.action = 'account.registered' OR member.user_id IS DISTINCT FROM NEW.actor_id)
    ON CONFLICT (audit_event_id, recipient_user_id) DO NOTHING;

    RETURN NEW;
END;
$$;

CREATE TRIGGER audit_events_queue_email
AFTER INSERT ON audit_events
FOR EACH ROW EXECUTE FUNCTION millena_queue_audit_email();
