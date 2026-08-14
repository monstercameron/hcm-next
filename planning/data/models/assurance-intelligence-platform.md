# Privacy, Records, Analytics, Agents, Operations, Commercial and Tenant Entities

## Privacy, consent and records

```text
ProcessingPurpose
  purpose_id, name/description
  allowed data categories/operations/recipients
  lawful-basis requirements
  retention/transfer/consent policies
  effective interval/version

ProcessingAuthority
  authority_id, subject/controller/purpose
  lawful basis, jurisdiction/rule refs
  scope/fields/operations/recipients
  effective interval/review/withdrawal behavior
  evidence/status

ProcessingDecision
  decision_id, subject/controller/purpose/activity refs
  requested data fields/operations/recipients/destinations
  lawful-basis composition/purpose-compatibility/minimization results
  authority/consent/contract/rule refs, unknowns/restrictions
  ALLOW | DENY | RESTRICT | REVIEW_REQUIRED, valid-until/trace/digest

Consent
  consent_id, data subject/controller/purpose
  notice/version, requested scopes
  GRANTED | REFUSED | WITHDRAWN | EXPIRED | INVALID
  identity/assurance/evidence/channel
  granted/withdrawn/effective times

ConsentPresentation
  presentation_id, subject/representative/notice version
  exact scope/purpose/controller/recipient disclosures
  locale/accessibility/content digest, shown/acknowledged times
  identity/session/channel evidence

ConsentTransition
  transition_id, consent, prior/new state
  grant/refuse/withdraw/expire/invalidate reason
  actor/representative authority, effective/recorded times
  propagation/re-evaluation refs

PrivacyPreference
  preference_id, person/principal
  processing/communication purpose
  opt-in/out/restriction value
  jurisdiction/source/effective interval

ProcessingActivity
  activity_id, controller/processor
  purpose/data subject/data categories
  systems/recipients/subprocessors/regions
  lawful basis/retention/security controls
  DPIA/transfer assessment refs, version/status

ProcessingDataFlow
  flow_id, activity/source/destination system refs
  controller/processor/subprocessor roles
  data categories/purpose/operations/transfer regions
  contracts/safeguards/security/retention refs
  onward-transfer constraints, effective interval/version

DataSubjectRequest
  request_id, subject/requester/representative
  ACCESS | RECTIFY | ERASE | RESTRICT | PORTABILITY | OBJECT | REVIEW
  intake/identity-proof/purpose/scope
  legal deadline/extensions
  item/exception/fulfillment/appeal refs
  status/closure/evidence

DataSubjectRequestVerification
  verification_id, request/subject/requester/representative refs
  identity-proof/representation/assurance/purpose checks
  result/limitations/expiry/attempts/evidence

DataSubjectRequestDecision
  decision_id, request/item refs[]
  jurisdiction/deadline/extension/exception/privilege/hold results
  fulfill/partial/deny/clarify outcome, rationale/citations
  reviewer/authority/appeal/notice refs

DataSubjectFulfillmentPackage
  package_id, request/source-snapshot/copy-inventory refs
  included/excluded/redacted items and reasons
  schema/format/portability, artifact/digest/encryption
  delivery/expiry/receipt/reconciliation/completeness refs

DataSubjectRequestAppeal
  appeal_id, request/decision refs, appellant/representative
  grounds/evidence/deadline/independent reviewer
  affirm/modify/remand outcome, remedy/notice/status

DataSubjectRequestItem
  item_id, request/data asset/consumer
  requested action, authority/exception
  included/excluded/redacted result
  fulfillment/propagation/reconciliation refs

ProcessingRestriction
  restriction_id, subject/data scope/purpose
  prohibited/allowed operations
  reason/authority/effective interval
  propagation/status

DataTransferAssessment
  assessment_id, source/destination regions
  data/purpose/processor/subprocessor scope
  transfer mechanism/safeguards/key location
  risk/approval/effective interval/status

DataTransferMechanism
  mechanism_id, source/destination/controller/processor refs
  legal mechanism/safeguards/contracts/key/data-localization controls
  onward-transfer/subprocessor restrictions, review/expiry/suspension

DataTransferOccurrence
  occurrence_id, assessment/mechanism/activity/effect refs
  actual data fields/categories/subjects/recipient/regions
  model/log/backup destinations, transferred-at, encryption/DLP evidence
  provider acknowledgement/reconciliation/status

DPIA
  dpia_id, processing activity/change
  necessity/proportionality/risk assessment
  controls/residual risk/consultation
  owner/approval/review/status

DPIARiskScenario
  scenario_id, DPIA/activity
  threat/harm/affected subjects/likelihood/severity
  existing/proposed controls, residual risk
  owner/consultation/acceptance/escalation/monitoring refs

PrivacyBreachAssessment
  assessment_id, incident/data subjects/data classes
  awareness time/evidence
  scope/likelihood/severity
  jurisdictions/notification obligations/deadlines
  decisions/filings/communications/status

PrivacyBreachResponse
  response_id, assessment/incident refs
  containment/forensics/copy-inventory/affected-subject actions
  authority/data-subject notification packages/deadlines
  processor coordination/remediation/postmortem refs, status

RecordDeclaration
  declaration_id, entity/artifact/data asset
  record series/class, owner/custodian
  cutoff trigger, retention schedule/version
  hold keys, disposition eligibility/status

RetentionSchedule
  schedule_id, record class/jurisdiction
  trigger, minimum/maximum duration
  disposition method/exceptions
  authority/citation/effective interval/version

RetentionCalculation
  calculation_id, record declaration
  trigger evidence/time, applied schedules
  composition trace, cutoff/disposition date
  uncertainty/correction/status

LegalHold
  hold_id, matter/authority/reason
  scope/match predicates/custodians
  placed/released actors/times
  notification/acknowledgement/propagation/status

HoldScopeSnapshot
  snapshot_id, hold/query/custodian/data-source refs
  matched record/copy/artifact refs, source watermarks
  exclusions/unknowns, immutable digest, captured_at

HoldNotice
  notice_id, hold/custodian/recipient
  exact scope/instructions/content digest
  delivery/acknowledgement/reminder/escalation refs

HoldRelease
  release_id, hold/full-or-partial scope
  release authority/approval/reason, effective time
  retained intersections/conflicts, propagation/verification refs

HoldIntersection
  intersection_id, hold/record/copy
  matched reason/time, active/released state

DataCopyInventory
  inventory_id, canonical asset
  authoritative/derived/search/vector/cache/telemetry/
  backup/export/processor copy refs[]
  owners/locations/retention/ack status

DataCopy
  copy_id, canonical asset/store/processor/region
  type = AUTHORITATIVE | PROJECTION | SEARCH | VECTOR | CACHE |
         TELEMETRY | AGENT_MEMORY | EXPORT | BACKUP | PROVIDER
  field/category scope, encryption/key/retention/hold refs
  lineage/created/last-verified/deletion-capability/restore policy

DeletionPlan
  deletion_id, subject/record/data scope
  authority/retention/hold decisions
  copy inventory and per-target actions
  ordering/retry/deadline/exception/status

DeletionReceipt
  receipt_id, plan/target store or processor
  action/time/method, payload/tombstone result
  verification/acknowledgement/hash
  exception/retry/reconciliation

DeletionTombstone
  tombstone_id, canonical subject/asset/target refs
  deletion/anonymization/restriction decision
  non-sensitive identifier/digest, deleted-at
  restore-resurrection prevention epoch, retention/expiry

AnonymizationAssessment
  assessment_id, source/derived asset/population
  threat model/quasi-identifiers/linkage risks
  transformation/generalization/suppression/noise methods
  re-identification test/results/limitations, reviewer/version/status

PrivacyControlEvaluation
  evaluation_id, control/activity/system/data-flow refs
  design/operating effectiveness, input evidence/watermark
  PASS | FAIL | PARTIAL | UNKNOWN, deficiency/remediation/owner/deadline

DestructionCertificate
  certificate_id, plan
  target receipts[], exceptions[]
  copy-inventory watermark
  signer/hash/generated time
```

