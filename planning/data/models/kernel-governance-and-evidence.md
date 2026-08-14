# Kernel, Governance and Evidence Entities

These entities support every intent family. Domain entities never duplicate
their lifecycle, approval, workflow, effect or evidence responsibilities.

## Intent and transaction kernel

```text
IntentDefinition
  intent_type_id, catalog_number, version, display_name, description
  owner_plane, owner_domain, kernel_family, maturity
  input_schema_ref, result_schema_ref
  initiator_kinds[], execution_modes[], subject_kinds[]
  side_effect_profile, risk_class, classification_floor
  required_capabilities[], governance_requirements[]
  preconditions[], invariants[]
  idempotency/conflict/proposal/revalidation rules
  cancellation/compensation/evidence/retention/outcome/SLO policies
  mutation_disposition = NONE | PROPOSE | EXECUTE | CORRECT | REPAIR
  target_authority_policy, result_authority_policy

IntentInstance
  intent_id, definition_ref, tenant/org scope
  initiator_principal_ref, delegation_ref?, purpose
  subject_refs[], requested_effective_interval?
  input_artifact_ref, request_digest, idempotency_key
  lifecycle_dimension_refs, parent_intent_ref?, root_intent_ref
  created/submitted/accepted/closed times

ProposalRevision
  proposal_id, intent_ref, revision, parent_revision_ref?
  canonical input/current/proposed/visible projection digests
  reads[], write_set[], effect_set[], conflicts[]
  reservations[], obligations[], estimates[], warnings[]
  context/control/schema/reference/rule snapshots[]
  simulation_ref, materiality_decision_ref
  status, created_by, created_at

BusinessTransaction
  transaction_id, intent/proposal/execution refs
  consistency_boundary_ref, coordinator workload/fence refs
  transaction_kind, subject/resource refs[]
  write_set_manifest_ref, expected_stream_heads[]
  authority_bindings[], reservation_refs[]
  commit_state, commit_receipt_ref?, ambiguity_state
  effective_interval, recorded_at

TransactionCommitReceipt
  receipt_id, transaction_ref, commit_attempt, commit_fence
  expected_stream_heads[], resulting_stream_heads[]
  appended_event_refs[], projection_write_refs[], outbox_effect_refs[]
  committed_at, database_commit_identity, canonical_commit_digest
  signature/integrity_epoch, verification_status

TransactionAbortRecord
  transaction_ref, attempt, reason, failed_precondition/conflict refs[]
  no_commit_proof, reservations disposition, aborted_at

AmbiguousCommitResolution
  transaction_ref, ambiguous_attempt_ref, first_detected_at
  authoritative_commit lookup strategy and observations[]
  COMMITTED | NOT_COMMITTED | STILL_UNKNOWN
  duplicate-effect fences, resolution/repair refs, resolved_at

MultiStreamTransactionPlan
  plan_id, proposal_revision_ref, canonical_plan_digest
  consistency_boundary_ref, cross-boundary effect refs[]
  expected_stream_heads[], intended_appends[], critical_projection_writes[]
  outbox_effects[], uniqueness/fence checks[], invariant checks[]
  isolation_level, all_or_nothing_boundary, expiry, revalidation policy

StreamHead
  stream_ref, expected sequence/revision token/prior event hash
  authority/fence/observed-at, canonical digest

TransactionParticipant
  participant_id, transaction/aggregate/stream refs
  consistency_boundary_ref, participant admission receipt
  expected stream head/CAS/fence
  ordered append/event/schema/payload digest refs[]
  critical projection writes/outbox effects[]
  PREPARED | COMMITTED | ABORTED | AMBIGUOUS
  participant receipt/error/observed resulting head

ConsistencyBoundary
  boundary_id, tenant/cell/storage/transaction-manager identity
  admitted aggregate/stream/storage selectors
  isolation level/commit protocol/coordinator identity
  participant count/size/deadline/admission rules
  prepare/commit/abort/recovery/fencing behavior
  cross-boundary disposition = REJECT | SPLIT_INTO_EFFECTS | CHILD_TRANSACTION
  replication/failover/durability/health requirements
  effective interval/version/status

ReservationProtocol
  protocol_id, reservation kind/domain/version
  target/quantity/unit/conflict-key semantics
  HELD | CONSUMED | RELEASE_PENDING | RELEASED | EXPIRED | AMBIGUOUS
  fence/expiry/idempotency/renewal/recovery rules

ReservationBinding
  binding_id, typed domain reservation/protocol
  holder intent/proposal/transaction, target resource
  quantity/unit/conflict key/fence/expiry
  consume/release/ambiguity/repair refs

CancellationDecision
  intent/workflow ref, requested_by, reason
  current safe point/effect inventory
  policy result, required compensation/repair
  cancellation fence epoch, decision time, result state

SupersessionLink
  prior_intent_ref, replacement_intent_ref
  relationship, reason, effective_at
  approvals/tasks/reservations/effects/obligations disposition

CorrectionReference
  corrective_intent_ref, target assertion/transaction refs[]
  original/correction effective intervals
  historical/current authority refs
  reason, supersession relation, downstream impact plan

AssertionCorrection
  correction_id, target assertion/entity/property path
  prior/corrected value digests and presence states
  original/corrected effective intervals
  reason/authority/evidence/actor/recorded time
  downstream invalidation/projection/recalculation/reconciliation/repair refs

ClosureRecord
  intent/workflow/transaction refs
  completion_dimensions
  approval_certificate_ref
  obligation/reconciliation/evidence manifests
  unresolved/waived/disputed refs[]
  outcome_tracking_ref?, closure_decision_ref

LifecycleCompatibilityProfile
  profile_id, resource_kind, version
  allowed_from_states[], allowed_to_states[], forbidden_transitions[]
  guard/invariant refs[], cancellation/correction semantics

LifecycleTransitionRecord
  transition_id, resource_ref, profile/version
  prior/new state, intent/transaction/event refs
  effective/known/recorded times, guard receipt refs[], actor

IntentRelationship
  relationship_id, parent/child intent refs
  relationship = DECOMPOSES_TO | DEPENDS_ON | BLOCKS | SUPERSEDES |
                 CORRECTS | REPAIRS | CAUSED_BY | ANALYZES
  input/output contract digest, lifecycle propagation policy
  cancellation/failure/completion propagation policy

IntentResult
  result_id, intent/transaction/workflow/node refs
  result_kind, current revision ref
  COMPLETE | PARTIAL | UNKNOWN | AMBIGUOUS | STALE | REDACTED |
  UNAVAILABLE | FAILED
  subject/population snapshot refs, closure linkage

IntentResultRevision
  revision_id, result/parent revision
  typed payload or artifact/schema ref
  authority/source/control/rule/schema snapshot refs
  effective/known/recorded/observed times
  provenance/explanation/quality/limitation refs
  valid-until/revalidation/result-authority
  correction/supersession refs, canonical digest
```

