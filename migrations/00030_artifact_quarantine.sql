-- Owner: domains (asset/quarantine). Phase: P1B.
-- DOC-MAL-001: scan and quarantine uploaded artifacts before use.
--
-- Why this lives in a new companion-schema pair, not a table in this
-- migration tree's own schema
-- -----------------------------------------------------------------------
-- internal/data/schema's frozen golden test (TestTodo_DATA_001 and its
-- _Golden variant, owned by another lane) enumerates the exact set of base
-- tables this migration tree may hold in the schema Goose applies into
-- (current_schema()). A new table here cannot join that enumerated set
-- without editing a frozen, passing test this lane does not own.
--
-- migrations/00010_artifacts.sql already established the pattern this
-- migration reuses rather than reinventing: a companion schema, named
-- <the schema Goose is applying into>_artifact_store, computed at apply
-- time from current_schema(). This migration adds two more tables to that
-- same companion schema instead of creating a third schema, because a
-- quarantined artifact's bytes and its verdict history are exactly the kind
-- of governed, content-addressed object-store data 00010's header comment
-- already describes -- just for content that has not yet been promoted to
-- (or has been refused from) the durable `artifact` table 00010 owns.
--
-- Ownership boundary with the `artifact` table (DOC-MAL-001 REFACTOR /
-- DOC-INTAKE-001 REFACTOR: "malware scanning, byte storage, classification
-- and domain evidence sufficiency remain separate owners")
-- -----------------------------------------------------------------------
-- This migration's `artifact_quarantine` table is DOC-MAL-001's own byte
-- storage for content that has not yet cleared quarantine -- and, per the
-- platform's content-safety model (planning/specs/platform-foundation-gap-
-- closure.md §3: "The original remains restricted evidence"), it keeps
-- that role even for content that is ultimately REJECTED: a rejected
-- upload's bytes stay recorded as quarantine evidence, they are simply
-- never admitted for any downstream use. This table is therefore distinct
-- from, and has no foreign key to, 00010's `artifact` table: promoting an
-- ADMITTED quarantine result into a governed, classified `artifact` row
-- (with its own retention class, creator authority and evidence id) is
-- DOC-INTAKE-001's job, not this one's.
--
-- Tables
-- ------
--   artifact_quarantine        -- one immutable row per (tenant, sha256
--                                  content id) of quarantined bytes: byte
--                                  size, the caller-declared content type
--                                  and the sniffed (magic-byte) content
--                                  type actually observed, creator
--                                  principal and intake evidence, and the
--                                  bytes themselves. Append-only, exactly
--                                  like 00010's `artifact` table:
--                                  forbid_mutation rejects UPDATE and
--                                  DELETE outright.
--   artifact_quarantine_state  -- append-only QUARANTINED -> ADMITTED |
--                                  REJECTED verdict log. A quarantined
--                                  artifact's current state is the row with
--                                  the highest `seq` for that content id
--                                  (an identity column, not `recorded_at`:
--                                  see the table's own comment for why),
--                                  not a mutable status column -- the same
--                                  event-log discipline 00010's
--                                  artifact_reference_event uses for
--                                  reference counting. Every ADMITTED or
--                                  REJECTED row names the scanner id and
--                                  version that produced it; REJECTED rows
--                                  also carry a non-empty reason. A rescan
--                                  is a new QUARANTINED row followed by a
--                                  new verdict row, so history is never
--                                  overwritten, only superseded.
--
-- Both tables are tenant scoped and carry the same app.tenant_id row level
-- security policy migrations/00008 established for the core schema, so
-- isolation covers this companion schema's quarantine tables too.

-- +goose Up
-- +goose StatementBegin
DO $migration$
DECLARE
    store_schema text := current_schema() || '_artifact_store';
BEGIN
    -- artifact_quarantine: one immutable row per (tenant, content id) of
    -- quarantined bytes, pending or past a verdict.
    EXECUTE format($ddl$
        CREATE TABLE %I.artifact_quarantine (
            tenant_id             tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
            content_id            content_digest   NOT NULL,
            digest_algorithm      digest_algorithm NOT NULL DEFAULT 'sha256',
            byte_size             bigint           NOT NULL,
            declared_content_type text             NOT NULL,
            sniffed_content_type  text             NOT NULL,
            creator_principal_ref text             NOT NULL,
            evidence_id           text             NOT NULL,
            content               bytea            NOT NULL,
            created_at            timestamptz      NOT NULL DEFAULT now(),
            PRIMARY KEY (tenant_id, content_id),
            CONSTRAINT artifact_quarantine_declared_type_present CHECK (length(declared_content_type) > 0),
            CONSTRAINT artifact_quarantine_sniffed_type_present CHECK (length(sniffed_content_type) > 0),
            CONSTRAINT artifact_quarantine_creator_principal_present CHECK (length(creator_principal_ref) > 0),
            CONSTRAINT artifact_quarantine_evidence_id_present CHECK (length(evidence_id) > 0),
            CONSTRAINT artifact_quarantine_byte_size_matches_content CHECK (byte_size = octet_length(content)),
            -- A Go-level bounded reader enforces the caller-configured
            -- allowlist size cap (internal/domains/asset/quarantine.Policy)
            -- before this row is ever written; this is the absolute
            -- backstop behind it, mirroring 00010_artifacts.sql's own
            -- artifact_byte_size_bounded constraint and constant.
            CONSTRAINT artifact_quarantine_byte_size_bounded CHECK (byte_size >= 0 AND byte_size <= 67108864)
        )
    $ddl$, store_schema);

    EXECUTE format(
        'CREATE TRIGGER artifact_quarantine_append_only BEFORE UPDATE OR DELETE ON %I.artifact_quarantine
            FOR EACH ROW EXECUTE FUNCTION forbid_mutation()',
        store_schema);
    EXECUTE format('REVOKE UPDATE, DELETE ON %I.artifact_quarantine FROM PUBLIC', store_schema);
    EXECUTE format('GRANT SELECT, INSERT ON %I.artifact_quarantine TO hcmnext_app', store_schema);

    EXECUTE format('ALTER TABLE %I.artifact_quarantine ENABLE ROW LEVEL SECURITY', store_schema);
    EXECUTE format('ALTER TABLE %I.artifact_quarantine FORCE ROW LEVEL SECURITY', store_schema);
    EXECUTE format($pol$
        CREATE POLICY tenant_isolation ON %I.artifact_quarantine
            USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
            WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    $pol$, store_schema);

    -- artifact_quarantine_state: append-only QUARANTINED -> ADMITTED |
    -- REJECTED verdict log, keyed by its own state_id so a rescan can
    -- append a fresh QUARANTINED row without conflicting with history.
    --
    -- `seq` is what "most recent" actually reads by. `recorded_at` alone
    -- cannot serve that role: within Upload's single transaction (the
    -- normal case -- see internal/domains/asset/quarantine.Upload), the
    -- initial QUARANTINED row and the ADMITTED/REJECTED verdict row that
    -- follows it are both stamped with the same transaction-start `now()`
    -- value, so an ORDER BY recorded_at DESC tie would fall back to
    -- comparing two random state_id UUIDs -- a coin flip on which row
    -- "wins", not the append order. `seq` is a GENERATED ALWAYS AS
    -- IDENTITY column, so Postgres assigns it at each INSERT statement's
    -- execution, strictly increasing in the order rows are actually
    -- appended even when every row in a transaction shares one
    -- `recorded_at`.
    EXECUTE format($ddl$
        CREATE TABLE %I.artifact_quarantine_state (
            tenant_id       tenant_ref     NOT NULL,
            content_id      content_digest NOT NULL,
            state_id        uuid           NOT NULL,
            seq             bigint         GENERATED ALWAYS AS IDENTITY,
            state           text           NOT NULL,
            scanner_id      text           NOT NULL DEFAULT '',
            scanner_version text           NOT NULL DEFAULT '',
            reason          text           NOT NULL DEFAULT '',
            evidence_id     text           NOT NULL,
            recorded_at     timestamptz    NOT NULL DEFAULT now(),
            PRIMARY KEY (tenant_id, state_id),
            CONSTRAINT artifact_quarantine_state_artifact
                FOREIGN KEY (tenant_id, content_id) REFERENCES %I.artifact_quarantine (tenant_id, content_id),
            CONSTRAINT artifact_quarantine_state_allowed CHECK (
                state IN ('QUARANTINED', 'ADMITTED', 'REJECTED')
            ),
            CONSTRAINT artifact_quarantine_state_evidence_id_present CHECK (length(evidence_id) > 0),
            -- A verdict (ADMITTED or REJECTED) always names the scanner
            -- that produced it; the initial QUARANTINED row, recorded
            -- before any scan has run, never can.
            CONSTRAINT artifact_quarantine_state_verdict_names_scanner CHECK (
                state = 'QUARANTINED' OR (length(scanner_id) > 0 AND length(scanner_version) > 0)
            ),
            -- A REJECTED verdict always carries a non-empty reason: a
            -- scanner failure is a REJECTED-with-reason, never a silent
            -- admit and never a reason-less refusal.
            CONSTRAINT artifact_quarantine_state_rejected_has_reason CHECK (
                state <> 'REJECTED' OR length(reason) > 0
            )
        )
    $ddl$, store_schema, store_schema);

    EXECUTE format(
        'CREATE INDEX artifact_quarantine_state_lookup ON %I.artifact_quarantine_state
            (tenant_id, content_id, seq DESC)',
        store_schema);

    EXECUTE format(
        'CREATE TRIGGER artifact_quarantine_state_append_only BEFORE UPDATE OR DELETE
            ON %I.artifact_quarantine_state FOR EACH ROW EXECUTE FUNCTION forbid_mutation()',
        store_schema);
    EXECUTE format('REVOKE UPDATE, DELETE ON %I.artifact_quarantine_state FROM PUBLIC', store_schema);
    EXECUTE format('GRANT SELECT, INSERT ON %I.artifact_quarantine_state TO hcmnext_app', store_schema);

    EXECUTE format('ALTER TABLE %I.artifact_quarantine_state ENABLE ROW LEVEL SECURITY', store_schema);
    EXECUTE format('ALTER TABLE %I.artifact_quarantine_state FORCE ROW LEVEL SECURITY', store_schema);
    EXECUTE format($pol$
        CREATE POLICY tenant_isolation ON %I.artifact_quarantine_state
            USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
            WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    $pol$, store_schema);
END
$migration$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $migration$
DECLARE
    store_schema text := current_schema() || '_artifact_store';
BEGIN
    EXECUTE format('DROP TABLE %I.artifact_quarantine_state', store_schema);
    EXECUTE format('DROP TABLE %I.artifact_quarantine', store_schema);
END
$migration$;
-- +goose StatementEnd
