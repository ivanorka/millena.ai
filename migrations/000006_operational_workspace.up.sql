CREATE TABLE plan_catalog (
    code TEXT PRIMARY KEY CHECK (code ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    owner_project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 2 AND 80),
    description TEXT NOT NULL DEFAULT '',
    price_cents INTEGER NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
    currency CHAR(3) NOT NULL DEFAULT 'EUR',
    billing_interval TEXT NOT NULL DEFAULT 'month' CHECK (billing_interval IN ('month', 'year', 'custom')),
    seat_limit INTEGER CHECK (seat_limit IS NULL OR seat_limit > 0),
    monthly_publication_limit INTEGER CHECK (monthly_publication_limit IS NULL OR monthly_publication_limit > 0),
    storage_limit_bytes BIGINT CHECK (storage_limit_bytes IS NULL OR storage_limit_bytes > 0),
    features JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(features) = 'object'),
    is_active BOOLEAN NOT NULL DEFAULT true,
    is_system BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO plan_catalog (
    code, name, description, price_cents, seat_limit, monthly_publication_limit,
    storage_limit_bytes, features, is_system
)
VALUES
    ('starter', 'Starter', 'Osnovni sadržajni workflow za mali tim.', 2900, 3, 30, 5368709120,
     '{"aiAgents":true,"analytics":false,"api":false,"auditLog":true,"automations":false,"prioritySupport":false,"socialChannels":3,"sso":false,"whiteLabel":false}'::jsonb, true),
    ('growth', 'Growth', 'Napredne automatizacije i svi glavni kanali.', 7900, 10, 250, 53687091200,
     '{"aiAgents":true,"analytics":true,"api":true,"auditLog":true,"automations":true,"prioritySupport":false,"socialChannels":"all","sso":false,"whiteLabel":false}'::jsonb, true),
    ('unlimited', 'Unlimited', 'Svi moduli, kanali i neograničen rad.', 0, NULL, NULL, NULL,
     '{"aiAgents":true,"analytics":true,"api":true,"auditLog":true,"automations":true,"prioritySupport":true,"socialChannels":"all","sso":true,"whiteLabel":true}'::jsonb, true)
ON CONFLICT (code) DO NOTHING;

ALTER TABLE project_entitlements
    DROP CONSTRAINT project_entitlements_plan_code_check,
    ADD CONSTRAINT project_entitlements_plan_code_fkey
        FOREIGN KEY (plan_code) REFERENCES plan_catalog(code);

CREATE TABLE project_profiles (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    company_name TEXT NOT NULL DEFAULT '',
    website_url TEXT NOT NULL DEFAULT '',
    industry TEXT NOT NULL DEFAULT '',
    primary_language CHAR(2) NOT NULL DEFAULT 'hr' CHECK (primary_language IN ('hr', 'en')),
    timezone TEXT NOT NULL DEFAULT 'Europe/Zagreb',
    social_posts_per_week INTEGER NOT NULL DEFAULT 4 CHECK (social_posts_per_week BETWEEN 0 AND 100),
    newsletter_cadence TEXT NOT NULL DEFAULT 'weekly' CHECK (newsletter_cadence IN ('off', 'weekly', 'biweekly', 'monthly')),
    signup_headline TEXT NOT NULL DEFAULT 'Budite u toku.',
    signup_copy TEXT NOT NULL DEFAULT 'Najvažnije priče jednom tjedno.',
    setup_completed BOOLEAN NOT NULL DEFAULT false,
    setup_completed_at TIMESTAMPTZ,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE automation_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    rule_key TEXT NOT NULL CHECK (rule_key ~ '^[a-z0-9]+(?:_[a-z0-9]+)*$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    description TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK (kind IN ('master', 'channel', 'bot_event', 'calendar_gap', 'newsletter', 'custom')),
    channel TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    review_policy TEXT NOT NULL DEFAULT 'always' CHECK (review_policy IN ('always', 'conditional', 'automatic')),
    schedule_rule TEXT NOT NULL DEFAULT '',
    configuration JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(configuration) = 'object'),
    run_count INTEGER NOT NULL DEFAULT 0 CHECK (run_count >= 0),
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, rule_key)
);

CREATE INDEX automation_rules_project_enabled_idx
    ON automation_rules (project_id, enabled, kind);

CREATE INDEX automation_rules_due_idx
    ON automation_rules (next_run_at)
    WHERE enabled AND next_run_at IS NOT NULL;

CREATE TABLE audience_lists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    description TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE UNIQUE INDEX audience_lists_project_default_idx
    ON audience_lists (project_id) WHERE is_default;

