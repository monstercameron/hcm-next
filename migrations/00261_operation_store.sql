-- Owner: experience-and-transport data adapter. Phase: P1A.
-- EP-OPS-001: durable owner-scoped long-running operation state.
-- storage-disposition: operation is mutable serving state; operation_cancel is
-- append-only idempotency evidence for cancellation requests.

-- +goose Up

CREATE TABLE IF NOT EXISTS operation (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    operation_id    semantic_key NOT NULL,
    owner_subject   semantic_key NOT NULL,
    request_type    text         NOT NULL DEFAULT '',
    state           text         NOT NULL,
    result_bytes    bytea,
    error_bytes     bytea,
    metadata_ref    text         NOT NULL DEFAULT '',
    created_at      timestamptz  NOT NULL,
    updated_at      timestamptz  NOT NULL,
    fence_token     cas_version  NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id, operation_id),
    CONSTRAINT operation_state_allowed CHECK (
        state IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED',
                  'CANCELLATION_REQUESTED', 'CANCELLED')
    ),
    CONSTRAINT operation_result_only_on_success CHECK (
        result_bytes IS NULL OR state = 'SUCCEEDED'
    ),
    CONSTRAINT operation_error_only_on_failure CHECK (
        error_bytes IS NULL OR state = 'FAILED'
    ),
    CONSTRAINT operation_updated_after_created CHECK (updated_at >= created_at)
);

CREATE INDEX IF NOT EXISTS operation_owner_lookup
    ON operation (tenant_id, owner_subject, updated_at, operation_id);

CREATE TABLE IF NOT EXISTS operation_cancel (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    operation_id    semantic_key NOT NULL,
    idempotency_key semantic_key NOT NULL,
    reason_ref      text         NOT NULL DEFAULT '',
    requested_at    timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, operation_id, idempotency_key),
    FOREIGN KEY (tenant_id, operation_id)
        REFERENCES operation (tenant_id, operation_id)
);

CREATE TRIGGER operation_cancel_append_only BEFORE UPDATE OR DELETE ON operation_cancel
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE operation ENABLE ROW LEVEL SECURITY;
ALTER TABLE operation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON operation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE operation_cancel ENABLE ROW LEVEL SECURITY;
ALTER TABLE operation_cancel FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON operation_cancel
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON operation TO hcmnext_app;
GRANT SELECT, INSERT ON operation_cancel TO hcmnext_app;
REVOKE UPDATE, DELETE ON operation_cancel FROM PUBLIC;

-- +goose Down

REVOKE ALL ON operation_cancel FROM hcmnext_app;
REVOKE ALL ON operation FROM hcmnext_app;
DROP TABLE operation_cancel;
DROP TABLE operation;
