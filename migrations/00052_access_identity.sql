-- Owner: access data plane. Phase: PERSIST-ACCESS-001.
-- Durable access identity and entitlement graph. Revision rows and provider
-- observations are immutable; corrections append a new revision.

-- +goose Up

CREATE TABLE IF NOT EXISTS workforce_identity (
    row_id uuid NOT NULL, tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    identity_id text NOT NULL, subject text NOT NULL, system text NOT NULL,
    worker_ref uuid NOT NULL, revision cas_version NOT NULL,
    authority_class text NOT NULL, effective_from timestamptz NOT NULL,
    effective_to timestamptz, known_at timestamptz NOT NULL, lifecycle text NOT NULL,
    digest content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id), UNIQUE (tenant_id, identity_id, revision),
    CONSTRAINT workforce_identity_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT workforce_identity_authority_allowed CHECK (authority_class IN ('NATIVE', 'EXTERNAL_OBSERVATION')),
    CONSTRAINT workforce_identity_lifecycle_allowed CHECK (lifecycle IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'DISABLED', 'REVOKED', 'RETIRED'))
);
CREATE INDEX IF NOT EXISTS workforce_identity_history ON workforce_identity (tenant_id, identity_id, revision);
CREATE TRIGGER workforce_identity_append_only BEFORE UPDATE OR DELETE ON workforce_identity FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON workforce_identity FROM PUBLIC;

CREATE TABLE IF NOT EXISTS account_link (
    row_id uuid NOT NULL, tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    link_id text NOT NULL, workforce_identity_ref uuid NOT NULL, source text NOT NULL,
    application text NOT NULL, account_id text NOT NULL, revision cas_version NOT NULL,
    effective_from timestamptz NOT NULL, effective_to timestamptz, digest content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, workforce_identity_ref) REFERENCES workforce_identity (tenant_id, row_id),
    CONSTRAINT account_link_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to)
);
CREATE UNIQUE INDEX IF NOT EXISTS account_link_revision_unique ON account_link (tenant_id, link_id, revision);
CREATE INDEX IF NOT EXISTS account_link_identity ON account_link (tenant_id, workforce_identity_ref, revision DESC);
CREATE TRIGGER account_link_append_only BEFORE UPDATE OR DELETE ON account_link FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON account_link FROM PUBLIC;

CREATE TABLE IF NOT EXISTS entitlement_definition (
    row_id uuid NOT NULL, tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    definition_id text NOT NULL, application text NOT NULL, code text NOT NULL,
    version text NOT NULL, risk_class text NOT NULL, owner text NOT NULL,
    revision cas_version NOT NULL, effective_from timestamptz NOT NULL,
    effective_to timestamptz, digest content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id), UNIQUE (tenant_id, definition_id, revision),
    UNIQUE (tenant_id, application, code, version),
    CONSTRAINT entitlement_definition_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT entitlement_definition_risk_allowed CHECK (risk_class IN ('LOW', 'MODERATE', 'HIGH', 'PRIVILEGED'))
);
CREATE INDEX IF NOT EXISTS entitlement_definition_lookup ON entitlement_definition (tenant_id, definition_id, revision DESC);
CREATE TRIGGER entitlement_definition_append_only BEFORE UPDATE OR DELETE ON entitlement_definition FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON entitlement_definition FROM PUBLIC;

CREATE TABLE IF NOT EXISTS expected_entitlement (
    row_id uuid NOT NULL, tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    expected_id text NOT NULL, workforce_identity_ref uuid NOT NULL, account_link_ref uuid,
    entitlement_ref uuid NOT NULL, employment_ref uuid, position_ref uuid, policy_ref uuid,
    revision cas_version NOT NULL, effective_from timestamptz NOT NULL, effective_to timestamptz,
    digest content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id), UNIQUE (tenant_id, expected_id, revision),
    FOREIGN KEY (tenant_id, workforce_identity_ref) REFERENCES workforce_identity (tenant_id, row_id),
    FOREIGN KEY (tenant_id, account_link_ref) REFERENCES account_link (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entitlement_ref) REFERENCES entitlement_definition (tenant_id, row_id),
    FOREIGN KEY (tenant_id, employment_ref) REFERENCES employment (tenant_id, row_id),
    FOREIGN KEY (tenant_id, position_ref) REFERENCES job_position (tenant_id, row_id),
    FOREIGN KEY (tenant_id, policy_ref) REFERENCES authorization_policy_snapshot (tenant_id, snapshot_id),
    CONSTRAINT expected_entitlement_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to)
);
CREATE INDEX IF NOT EXISTS expected_entitlement_identity ON expected_entitlement (tenant_id, workforce_identity_ref, revision DESC);
CREATE INDEX IF NOT EXISTS expected_entitlement_entitlement ON expected_entitlement (tenant_id, entitlement_ref, revision DESC);
CREATE TRIGGER expected_entitlement_append_only BEFORE UPDATE OR DELETE ON expected_entitlement FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON expected_entitlement FROM PUBLIC;

CREATE TABLE IF NOT EXISTS external_access_observation (
    row_id uuid NOT NULL, tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    observed_workforce_identity_ref uuid NOT NULL, provider_version text NOT NULL,
    observed_state jsonb NOT NULL, event_sequence bigint NOT NULL,
    observed_at timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, observed_workforce_identity_ref) REFERENCES workforce_identity (tenant_id, row_id),
    UNIQUE (tenant_id, observed_workforce_identity_ref, event_sequence),
    CONSTRAINT external_access_observation_sequence_positive CHECK (event_sequence >= 1)
);
CREATE INDEX IF NOT EXISTS external_access_observation_history ON external_access_observation (tenant_id, observed_workforce_identity_ref, event_sequence DESC);
CREATE TRIGGER external_access_observation_append_only BEFORE UPDATE OR DELETE ON external_access_observation FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON external_access_observation FROM PUBLIC;

-- +goose StatementBegin
DO $$
DECLARE target text;
BEGIN
    FOREACH target IN ARRAY ARRAY['workforce_identity','account_link','entitlement_definition','expected_entitlement','external_access_observation'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)', target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON workforce_identity, account_link, entitlement_definition, expected_entitlement, external_access_observation TO hcmnext_app;

-- +goose Down
REVOKE ALL ON external_access_observation, expected_entitlement, entitlement_definition, account_link, workforce_identity FROM hcmnext_app;
DROP POLICY tenant_isolation ON external_access_observation;
DROP POLICY tenant_isolation ON expected_entitlement;
DROP POLICY tenant_isolation ON entitlement_definition;
DROP POLICY tenant_isolation ON account_link;
DROP POLICY tenant_isolation ON workforce_identity;
DROP TABLE external_access_observation;
DROP TABLE expected_entitlement;
DROP TABLE account_link;
DROP TABLE entitlement_definition;
DROP TABLE workforce_identity;
