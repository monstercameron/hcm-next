-- Owner: workflow-runtime lane. Phase: P1B.
-- WF-RUN-005: durable signal matching, dispositions and reference-only wakeups.
--
-- 00026 established the durable signal tables, but its first shape only carried
-- a signal name and correlation key.  These additive columns preserve that
-- contract for existing callers while pinning the event/source/schema and
-- ordering facts needed by the typed SIGNAL step.  The disposition table is
-- append-only evidence for every evaluation, including refusals and unmatched
-- arrivals; the accepted continuation carries only a signal reference.

-- +goose Up

ALTER TABLE workflow_signal_subscription
    ADD COLUMN IF NOT EXISTS event_type semantic_key,
    ADD COLUMN IF NOT EXISTS correlation_value semantic_key NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS expected_schema_ref semantic_key NOT NULL DEFAULT 'legacy/unknown',
    ADD COLUMN IF NOT EXISTS accepted_sources jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS ordering_expectation text NOT NULL DEFAULT 'NONE',
    ADD COLUMN IF NOT EXISTS node_attempt integer NOT NULL DEFAULT 1;

UPDATE workflow_signal_subscription
SET event_type = signal_name,
    correlation_value = CASE WHEN correlation_value = '' THEN correlation_key ELSE correlation_value END
WHERE event_type IS NULL;

ALTER TABLE workflow_signal_subscription
    ALTER COLUMN event_type SET NOT NULL;

ALTER TABLE workflow_signal_subscription
    ADD CONSTRAINT workflow_signal_subscription_sources_array
        CHECK (jsonb_typeof(accepted_sources) = 'array'),
    ADD CONSTRAINT workflow_signal_subscription_ordering_allowed
        CHECK (ordering_expectation IN ('NONE', 'MONOTONIC_SEQUENCE')),
    ADD CONSTRAINT workflow_signal_subscription_node_attempt_positive
        CHECK (node_attempt >= 1);

CREATE INDEX IF NOT EXISTS workflow_signal_subscription_match
    ON workflow_signal_subscription
        (tenant_id, event_type, correlation_key, correlation_value, subscription_state);

ALTER TABLE workflow_signal
    ADD COLUMN IF NOT EXISTS event_type semantic_key,
    ADD COLUMN IF NOT EXISTS source semantic_key NOT NULL DEFAULT 'legacy/unknown',
    ADD COLUMN IF NOT EXISTS correlation_value semantic_key NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sequence_number bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS received_at timestamptz;

UPDATE workflow_signal
SET event_type = signal_name,
    correlation_value = CASE WHEN correlation_value = '' THEN correlation_key ELSE correlation_value END,
    received_at = delivered_at
WHERE event_type IS NULL OR received_at IS NULL;

ALTER TABLE workflow_signal
    ALTER COLUMN event_type SET NOT NULL,
    ALTER COLUMN received_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS workflow_signal_delivery_lookup
    ON workflow_signal (tenant_id, event_type, correlation_key, correlation_value, delivered_at);

CREATE TABLE IF NOT EXISTS workflow_signal_disposition (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    disposition_id     uuid NOT NULL,
    attempt_id         uuid NOT NULL,
    signal_id          uuid NOT NULL,
    subscription_id    uuid,
    status             text NOT NULL,
    reason             text NOT NULL DEFAULT '',
    continuation_ref   text,
    recorded_at        timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, disposition_id),
    FOREIGN KEY (tenant_id, signal_id)
        REFERENCES workflow_signal (tenant_id, signal_id),
    FOREIGN KEY (tenant_id, subscription_id)
        REFERENCES workflow_signal_subscription (tenant_id, subscription_id),
    CONSTRAINT workflow_signal_disposition_status_allowed CHECK (
        status IN (
            'ACCEPTED', 'DUPLICATE_SAME_BYTES', 'REFUSED_WRONG_TENANT',
            'REFUSED_UNMATCHED', 'REFUSED_WRONG_CORRELATION',
            'REFUSED_WRONG_SCHEMA', 'REFUSED_INVALID_SIGNATURE',
            'REFUSED_WRONG_SOURCE', 'REFUSED_WRONG_ORDER',
            'REFUSED_DUPLICATE_DIFFERENT_BYTES', 'REFUSED_LATE'
        )
    ),
    CONSTRAINT workflow_signal_disposition_reference_only CHECK (
        continuation_ref IS NULL OR continuation_ref LIKE 'signal:%'
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS workflow_signal_disposition_attempt
    ON workflow_signal_disposition (tenant_id, attempt_id, COALESCE(subscription_id, '00000000-0000-0000-0000-000000000000'::uuid), status);

CREATE INDEX IF NOT EXISTS workflow_signal_disposition_signal
    ON workflow_signal_disposition (tenant_id, signal_id, recorded_at);

ALTER TABLE workflow_signal_disposition ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_signal_disposition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_signal_disposition
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER workflow_signal_disposition_append_only
    BEFORE UPDATE OR DELETE ON workflow_signal_disposition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON workflow_signal_disposition FROM PUBLIC;
GRANT SELECT, INSERT ON workflow_signal_disposition TO hcmnext_app;

-- +goose Down

REVOKE ALL ON workflow_signal_disposition FROM hcmnext_app;
DROP TRIGGER workflow_signal_disposition_append_only ON workflow_signal_disposition;
DROP POLICY tenant_isolation ON workflow_signal_disposition;
ALTER TABLE workflow_signal_disposition NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_signal_disposition DISABLE ROW LEVEL SECURITY;
DROP INDEX workflow_signal_disposition_signal;
DROP INDEX workflow_signal_disposition_attempt;
DROP TABLE workflow_signal_disposition;

DROP INDEX workflow_signal_delivery_lookup;
ALTER TABLE workflow_signal
    DROP COLUMN received_at,
    DROP COLUMN sequence_number,
    DROP COLUMN correlation_value,
    DROP COLUMN source,
    DROP COLUMN event_type;

DROP INDEX workflow_signal_subscription_match;
ALTER TABLE workflow_signal_subscription
    DROP CONSTRAINT workflow_signal_subscription_node_attempt_positive,
    DROP CONSTRAINT workflow_signal_subscription_ordering_allowed,
    DROP CONSTRAINT workflow_signal_subscription_sources_array,
    DROP COLUMN node_attempt,
    DROP COLUMN ordering_expectation,
    DROP COLUMN accepted_sources,
    DROP COLUMN expected_schema_ref,
    DROP COLUMN correlation_value,
    DROP COLUMN event_type;