## Provenance, quality, invariants and audit

```text
DataAsset
  asset_id, kind/location/owner
  schema/classification/purpose
  authoritative/reconstructable status
  retention/hold/copy inventory refs

ProvenanceNode
  node_id, entity/artifact/value/decision/effect ref
  type, schema/digest, authority/time/classification

ProvenanceEdge
  edge_id, from/to nodes
  ASSERTED_BY | DERIVED_FROM | OBSERVED_FROM | DECIDED_BY |
  PRODUCED_BY | CORRECTS | SUPERSEDES | SENT_TO
  field paths/transformation/confidence/time

LineageQueryResult
  result_id, query/scope/as-of
  node/edge refs, completeness
  COMPLETE | PARTIAL | UNKNOWN | STALE | REDACTED | UNAVAILABLE
  missing/restricted reasons

DataQualityRule
  rule_id/version/domain/owner
  scope/fields/predicate/severity
  freshness/completeness/plausibility policy
  remediation/SLA/effective interval

DataQualityEvaluation
  evaluation_id, rule/resource/population
  input watermark, result/score
  violation refs[], unknowns/trace/time

DataQualityViolation
  violation_id, evaluation/resource/fields
  expected/observed/severity
  owner/status/remediation/correction refs

InvariantDefinition
  invariant_id/version/scope
  predicate/dependencies/severity
  continuous/boundary evaluation policy
  repair/incident route

InvariantViolation
  violation_id, invariant/resource
  evidence/input watermark/detected time
  risk/status/repair/incident refs

AuditEvidencePackage
  package_id, purpose/requester/recipient
  tenant/org/subject/time scope
  source watermarks/manifest/items[]
  redactions/exclusions/completeness
  retention/hold/DLP/egress refs
  signature/hash/delivery/status

IntegrityEpoch
  epoch_id, ledger/shard/cell
  event range/root hash
  signing key/algorithm/time
  WORM/snapshot/verification refs
```

