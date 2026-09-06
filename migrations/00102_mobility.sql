-- Owner: data plane. Phase: PERSIST-MOBILITY-001.
-- Durable, descriptive cross-border mobility state. This migration does not
-- call providers and does not make immigration decisions.

-- +goose Up

CREATE TABLE IF NOT EXISTS mobility_plan (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    mobility_id      text           NOT NULL,
    revision         cas_version    NOT NULL,
    parent_revision  bigint,
    parent_digest    content_digest,
    worker_ref       uuid           NOT NULL,
    home_assignment  jsonb,
    host_assignment  jsonb,
    legs             jsonb,
    relocation       jsonb,
    immigration     jsonb,
    obligations     jsonb,
    status           text           NOT NULL,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT mobility_plan_revision_unique
        UNIQUE (tenant_id, mobility_id, revision),
    CONSTRAINT mobility_plan_revision_positive CHECK (revision >= 1),
    CONSTRAINT mobility_plan_parent_consistent CHECK (
        (revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
        OR
        (revision > 1 AND parent_revision IS NOT NULL AND parent_digest IS NOT NULL
            AND parent_revision >= 1 AND parent_revision < revision)
    ),
    CONSTRAINT mobility_plan_status_allowed CHECK (
        status IN ('READY', 'CONDITIONAL', 'BLOCKED', 'UNKNOWN')
    )
);

CREATE OR REPLACE TRIGGER mobility_plan_append_only
    BEFORE UPDATE OR DELETE ON mobility_plan
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON mobility_plan FROM PUBLIC;

CREATE TABLE IF NOT EXISTS mobility_immigration_milestone (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid           NOT NULL,
    milestone_id     text           NOT NULL,
    process_ref      text           NOT NULL,
    kind             text           NOT NULL,
    jurisdiction     text,
    at               date           NOT NULL,
    source_ref       text,
    evidence_ref     text,
    expires_on       date,
    canonical_digest content_digest NOT NULL,
    event_sequence   bigint         NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT mobility_immigration_milestone_sequence_unique
        UNIQUE (tenant_id, process_ref, event_sequence),
    CONSTRAINT mobility_immigration_milestone_sequence_positive
        CHECK (event_sequence >= 1),
    CONSTRAINT mobility_immigration_milestone_kind_allowed CHECK (
        kind IN ('PETITION', 'APPROVAL', 'EXPIRY', 'RE_VERIFICATION')
    ),
    CONSTRAINT mobility_immigration_milestone_expiry_consistent CHECK (
        expires_on IS NULL OR expires_on >= at
    )
);

CREATE INDEX IF NOT EXISTS mobility_plan_latest
    ON mobility_plan (tenant_id, mobility_id, revision DESC);

CREATE INDEX IF NOT EXISTS mobility_immigration_milestone_process
    ON mobility_immigration_milestone (tenant_id, process_ref, event_sequence);

CREATE OR REPLACE TRIGGER mobility_immigration_milestone_append_only
    BEFORE UPDATE OR DELETE ON mobility_immigration_milestone
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON mobility_immigration_milestone FROM PUBLIC;

ALTER TABLE mobility_plan ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobility_plan FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mobility_plan
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE mobility_immigration_milestone ENABLE ROW LEVEL SECURITY;
ALTER TABLE mobility_immigration_milestone FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mobility_immigration_milestone
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON mobility_plan TO hcmnext_app;
GRANT SELECT, INSERT ON mobility_immigration_milestone TO hcmnext_app;

-- +goose Down

REVOKE ALL ON mobility_immigration_milestone FROM hcmnext_app;
REVOKE ALL ON mobility_plan FROM hcmnext_app;

DROP POLICY tenant_isolation ON mobility_immigration_milestone;
ALTER TABLE mobility_immigration_milestone NO FORCE ROW LEVEL SECURITY;
ALTER TABLE mobility_immigration_milestone DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON mobility_plan;
ALTER TABLE mobility_plan NO FORCE ROW LEVEL SECURITY;
ALTER TABLE mobility_plan DISABLE ROW LEVEL SECURITY;

DROP TABLE mobility_immigration_milestone;
DROP TABLE mobility_plan;
