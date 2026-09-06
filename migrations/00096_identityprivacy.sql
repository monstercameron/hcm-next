-- Owner: identity and privacy data plane. Phase: P1B.
-- PERSIST-IDENTITYPRIVACY-001: durable proofing, pseudonym custody and
-- data-subject-request evidence. Protected payloads remain outside hcmnext_app.

-- +goose Up

-- The escrow service is a separate least-privilege role. It is created here so
-- this migration is self-contained in the same way 00008 creates hcmnext_app.
-- The role is NOLOGIN and is assumed only by the custody service's bootstrap
-- identity; it never becomes the ordinary application role.
-- +goose StatementBegin
DO $$
BEGIN
    CREATE ROLE hcmnext_custody
        NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
        NOLOGIN NOREPLICATION NOBYPASSRLS;
EXCEPTION
    WHEN duplicate_object OR unique_violation THEN
        NULL;
END
$$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS proofing_session (
    row_id              uuid          NOT NULL,
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    session_id          text          NOT NULL,
    subject_ref         uuid          NOT NULL,
    purpose             text,
    target_assurance    text          NOT NULL,
    evidence            jsonb,
    verifier_principal  text,
    outcome             text,
    expires_at          timestamptz,
    revision            bigint        NOT NULL,
    supersedes_revision bigint,
    canonical_digest    content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT proofing_session_revision_unique
        UNIQUE (tenant_id, session_id, revision),
    CONSTRAINT proofing_session_revision_positive CHECK (revision >= 1),
    CONSTRAINT proofing_session_lineage_valid CHECK (
        supersedes_revision IS NULL OR supersedes_revision < revision
    ),
    CONSTRAINT proofing_session_target_assurance_allowed CHECK (
        target_assurance IN ('IAL1', 'IAL2', 'IAL3')
    ),
    CONSTRAINT proofing_session_outcome_allowed CHECK (
        outcome IS NULL OR outcome IN ('VERIFIED', 'REVIEW_REQUIRED', 'REJECTED', 'EXPIRED', 'UNKNOWN')
    )
);

CREATE TABLE IF NOT EXISTS work_authorization_evidence (
    row_id              uuid          NOT NULL,
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    evidence_id         text          NOT NULL,
    subject_ref         uuid          NOT NULL,
    revision            bigint        NOT NULL,
    supersedes_revision bigint,
    document_class      text,
    verification_method text,
    jurisdiction        text,
    category            text,
    valid_from          date,
    valid_until         date,
    reverification_due  date,
    evidence_digest     content_digest,
    source_ref          text,
    canonical_digest    content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT work_authorization_evidence_revision_unique
        UNIQUE (tenant_id, evidence_id, revision),
    CONSTRAINT work_authorization_evidence_revision_positive CHECK (revision >= 1),
    CONSTRAINT work_authorization_evidence_lineage_valid CHECK (
        supersedes_revision IS NULL OR supersedes_revision < revision
    )
);

CREATE TABLE IF NOT EXISTS pseudonym_identity (
    row_id          uuid          NOT NULL,
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    pseudonym_id    text          NOT NULL,
    value_digest    content_digest NOT NULL,
    generation      integer       NOT NULL,
    scope           text,
    purpose         text,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT pseudonym_identity_generation_unique
        UNIQUE (tenant_id, pseudonym_id, generation),
    CONSTRAINT pseudonym_identity_generation_positive CHECK (generation >= 1)
);

CREATE TABLE IF NOT EXISTS pseudonym_escrow_record (
    row_id            uuid       NOT NULL,
    tenant_id         tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    pseudonym_id      text       NOT NULL,
    generation        integer    NOT NULL,
    ciphertext        bytea      NOT NULL,
    escrow_custodian  text       NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT pseudonym_escrow_record_generation_positive CHECK (generation >= 1),
    CONSTRAINT pseudonym_escrow_record_ciphertext_present CHECK (octet_length(ciphertext) > 0)
);

