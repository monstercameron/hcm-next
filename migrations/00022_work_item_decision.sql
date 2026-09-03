-- Owner: human-work lane. Phase: P1B.
-- WORK-010: persist the full typed approval decision or task submission a
-- completed work item's output digest already commits to, not only the
-- digest itself.
--
-- planning/todos.md's WORK-010 RED clause names the defect directly: a
-- completed APPROVAL or TASK work item stores completed_output_digest and
-- completed_by (migrations/00017_work_item.sql), so who decided is
-- recoverable, but the decision's reason, the authority it cites, the
-- rendered-projection and control-snapshot bindings it was made against, or
-- a task submission's typed answer fields, are not: the digest proves the
-- content was not tampered with, but the content itself was never stored.
--
-- work_item_decision is that missing content, one row per completed work
-- item: the exact JSON body [internal/workflow/steps/approval.Complete] or
-- [internal/workflow/steps/task.Submit] minted, plus the digest that body
-- must reproduce. RecordDecision (internal/humanwork/workitem/decision.go)
-- refuses to insert a row whose digest disagrees with the one the caller
-- already recorded as the work item's own completed_output_digest, so the
-- two writes -- the immutable digest on work_item and the recoverable body
-- here -- can never drift apart. It is called in the same transaction as
-- [workitem.Store.Complete], never afterward.
--
-- One row per item, not a log: a work item is completed exactly once
-- (migration 00017's work_item_forbid_rewrite trigger makes the completed
-- output digest immutable, so there is only ever one decision to record),
-- so PRIMARY KEY (tenant_id, work_item_id) is both the identity and the
-- structural proof that a second RecordDecision for the same item can never
-- silently overwrite the first -- it collides on the primary key instead.
-- "Append-only" here means exactly that: insert once, never update or
-- delete, enforced the same way migration 00017's own work_item_transition
-- is -- the forbid_mutation trigger from migrations/00001_platform_control.sql,
-- and a grant of SELECT/INSERT only.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied verbatim from migrations/00017_work_item.sql
-- work_item_transition's own copy of the migrations/00008_tenant_isolation.sql
-- pattern.

-- +goose Up

CREATE TABLE IF NOT EXISTS work_item_decision (
    tenant_id            tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    decision_id          uuid         NOT NULL,
    work_item_id         uuid         NOT NULL,
    workflow_instance_id uuid         NOT NULL,

    -- The work item's item_version at the moment its completion was recorded
    -- (i.e. the version [workitem.Store.Complete]'s UPDATE produced). Carried
    -- as evidence of exactly which completion this decision body belongs to,
    -- not as a foreign key target (work_item's own primary key is
    -- (tenant_id, work_item_id); the version moves on with later escalations
    -- or re-routing on a distinct item, which this table never has, since a
    -- COMPLETED item accepts no further transition).
    item_version         cas_version  NOT NULL,

    -- APPROVAL specializes an [intent/approval.ApprovalDecision]; TASK
    -- specializes an [internal/workflow/steps/task.Submission]. The same two
    -- kinds work_item.kind declares (migration 00017), because a decision
    -- record only ever exists for a work item of that kind.
    kind                 text         NOT NULL,

    -- The full typed decision or submission, encoded exactly as the minting
    -- package (internal/workflow/steps/approval or .../steps/task) produced
    -- it. It is one JSON document because, like work_item.assignment, it is
    -- read back as a whole unit of evidence -- through the inspector and the
    -- admin API, under the work item's own redaction rules -- never queried
    -- field by field.
    decision_body        jsonb        NOT NULL,
    -- decision_body_digest must equal the work item's own
    -- completed_output_digest at the moment this row is inserted. This is
    -- WORK-010's whole contract, made physical: the recoverable content and
    -- the immutable digest are proven to be the same fact.
    decision_body_digest text        NOT NULL,

    decided_by           semantic_key NOT NULL,
    decided_at           timestamptz  NOT NULL,

    recorded_at          timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, work_item_id),
    CONSTRAINT work_item_decision_id_unique UNIQUE (tenant_id, decision_id),
    FOREIGN KEY (tenant_id, work_item_id)
        REFERENCES work_item (tenant_id, work_item_id),
    FOREIGN KEY (tenant_id, workflow_instance_id)
        REFERENCES workflow_instance (tenant_id, instance_id),

    CONSTRAINT work_item_decision_kind_allowed CHECK (kind IN ('APPROVAL', 'TASK')),
    CONSTRAINT work_item_decision_body_object CHECK (jsonb_typeof(decision_body) = 'object'),
    CONSTRAINT work_item_decision_digest_shape CHECK (
        decision_body_digest ~ '^sha256:[0-9a-f]{64}$'
    )
);

-- "Every decision for this instance", which is what the inspector's
-- chronology and WF-RUN-030's terminal write (approval decision ids and task
-- submission ids alongside the promotion outcome) both ask for.
CREATE INDEX IF NOT EXISTS work_item_decision_instance
    ON work_item_decision (tenant_id, workflow_instance_id);

CREATE OR REPLACE TRIGGER work_item_decision_append_only
    BEFORE UPDATE OR DELETE ON work_item_decision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON work_item_decision FROM PUBLIC;

-- Tenant isolation (DB-017), copied from migrations/00017_work_item.sql
-- work_item_transition's own copy of the migrations/00008_tenant_isolation.sql
-- pattern: fail closed on a missing/blank app.tenant_id session setting,
-- keyed on internal/data/tenancy.WithTenant.
ALTER TABLE work_item_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE work_item_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON work_item_decision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- work_item_decision is append-only evidence, so SELECT/INSERT only, exactly
-- like work_item_transition.
GRANT SELECT, INSERT ON work_item_decision TO hcmnext_app;

-- +goose Down
REVOKE ALL ON work_item_decision FROM hcmnext_app;
DROP POLICY tenant_isolation ON work_item_decision;
ALTER TABLE work_item_decision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE work_item_decision DISABLE ROW LEVEL SECURITY;
DROP TABLE work_item_decision;
