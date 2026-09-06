-- Owner: product experience. Phase: production frontend.
-- storage-disposition: presentation preferences | principal defaults and organization branding | local PostgreSQL | tenant-local ACID/CAS | principal/org-scoped.

-- +goose Up

CREATE TABLE IF NOT EXISTS user_presentation_preference (
    tenant_id      tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    principal_id   text NOT NULL CHECK (principal_id <> ''),
    version        cas_version NOT NULL,
    preferences    jsonb NOT NULL CHECK (jsonb_typeof(preferences) = 'object'),
    updated_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, principal_id)
);

CREATE TABLE IF NOT EXISTS tenant_appearance_preference (
    tenant_id             tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    organization_scope_id text NOT NULL CONSTRAINT tenant_appearance_organization_scope_nonempty CHECK (organization_scope_id <> ''),
    version               cas_version NOT NULL,
    theme                 jsonb NOT NULL CHECK (jsonb_typeof(theme) = 'object'),
    updated_by            text NOT NULL CHECK (updated_by <> ''),
    updated_at            timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, organization_scope_id)
);

ALTER TABLE user_presentation_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_presentation_preference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON user_presentation_preference
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE tenant_appearance_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_appearance_preference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_appearance_preference
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON user_presentation_preference TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON tenant_appearance_preference TO hcmnext_app;

-- +goose Down

REVOKE ALL ON tenant_appearance_preference FROM hcmnext_app;
REVOKE ALL ON user_presentation_preference FROM hcmnext_app;
DROP POLICY tenant_isolation ON tenant_appearance_preference;
DROP POLICY tenant_isolation ON user_presentation_preference;
ALTER TABLE tenant_appearance_preference NO FORCE ROW LEVEL SECURITY;
ALTER TABLE user_presentation_preference NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_appearance_preference DISABLE ROW LEVEL SECURITY;
ALTER TABLE user_presentation_preference DISABLE ROW LEVEL SECURITY;
DROP TABLE tenant_appearance_preference;
DROP TABLE user_presentation_preference;
