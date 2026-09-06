-- Owner: data plane. Phase: P1A.
-- DB-011: materialize the BusinessIntent, proposal, decision and transaction
-- control aggregates.
--
-- What was already here, and what was missing. Migration 00004 created
-- intent_instance (the five lifecycle dimensions, the request digest, the
-- idempotency key) and proposal_revision (an immutable revision row carrying
-- one proposal digest). That is the spine. Everything else DB-011's GREEN
-- clause names -- the origin/causation/correlation/family/mode context an
-- IntentInstance carries, the HCMChangeRequest, the immutable input snapshot
-- and simulation result a preflight produces, the proposal's own
-- write/effect/approval/obligation sets, typed intent relationships and
-- results, decisions, certificates, and the whole transaction plan / commit
-- receipt / abort receipt / ambiguity / correction / RepairPlan / closure
-- chain from planning/specs/transaction-plan-and-commit-coordinator.md --
-- had nowhere to land. A proposal_revision row proved a digest existed; it
-- could not answer what the proposal proposed.
--
-- The RED clause names six defects; each one is answered by a structure here
-- rather than by a convention a caller is trusted to follow:
--
--   1. "proposal/snapshot/simulation mutates after publication" --
--      intent_input_snapshot, intent_simulation_result and every proposal set
--      table carry the forbid_mutation trigger from
--      migrations/00001_platform_control.sql and are granted SELECT/INSERT
--      only, exactly as migration 00004 already does for proposal_revision.
--   2. "trusted origin/feature/family is caller-spoofed" --
--      intent_instance_context.origin_trust is a closed vocabulary, and a row
--      claiming TRUSTED must name the source authority snapshot digest that
--      makes it trusted (intent_instance_context_trusted_origin_attested).
--      The context row is itself append-only, so a request cannot be
--      re-labelled trusted after the fact.
--   3. "intent relationship cycles or changes parentage" -- intent_relationship
--      refuses a self-edge in the schema, refuses a second parent for the same
--      (relationship_type, child) with a unique constraint, and is append-only
--      so an existing parentage cannot be rewritten. Deeper cycles
--      (A -> B -> C -> A) are a graph property no single-row constraint can
--      see; internal/data/intentcontrol's RelationshipStore.Link walks the
--      ancestor chain inside the caller's transaction and refuses the edge
--      before inserting it.
--   4. "result overwrites revision" -- intent_result's primary key is
--      (tenant_id, intent_id, revision) and the table is append-only, so a
--      second result for a revision collides instead of replacing the first.
--      The proposal_revision row it explains is a separate, already-immutable
--      row that this table never touches.
--   5. "correction lacks target" -- transaction_correction.corrects_receipt_id
--      is NOT NULL and foreign-keyed to a commit receipt in the same tenant. A
--      correction with no target cannot be inserted at all.
--   6. "transaction/repair/closure loses intent/proposal/control digest" --
--      transaction_plan, transaction_commit_receipt, transaction_abort_receipt,
--      transaction_correction, repair_plan and intent_closure each carry
--      intent_digest, proposal_digest and control_digest as NOT NULL columns.
--      There is no insert path that writes one of these rows without all three.
--
-- Typed, not free JSON (the REFACTOR clause). Every payload-shaped column here
-- is either a digest, a governed artifact reference, or a jsonb column paired
-- with a NOT NULL schema_ref naming the schema the document must validate
-- against, plus a jsonb_typeof(...) = 'object' check. The pattern is migration
-- 00022's work_item_decision.decision_body: a document read back as one unit of
-- evidence, never queried field by field, and never accepted without the schema
-- it claims to be.
--
-- Append-only versus live state. Seventeen of these twenty-one tables are
-- evidence: they carry the forbid_mutation trigger and a SELECT/INSERT-only
-- grant. Four are live serving state whose status genuinely advances --
-- hcm_change_request, transaction_plan_binding, transaction_ambiguity and
-- repair_plan -- and those are UPDATE-granted and fenced by a compare-and-swap
-- version column instead. DELETE is granted nowhere, the same rule migration
-- 00008's header states for the whole data plane.
--
-- Storage disposition. Every table below needs a row in
-- definitions/storage/storage-disposition.yaml (STORE-001); the rows are listed
-- verbatim in this lane's report, and append_only: true is claimed only for the
-- tables that actually carry the trigger.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied verbatim from
-- migrations/00022_work_item_decision.sql's own copy of the
-- migrations/00008_tenant_isolation.sql pattern, keyed on
-- internal/data/tenancy.WithTenant.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 1. IntentInstance context: origin, causation, correlation, family, mode.
-- ---------------------------------------------------------------------------
-- One row per intent, written in the same transaction as the intent_instance
-- row it extends. It is a separate table rather than columns bolted onto
-- migration 00004's intent_instance because these facts are fixed at creation
-- and never transition, while intent_instance's five dimensions transition on
-- every command: keeping them apart is what lets this table be append-only
-- while intent_instance stays UPDATE-granted.
CREATE TABLE IF NOT EXISTS intent_instance_context (
    tenant_id                        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    intent_id                        uuid           NOT NULL,

    -- The intent family and execution mode, spelled exactly as
    -- internal/intent.Family and internal/intent.Mode name them.
    intent_family                    text           NOT NULL,
    execution_mode                   text           NOT NULL,

    -- FeatureIntentCoverage (INTENT-010). feature_ref names the coverage entry
    -- the calling feature is registered under; feature_coverage_digest pins the
    -- exact registry revision it was resolved against, so a feature that later
    -- changes its declared intent set does not silently re-explain an intent
    -- already created. INTENT-010's registry does not exist yet; when it lands,
    -- its loader validates feature_ref against the registry and this column
    -- stops being merely well-formed.
    feature_ref                      semantic_key   NOT NULL,
    feature_coverage_digest          content_digest NOT NULL,

    -- Origin. origin_trust is the anti-spoofing dimension: a caller may claim
    -- an origin, but only an origin attested by a source authority snapshot may
    -- be recorded TRUSTED (see the CHECK below).
    origin_trust                     text           NOT NULL,
    origin_kind                      text           NOT NULL,
    origin_event_ref                 text,
    source_authority_snapshot_digest content_digest,

    -- Causation and correlation. correlation_id is required (every intent
    -- belongs to a correlation); causation_id is null exactly for a root
    -- intent, which is what makes "what caused this" answerable without a
    -- sentinel value.
    correlation_id                   semantic_key   NOT NULL,
    causation_id                     semantic_key,
    trace_id                         semantic_key   NOT NULL,

    -- Initiator, purpose and classification. These are material -- they are
    -- hashed into the canonical request digest by internal/intent -- so they
    -- are recorded here as evidence of what was hashed, never as an editable
    -- label.
    initiator_kind                   text           NOT NULL,
    initiator_principal_id           semantic_key   NOT NULL,
    identity_assurance_ref           semantic_key   NOT NULL,
    purpose                          semantic_key   NOT NULL,
    classification                   semantic_key   NOT NULL,
    retention_class                  semantic_key   NOT NULL,

    -- The pinned control context (internal/intent.ControlSnapshots) reduced to
    -- one digest over the canonically ordered snapshot set, plus the risk
    -- context digest, which is not part of it.
    control_digest                   content_digest NOT NULL,
    risk_context_digest              content_digest NOT NULL,

    recorded_at                      timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, intent_id),
    CONSTRAINT intent_instance_context_intent
        FOREIGN KEY (tenant_id, intent_id) REFERENCES intent_instance (tenant_id, intent_id),

    CONSTRAINT intent_instance_context_family_allowed CHECK (
        intent_family IN ('CHANGE_REQUEST', 'CALCULATION_REQUEST', 'ANALYTICAL_REQUEST')
    ),
    CONSTRAINT intent_instance_context_mode_allowed CHECK (
        execution_mode IN ('SIMULATE', 'EXECUTE', 'REPLAY', 'REPAIR', 'SHADOW')
    ),
    CONSTRAINT intent_instance_context_initiator_allowed CHECK (
        initiator_kind IN ('HUMAN', 'AGENT', 'SERVICE', 'INTEGRATION', 'SCHEDULE',
                           'RULE', 'SYSTEM_EVENT')
    ),
    CONSTRAINT intent_instance_context_origin_trust_allowed CHECK (
        origin_trust IN ('TRUSTED', 'ASSERTED', 'UNVERIFIED')
    ),
    -- A TRUSTED origin must name the source authority snapshot that attests it,
    -- and a non-trusted one must not pretend to have one.
    CONSTRAINT intent_instance_context_trusted_origin_attested CHECK (
        (origin_trust = 'TRUSTED') = (source_authority_snapshot_digest IS NOT NULL)
    ),
    -- A caused intent may not name itself as its own cause.
    CONSTRAINT intent_instance_context_causation_not_self CHECK (
        causation_id IS NULL OR causation_id <> intent_id::text
    )
);

