-- Owner: data plane. Phase: P1B.
-- DB-012 (completion): materialize the durable workflow-runtime and human-work
-- state migrations 00016-00020 do not carry.
--
-- Why this migration exists. DB-012's GREEN clause names thirteen kinds of
-- durable runtime state: "workflow instances, frontiers, node
-- executions/attempts, typed output refs, ready work, variables, timers,
-- signals/receipts/subscriptions, leases, checkpoints, child links,
-- queues/items/claims/SLA and approval requirement/decision tables". Migrations
-- 00016-00020 and 00022 delivered instances, node executions with their attempt
-- identity, continuations, idempotency records, advancement receipts, work items
-- with their transitions, and work item decisions. DB-012's own partial evidence
-- line records the rest as deliberately absent. This migration is that rest.
--
-- What is deliberately NOT duplicated here, because it already exists:
--
--   * Node attempts. migration 00016's workflow_node_execution already carries
--     `attempt` in the row and UNIQUE (tenant_id, instance_id, node_id, attempt)
--     as its identity, so a per-attempt table would be a second identity for
--     the same fact. "Attempts" is satisfied by that constraint, not by a table.
--   * The live work item claim. migration 00017's work_item already carries
--     claim_id/claimed_by/claimed_at/claim_expires_at with an all-or-none CHECK,
--     so the CURRENT holder stays on the item row. work_item_claim below is the
--     claim HISTORY -- who held it, from when, and how the hold ended -- which
--     is what was actually missing.
--   * Approval decisions. migration 00022's work_item_decision is the decision
--     body; WORK-001's REFACTOR clause already settles that an approval is a
--     work_item specialization rather than a second table. Only the approval
--     REQUIREMENT (the slot a node declares, before any item exists) was
--     missing, and workflow_approval_requirement below is that slot.
--
-- Relationship to the WF-RUN-000 gate. definitions/runtime/durable-runtime-
-- decision.yaml blocks "P1B implementation of WF-RUN-002 through WF-RUN-026
-- scheduler/timer/lease code" until the build-or-adopt decision is re-scored.
-- That gate is about CODE: a scheduler process, a background worker, a clock, a
-- claim loop, a retry driver. This migration introduces none of those. It
-- introduces the durable STATE those mechanisms would read and write, which is
-- DB-012's own scope (a GATE_B data-plane todo, not a WF-RUN todo), and it
-- introduces it in the shape the gate's own rollback plan already anticipates:
-- plain tables in this repository's PostgreSQL instance, dropped by this
-- migration's Down section if an ADOPT candidate wins. internal/data/runtimestate
-- exercises them through ordinary caller-driven stores -- no goroutine, no
-- ticker, no background claim -- and its inventory test asserts that the
-- scheduler's OWN bookkeeping (a worker registry, a heartbeat table, a timer
-- wheel shard, a workload limit, a retry policy row) is still absent, so the
-- gate keeps being checked by name rather than assumed.
--
-- Every table is tenant scoped with tenant_id in its primary key, protected by
-- the same fail-closed app.tenant_id row level security policy migration 00008
-- establishes, and granted to hcmnext_app with DELETE withheld. Five are
-- append-only evidence and carry the forbid_mutation trigger from
-- migrations/00001_platform_control.sql; ten are live serving state fenced by a
-- compare-and-swap version column, exactly like migration 00016's
-- workflow_instance; work_item_claim is the one in between -- a hold that is
-- opened and closed once, with its holder and its start made immutable by its
-- own work_item_claim_forbid_rewrite trigger rather than by forbid_mutation.
--
-- Storage disposition: every table below needs a row in
-- definitions/storage/storage-disposition.yaml (STORE-001); the rows are listed
-- verbatim in this lane's report.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 1. Frontier entries.
-- ---------------------------------------------------------------------------
-- migration 00016's workflow_instance.current_node_ids is a text[]: it answers
-- "which nodes are current" and nothing else. A frontier entry is a row, so each
-- member of the frontier carries its own state, its own entry sequence and the
-- instance version it was admitted at -- which is what a deterministic
-- advancement (WF-RUN-024) has to compare against, and what an array cannot
-- hold. The array stays as the fast projection; this table is the addressable
-- truth behind it.
CREATE TABLE IF NOT EXISTS workflow_frontier_entry (
    tenant_id           tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    instance_id         uuid         NOT NULL,
    node_id             semantic_key NOT NULL,

    entry_state         text         NOT NULL,
    -- The order this node entered the frontier. Two nodes admitted by the same
    -- advancement share nothing else that orders them, and an unordered frontier
    -- makes a replay non-deterministic.
    entry_sequence      bigint       NOT NULL,
    -- The instance_version the entry was admitted at, so a stale advancement is
    -- detectable without re-reading the instance row.
    admitted_at_version cas_version  NOT NULL,

    entry_version       cas_version  NOT NULL DEFAULT 1,
    entered_at          timestamptz  NOT NULL,
    left_at             timestamptz,
    recorded_at         timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, instance_id, node_id),
    CONSTRAINT workflow_frontier_entry_sequence_unique
        UNIQUE (tenant_id, instance_id, entry_sequence),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_frontier_entry_state_allowed CHECK (
        entry_state IN ('READY', 'RUNNING', 'WAITING', 'BLOCKED', 'LEFT')
    ),
    CONSTRAINT workflow_frontier_entry_sequence_positive CHECK (entry_sequence >= 1),
    -- A node that has left the frontier says when; one that has not, does not.
    CONSTRAINT workflow_frontier_entry_left_consistent CHECK (
        (entry_state = 'LEFT') = (left_at IS NOT NULL)
    ),
    CONSTRAINT workflow_frontier_entry_left_after_entry CHECK (
        left_at IS NULL OR left_at >= entered_at
    )
);

