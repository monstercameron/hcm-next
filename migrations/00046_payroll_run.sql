-- Owner: payroll persistence lane. Phase: Gate A.
-- PERSIST-PAYROLL-001: durable payroll-run and frozen-population lifecycle.
--
-- This table set deliberately excludes payroll/auditpack checkpoint and
-- reconciliation evidence. That path is already ledger-backed by DB-024 in
-- migration 00024. These rows hold the immutable lifecycle revisions and the
-- append-only population amendment facts that the calculation kernel names.
--
-- Storage disposition (STORE-001):
--   payroll_run                 AGGREGATE, PERMANENT, tenant_id, immutable,
--                               rebuild_source none.
--   payroll_frozen_population   AGGREGATE, PERMANENT, tenant_id, immutable,
--                               rebuild_source none.
--   payroll_population_amendment LEDGER, PERMANENT, tenant_id, append-only,
--                               rebuild_source none.

-- +goose Up

CREATE TABLE IF NOT EXISTS payroll_run (
    tenant_id                 tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                    uuid         NOT NULL,
    run_id                    semantic_key NOT NULL,
    revision                  cas_version  NOT NULL,
    state                     text         NOT NULL,
    pay_group_ref             semantic_key NOT NULL,
    period                    jsonb        NOT NULL,
    population_binding_ref    text,
    calculation_input_digest  content_digest,
    calculation_digest        content_digest,
    release_digest            content_digest,
    reversal_digest           content_digest,
    supersedes_revision       cas_version,
    canonical_digest          content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT payroll_run_revision_unique
        UNIQUE (tenant_id, run_id, revision),
    CONSTRAINT payroll_run_state_allowed CHECK (
        state IN ('DRAFT', 'CALCULATED', 'RELEASED', 'SETTLED', 'REVERSED')
    )
);

CREATE OR REPLACE TRIGGER payroll_run_append_only
    BEFORE UPDATE OR DELETE ON payroll_run
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON payroll_run FROM PUBLIC;

CREATE TABLE IF NOT EXISTS payroll_frozen_population (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid         NOT NULL,
    run_id             semantic_key NOT NULL,
    run_revision       cas_version  NOT NULL,
    pay_group_ref      semantic_key,
    binding             jsonb,
    as_of              timestamptz  NOT NULL,
    members            jsonb        NOT NULL,
    revision           cas_version  NOT NULL,
    state              text         NOT NULL,
    supersedes_digest  content_digest,
    amendment_digest   content_digest,
    digest             content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT payroll_frozen_population_revision_unique
        UNIQUE (tenant_id, run_id, revision),
    CONSTRAINT payroll_frozen_population_run_revision_fk
        FOREIGN KEY (tenant_id, run_id, run_revision)
        REFERENCES payroll_run (tenant_id, run_id, revision),
    CONSTRAINT payroll_frozen_population_state_allowed CHECK (
        state IN ('FROZEN', 'SUPERSEDED')
    )
);

CREATE OR REPLACE TRIGGER payroll_frozen_population_append_only
    BEFORE UPDATE OR DELETE ON payroll_frozen_population
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON payroll_frozen_population FROM PUBLIC;

CREATE TABLE IF NOT EXISTS payroll_population_amendment (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid       NOT NULL,
    run_id             semantic_key NOT NULL,
    kind               text       NOT NULL,
    member             jsonb      NOT NULL,
    reason             text,
    effective_as_of    timestamptz,
    digest             content_digest NOT NULL,
    event_sequence     bigint     NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT payroll_population_amendment_sequence_unique
        UNIQUE (tenant_id, run_id, event_sequence),
    CONSTRAINT payroll_population_amendment_kind_allowed CHECK (
        kind IN ('LATE_ENTRY', 'REMOVAL')
    ),
    CONSTRAINT payroll_population_amendment_sequence_positive CHECK (
        event_sequence >= 1
    )
);

CREATE OR REPLACE TRIGGER payroll_population_amendment_append_only
    BEFORE UPDATE OR DELETE ON payroll_population_amendment
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON payroll_population_amendment FROM PUBLIC;

ALTER TABLE payroll_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE payroll_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payroll_run
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE payroll_frozen_population ENABLE ROW LEVEL SECURITY;
ALTER TABLE payroll_frozen_population FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payroll_frozen_population
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE payroll_population_amendment ENABLE ROW LEVEL SECURITY;
ALTER TABLE payroll_population_amendment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payroll_population_amendment
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON payroll_run TO hcmnext_app;
GRANT SELECT, INSERT ON payroll_frozen_population TO hcmnext_app;
GRANT SELECT, INSERT ON payroll_population_amendment TO hcmnext_app;

-- +goose Down

REVOKE ALL ON payroll_population_amendment FROM hcmnext_app;
REVOKE ALL ON payroll_frozen_population FROM hcmnext_app;
REVOKE ALL ON payroll_run FROM hcmnext_app;

DROP POLICY tenant_isolation ON payroll_population_amendment;
ALTER TABLE payroll_population_amendment NO FORCE ROW LEVEL SECURITY;
ALTER TABLE payroll_population_amendment DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON payroll_frozen_population;
ALTER TABLE payroll_frozen_population NO FORCE ROW LEVEL SECURITY;
ALTER TABLE payroll_frozen_population DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON payroll_run;
ALTER TABLE payroll_run NO FORCE ROW LEVEL SECURITY;
ALTER TABLE payroll_run DISABLE ROW LEVEL SECURITY;

DROP TABLE payroll_population_amendment;
DROP TABLE payroll_frozen_population;
DROP TABLE payroll_run;