CREATE INDEX IF NOT EXISTS intent_instance_context_correlation
    ON intent_instance_context (tenant_id, correlation_id);

CREATE OR REPLACE TRIGGER intent_instance_context_append_only
    BEFORE UPDATE OR DELETE ON intent_instance_context
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON intent_instance_context FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 2. HCMChangeRequest.
-- ---------------------------------------------------------------------------
-- The governed change request an intent of family CHANGE_REQUEST realizes: the
-- human- and API-facing envelope that carries a request through preflight,
-- submission, decision and closure. Unlike most of this migration it is live
-- serving state (its status advances), so it is UPDATE-granted and fenced by a
-- compare-and-swap request_version rather than by the forbid_mutation trigger.
CREATE TABLE IF NOT EXISTS hcm_change_request (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    change_request_id  uuid           NOT NULL,
    intent_id          uuid           NOT NULL,

    request_kind       semantic_key   NOT NULL,
    subject_ref        semantic_key   NOT NULL,
    requested_by       semantic_key   NOT NULL,
    requested_at       timestamptz    NOT NULL,

    -- Business validity of the requested change, half-open [from, to), the same
    -- shape migration 00002 establishes for every effective interval.
    effective_from     timestamptz    NOT NULL,
    effective_to       timestamptz,

    request_status     text           NOT NULL,
    request_version    cas_version    NOT NULL DEFAULT 1,

    -- The exact digests this request is bound to. Losing either would make the
    -- request unexplainable, which is the RED clause's sixth defect.
    intent_digest      content_digest NOT NULL,
    control_digest     content_digest NOT NULL,

    recorded_at        timestamptz    NOT NULL DEFAULT now(),
    last_transition_at timestamptz    NOT NULL,

    PRIMARY KEY (tenant_id, change_request_id),
    CONSTRAINT hcm_change_request_intent_unique UNIQUE (tenant_id, intent_id),
    CONSTRAINT hcm_change_request_intent
        FOREIGN KEY (tenant_id, intent_id) REFERENCES intent_instance (tenant_id, intent_id),
    CONSTRAINT hcm_change_request_status_allowed CHECK (
        request_status IN ('DRAFT', 'PREFLIGHTED', 'SUBMITTED', 'APPROVED', 'REJECTED',
                           'WITHDRAWN', 'COMMITTED', 'CLOSED')
    ),
    CONSTRAINT hcm_change_request_effective_order CHECK (
        effective_to IS NULL OR effective_to > effective_from
    ),
    CONSTRAINT hcm_change_request_transition_after_request CHECK (
        last_transition_at >= requested_at
    )
);

CREATE INDEX IF NOT EXISTS hcm_change_request_subject
    ON hcm_change_request (tenant_id, subject_ref);


-- ---------------------------------------------------------------------------
-- 3. Immutable input snapshots.
-- ---------------------------------------------------------------------------
-- internal/intent.BaselineSnapshot as stored evidence: the exact baseline a
-- preflight or simulation read, so a later disagreement about "what the data
-- said at the time" is answered from the record instead of re-read from a store
-- that has since moved on.
CREATE TABLE IF NOT EXISTS intent_input_snapshot (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    snapshot_id       uuid           NOT NULL,
    intent_id         uuid           NOT NULL,

    -- Which read this snapshot was taken for. One intent is preflighted and
    -- simulated at different moments against different baselines, so the
    -- purpose is part of the identity, not a label.
    snapshot_purpose  text           NOT NULL,
    -- Monotonic per (intent, purpose): the second preflight of the same intent
    -- gets sequence 2, it does not overwrite sequence 1.
    snapshot_sequence bigint         NOT NULL,

    observed_at       timestamptz    NOT NULL,
    snapshot_digest   content_digest NOT NULL,

    -- The typed body and the schema it must validate against. Never
    -- unrestricted JSON: schema_ref is NOT NULL and the document must be an
    -- object, the same contract migration 00022 puts on decision_body.
    schema_ref        semantic_key   NOT NULL,
    snapshot_body     jsonb          NOT NULL,

    recorded_at       timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, snapshot_id),
    CONSTRAINT intent_input_snapshot_sequence_unique
        UNIQUE (tenant_id, intent_id, snapshot_purpose, snapshot_sequence),
    CONSTRAINT intent_input_snapshot_intent
        FOREIGN KEY (tenant_id, intent_id) REFERENCES intent_instance (tenant_id, intent_id),
    CONSTRAINT intent_input_snapshot_purpose_allowed CHECK (
        snapshot_purpose IN ('PREFLIGHT', 'SIMULATION', 'REVALIDATION', 'REPAIR')
    ),
    CONSTRAINT intent_input_snapshot_sequence_positive CHECK (snapshot_sequence >= 1),
    CONSTRAINT intent_input_snapshot_body_object CHECK (jsonb_typeof(snapshot_body) = 'object')
);

