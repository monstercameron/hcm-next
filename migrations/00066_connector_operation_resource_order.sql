-- Owner: connectivity lane. Phase: P1B.
-- storage-disposition: connector_operation_resource_order | authoritative per-resource causal cursor | local PostgreSQL | serialized resource lease commit | tenant-scoped.

-- +goose Up

CREATE TABLE connector_operation_resource_order (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    connection_id         uuid         NOT NULL,
    external_resource_key semantic_key NOT NULL,
    committed_sequence    bigint       NOT NULL DEFAULT 0,
    committed_operation_id uuid,
    writer_fence_epoch    bigint       NOT NULL DEFAULT 1,
    updated_at            timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, connection_id, external_resource_key),
    FOREIGN KEY (tenant_id, connection_id) REFERENCES connector_connection (tenant_id, connection_id),
    FOREIGN KEY (tenant_id, committed_operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    CONSTRAINT connector_operation_order_sequence_nonnegative CHECK (committed_sequence >= 0),
    CONSTRAINT connector_operation_order_fence_positive CHECK (writer_fence_epoch >= 1)
);

CREATE INDEX connector_operation_resource_order_ready
    ON connector_operation_resource_order (tenant_id, connection_id, updated_at);

ALTER TABLE connector_operation_resource_order ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_resource_order FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation_resource_order
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE DELETE ON connector_operation_resource_order FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON connector_operation_resource_order TO hcmnext_app;

-- +goose Down

REVOKE ALL ON connector_operation_resource_order FROM hcmnext_app;
DROP POLICY tenant_isolation ON connector_operation_resource_order;
ALTER TABLE connector_operation_resource_order NO FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_resource_order DISABLE ROW LEVEL SECURITY;
DROP TABLE connector_operation_resource_order;
