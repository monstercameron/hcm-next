-- Owner: content platform/data plane. Phase: P1B.
-- PERSIST-CONTENTREGISTRY-001: durable industry-pack and knowledge content
-- registries. The two platform catalogs intentionally have no tenant_id: they
-- are governed reference data, like jurisdiction in migration 00021. The
-- remaining five tables are tenant-scoped and fail closed through app.tenant_id.

-- +goose Up

CREATE TABLE IF NOT EXISTS industry_pack_manifest (
    row_id         uuid PRIMARY KEY,
    pack_id        text NOT NULL,
    version        integer NOT NULL,
    parent_version integer,
    parent_digest  content_digest,
    canonical_digest content_digest NOT NULL,
    CONSTRAINT industry_pack_manifest_version_positive CHECK (version > 0),
    CONSTRAINT industry_pack_manifest_parent_positive CHECK (parent_version IS NULL OR parent_version > 0),
    CONSTRAINT industry_pack_manifest_not_self_parent CHECK (parent_version IS NULL OR parent_version < version),
    CONSTRAINT industry_pack_manifest_unique UNIQUE (pack_id, version)
);

CREATE TABLE IF NOT EXISTS content_registry_entry (
    row_id        uuid PRIMARY KEY,
    ref           text NOT NULL,
    effective_from timestamptz,
    effective_to   timestamptz,
    digest         content_digest NOT NULL,
    dependencies  jsonb,
    CONSTRAINT content_registry_entry_ref_unique UNIQUE (ref),
    CONSTRAINT content_registry_entry_effective_window CHECK (
        effective_to IS NULL OR effective_from IS NULL OR effective_from < effective_to
    ),
    CONSTRAINT content_registry_entry_dependencies_array CHECK (
        dependencies IS NULL OR jsonb_typeof(dependencies) = 'array'
    )
);

-- Platform catalog rows are immutable and the application role is read-only.
CREATE OR REPLACE TRIGGER industry_pack_manifest_append_only
    BEFORE UPDATE OR DELETE ON industry_pack_manifest
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER content_registry_entry_append_only
    BEFORE UPDATE OR DELETE ON content_registry_entry
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE IF NOT EXISTS industry_pack_binding (
    row_id          uuid NOT NULL,
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    packs           jsonb NOT NULL,
    contents        jsonb NOT NULL,
    canonical_digest content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT industry_pack_binding_packs_array CHECK (jsonb_typeof(packs) = 'array'),
    CONSTRAINT industry_pack_binding_contents_array CHECK (jsonb_typeof(contents) = 'array')
);

CREATE TABLE IF NOT EXISTS knowledge_article_revision (
    row_id         uuid NOT NULL,
    tenant_id      tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    article_id     text NOT NULL,
    revision       bigint NOT NULL,
    locale         text NOT NULL,
    audience_scope jsonb,
    classification text NOT NULL,
    owner          text,
    source_authority text,
    source_refs    jsonb,
    jurisdiction   text,
    effective_from timestamptz,
    effective_to   timestamptz,
    known_from     timestamptz,
    known_to       timestamptz,
    body_digest    content_digest NOT NULL,
    title          text NOT NULL,
    summary        text,
    review         jsonb,
    supersession   jsonb,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT knowledge_article_revision_identity UNIQUE (tenant_id, article_id, revision),
    CONSTRAINT knowledge_article_revision_revision_positive CHECK (revision > 0),
    CONSTRAINT knowledge_article_revision_effective_window CHECK (
        effective_to IS NULL OR effective_from IS NULL OR effective_from <= effective_to
    ),
    CONSTRAINT knowledge_article_revision_known_window CHECK (
        known_to IS NULL OR known_from IS NULL OR known_from <= known_to
    ),
    CONSTRAINT knowledge_article_revision_source_refs_array CHECK (
        source_refs IS NULL OR jsonb_typeof(source_refs) = 'array'
    )
);

CREATE TABLE IF NOT EXISTS knowledge_localized_revision (
    row_id                 uuid NOT NULL,
    tenant_id              tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    article_id             text NOT NULL,
    revision               bigint NOT NULL,
    locale                 text NOT NULL,
    reviewer               text,
    source_revision_digest content_digest,
    body_digest            content_digest NOT NULL,
    title                  text NOT NULL,
    summary                text,
    created_at             timestamptz NOT NULL,
    digest                 content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT knowledge_localized_revision_identity UNIQUE (tenant_id, article_id, locale, revision),
    CONSTRAINT knowledge_localized_revision_revision_positive CHECK (revision > 0)
);