CREATE OR REPLACE TRIGGER intent_input_snapshot_append_only
    BEFORE UPDATE OR DELETE ON intent_input_snapshot
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON intent_input_snapshot FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 4. Immutable simulation results.
-- ---------------------------------------------------------------------------
-- What a simulation of one proposal revision produced, bound to the input
-- snapshot it consumed. One row per (intent, revision, sequence): re-running a
-- simulation adds a row, it never edits the previous answer.
CREATE TABLE IF NOT EXISTS intent_simulation_result (
    tenant_id           tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    simulation_id       uuid           NOT NULL,
    intent_id           uuid           NOT NULL,
    revision            cas_version    NOT NULL,
    simulation_sequence bigint         NOT NULL,

    -- The snapshot this simulation consumed. NOT NULL: a simulation whose
    -- inputs are unknown proves nothing, so there is no such row.
    input_snapshot_id   uuid           NOT NULL,

    simulation_status   text           NOT NULL,
    result_digest       content_digest NOT NULL,
    proposal_digest     content_digest NOT NULL,
    control_digest      content_digest NOT NULL,

    schema_ref          semantic_key   NOT NULL,
    result_body         jsonb          NOT NULL,

    simulated_at        timestamptz    NOT NULL,
    recorded_at         timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, simulation_id),
    CONSTRAINT intent_simulation_result_sequence_unique
        UNIQUE (tenant_id, intent_id, revision, simulation_sequence),
    CONSTRAINT intent_simulation_result_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT intent_simulation_result_snapshot
        FOREIGN KEY (tenant_id, input_snapshot_id)
        REFERENCES intent_input_snapshot (tenant_id, snapshot_id),
    CONSTRAINT intent_simulation_result_status_allowed CHECK (
        simulation_status IN ('READY', 'BLOCKED', 'REJECTED', 'INCONCLUSIVE')
    ),
    CONSTRAINT intent_simulation_result_sequence_positive CHECK (simulation_sequence >= 1),
    CONSTRAINT intent_simulation_result_body_object CHECK (jsonb_typeof(result_body) = 'object')
);

CREATE OR REPLACE TRIGGER intent_simulation_result_append_only
    BEFORE UPDATE OR DELETE ON intent_simulation_result
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON intent_simulation_result FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 5..8. The proposal revision's own sets.
-- ---------------------------------------------------------------------------
-- internal/intent.ProposalRevision carries Writes, Effects, RequiredApprovals
-- and Obligations as slices. Migration 00004 stored only the revision's digest,
-- so those four sets existed in Go and nowhere else. Each becomes its own
-- append-only child table keyed on the revision's own primary key
-- (tenant_id, intent_id, revision), with an ordinal that pins the canonical
-- order the digest was computed over -- without it, two orderings of the same
-- set would look like the same proposal but hash differently.

CREATE TABLE IF NOT EXISTS proposal_write_item (
    tenant_id                 tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    intent_id                 uuid         NOT NULL,
    revision                  cas_version  NOT NULL,
    ordinal                   int          NOT NULL,

    subject_kind              semantic_key NOT NULL,
    subject_id                semantic_key NOT NULL,
    resource_key              semantic_key NOT NULL,
    field_path                semantic_key NOT NULL,

    -- The canonical text of the current and proposed values. Text, not a typed
    -- column per data type: the proposal's digest is computed over exactly
    -- these canonical strings, so storing anything else would store something
    -- the digest does not cover.
    current_canonical_text    text         NOT NULL,
    proposed_canonical_text   text         NOT NULL,

    -- The revision the write expects to find, which is what makes a planned
    -- append's optimistic concurrency check possible at commit time.
    expected_revision         text         NOT NULL,
    source_authority_decision text         NOT NULL,

    recorded_at               timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, intent_id, revision, ordinal),
    CONSTRAINT proposal_write_item_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT proposal_write_item_ordinal_positive CHECK (ordinal >= 1),
    -- A write that proposes what is already there is not a write; it is a no-op
    -- that would produce an event explaining nothing.
    CONSTRAINT proposal_write_item_changes_something CHECK (
        proposed_canonical_text <> current_canonical_text
    )
);

CREATE OR REPLACE TRIGGER proposal_write_item_append_only
    BEFORE UPDATE OR DELETE ON proposal_write_item
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON proposal_write_item FROM PUBLIC;


CREATE TABLE IF NOT EXISTS proposal_effect_item (
    tenant_id        tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    intent_id        uuid         NOT NULL,
    revision         cas_version  NOT NULL,
    ordinal          int          NOT NULL,

    effect_id        semantic_key NOT NULL,
    effect_kind      semantic_key NOT NULL,
    destination_ref  semantic_key NOT NULL,
    reversibility    text         NOT NULL,

    -- internal/intent.PlannedEffect carries CompensationRef and ObservationRef,
    -- and internal/intent.CompilePlan refuses a plan whose effect has neither.
    -- Making both NOT NULL here means the refusal is structural: an effect that
    -- nothing compensates and nothing watches cannot be recorded at all.
    compensation_ref semantic_key NOT NULL,
    observation_ref  semantic_key NOT NULL,

    recorded_at      timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, intent_id, revision, ordinal),
    CONSTRAINT proposal_effect_item_id_unique
        UNIQUE (tenant_id, intent_id, revision, effect_id),
    CONSTRAINT proposal_effect_item_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT proposal_effect_item_ordinal_positive CHECK (ordinal >= 1),
    CONSTRAINT proposal_effect_item_reversibility_allowed CHECK (
        reversibility IN ('REVERSIBLE', 'COMPENSATABLE', 'IRREVERSIBLE')
    )
);

CREATE OR REPLACE TRIGGER proposal_effect_item_append_only
    BEFORE UPDATE OR DELETE ON proposal_effect_item
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON proposal_effect_item FROM PUBLIC;


