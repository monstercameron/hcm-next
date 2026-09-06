-- Owner: FX domain/data lane. Phase: 3.
-- PERSIST-FX-001: tenant-scoped immutable exchange-rate source, quote and
-- conversion-profile revision projections.
--
-- Storage disposition:
--   fx_rate_source_revision       AGGREGATE / PERMANENT / rebuild_source none
--   fx_quote_revision             AGGREGATE / PERMANENT / rebuild_source none
--   fx_conversion_profile_revision AGGREGATE / PERMANENT / rebuild_source none
--
-- The source identity is deliberately separate from its quote observations:
-- source revisions govern the vocabulary and authority of a provider, while
-- quotes accumulate independently against one exact source revision.

-- +goose Up

CREATE TABLE IF NOT EXISTS fx_rate_source_revision (
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid          NOT NULL,
    source_id       text          NOT NULL,
    revision        cas_version   NOT NULL,
    parent_revision cas_version,
    parent_digest   content_digest,
    effective_from  timestamptz,
    effective_to    timestamptz,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT fx_rate_source_revision_identity
        UNIQUE (tenant_id, source_id, revision),
    CONSTRAINT fx_rate_source_revision_parent_order
        CHECK (parent_revision IS NULL OR parent_revision < revision),
    CONSTRAINT fx_rate_source_revision_parent_digest_pair
        CHECK ((parent_revision IS NULL) = (parent_digest IS NULL)),
    CONSTRAINT fx_rate_source_revision_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE OR REPLACE TRIGGER fx_rate_source_revision_append_only
    BEFORE UPDATE OR DELETE ON fx_rate_source_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON fx_rate_source_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS fx_quote_revision (
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid          NOT NULL,
    quote_id        text          NOT NULL,
    source_id       text          NOT NULL,
    source_revision cas_version   NOT NULL,
    as_of           timestamptz,
    effective_at    timestamptz,
    observed_at     timestamptz,
    known_at        timestamptz,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT fx_quote_revision_identity
        UNIQUE (tenant_id, quote_id),
    CONSTRAINT fx_quote_revision_source_fk
        FOREIGN KEY (tenant_id, source_id, source_revision)
        REFERENCES fx_rate_source_revision (tenant_id, source_id, revision)
);

CREATE OR REPLACE TRIGGER fx_quote_revision_append_only
    BEFORE UPDATE OR DELETE ON fx_quote_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON fx_quote_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS fx_conversion_profile_revision (
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id          uuid          NOT NULL,
    profile_id      text          NOT NULL,
    revision        cas_version   NOT NULL,
    parent_revision cas_version,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT fx_conversion_profile_revision_identity
        UNIQUE (tenant_id, profile_id, revision),
    CONSTRAINT fx_conversion_profile_revision_parent_order
        CHECK (parent_revision IS NULL OR parent_revision < revision)
);

CREATE OR REPLACE TRIGGER fx_conversion_profile_revision_append_only
    BEFORE UPDATE OR DELETE ON fx_conversion_profile_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON fx_conversion_profile_revision FROM PUBLIC;

-- Tenant isolation (DB-017): a missing or blank app.tenant_id fails closed.
ALTER TABLE fx_rate_source_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE fx_rate_source_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fx_rate_source_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE fx_quote_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE fx_quote_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fx_quote_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE fx_conversion_profile_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE fx_conversion_profile_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fx_conversion_profile_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON fx_rate_source_revision TO hcmnext_app;
GRANT SELECT, INSERT ON fx_quote_revision TO hcmnext_app;
GRANT SELECT, INSERT ON fx_conversion_profile_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON fx_conversion_profile_revision FROM hcmnext_app;
REVOKE ALL ON fx_quote_revision FROM hcmnext_app;
REVOKE ALL ON fx_rate_source_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON fx_conversion_profile_revision;
ALTER TABLE fx_conversion_profile_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE fx_conversion_profile_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON fx_quote_revision;
ALTER TABLE fx_quote_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE fx_quote_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON fx_rate_source_revision;
ALTER TABLE fx_rate_source_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE fx_rate_source_revision DISABLE ROW LEVEL SECURITY;

DROP TRIGGER fx_conversion_profile_revision_append_only ON fx_conversion_profile_revision;
DROP TABLE fx_conversion_profile_revision;
DROP TRIGGER fx_quote_revision_append_only ON fx_quote_revision;
DROP TABLE fx_quote_revision;
DROP TRIGGER fx_rate_source_revision_append_only ON fx_rate_source_revision;
DROP TABLE fx_rate_source_revision;
