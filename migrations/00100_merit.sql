-- Owner: merit persistence lane. Phase: Gate A.
-- PERSIST-MERIT-001: durable merit-cycle, recommendation and frozen
-- population-snapshot revisions.
--
-- Storage disposition (STORE-001):
--   merit_population_snapshot AGGREGATE, PERMANENT, tenant_id, immutable,
--                              rebuild_source none.
--   merit_cycle_revision      AGGREGATE, PERMANENT, tenant_id, immutable,
--                              rebuild_source none.
--   merit_recommendation      AGGREGATE, PERMANENT, tenant_id, immutable,
--                              rebuild_source none.

-- +goose Up

CREATE TABLE IF NOT EXISTS merit_population_snapshot (
    tenant_id        tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid          NOT NULL,
    snapshot_id      semantic_key  NOT NULL,
    revision         cas_version   NOT NULL,
    members          jsonb         NOT NULL,
    watermark        text,
    frozen           boolean       NOT NULL DEFAULT false,
    frozen_at        timestamptz,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT merit_population_snapshot_revision_unique
        UNIQUE (tenant_id, snapshot_id, revision),
    CONSTRAINT merit_population_snapshot_frozen_at_check CHECK (
        (frozen = false AND frozen_at IS NULL) OR
        (frozen = true AND frozen_at IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS merit_cycle_revision (
    tenant_id        tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid          NOT NULL,
    cycle_id         semantic_key  NOT NULL,
    revision         cas_version   NOT NULL,
    parent_revision  cas_version,
    parent_digest    content_digest,
    population_ref   uuid          NOT NULL,
    guidelines       jsonb         NOT NULL,
    budget           numeric(19,4) NOT NULL,
    state            text          NOT NULL,
    effective_at     timestamptz,
    known_at         timestamptz,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT merit_cycle_revision_unique
        UNIQUE (tenant_id, cycle_id, revision),
    CONSTRAINT merit_cycle_population_fk
        FOREIGN KEY (tenant_id, population_ref)
        REFERENCES merit_population_snapshot (tenant_id, row_id),
    CONSTRAINT merit_cycle_state_allowed CHECK (
        state IN ('DRAFT', 'OPEN', 'CALIBRATING', 'APPROVED', 'FINALIZED')
    )
);

CREATE TABLE IF NOT EXISTS merit_recommendation (
    tenant_id        tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid          NOT NULL,
    cycle_id         semantic_key  NOT NULL,
    cycle_revision   cas_version   NOT NULL,
    participant_id   semantic_key  NOT NULL,
    base_pay         numeric(19,4),
    state            text          NOT NULL,
    adjustments      jsonb,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT merit_recommendation_unique
        UNIQUE (tenant_id, cycle_id, participant_id, cycle_revision),
    CONSTRAINT merit_recommendation_cycle_fk
        FOREIGN KEY (tenant_id, cycle_id, cycle_revision)
        REFERENCES merit_cycle_revision (tenant_id, cycle_id, revision),
    CONSTRAINT merit_recommendation_state_allowed CHECK (
        state IN ('PROPOSED', 'ADJUSTED', 'APPROVED', 'FINALIZED')
    )
);

CREATE OR REPLACE TRIGGER merit_population_snapshot_append_only
    BEFORE UPDATE OR DELETE ON merit_population_snapshot
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE OR REPLACE TRIGGER merit_cycle_revision_append_only
    BEFORE UPDATE OR DELETE ON merit_cycle_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE OR REPLACE TRIGGER merit_recommendation_append_only
    BEFORE UPDATE OR DELETE ON merit_recommendation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE merit_population_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE merit_population_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON merit_population_snapshot
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE merit_cycle_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE merit_cycle_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON merit_cycle_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE merit_recommendation ENABLE ROW LEVEL SECURITY;
ALTER TABLE merit_recommendation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON merit_recommendation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Revisions are inserted by the app role and never rewritten or removed. The
-- write adapter enforces the revision/CAS chain; privileges make a raw app-role
-- UPDATE or DELETE fail even when it bypasses that adapter.
REVOKE UPDATE, DELETE ON merit_population_snapshot FROM PUBLIC;
REVOKE UPDATE, DELETE ON merit_cycle_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON merit_recommendation FROM PUBLIC;

GRANT SELECT, INSERT ON merit_population_snapshot TO hcmnext_app;
GRANT SELECT, INSERT ON merit_cycle_revision TO hcmnext_app;
GRANT SELECT, INSERT ON merit_recommendation TO hcmnext_app;

-- +goose Down

REVOKE ALL ON merit_recommendation FROM hcmnext_app;
REVOKE ALL ON merit_cycle_revision FROM hcmnext_app;
REVOKE ALL ON merit_population_snapshot FROM hcmnext_app;

DROP TRIGGER merit_recommendation_append_only ON merit_recommendation;
DROP TRIGGER merit_cycle_revision_append_only ON merit_cycle_revision;
DROP TRIGGER merit_population_snapshot_append_only ON merit_population_snapshot;

DROP POLICY tenant_isolation ON merit_recommendation;
ALTER TABLE merit_recommendation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE merit_recommendation DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON merit_cycle_revision;
ALTER TABLE merit_cycle_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE merit_cycle_revision DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON merit_population_snapshot;
ALTER TABLE merit_population_snapshot NO FORCE ROW LEVEL SECURITY;
ALTER TABLE merit_population_snapshot DISABLE ROW LEVEL SECURITY;

DROP TABLE merit_recommendation;
DROP TABLE merit_cycle_revision;
DROP TABLE merit_population_snapshot;
