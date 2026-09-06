-- Owner: employee-relations data adapter. Phase: P1A.
-- PERSIST-EMPLOYEERELATIONS-001: the six employee-relations revision
-- families are immutable, tenant-scoped evidence. Each table repeats the
-- lineage envelope so the adapter can use one persistence contract without
-- moving any policy or evaluation into PostgreSQL.

-- +goose Up

CREATE TABLE er_allegation_revision (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid         NOT NULL,
    case_ref              uuid         NOT NULL,
    compartment_ref       uuid         NOT NULL,
    allegation_id         uuid         NOT NULL,
    revision              cas_version  NOT NULL,
    parent_revision       cas_version,
    parent_digest         content_digest,
    participants          jsonb        NOT NULL,
    reporter_ref          uuid         NOT NULL,
    subject_ref           uuid         NOT NULL,
    summary               text         NOT NULL,
    status                text         NOT NULL,
    retaliation_safeguard boolean      NOT NULL DEFAULT true,
    canonical_digest      content_digest NOT NULL,
    recorded_at           timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, allegation_id, revision)
);

CREATE TABLE er_investigation_revision (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid         NOT NULL,
    case_ref              uuid         NOT NULL,
    compartment_ref       uuid         NOT NULL,
    investigation_id      uuid         NOT NULL,
    revision              cas_version  NOT NULL,
    parent_revision       cas_version,
    parent_digest         content_digest,
    participants          jsonb        NOT NULL,
    allegation_ref        uuid         NOT NULL,
    investigator_ref      uuid         NOT NULL,
    authority_ref         uuid         NOT NULL,
    purpose               text,
    scope                 text,
    status                text         NOT NULL,
    canonical_digest      content_digest NOT NULL,
    recorded_at           timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, investigation_id, revision)
);

CREATE TABLE er_interview_revision (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid         NOT NULL,
    case_ref              uuid         NOT NULL,
    compartment_ref       uuid         NOT NULL,
    interview_id          uuid         NOT NULL,
    revision              cas_version  NOT NULL,
    parent_revision       cas_version,
    parent_digest         content_digest,
    participants          jsonb        NOT NULL,
    investigation_ref     uuid         NOT NULL,
    interviewer_ref       uuid         NOT NULL,
    interviewee_ref       uuid         NOT NULL,
    statement             text,
    status                text         NOT NULL,
    canonical_digest      content_digest NOT NULL,
    recorded_at           timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, interview_id, revision)
);

CREATE TABLE er_finding_revision (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid         NOT NULL,
    case_ref              uuid         NOT NULL,
    compartment_ref       uuid         NOT NULL,
    finding_id            uuid         NOT NULL,
    revision              cas_version  NOT NULL,
    parent_revision       cas_version,
    parent_digest         content_digest,
    participants          jsonb        NOT NULL,
    investigation_ref     uuid         NOT NULL,
    investigator_ref      uuid         NOT NULL,
    subject_ref           uuid         NOT NULL,
    reporter_ref          uuid         NOT NULL,
    evidence_standard     text,
    evidence_refs         jsonb,
    disposition            text         NOT NULL,
    rationale              text,
    canonical_digest      content_digest NOT NULL,
    recorded_at           timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, finding_id, revision)
);

CREATE TABLE er_discipline_revision (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid         NOT NULL,
    case_ref              uuid         NOT NULL,
    compartment_ref       uuid         NOT NULL,
    discipline_id         uuid         NOT NULL,
    revision              cas_version  NOT NULL,
    parent_revision       cas_version,
    parent_digest         content_digest,
    participants          jsonb        NOT NULL,
    finding_ref           uuid         NOT NULL,
    subject_ref           uuid         NOT NULL,
    action                text         NOT NULL,
    legal_review_ref      uuid         NOT NULL,
    representation_review_ref uuid     NOT NULL,
    status                text         NOT NULL,
    canonical_digest      content_digest NOT NULL,
    recorded_at           timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, discipline_id, revision)
);

