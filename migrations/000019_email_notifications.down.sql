DROP TRIGGER IF EXISTS audit_events_queue_email ON audit_events;
DROP FUNCTION IF EXISTS millena_queue_audit_email();
DROP TABLE IF EXISTS email_notifications;
DROP TABLE IF EXISTS notification_preferences;
