-- Owner: governance lane. Phase: P1A.
-- DB-013: governance, AuthZ, legal and evidence control data.
-- Retention: evidence_artifact is PERMANENT (the row is the governed pointer
-- to a content-addressed object in the governed object store, not the bytes
-- themselves). REFACTOR keeps raw bytes out of relational rows.

-- +goose Up

CREATE TABLE IF NOT EXISTS principal (
    tenant_id         tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    principal_id      uuid         NOT NULL,
    kind              text         NOT NULL,
    subject           semantic_key NOT NULL,
    org_scope_id      uuid,
    assurance         text         NOT NULL,
    authn_method      text         NOT NULL,
    credential_digest content_digest,
    revocation_epoch  bigint       NOT NULL DEFAULT 1,
    lifecycle         text         NOT NULL DEFAULT 'ACTIVE',
    created_at        timestamptz  NOT NULL DEFAULT now(),
    updated_at        timestamptz  NOT NULL DEFAULT now(),
    expires_at        timestamptz,
    metadata          jsonb        NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id, principal_id),
    CONSTRAINT principal_kind_allowed CHECK (kind IN ('USER','SERVICE','SYSTEM','DELEGATE')),
    CONSTRAINT principal_assurance_allowed CHECK (assurance IN ('AAL1','AAL2','AAL3')),
    CONSTRAINT principal_lifecycle_allowed CHECK (lifecycle IN ('ACTIVE','SUSPENDED','REVOKED','EXPIRED')),
    CONSTRAINT principal_revocation_positive CHECK (revocation_epoch >= 1),
    CONSTRAINT principal_metadata_object CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT principal_expires_after_created CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS principal_subject ON principal (tenant_id, subject);
CREATE INDEX IF NOT EXISTS principal_org_scope ON principal (tenant_id, org_scope_id) WHERE org_scope_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS authority_source (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    authority_source_id uuid        NOT NULL,
    kind               text         NOT NULL,
    display_name       text         NOT NULL,
    uri                semantic_key NOT NULL,
    version            cas_version  NOT NULL DEFAULT 1,
    valid_interval     tstzrange    NOT NULL,
    content_digest     content_digest NOT NULL,
    created_at         timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, authority_source_id),
    CONSTRAINT authority_source_kind_allowed CHECK (kind IN ('HRIS','IDP','ATTESTATION','POLICY_BUNDLE')),
    CONSTRAINT authority_source_valid_interval CHECK (NOT isempty(valid_interval))
);

CREATE TABLE IF NOT EXISTS authority_binding (
    tenant_id           tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    binding_id          uuid         NOT NULL,
    principal_id        uuid         NOT NULL,
    authority_source_id uuid         NOT NULL,
    scope               jsonb        NOT NULL DEFAULT '{}'::jsonb,
    valid_from          timestamptz  NOT NULL,
    valid_to            timestamptz,
    created_at          timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, binding_id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES principal (tenant_id, principal_id),
    FOREIGN KEY (tenant_id, authority_source_id) REFERENCES authority_source (tenant_id, authority_source_id),
    CONSTRAINT authority_binding_scope_object CHECK (jsonb_typeof(scope) = 'object'),
    CONSTRAINT authority_binding_half_open CHECK (valid_to IS NULL OR valid_from < valid_to)
);
CREATE INDEX IF NOT EXISTS authority_binding_principal ON authority_binding (tenant_id, principal_id);
CREATE INDEX IF NOT EXISTS authority_binding_source ON authority_binding (tenant_id, authority_source_id);

