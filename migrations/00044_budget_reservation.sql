-- Owner: budget data lane. Phase: P3.
-- PERSIST-BUDGET-001: durable compensation-reservation state and transition log.
--
-- These tables deliberately do not reuse DB-010's budget_reservation from
-- migration 00013. That table is a bitemporal budget-capacity aggregate keyed
-- by budget_ref/proposal; these tables are the fenced compensation-reservation
-- state machine used by the promotion budget domain.
--
-- Storage disposition:
--   compensation_reservation: CONTROL, OPERATIONAL, mutable CAS state.
--   reservation_event: LEDGER, PERMANENT, append-only transition evidence.

-- +goose Up

CREATE TABLE IF NOT EXISTS compensation_reservation (
    tenant_id        tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid          NOT NULL,
    budget_id        text          NOT NULL,
    proposal_digest  content_digest,
    amount           numeric(19,4) NOT NULL,
    currency         text          NOT NULL,
    authority_digest content_digest,
    idempotency_key  text          NOT NULL,
    expires_at       timestamptz,
    state            text          NOT NULL,
    fence            bigint        NOT NULL DEFAULT 0,
    created_at       timestamptz   NOT NULL DEFAULT now(),
    updated_at       timestamptz   NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT compensation_reservation_amount_positive CHECK (amount > 0),
    CONSTRAINT compensation_reservation_currency_iso CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT compensation_reservation_state_allowed CHECK (
        state IN ('REQUESTED', 'HELD', 'COMMITTED', 'RELEASED', 'EXPIRED',
                  'RECONCILIATION_REQUIRED')
    ),
    CONSTRAINT compensation_reservation_fence_non_negative CHECK (fence >= 0)
);

CREATE INDEX IF NOT EXISTS compensation_reservation_budget_state
    ON compensation_reservation (tenant_id, budget_id, state);

ALTER TABLE compensation_reservation ENABLE ROW LEVEL SECURITY;
ALTER TABLE compensation_reservation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON compensation_reservation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

REVOKE ALL ON compensation_reservation FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON compensation_reservation TO hcmnext_app;

CREATE TABLE IF NOT EXISTS reservation_event (
    tenant_id      tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id         uuid       NOT NULL,
    reservation_id uuid       NOT NULL,
    sequence       bigint     NOT NULL,
    from_state     text,
    to_state       text       NOT NULL,
    fence          bigint     NOT NULL,
    "at"           timestamptz NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, reservation_id, sequence),
    FOREIGN KEY (tenant_id, reservation_id)
        REFERENCES compensation_reservation (tenant_id, row_id),
    CONSTRAINT reservation_event_sequence_positive CHECK (sequence >= 1),
    CONSTRAINT reservation_event_fence_non_negative CHECK (fence >= 0),
    CONSTRAINT reservation_event_from_state_allowed CHECK (
        from_state IS NULL OR from_state IN (
            'REQUESTED', 'HELD', 'COMMITTED', 'RELEASED', 'EXPIRED',
            'RECONCILIATION_REQUIRED'
        )
    ),
    CONSTRAINT reservation_event_to_state_allowed CHECK (
        to_state IN ('REQUESTED', 'HELD', 'COMMITTED', 'RELEASED', 'EXPIRED',
                     'RECONCILIATION_REQUIRED')
    )
);

CREATE INDEX IF NOT EXISTS reservation_event_reservation_sequence
    ON reservation_event (tenant_id, reservation_id, sequence);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reservation_event_enforce_sequence() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    expected bigint;
BEGIN
    SELECT COALESCE(MAX(sequence), 0) + 1
      INTO expected
      FROM reservation_event
     WHERE tenant_id = NEW.tenant_id
       AND reservation_id = NEW.reservation_id;
    IF NEW.sequence <> expected THEN
        RAISE EXCEPTION
            'reservation_event % for reservation % expected sequence %, got %',
            NEW.row_id, NEW.reservation_id, expected, NEW.sequence
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER reservation_event_gapless
    BEFORE INSERT ON reservation_event
    FOR EACH ROW EXECUTE FUNCTION reservation_event_enforce_sequence();

CREATE OR REPLACE TRIGGER reservation_event_append_only
    BEFORE UPDATE OR DELETE ON reservation_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE ALL ON reservation_event FROM PUBLIC;
GRANT SELECT, INSERT ON reservation_event TO hcmnext_app;

ALTER TABLE reservation_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE reservation_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON reservation_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- +goose Down

REVOKE ALL ON reservation_event FROM hcmnext_app;
DROP POLICY tenant_isolation ON reservation_event;
ALTER TABLE reservation_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE reservation_event DISABLE ROW LEVEL SECURITY;
DROP TABLE reservation_event;
DROP FUNCTION reservation_event_enforce_sequence();

REVOKE ALL ON compensation_reservation FROM hcmnext_app;
DROP POLICY tenant_isolation ON compensation_reservation;
ALTER TABLE compensation_reservation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE compensation_reservation DISABLE ROW LEVEL SECURITY;
DROP TABLE compensation_reservation;
