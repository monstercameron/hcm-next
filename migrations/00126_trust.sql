-- Owner: trust/data lane. Phase: Gate B.
-- PERSIST-TRUST-001: durable step-up consumption, JIT grant state/evidence,
-- and periodic access-review evidence.
--
-- Storage disposition (STORE-001):
--   stepup_proof_log          LEDGER    PERMANENT   append-only
--   jit_grant                 CONTROL   OPERATIONAL revision-fenced state
--   jit_evidence_record       LEDGER    PERMANENT   append-only
--   accessreview_grant        CONTROL   OPERATIONAL current grant state
--   accessreview_schedule     CONTROL   OPERATIONAL current schedule state
--   accessreview_review_record LEDGER   PERMANENT   append-only
--
-- The three ledger relations carry the forbid_mutation trigger. The three
-- control relations are caller-driven state and retain UPDATE but never
-- DELETE. Every relation is tenant scoped and is fail-closed without the
-- transaction-local app.tenant_id set by internal/data/tenancy.WithTenant.

-- +goose Up

CREATE TABLE IF NOT EXISTS stepup_proof_log (
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid        NOT NULL DEFAULT gen_random_uuid(),
    proof_id       text        NOT NULL,
    outcome        text        NOT NULL,
    consumed_at    timestamptz NOT NULL,
    event_sequence bigint      NOT NULL DEFAULT 1,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT stepup_proof_log_proof_unique UNIQUE (tenant_id, proof_id),
    CONSTRAINT stepup_proof_log_proof_not_blank CHECK (proof_id <> ''),
    CONSTRAINT stepup_proof_log_outcome_not_blank CHECK (outcome <> ''),
    CONSTRAINT stepup_proof_log_sequence_positive CHECK (event_sequence >= 1)
);

CREATE OR REPLACE TRIGGER stepup_proof_log_append_only
    BEFORE UPDATE OR DELETE ON stepup_proof_log
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON stepup_proof_log FROM PUBLIC;

CREATE TABLE IF NOT EXISTS jit_grant (
    tenant_id   tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id      uuid        NOT NULL,
    grant_id    semantic_key NOT NULL,
    revision    cas_version NOT NULL,
    state       text        NOT NULL,
    requester   text        NOT NULL,
    approver    text,
    scope       jsonb       NOT NULL,
    not_before  timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,
    revoked     boolean     NOT NULL DEFAULT false,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT jit_grant_revision_unique UNIQUE (tenant_id, grant_id, revision),
    CONSTRAINT jit_grant_state_not_blank CHECK (state <> ''),
    CONSTRAINT jit_grant_requester_not_blank CHECK (requester <> ''),
    CONSTRAINT jit_grant_window_valid CHECK (expires_at > not_before)
);

CREATE INDEX IF NOT EXISTS jit_grant_current_lookup
    ON jit_grant (tenant_id, grant_id, revision DESC);

CREATE TABLE IF NOT EXISTS jit_evidence_record (
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid        NOT NULL,
    grant_id       semantic_key NOT NULL,
    evidence_kind  text        NOT NULL,
    detail         jsonb,
    at             timestamptz NOT NULL,
    event_sequence bigint      NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT jit_evidence_record_sequence_unique
        UNIQUE (tenant_id, grant_id, event_sequence),
    CONSTRAINT jit_evidence_record_kind_not_blank CHECK (evidence_kind <> ''),
    CONSTRAINT jit_evidence_record_sequence_positive CHECK (event_sequence >= 1)
);

CREATE INDEX IF NOT EXISTS jit_evidence_record_history
    ON jit_evidence_record (tenant_id, grant_id, event_sequence);

CREATE OR REPLACE TRIGGER jit_evidence_record_append_only
    BEFORE UPDATE OR DELETE ON jit_evidence_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON jit_evidence_record FROM PUBLIC;

CREATE TABLE IF NOT EXISTS accessreview_grant (
    tenant_id    tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id       uuid        NOT NULL,
    grant_id     semantic_key NOT NULL,
    principal_id text        NOT NULL,
    capabilities jsonb       NOT NULL,
    granted_at   timestamptz NOT NULL,
    expires_at   timestamptz,
    revoked      boolean     NOT NULL DEFAULT false,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT accessreview_grant_id_unique UNIQUE (tenant_id, grant_id),
    CONSTRAINT accessreview_grant_principal_not_blank CHECK (principal_id <> ''),
    CONSTRAINT accessreview_grant_expiry_valid CHECK (
        expires_at IS NULL OR expires_at > granted_at
    )
);

CREATE TABLE IF NOT EXISTS accessreview_schedule (
    tenant_id  tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id     uuid          NOT NULL,
    schedule_id semantic_key NOT NULL,
    entries    jsonb         NOT NULL,
    digest     content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT accessreview_schedule_id_unique UNIQUE (tenant_id, schedule_id)
);

CREATE TABLE IF NOT EXISTS accessreview_review_record (
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid        NOT NULL,
    schedule_id    semantic_key NOT NULL,
    reviewer_id    text        NOT NULL,
    decision       text        NOT NULL,
    ticket_ref     text,
    justification  text,
    capabilities   jsonb,
    at             timestamptz NOT NULL,
    event_sequence bigint      NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT accessreview_review_sequence_unique
        UNIQUE (tenant_id, schedule_id, event_sequence),
    CONSTRAINT accessreview_review_reviewer_not_blank CHECK (reviewer_id <> ''),
    CONSTRAINT accessreview_review_decision_not_blank CHECK (decision <> ''),
    CONSTRAINT accessreview_review_sequence_positive CHECK (event_sequence >= 1)
);

CREATE INDEX IF NOT EXISTS accessreview_review_history
    ON accessreview_review_record (tenant_id, schedule_id, event_sequence);

CREATE OR REPLACE TRIGGER accessreview_review_record_append_only
    BEFORE UPDATE OR DELETE ON accessreview_review_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON accessreview_review_record FROM PUBLIC;

-- DB-017: all six relations use the same fail-closed tenant predicate.
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'stepup_proof_log',
        'jit_grant',
        'jit_evidence_record',
        'accessreview_grant',
        'accessreview_schedule',
        'accessreview_review_record'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I '
            'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
            'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
            target);
        EXECUTE format('REVOKE DELETE ON %I FROM PUBLIC', target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON
    stepup_proof_log,
    jit_evidence_record,
    accessreview_review_record
TO hcmnext_app;

GRANT SELECT, INSERT, UPDATE ON
    jit_grant,
    accessreview_grant,
    accessreview_schedule
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON
    accessreview_review_record,
    accessreview_schedule,
    accessreview_grant,
    jit_evidence_record,
    jit_grant,
    stepup_proof_log
FROM hcmnext_app;

DROP TABLE accessreview_review_record;
DROP TABLE accessreview_schedule;
DROP TABLE accessreview_grant;
DROP TABLE jit_evidence_record;
DROP TABLE jit_grant;
DROP TABLE stepup_proof_log;
