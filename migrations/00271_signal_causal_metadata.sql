-- Owner: signal data adapter. OBS-013 durable signal continuation metadata.
-- Causal identifiers are bounded business correlation; trace columns are
-- optional diagnostic context and never affect signal identity or dedupe.

-- +goose Up

ALTER TABLE workflow_signal_subscription
    ADD COLUMN IF NOT EXISTS correlation_id text, ADD COLUMN IF NOT EXISTS causation_id text,
    ADD COLUMN IF NOT EXISTS logical_operation_id text, ADD COLUMN IF NOT EXISTS attempt_id text,
    ADD COLUMN IF NOT EXISTS trace_id text, ADD COLUMN IF NOT EXISTS trace_span_id text,
    ADD COLUMN IF NOT EXISTS trace_flags smallint, ADD COLUMN IF NOT EXISTS trace_state text,
    ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;
ALTER TABLE workflow_signal
    ADD COLUMN IF NOT EXISTS correlation_id text, ADD COLUMN IF NOT EXISTS causation_id text,
    ADD COLUMN IF NOT EXISTS logical_operation_id text, ADD COLUMN IF NOT EXISTS attempt_id text,
    ADD COLUMN IF NOT EXISTS trace_id text, ADD COLUMN IF NOT EXISTS trace_span_id text,
    ADD COLUMN IF NOT EXISTS trace_flags smallint, ADD COLUMN IF NOT EXISTS trace_state text,
    ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;
ALTER TABLE workflow_signal_disposition
    ADD COLUMN IF NOT EXISTS correlation_id text, ADD COLUMN IF NOT EXISTS causation_id text,
    ADD COLUMN IF NOT EXISTS logical_operation_id text, ADD COLUMN IF NOT EXISTS causal_attempt_id text,
    ADD COLUMN IF NOT EXISTS trace_id text,
    ADD COLUMN IF NOT EXISTS trace_span_id text, ADD COLUMN IF NOT EXISTS trace_flags smallint,
    ADD COLUMN IF NOT EXISTS trace_state text, ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;

