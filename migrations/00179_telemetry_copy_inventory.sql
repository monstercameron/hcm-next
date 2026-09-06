-- Owner: operations/privacy platform. Phase: Gate A.
-- OBS-021: tenant-scoped primary/index/archive/backup copy disposition for
-- telemetry. This table is metadata only; payloads remain in backend stores.

-- +goose Up

-- storage-disposition: telemetry_copy_inventory | authoritative tenant-scoped telemetry copy, retention, hold and deletion metadata | local PostgreSQL | records and privacy lifecycle | tenant-scoped; signal payloads remain in backend stores.
CREATE TABLE IF NOT EXISTS telemetry_copy_inventory (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    copy_id            uuid           NOT NULL,
    signal_kind        text           NOT NULL,
    copy_role          text           NOT NULL,
    backend_ref        semantic_key   NOT NULL,
    region             text           NOT NULL,
    content_digest     content_digest NOT NULL,
    retention_until    timestamptz    NOT NULL,
    hold_state         text           NOT NULL DEFAULT 'NONE',
    disposition_state  text           NOT NULL DEFAULT 'ACTIVE',
    deletion_capability text          NOT NULL,
    last_verified_at   timestamptz,
    created_at         timestamptz    NOT NULL,
    updated_at         timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, copy_id),
    CONSTRAINT telemetry_copy_signal_kind_allowed CHECK (signal_kind IN ('METRICS', 'LOGS', 'TRACES', 'EXEMPLARS')),
    CONSTRAINT telemetry_copy_role_allowed CHECK (copy_role IN ('PRIMARY', 'INDEX', 'ARCHIVE', 'BACKUP')),
    CONSTRAINT telemetry_copy_hold_allowed CHECK (hold_state IN ('NONE', 'HELD', 'RELEASED')),
    CONSTRAINT telemetry_copy_disposition_allowed CHECK (disposition_state IN ('ACTIVE', 'DELETED', 'EXCEPTION')),
    CONSTRAINT telemetry_copy_deletion_capability_present CHECK (length(deletion_capability) > 0),
    CONSTRAINT telemetry_copy_window_valid CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS telemetry_copy_inventory_backend_identity
    ON telemetry_copy_inventory (tenant_id, copy_id, backend_ref, copy_role);

ALTER TABLE telemetry_copy_inventory ENABLE ROW LEVEL SECURITY;
ALTER TABLE telemetry_copy_inventory FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON telemetry_copy_inventory
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON telemetry_copy_inventory TO hcmnext_app;
REVOKE DELETE ON telemetry_copy_inventory FROM PUBLIC;

-- +goose Down

REVOKE ALL ON telemetry_copy_inventory FROM hcmnext_app;
DROP POLICY tenant_isolation ON telemetry_copy_inventory;
ALTER TABLE telemetry_copy_inventory NO FORCE ROW LEVEL SECURITY;
ALTER TABLE telemetry_copy_inventory DISABLE ROW LEVEL SECURITY;
DROP TABLE telemetry_copy_inventory;
