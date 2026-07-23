DROP TRIGGER IF EXISTS audit_events_queue_newsletter_sent_email ON audit_events;
DROP FUNCTION IF EXISTS millena_queue_newsletter_sent_email();
DROP TRIGGER IF EXISTS email_notifications_apply_preferences ON email_notifications;
DROP FUNCTION IF EXISTS millena_filter_email_notification();
DROP TABLE IF EXISTS notification_event_preferences;