CREATE TABLE IF NOT EXISTS proposal_approval_requirement (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    intent_id             uuid         NOT NULL,
    revision              cas_version  NOT NULL,
    ordinal               int          NOT NULL,

    requirement_id        semantic_key NOT NULL,
    -- The separation-of-duties constraint this requirement enforces, spelled as
    -- internal/intent.RequiredApproval names it.
    separation_constraint semantic_key NOT NULL,
    materiality_class     text         NOT NULL,

    recorded_at           timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, intent_id, revision, ordinal),
    CONSTRAINT proposal_approval_requirement_id_unique
        UNIQUE (tenant_id, intent_id, revision, requirement_id),
    CONSTRAINT proposal_approval_requirement_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT proposal_approval_requirement_ordinal_positive CHECK (ordinal >= 1),
    CONSTRAINT proposal_approval_requirement_materiality_allowed CHECK (
        materiality_class IN ('MATERIAL', 'NON_MATERIAL')
    )
);

CREATE OR REPLACE TRIGGER proposal_approval_requirement_append_only
    BEFORE UPDATE OR DELETE ON proposal_approval_requirement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON proposal_approval_requirement FROM PUBLIC;


CREATE TABLE IF NOT EXISTS proposal_obligation (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    intent_id       uuid         NOT NULL,
    revision        cas_version  NOT NULL,
    ordinal         int          NOT NULL,

    obligation_id   semantic_key NOT NULL,
    obligation_kind semantic_key NOT NULL,
    -- When the obligation must be discharged. An obligation with no deadline
    -- cannot become OVERDUE, which is one of intent_instance's five dimensions,
    -- so it would be an obligation the lifecycle cannot express.
    due_at          timestamptz  NOT NULL,

    recorded_at     timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, intent_id, revision, ordinal),
    CONSTRAINT proposal_obligation_id_unique
        UNIQUE (tenant_id, intent_id, revision, obligation_id),
    CONSTRAINT proposal_obligation_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT proposal_obligation_ordinal_positive CHECK (ordinal >= 1)
);

CREATE OR REPLACE TRIGGER proposal_obligation_append_only
    BEFORE UPDATE OR DELETE ON proposal_obligation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON proposal_obligation FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 9. Typed immutable intent relationships.
-- ---------------------------------------------------------------------------
-- Parent/child, supersession, correction and repair edges between intents. The
-- unique constraint on (tenant_id, relationship_type, child_intent_id) is the
-- structural answer to "changes parentage": an intent has at most one parent per
-- relationship type, and because the table is append-only, the one it has can
-- never be rewritten. Deeper cycles are refused in Go before insert (see the
-- header).
CREATE TABLE IF NOT EXISTS intent_relationship (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    relationship_id       uuid           NOT NULL,

    relationship_type     text           NOT NULL,
    parent_intent_id      uuid           NOT NULL,
    child_intent_id       uuid           NOT NULL,

    -- The ordinal a parent's children are ordered by, which is what makes a
    -- fan-out reproducible rather than arbitrary.
    ordinal               int            NOT NULL,
    -- The child's material input digest as the parent declared it, so a child
    -- that was created from something else is detectable.
    material_input_digest content_digest NOT NULL,

    established_at        timestamptz    NOT NULL,
    recorded_at           timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, relationship_id),
    -- One parent per (type, child): the parentage invariant.
    CONSTRAINT intent_relationship_single_parent
        UNIQUE (tenant_id, relationship_type, child_intent_id),
    CONSTRAINT intent_relationship_ordinal_unique
        UNIQUE (tenant_id, relationship_type, parent_intent_id, ordinal),
    CONSTRAINT intent_relationship_parent
        FOREIGN KEY (tenant_id, parent_intent_id)
        REFERENCES intent_instance (tenant_id, intent_id),
    CONSTRAINT intent_relationship_child
        FOREIGN KEY (tenant_id, child_intent_id)
        REFERENCES intent_instance (tenant_id, intent_id),
    CONSTRAINT intent_relationship_type_allowed CHECK (
        relationship_type IN ('CHILD_OF', 'SUPERSEDES', 'CORRECTS', 'REPAIRS')
    ),
    -- The one-hop cycle, refused in the schema.
    CONSTRAINT intent_relationship_not_self CHECK (parent_intent_id <> child_intent_id),
    CONSTRAINT intent_relationship_ordinal_positive CHECK (ordinal >= 1)
);

CREATE INDEX IF NOT EXISTS intent_relationship_parent_children
    ON intent_relationship (tenant_id, parent_intent_id, relationship_type);

CREATE OR REPLACE TRIGGER intent_relationship_append_only
    BEFORE UPDATE OR DELETE ON intent_relationship
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON intent_relationship FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 10. Typed immutable intent results.
-- ---------------------------------------------------------------------------
-- What an intent's revision actually produced. The primary key is the revision
-- itself, so a second result for a revision collides; the proposal_revision row
-- it explains is never touched, which is the RED clause's "result overwrites
-- revision" made impossible rather than merely discouraged.
CREATE TABLE IF NOT EXISTS intent_result (
    tenant_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    intent_id       uuid           NOT NULL,
    revision        cas_version    NOT NULL,

    result_kind     text           NOT NULL,
    result_digest   content_digest NOT NULL,
    proposal_digest content_digest NOT NULL,
    control_digest  content_digest NOT NULL,

    schema_ref      semantic_key   NOT NULL,
    result_body     jsonb          NOT NULL,

    produced_by     semantic_key   NOT NULL,
    produced_at     timestamptz    NOT NULL,
    recorded_at     timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, intent_id, revision),
    CONSTRAINT intent_result_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT intent_result_kind_allowed CHECK (
        result_kind IN ('SIMULATED', 'COMMITTED', 'REJECTED', 'CORRECTED', 'ABANDONED')
    ),
    CONSTRAINT intent_result_body_object CHECK (jsonb_typeof(result_body) = 'object')
);

