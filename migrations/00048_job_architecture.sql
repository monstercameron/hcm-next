-- Owner: job architecture data lane. Phase: P3.
-- PERSIST-JOBARCH-001: immutable tenant-scoped job-architecture revisions.
--
-- The four node tables deliberately share one storage envelope. Their
-- lineage JSONB carries the domain revision payload in addition to the
-- root/supersedes lineage fields because the persistence contract fixes the
-- physical envelope rather than allowing a second, table-specific payload
-- shape. job_architecture_revision stores only references to those rows; the
-- data adapter resolves them in the same tenant before returning a graph.
--
-- Storage disposition (STORE-001): all five tables are permanent,
-- append-only aggregate revisions, tenant scoped by tenant_id, owned by
-- internal/data/jobarchstore, and have no rebuild source.

-- +goose Up

CREATE TABLE IF NOT EXISTS job_family_revision (
    row_id         uuid        NOT NULL,
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    family_id      semantic_key NOT NULL,
    revision       semantic_key NOT NULL,
    parent_id      semantic_key,
    lifecycle      text        NOT NULL,
    effective_from timestamptz,
    effective_to   timestamptz,
    known_from     timestamptz,
    known_to       timestamptz,
    lineage        jsonb       NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, family_id, revision)
);

CREATE TABLE IF NOT EXISTS job_level_revision (
    row_id         uuid        NOT NULL,
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    level_id       semantic_key NOT NULL,
    revision       semantic_key NOT NULL,
    parent_id      semantic_key,
    lifecycle      text        NOT NULL,
    effective_from timestamptz,
    effective_to   timestamptz,
    known_from     timestamptz,
    known_to       timestamptz,
    lineage        jsonb       NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, level_id, revision)
);

CREATE TABLE IF NOT EXISTS job_grade_revision (
    row_id         uuid        NOT NULL,
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    grade_id       semantic_key NOT NULL,
    revision       semantic_key NOT NULL,
    parent_id      semantic_key,
    lifecycle      text        NOT NULL,
    effective_from timestamptz,
    effective_to   timestamptz,
    known_from     timestamptz,
    known_to       timestamptz,
    lineage        jsonb       NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, grade_id, revision)
);

CREATE TABLE IF NOT EXISTS job_profile_revision (
    row_id         uuid        NOT NULL,
    tenant_id      tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    profile_id     semantic_key NOT NULL,
    revision       semantic_key NOT NULL,
    parent_id      semantic_key,
    lifecycle      text        NOT NULL,
    effective_from timestamptz,
    effective_to   timestamptz,
    known_from     timestamptz,
    known_to       timestamptz,
    lineage        jsonb       NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, profile_id, revision)
);

CREATE TABLE IF NOT EXISTS job_architecture_revision (
    row_id             uuid        NOT NULL,
    tenant_id          tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    architecture_id    semantic_key NOT NULL,
    revision           semantic_key NOT NULL,
    supersedes_revision semantic_key,
    families           jsonb       NOT NULL,
    levels             jsonb       NOT NULL,
    grades             jsonb       NOT NULL,
    profiles           jsonb       NOT NULL,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, architecture_id, revision)
);

-- All five relations are immutable revisions. A successor is a new row whose
-- lineage names its parent; no caller receives UPDATE or DELETE capability.
CREATE OR REPLACE TRIGGER job_family_revision_append_only
    BEFORE UPDATE OR DELETE ON job_family_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER job_level_revision_append_only
    BEFORE UPDATE OR DELETE ON job_level_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER job_grade_revision_append_only
    BEFORE UPDATE OR DELETE ON job_grade_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER job_profile_revision_append_only
    BEFORE UPDATE OR DELETE ON job_profile_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER job_architecture_revision_append_only
    BEFORE UPDATE OR DELETE ON job_architecture_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON job_family_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON job_level_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON job_grade_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON job_profile_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON job_architecture_revision FROM PUBLIC;

ALTER TABLE job_family_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_family_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_family_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE job_level_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_level_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_level_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE job_grade_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_grade_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_grade_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE job_profile_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_profile_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_profile_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE job_architecture_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_architecture_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON job_architecture_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON job_family_revision TO hcmnext_app;
GRANT SELECT, INSERT ON job_level_revision TO hcmnext_app;
GRANT SELECT, INSERT ON job_grade_revision TO hcmnext_app;
GRANT SELECT, INSERT ON job_profile_revision TO hcmnext_app;
GRANT SELECT, INSERT ON job_architecture_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON job_architecture_revision FROM hcmnext_app;
REVOKE ALL ON job_profile_revision FROM hcmnext_app;
REVOKE ALL ON job_grade_revision FROM hcmnext_app;
REVOKE ALL ON job_level_revision FROM hcmnext_app;
REVOKE ALL ON job_family_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON job_architecture_revision;
ALTER TABLE job_architecture_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE job_architecture_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON job_profile_revision;
ALTER TABLE job_profile_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE job_profile_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON job_grade_revision;
ALTER TABLE job_grade_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE job_grade_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON job_level_revision;
ALTER TABLE job_level_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE job_level_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON job_family_revision;
ALTER TABLE job_family_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE job_family_revision DISABLE ROW LEVEL SECURITY;

DROP TABLE job_architecture_revision;
DROP TABLE job_profile_revision;
DROP TABLE job_grade_revision;
DROP TABLE job_level_revision;
DROP TABLE job_family_revision;
