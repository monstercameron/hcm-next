-- Owner: connectivity lane. Phase: P1B.
-- storage-disposition: connector_operation_redrive | authoritative append-only failed-operation lineage and approval evidence | local PostgreSQL | targeted redrive audit | tenant-scoped.

-- +goose Up

CREATE TABLE connector_operation_redrive (
    tenant_id        tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    redrive_id       uuid         NOT NULL,
    operation_id     uuid         NOT NULL,
    actor_ref        semantic_key NOT NULL,
    repair_plan_id   semantic_key NOT NULL DEFAULT '',
    material_changes jsonb        NOT NULL DEFAULT '[]'::jsonb,
    approved         boolean      NOT NULL,
    redriven_at      timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, redrive_id),
    FOREIGN KEY (tenant_id, operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    CONSTRAINT connector_operation_redrive_changes_array CHECK (jsonb_typeof(material_changes) = 'array')
);

CREATE INDEX connector_operation_redrive_operation
    ON connector_operation_redrive (tenant_id, operation_id, redriven_at);

CREATE TRIGGER connector_operation_redrive_append_only
    BEFORE UPDATE OR DELETE ON connector_operation_redrive
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE connector_operation_redrive ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_redrive FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation_redrive
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON connector_operation_redrive FROM PUBLIC;
GRANT SELECT, INSERT ON connector_operation_redrive TO hcmnext_app;

-- +goose Down

REVOKE ALL ON connector_operation_redrive FROM hcmnext_app;
DROP POLICY tenant_isolation ON connector_operation_redrive;
ALTER TABLE connector_operation_redrive NO FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_redrive DISABLE ROW LEVEL SECURITY;
DROP TABLE connector_operation_redrive;
