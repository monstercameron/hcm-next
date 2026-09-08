-- Owner: legal evidence data plane. Phase: P2. LEGAL-014.
-- Durable immutable receipt and proposal-binding artifacts. Both signatures
-- are retained in the JSON envelope and issuer_key for audit/index purposes;
-- authorization requires the caller's explicit trusted issuer key set.

-- +goose Up
CREATE TABLE IF NOT EXISTS legal_evaluation_receipt (
    tenant_id    tenant_ref NOT NULL REFERENCES tenant(tenant_id),
    receipt_ref  text NOT NULL,
    receipt_digest content_digest NOT NULL,
    receipt      jsonb NOT NULL,
    issuer_key   bytea NOT NULL,
    PRIMARY KEY (tenant_id, receipt_ref),
    UNIQUE (tenant_id, receipt_ref, receipt_digest),
    UNIQUE (tenant_id, receipt_digest)
);
CREATE OR REPLACE TRIGGER legal_evaluation_receipt_append_only
    BEFORE UPDATE OR DELETE ON legal_evaluation_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_evaluation_receipt FROM PUBLIC;

CREATE TABLE IF NOT EXISTS legal_evaluation_binding (
    tenant_id             tenant_ref NOT NULL REFERENCES tenant(tenant_id),
    binding_digest        content_digest NOT NULL,
    receipt_ref           text NOT NULL,
    receipt_digest        content_digest NOT NULL,
    intent_id             text NOT NULL,
    proposal_revision_id  text NOT NULL,
    material_digest       content_digest NOT NULL,
    legal_context_digest  content_digest NOT NULL,
    binding               jsonb NOT NULL,
    issuer_key            bytea NOT NULL,
    PRIMARY KEY (tenant_id, binding_digest),
    UNIQUE (tenant_id, intent_id, proposal_revision_id, material_digest, legal_context_digest),
    FOREIGN KEY (tenant_id, receipt_ref, receipt_digest)
        REFERENCES legal_evaluation_receipt(tenant_id, receipt_ref, receipt_digest)
);
CREATE OR REPLACE TRIGGER legal_evaluation_binding_append_only
    BEFORE UPDATE OR DELETE ON legal_evaluation_binding
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON legal_evaluation_binding FROM PUBLIC;

ALTER TABLE legal_evaluation_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_evaluation_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_evaluation_receipt
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE legal_evaluation_binding ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_evaluation_binding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_evaluation_binding
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON legal_evaluation_receipt, legal_evaluation_binding TO hcmnext_app;

-- +goose Down
REVOKE ALL ON legal_evaluation_binding FROM hcmnext_app;
REVOKE ALL ON legal_evaluation_receipt FROM hcmnext_app;
DROP POLICY tenant_isolation ON legal_evaluation_binding;
ALTER TABLE legal_evaluation_binding NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_evaluation_binding DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON legal_evaluation_receipt;
ALTER TABLE legal_evaluation_receipt NO FORCE ROW LEVEL SECURITY;
ALTER TABLE legal_evaluation_receipt DISABLE ROW LEVEL SECURITY;
DROP TABLE legal_evaluation_binding;
DROP TABLE legal_evaluation_receipt;