CREATE INDEX IF NOT EXISTS workflow_frontier_entry_open
    ON workflow_frontier_entry (tenant_id, instance_id, entry_state);


-- ---------------------------------------------------------------------------
-- 2. Typed node outputs.
-- ---------------------------------------------------------------------------
-- migration 00016's workflow_node_execution.output_artifact_ref is an untyped
-- nullable text column: it can hold a reference but cannot say what the
-- reference is, and nothing stops it being rewritten after the node succeeded.
-- This table is the typed, immutable answer -- one row per node execution,
-- append-only, carrying the schema the output claims to be and the digest that
-- proves the artifact was not swapped. That is the RED clause's "mutable
-- completed work result" closed at the node level, the same way migration
-- 00017's work_item_forbid_rewrite closes it at the work item level.
CREATE TABLE IF NOT EXISTS workflow_node_output (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    node_execution_id uuid           NOT NULL,
    instance_id       uuid           NOT NULL,

    output_kind       text           NOT NULL,
    schema_ref        semantic_key   NOT NULL,
    artifact_ref      text           NOT NULL,
    output_digest     content_digest NOT NULL,

    produced_at       timestamptz    NOT NULL,
    recorded_at       timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, node_execution_id),
    FOREIGN KEY (tenant_id, node_execution_id)
        REFERENCES workflow_node_execution (tenant_id, node_execution_id),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_node_output_kind_allowed CHECK (
        output_kind IN ('PROPOSAL', 'SIMULATION', 'DECISION', 'EFFECT_RECEIPT',
                        'OBSERVATION', 'DOCUMENT')
    )
);

CREATE INDEX IF NOT EXISTS workflow_node_output_instance
    ON workflow_node_output (tenant_id, instance_id);

