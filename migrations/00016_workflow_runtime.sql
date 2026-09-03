-- Owner: workflow-runtime lane. Phase: P1A.
-- WF-RUN-001: durable WorkflowInstance and NodeExecution state.
--
-- planning/specs/workflow-runtime.md "Durable Runtime State" and "Durable Node
-- Execution" declare the two entities this migration makes physical. The
-- column list below is that spec's field list, with three deliberate
-- omissions and one rename:
--
--   * no execution_lease_id, no lease/fence table, no timer table and no
--     retry_at. definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000)
--     records BUILD but gates every scheduler, lease, fencing and timer
--     primitive behind a blocking P1B re-evaluation, so a column that only a
--     scheduler could populate would be a scheduler this repository has not
--     decided to build. Concurrency is carried entirely by
--     workflow_instance.instance_version (optimistic, fencing-free): a writer
--     that lost the race is refused, rather than a lease holder being trusted.
--   * retry_policy_ref on a node execution names the retry policy the
--     compiled plan declared. It is a reference to a declared policy, not a
--     scheduled retry; nothing in this phase reads it to decide when to run.
--   * status RETRYING exists because the spec's node-state machine names it.
--     It is a persisted state a caller may write, not a timer that produces
--     one.
--
-- Instance runtime status is a superset of internal/workflow.RuntimeStatus:
-- that Go type enumerates the statuses an END node may declare (COMPLETED,
-- CANCELLED, BLOCKED, REPAIR_REQUIRED, QUARANTINED, SUPERSEDED), while an
-- instance additionally passes through CREATED, RUNNING, WAITING,
-- PAUSE_REQUESTED, PAUSED and CANCELLING before it reaches one of those.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied from migrations/00014_ledger_hash_chain.sql
-- and migrations/00015_provenance.sql, which in turn follow every policy in
-- migrations/00008_tenant_isolation.sql. Unlike those two tables these are NOT
-- append-only: runtime state answers "where is execution now", so a row
-- transitions in place under an instance-version check and the grant includes
-- UPDATE (never DELETE, which this data plane has nowhere).

-- +goose Up

CREATE TABLE IF NOT EXISTS workflow_instance (
    tenant_id               tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    instance_id             uuid           NOT NULL,
    cell_id                 semantic_key   NOT NULL,

    workflow_id             semantic_key   NOT NULL,
    workflow_version        integer        NOT NULL,
    compiled_plan_hash      content_digest NOT NULL,

    business_subject_refs   text[]         NOT NULL DEFAULT '{}',
    business_transaction_id uuid,

    execution_mode          text           NOT NULL,
    runtime_status          text           NOT NULL,
    -- The intent's five lifecycle dimensions, never collapsed into
    -- runtime_status: an instance may be COMPLETED while ConsistencyState is
    -- DEGRADED, which is exactly the case the spec calls out.
    completion_dimensions   jsonb          NOT NULL DEFAULT '{}'::jsonb,

    -- input_ref is the content digest of the declared workflow input snapshot,
    -- not the input itself: raw input is separately authorized artifact
    -- storage, and the runtime store answers identity, not content.
    input_ref               text           NOT NULL,
    variable_revision_head  bigint         NOT NULL DEFAULT 0,
    -- The execution frontier. An array rather than a single node id because a
    -- parallel region has more than one current node.
    current_node_ids        text[]         NOT NULL DEFAULT '{}',

    effective_context_ref   text,
    last_checkpoint_ref     text,
    instance_version        cas_version    NOT NULL DEFAULT 1,

    correlation_id          semantic_key   NOT NULL,
    created_at              timestamptz    NOT NULL DEFAULT now(),
    started_at              timestamptz,
    completed_at            timestamptz,

    PRIMARY KEY (tenant_id, instance_id),

    CONSTRAINT workflow_instance_workflow_version_positive CHECK (workflow_version >= 1),
    CONSTRAINT workflow_instance_execution_mode_allowed CHECK (
        execution_mode IN ('SIMULATE', 'EXECUTE', 'REPLAY', 'REPAIR', 'SHADOW')
    ),
    CONSTRAINT workflow_instance_runtime_status_allowed CHECK (
        runtime_status IN (
            'CREATED', 'RUNNING', 'WAITING', 'PAUSE_REQUESTED', 'PAUSED',
            'CANCELLING', 'BLOCKED', 'COMPLETED', 'CANCELLED',
            'REPAIR_REQUIRED', 'QUARANTINED', 'SUPERSEDED'
        )
    ),
    CONSTRAINT workflow_instance_dimensions_object CHECK (
        jsonb_typeof(completion_dimensions) = 'object'
    ),
    CONSTRAINT workflow_instance_variable_revision_nonnegative CHECK (
        variable_revision_head >= 0
    ),
    -- A completion instant only exists for a status that actually ended the
    -- instance; a RUNNING row carrying completed_at would make every
    -- operational projection lie about what is still open.
    CONSTRAINT workflow_instance_completed_at_requires_end CHECK (
        completed_at IS NULL OR runtime_status IN (
            'COMPLETED', 'CANCELLED', 'REPAIR_REQUIRED', 'QUARANTINED', 'SUPERSEDED'
        )
    )
);

