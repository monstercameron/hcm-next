-- Owner: reference-data platform lane. Phase: Gate A.
-- storage-disposition: reference_dataset_release | authoritative immutable global release metadata and members | local PostgreSQL | source release and publication evidence | global, tenant adoption references it.

-- +goose Up

CREATE TABLE IF NOT EXISTS reference_dataset_release (
    dataset_id        semantic_key NOT NULL,
    version           text         NOT NULL,
    release_digest    content_digest NOT NULL,
    state             text         NOT NULL,
    source            jsonb        NOT NULL,
    schema_digest     content_digest NOT NULL,
    applicability     jsonb        NOT NULL,
    members           jsonb        NOT NULL,
    consumer_refs     jsonb        NOT NULL,
    affected_intents  jsonb        NOT NULL,
    effective_from    timestamptz  NOT NULL,
    effective_to      timestamptz,
    known_at          timestamptz  NOT NULL,
    PRIMARY KEY (dataset_id, version),
    UNIQUE (dataset_id, release_digest),
    CONSTRAINT reference_dataset_release_state_allowed
        CHECK (state IN ('PUBLISHED', 'WITHDRAWN')),
    CONSTRAINT reference_dataset_release_source_object
        CHECK (jsonb_typeof(source) = 'object'),
    CONSTRAINT reference_dataset_release_applicability_array
        CHECK (jsonb_typeof(applicability) = 'array'),
    CONSTRAINT reference_dataset_release_members_array
        CHECK (jsonb_typeof(members) = 'array'),
    CONSTRAINT reference_dataset_release_consumers_array
        CHECK (jsonb_typeof(consumer_refs) = 'array'),
    CONSTRAINT reference_dataset_release_impacts_array
        CHECK (jsonb_typeof(affected_intents) = 'array'),
    CONSTRAINT reference_dataset_release_interval
        CHECK (effective_to IS NULL OR effective_from < effective_to)
);

CREATE INDEX IF NOT EXISTS reference_dataset_release_latest
    ON reference_dataset_release (dataset_id, effective_from DESC, version DESC);

CREATE OR REPLACE TRIGGER reference_dataset_release_append_only
    BEFORE UPDATE OR DELETE ON reference_dataset_release
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON reference_dataset_release FROM PUBLIC;
GRANT SELECT, INSERT ON reference_dataset_release TO hcmnext_app;

-- +goose Down

REVOKE ALL ON reference_dataset_release FROM hcmnext_app;
DROP TABLE reference_dataset_release;
