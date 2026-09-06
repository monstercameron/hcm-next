-- Owner: data plane. Phase: P2. JOB-001 (depends: DB-004, DB-012, TRUST-006).
--
-- Why this migration exists. JOB-001's GREEN clause asks for durable job
-- definitions, runs, partitions and checkpoints whose attempts survive
-- crash/restart with exact counts and digests, and its RED clause refuses a
-- mutable, cross-tenant or unversioned run. This migration is the durable
-- state that satisfies both, built on the same house pattern migration
-- 00026 already established for DB-012 (workflow scheduling state): CAS-
-- fenced live rows for state that opens and advances, append-only evidence
-- for facts that must never be rewritten, and no sweeper -- nothing in this
-- repository claims a partition, fires a timer or dispatches a job on a
-- clock. internal/data/jobs exercises these tables through ordinary
-- caller-driven stores exactly the way internal/data/runtimestate exercises
-- 00026's tables; a future scheduler (JOB-002's admission, JOB-003's fenced
-- leases) is what will call Publish/StartRun/ClaimPartition on a trigger
-- firing, and that dispatcher is out of scope here under the same
-- WF-RUN-000 gate 00026 already cites: this migration introduces the state
-- those mechanisms would read and write, never the mechanism itself.
--
-- What a job definition references, and why neither reference is a foreign
-- key. A job_definition binds a governed job to two things:
--
--   * trigger_digest -- the internal/engines/schedule PublishedTrigger this
--     definition is scheduled by. SCHED-001's own registry is an in-memory,
--     content-addressed publication port (Store is an interface; Registry is
--     its in-memory adapter): no durable trigger table exists anywhere in
--     this migration tree yet, so trigger_digest is recorded as the value
--     reference it already is -- a canonicalbytes digest -- rather than a
--     foreign key to a table that has not been built. Content-addressing is
--     what makes the reference meaningful without one: the digest changes if
--     and only if the trigger definition does.
--   * target_definition_ref / target_definition_version -- the intent this
--     job's governed operation targets, in exactly the (type id, version)
--     shape internal/intent.Ref and SCHED-001's own TriggerDefinition.Target
--     already use. It is not a foreign key for the same reason
--     intent_instance.definition_ref is not one (migration 00004): the
--     intent type catalog is declared configuration, not a row this schema
--     owns.
--
-- Every table is tenant scoped with tenant_id in its primary key, protected
-- by the same fail-closed app.tenant_id row level security policy migration
-- 00008 establishes, and granted to hcmnext_app with DELETE withheld.
-- job_definition and job_checkpoint are append-only evidence carrying the
-- forbid_mutation trigger from migrations/00001_platform_control.sql:
-- a published job definition is immutable identity (RED: "unversioned run"
-- is refused because every run pins an exact (job_id, version) that can
-- never be rewritten under it), and a checkpoint is a safe point that would
-- not be safe if it could be rewritten. job_run and job_partition are live
-- serving state fenced by a compare-and-swap version column, exactly like
-- migration 00016's workflow_instance and 00026's workflow_ready_work.
--
-- Storage disposition: every table below needs a row in
-- definitions/storage/storage-disposition.yaml (STORE-001); the rows are
-- listed verbatim in this lane's report.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 1. Job definitions.
-- ---------------------------------------------------------------------------
-- One immutable published revision of a governed job, keyed the same way
-- migration 00003's definition_version keys a definition: (tenant, natural
-- key, version). A second publish of the same (tenant_id, job_id, version)
-- is a collision, not an update -- exactly the "mutable run" the RED clause
-- refuses, pushed back to its source: a run cannot be declared against a
-- version that could later change under it, because no publish ever changes
-- one.
CREATE TABLE IF NOT EXISTS job_definition (
    tenant_id                  tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    job_id                     semantic_key     NOT NULL,
    version                    cas_version      NOT NULL,

    -- Content identity of the full published definition body.
    definition_digest          content_digest   NOT NULL,
    -- The internal/engines/schedule PublishedTrigger.Digest this definition is
    -- scheduled by. A value reference, not a foreign key -- see the header.
    trigger_digest             content_digest   NOT NULL,
    digest_algorithm           digest_algorithm NOT NULL DEFAULT 'sha256',

    -- The intent this job's governed operation targets, in internal/intent.Ref
    -- shape. Not a foreign key -- see the header.
    target_definition_ref      semantic_key     NOT NULL,
    target_definition_version  cas_version      NOT NULL,

    -- The opaque governed job spec: partitioning plan, connector/import
    -- parameters, whatever the owning capability declares. Interpreted by the
    -- caller, not by this schema.
    body                       bytea            NOT NULL,

    published_by               text             NOT NULL,
    published_at               timestamptz      NOT NULL,
    recorded_at                timestamptz      NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, job_id, version)
);

