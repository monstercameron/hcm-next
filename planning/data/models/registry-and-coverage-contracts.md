# Registry and Coverage Contracts

The prose catalogs define the exploratory vocabulary. They do not, by
themselves, prove that every BusinessIntent is executable. These registries are
the normative closure model that later SchemaFlux schemas and Go generators
must implement. An intent is `COVERED` only when the coverage checker can resolve
all referenced types, properties, relationships, transitions and policies.

## Canonical entity registry

```text
EntityDefinition
  entity_type_id, canonical name, aliases[]
  owner plane/domain/service/team
  entity class = AGGREGATE_ROOT | CHILD | VALUE | EVENT | EVIDENCE | READ_MODEL
  aggregate root ref, lifecycle profile ref
  tenant/org/partition/stream identity rules
  authority/correction/retention/deletion policies
  property definition refs[], relationship definition refs[]
  invariant refs[], schema release ref, status

PropertyDefinition
  property_id, entity/path/name
  SchemaFlux/Protobuf/Go type refs
  required/optional/presence/null/default semantics
  allowed values/constraints
  unit/currency/scale/precision/rounding/calendar semantics
  classification/compartment/purpose/agent/search/egress policies
  effective/known/recorded temporal behavior
  authority/correction/supersession/retention behavior
  validation/invariant refs, status

AggregateDefinition
  aggregate_id, root/child entity refs
  command boundary/stream identity/version-CAS rules
  local ACID/cross-aggregate transaction/reservation policies
  lifecycle/correction/deletion/retention rules

RelationshipDefinition
  relationship_type_id, canonical name
  source/target entity types, cardinality/ownership
  effective-time/overlap/uniqueness/ordering rules
  tenant/org/authority/visibility constraints
  merge/split/correction/deletion behavior

StateDefinition
  state_id, lifecycle profile/entity, name
  initial/terminal/absorbing/correctable semantics

TransitionDefinition
  transition_id, lifecycle profile/from/to states
  triggering intent/capability, guards/invariants
  actor authority/effective behavior
  cancellation/correction/supersession/compensation behavior

AuthorityAssignment
  assignment_id, tenant/org/entity/property/operation scope
  authority system/connection/source role/epoch
  effective/recorded interval, precedence/composition
  observation/correction/handoff/fence/conflict policies
```

## Exact intent registry and binding

The canonical 530-name manifest is a required source artifact, not a generated
guess. It must preserve the catalog number and spelling supplied by product
planning. Each entry resolves to one `IntentEntityBinding`:

```text
BusinessIntentManifestEntry
  catalog_number, intent_type_id, display_name
  domain/owner/kernel family/maturity/phase depth
  input/result schema refs, subject kinds
  risk/side-effect/authority classes
  canonical source digest/version/status

IntentEntityBinding
  binding_id, intent manifest entry/version
  aggregate root refs[]
  read property refs[]
  propose/create/update/correct property refs[]
  calculation/decision/approval/human-work entity refs[]
  legal/privacy/evidence/retention requirements[]
  reservation/conflict/revalidation rules[]
  internal/external/human/system effect definitions[]
  observation/reconciliation/repair policies[]
  lifecycle transitions[]
  negative-state policy ref
  conformance scenario refs[], status

IntentEffectBinding
  binding_id, intent/entity-property mutation or side effect
  owning capability/authority assignment
  side-effect class/idempotency/ordering/reversibility
  expected observation/reconciliation/repair contract

IntentEvidenceBinding
  binding_id, intent/risk/jurisdiction context
  evidence requirements/satisfiers/validity/compartment
  required before proposal/approval/execution/closure
```

## Negative-state policy

