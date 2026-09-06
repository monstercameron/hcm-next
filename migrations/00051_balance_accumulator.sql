-- Owner: data plane. Phase: PERSIST-BALANCE-001.
-- Durable accumulator definitions and the balance ledger. Definitions are
-- immutable revisions; entries are append-only facts and are never projected
-- into a mutable balance column.

-- +goose Up

CREATE TABLE accumulator_definition (
    row_id          uuid          NOT NULL,
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    definition_id   text          NOT NULL,
    version         text          NOT NULL,
    revision        cas_version   NOT NULL,
    name            text          NOT NULL,
    unit            text          NOT NULL,
    currency        text,
    subject         text          NOT NULL,
    period          text          NOT NULL,
    dimensions      jsonb,
    entry_types     jsonb,
    authority       text,
    floor_policy    jsonb,
    cap_policy      jsonb,
    expiry_policy   jsonb,
    rollover_policy jsonb,
    correction_policy jsonb,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT accumulator_definition_identity_unique
        UNIQUE (tenant_id, definition_id, version),
    CONSTRAINT accumulator_definition_revision_unique
        UNIQUE (tenant_id, definition_id, revision)
);

CREATE OR REPLACE TRIGGER accumulator_definition_append_only
    BEFORE UPDATE OR DELETE ON accumulator_definition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE balance_entry (
    row_id              uuid          NOT NULL,
    tenant_id           tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    account_id          text          NOT NULL,
    definition_id       text          NOT NULL,
    definition_version  cas_version   NOT NULL,
    unit                text          NOT NULL,
    currency            text,
    subject             text          NOT NULL,
    period              text          NOT NULL,
    dimensions          jsonb,
    kind                text          NOT NULL,
    amount              numeric(19,4) NOT NULL,
    entry_type          text          NOT NULL,
    source_transaction_id text,
    idempotency_key     text          NOT NULL,
    effective_at        timestamptz,
    recorded_at         timestamptz   NOT NULL DEFAULT now(),
    authorized_at       timestamptz,
    event_sequence      bigint        NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT balance_entry_idempotency_unique
        UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT balance_entry_sequence_unique
        UNIQUE (tenant_id, account_id, event_sequence),
    FOREIGN KEY (tenant_id, definition_id, definition_version)
        REFERENCES accumulator_definition (tenant_id, definition_id, revision),
    CONSTRAINT balance_entry_sequence_positive CHECK (event_sequence >= 1)
);

CREATE INDEX balance_entry_account_order
    ON balance_entry (tenant_id, account_id, event_sequence);

CREATE OR REPLACE TRIGGER balance_entry_append_only
    BEFORE UPDATE OR DELETE ON balance_entry
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE TABLE balance_lifecycle_entry (
    row_id          uuid        NOT NULL,
    tenant_id       tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    entry_ref       uuid        NOT NULL,
    operation       text        NOT NULL,
    reason          text,
    policy_id       text,
    policy_version  cas_version,
    source_period   text,
    target_period   text,
    source_entries  jsonb,
    event_sequence  bigint      NOT NULL,
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT balance_lifecycle_sequence_unique
        UNIQUE (tenant_id, entry_ref, event_sequence),
    FOREIGN KEY (tenant_id, entry_ref)
        REFERENCES balance_entry (tenant_id, row_id),
    CONSTRAINT balance_lifecycle_sequence_positive CHECK (event_sequence >= 1)
);

CREATE OR REPLACE TRIGGER balance_lifecycle_entry_append_only
    BEFORE UPDATE OR DELETE ON balance_lifecycle_entry
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- DB-017: every balance table fails closed without a transaction-local tenant.
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'accumulator_definition', 'balance_entry', 'balance_lifecycle_entry'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I '
            'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
            'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
            target);
        EXECUTE format('REVOKE UPDATE, DELETE ON %I FROM PUBLIC', target);
        EXECUTE format('REVOKE UPDATE, DELETE ON %I FROM hcmnext_app', target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON
    accumulator_definition, balance_entry, balance_lifecycle_entry
TO hcmnext_app;

-- +goose Down
REVOKE ALL ON balance_lifecycle_entry, balance_entry, accumulator_definition FROM hcmnext_app;
DROP TABLE balance_lifecycle_entry;
DROP TABLE balance_entry;
DROP TABLE accumulator_definition;