## Reporting, metrics and analytics

```text
ReportDefinition
  report_id/version/owner
  purpose/input semantic model
  dimensions/measures/filters/parameters
  row/field/population security
  freshness/format/retention/publication

ReportRun
  run_id, definition/parameters
  requester/purpose/AuthZ snapshot
  source watermarks/as-of
  result artifact/dataset refs
  status/quality/completeness/cost
  definition/query/population digests, execution mode
  effective/known-at context, source watermarks by source
  privacy/disclosure/quality gates, correction/supersession refs

ReportSchedule
  schedule_id, report/parameters
  trigger/calendar/timezone
  recipient/delivery/export policy
  lifecycle/next run
  tzdb/business-calendar version, misfire/backfill/overlap policies
  parameter/recipient re-resolution, last attempt/success
  pause/quarantine/failure/escalation state

ReportExport
  export_id, report run/artifact/recipient/destination
  format/classification/DLP/egress/delivery requirement
  expiry/revocation/access/download/signature refs, status

MetricDefinition
  metric_id/version/name
  semantic formula/numerator/denominator
  grain/dimensions/time semantics
  inclusion/exclusion/population rules
  data quality/privacy thresholds
  owner/approval/effective interval

MetricObservation
  observation_id, metric
  population/scope/window/as-of
  value/unit/confidence
  source watermarks/quality/limitations
  calculated_at/version
  definition/calculation/population digests, numerator/denominator/sample size
  lineage/quality/suppression/uncertainty/statistical refs
  correction/supersession refs

MetricCorrection
  correction_id, observation/reason/affected periods-populations
  original/corrected values, authority
  downstream invalidations/publication/supersession refs

CalculationSpecification
  calculation_id, version, typed formula AST
  numerator/denominator/aggregation/null/missing/zero policies
  unit/currency/rounding/calendar/statistical/baseline semantics
  approval/effective interval/digest

SemanticQuery
  query_id, version, requester/purpose/domain
  dimensions/measures/filters/joins/population refs
  effective/known-at/authority/privacy context
  output schema/compiled plan/digest/status

DatasetDefinition
  dataset_id/version/owner/purpose
  schema/semantic mappings
  source/refresh/partition/security policies
  retention/lifecycle

DatasetSnapshot
  snapshot_id, definition
  source watermarks/population
  as-of/generated times
  row count/schema digest/quality
  storage/artifact/expiry
  state = COMPLETE | PARTIAL | STALE | LATE_DATA | WATERMARK_UNKNOWN |
          RESTRICTED | FAILED

DatasetDependency
  dependency_id, dataset/upstream/field mapping/transform refs
  authority/freshness/failure policy/effective interval

DatasetRefreshRun
  run_id, dataset/trigger/input watermarks by source and partition
  partitions/counts/late/duplicate/rejected rows
  quality gate/cost/capacity/error/DLQ refs, status

DatasetLineage
  lineage_id, snapshot/source/transformation refs
  field mappings/join keys/redactions/quality decisions
  completeness/authority/digest

DashboardDefinition
  dashboard_id/version/owner
  widget/report/metric refs
  parameters/security/refresh
  locale/publication/lifecycle
  pinned widget metric/query versions, cross-widget temporal/population policy
  freshness/cache/security/export/degraded-display policies

DashboardSnapshot
  snapshot_id, dashboard/widget result refs
  source watermarks/generated-at/freshness
  privacy/suppression/temporal consistency state

AnalysisPlan
  analysis_id, analytical intent
  question/purpose/population
  metric/dataset/query/tool refs
  privacy/authorization constraints
  methods/tests/limitations/cost/status

AnalysisResult
  result_id, plan
  results/tables/visualization artifacts
  evidence/citations/source watermarks
  confidence/limitations/quality
  generated/reviewed/expiry

CohortDefinition
  cohort_id/version/purpose
  population expression
  inclusion/exclusion/as-of
  privacy minimum-size/re-identification rules
  dynamic-or-frozen, membership version/evaluation time
  inclusion/exclusion reason schemas, snapshot ref

PopulationSnapshot
  snapshot_id, cohort/source refs, secure membership digest/count
  inclusion/exclusion counts and reason summaries
  effective/known-at/source watermarks/authority
  privacy/suppression/classification state

DomainAlignmentPlan
  plan_id, domain/source refs[]
  authority per field, identity/join versions
  effective/known-at/precedence/conflict/missing-source policies
  currency/unit/locale normalization, privacy intersection

DisclosureControlPolicy
  policy_id, purpose/data class/population
  minimum cell/complementary suppression/rounding/noise rules
  query budget/repeated-query/differencing controls
  sensitive dimensions/reviewer requirements/version

DisclosureReview
  review_id, query/result/requester/purpose/population
  risk/suppressed cells-fields/transformations
  reviewer/approval/expiry/evidence

PopulationComparison
  comparison_id, left/right population snapshot refs
  alignment/overlap/denominator/privacy policies
  statistical test/effect size/confidence/limitations

DecisionOutcomeLink
  link_id, decision/outcome refs, outcome definition/window
  attribution method/confounders/censoring/source authority
  descriptive | associational | predictive | causal class/confidence

MetricExplanation
  explanation_id, metric observation/definition/calculation refs
  formula/trace/population/source authority/watermarks
  temporal context/numerator/denominator/filters
  quality/privacy/correction/citation/audience refs, expiry

TrendAnalysis
  analysis result specialization with time series, change points and comparability refs

ProcessMiningModel
  model_id/version/workflow/event scope
  event semantics/variants/bottlenecks
  conformance/deviation/privacy refs

Forecast
  forecast_id, target/population/horizon
  model/version/training snapshot
  predicted values/intervals/scenarios
  assumptions/error metrics/expiry
```