```text
NegativeStatePolicy
  policy_id, intent/binding/version
  per-state actions for UNKNOWN | PARTIAL | DEGRADED | AMBIGUOUS |
    REDACTED | UNAVAILABLE | STALE | NOT_APPLICABLE | REPAIR_REQUIRED |
    QUARANTINED | WAIVED | DISPUTED
  action = BLOCK | ROUTE_HUMAN | ALLOW_WITH_WARNING | USE_STALE |
           CREATE_OBLIGATION | CREATE_REPAIR | DEGRADE | PERMIT_CLOSURE
  evidence/authority/expiry/revalidation requirements
```

## Coverage checker rejection rules

```text
Reject an intent without an exact manifest entry, owner, input or result schema.
Reject an unknown entity, property, relationship, lifecycle state or transition.
Reject a property without a concrete type and presence semantics.
Reject a write without an authority assignment and correction behavior.
Reject a mutation without a lifecycle transition or explicit no-state-change rule.
Reject an external effect without observation, ambiguity and repair policies.
Reject a high-risk action without governance/evidence/negative-state handling.
Reject a correction without the target assertion and downstream impact policy.
Reject a batch without a frozen population and per-item result semantics.
Reject analytics without purpose, temporal, authority and disclosure policies.
Reject a trigger without source schema, ordering, dedupe and replay behavior.
Reject a sensitive property without compartment, search, agent and egress policy.
Reject retirement while a hard consumer lacks an adopted migration.
```

## Canonical boundary decisions

```text
Person is a party; Candidate and Worker are roles around Person.
Employment is a legal relationship; Assignment locates work within it.
WorkerStatus is a derived projection; EmploymentStatusRevision is evidence.
OrganizationUnit is a business structure; LegalEntity is a registered employer.
Job describes work; Position represents capacity; Requisition requests openings.
Document is a governed logical record; ArtifactObject owns immutable bytes.
EvidenceArtifact expresses evidentiary use; Attachment is an ingestion relation.
Credential is issued qualification; WorkAuthorization is permission to work;
IdentityProof is purpose-bound identity assurance.
Case coordinates protected work; LeaveCase, AccommodationCase, MobilityCase and
Investigation are specialized roots with their own compartments and lifecycles.
Assessment evaluates evidence; Decision selects an authorized outcome;
Recommendation is non-authoritative; Observation records sourced state.
ContactPoint identifies a communication value; DeliveryEndpoint adds channel
delivery policy/provider state.
Reservations are domain-owned typed aggregates and share a reservation protocol;
there is no untyped global resource reservation row.
HumanTask is the workflow primitive; WorkItem is its single durable aggregate.
```

Canonical type/alias rules:

```text
Schema + SchemaRelease are canonical internal contract types.
SchemaSnapshot is an immutable observed vendor descriptor.
ExternalSchemaContract is a reviewed connector-facing contract derived from a
SchemaSnapshot; it is not a competing global SchemaRelease.
ReconciliationPolicy is canonical; ReconciliationRun executes it.
RecoveryContract states objectives; RecoveryPlan selects actions for an incident;
RecoveryScenario supplies assumptions; RecoveryRun records execution.
BackupArtifact is one immutable object; BackupSet is its chain/manifest aggregate.
TenantCell and TenantPlacement in operations-production.md are canonical;
TenantCellSummary and TenantPlacementRecord are read models only.
TenantRelocationPlan is canonical; TenantRelocationSummary is a read model.
CredentialLease is the general secret/key lease; WorkloadCredentialLease is its
workload-bound specialization and must reference the general lease.
SoftwareApplication is the IAM target; Application is a recruiting application.
```

## Initial aggregate registry

