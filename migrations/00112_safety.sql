-- Owner: data plane. Phase: Gate A.
-- PERSIST-SAFETY-001: immutable workplace-safety revision families.
--
-- Each table is a tenant-scoped append-only fact log. The domain package keeps
-- the semantic vocabulary and clock calculation pure; this migration keeps
-- the durable lineage envelope and the compartment references separate.
-- Storage disposition rows are required in definitions/storage/storage-
-- disposition.yaml by STORE-001; this lane reports those rows without editing
-- the definitions root.

-- +goose Up

CREATE TABLE IF NOT EXISTS safety_incident_revision (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid NOT NULL,
    case_ref        uuid NOT NULL,
    compartment_ref uuid NOT NULL,
    incident_id     uuid NOT NULL,
    revision        cas_version NOT NULL,
    parent_revision cas_version,
    parent_digest   content_digest,
    canonical_digest content_digest NOT NULL,
    incident_at     timestamptz NOT NULL,
    kind            text NOT NULL,
    worker_ref      uuid,
    reporter_ref    uuid,
    location_ref    uuid,
    description     text,
    status          text NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, incident_id, revision),
    CONSTRAINT safety_incident_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision = revision - 1 AND parent_digest IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS safety_injury_revision (
    tenant_id           tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid NOT NULL,
    case_ref            uuid NOT NULL,
    compartment_ref     uuid NOT NULL,
    injury_id           uuid NOT NULL,
    revision            cas_version NOT NULL,
    parent_revision     cas_version,
    parent_digest       content_digest,
    canonical_digest    content_digest NOT NULL,
    incident_ref        uuid NOT NULL,
    worker_ref          uuid NOT NULL,
    kind                text NOT NULL,
    medical_evidence_ref uuid,
    severity            text,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, injury_id, revision),
    CONSTRAINT safety_injury_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision = revision - 1 AND parent_digest IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS safety_reportability_revision (
    tenant_id        tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid NOT NULL,
    case_ref         uuid NOT NULL,
    compartment_ref  uuid NOT NULL,
    reportability_id uuid NOT NULL,
    revision         cas_version NOT NULL,
    parent_revision  cas_version,
    parent_digest    content_digest,
    canonical_digest content_digest NOT NULL,
    incident_ref     uuid NOT NULL,
    incident_at      timestamptz NOT NULL,
    class            text NOT NULL,
    clock            jsonb NOT NULL,
    rule_citation    text,
    deadline         timestamptz NOT NULL,
    rationale        text,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, reportability_id, revision),
    CONSTRAINT safety_reportability_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision = revision - 1 AND parent_digest IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS safety_claim_revision (
    tenant_id        tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid NOT NULL,
    case_ref         uuid NOT NULL,
    compartment_ref  uuid NOT NULL,
    claim_id         uuid NOT NULL,
    revision         cas_version NOT NULL,
    parent_revision  cas_version,
    parent_digest    content_digest,
    canonical_digest content_digest NOT NULL,
    incident_ref     uuid NOT NULL,
    worker_ref       uuid NOT NULL,
    claim_ref        text,
    authority_ref    uuid,
    status           text NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, claim_id, revision),
    CONSTRAINT safety_claim_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision = revision - 1 AND parent_digest IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS safety_work_restriction_revision (
    tenant_id            tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id               uuid NOT NULL,
    case_ref             uuid NOT NULL,
    compartment_ref      uuid NOT NULL,
    work_restriction_id  uuid NOT NULL,
    revision             cas_version NOT NULL,
    parent_revision      cas_version,
    parent_digest        content_digest,
    canonical_digest     content_digest NOT NULL,
    incident_ref         uuid NOT NULL,
    worker_ref           uuid NOT NULL,
    kind                 text NOT NULL,
    medical_evidence_ref uuid,
    status               text NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, work_restriction_id, revision),
    CONSTRAINT safety_work_restriction_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision = revision - 1 AND parent_digest IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS safety_corrective_action_revision (
    tenant_id              tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id                 uuid NOT NULL,
    case_ref               uuid NOT NULL,
    compartment_ref        uuid NOT NULL,
    corrective_action_id   uuid NOT NULL,
    revision               cas_version NOT NULL,
    parent_revision        cas_version,
    parent_digest          content_digest,
    canonical_digest       content_digest NOT NULL,
    incident_ref           uuid NOT NULL,
    owner_ref              uuid,
    due_rule               text,
    verification_evidence_ref uuid,
    action                 text NOT NULL,
    status                 text NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, corrective_action_id, revision),
    CONSTRAINT safety_corrective_action_revision_positive CHECK (revision IS NOT NULL AND revision >= 1),
    CONSTRAINT safety_corrective_action_lineage CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR (revision > 1 AND parent_revision = revision - 1 AND parent_digest IS NOT NULL)
    )
);