CREATE TABLE IF NOT EXISTS authentication_session (
    tenant_id     tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    session_id    uuid         NOT NULL,
    principal_id  uuid         NOT NULL,
    issued_at     timestamptz  NOT NULL DEFAULT now(),
    expires_at    timestamptz  NOT NULL,
    authn_method  text         NOT NULL,
    assurance     text         NOT NULL,
    session_digest content_digest NOT NULL,
    ip_hash       content_digest,
    metadata      jsonb        NOT NULL DEFAULT '{}'::jsonb,
    revoked_at    timestamptz,
    PRIMARY KEY (tenant_id, session_id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES principal (tenant_id, principal_id),
    CONSTRAINT authentication_session_expires_after_issued CHECK (expires_at > issued_at),
    CONSTRAINT authentication_session_metadata_object CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT authentication_session_assurance_allowed CHECK (assurance IN ('AAL1','AAL2','AAL3'))
);
CREATE INDEX IF NOT EXISTS authentication_session_principal ON authentication_session (tenant_id, principal_id, issued_at);
CREATE INDEX IF NOT EXISTS authentication_session_expires ON authentication_session (tenant_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS delegation_grant (
    tenant_id               tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    grant_id                uuid         NOT NULL,
    delegator_principal_id  uuid         NOT NULL,
    delegate_principal_id   uuid         NOT NULL,
    delegator_tenant        tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    delegate_tenant         tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    scope                   jsonb        NOT NULL DEFAULT '{}'::jsonb,
    valid_from              timestamptz  NOT NULL DEFAULT now(),
    valid_to                timestamptz,
    created_at              timestamptz  NOT NULL DEFAULT now(),
    revoked_at              timestamptz,
    revoked_by              uuid,
    reason                  text,
    PRIMARY KEY (tenant_id, grant_id),
    FOREIGN KEY (tenant_id, delegator_principal_id) REFERENCES principal (tenant_id, principal_id),
    FOREIGN KEY (tenant_id, delegate_principal_id) REFERENCES principal (tenant_id, principal_id),
    CONSTRAINT delegation_grant_same_tenant CHECK (delegate_tenant = delegator_tenant AND delegator_tenant = tenant_id),
    CONSTRAINT delegation_grant_not_self CHECK (delegator_principal_id <> delegate_principal_id),
    CONSTRAINT delegation_grant_half_open CHECK (valid_to IS NULL OR valid_from < valid_to),
    CONSTRAINT delegation_grant_scope_object CHECK (jsonb_typeof(scope) = 'object'),
    CONSTRAINT delegation_grant_revoked_after_created CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX IF NOT EXISTS delegation_grant_delegate ON delegation_grant (tenant_id, delegate_principal_id);
CREATE INDEX IF NOT EXISTS delegation_grant_delegator ON delegation_grant (tenant_id, delegator_principal_id);

CREATE TABLE IF NOT EXISTS authorization_policy_snapshot (
    tenant_id     tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    snapshot_id   uuid           NOT NULL,
    policy_key    semantic_key   NOT NULL,
    version       cas_version    NOT NULL,
    content_digest content_digest NOT NULL,
    body          jsonb          NOT NULL,
    valid_from    timestamptz    NOT NULL DEFAULT now(),
    valid_to      timestamptz,
    created_at    timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, snapshot_id),
    CONSTRAINT authorization_policy_snapshot_version_positive CHECK (version >= 1),
    CONSTRAINT authorization_policy_snapshot_body_object CHECK (jsonb_typeof(body) = 'object'),
    CONSTRAINT authorization_policy_snapshot_half_open CHECK (valid_to IS NULL OR valid_from < valid_to),
    CONSTRAINT authorization_policy_snapshot_key_version_unique UNIQUE (tenant_id, policy_key, version)
);
CREATE INDEX IF NOT EXISTS authorization_policy_snapshot_key ON authorization_policy_snapshot (tenant_id, policy_key, version);

CREATE TABLE IF NOT EXISTS authorization_decision (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    decision_id        uuid           NOT NULL,
    principal_id       uuid           NOT NULL,
    policy_snapshot_id uuid           NOT NULL,
    input_digest       content_digest NOT NULL,
    result_digest      content_digest NOT NULL,
    evidence_id        uuid           NOT NULL UNIQUE,
    result             text           NOT NULL,
    valid_until        timestamptz    NOT NULL,
    evaluated_at       timestamptz    NOT NULL DEFAULT now(),
    policy_snapshot_ids uuid[]        NOT NULL DEFAULT '{}',
    session_id         uuid,
    metadata           jsonb          NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id, decision_id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES principal (tenant_id, principal_id),
    FOREIGN KEY (tenant_id, policy_snapshot_id) REFERENCES authorization_policy_snapshot (tenant_id, snapshot_id),
    CONSTRAINT authorization_decision_result_allowed CHECK (result IN ('ALLOW','DENY','ABSTAIN')),
    CONSTRAINT authorization_decision_metadata_object CHECK (jsonb_typeof(metadata) = 'object')
);
CREATE INDEX IF NOT EXISTS authorization_decision_principal ON authorization_decision (tenant_id, principal_id, evaluated_at);
CREATE INDEX IF NOT EXISTS authorization_decision_snapshot ON authorization_decision (tenant_id, policy_snapshot_id);

CREATE TABLE IF NOT EXISTS data_access_manifest (
    tenant_id   tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    manifest_id uuid         NOT NULL,
    decision_id uuid         NOT NULL,
    principal_id uuid        NOT NULL,
    resource_ref semantic_key NOT NULL,
    access_kind text         NOT NULL,
    fields      jsonb        NOT NULL DEFAULT '{}'::jsonb,
    valid_interval tstzrange NOT NULL,
    created_at  timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, manifest_id),
    FOREIGN KEY (tenant_id, decision_id) REFERENCES authorization_decision (tenant_id, decision_id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES principal (tenant_id, principal_id),
    CONSTRAINT data_access_manifest_kind_allowed CHECK (access_kind IN ('READ','WRITE','ADMIN','EXECUTE')),
    CONSTRAINT data_access_manifest_interval CHECK (NOT isempty(valid_interval)),
    CONSTRAINT data_access_manifest_fields_object CHECK (jsonb_typeof(fields) = 'object')
);
CREATE INDEX IF NOT EXISTS data_access_manifest_decision ON data_access_manifest (tenant_id, decision_id);
CREATE INDEX IF NOT EXISTS data_access_manifest_resource ON data_access_manifest (tenant_id, resource_ref);

CREATE TABLE IF NOT EXISTS jurisdiction (
    jurisdiction_id uuid         PRIMARY KEY,
    country         text         NOT NULL,
    state           text,
    region          text,
    version         cas_version  NOT NULL DEFAULT 1,
    valid_interval  tstzrange    NOT NULL,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT jurisdiction_country_present CHECK (country <> '' AND country = btrim(country)),
    CONSTRAINT jurisdiction_valid_interval CHECK (NOT isempty(valid_interval))
);
CREATE INDEX IF NOT EXISTS jurisdiction_country ON jurisdiction (country, state);

CREATE TABLE IF NOT EXISTS legal_rule_pack (
    tenant_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    pack_id         uuid           NOT NULL,
    jurisdiction_id uuid           NOT NULL REFERENCES jurisdiction (jurisdiction_id),
    pack_key        semantic_key   NOT NULL,
    version         cas_version    NOT NULL,
    valid_interval  tstzrange      NOT NULL,
    content_digest  content_digest NOT NULL,
    body            jsonb          NOT NULL,
    created_at      timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, pack_id),
    CONSTRAINT legal_rule_pack_version_positive CHECK (version >= 1),
    CONSTRAINT legal_rule_pack_body_object CHECK (jsonb_typeof(body) = 'object'),
    CONSTRAINT legal_rule_pack_interval CHECK (NOT isempty(valid_interval)),
    CONSTRAINT legal_rule_pack_key_version_unique UNIQUE (tenant_id, pack_key, version)
);
CREATE INDEX IF NOT EXISTS legal_rule_pack_jurisdiction ON legal_rule_pack (tenant_id, jurisdiction_id);

CREATE TABLE IF NOT EXISTS rule_evaluation (
    tenant_id     tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    evaluation_id uuid           NOT NULL,
    pack_id       uuid           NOT NULL,
    input_digest  content_digest NOT NULL,
    result_digest content_digest NOT NULL,
    evidence_id   uuid           NOT NULL UNIQUE,
    result        text           NOT NULL,
    valid_until   timestamptz    NOT NULL,
    evaluated_at  timestamptz    NOT NULL DEFAULT now(),
    input_snapshot jsonb         NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id, evaluation_id),
    FOREIGN KEY (tenant_id, pack_id) REFERENCES legal_rule_pack (tenant_id, pack_id),
    CONSTRAINT rule_evaluation_result_allowed CHECK (result IN ('COMPLIANT','NON_COMPLIANT','NEEDS_REVIEW','NOT_APPLICABLE')),
    CONSTRAINT rule_evaluation_snapshot_object CHECK (jsonb_typeof(input_snapshot) = 'object')
);
CREATE INDEX IF NOT EXISTS rule_evaluation_pack ON rule_evaluation (tenant_id, pack_id, evaluated_at);

