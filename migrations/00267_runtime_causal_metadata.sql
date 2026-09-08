-- Owner: workflow runtime. OBS-013 durable continuation, timer and ready-work metadata.
-- Causal identifiers are business correlation only; trace columns are optional
-- diagnostic context with independent expiry and never participate in authority.

-- +goose Up

ALTER TABLE workflow_continuation
    ADD COLUMN IF NOT EXISTS correlation_id text,
    ADD COLUMN IF NOT EXISTS causation_id text,
    ADD COLUMN IF NOT EXISTS logical_operation_id text,
    ADD COLUMN IF NOT EXISTS attempt_id text,
    ADD COLUMN IF NOT EXISTS trace_id text,
    ADD COLUMN IF NOT EXISTS trace_span_id text,
    ADD COLUMN IF NOT EXISTS trace_flags smallint,
    ADD COLUMN IF NOT EXISTS trace_state text,
    ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;

ALTER TABLE workflow_timer
    ADD COLUMN IF NOT EXISTS correlation_id text,
    ADD COLUMN IF NOT EXISTS causation_id text,
    ADD COLUMN IF NOT EXISTS logical_operation_id text,
    ADD COLUMN IF NOT EXISTS attempt_id text,
    ADD COLUMN IF NOT EXISTS trace_id text,
    ADD COLUMN IF NOT EXISTS trace_span_id text,
    ADD COLUMN IF NOT EXISTS trace_flags smallint,
    ADD COLUMN IF NOT EXISTS trace_state text,
    ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;

ALTER TABLE workflow_ready_work
    ADD COLUMN IF NOT EXISTS correlation_id text,
    ADD COLUMN IF NOT EXISTS causation_id text,
    ADD COLUMN IF NOT EXISTS logical_operation_id text,
    ADD COLUMN IF NOT EXISTS attempt_id text,
    ADD COLUMN IF NOT EXISTS trace_id text,
    ADD COLUMN IF NOT EXISTS trace_span_id text,
    ADD COLUMN IF NOT EXISTS trace_flags smallint,
    ADD COLUMN IF NOT EXISTS trace_state text,
    ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;

-- +goose StatementBegin
DO $$
DECLARE
    table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['workflow_continuation', 'workflow_timer', 'workflow_ready_work'] LOOP
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((correlation_id IS NULL OR (length(btrim(correlation_id)) BETWEEN 1 AND 128)) AND (causation_id IS NULL OR (length(btrim(causation_id)) BETWEEN 1 AND 128)) AND (logical_operation_id IS NULL OR (length(btrim(logical_operation_id)) BETWEEN 1 AND 128)) AND (attempt_id IS NULL OR (length(btrim(attempt_id)) BETWEEN 1 AND 128)))', table_name, table_name || '_causal_metadata_bounded');
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((correlation_id IS NULL AND causation_id IS NULL AND logical_operation_id IS NULL AND attempt_id IS NULL) OR (correlation_id IS NOT NULL AND causation_id IS NOT NULL AND logical_operation_id IS NOT NULL AND attempt_id IS NOT NULL))', table_name, table_name || '_causal_metadata_complete');
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((trace_id IS NULL AND trace_span_id IS NULL AND trace_flags IS NULL AND trace_state IS NULL AND trace_link_expires_at IS NULL) OR (trace_id IS NOT NULL AND trace_id ~ ''^[0-9a-f]{32}$'' AND trace_id <> repeat(''0'', 32) AND trace_span_id IS NOT NULL AND trace_span_id ~ ''^[0-9a-f]{16}$'' AND trace_span_id <> repeat(''0'', 16) AND trace_flags IS NOT NULL AND trace_flags BETWEEN 0 AND 255 AND (trace_state IS NULL OR length(trace_state) <= 256)))', table_name, table_name || '_trace_metadata_bounded');
    END LOOP;
END $$;
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS workflow_continuation_logical_operation
    ON workflow_continuation (tenant_id, logical_operation_id, attempt_id)
    WHERE logical_operation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS workflow_timer_logical_operation
    ON workflow_timer (tenant_id, logical_operation_id, attempt_id)
    WHERE logical_operation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS workflow_ready_work_logical_operation
    ON workflow_ready_work (tenant_id, logical_operation_id, attempt_id)
    WHERE logical_operation_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS workflow_ready_work_logical_operation;
DROP INDEX IF EXISTS workflow_timer_logical_operation;
DROP INDEX IF EXISTS workflow_continuation_logical_operation;
-- +goose StatementBegin
DO $$
DECLARE
    table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['workflow_continuation', 'workflow_timer', 'workflow_ready_work'] LOOP
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT IF EXISTS %I, DROP CONSTRAINT IF EXISTS %I, DROP CONSTRAINT IF EXISTS %I', table_name, table_name || '_trace_metadata_bounded', table_name || '_causal_metadata_complete', table_name || '_causal_metadata_bounded');
        EXECUTE format('ALTER TABLE %I DROP COLUMN IF EXISTS trace_link_expires_at, DROP COLUMN IF EXISTS trace_state, DROP COLUMN IF EXISTS trace_flags, DROP COLUMN IF EXISTS trace_span_id, DROP COLUMN IF EXISTS trace_id, DROP COLUMN IF EXISTS attempt_id, DROP COLUMN IF EXISTS logical_operation_id, DROP COLUMN IF EXISTS causation_id, DROP COLUMN IF EXISTS correlation_id', table_name);
    END LOOP;
END $$;
-- +goose StatementEnd
