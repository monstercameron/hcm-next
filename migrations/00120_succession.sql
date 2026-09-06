-- Owner: succession domain lane. Phase: 4.
-- PERSIST-SUCCESSION-001: immutable critical-role, successor-readiness and
-- succession-slate revisions under tenant isolation.
--
-- All three tables are tenant-scoped AGGREGATE rows retained permanently.
-- Revisions are never rewritten: a later revision names its parent and is
-- inserted as a new row. The composite slate foreign key makes the role
-- binding a same-tenant, exact-revision reference at the database boundary.

-- +goose Up

CREATE TABLE IF NOT EXISTS succession_critical_role (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    role_id          text           NOT NULL,
    revision         cas_version    NOT NULL,
    parent_revision  cas_version,
    parent_digest    content_digest,
    position_ref     text,
    job_revision_ref text,
    owner_ref        text,
    authority_ref    text,
    effective_at     timestamptz,
    known_at         timestamptz,
    evidence_refs    jsonb,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT succession_critical_role_identity
        UNIQUE (tenant_id, role_id, revision),
    CONSTRAINT succession_critical_role_parent_order
        CHECK (parent_revision IS NULL OR parent_revision < revision),
    CONSTRAINT succession_critical_role_parent_digest_pair
        CHECK ((parent_revision IS NULL) = (parent_digest IS NULL))
);

CREATE OR REPLACE TRIGGER succession_critical_role_append_only
    BEFORE UPDATE OR DELETE ON succession_critical_role
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON succession_critical_role FROM PUBLIC;

CREATE TABLE IF NOT EXISTS succession_readiness_revision (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    successor_id     text           NOT NULL,
    revision         cas_version    NOT NULL,
    parent_revision  cas_version,
    readiness        text           NOT NULL,
    vacancy_risk     text,
    assessed_by      text,
    assessment_source text,
    evidence_refs    jsonb,
    effective_at     timestamptz,
    known_at         timestamptz,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT succession_readiness_revision_identity
        UNIQUE (tenant_id, successor_id, revision),
    CONSTRAINT succession_readiness_revision_parent_order
        CHECK (parent_revision IS NULL OR parent_revision < revision)
);

CREATE OR REPLACE TRIGGER succession_readiness_revision_append_only
    BEFORE UPDATE OR DELETE ON succession_readiness_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON succession_readiness_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS succession_slate (
    tenant_id           tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid           NOT NULL,
    slate_id            text           NOT NULL,
    revision            cas_version    NOT NULL,
    parent_revision     cas_version,
    critical_role_id    text           NOT NULL,
    critical_role_revision cas_version NOT NULL,
    position_ref        text,
    job_revision_ref    text,
    nominator_id        text,
    visibility          text           NOT NULL,
    candidates          jsonb,
    effective_at        timestamptz,
    known_at            timestamptz,
    canonical_digest    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT succession_slate_identity
        UNIQUE (tenant_id, slate_id, revision),
    CONSTRAINT succession_slate_parent_order
        CHECK (parent_revision IS NULL OR parent_revision < revision),
    CONSTRAINT succession_slate_critical_role_revision
        FOREIGN KEY (tenant_id, critical_role_id, critical_role_revision)
        REFERENCES succession_critical_role (tenant_id, role_id, revision)
);

CREATE OR REPLACE TRIGGER succession_slate_append_only
    BEFORE UPDATE OR DELETE ON succession_slate
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON succession_slate FROM PUBLIC;

-- Tenant isolation (DB-017): missing or blank app.tenant_id fails closed.
ALTER TABLE succession_critical_role ENABLE ROW LEVEL SECURITY;
ALTER TABLE succession_critical_role FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON succession_critical_role
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE succession_readiness_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE succession_readiness_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON succession_readiness_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE succession_slate ENABLE ROW LEVEL SECURITY;
ALTER TABLE succession_slate FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON succession_slate
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON
    succession_critical_role,
    succession_readiness_revision,
    succession_slate
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON succession_slate FROM hcmnext_app;
REVOKE ALL ON succession_readiness_revision FROM hcmnext_app;
REVOKE ALL ON succession_critical_role FROM hcmnext_app;

DROP POLICY tenant_isolation ON succession_slate;
ALTER TABLE succession_slate NO FORCE ROW LEVEL SECURITY;
ALTER TABLE succession_slate DISABLE ROW LEVEL SECURITY;
DROP TRIGGER succession_slate_append_only ON succession_slate;
DROP TABLE succession_slate;

DROP POLICY tenant_isolation ON succession_readiness_revision;
ALTER TABLE succession_readiness_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE succession_readiness_revision DISABLE ROW LEVEL SECURITY;
DROP TRIGGER succession_readiness_revision_append_only ON succession_readiness_revision;
DROP TABLE succession_readiness_revision;

DROP POLICY tenant_isolation ON succession_critical_role;
ALTER TABLE succession_critical_role NO FORCE ROW LEVEL SECURITY;
ALTER TABLE succession_critical_role DISABLE ROW LEVEL SECURITY;
DROP TRIGGER succession_critical_role_append_only ON succession_critical_role;
DROP TABLE succession_critical_role;
