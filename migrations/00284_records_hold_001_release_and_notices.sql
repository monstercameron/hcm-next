-- RECORDS-HOLD-001: closes four gaps left by DB-015 (migration 00032) and
-- DATA-018 (migration 00056) in the legal-hold model.
--
--  1. PropagateHold only ever flipped record_copy_link.hold_state; the
--     hold_intersection table -- the model that makes a hold enforceable
--     against a specific copy rather than a statement of intent about a
--     declaration -- was never populated per copy. hold_intersection gains
--     a link_id column naming the exact record_copy_link row a copy-level
--     grip covers. Its uniqueness moves from "one row per (hold,
--     declaration)" to three disjoint cases, enforced by three partial
--     unique indexes instead of the old table constraint: one
--     declaration-level grip with no link_id and no copy_id (the DB-015
--     shape, kept working unmodified), one grip per link_id, and one grip
--     per copy_id. A hold can now grip every copy of a declaration
--     individually and still refuse to grip the same copy twice.
--  2. There was no hold-level release, only ReleaseIntersection (recordsmeta
--     package, one intersection at a time). No new table is needed for the
--     release path itself; internal/data/recordsmeta.ReleaseHold walks
--     every intersection for a hold (optionally scoped to one declaration,
--     which is what makes a legal_hold.status of PARTIALLY_RELEASED
--     possible) and recalculates record_copy_link's hold_state and
--     disposition_state from whichever ACTIVE intersections survive, so a
--     copy still gripped by a second hold is never resurrected.
--  3. There were no notices or acknowledgements for a hold at all.
--     legal_hold_notice records that a hold was issued to a named
--     custodian or recipient; legal_hold_acknowledgement records that one
--     specific notice was acknowledged. A notice is never mutated to say it
--     was acknowledged -- the acknowledgement is its own append-only row,
--     so "was this notice acknowledged, and when" is a join, never a
--     rewrite.
--  4. record_copy_link had no schema-level guarantee that a HELD copy could
--     never be marked DISPOSED. retention_disposition already has this
--     guarantee independent of the Go layer
--     (retention_disposition_hold_blocks_execution, 00032);
--     record_copy_link_hold_blocks_disposal closes the same gap for the
--     copy-level table, for every copy_type record_copy_link allows.

-- +goose Up

ALTER TABLE hold_intersection ADD COLUMN link_id uuid;
ALTER TABLE hold_intersection
    ADD CONSTRAINT hold_intersection_link_fk
    FOREIGN KEY (tenant_id, link_id) REFERENCES record_copy_link (tenant_id, link_id);

ALTER TABLE hold_intersection DROP CONSTRAINT hold_intersection_unique;

-- One declaration-level grip (no named copy or link) per hold/declaration --
-- the shape DB-015 shipped with, unchanged.
CREATE UNIQUE INDEX hold_intersection_unique_general ON hold_intersection (tenant_id, hold_id, declaration_id)
    WHERE link_id IS NULL AND copy_id IS NULL;
-- One grip per record_copy_link row per hold/declaration -- what
-- PropagateHold now writes, one per tracked copy.
CREATE UNIQUE INDEX hold_intersection_unique_link ON hold_intersection (tenant_id, hold_id, declaration_id, link_id)
    WHERE link_id IS NOT NULL;
-- One grip per data_copy row per hold/declaration -- the discovery-inventory
-- shape DB-015's own integration test already exercises.
CREATE UNIQUE INDEX hold_intersection_unique_copy ON hold_intersection (tenant_id, hold_id, declaration_id, copy_id)
    WHERE copy_id IS NOT NULL;

CREATE INDEX hold_intersection_link ON hold_intersection (tenant_id, link_id) WHERE state = 'ACTIVE';

-- A held copy can never be recorded as disposed, independent of the Go
-- layer, mirroring retention_disposition_hold_blocks_execution (00032).
ALTER TABLE record_copy_link
    ADD CONSTRAINT record_copy_link_hold_blocks_disposal
    CHECK (hold_state <> 'HELD' OR disposition_state <> 'DISPOSED');

-- legal_hold_notice: durable evidence that a hold was issued to a named
-- custodian or recipient.
CREATE TABLE legal_hold_notice (
    tenant_id      tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    notice_id      uuid           NOT NULL,
    hold_id        uuid           NOT NULL,
    recipient_ref  semantic_key   NOT NULL,
    recipient_role text           NOT NULL DEFAULT 'CUSTODIAN',
    issued_by      semantic_key   NOT NULL,
    issued_at      timestamptz    NOT NULL,
    method         text           NOT NULL,
    content_digest content_digest NOT NULL,
    PRIMARY KEY (tenant_id, notice_id),
    FOREIGN KEY (tenant_id, hold_id) REFERENCES legal_hold (tenant_id, hold_id),
    CONSTRAINT legal_hold_notice_role_allowed CHECK (recipient_role IN ('CUSTODIAN', 'RECIPIENT', 'COUNSEL')),
    CONSTRAINT legal_hold_notice_method_present CHECK (length(method) > 0)
);
CREATE INDEX legal_hold_notice_hold ON legal_hold_notice (tenant_id, hold_id);

CREATE TRIGGER legal_hold_notice_append_only
    BEFORE UPDATE OR DELETE ON legal_hold_notice
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE legal_hold_notice ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_hold_notice FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_hold_notice
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- legal_hold_acknowledgement: durable evidence that one specific notice was
-- acknowledged. legal_hold_acknowledgement_unique caps it at one
-- acknowledgement per notice.
CREATE TABLE legal_hold_acknowledgement (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    acknowledgement_id uuid           NOT NULL,
    notice_id          uuid           NOT NULL,
    hold_id            uuid           NOT NULL,
    acknowledged_by    semantic_key   NOT NULL,
    acknowledged_at    timestamptz    NOT NULL,
    method             text           NOT NULL,
    content_digest     content_digest NOT NULL,
    PRIMARY KEY (tenant_id, acknowledgement_id),
    FOREIGN KEY (tenant_id, notice_id) REFERENCES legal_hold_notice (tenant_id, notice_id),
    FOREIGN KEY (tenant_id, hold_id) REFERENCES legal_hold (tenant_id, hold_id),
    CONSTRAINT legal_hold_acknowledgement_unique UNIQUE (tenant_id, notice_id),
    CONSTRAINT legal_hold_acknowledgement_method_present CHECK (length(method) > 0)
);

CREATE TRIGGER legal_hold_acknowledgement_append_only
    BEFORE UPDATE OR DELETE ON legal_hold_acknowledgement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE legal_hold_acknowledgement ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_hold_acknowledgement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_hold_acknowledgement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON legal_hold_notice FROM PUBLIC;
REVOKE UPDATE, DELETE ON legal_hold_acknowledgement FROM PUBLIC;

GRANT SELECT, INSERT ON legal_hold_notice TO hcmnext_app;
GRANT SELECT, INSERT ON legal_hold_acknowledgement TO hcmnext_app;

-- +goose Down
-- legal_hold_notice and legal_hold_acknowledgement are durable evidence the
-- moment they are written, and hold_intersection's per-copy grip is
-- load-bearing for every hold placed after this migration lands. This
-- migration is intentionally irreversible.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00284 is irreversible: legal hold notices/acknowledgements are durable evidence and hold_intersection''s per-copy grip cannot be safely unwound'; END $$;
-- +goose StatementEnd