CREATE INDEX IF NOT EXISTS job_definition_job
    ON job_definition (tenant_id, job_id);

CREATE OR REPLACE TRIGGER job_definition_append_only
    BEFORE UPDATE OR DELETE ON job_definition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON job_definition FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 2. Job runs.
-- ---------------------------------------------------------------------------
-- One row per run of a published job definition. run_version is the
-- compare-and-swap fence every state transition presents, the same shape as
-- migration 00016's workflow_instance.instance_version; attempt counts how
-- many times this run identity has been (re)started, so a redrive after
-- FAILED is a new attempt on the same run row rather than a new run losing
-- the lineage back to its first try.
CREATE TABLE IF NOT EXISTS job_run (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    run_id          uuid         NOT NULL,
    -- Pins the exact published revision this run was declared against. The
    -- foreign key is what makes an unversioned or since-rewritten definition
    -- unreachable: there is no version to point at until job_definition
    -- publishes one, and job_definition never rewrites the one it published.
    job_id          semantic_key NOT NULL,
    job_version     cas_version  NOT NULL,

    run_state       text         NOT NULL,
    attempt         integer      NOT NULL DEFAULT 1,
    run_version     cas_version  NOT NULL DEFAULT 1,

    declared_by     text         NOT NULL,
    declared_at     timestamptz  NOT NULL,
    started_at      timestamptz,
    completed_at    timestamptz,
    -- Set only on a FAILED transition; cleared implicitly by Retry moving the
    -- row back to DECLARED for the next attempt.
    failure_detail  text,

    recorded_at     timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, run_id),
    FOREIGN KEY (tenant_id, job_id, job_version)
        REFERENCES job_definition (tenant_id, job_id, version),
    CONSTRAINT job_run_state_allowed CHECK (
        run_state IN ('DECLARED', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED')
    ),
    CONSTRAINT job_run_attempt_positive CHECK (attempt >= 1),
    CONSTRAINT job_run_started_after_declared CHECK (
        started_at IS NULL OR started_at >= declared_at
    ),
    CONSTRAINT job_run_completed_after_declared CHECK (
        completed_at IS NULL OR completed_at >= declared_at
    ),
    -- A terminal state says when it finished; a non-terminal one does not.
    CONSTRAINT job_run_completed_consistent CHECK (
        (run_state IN ('COMPLETED', 'FAILED', 'CANCELLED')) = (completed_at IS NOT NULL)
    ),
    CONSTRAINT job_run_failure_detail_only_on_failure CHECK (
        failure_detail IS NULL OR run_state = 'FAILED'
    )
);

CREATE INDEX IF NOT EXISTS job_run_job
    ON job_run (tenant_id, job_id, job_version);

CREATE INDEX IF NOT EXISTS job_run_state
    ON job_run (tenant_id, run_state);


