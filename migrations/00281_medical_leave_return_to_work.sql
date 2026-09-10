-- Owner: leave lane. Phase: P1B.
-- DB-023: materialize the Medical Leave and Return-to-Work domain state.
--
-- data/models/rewards-payroll-workforce.md "Leave models" declares the Leave
-- entities; this migration makes the Phase 2 subset physical. Every revision
-- table is append-only evidence (SELECT/INSERT only, with the forbid_mutation
-- trigger 00001 declares): a mutable leave, eligibility or entitlement
-- revision cannot commit because no UPDATE path exists at all.
--
-- Common intent/workflow/ledger/integration rows stay in their existing
-- owners; these twelve tables add only Leave-domain authority and typed
-- references back to them (proposal digests, intent digests, balance
-- accounts, availability intervals).
--
-- # Columns the RED clause names
--
--   * employment_state is CHECKed to ACTIVE on every leave row: a leave
--     transition that inactivates Employment cannot commit.
--   * leave_event_id is UNIQUE per tenant on availability revisions and
--     idempotency keys are UNIQUE per tenant on requests and balance
--     postings: duplicate leave/return effects and double-posted balance
--     have nowhere to land. Overlapping incompatible active leave is
--     refused by the store under lock (interval overlap is not expressible
--     in a UNIQUE index without a gist extension this plane does not take).
--   * amounts are numeric without a typmod, preserving the kernel's full
--     bounded decimal precision: float has nowhere to land.
--   * medical detail lives in its own RESTRICTED compartment table; the
--     store is the sole reader path and gates it by role.
--   * every child table carries a composite (tenant, record) FOREIGN KEY to
--     leave_record: orphan child intent, evidence or obligation rows cannot
--     commit.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied from migrations/00017_work_item.sql
-- and, through it, from migrations/00008_tenant_isolation.sql.

-- +goose Up

CREATE TABLE IF NOT EXISTS leave_request (
    tenant_id        tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    request_id       uuid         NOT NULL,
    revision         cas_version  NOT NULL DEFAULT 1,
    state            text         NOT NULL,
    employment_state text         NOT NULL DEFAULT 'ACTIVE',
    proposal_digest  text         NOT NULL,
    idempotency_key  semantic_key NOT NULL,
    PRIMARY KEY (tenant_id, request_id, revision),
    CONSTRAINT leave_request_state_check
        CHECK (state IN ('DRAFT','SUBMITTED','ELIGIBLE','PLANNED','APPROVED','ACTIVE','CLOSED','CANCELLED')),
    CONSTRAINT leave_request_employment_check
        CHECK (employment_state = 'ACTIVE'),
    CONSTRAINT leave_request_proposal_check
        CHECK (length(proposal_digest) > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS leave_request_idempotency_unique
    ON leave_request (tenant_id, idempotency_key);

CREATE TABLE IF NOT EXISTS leave_record (
    tenant_id        tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    record_id        uuid         NOT NULL,
    request_id       uuid         NOT NULL,
    request_revision cas_version  NOT NULL,
    revision         cas_version  NOT NULL DEFAULT 1,
    state            text         NOT NULL,
    employment_state text         NOT NULL DEFAULT 'ACTIVE',
    proposal_digest  text         NOT NULL,
    PRIMARY KEY (tenant_id, record_id, revision),
    CONSTRAINT leave_record_request_fk
        FOREIGN KEY (tenant_id, request_id, request_revision)
        REFERENCES leave_request (tenant_id, request_id, revision),
    CONSTRAINT leave_record_state_check
        CHECK (state IN ('OPEN','ELIGIBLE','PLANNED','SIMULATED','APPROVED','LEAVE_ACTIVE','RETURNED','CLOSED','CANCELLED')),
    CONSTRAINT leave_record_employment_check
        CHECK (employment_state = 'ACTIVE')
);

CREATE TABLE IF NOT EXISTS leave_program_eligibility (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    program_id       semantic_key NOT NULL,
    authority        text         NOT NULL,
    result           text         NOT NULL,
    rule_id          semantic_key NOT NULL,
    rule_version     text         NOT NULL,
    digest           text         NOT NULL,
    PRIMARY KEY (tenant_id, record_id, program_id),
    CONSTRAINT leave_program_eligibility_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision),
    CONSTRAINT leave_program_eligibility_result_check
        CHECK (result IN ('ELIGIBLE','INELIGIBLE','CONDITIONAL','UNKNOWN'))
);

CREATE TABLE IF NOT EXISTS leave_entitlement_segment (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    start_day        integer      NOT NULL,
    end_day          integer      NOT NULL,
    kind             text         NOT NULL,
    hours            integer      NOT NULL,
    programs         text[]       NOT NULL,
    PRIMARY KEY (tenant_id, record_id, start_day),
    CONSTRAINT leave_entitlement_segment_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision),
    CONSTRAINT leave_entitlement_segment_interval_check
        CHECK (end_day >= start_day AND hours >= 0),
    CONSTRAINT leave_entitlement_segment_kind_check
        CHECK (kind IN ('protected','paid','unpaid','unknown'))
);