## Workflow definition and runtime

```text
CapabilityDefinition
  capability_id, canonical name/owner plane-domain-service
  purpose/description/risk/maturity/lifecycle
  supported subject/resource kinds, discovery metadata

CapabilityVersion
  version_id, capability/parent version
  request/result/error schema refs
  read/write/effect/obligation manifest refs
  AuthZ/legal/privacy/entitlement/DLP requirements
  authority/idempotency/concurrency/timeout/retry contracts
  observation/reconciliation/repair/cancellation contracts
  implementation adapter refs, effective interval/publication/digest

CapabilityInvocation
  invocation_id, version/intent/workflow/node/principal refs
  exact request artifact/digest and context/governance snapshots
  idempotency/resource-ordering/fence/deadline
  transaction/effect/result/attempt/observation/repair refs
  started/completed times, status/ambiguity

CapabilityImplementationBinding
  binding_id, capability version/authority scope
  NATIVE_DOMAIN | CONNECTOR | HUMAN_WORK | CALCULATION | AGENT
  implementation/connection/workflow refs
  routing/eligibility/health/cutover/fallback/fence policy

WorkflowDefinition
  workflow_id, version, name, owner
  input/output/variable schema refs
  node/edge definitions, risk/scope
  failure/cancellation/migration/retention policies
  compilation dependencies[]

WorkflowVersion
  version_id, workflow definition/version/parent
  node/edge/schema/policy refs, source artifact/digest
  DRAFT | VALIDATED | PUBLISHED | QUARANTINED | SUPERSEDED | RETIRED
  migration/compatibility/publication/signature refs

NodeDefinition
  node_id, workflow version, primitive type
  input/output schema/mappings, capability/rule/agent/task refs
  side-effect/read/write/obligation declarations
  timeout/retry/failure/compensation/safe-point policies

EdgeDefinition
  edge_id, workflow version, source/target nodes
  route condition/priority/default/error semantics
  join/loop/cardinality/back-edge policy

CompiledWorkflow
  definition_ref, compiler/build fingerprint
  compiled_plan_hash, dependency/schema/capability snapshot digests
  type/effect/write-set/obligation proofs
  publication state, signature, published_at

TypedReadManifest
  manifest_id, subject/resource selectors[]
  field paths[], query/effective/known-at semantics
  expected authority/freshness/completeness, data-purpose
  schema versions, canonical digest

TypedWriteManifest
  manifest_id, aggregate/stream/resource targets[]
  field paths and mutation operators[]
  expected revisions/stream heads, effective intervals
  invariant/uniqueness/fence requirements[], canonical digest

EffectManifest
  manifest_id, internal/external/human/system effects[]
  side-effect class, ordering/dependency graph
  idempotency/reversibility/observation/repair contracts
  canonical digest

ConflictManifest
  manifest_id, resource/field/effective-range conflict keys[]
  conflict classes, compatibility rules, detected conflicts[]
  resolution/override policy, canonical digest

SimulationManifest
  simulation_id, proposal/compiled-workflow refs
  read/write/effect/conflict/reservation/obligation manifest refs
  pinned authority/control/rule/schema/reference/calculation versions[]
  assumptions/unknowns/warnings/costs/risks[]
  result artifact refs[], deterministic digest, simulated_at

WorkflowInstance
  instance_id, definition/compiled refs
  intent/root/parent refs
  subjects[], current_nodes[], execution_mode
  workflow status dimension ref, business/external-consistency/
  reconciliation/obligation/operational dimension refs[]
  context refs[], last_checkpoint_ref
  placement epoch, version/fence, created/started/completed times

NodeExecution
  execution_id, instance/node/attempt
  tenant/cell/placement/org
  definition/schema/context/governance digests
  authority/purpose/classification/data-access refs
  effective interval, known-at, policy-as-of
  lease/fence, causal predecessors, correlation
  input/output digests/refs, status/times/error/retry
  capability/decision/task/agent/artifact/effect refs[]

ExecutionAttempt
  attempt_id, node execution/attempt number
  lease/fence/worker/runtime refs, input digest
  started/deadline/completed times, timeout/cancellation state
  request/response/error/ambiguity/retry-budget refs
  effects/observations/output digest/status

WorkflowTimer
  timer_id, instance/node
  deadline/time/calendar/tzdb refs
  wake_at, trusted-clock epoch/uncertainty
  dedupe/firing count, status, lease/fence

SignalSubscription
  subscription_id, instance/node
  tenant/cell/placement/org
  event/schema/correlation filter
  source/trust/receipt requirements
  dedupe/ordering/late/timeout policies
  status, accepted_signal_ref?

WorkflowSignal
  signal_id, subscription_ref
  source kind, integration/message/task/document receipt ref?
  normalized payload/taint/digest
  occurred/effective/received/recorded times
  ordering/dedupe decision, acceptance status

SignalReceipt
  receipt_id, signal/source/transport
  authentication/signature/schema/taint/replay/skew results
  raw artifact/digest, received/trusted times, status

SignalDispatch
  dispatch_id, signal/matched subscription refs[]
  matching version, accepted/rejected/late/duplicate/unauthorized results
  quarantine/dead-letter/continuation refs, dispatched_at

ExecutionLease
  lease_id, execution_ref, worker_identity_ref
  fencing_token, issued/heartbeat/expires times, status

Checkpoint
  checkpoint_id, instance/node/version
  state/context/control/legal/authority snapshots
  timer/clock epoch, effect inventory, canonical digest
  active frontier, variable revision refs[]
  pending task/timer/signal refs[], child workflow refs[]

WorkflowVariableRevision
  revision_id, instance/variable path/schema
  typed value/artifact ref, producer node/causal refs
  revision/CAS/digest, created-at, correction/supersession

WorkflowIntervention
  intervention_id, instance/version/node scope
  PAUSE | RESUME | CANCEL | RETRY | SKIP | SATISFY | OVERRIDE |
  REWIND | COMPENSATE | SUPERSEDE | MIGRATE | QUARANTINE
  requester/reason/authority/approval refs
  requested safe point/effect inventory/current fence
  validation/impact/result/new fence/repair refs, lifecycle

WorkflowMigrationPlan
  plan_id, source/target workflow versions
  eligible instance/population snapshot, node/variable/task/timer mappings
  semantic compatibility/effect/safe-point/rollback checks
  dry-run/results/approval/batch/repair refs, status

WorkflowReplayRun
  replay_id, historical instance/compiled plan/runtime versions
  mode = REPLAY | SIMULATE | SHADOW
  frozen inputs/events/context/stubbed side effects
  deterministic outputs/differences/unknowns, digest/status

ChildWorkflowLink
  link_id, parent instance/node/child instance
  input/output contract, cancellation/failure/completion propagation
  join policy, child outcome summary, status

WorkflowEvent
  event_id, instance/node/attempt/intervention refs
  event type/schema/payload digest, causal predecessor refs
  effective/occurred/recorded times, actor/source

WorkflowStatusDimensions
  dimensions_id, instance/revision
  runtime = CREATED | RUNNING | WAITING | PAUSE_REQUESTED | PAUSED |
    BLOCKED | CANCELLING | CANCELLED | COMPLETED | REPAIR_REQUIRED |
    QUARANTINED | SUPERSEDED
  business = NOT_STARTED | IN_PROGRESS | COMPLETED | CANCELLED |
    CORRECTION_REQUIRED | UNKNOWN
  external consistency = NOT_REQUIRED | PENDING | CONSISTENT | DEGRADED |
    INCONSISTENT | UNKNOWN
  reconciliation = NOT_STARTED | PENDING | PASSED | FAILED |
    REPAIR_REQUIRED | WAIVED | UNKNOWN
  obligations = NOT_EVALUATED | PENDING | SATISFIED | OVERDUE |
    WAIVED | DISPUTED | UNKNOWN
  operational = HEALTHY | DEGRADED | INCIDENT | QUARANTINED
  transition reason/evidence/effective/recorded times, digest

WorkflowContextBinding
  binding_id, instance/node, context_type
  context_ref, schema/version/digest
  resolved_at, valid_until, refresh/revalidation policy
  permitted field projection, classification/purpose limits
```

