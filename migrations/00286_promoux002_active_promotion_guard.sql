-- PROMOUX-002: admit exactly one active promotion per worker and overlapping
-- effective window, at the database, not only in application code that
-- checks before it writes.
--
-- The live-audit RED this migration closes: two concurrent "Start Promotion"
-- calls for the same worker each ran a read (does a nonterminal journey
-- already exist?) and then a write (create the intent), and nothing stopped
-- both reads from seeing "no conflict" before either write landed. Moving
-- the check into application code closer to the write narrows the race
-- window but cannot close it -- only a constraint the database evaluates as
-- part of the write itself can, because that is the only point two
-- concurrent transactions are forced to serialize against each other.
--
-- What a promotion's "effective window" means here: the request contract
-- (internal/intent/app.ProposePromotion / journeyEngine.Propose) carries a
-- single effective_date, never a range, so this table models each pending
-- promotion's window as the single day it takes effect -- a degenerate
-- one-day range. Two promotions for one worker whose effective dates differ
-- do not overlap and are both admitted; two whose effective dates are the
-- same do, and only the first commits. If a future revision of the
-- promotion contract ever carries an explicit end date, the exclusivity
-- predicate below is where that range would be compared instead of date
-- equality -- the shape of the guarantee does not change, only what two
-- rows are compared on.
--
-- The guarantee itself: promotion_active_intent_guard_one_active_window is a
-- partial UNIQUE index on (tenant_id, worker_ref, effective_date) WHERE
-- status = 'ACTIVE'. Two concurrent transactions can both evaluate "is there
-- already an ACTIVE row for this worker and date" and both see none -- that
-- race is real and this schema does not prevent the read from lying. What it
-- prevents is both of their INSERTs succeeding: PostgreSQL evaluates a
-- unique index as part of each INSERT's own commit, so the second
-- transaction to reach that point is refused with unique_violation (23505)
-- regardless of what its own prior read observed. internal/data/promotionguard
-- is the one Go package that writes this table, and it never performs a
-- bare SELECT-then-INSERT to decide admission: the INSERT (an
-- INSERT .. ON CONFLICT .. DO UPDATE, so a replay of the same idempotency
-- key resolves to the row already there instead of colliding with it) is
-- itself the decision, and the constraint is what makes that decision safe
-- under real concurrency.
--
-- Rows are mutable (status transitions ACTIVE -> CLOSED when the guarded
-- promotion reaches a terminal stage), so this is not an append-only ledger
-- table and carries no forbid_mutation trigger; UPDATE is granted, DELETE is
-- not, matching admission_retry_budget's shape (migration 00280) rather than
-- ledger_event's.

-- +goose Up

CREATE TABLE promotion_active_intent_guard (
    tenant_id        tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    -- guard_id is application-generated (intent.UUIDv7Source, the same
    -- source every other identifier in this cell is minted from), not
    -- database-generated: the row this migration's tables live beside never
    -- relies on gen_random_uuid()/pgcrypto, and this table does not start.
    guard_id         uuid         NOT NULL,
    -- The canonical entity-reference string every promotion journey is
    -- listed by (values.EntityRef.String(), the same text
    -- tools/uxqual/productclient projects as WorkItem.PersonRef and
    -- journeyv1.Journey.WorkerRef). Not a foreign key: a promotion subject
    -- may be a corpus worker or a tenant-created one, and the two
    -- populations share no single referenceable table today.
    worker_ref       semantic_key NOT NULL,
    effective_date   date         NOT NULL,
    -- Ties one reservation to the specific promotion.propose call that
    -- opened it (internal/intent/app's own "promotion.propose:<client_request_id>"
    -- idempotency key or the legacy form's minted key). A second call
    -- presenting the same key is a replay of this same request and resolves
    -- to this same row; a call presenting a different key for the same
    -- worker and date is a genuinely different, conflicting promotion.
    idempotency_key  semantic_key NOT NULL,
    -- Set once CreateIntent actually mints the intent this reservation
    -- protects. NULL between the reservation landing and that confirmation,
    -- which is a real, transient, and safe state: nothing else depends on it
    -- being non-null, and a crash in that window simply leaves a reservation
    -- whose own idempotency key still resolves the same caller's retry to
    -- the same row.
    intent_id        uuid,
    status           text         NOT NULL DEFAULT 'ACTIVE',
    opened_at        timestamptz  NOT NULL DEFAULT now(),
    closed_at        timestamptz,
    PRIMARY KEY (tenant_id, guard_id),
    CONSTRAINT promotion_active_intent_guard_status_allowed CHECK (
        status IN ('ACTIVE', 'CLOSED')
    ),
    CONSTRAINT promotion_active_intent_guard_closed_at_matches_status CHECK (
        (status = 'CLOSED') = (closed_at IS NOT NULL)
    )
);

-- THE guarantee. See the header for the full argument; in one sentence: this
-- is a real constraint the database enforces as part of committing an
-- INSERT, not an apply-side check that runs before one.
CREATE UNIQUE INDEX promotion_active_intent_guard_one_active_window
    ON promotion_active_intent_guard (tenant_id, worker_ref, effective_date)
    WHERE status = 'ACTIVE';

-- Lets a caller holding an intent id find (and later close) its guard row
-- without a full-table scan. Partial because most callers that need this
-- index are asking about a specific intent that has already been confirmed.
CREATE INDEX promotion_active_intent_guard_intent_lookup
    ON promotion_active_intent_guard (tenant_id, intent_id)
    WHERE intent_id IS NOT NULL;

ALTER TABLE promotion_active_intent_guard ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_active_intent_guard FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_active_intent_guard
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- No DELETE: a reservation transitions to CLOSED, it is never removed. This
-- table is mutable (UPDATE is granted) but never append-only, so it carries
-- no forbid_mutation trigger.
GRANT SELECT, INSERT, UPDATE ON promotion_active_intent_guard TO hcmnext_app;

-- +goose Down
-- Every migration from 00279 on declares itself irreversible
-- (migrations/migrations_test.go's
-- TestNewestReversibleVersionStopsBelowDeclaredIrreversibles enforces this as
-- a chain-wide invariant: goose Down runs newest-to-oldest, so a real
-- rollback below the tip must pass back through 00279-00285 in order and
-- would stop at the first of those regardless of what this file's Down
-- section claimed). This migration keeps that chain true rather than
-- asserting a reversibility no rollback can ever actually reach.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00286 is irreversible: migrations 00279-00285 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
