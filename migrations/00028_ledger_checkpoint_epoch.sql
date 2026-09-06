-- Owner: data plane (ledger/checkpoint lane). Phase: P1A.
-- LEDGER-010: signed ledger checkpoints and integrity epochs.
--
-- A checkpoint is a signed statement about where every stream in a tenant's
-- ledger stood, and what its hash chain proved, at one instant. The epochs
-- those checkpoints form are append-only and chained: each names the epoch it
-- follows and carries that epoch's manifest digest, so a removed epoch leaves
-- a detectable hole rather than a gap that closes silently.
--
-- Nothing here is ever rewritten. A checkpoint later found to have been taken
-- over an already-damaged ledger is corrected by a NEW, higher-numbered epoch
-- that names the one it corrects and says why (corrects_epoch_id /
-- corrects_reason); the superseded row keeps its original signature, because
-- the fact that a false attestation was made at a particular time is itself
-- part of the record. Both tables therefore carry the same forbid_mutation()
-- trigger migrations/00001_platform_control.sql defines for ledger_event, and
-- are granted SELECT/INSERT only.
--
-- Key material never enters this schema. Only the signing key's identifier and
-- its public key are stored, both of which are also inside the signed bytes
-- (internal/data/ledger/checkpoint/manifest.go), so a signature cannot be
-- re-attributed to a different key by rewriting the row around it.

-- +goose Up

CREATE TABLE ledger_checkpoint_epoch (
    tenant_id                tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    epoch_id                 uuid             NOT NULL,
    epoch_number             bigint           NOT NULL,
    manifest_schema_version  integer          NOT NULL,

    -- Epoch chain. Both columns are absent on epoch 1 and required on every
    -- later epoch.
    previous_epoch_id        uuid,
    previous_manifest_digest content_digest,

    -- Set only on a corrective epoch. A correction always states its reason.
    corrects_epoch_id        uuid,
    corrects_reason          text,

    -- The physical schema the ledger was read under, so a manifest cannot be
    -- replayed against a schema in which its stream keys mean something else.
    schema_release_version   bigint           NOT NULL,
    schema_release_digest    content_digest   NOT NULL,

    -- The fold over every included stream head, and the digest of the whole
    -- canonical manifest that the signature covers.
    root_digest              content_digest   NOT NULL,
    root_digest_algorithm    digest_algorithm NOT NULL,
    manifest_digest          content_digest   NOT NULL,

    -- Half-open recorded-time window the checkpoint makes its claim over.
    covers_from              timestamptz      NOT NULL,
    covers_to                timestamptz      NOT NULL,
    created_at               timestamptz      NOT NULL,

    signature_algorithm      text             NOT NULL,
    signing_key_id           semantic_key     NOT NULL,
    signing_public_key       text             NOT NULL,
    signature_value          text             NOT NULL,
    signed_at                timestamptz      NOT NULL,

    recorded_at              timestamptz      NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, epoch_id),
    CONSTRAINT ledger_checkpoint_epoch_number_unique UNIQUE (tenant_id, epoch_number),
    CONSTRAINT ledger_checkpoint_epoch_number_positive CHECK (epoch_number >= 1),
    CONSTRAINT ledger_checkpoint_epoch_window_half_open CHECK (covers_from < covers_to),
    -- Epoch 1 opens the chain; every later epoch carries both links.
    CONSTRAINT ledger_checkpoint_epoch_chain_complete CHECK (
        (epoch_number = 1)
        = (previous_epoch_id IS NULL AND previous_manifest_digest IS NULL)
    ),
    CONSTRAINT ledger_checkpoint_epoch_correction_reason CHECK (
        (corrects_epoch_id IS NULL) = (corrects_reason IS NULL)
    ),
    CONSTRAINT ledger_checkpoint_epoch_signature_algorithm_allowed CHECK (
        signature_algorithm IN ('ed25519')
    ),
    CONSTRAINT ledger_checkpoint_epoch_previous
        FOREIGN KEY (tenant_id, previous_epoch_id)
        REFERENCES ledger_checkpoint_epoch (tenant_id, epoch_id),
    CONSTRAINT ledger_checkpoint_epoch_corrects
        FOREIGN KEY (tenant_id, corrects_epoch_id)
        REFERENCES ledger_checkpoint_epoch (tenant_id, epoch_id)
);

CREATE TABLE ledger_checkpoint_stream_head (
    tenant_id       tenant_ref       NOT NULL,
    epoch_id        uuid             NOT NULL,
    stream_key      semantic_key     NOT NULL,
    head_sequence   bigint           NOT NULL,
    chain_hash      content_digest   NOT NULL,
    chain_algorithm digest_algorithm NOT NULL,
    recorded_at     timestamptz      NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, epoch_id, stream_key),
    CONSTRAINT ledger_checkpoint_stream_head_sequence_positive CHECK (head_sequence >= 1),
    CONSTRAINT ledger_checkpoint_stream_head_epoch
        FOREIGN KEY (tenant_id, epoch_id)
        REFERENCES ledger_checkpoint_epoch (tenant_id, epoch_id),
    CONSTRAINT ledger_checkpoint_stream_head_stream
        FOREIGN KEY (tenant_id, stream_key)
        REFERENCES ledger_stream (tenant_id, stream_key)
);

-- A checkpoint is immutable evidence, exactly like the events it attests to.
CREATE TRIGGER ledger_checkpoint_epoch_append_only
    BEFORE UPDATE OR DELETE ON ledger_checkpoint_epoch
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TRIGGER ledger_checkpoint_stream_head_append_only
    BEFORE UPDATE OR DELETE ON ledger_checkpoint_stream_head
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON ledger_checkpoint_epoch FROM PUBLIC;
REVOKE UPDATE, DELETE ON ledger_checkpoint_stream_head FROM PUBLIC;

-- Tenant isolation (DB-017), same shape as every policy in
-- migrations/00008_tenant_isolation.sql: fail closed on a missing or blank
-- app.tenant_id session setting, keyed on internal/data/tenancy.WithTenant.
ALTER TABLE ledger_checkpoint_epoch ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_checkpoint_epoch FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_checkpoint_epoch
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE ledger_checkpoint_stream_head ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_checkpoint_stream_head FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ledger_checkpoint_stream_head
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON ledger_checkpoint_epoch TO hcmnext_app;
GRANT SELECT, INSERT ON ledger_checkpoint_stream_head TO hcmnext_app;

-- +goose Down
REVOKE ALL ON ledger_checkpoint_stream_head FROM hcmnext_app;
REVOKE ALL ON ledger_checkpoint_epoch FROM hcmnext_app;
DROP POLICY tenant_isolation ON ledger_checkpoint_stream_head;
DROP POLICY tenant_isolation ON ledger_checkpoint_epoch;
ALTER TABLE ledger_checkpoint_stream_head NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_checkpoint_stream_head DISABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_checkpoint_epoch NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ledger_checkpoint_epoch DISABLE ROW LEVEL SECURITY;
DROP TABLE ledger_checkpoint_stream_head;
DROP TABLE ledger_checkpoint_epoch;
