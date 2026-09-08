-- Owner: data plane. OBS-013 / DATA-007 durable asynchronous envelope metadata.
-- Business causation is durable and bounded; trace linkage is nullable,
-- diagnostic-only context with independent expiry.

-- +goose Up

ALTER TABLE outbox
    ADD COLUMN IF NOT EXISTS correlation_id text,
    ADD COLUMN IF NOT EXISTS causation_id text,
    ADD COLUMN IF NOT EXISTS logical_operation_id text,
    ADD COLUMN IF NOT EXISTS attempt_id text,
    ADD COLUMN IF NOT EXISTS trace_id text,
    ADD COLUMN IF NOT EXISTS trace_span_id text,
    ADD COLUMN IF NOT EXISTS trace_flags smallint,
    ADD COLUMN IF NOT EXISTS trace_state text,
    ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;

ALTER TABLE outbox
    ADD CONSTRAINT outbox_causal_metadata_bounded CHECK (
        (correlation_id IS NULL OR (length(btrim(correlation_id)) BETWEEN 1 AND 128)) AND
        (causation_id IS NULL OR (length(btrim(causation_id)) BETWEEN 1 AND 128)) AND
        (logical_operation_id IS NULL OR (length(btrim(logical_operation_id)) BETWEEN 1 AND 128)) AND
        (attempt_id IS NULL OR (length(btrim(attempt_id)) BETWEEN 1 AND 128))
    ),
    ADD CONSTRAINT outbox_causal_metadata_complete CHECK (
        (correlation_id IS NULL AND causation_id IS NULL AND logical_operation_id IS NULL AND attempt_id IS NULL) OR
        (correlation_id IS NOT NULL AND causation_id IS NOT NULL AND logical_operation_id IS NOT NULL AND attempt_id IS NOT NULL)
    ),
    ADD CONSTRAINT outbox_trace_metadata_bounded CHECK (
        (trace_id IS NULL AND trace_span_id IS NULL AND trace_flags IS NULL AND trace_state IS NULL AND trace_link_expires_at IS NULL) OR
        (trace_id IS NOT NULL AND trace_id ~ '^[0-9a-f]{32}$' AND trace_id <> repeat('0', 32) AND
         trace_span_id IS NOT NULL AND trace_span_id ~ '^[0-9a-f]{16}$' AND trace_span_id <> repeat('0', 16) AND
         trace_flags IS NOT NULL AND trace_flags BETWEEN 0 AND 255 AND
         (trace_state IS NULL OR length(trace_state) <= 256))
    );

CREATE INDEX IF NOT EXISTS outbox_logical_operation
    ON outbox (tenant_id, logical_operation_id, attempt_id)
    WHERE logical_operation_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS outbox_logical_operation;
ALTER TABLE outbox
    DROP CONSTRAINT IF EXISTS outbox_trace_metadata_bounded,
    DROP CONSTRAINT IF EXISTS outbox_causal_metadata_complete,
    DROP CONSTRAINT IF EXISTS outbox_causal_metadata_bounded,
    DROP COLUMN IF EXISTS trace_link_expires_at,
    DROP COLUMN IF EXISTS trace_state,
    DROP COLUMN IF EXISTS trace_flags,
    DROP COLUMN IF EXISTS trace_span_id,
    DROP COLUMN IF EXISTS trace_id,
    DROP COLUMN IF EXISTS attempt_id,
    DROP COLUMN IF EXISTS logical_operation_id,
    DROP COLUMN IF EXISTS causation_id,
    DROP COLUMN IF EXISTS correlation_id;
