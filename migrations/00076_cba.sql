-- Owner: data plane. Phase: P4.
-- PERSIST-CBA-001: immutable collective-bargaining agreement, bargaining-unit,
-- membership and agreement-clause revisions.
--
-- Storage disposition: all four tables are tenant-scoped AGGREGATE rows,
-- retained PERMANENTLY, with no rebuild source. Every meaning change is a new
-- revision; the old row is never updated or deleted.

-- +goose Up

CREATE TABLE IF NOT EXISTS cba_agreement_revision (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid       NOT NULL,
    agreement_id    text       NOT NULL,
    revision        bigint     NOT NULL,
    version         text       NOT NULL,
    title           text       NOT NULL,
    representative  text,
    source          text,
    effective_from  timestamptz,
    effective_to    timestamptz,
    known_from      timestamptz,
    known_to        timestamptz,
    precedence      int        NOT NULL,
    retired         boolean    NOT NULL DEFAULT false,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT cba_agreement_revision_identity
        UNIQUE (tenant_id, agreement_id, revision),
    CONSTRAINT cba_agreement_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT cba_agreement_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from),
    CONSTRAINT cba_agreement_revision_known_order
        CHECK (known_to IS NULL OR known_from IS NULL OR known_to > known_from)
);

CREATE OR REPLACE TRIGGER cba_agreement_revision_append_only
    BEFORE UPDATE OR DELETE ON cba_agreement_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON cba_agreement_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS cba_bargaining_unit_revision (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid       NOT NULL,
    unit_id         text       NOT NULL,
    revision        bigint     NOT NULL,
    agreement_id    text       NOT NULL,
    name            text       NOT NULL,
    representative  text,
    source          text,
    effective_from  timestamptz,
    effective_to    timestamptz,
    known_from      timestamptz,
    known_to        timestamptz,
    retired         boolean    NOT NULL DEFAULT false,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT cba_bargaining_unit_revision_identity
        UNIQUE (tenant_id, unit_id, revision),
    CONSTRAINT cba_bargaining_unit_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT cba_bargaining_unit_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from),
    CONSTRAINT cba_bargaining_unit_revision_known_order
        CHECK (known_to IS NULL OR known_from IS NULL OR known_to > known_from)
);

CREATE OR REPLACE TRIGGER cba_bargaining_unit_revision_append_only
    BEFORE UPDATE OR DELETE ON cba_bargaining_unit_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON cba_bargaining_unit_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS cba_membership_revision (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid       NOT NULL,
    membership_id   text       NOT NULL,
    revision        bigint     NOT NULL,
    worker_id       text       NOT NULL,
    unit_id         text       NOT NULL,
    source          text,
    effective_from  timestamptz,
    effective_to    timestamptz,
    known_from      timestamptz,
    known_to        timestamptz,
    retired         boolean    NOT NULL DEFAULT false,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT cba_membership_revision_identity
        UNIQUE (tenant_id, membership_id, revision),
    CONSTRAINT cba_membership_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT cba_membership_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from),
    CONSTRAINT cba_membership_revision_known_order
        CHECK (known_to IS NULL OR known_from IS NULL OR known_to > known_from)
);

CREATE OR REPLACE TRIGGER cba_membership_revision_append_only
    BEFORE UPDATE OR DELETE ON cba_membership_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON cba_membership_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS cba_agreement_clause_revision (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid       NOT NULL,
    clause_id          text       NOT NULL,
    agreement_id       text       NOT NULL,
    agreement_revision bigint     NOT NULL,
    revision           bigint     NOT NULL,
    kind               text       NOT NULL,
    job_codes          jsonb,
    location_ids       jsonb,
    effective_from     timestamptz,
    effective_to       timestamptz,
    known_from         timestamptz,
    known_to           timestamptz,
    retired            boolean    NOT NULL DEFAULT false,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT cba_agreement_clause_revision_identity
        UNIQUE (tenant_id, clause_id, revision),
    CONSTRAINT cba_agreement_clause_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT cba_agreement_clause_revision_agreement_positive
        CHECK (agreement_revision >= 1),
    CONSTRAINT cba_agreement_clause_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from),
    CONSTRAINT cba_agreement_clause_revision_known_order
        CHECK (known_to IS NULL OR known_from IS NULL OR known_to > known_from),
    CONSTRAINT cba_agreement_clause_revision_job_codes_array
        CHECK (job_codes IS NULL OR jsonb_typeof(job_codes) = 'array'),
    CONSTRAINT cba_agreement_clause_revision_location_ids_array
        CHECK (location_ids IS NULL OR jsonb_typeof(location_ids) = 'array')
);

CREATE OR REPLACE TRIGGER cba_agreement_clause_revision_append_only
    BEFORE UPDATE OR DELETE ON cba_agreement_clause_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON cba_agreement_clause_revision FROM PUBLIC;

-- Every table fails closed until a transaction calls tenancy.WithTenant.
ALTER TABLE cba_agreement_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE cba_agreement_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cba_agreement_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE cba_bargaining_unit_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE cba_bargaining_unit_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cba_bargaining_unit_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE cba_membership_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE cba_membership_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cba_membership_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE cba_agreement_clause_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE cba_agreement_clause_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cba_agreement_clause_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON cba_agreement_revision TO hcmnext_app;
GRANT SELECT, INSERT ON cba_bargaining_unit_revision TO hcmnext_app;
GRANT SELECT, INSERT ON cba_membership_revision TO hcmnext_app;
GRANT SELECT, INSERT ON cba_agreement_clause_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON cba_agreement_clause_revision FROM hcmnext_app;
REVOKE ALL ON cba_membership_revision FROM hcmnext_app;
REVOKE ALL ON cba_bargaining_unit_revision FROM hcmnext_app;
REVOKE ALL ON cba_agreement_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON cba_agreement_clause_revision;
ALTER TABLE cba_agreement_clause_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cba_agreement_clause_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON cba_membership_revision;
ALTER TABLE cba_membership_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cba_membership_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON cba_bargaining_unit_revision;
ALTER TABLE cba_bargaining_unit_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cba_bargaining_unit_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON cba_agreement_revision;
ALTER TABLE cba_agreement_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cba_agreement_revision DISABLE ROW LEVEL SECURITY;

DROP TRIGGER cba_agreement_clause_revision_append_only ON cba_agreement_clause_revision;
DROP TABLE cba_agreement_clause_revision;
DROP TRIGGER cba_membership_revision_append_only ON cba_membership_revision;
DROP TABLE cba_membership_revision;
DROP TRIGGER cba_bargaining_unit_revision_append_only ON cba_bargaining_unit_revision;
DROP TABLE cba_bargaining_unit_revision;
DROP TRIGGER cba_agreement_revision_append_only ON cba_agreement_revision;
DROP TABLE cba_agreement_revision;