CREATE TABLE IF NOT EXISTS pseudonym_evidence_record (
    row_id                 uuid          NOT NULL,
    tenant_id              tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    pseudonym_id           text          NOT NULL,
    generation             integer       NOT NULL,
    intake_content_digest  content_digest,
    intake_at              timestamptz   NOT NULL,
    operation              text          NOT NULL,
    request_digest         content_digest,
    event_sequence         bigint        NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT pseudonym_evidence_record_sequence_unique
        UNIQUE (tenant_id, pseudonym_id, event_sequence),
    CONSTRAINT pseudonym_evidence_record_sequence_positive CHECK (event_sequence >= 1),
    CONSTRAINT pseudonym_evidence_record_generation_positive CHECK (generation >= 1)
);

CREATE TABLE IF NOT EXISTS pseudonym_escrow_event (
    row_id             uuid          NOT NULL,
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    pseudonym_id       text          NOT NULL,
    operation          text          NOT NULL,
    requested_by       text,
    escrow_custodian   text,
    outcome            text          NOT NULL,
    digest             content_digest NOT NULL,
    event_sequence     bigint        NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT pseudonym_escrow_event_sequence_unique
        UNIQUE (tenant_id, pseudonym_id, event_sequence),
    CONSTRAINT pseudonym_escrow_event_sequence_positive CHECK (event_sequence >= 1)
);

CREATE TABLE IF NOT EXISTS data_subject_request (
    row_id                 uuid        NOT NULL,
    tenant_id              tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    request_id             text        NOT NULL,
    kind                   text        NOT NULL,
    claims                 jsonb       NOT NULL,
    jurisdiction           text        NOT NULL,
    received_at            timestamptz NOT NULL,
    deadline               timestamptz NOT NULL,
    verification_state     text        NOT NULL,
    identity_evidence_ref  text,
    identity_assurance     text,
    verified_at            timestamptz,
    duplicate_of           text,
    evidence_id            text,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT data_subject_request_id_unique UNIQUE (tenant_id, request_id),
    CONSTRAINT data_subject_request_kind_allowed CHECK (
        kind IN ('ACCESS', 'RECTIFICATION', 'ERASURE', 'RESTRICTION', 'PORTABILITY', 'OBJECTION')
    ),
    CONSTRAINT data_subject_request_verification_state_allowed CHECK (
        verification_state IN ('UNVERIFIED', 'VERIFIED', 'REFUSED')
    ),
    CONSTRAINT data_subject_request_deadline_ordered CHECK (deadline >= received_at)
);

CREATE TABLE IF NOT EXISTS data_subject_request_evidence (
    row_id             uuid          NOT NULL,
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    request_digest     content_digest NOT NULL,
    kind               text          NOT NULL,
    at                 timestamptz   NOT NULL,
    detail             jsonb,
    event_sequence     bigint        NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT data_subject_request_evidence_sequence_unique
        UNIQUE (tenant_id, request_digest, event_sequence),
    CONSTRAINT data_subject_request_evidence_sequence_positive CHECK (event_sequence >= 1)
);

-- The three evidence streams cannot be rewritten after intake. The database
-- trigger is the same forbid_mutation primitive used by the ledger tables.
CREATE OR REPLACE TRIGGER pseudonym_evidence_record_append_only
    BEFORE UPDATE OR DELETE ON pseudonym_evidence_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER pseudonym_escrow_event_append_only
    BEFORE UPDATE OR DELETE ON pseudonym_escrow_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER data_subject_request_evidence_append_only
    BEFORE UPDATE OR DELETE ON data_subject_request_evidence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON pseudonym_evidence_record FROM PUBLIC;
REVOKE UPDATE, DELETE ON pseudonym_escrow_event FROM PUBLIC;
REVOKE UPDATE, DELETE ON data_subject_request_evidence FROM PUBLIC;
REVOKE UPDATE, DELETE ON proofing_session FROM PUBLIC;
REVOKE UPDATE, DELETE ON work_authorization_evidence FROM PUBLIC;

