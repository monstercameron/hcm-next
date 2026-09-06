-- Owner: payment-method data plane. Phase: 4.
-- PERSIST-PAYMETHOD-001: governed payment destinations, bank-detail change
-- controls, verification evidence and settlement payment instructions.
-- Raw account and routing data never has a column in this table set.

-- +goose Up

CREATE TABLE IF NOT EXISTS paymethod_destination (
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid          NOT NULL,
    destination_id     text          NOT NULL,
    worker_ref         uuid          NOT NULL,
    rail               text          NOT NULL,
    risk_class         text,
    governed_ref       text,
    provider_ref       text,
    bank_detail_ref    text,
    token_ref          text,
    verification_state text          NOT NULL,
    effective_from     timestamptz,
    effective_to       timestamptz,
    state              text          NOT NULL,
    revision           cas_version   NOT NULL,
    supersedes_revision cas_version,
    supersedes_digest  content_digest,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT paymethod_destination_identity
        UNIQUE (tenant_id, destination_id, revision),
    CONSTRAINT paymethod_destination_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT paymethod_destination_supersedes_pair
        CHECK ((supersedes_revision IS NULL) = (supersedes_digest IS NULL)),
    CONSTRAINT paymethod_destination_effective_order
        CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from)
);

CREATE TABLE IF NOT EXISTS paymethod_bank_detail_change (
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid          NOT NULL,
    change_id          text          NOT NULL,
    destination_id     text          NOT NULL,
    worker_ref         uuid          NOT NULL,
    before_digest      content_digest,
    after_digest       content_digest,
    requested_by       text,
    approver           text,
    requested_at       timestamptz,
    confirmed_at       timestamptz,
    available_at       timestamptz,
    cooling_off        interval      NOT NULL,
    status             text          NOT NULL,
    revision           cas_version   NOT NULL,
    supersedes_revision cas_version,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT paymethod_bank_detail_change_identity
        UNIQUE (tenant_id, change_id, revision),
    CONSTRAINT paymethod_bank_detail_change_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT paymethod_bank_detail_change_cooling_nonnegative
        CHECK (cooling_off >= interval '0 seconds'),
    CONSTRAINT paymethod_bank_detail_change_supersedes_positive
        CHECK (supersedes_revision IS NULL OR supersedes_revision < revision),
    CONSTRAINT paymethod_bank_detail_change_availability_order
        CHECK (available_at IS NULL OR confirmed_at IS NULL OR available_at >= confirmed_at)
);

CREATE TABLE IF NOT EXISTS paymethod_verification_event (
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid          NOT NULL,
    challenge_id       text          NOT NULL,
    destination_id     text          NOT NULL,
    method             text          NOT NULL,
    attempt            integer       NOT NULL,
    verified_at        timestamptz,
    evidence_digest    content_digest,
    canonical_digest   content_digest NOT NULL,
    event_sequence     bigint        NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT paymethod_verification_event_identity
        UNIQUE (tenant_id, destination_id, event_sequence),
    CONSTRAINT paymethod_verification_event_attempt_positive
        CHECK (attempt >= 1),
    CONSTRAINT paymethod_verification_event_sequence_positive
        CHECK (event_sequence >= 1)
);

CREATE OR REPLACE TRIGGER paymethod_verification_event_append_only
    BEFORE UPDATE OR DELETE ON paymethod_verification_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON paymethod_verification_event FROM PUBLIC;

CREATE TABLE IF NOT EXISTS paymethod_change_confirmation (
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid          NOT NULL,
    change_digest      content_digest NOT NULL,
    destination_id     text          NOT NULL,
    dispatched_at      timestamptz   NOT NULL,
    event_sequence     bigint        NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT paymethod_change_confirmation_identity
        UNIQUE (tenant_id, destination_id, event_sequence),
    CONSTRAINT paymethod_change_confirmation_sequence_positive
        CHECK (event_sequence >= 1)
);

CREATE OR REPLACE TRIGGER paymethod_change_confirmation_append_only
    BEFORE UPDATE OR DELETE ON paymethod_change_confirmation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON paymethod_change_confirmation FROM PUBLIC;

CREATE TABLE IF NOT EXISTS paymethod_protected_account (
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid          NOT NULL,
    account_id         text          NOT NULL,
    mode               text          NOT NULL,
    opaque_reference   text,
    value_digest       content_digest,

    PRIMARY KEY (tenant_id, row_id)
);

