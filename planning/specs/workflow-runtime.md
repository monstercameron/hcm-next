# Workflow Execution Kernel

This specification defines the domain-neutral execution kernel used by HCM Next workflows. The master delivery scope remains governed by [the Phase 1 execution plan](../execution-plan.md).

The engine does not understand payroll, GDPR, IAM, benefits, or compensation internally. Those systems expose typed, governed capabilities. The engine compiles and durably coordinates their invocation.

```text
INPUTS
  |
  +-- HCM facts
  +-- Legal and AuthZ obligations
  +-- Customer policy
  +-- Human and agent decisions
  +-- External observations
  +-- Time and events
  |
  v
WORKFLOW EXECUTION KERNEL
  |
  +-- typed graph
  +-- compiler
  +-- durable instances and nodes
  +-- tasks, timers, signals, leases
  +-- safe points and intervention
  |
  v
OUTPUTS
  +-- BusinessTransaction / Decision
  +-- Approval / HumanTask / Document
  +-- ExternalEffect / Observation
  +-- RepairPlan / Child Workflow
```

The governing boundary is:

> The workflow kernel coordinates typed behavior. Capability services own domain semantics and deterministic effects.

The runtime's governing philosophy remains:

> Nothing is unfixable, but nothing is silently fixable.

## Kernel Vocabulary

The runtime begins with a deliberately small set of block classes:

| Primitive     | Runtime meaning                                               |
| ------------- | ------------------------------------------------------------- |
| `CAPABILITY`  | Invoke a governed typed capability                            |
| `DECISION`    | Choose an edge from typed deterministic input                 |
| `APPROVAL`    | Create and resolve a proposal-bound approval requirement      |
| `TASK`        | Request governed human input or work                          |
| `WAIT`        | Suspend durably until a time/calendar condition               |
| `SIGNAL`      | Suspend durably until a correlated event                      |
| `PARALLEL`    | Schedule bounded independent branches                         |
| `JOIN`        | Apply declared completion semantics to branches               |
| `SUBWORKFLOW` | Start a pinned child workflow and bind its outcome            |
| `TRANSFORM`   | Apply versioned deterministic data transformation             |
| `RULE`        | Evaluate a typed business or policy rule                      |
| `AGENT`       | Invoke a separately governed agent capability                 |
| `DOCUMENT`    | Generate, deliver, sign, or collect a typed document artifact |
| `OBSERVE`     | Read or reconcile external/derived state                      |
| `CHECKPOINT`  | Establish an operational safe point                           |
| `COMPENSATE`  | Invoke an explicitly declared corrective capability           |
| `END`         | Produce a typed runtime and business-completion result        |

The exploratory [step-type catalog](../workflows/_engine/step-types.md) expands
the common envelope, compiler rules, runtime semantics, evidence and failure
behavior for every primitive. Its [sample coverage matrix](../workflows/_engine/step-type-coverage.md)
tracks at least one substantive workflow example per type. Those documents guide
schema/runtime discovery; this specification remains the normative boundary.

The primitive name never grants authority. `CAPABILITY payroll.write`, for example, is still subject to current entitlement, AuthZ, Legal, purpose, DLP, risk, idempotency, and connector controls.

Advanced loops, unbounded fan-out, deeply nested subworkflows, arbitrary customer code, and customer-authored compensation logic remain gated until the operating model is proven.

## Declarative Definition and Type System

```text
WorkflowDefinition

workflow_id
version
name

input_schema_ref
output_schema_ref
variables_schema_ref

nodes[]
edges[]

tenant_scope
organization_scope
risk_class

failure_policy
cancellation_policy
migration_policy
retention_policy
```

```text
NodeDefinition

node_id
node_type
input_mapping
output_schema_ref

capability_ref?
resolver_ref?
rule_ref?

timeout_policy?
retry_policy?
failure_route?
compensation_ref?

side_effect_profile
safe_point_behavior
required_context[]
metadata
```

Published graphs are immutable. Changes produce a new version. Schemas, capabilities, rules, mappings, document templates, and deterministic block implementations are referenced by immutable identity/version rather than resolved silently at execution time.

Node dataflow is typed:

```text
nodes.compensation_simulation.output.band_position
                         |
                         v
              decision.raise_outside_band.input
```

The runtime does not expose a global mutable `map[string]any`. Node outputs are immutable typed artifacts. Shared values that genuinely evolve use explicit `WorkflowVariableRevision` records with writer, reason, schema, and causation.

## Compiler and Publication Pipeline