CREATE TABLE er_grievance_revision (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid         NOT NULL,
    case_ref              uuid         NOT NULL,
    compartment_ref       uuid         NOT NULL,
    grievance_id          uuid         NOT NULL,
    revision              cas_version  NOT NULL,
    parent_revision       cas_version,
    parent_digest         content_digest,
    participants          jsonb        NOT NULL,
    decision_ref           uuid         NOT NULL,
    grievant_ref           uuid         NOT NULL,
    grounds                text,
    outcome                text,
    status                 text         NOT NULL,
    canonical_digest       content_digest NOT NULL,
    recorded_at            timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, case_ref, grievance_id, revision)
);

CREATE INDEX er_allegation_revision_lookup
    ON er_allegation_revision (tenant_id, allegation_id, revision);
CREATE INDEX er_investigation_revision_lookup
    ON er_investigation_revision (tenant_id, investigation_id, revision);
CREATE INDEX er_interview_revision_lookup
    ON er_interview_revision (tenant_id, interview_id, revision);
CREATE INDEX er_finding_revision_lookup
    ON er_finding_revision (tenant_id, finding_id, revision);
CREATE INDEX er_discipline_revision_lookup
    ON er_discipline_revision (tenant_id, discipline_id, revision);
CREATE INDEX er_grievance_revision_lookup
    ON er_grievance_revision (tenant_id, grievance_id, revision);

CREATE OR REPLACE TRIGGER er_allegation_revision_append_only
    BEFORE UPDATE OR DELETE ON er_allegation_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER er_investigation_revision_append_only
    BEFORE UPDATE OR DELETE ON er_investigation_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER er_interview_revision_append_only
    BEFORE UPDATE OR DELETE ON er_interview_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER er_finding_revision_append_only
    BEFORE UPDATE OR DELETE ON er_finding_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER er_discipline_revision_append_only
    BEFORE UPDATE OR DELETE ON er_discipline_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER er_grievance_revision_append_only
    BEFORE UPDATE OR DELETE ON er_grievance_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON
    er_allegation_revision, er_investigation_revision, er_interview_revision,
    er_finding_revision, er_discipline_revision, er_grievance_revision
FROM PUBLIC;

GRANT SELECT, INSERT ON
    er_allegation_revision, er_investigation_revision, er_interview_revision,
    er_finding_revision, er_discipline_revision, er_grievance_revision
TO hcmnext_app;

-- Tenant isolation is deliberately the only database visibility rule here.
-- Participant/compartment authorization remains a governed follow-up policy;
-- the migration must not infer it from a JSONB value.
ALTER TABLE er_allegation_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE er_allegation_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON er_allegation_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE er_investigation_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE er_investigation_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON er_investigation_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE er_interview_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE er_interview_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON er_interview_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE er_finding_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE er_finding_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON er_finding_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE er_discipline_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE er_discipline_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON er_discipline_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE er_grievance_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE er_grievance_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON er_grievance_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- +goose Down

REVOKE ALL ON
    er_allegation_revision, er_investigation_revision, er_interview_revision,
    er_finding_revision, er_discipline_revision, er_grievance_revision
FROM hcmnext_app;

DROP POLICY tenant_isolation ON er_grievance_revision;
ALTER TABLE er_grievance_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE er_grievance_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON er_discipline_revision;
ALTER TABLE er_discipline_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE er_discipline_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON er_finding_revision;
ALTER TABLE er_finding_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE er_finding_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON er_interview_revision;
ALTER TABLE er_interview_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE er_interview_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON er_investigation_revision;
ALTER TABLE er_investigation_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE er_investigation_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON er_allegation_revision;
ALTER TABLE er_allegation_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE er_allegation_revision DISABLE ROW LEVEL SECURITY;

DROP TABLE er_grievance_revision;
DROP TABLE er_discipline_revision;
DROP TABLE er_finding_revision;
DROP TABLE er_interview_revision;
DROP TABLE er_investigation_revision;
DROP TABLE er_allegation_revision;