CREATE TABLE IF NOT EXISTS obligation (
    tenant_id     tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    obligation_id uuid         NOT NULL,
    evaluation_id uuid         NOT NULL,
    kind          text         NOT NULL,
    status        text         NOT NULL DEFAULT 'OPEN',
    due_at        timestamptz,
    payload       jsonb        NOT NULL DEFAULT '{}'::jsonb,
    content_digest content_digest NOT NULL,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, obligation_id),
    FOREIGN KEY (tenant_id, evaluation_id) REFERENCES rule_evaluation (tenant_id, evaluation_id),
    CONSTRAINT obligation_kind_present CHECK (kind <> ''),
    CONSTRAINT obligation_status_allowed CHECK (status IN ('OPEN','FULFILLED','WAIVED','OVERDUE','CANCELLED')),
    CONSTRAINT obligation_payload_object CHECK (jsonb_typeof(payload) = 'object')
);
CREATE INDEX IF NOT EXISTS obligation_evaluation ON obligation (tenant_id, evaluation_id);

CREATE TABLE IF NOT EXISTS obligation_binding (
    tenant_id     tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    binding_id    uuid         NOT NULL,
    obligation_id uuid         NOT NULL,
    principal_id  uuid         NOT NULL,
    bound_at      timestamptz  NOT NULL DEFAULT now(),
    status        text         NOT NULL DEFAULT 'BOUND',
    metadata      jsonb        NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id, binding_id),
    FOREIGN KEY (tenant_id, obligation_id) REFERENCES obligation (tenant_id, obligation_id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES principal (tenant_id, principal_id),
    CONSTRAINT obligation_binding_status_allowed CHECK (status IN ('BOUND','RELEASED','FULFILLED')),
    CONSTRAINT obligation_binding_metadata_object CHECK (jsonb_typeof(metadata) = 'object')
);
CREATE INDEX IF NOT EXISTS obligation_binding_obligation ON obligation_binding (tenant_id, obligation_id);

