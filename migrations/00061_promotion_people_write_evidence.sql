-- Owner: promotion domain lane. Phase: P1B.
-- storage-disposition: people_promotion_write_evidence | authoritative append-only evidence | local PostgreSQL | Promotion local ACID commit | tenant-scoped.

-- +goose Up

CREATE TABLE people_promotion_write_evidence (
    tenant_id              tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    write_id               uuid NOT NULL,
    proposal_revision_id   text NOT NULL,
    proposal_digest        text NOT NULL,
    actor_principal_id     text NOT NULL,
    authority_decision     text NOT NULL,
    worker_id              text NOT NULL,
    assignment_id          text NOT NULL,
    field_path             text NOT NULL,
    current_value          text NOT NULL,
    proposed_value         text NOT NULL,
    expected_revision      text NOT NULL,
    effective_from         timestamptz NOT NULL,
    effective_to           timestamptz,
    recorded_at             timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, write_id),
    CONSTRAINT people_promotion_write_field_allowed CHECK (field_path IN ('assignment.job_code', 'assignment.grade')),
    CONSTRAINT people_promotion_write_window_half_open CHECK (effective_to IS NULL OR effective_from < effective_to)
);

CREATE INDEX people_promotion_write_proposal ON people_promotion_write_evidence (tenant_id, proposal_revision_id);
CREATE TRIGGER people_promotion_write_append_only
    BEFORE UPDATE OR DELETE ON people_promotion_write_evidence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE people_promotion_write_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE people_promotion_write_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON people_promotion_write_evidence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
REVOKE UPDATE, DELETE ON people_promotion_write_evidence FROM PUBLIC;
GRANT SELECT, INSERT ON people_promotion_write_evidence TO hcmnext_app;

-- +goose Down
REVOKE ALL ON people_promotion_write_evidence FROM hcmnext_app;
DROP POLICY tenant_isolation ON people_promotion_write_evidence;
ALTER TABLE people_promotion_write_evidence NO FORCE ROW LEVEL SECURITY;
ALTER TABLE people_promotion_write_evidence DISABLE ROW LEVEL SECURITY;
DROP TABLE people_promotion_write_evidence;
