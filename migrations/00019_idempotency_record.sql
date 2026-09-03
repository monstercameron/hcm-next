-- Owner: transaction (coordination) lane. Phase: P1B.
-- TX-006: durable semantic idempotency, the platform-foundation-gap-closure.md
-- section 6 lifecycle made physical:
--
--   (tenant, capability, semantic_effect_scope, idempotency_key)
--
-- is the whole uniqueness scope, and it is the table's own primary key rather
-- than a surrogate id plus a separate unique index: internal/transaction/
-- idempotency.Guard reserves a row with an INSERT ... ON CONFLICT (that exact
-- tuple) DO NOTHING, so the primary key itself is what makes a second
-- concurrent reservation for the same key lose deterministically -- no
-- process-local lock, no advisory lock, just the constraint every concurrent
-- inserter contends on.
--
-- The lifecycle has exactly two states. RESERVED is written the instant a
-- caller wins the race to run its effect; COMPLETED is written, in the same
-- caller transaction, once that effect has produced a result identity to
-- remember. There is no third, failed state: a caller whose effect errors
-- before completing rolls its whole transaction back, and the reservation
-- disappears with it -- Guard never leaves a dangling RESERVED row for a
-- caller that never got to commit. A row a concurrent reader can actually see
-- committed is therefore always COMPLETED in the ordinary path; RESERVED is
-- reachable only by a caller that deliberately calls Store.Reserve on its own
-- outside Guard's atomic pattern (Store exposes Reserve/Complete/Lookup/Expire
-- as four independent primitives precisely so such a caller can).
--
-- request_digest is the canonical request hash the spec's lifecycle diagram
-- calls out: a replay under the same key compares it, and same digest ->
-- same stored identity while a different digest -> IDEMPOTENCY_CONFLICT with
-- no mutation. It is the content_digest domain (a sha256 hex digest) because
-- every canonical digest in this codebase (ledger_event.digest, stream_head.
-- head_digest) is that same profile; this table records no separate
-- algorithm column because the domain itself is single-algorithm today, same
-- as ledger_stream's own head digest before 00014 needed to branch it.
--
-- result_ref / event_ref / effect_identity / evidence_id are the stored
-- result identity GREEN requires: "one stored result/event/effect identity".
-- All four are optional references (a caller states whichever its own effect
-- actually produced) but COMPLETED requires at least one of them, and
-- RESERVED forbids all four -- identity is only ever attached at completion,
-- never invented at reservation time.
--
-- expires_at is retention, not a TTL the database enforces on its own: TX-006
-- refuses at Reserve time (in Go, before this table is ever touched) any
-- RetentionPolicy whose declared retention is shorter than the caller's own
-- retry/redelivery window, so a row that exists always outlives the window
-- that could still legitimately replay it. Sweeping expired rows is a
-- policy-owned, explicit Store.Expire call (DELETE), never a trigger or a
-- cron this migration installs.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied verbatim from
-- migrations/00016_workflow_runtime.sql, which is itself copied from
-- migrations/00014_ledger_hash_chain.sql and migrations/00015_provenance.sql,
-- which in turn follow every policy in migrations/00008_tenant_isolation.sql.
-- Unlike those append-only/state-transition tables this data plane's grant
-- also includes DELETE: retention expiry is this table's own documented
-- lifecycle step, not a data-loss incident, so Store.Expire's DELETE is a
-- normal, expected write the least-privilege role must be able to perform.

-- +goose Up

CREATE TABLE IF NOT EXISTS idempotency_record (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    capability_id     semantic_key   NOT NULL,
    effect_scope      semantic_key   NOT NULL,
    idempotency_key   semantic_key   NOT NULL,

    request_digest    content_digest NOT NULL,
    status            text           NOT NULL,

    -- Stored result identity. Optional individually; COMPLETED requires at
    -- least one, RESERVED forbids all four (see the constraints below).
    result_ref        text,
    event_ref         text,
    effect_identity   text,
    evidence_id       text,

    created_at        timestamptz    NOT NULL DEFAULT now(),
    -- Retention policy's own deadline, computed by the caller-supplied
    -- RetentionPolicy at Reserve time -- never a clock this table reads
    -- itself.
    expires_at        timestamptz    NOT NULL,

    PRIMARY KEY (tenant_id, capability_id, effect_scope, idempotency_key),

    CONSTRAINT idempotency_record_status_allowed CHECK (
        status IN ('RESERVED', 'COMPLETED')
    ),
    CONSTRAINT idempotency_record_expires_after_created CHECK (
        expires_at > created_at
    ),
    CONSTRAINT idempotency_record_reserved_has_no_identity CHECK (
        status <> 'RESERVED' OR (
            result_ref IS NULL AND event_ref IS NULL
            AND effect_identity IS NULL AND evidence_id IS NULL
        )
    ),
    CONSTRAINT idempotency_record_completed_has_identity CHECK (
        status <> 'COMPLETED' OR (
            result_ref IS NOT NULL OR event_ref IS NOT NULL
            OR effect_identity IS NOT NULL OR evidence_id IS NOT NULL
        )
    )
);

-- Retention sweep: "what has expired for this tenant" is Store.Expire's whole
-- query shape.
CREATE INDEX IF NOT EXISTS idempotency_record_expiry
    ON idempotency_record (tenant_id, expires_at);

-- Tenant isolation (DB-017), copied verbatim from migration 00016.
ALTER TABLE idempotency_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON idempotency_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Live serving state that also expires: SELECT/INSERT/UPDATE/DELETE. DELETE
-- is this table's own documented retention step (Store.Expire), not a
-- capability this data plane otherwise grants.
GRANT SELECT, INSERT, UPDATE, DELETE ON idempotency_record TO hcmnext_app;

-- +goose Down
REVOKE ALL ON idempotency_record FROM hcmnext_app;
DROP POLICY tenant_isolation ON idempotency_record;
ALTER TABLE idempotency_record NO FORCE ROW LEVEL SECURITY;
ALTER TABLE idempotency_record DISABLE ROW LEVEL SECURITY;
DROP TABLE idempotency_record;
