-- Owner: human-work lane. Phase: P1B.
-- WORK-001/002/003: the durable WorkItem the frontier's WORK_ITEM_REQUIRED
-- intent creates, its assignment evidence, and its exclusive claim.
--
-- planning/specs/human-work-forms-and-rules.md "Human Work Management"
-- declares the WorkItem entity and its lifecycle; this migration makes the
-- Phase 1 subset of it physical. Two tables, and the split between them is the
-- point:
--
--   work_item             where responsibility is now  (transitions in place)
--   work_item_transition  how it got there             (append-only evidence)
--
-- Every write to the first appends a row to the second inside one transaction
-- and bumps item_version, so "appends transition evidence" in WORK-001's GREEN
-- clause is a property of the schema (a UNIQUE on (work_item, item_version))
-- rather than a convention a caller could forget.
--
-- # What this deliberately is not
--
-- definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000) gates every
-- scheduler, lease, fencing and timer primitive behind a blocking P1B
-- re-evaluation. So WORK-003's "fenced lease" is NOT a lease table, a fence
-- token, a lease-expiry sweeper or a timer. It is:
--
--   * an exclusive claim recorded on the item row itself (claim_id,
--     claimed_by, claimed_at, claim_expires_at), won by an optimistic
--     item_version compare-and-swap -- exactly one UPDATE can match a given
--     version, so exactly one claimant wins and the losers mutate nothing;
--   * a caller-supplied claim_expires_at, never a clock this schema or the Go
--     store reads;
--   * expiry evaluated only when a caller next touches the item. Nothing
--     sweeps. An expired claim is a fact about a row, discovered by whoever
--     reads it, and the touch that discovers it returns the item to its
--     policy route.
--
-- # Columns the RED clause names
--
-- WORK-001's RED clause refuses an item missing correlation, subject, owner,
-- visibility or deadline, so those five are NOT NULL here and not merely
-- validated in Go. Two of them need a word:
--
--   * owner is NOT NULL from creation, before any routing has happened,
--     because an item always has an owner -- it is just not always a person.
--     owner_kind says which: POLICY_ROUTE (nobody yet; the route named in
--     policy_route_ref owns it), CANDIDATE_SET (a resolved set, open for
--     claim) or PRINCIPAL (a named owner). policy_route_ref is immutable and
--     is what "returns the item to its policy route" returns it to.
--   * status names the exact lifecycle state, never a generic one. The CHECK
--     below is the same eleven-value set internal/humanwork/workitem.Status
--     declares, and a test compares the two lists rather than trusting them.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied from migrations/00016_workflow_runtime.sql
-- and, through it, from migrations/00008_tenant_isolation.sql. work_item is
-- live serving state (SELECT/INSERT/UPDATE, never DELETE);
-- work_item_transition is append-only evidence (SELECT/INSERT only, with the
-- forbid_mutation trigger 00001 declares).

-- +goose Up

