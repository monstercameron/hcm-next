-- Owner: governance/legal lane. Phase: P1A.
-- PERSIST-LEGALEVIDENCE-001: durable review, composition and authoring
-- pipeline evidence. These rows complement, but do not extend or foreign-key
-- into, the jurisdiction/legal_rule_pack/rule_evaluation/obligation family
-- created by migration 00021.
--
-- All three tables are tenant-scoped immutable evidence. The pipeline chain's
-- prev_digest link is verified by the Go store on read, following the
-- ledger_hash_chain_link precedent in migration 00014; the schema deliberately
-- stores no second, database-owned chain interpretation.
--
-- Storage disposition:
-- legal_review_record: migration 00053, owner_package internal/data/legalevidencestore,
-- data_role LEDGER, tenant_scoping_column tenant_id, append_only true,
-- retention_class PERMANENT, rebuild_source legal.ReviewRecord.
-- legal_composition_receipt: migration 00053, owner_package internal/data/legalevidencestore,
-- data_role LEDGER, tenant_scoping_column tenant_id, append_only true,
-- retention_class PERMANENT, rebuild_source legal.CompositionReceipt.
-- legal_pipeline_event: migration 00053, owner_package internal/data/legalevidencestore,
-- data_role LEDGER, tenant_scoping_column tenant_id, append_only true,
-- retention_class PERMANENT, rebuild_source legal/pipeline.Event.

-- +goose Up

CREATE TABLE IF NOT EXISTS legal_review_record (
    tenant_id      tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid            NOT NULL,
    pack_digest    content_digest  NOT NULL,
    author_id      text            NOT NULL,
    reviewer_id    text            NOT NULL,
    status         text            NOT NULL,
    findings       jsonb,
    digest         content_digest  NOT NULL,
    event_sequence bigint          NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_review_record_sequence_positive CHECK (event_sequence >= 1),
    CONSTRAINT legal_review_record_distinct_actors CHECK (author_id <> reviewer_id),
    CONSTRAINT legal_review_record_sequence_unique
        UNIQUE (tenant_id, pack_digest, event_sequence)
);

CREATE OR REPLACE TRIGGER legal_review_record_append_only
    BEFORE UPDATE OR DELETE ON legal_review_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON legal_review_record FROM PUBLIC;

CREATE TABLE IF NOT EXISTS legal_composition_receipt (
    tenant_id      tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid            NOT NULL,
    status         text            NOT NULL,
    jurisdictions  jsonb           NOT NULL,
    inputs         jsonb,
    obligations    jsonb,
    traces         jsonb,
    contradictions jsonb,
    digest         content_digest  NOT NULL,
    event_sequence bigint          NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_composition_receipt_sequence_positive CHECK (event_sequence >= 1),
    CONSTRAINT legal_composition_receipt_digest_unique UNIQUE (tenant_id, digest)
);

CREATE OR REPLACE TRIGGER legal_composition_receipt_append_only
    BEFORE UPDATE OR DELETE ON legal_composition_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON legal_composition_receipt FROM PUBLIC;

CREATE TABLE IF NOT EXISTS legal_pipeline_event (
    tenant_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid            NOT NULL,
    stage           text            NOT NULL,
    principal_id    text            NOT NULL,
    role            text            NOT NULL,
    artifact_digest content_digest  NOT NULL,
    prev_digest     content_digest,
    digest          content_digest  NOT NULL,
    signature       bytea           NOT NULL,
    event_sequence  bigint          NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT legal_pipeline_event_sequence_positive CHECK (event_sequence >= 1),
    CONSTRAINT legal_pipeline_event_sequence_unique UNIQUE (tenant_id, event_sequence)
);

CREATE OR REPLACE TRIGGER legal_pipeline_event_append_only
    BEFORE UPDATE OR DELETE ON legal_pipeline_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON legal_pipeline_event FROM PUBLIC;

ALTER TABLE legal_review_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_review_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_review_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE legal_composition_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_composition_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_composition_receipt
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE legal_pipeline_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_pipeline_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_pipeline_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON
    legal_review_record,
    legal_composition_receipt,
    legal_pipeline_event
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON legal_pipeline_event FROM hcmnext_app;
REVOKE ALL ON legal_composition_receipt FROM hcmnext_app;
REVOKE ALL ON legal_review_record FROM hcmnext_app;

DROP POLICY tenant_isolation ON legal_pipeline_event;
ALTER TABLE legal_pipeline_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_pipeline_event DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON legal_composition_receipt;
ALTER TABLE legal_composition_receipt NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_composition_receipt DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON legal_review_record;
ALTER TABLE legal_review_record NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_review_record DISABLE ROW LEVEL SECURITY;

DROP TABLE legal_pipeline_event;
DROP TABLE legal_composition_receipt;
DROP TABLE legal_review_record;