CREATE OR REPLACE TRIGGER intent_result_append_only
    BEFORE UPDATE OR DELETE ON intent_result
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON intent_result FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 11. Decisions.
-- ---------------------------------------------------------------------------
-- A governance or human decision taken on one proposal revision. It is a
-- distinct table from migration 00022's work_item_decision: that row records
-- what a human typed into a work item, this one records the decision the kernel
-- bound to a revision, and one intent can carry several (an approval per
-- requirement, then a submission decision). The unique constraint is per
-- (revision, requirement, decider): the same person may not vote twice on the
-- same requirement, and because the table is append-only, a recorded vote is
-- never rewritten.
CREATE TABLE IF NOT EXISTS intent_decision (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    decision_id       uuid           NOT NULL,
    intent_id         uuid           NOT NULL,
    revision          cas_version    NOT NULL,

    requirement_id    semantic_key   NOT NULL,
    decision_kind     text           NOT NULL,
    decision_outcome  text           NOT NULL,

    -- What the decision was made against. A decision that does not pin the
    -- proposal and control context it saw can be neither revalidated nor
    -- invalidated, which is exactly what an approval binding must be able to do.
    proposal_digest   content_digest NOT NULL,
    control_digest    content_digest NOT NULL,
    materiality_class text           NOT NULL,

    decided_by        semantic_key   NOT NULL,
    authority_ref     semantic_key   NOT NULL,
    decision_reason   text           NOT NULL,
    decided_at        timestamptz    NOT NULL,
    recorded_at       timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, decision_id),
    CONSTRAINT intent_decision_one_vote_per_requirement
        UNIQUE (tenant_id, intent_id, revision, requirement_id, decided_by),
    CONSTRAINT intent_decision_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT intent_decision_kind_allowed CHECK (
        decision_kind IN ('HUMAN_APPROVAL', 'POLICY', 'AUTHZ', 'LEGAL', 'RISK')
    ),
    CONSTRAINT intent_decision_outcome_allowed CHECK (
        decision_outcome IN ('APPROVED', 'REJECTED', 'ABSTAINED', 'DELEGATED')
    ),
    CONSTRAINT intent_decision_materiality_allowed CHECK (
        materiality_class IN ('MATERIAL', 'NON_MATERIAL')
    )
);

CREATE INDEX IF NOT EXISTS intent_decision_revision
    ON intent_decision (tenant_id, intent_id, revision);

CREATE OR REPLACE TRIGGER intent_decision_append_only
    BEFORE UPDATE OR DELETE ON intent_decision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON intent_decision FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 12. Certificates.
-- ---------------------------------------------------------------------------
-- A signed statement that a named check passed against an exact revision: the
-- artifact an auditor reads instead of re-deriving the check. One per (revision,
-- kind) -- a second certificate of the same kind for the same revision would be
-- either a duplicate or a contradiction, and both are refused by collision
-- rather than resolved by overwrite.
CREATE TABLE IF NOT EXISTS intent_certificate (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    certificate_id     uuid           NOT NULL,
    intent_id          uuid           NOT NULL,
    revision           cas_version    NOT NULL,

    certificate_kind   text           NOT NULL,
    certificate_digest content_digest NOT NULL,
    proposal_digest    content_digest NOT NULL,
    control_digest     content_digest NOT NULL,

    -- The governed artifact carrying the certificate bytes. A certificate is
    -- content-addressed evidence, so the bytes live in migration 00010's
    -- artifact store and this row references them; it never inlines them.
    artifact_ref       text           NOT NULL,
    issued_by          semantic_key   NOT NULL,
    issued_at          timestamptz    NOT NULL,
    valid_until        timestamptz,
    recorded_at        timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, certificate_id),
    CONSTRAINT intent_certificate_kind_unique
        UNIQUE (tenant_id, intent_id, revision, certificate_kind),
    CONSTRAINT intent_certificate_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT intent_certificate_kind_allowed CHECK (
        certificate_kind IN ('PREFLIGHT', 'SIMULATION', 'GOVERNANCE', 'APPROVAL', 'COMMIT')
    ),
    CONSTRAINT intent_certificate_validity_order CHECK (
        valid_until IS NULL OR valid_until > issued_at
    )
);

CREATE OR REPLACE TRIGGER intent_certificate_append_only
    BEFORE UPDATE OR DELETE ON intent_certificate
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON intent_certificate FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 13. Transaction plan.
-- ---------------------------------------------------------------------------
-- planning/specs/transaction-plan-and-commit-coordinator.md: "the plan is never
-- mutated in place". The table is therefore append-only, and a plan whose
-- material context changed is a new row with a new plan_digest, not an edit. The
-- plan's state, which does move (APPROVAL_BOUND -> RESERVED -> READY ->
-- COMMITTING), lives on transaction_plan_binding below precisely so that the
-- immutable plan and its moving binding are two different rows.
CREATE TABLE IF NOT EXISTS transaction_plan (
    tenant_id                  tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    plan_id                    uuid           NOT NULL,
    intent_id                  uuid           NOT NULL,
    revision                   cas_version    NOT NULL,

    plan_digest                content_digest NOT NULL,
    -- The three digests the RED clause forbids losing.
    intent_digest              content_digest NOT NULL,
    proposal_digest            content_digest NOT NULL,
    control_digest             content_digest NOT NULL,

    execution_mode             text           NOT NULL,
    compiled_status            text           NOT NULL,

    governance_snapshot_digest content_digest NOT NULL,
    conflict_snapshot_digest   content_digest NOT NULL,
    conflict_fence_token       semantic_key   NOT NULL,
    idempotency_record_ref     semantic_key   NOT NULL,

    -- Business validity of what the plan would apply, half-open.
    effective_from             timestamptz    NOT NULL,
    effective_to               timestamptz,
    -- How long the compiled plan may be considered current. A plan with no
    -- expiry could be committed against arbitrarily stale context.
    expires_at                 timestamptz    NOT NULL,

    compiled_by                semantic_key   NOT NULL,
    compiled_at                timestamptz    NOT NULL,
    recorded_at                timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, plan_id),
    CONSTRAINT transaction_plan_digest_unique UNIQUE (tenant_id, plan_digest),
    CONSTRAINT transaction_plan_revision
        FOREIGN KEY (tenant_id, intent_id, revision)
        REFERENCES proposal_revision (tenant_id, intent_id, revision),
    CONSTRAINT transaction_plan_mode_allowed CHECK (
        execution_mode IN ('SIMULATE', 'EXECUTE', 'REPLAY', 'REPAIR', 'SHADOW')
    ),
    CONSTRAINT transaction_plan_status_allowed CHECK (
        compiled_status IN ('GOVERNANCE_VALIDATED', 'BLOCKED')
    ),
    CONSTRAINT transaction_plan_effective_order CHECK (
        effective_to IS NULL OR effective_to > effective_from
    ),
    CONSTRAINT transaction_plan_expires_after_compile CHECK (expires_at > compiled_at)
);

CREATE INDEX IF NOT EXISTS transaction_plan_intent
    ON transaction_plan (tenant_id, intent_id, revision);