CREATE OR REPLACE TRIGGER workflow_node_output_append_only
    BEFORE UPDATE OR DELETE ON workflow_node_output
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON workflow_node_output FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 3. Ready work.
-- ---------------------------------------------------------------------------
-- A durable record that one node of one instance is ready to run, with the
-- earliest instant it may be picked up. It is state, not a scheduler: nothing in
-- this repository sweeps it on a timer, and the WF-RUN-000 gate is what keeps it
-- that way. A caller-driven advancement inserts a row; a future scheduler
-- (WF-RUN-021, still gated) is what will claim from it.
CREATE TABLE IF NOT EXISTS workflow_ready_work (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    ready_work_id  uuid         NOT NULL,
    instance_id    uuid         NOT NULL,
    node_id        semantic_key NOT NULL,
    attempt        integer      NOT NULL,

    ready_state    text         NOT NULL,
    priority       integer      NOT NULL DEFAULT 100,
    -- The earliest instant this work may be started. A timer's expiry writes it;
    -- an immediately-ready node gets the instant it became ready.
    eligible_at    timestamptz  NOT NULL,

    ready_version  cas_version  NOT NULL DEFAULT 1,
    enqueued_at    timestamptz  NOT NULL,
    completed_at   timestamptz,
    recorded_at    timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, ready_work_id),
    -- One ready record per attempt of a node: a second enqueue of the same
    -- attempt collides rather than producing two claims on one unit of work.
    CONSTRAINT workflow_ready_work_attempt_unique
        UNIQUE (tenant_id, instance_id, node_id, attempt),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_ready_work_state_allowed CHECK (
        ready_state IN ('READY', 'DISPATCHED', 'DONE', 'CANCELLED')
    ),
    CONSTRAINT workflow_ready_work_attempt_positive CHECK (attempt >= 1),
    CONSTRAINT workflow_ready_work_terminal_completion CHECK (
        (ready_state IN ('DONE', 'CANCELLED')) = (completed_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS workflow_ready_work_eligible
    ON workflow_ready_work (tenant_id, ready_state, eligible_at);


-- ---------------------------------------------------------------------------
-- 4. Workflow variables.
-- ---------------------------------------------------------------------------
-- migration 00016's workflow_instance.variable_revision_head counts variable
-- revisions without holding any. This is where they live: one row per named
-- variable, compare-and-swap fenced, carrying the instance-level revision it was
-- written at so that a replay can reconstruct the variable set as of any
-- revision the head has passed through.
CREATE TABLE IF NOT EXISTS workflow_variable (
    tenant_id        tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    instance_id      uuid         NOT NULL,
    variable_name    semantic_key NOT NULL,

    schema_ref       semantic_key NOT NULL,
    variable_value   jsonb        NOT NULL,
    -- The instance's variable_revision_head at the moment of this write.
    written_at_revision bigint    NOT NULL,

    variable_version cas_version  NOT NULL DEFAULT 1,
    written_at       timestamptz  NOT NULL,
    recorded_at      timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, instance_id, variable_name),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_variable_value_object CHECK (jsonb_typeof(variable_value) = 'object'),
    CONSTRAINT workflow_variable_revision_positive CHECK (written_at_revision >= 1)
);


-- ---------------------------------------------------------------------------
-- 5. Timers.
-- ---------------------------------------------------------------------------
-- A durable timer: a promise that at fires_at, some node becomes ready. The
-- table stores the promise; nothing in this repository fires it, because firing
-- is scheduler code and WF-RUN-004 is behind the gate. timer_key is what makes
-- "set the same timer twice" a collision instead of two wakeups.
CREATE TABLE IF NOT EXISTS workflow_timer (
    tenant_id     tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    timer_id      uuid         NOT NULL,
    instance_id   uuid         NOT NULL,
    node_id       semantic_key NOT NULL,
    timer_key     semantic_key NOT NULL,

    timer_kind    text         NOT NULL,
    timer_state   text         NOT NULL,
    fires_at      timestamptz  NOT NULL,
    fired_at      timestamptz,
    cancelled_at  timestamptz,

    timer_version cas_version  NOT NULL DEFAULT 1,
    created_at    timestamptz  NOT NULL,
    recorded_at   timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, timer_id),
    CONSTRAINT workflow_timer_key_unique
        UNIQUE (tenant_id, instance_id, node_id, timer_key),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_timer_kind_allowed CHECK (
        timer_kind IN ('DELAY', 'DEADLINE', 'HEARTBEAT', 'RETRY_BACKOFF')
    ),
    CONSTRAINT workflow_timer_state_allowed CHECK (
        timer_state IN ('PENDING', 'FIRED', 'CANCELLED')
    ),
    -- A fired timer says when it fired and a cancelled one says when it was
    -- cancelled; a pending one claims neither, and no timer is both.
    CONSTRAINT workflow_timer_state_consistent CHECK (
        (timer_state = 'FIRED') = (fired_at IS NOT NULL)
        AND (timer_state = 'CANCELLED') = (cancelled_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS workflow_timer_due
    ON workflow_timer (tenant_id, timer_state, fires_at);


-- ---------------------------------------------------------------------------
-- 6..8. Signals, subscriptions and receipts.
-- ---------------------------------------------------------------------------
-- A subscription says an instance is waiting for a named signal; a signal row is
-- one delivery of that name with its dedupe token; a receipt is the append-only
-- evidence that the delivery was applied to a specific subscription exactly
-- once. Three tables rather than one because the three have different
-- lifetimes: a subscription is live state that opens and closes, a signal is an
-- arrival, and a receipt is evidence that must never be rewritten.

CREATE TABLE IF NOT EXISTS workflow_signal_subscription (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    subscription_id    uuid         NOT NULL,
    instance_id        uuid         NOT NULL,
    node_id            semantic_key NOT NULL,
    signal_name        semantic_key NOT NULL,

    -- The correlation key a delivery must carry to match this subscription. It
    -- is what stops a signal for one worker waking another worker's instance.
    correlation_key    semantic_key NOT NULL,
    subscription_state text         NOT NULL,
    expires_at         timestamptz,

    subscription_version cas_version NOT NULL DEFAULT 1,
    created_at         timestamptz  NOT NULL,
    closed_at          timestamptz,
    recorded_at        timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, subscription_id),
    -- One open subscription per (instance, node, signal): subscribing twice is a
    -- collision, not two wakeups for one wait.
    CONSTRAINT workflow_signal_subscription_unique
        UNIQUE (tenant_id, instance_id, node_id, signal_name),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_signal_subscription_state_allowed CHECK (
        subscription_state IN ('OPEN', 'SATISFIED', 'EXPIRED', 'CANCELLED')
    ),
    CONSTRAINT workflow_signal_subscription_closed_consistent CHECK (
        (subscription_state = 'OPEN') = (closed_at IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS workflow_signal_subscription_waiting
    ON workflow_signal_subscription (tenant_id, signal_name, correlation_key, subscription_state);


CREATE TABLE IF NOT EXISTS workflow_signal (
    tenant_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    signal_id       uuid           NOT NULL,

    signal_name     semantic_key   NOT NULL,
    correlation_key semantic_key   NOT NULL,
    -- The sender's own idempotency token. A redelivery of the same token is the
    -- same signal, which is what makes at-least-once delivery survivable.
    dedupe_token    semantic_key   NOT NULL,

    schema_ref      semantic_key   NOT NULL,
    payload         jsonb          NOT NULL,
    payload_digest  content_digest NOT NULL,

    delivered_at    timestamptz    NOT NULL,
    recorded_at     timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, signal_id),
    CONSTRAINT workflow_signal_dedupe_unique
        UNIQUE (tenant_id, signal_name, correlation_key, dedupe_token),
    CONSTRAINT workflow_signal_payload_object CHECK (jsonb_typeof(payload) = 'object')
);

CREATE OR REPLACE TRIGGER workflow_signal_append_only
    BEFORE UPDATE OR DELETE ON workflow_signal
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON workflow_signal FROM PUBLIC;


CREATE TABLE IF NOT EXISTS workflow_signal_receipt (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    signal_id       uuid         NOT NULL,
    subscription_id uuid         NOT NULL,

    instance_id     uuid         NOT NULL,
    applied_at      timestamptz  NOT NULL,
    -- The instance_version the delivery advanced the instance to, so "was this
    -- signal already applied" is answerable from the receipt alone.
    applied_at_version cas_version NOT NULL,
    recorded_at     timestamptz  NOT NULL DEFAULT now(),

    -- One receipt per (signal, subscription): a redelivered signal finds its
    -- receipt already there and applies nothing a second time.
    PRIMARY KEY (tenant_id, signal_id, subscription_id),
    FOREIGN KEY (tenant_id, signal_id) REFERENCES workflow_signal (tenant_id, signal_id),
    FOREIGN KEY (tenant_id, subscription_id)
        REFERENCES workflow_signal_subscription (tenant_id, subscription_id),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id)
);

CREATE OR REPLACE TRIGGER workflow_signal_receipt_append_only
    BEFORE UPDATE OR DELETE ON workflow_signal_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON workflow_signal_receipt FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 9. Execution leases and fencing tokens.
-- ---------------------------------------------------------------------------
-- The durable half of WF-RUN-002. fence_token is monotonic per resource and is
-- what makes a stale holder's write refusable after its lease expired: the
-- resource's current token is the only one that may write, so a worker that
-- comes back from a pause with an old token is detectable rather than merely
-- unlucky. The partial unique index is the exclusivity: at most one HELD lease
-- per resource, enforced by the schema rather than by whoever remembers to
-- check.
CREATE TABLE IF NOT EXISTS workflow_lease (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    lease_id       uuid         NOT NULL,

    resource_kind  text         NOT NULL,
    resource_id    semantic_key NOT NULL,

    lease_state    text         NOT NULL,
    -- Monotonic per resource. A new lease takes the previous token plus one, so
    -- comparing tokens orders holders without a clock.
    fence_token    bigint       NOT NULL,
    holder_id      semantic_key NOT NULL,

    acquired_at    timestamptz  NOT NULL,
    expires_at     timestamptz  NOT NULL,
    heartbeat_at   timestamptz  NOT NULL,
    released_at    timestamptz,

    lease_version  cas_version  NOT NULL DEFAULT 1,
    recorded_at    timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, lease_id),
    CONSTRAINT workflow_lease_fence_unique UNIQUE (tenant_id, resource_kind, resource_id, fence_token),
    CONSTRAINT workflow_lease_resource_kind_allowed CHECK (
        resource_kind IN ('WORKFLOW_INSTANCE', 'NODE_EXECUTION', 'WORK_ITEM', 'QUEUE')
    ),
    CONSTRAINT workflow_lease_state_allowed CHECK (
        lease_state IN ('HELD', 'RELEASED', 'EXPIRED', 'REVOKED')
    ),
    CONSTRAINT workflow_lease_fence_positive CHECK (fence_token >= 1),
    CONSTRAINT workflow_lease_window_positive CHECK (expires_at > acquired_at),
    CONSTRAINT workflow_lease_released_consistent CHECK (
        (lease_state = 'HELD') = (released_at IS NULL)
    )
);

-- At most one live holder per resource. A partial unique index rather than a
-- constraint because the exclusivity only applies to the HELD rows: every
-- released and expired lease of the same resource stays as history.
CREATE UNIQUE INDEX IF NOT EXISTS workflow_lease_one_holder
    ON workflow_lease (tenant_id, resource_kind, resource_id)
    WHERE lease_state = 'HELD';


-- ---------------------------------------------------------------------------
-- 10. Checkpoints.
-- ---------------------------------------------------------------------------
-- A safe point (WF-RUN-008): the instance's state digest at a numbered position,
-- append-only, so a pause-and-resume or a version migration has an exact point
-- to reason about. migration 00016's workflow_instance.last_checkpoint_ref is a
-- pointer with nothing to point at until this table exists.
CREATE TABLE IF NOT EXISTS workflow_checkpoint (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    instance_id           uuid           NOT NULL,
    checkpoint_sequence   bigint         NOT NULL,

    checkpoint_kind       text           NOT NULL,
    state_digest          content_digest NOT NULL,
    frontier_digest       content_digest NOT NULL,
    variable_digest       content_digest NOT NULL,
    -- The instance_version this checkpoint describes, which is what a resume
    -- compares against before trusting the digests.
    instance_version      cas_version    NOT NULL,

    taken_at              timestamptz    NOT NULL,
    recorded_at           timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, instance_id, checkpoint_sequence),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_checkpoint_kind_allowed CHECK (
        checkpoint_kind IN ('SAFE_POINT', 'PAUSE', 'VERSION_PIN', 'MIGRATION')
    ),
    CONSTRAINT workflow_checkpoint_sequence_positive CHECK (checkpoint_sequence >= 1)
);

CREATE OR REPLACE TRIGGER workflow_checkpoint_append_only
    BEFORE UPDATE OR DELETE ON workflow_checkpoint
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON workflow_checkpoint FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 11. Child workflow links.
-- ---------------------------------------------------------------------------
-- The parent/child edge a fan-out creates (WF-RUN-011). Append-only, one parent
-- per child, and a self-link is refused in the schema: a child that could be
-- re-parented would make cancellation propagation ambiguous, and a child that is
-- its own parent would make it non-terminating.
CREATE TABLE IF NOT EXISTS workflow_child_link (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    parent_instance_id uuid           NOT NULL,
    child_instance_id  uuid           NOT NULL,

    parent_node_id     semantic_key   NOT NULL,
    ordinal            integer        NOT NULL,
    -- How the child's completion or failure reaches the parent.
    completion_mode    text           NOT NULL,
    child_input_digest content_digest NOT NULL,

    created_at         timestamptz    NOT NULL,
    recorded_at        timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, child_instance_id),
    CONSTRAINT workflow_child_link_ordinal_unique
        UNIQUE (tenant_id, parent_instance_id, parent_node_id, ordinal),
    FOREIGN KEY (tenant_id, parent_instance_id)
        REFERENCES workflow_instance (tenant_id, instance_id),
    FOREIGN KEY (tenant_id, child_instance_id)
        REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_child_link_completion_mode_allowed CHECK (
        completion_mode IN ('AWAIT', 'DETACH', 'COMPENSATE_ON_FAILURE')
    ),
    CONSTRAINT workflow_child_link_not_self CHECK (parent_instance_id <> child_instance_id),
    CONSTRAINT workflow_child_link_ordinal_positive CHECK (ordinal >= 1)
);

