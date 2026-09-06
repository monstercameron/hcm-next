-- Owner: incentive persistence lane. Phase: Gate A.
-- PERSIST-INCENTIVE-001: durable incentive-plan, attainment and award metadata.
--
-- These relations retain governed lineage and evidence only. They do not
-- approve, calculate payment, or issue a payroll instruction.
--
-- Storage disposition (STORE-001): all three tables are permanent and tenant
-- scoped by tenant_id with no rebuild source. incentive_plan_revision and
-- award_calculation are AGGREGATE revisions; attainment_observation is a
-- LEDGER fact stream. Every relation is immutable and append-only.

-- +goose Up

CREATE TABLE IF NOT EXISTS incentive_plan_revision (
    tenant_id           tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid         NOT NULL,
    plan_id             text         NOT NULL,
    revision            cas_version  NOT NULL,
    supersedes_revision cas_version,
    parent_digest       content_digest,
    canonical_digest    content_digest NOT NULL,
    digest              content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT incentive_plan_revision_unique
        UNIQUE (tenant_id, plan_id, revision)
);

CREATE OR REPLACE TRIGGER incentive_plan_revision_append_only
    BEFORE UPDATE OR DELETE ON incentive_plan_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON incentive_plan_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS attainment_observation (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid          NOT NULL,
    observation_id  text          NOT NULL,
    plan_id        text          NOT NULL,
    plan_revision  cas_version   NOT NULL,
    worker_ref     uuid          NOT NULL,
    value          numeric(19,4) NOT NULL,
    as_of          date          NOT NULL,
    known_at       timestamptz   NOT NULL,
    source_ref     text,
    digest         content_digest NOT NULL,
    event_sequence bigint        NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT attainment_observation_sequence_unique
        UNIQUE (tenant_id, worker_ref, plan_id, event_sequence),
    CONSTRAINT attainment_observation_sequence_positive
        CHECK (event_sequence >= 1)
);

CREATE OR REPLACE TRIGGER attainment_observation_append_only
    BEFORE UPDATE OR DELETE ON attainment_observation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON attainment_observation FROM PUBLIC;

CREATE TABLE IF NOT EXISTS award_calculation (
    tenant_id           tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid         NOT NULL,
    calculation_id      text         NOT NULL,
    worker_ref          uuid         NOT NULL,
    plan_digest         content_digest NOT NULL,
    plan_revision       cas_version  NOT NULL,
    state               text         NOT NULL,
    revision            cas_version  NOT NULL,
    supersedes_revision cas_version,
    canonical_digest    content_digest NOT NULL,
    digest              content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT award_calculation_revision_unique
        UNIQUE (tenant_id, calculation_id, revision),
    CONSTRAINT award_calculation_state_allowed
        CHECK (state IN ('CALCULATED', 'APPROVED', 'FINALIZED'))
);

CREATE OR REPLACE TRIGGER award_calculation_append_only
    BEFORE UPDATE OR DELETE ON award_calculation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON award_calculation FROM PUBLIC;

-- Tenant isolation (DB-017): fail closed when app.tenant_id is absent or blank.
ALTER TABLE incentive_plan_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE incentive_plan_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON incentive_plan_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE attainment_observation ENABLE ROW LEVEL SECURITY;
ALTER TABLE attainment_observation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON attainment_observation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE award_calculation ENABLE ROW LEVEL SECURITY;
ALTER TABLE award_calculation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON award_calculation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON
    incentive_plan_revision,
    attainment_observation,
    award_calculation
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON award_calculation FROM hcmnext_app;
REVOKE ALL ON attainment_observation FROM hcmnext_app;
REVOKE ALL ON incentive_plan_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON award_calculation;
ALTER TABLE award_calculation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE award_calculation DISABLE ROW LEVEL SECURITY;
DROP TRIGGER award_calculation_append_only ON award_calculation;
DROP TABLE award_calculation;

DROP POLICY tenant_isolation ON attainment_observation;
ALTER TABLE attainment_observation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE attainment_observation DISABLE ROW LEVEL SECURITY;
DROP TRIGGER attainment_observation_append_only ON attainment_observation;
DROP TABLE attainment_observation;

DROP POLICY tenant_isolation ON incentive_plan_revision;
ALTER TABLE incentive_plan_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE incentive_plan_revision DISABLE ROW LEVEL SECURITY;
DROP TRIGGER incentive_plan_revision_append_only ON incentive_plan_revision;
DROP TABLE incentive_plan_revision;
