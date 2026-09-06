-- Owner: benefits data lane. Phase: 3.
-- PERSIST-BENEFITS-001: the tenant-scoped immutable benefit plan-year
-- revision catalogue. The logical plan and plan-year identities remain in the
-- kernel; this table is their durable, append-only revision chain.
--
-- Storage disposition: benefit_plan_revision is an AGGREGATE, permanently
-- retained, tenant-scoped row set owned by internal/data/benefitsstore. It has
-- no rebuild source: the revision chain is authoritative.

-- +goose Up

CREATE TABLE IF NOT EXISTS benefit_plan_revision (
    tenant_id                    tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id                       uuid          NOT NULL,
    plan_id                      uuid          NOT NULL,
    revision                     cas_version   NOT NULL,
    supersedes                   uuid,
    plan_year                    int           NOT NULL,
    name                         text          NOT NULL,
    carrier_ref                  uuid,
    provider_ref                 uuid,
    sponsor_ref                  uuid,
    jurisdiction                 text,
    currency                     text,
    coverage_tiers               jsonb,
    options                      jsonb,
    rate_schedule_ref            uuid,
    eligibility_rules_ref        uuid,
    enrollment_rules_ref         uuid,
    contribution_rules_ref       uuid,
    effective_from               timestamptz,
    effective_to                 timestamptz,
    authority_ref                uuid,
    canonical_digest             content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT benefit_plan_revision_identity
        UNIQUE (tenant_id, plan_id, revision),
    FOREIGN KEY (tenant_id, supersedes)
        REFERENCES benefit_plan_revision (tenant_id, row_id),
    CONSTRAINT benefit_plan_revision_year_valid
        CHECK (plan_year BETWEEN 1 AND 9999),
    CONSTRAINT benefit_plan_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_from < effective_to),
    CONSTRAINT benefit_plan_revision_coverage_array
        CHECK (coverage_tiers IS NULL OR jsonb_typeof(coverage_tiers) = 'array'),
    -- options is a JSON object containing the kernel option array and the
    -- interval/reference envelope needed to rehydrate the pure value exactly.
    CONSTRAINT benefit_plan_revision_options_object
        CHECK (options IS NULL OR jsonb_typeof(options) = 'object')
);

-- The FK above proves that supersedes is in this tenant, but row_id is a
-- surrogate and therefore cannot express that it is also in this plan. Keep
-- that business invariant at the storage boundary as well as in the adapter.
-- +goose StatementBegin
CREATE FUNCTION benefit_plan_revision_same_plan() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    prior_plan uuid;
BEGIN
    IF NEW.supersedes IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT plan_id INTO prior_plan
      FROM benefit_plan_revision
     WHERE tenant_id = NEW.tenant_id AND row_id = NEW.supersedes;
    IF prior_plan IS DISTINCT FROM NEW.plan_id THEN
        RAISE EXCEPTION
            'benefit_plan_revision % supersedes a revision from another plan', NEW.row_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER benefit_plan_revision_lineage
    BEFORE INSERT ON benefit_plan_revision
    FOR EACH ROW EXECUTE FUNCTION benefit_plan_revision_same_plan();

-- Revisions are immutable facts. A correction is a new row whose supersedes
-- value points at the previous row; no UPDATE or DELETE path is granted.
CREATE OR REPLACE TRIGGER benefit_plan_revision_append_only
    BEFORE UPDATE OR DELETE ON benefit_plan_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON benefit_plan_revision FROM PUBLIC;

ALTER TABLE benefit_plan_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE benefit_plan_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON benefit_plan_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON benefit_plan_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON benefit_plan_revision FROM hcmnext_app;
DROP POLICY tenant_isolation ON benefit_plan_revision;
ALTER TABLE benefit_plan_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE benefit_plan_revision DISABLE ROW LEVEL SECURITY;
DROP TRIGGER benefit_plan_revision_append_only ON benefit_plan_revision;
DROP TRIGGER benefit_plan_revision_lineage ON benefit_plan_revision;
DROP FUNCTION benefit_plan_revision_same_plan();
DROP TABLE benefit_plan_revision;