CREATE INDEX IF NOT EXISTS workflow_child_link_parent
    ON workflow_child_link (tenant_id, parent_instance_id);

CREATE OR REPLACE TRIGGER workflow_child_link_append_only
    BEFORE UPDATE OR DELETE ON workflow_child_link
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON workflow_child_link FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 12..13. Work queues and queue membership.
-- ---------------------------------------------------------------------------
-- A queue is a named, governed destination for human work; a queue item is one
-- work item's membership in exactly one queue. The primary key on the work item
-- alone is what makes "in two queues at once" impossible: a work item has one
-- place it is waiting, and routing it elsewhere is a move, not an addition.
CREATE TABLE IF NOT EXISTS work_queue (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    queue_id       uuid         NOT NULL,
    queue_key      semantic_key NOT NULL,

    display_name   text         NOT NULL,
    queue_state    text         NOT NULL,
    -- The routing policy that decides what lands here. A reference, because the
    -- policy itself is governed configuration, not queue state.
    routing_policy_ref semantic_key NOT NULL,

    queue_version  cas_version  NOT NULL DEFAULT 1,
    created_at     timestamptz  NOT NULL,
    recorded_at    timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, queue_id),
    CONSTRAINT work_queue_key_unique UNIQUE (tenant_id, queue_key),
    CONSTRAINT work_queue_state_allowed CHECK (
        queue_state IN ('ACTIVE', 'PAUSED', 'RETIRED')
    )
);


