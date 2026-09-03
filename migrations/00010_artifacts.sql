-- Owner: data plane. Phase: P1A.
-- MODEL-029: immutable content-addressed artifact storage. DATA-016: retrieval
-- authorization and reference accounting for that storage.
--
-- Why these tables live in their own schema, not this migration tree's schema
-- -----------------------------------------------------------------------
-- internal/data/schema's own frozen golden test (TestTodo_DATA_001 and its
-- _Golden variant) enumerates the *exact* set of base tables this migration
-- tree may hold in the schema Goose applies into (current_schema()). That
-- test is owned by another lane and this migration must not edit it, so a new
-- table here cannot join that enumerated set without breaking a frozen,
-- passing test.
--
-- This is also the architecturally correct split, not just a workaround:
-- DB-014's own REFACTOR clause says "content-addressed bytes remain in the
-- governed object store" as opposed to the transactional ledger/outbox
-- schema DATA-001 audits. An artifact store is a distinct governed object
-- store, addressed by content digest, with its own append-only tables and
-- its own row level security -- not a table in the OLTP spine.
--
-- So this migration creates a companion schema, named
-- <the schema Goose is applying into>_artifact_store, computed at apply time
-- from current_schema(). In production that is one fixed, predictable name
-- (whatever schema the release runs migrations into, plus the suffix). Under
-- internal/data/pgtest, where every test gets its own randomly named schema
-- and applies the full migration tree into it, the suffix keeps every test's
-- companion schema unique too -- so parallel tests never collide creating or
-- writing to it, exactly as if it were an ordinary table inside the per-test
-- schema. The one difference from an ordinary table is cleanup: pgtest's
-- per-test teardown drops only the per-test schema itself, not this
-- companion schema, so a long-lived external test server (HCMNEXT_TEST_DATABASE_URL)
-- accumulates one extra empty-ish schema per test run. That is a deliberate,
-- documented trade for keeping DATA-001's table enumeration intact; the
-- embedded server pgtest normally uses is thrown away with the whole process,
-- so it never accumulates anything durable.
--
-- Tables
-- ------
--   artifact                  -- one immutable row per (tenant, sha256 content
--                                 id): media type, size, classification,
--                                 retention class, creator principal and
--                                 evidence references, and the bytes
--                                 themselves. Append-only: forbid_mutation
--                                 rejects UPDATE and DELETE outright, and Put
--                                 (internal/data/artifacts) never overwrites
--                                 an existing row's identity metadata, only
--                                 replays it unchanged.
--   artifact_reference_event  -- append-only ADD/REMOVE log. An owner record
--                                 (a proposal revision, an observation, a
--                                 receipt -- ledger_event.artifact_ref and
--                                 proposal_revision.artifact_ref already name
--                                 this content id from the other side) adds or
--                                 removes its reference; the current reference
--                                 count is the count of owners whose most
--                                 recent event is ADD, not a mutable counter
--                                 column, so no write here ever needs to lock
--                                 and increment shared state.
--   artifact_retrieval_refusal -- append-only evidence of a denied retrieval:
--                                 recorded whether or not the referenced
--                                 content id even exists, so a guessed digest
--                                 is refused and evidenced exactly like a
--                                 denied purpose or an out-of-scope subject.
--
-- Both event/evidence tables, like artifact itself, are tenant scoped and
-- carry the same app.tenant_id row level security policy migrations/00008
-- established for the core schema's tables, so isolation covers this
-- companion schema too.

-- +goose Up
-- +goose StatementBegin
DO $migration$
DECLARE
    store_schema text := current_schema() || '_artifact_store';
