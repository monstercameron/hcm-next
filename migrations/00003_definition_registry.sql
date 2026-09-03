-- Owner: data plane. Phase: P1A.
-- DB-007 (minimal): immutable definition versions with supersession lineage and
-- a mutable active pointer. The pointer is a convenience; the version rows are
-- the authoritative history.

-- +goose Up

CREATE TABLE definition_version (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    definition_kind    text           NOT NULL,
    definition_key     semantic_key   NOT NULL,
    version            cas_version    NOT NULL,
    definition_digest  content_digest NOT NULL,
    digest_algorithm   digest_algorithm NOT NULL DEFAULT 'sha256',
    source_ref         text           NOT NULL,
    body               bytea          NOT NULL,
    supersedes_version bigint,
    published_by       text           NOT NULL,
    published_at       timestamptz    NOT NULL,
    recorded_at        timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, definition_kind, definition_key, version),
    CONSTRAINT definition_version_kind_allowed CHECK (
        definition_kind IN (
            'ENTITY', 'PROPERTY', 'RELATIONSHIP', 'INTENT', 'SCHEMA',
            'CAPABILITY', 'WORKFLOW', 'RULE', 'CONFIG'
        )
    ),
    CONSTRAINT definition_version_supersedes_is_earlier CHECK (
        supersedes_version IS NULL OR supersedes_version < version
    ),
    CONSTRAINT definition_version_supersession_lineage
        FOREIGN KEY (tenant_id, definition_kind, definition_key, supersedes_version)
        REFERENCES definition_version (tenant_id, definition_kind, definition_key, version)
);

CREATE TRIGGER definition_version_append_only
    BEFORE UPDATE OR DELETE ON definition_version
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON definition_version FROM PUBLIC;

CREATE TABLE definition_active_pointer (
    tenant_id       tenant_ref   NOT NULL,
    definition_kind text         NOT NULL,
    definition_key  semantic_key NOT NULL,
    active_version  cas_version  NOT NULL,
    effective_from  timestamptz  NOT NULL,
    effective_to    timestamptz,
    pointer_version cas_version  NOT NULL DEFAULT 1,
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, definition_kind, definition_key),
    CONSTRAINT definition_active_pointer_target
        FOREIGN KEY (tenant_id, definition_kind, definition_key, active_version)
        REFERENCES definition_version (tenant_id, definition_kind, definition_key, version),
    CONSTRAINT definition_active_pointer_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);

-- +goose Down
DROP TABLE definition_active_pointer;
DROP TABLE definition_version;
