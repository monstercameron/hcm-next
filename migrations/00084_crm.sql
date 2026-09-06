-- Owner: CRM data lane. Phase: P3.
-- PERSIST-CRM-001: tenant-isolated talent-pool and pool-membership revisions.
--
-- Storage disposition (STORE-001): both tables are permanent, immutable
-- aggregate revisions owned by internal/data/crmstore. They are tenant scoped
-- by tenant_id and have no rebuild source.

-- +goose Up

CREATE TABLE IF NOT EXISTS talent_pool_revision (
    row_id         uuid          NOT NULL,
    tenant_id      tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    pool_id        uuid          NOT NULL,
    revision       cas_version   NOT NULL,
    purpose        text          NOT NULL,
    criteria       jsonb,
    source         jsonb         NOT NULL,
    consent        jsonb         NOT NULL,
    scope          jsonb         NOT NULL,
    effective_from timestamptz,
    effective_to   timestamptz,
    owner_ref      uuid,
    removal_policy jsonb,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, pool_id, revision),
    CONSTRAINT talent_pool_revision_criteria_object
        CHECK (criteria IS NULL OR jsonb_typeof(criteria) = 'object'),
    CONSTRAINT talent_pool_revision_source_object
        CHECK (jsonb_typeof(source) = 'object'),
    CONSTRAINT talent_pool_revision_consent_object
        CHECK (jsonb_typeof(consent) = 'object'),
    CONSTRAINT talent_pool_revision_scope_object
        CHECK (jsonb_typeof(scope) = 'object'),
    CONSTRAINT talent_pool_revision_removal_policy_object
        CHECK (removal_policy IS NULL OR jsonb_typeof(removal_policy) = 'object'),
    CONSTRAINT talent_pool_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE TABLE IF NOT EXISTS talent_pool_membership_revision (
    row_id         uuid          NOT NULL,
    tenant_id      tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    membership_id  uuid          NOT NULL,
    revision       cas_version   NOT NULL,
    pool_ref       uuid          NOT NULL,
    subject_ref    uuid          NOT NULL,
    role           text          NOT NULL,
    purpose        text,
    source         jsonb,
    consent        jsonb         NOT NULL,
    scope          jsonb,
    effective_from timestamptz,
    effective_to   timestamptz,
    owner_ref      uuid,
    removal_policy jsonb,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, membership_id, revision),
    CONSTRAINT talent_pool_membership_revision_source_object
        CHECK (source IS NULL OR jsonb_typeof(source) = 'object'),
    CONSTRAINT talent_pool_membership_revision_consent_object
        CHECK (jsonb_typeof(consent) = 'object'),
    CONSTRAINT talent_pool_membership_revision_scope_object
        CHECK (scope IS NULL OR jsonb_typeof(scope) = 'object'),
    CONSTRAINT talent_pool_membership_revision_removal_policy_object
        CHECK (removal_policy IS NULL OR jsonb_typeof(removal_policy) = 'object'),
    CONSTRAINT talent_pool_membership_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

-- A successor is a new row whose revision carries the CAS lineage. The pool
-- reference is deliberately checked by the adapter: pool_id is the natural
-- identity and is not the table's surrogate row_id alone.
CREATE OR REPLACE TRIGGER talent_pool_revision_append_only
    BEFORE UPDATE OR DELETE ON talent_pool_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER talent_pool_membership_revision_append_only
    BEFORE UPDATE OR DELETE ON talent_pool_membership_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON talent_pool_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON talent_pool_membership_revision FROM PUBLIC;

ALTER TABLE talent_pool_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE talent_pool_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON talent_pool_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE talent_pool_membership_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE talent_pool_membership_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON talent_pool_membership_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON talent_pool_revision TO hcmnext_app;
GRANT SELECT, INSERT ON talent_pool_membership_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON talent_pool_membership_revision FROM hcmnext_app;
REVOKE ALL ON talent_pool_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON talent_pool_membership_revision;
ALTER TABLE talent_pool_membership_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE talent_pool_membership_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON talent_pool_revision;
ALTER TABLE talent_pool_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE talent_pool_revision DISABLE ROW LEVEL SECURITY;

DROP TRIGGER talent_pool_membership_revision_append_only ON talent_pool_membership_revision;
DROP TABLE talent_pool_membership_revision;
DROP TRIGGER talent_pool_revision_append_only ON talent_pool_revision;
DROP TABLE talent_pool_revision;
