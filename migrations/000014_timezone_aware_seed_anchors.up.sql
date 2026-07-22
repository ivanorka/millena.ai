-- Repair only untouched operational defaults. The strict identity/configuration
-- predicates and created_at = updated_at guard preserve rules that an operator
-- has edited, disabled, cleared or already executed.
WITH project_clock AS (
    SELECT profile.project_id,
           profile.timezone,
           now() AT TIME ZONE profile.timezone AS local_now
    FROM project_profiles AS profile
    WHERE EXISTS (
        SELECT 1
        FROM pg_timezone_names AS zone
        WHERE zone.name = profile.timezone
    )
),
project_anchors AS (
    SELECT project_id,
           (CASE
              WHEN date_trunc('day', local_now) + interval '10 hours' > local_now
                THEN date_trunc('day', local_now) + interval '10 hours'
              ELSE date_trunc('day', local_now) + interval '1 day 10 hours'
            END) AT TIME ZONE timezone AS gap_next_run_at,
           (CASE
              WHEN date_trunc('week', local_now) + interval '4 days 10 hours' > local_now
                THEN date_trunc('week', local_now) + interval '4 days 10 hours'
              ELSE date_trunc('week', local_now) + interval '11 days 10 hours'
            END) AT TIME ZONE timezone AS weekly_next_run_at
    FROM project_clock
),
eligible_rules AS (
    SELECT rule.id,
           CASE rule.rule_key
             WHEN 'calendar_gap' THEN anchor.gap_next_run_at
             ELSE anchor.weekly_next_run_at
           END AS next_run_at
    FROM automation_rules AS rule
    JOIN project_anchors AS anchor ON anchor.project_id = rule.project_id
    WHERE rule.run_count = 0
      AND rule.last_run_at IS NULL
      AND rule.next_run_at IS NOT NULL
      AND rule.created_at = rule.updated_at
      AND (
        (
          rule.rule_key = 'calendar_gap'
          AND rule.kind = 'calendar_gap'
          AND rule.channel = 'linkedin'
          AND rule.enabled
          AND rule.review_policy = 'always'
          AND rule.schedule_rule = 'gap:5d'
          AND rule.configuration = '{"gapDays":5}'::jsonb
        )
        OR (
          rule.rule_key = 'weekly_newsletter'
          AND rule.kind = 'newsletter'
          AND rule.channel = 'newsletter'
          AND rule.enabled
          AND rule.review_policy = 'always'
          AND rule.schedule_rule = 'FREQ=WEEKLY;BYDAY=FR;BYHOUR=10'
          AND rule.configuration = '{"weekday":"friday","hour":10}'::jsonb
        )
        OR (
          rule.rule_key = 'newsletter'
          AND rule.kind = 'channel'
          AND rule.channel = 'newsletter'
          AND rule.enabled
          AND rule.review_policy = 'always'
          AND rule.schedule_rule = 'FREQ=WEEKLY;BYDAY=FR;BYHOUR=10'
          AND rule.configuration = '{}'::jsonb
        )
      )
)
UPDATE automation_rules AS rule
SET next_run_at = eligible.next_run_at,
    updated_at = clock_timestamp()
FROM eligible_rules AS eligible
WHERE rule.id = eligible.id;
