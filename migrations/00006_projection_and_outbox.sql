-- Owner: data plane. Phase: P1A.
-- Serving and distribution subplanes (specs/platform-plane-model.md). Both are
-- rebuildable: a projection checkpoint records the last source sequence it
-- applied, and the outbox carries idempotent effect identities for at-least-once
-- distribution after the authoritative commit (spec 8.10).

-- +goose Up

CREATE TABLE projection_checkpoint (
    tenant_id             tenant_ref   NOT NULL,
    projection_name       semantic_key NOT NULL,
    stream_key            semantic_key NOT NULL,
    last_applied_sequence bigint       NOT NULL DEFAULT 0,
    last_applied_digest   content_digest,
    projection_version    cas_version  NOT NULL DEFAULT 1,
    status                text         NOT NULL DEFAULT 'CURRENT',
    updated_at            timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, projection_name, stream_key),
    CONSTRAINT projection_checkpoint_stream
        FOREIGN KEY (tenant_id, stream_key) REFERENCES ledger_stream (tenant_id, stream_key),
    CONSTRAINT projection_checkpoint_sequence_non_negative CHECK (last_applied_sequence >= 0),
    CONSTRAINT projection_checkpoint_status_allowed CHECK (
        status IN ('CURRENT', 'LAGGING', 'STALE', 'DISAGREEING', 'REBUILDING')
    ),
    CONSTRAINT projection_checkpoint_digest_matches_sequence CHECK (
        (last_applied_sequence = 0) = (last_applied_digest IS NULL)
    )
);

CREATE TABLE outbox (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    outbox_id       uuid         NOT NULL,
    effect_identity semantic_key NOT NULL,
    ordering_key    semantic_key NOT NULL,
    schema_ref      semantic_key NOT NULL,
    payload         bytea        NOT NULL,
    status          text         NOT NULL DEFAULT 'PENDING',
    attempts        integer      NOT NULL DEFAULT 0,
    available_at    timestamptz  NOT NULL DEFAULT now(),
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    last_error      text,
    PRIMARY KEY (tenant_id, outbox_id),
    -- One record per idempotent effect: duplicate application is impossible.
    CONSTRAINT outbox_effect_identity_unique UNIQUE (tenant_id, effect_identity),
    CONSTRAINT outbox_schema
        FOREIGN KEY (tenant_id, schema_ref) REFERENCES payload_schema (tenant_id, schema_ref),
    CONSTRAINT outbox_status_allowed CHECK (
        status IN ('PENDING', 'IN_FLIGHT', 'DELIVERED', 'FAILED', 'ABANDONED')
    ),
    CONSTRAINT outbox_attempts_non_negative CHECK (attempts >= 0)
);

CREATE INDEX outbox_dispatch ON outbox (tenant_id, status, available_at, ordering_key);

-- +goose Down
DROP TABLE outbox;
DROP TABLE projection_checkpoint;
