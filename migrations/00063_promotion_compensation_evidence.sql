-- Owner: promotion domain lane. Phase: P1B.
-- storage-disposition: promotion_base_pay_evidence | authoritative append-only evidence | local PostgreSQL | Promotion local ACID commit | tenant-scoped.

-- +goose Up

CREATE TABLE promotion_base_pay_evidence (
    tenant_id              tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    write_id               uuid NOT NULL,
    proposal_revision_id   text NOT NULL,
    proposal_digest        text NOT NULL,
    actor_principal_id     text NOT NULL,
    authority_decision     text NOT NULL,
    worker_id              text NOT NULL,
    package_id             text NOT NULL,
    component_id           text NOT NULL,
    current_amount         numeric(38, 12) NOT NULL,
    proposed_amount        numeric(38, 12) NOT NULL,
    currency               char(3) NOT NULL,
    expected_revision      text NOT NULL,
    effective_from         timestamptz NOT NULL,
    effective_to           timestamptz,
    recorded_at             timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, write_id),
    CONSTRAINT promotion_base_pay_currency_upper CHECK (currency = upper(currency)),
    CONSTRAINT promotion_base_pay_window_half_open CHECK (effective_to IS NULL OR effective_from < effective_to)
);

CREATE INDEX promotion_base_pay_proposal ON promotion_base_pay_evidence (tenant_id, proposal_revision_id);
CREATE TRIGGER promotion_base_pay_append_only
    BEFORE UPDATE OR DELETE ON promotion_base_pay_evidence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE promotion_base_pay_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_base_pay_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_base_pay_evidence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON promotion_base_pay_evidence FROM PUBLIC;
GRANT SELECT, INSERT ON promotion_base_pay_evidence TO hcmnext_app;

-- +goose Down
REVOKE ALL ON promotion_base_pay_evidence FROM hcmnext_app;
DROP POLICY tenant_isolation ON promotion_base_pay_evidence;
ALTER TABLE promotion_base_pay_evidence NO FORCE ROW LEVEL SECURITY;
ALTER TABLE promotion_base_pay_evidence DISABLE ROW LEVEL SECURITY;
DROP TABLE promotion_base_pay_evidence;
