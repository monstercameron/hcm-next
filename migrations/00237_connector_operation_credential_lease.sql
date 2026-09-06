-- Owner: connectivity lane. Phase: Gate B.
-- storage-disposition: connector_operation_credential_lease | authoritative append-only dispatch credential binding evidence | local PostgreSQL | short-lived lease binding and refusal audit | tenant-scoped.

-- +goose Up

CREATE TABLE connector_operation_credential_lease (
    tenant_id            tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    binding_id           uuid         NOT NULL,
    operation_id         uuid         NOT NULL,
    attempt_id           uuid,
    credential_lease_ref semantic_key NOT NULL,
    workload_ref         semantic_key NOT NULL,
    destination_ref      semantic_key NOT NULL,
    purpose              semantic_key NOT NULL,
    custody_operation    semantic_key NOT NULL,
    lease_expires_at     timestamptz  NOT NULL,
    revocation_epoch     bigint       NOT NULL,
    outcome              text         NOT NULL,
    refusal_field        text         NOT NULL DEFAULT '',
    bound_at             timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, binding_id),
    FOREIGN KEY (tenant_id, operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    FOREIGN KEY (tenant_id, attempt_id) REFERENCES connector_operation_attempt (tenant_id, attempt_id),
    CONSTRAINT connector_operation_credential_lease_outcome_allowed CHECK (outcome IN ('BOUND', 'REJECTED')),
    CONSTRAINT connector_operation_credential_lease_epoch_nonnegative CHECK (revocation_epoch >= 0),
    CONSTRAINT connector_operation_credential_lease_expiry_after_binding CHECK (outcome <> 'BOUND' OR lease_expires_at > bound_at)
);

CREATE INDEX connector_operation_credential_lease_operation
    ON connector_operation_credential_lease (tenant_id, operation_id, bound_at, binding_id);

CREATE TRIGGER connector_operation_credential_lease_append_only
    BEFORE UPDATE OR DELETE ON connector_operation_credential_lease
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE connector_operation_credential_lease ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_credential_lease FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation_credential_lease
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON connector_operation_credential_lease FROM PUBLIC;
GRANT SELECT, INSERT ON connector_operation_credential_lease TO hcmnext_app;

-- +goose Down

REVOKE ALL ON connector_operation_credential_lease FROM hcmnext_app;
DROP POLICY tenant_isolation ON connector_operation_credential_lease;
ALTER TABLE connector_operation_credential_lease NO FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_credential_lease DISABLE ROW LEVEL SECURITY;
DROP TABLE connector_operation_credential_lease;