CREATE TABLE IF NOT EXISTS work_item (
    tenant_id                tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    work_item_id             uuid         NOT NULL,
    item_version             cas_version  NOT NULL DEFAULT 1,

    -- An approval is a specialization of a work item, not a second table
    -- (WORK-001's REFACTOR clause). kind says which specialization, and
    -- approval_requirement_ref is the extra field the APPROVAL kind carries.
    kind                     text         NOT NULL,
    work_type                semantic_key NOT NULL,
    status                   text         NOT NULL,

    -- Correlation. workflow_instance_id and node_id are the WORK_ITEM_REQUIRED
    -- intent this item was created from; correlation_id is the identifier the
    -- Phase 1 acceptance contract traverses work, ledger and messaging by.
    correlation_id           semantic_key NOT NULL,
    workflow_instance_id     uuid         NOT NULL,
    node_id                  semantic_key NOT NULL,
    approval_requirement_ref text,
    proposal_ref             text,

    -- Subject. Typed references to the workers the item is about, never their
    -- facts: this table stores responsibility, not people.
    subject_refs             text[]       NOT NULL,

    -- Owner, in the three-kind sense described in the header.
    owner_kind               text         NOT NULL,
    owner_ref                semantic_key NOT NULL,
    policy_route_ref         semantic_key NOT NULL,

    -- Visibility and the organizational frame it is evaluated in.
    visibility               text         NOT NULL,
    organization_scope_id    semantic_key NOT NULL,

    -- Deadline. A business instant supplied by the caller; nothing here reads
    -- a clock, and nothing here fires when it passes.
    deadline_at              timestamptz  NOT NULL,

    -- WORK-002 assignment evidence: the resolution expression, the candidate
    -- set, the exclusions with their rule ids, the chosen owner, the
    -- relationship and policy versions, and the trigger that made this
    -- resolution run. It is one JSON document plus its digest because it is
    -- read as a whole unit of evidence, never queried field by field.
    assignment               jsonb        NOT NULL DEFAULT '{}'::jsonb,
    assignment_digest        text,

    -- WORK-003 exclusive claim. All four move together or not at all.
    claim_id                 uuid,
    claimed_by               text,
    claimed_at               timestamptz,
    claim_expires_at         timestamptz,

    -- Completion. The output digest is immutable once set; the trigger below
    -- enforces that against any writer, including this repository's own store.
    completed_by             text,
    completed_at             timestamptz,
    completed_output_digest  text,

    created_at               timestamptz  NOT NULL,
    recorded_at              timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, work_item_id),
    FOREIGN KEY (tenant_id, workflow_instance_id)
        REFERENCES workflow_instance (tenant_id, instance_id),

    CONSTRAINT work_item_kind_allowed CHECK (kind IN ('APPROVAL', 'TASK')),
    -- The APPROVAL specialization must name the requirement it decides; a TASK
    -- must not carry one, so an approval requirement can never be smuggled
    -- onto an item whose completion is not an approval decision.
    CONSTRAINT work_item_approval_requires_requirement CHECK (
        (kind = 'APPROVAL') = (approval_requirement_ref IS NOT NULL)
    ),
    -- The exact status set internal/humanwork/workitem.Statuses() returns.
    CONSTRAINT work_item_status_allowed CHECK (
        status IN (
            'CREATED', 'ROUTED', 'ASSIGNED', 'AVAILABLE', 'CLAIMED',
            'IN_PROGRESS', 'COMPLETED', 'RETURNED', 'ESCALATED', 'EXPIRED',
            'CANCELLED'
        )
    ),
    CONSTRAINT work_item_owner_kind_allowed CHECK (
        owner_kind IN ('POLICY_ROUTE', 'CANDIDATE_SET', 'PRINCIPAL')
    ),
    CONSTRAINT work_item_visibility_allowed CHECK (
        visibility IN (
            'ASSIGNEE_ONLY', 'CANDIDATE_SET', 'ORGANIZATION_SCOPE',
            'TENANT_GOVERNANCE'
        )
    ),
    CONSTRAINT work_item_subject_refs_present CHECK (
        cardinality(subject_refs) >= 1
    ),
    CONSTRAINT work_item_assignment_object CHECK (
        jsonb_typeof(assignment) = 'object'
    ),
    CONSTRAINT work_item_assignment_digest_shape CHECK (
        assignment_digest IS NULL OR assignment_digest ~ '^sha256:[0-9a-f]{64}$'
    ),
    -- A claim is held by exactly one principal, for a bounded window, in
    -- exactly the two states that mean somebody holds the item. A CLAIMED row
    -- with no holder, or an AVAILABLE row with one, would be a double-owner
    -- bug that no amount of Go-side checking could rule out.
    CONSTRAINT work_item_claim_all_or_none CHECK (
        (claim_id IS NULL AND claimed_by IS NULL AND claimed_at IS NULL
            AND claim_expires_at IS NULL)
        OR (claim_id IS NOT NULL AND claimed_by IS NOT NULL AND claimed_at IS NOT NULL
            AND claim_expires_at IS NOT NULL)
    ),
    CONSTRAINT work_item_claim_matches_status CHECK (
        (status IN ('CLAIMED', 'IN_PROGRESS')) = (claim_id IS NOT NULL)
    ),
    CONSTRAINT work_item_claim_window_positive CHECK (
        claim_expires_at IS NULL OR claim_expires_at > claimed_at
    ),
    CONSTRAINT work_item_completion_all_or_none CHECK (
        (completed_by IS NULL AND completed_at IS NULL AND completed_output_digest IS NULL)
        OR (completed_by IS NOT NULL AND completed_at IS NOT NULL
            AND completed_output_digest IS NOT NULL)
    ),
    CONSTRAINT work_item_completion_matches_status CHECK (
        (status = 'COMPLETED') = (completed_output_digest IS NOT NULL)
    ),
    CONSTRAINT work_item_output_digest_shape CHECK (
        completed_output_digest IS NULL
            OR completed_output_digest ~ '^sha256:[0-9a-f]{64}$'
    )
);