CREATE TABLE IF NOT EXISTS paymethod_account_validation (
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid          NOT NULL,
    account_id         text          NOT NULL,
    method             text          NOT NULL,
    validated_at       timestamptz   NOT NULL,
    result             text          NOT NULL,
    revision           cas_version   NOT NULL,
    supersedes_digest  content_digest,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT paymethod_account_validation_identity
        UNIQUE (tenant_id, account_id, revision),
    CONSTRAINT paymethod_account_validation_revision_positive
        CHECK (revision >= 1)
);

CREATE TABLE IF NOT EXISTS settlement_payment_instruction (
    tenant_id          tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid          NOT NULL,
    instruction_id     text          NOT NULL,
    payroll_run_ref    text,
    payee_ref          uuid          NOT NULL,
    amount             numeric(19,4) NOT NULL,
    currency           text          NOT NULL,
    funding_source_ref text,
    rail               text          NOT NULL,
    bank_detail_ref    text,
    schedule_ref       text,
    value_date         date,
    state              text          NOT NULL,
    revision           cas_version   NOT NULL,
    supersedes_revision cas_version,
    evidence_ref       text,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT settlement_payment_instruction_identity
        UNIQUE (tenant_id, instruction_id, revision),
    CONSTRAINT settlement_payment_instruction_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT settlement_payment_instruction_amount_positive
        CHECK (amount > 0),
    CONSTRAINT settlement_payment_instruction_supersedes_order
        CHECK (supersedes_revision IS NULL OR supersedes_revision < revision)
);

-- Every table is fail-closed tenant data. The application role can append and
-- read, but cannot rewrite or delete a governed record.
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'paymethod_destination',
        'paymethod_bank_detail_change',
        'paymethod_verification_event',
        'paymethod_change_confirmation',
        'paymethod_protected_account',
        'paymethod_account_validation',
        'settlement_payment_instruction'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I '
            'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
            'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
            target);
        EXECUTE format('REVOKE UPDATE, DELETE ON %I FROM PUBLIC', target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON
    paymethod_destination,
    paymethod_bank_detail_change,
    paymethod_verification_event,
    paymethod_change_confirmation,
    paymethod_protected_account,
    paymethod_account_validation,
    settlement_payment_instruction
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON
    settlement_payment_instruction,
    paymethod_account_validation,
    paymethod_protected_account,
    paymethod_change_confirmation,
    paymethod_verification_event,
    paymethod_bank_detail_change,
    paymethod_destination
FROM hcmnext_app;

DROP POLICY tenant_isolation ON settlement_payment_instruction;
ALTER TABLE settlement_payment_instruction NO FORCE ROW LEVEL SECURITY;
ALTER TABLE settlement_payment_instruction DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON paymethod_account_validation;
ALTER TABLE paymethod_account_validation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE paymethod_account_validation DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON paymethod_protected_account;
ALTER TABLE paymethod_protected_account NO FORCE ROW LEVEL SECURITY;
ALTER TABLE paymethod_protected_account DISABLE ROW LEVEL SECURITY;
DROP TRIGGER paymethod_change_confirmation_append_only ON paymethod_change_confirmation;
DROP POLICY tenant_isolation ON paymethod_change_confirmation;
ALTER TABLE paymethod_change_confirmation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE paymethod_change_confirmation DISABLE ROW LEVEL SECURITY;
DROP TRIGGER paymethod_verification_event_append_only ON paymethod_verification_event;
DROP POLICY tenant_isolation ON paymethod_verification_event;
ALTER TABLE paymethod_verification_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE paymethod_verification_event DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON paymethod_bank_detail_change;
ALTER TABLE paymethod_bank_detail_change NO FORCE ROW LEVEL SECURITY;
ALTER TABLE paymethod_bank_detail_change DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON paymethod_destination;
ALTER TABLE paymethod_destination NO FORCE ROW LEVEL SECURITY;
ALTER TABLE paymethod_destination DISABLE ROW LEVEL SECURITY;

DROP TABLE settlement_payment_instruction;
DROP TABLE paymethod_account_validation;
DROP TABLE paymethod_protected_account;
DROP TABLE paymethod_change_confirmation;
DROP TABLE paymethod_verification_event;
DROP TABLE paymethod_bank_detail_change;
DROP TABLE paymethod_destination;