-- +goose StatementBegin
DO $$
DECLARE
  table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY['workflow_signal_subscription', 'workflow_signal'] LOOP
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((correlation_id IS NULL OR length(btrim(correlation_id)) BETWEEN 1 AND 128) AND (causation_id IS NULL OR length(btrim(causation_id)) BETWEEN 1 AND 128) AND (logical_operation_id IS NULL OR length(btrim(logical_operation_id)) BETWEEN 1 AND 128) AND (attempt_id IS NULL OR length(btrim(attempt_id)) BETWEEN 1 AND 128))', table_name, table_name || '_causal_metadata_bounded');
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((correlation_id IS NULL AND causation_id IS NULL AND logical_operation_id IS NULL AND attempt_id IS NULL) OR (correlation_id IS NOT NULL AND causation_id IS NOT NULL AND logical_operation_id IS NOT NULL AND attempt_id IS NOT NULL))', table_name, table_name || '_causal_metadata_complete');
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((trace_id IS NULL AND trace_span_id IS NULL AND trace_flags IS NULL AND trace_state IS NULL AND trace_link_expires_at IS NULL) OR (trace_id IS NOT NULL AND trace_id ~ ''^[0-9a-f]{32}$'' AND trace_id <> repeat(''0'', 32) AND trace_span_id IS NOT NULL AND trace_span_id ~ ''^[0-9a-f]{16}$'' AND trace_span_id <> repeat(''0'', 16) AND trace_flags IS NOT NULL AND trace_flags BETWEEN 0 AND 255 AND (trace_state IS NULL OR length(trace_state) <= 256)))', table_name, table_name || '_trace_metadata_bounded');
  END LOOP;
  ALTER TABLE workflow_signal_disposition ADD CONSTRAINT workflow_signal_disposition_causal_metadata_bounded CHECK (
    (correlation_id IS NULL OR length(btrim(correlation_id)) BETWEEN 1 AND 128) AND (causation_id IS NULL OR length(btrim(causation_id)) BETWEEN 1 AND 128) AND (logical_operation_id IS NULL OR length(btrim(logical_operation_id)) BETWEEN 1 AND 128) AND (causal_attempt_id IS NULL OR length(btrim(causal_attempt_id)) BETWEEN 1 AND 128));
  ALTER TABLE workflow_signal_disposition ADD CONSTRAINT workflow_signal_disposition_causal_metadata_complete CHECK (
    (correlation_id IS NULL AND causation_id IS NULL AND logical_operation_id IS NULL AND causal_attempt_id IS NULL) OR (correlation_id IS NOT NULL AND causation_id IS NOT NULL AND logical_operation_id IS NOT NULL AND causal_attempt_id IS NOT NULL));
  ALTER TABLE workflow_signal_disposition ADD CONSTRAINT workflow_signal_disposition_trace_metadata_bounded CHECK (
    (trace_id IS NULL AND trace_span_id IS NULL AND trace_flags IS NULL AND trace_state IS NULL AND trace_link_expires_at IS NULL) OR (trace_id IS NOT NULL AND trace_id ~ '^[0-9a-f]{32}$' AND trace_id <> repeat('0', 32) AND trace_span_id IS NOT NULL AND trace_span_id ~ '^[0-9a-f]{16}$' AND trace_span_id <> repeat('0', 16) AND trace_flags IS NOT NULL AND trace_flags BETWEEN 0 AND 255 AND (trace_state IS NULL OR length(trace_state) <= 256)));
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE workflow_signal_disposition DROP CONSTRAINT IF EXISTS workflow_signal_disposition_trace_metadata_bounded, DROP CONSTRAINT IF EXISTS workflow_signal_disposition_causal_metadata_complete, DROP CONSTRAINT IF EXISTS workflow_signal_disposition_causal_metadata_bounded;
ALTER TABLE workflow_signal DROP CONSTRAINT IF EXISTS workflow_signal_trace_metadata_bounded, DROP CONSTRAINT IF EXISTS workflow_signal_causal_metadata_complete, DROP CONSTRAINT IF EXISTS workflow_signal_causal_metadata_bounded;
ALTER TABLE workflow_signal_subscription DROP CONSTRAINT IF EXISTS workflow_signal_subscription_trace_metadata_bounded, DROP CONSTRAINT IF EXISTS workflow_signal_subscription_causal_metadata_complete, DROP CONSTRAINT IF EXISTS workflow_signal_subscription_causal_metadata_bounded;
ALTER TABLE workflow_signal_disposition DROP COLUMN IF EXISTS trace_link_expires_at, DROP COLUMN IF EXISTS trace_state, DROP COLUMN IF EXISTS trace_flags, DROP COLUMN IF EXISTS trace_span_id, DROP COLUMN IF EXISTS trace_id, DROP COLUMN IF EXISTS causal_attempt_id, DROP COLUMN IF EXISTS logical_operation_id, DROP COLUMN IF EXISTS causation_id, DROP COLUMN IF EXISTS correlation_id;
ALTER TABLE workflow_signal DROP COLUMN IF EXISTS trace_link_expires_at, DROP COLUMN IF EXISTS trace_state, DROP COLUMN IF EXISTS trace_flags, DROP COLUMN IF EXISTS trace_span_id, DROP COLUMN IF EXISTS trace_id, DROP COLUMN IF EXISTS attempt_id, DROP COLUMN IF EXISTS logical_operation_id, DROP COLUMN IF EXISTS causation_id, DROP COLUMN IF EXISTS correlation_id;
ALTER TABLE workflow_signal_subscription DROP COLUMN IF EXISTS trace_link_expires_at, DROP COLUMN IF EXISTS trace_state, DROP COLUMN IF EXISTS trace_flags, DROP COLUMN IF EXISTS trace_span_id, DROP COLUMN IF EXISTS trace_id, DROP COLUMN IF EXISTS attempt_id, DROP COLUMN IF EXISTS logical_operation_id, DROP COLUMN IF EXISTS causation_id, DROP COLUMN IF EXISTS correlation_id;