CREATE TABLE IF NOT EXISTS evidence_artifact (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    artifact_id      uuid           NOT NULL,
    kind             text           NOT NULL,
    content_digest   content_digest NOT NULL,
    object_store_ref text           NOT NULL,
    size_bytes       bigint         NOT NULL,
    retention        text           NOT NULL DEFAULT 'PERMANENT',
    collected_at     timestamptz    NOT NULL DEFAULT now(),
    created_at       timestamptz    NOT NULL DEFAULT now(),
    metadata         jsonb          NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id, artifact_id),
    CONSTRAINT evidence_artifact_retention_permanent CHECK (retention = 'PERMANENT'),
    CONSTRAINT evidence_artifact_size_positive CHECK (size_bytes >= 0),
    CONSTRAINT evidence_artifact_store_ref_present CHECK (object_store_ref <> '' AND object_store_ref = btrim(object_store_ref)),
    CONSTRAINT evidence_artifact_metadata_object CHECK (jsonb_typeof(metadata) = 'object')
);
CREATE INDEX IF NOT EXISTS evidence_artifact_digest ON evidence_artifact (tenant_id, content_digest);

CREATE TABLE IF NOT EXISTS evidence_manifest (
    tenant_id   tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    manifest_id uuid           NOT NULL,
    artifact_ids uuid[]        NOT NULL DEFAULT '{}',
    root_digest content_digest NOT NULL,
    collected_at timestamptz   NOT NULL DEFAULT now(),
    created_at  timestamptz    NOT NULL DEFAULT now(),
    metadata    jsonb          NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id, manifest_id),
    CONSTRAINT evidence_manifest_root_present CHECK (root_digest <> ''),
    CONSTRAINT evidence_manifest_metadata_object CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS governance_decision_bundle (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    bundle_id            uuid           NOT NULL,
    authz_decision_ids   uuid[]         NOT NULL DEFAULT '{}',
    legal_evaluation_ids uuid[]         NOT NULL DEFAULT '{}',
    obligation_ids       uuid[]         NOT NULL DEFAULT '{}',
    evidence_manifest_id uuid,
    canonical_digest     content_digest NOT NULL,
    valid_until          timestamptz    NOT NULL,
    created_at           timestamptz    NOT NULL DEFAULT now(),
    metadata             jsonb          NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id, bundle_id),
    FOREIGN KEY (tenant_id, evidence_manifest_id) REFERENCES evidence_manifest (tenant_id, manifest_id),
    CONSTRAINT governance_bundle_canonical_present CHECK (canonical_digest <> ''),
    CONSTRAINT governance_bundle_metadata_object CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT governance_bundle_valid_future CHECK (valid_until > created_at)
);
CREATE INDEX IF NOT EXISTS governance_bundle_manifest ON governance_decision_bundle (tenant_id, evidence_manifest_id) WHERE evidence_manifest_id IS NOT NULL;

