-- Owner: tenant data plane. Phase: P1B.
-- PERSIST-TENANT-001: durable logical tenant placement, provisioning evidence
-- and government-authorization profile revisions.
--
-- tenant_bootstrap_receipt (00029) already satisfies TENANT-002 and is not
-- duplicated here. These tables cover the distinct placement/control,
-- provisioning-event/ledger and SECARCH-018 authorization-profile concerns.

-- +goose Up

CREATE TABLE IF NOT EXISTS tenant_placement (
    tenant_id        tenant_ref NOT NULL PRIMARY KEY REFERENCES tenant (tenant_id),
    cell             text       NOT NULL,
    region           text       NOT NULL,
    residency_profile text      NOT NULL,
    isolation_tier   text       NOT NULL,
    epoch            bigint     NOT NULL,
    signature        bytea      NOT NULL,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_placement_epoch_positive CHECK (epoch >= 1),
    CONSTRAINT tenant_placement_signature_present CHECK (octet_length(signature) = 32),
    CONSTRAINT tenant_placement_cell_not_blank CHECK (btrim(cell) <> ''),
    CONSTRAINT tenant_placement_region_not_blank CHECK (btrim(region) <> ''),
    CONSTRAINT tenant_placement_residency_not_blank CHECK (btrim(residency_profile) <> ''),
    CONSTRAINT tenant_placement_isolation_not_blank CHECK (btrim(isolation_tier) <> '')
);

-- A placement is current state, but its fencing epoch may only move forward.
-- The trigger rejects same-epoch rewrites as well as backwards movement.
-- +goose StatementBegin
CREATE FUNCTION tenant_placement_epoch_fence() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.epoch <= OLD.epoch THEN
        RAISE EXCEPTION
            'tenant_placement %: epoch % must be greater than %',
            OLD.tenant_id, NEW.epoch, OLD.epoch
            USING ERRCODE = 'serialization_failure';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER tenant_placement_epoch_fence
    BEFORE UPDATE ON tenant_placement
    FOR EACH ROW EXECUTE FUNCTION tenant_placement_epoch_fence();

CREATE TABLE IF NOT EXISTS tenant_provisioning_event (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid       NOT NULL,
    kind               text       NOT NULL,
    plane              text       NOT NULL,
    verifier_principal text,
    reason             text,
    at                 timestamptz NOT NULL,
    event_sequence     bigint     NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT tenant_provisioning_event_sequence_unique
        UNIQUE (tenant_id, event_sequence),
    CONSTRAINT tenant_provisioning_event_sequence_positive
        CHECK (event_sequence >= 1),
    CONSTRAINT tenant_provisioning_event_kind_not_blank CHECK (btrim(kind) <> ''),
    CONSTRAINT tenant_provisioning_event_plane_not_blank CHECK (btrim(plane) <> '')
);

CREATE OR REPLACE TRIGGER tenant_provisioning_event_append_only
    BEFORE UPDATE OR DELETE ON tenant_provisioning_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON tenant_provisioning_event FROM PUBLIC;

CREATE TABLE IF NOT EXISTS government_authorization_profile (
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid          NOT NULL,
    schema_version      text          NOT NULL,
    revision            bigint        NOT NULL,
    integration_id      text          NOT NULL,
    applicable_programs  jsonb,
    system_boundary     jsonb,
    inherited_controls  jsonb,
    evidence            jsonb,
    assessor_status      text,
    review_date         date,
    fedramp             jsonb,
    cms                 jsonb,
    procurement_answers jsonb,
    revision_digest     content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT government_authorization_profile_revision_unique
        UNIQUE (tenant_id, integration_id, revision),
    CONSTRAINT government_authorization_profile_revision_positive CHECK (revision >= 1),
    CONSTRAINT government_authorization_profile_schema_not_blank CHECK (btrim(schema_version) <> ''),
    CONSTRAINT government_authorization_profile_integration_not_blank CHECK (btrim(integration_id) <> '')
);

CREATE INDEX IF NOT EXISTS government_authorization_profile_latest
    ON government_authorization_profile (tenant_id, integration_id, revision DESC);

-- Tenant isolation (DB-017): fail closed when app.tenant_id is absent or
-- blank. hcmnext_app is the only application role allowed to use these rows.
ALTER TABLE tenant_placement ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_placement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_placement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE tenant_provisioning_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_provisioning_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_provisioning_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE government_authorization_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE government_authorization_profile FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON government_authorization_profile
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Placement is mutable control state; the epoch trigger is the write fence.
GRANT SELECT, INSERT, UPDATE ON tenant_placement TO hcmnext_app;
-- Provisioning evidence is append-only.
GRANT SELECT, INSERT ON tenant_provisioning_event TO hcmnext_app;
-- Profile revisions are inserted as immutable identities by the store; no
-- UPDATE or DELETE privilege is granted to the application role.
GRANT SELECT, INSERT ON government_authorization_profile TO hcmnext_app;

-- +goose Down
REVOKE ALL ON government_authorization_profile FROM hcmnext_app;
REVOKE ALL ON tenant_provisioning_event FROM hcmnext_app;
REVOKE ALL ON tenant_placement FROM hcmnext_app;

DROP POLICY tenant_isolation ON government_authorization_profile;
ALTER TABLE government_authorization_profile NO FORCE ROW LEVEL SECURITY;
ALTER TABLE government_authorization_profile DISABLE ROW LEVEL SECURITY;
DROP TABLE government_authorization_profile;

DROP POLICY tenant_isolation ON tenant_provisioning_event;
ALTER TABLE tenant_provisioning_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_provisioning_event DISABLE ROW LEVEL SECURITY;
DROP TABLE tenant_provisioning_event;

DROP TRIGGER tenant_placement_epoch_fence ON tenant_placement;
DROP POLICY tenant_isolation ON tenant_placement;
ALTER TABLE tenant_placement NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_placement DISABLE ROW LEVEL SECURITY;
DROP TABLE tenant_placement;
DROP FUNCTION tenant_placement_epoch_fence();
