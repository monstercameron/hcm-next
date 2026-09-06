-- Owner: trust and product experience. Phase: production frontend.
-- storage-disposition: role authorization | tenant-scoped role catalog, employee assignments, and org visibility | local PostgreSQL | tenant-local ACID/CAS | admin governed.

-- +goose Up

CREATE TABLE IF NOT EXISTS access_role (
    tenant_id   tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    role_id     text NOT NULL CHECK (role_id ~ '^[a-z][a-z0-9_]{1,62}$'),
    version     cas_version NOT NULL,
    name        text NOT NULL CHECK (name <> '' AND length(name) <= 96),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 400),
    system_role boolean NOT NULL DEFAULT false,
    active      boolean NOT NULL DEFAULT true,
    updated_by  text NOT NULL CHECK (updated_by <> ''),
    updated_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, role_id)
);

CREATE TABLE IF NOT EXISTS worker_access_role_set (
    tenant_id  tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    worker_ref text NOT NULL CHECK (worker_ref <> ''),
    version    cas_version NOT NULL,
    updated_by text NOT NULL CHECK (updated_by <> ''),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, worker_ref)
);

CREATE TABLE IF NOT EXISTS worker_access_role_assignment (
    tenant_id  tenant_ref NOT NULL,
    worker_ref text NOT NULL,
    role_id    text NOT NULL,
    assigned_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, worker_ref, role_id),
    FOREIGN KEY (tenant_id, worker_ref) REFERENCES worker_access_role_set (tenant_id, worker_ref) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id) REFERENCES access_role (tenant_id, role_id)
);

CREATE TABLE IF NOT EXISTS role_organization_visibility (
    tenant_id             tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    organization_scope_id text NOT NULL CHECK (organization_scope_id <> ''),
    role_id               text NOT NULL,
    version               cas_version NOT NULL,
    mode                  text NOT NULL CHECK (mode IN ('ALL','OWN_UNIT','ALLOWLIST','DENYLIST')),
    organization_units    text[] NOT NULL DEFAULT '{}',
    updated_by            text NOT NULL CHECK (updated_by <> ''),
    updated_at            timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, organization_scope_id, role_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES access_role (tenant_id, role_id)
);

ALTER TABLE access_role ENABLE ROW LEVEL SECURITY;
ALTER TABLE access_role FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON access_role USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE worker_access_role_set ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_access_role_set FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worker_access_role_set USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE worker_access_role_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_access_role_assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worker_access_role_assignment USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE role_organization_visibility ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_organization_visibility FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON role_organization_visibility USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON access_role, worker_access_role_set, role_organization_visibility TO hcmnext_app;
GRANT SELECT, INSERT, DELETE ON worker_access_role_assignment TO hcmnext_app;

-- +goose Down

DROP TABLE role_organization_visibility;
DROP TABLE worker_access_role_assignment;
DROP TABLE worker_access_role_set;
DROP TABLE access_role;