## Human work and approval

```text
WorkQueue
  queue_id, scope, purpose, classification/compartment
  membership/assignment/availability policies
  priority/fairness/capacity/SLA policies
  lifecycle, version

WorkItem
  work_item_id, workflow/node/requirement/proposal refs
  task_type, subject refs[], queue_ref?
  eligible candidate/assignment snapshots
  delivery recipient, assignee, claim holder
  input/form/output schema refs
  classification/visibility/purpose
  operational SLA and legal deadline refs
  priority/aging, status, revision

WorkAssignmentDecision
  decision_id, work-item revision/queue
  candidate/availability/workload/skill/authority snapshots
  assignment policy/fairness/version, selected assignee
  exclusions/unknowns/rationale/validity/digest

WorkAssignmentEvent
  event_id, work-item revision
  ASSIGN | CLAIM | RELEASE | REASSIGN | DELEGATE | ESCALATE | RETURN
  from/to principal-or-queue, authority/fence/reason
  effective/recorded times, notification refs

WorkDependency
  dependency_id, predecessor/successor work-item refs
  required outcome, blocking/soft relation
  satisfaction/waiver/override refs, status

SLAClock
  clock_id, work-item/obligation/case/workflow refs
  clock type, calendar/timezone/version
  started/paused/resumed/due/breached/completed times
  pause/tolling rules, source trigger, status

DeadlineEvaluation
  evaluation_id, SLA clock/as-of
  elapsed/remaining business time, calendar/rule versions
  ON_TRACK | AT_RISK | BREACHED | TOLLED | UNKNOWN
  escalation/notification/obligation refs

WorkItemSet
  set_id, purpose/population snapshot/query digest
  member work-item refs[], batch/fairness/priority policy
  aggregate completion/cancellation status

ClaimLease
  lease_id, work_item/version, holder
  fencing_token, issued/renewed/expires times, clock epoch

WorkHandoff
  handoff_id, work_item/version
  from/to principals, actor, reason
  authority/delegation/SoD snapshots
  effective_at, old fence revocation, notification refs

WorkCompletion
  completion_id, work_item/version
  principal/session/delegation/authority refs
  output artifact/digest, validation receipt
  semantic completion identity, completed_at
  emitted_signal_ref, duplicate_of?

WorkCompletionRevision
  revision_id, completion/parent revision
  exact output/evidence/validation digest
  accepted/rejected/corrected/invalidation state
  actor/authority/effective/recorded times

ApprovalRequirement
  requirement_id, revision, proposal_digest
  resolver AST and policy digests
  cardinality, quorum denominator/count, veto/reject/abstain policies
  resolution-time and candidate-churn policies
  scope/effective-time/deadline/validity
  delegation/SoD/step-up/reason policies

ApprovalResolutionSnapshot
  requirement_ref, candidate principals/identity-equivalence sets
  person/worker/employment/assignment refs
  resolver paths/authority/effective intervals
  graph/group/role watermarks
  included/excluded/ambiguity reasons, candidate digest

ApprovalPresentationReceipt
  receipt_id, requirement/proposal/candidate digests
  principal/session, displayed_at, expires_at
  rendered summary/full-diff/visible projection digests
  shown/hidden field manifest, decision-safety result
  sources/freshness/uncertainty/warnings/effects/cost
  locale/translation/accessibility versions

ApprovalDecision
  decision_id, requirement/proposal refs
  principal/session/delegation/authority refs
  presentation_receipt_ref/digest
  decision kind/reason/input digest
  decided_at, valid_until, idempotency identity

ApprovalVoteRevision
  revision_id, decision/requirement/principal
  APPROVE | REJECT | ABSTAIN | REQUEST_INFO | WITHDRAW
  structured reason/evidence/form refs
  exact proposal/presentation digest, session/step-up/offline evidence
  effective/recorded times, supersession/invalidation

ApprovalInvalidation
  invalidation_id, decision/certificate ref
  typed cause/source digest, effective/detected times
  actor, monotonic sequence, resulting disposition

ApprovalCertificate
  certificate_id, proposal/requirement-set digests
  decision refs[], per-requirement counts/denominator
  quorum/veto results, evaluated_at, status, invalidators[]

ApprovalRequirementSet
  set_id, revision, proposal_revision_ref/digest
  requirement_refs[], dependency/ordering graph
  global SoD/distinct-principal constraints[]
  acceptance expression, invalidation/reapproval policy
  evaluated certificate ref?, canonical digest
```

