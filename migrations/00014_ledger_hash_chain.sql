-- Owner: data plane (ledger/hashchain lane). Phase: P1A.
-- Folds internal/data/ledger/hashchain.SchemaDDL into a real migration, as
-- that package's own doc.go asks: "Whoever next owns a free migration number
-- should fold SchemaDDL into a migration verbatim." The table body below is
-- copied unchanged from internal/data/ledger/hashchain/schema.go's SchemaDDL
-- constant; everything after it (row level security, grants) is new,
-- following the same pattern migrations/00008_tenant_isolation.sql
-- established for every other tenant-scoped table.
--
-- ledger_hash_chain_link is a side table for internal/data/ledger/hashchain's
-- tamper-evident hash chain over ledger_event: one link per
-- (tenant, stream_key, sequence), append-only via the same forbid_mutation()
-- trigger migrations/00001_platform_control.sql defines. It carries no
-- foreign key to ledger_event/ledger_stream in the verbatim DDL below (see
-- schema.go's own comment: it references only domains and the trigger
-- function that already exist), so this migration does not add one either.

-- +goose Up

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

-- Tenant isolation (DB-017), same shape as every policy in
-- migrations/00008_tenant_isolation.sql: fail closed on a missing/blank
-- app.tenant_id session setting, keyed on internal/data/tenancy.WithTenant.
ALTER TABLE ledger_hash_chain_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_hash_chain_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_hash_chain_link
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Append-only grant, matching ledger_event's own treatment in 00008: SELECT
-- and INSERT only, never UPDATE/DELETE (already revoked above and enforced
-- again by the trigger).
GRANT SELECT, INSERT ON ledger_hash_chain_link TO hcmnext_app;

-- +goose Down
REVOKE ALL ON ledger_hash_chain_link FROM hcmnext_app;
DROP POLICY tenant_isolation ON ledger_hash_chain_link;
ALTER TABLE ledger_hash_chain_link NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_hash_chain_link DISABLE ROW LEVEL SECURITY;
DROP TABLE ledger_hash_chain_link;