CREATE OR REPLACE TRIGGER transaction_plan_append_only
    BEFORE UPDATE OR DELETE ON transaction_plan
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON transaction_plan FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 14. Transaction plan effects.
-- ---------------------------------------------------------------------------
-- The outbox effects a plan declares, each with the compensation and post-commit
-- observation internal/intent.CompilePlan already refuses to compile without.
-- Making both NOT NULL is the same refusal expressed in the schema, so a plan
-- row assembled by any other writer inherits it.
CREATE TABLE IF NOT EXISTS transaction_plan_effect (
    tenant_id              tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    plan_id                uuid         NOT NULL,
    effect_id              semantic_key NOT NULL,

    ordinal                int          NOT NULL,
    destination_ref        semantic_key NOT NULL,
    effect_idempotency_key semantic_key NOT NULL,
    reversibility          text         NOT NULL,

    compensation_strategy  semantic_key NOT NULL,
    repair_plan_ref        semantic_key NOT NULL,
    observation_ref        semantic_key NOT NULL,
    observation_deadline   timestamptz  NOT NULL,

    recorded_at            timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, plan_id, effect_id),
    CONSTRAINT transaction_plan_effect_ordinal_unique UNIQUE (tenant_id, plan_id, ordinal),
    CONSTRAINT transaction_plan_effect_plan
        FOREIGN KEY (tenant_id, plan_id) REFERENCES transaction_plan (tenant_id, plan_id),
    CONSTRAINT transaction_plan_effect_ordinal_positive CHECK (ordinal >= 1),
    CONSTRAINT transaction_plan_effect_reversibility_allowed CHECK (
        reversibility IN ('REVERSIBLE', 'COMPENSATABLE', 'IRREVERSIBLE')
    )
);

CREATE OR REPLACE TRIGGER transaction_plan_effect_append_only
    BEFORE UPDATE OR DELETE ON transaction_plan_effect
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON transaction_plan_effect FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 15. Transaction plan binding (live plan state).
-- ---------------------------------------------------------------------------
-- The one moving part of a plan: where it currently sits on the coordinator's
-- state machine, fenced by a compare-and-swap binding_version. Splitting it out
-- is what lets transaction_plan itself stay append-only while the spec's
-- DRAFT -> ... -> COMMITTED progression still has somewhere to live.
CREATE TABLE IF NOT EXISTS transaction_plan_binding (
    tenant_id               tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    plan_id                 uuid           NOT NULL,

    plan_state              text           NOT NULL,
    binding_version         cas_version    NOT NULL DEFAULT 1,

    -- Set once the plan's approvals are bound; null before that. A plan in
    -- APPROVAL_BOUND or beyond must have it (the CHECK below), which is how
    -- "approval binds the exact canonical proposal and plan material context"
    -- becomes unskippable.
    approval_binding_digest content_digest,

    last_transition_at      timestamptz    NOT NULL,
    recorded_at             timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, plan_id),
    CONSTRAINT transaction_plan_binding_plan
        FOREIGN KEY (tenant_id, plan_id) REFERENCES transaction_plan (tenant_id, plan_id),
    CONSTRAINT transaction_plan_binding_state_allowed CHECK (
        plan_state IN ('DRAFT', 'DOMAIN_VALIDATED', 'GOVERNANCE_VALIDATED', 'APPROVAL_BOUND',
                       'RESERVED', 'READY', 'COMMITTING', 'COMMITTED',
                       'STALE', 'ABORTED', 'AMBIGUOUS', 'REPAIR_REQUIRED')
    ),
    CONSTRAINT transaction_plan_binding_approval_present CHECK (
        plan_state NOT IN ('APPROVAL_BOUND', 'RESERVED', 'READY', 'COMMITTING', 'COMMITTED')
        OR approval_binding_digest IS NOT NULL
    )
);


-- ---------------------------------------------------------------------------
-- 16. Commit receipt.
-- ---------------------------------------------------------------------------
-- The evidence that exactly one local database transaction committed this plan.
-- UNIQUE (tenant_id, plan_id) is the "exactly one" -- a second commit of the
-- same plan collides -- and the three digests are the RED clause's sixth defect
-- made structurally impossible.
CREATE TABLE IF NOT EXISTS transaction_commit_receipt (
    tenant_id                 tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    receipt_id                uuid           NOT NULL,
    plan_id                   uuid           NOT NULL,

    intent_digest             content_digest NOT NULL,
    proposal_digest           content_digest NOT NULL,
    control_digest            content_digest NOT NULL,
    plan_digest               content_digest NOT NULL,

    -- The database transaction identity the commit ran under, so an ambiguous
    -- acknowledgement can be resolved by asking the database rather than by
    -- retrying blind (the spec's "queries the transaction/idempotency receipt
    -- before any retry").
    database_transaction_id   semantic_key   NOT NULL,
    idempotency_record_ref    semantic_key   NOT NULL,

    -- What the commit produced, as counts plus one digest over the canonical id
    -- lists. The lists themselves are the ledger's and the outbox's own rows;
    -- duplicating them here would create a second source of truth.
    appended_event_count      int            NOT NULL,
    projection_mutation_count int            NOT NULL,
    outbox_effect_count       int            NOT NULL,
    produced_reference_digest content_digest NOT NULL,

    committed_at              timestamptz    NOT NULL,
    recorded_at               timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, receipt_id),
    CONSTRAINT transaction_commit_receipt_plan_unique UNIQUE (tenant_id, plan_id),
    CONSTRAINT transaction_commit_receipt_plan
        FOREIGN KEY (tenant_id, plan_id) REFERENCES transaction_plan (tenant_id, plan_id),
    CONSTRAINT transaction_commit_receipt_counts_nonnegative CHECK (
        appended_event_count >= 0
        AND projection_mutation_count >= 0
        AND outbox_effect_count >= 0
    )
);

CREATE OR REPLACE TRIGGER transaction_commit_receipt_append_only
    BEFORE UPDATE OR DELETE ON transaction_commit_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON transaction_commit_receipt FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 17. Abort receipt.