## Governance, legal and policy

```text
Principal
  principal_id, kind = HUMAN | AGENT | SERVICE | INTEGRATION | SUPPORT
  linked person/workload/application refs?
  tenant bindings[], lifecycle, revocation_epoch

AuthoritySource
  authority_source_id, system/service/principal/organization identity
  source role = AUTHORITATIVE_WRITER | AUTHORITATIVE_READER | OBSERVER |
                CLAIMANT | DERIVER | REVIEWER
  tenant/org/domain/entity/property/operation scopes
  connection/environment, trust/assurance profile
  epoch/lifecycle/effective interval/handoff refs

AuthorityBinding
  binding_id, authority source/entity/property/operation scope
  precedence/composition/writer/observation/correction rights
  effective/recorded interval, epoch/fence/external version
  conflict/handoff/expiry/status

AuthenticationSession
  session_id, principal_ref, issuer/audience
  identity/authentication assurance, methods[]
  authenticated_at, idle/absolute expiry
  device/risk refs, revocation epoch, status

DelegationGrant
  grant_id, delegator/delegate, delegation root/chain
  tenant/org/capability/resource/field/purpose scope
  effective interval, transitive/depth policy
  source authority, revocation, status

AuthorizationDecision
  decision_id, principal/session/workload/delegation refs
  tenant/cell/placement/org/population
  capability/resource/fields/effective interval
  purpose/channel/risk/current+proposed scopes
  policy/graph/source watermarks
  ALLOW | DENY | RESTRICT | UNKNOWN_FAIL_CLOSED
  restrictions/obligations/invalidators/max-age/expiry
  input/result digests

DataAccessManifest
  manifest_id, fields/classifications/compartments
  purpose/lawful-authority refs
  recipients/destinations/regions
  masks/transforms/minimization decision
  derived-data/retention/hold/copy-inventory refs
  egress decision/expiry/digest

JurisdictionResolution
  resolution_id, action/subject/domain
  assertions/candidates/selected/rejected authorities
  composition profile/trace, unresolved facts/conflicts
  status, resolver version, correction/supersession

RulePack
  pack_id, jurisdiction, authority, domain
  effective interval, rules[], citations/source refs[]
  version/status/interpretation class
  validation/counsel/publication refs

RuleEvaluation
  evaluation_id, rule/pack/set digests
  subject/input/context/jurisdiction refs
  evaluated/effective/known/policy-as-of times
  evaluator version, status, effects/obligations/results, trace

EvaluationResultEnvelope
  result_id, evaluation kind = LEGAL | REGULATORY | CUSTOMER_POLICY |
    CBA | CONTRACT | ELIGIBILITY | CALCULATION
  evaluator/version/input snapshot/subject/context refs
  PASS | FAIL | PARTIAL | UNKNOWN | NOT_APPLICABLE
  typed outputs/restrictions/obligations/warnings
  trace/citations/override/composition refs, valid-until/digest

EvaluationComposition
  composition_id, subject/rule-family/context
  ordered result envelope refs[]
  ADDITIVE | MOST_PROTECTIVE | MOST_RESTRICTIVE | PRECEDENCE |
  CONCURRENT | OFFSET | STACK | EXCLUSIVE | CUSTOM
  conflicts/unknowns/overrides/intermediate-final results/trace

Obligation
  obligation_id, type, authority, subject/trigger
  required action, responsible party
  deadline/evidence/satisfaction refs
  scope/risk/penalty, rule/version, status

ObligationBinding
  binding_id, obligation/workflow/effect refs
  NODE | GUARD | FIELD_MASK | DESTINATION_GATE | TASK | TIMER | CHILD_INTENT
  bound target, dependencies, revalidation, non-removable

EvidenceRequirement
  requirement_id, obligation/type/schema/required facts
  acceptable issuers/authorities, subject/as-of/freshness
  signature/attestation, classification/retention/hold/purpose/privilege

EvidenceSatisfaction
  satisfaction_id, requirement/artifact refs
  canonical digest, issuer/source/times
  verification method, schema/rule versions
  redaction/minimization, validity, verifier authority

EvidenceArtifact
  artifact_id, subject/issuer/holder refs
  evidence_type, content/artifact ref, canonical digest
  assertion schema/version, issued/effective/received/recorded times
  valid interval, revocation/supersession refs
  signature/attestation/verification refs
  classification/compartment/purpose/retention/hold

EvidenceInvalidation
  invalidation_id, artifact/satisfaction refs
  cause, source evidence, effective/detected times
  invalidated facts/uses[], replacement evidence ref?, actor

EvidenceManifest
  manifest_id, subject/transaction/filing/case refs
  requirement/satisfaction/artifact refs[]
  missing/expired/disputed/waived refs[]
  completeness decision, evaluated_at, canonical digest

GovernanceDecisionBundle
  bundle_id, intent/proposal/execution refs
  authentication/authz/legal/privacy/risk/entitlement/DLP decision refs[]
  obligations/restrictions/unknowns[]
  evaluated-at and valid-until bounds, invalidators[]
  combined outcome, combination trace, canonical digest
```

