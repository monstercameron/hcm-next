-- Owner: connectivity lane. Phase: Gate B.
-- storage-disposition: connector_operation_queue | authoritative mutable queue membership and fenced claim state | local PostgreSQL | queue eligibility and lease reclamation | tenant-scoped.

-- +goose Up

ALTER TABLE connector_operation
    DROP CONSTRAINT connector_operation_state_allowed;
ALTER TABLE connector_operation
    ADD CONSTRAINT connector_operation_state_allowed CHECK (
        state IN ('PLANNED', 'QUEUED', 'LEASED', 'SENDING', 'SUBMITTED', 'SENT',
                  'PROVIDER_ACCEPTED', 'ACKNOWLEDGED', 'OBSERVING', 'OBSERVED',
                  'RECONCILED', 'FAILED', 'RETRYABLE', 'DEAD_LETTER',
                  'AMBIGUOUS', 'REPAIR_REQUIRED', 'REJECTED')
    );

CREATE TABLE connector_operation_queue (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    queue_id        uuid         NOT NULL,
    operation_id    uuid         NOT NULL,
    available_at    timestamptz  NOT NULL,
    queue_state     text         NOT NULL DEFAULT 'QUEUED',
    worker_ref      text         NOT NULL DEFAULT '',
    lease_token     uuid,
    fence_token     bigint       NOT NULL DEFAULT 0,
    leased_at       timestamptz,
    expires_at      timestamptz,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, queue_id),
    UNIQUE (tenant_id, operation_id),
    FOREIGN KEY (tenant_id, operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    CONSTRAINT connector_operation_queue_state_allowed CHECK (queue_state IN ('QUEUED', 'LEASED', 'REQUEUE', 'REMOVED')),
    CONSTRAINT connector_operation_queue_fence_nonnegative CHECK (fence_token >= 0),
    CONSTRAINT connector_operation_queue_lease_pair CHECK ((lease_token IS NULL) = (expires_at IS NULL)),
    CONSTRAINT connector_operation_queue_lease_times CHECK (expires_at IS NULL OR leased_at IS NOT NULL AND expires_at > leased_at),
    CONSTRAINT connector_operation_queue_claim_consistent CHECK (
        queue_state <> 'LEASED' OR (worker_ref <> '' AND lease_token IS NOT NULL AND leased_at IS NOT NULL AND expires_at IS NOT NULL AND fence_token >= 1)
    )
);

CREATE INDEX connector_operation_queue_ready
    ON connector_operation_queue (tenant_id, queue_state, available_at, updated_at);

ALTER TABLE connector_operation_queue ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_queue FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation_queue
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE DELETE ON connector_operation_queue FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON connector_operation_queue TO hcmnext_app;

-- +goose Down

REVOKE ALL ON connector_operation_queue FROM hcmnext_app;
DROP POLICY tenant_isolation ON connector_operation_queue;
ALTER TABLE connector_operation_queue NO FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_queue DISABLE ROW LEVEL SECURITY;
DROP TABLE connector_operation_queue;
ALTER TABLE connector_operation
    DROP CONSTRAINT connector_operation_state_allowed;
ALTER TABLE connector_operation
    ADD CONSTRAINT connector_operation_state_allowed CHECK (
        state IN ('PLANNED', 'SUBMITTED', 'PROVIDER_ACCEPTED', 'OBSERVED', 'RECONCILED', 'FAILED', 'AMBIGUOUS')
    );
