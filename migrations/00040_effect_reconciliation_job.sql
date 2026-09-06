-- Owner: data plane. Phase: P1B.
-- RECON-001: the durable reconciliation-job lifecycle for committed
-- mandatory effects.
--
-- Naming note: migration 00031 already owns a table literally named
-- `reconciliation_job` for INTG-009's connector sync runs (RUNNING,
-- COMPLETED, FAILED, ABORTED over a direction/mode). That is a different
-- concept from this one -- a bulk data-sync run versus one committed
-- effect's own external-truth check -- so this table is named
-- effect_reconciliation_job to keep the two apart rather than colliding on
-- the same name (a migration 00039 attempt at this table under the bare
-- name `reconciliation_job` collided with 00031's table: CREATE TABLE IF
-- NOT EXISTS silently no-opped against the pre-existing table, and every
-- statement assuming this table's own columns then failed; see this
-- migration's number, chosen after that collision was discovered).
--
-- Why this migration exists. A committed mandatory effect (internal/effectgraph's
-- EffectNode with Observation.Required = true) promises that its external result
-- will be checked, not merely attempted. Today nothing durably remembers that
-- promise: a process that scheduled a check and then crashed, restarted or was
-- superseded has no row to resume from, and a second caller triggering the same
-- check has no way to discover the first one already exists. This table is that
-- row, one per (effect, comparison policy), so a reconciliation attempt is state
-- a caller resumes rather than a fact only a live process remembers.
--
-- What this table is not. It is not the comparison engine (RECON-002, not yet
-- built, owns turning an observation and a policy into a business completion
-- verdict) and it is not a repair executor (REPAIR-001/002 own that). This row
-- only tracks the job's own lifecycle: what it is watching for, how many times it
-- has looked, when to look again, when to give up, who is currently responsible,
-- and the terminal or resting state that resulted. internal/operations/reconcile
-- coordinates observation and comparison through ports it declares; it writes
-- nothing here but its own bookkeeping.
--
-- Lifecycle. PENDING is a freshly triggered job that has not yet been polled.
-- OBSERVING and UNKNOWN are resting, non-terminal states: an observation that
-- does not meet the job's declared freshness requirement, or a comparison
-- verdict of UNKNOWN, both leave the job here rather than ending polling --
-- that is RECON-001's RED clause "a stale observation result never ends
-- polling". PASS, MISMATCH and PARTIAL are terminal comparison verdicts. EXPIRED
-- and REPAIR_REQUIRED are what an exhausted job becomes at its deadline instead
-- of disappearing -- REPAIR_REQUIRED when the committed effect declared a repair
-- route (repair_policy is not NONE), EXPIRED when it did not.
--
-- Fencing and idempotency. job_id is derived (tenant, effect_ref, policy_ref),
-- so a duplicate trigger for the same effect and policy addresses the row that
-- already exists rather than creating a parallel one; the explicit unique
-- constraint below is the same guarantee enforced a second way for a caller that
-- computed a different id for the same natural key by mistake. Every write that
-- advances a job presents a workflow_lease fence (internal/workflow/lease),
-- verified by the caller's own transaction before this table is touched, so a
-- superseded worker cannot resume a job it no longer owns. Every advance is also
-- a compare-and-swap on job_version, exactly like migration 00026's live runtime
-- state, so restart loses nothing: deadline, next_check_at, observation_attempts
-- and owner_ref all reload from the row as it was last durably left.
--
-- Storage disposition: this table needs a row in
-- definitions/storage/storage-disposition.yaml (STORE-001); the row is reported
-- verbatim by this lane.

-- +goose Up

CREATE TABLE IF NOT EXISTS effect_reconciliation_job (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    job_id                uuid         NOT NULL,

    -- The committed mandatory effect this job watches, and the comparison
    -- policy it watches under. effect_ref is the effect's own durable
    -- idempotency key (internal/effectgraph.EffectNode.IdempotencyKey);
    -- effect_id is the compiled graph's local node id, kept for evidence only.
    effect_ref            semantic_key NOT NULL,
    effect_id             semantic_key NOT NULL,
    policy_ref            semantic_key NOT NULL,

    -- What was intended, and what the external system is now known to say.
    -- canonical_ref starts absent -- a job may rest at PENDING or OBSERVING
    -- before any observation has named one -- and is never semantic_key because
    -- an absent reference is a blank string, not a contradiction in terms.
    intended_ref          text         NOT NULL,
    canonical_ref         text         NOT NULL DEFAULT '',

    -- The freshness (internal/connectivity/observe.Freshness) an observation
    -- must meet before this job's comparer is even asked for a verdict. A less
    -- fresh observation still counts as an attempt but never settles the job.
    required_freshness    text         NOT NULL,
    observation_attempts  integer      NOT NULL DEFAULT 0,

    -- The next instant a caller should poll this job, and the instant beyond
    -- which an unsettled job is exhausted rather than polled again.
    next_check_at         timestamptz  NOT NULL,
    deadline_at           timestamptz  NOT NULL,

    -- The holder_id (internal/workflow/lease.Identity) of whichever caller most
    -- recently advanced this job under a verified fence.
    owner_ref             semantic_key NOT NULL,
    sla_ref               semantic_key NOT NULL,

    -- Carried from the committed effect at trigger time, so an exhausted job
    -- can be settled to EXPIRED or REPAIR_REQUIRED without re-reading the
    -- compiled graph.
    repair_policy         semantic_key NOT NULL,

    job_state             text         NOT NULL,
    job_version           cas_version  NOT NULL DEFAULT 1,

    created_at            timestamptz  NOT NULL,
    updated_at            timestamptz  NOT NULL,
    recorded_at           timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, job_id),
    -- One job per effect and comparison policy: the structural half of RECON-001's
    -- idempotent trigger. job_id is already derived from this same triple, so
    -- this constraint only matters for a caller that computed job_id differently
    -- for the same natural key -- which is exactly the mistake it exists to
    -- catch rather than silently accept.
    CONSTRAINT effect_reconciliation_job_effect_policy_unique
        UNIQUE (tenant_id, effect_ref, policy_ref),
    CONSTRAINT effect_reconciliation_job_state_allowed CHECK (
        job_state IN ('PENDING', 'OBSERVING', 'PASS', 'MISMATCH', 'PARTIAL',
                      'UNKNOWN', 'EXPIRED', 'REPAIR_REQUIRED')
    ),
    CONSTRAINT effect_reconciliation_job_freshness_allowed CHECK (
        required_freshness IN ('FRESH', 'STALE', 'PARTIAL', 'UNKNOWN', 'UNAVAILABLE')
    ),
    CONSTRAINT effect_reconciliation_job_attempts_non_negative CHECK (observation_attempts >= 0),
    CONSTRAINT effect_reconciliation_job_deadline_after_creation CHECK (deadline_at > created_at)
);

