-- Owner: workflow-runtime lane. Phase: P1B.
-- WF-RUN-025: durable continuation ledger for [runtime.ContinuationStore].
--
-- This table is this package's own audit-only record of the scheduling
-- intents internal/workflow/frontier.Advance derives on each advancement --
-- IntentReady, IntentWorkItemRequired, IntentSignalSubscriptionRequired,
-- IntentTimerRequired and IntentComplete -- never a WorkItem, timer or signal
-- subscription in its own right. Those remain owned by the systems that
-- actually grant that governed state (internal/humanwork and whatever owns
-- timers/signals); a caller composes its own internal/workflow/runtime.
-- ContinuationSink around or instead of this one to create them. What this
-- table exists to make provable is WF-RUN-025's atomicity claim: every
-- derived continuation record lands in the same transaction as the node and
-- instance writes advance.go performs, or none of it does, and a retried
-- write of the same record is a no-op rather than a duplicate.
--
-- continuation_id is derived from (tenant, instance, source_node_id,
-- source_attempt, target_node_id, kind) by internal/workflow/runtime.
-- ContinuationID, the same derived-identity pattern
-- migrations/00016_workflow_runtime.sql already uses for node_execution_id:
-- a repeated record for the same tuple collides on the primary key (and the
-- insert path additionally uses ON CONFLICT DO NOTHING) instead of forking a
-- second row for one logical intent.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied verbatim from
-- migrations/00016_workflow_runtime.sql, which in turn follows every policy in
-- migrations/00008_tenant_isolation.sql. Unlike workflow_instance and
-- workflow_node_execution, this table is append-only -- a continuation record
-- is never revised once written -- so the grant is SELECT/INSERT only, the
-- same append-only shape migrations/00014_ledger_hash_chain.sql and
-- migrations/00015_provenance.sql use.

-- +goose Up

CREATE TABLE IF NOT EXISTS workflow_continuation (
    tenant_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    continuation_id uuid           NOT NULL,
    instance_id     uuid           NOT NULL,

    -- The node execution attempt whose advancement derived this record.
    source_node_id     semantic_key NOT NULL,
    source_attempt      integer      NOT NULL,

    -- The node this record is about: a successor for every kind but
    -- COMPLETE, where it is the completed node itself.
    target_node_id  semantic_key NOT NULL,
    kind            text         NOT NULL,

    route_key       text,
    ref             text,
    terminal_code   text,

    recorded_at     timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, continuation_id),
    UNIQUE (tenant_id, instance_id, source_node_id, source_attempt, target_node_id, kind),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),

    CONSTRAINT workflow_continuation_source_attempt_positive CHECK (source_attempt >= 1),
    CONSTRAINT workflow_continuation_kind_allowed CHECK (
        kind IN (
            'READY', 'WORK_ITEM_REQUIRED', 'SIGNAL_SUBSCRIPTION_REQUIRED',
            'TIMER_REQUIRED', 'COMPLETE'
        )
    ),
    CONSTRAINT workflow_continuation_terminal_code_requires_complete CHECK (
        terminal_code IS NULL OR kind = 'COMPLETE'
    )
);

-- The frontier projection reads "everything one advancement derived"; the
-- operational queue reads "everything of one kind still to satisfy" per
-- instance.
CREATE INDEX IF NOT EXISTS workflow_continuation_source
    ON workflow_continuation (tenant_id, instance_id, source_node_id, source_attempt);

CREATE INDEX IF NOT EXISTS workflow_continuation_kind
    ON workflow_continuation (tenant_id, instance_id, kind);

-- Tenant isolation (DB-017), copied from migrations/00016_workflow_runtime.sql.
ALTER TABLE workflow_continuation ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_continuation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_continuation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Append-only: no UPDATE, no DELETE.
REVOKE UPDATE, DELETE ON workflow_continuation FROM PUBLIC;
GRANT SELECT, INSERT ON workflow_continuation TO hcmnext_app;

-- +goose Down
REVOKE ALL ON workflow_continuation FROM hcmnext_app;
DROP POLICY tenant_isolation ON workflow_continuation;
ALTER TABLE workflow_continuation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_continuation DISABLE ROW LEVEL SECURITY;
DROP TABLE workflow_continuation;
