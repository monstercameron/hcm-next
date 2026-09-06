-- Owner: attestation and human-work data lane. Phase: PERSIST-ATTESTATION-001.
--
-- Four tenant-scoped records make attestation and approval evidence durable.
-- The statement and requirement tables are immutable revision histories; their
-- current meaning is selected by the highest version for a natural key. The
-- binding and resolution tables are append-only evidence logs. This table set
-- is intentionally separate from work_item and its transition/decision rows:
-- a work item may name an approval requirement by value, but it does not own
-- the requirement or its resolution evidence.
--
-- Storage disposition (STORE-001): attestation_statement and
-- approval_requirement are AGGREGATE/PERMANENT; attestation_binding and
-- approval_resolution are LEDGER/PERMANENT. None is rebuildable from another
-- table (rebuild_source: null). All four are tenant scoped by tenant_id.

-- +goose Up

CREATE TABLE IF NOT EXISTS attestation_statement (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid            NOT NULL,
    statement_id     semantic_key    NOT NULL,
    version          cas_version     NOT NULL,
    kind             text            NOT NULL,
    subject_ref      uuid            NOT NULL,
    attester         jsonb           NOT NULL,
    text_digest      content_digest  NOT NULL,
    evidence_refs    jsonb,
    validity_from    timestamptz,
    validity_to      timestamptz,
    jurisdiction_ref uuid,
    revocation_link  jsonb,
    digest           content_digest  NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, statement_id, version),
    CONSTRAINT attestation_statement_version_positive CHECK (version >= 1),
    CONSTRAINT attestation_statement_validity_order CHECK (
        validity_to IS NULL OR validity_from IS NULL OR validity_to > validity_from
    )
);

CREATE INDEX IF NOT EXISTS attestation_statement_latest
    ON attestation_statement (tenant_id, statement_id, version DESC);

CREATE OR REPLACE TRIGGER attestation_statement_append_only
    BEFORE UPDATE OR DELETE ON attestation_statement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON attestation_statement FROM PUBLIC;

CREATE TABLE IF NOT EXISTS attestation_binding (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id            uuid            NOT NULL,
    statement_id      semantic_key    NOT NULL,
    statement_version cas_version     NOT NULL,
    context_digest    content_digest  NOT NULL,
    evidence_bindings jsonb,
    binding_version   bigint          NOT NULL,
    bound_at          timestamptz     NOT NULL,
    signer            uuid            NOT NULL,
    digest            content_digest  NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, statement_id, binding_version),
    CONSTRAINT attestation_binding_statement_version_positive CHECK (statement_version >= 1),
    CONSTRAINT attestation_binding_version_positive CHECK (binding_version >= 1)
);

CREATE INDEX IF NOT EXISTS attestation_binding_latest
    ON attestation_binding (tenant_id, statement_id, binding_version DESC);

CREATE OR REPLACE TRIGGER attestation_binding_append_only
    BEFORE UPDATE OR DELETE ON attestation_binding
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON attestation_binding FROM PUBLIC;

CREATE TABLE IF NOT EXISTS approval_requirement (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid            NOT NULL,
    requirement_id     semantic_key    NOT NULL,
    revision           cas_version     NOT NULL,
    stage              integer         NOT NULL,
    candidates         jsonb,
    authority_floor    jsonb,
    quorum             jsonb,
    deadline           interval,
    escalation         jsonb,
    separation         jsonb,
    invalidators       jsonb,
    source             text,
    expression_digest  content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, requirement_id, revision),
    CONSTRAINT approval_requirement_revision_positive CHECK (revision >= 1),
    CONSTRAINT approval_requirement_stage_positive CHECK (stage >= 1)
);

CREATE INDEX IF NOT EXISTS approval_requirement_latest
    ON approval_requirement (tenant_id, requirement_id, revision DESC);

CREATE OR REPLACE TRIGGER approval_requirement_append_only
    BEFORE UPDATE OR DELETE ON approval_requirement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON approval_requirement FROM PUBLIC;

CREATE TABLE IF NOT EXISTS approval_resolution (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid            NOT NULL,
    requirement_id        semantic_key   NOT NULL,
    requirement_revision  cas_version    NOT NULL,
    outcome               text           NOT NULL,
    candidates            jsonb,
    excluded              jsonb,
    fallback_used         boolean        NOT NULL DEFAULT false,
    resolved_at           timestamptz    NOT NULL,
    effective_at          timestamptz,
    directory_version     text,
    expression_digest     content_digest,
    requirement_digest    content_digest,
    quorum_required       integer,
    event_sequence        bigint         NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, requirement_id, event_sequence),
    CONSTRAINT approval_resolution_requirement_revision_positive CHECK (requirement_revision >= 1),
    CONSTRAINT approval_resolution_event_sequence_positive CHECK (event_sequence >= 1)
);

CREATE INDEX IF NOT EXISTS approval_resolution_latest
    ON approval_resolution (tenant_id, requirement_id, event_sequence DESC);

CREATE OR REPLACE TRIGGER approval_resolution_append_only
    BEFORE UPDATE OR DELETE ON approval_resolution
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON approval_resolution FROM PUBLIC;

-- DB-017: fail closed unless the transaction has selected one tenant.
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'attestation_statement',
        'attestation_binding',
        'approval_requirement',
        'approval_resolution'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I '
            'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
            'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
            target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON
    attestation_statement,
    attestation_binding,
    approval_requirement,
    approval_resolution
TO hcmnext_app;

-- +goose Down
REVOKE ALL ON
    approval_resolution,
    approval_requirement,
    attestation_binding,
    attestation_statement
FROM hcmnext_app;

DROP TABLE approval_resolution;
DROP TABLE approval_requirement;
DROP TABLE attestation_binding;
DROP TABLE attestation_statement;
