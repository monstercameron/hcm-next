-- Owner: connectivity lane. Phase: Gate B.
-- storage-disposition: connector_operation_journal | authoritative append-only operation transition and recovery evidence | local PostgreSQL | operation replay, crash recovery and audit | tenant-scoped.

-- +goose Up

CREATE TABLE connector_operation_journal (
    tenant_id           tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    journal_id          uuid         NOT NULL,
    operation_id        uuid         NOT NULL,
    operation_sequence  bigint       NOT NULL,
    event_kind          semantic_key NOT NULL,
    from_state          text         NOT NULL DEFAULT '',
    to_state            text         NOT NULL,
    attempt_id          uuid,
    fence_token         bigint       NOT NULL DEFAULT 0,
    credential_lease_ref text         NOT NULL DEFAULT '',
    request_digest      content_digest,
    response_digest     content_digest,
    previous_digest     content_digest NOT NULL DEFAULT repeat('0', 64),
    event_digest        content_digest NOT NULL,
    occurred_at         timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, journal_id),
    UNIQUE (tenant_id, operation_id, operation_sequence),
    FOREIGN KEY (tenant_id, operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    FOREIGN KEY (tenant_id, attempt_id) REFERENCES connector_operation_attempt (tenant_id, attempt_id),
    CONSTRAINT connector_operation_journal_sequence_positive CHECK (operation_sequence >= 1),
    CONSTRAINT connector_operation_journal_fence_nonnegative CHECK (fence_token >= 0),
    CONSTRAINT connector_operation_journal_state_present CHECK (length(to_state) > 0),
    CONSTRAINT connector_operation_journal_digest_present CHECK (length(event_digest) > 0)
);

CREATE INDEX connector_operation_journal_operation
    ON connector_operation_journal (tenant_id, operation_id, occurred_at, journal_id);

CREATE TRIGGER connector_operation_journal_append_only
    BEFORE UPDATE OR DELETE ON connector_operation_journal
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE connector_operation_journal ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_journal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation_journal
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON connector_operation_journal FROM PUBLIC;
GRANT SELECT, INSERT ON connector_operation_journal TO hcmnext_app;

-- +goose Down

REVOKE ALL ON connector_operation_journal FROM hcmnext_app;
DROP POLICY tenant_isolation ON connector_operation_journal;
ALTER TABLE connector_operation_journal NO FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_journal DISABLE ROW LEVEL SECURITY;
DROP TABLE connector_operation_journal;
