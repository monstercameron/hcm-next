-- Owner: trust/attest and attestationstore. Phase: ATTEST-004 through ATTEST-008.
--
-- The response is an append-only, tenant-scoped evidence stream. The trusted
-- time receipt is stored with its source evidence; response revisions never
-- update or delete an earlier accepted, refused, unknown, corrective or
-- revocation assertion.
--
-- Storage disposition: attestation_response is a LEDGER/PERMANENT record.
-- It is not rebuildable from another table (rebuild_source: null).

-- +goose Up

CREATE TABLE IF NOT EXISTS attestation_response (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id               uuid            NOT NULL,
    response_id          semantic_key   NOT NULL,
    revision             cas_version     NOT NULL,
    statement_id         semantic_key   NOT NULL,
    statement_version    cas_version     NOT NULL,
    statement_digest     content_digest  NOT NULL,
    binding_digest       content_digest  NOT NULL,
    status               text            NOT NULL,
    kind                 text            NOT NULL,
    reason               text,
    evidence_receipt     content_digest  NOT NULL,
    idempotency_key      semantic_key    NOT NULL,
    corrects_response_id semantic_key,
    authority_ref        text,
    affected_obligations jsonb           NOT NULL DEFAULT '[]'::jsonb,
    transaction_id       text,
    trusted_at           timestamptz     NOT NULL,
    trusted_time         jsonb           NOT NULL,
    request_digest       content_digest  NOT NULL,
    digest               content_digest  NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, response_id, revision),
    UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT attestation_response_revision_positive CHECK (revision >= 1),
    CONSTRAINT attestation_response_statement_version_positive CHECK (statement_version >= 1),
    CONSTRAINT attestation_response_status_declared CHECK (status IN ('ACCEPTED', 'REFUSED', 'UNKNOWN')),
    CONSTRAINT attestation_response_kind_declared CHECK (kind IN ('RESPONSE', 'CORRECTION', 'REVOCATION'))
);

CREATE INDEX IF NOT EXISTS attestation_response_history
    ON attestation_response (tenant_id, response_id, revision DESC);

CREATE OR REPLACE TRIGGER attestation_response_append_only
    BEFORE UPDATE OR DELETE ON attestation_response
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON attestation_response FROM PUBLIC;

ALTER TABLE attestation_response ENABLE ROW LEVEL SECURITY;
ALTER TABLE attestation_response FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON attestation_response
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON attestation_response TO hcmnext_app;

-- +goose Down

REVOKE ALL ON attestation_response FROM hcmnext_app;
DROP TABLE attestation_response;