ALTER TABLE principal ENABLE ROW LEVEL SECURITY;
ALTER TABLE principal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON principal USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE authority_source ENABLE ROW LEVEL SECURITY;
ALTER TABLE authority_source FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON authority_source USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE authority_binding ENABLE ROW LEVEL SECURITY;
ALTER TABLE authority_binding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON authority_binding USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE authentication_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE authentication_session FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON authentication_session USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE delegation_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE delegation_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON delegation_grant USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE authorization_policy_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE authorization_policy_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON authorization_policy_snapshot USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE authorization_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE authorization_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON authorization_decision USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE data_access_manifest ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_access_manifest FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON data_access_manifest USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE legal_rule_pack ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_rule_pack FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_rule_pack USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE rule_evaluation ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_evaluation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON rule_evaluation USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE obligation ENABLE ROW LEVEL SECURITY;
ALTER TABLE obligation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON obligation USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE obligation_binding ENABLE ROW LEVEL SECURITY;
ALTER TABLE obligation_binding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON obligation_binding USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE evidence_artifact ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_artifact FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON evidence_artifact USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE evidence_manifest ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_manifest FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON evidence_manifest USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE governance_decision_bundle ENABLE ROW LEVEL SECURITY;
ALTER TABLE governance_decision_bundle FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON governance_decision_bundle USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON authorization_policy_snapshot FROM PUBLIC;
REVOKE UPDATE, DELETE ON authorization_decision FROM PUBLIC;
REVOKE UPDATE, DELETE ON evidence_artifact FROM PUBLIC;
REVOKE UPDATE, DELETE ON evidence_manifest FROM PUBLIC;
REVOKE DELETE ON principal FROM PUBLIC;
REVOKE DELETE ON authority_source FROM PUBLIC;
REVOKE DELETE ON authority_binding FROM PUBLIC;
REVOKE DELETE ON authentication_session FROM PUBLIC;
REVOKE DELETE ON delegation_grant FROM PUBLIC;
REVOKE DELETE ON data_access_manifest FROM PUBLIC;
REVOKE DELETE ON legal_rule_pack FROM PUBLIC;
REVOKE DELETE ON rule_evaluation FROM PUBLIC;
REVOKE DELETE ON obligation FROM PUBLIC;
REVOKE DELETE ON obligation_binding FROM PUBLIC;
REVOKE DELETE ON governance_decision_bundle FROM PUBLIC;

GRANT SELECT, INSERT, UPDATE ON principal TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON authority_source TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON authority_binding TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON authentication_session TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON delegation_grant TO hcmnext_app;
GRANT SELECT, INSERT ON authorization_policy_snapshot TO hcmnext_app;
GRANT SELECT, INSERT ON authorization_decision TO hcmnext_app;
GRANT SELECT, INSERT ON data_access_manifest TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON legal_rule_pack TO hcmnext_app;
GRANT SELECT, INSERT ON rule_evaluation TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON obligation TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON obligation_binding TO hcmnext_app;
GRANT SELECT, INSERT ON evidence_artifact TO hcmnext_app;
GRANT SELECT, INSERT ON evidence_manifest TO hcmnext_app;
GRANT SELECT, INSERT ON governance_decision_bundle TO hcmnext_app;
GRANT SELECT ON jurisdiction TO hcmnext_app;
GRANT SELECT, INSERT ON jurisdiction TO hcmnext_app;