## Semantic knowledge and agents

```text
OntologyConcept
  concept_id/version/domain
  canonical name/definitions/aliases
  parent/relationship refs
  effective interval/status

SemanticMapping
  mapping_id, source schema/field/value
  target concept/property
  transform/confidence/approval/version

KnowledgeSource
  source_id, owner/purpose
  policy/handbook/contract/case/data source type
  authority/classification/retention
  ingestion/freshness/status

KnowledgeDocument
  document_id/version/source
  artifact/content hash
  effective interval/approval/authority
  classification/visibility/freshness

KnowledgeDocumentRevision
  revision_id, document/source version/content digest
  effective/known/received times, locale
  approval/publication/supersedes/refutes refs
  classification/retention/hold/status

KnowledgeIngestionJob
  job_id, source/document/connector refs
  parser/OCR/malware/classification/dedup versions/results
  source artifact/hash, extracted revision/chunk refs, status/errors

ChunkingProfile
  profile_id, version, parser/chunker/boundary/overlap rules
  section/path metadata schema, locale/content-type scope

EmbeddingProfile
  profile_id, provider/model/version/dimensions
  distance/normalization, region/residency/no-training/retention
  classification/purpose eligibility, lifecycle

SemanticIndex
  index_id, version, tenant/cell/region
  embedding/chunking profiles, source watermark
  ACL/filtering strategy, build/rebuild/migration/status

RetrievalExecution
  retrieval_id, query/plan/index version
  candidate/authorized/ranked/discarded chunk refs and reasons
  freshness/authority/taint checks, reranker/version/result digest

KnowledgeChunk
  chunk_id, document/version/offset
  text or artifact ref/digest
  concepts/embedding refs
  classification/purpose/taint

SemanticObservation
  observation_id, subject/concept
  assertion/assessment content
  kind = FACT | CLAIM | OPINION | ASSESSMENT | INFERENCE |
         MODEL_OUTPUT | HUMAN_FEEDBACK
  source principal/system/authority, intended/prohibited uses
  evidence/confidence/uncertainty
  effective/as-of/expiry
  classification/status/supersession

TrustLabel
  label_id, source ref
  CANONICAL_FACT | APPROVED_POLICY | AUTHORIZED_HUMAN_ASSERTION |
  EXTERNAL_ASSERTION | UNTRUSTED_DOCUMENT | UNTRUSTED_WEB_CONTENT |
  MODEL_DERIVED | TOOL_DERIVED | UNKNOWN
  authority/purpose/classification/confidence/effective interval
  propagation/prohibited-transition policy

InputTrustAssessment
  assessment_id, input/trust label
  injection/taint indicators, detector/version
  allow/sanitize/quarantine/reject decision, human review/evidence

InstructionBoundary
  boundary_id, agent execution
  system/developer/user/retrieved/tool-output segments and digests
  trust labels/instruction eligibility, policy/version

TaintPropagationDecision
  decision_id, source labels/operation/result label
  prohibited transitions/sanitization/policy/version/trace

Hypothesis
  hypothesis_id, question/claim
  subject/population/scope
  supporting/refuting evidence
  method/confidence/status/expiry
  null/alternative hypotheses, cohort/window/confounders
  statistical method/tests/intervals/limitations/reproducibility refs

Prediction
  prediction_id, subject/population/outcome
  model/version/input snapshot
  predicted value/probability/interval
  explanation/limitations/valid-until
  later outcome/evaluation refs

PredictionModel
  model_id, version/provider/training snapshot/feature schema
  model card/permitted-prohibited purposes/jurisdiction eligibility
  fairness/robustness/calibration/drift thresholds/lifecycle

PredictionExecution
  execution_id, model/input snapshot/feature digest
  output/probability/interval/explanation/uncertainty
  policy/human-review/validity refs

PredictionEvaluation
  evaluation_id, prediction/outcome/label window
  calibration/error/subgroup/drift results
  remediation/retirement/status

Recommendation
  recommendation_id, subject/purpose/type
  proposed action/options
  evidence/rationale/confidence/risks
  agent/model/version
  human decision/outcome refs, expiry
  constraints/prohibited actions/required human role
  accepted/rejected/deferred/expired decision and reason
  resulting intent/proposal/workflow refs

WorkforceQuestion
  question_id, requester/purpose/scope/input
  normalized question/subject/population/effective/known-at
  classification/status

AnalysisExecution
  execution_id, plan/version/input watermarks/snapshots
  query/retrieval/tool steps, results/quality/warnings
  model/provider refs, status

AnswerPackage
  answer_id, question/plan/execution refs
  answer/result artifact refs, citation refs[]
  confidence/uncertainty/limitations/freshness
  authorization/purpose proof, generated-by/expiry/supersession

Citation
  citation_id, source/document/chunk/artifact
  exact page/path/offset/span digest
  source authority/effective interval/retrieval rank/validity

AgentDefinition
  agent_id/version/purpose/owner
  allowed capabilities/tools/data classes
  model/provider eligibility
  prompt/policy/memory/budget refs
  risk/write authority/lifecycle

ModelProvider
  provider_id, legal entity/regions/subprocessors
  retention/no-training/security/residency/health
  allowed classifications/lifecycle

ModelDefinition
  model_id, provider/version/modalities/context limits
  capabilities/quality/safety limits/regions/cost/lifecycle

ModelEligibilityPolicy
  policy_id, model/provider
  data classifications/jurisdictions/purposes/agent/risk classes
  approvals/fallback/retention/region constraints, version

PromptDefinition
  prompt_id, version, system/developer template refs
  input/output schemas/policy/injection defenses
  evaluation/publication/status/digest

ToolDefinition
  tool_id, version/capability/argument-result schemas
  side-effect class/AuthZ/approval/confirmation/rate/cost/egress rules
  lifecycle/status

AgentEvaluationSuite
  suite_id, version, safety/correctness/privacy/adversarial cases
  golden/forbidden outputs, thresholds/publication

AgentEvaluationRun
  run_id, suite/agent/model/prompt/tool versions/dataset snapshot
  results/failures/regression/promotion/rollback refs

AgentExecution
  execution_id, agent/intent/workflow/node
  principal/delegation/context refs
  model/provider/prompt/tool versions
  input/output/taint/provenance digests
  tool invocation refs[], validations[]
  cost/tokens/times/status/incident refs

AgentMemory
  memory_id, agent/scope/subject?
  content/artifact ref, provenance/taint
  visibility/purpose/classification
  created/expires/supersedes/deletion refs
  kind = EPISODIC | SEMANTIC | WORKING | PROCEDURAL | USER_PREFERENCE
  source authority/consent/legal basis/write/retrieval policies

AgentMemoryOperation
  operation_id, memory/agent execution/principal
  WRITE | READ | RECALL | FORGET | CORRECT
  purpose/access decision/input-output digests/use evidence/status

ToolInvocation
  invocation_id, agent execution
  server nonce/capability/version
  exact argument/provenance/data manifest
  authority/risk/effect/budget decisions
  output/taint/validation/status
  argument schema/authz/purpose/risk/confirmation/idempotency gates
  egress/rate/budget/input-output trust/side-effect receipt/ambiguity

ToolValidationResult
  validation_id, invocation/arguments/tool version
  schema/semantic/purpose/authz/risk/DLP checks
  PASS | FAIL | PARTIAL | UNKNOWN, findings/digest

OutputValidationResult
  validation_id, invocation/output
  schema/type/business/invariant/taint/DLP checks
  accept/reject/quarantine/human-review result

AgentKillSwitch
  switch_id, target agent/model/tool/capability/tenant scope
  reason/evidence/authority/fail behavior
  activated/expires/released times/status

WorkflowDraft
  draft_id, originating intent/agent execution
  generated graph/schema/capability/read-write-effect manifests
  trust/legal/authz/side-effect/simulation/test refs
  human publication decision/status/digest

WorkflowTestScenario
  scenario_id, workflow draft/version
  synthetic fixtures/expected decisions/effects/forbidden effects
  failure/fault/authz/legal cases, replay seed/result

AIIncident
  incident_id, agent/model/provider/tool
  type = INJECTION | EXFILTRATION | TOOL_MISUSE | REGRESSION |
         BIAS | DATA_LEAK | UNEXPECTED_AGENCY
  affected scope/executions/effects
  containment/kill-switch/recovery/notice refs
```