-- "What is open for this instance", which is what ListForInstance answers and
-- what a frontier resumption asks after a restart.
CREATE INDEX IF NOT EXISTS work_item_instance
    ON work_item (tenant_id, workflow_instance_id, status);

CREATE INDEX IF NOT EXISTS work_item_correlation
    ON work_item (tenant_id, correlation_id);

-- The claimant's own queue, and the deadline projection a caller drives. It is
-- an index, not a sweeper: nothing in this repository reads it on a timer.
CREATE INDEX IF NOT EXISTS work_item_owner
    ON work_item (tenant_id, owner_kind, owner_ref, status);

CREATE INDEX IF NOT EXISTS work_item_deadline
    ON work_item (tenant_id, status, deadline_at);


CREATE TABLE IF NOT EXISTS work_item_transition (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    transition_id      uuid         NOT NULL,
    work_item_id       uuid         NOT NULL,
    -- The item_version the item holds AFTER this transition. The UNIQUE below
    -- makes the evidence chain gap-free and fork-free by construction: two
    -- writers cannot both claim to have produced version n.
    item_version       cas_version  NOT NULL,

    -- NULL only on the creation row, where there is no previous status to
    -- name. Every other row states both ends of the edge it recorded.
    from_status        text,
    to_status          text         NOT NULL,

    actor_principal_id semantic_key NOT NULL,
    -- A typed reason code, not free prose: WORK-001's evidence is only useful
    -- if a projection can group by why things happened.
    reason             semantic_key NOT NULL,
    detail             text         NOT NULL DEFAULT '',
    evidence_ref       text,

    -- The business instant the transition happened at, supplied by the caller,
    -- kept distinct from the recording time the database stamps.
    at                 timestamptz  NOT NULL,
    recorded_at        timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, transition_id),
    CONSTRAINT work_item_transition_version_unique
        UNIQUE (tenant_id, work_item_id, item_version),
    FOREIGN KEY (tenant_id, work_item_id) REFERENCES work_item (tenant_id, work_item_id),

    CONSTRAINT work_item_transition_from_status_allowed CHECK (
        from_status IS NULL OR from_status IN (
            'CREATED', 'ROUTED', 'ASSIGNED', 'AVAILABLE', 'CLAIMED',
            'IN_PROGRESS', 'COMPLETED', 'RETURNED', 'ESCALATED', 'EXPIRED',
            'CANCELLED'
        )
    ),
    CONSTRAINT work_item_transition_to_status_allowed CHECK (
        to_status IN (
            'CREATED', 'ROUTED', 'ASSIGNED', 'AVAILABLE', 'CLAIMED',
            'IN_PROGRESS', 'COMPLETED', 'RETURNED', 'ESCALATED', 'EXPIRED',
            'CANCELLED'
        )
    ),
    -- Exactly one row has no predecessor, and it is the creation row.
    CONSTRAINT work_item_transition_origin CHECK (
        (from_status IS NULL) = (to_status = 'CREATED')
    ),
    CONSTRAINT work_item_transition_is_a_move CHECK (
        from_status IS NULL OR from_status <> to_status
    )
);

