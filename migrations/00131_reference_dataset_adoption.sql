-- Owner: reference-data platform lane. Phase: Gate A.
-- storage-disposition: reference_dataset_adoption | authoritative append-only tenant adoption, impact and rollback evidence | local PostgreSQL | tenant-local ACID adoption event | tenant-scoped.

-- +goose Up

CREATE TABLE IF NOT EXISTS reference_dataset_adoption (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    adoption_id     uuid NOT NULL,
    dataset_id      semantic_key NOT NULL,
    version         text NOT NULL,
    release_digest  content_digest NOT NULL,
    event           text NOT NULL,
    previous_version text NOT NULL DEFAULT '',
    effective_at    timestamptz NOT NULL,
    adopted_at      timestamptz NOT NULL,
    actor           text NOT NULL,
    rollout_digest  content_digest NOT NULL,
    overrides       jsonb NOT NULL,
    consumer_refs   jsonb NOT NULL,
    impact_refs     jsonb NOT NULL,
    event_sequence  bigint NOT NULL,
    record_digest   content_digest NOT NULL,
    PRIMARY KEY (tenant_id, adoption_id),
    UNIQUE (tenant_id, dataset_id, event_sequence),
    FOREIGN KEY (dataset_id, version)
        REFERENCES reference_dataset_release (dataset_id, version),
    CONSTRAINT reference_dataset_adoption_event_allowed
        CHECK (event IN ('ADOPTED', 'ROLLED_BACK')),
    CONSTRAINT reference_dataset_adoption_sequence_positive
        CHECK (event_sequence >= 1),
    CONSTRAINT reference_dataset_adoption_overrides_array
        CHECK (jsonb_typeof(overrides) = 'array'),
    CONSTRAINT reference_dataset_adoption_consumers_array
        CHECK (jsonb_typeof(consumer_refs) = 'array'),
    CONSTRAINT reference_dataset_adoption_impacts_array
        CHECK (jsonb_typeof(impact_refs) = 'array'),
    CONSTRAINT reference_dataset_adoption_actor_not_blank
        CHECK (actor <> '')
);

CREATE INDEX IF NOT EXISTS reference_dataset_adoption_latest
    ON reference_dataset_adoption (tenant_id, dataset_id, event_sequence DESC);

CREATE OR REPLACE TRIGGER reference_dataset_adoption_append_only
    BEFORE UPDATE OR DELETE ON reference_dataset_adoption
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE reference_dataset_adoption ENABLE ROW LEVEL SECURITY;
ALTER TABLE reference_dataset_adoption FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON reference_dataset_adoption
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON reference_dataset_adoption FROM PUBLIC;
GRANT SELECT, INSERT ON reference_dataset_adoption TO hcmnext_app;

-- +goose Down

REVOKE ALL ON reference_dataset_adoption FROM hcmnext_app;
DROP POLICY tenant_isolation ON reference_dataset_adoption;
ALTER TABLE reference_dataset_adoption NO FORCE ROW LEVEL SECURITY;
ALTER TABLE reference_dataset_adoption DISABLE ROW LEVEL SECURITY;
DROP TABLE reference_dataset_adoption;