## Operations, recovery and platform assurance

```text
ReconciliationPolicy
  policy_id/version/domain
  expected/observed dimensions
  authority/freshness/tolerance
  mismatch/unknown/repair routes

RepairAction
  action_id, repair plan
  capability/effect target
  expected version/idempotency/order
  reversibility/authority/status/result

InterventionPreview
  preview_id, target workflow/capability/workload
  current state/version/safe point
  affected subjects/children/reservations/effects
  irreversible/ambiguous inventory
  predicted completion states
  required authority/SoD/step-up
  expected post-state/repair route/digest/expiry

InterventionCommand
  command_id, preview/state CAS
  actor/session/JIT/support refs
  action/scope/reason/confirmation
  authority/approval/expiry/status/result

SLODefinition
  slo_id/version/service/capability/workflow class
  availability/latency/freshness/reconciliation/RPO/RTO objectives
  measurement windows/budgets/owner

SLOObservation
  observation_id, SLO/window/scope
  good/total events, latency/freshness
  error-budget state, source quality

WorkloadContext
  context_id, tenant/cell/placement
  criticality/admission class
  queue/node/workflow/external deadlines
  attempt/time/cost retry budget
  fanout/bytes/cost/dependency limits
  degradation policy

AdmissionDecision
  decision_id, workload/context/resource state
  ADMIT | DEFER | SHED | DEGRADE | REJECT
  reason/retry-after/reservation/evidence

QuarantinedWork
  quarantine_id, work/effect/event ref
  cause/risk/tenant/criticality
  original identity/payload digest
  owner/review/redrive/expiry/status

RecoveryPlan
  recovery_id, incident/failure scenario
  target systems/data/cell/region
  source backup/snapshot/watermark
  RPO/RTO, validation/tests, owner/status

BackupArtifact
  backup_id, plane/store/tenant scope
  point-in-time/watermark
  encrypted/immutable/isolated location
  key/config/schema refs
  created/verified/expiry/status

RestoreTest
  test_id, backup/recovery plan
  isolated environment, restored watermark
  integrity/domain/workflow checks
  RTO/RPO actuals, failures/status

TenantCellSummary
  cell_id, region/capacity/isolation boundary
  services/stores/queues/index refs
  lifecycle/health/failure domain

TenantPlacementRecord
  placement_id, tenant/cell/region
  epoch/lease/signature
  residency/SLA/tier/capacity reasons
  effective interval/status

TenantRelocationSummary
  relocation_id, tenant/source/target cells
  source/target placement epochs
  data/work/timer/signal/connector migration manifests
  writer fences/cutover/rollback/reconciliation/status

SupportCase
  support_case_id, tenant/requester/problem
  purpose/scope/consent/ticket
  diagnostic/evidence/communication refs
  status/closure

SupportGrant
  grant_id, support case/operator
  customer approver/consent
  tenant/org/field/action/purpose scope
  step-up/session/device binding
  effective interval/revocation/recording/status

FeatureFlag
  flag_id/version/owner
  capability/config behavior
  tenant/org/region/user targeting
  rollout/rollback/kill policy/status

ConfigurationPackageSummary
  package_id/version/environment
  workflow/policy/schema/mapping/reference/agent refs
  dependency graph/digests
  validation/simulation/approval/signature/status

ConfigurationPromotion
  promotion_id, package/source/target
  diff/impact/test/approval refs
  rollout/rollback/status/evidence
```