```text
PersonAggregate
  root Person; owns person names, demographic assertions, contact/address links,
  identity claims and PersonRole links; versioned by Person stream.

WorkerAggregate
  root Worker; references Person and Employment roots; it does not own legal
  employment facts. WorkerStatus is a reconstructable projection.

EmploymentAggregate
  root Employment; owns status/start/end/classification/contract/amendment facts
  and references independently versioned Assignment roots.

AssignmentAggregate
  root Assignment; owns assignment revisions, work-location segments and
  assignment-scoped relationships. Manager edges are first-class relationships.

PositionAggregate
  root Position; owns revisions/capacity intervals/reservations/occupancies with
  position-stream CAS. Worker assignment changes join through BusinessTransaction.

BusinessIntentAggregate
  root IntentInstance; owns proposal/result/relationship/closure refs but does
  not embed their mutable rows. Each child aggregate has its own identity/stream.

WorkflowAggregate
  root WorkflowInstance; owns runtime frontier and references NodeExecution,
  timers, signals, work items, leases and variable revisions by stable IDs.

PayrollRunAggregate
  root PayrollRun; owns frozen population/results/release control state while
  worker calculations and payroll-ledger entries retain independent identities.

LeaveCaseAggregate
  root LeaveCase; coordinates but does not embed entitlement ledgers, medical
  evidence, occurrences, accommodation cases or external effect journals.

CaseAggregate
  root Case; coordinates participants/tasks/decisions. Notes, evidence artifacts,
  holds and investigations are separately access-controlled aggregates.

CompensationAggregate
  root CompensationPackage; owns package/component revisions. Grades, bands,
  budgets, grants and merit cycles are independent roots joined by transactions;
  their canonical roots are CompensationGrade, CompensationBand,
  CompensationBudget, RewardGrant and MeritCycle.

BenefitElectionAggregate
  root BenefitElection; owns election revisions and covered-dependent intervals.
  BenefitProgram, BenefitPlan, EnrollmentWindow, BenefitRateSchedule, LifeEvent and
  BenefitContinuation are separate roots.

TimecardAggregate
  root Timecard; owns immutable revisions and entry/punch associations. Raw punches,
  TimeBalance, WorkSchedule, Shift, ShiftAssignment and ShiftOffer are separate
  roots; TimeBalanceLedgerEntry is immutable evidence owned by TimeBalance.

RecruitingAggregateSet
  roots Requisition, JobPosting, Candidate, Application, BackgroundCheck and Offer;
  no single ATS mega-aggregate. Opening allocation and proposal transactions join them.

AccessAggregateSet
  roots WorkforceIdentity, DigitalAccount, AccessGrant, AccessRequest and
  AccessReviewCampaign. DesiredAccessState and AccessPlan coordinate without ownership.

LearningAggregateSet
  roots LearningAssignment, LearningEnrollment, LearningCompletion, Credential and
  WorkerSkillProfile. Requirements/evaluations are independent versioned roots.

CommunicationAggregateSet
  roots MessageIntent, RecipientMessage and ConversationThread. Provider attempts and
  observations have independent immutable identities; bulk plans coordinate recipients.

DocumentAggregateSet
  roots Document, FormSubmission and SignatureRequest. ArtifactObject owns bytes;
  signatures/acknowledgements/evidence packages are independent evidence roots.

RegulatoryAggregateSet
  roots RulePackRelease, RegulatoryObligation, GovernmentFiling and FilingPackage.
  Evaluations/calculations are immutable result roots, not children of workers.

IntegrationAggregateSet
  roots ConnectorConnection, IntegrationReceipt, SyncJob and ConnectorOperation.
  External observations and conflicts remain separate evidence/repair roots.

DataOpsAggregateSet
  roots ImportJob, ImportRun, BatchOperation, ExportJob and ConfigurationPackage.
  Staged records/items are partition-owned children with independent item outcomes.

TenantAggregateSet
  roots Tenant, TenantEnvironment, TenantProvisioningRun, TenantExitRun and
  TenantRelocationPlan. Placement/cell/key/config resources are separate roots.

CommercialAggregateSet
  roots BillingAccount, CommercialContract, CommercialOrder, Subscription, Invoice,
  CommercialPayment, BillingDispute and CustomerBudget. Usage/rating facts are immutable.
```

