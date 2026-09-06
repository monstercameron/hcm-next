-- Owner: position data plane. Phase: P1A.
-- PERSIST-POSITION-001: pre-commit position reservations and their transition log.
--
-- A reservation is deliberately separate from DB-009's position_occupancy:
-- occupancy is committed assignment state, while this table is the fenced
-- hold a proposal owns before commit. A reservation becomes occupancy only
-- through DB-009's existing write path; this migration never mutates it.
--
-- Storage disposition:
-- position_reservation: CONTROL, OPERATIONAL, tenant_id, mutable, rebuild_source none.
-- position_reservation_event: LEDGER, PERMANENT, tenant_id, append-only, rebuild_source none.

-- +goose Up

CREATE TABLE IF NOT EXISTS position_reservation (
    row_id                 uuid         NOT NULL,
    tenant_id              tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    reservation_id         semantic_key NOT NULL,
    request                jsonb        NOT NULL,
    position_revision_ref  uuid,
    state                  text         NOT NULL,
    fence                  bigint       NOT NULL DEFAULT 0,
    created_at             timestamptz  NOT NULL DEFAULT now(),
    updated_at             timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT position_reservation_id_unique UNIQUE (tenant_id, reservation_id),
    CONSTRAINT position_reservation_state_allowed CHECK (
        state IN ('HELD', 'RELEASED', 'EXPIRED')
    ),
    CONSTRAINT position_reservation_fence_nonnegative CHECK (fence >= 0)
);

-- A live-state rewrite is valid only when it advances the fence by exactly
-- one. The application also uses this compare-and-set predicate; the trigger
-- closes the same gap for direct SQL callers.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION position_reservation_require_next_fence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.fence <> OLD.fence + 1 THEN
        RAISE EXCEPTION 'position_reservation fence must advance by exactly one'
            USING ERRCODE = '40001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER position_reservation_fence_trigger
    BEFORE UPDATE ON position_reservation
    FOR EACH ROW EXECUTE FUNCTION position_reservation_require_next_fence();

CREATE INDEX IF NOT EXISTS position_reservation_state
    ON position_reservation (tenant_id, state, updated_at);

CREATE TABLE IF NOT EXISTS position_reservation_event (
    row_id         uuid         NOT NULL,
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    sequence       bigint       NOT NULL,
    reservation_id semantic_key NOT NULL,
    from_state     text,
    to_state       text         NOT NULL,
    reason         text,
    fence          bigint       NOT NULL,
    at             timestamptz  NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT position_reservation_event_sequence_unique
        UNIQUE (tenant_id, reservation_id, sequence),
    CONSTRAINT position_reservation_event_sequence_positive CHECK (sequence >= 1),
    CONSTRAINT position_reservation_event_fence_nonnegative CHECK (fence >= 0),
    CONSTRAINT position_reservation_event_to_state_allowed CHECK (
        to_state IN ('HELD', 'RELEASED', 'EXPIRED')
    ),
    CONSTRAINT position_reservation_event_from_state_allowed CHECK (
        from_state IS NULL OR from_state IN ('HELD', 'RELEASED', 'EXPIRED')
    ),
    CONSTRAINT position_reservation_event_reservation_fk
        FOREIGN KEY (tenant_id, reservation_id)
        REFERENCES position_reservation (tenant_id, reservation_id)
);

CREATE INDEX IF NOT EXISTS position_reservation_event_order
    ON position_reservation_event (tenant_id, reservation_id, sequence);

CREATE OR REPLACE TRIGGER position_reservation_event_append_only
    BEFORE UPDATE OR DELETE ON position_reservation_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE DELETE ON position_reservation FROM PUBLIC;
REVOKE UPDATE, DELETE ON position_reservation_event FROM PUBLIC;

ALTER TABLE position_reservation ENABLE ROW LEVEL SECURITY;
ALTER TABLE position_reservation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON position_reservation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE position_reservation_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE position_reservation_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON position_reservation_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON position_reservation TO hcmnext_app;
GRANT SELECT, INSERT ON position_reservation_event TO hcmnext_app;

-- +goose Down
REVOKE ALL ON position_reservation_event FROM hcmnext_app;
REVOKE ALL ON position_reservation FROM hcmnext_app;

DROP POLICY tenant_isolation ON position_reservation_event;
ALTER TABLE position_reservation_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE position_reservation_event DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON position_reservation;
ALTER TABLE position_reservation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE position_reservation DISABLE ROW LEVEL SECURITY;

DROP TRIGGER position_reservation_event_append_only ON position_reservation_event;
DROP INDEX IF EXISTS position_reservation_event_order;
DROP TABLE position_reservation_event;

DROP TRIGGER position_reservation_fence_trigger ON position_reservation;
DROP FUNCTION position_reservation_require_next_fence();
DROP INDEX IF EXISTS position_reservation_state;
DROP TABLE position_reservation;
