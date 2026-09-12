-- LEDGER-011: apply retention, holds and crypto-erasure to one ledger
-- event's inline payload without ever falsifying its chronology.
--
-- ledger_event (migration 00005) is append-only by construction: a BEFORE
-- UPDATE OR DELETE trigger forbids mutation, UPDATE/DELETE are revoked from
-- PUBLIC, and hcmnext_app is granted only SELECT/INSERT on it and its
-- partitions. That is deliberate and this migration does not weaken it --
-- Erase (internal/data/ledger/disposition) never attempts to touch
-- ledger_event at all. Instead, disposition of one event's payload is
-- recorded as its own append-only fact in ledger_payload_disposition, keyed
-- to the exact event it disposes and carrying that event's own digest as
-- evidence that the chronology-bearing columns were read, not altered.
--
-- The Go read path (disposition.ReadView) is what actually withholds the
-- payload once a disposition row exists: it never returns
-- ledger.EventRecord.Payload to a caller once this table names that event,
-- regardless of what ledger_event physically still stores. See
-- internal/data/ledger/disposition's package doc for the honest limits of
-- that boundary (this migration does not, and cannot without abandoning
-- ledger_event's append-only guarantee, physically zero the payload bytes at
-- rest).
--
-- classification is the boundary LEDGER-011's REFACTOR names explicitly:
-- STANDARD classification always erases by mechanism OVERWRITE into state
-- PAYLOAD_ERASED; ENCRYPTED_AT_REST classification always erases by
-- mechanism CRYPTO_ERASURE into state RESTRICTED. The pairing is fixed by
-- ledger_payload_disposition_classification_bound below, independent of the
-- Go layer, so a row can never claim a classification and a mechanism that
-- do not belong together.
--
-- A hold blocks disposition the same way it already blocks a copy-level
-- disposal (record_copy_link_hold_blocks_disposal, migration 00284):
-- disposition.Erase composes recordsmeta.DisposeCopy against the tracked
-- copy naming this event (record_copy_link.ledger_stream/ledger_sequence),
-- and DisposeCopy's own row lock plus record_copy_link_hold_blocks_disposal
-- are what actually refuse a held event -- this migration adds no new hold
-- logic, only the lookup index a held-check needs to find the tracked copy
-- for an arbitrary (stream, sequence) without a full scan.

-- +goose Up

CREATE TABLE ledger_payload_disposition (
    tenant_id         tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    stream_key        semantic_key     NOT NULL,
    sequence          bigint           NOT NULL,
    event_id          uuid             NOT NULL,
    declaration_id    uuid             NOT NULL,
    link_id           uuid             NOT NULL,
    classification    text             NOT NULL,
    mechanism         text             NOT NULL,
    state             text             NOT NULL,
    reason            text             NOT NULL,
    actor             semantic_key     NOT NULL,
    key_ref           text,
    key_destroyed_at  timestamptz,
    digest            content_digest   NOT NULL,
    digest_algorithm  digest_algorithm NOT NULL,
    recorded_at       timestamptz      NOT NULL,
    PRIMARY KEY (tenant_id, stream_key, sequence),
    CONSTRAINT ledger_payload_disposition_event
        FOREIGN KEY (tenant_id, stream_key, sequence) REFERENCES ledger_event (tenant_id, stream_key, sequence),
    CONSTRAINT ledger_payload_disposition_event_id
        FOREIGN KEY (tenant_id, event_id) REFERENCES ledger_event (tenant_id, event_id),
    CONSTRAINT ledger_payload_disposition_declaration
        FOREIGN KEY (tenant_id, declaration_id) REFERENCES record_declaration (tenant_id, declaration_id),
    CONSTRAINT ledger_payload_disposition_link
        FOREIGN KEY (tenant_id, link_id) REFERENCES record_copy_link (tenant_id, link_id),
    CONSTRAINT ledger_payload_disposition_sequence_positive CHECK (sequence >= 1),
    CONSTRAINT ledger_payload_disposition_classification_allowed CHECK (
        classification IN ('STANDARD', 'ENCRYPTED_AT_REST')
    ),
    CONSTRAINT ledger_payload_disposition_mechanism_allowed CHECK (
        mechanism IN ('OVERWRITE', 'CRYPTO_ERASURE')
    ),
    CONSTRAINT ledger_payload_disposition_state_allowed CHECK (
        state IN ('PAYLOAD_ERASED', 'RESTRICTED')
    ),
    -- The classification -> mechanism -> state pairing is exhaustive and
    -- fixed here, independent of the Go layer's own Classify function: a row
    -- can never pair ENCRYPTED_AT_REST with OVERWRITE/PAYLOAD_ERASED, or
    -- STANDARD with CRYPTO_ERASURE/RESTRICTED.
    CONSTRAINT ledger_payload_disposition_classification_bound CHECK (
        (classification = 'STANDARD' AND mechanism = 'OVERWRITE' AND state = 'PAYLOAD_ERASED')
        OR (classification = 'ENCRYPTED_AT_REST' AND mechanism = 'CRYPTO_ERASURE' AND state = 'RESTRICTED')
    ),
    -- A crypto-erasure names the key it destroyed and when; an overwrite
    -- names neither, because there was no key to destroy.
    CONSTRAINT ledger_payload_disposition_key_matches_mechanism CHECK (
        (mechanism = 'CRYPTO_ERASURE') = (key_ref IS NOT NULL)
        AND (key_ref IS NULL) = (key_destroyed_at IS NULL)
    ),
    CONSTRAINT ledger_payload_disposition_reason_present CHECK (length(reason) > 0),
    CONSTRAINT ledger_payload_disposition_actor_present CHECK (length(actor) > 0)
);

CREATE INDEX ledger_payload_disposition_declaration_lookup
    ON ledger_payload_disposition (tenant_id, declaration_id);

CREATE TRIGGER ledger_payload_disposition_append_only
    BEFORE UPDATE OR DELETE ON ledger_payload_disposition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON ledger_payload_disposition FROM PUBLIC;

ALTER TABLE ledger_payload_disposition ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_payload_disposition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_payload_disposition
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON ledger_payload_disposition TO hcmnext_app;

-- A hold check against an arbitrary (stream, sequence) needs to find the
-- tracked copy that names it without a full table scan; nothing before this
-- migration ever looked record_copy_link up by its ledger reference, only by
-- (declaration_id, copy_type, store_ref) or by link_id.
CREATE INDEX record_copy_link_ledger_lookup
    ON record_copy_link (tenant_id, ledger_stream, ledger_sequence)
    WHERE ledger_stream <> '';

-- +goose Down
-- This migration's own DROP TABLE would be perfectly safe in isolation: it
-- only removes a table this migration itself created. It is marked
-- irreversible anyway because migrations/migrations_test.go's
-- TestNewestReversibleVersionStopsBelowDeclaredIrreversibles enforces a
-- chain-wide invariant, not a per-file one: goose Down runs newest-to-oldest,
-- so any real rollback below the tip must pass back through every migration
-- in between in order. Migrations 00279 through 00284 already declare
-- themselves irreversible, so no rollback can ever reach this file's Down
-- section without first hitting one of those and stopping -- claiming this
-- migration is reversible would be true only in a state no rollback can
-- ever actually be in. NewestReversibleVersion() reports the highest
-- version above which every migration is irreversible; once that streak
-- starts, every later migration keeps it true rather than reopening a
-- rollback path that does not really exist.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00285 is irreversible: migrations 00279-00284 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