CREATE INDEX IF NOT EXISTS work_item_transition_item
    ON work_item_transition (tenant_id, work_item_id, item_version);

CREATE OR REPLACE TRIGGER work_item_transition_append_only
    BEFORE UPDATE OR DELETE ON work_item_transition
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON work_item_transition FROM PUBLIC;


-- The completed output digest is immutable once set, and so is the identity a
-- work item was created with. These are the two mutations WORK-001's RED
-- clause names that an UPDATE-granting table cannot otherwise refuse: the
-- column-level guarantee has to be a trigger, because a CHECK constraint sees
-- only the new row and never the old one.
-- +goose StatementBegin
CREATE FUNCTION work_item_forbid_rewrite() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.completed_output_digest IS NOT NULL
        AND NEW.completed_output_digest IS DISTINCT FROM OLD.completed_output_digest THEN
        RAISE EXCEPTION
            'work_item %: completed output digest is immutable once set', OLD.work_item_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.work_item_id IS DISTINCT FROM OLD.work_item_id
        OR NEW.correlation_id IS DISTINCT FROM OLD.correlation_id
        OR NEW.workflow_instance_id IS DISTINCT FROM OLD.workflow_instance_id
        OR NEW.node_id IS DISTINCT FROM OLD.node_id
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.approval_requirement_ref IS DISTINCT FROM OLD.approval_requirement_ref
        OR NEW.policy_route_ref IS DISTINCT FROM OLD.policy_route_ref
        OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION
            'work_item %: identity, correlation and policy route are immutable', OLD.work_item_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    -- The version is a compare-and-swap token, so it only ever goes up.
    IF NEW.item_version <= OLD.item_version THEN
        RAISE EXCEPTION
            'work_item %: item_version must advance (% -> %)',
            OLD.work_item_id, OLD.item_version, NEW.item_version
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION work_item_forbid_rewrite() IS
    'WORK-001: a completed output digest, the item identity/correlation and the policy route may not be rewritten, and item_version may only advance.';

CREATE OR REPLACE TRIGGER work_item_no_rewrite
    BEFORE UPDATE ON work_item
    FOR EACH ROW EXECUTE FUNCTION work_item_forbid_rewrite();


-- Tenant isolation (DB-017), same shape as every policy in
-- migrations/00008_tenant_isolation.sql: fail closed on a missing/blank
-- app.tenant_id session setting, keyed on internal/data/tenancy.WithTenant.
ALTER TABLE work_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE work_item FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON work_item
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE work_item_transition ENABLE ROW LEVEL SECURITY;
ALTER TABLE work_item_transition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON work_item_transition
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- work_item is live serving state, so SELECT/INSERT/UPDATE, never DELETE (this
-- data plane has no delete semantics). work_item_transition is evidence, so
-- SELECT/INSERT only.
REVOKE DELETE ON work_item FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON work_item TO hcmnext_app;
GRANT SELECT, INSERT ON work_item_transition TO hcmnext_app;

-- +goose Down
REVOKE ALL ON work_item_transition FROM hcmnext_app;
REVOKE ALL ON work_item FROM hcmnext_app;
DROP POLICY tenant_isolation ON work_item_transition;
DROP POLICY tenant_isolation ON work_item;
ALTER TABLE work_item_transition NO FORCE ROW LEVEL SECURITY;
ALTER TABLE work_item_transition DISABLE ROW LEVEL SECURITY;
ALTER TABLE work_item NO FORCE ROW LEVEL SECURITY;
ALTER TABLE work_item DISABLE ROW LEVEL SECURITY;
DROP TRIGGER work_item_no_rewrite ON work_item;
DROP FUNCTION work_item_forbid_rewrite();
DROP TABLE work_item_transition;
DROP TABLE work_item;
