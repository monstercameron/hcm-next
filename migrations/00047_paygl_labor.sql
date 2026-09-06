-- Owner: data plane. Phase: P3.
-- PERSIST-PAYGL-001: durable immutable labor-cost and payroll-to-GL rule
-- revisions, plus append-only mapping results. paygl directly imports the
-- labor dimension vocabulary, so both domain rows share this migration.
--
-- Storage disposition (STORE-001): labor_rule and paygl_accounting_rule are
-- tenant-scoped AGGREGATE rows retained PERMANENTLY; paygl_mapping_result is
-- tenant-scoped LEDGER evidence retained PERMANENTLY and append-only. Rule
-- revisions are superseded by new rows; mapping results are events per run.

-- +goose Up

CREATE TABLE IF NOT EXISTS labor_rule (
    row_id              uuid          NOT NULL,
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    rule_id             text          NOT NULL,
    version             text          NOT NULL,
    effective_from      timestamptz,
    effective_to        timestamptz,
    dimensions          jsonb         NOT NULL,
    base_rate           numeric(19,6),
    differential_rate   numeric(19,6),
    overtime_multiplier numeric(9,4),
    employer_burden_rate numeric(9,6),
    supersedes          text,
    canonical_digest    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT labor_rule_revision_unique UNIQUE (tenant_id, rule_id, version),
    CONSTRAINT labor_rule_effective_interval CHECK (
        effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from
    )
);

CREATE TABLE IF NOT EXISTS paygl_accounting_rule (
    row_id           uuid          NOT NULL,
    tenant_id        tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    rule_id          text          NOT NULL,
    version          text          NOT NULL,
    effective_from   timestamptz,
    effective_to     timestamptz,
    component_kind   text,
    component_code   text,
    dimensions       jsonb         NOT NULL,
    debit_account    text          NOT NULL,
    credit_account   text          NOT NULL,
    supersedes       text,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT paygl_accounting_rule_revision_unique UNIQUE (tenant_id, rule_id, version),
    CONSTRAINT paygl_accounting_rule_effective_interval CHECK (
        effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from
    )
);

CREATE TABLE IF NOT EXISTS paygl_mapping_result (
    row_id         uuid          NOT NULL,
    tenant_id      tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    run_id         text          NOT NULL,
    run_revision   bigint        NOT NULL,
    run_digest     content_digest,
    mappings       jsonb         NOT NULL,
    digest         content_digest NOT NULL,
    event_sequence bigint        NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT paygl_mapping_result_event_unique
        UNIQUE (tenant_id, run_id, event_sequence),
    CONSTRAINT paygl_mapping_result_sequence_positive CHECK (event_sequence >= 1),
    CONSTRAINT paygl_mapping_result_revision_positive CHECK (run_revision >= 1)
);

CREATE OR REPLACE TRIGGER paygl_mapping_result_append_only
    BEFORE UPDATE OR DELETE ON paygl_mapping_result
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON paygl_mapping_result FROM PUBLIC;

-- Every table is fail-closed unless the caller has explicitly scoped the
-- transaction through app.tenant_id. FORCE keeps the policy in force for the
-- table owner as well as hcmnext_app.
ALTER TABLE labor_rule ENABLE ROW LEVEL SECURITY;
ALTER TABLE labor_rule FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON labor_rule
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE paygl_accounting_rule ENABLE ROW LEVEL SECURITY;
ALTER TABLE paygl_accounting_rule FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON paygl_accounting_rule
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE paygl_mapping_result ENABLE ROW LEVEL SECURITY;
ALTER TABLE paygl_mapping_result FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON paygl_mapping_result
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON labor_rule TO hcmnext_app;
GRANT SELECT, INSERT ON paygl_accounting_rule TO hcmnext_app;
GRANT SELECT, INSERT ON paygl_mapping_result TO hcmnext_app;

-- +goose Down

REVOKE ALL ON paygl_mapping_result FROM hcmnext_app;
REVOKE ALL ON paygl_accounting_rule FROM hcmnext_app;
REVOKE ALL ON labor_rule FROM hcmnext_app;

DROP POLICY tenant_isolation ON paygl_mapping_result;
ALTER TABLE paygl_mapping_result NO FORCE ROW LEVEL SECURITY;
ALTER TABLE paygl_mapping_result DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON paygl_accounting_rule;
ALTER TABLE paygl_accounting_rule NO FORCE ROW LEVEL SECURITY;
ALTER TABLE paygl_accounting_rule DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON labor_rule;
ALTER TABLE labor_rule NO FORCE ROW LEVEL SECURITY;
ALTER TABLE labor_rule DISABLE ROW LEVEL SECURITY;

DROP TABLE paygl_mapping_result;
DROP TABLE paygl_accounting_rule;
DROP TABLE labor_rule;