```text
Draft Definition
      |
      v
schema + graph + capability resolution
      |
      v
AuthZ/legal obligation compatibility
      |
      v
side-effect + idempotency + compensation analysis
      |
      v
safe-point + cancellation + reachability analysis
      |
      v
simulation fixtures + generated conformance tests
      |
      v
human publication approval
      |
      v
CompiledWorkflowPlan (immutable)
```

Compilation rejects at least:

- Missing, incompatible, or retired capability/schema references
- Type-invalid edges or input mappings
- Unreachable nodes and invalid terminal paths
- Undeclared or unbounded cycles and fan-out
- Retried mutations without compatible idempotency
- Irreversible effects without explicit failure/cancellation behavior
- Parallel branches with conflicting declared write sets
- Unsafe checkpoint placement around atomic regions
- Approval requirements that cannot resolve within declared scope
- Missing required legal/AuthZ obligation insertion points
- Simulation claims unsupported by a capability's side-effect profile
- Agent outputs flowing into effects without typed output validation

The compiled plan contains normalized nodes and edges, resolved immutable references, required contexts, read/write/effect sets, approval/task requirements, timer/signal indexes, compensation paths, safe points, execution limits, and a compiler fingerprint.

## Durable Runtime State

```text
WorkflowInstance

instance_id
tenant_id
cell_id

workflow_id
workflow_version
compiled_plan_hash

business_subject_refs[]
business_transaction_id?

execution_mode
runtime_status
completion_dimensions

input_ref
variable_revision_head
current_node_ids[]

effective_context_ref
last_checkpoint_ref
instance_version

correlation_id
created_at
started_at
completed_at?
```

Instance runtime states are:

```text
CREATED -> RUNNING -> WAITING -> RUNNING -> COMPLETED
              |          |
              |          +-> PAUSE_REQUESTED -> PAUSED
              +-> BLOCKED / REPAIR_REQUIRED / QUARANTINED
              +-> CANCELLING -> CANCELLED / REPAIR_REQUIRED
              +-> SUPERSEDED
```

Runtime completion does not collapse business state:

```text
RuntimeState          COMPLETED
BusinessState         COMPLETED
ExternalConsistency  DEGRADED
ReconciliationState  REPAIR_REQUIRED
ObligationState       SATISFIED
```

### Authoritative core versus downstream effects

Workflow compilation classifies every planned action into one of three sets:

```text
AUTHORITATIVE_CORE
  domain writes that share one declared transaction authority and commit boundary

DOWNSTREAM_EFFECT
  payroll, IAM, messaging, learning, document, connector, or other independently
  committed work observed and reconciled after the core commit

DERIVED_UPDATE
  rebuildable projection, search, analytics, semantic, or cache work
```

A single-domain workflow may still use approvals, timers, governance, conflicts,
ledger, observation, and reconciliation; `single-domain` only describes ownership
of the authoritative mutation. A cross-domain workflow may have one parent intent
and multiple child intents, but it may place writes in one ACID core only when the
Transaction Coordinator proves they share authority, storage, lock ordering, and
rollback semantics. External providers never participate in a fictitious global
ACID transaction.

```text
parent intent
     |
     v
atomic authoritative core --COMMIT--> outbox
                                      |
                    +-----------------+-----------------+
                    v                 v                 v
                 payroll            access          messaging
                    |                 |                 |
                    +-----------------+-----------------+
                                      v
                              observe/reconcile/repair
```

Core commit success may set `BusinessState=COMPLETED` while downstream failure
sets `ExternalConsistency=DEGRADED` and `ReconciliationState=REPAIR_REQUIRED`.
The workflow must not rewrite core history, report the business action as wholly
failed, or hide the open effect. Closure policy separately decides whether an
instance may close with an acknowledged repair or incident still open.

## Durable Node Execution

```text
NodeExecution

node_execution_id
workflow_instance_id
node_id
attempt

status
input_snapshot_ref
output_artifact_ref?

capability_execution_id?
authorization_decision_id?
decision_id?
human_task_id?
agent_execution_id?

error_class?
retry_at?
execution_lease_id?

started_at?
completed_at?
trace_id?
```

Node states are:

```text
READY -> RUNNING -> SUCCEEDED
          |
          +-> WAITING
          +-> FAILED -> RETRYING -> READY
          +-> SKIPPED / OVERRIDDEN / COMPENSATED / CANCELLED
```

Transitions, output references, timer/signal creation, and the runtime outbox commit atomically with an instance-version check. Runtime workers never own an instance merely because a process has it in memory.

## Capability Invocation Contract

```text
NodeExecution
      |
      v
Capability Gateway
      |
      +-- resolve exact capability version
      +-- fetch only declared data domains/fields
      +-- entitlement + AuthZ + legal + purpose + risk
      +-- validate typed input and side-effect profile
      +-- derive stable idempotency key
      +-- invoke deterministic service/connector/agent
      +-- validate typed output
      +-- record evidence references
      v
Node Output Artifact
```

