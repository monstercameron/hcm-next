-- Owner: data plane. Phase: P1A.
-- TENANT-002: one pilot tenant bootstrapped from a signed-shape manifest
-- idempotently. tenant_bootstrap_receipt is the durable evidence of every
-- manifest revision actually applied against a tenant.
--
-- Only APPLY decisions are ever recorded here: internal/data/tenancy.Bootstrap
-- resolves an incoming internal/domains/tenant.BootstrapManifest against the
-- most recently applied receipt (internal/domains/tenant.ResolveBootstrap)
-- before writing anything, so a replay of the identical manifest (same
-- manifest id, revision and content digest) is a true no-op -- no new row --
-- exactly like internal/data/artifacts.Put's own idempotent-by-identity
-- contract; a manifest that changes content without declaring a new revision
-- is refused before this table is ever touched. The unique index on
-- (tenant_id, manifest_id, revision) is the same-revision race backstop: two
-- concurrent appliers of the identical manifest can both pass the read-side
-- check, but only one INSERT wins, and internal/data/tenancy.Bootstrap
-- re-reads the winning row to report NOOP (matching digest) or REJECTED
-- (a genuine same-revision conflict) rather than surfacing the raw
-- constraint violation.
--
-- This is deliberately a new table rather than a ledger_event stream: the
-- ledger's payload_schema.wire_format is constrained to 'PROTOBUF'
-- (migrations/00005_ledger.sql), and a manifest is not a generated protobuf
-- message. Row level security, the least-privilege grant, the fail-closed
-- app.tenant_id predicate and the append-only trigger are copied verbatim
-- from migrations/00023_journey_workforce.sql's own copy of the
-- migrations/00008_tenant_isolation.sql pattern.
--
-- This table is a new base table in the core schema, so it must be added to
-- definitions/storage/storage-disposition.yaml (STORE-001) for
-- internal/data/schema's registry-driven TestTodo_DATA_001/_Golden to see it
-- as classified rather than unregistered; see this lane's report for the
-- exact entry.

-- +goose Up

CREATE TABLE IF NOT EXISTS tenant_bootstrap_receipt (
    tenant_id   tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    receipt_id  uuid         NOT NULL,
    manifest_id semantic_key NOT NULL,
    revision    bigint       NOT NULL,
    digest      text         NOT NULL,
    decision    text         NOT NULL DEFAULT 'APPLIED',
    manifest    jsonb        NOT NULL,
    applied_at  timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, receipt_id),
    CONSTRAINT tenant_bootstrap_receipt_revision_positive CHECK (revision >= 1),
    CONSTRAINT tenant_bootstrap_receipt_digest_present CHECK (length(digest) > 0),
    CONSTRAINT tenant_bootstrap_receipt_decision_allowed CHECK (decision IN ('APPLIED')),
    CONSTRAINT tenant_bootstrap_receipt_revision_unique UNIQUE (tenant_id, manifest_id, revision)
);

-- "The latest applied revision for this manifest", the one read
-- internal/data/tenancy.LatestBootstrap performs.
CREATE INDEX IF NOT EXISTS tenant_bootstrap_receipt_latest
    ON tenant_bootstrap_receipt (tenant_id, manifest_id, revision DESC);

CREATE OR REPLACE TRIGGER tenant_bootstrap_receipt_append_only
    BEFORE UPDATE OR DELETE ON tenant_bootstrap_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON tenant_bootstrap_receipt FROM PUBLIC;

-- Tenant isolation (DB-017): fail closed on a missing/blank app.tenant_id
-- session setting, keyed on internal/data/tenancy.WithTenant. The bootstrap
-- write path itself runs as the elevated migration/admin identity (the same
-- way internal/intent/app/pgstore.Store.Bootstrap registers the tenant row
-- itself, before any request-scoped code ever assumes hcmnext_app), so this
-- policy governs later reads/writes through the least-privilege role, not the
-- very first bootstrap of a brand new tenant.
ALTER TABLE tenant_bootstrap_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_bootstrap_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_bootstrap_receipt
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Append-only evidence, so SELECT/INSERT only.
GRANT SELECT, INSERT ON tenant_bootstrap_receipt TO hcmnext_app;

-- +goose Down
REVOKE ALL ON tenant_bootstrap_receipt FROM hcmnext_app;
DROP POLICY tenant_isolation ON tenant_bootstrap_receipt;
ALTER TABLE tenant_bootstrap_receipt NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_bootstrap_receipt DISABLE ROW LEVEL SECURITY;
DROP TABLE tenant_bootstrap_receipt;