-- Operational lookups: "what is open for this workflow in this tenant".
CREATE INDEX IF NOT EXISTS workflow_instance_status
    ON workflow_instance (tenant_id, workflow_id, runtime_status);

CREATE INDEX IF NOT EXISTS workflow_instance_correlation
    ON workflow_instance (tenant_id, correlation_id);


CREATE TABLE IF NOT EXISTS workflow_node_execution (
    tenant_id                 tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    node_execution_id         uuid         NOT NULL,
    instance_id               uuid         NOT NULL,
    node_id                   semantic_key NOT NULL,
    attempt                   integer      NOT NULL,
    step_type                 semantic_key NOT NULL,
    status                    text         NOT NULL,

    -- Typed artifact references, never inlined payloads.
    input_snapshot_ref        text,
    output_artifact_ref       text,

    -- Governance and evidence references the execution inspector (WF-RUN-019)
    -- traverses: definition -> instance -> node -> governance -> transaction ->
    -- connector -> observation/reconciliation -> trace.
    capability_execution_id   text,
    authorization_decision_id text,
    decision_id               text,
    human_task_id             text,
    agent_execution_id        text,
    proposal_ref              text,
    baseline_ref              text,
    policy_ref                text,
    repair_ref                text,
    effect_refs               text[]       NOT NULL DEFAULT '{}',
    retry_policy_ref          text,

    error_class               text,
    trace_id                  text,

    started_at                timestamptz,
    completed_at              timestamptz,
    recorded_at               timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, node_execution_id),
    -- One row per attempt of one node of one instance. The Go store derives
    -- node_execution_id deterministically from exactly this tuple, so a
    -- repeated record for the same attempt collides on the primary key rather
    -- than silently forking a second identity for the same execution.
    CONSTRAINT workflow_node_execution_attempt_unique UNIQUE (tenant_id, instance_id, node_id, attempt),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),

    CONSTRAINT workflow_node_execution_attempt_positive CHECK (attempt >= 1),
    CONSTRAINT workflow_node_execution_status_allowed CHECK (
        status IN (
            'READY', 'RUNNING', 'WAITING', 'SUCCEEDED', 'FAILED', 'RETRYING',
            'SKIPPED', 'OVERRIDDEN', 'COMPENSATED', 'CANCELLED'
        )
    ),
    CONSTRAINT workflow_node_execution_completed_at_requires_end CHECK (
        completed_at IS NULL OR status IN (
            'SUCCEEDED', 'FAILED', 'SKIPPED', 'OVERRIDDEN', 'COMPENSATED', 'CANCELLED'
        )
    )
);

-- The inspector reads a whole instance in node order; the frontier projection
-- reads the not-yet-finished nodes of one instance.
CREATE INDEX IF NOT EXISTS workflow_node_execution_instance
    ON workflow_node_execution (tenant_id, instance_id, node_id, attempt);

CREATE INDEX IF NOT EXISTS workflow_node_execution_status
    ON workflow_node_execution (tenant_id, instance_id, status);


-- Tenant isolation (DB-017), same shape as every policy in
-- migrations/00008_tenant_isolation.sql: fail closed on a missing/blank
-- app.tenant_id session setting, keyed on internal/data/tenancy.WithTenant.
ALTER TABLE workflow_instance ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_instance FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_instance
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE workflow_node_execution ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_node_execution FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_node_execution
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Live serving state, so SELECT/INSERT/UPDATE. Never DELETE: this data plane
-- has no delete semantics, only append and state transition (00008's header).
REVOKE DELETE ON workflow_instance FROM PUBLIC;
REVOKE DELETE ON workflow_node_execution FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON workflow_instance TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON workflow_node_execution TO hcmnext_app;

-- +goose Down
REVOKE ALL ON workflow_node_execution FROM hcmnext_app;
REVOKE ALL ON workflow_instance FROM hcmnext_app;
DROP POLICY tenant_isolation ON workflow_node_execution;
DROP POLICY tenant_isolation ON workflow_instance;
ALTER TABLE workflow_node_execution NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_node_execution DISABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_instance NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_instance DISABLE ROW LEVEL SECURITY;
DROP TABLE workflow_node_execution;
DROP TABLE workflow_instance;