-- Tenant isolation is fail-closed when app.tenant_id is absent or blank.
-- +goose StatementBegin
DO $$
DECLARE
    table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'proofing_session', 'work_authorization_evidence', 'pseudonym_identity',
        'pseudonym_escrow_record', 'pseudonym_evidence_record',
        'pseudonym_escrow_event', 'data_subject_request',
        'data_subject_request_evidence'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
            table_name
        );
    END LOOP;
END
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON proofing_session TO hcmnext_app;
GRANT SELECT, INSERT ON work_authorization_evidence TO hcmnext_app;
GRANT SELECT, INSERT ON pseudonym_identity TO hcmnext_app;
GRANT SELECT, INSERT ON pseudonym_evidence_record TO hcmnext_app;
GRANT SELECT, INSERT ON pseudonym_escrow_event TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON data_subject_request TO hcmnext_app;
GRANT SELECT, INSERT ON data_subject_request_evidence TO hcmnext_app;

-- Ciphertext-only escrow is visible to the custody service only. hcmnext_app
-- has no privilege on this table and therefore cannot read or insert mapping
-- material directly.
GRANT SELECT, INSERT ON pseudonym_escrow_record TO hcmnext_custody;

-- +goose Down

REVOKE ALL ON pseudonym_escrow_record FROM hcmnext_custody;
REVOKE ALL ON data_subject_request_evidence FROM hcmnext_app;
REVOKE ALL ON data_subject_request FROM hcmnext_app;
REVOKE ALL ON pseudonym_escrow_event FROM hcmnext_app;
REVOKE ALL ON pseudonym_evidence_record FROM hcmnext_app;
REVOKE ALL ON pseudonym_identity FROM hcmnext_app;
REVOKE ALL ON work_authorization_evidence FROM hcmnext_app;
REVOKE ALL ON proofing_session FROM hcmnext_app;

DROP POLICY tenant_isolation ON data_subject_request_evidence;
ALTER TABLE data_subject_request_evidence NO FORCE ROW LEVEL SECURITY;
ALTER TABLE data_subject_request_evidence DISABLE ROW LEVEL SECURITY;
DROP TABLE data_subject_request_evidence;

DROP POLICY tenant_isolation ON data_subject_request;
ALTER TABLE data_subject_request NO FORCE ROW LEVEL SECURITY;
ALTER TABLE data_subject_request DISABLE ROW LEVEL SECURITY;
DROP TABLE data_subject_request;

DROP POLICY tenant_isolation ON pseudonym_escrow_event;
ALTER TABLE pseudonym_escrow_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE pseudonym_escrow_event DISABLE ROW LEVEL SECURITY;
DROP TABLE pseudonym_escrow_event;

DROP POLICY tenant_isolation ON pseudonym_evidence_record;
ALTER TABLE pseudonym_evidence_record NO FORCE ROW LEVEL SECURITY;
ALTER TABLE pseudonym_evidence_record DISABLE ROW LEVEL SECURITY;
DROP TABLE pseudonym_evidence_record;

DROP POLICY tenant_isolation ON pseudonym_escrow_record;
ALTER TABLE pseudonym_escrow_record NO FORCE ROW LEVEL SECURITY;
ALTER TABLE pseudonym_escrow_record DISABLE ROW LEVEL SECURITY;
DROP TABLE pseudonym_escrow_record;

DROP POLICY tenant_isolation ON pseudonym_identity;
ALTER TABLE pseudonym_identity NO FORCE ROW LEVEL SECURITY;
ALTER TABLE pseudonym_identity DISABLE ROW LEVEL SECURITY;
DROP TABLE pseudonym_identity;

DROP POLICY tenant_isolation ON work_authorization_evidence;
ALTER TABLE work_authorization_evidence NO FORCE ROW LEVEL SECURITY;
ALTER TABLE work_authorization_evidence DISABLE ROW LEVEL SECURITY;
DROP TABLE work_authorization_evidence;

DROP POLICY tenant_isolation ON proofing_session;
ALTER TABLE proofing_session NO FORCE ROW LEVEL SECURITY;
ALTER TABLE proofing_session DISABLE ROW LEVEL SECURITY;
DROP TABLE proofing_session;