CREATE TABLE IF NOT EXISTS leave_absence_link (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    absence_id       semantic_key NOT NULL,
    PRIMARY KEY (tenant_id, record_id, absence_id),
    CONSTRAINT leave_absence_link_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision)
);

CREATE TABLE IF NOT EXISTS leave_availability_revision (
    tenant_id        tenant_ref   NOT NULL,
    revision_id      uuid         NOT NULL,
    worker_ref       semantic_key NOT NULL,
    prior_revision   uuid,
    state            text         NOT NULL,
    restored         boolean      NOT NULL DEFAULT false,
    intent_ref       semantic_key NOT NULL,
    proposal_digest  text         NOT NULL,
    reason           text         NOT NULL,
    leave_event_id   semantic_key NOT NULL,
    effective_start  integer      NOT NULL,
    effective_end    integer      NOT NULL,
    digest           text         NOT NULL,
    PRIMARY KEY (tenant_id, revision_id),
    CONSTRAINT leave_availability_revision_interval_check
        CHECK (effective_end >= effective_start),
    CONSTRAINT leave_availability_revision_state_check
        CHECK (state IN ('UNAVAILABLE','RESTRICTED','AVAILABLE')),
    CONSTRAINT leave_availability_revision_restore_check
        CHECK ((state = 'AVAILABLE' AND restored) OR state IN ('UNAVAILABLE','RESTRICTED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS leave_availability_event_unique
    ON leave_availability_revision (tenant_id, leave_event_id);

CREATE TABLE IF NOT EXISTS leave_balance_posting (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    account_ref      semantic_key NOT NULL,
    opening          numeric      NOT NULL,
    ending           numeric      NOT NULL,
    remainder        numeric      NOT NULL,
    receipt_digest   text         NOT NULL,
    idempotency_key  semantic_key NOT NULL,
    PRIMARY KEY (tenant_id, record_id, idempotency_key),
    CONSTRAINT leave_balance_posting_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision)
);

CREATE TABLE IF NOT EXISTS leave_evidence_ref (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    ref              text         NOT NULL,
    compartment      text         NOT NULL,
    quarantined      boolean      NOT NULL,
    classified       boolean      NOT NULL,
    authority_current boolean     NOT NULL,
    PRIMARY KEY (tenant_id, record_id, ref),
    CONSTRAINT leave_evidence_ref_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision)
);

CREATE TABLE IF NOT EXISTS leave_work_restriction (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    restriction      text         NOT NULL,
    readiness_ref    text         NOT NULL,
    PRIMARY KEY (tenant_id, record_id, restriction),
    CONSTRAINT leave_work_restriction_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision)
);

CREATE TABLE IF NOT EXISTS leave_obligation (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    obligation       text         NOT NULL,
    state            text         NOT NULL,
    PRIMARY KEY (tenant_id, record_id, obligation),
    CONSTRAINT leave_obligation_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision),
    CONSTRAINT leave_obligation_state_check
        CHECK (state IN ('OPEN','SATISFIED','WAIVED','OVERDUE','BLOCKED'))
);

CREATE TABLE IF NOT EXISTS leave_intent_link (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    intent_digest    text         NOT NULL,
    kind             text         NOT NULL,
    PRIMARY KEY (tenant_id, record_id, intent_digest),
    CONSTRAINT leave_intent_link_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision),
    CONSTRAINT leave_intent_link_kind_check
        CHECK (kind IN ('child','follow-up','trigger','correction'))
);

CREATE TABLE IF NOT EXISTS leave_medical_detail (
    tenant_id        tenant_ref   NOT NULL,
    record_id        uuid         NOT NULL,
    record_revision  cas_version  NOT NULL,
    detail_ref       text         NOT NULL,
    compartment      text         NOT NULL DEFAULT 'RESTRICTED',
    PRIMARY KEY (tenant_id, record_id, detail_ref),
    CONSTRAINT leave_medical_detail_record_fk
        FOREIGN KEY (tenant_id, record_id, record_revision)
        REFERENCES leave_record (tenant_id, record_id, revision),
    CONSTRAINT leave_medical_detail_compartment_check
        CHECK (compartment = 'RESTRICTED')
);

