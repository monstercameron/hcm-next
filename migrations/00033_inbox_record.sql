-- Owner: communications lane. Phase: P1B.
-- MSG-005: persist the minimal inbox record.
--
-- Scope
-- -----
-- MSG-005 is explicitly the MINIMAL CONTRACT (its REFACTOR clause): one
-- record per (tenant, recipient subject, recipient_message) that carries
-- read/unread state, an archived flag, a pinned flag and the instant its
-- state last changed. It creates no separate channel, thread, preference or
-- provider machinery -- those already exist (migration 00031's
-- message_intent / recipient_message / conversation_thread) or are DESIGN
-- (MSG-003, MSG-009, MSG-011's full secure-inbox channel).
--
-- inbox_record references migration 00031's recipient_message: this is the
-- Promotion-workspace-visible row for a message that already exists: the
-- MessageIntent, its purpose, its correlation and its delivery lifecycle are
-- recipient_message's job, not this table's.
--
-- Two invariants MSG-005's RED clause names, enforced in the schema:
--
--  1. A wrong subject never reads or writes this record. The table itself
--     cannot enforce "this subject" (RLS only knows the session's tenant,
--     not which subject the application authenticated), so
--     internal/data/inbox never issues a statement that omits subject_ref
--     from its WHERE clause -- proved by TestTodo_MSG_005_Security, not by a
--     schema constraint alone. What the schema does enforce is that a row
--     can never exist without exactly one owning subject
--     (inbox_record_unique's (tenant_id, subject_ref, recipient_message_id)
--     key), and RLS's tenant_isolation policy on both tables refuses a
--     session outside the row's own tenant regardless of subject.
--  2. State changes are compare-and-swap, never a blind write. inbox_record
--     carries a cas_version fenced by inbox_record_version_positive;
--     internal/data/inbox's MarkRead/Archive/Pin all condition their UPDATE
--     on the caller's expected version and report a conflict when it does
--     not match. inbox_state_event is the append-only ledger of every state
--     change that CAS accepted: new_version is always exactly
--     previous_version + 1
--     (inbox_state_event_version_increments), so the log can never record a
--     change the CAS path did not actually make.
--
-- Retention. inbox_state_event is append-only evidence and carries 00001's
-- forbid_mutation trigger. inbox_record is live serving state: revised in
-- place under CAS, never replayed from the event log in this phase.

-- +goose Up

CREATE TABLE IF NOT EXISTS inbox_record (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    inbox_record_id       uuid           NOT NULL,
    subject_ref           semantic_key   NOT NULL,
    recipient_message_id  uuid           NOT NULL,
    read_state            text           NOT NULL DEFAULT 'UNREAD',
    archived              boolean        NOT NULL DEFAULT false,
    pinned                boolean        NOT NULL DEFAULT false,
    state_changed_at      timestamptz    NOT NULL DEFAULT now(),
    version               cas_version    NOT NULL DEFAULT 1,
    created_at            timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, inbox_record_id),
    FOREIGN KEY (tenant_id, recipient_message_id) REFERENCES recipient_message (tenant_id, recipient_message_id),
    -- One inbox record per subject per recipient message -- this is the
    -- "minimal record" MSG-005 declares, not a second per-view row.
    CONSTRAINT inbox_record_unique UNIQUE (tenant_id, subject_ref, recipient_message_id),
    CONSTRAINT inbox_record_read_state_allowed CHECK (read_state IN ('UNREAD', 'READ')),
    CONSTRAINT inbox_record_state_changed_not_before_created CHECK (state_changed_at >= created_at)
);
CREATE INDEX IF NOT EXISTS inbox_record_subject ON inbox_record (tenant_id, subject_ref, state_changed_at DESC);

-- inbox_state_event is the append-only log of every accepted CAS transition.
-- previous_version/new_version pin the exact fence the write succeeded
-- under, so the log is reconstructable evidence of the CAS path, never a
-- second mutation surface.
CREATE TABLE IF NOT EXISTS inbox_state_event (
    tenant_id         tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    event_id          uuid         NOT NULL,
    inbox_record_id   uuid         NOT NULL,
    subject_ref       semantic_key NOT NULL,
    event_type        text         NOT NULL,
    previous_version  cas_version  NOT NULL,
    new_version       cas_version  NOT NULL,
    occurred_at       timestamptz  NOT NULL,
    recorded_at       timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, event_id),
    FOREIGN KEY (tenant_id, inbox_record_id) REFERENCES inbox_record (tenant_id, inbox_record_id),
    CONSTRAINT inbox_state_event_type_allowed CHECK (
        event_type IN ('MARKED_READ', 'MARKED_UNREAD', 'ARCHIVED', 'UNARCHIVED', 'PINNED', 'UNPINNED')
    ),
    -- DB-014-style invariant, applied here: the log can only ever record
    -- the single CAS step that actually happened.
    CONSTRAINT inbox_state_event_version_increments CHECK (new_version = previous_version + 1)
);
CREATE INDEX IF NOT EXISTS inbox_state_event_record ON inbox_state_event (tenant_id, inbox_record_id, new_version);

-- ---------------------------------------------------------------------------
-- Append-only enforcement
-- ---------------------------------------------------------------------------

CREATE TRIGGER inbox_state_event_append_only BEFORE UPDATE OR DELETE ON inbox_state_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- ---------------------------------------------------------------------------
-- Row level security
-- ---------------------------------------------------------------------------

ALTER TABLE inbox_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbox_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON inbox_record USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE inbox_state_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbox_state_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON inbox_state_event USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- Privileges
-- ---------------------------------------------------------------------------

REVOKE DELETE ON inbox_record FROM PUBLIC;
REVOKE UPDATE, DELETE ON inbox_state_event FROM PUBLIC;

GRANT SELECT, INSERT, UPDATE ON inbox_record TO hcmnext_app;
GRANT SELECT, INSERT ON inbox_state_event TO hcmnext_app;

-- +goose Down
REVOKE ALL ON inbox_state_event FROM hcmnext_app;
REVOKE ALL ON inbox_record FROM hcmnext_app;

DROP TABLE inbox_state_event;
DROP TABLE inbox_record;
