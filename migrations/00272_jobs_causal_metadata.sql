-- OBS-013 / JOB-001: optional causal metadata for durable job envelopes.
-- +goose Up
ALTER TABLE job_run ADD COLUMN IF NOT EXISTS correlation_id text, ADD COLUMN IF NOT EXISTS causation_id text, ADD COLUMN IF NOT EXISTS logical_operation_id text, ADD COLUMN IF NOT EXISTS attempt_id text, ADD COLUMN IF NOT EXISTS trace_id text, ADD COLUMN IF NOT EXISTS trace_span_id text, ADD COLUMN IF NOT EXISTS trace_flags smallint, ADD COLUMN IF NOT EXISTS trace_state text, ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;
ALTER TABLE job_partition ADD COLUMN IF NOT EXISTS attempt integer NOT NULL DEFAULT 1, ADD COLUMN IF NOT EXISTS correlation_id text, ADD COLUMN IF NOT EXISTS causation_id text, ADD COLUMN IF NOT EXISTS logical_operation_id text, ADD COLUMN IF NOT EXISTS attempt_id text, ADD COLUMN IF NOT EXISTS trace_id text, ADD COLUMN IF NOT EXISTS trace_span_id text, ADD COLUMN IF NOT EXISTS trace_flags smallint, ADD COLUMN IF NOT EXISTS trace_state text, ADD COLUMN IF NOT EXISTS trace_link_expires_at timestamptz;
ALTER TABLE job_checkpoint ADD COLUMN IF NOT EXISTS correlation_id text, ADD COLUMN IF NOT EXISTS causation_id text, ADD COLUMN IF NOT EXISTS logical_operation_id text, ADD COLUMN IF NOT EXISTS attempt_id text;
CREATE TABLE job_checkpoint_trace_link (
    tenant_id uuid NOT NULL,
    partition_id uuid NOT NULL,
    checkpoint_sequence bigint NOT NULL,
    trace_id text NOT NULL,
    trace_span_id text NOT NULL,
    trace_flags smallint NOT NULL,
    trace_state text,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, partition_id, checkpoint_sequence),
    FOREIGN KEY (tenant_id, partition_id, checkpoint_sequence) REFERENCES job_checkpoint (tenant_id, partition_id, checkpoint_sequence) ON DELETE CASCADE,
    CHECK (trace_id ~ '^[0-9a-f]{32}$' AND trace_id <> '00000000000000000000000000000000'),
    CHECK (trace_span_id ~ '^[0-9a-f]{16}$' AND trace_span_id <> '0000000000000000'),
    CHECK (trace_flags BETWEEN 0 AND 255),
    CHECK (length(trace_state) <= 256)
);
ALTER TABLE job_checkpoint_trace_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_checkpoint_trace_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_checkpoint_trace_link USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, DELETE ON job_checkpoint_trace_link TO hcmnext_app;
ALTER TABLE job_run ADD CONSTRAINT job_run_causal_metadata_bounded CHECK (length(correlation_id) <= 128 AND length(causation_id) <= 128 AND length(logical_operation_id) <= 128 AND length(attempt_id) <= 128 AND length(trace_state) <= 256 AND trace_flags BETWEEN 0 AND 255), ADD CONSTRAINT job_run_trace_link_complete CHECK ((trace_id IS NULL AND trace_span_id IS NULL AND trace_flags IS NULL AND trace_state IS NULL AND trace_link_expires_at IS NULL) OR (trace_id IS NOT NULL AND trace_span_id IS NOT NULL AND trace_flags IS NOT NULL AND trace_link_expires_at IS NOT NULL AND trace_id ~ '^[0-9a-f]{32}$' AND trace_id <> '00000000000000000000000000000000' AND trace_span_id ~ '^[0-9a-f]{16}$' AND trace_span_id <> '0000000000000000'));
ALTER TABLE job_partition ADD CONSTRAINT job_partition_causal_metadata_bounded CHECK (length(correlation_id) <= 128 AND length(causation_id) <= 128 AND length(logical_operation_id) <= 128 AND length(attempt_id) <= 128 AND length(trace_state) <= 256 AND trace_flags BETWEEN 0 AND 255), ADD CONSTRAINT job_partition_trace_link_complete CHECK ((trace_id IS NULL AND trace_span_id IS NULL AND trace_flags IS NULL AND trace_state IS NULL AND trace_link_expires_at IS NULL) OR (trace_id IS NOT NULL AND trace_span_id IS NOT NULL AND trace_flags IS NOT NULL AND trace_link_expires_at IS NOT NULL AND trace_id ~ '^[0-9a-f]{32}$' AND trace_id <> '00000000000000000000000000000000' AND trace_span_id ~ '^[0-9a-f]{16}$' AND trace_span_id <> '0000000000000000'));
ALTER TABLE job_checkpoint ADD CONSTRAINT job_checkpoint_causal_metadata_bounded CHECK (length(correlation_id) <= 128 AND length(causation_id) <= 128 AND length(logical_operation_id) <= 128 AND length(attempt_id) <= 128);
-- +goose Down
DROP TABLE IF EXISTS job_checkpoint_trace_link;
ALTER TABLE job_checkpoint DROP COLUMN IF EXISTS trace_link_expires_at, DROP COLUMN IF EXISTS trace_state, DROP COLUMN IF EXISTS trace_flags, DROP COLUMN IF EXISTS trace_span_id, DROP COLUMN IF EXISTS trace_id, DROP COLUMN IF EXISTS attempt_id, DROP COLUMN IF EXISTS logical_operation_id, DROP COLUMN IF EXISTS causation_id, DROP COLUMN IF EXISTS correlation_id;
ALTER TABLE job_partition DROP COLUMN IF EXISTS trace_link_expires_at, DROP COLUMN IF EXISTS trace_state, DROP COLUMN IF EXISTS trace_flags, DROP COLUMN IF EXISTS trace_span_id, DROP COLUMN IF EXISTS trace_id, DROP COLUMN IF EXISTS attempt_id, DROP COLUMN IF EXISTS logical_operation_id, DROP COLUMN IF EXISTS causation_id, DROP COLUMN IF EXISTS correlation_id, DROP COLUMN IF EXISTS attempt;
ALTER TABLE job_run DROP COLUMN IF EXISTS trace_link_expires_at, DROP COLUMN IF EXISTS trace_state, DROP COLUMN IF EXISTS trace_flags, DROP COLUMN IF EXISTS trace_span_id, DROP COLUMN IF EXISTS trace_id, DROP COLUMN IF EXISTS attempt_id, DROP COLUMN IF EXISTS logical_operation_id, DROP COLUMN IF EXISTS causation_id, DROP COLUMN IF EXISTS correlation_id;