## Ledger, effects, observation and repair

```text
LedgerStream
  stream_id, tenant, aggregate/resource
  current_sequence, last_event_hash, integrity epoch

LedgerEvent
  event_id, stream/sequence, event/schema type
  fact_class = TRANSACTION_FACT | DOMAIN_FACT | EXTERNAL_OBSERVATION |
               CLAIM | CORRECTION
  subject/resource refs, effective/known/recorded times
  payload/artifact digest, actor/transaction/causal refs
  prior hash/event hash/signature epoch

OutboxEffect
  effect_id, transaction/workflow/node
  effect class, resource key, causal predecessor/sequence
  authority/fence/mapping/schema/destination refs
  payload/idempotency/dedupe digests
  status, deadline/retry budget, observation requirement

ExternalOperationAttempt
  attempt_id, effect_ref, lease/fence
  request/response hashes, sent/received times
  provider operation/receipt refs
  result class, retry-after, ambiguity status

Observation
  observation_id, source/connection/source-role
  authority decision/epoch, canonical resource/link assurance
  completeness/requested/provided fields
  values/digest, source version/watermark/read-as-of
  occurred/effective/recorded/received times
  PASS | FAIL | UNKNOWN | PARTIAL, promotion policy

ReconciliationResult
  reconciliation_id, intent/transaction/effect refs
  expected/observed refs and dimensions
  field differences, authority/watermark/freshness
  PASS | FAIL | UNKNOWN | PARTIAL
  repair/incident refs?, verified_at

RepairPlan
  repair_id, diagnosis/causal refs
  affected resources/effects/subjects
  corrective actions/order/idempotency
  authority/approval/SoD/simulation
  reversibility/ambiguity/risk
  execution/observation/verification refs

Incident
  incident_id, class/severity/scope
  detected signals/failures, affected intents/effects/tenants
  owner, containment/recovery refs
  timeline, communication refs, resolution/verification

OutcomeObservation
  outcome_id, originating decision/intent/transaction
  metric/semantic outcome definition
  observation window, subject/population
  value/confidence/evidence, confounders/limitations
  observed_at, disputed/superseded state
```
