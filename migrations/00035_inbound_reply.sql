-- Owner: communications lane. Phase: PHASE_2.
-- MSG-011: governed inbound replies and conversations.
--
-- Scope
-- -----
-- MSG-011 lets a person reply to a governed outbound message (migration
-- 00031's message_intent / recipient_message / conversation_thread /
-- thread_participant) and have that reply resolved back to the exact
-- recipient_message it answers, without ever trusting the inbound transport
-- to say so honestly. Two tables:
--
--   * inbound_message -- the append-only record of what arrived: which
--     thread it claims to belong to, who the channel says sent it, when it
--     was received, the channel's own message id (for dedupe), a digest and
--     a governed-store reference for its content, and its classification.
--     It never stores the raw body: content_ref points into the same
--     governed object store artifact.go's tables use, exactly the way
--     migration 00021's evidence_artifact and 00025's schema snapshot keep
--     content-addressed bytes out of an ordinary relational row.
--   * reply_binding -- the one resolution attempt for one inbound_message,
--     fenced by compare-and-swap. It starts UNRESOLVED, and internal/data/
--     inboundmsg's Bind is the only path out: it looks up the
--     recipient_message the channel's correlation token actually names and
--     moves to BOUND only when that message belongs to this same tenant and
--     to the participant who is claimed to be replying; any other outcome
--     -- no match, another tenant's message, another participant's message
--     -- moves to REJECTED with a reason, never silently to BOUND. Nothing
--     resumes a workflow from an unresolved or rejected binding.
--
-- Two invariants MSG-011's RED clause names, enforced in the schema:
--
--  1. Content and identity are references, not raw data. inbound_message
--     stores a sha256 content_digest plus a non-blank content_ref into the
--     governed store, never the message body; sender_endpoint_digest is a
--     digest, never the sender's address, the same discipline migration
--     00031's delivery_endpoint already keeps.
--  2. A reply cannot resume a workflow until it is actually bound.
--     reply_binding_bound_requires_recipient CHECK means the only way a row
--     ever names a recipient_message is state = 'BOUND'; a UNRESOLVED or
--     REJECTED row carries no recipient_message_id at all, so a caller that
--     naively read the column and skipped the state check would find
--     nothing to act on rather than an unverified guess.
--
-- Retention. inbound_message is append-only evidence and carries 00001's
-- forbid_mutation trigger: quarantined or hostile content is never edited
-- in place, only ever superseded by administrative process outside this
-- table. reply_binding is live serving state, revised in place under
-- compare-and-swap exactly once (UNRESOLVED -> BOUND or UNRESOLVED ->
-- REJECTED); it carries no append-only ledger of its own transition in this
-- phase (a second ledger table is DESIGN, not required by MSG-011's GREEN
-- clause), so its own CHECK constraints are the only invariant a caller
-- cannot bypass by mistake.

-- +goose Up

CREATE TABLE IF NOT EXISTS inbound_message (
    tenant_id              tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    inbound_message_id     uuid           NOT NULL,
    thread_id              uuid           NOT NULL,
    channel                text           NOT NULL,
    sender_endpoint_digest content_digest NOT NULL,
    received_at            timestamptz    NOT NULL,
    provider_message_id    text           NOT NULL,
    content_digest         content_digest NOT NULL,
    content_ref            text           NOT NULL,
    classification         text           NOT NULL,
    created_at             timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, inbound_message_id),
    FOREIGN KEY (tenant_id, thread_id) REFERENCES conversation_thread (tenant_id, thread_id),
    -- Dedupe: the same provider can redeliver the same inbound message
    -- (webhook retries, IMAP re-sync); the channel's own message id is
    -- unique per channel, never globally, so the key is composite.
    CONSTRAINT inbound_message_provider_unique UNIQUE (tenant_id, channel, provider_message_id),
    CONSTRAINT inbound_message_channel_allowed CHECK (
        channel IN ('EMAIL', 'SMS', 'PUSH', 'INBOX', 'VOICE', 'POSTAL', 'WEBHOOK')
    ),
    CONSTRAINT inbound_message_provider_id_present CHECK (
        provider_message_id <> '' AND provider_message_id = btrim(provider_message_id)
    ),
    -- MSG-011 RED: content is a governed reference, never the raw body.
    CONSTRAINT inbound_message_content_ref_present CHECK (
        content_ref <> '' AND content_ref = btrim(content_ref)
    ),
    CONSTRAINT inbound_message_classification_present CHECK (
        classification <> '' AND classification = btrim(classification)
    )
);
CREATE INDEX IF NOT EXISTS inbound_message_thread ON inbound_message (tenant_id, thread_id, received_at);

CREATE TABLE IF NOT EXISTS reply_binding (
    tenant_id            tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    reply_binding_id     uuid         NOT NULL,
    inbound_message_id   uuid         NOT NULL,
    correlation_token    semantic_key NOT NULL,
    recipient_message_id uuid,
    state                text         NOT NULL DEFAULT 'UNRESOLVED',
    rejection_reason     text         NOT NULL DEFAULT '',
    resolved_at          timestamptz,
    version              cas_version  NOT NULL DEFAULT 1,
    created_at           timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, reply_binding_id),
    FOREIGN KEY (tenant_id, inbound_message_id) REFERENCES inbound_message (tenant_id, inbound_message_id),
    FOREIGN KEY (tenant_id, recipient_message_id) REFERENCES recipient_message (tenant_id, recipient_message_id),
    -- One resolution attempt per inbound message.
    CONSTRAINT reply_binding_inbound_unique UNIQUE (tenant_id, inbound_message_id),
    CONSTRAINT reply_binding_state_allowed CHECK (state IN ('UNRESOLVED', 'BOUND', 'REJECTED')),
    -- MSG-011 RED: a workflow cannot be resumed from a guess. Only a BOUND
    -- row ever names the recipient_message it resolved to.
    CONSTRAINT reply_binding_bound_requires_recipient CHECK (
        state <> 'BOUND' OR recipient_message_id IS NOT NULL
    ),
    CONSTRAINT reply_binding_unresolved_has_no_recipient CHECK (
        state <> 'UNRESOLVED' OR recipient_message_id IS NULL
    ),
    CONSTRAINT reply_binding_rejected_requires_reason CHECK (
        state <> 'REJECTED' OR rejection_reason <> ''
    ),
    CONSTRAINT reply_binding_resolved_at_set CHECK (
        state = 'UNRESOLVED' OR resolved_at IS NOT NULL
    )
);
CREATE INDEX IF NOT EXISTS reply_binding_correlation ON reply_binding (tenant_id, correlation_token);

-- ---------------------------------------------------------------------------
-- Append-only enforcement
-- ---------------------------------------------------------------------------

CREATE TRIGGER inbound_message_append_only BEFORE UPDATE OR DELETE ON inbound_message
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- ---------------------------------------------------------------------------
-- Row level security
-- ---------------------------------------------------------------------------

ALTER TABLE inbound_message ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbound_message FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON inbound_message USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE reply_binding ENABLE ROW LEVEL SECURITY;
ALTER TABLE reply_binding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON reply_binding USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- Privileges
-- ---------------------------------------------------------------------------

REVOKE UPDATE, DELETE ON inbound_message FROM PUBLIC;
REVOKE DELETE ON reply_binding FROM PUBLIC;

GRANT SELECT, INSERT ON inbound_message TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON reply_binding TO hcmnext_app;

-- +goose Down
REVOKE ALL ON reply_binding FROM hcmnext_app;
REVOKE ALL ON inbound_message FROM hcmnext_app;

DROP TABLE reply_binding;
DROP TABLE inbound_message;
