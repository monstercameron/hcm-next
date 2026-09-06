-- Owner: data plane. DATA-013 replay snapshot manifests.
-- The manifest is a performance artifact. The ledger remains the chronology;
-- this row is immutable and is accepted only after its canonical digest is
-- verified by internal/data/rebuild.

-- +goose Up

CREATE TABLE replay_snapshot (
    tenant_id         tenant_ref      NOT NULL REFERENCES tenant (tenant_id),
    snapshot_id       uuid            NOT NULL,
    projection_name   semantic_key    NOT NULL,
    manifest          jsonb           NOT NULL,
    manifest_digest   content_digest  NOT NULL,
    state_row_count   bigint          NOT NULL,
    state_digest      content_digest  NOT NULL,
    created_at        timestamptz     NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, snapshot_id),
    CONSTRAINT replay_snapshot_manifest_object CHECK (jsonb_typeof(manifest) = 'object'),
    CONSTRAINT replay_snapshot_row_count_non_negative CHECK (state_row_count >= 0),
    CONSTRAINT replay_snapshot_projection_unique UNIQUE (tenant_id, projection_name, manifest_digest)
);

CREATE TRIGGER replay_snapshot_append_only
    BEFORE UPDATE OR DELETE ON replay_snapshot
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE replay_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE replay_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON replay_snapshot
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON replay_snapshot TO hcmnext_app;
REVOKE UPDATE, DELETE ON replay_snapshot FROM PUBLIC;

-- +goose Down

REVOKE ALL ON replay_snapshot FROM hcmnext_app;
DROP POLICY tenant_isolation ON replay_snapshot;
ALTER TABLE replay_snapshot NO FORCE ROW LEVEL SECURITY;
ALTER TABLE replay_snapshot DISABLE ROW LEVEL SECURITY;
DROP TABLE replay_snapshot;