CREATE TABLE IF NOT EXISTS knowledge_lifecycle_event (
    row_id           uuid NOT NULL,
    tenant_id        tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    event_id         text NOT NULL,
    kind             text NOT NULL,
    previous_state   text,
    state            text NOT NULL,
    article_id       text NOT NULL,
    revision         bigint,
    locale           text,
    reviewer         text,
    bundle_id        text,
    bundle_digest    content_digest,
    receipt_digest   content_digest,
    activation_epoch bigint,
    at               timestamptz NOT NULL,
    digest           content_digest NOT NULL,
    event_sequence   bigint NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT knowledge_lifecycle_event_identity UNIQUE (tenant_id, event_id),
    CONSTRAINT knowledge_lifecycle_event_sequence_unique UNIQUE (tenant_id, article_id, event_sequence),
    CONSTRAINT knowledge_lifecycle_event_sequence_positive CHECK (event_sequence > 0),
    CONSTRAINT knowledge_lifecycle_event_revision_positive CHECK (revision IS NULL OR revision > 0),
    CONSTRAINT knowledge_lifecycle_event_epoch_positive CHECK (activation_epoch IS NULL OR activation_epoch > 0)
);

CREATE OR REPLACE TRIGGER knowledge_lifecycle_event_append_only
    BEFORE UPDATE OR DELETE ON knowledge_lifecycle_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE IF NOT EXISTS knowledge_activation (
    row_id           uuid NOT NULL,
    tenant_id        tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    article_id       text NOT NULL,
    revision         bigint NOT NULL,
    locale           text NOT NULL,
    bundle_id        text,
    bundle_digest    content_digest,
    activation_epoch bigint NOT NULL,
    activated_at     timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT knowledge_activation_identity UNIQUE (tenant_id, article_id, locale),
    CONSTRAINT knowledge_activation_revision_positive CHECK (revision > 0),
    CONSTRAINT knowledge_activation_epoch_positive CHECK (activation_epoch > 0)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION knowledge_activation_epoch_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.activation_epoch < OLD.activation_epoch THEN
        RAISE EXCEPTION 'knowledge activation epoch cannot move backward';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER knowledge_activation_epoch_monotonic
    BEFORE UPDATE ON knowledge_activation
    FOR EACH ROW EXECUTE FUNCTION knowledge_activation_epoch_guard();

-- Tenant isolation is deliberately repeated for every tenant-scoped table so
-- adding a new table cannot accidentally inherit a permissive policy.
ALTER TABLE industry_pack_binding ENABLE ROW LEVEL SECURITY;
ALTER TABLE industry_pack_binding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON industry_pack_binding
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE knowledge_article_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_article_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON knowledge_article_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE knowledge_localized_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_localized_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON knowledge_localized_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE knowledge_lifecycle_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_lifecycle_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON knowledge_lifecycle_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE knowledge_activation ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_activation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON knowledge_activation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT ON industry_pack_manifest, content_registry_entry TO hcmnext_app;
GRANT SELECT, INSERT ON industry_pack_binding, knowledge_article_revision,
    knowledge_localized_revision, knowledge_lifecycle_event TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON knowledge_activation TO hcmnext_app;
REVOKE UPDATE, DELETE ON industry_pack_binding, knowledge_article_revision,
    knowledge_localized_revision, knowledge_lifecycle_event FROM PUBLIC;
REVOKE DELETE ON knowledge_activation FROM PUBLIC;

-- +goose Down

REVOKE ALL ON knowledge_activation FROM hcmnext_app;
REVOKE ALL ON knowledge_lifecycle_event, knowledge_localized_revision,
    knowledge_article_revision, industry_pack_binding FROM hcmnext_app;
REVOKE ALL ON content_registry_entry, industry_pack_manifest FROM hcmnext_app;

DROP TRIGGER knowledge_activation_epoch_monotonic ON knowledge_activation;
DROP FUNCTION knowledge_activation_epoch_guard();

DROP POLICY tenant_isolation ON knowledge_activation;
ALTER TABLE knowledge_activation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE knowledge_activation DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON knowledge_lifecycle_event;
ALTER TABLE knowledge_lifecycle_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE knowledge_lifecycle_event DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON knowledge_localized_revision;
ALTER TABLE knowledge_localized_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE knowledge_localized_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON knowledge_article_revision;
ALTER TABLE knowledge_article_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE knowledge_article_revision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON industry_pack_binding;
ALTER TABLE industry_pack_binding NO FORCE ROW LEVEL SECURITY;
ALTER TABLE industry_pack_binding DISABLE ROW LEVEL SECURITY;

DROP TABLE knowledge_activation;
DROP TABLE knowledge_lifecycle_event;
DROP TABLE knowledge_localized_revision;
DROP TABLE knowledge_article_revision;
DROP TABLE industry_pack_binding;
DROP TABLE content_registry_entry;
DROP TABLE industry_pack_manifest;
