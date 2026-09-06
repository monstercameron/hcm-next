-- Owner: promotion domain lane. Phase: P1B.
-- storage-disposition: promotion_invariant_evaluation | authoritative append-only conformance evidence | local PostgreSQL | Promotion local ACID commit | tenant-scoped.

-- +goose Up

CREATE TABLE promotion_invariant_evaluation (
    tenant_id              tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    evaluation_id          uuid NOT NULL,
    plan_id                text NOT NULL,
    proposal_revision_id   text NOT NULL,
    proposal_digest        text NOT NULL,
    status                 text NOT NULL,
    invariant_versions     jsonb NOT NULL,
    findings               jsonb NOT NULL,
    recorded_at             timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, evaluation_id),
    CONSTRAINT promotion_invariant_status_allowed CHECK (status IN ('PASS', 'FAIL', 'UNKNOWN')),
    CONSTRAINT promotion_invariant_versions_array CHECK (jsonb_typeof(invariant_versions) = 'array'),
    CONSTRAINT promotion_invariant_findings_array CHECK (jsonb_typeof(findings) = 'array')
);

CREATE INDEX promotion_invariant_plan ON promotion_invariant_evaluation (tenant_id, plan_id);
CREATE TRIGGER promotion_invariant_append_only
    BEFORE UPDATE OR DELETE ON promotion_invariant_evaluation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE promotion_invariant_evaluation ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_invariant_evaluation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_invariant_evaluation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON promotion_invariant_evaluation FROM PUBLIC;
GRANT SELECT, INSERT ON promotion_invariant_evaluation TO hcmnext_app;

-- +goose Down
REVOKE ALL ON promotion_invariant_evaluation FROM hcmnext_app;
DROP POLICY tenant_isolation ON promotion_invariant_evaluation;
ALTER TABLE promotion_invariant_evaluation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE promotion_invariant_evaluation DISABLE ROW LEVEL SECURITY;
DROP TABLE promotion_invariant_evaluation;
