-- Owner: data plane. Phase: PERSIST-PAYINPUT-001.
-- Durable, tenant-isolated earning/deduction definitions and worker input
-- assignments. Payroll calculation remains outside this table set.

-- +goose Up

CREATE TABLE IF NOT EXISTS payinput_definition (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid         NOT NULL,
    definition_id      text         NOT NULL,
    code               text         NOT NULL,
    kind               text         NOT NULL,
    taxability         jsonb,
    calculation_basis  jsonb,
    limits             jsonb,
    effective_from     timestamptz,
    effective_to       timestamptz,
    version            text         NOT NULL,
    revision           bigint       NOT NULL,
    state              text         NOT NULL,
    supersedes_digest  content_digest,
    supersedes_revision bigint,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT payinput_definition_identity
        UNIQUE (tenant_id, definition_id, revision),
    CONSTRAINT payinput_definition_kind_allowed
        CHECK (kind IN ('EARNING', 'DEDUCTION')),
    CONSTRAINT payinput_definition_state_allowed
        CHECK (state IN ('DRAFT', 'PUBLISHED', 'RETIRED', 'SUPERSEDED')),
    CONSTRAINT payinput_definition_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT payinput_definition_lineage_consistent
        CHECK ((revision = 1 AND supersedes_revision IS NULL AND supersedes_digest IS NULL)
            OR (revision > 1 AND supersedes_revision IS NOT NULL AND supersedes_digest IS NOT NULL)),
    CONSTRAINT payinput_definition_effective_half_open
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_from < effective_to)
);

-- A new definition revision is the only way to change a definition. The
-- trigger is defense in depth around the application CAS and the RLS grant.
CREATE OR REPLACE TRIGGER payinput_definition_forbid_mutation
    BEFORE UPDATE OR DELETE ON payinput_definition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON payinput_definition FROM PUBLIC;

CREATE TABLE IF NOT EXISTS payinput_worker_assignment (
    tenant_id           tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid         NOT NULL,
    assignment_id       text         NOT NULL,
    worker_ref          uuid         NOT NULL,
    definition_ref      text         NOT NULL,
    definition_revision bigint       NOT NULL,
    effective_from      timestamptz,
    effective_to        timestamptz,
    amount              numeric(19,4),
    rate                numeric(19,6),
    recurrence          text,
    canonical_digest    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT payinput_worker_assignment_identity
        UNIQUE (tenant_id, assignment_id),
    CONSTRAINT payinput_worker_assignment_definition_fk
        FOREIGN KEY (tenant_id, definition_ref, definition_revision)
        REFERENCES payinput_definition (tenant_id, definition_id, revision),
    CONSTRAINT payinput_worker_assignment_effective_half_open
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_from < effective_to),
    CONSTRAINT payinput_worker_assignment_one_value
        CHECK ((amount IS NOT NULL) <> (rate IS NOT NULL)),
    CONSTRAINT payinput_worker_assignment_recurrence_allowed
        CHECK (recurrence IN ('ONE_TIME', 'PER_PAYROLL_PERIOD', 'MONTHLY', 'ANNUAL'))
);

-- WorkerAssignment is an immutable binding: replacing its meaning requires a
-- new assignment identity, just as replacing a definition requires a revision.
CREATE OR REPLACE TRIGGER payinput_worker_assignment_forbid_mutation
    BEFORE UPDATE OR DELETE ON payinput_worker_assignment
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON payinput_worker_assignment FROM PUBLIC;

CREATE INDEX IF NOT EXISTS payinput_definition_current
    ON payinput_definition (tenant_id, definition_id, revision DESC);

CREATE INDEX IF NOT EXISTS payinput_worker_assignment_worker
    ON payinput_worker_assignment (tenant_id, worker_ref, assignment_id);

ALTER TABLE payinput_definition ENABLE ROW LEVEL SECURITY;
ALTER TABLE payinput_definition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payinput_definition
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE payinput_worker_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE payinput_worker_assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payinput_worker_assignment
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON payinput_definition TO hcmnext_app;
GRANT SELECT, INSERT ON payinput_worker_assignment TO hcmnext_app;

-- +goose Down

REVOKE ALL ON payinput_worker_assignment FROM hcmnext_app;
REVOKE ALL ON payinput_definition FROM hcmnext_app;

DROP POLICY tenant_isolation ON payinput_worker_assignment;
ALTER TABLE payinput_worker_assignment NO FORCE ROW LEVEL SECURITY;
ALTER TABLE payinput_worker_assignment DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON payinput_definition;
ALTER TABLE payinput_definition NO FORCE ROW LEVEL SECURITY;
ALTER TABLE payinput_definition DISABLE ROW LEVEL SECURITY;

DROP TABLE payinput_worker_assignment;
DROP TABLE payinput_definition;