-- ---------------------------------------------------------------------------
-- The same shape for the other outcome. A plan may hold at most one abort
-- receipt; that it may not hold both an abort and a commit receipt is a
-- two-table invariant no single constraint can express, so
-- internal/data/intentcontrol's ReceiptStore checks for the counterpart inside
-- the caller's transaction and refuses -- and its test proves the refusal.
CREATE TABLE IF NOT EXISTS transaction_abort_receipt (
    tenant_id              tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    receipt_id             uuid           NOT NULL,
    plan_id                uuid           NOT NULL,

    intent_digest          content_digest NOT NULL,
    proposal_digest        content_digest NOT NULL,
    control_digest         content_digest NOT NULL,
    plan_digest            content_digest NOT NULL,

    abort_reason_code      text           NOT NULL,
    abort_detail           text           NOT NULL,
    -- True when the abort happened before any durable effect existed, which is
    -- the difference between "nothing happened" and "something must be
    -- compensated".
    aborted_before_effects boolean        NOT NULL,

    aborted_at             timestamptz    NOT NULL,
    recorded_at            timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, receipt_id),
    CONSTRAINT transaction_abort_receipt_plan_unique UNIQUE (tenant_id, plan_id),
    CONSTRAINT transaction_abort_receipt_plan
        FOREIGN KEY (tenant_id, plan_id) REFERENCES transaction_plan (tenant_id, plan_id),
    CONSTRAINT transaction_abort_receipt_reason_allowed CHECK (
        abort_reason_code IN ('PLAN_EXPIRED', 'PLAN_STALE', 'DIGEST_MISMATCH',
                              'APPROVAL_MISMATCH', 'DOMAIN_REJECTED', 'GOVERNANCE_REJECTED',
                              'SEQUENCE_CONFLICT', 'RESERVATION_LOST', 'AUTHORITY_CHANGED',
                              'IDEMPOTENCY_CONFLICT', 'DATABASE_ABORT', 'CANCELLED')
    )
);

CREATE OR REPLACE TRIGGER transaction_abort_receipt_append_only
    BEFORE UPDATE OR DELETE ON transaction_abort_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON transaction_abort_receipt FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 18. Ambiguity record (live state).
-- ---------------------------------------------------------------------------
-- An ambiguous database outcome: the commit may or may not have happened. The
-- row is opened UNRESOLVED and later resolved once the coordinator has queried
-- the receipt, so unlike the receipts this table is live state, UPDATE-granted
-- and fenced by a compare-and-swap ambiguity_version. The CHECK makes the
-- resolution self-consistent: a resolved ambiguity names when, how and on what
-- evidence it was resolved; an unresolved one names none of the three.
CREATE TABLE IF NOT EXISTS transaction_ambiguity (
    tenant_id               tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    ambiguity_id            uuid         NOT NULL,
    plan_id                 uuid         NOT NULL,

    detected_at             timestamptz  NOT NULL,
    detection_detail        text         NOT NULL,

    resolution_state        text         NOT NULL,
    resolved_outcome        text,
    resolved_at             timestamptz,
    resolution_evidence_ref semantic_key,

    ambiguity_version       cas_version  NOT NULL DEFAULT 1,
    recorded_at             timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, ambiguity_id),
    CONSTRAINT transaction_ambiguity_plan_unique UNIQUE (tenant_id, plan_id),
    CONSTRAINT transaction_ambiguity_plan
        FOREIGN KEY (tenant_id, plan_id) REFERENCES transaction_plan (tenant_id, plan_id),
    CONSTRAINT transaction_ambiguity_state_allowed CHECK (
        resolution_state IN ('UNRESOLVED', 'RESOLVED')
    ),
    CONSTRAINT transaction_ambiguity_outcome_allowed CHECK (
        resolved_outcome IS NULL
        OR resolved_outcome IN ('COMMITTED', 'ABORTED', 'REPAIR_REQUIRED')
    ),
    CONSTRAINT transaction_ambiguity_resolution_complete CHECK (
        (resolution_state = 'RESOLVED')
        = (resolved_outcome IS NOT NULL AND resolved_at IS NOT NULL
           AND resolution_evidence_ref IS NOT NULL)
    ),
    CONSTRAINT transaction_ambiguity_resolution_order CHECK (
        resolved_at IS NULL OR resolved_at >= detected_at
    )
);


-- ---------------------------------------------------------------------------
-- 19. Correction.
-- ---------------------------------------------------------------------------
-- A correction of an already-committed transaction. corrects_receipt_id is NOT
-- NULL and foreign-keyed: "correction lacks target" is not a validation rule
-- here, it is an impossible row.
CREATE TABLE IF NOT EXISTS transaction_correction (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    correction_id        uuid           NOT NULL,
    corrects_receipt_id  uuid           NOT NULL,

    -- The intent that carries the correction. It is a different intent from the
    -- one being corrected -- a correction is itself a governed request -- and
    -- the CORRECTS edge between them lives in intent_relationship.
    correcting_intent_id uuid           NOT NULL,

    intent_digest        content_digest NOT NULL,
    proposal_digest      content_digest NOT NULL,
    control_digest       content_digest NOT NULL,

    correction_reason    text           NOT NULL,
    corrected_by         semantic_key   NOT NULL,
    corrected_at         timestamptz    NOT NULL,
    recorded_at          timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, correction_id),
    CONSTRAINT transaction_correction_target
        FOREIGN KEY (tenant_id, corrects_receipt_id)
        REFERENCES transaction_commit_receipt (tenant_id, receipt_id),
    CONSTRAINT transaction_correction_intent
        FOREIGN KEY (tenant_id, correcting_intent_id)
        REFERENCES intent_instance (tenant_id, intent_id)
);

CREATE INDEX IF NOT EXISTS transaction_correction_receipt
    ON transaction_correction (tenant_id, corrects_receipt_id);

CREATE OR REPLACE TRIGGER transaction_correction_append_only
    BEFORE UPDATE OR DELETE ON transaction_correction
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON transaction_correction FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- 20. RepairPlan (live state).
-- ---------------------------------------------------------------------------
-- The plan for repairing a partially-applied or ambiguous transaction. Its
-- status advances as repair proceeds, so it is UPDATE-granted and fenced by a
-- compare-and-swap repair_version; it still carries all three digests, because a
-- repair that cannot say what it is repairing is the RED clause's sixth defect
-- wearing a different hat.
CREATE TABLE IF NOT EXISTS repair_plan (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    repair_plan_id     uuid           NOT NULL,
    plan_id            uuid           NOT NULL,

    intent_digest      content_digest NOT NULL,
    proposal_digest    content_digest NOT NULL,
    control_digest     content_digest NOT NULL,

    repair_status      text           NOT NULL,
    repair_strategy    semantic_key   NOT NULL,
    repair_version     cas_version    NOT NULL DEFAULT 1,

    -- What the repair must put right, as a schema-bound document rather than
    -- free JSON.
    schema_ref         semantic_key   NOT NULL,
    repair_body        jsonb          NOT NULL,

    opened_by          semantic_key   NOT NULL,
    opened_at          timestamptz    NOT NULL,
    last_transition_at timestamptz    NOT NULL,
    recorded_at        timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, repair_plan_id),
    CONSTRAINT repair_plan_plan_unique UNIQUE (tenant_id, plan_id),
    CONSTRAINT repair_plan_plan
        FOREIGN KEY (tenant_id, plan_id) REFERENCES transaction_plan (tenant_id, plan_id),
    CONSTRAINT repair_plan_status_allowed CHECK (
        repair_status IN ('OPEN', 'IN_PROGRESS', 'REPAIRED', 'ABANDONED')
    ),
    CONSTRAINT repair_plan_body_object CHECK (jsonb_typeof(repair_body) = 'object'),
    CONSTRAINT repair_plan_transition_after_open CHECK (last_transition_at >= opened_at)
);


