-- Owner: operations platform. Phase: Gate A.
-- OBS-003/006/019: cell-scoped backend capability and independent pipeline
-- health evidence. Signal payloads and metric series are not stored here.

-- +goose Up

-- storage-disposition: telemetry_backend_registry | authoritative portable backend capability and deployment metadata | local PostgreSQL | operations control plane | cell-scoped, non-tenant payload.
CREATE TABLE IF NOT EXISTS telemetry_backend_registry (
    backend_id          uuid        PRIMARY KEY,
    backend_kind        text        NOT NULL,
    protocol            text        NOT NULL,
    license             text        NOT NULL,
    cell_id             text        NOT NULL,
    region              text        NOT NULL,
    private_endpoint    boolean     NOT NULL,
    tenant_isolated     boolean     NOT NULL,
    restore_supported   boolean     NOT NULL,
    retention_seconds   bigint      NOT NULL,
    cost_budget_bytes   bigint      NOT NULL,
    status              text        NOT NULL DEFAULT 'ACTIVE',
    registered_at       timestamptz NOT NULL,
    CONSTRAINT telemetry_backend_kind_allowed CHECK (backend_kind IN ('PROMETHEUS', 'LOKI', 'TEMPO', 'GRAFANA')),
    CONSTRAINT telemetry_backend_protocol_present CHECK (length(protocol) > 0),
    CONSTRAINT telemetry_backend_license_present CHECK (length(license) > 0),
    CONSTRAINT telemetry_backend_positive_limits CHECK (retention_seconds > 0 AND cost_budget_bytes > 0),
    CONSTRAINT telemetry_backend_status_allowed CHECK (status IN ('ACTIVE', 'DEGRADED', 'DISABLED')),
    CONSTRAINT telemetry_backend_kind_cell_unique UNIQUE (backend_kind, cell_id)
);

-- storage-disposition: telemetry_pipeline_health | authoritative append-only telemetry pipeline health and drop evidence | local PostgreSQL | independent operations health path | cell-scoped, non-tenant payload.
CREATE TABLE IF NOT EXISTS telemetry_pipeline_health (
    observation_id    uuid        PRIMARY KEY,
    backend_id        uuid        NOT NULL REFERENCES telemetry_backend_registry (backend_id),
    queue_depth       bigint      NOT NULL,
    accepted_count    bigint      NOT NULL,
    exported_count    bigint      NOT NULL,
    dropped_count     bigint      NOT NULL,
    retry_count       bigint      NOT NULL,
    state             text        NOT NULL,
    observed_at       timestamptz NOT NULL,
    CONSTRAINT telemetry_health_counts_nonnegative CHECK (queue_depth >= 0 AND accepted_count >= 0 AND exported_count >= 0 AND dropped_count >= 0 AND retry_count >= 0),
    CONSTRAINT telemetry_health_state_allowed CHECK (state IN ('UNKNOWN', 'DEGRADED', 'HEALTHY'))
);

CREATE INDEX IF NOT EXISTS telemetry_pipeline_health_backend_time
    ON telemetry_pipeline_health (backend_id, observed_at DESC);

CREATE TRIGGER telemetry_pipeline_health_append_only
    BEFORE UPDATE OR DELETE ON telemetry_pipeline_health
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT, UPDATE ON telemetry_backend_registry TO hcmnext_app;
GRANT SELECT, INSERT ON telemetry_pipeline_health TO hcmnext_app;
REVOKE UPDATE, DELETE ON telemetry_pipeline_health FROM PUBLIC;

-- +goose Down

REVOKE ALL ON telemetry_pipeline_health FROM hcmnext_app;
REVOKE ALL ON telemetry_backend_registry FROM hcmnext_app;
DROP TABLE telemetry_pipeline_health;
DROP TABLE telemetry_backend_registry;
