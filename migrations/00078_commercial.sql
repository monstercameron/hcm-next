-- Owner: commercial/data plane. Phase: P1B.
-- PERSIST-COMMERCIAL-001: durable commercial contract, entitlement and
-- partner-application installation metadata. Billing authority is deliberately
-- outside this table set: these rows describe a governed product boundary.
--
-- Storage disposition:
--   commercial_contract_revision, entitlement_snapshot, partner_installation,
--   partner_data_processing_review: AGGREGATE/PERMANENT.
--   partner_application, partner_application_version_revision: REGISTRY/
--   PERMANENT, platform scoped and read-only to hcmnext_app.
--   partner_installation_event: LEDGER/PERMANENT, append-only.

-- +goose Up

CREATE TABLE IF NOT EXISTS commercial_contract_revision (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid         NOT NULL,
    contract_id    text         NOT NULL,
    revision       bigint       NOT NULL,
    effective_from timestamptz,
    effective_to   timestamptz,
    capabilities   jsonb        NOT NULL,
    bound          boolean      NOT NULL DEFAULT false,
    price_cents    bigint,
    currency       text,
    fingerprint    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT commercial_contract_revision_unique
        UNIQUE (tenant_id, contract_id, revision)
);

CREATE OR REPLACE TRIGGER commercial_contract_revision_append_only
    BEFORE UPDATE OR DELETE ON commercial_contract_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON commercial_contract_revision FROM PUBLIC;

CREATE TABLE IF NOT EXISTS entitlement_snapshot (
    tenant_id   tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id      uuid           NOT NULL,
    contract_id text           NOT NULL,
    revision    bigint         NOT NULL,
    fingerprint content_digest NOT NULL,
    frozen_at   timestamptz    NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, contract_id, revision)
        REFERENCES commercial_contract_revision (tenant_id, contract_id, revision)
);

CREATE OR REPLACE TRIGGER entitlement_snapshot_append_only
    BEFORE UPDATE OR DELETE ON entitlement_snapshot
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON entitlement_snapshot FROM PUBLIC;

-- Platform catalogs are shared reference data. They have no tenant identity;
-- hcmnext_app can read them, while a registry owner populates them.
CREATE TABLE IF NOT EXISTS partner_application (
    row_id               uuid PRIMARY KEY,
    partner_ref          text NOT NULL,
    application_id       text NOT NULL,
    agreement_ref        text,
    declared_capabilities jsonb,
    data_classes         jsonb,
    redirect_endpoints   jsonb,
    callback_endpoints   jsonb,
    contact_ref          text,
    legal_ref            text,

    CONSTRAINT partner_application_application_unique UNIQUE (application_id)
);

CREATE TABLE IF NOT EXISTS partner_application_version_revision (
    row_id          uuid PRIMARY KEY,
    application_id  text NOT NULL,
    version         text NOT NULL,
    revision        bigint NOT NULL,
    state           text NOT NULL,
    review          jsonb,
    successor_version text,
    digest          content_digest NOT NULL,

    CONSTRAINT partner_application_version_revision_unique
        UNIQUE (application_id, version, revision)
);

CREATE TABLE IF NOT EXISTS partner_installation (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid       NOT NULL,
    installation_id    text       NOT NULL,
    revision           bigint     NOT NULL,
    state              text       NOT NULL,
    version_binding    jsonb      NOT NULL,
    organization_scope text,
    population_scope   text,
    data_classes       jsonb,
    field_scopes       jsonb,
    requester          text,
    approver_evidence  jsonb,
    review_ref         text,
    digest             content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT partner_installation_revision_unique
        UNIQUE (tenant_id, installation_id, revision)
);

CREATE OR REPLACE TRIGGER partner_installation_append_only
    BEFORE UPDATE OR DELETE ON partner_installation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON partner_installation FROM PUBLIC;

CREATE TABLE IF NOT EXISTS partner_installation_event (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid       NOT NULL,
    installation_id    text       NOT NULL,
    revision           bigint     NOT NULL,
    previous_digest    content_digest,
    previous_state     text,
    state              text       NOT NULL,
    actor              text,
    evidence_ref       text,
    review_ref         text,
    installation_digest content_digest,
    digest             content_digest NOT NULL,
    event_sequence     bigint     NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT partner_installation_event_sequence_unique
        UNIQUE (tenant_id, installation_id, event_sequence),
    FOREIGN KEY (tenant_id, installation_id, revision)
        REFERENCES partner_installation (tenant_id, installation_id, revision)
);

CREATE OR REPLACE TRIGGER partner_installation_event_append_only
    BEFORE UPDATE OR DELETE ON partner_installation_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON partner_installation_event FROM PUBLIC;

CREATE TABLE IF NOT EXISTS partner_data_processing_review (
    tenant_id        tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid       NOT NULL,
    application_id   text       NOT NULL,
    version          text       NOT NULL,
    submitter        text,
    owner_ref        text,
    reviewer         text,
    reviewed_at      timestamptz,
    expires_at       timestamptz,
    data_classes     jsonb,
    scope_refs       jsonb,
    destination_refs jsonb,
    processor_refs   jsonb,
    residency_refs   jsonb,
    items            jsonb,
    digest           content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id)
);

CREATE OR REPLACE TRIGGER partner_data_processing_review_append_only
    BEFORE UPDATE OR DELETE ON partner_data_processing_review
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON partner_data_processing_review FROM PUBLIC;

-- Tenant isolation is fail-closed when app.tenant_id is absent. The platform
-- catalogs intentionally have no policy because they intentionally have no
-- tenant column.
ALTER TABLE commercial_contract_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE commercial_contract_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON commercial_contract_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE entitlement_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE entitlement_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON entitlement_snapshot
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE partner_installation ENABLE ROW LEVEL SECURITY;
ALTER TABLE partner_installation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON partner_installation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE partner_installation_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE partner_installation_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON partner_installation_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE partner_data_processing_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE partner_data_processing_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON partner_data_processing_review
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT ON partner_application, partner_application_version_revision TO hcmnext_app;
GRANT SELECT, INSERT ON
    commercial_contract_revision,
    entitlement_snapshot,
    partner_installation,
    partner_installation_event,
    partner_data_processing_review
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON partner_data_processing_review FROM hcmnext_app;
REVOKE ALL ON partner_installation_event FROM hcmnext_app;
REVOKE ALL ON partner_installation FROM hcmnext_app;
REVOKE ALL ON entitlement_snapshot FROM hcmnext_app;
REVOKE ALL ON commercial_contract_revision FROM hcmnext_app;
REVOKE ALL ON partner_application_version_revision FROM hcmnext_app;
REVOKE ALL ON partner_application FROM hcmnext_app;

DROP POLICY tenant_isolation ON partner_data_processing_review;
ALTER TABLE partner_data_processing_review NO FORCE ROW LEVEL SECURITY;
ALTER TABLE partner_data_processing_review DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON partner_installation_event;
ALTER TABLE partner_installation_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE partner_installation_event DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON partner_installation;
ALTER TABLE partner_installation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE partner_installation DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON entitlement_snapshot;
ALTER TABLE entitlement_snapshot NO FORCE ROW LEVEL SECURITY;
ALTER TABLE entitlement_snapshot DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON commercial_contract_revision;
ALTER TABLE commercial_contract_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE commercial_contract_revision DISABLE ROW LEVEL SECURITY;

DROP TABLE partner_data_processing_review;
DROP TABLE partner_installation_event;
DROP TABLE partner_installation;
DROP TABLE partner_application_version_revision;
DROP TABLE partner_application;
DROP TABLE entitlement_snapshot;
DROP TABLE commercial_contract_revision;