Each invocation binds:

- Tenant, organization, principal/delegation, purpose, and channel
- Workflow, instance, node, attempt, transaction, and correlation identity
- Capability and request/response schema versions
- Execution mode and side-effect permission
- Proposal, policy, legal, entitlement, mapping, and baseline versions where applicable
- Idempotency key, deadline, retry budget, and declared effect set
- Redacted evidence and telemetry references

The workflow stores resource references and required immutable snapshots, not an independent mutable copy of the worker. Capabilities receive only their declared context subset.

## Execution Context and Side Effects

```text
WorkflowExecutionContext

PrincipalContext
TenantContext
OrganizationContext

LocaleContext
LegalContext
EntitlementContext
RiskContext
BillingContext

ExecutionMode
workflow_version
runtime_version
```

Side effects are declared:

```text
PURE
READ_ONLY
INTERNAL_MUTATION
EXTERNAL_MUTATION
IRREVERSIBLE_EXTERNAL_MUTATION
```

The compiler and runtime use this declaration to determine which nodes may run in simulation/replay/shadow, require revalidation or approval, need idempotency and observation, constrain pause/cancel/migration, and may require compensation or repair.

## Approval and Human Work Kernel

Queue, assignment, claim, delegation, SLA, form-submission, and deterministic customer-rule semantics are defined by [Human Work, Forms, and Business Rules](human-work-forms-and-rules.md). The workflow kernel coordinates their typed nodes and signals; it does not duplicate those services.

`HumanTask` generalizes approvals, missing-data requests, evidence collection, review, acknowledgement, discrepancy resolution, identity matching, and repair work.

```text
HumanTask

task_id
task_type
workflow_instance_id
node_execution_id

assignee_expression
resolved_candidates[]

input_artifact_refs[]
required_output_schema_ref

deadline
escalation_policy
delegation_policy
visibility_policy

status
decision_or_output_ref?
```

An `ApprovalRequirement` adds proposal and decision semantics:

```text
ApprovalRequirement

requirement_id
decision_type
resolver

cardinality: ONE | ANY | ALL | QUORUM
required_count?

proposal_digest   // full CanonicalDigest envelope, not a naked serializer hash
scope
deadline

resolution_time_policy
validity_policy
invalidators[]

escalation
fallback
delegation

separation_of_duties[]
step_up_required
reason_required
```

`proposal_digest` is produced and verified only through the [Canonical Envelope,
Serialization, and Digest Contract](canonical-envelope-and-digest.md); workflow
code and browser clients never choose a serializer or digest preimage.

Resolvers may target a named principal, role, scoped role, relationship, group, capability-based business relationship, or composition:

```text
ALL_OF
  ManagerOf(worker)
  HRBPFor(worker.organization)
  FinancePartnerFor(worker.cost_center)

QUORUM 2 OF
  RegionalHRDirectors(worker.region)
```

Candidate delivery and current approval authority are distinct:

```text
task creation       resolve recipients
       |
relationship changes / delegation expires
       |
decision submitted  re-evaluate current authority
       |
proposal changed?   invalidate approval
       |
record decision     bind exact proposal + context
```

Resolution policy states whether relationships are evaluated at creation, decision, execution, or multiple points. Fallback, unavailability, expiry, delegation, escalation, quorum, and separation of duties are declarative rather than runtime special cases.

## Durable Timers and Signals

Human-delivery acknowledgement, reply, and failure signals are produced by the [Messaging and Notification Plane](messaging-and-notification-plane.md). The workflow kernel stores only typed signal identity/output and fetches protected message content through separately authorized capabilities.

```text
WorkflowTimer

timer_id
workflow_instance_id
node_execution_id
wake_at

timezone?
calendar_ref?
calendar_version?
tzdb_version?
reference_update_policy
original_reference_fingerprint
resolved_wake_instant
local_time_disambiguation?
calculation_trace_ref?
invalidators[]
review_obligation_ref?

reason
status
dedupe_key
```

`reference_update_policy` is mandatory and is one of `PIN_ORIGINAL`,
`RECALCULATE_CURRENT`, or `REQUIRE_REVIEW`. Business-time calculations preserve
the timezone/calendar/tzdb versions, resolved instant, disambiguation and trace.
A reference-dataset publication computes affected timers and emits timer-impact,
recalculation or review-obligation evidence. Runtime workers never sleep for
business duration.

```text
SignalSubscription

subscription_id
workflow_instance_id
node_execution_id
event_type
correlation_filter
timeout_at?
dedupe_policy
status
```