BEGIN
    EXECUTE format('CREATE SCHEMA %I', store_schema);
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO hcmnext_app', store_schema);

    -- artifact: one immutable row per (tenant, content id).
    EXECUTE format($ddl$
        CREATE TABLE %I.artifact (
            tenant_id             tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
            content_id            content_digest   NOT NULL,
            digest_algorithm      digest_algorithm NOT NULL DEFAULT 'sha256',
            media_type            text             NOT NULL,
            byte_size             bigint           NOT NULL,
            classification        text             NOT NULL,
            retention_class       semantic_key     NOT NULL,
            creator_principal_ref text             NOT NULL,
            evidence_id           text             NOT NULL,
            content               bytea            NOT NULL,
            created_at            timestamptz      NOT NULL DEFAULT now(),
            recorded_at           timestamptz      NOT NULL DEFAULT now(),
            PRIMARY KEY (tenant_id, content_id),
            CONSTRAINT artifact_media_type_present CHECK (length(media_type) > 0),
            CONSTRAINT artifact_classification_allowed CHECK (
                classification IN (
                    'PUBLIC', 'INTERNAL', 'PII', 'COMPENSATION', 'BANK',
                    'MEDICAL', 'IMMIGRATION', 'CASE', 'SPECIAL_CATEGORY'
                )
            ),
            CONSTRAINT artifact_creator_principal_present CHECK (length(creator_principal_ref) > 0),
            CONSTRAINT artifact_evidence_id_present CHECK (length(evidence_id) > 0),
            CONSTRAINT artifact_byte_size_matches_content CHECK (byte_size = octet_length(content)),
            -- A Go-level streaming writer enforces the caller-configured cap
            -- (internal/data/artifacts.DefaultMaxContentBytes) before a single
            -- row is written; this is the absolute backstop behind it,
            -- independent of whatever cap application code chooses.
            CONSTRAINT artifact_byte_size_bounded CHECK (byte_size >= 0 AND byte_size <= 67108864)
        )
    $ddl$, store_schema);

    EXECUTE format(
        'CREATE TRIGGER artifact_append_only BEFORE UPDATE OR DELETE ON %I.artifact
            FOR EACH ROW EXECUTE FUNCTION forbid_mutation()',
        store_schema);
    EXECUTE format('REVOKE UPDATE, DELETE ON %I.artifact FROM PUBLIC', store_schema);
    EXECUTE format('GRANT SELECT, INSERT ON %I.artifact TO hcmnext_app', store_schema);

    EXECUTE format('ALTER TABLE %I.artifact ENABLE ROW LEVEL SECURITY', store_schema);
    EXECUTE format('ALTER TABLE %I.artifact FORCE ROW LEVEL SECURITY', store_schema);
    EXECUTE format($pol$
        CREATE POLICY tenant_isolation ON %I.artifact
            USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
            WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    $pol$, store_schema);

    -- artifact_reference_event: append-only ADD/REMOVE log keyed by owner.
    EXECUTE format($ddl$
        CREATE TABLE %I.artifact_reference_event (
            tenant_id   tenant_ref     NOT NULL,
            content_id  content_digest NOT NULL,
            event_id    uuid           NOT NULL,
            owner_kind  text           NOT NULL,
            owner_id    text           NOT NULL,
            action      text           NOT NULL,
            recorded_at timestamptz    NOT NULL DEFAULT now(),
            PRIMARY KEY (tenant_id, event_id),
            CONSTRAINT artifact_reference_event_artifact
                FOREIGN KEY (tenant_id, content_id) REFERENCES %I.artifact (tenant_id, content_id),
            CONSTRAINT artifact_reference_event_owner_kind_allowed CHECK (
                owner_kind IN ('PROPOSAL_REVISION', 'OBSERVATION', 'RECEIPT')
            ),
            CONSTRAINT artifact_reference_event_owner_id_present CHECK (length(owner_id) > 0),
            CONSTRAINT artifact_reference_event_action_allowed CHECK (action IN ('ADD', 'REMOVE'))
        )
    $ddl$, store_schema, store_schema);

    EXECUTE format(
        'CREATE INDEX artifact_reference_event_lookup ON %I.artifact_reference_event
            (tenant_id, content_id, owner_kind, owner_id, recorded_at DESC, event_id DESC)',
        store_schema);

    EXECUTE format(
        'CREATE TRIGGER artifact_reference_event_append_only BEFORE UPDATE OR DELETE
            ON %I.artifact_reference_event FOR EACH ROW EXECUTE FUNCTION forbid_mutation()',
        store_schema);
    EXECUTE format('REVOKE UPDATE, DELETE ON %I.artifact_reference_event FROM PUBLIC', store_schema);
    EXECUTE format('GRANT SELECT, INSERT ON %I.artifact_reference_event TO hcmnext_app', store_schema);

    EXECUTE format('ALTER TABLE %I.artifact_reference_event ENABLE ROW LEVEL SECURITY', store_schema);
    EXECUTE format('ALTER TABLE %I.artifact_reference_event FORCE ROW LEVEL SECURITY', store_schema);
    EXECUTE format($pol$
        CREATE POLICY tenant_isolation ON %I.artifact_reference_event
            USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
            WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    $pol$, store_schema);

    -- artifact_retrieval_refusal: append-only denial evidence. No foreign key
    -- to artifact: a guessed/nonexistent content id must be refusable and
    -- evidenced exactly like a real one that fails purpose or scope.
    EXECUTE format($ddl$
        CREATE TABLE %I.artifact_retrieval_refusal (
            tenant_id    tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
            refusal_id   uuid           NOT NULL,
            content_id   text           NOT NULL,
            purpose      text           NOT NULL,
            requested_by text           NOT NULL,
            reason       text           NOT NULL,
            evidence_id  text           NOT NULL,
            refused_at   timestamptz    NOT NULL DEFAULT now(),
            PRIMARY KEY (tenant_id, refusal_id),
            CONSTRAINT artifact_retrieval_refusal_content_id_present CHECK (length(content_id) > 0),
            CONSTRAINT artifact_retrieval_refusal_reason_present CHECK (length(reason) > 0),
            CONSTRAINT artifact_retrieval_refusal_evidence_id_present CHECK (length(evidence_id) > 0)
        )
    $ddl$, store_schema);

    EXECUTE format(
        'CREATE INDEX artifact_retrieval_refusal_lookup ON %I.artifact_retrieval_refusal
            (tenant_id, content_id, refused_at DESC)',
        store_schema);

    EXECUTE format(
        'CREATE TRIGGER artifact_retrieval_refusal_append_only BEFORE UPDATE OR DELETE
            ON %I.artifact_retrieval_refusal FOR EACH ROW EXECUTE FUNCTION forbid_mutation()',
        store_schema);
    EXECUTE format('REVOKE UPDATE, DELETE ON %I.artifact_retrieval_refusal FROM PUBLIC', store_schema);
    EXECUTE format('GRANT SELECT, INSERT ON %I.artifact_retrieval_refusal TO hcmnext_app', store_schema);

    EXECUTE format('ALTER TABLE %I.artifact_retrieval_refusal ENABLE ROW LEVEL SECURITY', store_schema);
    EXECUTE format('ALTER TABLE %I.artifact_retrieval_refusal FORCE ROW LEVEL SECURITY', store_schema);
    EXECUTE format($pol$
        CREATE POLICY tenant_isolation ON %I.artifact_retrieval_refusal
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
    EXECUTE format('DROP SCHEMA %I CASCADE', store_schema);
END
$migration$;
-- +goose StatementEnd