Cross-root writes use `BusinessTransaction` + `TransactionParticipant`. Atomicity
is limited to participants in one declared database boundary; external systems
are durable effects with observation/reconciliation/repair, never fake ACID.

## Initial lifecycle profiles

```text
BusinessTransactionLifecycle
  PROPOSED -> REVALIDATING -> COMMITTING -> COMMITTED
  REVALIDATING -> ABORTED
  COMMITTING -> AMBIGUOUS -> COMMITTED | ABORTED | REPAIR_REQUIRED

WorkflowInstanceLifecycle
  CREATED -> RUNNING -> WAITING | PAUSE_REQUESTED | BLOCKED | COMPLETED |
  REPAIR_REQUIRED | QUARANTINED
  PAUSE_REQUESTED -> PAUSED -> RUNNING
  RUNNING|WAITING|PAUSED -> CANCELLING -> CANCELLED | REPAIR_REQUIRED
  BLOCKED -> RUNNING | CANCELLING | REPAIR_REQUIRED | QUARANTINED
  REPAIR_REQUIRED -> RUNNING | CANCELLED | SUPERSEDED | QUARANTINED
  QUARANTINED -> RUNNING | CANCELLED | SUPERSEDED after release evidence
  any nonterminal -> SUPERSEDED
  Runtime state is only one dimension; WorkflowStatusDimensions separately records
  business, external-consistency, reconciliation, obligation and operational state.

NodeExecutionLifecycle
  READY -> RUNNING -> SUCCEEDED | WAITING | FAILED | RETRYING | AMBIGUOUS
  RETRYING -> READY; WAITING -> READY; FAILED|AMBIGUOUS -> REPAIR_REQUIRED
  READY|WAITING -> SKIPPED | CANCELLED; SUCCEEDED -> COMPENSATED

WorkItemLifecycle
  CREATED -> ROUTED -> ASSIGNED | AVAILABLE
  AVAILABLE -> CLAIMED; ASSIGNED|CLAIMED -> IN_PROGRESS
  IN_PROGRESS -> COMPLETED | RETURNED | ESCALATED | EXPIRED
  nonterminal -> CANCELLED; COMPLETED -> INVALIDATED -> CORRECTION_REQUIRED

WorkflowTimerLifecycle
  SCHEDULED -> CLAIMED -> FIRED
  SCHEDULED|CLAIMED -> CANCELLED | SUPERSEDED
  CLAIMED -> SCHEDULED only after lease expiry and unchanged fence

EmploymentLifecycle
  PENDING -> ACTIVE -> SUSPENDED | LEAVE | ENDED
  SUSPENDED|LEAVE -> ACTIVE
  ENDED -> REINSTATED only through corrective intent; rehire creates new Employment

PayrollRunLifecycle
  DRAFT -> SNAPSHOTTED -> CALCULATING -> EXCEPTIONS | CALCULATED
  EXCEPTIONS -> CALCULATING | CALCULATED
  CALCULATED -> APPROVED -> RELEASED -> SETTLING -> CLOSED
  pre-release states -> CANCELLED; post-release correction creates a new run/repair

CaseLifecycle
  OPEN -> TRIAGED -> ASSIGNED -> IN_PROGRESS -> RESOLVED -> CLOSED
  RESOLVED|CLOSED -> REOPENED -> IN_PROGRESS
  nonterminal -> ON_HOLD | ESCALATED; hold exit returns to prior state

DocumentLifecycle
  RECEIVED -> QUARANTINED | VALIDATING -> VALID | INVALID
  VALID -> ACTIVE -> EXPIRED | SUPERSEDED | RETENTION_PENDING
  RETENTION_PENDING -> DESTROYED only when holds/obligations permit

CompensationPackageLifecycle
  DRAFT -> PROPOSED -> APPROVED -> SCHEDULED -> ACTIVE -> SUPERSEDED | ENDED
  any assertion -> CORRECTION_PENDING -> superseding immutable revision

BenefitElectionLifecycle
  DRAFT -> SUBMITTED -> VALIDATED -> PENDING_CARRIER -> ACTIVE
  PENDING_CARRIER -> FAILED | AMBIGUOUS | ACTIVE
  ACTIVE -> CHANGE_PENDING | END_PENDING -> ACTIVE | ENDED
  any authoritative state -> CORRECTION_PENDING -> superseding revision

TimecardLifecycle
  OPEN -> SUBMITTED -> APPROVED | REJECTED
  REJECTED -> OPEN; APPROVED -> LOCKED -> PAYROLL_BOUND
  LOCKED|PAYROLL_BOUND -> REOPEN_PENDING -> OPEN | CORRECTION_REQUIRED

ScheduleLifecycle
  DRAFT -> VALIDATED -> APPROVED -> PUBLISHED -> ACTIVE -> COMPLETED | CANCELLED
  PUBLISHED|ACTIVE changes create a new revision plus notice/premium consequences

RequisitionLifecycle
  DRAFT -> SUBMITTED -> APPROVED -> OPEN -> ON_HOLD -> OPEN
  OPEN -> FILLED | CLOSED | CANCELLED; material revision returns to approval

ApplicationLifecycle
  DRAFT -> SUBMITTED -> ACTIVE -> SELECTED | REJECTED | WITHDRAWN | DUPLICATE
  ACTIVE stage changes append transitions; closed dispositions never erase history

OfferLifecycle
  DRAFT -> APPROVAL_PENDING -> APPROVED -> SENT -> ACCEPTED | DECLINED |
  EXPIRED | RESCINDED; material revision invalidates prior response and returns to approval

AccessGrantLifecycle
  PROPOSED -> APPROVED -> PROVISIONING -> ACTIVE
  PROVISIONING -> FAILED | AMBIGUOUS | ACTIVE
  ACTIVE -> CHANGE_PENDING | REVOCATION_PENDING -> ACTIVE | REVOKED
  revocation takes precedence over stale provisioning

LearningAssignmentLifecycle
  PROPOSED -> ASSIGNED -> ACCEPTED | ENROLLED -> IN_PROGRESS -> COMPLETED
  ASSIGNED|ACCEPTED|ENROLLED|IN_PROGRESS -> WAIVED | CANCELLED | EXPIRED
  due-date transition may add OVERDUE without erasing the execution state

CredentialLifecycle
  UNVERIFIED -> ISSUED -> ACTIVE -> EXPIRING -> EXPIRED
  ACTIVE|EXPIRING -> SUSPENDED | REVOKED | RENEWAL_PENDING
  RENEWAL_PENDING -> REPLACED | ACTIVE; renewal creates a linked successor

MessageLifecycle
  DRAFT -> PLANNED -> QUEUED -> DELIVERING -> PARTIAL | SATISFIED | FAILED |
  EXPIRED; any pre-satisfaction state -> CANCELLED | SUPERSEDED

ConversationLifecycle
  OPEN -> PENDING_PARTICIPANT | PENDING_HR | RESOLVED -> CLOSED
  CLOSED -> REOPENED -> OPEN; participant membership remains independently effective-dated

SignatureRequestLifecycle
  DRAFT -> READY -> SENT -> VIEWED -> SIGNING -> PARTIALLY_SIGNED | COMPLETED
  SENT|VIEWED|SIGNING|PARTIALLY_SIGNED -> DECLINED | EXPIRED | CANCELLED | AMBIGUOUS

RegulatoryObligationLifecycle
  IDENTIFIED -> ASSIGNED -> IN_PROGRESS -> SATISFIED
  IDENTIFIED|ASSIGNED|IN_PROGRESS -> OVERDUE | WAIVER_PENDING | DISPUTED
  WAIVER_PENDING -> WAIVED | IN_PROGRESS; evidence invalidation reopens evaluation

GovernmentFilingLifecycle
  DRAFT -> GENERATED -> VALIDATED -> APPROVED -> SUBMITTING
  SUBMITTING -> SUBMITTED | REJECTED | AMBIGUOUS
  SUBMITTED -> ACCEPTED | REJECTED; amendment creates a linked filing

ConnectorConnectionLifecycle
  DRAFT -> VALIDATING -> READY -> ACTIVE -> DEGRADED | SUSPENDED
  any active state -> QUARANTINED | REVOKED; release requires validation

ConnectorOperationLifecycle
  PLANNED -> QUEUED -> RUNNING -> SUCCEEDED | FAILED | AMBIGUOUS | PARTIAL
  FAILED -> REDRIVE_PENDING; AMBIGUOUS -> OBSERVATION_PENDING before retry

ImportRunLifecycle
  CREATED -> PROFILING -> MAPPING -> VALIDATING -> SIMULATED -> APPROVED -> COMMITTING
  COMMITTING -> PARTIAL | COMMITTED | FAILED | AMBIGUOUS | REPAIR_REQUIRED

ConfigurationPackageLifecycle
  DRAFT -> VALIDATED -> TESTED -> APPROVED -> PUBLISHED -> SUPERSEDED | RETIRED
  PUBLISHED -> QUARANTINED; rollback publishes a prior known-good package anew

TenantLifecycle
  PROVISIONING -> ACTIVE -> SUSPENDED -> ACTIVE
  ACTIVE|SUSPENDED -> EXIT_PENDING -> RETENTION_ONLY | CLOSED
  CLOSED -> DESTRUCTION_PENDING -> DESTROYED when holds and obligations permit

InvoiceLifecycle
  DRAFT -> FINAL -> ISSUED -> PARTIALLY_PAID | PAID | PAST_DUE | VOID
  issued corrections create adjustment/amended invoice facts, never mutate final lines

RevisionedReferenceLifecycle
  DRAFT -> VALIDATED -> APPROVED -> ACTIVE -> SUPERSEDED | RETIRED | QUARANTINED
  an active definition is never edited in place; correction publishes a successor

RevisionedFactLifecycle
  PROPOSED -> VALIDATED -> SCHEDULED | EFFECTIVE
  SCHEDULED -> EFFECTIVE; EFFECTIVE -> ENDED | SUPERSEDED | CORRECTION_PENDING
  correction appends a successor assertion and preserves the original assertion

ImmutableEvidenceLifecycle
  RECORDED -> VERIFIED | DISPUTED | INVALIDATED
  DISPUTED -> VERIFIED | INVALIDATED
  terminal evidence is immutable; a correction or reinterpretation is a linked record

PlanLifecycle
  DRAFT -> VALIDATED -> APPROVAL_PENDING -> APPROVED -> EXECUTING
  EXECUTING -> PARTIAL | COMPLETED | FAILED | AMBIGUOUS | REPAIR_REQUIRED
  pre-execution states -> CANCELLED | SUPERSEDED; execution outcomes are immutable

RequestLifecycle
  DRAFT -> SUBMITTED -> TRIAGED -> APPROVAL_PENDING | IN_PROGRESS
  APPROVAL_PENDING -> APPROVED | REJECTED | MORE_INFORMATION_REQUIRED
  APPROVED|IN_PROGRESS -> FULFILLED | FAILED | CANCELLED | REPAIR_REQUIRED

CampaignLifecycle
  DRAFT -> PLANNED -> ACTIVE -> PAUSED | COMPLETED | CANCELLED
  PAUSED -> ACTIVE | CANCELLED; completion corrections append a linked campaign result

AccountLifecycle
  PENDING -> ACTIVE -> SUSPENDED | RESTRICTED | CLOSURE_PENDING
  SUSPENDED|RESTRICTED -> ACTIVE; CLOSURE_PENDING -> CLOSED
  CLOSED financial or access accounts are not silently reactivated

BatchLifecycle
  DRAFT -> POPULATION_FROZEN -> VALIDATED -> APPROVED -> QUEUED -> RUNNING
  RUNNING -> PARTIAL | COMPLETED | FAILED | AMBIGUOUS | REPAIR_REQUIRED
  item outcomes remain independent and resumable; cancellation stops unscheduled items
```

