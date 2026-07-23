DROP TRIGGER IF EXISTS project_entitlements_sync_organization ON project_entitlements;
DROP FUNCTION IF EXISTS millena_sync_project_entitlement_organization();
DROP TRIGGER IF EXISTS project_members_sync_organization ON project_members;
DROP FUNCTION IF EXISTS millena_sync_project_member_organization();
DROP TRIGGER IF EXISTS projects_assign_organization ON projects;
DROP FUNCTION IF EXISTS millena_assign_project_organization();
