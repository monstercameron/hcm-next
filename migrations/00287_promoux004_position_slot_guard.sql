-- PROMOUX-004: admit exactly one active proposal per target position and
-- effective date, at the database, not only in application code that checks
-- before it writes.
--
-- The RED this migration closes: a promotion preflight can prove a target
-- position exists, is vacant, is compatible with the proposed job/org and is
-- effective at the requested date, and still race a second, concurrent
-- proposal for the very same position and date -- both reads can observe
-- "no competing hold" before either write commits. Narrowing that window in
-- application code (as PROMOUX-002's own header explains at length) can
-- never close it; only a constraint the database evaluates as part of the
-- write itself can, because that is the only point two concurrent
-- transactions are forced to serialize against each other.
--
-- Shape of the guarantee: promotion_target_position_guard_one_active_window
-- is a partial UNIQUE index on (tenant_id, position_ref, effective_date)
-- WHERE status = 'ACTIVE'. Two concurrent transactions can both evaluate "is
-- this position already held for this date" and both see none -- that race
-- is real and this schema does not prevent the read from lying. What it
-- prevents is both of their INSERTs succeeding: PostgreSQL evaluates a
-- unique index as part of each INSERT's own commit, so the second
-- transaction to reach that point is refused with unique_violation (23505)
-- regardless of what its own prior read observed.
-- internal/data/positionguard is the one Go package that writes this table,
-- and it never performs a bare SELECT-then-INSERT to decide admission: the
-- INSERT (an INSERT .. ON CONFLICT .. DO UPDATE, so a replay of the same
-- idempotency key resolves to the row already there instead of colliding
-- with it) is itself the decision, and the constraint is what makes that
-- decision safe under real concurrency. See
-- internal/domains/promotion.PositionReservationAdmitter for the domain-side
-- port this table backs (PROMOUX-004's "reservation ownership" ground) and
-- TestTodo_PROMOUX_004_Race for the proof against real concurrent
-- PostgreSQL sessions.
--
-- This is deliberately a distinct table from position_reservation
-- (migrations/00043): that table persists POSITION-003's fenced,
-- append-ledgered hold aggregate and carries no natural-key uniqueness on
-- (position, effective date) at all -- its own conflict handling is keyed on
-- an application-computed reservation_id, which two independently-digested
-- proposals for the same slot would never collide on. Retrofitting that
-- table with this constraint was judged riskier than adding one narrow,
-- purpose-built guard in the same shape PROMOUX-002 already established for
-- exactly this kind of decision, and the two can be reconciled by a later,
-- separately-reviewed change if position_reservation ever needs the same
-- exclusivity.
--
-- Rows are mutable (status transitions ACTIVE -> CLOSED once the guarded
-- proposal reaches a terminal stage or is superseded), so this is not an
-- append-only ledger table and carries no forbid_mutation trigger; UPDATE is
-- granted, DELETE is not, matching promotion_active_intent_guard's shape
-- (migration 00286) rather than ledger_event's.

-- +goose Up

CREATE TABLE promotion_target_position_guard (
    tenant_id        tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    -- guard_id is application-generated, not database-generated: this table
    -- never relies on gen_random_uuid()/pgcrypto.
    guard_id         uuid         NOT NULL,
    -- The canonical entity-reference string a selected position is named by
    -- (values.EntityRef.String()) -- the same text every other cross-domain
    -- reference in this cell uses. Not a foreign key: no single physical
    -- position table is shared by every tenant fixture this cell runs
    -- against today (see migrations/00286's worker_ref for the identical
    -- argument about promotion subjects).
    position_ref     semantic_key NOT NULL,
    effective_date   date         NOT NULL,
    -- The proposal this reservation protects, named by its own stable
    -- reference (never a raw digest or free-text position id) so an
    -- operator can trace which proposal is holding a slot without decoding
    -- the idempotency key.
    proposal_ref     semantic_key NOT NULL,
    -- Ties one reservation to the specific preflight/propose call that
    -- opened it. A second call presenting the same key is a replay of this
    -- same request and resolves to this same row; a call presenting a
    -- different key for the same position and date is a genuinely
    -- different, conflicting proposal.
    idempotency_key  semantic_key NOT NULL,
    status           text         NOT NULL DEFAULT 'ACTIVE',
    opened_at        timestamptz  NOT NULL DEFAULT now(),
    closed_at        timestamptz,
    PRIMARY KEY (tenant_id, guard_id),
    CONSTRAINT promotion_target_position_guard_status_allowed CHECK (
        status IN ('ACTIVE', 'CLOSED')
    ),
    CONSTRAINT promotion_target_position_guard_closed_at_matches_status CHECK (
        (status = 'CLOSED') = (closed_at IS NOT NULL)
    )
);

-- THE guarantee. See the header for the full argument; in one sentence: this
-- is a real constraint the database enforces as part of committing an
-- INSERT, not an apply-side check that runs before one.
CREATE UNIQUE INDEX promotion_target_position_guard_one_active_window
    ON promotion_target_position_guard (tenant_id, position_ref, effective_date)
    WHERE status = 'ACTIVE';

-- Lets a caller holding a proposal ref find (and later close) its guard row
-- without a full-table scan.
CREATE INDEX promotion_target_position_guard_proposal_lookup
    ON promotion_target_position_guard (tenant_id, proposal_ref);

ALTER TABLE promotion_target_position_guard ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_target_position_guard FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_target_position_guard
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- No DELETE: a reservation transitions to CLOSED, it is never removed. This
-- table is mutable (UPDATE is granted) but never append-only, so it carries
-- no forbid_mutation trigger.
GRANT SELECT, INSERT, UPDATE ON promotion_target_position_guard TO hcmnext_app;

-- +goose Down
-- Every migration from 00279 on declares itself irreversible
-- (migrations/migrations_test.go's
-- TestNewestReversibleVersionStopsBelowDeclaredIrreversibles enforces this as
-- a chain-wide invariant: goose Down runs newest-to-oldest, so a real
-- rollback below the tip must pass back through 00279-00286 in order and
-- would stop at the first of those regardless of what this file's Down
-- section claimed). This migration keeps that chain true rather than
-- asserting a reversibility no rollback can ever actually reach.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00287 is irreversible: migrations 00279-00286 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
