package provenance

// SchemaDDL creates the provenance_record side table this package stores
// published provenance edges in. See the package doc's "DDL note" for why
// this is a side table rather than a migration.
//
// evidence_ids uses the postgres array type directly (rather than jsonb) so
// the "never published without its evidence id" invariant can be a real
// CHECK constraint (provenance_record_evidence_ids_present), not just
// application-level validation in [Publish]. digests is jsonb because a
// record can carry more than one named digest (e.g. a request digest and a
// material digest) with no fixed arity.
const SchemaDDL = `
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
`
