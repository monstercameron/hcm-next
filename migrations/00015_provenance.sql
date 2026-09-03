-- Owner: data plane (provenance lane). Phase: P1A.
-- Folds internal/data/provenance.SchemaDDL into a real migration, as that
-- package's schema.go asks. The table body below is copied unchanged from the
-- SchemaDDL constant; everything after it (row level security, grants, down)
-- follows the pattern migrations/00008_tenant_isolation.sql and
-- migrations/00014_ledger_hash_chain.sql established.
--
-- provenance_record publishes one append-only provenance record per ledger
-- event or external observation (DATA-014), with evidence ids and digests
-- enforced non-empty by CHECK constraints.

-- +goose Up

CREATE TABLE IF NOT EXISTS provenance_record (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    source_kind      text         NOT NULL,
    source_ref       semantic_key NOT NULL,
    intent_ref       semantic_key NOT NULL,
    stream_key       text,
    sequence         bigint,
    event_id         uuid,
    observation_ref  text,
    connector_ref    text,
    source_authority text         NOT NULL,
    principal_ref    text         NOT NULL,
    evidence_ids     text[]       NOT NULL,
    digests          jsonb        NOT NULL,
    published_at     timestamptz  NOT NULL,
    recorded_at      timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, record_id),
    CONSTRAINT provenance_record_source_kind_allowed CHECK (
        source_kind IN ('LEDGER_EVENT', 'EXTERNAL_OBSERVATION')
    ),
    CONSTRAINT provenance_record_evidence_ids_present CHECK (
        array_length(evidence_ids, 1) >= 1
    ),
    CONSTRAINT provenance_record_digests_present CHECK (
        jsonb_typeof(digests) = 'array' AND jsonb_array_length(digests) >= 1
    )
);

-- record_id is a deterministic function of (tenant_id, source_kind,
-- source_ref) (publish.go's recordID), so the primary key above already
-- makes "publish twice for the same source" collide on one row; a second,
-- separate UNIQUE(tenant_id, source_kind, source_ref) would name the same
-- fact under a different index and would not be covered by Publish's
-- ON CONFLICT (tenant_id, record_id) target, so it is deliberately not
-- declared here. This index exists for lookups only, not uniqueness.
CREATE INDEX IF NOT EXISTS provenance_record_source ON provenance_record (tenant_id, source_kind, source_ref);

CREATE INDEX IF NOT EXISTS provenance_record_intent ON provenance_record (tenant_id, intent_ref);

CREATE OR REPLACE TRIGGER provenance_record_append_only
    BEFORE UPDATE OR DELETE ON provenance_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON provenance_record FROM PUBLIC;


-- Tenant isolation (DB-017), same shape as every policy in
-- migrations/00008_tenant_isolation.sql: fail closed on a missing/blank
-- app.tenant_id session setting, keyed on internal/data/tenancy.WithTenant.
ALTER TABLE provenance_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE provenance_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON provenance_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Append-only grant, matching ledger_event's own treatment in 00008: SELECT
-- and INSERT only, never UPDATE/DELETE (already revoked above and enforced
-- again by the trigger).
GRANT SELECT, INSERT ON provenance_record TO hcmnext_app;

-- +goose Down
REVOKE ALL ON provenance_record FROM hcmnext_app;
DROP POLICY tenant_isolation ON provenance_record;
ALTER TABLE provenance_record NO FORCE ROW LEVEL SECURITY;
ALTER TABLE provenance_record DISABLE ROW LEVEL SECURITY;
DROP TABLE provenance_record;