-- +goose Down
REVOKE ALL ON governance_decision_bundle FROM hcmnext_app;
REVOKE ALL ON evidence_manifest FROM hcmnext_app;
REVOKE ALL ON evidence_artifact FROM hcmnext_app;
REVOKE ALL ON obligation_binding FROM hcmnext_app;
REVOKE ALL ON obligation FROM hcmnext_app;
REVOKE ALL ON rule_evaluation FROM hcmnext_app;
REVOKE ALL ON legal_rule_pack FROM hcmnext_app;
REVOKE ALL ON jurisdiction FROM hcmnext_app;
REVOKE ALL ON data_access_manifest FROM hcmnext_app;
REVOKE ALL ON authorization_decision FROM hcmnext_app;
REVOKE ALL ON authorization_policy_snapshot FROM hcmnext_app;
REVOKE ALL ON delegation_grant FROM hcmnext_app;
REVOKE ALL ON authentication_session FROM hcmnext_app;
REVOKE ALL ON authority_binding FROM hcmnext_app;
REVOKE ALL ON authority_source FROM hcmnext_app;
REVOKE ALL ON principal FROM hcmnext_app;
DROP POLICY tenant_isolation ON governance_decision_bundle;
ALTER TABLE governance_decision_bundle NO FORCE ROW LEVEL SECURITY;
ALTER TABLE governance_decision_bundle DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON evidence_manifest;
ALTER TABLE evidence_manifest NO FORCE ROW LEVEL SECURITY;
ALTER TABLE evidence_manifest DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON evidence_artifact;
ALTER TABLE evidence_artifact NO FORCE ROW LEVEL SECURITY;
ALTER TABLE evidence_artifact DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON obligation_binding;
ALTER TABLE obligation_binding NO FORCE ROW LEVEL SECURITY;
ALTER TABLE obligation_binding DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON obligation;
ALTER TABLE obligation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE obligation DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON rule_evaluation;
ALTER TABLE rule_evaluation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE rule_evaluation DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON legal_rule_pack;
ALTER TABLE legal_rule_pack NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_rule_pack DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON data_access_manifest;
ALTER TABLE data_access_manifest NO FORCE ROW LEVEL SECURITY;
ALTER TABLE data_access_manifest DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON authorization_decision;
ALTER TABLE authorization_decision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE authorization_decision DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON authorization_policy_snapshot;
ALTER TABLE authorization_policy_snapshot NO FORCE ROW LEVEL SECURITY;
ALTER TABLE authorization_policy_snapshot DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON delegation_grant;
ALTER TABLE delegation_grant NO FORCE ROW LEVEL SECURITY;
ALTER TABLE delegation_grant DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON authentication_session;
ALTER TABLE authentication_session NO FORCE ROW LEVEL SECURITY;
ALTER TABLE authentication_session DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON authority_binding;
ALTER TABLE authority_binding NO FORCE ROW LEVEL SECURITY;
ALTER TABLE authority_binding DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON authority_source;
ALTER TABLE authority_source NO FORCE ROW LEVEL SECURITY;
ALTER TABLE authority_source DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON principal;
ALTER TABLE principal NO FORCE ROW LEVEL SECURITY;
ALTER TABLE principal DISABLE ROW LEVEL SECURITY;
DROP TABLE governance_decision_bundle;
DROP TABLE evidence_manifest;
DROP TABLE evidence_artifact;
DROP TABLE obligation_binding;
DROP TABLE obligation;
DROP TABLE rule_evaluation;
DROP TABLE legal_rule_pack;
DROP TABLE jurisdiction;
DROP TABLE data_access_manifest;
DROP TABLE authorization_decision;
DROP TABLE authorization_policy_snapshot;
DROP TABLE delegation_grant;
DROP TABLE authentication_session;
DROP TABLE authority_binding;
DROP TABLE authority_source;
DROP TABLE principal;
