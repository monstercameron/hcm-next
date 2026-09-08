-- Owner: transaction/conflict. CONFLICT-003 durable commit fence.
-- +goose Up
CREATE TABLE conflict_write_intent (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    intent_id text NOT NULL,
    proposal_id text NOT NULL,
    snapshot_digest text NOT NULL,
    status text NOT NULL CHECK (status IN ('SUBMITTED','COMMITTED','RELEASED','CONFLICTED','CANCELLED')),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, intent_id)
);
CREATE TABLE conflict_write_footprint (
    tenant_id uuid NOT NULL,
    intent_id text NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    stream_key text NOT NULL,
    expected_sequence bigint NOT NULL CHECK (expected_sequence >= 0),
    scope_digest text NOT NULL,
    resource_canonical text NOT NULL,
    field_path text NOT NULL,
    operation text NOT NULL CHECK (operation IN ('CREATE','UPDATE','DELETE','UPSERT')),
    authority_domain text NOT NULL,
    authority_policy_ref text NOT NULL,
    effective_interval_canonical bytea NOT NULL,
    interval_kind text NOT NULL CHECK (interval_kind IN ('LOCAL_DATE','INSTANT')),
    interval_start text NOT NULL,
    interval_end text,
    PRIMARY KEY (tenant_id, intent_id, ordinal),
    FOREIGN KEY (tenant_id, intent_id) REFERENCES conflict_write_intent(tenant_id, intent_id)
);
CREATE TRIGGER conflict_write_footprint_append_only BEFORE UPDATE OR DELETE ON conflict_write_footprint FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TABLE conflict_scope_fence (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    scope_digest text NOT NULL,
    intent_id text NOT NULL,
    fence bigint NOT NULL,
    resource_canonical text NOT NULL,
    field_path text NOT NULL,
    interval_kind text NOT NULL CHECK (interval_kind IN ('LOCAL_DATE','INSTANT')),
    interval_start text NOT NULL,
    interval_end text,
    PRIMARY KEY (tenant_id, intent_id, scope_digest)
);
ALTER TABLE conflict_write_intent ENABLE ROW LEVEL SECURITY;
ALTER TABLE conflict_write_intent FORCE ROW LEVEL SECURITY;
ALTER TABLE conflict_write_footprint ENABLE ROW LEVEL SECURITY;
ALTER TABLE conflict_write_footprint FORCE ROW LEVEL SECURITY;
ALTER TABLE conflict_scope_fence ENABLE ROW LEVEL SECURITY;
ALTER TABLE conflict_scope_fence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON conflict_write_intent USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON conflict_write_footprint USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON conflict_scope_fence USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON conflict_write_intent TO hcmnext_app;
GRANT SELECT, INSERT ON conflict_write_footprint TO hcmnext_app;
GRANT SELECT, INSERT, DELETE ON conflict_scope_fence TO hcmnext_app;

-- +goose Down
DROP TABLE conflict_write_footprint;
DROP TABLE conflict_scope_fence;
DROP TABLE conflict_write_intent;