## Billing, commercial and tenant administration

```text
ProductOffering
  offering_id/version/name
  included capabilities/features/limits
  pricing/eligibility/region terms
  lifecycle/effective interval

BillingAccount
  account_id, tenant/customer/legal entity
  billing/tax addresses, currency/payment terms
  contract/order/subscription/invoice/payment refs, lifecycle

CommercialContract
  contract_id, account/customer/vendor parties
  term/renewal/termination/payment/SLA/data-processing refs
  signed artifact/authority/effective interval/status

CommercialOrder
  order_id, account/contract/offering quantities
  prices/discounts/terms/start-end dates
  approval/signature/provisioning/status

Subscription
  subscription_id, account/order/offering
  quantity/unit/limits/renewal/suspension
  effective interval/entitlement refs/status

CommercialEntitlement
  entitlement_id, tenant/offering/capability
  scope/limits/start/end
  contract/order/source/status

UsageEvent
  usage_id, tenant/account/capability
  semantic unit/quantity/time
  source intent/workflow/effect/agent refs
  idempotency/digest/classification/status
  meter/version/unit, event/effective/received times
  org/principal/plan/region dimensions, watermark
  late-arrival/correction/reversal refs

UsageCorrection
  correction_id, original usage event
  corrected quantity/dimensions/time, reason/authority
  reversal/replacement events/downstream rerating refs

RatedUsage
  rated_id, usage/rate-card version
  quantity/tier/rate/amount/currency
  discounts/taxes/credits, trace

RatingRun
  run_id, account/period/source usage watermark
  rate-card/contract/FX/tax/rounding versions
  input/output control totals, status/correction

RatingTrace
  trace_id, rated usage/rating run
  normalization/tier/minimum/discount/credit/FX/rounding steps
  input/output digests, deterministic formula/version

BillingTaxCalculation
  calculation_id, rated usage/invoice/account
  commercial tax jurisdiction/type/base/rate/amount/currency
  exemption/evidence/rule/provider/rounding/trace refs

RateCard
  rate_card_id/version/offering
  metric/tier/unit prices
  currency/region/customer overrides
  effective interval/status

CustomerBudget
  budget_id, tenant/org/cost center
  period/currency/amount
  reserved/consumed/forecast
  threshold/action policies
  authority/hierarchy/period calendar, committed/reserved/available
  hysteresis/warn/deny/queue actions, FX/version

CommercialBudgetReservation
  reservation_id, budget/usage-or-operation
  amount/currency/fence/expiry
  consumed/released/ambiguous states

CostAllocationRule
  rule_id, source usage/cost scope, target dimensions
  method/weights/residual/rounding/version/effective interval

CostAllocationRun
  run_id, rule/source rated usage snapshot
  allocation lines/control totals/residuals
  approval/accounting/reconciliation/status

UsageForecast
  forecast_id, account/org/offering/horizon
  source watermark/model/version/scenario/assumptions
  quantity ranges/confidence/expiry

CostForecast
  forecast_id, usage forecast/rate-card/contract/FX refs
  amount ranges/currency/assumptions/confidence

Invoice
  invoice_id, billing account/period
  line refs, subtotal/tax/total/currency
  issued/due/status/artifact/payment refs
  revision/parent, source usage/rating watermarks
  billing/legal/tax jurisdictions, correction chain
  DRAFT | FINAL | ISSUED | VOID | PARTIALLY_PAID | PAID | PAST_DUE

InvoiceLine
  line_id, invoice/offering/usage refs
  description/quantity/rate/amount
  tax/discount/credit/adjustment refs

BillingAdjustment
  adjustment_id, account/invoice/line
  CREDIT | REFUND | CHARGE_CORRECTION
  amount/currency/reason
  original charge/evidence/approval/status

BillingDispute
  dispute_id, account/invoice/lines
  reason/evidence/amount
  owner/case/resolution/adjustment/status

CommercialPayment
  payment_id, account/invoice/payer/provider
  amount/currency/method/value date
  instruction/settlement/return/reversal refs/status

PaymentAllocation
  allocation_id, payment/invoice/line refs
  amount/currency/effective time/correction refs

RefundExecution
  refund_id, adjustment/original payment/account
  amount/currency/destination/approval/idempotency
  provider attempts/settlement/reconciliation/status

InvoiceExplanation
  explanation_id, invoice/revision/account
  line-to-usage/rating/tax/discount/credit/contract provenance
  control totals/corrections/limitations/audience/digest

Tenant
  tenant_id, legal/customer identity
  lifecycle/status/tier
  organization root refs
  placement/residency/key/entitlement refs
  default locale/currency/calendar
  administrators/support policy
  master contract/billing/data-processing/SLA refs
  lifecycle transition/hold/retention/closure gates

TenantLifecycleTransition
  transition_id, tenant, prior/new state/reason
  actor/authority/approval/effective/recorded times
  gate/evidence/rollback refs

TenantProvisioningPlan
  plan_id, tenant/order
  cell/region/key/schema/store/bootstrap steps
  default config/org/roles/connectors
  validation/owner/status

TenantProvisioningRun
  run_id, plan/order/target placement
  step/dependency/checkpoint refs, idempotency/fences
  cost/readiness/rollback/evidence/status

TenantProvisioningStep
  step_id, run/type/owner/prerequisites
  expected resource/outputs/lease/retry/DLQ
  checks/compensation/rollback/evidence/status

TenantEnvironment
  environment_id, tenant
  PROD | STAGE | TEST | SANDBOX
  placement/config/data policy
  masked/synthetic/source refs
  lifecycle/reset status
  config/schema/policy epochs, source watermark
  isolation/admin/integration gates/masking profile

SandboxResetRun
  run_id, environment/source snapshot/synthetic scenario
  masking/prohibited-data/reproducibility checks
  reset/restore/verification/approval/status

TenantExitPlan
  exit_id, tenant/contract
  export/hold/retention/connector/key actions
  customer handoff/acknowledgement
  destruction/certification/status

TenantExitRun
  run_id, plan, typed export/shutdown/hold/key/destruction steps
  grace/appeal/checkpoint/owner/evidence/status

TenantExitPackage
  package_id, run/domain/store manifests[]
  schema/portability/artifact/digest/encryption/delivery refs
  exclusions/holds/retention/completeness/customer receipt

TenantClosureCertificate
  certificate_id, exit run
  export/shutdown/processor deletion/key revocation/destruction refs
  unresolved legal retention, signer/digest/closed-at

OrganizationScopeShare
  share_id, provider/consumer org scopes
  resource/capability/field/purpose scope
  effective interval/approval/revocation

PlatformApplication
  application_id, tenant/developer/owner
  OAuth client/API key/cert refs
  redirect/webhook destinations
  scopes/purposes/rate limits
  lifecycle/rotation/revocation

DeveloperCredential
  credential_id, application/type
  key/cert/secret ref (never raw)
  issuer/audience/scopes
  issued/expires/rotated/revoked times
  grant type/PKCE/redirect/audience/mTLS/IP-region policies
  last-used/rotation overlap/webhook signing/owner evidence
```

