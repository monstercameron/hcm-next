-- Owner: data plane. Phase: P1A.
-- DB-017: tenant isolation enforced physically in PostgreSQL, not only by
-- repository code that remembers to filter on tenant_id. Two mechanisms carry
-- this together:
--
--   1. hcmnext_app -- a least-privilege role. It is never granted BYPASSRLS,
--      never owns a table, and is granted exactly the statements each table
--      needs (never DELETE: this data plane has no delete semantics, only
--      append and state transition). A role that could bypass row security or
--      that owns the tables it reads would make every policy below decorative.
--
--   2. A row level security policy on every tenant-scoped table: the twelve
--      migrations/00001..00006 already require to carry tenant_id NOT NULL
--      (internal/data/schema's own tenantScopedTables list) plus
--      external_observation and observation_checkpoint, added by
--      migrations/00007_connectivity.sql (INTG-008/009) after this
--      migration's tenant list was first drafted -- both are tenant-scoped
--      the same way, so they are governed here too even though the frozen
--      internal/data/schema golden test has not (as of this writing) been
--      updated to name them; that file is out of this migration's lane to
--      edit. The policy is keyed on the app.tenant_id session-local setting.
--      internal/data/tenancy.WithTenant is the one Go entry point that sets
--      it, via set_config(..., true) inside the caller's own transaction, so
--      the value never survives past that transaction and never leaks onto a
--      pooled connection's next, unrelated use.
--
-- Fail-closed predicate. Every policy compares tenant_id against
--   NULLIF(current_setting('app.tenant_id', true), '')::uuid
-- rather than a bare current_setting(...)::uuid:
--   - current_setting(..., true) (missing_ok) returns NULL, never an error,
--     when WithTenant was never called for this transaction.
--   - NULLIF(..., '') folds an explicitly blank setting to NULL the same way,
--     so there is exactly one "no tenant selected" value, not two.
--   - tenant_id = NULL is never true, in any row, for any table. A
--     transaction that forgot to call WithTenant therefore sees zero rows
--     from every tenant-scoped table -- the RED case this migration exists to
--     close ("missing tenant context defaults open") cannot happen; it
--     defaults closed instead.
--   - A forged, syntactically-invalid UUID in the session setting fails the
--     ::uuid cast and raises rather than silently matching some row or
--     silently returning an ambiguous empty result; a forged but
--     well-formed UUID for a real tenant simply selects that tenant's own
--     rows, which is the same authority a legitimate call with that tenant id
--     would have, so it establishes no new access.
--
-- Adoption path. Production connections authenticate as a bootstrap identity
-- and run "SET ROLE hcmnext_app" (a role a superuser, or any role granted
-- membership, may always assume) before touching tenant-scoped data, exactly
-- as the pgtest-backed integration tests in internal/data/tenancy do; the
-- bootstrap identity itself is never exposed to request-scoped code. This
-- keeps the still-forming migration/repair role split (also named in
-- DB-017's own GREEN clause) as a later addition on the same pattern rather
-- than a redesign.

-- +goose Up

-- CREATE ROLE is guarded by an exception handler rather than an
-- IF NOT EXISTS check on pg_roles: the role is cluster-wide but this file is
-- applied once per schema, including once per parallel test in
-- internal/data/pgtest, so two applications can genuinely race between the
-- existence check and the CREATE. Two concurrent CREATE ROLE statements that
-- both pass PostgreSQL's own pre-check race the same way: PostgreSQL normally
-- reports a second CREATE ROLE of the same name as duplicate_object (42710),
-- but true concurrent inserts can instead surface as the underlying unique
-- index violation, unique_violation (23505), so both are caught.
-- Two schemas migrating at once (every parallel test on one embedded server)
-- can both pass the pre-check while the first CREATE ROLE is still
-- uncommitted; the second then swallows the duplicate and its COMMENT ON
-- ROLE fails with "role does not exist". The transaction-scoped advisory
-- lock serialises this block across sessions, so the second application
-- waits for the first to commit and sees the role.
SELECT pg_advisory_xact_lock(hashtext('migration:00008:hcmnext_app'));

-- +goose StatementBegin
DO $$
BEGIN
    CREATE ROLE hcmnext_app
        NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
        NOLOGIN NOREPLICATION NOBYPASSRLS;
EXCEPTION
    WHEN duplicate_object OR unique_violation THEN
        NULL;
END
$$;
-- +goose StatementEnd

COMMENT ON ROLE hcmnext_app IS
    'DB-017 least-privilege application role. Never BYPASSRLS, never a table '
    'owner; reached only via SET ROLE from an already-authenticated session, '
    'never by direct login.';

-- GRANT USAGE ON SCHEMA needs the active schema's name, not a literal: the
-- data-plane test harness (internal/data/pgtest) applies every migration
-- inside a fresh, differently-named schema per test so that tests may run in
-- parallel without seeing each other's rows, and production applies this same
-- file inside whichever schema its search_path names. current_schema() is
-- already resolved to that schema for the connection running this migration.
-- +goose StatementBegin
DO $$
DECLARE
    target_schema text := current_schema();
BEGIN
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_app', target_schema);
END
$$;
-- +goose StatementEnd

-- Mutable tenant-scoped tables: the ordinary read/write/update surface.
-- DELETE is withheld from every table in this migration, mutable or not: the
-- domain model has no delete semantics, only append and state transition
-- (a status column, a superseding version, a moved pointer), so granting
-- DELETE here would open a capability the application layer never needs and
-- the audit trail can never afford.
GRANT SELECT, INSERT, UPDATE ON
    tenant,
    definition_active_pointer,
    intent_instance,
    payload_schema,
    authority_assignment,
    ledger_stream,
    stream_head,
    projection_checkpoint,
    outbox,
    observation_checkpoint
TO hcmnext_app;

-- Append-only tables: SELECT and INSERT only. UPDATE/DELETE are already
-- revoked from PUBLIC by 00001/00004/00005 and refused again at the row level
-- by the forbid_mutation trigger; hcmnext_app is simply never granted them, so
-- the trigger is defense in depth rather than the only thing standing in the
-- way.
GRANT SELECT, INSERT ON
    definition_version,
    proposal_revision,
    ledger_event,
    external_observation
TO hcmnext_app;

-- ledger_event's hash partitions do not inherit the parent's GRANTs -- a
-- direct grant on the partitioned table does not extend to its partitions,
-- which is exactly why 00005 already revokes UPDATE/DELETE on each of them
-- individually. The read/write grant needs the same per-partition treatment.
GRANT SELECT, INSERT ON
    ledger_event_p0, ledger_event_p1, ledger_event_p2, ledger_event_p3
TO hcmnext_app;

-- Row level security, one policy per tenant-scoped table. FORCE ROW LEVEL
-- SECURITY is set even though hcmnext_app never owns these tables (ownership
-- stays with whichever role ran the migration): the owner-bypass FORCE
-- exists to close would only ever matter for a future role that both owns a
-- table and lacks BYPASSRLS, and setting it now costs nothing.

ALTER TABLE tenant ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE definition_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE definition_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON definition_version
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE definition_active_pointer ENABLE ROW LEVEL SECURITY;
ALTER TABLE definition_active_pointer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON definition_active_pointer
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE intent_instance ENABLE ROW LEVEL SECURITY;
ALTER TABLE intent_instance FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON intent_instance
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE proposal_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposal_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposal_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE payload_schema ENABLE ROW LEVEL SECURITY;
ALTER TABLE payload_schema FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payload_schema
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE authority_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE authority_assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON authority_assignment
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE ledger_stream ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_stream FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_stream
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE stream_head ENABLE ROW LEVEL SECURITY;
ALTER TABLE stream_head FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON stream_head
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- ledger_event is PARTITION BY HASH (tenant_id). A policy declared on the
-- partitioned parent governs access routed through the parent (an ordinary
-- "FROM ledger_event" query: PostgreSQL plans it as a scan of the partitions
-- and pushes the parent's policy quals down into each one). It does NOT by
-- itself protect a query that names one partition directly -- a partition is
-- an ordinary table with its own independent row-security state, and
-- PostgreSQL applies only the policies declared on the exact relation named
-- in the query. Enabling row level security on a partition with no policy of
-- its own denies everything (the documented default-deny for RLS-enabled,
-- policy-less tables), which would be a availability bug, not a leak -- but
-- leaving row level security disabled on the partition would be the opposite
-- and far worse: full, ungoverned read/write on whichever tenants' rows
-- happen to hash into that partition. So each partition gets its own copy of
-- the identical policy in addition to ENABLE/FORCE, and a direct partition
-- query ends up exactly as governed as one that goes through the parent.
ALTER TABLE ledger_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE ledger_event_p0 ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_event_p0 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_event_p0
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE ledger_event_p1 ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_event_p1 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_event_p1
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE ledger_event_p2 ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_event_p2 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_event_p2
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE ledger_event_p3 ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_event_p3 FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_event_p3
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE projection_checkpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE projection_checkpoint FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON projection_checkpoint
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON outbox
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Added by migrations/00007_connectivity.sql, after 00001..00006's tenant list
-- above; tenant-scoped the same way, so governed the same way.
ALTER TABLE external_observation ENABLE ROW LEVEL SECURITY;
ALTER TABLE external_observation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON external_observation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE observation_checkpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE observation_checkpoint FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON observation_checkpoint
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- +goose Down
DROP POLICY tenant_isolation ON observation_checkpoint;
ALTER TABLE observation_checkpoint NO FORCE ROW LEVEL SECURITY;
ALTER TABLE observation_checkpoint DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON external_observation;
ALTER TABLE external_observation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE external_observation DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON outbox;
ALTER TABLE outbox NO FORCE ROW LEVEL SECURITY;
ALTER TABLE outbox DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON projection_checkpoint;
ALTER TABLE projection_checkpoint NO FORCE ROW LEVEL SECURITY;
ALTER TABLE projection_checkpoint DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON ledger_event_p3;
ALTER TABLE ledger_event_p3 NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_event_p3 DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON ledger_event_p2;
ALTER TABLE ledger_event_p2 NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_event_p2 DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON ledger_event_p1;
ALTER TABLE ledger_event_p1 NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_event_p1 DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON ledger_event_p0;
ALTER TABLE ledger_event_p0 NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_event_p0 DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON ledger_event;
ALTER TABLE ledger_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_event DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON stream_head;
ALTER TABLE stream_head NO FORCE ROW LEVEL SECURITY;
ALTER TABLE stream_head DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON ledger_stream;
ALTER TABLE ledger_stream NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_stream DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON authority_assignment;
ALTER TABLE authority_assignment NO FORCE ROW LEVEL SECURITY;
ALTER TABLE authority_assignment DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON payload_schema;
ALTER TABLE payload_schema NO FORCE ROW LEVEL SECURITY;
ALTER TABLE payload_schema DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON proposal_revision;
ALTER TABLE proposal_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE proposal_revision DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON intent_instance;
ALTER TABLE intent_instance NO FORCE ROW LEVEL SECURITY;
ALTER TABLE intent_instance DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON definition_active_pointer;
ALTER TABLE definition_active_pointer NO FORCE ROW LEVEL SECURITY;
ALTER TABLE definition_active_pointer DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON definition_version;
ALTER TABLE definition_version NO FORCE ROW LEVEL SECURITY;
ALTER TABLE definition_version DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON tenant;
ALTER TABLE tenant NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant DISABLE ROW LEVEL SECURITY;

REVOKE ALL ON ledger_event_p0, ledger_event_p1, ledger_event_p2, ledger_event_p3 FROM hcmnext_app;
REVOKE ALL ON definition_version, proposal_revision, ledger_event, external_observation FROM hcmnext_app;
REVOKE ALL ON
    tenant, definition_active_pointer, intent_instance, payload_schema,
    authority_assignment, ledger_stream, stream_head, projection_checkpoint, outbox,
    observation_checkpoint
FROM hcmnext_app;

-- +goose StatementBegin
DO $$
DECLARE
    target_schema text := current_schema();
BEGIN
    EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM hcmnext_app', target_schema);
END
$$;
-- +goose StatementEnd

-- hcmnext_app itself is intentionally not dropped here: it is a
-- cluster-wide role shared by every schema/test on the server (created only
-- once, guarded by the exception handler in Up), so tearing down one
-- schema's migrations must not drop a role other schemas still depend on.