-- All twelve tables are append-only evidence: SELECT/INSERT only, with the
-- forbid_mutation trigger 00001 declares. Serving-state transitions are new
-- revision rows, never UPDATEs.
CREATE TRIGGER leave_request_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_request
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_record_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_program_eligibility_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_program_eligibility
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_entitlement_segment_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_entitlement_segment
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_absence_link_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_absence_link
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_availability_revision_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_availability_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_balance_posting_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_balance_posting
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_evidence_ref_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_evidence_ref
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_work_restriction_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_work_restriction
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_obligation_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_obligation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_intent_link_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_intent_link
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER leave_medical_detail_no_rewrite
    BEFORE UPDATE OR DELETE ON leave_medical_detail
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE leave_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_request
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_program_eligibility ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_program_eligibility FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_program_eligibility
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_entitlement_segment ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_entitlement_segment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_entitlement_segment
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_absence_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_absence_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_absence_link
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_availability_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_availability_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_availability_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_balance_posting ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_balance_posting FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_balance_posting
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_evidence_ref ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_evidence_ref FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_evidence_ref
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_work_restriction ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_work_restriction FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_work_restriction
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_obligation ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_obligation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_obligation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_intent_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_intent_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_intent_link
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE leave_medical_detail ENABLE ROW LEVEL SECURITY;
ALTER TABLE leave_medical_detail FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON leave_medical_detail
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE UPDATE, DELETE ON leave_request FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_record FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_program_eligibility FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_entitlement_segment FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_absence_link FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_availability_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_balance_posting FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_evidence_ref FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_work_restriction FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_obligation FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_intent_link FROM PUBLIC;
REVOKE UPDATE, DELETE ON leave_medical_detail FROM PUBLIC;
GRANT SELECT, INSERT ON leave_request TO hcmnext_app;
GRANT SELECT, INSERT ON leave_record TO hcmnext_app;
GRANT SELECT, INSERT ON leave_program_eligibility TO hcmnext_app;
GRANT SELECT, INSERT ON leave_entitlement_segment TO hcmnext_app;
GRANT SELECT, INSERT ON leave_absence_link TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON leave_availability_revision TO hcmnext_app;
GRANT SELECT, INSERT ON leave_balance_posting TO hcmnext_app;
GRANT SELECT, INSERT ON leave_evidence_ref TO hcmnext_app;
GRANT SELECT, INSERT ON leave_work_restriction TO hcmnext_app;
GRANT SELECT, INSERT ON leave_obligation TO hcmnext_app;
GRANT SELECT, INSERT ON leave_intent_link TO hcmnext_app;
GRANT SELECT, INSERT ON leave_medical_detail TO hcmnext_app;

-- +goose Down
REVOKE ALL ON leave_medical_detail FROM hcmnext_app;
REVOKE ALL ON leave_intent_link FROM hcmnext_app;
REVOKE ALL ON leave_obligation FROM hcmnext_app;
REVOKE ALL ON leave_work_restriction FROM hcmnext_app;
REVOKE ALL ON leave_evidence_ref FROM hcmnext_app;
REVOKE ALL ON leave_balance_posting FROM hcmnext_app;
REVOKE ALL ON leave_availability_revision FROM hcmnext_app;
REVOKE ALL ON leave_absence_link FROM hcmnext_app;
REVOKE ALL ON leave_entitlement_segment FROM hcmnext_app;
REVOKE ALL ON leave_program_eligibility FROM hcmnext_app;
REVOKE ALL ON leave_record FROM hcmnext_app;
REVOKE ALL ON leave_request FROM hcmnext_app;
DROP POLICY tenant_isolation ON leave_medical_detail;
DROP POLICY tenant_isolation ON leave_intent_link;
DROP POLICY tenant_isolation ON leave_obligation;
DROP POLICY tenant_isolation ON leave_work_restriction;
DROP POLICY tenant_isolation ON leave_evidence_ref;
DROP POLICY tenant_isolation ON leave_balance_posting;
DROP POLICY tenant_isolation ON leave_availability_revision;
DROP POLICY tenant_isolation ON leave_absence_link;
DROP POLICY tenant_isolation ON leave_entitlement_segment;
DROP POLICY tenant_isolation ON leave_program_eligibility;
DROP POLICY tenant_isolation ON leave_record;
DROP POLICY tenant_isolation ON leave_request;
DROP TRIGGER leave_request_no_rewrite ON leave_request;
DROP TRIGGER leave_record_no_rewrite ON leave_record;
DROP TRIGGER leave_program_eligibility_no_rewrite ON leave_program_eligibility;
DROP TRIGGER leave_entitlement_segment_no_rewrite ON leave_entitlement_segment;
DROP TRIGGER leave_absence_link_no_rewrite ON leave_absence_link;
DROP TRIGGER leave_availability_revision_no_rewrite ON leave_availability_revision;
DROP TRIGGER leave_balance_posting_no_rewrite ON leave_balance_posting;
DROP TRIGGER leave_evidence_ref_no_rewrite ON leave_evidence_ref;
DROP TRIGGER leave_work_restriction_no_rewrite ON leave_work_restriction;
DROP TRIGGER leave_obligation_no_rewrite ON leave_obligation;
DROP TRIGGER leave_intent_link_no_rewrite ON leave_intent_link;
DROP TRIGGER leave_medical_detail_no_rewrite ON leave_medical_detail;
DROP TABLE leave_medical_detail;
DROP TABLE leave_intent_link;
DROP TABLE leave_obligation;
DROP TABLE leave_work_restriction;
DROP TABLE leave_evidence_ref;
DROP TABLE leave_balance_posting;
DROP TABLE leave_availability_revision;
DROP TABLE leave_absence_link;
DROP TABLE leave_entitlement_segment;
DROP TABLE leave_program_eligibility;
DROP TABLE leave_record;
DROP TABLE leave_request;