Every transition is append-only, requires a `LifecycleTransitionRecord`, and may
be narrowed by the owning domain. Domain profiles cannot add a transition that
bypasses governance, legal obligation, correction, or retention rules.

## Aggregate lifecycle assignment registry

Every aggregate root has an explicit lifecycle policy. `AggregateDefinition`
must reference one of these assignments; publishing an unassigned root fails.
`ImmutableEvidenceLifecycle` and `NO_BUSINESS_LIFECYCLE` are affirmative policies,
not defaults. The latter permits only rebuildable read models with no commands.

| Aggregate root(s)                                                                              | Lifecycle assignment                                                               |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Person, Worker                                                                                 | RevisionedFactLifecycle                                                            |
| Employment                                                                                     | EmploymentLifecycle                                                                |
| Assignment, Position                                                                           | RevisionedFactLifecycle                                                            |
| IntentInstance                                                                                 | RequestLifecycle; terminal result additionally follows ImmutableEvidenceLifecycle  |
| WorkflowInstance                                                                               | WorkflowInstanceLifecycle                                                          |
| PayrollRun                                                                                     | PayrollRunLifecycle                                                                |
| LeaveCase, Case                                                                                | CaseLifecycle, with domain-specific protected-state extensions                     |
| CompensationPackage                                                                            | CompensationPackageLifecycle                                                       |
| CompensationGrade, CompensationBand                                                            | RevisionedReferenceLifecycle                                                       |
| CompensationBudget, RewardGrant                                                                | AccountLifecycle, RevisionedFactLifecycle respectively                             |
| MeritCycle                                                                                     | CampaignLifecycle                                                                  |
| BenefitElection                                                                                | BenefitElectionLifecycle                                                           |
| BenefitProgram, BenefitPlan, EnrollmentWindow, BenefitRateSchedule                             | RevisionedReferenceLifecycle                                                       |
| LifeEvent                                                                                      | ImmutableEvidenceLifecycle                                                         |
| BenefitContinuation                                                                            | AccountLifecycle                                                                   |
| Timecard                                                                                       | TimecardLifecycle                                                                  |
| TimePunch                                                                                      | ImmutableEvidenceLifecycle; corrections are linked punches                         |
| TimeBalance                                                                                    | AccountLifecycle; TimeBalanceLedgerEntry follows ImmutableEvidenceLifecycle        |
| WorkSchedule                                                                                   | ScheduleLifecycle                                                                  |
| Shift, ShiftAssignment                                                                         | RevisionedFactLifecycle                                                            |
| ShiftOffer                                                                                     | RequestLifecycle                                                                   |
| Requisition                                                                                    | RequisitionLifecycle                                                               |
| JobPosting                                                                                     | RevisionedReferenceLifecycle with publication/withdrawal extensions                |
| Candidate                                                                                      | RevisionedFactLifecycle; candidate is a concurrent Person role, not a person state |
| Application                                                                                    | ApplicationLifecycle                                                               |
| BackgroundCheck                                                                                | RequestLifecycle; returned evidence follows ImmutableEvidenceLifecycle             |
| Offer                                                                                          | OfferLifecycle                                                                     |
| WorkforceIdentity, DigitalAccount                                                              | AccountLifecycle                                                                   |
| AccessGrant                                                                                    | AccessGrantLifecycle                                                               |
| AccessRequest                                                                                  | RequestLifecycle                                                                   |
| AccessReviewCampaign                                                                           | CampaignLifecycle                                                                  |
| LearningAssignment                                                                             | LearningAssignmentLifecycle                                                        |
| LearningEnrollment                                                                             | RequestLifecycle with enrolled/in-progress/completed extensions                    |
| LearningCompletion                                                                             | ImmutableEvidenceLifecycle                                                         |
| Credential                                                                                     | CredentialLifecycle                                                                |
| WorkerSkillProfile                                                                             | RevisionedFactLifecycle                                                            |
| MessageIntent                                                                                  | MessageLifecycle                                                                   |
| RecipientMessage                                                                               | MessageLifecycle with per-recipient delivery/recipient state dimensions            |
| ConversationThread                                                                             | ConversationLifecycle                                                              |
| Provider delivery attempt/observation                                                          | ImmutableEvidenceLifecycle                                                         |
| Document                                                                                       | DocumentLifecycle                                                                  |
| FormSubmission                                                                                 | RequestLifecycle; accepted submission content is immutable evidence                |
| SignatureRequest                                                                               | SignatureRequestLifecycle                                                          |
| Signature, acknowledgement, evidence package                                                   | ImmutableEvidenceLifecycle                                                         |
| RulePackRelease                                                                                | RevisionedReferenceLifecycle                                                       |
| RegulatoryObligation                                                                           | RegulatoryObligationLifecycle                                                      |
| GovernmentFiling                                                                               | GovernmentFilingLifecycle                                                          |
| FilingPackage, calculation/evaluation result                                                   | ImmutableEvidenceLifecycle                                                         |
| ConnectorConnection                                                                            | ConnectorConnectionLifecycle                                                       |
| ConnectorOperation                                                                             | ConnectorOperationLifecycle                                                        |
| IntegrationReceipt, external observation                                                       | ImmutableEvidenceLifecycle                                                         |
| SyncJob                                                                                        | PlanLifecycle                                                                      |
| ImportJob, BatchOperation, ExportJob                                                           | BatchLifecycle                                                                     |
| ImportRun                                                                                      | ImportRunLifecycle                                                                 |
| ConfigurationPackage                                                                           | ConfigurationPackageLifecycle                                                      |
| Tenant                                                                                         | TenantLifecycle                                                                    |
| TenantEnvironment                                                                              | AccountLifecycle                                                                   |
| TenantProvisioningRun, TenantExitRun                                                           | PlanLifecycle                                                                      |
| TenantRelocationPlan                                                                           | PlanLifecycle                                                                      |
| BillingAccount, Subscription, CustomerBudget                                                   | AccountLifecycle                                                                   |
| CommercialContract, CommercialOrder                                                            | RevisionedFactLifecycle, RequestLifecycle respectively                             |
| Invoice                                                                                        | InvoiceLifecycle                                                                   |
| CommercialPayment                                                                              | ImmutableEvidenceLifecycle; reversals are linked payment facts                     |
| BillingDispute                                                                                 | CaseLifecycle                                                                      |
| TenantCellSummary, TenantPlacementRecord, TenantRelocationSummary, ConfigurationPackageSummary | NO_BUSINESS_LIFECYCLE; reconstructable read models only                            |

Child entities not listed as roots inherit no independent mutation authority.
Their owner, ordering, identity, revision and deletion behavior must be declared in
the owning `AggregateDefinition`. Promoting a child to an independently commanded
resource requires a new root registration and lifecycle assignment first.

## Minimum conformance proof per intent

```text
happy path
authorization deny and restricted-field path
missing/unknown/stale input path
concurrent conflict and stale-baseline path
cancellation before and after first irreversible effect
retry, duplicate and ambiguous commit path
external partial failure, observation and repair path
correction/supersession path
retention/hold/DLP path where data is material
effective-date and known-at replay path
```

The current Markdown catalog is exploratory and broad. Full `530/530 VERIFIED`
status must not be claimed until these registries are instantiated and checked;
until then the honest status is `CONCEPTUALLY_COVERED, EXACT_BINDING_PENDING`.
