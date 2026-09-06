-- Owner: location domain lane. Phase: 3.
-- PERSIST-LOCATION-001: immutable work-location and worksite revisions plus
-- versioned platform jurisdiction reference data.

-- +goose Up

CREATE TABLE IF NOT EXISTS work_location_revision (
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid          NOT NULL,
    location_id     text          NOT NULL,
    revision        cas_version   NOT NULL,
    parent_revision cas_version,
    parent_digest   content_digest,
    address         jsonb         NOT NULL,
    source_authority text,
    confidence      text,
    effective_from  timestamptz,
    effective_to    timestamptz,
    known_at        timestamptz   NOT NULL,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT work_location_revision_identity
        UNIQUE (tenant_id, location_id, revision),
    CONSTRAINT work_location_revision_address_object
        CHECK (jsonb_typeof(address) = 'object'),
    CONSTRAINT work_location_revision_parent_order
        CHECK (parent_revision IS NULL OR parent_revision < revision),
    CONSTRAINT work_location_revision_parent_digest_pair
        CHECK ((parent_revision IS NULL) = (parent_digest IS NULL)),
    CONSTRAINT work_location_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE OR REPLACE TRIGGER work_location_revision_append_only
    BEFORE UPDATE OR DELETE ON work_location_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON work_location_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS worksite_revision (
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid          NOT NULL,
    worksite_id     text          NOT NULL,
    revision        cas_version   NOT NULL,
    parent_revision cas_version,
    work_location_ref text         NOT NULL,
    name            text          NOT NULL,
    address         jsonb,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT worksite_revision_identity
        UNIQUE (tenant_id, worksite_id, revision),
    CONSTRAINT worksite_revision_parent_order
        CHECK (parent_revision IS NULL OR parent_revision < revision),
    CONSTRAINT worksite_revision_address_object
        CHECK (address IS NULL OR jsonb_typeof(address) = 'object')
);

CREATE OR REPLACE TRIGGER worksite_revision_append_only
    BEFORE UPDATE OR DELETE ON worksite_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON worksite_revision FROM PUBLIC;

-- Jurisdiction data is platform reference data. It is versioned by identity,
-- has no tenant column and is readable by the application role, but is not
-- writable by that role.
CREATE TABLE IF NOT EXISTS jurisdiction_table (
    row_id           uuid          PRIMARY KEY,
    table_id         text          NOT NULL,
    version          text          NOT NULL,
    rules            jsonb         NOT NULL,
    canonical_digest content_digest NOT NULL,

    CONSTRAINT jurisdiction_table_identity UNIQUE (table_id, version),
    CONSTRAINT jurisdiction_table_rules_array
        CHECK (jsonb_typeof(rules) = 'array')
);

REVOKE INSERT, UPDATE, DELETE ON jurisdiction_table FROM PUBLIC;
GRANT SELECT ON jurisdiction_table TO hcmnext_app;
GRANT SELECT, INSERT ON work_location_revision, worksite_revision TO hcmnext_app;

-- Tenant isolation (DB-017): missing or blank app.tenant_id fails closed.
ALTER TABLE work_location_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE work_location_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON work_location_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE worksite_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE worksite_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worksite_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- +goose Down

REVOKE ALL ON jurisdiction_table FROM hcmnext_app;
REVOKE ALL ON worksite_revision FROM hcmnext_app;
REVOKE ALL ON work_location_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON worksite_revision;
ALTER TABLE worksite_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE worksite_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON work_location_revision;
ALTER TABLE work_location_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE work_location_revision DISABLE ROW LEVEL SECURITY;

DROP TRIGGER worksite_revision_append_only ON worksite_revision;
DROP TABLE worksite_revision;
DROP TRIGGER work_location_revision_append_only ON work_location_revision;
DROP TABLE work_location_revision;
DROP TABLE jurisdiction_table;