-- ---------------------------------------------------------------------------
-- 21. Closure and execution-receipt reference.
-- ---------------------------------------------------------------------------
-- The terminal record: this intent is closed, and here is the receipt that
-- explains how. One row per intent -- an intent closes once -- and the receipt
-- reference is NOT NULL for exactly the closure kinds that claim execution
-- happened (the CHECK), so a COMMITTED closure naming no receipt cannot be
-- written and a WITHDRAWN one cannot invent a receipt it never had.
CREATE TABLE IF NOT EXISTS intent_closure (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    intent_id            uuid           NOT NULL,

    closure_kind         text           NOT NULL,
    closure_reason       text           NOT NULL,

    intent_digest        content_digest NOT NULL,
    proposal_digest      content_digest NOT NULL,
    control_digest       content_digest NOT NULL,

    -- The execution receipt this closure points at. Null exactly for the
    -- closure kinds that never executed anything.
    execution_receipt_id uuid,

    closed_by            semantic_key   NOT NULL,
    closed_at            timestamptz    NOT NULL,
    recorded_at          timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, intent_id),
    CONSTRAINT intent_closure_intent
        FOREIGN KEY (tenant_id, intent_id) REFERENCES intent_instance (tenant_id, intent_id),
    CONSTRAINT intent_closure_receipt
        FOREIGN KEY (tenant_id, execution_receipt_id)
        REFERENCES transaction_commit_receipt (tenant_id, receipt_id),
    CONSTRAINT intent_closure_kind_allowed CHECK (
        closure_kind IN ('COMMITTED', 'CORRECTED', 'REJECTED', 'WITHDRAWN',
                         'CANCELLED', 'SUPERSEDED', 'EXPIRED')
    ),
    CONSTRAINT intent_closure_receipt_present CHECK (
        (closure_kind IN ('COMMITTED', 'CORRECTED')) = (execution_receipt_id IS NOT NULL)
    )
);

CREATE OR REPLACE TRIGGER intent_closure_append_only
    BEFORE UPDATE OR DELETE ON intent_closure
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON intent_closure FROM PUBLIC;


-- ---------------------------------------------------------------------------
-- Tenant isolation (DB-017) for every table above.
-- ---------------------------------------------------------------------------
-- Fail closed on a missing or blank app.tenant_id session setting, keyed on
-- internal/data/tenancy.WithTenant. Written as a loop rather than twenty-one
-- copy-pasted blocks: the array below is this migration's table list, and a
-- table added here but misspelled in the loop fails the migration instead of
-- shipping an unprotected table.
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'intent_instance_context',
        'hcm_change_request',
        'intent_input_snapshot',
        'intent_simulation_result',
        'proposal_write_item',
        'proposal_effect_item',
        'proposal_approval_requirement',
        'proposal_obligation',
        'intent_relationship',
        'intent_result',
        'intent_decision',
        'intent_certificate',
        'transaction_plan',
        'transaction_plan_effect',
        'transaction_plan_binding',
        'transaction_commit_receipt',
        'transaction_abort_receipt',
        'transaction_ambiguity',
        'transaction_correction',
        'repair_plan',
        'intent_closure'
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

-- Append-only evidence: SELECT and INSERT only, exactly like proposal_revision,
-- work_item_transition and work_item_decision. UPDATE and DELETE are already
-- revoked from PUBLIC above and refused again at the row level by the
-- forbid_mutation trigger, so the withheld grant is the first of three
-- independent refusals rather than the only one.
GRANT SELECT, INSERT ON
    intent_instance_context,
    intent_input_snapshot,
    intent_simulation_result,
    proposal_write_item,
    proposal_effect_item,
    proposal_approval_requirement,
    proposal_obligation,
    intent_relationship,
    intent_result,
    intent_decision,
    intent_certificate,
    transaction_plan,
    transaction_plan_effect,
    transaction_commit_receipt,
    transaction_abort_receipt,
    transaction_correction,
    intent_closure
TO hcmnext_app;

-- Live serving state: SELECT/INSERT/UPDATE, never DELETE (this data plane has no
-- delete semantics, only append and state transition).
GRANT SELECT, INSERT, UPDATE ON
    hcm_change_request,
    transaction_plan_binding,
    transaction_ambiguity,
    repair_plan
TO hcmnext_app;

-- +goose Down
REVOKE ALL ON
    intent_closure,
    repair_plan,
    transaction_correction,
    transaction_ambiguity,
    transaction_abort_receipt,
    transaction_commit_receipt,
    transaction_plan_binding,
    transaction_plan_effect,
    transaction_plan,
    intent_certificate,
    intent_decision,
    intent_result,
    intent_relationship,
    proposal_obligation,
    proposal_approval_requirement,
    proposal_effect_item,
    proposal_write_item,
    intent_simulation_result,
    intent_input_snapshot,
    hcm_change_request,
    intent_instance_context
FROM hcmnext_app;

DROP TABLE intent_closure;
DROP TABLE repair_plan;
DROP TABLE transaction_correction;
DROP TABLE transaction_ambiguity;
DROP TABLE transaction_abort_receipt;
DROP TABLE transaction_commit_receipt;
DROP TABLE transaction_plan_binding;
DROP TABLE transaction_plan_effect;
DROP TABLE transaction_plan;
DROP TABLE intent_certificate;
DROP TABLE intent_decision;
DROP TABLE intent_result;
DROP TABLE intent_relationship;
DROP TABLE proposal_obligation;
DROP TABLE proposal_approval_requirement;
DROP TABLE proposal_effect_item;
DROP TABLE proposal_write_item;
DROP TABLE intent_simulation_result;
DROP TABLE intent_input_snapshot;
DROP TABLE hcm_change_request;
DROP TABLE intent_instance_context;