```text
external event/webhook
      |
validate + deduplicate + preserve receipt
      |
match active subscriptions
      |
record signal + transition + outbox atomically
      |
schedule continuation
```

Late, duplicate, unmatched, unauthorized, and schema-incompatible signals remain inspectable and follow explicit policy; they never mutate runtime state opportunistically.

## Execution Modes

Every execution declares one mode:

| Mode       | Contract                                                                                |
| ---------- | --------------------------------------------------------------------------------------- |
| `SIMULATE` | Evaluate reads, pure logic, plans, obligations, cost, and mocked effects; no mutation   |
| `EXECUTE`  | Perform normal governed effects                                                         |
| `REPLAY`   | Reconstruct historical deterministic behavior from pinned inputs and implementations    |
| `REPAIR`   | Execute approved corrective behavior linked to a RepairPlan                             |
| `SHADOW`   | Run candidate logic against copied references/events while suppressing business effects |

Simulation does not claim success for an opaque external system. Replay uses historical versions or reports precisely which artifact is unavailable. Shadow execution is isolated from production writes, tasks, notifications, billing, and external delivery unless a separately approved synthetic destination is configured.

## Pause, Cancellation, and Propagation

Pause exists at three independent scopes:

```text
INSTANCE
  pause workflow_instance_123

VERSION
  quarantine promotion:v17
  block new starts; stop live instances at safe points

WORKLOAD
  pause tenant ACME + capability payroll.write
  preserve unrelated reads and critical IAM work
```

A pause request during an atomic region produces `PAUSE_REQUESTED`, not a false claim that execution is cleanly paused. The instance becomes `PAUSED` only when it reaches the next declared safe point or an operator approves a high-risk intervention.

Cancellation is a governed business transition:

```text
CancellationRequested
      |
      v
evaluate current phase + cancellation policy
      |
      +-- cleanly cancellable ------> CANCELLED
      +-- effects need reversal ----> compensation + observe
      +-- cannot cancel ------------> supersede / domain workflow
      +-- ambiguous partial effect -> REPAIR_REQUIRED
```

Killing a Go worker is process failure, not business cancellation. Child cancellation propagates by declared policy, and each child reports `CANCELLED`, `COMPENSATED`, `CANNOT_CANCEL`, or `ALREADY_COMPLETED`. The parent reconciles those outcomes before declaring cancellation complete.

## Recovery Layers

Recovery semantics remain separate:

| Layer          | Example                                 | Mechanism                                      |
| -------------- | --------------------------------------- | ---------------------------------------------- |
| Process        | Executor crashes                        | Lease expiry, fencing, safe redelivery         |
| Node           | Dependency returns a retryable error    | Bounded retry policy and shared retry budget   |
| Workflow       | Business/external state is inconsistent | RepairPlan, compensation, or roll-forward      |
| Version        | Published graph or block is unsafe      | Quarantine, migration, repair, controlled exit |
| Infrastructure | Database, cell, or region is lost       | Restore durable state, resume scheduler        |

```text
RetryPolicy

max_attempts
backoff
jitter
retryable_error_classes[]
non_retryable_error_classes[]
deadline
retry_budget
exhaustion_route
```