## Trigger and scheduling entities

```text
TriggerDefinition
  trigger_id/version/type = CRON | BUSINESS_CALENDAR | EVENT | DEADLINE |
                            EFFECTIVE_DATE | THRESHOLD | HEALTH_SIGNAL
  owner/domain/target intent definition
  expression/filter/correlation
  tenant/org/population scope
  timezone/calendar/catch-up/coalescing
  idempotency/rate/criticality/status
  source/schema/target-input versions, authorization/purpose
  overlap/concurrency/misfire/jitter/max-catch-up/debounce policies
  storm/retry/DLQ/outage/quarantine/recursion policies

TriggerSubscription
  subscription_id, trigger/source/partition scope
  cursor/start position/filter/schema version
  generation/lease/fence/status

EventCursor
  cursor_id, subscription/source partition
  last read/matched/committed offsets, watermark
  replay epoch/fence/checkpoint/status

TriggerInbox
  inbox_id, trigger/source event/deadline key
  immutable payload/evidence digest
  ordering/dedupe/schema/auth/taint results
  received/effective/occurred/recorded times/status

TriggerPayload
  payload_id, inbox/cause/source authority
  tenant/cell/affected population snapshot
  dedupe/ordering keys, temporal points
  target intent definition/input schema/typed input artifact
  late/duplicate/replay/unknown policy

TriggerFiring
  firing_id, trigger/version
  scheduled/actual/observed times
  source event/deadline/threshold refs
  tenant/cell/placement
  dedupe/order/admission decisions
  created intent/workflow ref?, status
  deterministic firing key/source partition-offset
  fence/retry/dead-letter/replay provenance
  exact target definition/input snapshot/system principal

TriggerExecutionAttempt
  attempt_id, firing/worker/lease/fence
  admission/priority/retry-budget/recursion depth
  created intent/idempotency/result/error/DLQ refs

ScheduledJob
  job_id, definition/tenant scope
  parameters/source watermark
  priority/deadline/retry/budget
  lease/fence/checkpoint/status/result

ScheduledJobAttempt
  attempt_id, job/schedule occurrence
  worker/lease/fence/heartbeat/idempotency
  started/deadline/completed/next-retry times
  output/error/DLQ/cancel/quarantine refs/status

BusinessDeadlineEvent
  event_id, deadline/obligation
  due state, owner/escalation
  created task/intent/incident refs
  deadline revision/clock-start evidence/timezone/calendar/tzdb
  tolling/extensions/grace/recurrence/dedupe
  DUE | OVERDUE | ESCALATED | SATISFIED | WAIVED | CANCELLED

EffectiveDateActivation
  activation_id, proposed revision/effective interval
  trusted-time/fence/revalidation refs
  transaction/effect/result/status

EffectiveDateActivationAttempt
  attempt_id, activation/source proposal revision
  dependency readiness/revalidation/trusted-time/skew
  idempotency/fence/ambiguous outcome/rollback/repair/status

ThresholdEvent
  event_id, metric/budget/quality/security threshold
  observed value/window/source
  policy/action/created intent refs
  metric/version/source watermark/cohort/window
  direction/hysteresis/debounce/cooldown/repeat/source-quality
  override/action outcome/loop-protection refs

TriggerDeadLetter
  dead_letter_id, firing/inbox/attempt
  poison/error/retry history/payload digest
  owner/review/redrive/expiry/status
```
