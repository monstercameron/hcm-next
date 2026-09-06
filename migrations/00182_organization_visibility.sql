-- Owner: product experience. Phase: production frontend.
-- storage-disposition: organization directory visibility | organization-scoped policy | local PostgreSQL | tenant-local ACID/CAS | admin governed.

-- +goose Up

CREATE TABLE IF NOT EXISTS organization_visibility_preference (
    tenant_id             tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    organization_scope_id text NOT NULL CONSTRAINT organization_visibility_scope_nonempty CHECK (organization_scope_id <> ''),
    version               cas_version NOT NULL,
    policy                jsonb NOT NULL CHECK (jsonb_typeof(policy) = 'object'),
    updated_by            text NOT NULL CHECK (updated_by <> ''),
    updated_at            timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, organization_scope_id)
);

ALTER TABLE organization_visibility_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_visibility_preference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_visibility_preference
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON organization_visibility_preference TO hcmnext_app;

-- +goose Down

REVOKE ALL ON organization_visibility_preference FROM hcmnext_app;
DROP POLICY tenant_isolation ON organization_visibility_preference;
ALTER TABLE organization_visibility_preference NO FORCE ROW LEVEL SECURITY;
ALTER TABLE organization_visibility_preference DISABLE ROW LEVEL SECURITY;
DROP TABLE organization_visibility_preference;
