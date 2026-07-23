CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 2 AND 120),
    slug TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE projects
    ADD COLUMN organization_id UUID REFERENCES organizations(id) ON DELETE RESTRICT;

CREATE TEMP TABLE organization_project_backfill (
    project_id UUID PRIMARY KEY,
    organization_id UUID NOT NULL
) ON COMMIT DROP;

INSERT INTO organization_project_backfill (project_id, organization_id)
SELECT id, gen_random_uuid() FROM projects;

INSERT INTO organizations (id, name, slug, status, created_at, updated_at)
SELECT mapping.organization_id, project.name, project.slug,
       CASE project.status WHEN 'active' THEN 'active' ELSE 'closed' END,
       project.created_at, project.updated_at
FROM organization_project_backfill AS mapping
JOIN projects AS project ON project.id = mapping.project_id;

UPDATE projects AS project
SET organization_id = mapping.organization_id
FROM organization_project_backfill AS mapping
WHERE mapping.project_id = project.id;

ALTER TABLE projects
    ALTER COLUMN organization_id SET NOT NULL;

CREATE INDEX projects_organization_idx ON projects (organization_id, status, created_at);

CREATE TABLE organization_members (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id)
);

CREATE INDEX organization_members_user_idx
    ON organization_members (user_id, status, organization_id);

INSERT INTO organization_members (organization_id, user_id, role, status, created_at, updated_at)
SELECT project.organization_id, member.user_id,
       CASE member.role WHEN 'owner' THEN 'owner' WHEN 'lead' THEN 'admin' ELSE 'member' END,
       member.status, member.created_at, member.created_at
FROM project_members AS member
JOIN projects AS project ON project.id = member.project_id
ON CONFLICT (organization_id, user_id) DO UPDATE
SET role = CASE
        WHEN organization_members.role = 'owner' OR EXCLUDED.role = 'owner' THEN 'owner'
        WHEN organization_members.role = 'admin' OR EXCLUDED.role = 'admin' THEN 'admin'
        ELSE 'member'
    END,
    status = CASE WHEN organization_members.status = 'active' OR EXCLUDED.status = 'active' THEN 'active' ELSE 'suspended' END,
    updated_at = now();

CREATE TABLE organization_entitlements (
    organization_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    plan_code TEXT NOT NULL REFERENCES plan_catalog(code),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('trial', 'active', 'past_due', 'cancelled')),
    monthly_publication_limit INTEGER CHECK (monthly_publication_limit IS NULL OR monthly_publication_limit > 0),
    storage_limit_bytes BIGINT CHECK (storage_limit_bytes IS NULL OR storage_limit_bytes > 0),
    features JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(features) = 'object'),
    renews_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO organization_entitlements (
    organization_id, plan_code, status, monthly_publication_limit,
    storage_limit_bytes, features, renews_at, created_at, updated_at
)
SELECT project.organization_id, entitlement.plan_code, entitlement.status,
       entitlement.monthly_publication_limit, entitlement.storage_limit_bytes,
       entitlement.features, entitlement.renews_at, entitlement.created_at, entitlement.updated_at
FROM project_entitlements AS entitlement
JOIN projects AS project ON project.id = entitlement.project_id
ON CONFLICT (organization_id) DO NOTHING;

INSERT INTO organization_entitlements (
    organization_id, plan_code, status, monthly_publication_limit,
    storage_limit_bytes, features
)
SELECT organization.id, plan.code, 'active', plan.monthly_publication_limit,
       plan.storage_limit_bytes, plan.features
FROM organizations AS organization
JOIN plan_catalog AS plan ON plan.code='starter'
WHERE NOT EXISTS (
    SELECT 1 FROM organization_entitlements AS entitlement
    WHERE entitlement.organization_id=organization.id
);

-- Public packages are differentiated by publication volume and capabilities,
-- not by seats. Every tenant may invite an unlimited number of users.
UPDATE plan_catalog
SET seat_limit = NULL,
    description = CASE code
        WHEN 'starter' THEN 'Do 30 objava mjesečno i neograničen broj korisnika.'
        WHEN 'optimum' THEN 'Do 100 objava mjesečno, napredni workflow i neograničen broj korisnika.'
        WHEN 'unlimited' THEN 'Svi moduli te neograničen broj objava i korisnika.'
        ELSE description
    END,
    updated_at = now()
WHERE is_system AND code IN ('starter', 'optimum', 'unlimited');

UPDATE project_entitlements SET seat_limit = NULL, updated_at = now();