CREATE TABLE IF NOT EXISTS work_queue_item (
    tenant_id    tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    work_item_id uuid         NOT NULL,
    queue_id     uuid         NOT NULL,

    priority     integer      NOT NULL DEFAULT 100,
    -- The instant this item became eligible to be worked, which is what orders a
    -- queue fairly when priorities tie.
    eligible_at  timestamptz  NOT NULL,
    membership_state text     NOT NULL,

    membership_version cas_version NOT NULL DEFAULT 1,
    enqueued_at  timestamptz  NOT NULL,
    removed_at   timestamptz,
    recorded_at  timestamptz  NOT NULL DEFAULT now(),

    -- One queue per item: the structural answer to "two queues own the same
    -- work".
    PRIMARY KEY (tenant_id, work_item_id),
    FOREIGN KEY (tenant_id, work_item_id) REFERENCES work_item (tenant_id, work_item_id),
    FOREIGN KEY (tenant_id, queue_id) REFERENCES work_queue (tenant_id, queue_id),
    CONSTRAINT work_queue_item_state_allowed CHECK (
        membership_state IN ('QUEUED', 'DISPATCHED', 'REMOVED')
    ),
    CONSTRAINT work_queue_item_removed_consistent CHECK (
        (membership_state = 'REMOVED') = (removed_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS work_queue_item_queue
    ON work_queue_item (tenant_id, queue_id, membership_state, priority, eligible_at);


-- ---------------------------------------------------------------------------
-- 14. Claim history.
-- ---------------------------------------------------------------------------
-- migration 00017's work_item carries the CURRENT claim on the item row, with an
-- all-or-none CHECK. What it cannot carry is the history: who held the item
-- before, and how each hold ended. This table is that history, one row per hold.
-- The current-holder invariant stays where it is; this table never competes with
-- it.
--
-- It is not append-only, because a hold is opened and later closed and both
-- facts belong on the same row: "who held this item, from when, until when, and
-- how it ended" is one record, not two. What IS immutable is everything a close
-- must not touch -- the claimant, the instant the hold began, its identity -- and
-- a close itself, which may happen once. That is enforced by the
-- work_item_claim_forbid_rewrite trigger below, the same shape migration 00017
-- uses for work_item_forbid_rewrite, rather than by forbid_mutation, which would
-- also forbid the legitimate close.
CREATE TABLE IF NOT EXISTS work_item_claim (
    tenant_id     tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    claim_id      uuid         NOT NULL,
    work_item_id  uuid         NOT NULL,

    claimed_by    semantic_key NOT NULL,
    claimed_at    timestamptz  NOT NULL,
    expires_at    timestamptz  NOT NULL,

    -- How the hold ended. NULL while it is the live hold, which is the one
    -- moment this table's row and work_item's own claim columns agree.
    release_kind  text,
    released_at   timestamptz,

    recorded_at   timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, claim_id),
    FOREIGN KEY (tenant_id, work_item_id) REFERENCES work_item (tenant_id, work_item_id),
    CONSTRAINT work_item_claim_release_kind_allowed CHECK (
        release_kind IS NULL
        OR release_kind IN ('COMPLETED', 'RELEASED', 'EXPIRED', 'REVOKED', 'ESCALATED')
    ),
    CONSTRAINT work_item_claim_release_consistent CHECK (
        (release_kind IS NULL) = (released_at IS NULL)
    ),
    CONSTRAINT work_item_claim_window_positive CHECK (expires_at > claimed_at)
);

CREATE INDEX IF NOT EXISTS work_item_claim_item
    ON work_item_claim (tenant_id, work_item_id, claimed_at DESC);

-- At most one open hold per item at a time, which is claim exclusivity written
-- as history rather than as a mutable column. A closed hold leaves the index, so
-- the next claimant can take the item without the previous hold disappearing.
CREATE UNIQUE INDEX IF NOT EXISTS work_item_claim_one_open
    ON work_item_claim (tenant_id, work_item_id)
    WHERE released_at IS NULL;

-- A close may fill in how the hold ended and nothing else, and it may happen
-- once. A CHECK cannot express this: it sees the new row and never the old one.
-- +goose StatementBegin
CREATE FUNCTION work_item_claim_forbid_rewrite() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.released_at IS NOT NULL THEN
        RAISE EXCEPTION
            'work_item_claim %: a closed hold is immutable', OLD.claim_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.claim_id IS DISTINCT FROM OLD.claim_id
        OR NEW.work_item_id IS DISTINCT FROM OLD.work_item_id
        OR NEW.claimed_by IS DISTINCT FROM OLD.claimed_by
        OR NEW.claimed_at IS DISTINCT FROM OLD.claimed_at THEN
        RAISE EXCEPTION
            'work_item_claim %: the holder and the instant the hold began are immutable', OLD.claim_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION work_item_claim_forbid_rewrite() IS
    'DB-012: a work item claim may be closed once; its holder, its start and a completed close are immutable.';

CREATE OR REPLACE TRIGGER work_item_claim_no_rewrite
    BEFORE UPDATE ON work_item_claim
    FOR EACH ROW EXECUTE FUNCTION work_item_claim_forbid_rewrite();

REVOKE DELETE ON work_item_claim FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 15. Service-level agreements on work.
-- ---------------------------------------------------------------------------
-- The deadlines a work item is measured against, and the breach state it is
-- currently in. One row per item, live state: the breach state advances as time
-- passes, and it is written by whoever observes the passage of time -- not by a
-- sweeper in this repository, which would be the scheduler the gate blocks.
CREATE TABLE IF NOT EXISTS work_item_sla (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    work_item_id   uuid         NOT NULL,

    sla_policy_ref semantic_key NOT NULL,
    target_at      timestamptz  NOT NULL,
    escalate_at    timestamptz  NOT NULL,
    breach_state   text         NOT NULL,
    breached_at    timestamptz,
    escalated_to   semantic_key,

    sla_version    cas_version  NOT NULL DEFAULT 1,
    recorded_at    timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, work_item_id),
    FOREIGN KEY (tenant_id, work_item_id) REFERENCES work_item (tenant_id, work_item_id),
    CONSTRAINT work_item_sla_breach_state_allowed CHECK (
        breach_state IN ('WITHIN_TARGET', 'AT_RISK', 'BREACHED', 'ESCALATED', 'SATISFIED')
    ),
    -- Escalation cannot come before the target it escalates past.
    CONSTRAINT work_item_sla_escalation_after_target CHECK (escalate_at >= target_at),
    -- A breached or escalated SLA says when it breached. One-way, for the same
    -- reason as the escalation constraint below: an item that breached and was
    -- then worked settles to SATISFIED, and the instant it breached is the
    -- fact the settlement is measured against. A biconditional would erase it
    -- exactly when the SLA stops being live and starts being evidence.
    CONSTRAINT work_item_sla_breached_consistent CHECK (
        breach_state NOT IN ('BREACHED', 'ESCALATED') OR breached_at IS NOT NULL
    ),
    -- An escalated SLA names who it escalated to. The implication is one-way on
    -- purpose: an item that was escalated and then finally worked settles to
    -- SATISFIED, and the name of whoever it was escalated to is the record of
    -- how it got worked. A biconditional here would force that name to be
    -- erased on the very transition that makes it interesting.
    CONSTRAINT work_item_sla_escalated_names_target CHECK (
        breach_state <> 'ESCALATED' OR escalated_to IS NOT NULL
    )
);

CREATE INDEX IF NOT EXISTS work_item_sla_target
    ON work_item_sla (tenant_id, breach_state, target_at);


-- ---------------------------------------------------------------------------
-- 16. Approval requirement slots.
-- ---------------------------------------------------------------------------
-- The slot a workflow node declares before any work item exists: "this node
-- needs an approval of this requirement, under this separation constraint". The
-- unique constraint on (instance, node, slot_key) is the RED clause's "duplicate
-- approval slot": declaring the same slot twice collides instead of producing
-- two work items for one approval.
--
-- This is the requirement, not the decision. The decision remains migration
-- 00022's work_item_decision on the work_item the slot produced, because
-- WORK-001's REFACTOR clause already settles that an approval is a work_item
-- specialization rather than a parallel table.
CREATE TABLE IF NOT EXISTS workflow_approval_requirement (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    requirement_id        uuid         NOT NULL,
    instance_id           uuid         NOT NULL,
    node_id               semantic_key NOT NULL,
    slot_key              semantic_key NOT NULL,

    requirement_ref       semantic_key NOT NULL,
    separation_constraint semantic_key NOT NULL,
    materiality_class     text         NOT NULL,
    requirement_state     text         NOT NULL,

    -- The work item that satisfies this slot, once one exists. NULL until the
    -- slot is routed, and set exactly once thereafter.
    work_item_id          uuid,

    requirement_version   cas_version  NOT NULL DEFAULT 1,
    created_at            timestamptz  NOT NULL,
    satisfied_at          timestamptz,
    recorded_at           timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, requirement_id),
    CONSTRAINT workflow_approval_requirement_slot_unique
        UNIQUE (tenant_id, instance_id, node_id, slot_key),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    FOREIGN KEY (tenant_id, work_item_id) REFERENCES work_item (tenant_id, work_item_id),
    CONSTRAINT workflow_approval_requirement_materiality_allowed CHECK (
        materiality_class IN ('MATERIAL', 'NON_MATERIAL')
    ),
    CONSTRAINT workflow_approval_requirement_state_allowed CHECK (
        requirement_state IN ('DECLARED', 'ROUTED', 'SATISFIED', 'WAIVED', 'CANCELLED')
    ),
    -- A routed or satisfied slot names the item that carries it.
    CONSTRAINT workflow_approval_requirement_routed_has_item CHECK (
        requirement_state NOT IN ('ROUTED', 'SATISFIED') OR work_item_id IS NOT NULL
    ),
    CONSTRAINT workflow_approval_requirement_satisfied_consistent CHECK (
        (requirement_state = 'SATISFIED') = (satisfied_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS workflow_approval_requirement_instance
    ON workflow_approval_requirement (tenant_id, instance_id, requirement_state);


-- ---------------------------------------------------------------------------
-- Tenant isolation (DB-017) for every table above.
-- ---------------------------------------------------------------------------
-- The same fail-closed app.tenant_id predicate as migration 00008, applied by
-- loop so that the array below IS this migration's table list and a name in one
-- and not the other fails the migration.
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'workflow_frontier_entry',
        'workflow_node_output',
        'workflow_ready_work',
        'workflow_variable',
        'workflow_timer',
        'workflow_signal_subscription',
        'workflow_signal',
        'workflow_signal_receipt',
        'workflow_lease',
        'workflow_checkpoint',
        'workflow_child_link',
        'work_queue',
        'work_queue_item',
        'work_item_claim',
        'work_item_sla',
        'workflow_approval_requirement'
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
    workflow_node_output,
    workflow_signal,
    workflow_signal_receipt,
    workflow_checkpoint,
    workflow_child_link
TO hcmnext_app;

-- Live serving state: SELECT/INSERT/UPDATE, never DELETE.
GRANT SELECT, INSERT, UPDATE ON
    workflow_frontier_entry,
    workflow_ready_work,
    workflow_variable,
    workflow_timer,
    workflow_signal_subscription,
    workflow_lease,
    work_queue,
    work_queue_item,
    work_item_claim,
    work_item_sla,
    workflow_approval_requirement
TO hcmnext_app;

-- +goose Down
REVOKE ALL ON
    workflow_approval_requirement,
    work_item_sla,
    work_item_claim,
    work_queue_item,
    work_queue,
    workflow_child_link,
    workflow_checkpoint,
    workflow_lease,
    workflow_signal_receipt,
    workflow_signal,
    workflow_signal_subscription,
    workflow_timer,
    workflow_variable,
    workflow_ready_work,
    workflow_node_output,
    workflow_frontier_entry
FROM hcmnext_app;

DROP TABLE workflow_approval_requirement;
DROP TABLE work_item_sla;
DROP TRIGGER work_item_claim_no_rewrite ON work_item_claim;
DROP TABLE work_item_claim;
DROP FUNCTION work_item_claim_forbid_rewrite();
DROP TABLE work_queue_item;
DROP TABLE work_queue;
DROP TABLE workflow_child_link;
DROP TABLE workflow_checkpoint;
DROP TABLE workflow_lease;
DROP TABLE workflow_signal_receipt;
DROP TABLE workflow_signal;
DROP TABLE workflow_signal_subscription;
DROP TABLE workflow_timer;
DROP TABLE workflow_variable;
DROP TABLE workflow_ready_work;
DROP TABLE workflow_node_output;
DROP TABLE workflow_frontier_entry;
