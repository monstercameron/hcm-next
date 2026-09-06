-- Owner: connectivity lane. Phase: P1B.
-- storage-disposition: connector_operation_observation_requirement | authoritative append-only typed observation obligation | local PostgreSQL | operation state transition and reconciliation | tenant-scoped.

-- +goose Up

CREATE TABLE connector_operation_observation_requirement (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    requirement_id     uuid         NOT NULL,
    operation_id       uuid         NOT NULL,
    requirement_kind   semantic_key NOT NULL,
    semantic_key       semantic_key NOT NULL,
    created_at         timestamptz  NOT NULL DEFAULT now(),
    satisfied_by       uuid,
    satisfied_at       timestamptz,
    PRIMARY KEY (tenant_id, requirement_id),
    FOREIGN KEY (tenant_id, operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    FOREIGN KEY (tenant_id, satisfied_by) REFERENCES external_operation_observation (tenant_id, observation_id),
    CONSTRAINT connector_operation_observation_kind_allowed CHECK (requirement_kind IN ('SEMANTIC_IDENTITY')),
    CONSTRAINT connector_operation_observation_satisfaction_pair CHECK ((satisfied_by IS NULL) = (satisfied_at IS NULL))
);

CREATE UNIQUE INDEX connector_operation_observation_one_open
    ON connector_operation_observation_requirement (tenant_id, operation_id)
    WHERE satisfied_by IS NULL;

CREATE TRIGGER connector_operation_observation_append_only
    BEFORE UPDATE OR DELETE ON connector_operation_observation_requirement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE connector_operation_observation_requirement ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_observation_requirement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation_observation_requirement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON connector_operation_observation_requirement FROM PUBLIC;
GRANT SELECT, INSERT ON connector_operation_observation_requirement TO hcmnext_app;

-- +goose Down

REVOKE ALL ON connector_operation_observation_requirement FROM hcmnext_app;
DROP POLICY tenant_isolation ON connector_operation_observation_requirement;
ALTER TABLE connector_operation_observation_requirement NO FORCE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_observation_requirement DISABLE ROW LEVEL SECURITY;
DROP TABLE connector_operation_observation_requirement;
