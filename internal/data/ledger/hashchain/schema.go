package hashchain

// SchemaDDL creates the ledger_hash_chain_link side table this package
// stores chain links in.
//
// It is a side table, not a migration, because migrations/ is owned by
// other in-flight work and this package's instructions are to report the
// DDL it needs rather than add a migration file. Everything it references -
// the tenant_ref, semantic_key, content_digest and digest_algorithm domains,
// and the forbid_mutation() trigger function - already exists
// (migrations/00001_platform_control.sql, migrations/00002_tenant_primitives.sql):
// this DDL defines no new domain or function, only a new table built from
// them, so it is safe to fold verbatim into a future numbered migration.
//
// Callers apply it once per schema, typically through pgtest.DB.Exec in
// tests (see hashchain_test.go); a production deployment would instead apply
// it as part of a real migration once one of the frozen migration numbers
// is free.
const SchemaDDL = `
CREATE TABLE IF NOT EXISTS ledger_hash_chain_link (
    tenant_id       tenant_ref       NOT NULL,
    stream_key      semantic_key     NOT NULL,
    sequence        bigint           NOT NULL,
    event_id        uuid             NOT NULL,
    prev_hash       text             NOT NULL,
    chain_hash      content_digest   NOT NULL,
    chain_algorithm digest_algorithm NOT NULL,
    recorded_at     timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, stream_key, sequence),
    CONSTRAINT ledger_hash_chain_link_sequence_positive CHECK (sequence >= 1)
);

CREATE OR REPLACE TRIGGER ledger_hash_chain_link_append_only
    BEFORE UPDATE OR DELETE ON ledger_hash_chain_link
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON ledger_hash_chain_link FROM PUBLIC;
`