-- What a caller polls: the still-open jobs (PENDING, OBSERVING or UNKNOWN),
-- soonest check first. A settled job (PASS, MISMATCH, PARTIAL, EXPIRED,
-- REPAIR_REQUIRED) is never due again, so it drops out of this index's
-- selectivity even though it stays in the table forever as its own evidence.
CREATE INDEX IF NOT EXISTS effect_reconciliation_job_due
    ON effect_reconciliation_job (tenant_id, next_check_at)
    WHERE job_state IN ('PENDING', 'OBSERVING', 'UNKNOWN');

-- Tenant isolation (DB-017), the same fail-closed app.tenant_id predicate as
-- every other data-plane table since migration 00008.
ALTER TABLE effect_reconciliation_job ENABLE ROW LEVEL SECURITY;
ALTER TABLE effect_reconciliation_job FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON effect_reconciliation_job
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Live serving state, fenced and version-CAS'd like migration 00026's runtime
-- tables: SELECT/INSERT/UPDATE, never DELETE. A settled job is evidence that a
-- promise was kept or exhausted, not a row to clean up.
REVOKE DELETE ON effect_reconciliation_job FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON effect_reconciliation_job TO hcmnext_app;

-- +goose Down
REVOKE ALL ON effect_reconciliation_job FROM hcmnext_app;
DROP TABLE effect_reconciliation_job;