CREATE TABLE audience_contacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    list_id UUID REFERENCES audience_lists(id) ON DELETE SET NULL,
    first_name TEXT NOT NULL DEFAULT '',
    last_name TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'csv', 'website', 'api')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('pending', 'active', 'unsubscribed', 'bounced')),
    consent BOOLEAN NOT NULL DEFAULT false,
    subscribed_at TIMESTAMPTZ,
    unsubscribed_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX audience_contacts_project_email_idx
    ON audience_contacts (project_id, lower(email));
CREATE INDEX audience_contacts_project_status_idx
    ON audience_contacts (project_id, status, created_at DESC);

CREATE TABLE channel_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('whatsapp', 'telegram', 'website', 'newsletter', 'webhook', 'custom_api')),
    mode TEXT NOT NULL DEFAULT 'sandbox' CHECK (mode IN ('sandbox', 'api', 'webhook')),
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 2 AND 120),
    account_handle TEXT NOT NULL DEFAULT '',
    endpoint_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'error', 'disconnected')),
    credential_fingerprint TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    last_checked_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, provider)
);

CREATE INDEX channel_connections_project_status_idx
    ON channel_connections (project_id, status, provider);

CREATE TABLE assistant_threads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT 'Razgovor s Millenom',
    channel TEXT NOT NULL DEFAULT 'app' CHECK (channel IN ('app', 'telegram', 'whatsapp')),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX assistant_threads_project_time_idx
    ON assistant_threads (project_id, updated_at DESC);

CREATE TABLE assistant_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id UUID NOT NULL REFERENCES assistant_threads(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    body TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 20000),
    action_type TEXT NOT NULL DEFAULT '',
    action_entity_id UUID,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX assistant_messages_thread_time_idx
    ON assistant_messages (thread_id, created_at);

CREATE TABLE newsletter_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    content_item_id UUID NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
    list_id UUID REFERENCES audience_lists(id) ON DELETE SET NULL,
    mode TEXT NOT NULL DEFAULT 'sandbox' CHECK (mode IN ('sandbox', 'provider')),
    status TEXT NOT NULL CHECK (status IN ('test_sent', 'scheduled', 'sent', 'failed', 'cancelled')),
    subject TEXT NOT NULL CHECK (char_length(subject) BETWEEN 2 AND 180),
    test_recipient TEXT,
    recipient_count INTEGER NOT NULL DEFAULT 0 CHECK (recipient_count >= 0),
    scheduled_for TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    external_reference TEXT,
    last_error TEXT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX newsletter_deliveries_project_time_idx
    ON newsletter_deliveries (project_id, created_at DESC);

CREATE INDEX newsletter_deliveries_due_idx
    ON newsletter_deliveries (scheduled_for)
    WHERE status = 'scheduled';

CREATE TABLE service_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    request_type TEXT NOT NULL CHECK (request_type IN ('website_proposal', 'integration_help', 'support')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'completed', 'cancelled')),
    summary TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE content_variants
    ADD COLUMN title TEXT NOT NULL DEFAULT '',
    ADD COLUMN summary TEXT NOT NULL DEFAULT '',
    ADD COLUMN scheduled_for TIMESTAMPTZ,
    ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0);

CREATE UNIQUE INDEX publication_jobs_variant_active_idx
    ON publication_jobs (content_variant_id)
    WHERE status IN ('pending', 'running');