Retry exhaustion creates or updates owned `QuarantinedWork` before routing to a
human task, compensation, RepairPlan, incident, cancellation, supersession, or
governed discard. The record retains the original semantic idempotency identity,
classification, ambiguity, attempts, owner, SLA and next action. Moving work to a
dead-letter store is not terminal success and does not remove it from SLO totals.
See [the adversarial gap-closure contract](adversarial-gap-closure-2026-08-13.md#6-poison-work-and-dead-letter-ownership).

## Execution Leases and Fencing

```text
ExecutionLease

lease_id
node_execution_id
worker_identity
fencing_token
leased_at
lease_until
last_heartbeat_at
```

An executor must hold the current fencing token when committing a transition. Lease loss prevents late workers from completing stale attempts. A stable node/capability idempotency key protects business effects across redelivery, but idempotency never substitutes for observing an ambiguous external outcome.

## Storage and Evidence Boundaries

The initial physical implementation may be PostgreSQL, but four logical concerns remain separate:

```text
DEFINITIONS
  workflow definitions / versions / compiled plans

RUNTIME
  instances / node executions / timers / signals / tasks / leases

BUSINESS EVIDENCE
  ledger events / approvals / decisions / transactions / repairs

OPERATIONAL INDEXES
  ready queue / workflow health / stuck instances / task queue / repair queue
```

Runtime state answers _where execution is now_. The ledger answers _how the business reached this state_. Telemetry explains software behavior. They correlate through workflow, node execution, transaction, correlation, and trace identifiers but are not interchangeable stores.

```text
Business ledger                     Telemetry
---------------                     ---------
FinanceApprovalGranted              lease acquisition 18ms
PayrollSyncRequested                gRPC deadline exceeded
PayrollSyncFailed                   stack / retry metric
RepairPlanCreated
```

Product screens use purpose-built runtime/task/health projections rather than reconstructing current operations by scanning the ledger.

## Execution Inspector and Deterministic Replay

The runtime exposes a governed execution inspector:

```text
Promotion / wf_88191 / v17

RUNNING -> PayrollSync / attempt 3

[ok] preflight          [ok] finance approval
[ok] effective wait     [ok] worker transaction
[!!] payroll sync       [--] IAM
[--] reconciliation

failure       ADP_TIMEOUT
next retry    20:14
proposal      sha256:8871...
baseline      still valid
repair        not required yet
```

Node detail links typed input/output artifacts, capability and policy decisions, AuthZ/legal evidence, mapping and connector versions, request/response hashes, attempts, timers/signals, and telemetry traces. Sensitive raw content remains separately authorized and redacted.

`workflow.debug.replay` reruns only pure/deterministic nodes against historical snapshots and pinned implementations. It compares historical output with the selected implementation without performing tasks, notifications, mutations, or external effects.

```text
Historical execution       Candidate replay
comp-calc:v8 -> 145000      comp-calc:v9 -> 147000
             \______________/
                  DIFF
```

## Runtime Architecture

```text
                  WORKFLOW DEFINITION
                          |
                          v
                  WORKFLOW COMPILER
                          |
                          v
               COMPILED IMMUTABLE PLAN
                          |
                          v
                    INSTANCE SERVICE
                          |
                          v
                    DURABLE SCHEDULER
                          |
          +---------------+---------------+
          v               v               v
       Timers          Signals        Ready Nodes
                                          |
                                          v
                                  EXECUTION WORKERS
                                          |
                         +----------------+----------------+
                         v                v                v
                    Capability        Human Task        Agent
                     Gateway            Service         Runtime
                         |
                         v
                Entitlement + AuthZ + Legal
                         |
                         v
                    Domain Systems
                         |
                         v
                       Ledger
                         |
             +-----------+-----------+
             v           v           v
        Projections   Operations   Telemetry
                         |
                         v
                  Repair / Debug Center
```

The central entities are `WorkflowDefinition`, `WorkflowVersion`, `CompiledWorkflow`, `WorkflowInstance`, `NodeDefinition`, `NodeExecution`, `WorkflowTimer`, `SignalSubscription`, `HumanTask`, `ApprovalRequirement`, `ApprovalDecision`, `ExecutionLease`, `Checkpoint`, `TransactionPlan`, `RepairPlan`, `MigrationPlan`, and `WorkflowEvent`.

## Example: Compiled Promotion Workflow

```text
START
  |
  v
CAPABILITY people.worker.snapshot
  |
  v
CAPABILITY compensation.simulate
  |
  v
CAPABILITY legal.rules.evaluate
  |
  v
DECISION raise > 10%?
  | no ------------------------------+
  | yes                              |
  v                                  |
APPROVAL FinancePartnerFor(costCenter)
  |                                  |
  +----------------------------------+
  |
  v
APPROVAL ManagerOf(worker)
  |
CHECKPOINT -> WAIT effective date
  |
CAPABILITY transaction.revalidate
  |
DECISION still valid?
  +-- no --> TASK / REAPPROVAL
  +-- yes -> CHECKPOINT -> worker.promote.execute
                              |
                     +--------+--------+
                     v        v        v
                  payroll    IAM    learning
                     +--------+--------+
                              |
                             JOIN
                              |
                      OBSERVE reconciliation
                              |
                   +----------+----------+
                   v                     v
               COMPLETE              RepairPlan
```

## Immutable History, Repairable Execution

Past execution remains immutable. Current and future execution may be repaired through new authorized actions and ledger events.

If a completed step was wrong, HCM Next does not delete that step from history. It records detection, intervention, compensation or correction, revalidation, and resumed execution.

```text
A -> B -> C -> D
          │
          ▼
     Problem detected
          │
          ▼
      Repair opened
          │
          ├── compensate C
          ├── execute corrected C2
          └── revalidate D
                    │
                    ▼
                    E
```

The effective business state changes because later facts compensate, correct, or supersede earlier facts. The historical record continues to show what actually happened.

## Intervention Taxonomy

There is no generic `force workflow` operation. Every intervention declares its semantics.

| Operation         | Meaning                                                          |
| ----------------- | ---------------------------------------------------------------- |
| Retry             | Run the same failed step again under its idempotency contract    |
| Resume            | Continue from the last valid checkpoint                          |
| Skip              | Declare a step unnecessary under an authorized exception         |
| Satisfy           | Supply the trusted evidence or result a step required            |
| Override          | Proceed despite a blocking condition under exceptional authority |
| Rewind            | Return control to an earlier safe checkpoint                     |
| Compensate        | Execute business operations that counter prior effects           |
| Roll forward      | Accept existing effects and perform corrective next steps        |
| Supersede         | End this transaction in favor of a linked replacement            |
| Migrate           | Move the live instance to another compatible workflow version    |
| Cancel            | Stop further execution under cancellation policy                 |
| Repair projection | Rebuild derived state from authoritative ledger facts            |
| Reconcile         | Compare intended, derived, and observed state                    |

Each operation has its own capability, required authority, preconditions, risk class, simulation behavior, approval policy, ledger event, and reconciliation obligations.

## Short Circuit, Bypass, Repair, and Break Glass

These terms are deliberately distinct:

| Concept       | Meaning                                                                    |
| ------------- | -------------------------------------------------------------------------- |
| Short circuit | An expected alternate path defined by workflow and policy                  |
| Bypass        | An exceptional path outside the ordinary process                           |
| Repair        | An intervention responding to failed, incorrect, or inconsistent execution |
| Break glass   | Temporary emergency elevation of privilege                                 |

A short circuit is normal business behavior. For example, a returning worker may not require a background check, or a low-value merit adjustment may automatically satisfy one approval under a published policy.

```text
Hire -> evaluate background requirement
          ├── required -> background check -> continue
          └── exempt   -> step skipped      -> continue
```

The skip still produces a fact identifying the step, rule, reason, actor, and policy version.

A bypass is exceptional. It may allow a payroll executive to proceed near a cutoff when an approver is unavailable. A repair responds to something already wrong. Break glass temporarily expands authority so an emergency action can be attempted. None of these concepts implicitly grants the others.

## Bypass Obligations

A bypass decision is never merely `allowed = true`. It produces enforceable obligations.

Potential obligations include:

- Capture a structured reason
- Identify the granting authority
- Link an incident, ticket, or case
- Require a second approver
- Notify designated roles
- Restrict the bypass to one transaction or capability
- Expire the elevated authority immediately or at a defined time
- Require retrospective review within a deadline
- Require post-action reconciliation
- Prevent reuse for bulk operations

The platform tracks whether each obligation was fulfilled. An overdue retrospective review or failed reconciliation becomes an operational condition and may suspend further bypass authority.

## Separate Repair Authority

Normal execution, approval, repair, override, migration, and administration are different authority families.

```text
workflow.execute.*
workflow.approve.*
workflow.repair.*
workflow.override.*
workflow.migrate.*
workflow.admin.*
```

Sensitive capabilities include:

- `workflow.repair.force_transition`
- `workflow.repair.satisfy`
- `workflow.repair.compensate`
- `workflow.repair.rewind`
- `workflow.repair.supersede`
- `workflow.instances.migration.execute`
- `workflow.versions.quarantine`

An approver is not automatically a repair operator. Repair authority may require specialized roles, step-up authentication, dual control, narrower tenant or company scope, incident linkage, and mandatory after-action review.

Agents receive repair capabilities only through explicit grants and cannot infer them from ordinary workflow authority.

## Workflow Version Operational States

Workflow definitions have lifecycle and operational states:

```text
draft
  -> validated
  -> published
  -> active
  -> quarantined
  -> deprecated
  -> retired
```

`Quarantined` means the version is suspected or known to be unsafe:

- No new instances may start.
- Existing instances pause at the next safe point or continue according to incident policy.
- Affected instances and transactions are identified.
- Operators investigate the common execution fingerprint.
- Repair, migration, compensation, or controlled completion plans are generated.

Quarantine is reversible only through an authorized, reviewed activation decision. It is distinct from deprecation, which discourages new use, and retirement, which ends normal availability.

## Version Pinning and Revalidation

Running workflow instances remain pinned to their published workflow version by default. New publication never silently changes the semantics of an existing instance.

Different configuration dimensions have different temporal behavior:

| Dimension                 | Default treatment                                                               |
| ------------------------- | ------------------------------------------------------------------------------- |
| Workflow graph            | Pinned to the instance                                                          |
| Deterministic block logic | Pinned                                                                          |
| Input/output schemas      | Pinned or explicitly migrated                                                   |
| Integration mapping       | Pinned to the approved transaction plan                                         |
| Metadata/reference data   | Pinned where required for reproducibility; freshness checked as policy requires |
| Business policy           | Historical version retained; current validity rechecked at critical boundaries  |
| Authorization policy      | Always evaluated against current authority before execution                     |
| External observed state   | Refreshed according to risk and freshness requirements                          |

The key distinction is:

```text
Workflow semantics = pinned
Security authority = current
Business validity  = revalidated at critical boundaries
```

Historical approval or authorization cannot justify a material action indefinitely after roles, relationships, policy, jurisdiction, or worker state change.

## Execution Fingerprint

Every material execution records a stable fingerprint covering:

- Workflow version
- Block and runtime versions
- Event and data schema versions
- Proposal version
- Policy and permission-policy versions
- Mapping and connector versions
- Metadata and reference-data versions
- AI prompt, model, and review-policy versions
- Hierarchical scope resolution and override versions
- Repair or migration algorithm version when applicable

The fingerprint allows operators to identify every transaction affected by one bad workflow, policy, mapping, block, runtime, or scoped override release.

## Workflow Migration

Migration of a running instance is a governed transaction, never an implicit consequence of publication.

A `WorkflowMigrationPlan` contains:

- Source and target workflow versions
- Current instance state and last safe point
- State mapping
- Data and schema transforms
- Completed side effects and compensation constraints
- Newly required, removed, or changed steps
- Approval validity impact
- Current authorization and policy requirements
- Expected business and operational impact
- Validation, simulation, rollback, and reconciliation criteria
- Affected scope and instance population

```text
promotion:v7 / awaiting_finance
             │
             ▼
     migration preview
       state mapping
       data transforms
       new compliance step
       approval revalidation
             │
             ▼
      approve migration
             │
             ▼
promotion:v9 / mapped state
```

The ledger records the source and target versions, plan, approvers, transforms, exact migration time, and subsequent verification.

## Migration Compatibility

Compatibility is evaluated before a live instance may migrate:

| Classification  | Meaning                                                            |
| --------------- | ------------------------------------------------------------------ |
| Safe            | Current state and data have direct equivalents                     |
| Transformable   | State or data can be mapped deterministically                      |
| Requires repair | Business semantics changed and need corrective or additional work  |
| Impossible      | Irreversible effects or incompatible meaning make migration unsafe |

Migration may be forbidden when required steps can no longer be meaningfully performed, completed external effects cannot be reconciled under the target semantics, approvals refer to an incompatible proposal, or the state transform would invent unsupported business facts.

An impossible migration leads to controlled completion on the old version, supersession, compensation, cancellation, or a dedicated repair workflow—not an ad hoc state edit.

## Safe Points and Atomic Regions

Checkpoints define operational safe points, not merely replay optimizations.

```text
Safe point A
    │
    ▼
atomic execution region
    │
    ▼
Safe point B
```

Pause, migration, rewind, cancellation, and most repairs occur only at safe points. Atomic regions declare the side effects that must complete, fail, or compensate together before administrative intervention is allowed.

If an emergency requires intervention inside an atomic region, the runtime creates a high-risk repair case describing partial effects rather than pretending the instance is at a clean checkpoint.

Safe-point policy also defines how long-running waits, human tasks, child workflows, and external calls respond to quarantine, cancellation, or migration.

## Governed Intervention APIs

Execution escape hatches use the same capability plane as normal product behavior:

```text
workflow.instances.pause
workflow.instances.resume
workflow.instances.retry
workflow.instances.skip
workflow.instances.satisfy
workflow.instances.override
workflow.instances.rewind
workflow.instances.compensate
workflow.instances.supersede
workflow.instances.cancel

workflow.instances.migration.preview
workflow.instances.migration.execute

workflow.versions.quarantine
workflow.versions.activate

repair.plan
repair.simulate
repair.execute

scope.resources.resolve
scope.resources.share
scope.resources.override
```

These capabilities expose governed operations rather than raw runtime state. Their manifests declare risk, required evidence, safe-point behavior, fields and systems affected, approval requirements, reversibility, and audit obligations.

Agents and operators use identical primitives. Agents may diagnose and propose intervention plans within their authority, while high-risk execution remains subject to current authorization, simulation, approval, and deterministic runtime controls.

## Runtime Scheduling and Resource Contract

Business semantics sit above explicit durable-execution mechanics:

```text
workflow ready/timer due
          |
          v
priority queue partitioned by cell + tenant + criticality
          |
          v
admission + tenant budget + dependency health
          |
     execution lease
          |
   +------+-------+
   |              |
heartbeat       lease lost / worker crash
   |              |
   v              v
checkpoint     safe redelivery after lease expiry
   |
deduplicated transition + outbox commit
```

The runtime contract defines:

- Durable timer ownership, sharding, clock-skew bounds, and wake-up deduplication
- Cell, tenant, capability, and P0-P4 priority queues
- Execution leases, fencing tokens, heartbeats, expiry, and crash recovery
- Maximum fan-out, child-workflow count, payload size, and concurrent activities
- Per-tenant concurrency and cost budgets
- Queue backpressure propagated to workflow admission and agent planning
- One retrying layer, shared retry budgets, deadlines, jitter, and `DO_NOT_RETRY`
- Poison-message quarantine and typed dead-letter cases with replay authorization
- Stuck-workflow detection based on expected progress, not wall-clock duration alone
- Replay isolation so backfills and repairs cannot starve live P0/P1 work
- Bulk-work throttling, pause/resume checkpoints, drain behavior, and fair scheduling

Phase 1 may implement this with PostgreSQL-backed durable timers and queues plus Go workers. A Kafka-class bus or dedicated orchestration product is not required until measured throughput, isolation, or operational evidence justifies it.

## Go-Only Implementation Shape

```text
internal/workflow/
  definitions/       compiler/          plans/
  instances/         nodes/             transitions/
  scheduler/         leases/            retries/
  timers/            signals/           tasks/
  approvals/         checkpoints/       cancellation/
  intervention/      migration/         replay/
  inspector/         projections/

api/proto/workflow/v1/
  definition.proto   compiled_plan.proto
  instance.proto     node_execution.proto
  task.proto         approval.proto
  timer.proto        signal.proto
  intervention.proto inspector.proto

cmd/
  hcm-api             hcm-workflow-worker
  hcm-timer-worker    hcm-workflow-operator
```

Start in the modular Go platform with PostgreSQL transactions, `FOR UPDATE SKIP LOCKED`-style bounded claiming where appropriate, fencing tokens, transactional outbox records, Protobuf contracts, generated clients, and OpenTelemetry correlation. A third-party durable-workflow engine remains an implementation option only if it preserves HCM Next's authority, evidence, portability, tenant isolation, intervention, and replay contracts without making its internal history the sole business ledger.

GoWebComponents consumes task, inspector, simulation, and intervention capabilities through grpcbridge; it does not receive direct runtime-table access. SchemaFlux may compile the declarative workflow IR and supporting schemas where it fits, while Protobuf remains the service contract authority and database migrations remain explicit.

## Phase Classification

| Capability                                             | Phase 1 depth                                          |
| ------------------------------------------------------ | ------------------------------------------------------ |
| Definition/version and compiled plan                   | **IMPLEMENT** for Promotion primitives                 |
| Capability, decision, approval, task, wait, observe    | **IMPLEMENT**                                          |
| Checkpoint, bounded parallel/join, typed end           | **IMPLEMENT** where Promotion requires them            |
| Instance/node state, leases, timers, retries, outbox   | **IMPLEMENT**                                          |
| Proposal-bound approval and current-authority recheck  | **IMPLEMENT**                                          |
| Safe pause, quarantine, cancellation, RepairPlan route | **IMPLEMENT** for pilot failure cases                  |
| Simulation and execution modes                         | **IMPLEMENT**                                          |
| Read-only inspector and pure-node replay               | **IMPLEMENT** to pilot-operability depth               |
| Signals, subworkflows, documents, agents               | **MINIMAL CONTRACT** unless required by the pilot      |
| Shadow mode and live-instance migration                | **DESIGN / CONFORMANCE ONLY**                          |
| Customer-authored compensation and arbitrary loops     | **OUT OF PHASE**                                       |
| General-purpose orchestration product compatibility    | **OUT OF PHASE** unless an adoption decision is opened |

## Phase 1 Acceptance Contract

The Promotion workflow must prove:

- Compiler rejection of type-invalid edges, unresolved capabilities, conflicting parallel write sets, and retried non-idempotent mutations
- Durable resume after process failure during approval, effective-date wait, and connector retry
- Exact proposal binding plus current approver-authority re-evaluation after relationship change
- Stable node/capability idempotency across lease expiry and redelivery
- `PAUSE_REQUESTED` truthfulness inside an atomic external-effect region
- Cancellation behavior before approval, after approval, and after an internal or ambiguous external effect
- Bounded retry exhaustion into a typed RepairPlan or human route
- Separate runtime, business, external-consistency, reconciliation, and obligation completion states
- Simulation suppresses all mutations and external effects
- Replay of pure nodes cannot produce tasks, notifications, charges, or business effects
- Inspector traversal from workflow to node, approval/AuthZ/legal decision, transaction, connector operation, reconciliation, and trace
- Tenant and criticality limits prevent bulk/replay work from starving the live pilot path

The reference workflows remain compiler and simulation fixtures. They must not expand Phase 1 implementation merely because their graphs exercise future primitives.
