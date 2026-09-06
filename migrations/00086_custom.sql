-- Owner: custom domain and data planes. Phase: P4.
-- PERSIST-CUSTOM-001: tenant-scoped custom object and relationship definitions
-- plus the append-only effective-dated custom record revisions that reference
-- an exact object-definition version.
--
-- Storage disposition:
--   custom_object_definition       REGISTRY  PERMANENT  rebuild_source: none
--   custom_relationship_definition REGISTRY  PERMANENT  rebuild_source: none
--   custom_record_revision         AGGREGATE PERMANENT  rebuild_source: none

-- +goose Up

CREATE TABLE IF NOT EXISTS custom_object_definition (
    tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id    uuid        NOT NULL,

    kind      text       NOT NULL,
    namespace text       NOT NULL,
    version   cas_version NOT NULL,
    fields    jsonb      NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT custom_object_definition_identity
        UNIQUE (tenant_id, kind, namespace, version)
);

CREATE TABLE IF NOT EXISTS custom_relationship_definition (
    tenant_id   tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id      uuid        NOT NULL,

    name              text    NOT NULL,
    namespace         text    NOT NULL,
    version           cas_version NOT NULL,
    source_kind       text    NOT NULL,
    target_kind       text    NOT NULL,
    cardinality       text    NOT NULL,
    effective_date_rule text,
    allow_cycles      boolean NOT NULL DEFAULT false,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT custom_relationship_definition_identity
        UNIQUE (tenant_id, name, namespace, version)
);

CREATE TABLE IF NOT EXISTS custom_record_revision (
    tenant_id    tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id       uuid        NOT NULL,

    object_id          text       NOT NULL,
    object_kind        text       NOT NULL,
    namespace          text       NOT NULL,
    definition_version cas_version NOT NULL,
    effective_from     timestamptz,
    effective_to       timestamptz,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    known_at           timestamptz NOT NULL,
    field_values       jsonb      NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, object_kind, namespace, definition_version)
        REFERENCES custom_object_definition (tenant_id, kind, namespace, version)
);

CREATE INDEX IF NOT EXISTS custom_record_revision_lookup
    ON custom_record_revision (tenant_id, object_id, effective_from, recorded_at);

-- Definitions are superseded by a new version; record revisions are evidence
-- and are never rewritten or removed. The shared trigger is the same
-- append-only enforcement used by the issuer and workflow evidence tables.
CREATE OR REPLACE TRIGGER custom_object_definition_append_only
    BEFORE UPDATE OR DELETE ON custom_object_definition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE OR REPLACE TRIGGER custom_relationship_definition_append_only
    BEFORE UPDATE OR DELETE ON custom_relationship_definition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE OR REPLACE TRIGGER custom_record_revision_append_only
    BEFORE UPDATE OR DELETE ON custom_record_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON
    custom_object_definition,
    custom_relationship_definition,
    custom_record_revision
FROM PUBLIC;

ALTER TABLE custom_object_definition ENABLE ROW LEVEL SECURITY;
ALTER TABLE custom_object_definition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON custom_object_definition
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE custom_relationship_definition ENABLE ROW LEVEL SECURITY;
ALTER TABLE custom_relationship_definition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON custom_relationship_definition
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE custom_record_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE custom_record_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON custom_record_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON
    custom_object_definition,
    custom_relationship_definition,
    custom_record_revision
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON
    custom_record_revision,
    custom_relationship_definition,
    custom_object_definition
FROM hcmnext_app;

DROP TABLE custom_record_revision;
DROP TABLE custom_relationship_definition;
DROP TABLE custom_object_definition;