ALTER TABLE calendar_items
    ADD COLUMN content_item_id UUID REFERENCES content_items(id) ON DELETE SET NULL,
    ADD COLUMN content_variant_id UUID REFERENCES content_variants(id) ON DELETE SET NULL,
    ADD COLUMN publication_job_id UUID REFERENCES publication_jobs(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX calendar_items_variant_idx
    ON calendar_items (content_variant_id)
    WHERE content_variant_id IS NOT NULL;

ALTER TABLE social_posts
    ADD COLUMN content_item_id UUID REFERENCES content_items(id) ON DELETE SET NULL,
    ADD COLUMN content_variant_id UUID REFERENCES content_variants(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX social_posts_content_variant_idx
    ON social_posts (content_variant_id)
    WHERE content_variant_id IS NOT NULL;

INSERT INTO project_profiles (
    project_id, company_name, website_url, industry, primary_language,
    social_posts_per_week, newsletter_cadence, setup_completed
)
SELECT id,
       COALESCE(settings->>'brand', name),
       CASE WHEN slug = 'millena-demo' THEN 'https://mpr.hr' ELSE '' END,
       CASE WHEN slug = 'millena-demo' THEN 'Komunikacije i odnosi s javnošću' ELSE '' END,
       default_locale,
       4,
       'weekly',
       slug = 'millena-demo'
FROM projects
ON CONFLICT (project_id) DO NOTHING;

INSERT INTO automation_rules (
    project_id, rule_key, name, description, kind, channel, enabled,
    review_policy, schedule_rule, configuration, next_run_at
)
SELECT project.id, seed.rule_key, seed.name, seed.description, seed.kind,
       seed.channel, seed.enabled, seed.review_policy, seed.schedule_rule,
       seed.configuration, seed.next_run_at
FROM projects AS project
JOIN project_profiles AS profile ON profile.project_id = project.id
CROSS JOIN LATERAL (
    VALUES
      ('master', 'Glavni tijek sadržaja', 'Prikupljanje, provjera, izrada i objava prema pravilima projekta.', 'master', '', true, 'conditional', '', '{"factCheck":true,"respectForbiddenTopics":true}'::jsonb, NULL::timestamptz),
      ('bot_event', 'Događaj iz bota', 'Iz poruke izradi social, blog i newsletter nacrt.', 'bot_event', 'whatsapp', true, 'always', '', '{"formats":["social","blog","newsletter"]}'::jsonb, NULL::timestamptz),
      ('calendar_gap', 'Popuni praznine u kalendaru', 'Ako pet dana nema LinkedIn sadržaja, pripremi prijedlog.', 'calendar_gap', 'linkedin', true, 'always', 'gap:5d', '{"gapDays":5}'::jsonb,
       (CASE
          WHEN date_trunc('day', now() AT TIME ZONE profile.timezone) + interval '10 hours' > now() AT TIME ZONE profile.timezone
            THEN date_trunc('day', now() AT TIME ZONE profile.timezone) + interval '10 hours'
          ELSE date_trunc('day', now() AT TIME ZONE profile.timezone) + interval '1 day 10 hours'
        END) AT TIME ZONE profile.timezone),
      ('weekly_newsletter', 'Tjedni newsletter', 'Petkom složi pregled najboljeg sadržaja.', 'newsletter', 'newsletter', true, 'always', 'FREQ=WEEKLY;BYDAY=FR;BYHOUR=10', '{"weekday":"friday","hour":10}'::jsonb,
       (CASE
          WHEN date_trunc('week', now() AT TIME ZONE profile.timezone) + interval '4 days 10 hours' > now() AT TIME ZONE profile.timezone
            THEN date_trunc('week', now() AT TIME ZONE profile.timezone) + interval '4 days 10 hours'
          ELSE date_trunc('week', now() AT TIME ZONE profile.timezone) + interval '11 days 10 hours'
        END) AT TIME ZONE profile.timezone),
      ('linkedin', 'LinkedIn pravilo', 'Objavi nakon provjere činjenica.', 'channel', 'linkedin', true, 'conditional', '', '{}'::jsonb, NULL::timestamptz),
      ('instagram', 'Instagram pravilo', 'Traži pregled kada nedostaje fotografija.', 'channel', 'instagram', true, 'conditional', '', '{}'::jsonb, NULL::timestamptz),
      ('facebook', 'Facebook pravilo', 'Pripremi prilagođenu verziju.', 'channel', 'facebook', true, 'conditional', '', '{}'::jsonb, NULL::timestamptz),
      ('youtube', 'YouTube pravilo', 'Pripremi naslov, opis i Shorts tekst.', 'channel', 'youtube', true, 'always', '', '{}'::jsonb, NULL::timestamptz),
      ('x', 'X pravilo', 'Pripremi kratku verziju ili niz.', 'channel', 'x', true, 'always', '', '{}'::jsonb, NULL::timestamptz),
      ('reddit', 'Reddit pravilo', 'Uvijek pošalji osobi na pregled.', 'channel', 'reddit', false, 'always', '', '{}'::jsonb, NULL::timestamptz),
      ('pinterest', 'Pinterest pravilo', 'Izradi pin iz vizuala i članka.', 'channel', 'pinterest', false, 'always', '', '{}'::jsonb, NULL::timestamptz),
      ('threads', 'Threads pravilo', 'Pripremi razgovornu verziju.', 'channel', 'threads', false, 'always', '', '{}'::jsonb, NULL::timestamptz),
      ('telegram', 'Telegram pravilo', 'Objavi sažetak u odabrani kanal.', 'channel', 'telegram', true, 'conditional', '', '{}'::jsonb, NULL::timestamptz),
      ('blog', 'Blog pravilo', 'Izradi puni članak i pripremi objavu na web.', 'channel', 'blog', true, 'always', '', '{}'::jsonb, NULL::timestamptz),
      ('newsletter', 'Newsletter pravilo', 'Pripremi tjedni pregled aktivnoj listi.', 'channel', 'newsletter', true, 'always', 'FREQ=WEEKLY;BYDAY=FR;BYHOUR=10', '{}'::jsonb,
       (CASE
          WHEN date_trunc('week', now() AT TIME ZONE profile.timezone) + interval '4 days 10 hours' > now() AT TIME ZONE profile.timezone
            THEN date_trunc('week', now() AT TIME ZONE profile.timezone) + interval '4 days 10 hours'
          ELSE date_trunc('week', now() AT TIME ZONE profile.timezone) + interval '11 days 10 hours'
        END) AT TIME ZONE profile.timezone)
) AS seed(rule_key, name, description, kind, channel, enabled, review_policy, schedule_rule, configuration, next_run_at)
ON CONFLICT (project_id, rule_key) DO NOTHING;

INSERT INTO audience_lists (project_id, name, description, is_default)
SELECT id, 'Aktivni pretplatnici', 'Glavna newsletter lista projekta.', true
FROM projects
ON CONFLICT (project_id, name) DO NOTHING;

INSERT INTO audience_contacts (
    project_id, list_id, first_name, last_name, email, source, status, consent, subscribed_at, metadata
)
SELECT project.id, list.id, seed.first_name, seed.last_name, seed.email,
       seed.source, seed.status, seed.consent, seed.subscribed_at, '{"seeded":true}'::jsonb
FROM projects AS project
JOIN audience_lists AS list ON list.project_id = project.id AND list.is_default
CROSS JOIN LATERAL (
    VALUES
      ('Ana', 'Kovač', 'ana.kovac@example.com', 'website', 'active', true, now() - interval '3 hours'),
      ('Marko', 'Marić', 'marko@primjer.hr', 'csv', 'active', true, now() - interval '1 day'),
      ('Lucija', 'Perić', 'lucija@studio.hr', 'manual', 'pending', false, now() - interval '12 days')
) AS seed(first_name, last_name, email, source, status, consent, subscribed_at)
WHERE project.slug = 'millena-demo'
ON CONFLICT (project_id, lower(email)) DO NOTHING;

INSERT INTO channel_connections (
    project_id, provider, mode, display_name, account_handle, endpoint_url,
    status, metadata, last_checked_at
)
SELECT project.id, seed.provider, 'sandbox', seed.display_name, seed.account_handle,
       seed.endpoint_url, 'connected', '{"seeded":true,"localOnly":true}'::jsonb, now()
FROM projects AS project
CROSS JOIN LATERAL (
    VALUES
      ('whatsapp', 'MPR WhatsApp Business', '+385 91 555 0123', ''),
      ('telegram', 'Millena AI bot', '@millena_ai_bot', ''),
      ('website', 'MPR web', 'mpr.hr', 'https://mpr.hr'),
      ('newsletter', 'MPR Newsletter', 'Aktivni pretplatnici', '')
) AS seed(provider, display_name, account_handle, endpoint_url)
WHERE project.slug = 'millena-demo'
ON CONFLICT (project_id, provider) DO NOTHING;

INSERT INTO assistant_threads (project_id, title, channel, created_by)
SELECT project.id, 'Glavni razgovor', 'app', member.user_id
FROM projects AS project
LEFT JOIN LATERAL (
    SELECT user_id FROM project_members
    WHERE project_id = project.id AND status = 'active'
    ORDER BY role = 'owner' DESC, created_at
    LIMIT 1
) AS member ON true
WHERE project.slug = 'millena-demo'
  AND NOT EXISTS (SELECT 1 FROM assistant_threads WHERE project_id = project.id);

INSERT INTO assistant_messages (thread_id, project_id, role, body, metadata)
SELECT thread.id, thread.project_id, 'assistant',
       'Bok! Koristim spremljenu strategiju, sadržaj, kalendar, publiku i pravila projekta. Mogu pripremiti nacrt, sažeti stanje ili pokrenuti lokalnu automatizaciju bez API ključa.',
       '{"seeded":true}'::jsonb
FROM assistant_threads AS thread
WHERE thread.title = 'Glavni razgovor'
  AND NOT EXISTS (SELECT 1 FROM assistant_messages WHERE thread_id = thread.id);