CREATE OR REPLACE TRIGGER safety_incident_revision_append_only
    BEFORE UPDATE OR DELETE ON safety_incident_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER safety_injury_revision_append_only
    BEFORE UPDATE OR DELETE ON safety_injury_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER safety_reportability_revision_append_only
    BEFORE UPDATE OR DELETE ON safety_reportability_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER safety_claim_revision_append_only
    BEFORE UPDATE OR DELETE ON safety_claim_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER safety_work_restriction_revision_append_only
    BEFORE UPDATE OR DELETE ON safety_work_restriction_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER safety_corrective_action_revision_append_only
    BEFORE UPDATE OR DELETE ON safety_corrective_action_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON safety_incident_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON safety_injury_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON safety_reportability_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON safety_claim_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON safety_work_restriction_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON safety_corrective_action_revision FROM PUBLIC;

ALTER TABLE safety_incident_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE safety_incident_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON safety_incident_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE safety_injury_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE safety_injury_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON safety_injury_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE safety_reportability_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE safety_reportability_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON safety_reportability_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE safety_claim_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE safety_claim_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON safety_claim_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE safety_work_restriction_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE safety_work_restriction_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON safety_work_restriction_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE safety_corrective_action_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE safety_corrective_action_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON safety_corrective_action_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON safety_incident_revision TO hcmnext_app;
GRANT SELECT, INSERT ON safety_injury_revision TO hcmnext_app;
GRANT SELECT, INSERT ON safety_reportability_revision TO hcmnext_app;
GRANT SELECT, INSERT ON safety_claim_revision TO hcmnext_app;
GRANT SELECT, INSERT ON safety_work_restriction_revision TO hcmnext_app;
GRANT SELECT, INSERT ON safety_corrective_action_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON safety_corrective_action_revision FROM hcmnext_app;
REVOKE ALL ON safety_work_restriction_revision FROM hcmnext_app;
REVOKE ALL ON safety_claim_revision FROM hcmnext_app;
REVOKE ALL ON safety_reportability_revision FROM hcmnext_app;
REVOKE ALL ON safety_injury_revision FROM hcmnext_app;
REVOKE ALL ON safety_incident_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON safety_corrective_action_revision;
ALTER TABLE safety_corrective_action_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE safety_corrective_action_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON safety_work_restriction_revision;
ALTER TABLE safety_work_restriction_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE safety_work_restriction_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON safety_claim_revision;
ALTER TABLE safety_claim_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE safety_claim_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON safety_reportability_revision;
ALTER TABLE safety_reportability_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE safety_reportability_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON safety_injury_revision;
ALTER TABLE safety_injury_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE safety_injury_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON safety_incident_revision;
ALTER TABLE safety_incident_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE safety_incident_revision DISABLE ROW LEVEL SECURITY;

DROP TABLE safety_corrective_action_revision;
DROP TABLE safety_work_restriction_revision;
DROP TABLE safety_claim_revision;
DROP TABLE safety_reportability_revision;
DROP TABLE safety_injury_revision;
DROP TABLE safety_incident_revision;
