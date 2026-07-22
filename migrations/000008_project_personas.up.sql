CREATE TABLE project_personas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    demographics TEXT NOT NULL DEFAULT '' CHECK (char_length(demographics) <= 500),
    is_primary BOOLEAN NOT NULL DEFAULT false,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_personas_project_id_id_key UNIQUE (project_id, id)
);

CREATE UNIQUE INDEX project_personas_project_name_idx
    ON project_personas (project_id, lower(name));

CREATE UNIQUE INDEX project_personas_primary_idx
    ON project_personas (project_id)
    WHERE is_primary;

CREATE INDEX project_personas_project_time_idx
    ON project_personas (project_id, created_at);

WITH ranked_active_requests AS (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY project_id, request_type
               ORDER BY updated_at DESC, created_at DESC, id DESC
           ) AS retained_id,
           row_number() OVER (
               PARTITION BY project_id, request_type
               ORDER BY updated_at DESC, created_at DESC, id DESC
           ) AS position
    FROM service_requests
    WHERE status IN ('open', 'in_progress')
)
UPDATE service_requests AS request
SET status = 'cancelled',
    metadata = request.metadata || jsonb_build_object(
        'deduplicatedByMigration', '000008_project_personas',
        'retainedRequestId', ranked.retained_id
    ),
    updated_at = now()
FROM ranked_active_requests AS ranked
WHERE request.id = ranked.id
  AND ranked.position > 1;

CREATE UNIQUE INDEX service_requests_project_active_type_idx
    ON service_requests (project_id, request_type)
    WHERE status IN ('open', 'in_progress');

INSERT INTO project_personas (
    project_id, name, description, demographics, is_primary, metadata, created_by
)
SELECT strategy.project_id,
       left(strategy.audience, 120),
       left(strategy.audience_problem, 1000),
       '',
       true,
       '{"migratedFromStrategy":true}'::jsonb,
       strategy.updated_by
FROM project_strategies AS strategy
WHERE char_length(trim(strategy.audience)) >= 2
ON CONFLICT DO NOTHING;
