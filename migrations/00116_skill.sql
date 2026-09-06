-- Owner: skill data lane. Phase: P3.
-- PERSIST-SKILL-001: tenant-scoped skill ontology revisions, skill-definition
-- revisions and append-only worker-skill evidence.
--
-- Storage disposition: skill_ontology_revision and skill_definition_revision
-- are permanent AGGREGATE revisions owned by internal/data/skillstore and
-- rebuild from no other source. worker_skill_evidence is a permanent LEDGER,
-- append-only table owned by internal/data/skillstore and has no rebuild source.

-- +goose Up

CREATE TABLE IF NOT EXISTS skill_ontology_revision (
    tenant_id        tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid          NOT NULL,
    ontology_id      text          NOT NULL,
    revision         cas_version   NOT NULL,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, ontology_id, revision)
);

CREATE TABLE IF NOT EXISTS skill_definition_revision (
    tenant_id        tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid          NOT NULL,
    skill_ref        text          NOT NULL,
    ontology_ref     text          NOT NULL,
    revision         cas_version   NOT NULL,
    supersedes       bigint,
    name             text          NOT NULL,
    parent_refs      jsonb,
    aliases          jsonb,
    proficiency_scale jsonb,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, skill_ref, revision),
    CONSTRAINT skill_definition_revision_parent_order
        CHECK (supersedes IS NULL OR supersedes < revision),
    CONSTRAINT skill_definition_revision_parent_refs_array
        CHECK (parent_refs IS NULL OR jsonb_typeof(parent_refs) = 'array'),
    CONSTRAINT skill_definition_revision_aliases_array
        CHECK (aliases IS NULL OR jsonb_typeof(aliases) = 'array'),
    CONSTRAINT skill_definition_revision_scale_array
        CHECK (proficiency_scale IS NULL OR jsonb_typeof(proficiency_scale) = 'array')
);

CREATE TABLE IF NOT EXISTS worker_skill_evidence (
    tenant_id        tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid          NOT NULL,
    evidence_id      text          NOT NULL,
    worker_ref       uuid          NOT NULL,
    skill_ref        text          NOT NULL,
    level            text,
    proficiency      numeric(5,2),
    evidence_kind    text          NOT NULL,
    evidence_ref     text,
    verified         boolean       NOT NULL DEFAULT false,
    disputed         boolean       NOT NULL DEFAULT false,
    effective_from   timestamptz,
    effective_to     timestamptz,
    canonical_digest content_digest NOT NULL,
    event_sequence   bigint        NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, worker_ref, skill_ref, event_sequence),
    CONSTRAINT worker_skill_evidence_sequence_positive CHECK (event_sequence >= 1),
    CONSTRAINT worker_skill_evidence_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

-- All three records are immutable. Worker evidence is additionally the
-- append-only event log named by the todo; revisions remain immutable because
-- a successor is represented by a new row, never by an in-place rewrite.
CREATE OR REPLACE TRIGGER skill_ontology_revision_append_only
    BEFORE UPDATE OR DELETE ON skill_ontology_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER skill_definition_revision_append_only
    BEFORE UPDATE OR DELETE ON skill_definition_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER worker_skill_evidence_append_only
    BEFORE UPDATE OR DELETE ON worker_skill_evidence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON skill_ontology_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON skill_definition_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON worker_skill_evidence FROM PUBLIC;

ALTER TABLE skill_ontology_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_ontology_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON skill_ontology_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE skill_definition_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_definition_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON skill_definition_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE worker_skill_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_skill_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worker_skill_evidence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON
    skill_ontology_revision,
    skill_definition_revision,
    worker_skill_evidence
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON worker_skill_evidence FROM hcmnext_app;
REVOKE ALL ON skill_definition_revision FROM hcmnext_app;
REVOKE ALL ON skill_ontology_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON worker_skill_evidence;
ALTER TABLE worker_skill_evidence NO FORCE ROW LEVEL SECURITY;
ALTER TABLE worker_skill_evidence DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON skill_definition_revision;
ALTER TABLE skill_definition_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE skill_definition_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON skill_ontology_revision;
ALTER TABLE skill_ontology_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE skill_ontology_revision DISABLE ROW LEVEL SECURITY;

DROP TABLE worker_skill_evidence;
DROP TABLE skill_definition_revision;
DROP TABLE skill_ontology_revision;