-- ---------------------------------------------------------------------------
-- 3. Job partitions.
-- ---------------------------------------------------------------------------
-- One row per partition of a run, its own compare-and-swap fenced state, and
-- a partition_key that is deterministic for the run that produced it: the
-- same partitioning decision replayed after a crash names the same key,
-- which is what lets ClaimPartition's ON CONFLICT and the unique constraint
-- below refuse a second row for a partition that already exists rather than
-- forking a duplicate unit of work.
CREATE TABLE IF NOT EXISTS job_partition (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    partition_id       uuid         NOT NULL,
    run_id             uuid         NOT NULL,
    partition_key      semantic_key NOT NULL,

    partition_state    text         NOT NULL,
    -- The current holder, set exactly when ClaimPartition's compare-and-swap
    -- wins. There is no partition_claim history table here the way migration
    -- 00026's work_item_claim sits behind work_item: JOB-001 asks for the
    -- live claim only, and a future JOB-003 (fenced leases, resumable
    -- partition execution) is where claim history, if it turns out to be
    -- needed, belongs.
    claimed_by         semantic_key,
    claimed_at         timestamptz,
    failure_detail     text,

    partition_version  cas_version  NOT NULL DEFAULT 1,
    created_at         timestamptz  NOT NULL,
    completed_at       timestamptz,
    recorded_at        timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, partition_id),
    CONSTRAINT job_partition_key_unique
        UNIQUE (tenant_id, run_id, partition_key),
    FOREIGN KEY (tenant_id, run_id) REFERENCES job_run (tenant_id, run_id),
    CONSTRAINT job_partition_state_allowed CHECK (
        partition_state IN ('PENDING', 'CLAIMED', 'COMPLETED', 'FAILED', 'CANCELLED')
    ),
    -- An unclaimed partition names no holder; a claimed or settled one always
    -- does, because ClaimPartition is the only path out of PENDING.
    CONSTRAINT job_partition_claimed_consistent CHECK (
        (partition_state = 'PENDING') = (claimed_by IS NULL)
    ),
    CONSTRAINT job_partition_claimed_at_consistent CHECK (
        (claimed_by IS NULL) = (claimed_at IS NULL)
    ),
    CONSTRAINT job_partition_completed_consistent CHECK (
        (partition_state IN ('COMPLETED', 'FAILED', 'CANCELLED')) = (completed_at IS NOT NULL)
    ),
    CONSTRAINT job_partition_completed_after_created CHECK (
        completed_at IS NULL OR completed_at >= created_at
    ),
    CONSTRAINT job_partition_failure_detail_only_on_failure CHECK (
        failure_detail IS NULL OR partition_state = 'FAILED'
    )
);

CREATE INDEX IF NOT EXISTS job_partition_run
    ON job_partition (tenant_id, run_id, partition_state);


-- ---------------------------------------------------------------------------
-- 4. Job checkpoints.
-- ---------------------------------------------------------------------------
-- An append-only, numbered checkpoint per partition, the same shape as
-- migration 00026's workflow_checkpoint: a state digest at a numbered
-- position plus the partition_version it describes, so a resume after crash
-- has an exact point to compare against rather than trusting whatever the
-- partition row's own live state happens to say.
CREATE TABLE IF NOT EXISTS job_checkpoint (
    tenant_id            tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    partition_id         uuid             NOT NULL,
    checkpoint_sequence  bigint           NOT NULL,

    state_digest         content_digest   NOT NULL,
    digest_algorithm     digest_algorithm NOT NULL DEFAULT 'sha256',
    -- The partition_version this checkpoint describes, so a resume can tell
    -- whether the live row has moved past what the checkpoint recorded.
    partition_version    cas_version      NOT NULL,

    taken_at             timestamptz      NOT NULL,
    recorded_at          timestamptz      NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, partition_id, checkpoint_sequence),
    FOREIGN KEY (tenant_id, partition_id) REFERENCES job_partition (tenant_id, partition_id),
    CONSTRAINT job_checkpoint_sequence_positive CHECK (checkpoint_sequence >= 1)
);

CREATE OR REPLACE TRIGGER job_checkpoint_append_only
    BEFORE UPDATE OR DELETE ON job_checkpoint
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON job_checkpoint FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- Tenant isolation (DB-017) for every table above.
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'job_definition',
        'job_run',
        'job_partition',
        'job_checkpoint'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I '
            'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
            'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
            target);
        EXECUTE format('REVOKE DELETE ON %I FROM PUBLIC', target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- Append-only evidence: SELECT and INSERT only.
GRANT SELECT, INSERT ON
    job_definition,
    job_checkpoint
TO hcmnext_app;

-- Live serving state: SELECT/INSERT/UPDATE, never DELETE.
GRANT SELECT, INSERT, UPDATE ON
    job_run,
    job_partition
TO hcmnext_app;

-- +goose Down
REVOKE ALL ON
    job_checkpoint,
    job_partition,
    job_run,
    job_definition
FROM hcmnext_app;

DROP TABLE job_checkpoint;
DROP TABLE job_partition;
DROP TABLE job_run;
DROP TABLE job_definition;
