-- Owner: data plane. Phase: P3.
-- PERSIST-PLANNING-001: durable, tenant-isolated workforce-planning input.
--
-- These tables store descriptive demand, coverage and hypothetical scenario
-- state only. They do not reserve positions, create assignments or grant
-- budget authority. The JSON columns retain the domain's typed value metadata
-- (interval kind, calendar and decimal scale) while the scalar columns remain
-- queryable projections of the same immutable value.

-- +goose Up

CREATE TABLE IF NOT EXISTS demand_signal (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid           NOT NULL,
    signal_id             semantic_key   NOT NULL,
    work_effective_from   timestamptz,
    work_effective_to     timestamptz,
    location              text,
    org_unit              text,
    quantity              numeric(9,2)   NOT NULL,
    unit                  text           NOT NULL,
    skill                 text,
    role                  text,
    priority              text,
    source                text,
    source_ref            text,
    confidence            numeric(5,4),
    confidence_class      text,
    scenario_ref          text,
    version               semantic_key   NOT NULL,
    canonical_digest      content_digest NOT NULL,
    -- Projection columns cannot retain LOCAL_DATE calendar semantics or the
    -- declared decimal rounding mode, so this wire value is part of the
    -- immutable row rather than process memory.
    work_interval         jsonb          NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT demand_signal_identity_unique
        UNIQUE (tenant_id, signal_id, version),
    CONSTRAINT demand_signal_quantity_finite
        CHECK (quantity >= 0),
    CONSTRAINT demand_signal_confidence_range
        CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    CONSTRAINT demand_signal_confidence_class_allowed
        CHECK (confidence_class IS NULL OR confidence_class IN ('LOW', 'MEDIUM', 'HIGH', 'UNKNOWN')),
    CONSTRAINT demand_signal_work_interval_object
        CHECK (jsonb_typeof(work_interval) = 'object')
);

CREATE OR REPLACE TRIGGER demand_signal_append_only
    BEFORE UPDATE OR DELETE ON demand_signal
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON demand_signal FROM PUBLIC;

CREATE TABLE IF NOT EXISTS coverage_requirement (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid           NOT NULL,
    requirement_id     semantic_key   NOT NULL,
    scenario_ref       text,
    version            semantic_key   NOT NULL,
    signal_refs        jsonb,
    supply_refs        jsonb,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT coverage_requirement_identity_unique
        UNIQUE (tenant_id, requirement_id, version),
    CONSTRAINT coverage_requirement_signal_refs_array
        CHECK (signal_refs IS NULL OR jsonb_typeof(signal_refs) = 'array'),
    CONSTRAINT coverage_requirement_supply_refs_array
        CHECK (supply_refs IS NULL OR jsonb_typeof(supply_refs) = 'array')
);

CREATE OR REPLACE TRIGGER coverage_requirement_append_only
    BEFORE UPDATE OR DELETE ON coverage_requirement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON coverage_requirement FROM PUBLIC;

CREATE TABLE IF NOT EXISTS scenario_revision (
    tenant_id              tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id                 uuid           NOT NULL,
    scenario_id            semantic_key   NOT NULL,
    revision               cas_version    NOT NULL,
    parent_revision        cas_version,
    parent_digest          content_digest,
    owner                  text,
    scope                  text,
    horizon_from           timestamptz,
    horizon_to             timestamptz,
    assumptions            jsonb          NOT NULL,
    baseline_snapshot_ref  text,
    author                 text,
    authority_disclaimer   text           NOT NULL,
    lifecycle              text           NOT NULL,
    canonical_digest       content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT scenario_revision_identity_unique
        UNIQUE (tenant_id, scenario_id, revision),
    CONSTRAINT scenario_revision_parent_same_scenario
        FOREIGN KEY (tenant_id, scenario_id, parent_revision)
        REFERENCES scenario_revision (tenant_id, scenario_id, revision),
    CONSTRAINT scenario_revision_lineage_consistent
        CHECK (
            (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
            OR
            (revision > 1 AND parent_revision IS NOT NULL
                AND parent_revision < revision AND parent_digest IS NOT NULL)
        ),
    CONSTRAINT scenario_revision_authority_disclaimer_not_blank
        CHECK (btrim(authority_disclaimer) <> ''),
    CONSTRAINT scenario_revision_lifecycle_allowed
        CHECK (lifecycle IN ('DRAFT', 'READY', 'RETIRED')),
    CONSTRAINT scenario_revision_assumptions_array
        CHECK (jsonb_typeof(assumptions) = 'object')
);

CREATE OR REPLACE TRIGGER scenario_revision_append_only
    BEFORE UPDATE OR DELETE ON scenario_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON scenario_revision FROM PUBLIC;

CREATE INDEX IF NOT EXISTS demand_signal_lookup
    ON demand_signal (tenant_id, signal_id, version);
CREATE INDEX IF NOT EXISTS coverage_requirement_lookup
    ON coverage_requirement (tenant_id, requirement_id, version);
CREATE INDEX IF NOT EXISTS scenario_revision_lookup
    ON scenario_revision (tenant_id, scenario_id, revision);

ALTER TABLE demand_signal ENABLE ROW LEVEL SECURITY;
ALTER TABLE demand_signal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON demand_signal
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE coverage_requirement ENABLE ROW LEVEL SECURITY;
ALTER TABLE coverage_requirement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON coverage_requirement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE scenario_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE scenario_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON scenario_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON demand_signal TO hcmnext_app;
GRANT SELECT, INSERT ON coverage_requirement TO hcmnext_app;
GRANT SELECT, INSERT ON scenario_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON scenario_revision FROM hcmnext_app;
REVOKE ALL ON coverage_requirement FROM hcmnext_app;
REVOKE ALL ON demand_signal FROM hcmnext_app;

DROP POLICY tenant_isolation ON scenario_revision;
ALTER TABLE scenario_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE scenario_revision DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON coverage_requirement;
ALTER TABLE coverage_requirement NO FORCE ROW LEVEL SECURITY;
ALTER TABLE coverage_requirement DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON demand_signal;
ALTER TABLE demand_signal NO FORCE ROW LEVEL SECURITY;
ALTER TABLE demand_signal DISABLE ROW LEVEL SECURITY;

DROP TABLE scenario_revision;
DROP TABLE coverage_requirement;
DROP TABLE demand_signal;
