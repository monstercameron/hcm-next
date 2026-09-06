-- Owner: trust and product experience. Phase: production frontend.
-- storage-disposition: page action authorization | per-role product page CRUD grants | local PostgreSQL | tenant-local ACID/CAS | admin governed.

-- +goose Up

CREATE TABLE role_page_permission (
    tenant_id  tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    role_id    text NOT NULL,
    page_id    text NOT NULL CHECK (page_id ~ '^[a-z][a-z0-9-]{1,63}$'),
    version    cas_version NOT NULL,
    can_view   boolean NOT NULL DEFAULT false,
    can_create boolean NOT NULL DEFAULT false,
    can_update boolean NOT NULL DEFAULT false,
    can_delete boolean NOT NULL DEFAULT false,
    updated_by text NOT NULL CHECK (updated_by <> ''),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, role_id, page_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES access_role (tenant_id, role_id) ON DELETE CASCADE,
    CHECK (can_view OR NOT (can_create OR can_update OR can_delete))
);

ALTER TABLE role_page_permission ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_page_permission FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON role_page_permission
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON role_page_permission TO hcmnext_app;

-- +goose Down

DROP TABLE role_page_permission;
