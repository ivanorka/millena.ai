CREATE OR REPLACE FUNCTION millena_assign_project_organization()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS NULL THEN
        INSERT INTO organizations (name,slug,status)
        VALUES (NEW.name,NEW.slug,CASE NEW.status WHEN 'active' THEN 'active' ELSE 'closed' END)
        ON CONFLICT (slug) DO UPDATE SET updated_at=now()
        RETURNING id INTO NEW.organization_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER projects_assign_organization
BEFORE INSERT ON projects
FOR EACH ROW EXECUTE FUNCTION millena_assign_project_organization();

CREATE OR REPLACE FUNCTION millena_sync_project_member_organization()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE tenant_id UUID;
BEGIN
    SELECT organization_id INTO tenant_id FROM projects WHERE id=NEW.project_id;
    INSERT INTO organization_members (organization_id,user_id,role,status)
    VALUES (
        tenant_id,NEW.user_id,
        CASE NEW.role WHEN 'owner' THEN 'owner' WHEN 'lead' THEN 'admin' ELSE 'member' END,
        NEW.status
    )
    ON CONFLICT (organization_id,user_id) DO NOTHING;
    RETURN NEW;
END;
$$;

CREATE TRIGGER project_members_sync_organization
AFTER INSERT ON project_members
FOR EACH ROW EXECUTE FUNCTION millena_sync_project_member_organization();

CREATE OR REPLACE FUNCTION millena_sync_project_entitlement_organization()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE tenant_id UUID;
BEGIN
    SELECT organization_id INTO tenant_id FROM projects WHERE id=NEW.project_id;
    INSERT INTO organization_entitlements (
        organization_id,plan_code,status,monthly_publication_limit,
        storage_limit_bytes,features,renews_at
    )
    VALUES (
        tenant_id,NEW.plan_code,NEW.status,NEW.monthly_publication_limit,
        NEW.storage_limit_bytes,NEW.features,NEW.renews_at
    )
    ON CONFLICT (organization_id) DO UPDATE SET
        plan_code=EXCLUDED.plan_code,status=EXCLUDED.status,
        monthly_publication_limit=EXCLUDED.monthly_publication_limit,
        storage_limit_bytes=EXCLUDED.storage_limit_bytes,features=EXCLUDED.features,
        renews_at=EXCLUDED.renews_at,updated_at=now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER project_entitlements_sync_organization
AFTER INSERT OR UPDATE OF plan_code,status,monthly_publication_limit,storage_limit_bytes,features,renews_at
ON project_entitlements
FOR EACH ROW EXECUTE FUNCTION millena_sync_project_entitlement_organization();
