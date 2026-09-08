-- +goose Up

CREATE TABLE merit_compensation_intent_emission (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    intent_id          text NOT NULL,
    cycle_id           semantic_key NOT NULL,
    cycle_revision     cas_version NOT NULL,
    participant_id     semantic_key NOT NULL,
    payload            jsonb NOT NULL,
    canonical_digest   content_digest NOT NULL,
    source_digest      content_digest NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT clock_timestamp(),

    PRIMARY KEY (tenant_id, intent_id),
    CONSTRAINT merit_compensation_emission_cycle_fk
        FOREIGN KEY (tenant_id, cycle_id, cycle_revision)
        REFERENCES merit_cycle_revision (tenant_id, cycle_id, revision)
);

CREATE OR REPLACE TRIGGER merit_compensation_intent_emission_append_only
    BEFORE UPDATE OR DELETE ON merit_compensation_intent_emission
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE merit_compensation_intent_emission ENABLE ROW LEVEL SECURITY;
ALTER TABLE merit_compensation_intent_emission FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON merit_compensation_intent_emission
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON merit_compensation_intent_emission FROM PUBLIC;
GRANT SELECT, INSERT ON merit_compensation_intent_emission TO hcmnext_app;

-- +goose Down

REVOKE ALL ON merit_compensation_intent_emission FROM hcmnext_app;
DROP POLICY tenant_isolation ON merit_compensation_intent_emission;
ALTER TABLE merit_compensation_intent_emission NO FORCE ROW LEVEL SECURITY;
ALTER TABLE merit_compensation_intent_emission DISABLE ROW LEVEL SECURITY;
DROP TRIGGER merit_compensation_intent_emission_append_only ON merit_compensation_intent_emission;
DROP TABLE merit_compensation_intent_emission;
