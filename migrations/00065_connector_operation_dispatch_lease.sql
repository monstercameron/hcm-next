-- Owner: connectivity lane. Phase: P1B.
-- storage-disposition: connector_operation_dispatch_lease | authoritative mutable dispatch lease/fence | local PostgreSQL | lease acquisition and fenced acknowledgement | tenant-scoped.

-- +goose Up

CREATE TABLE connector_operation_dispatch_lease (
    tenant_id    tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    operation_id uuid        NOT NULL,
    lease_token  uuid        NOT NULL,
    fence_token  bigint      NOT NULL,
    worker_ref   semantic_key NOT NULL,
    leased_at    timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, operation_id),
    FOREIGN KEY (tenant_id, operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    CONSTRAINT connector_operation_lease_fence_positive CHECK (fence_token >= 1),
    CONSTRAINT connector_operation_lease_expiry_after_lease CHECK (expires_at > leased_at)
);

CREATE INDEX connector_operation_lease_expiry
    ON connector_operation_dispatch_lease (tenant_id, expires_at);

ALTER TABLE connector_operation_dispatch_lease ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_dispatch_lease FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation_dispatch_lease
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON connector_operation_dispatch_lease FROM PUBLIC;
GRANT SELECT, INSERT ON connector_operation_dispatch_lease TO hcmnext_app;

-- +goose Down

REVOKE ALL ON connector_operation_dispatch_lease FROM hcmnext_app;
DROP POLICY tenant_isolation ON connector_operation_dispatch_lease;
ALTER TABLE connector_operation_dispatch_lease NO FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_dispatch_lease DISABLE ROW LEVEL SECURITY;
DROP TABLE connector_operation_dispatch_lease;
