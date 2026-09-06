-- Owner: career domain data lane. Phase: 4.
-- PERSIST-CAREER-001: durable tenant-scoped revisions for the canonical
-- career profile generation. These four aggregates are descriptive career
-- metadata; they are not hiring, assignment or promotion authority.
--
-- Storage disposition: all four tables are permanent, append-only aggregate
-- revisions owned by internal/data/careerstore, tenant-scoped by tenant_id,
-- and have no rebuild source.

-- +goose Up

CREATE TABLE IF NOT EXISTS career_preference_revision (
    row_id              uuid          NOT NULL,
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    preference_id       uuid          NOT NULL,
    revision            cas_version   NOT NULL,
    supersedes          cas_version,
    worker_ref          uuid          NOT NULL,
    roles               jsonb,
    locations           jsonb,
    work_arrangements   jsonb,
    mobility_preference text,
    travel_preference   text,
    timing_constraints  jsonb,
    visibility          text          NOT NULL,
    consent_ref         uuid,
    effective_from      timestamptz,
    effective_to        timestamptz,
    canonical_digest    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT career_preference_revision_identity
        UNIQUE (tenant_id, preference_id, revision),
    CONSTRAINT career_preference_revision_supersedes_order
        CHECK (supersedes IS NULL OR supersedes < revision),
    CONSTRAINT career_preference_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE TABLE IF NOT EXISTS target_role_revision (
    row_id              uuid          NOT NULL,
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    target_role_id      uuid          NOT NULL,
    revision            cas_version   NOT NULL,
    supersedes          cas_version,
    worker_ref          uuid          NOT NULL,
    job_profile_ref     uuid,
    job_profile_revision cas_version,
    priority            int,
    rationale           text,
    visibility          text          NOT NULL,
    effective_from      timestamptz,
    effective_to        timestamptz,
    canonical_digest    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT target_role_revision_identity
        UNIQUE (tenant_id, target_role_id, revision),
    CONSTRAINT target_role_revision_supersedes_order
        CHECK (supersedes IS NULL OR supersedes < revision),
    CONSTRAINT target_role_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE TABLE IF NOT EXISTS development_objective_revision (
    row_id              uuid          NOT NULL,
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    objective_id        uuid          NOT NULL,
    revision            cas_version   NOT NULL,
    supersedes          cas_version,
    worker_ref          uuid          NOT NULL,
    target_role_ref     uuid,
    skill_refs          jsonb,
    description         text,
    owner               text,
    visibility          text          NOT NULL,
    effective_from      timestamptz,
    effective_to        timestamptz,
    canonical_digest    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT development_objective_revision_identity
        UNIQUE (tenant_id, objective_id, revision),
    CONSTRAINT development_objective_revision_supersedes_order
        CHECK (supersedes IS NULL OR supersedes < revision),
    CONSTRAINT development_objective_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE TABLE IF NOT EXISTS career_assessment_revision (
    row_id              uuid          NOT NULL,
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    assessment_id       uuid          NOT NULL,
    revision            cas_version   NOT NULL,
    supersedes          cas_version,
    worker_ref          uuid          NOT NULL,
    target_role_ref     uuid,
    source              text,
    epistemic_class     text          NOT NULL,
    summary             text,
    visibility          text          NOT NULL,
    effective_from      timestamptz,
    effective_to        timestamptz,
    canonical_digest    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT career_assessment_revision_identity
        UNIQUE (tenant_id, assessment_id, revision),
    CONSTRAINT career_assessment_revision_supersedes_order
        CHECK (supersedes IS NULL OR supersedes < revision),
    CONSTRAINT career_assessment_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE OR REPLACE TRIGGER career_preference_revision_append_only
    BEFORE UPDATE OR DELETE ON career_preference_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER target_role_revision_append_only
    BEFORE UPDATE OR DELETE ON target_role_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER development_objective_revision_append_only
    BEFORE UPDATE OR DELETE ON development_objective_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER career_assessment_revision_append_only
    BEFORE UPDATE OR DELETE ON career_assessment_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON career_preference_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON target_role_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON development_objective_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON career_assessment_revision FROM PUBLIC;

ALTER TABLE career_preference_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE career_preference_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON career_preference_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE target_role_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE target_role_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON target_role_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE development_objective_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE development_objective_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON development_objective_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE career_assessment_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE career_assessment_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON career_assessment_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON career_preference_revision TO hcmnext_app;
GRANT SELECT, INSERT ON target_role_revision TO hcmnext_app;
GRANT SELECT, INSERT ON development_objective_revision TO hcmnext_app;
GRANT SELECT, INSERT ON career_assessment_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON career_assessment_revision FROM hcmnext_app;
REVOKE ALL ON development_objective_revision FROM hcmnext_app;
REVOKE ALL ON target_role_revision FROM hcmnext_app;
REVOKE ALL ON career_preference_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON career_assessment_revision;
ALTER TABLE career_assessment_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE career_assessment_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON development_objective_revision;
ALTER TABLE development_objective_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE development_objective_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON target_role_revision;
ALTER TABLE target_role_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE target_role_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON career_preference_revision;
ALTER TABLE career_preference_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE career_preference_revision DISABLE ROW LEVEL SECURITY;

DROP TRIGGER career_assessment_revision_append_only ON career_assessment_revision;
DROP TRIGGER development_objective_revision_append_only ON development_objective_revision;
DROP TRIGGER target_role_revision_append_only ON target_role_revision;
DROP TRIGGER career_preference_revision_append_only ON career_preference_revision;

DROP TABLE career_assessment_revision;
DROP TABLE development_objective_revision;
DROP TABLE target_role_revision;
DROP TABLE career_preference_revision;
