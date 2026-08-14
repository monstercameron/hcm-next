# Access, Assets, Documents, Communications and Integration Entities

## Workforce identity and access

```text
WorkforceIdentity
  workforce_identity_id, person/worker refs
  username/subject links[]
  lifecycle/status revisions
  source employment/assignment facts
  authentication/account/entitlement/device refs[]

WorkforceIdentityRevision
  revision_id, workforce identity/person/worker/employment/assignment refs
  namespace/subject/correlation, lifecycle state and reason
  source authority, effective/known/recorded times
  rehire/reinstatement/predecessor refs, correction/supersession

DigitalAccount
  account_id, workforce identity/software application refs
  external object ref, account name
  lifecycle/provisioning state
  created/disabled/deleted times
  source authority/observation refs

SoftwareApplication
  application_id, name/vendor
  environment/tenant connection refs
  data/risk/classification profile
  entitlement/catalog/provisioning refs
  owner/lifecycle

Entitlement
  entitlement_id, application
  code/name/type/description
  privilege/risk/classification
  prerequisite/conflict/SoD refs[]
  lifecycle/effective interval
  resource/action/condition/data/org scope
  bundle parent/child refs, owner/issuer/environment/version

AccessGrant
  grant_id, principal/account/entitlement
  source = BIRTHRIGHT | REQUEST | DELEGATION | EMERGENCY | MANUAL
  scope/purpose, effective interval
  request/approval/policy refs
  provisioning/observation/status
  revision/predecessor refs, proposal/policy/approval digests
  delegation/emergency authority, expiry/review/revocation refs
  desired/observed states, version/fence

AccessRequest
  request_id, requester/beneficiary
  application/entitlement/scope/purpose
  duration/justification/risk
  approval/provisioning/status
  input snapshot, requester/beneficiary separation
  step-up/SoD/re-evaluation/expiry/idempotency refs

EntitlementCalculation
  calculation_id, workforce identity
  source worker/employment/assignment snapshot
  policy/version, expected grants/revocations
  conflicts/unknowns/trace

AccessReviewCampaign
  campaign_id, scope/population snapshot
  application/entitlement/grant filters
  reviewer resolution/rules/deadlines
  item refs[], status/results

AccessReviewItem
  item_id, campaign/grant/beneficiary
  reviewer/authority/presentation refs
  certify/revoke/modify/unknown decision
  reason/evidence/remediation status

AccessObservation
  observation_id, application/account/entitlement
  source/provider version/watermark
  observed state/time/completeness
  expected/reconciliation refs

AccessDrift
  drift_id, expected grant/revocation ref
  observed access ref, difference/risk
  cause/repair/incident/status

PhysicalAccessCredential
  credential_id, workforce identity/facility
  badge/token ref, access-zone grants
  issue/activate/deactivate/return times
  provider/external refs/status

AccessProfile
  profile_id, scope/worker-population expression
  entitlement eligibility/birthright/conditional rules
  prerequisites/conflicts/SoD, effective interval/version

DesiredAccessState
  state_id, workforce identity/as-of
  expected account/grant/revocation refs[]
  policy/input/authority snapshots, SoD/conflict results
  effective interval, completeness/unknowns, digest

AccessPlan
  plan_id, desired state/current observation refs
  ordered identity/account/grant/revoke/session/device/physical effects[]
  dependencies/deadlines/risk/idempotency/fences
  approval/observation/reconciliation/repair refs

AccessTarget
  target_id, application/environment/connector refs
  authoritative namespace, account correlation/matching policy
  provisioning schema/mapping/profile version
  desired-vs-observed authority/takeover/writer-fence policy

AccessOperation
  operation_id, plan/target/account/entitlement refs
  semantic operation, expected prior/new state
  effective time/idempotency/fence/dependency refs
  lifecycle/attempt/observation/reconciliation/repair refs

AccessOperationAttempt
  attempt_id, operation, request/response/receipt refs
  lease/fence, attempted/received times, provider result
  retry/dead-letter/ambiguity disposition

AccessItemObservation
  observation_id, target/account/entitlement/operation refs
  CURRENT | REVISION | UNKNOWN | INCOMPLETE | STALE
  observed state, provider version/watermark/times
  completeness/authority, reconciliation difference ref?

EmergencyAccessSession
  session_id, grant/principal/incident refs
  reason/scope/TTL/device/step-up/approval refs
  monitoring/action event refs[], automatic expiry/revocation
  post-use review/evidence/status

Facility
  facility_id, location/legal entity refs, provider/external refs
  site/zone refs[], timezone/security owner/lifecycle

PhysicalAccessZone
  zone_id, facility/parent zone refs, risk/classification
  eligibility/conflict/escort/schedule rules, lifecycle

PhysicalAccessGrant
  grant_id, workforce identity/credential/zone refs
  profile/policy/approval/source, effective interval
  desired/observed/revocation states, operation refs

PhysicalAccessOperation
  operation_id, grant/credential/provider refs
  issue/activate/change/deactivate/return action
  idempotency/fence/attempt/observation/reconciliation refs
```

## Devices, equipment and physical assets

```text
Asset
  asset_id, asset type/tag/serial token
  model/vendor/ownership
  location/custodian/assignment refs
  lifecycle/condition/security class
  acquisition/warranty/disposition refs

Device
  device_id, asset ref
  hardware/OS/management identifiers
  MDM/provider refs, compliance/attestation
  encryption/lock/wipe states
  account/access associations[]

DeviceIdentity
  identity_id, device/MDM/provider refs
  hardware/attestation/certificate identifiers
  assurance/enrollment/effective interval, revocation/status

DeviceEnrollment
  enrollment_id, device/identity/MDM/account/worker refs
  ownership = COMPANY | BYOD, privacy boundary
  enrollment profile/version, enrolled/unenrolled times
  compliance/wipe/lock authority, status

DeviceComplianceSnapshot
  snapshot_id, device/enrollment/as-of
  encryption/OS/patch/attestation/policy results[]
  provider watermark, completeness/unknowns, expiry/digest

DeviceManagementOperation
  operation_id, device/enrollment/provider
  LOCK | WIPE | QUARANTINE | CONFIGURE | RETIRE
  authority/approval/idempotency/fence
  request/attempt/observation/reconciliation/ambiguity refs

AssetAssignment
  assignment_id, asset/worker/employment
  purpose/location, issued/due/returned times
  condition/custody/shipment refs
  approval/status

AssetCustodyEvent
  event_id, asset, sequence
  from/to custodian/location
  event type/reason/time
  actor/artifact/hash-chain refs

Shipment
  shipment_id, asset/package
  sender/recipient/location endpoints
  carrier/tracking/external refs
  planned/actual milestones
  cancellation/return/status/observations

AssetRecovery
  recovery_id, assignment/offboarding plan
  request/communications/shipment refs
  received/condition/disposition
  loss/escalation/financial refs

AssetReturnAuthorization
  authorization_id, recovery/assignment/asset refs
  return method/shipping destination/deadline
  custody/packaging/wipe/chain requirements, status

AssetInspectionDisposition
  disposition_id, recovery/asset/receipt refs
  returned condition/damage/loss/evidence
  reuse/repair/dispose/quarantine action
  charge/waiver authority, financial/reconciliation refs
```

## Documents, forms and signatures

```text
Document
  document_id, document type/owner/subject refs
  current version ref, lifecycle
  classification/compartment
  record declaration/retention/hold refs

ArtifactObject
  artifact_id, immutable object-store locator
  media type/byte size/canonical digest
  encryption/key/residency/availability refs
  created/received times, source/derivative lineage

ArtifactRevision
  revision_id, logical artifact/parent refs
  immutable object ref, transformation/provenance refs
  classification/retention/hold/index-publication state

ContentInspection
  inspection_id, artifact revision
  malware/sandbox/CDR/parser/DLP checks[]
  tool/provider/version/timestamps, findings
  PASS | FAIL | PARTIAL | UNKNOWN, quarantine/release refs

DocumentClassification
  classification_id, document/artifact revision
  class/compartment/purpose restrictions/confidence
  classifier/version/reviewer/override, derivative propagation policy

DocumentTransformation
  transformation_id, source/derived artifact refs
  OCR | NORMALIZE | CONVERT | COMPRESS | REDACT | CDR | THUMBNAIL
  tool/model/rule versions, parameters/input-output digests
  taint/provenance/review status

DocumentIndexPublication
  publication_id, document/artifact revision
  SEARCH | SEMANTIC | RAG | WORKFLOW_CONTEXT target
  purpose/field/redaction/retention policy refs
  published/revoked times, decision/evidence/status

DocumentQuarantine
  quarantine_id, document/artifact/inspection refs
  reason/blocked operations/authority
  review/release/reject/appeal/expiry refs, status

DocumentVersion
  version_id, document/version/parent
  template/render/canonicalization versions
  source/rendered artifact refs and hashes
  locale/jurisdiction/accessibility refs
  created/sealed times, status/supersession

DocumentTemplate
  template_id, type/purpose/version
  source locale, localized variants[]
  required parameters/schema
  legal/content/accessibility approvals
  effective interval/publication state

DocumentCollectionRequest
  request_id, subject/requester/document type
  evidence requirement, upload/form task
  due date/channel/format constraints
  status/submission refs

DocumentCollectionAttempt
  attempt_id, request/upload session/provider/sender refs
  transport/file metadata/checksum/idempotency
  started/completed times, result/error/quarantine/document refs

DocumentValidation
  validation_id, document version
  schema/content/issuer/freshness/signature checks
  extraction/classification/malware/DLP results
  PASS | FAIL | UNKNOWN | PARTIAL, diagnostics

DocumentExtraction
  extraction_id, source version/tool/version
  extracted fields/confidence/provenance
  human verification/correction refs
  taint/classification

ExtractionField
  field_id, extraction/semantic field path
  raw/normalized typed value, page/region/text span
  OCR/parser/model versions/confidence/taint
  reviewer correction/source provenance refs

Redaction
  redaction_id, source/derived version
  policy/purpose/recipient
  field/region transformations
  completeness/decision-safety/hash

RedactionReview
  review_id, redaction/purpose/recipient
  visible-text/OCR/metadata/embed/comment/annotation/hidden-layer checks
  missed-content findings, reviewer/approval/override/status

RedactionCoverageProof
  proof_id, source/derived artifact refs
  covered content surfaces/embedded objects[]
  verification tool/version/digest, completeness result

FormDefinition
  form_id, version, purpose
  question/section/repeat-group definitions
  answer schema, conditional/calculation/validation rules
  locale/accessibility variants
  field classification/AuthZ/retention
  publication/effective interval

FormQuestionDefinition
  question_id, form version/semantic field binding
  type/cardinality/repeat path/requiredness/sensitivity
  option-set/validation/branch/calculation refs
  localized label/help/accessibility metadata

FormOptionSetRevision
  revision_id, option set/version
  canonical codes/localized display values/order
  effective/retired options and migration mappings

FormRenderPlan
  plan_id, form/version/subject/work-item refs
  exact visible/editable/required/masked question set
  option/rule/locale/legal/accessibility/context/authz versions
  calculated/default values, warnings, canonical render digest

FormDraft
  draft_id, form/version, owner/delegation
  subject/work item refs
  answer/attachment refs, revision/CAS
  context/policy/rule/locale digests
  encrypted storage/expiry/status

FormDraftRevision
  revision_id, draft/author/session
  answer/attachment refs, CAS token/lock/claim
  validation/autosave/expiry/status, supersession

FormSubmission
  submission_id, form/version/draft
  submitter/session/representative refs
  canonical answer/attachment manifest digests
  definition/rule/context/governance digests
  validation/signature/evidence refs
  submitted_at, status/supersession

FormSubmissionReceipt
  receipt_id, submission/idempotency key/request digest
  accepted/rejected state, server/trusted time
  schema/rule/render-plan versions, validation findings/digest

FormSubmissionInvalidation
  invalidation_id, submission/reason/authority
  affected answers/downstream consumers
  replacement submission/revalidation/repair refs

FormAnswer
  answer_id, submission/draft
  question/path/repeat-instance
  typed value/artifact ref
  visibility/branch provenance
  classification/purpose, correction

Attachment
  attachment_id, owner/upload session
  artifact hash/type/size
  tenant/subject/classification
  malware/DLP/quarantine state
  retention/hold/reference count

SignatureRequest
  request_id, document version
  signer requirements/order
  assurance mode = NATIVE_EVIDENCE | EXTERNAL_PROVIDER | MANUAL_GATE
  identity-proof/disclosure/intent requirements
  provider/deadline/status

SignerRequirement
  requirement_id, request/role-or-relationship expression
  assurance/cardinality/order/quorum/delegation/proxy/SoD rules
  jurisdiction/deadline/revalidation/fallback policy

SignerResolutionSnapshot
  snapshot_id, requirement/candidate/selected signer refs
  relationship/role/authority/authz basis
  resolved-at/valid-until/revalidation, digest

IdentityProof
  proof_id, subject
  assurance level/method/provider/version
  provider transaction/evidence hashes
  session/nonce binding
  verified/expires/revoked times, result

SignatureCeremony
  ceremony_id, request/signer/session
  exact document hash/disclosure version
  intent captured, method/provider
  started/completed times, result

SignatureAttempt
  attempt_id, request/ceremony/provider
  idempotency/request/response/redirect refs
  status/timestamps/ambiguity/retry/repair state

SignatureDisclosure
  disclosure_id, ceremony/signer
  exact terms/privacy/consent/locale/version/digest
  presented/accepted/declined times and evidence

SignatureProviderEvent
  event_id, attempt/provider/envelope refs
  authenticated sequence/event state/time
  raw artifact/signature verification/dedupe/replay disposition

Signature
  signature_id, ceremony/document version/signer
  signed hash/value or provider ref
  certificate/key refs, signed_at
  validity/current key state
  decline/dispute/repudiation refs

SignatureEvidenceVerification
  verification_id, signature/document/artifact refs
  certificate chain/timestamp authority/revocation/trust checks
  provider evidence completeness/repudiation status
  verifier/tool/version/evaluated-at/result

AcknowledgementRequirement
  requirement_id, document/content/recipient refs
  PRESENTED | DELIVERED | VIEWED | READ | ACKNOWLEDGED | ACCEPTED | SIGNED
  identity assurance/deadline/evidence/fallback policy

AcknowledgementObservation
  observation_id, requirement/recipient/exact content digest
  channel/identity assurance/evidence source
  presented/viewed/read/acknowledged times, status/ambiguity

BusinessAcceptance
  acceptance_id, subject/document/terms digest
  ACCEPT | DECLINE | WITHDRAW
  identity/session/intent evidence
  action time/channel/supersession

EvidencePackage
  package_id, purpose/scope/recipient
  included artifact/evidence refs[]
  manifest hash/signature
  verification/completeness/redaction
  generated/delivered/expiry times

DocumentRetentionDisposition
  disposition_id, document and complete derivative inventory
  retention trigger/due date/holds/disputes/obligations
  destroy/anonymize/restrict actions and receipts
  verification/certificate/status
```

## Communications

```text
MessageIntent
  message_intent_id, purpose
  tenant/org, audience expression
  template/content/parameter refs
  classification/urgency
  delivery/response requirements
  workflow/correlation/expiry
  purpose taxonomy includes APPROVAL | TASK | REMINDER | NOTICE |
    LEGAL_NOTICE | INCIDENT | EMPLOYEE_MESSAGE | WORKFLOW_UPDATE |
    SURVEY_INVITATION | SURVEY_REMINDER | ANNOUNCEMENT | RECOGNITION |
    ACKNOWLEDGEMENT_REQUEST | SYSTEM_ALERT | LEGAL_CORRECTION

MessageRevision
  revision_id, intent/parent revision
  content/template/parameter/audience/policy refs
  status, correction/recall/withdrawal/expiry/supersession reason
  effective/recorded times, canonical digest

RecipientMessage
  recipient_message_id, intent/revision/audience/binding/recipient refs
  rendered message/delivery plan/attempt/receipt refs[]
  recipient state/requirement satisfaction refs
  independent lifecycle/retry/fallback/correction status

AudienceSnapshot
  snapshot_id, intent
  population/source watermarks
  recipient bindings[]
  resolution/policy versions
  frozen/revalidate mode, digest

DeliveryEndpoint
  endpoint_id, principal/person
  channel/address/provider account
  ownership/verification/tenant binding
  business/personal, purposes/class limit
  locale, effective interval, status/revocation

EndpointSuppression
  suppression_id, endpoint/principal/channel/provider
  BOUNCE | COMPLAINT | OPT_OUT | UNSUBSCRIBE | INVALID | VERIFICATION_EXPIRED
  source/evidence/effective interval, purpose scope
  mandatory-purpose override authority/evaluation refs

RecipientBinding
  binding_id, audience/recipient/endpoint version
  identity/relationship/authority basis
  purpose/disclosure/channel decision
  resolved/valid-until, policy/watermark

MessageTemplate
  template_id, purpose/version
  channel/locale variants
  parameter schema/classification
  legal/accessibility/content approval
  fallback/effective interval/status

RenderedMessage
  rendered_id, intent/recipient/template version
  locale/channel, content/artifact refs
  render input/content hashes
  classification/redaction/link policy

DeliveryPlan
  plan_id, message/recipient
  ordered channel/provider attempts
  preference/legal/quiet-hour/deadline decisions
  cost/rate/fallback rules, status

DeliveryAttempt
  attempt_id, plan/endpoint/provider
  semantic idempotency, payload hash
  QUEUED | SUBMITTED | PROVIDER_ACCEPTED | DELIVERED |
  BOUNCED | REJECTED | EXPIRED | FAILED | AMBIGUOUS
  provider IDs/timestamps/receipts

ProviderDeliveryObservation
  observation_id, attempt/provider event ID
  event type/time/received time, source authority
  signature/authentication result, sequence/dedupe status
  normalized result/confidence/ambiguity, raw artifact ref

CommunicationRequirement
  requirement_id, intent/recipient/jurisdiction/authority
  delivery semantics/recipient assurance/evidence predicate
  deadline/fallback/human-contact policy, legal/rule refs
  invalidation/override/review policy

CommunicationRequirementSatisfaction
  satisfaction_id, requirement/recipient message
  qualifying receipt/read/ack/sign/reply evidence refs[]
  satisfied/invalidated times, status/reason/reviewer

PreferenceEvaluationTrace
  trace_id, recipient message/endpoint
  recipient preference/company policy/legal/DLP/urgency inputs[]
  precedence/composition decisions, selected/rejected channels
  quiet-hour/schedule result, versions/digest

FallbackAttempt
  fallback_id, recipient message/prior attempt
  reason, selected alternate endpoint/channel/recipient/human task
  policy/version, result = DELIVERED | FAILED | NO_ELIGIBLE_CHANNEL |
                   UNRESOLVED_RECIPIENT | MANUAL_CONTACT_REQUIRED

RecipientMessageState
  state_id, message/recipient
  UNSEEN | SEEN | READ | ACKNOWLEDGED | RESPONDED
  evidence/timestamps/supersession

InboxMessage
  inbox_message_id, recipient
  rendered content/artifact ref
  classification/thread/workflow/task refs
  available/read/acknowledged/expiry times
  action capability refs[]
  open/download/attachment/action/forward/share evidence refs[]
  action token expiry/replay fence, delegate/representative access policy
  post-employment access and content-revision history refs

ConversationThread
  thread_id, subject/matter/case/workflow refs
  participants/purpose/classification
  opened/closed times/status

ThreadParticipant
  participant_id, thread/principal/person/relationship refs
  role, membership interval, authorization snapshot
  historical-content visibility, add/remove reason, status

ThreadRelationship
  relationship_id, source/target thread refs
  MERGED_INTO | SPLIT_FROM | SUPERSEDES | RELATED_TO
  message disposition map, authority/reason/effective time

InboundMessage
  inbound_id, provider/channel
  sender endpoint/resolved principal?
  thread/in-reply-to/correlation
  received/content/attachment refs
  trust/taint/classification
  processing/disposition/signal refs
  sender authentication/spoofing/DMARC/provider verification
  quoted-content extraction/out-of-office classification
  malware/DLP/prompt-injection/taint results, human review

BulkCommunicationPlan
  plan_id, audience snapshot/template/channels
  localization distribution
  policy exclusions/cost/rate/batches
  approval/pause/kill/partial status
  per-recipient snapshot fingerprint, exclusions/appeals
  canary result/pause boundary/TOCTOU revalidation
  partial/cancellation/redelivery disposition

CommunicationRateAccount
  account_id, tenant/provider/purpose/channel/recipient scope
  quota/rate/concurrency/cost limits, window/reset
  consumed/reserved/pending values, degradation/kill policy

ContentAccessibilityEvaluation
  evaluation_id, rendered message/artifact/locale refs
  WCAG/conformance, reading order/alt text/captions/transcript
  directionality/language fallback/attachment checks
  PASS | FAIL | PARTIAL | UNKNOWN, reviewer/tool/version
```

## Integration and external-system entities

```text
ExternalSystem
  system_id, vendor/product
  environment/region/accounts[]
  owner/data-classification/residency profile
  lifecycle/health

ConnectorDefinition
  connector_id, vendor/version
  supported objects/capabilities
  auth/read/write/event modes
  pagination/rate/idempotency/observation semantics
  schemas/mappings/health/sandbox/certification

ConnectorConnection
  connection_id, tenant/org/system/account
  connector version, endpoint/environment
  credential ref, configuration/mapping/sync policies
  residency/rate/status/health
  lifecycle = DRAFT | VALIDATING | READY | ACTIVE | DEGRADED |
              SUSPENDED | REVOKED | QUARANTINED

ConnectionTestRun
  run_id, connection/environment, mode = READ_ONLY | SYNTHETIC_WRITE
  credential lease/scope/egress/residency checks
  probe refs[], provider response classes, evidence/status

ConnectionTestProbe
  probe_id, run/capability/object/operation
  request/expected result, observed response/latency
  required/existing external scopes, PASS | FAIL | UNKNOWN

IntegrationReceipt
  receipt_id, tenant/cell/placement/org
  connection/account/subscription generation
  trust profile/auth result/provider ID
  raw artifact/hash/content/schema/parser
  received/trusted-time/signature/replay/malware/DLP
  dedupe/authority/sequence/time/disposition refs

IntegrationReceiptItem
  item_id, receipt/index/path
  payload hash/schema result
  correlation/mapping/authority/disposition
  error/quarantine/reprocessing refs

IngressTrustProfile
  profile_id/version/connection
  authentication/issuer/audience/key refs
  canonical-byte/replay/skew/assurance policy
  unsigned disposition

CorrelationDecision
  decision_id, external namespace/ID
  canonical candidate/selected refs
  crosswalk/effective interval
  UNIQUE | UNKNOWN | AMBIGUOUS | CONFLICTING | REUSED_ID
  assurance/evidence/reasons

SchemaSnapshot
  snapshot_id, connector/object/API version
  descriptor digest/observed time
  fields/enums/presence semantics
  compatibility/consumers/status

SchemaDiscoveryRun
  run_id, connection/object/API version
  discovered descriptor/artifact/digest, trust/inspection result
  differences/unknowns/reviewer, never-auto-publish status

ExternalSchemaContract
  schema_id, version, kind = API | EVENT | FILE | SEMANTIC | STORAGE
  canonical descriptor hash, owner/scope/status
  compatibility/deprecation/effective interval
  generated Protobuf/Go/SchemaFlux artifact refs

ExternalSchemaConsumerBinding
  binding_id, schema version/consumer kind/ref
  required compatibility, adopted version
  migration/test/rollout/retirement status

SchemaChangeImpact
  impact_id, source/target schema versions
  affected mapping/workflow/report/agent/connector/test refs[]
  breaking/lossy/unknown results, migration blockers/remediation

MappingProfile
  mapping_id/version, source/destination schemas
  field rules/transforms/defaults/lookups
  canonicalization/presence/lossiness policies
  authority/crosswalk/golden-vector refs
  effective/publication status
  deterministic transform language/version
  enum/reference/unit/currency/locale/time conversion rules
  null/delete/omission/error/reversibility semantics

MappingPublication
  publication_id, mapping/version/target connection scope
  golden vectors/fixtures/compatibility/impact refs
  approval/signature/effective interval, rollback/supersession/status

MappingExecution
  execution_id, mapping/input receipt item/raw digest
  row/cell/per-field results, transform/lookup/crosswalk versions
  presence/null/delete/lossiness/reversibility diagnostics
  canonical output digest, status/quarantine

MappingResult
  result_id, profile/input record
  per-field source/destination/presence/value refs
  transform/lossiness/warnings/errors
  output digest/status

SyncJob
  sync_id, connection/object/direction/mode
  source snapshot/filter/cursor/watermark
  population/counts/delta refs
  started/completed/status/errors

SyncCheckpoint
  checkpoint_id, sync job/partition
  stable snapshot/cursor/watermark/authority digest
  processed counts/last key/fence, created-at/status

SyncPartition
  partition_id, sync job/range/shard
  lease/fence/priority/retry budget
  counts/errors/checkpoint/status

ExternalObservationRevision
  revision_id, canonical/external resource refs
  field values/presence states, source authority/watermark/version
  observed/received/known/recorded times, freshness/completeness
  supersession/correction

PaginationSnapshot
  snapshot_id, connection/object
  source snapshot/filter/sort/tie-break
  token version/expiry/seen strategy
  expected/observed counts/completion

ConnectorOperation
  operation_id, business/workflow/effect refs
  connection/account/semantic operation
  resource key/causal predecessor/sequence
  authority/fence/cutover/mapping/schema refs
  expected external version
  canonical/mapped payload hashes
  DLP/purpose/destination/idempotency/retry/deadline
  state/attempt/observation/reconciliation refs

ExternalOperationDefinition
  definition_id, connector/version/semantic capability
  request/response schemas, side-effect/idempotency/ordering semantics
  required external scopes, observation/ambiguity/repair policy

ExternalOperationResult
  result_id, operation/attempt/provider request ID
  normalized result, response artifact/hash/external version
  SUCCESS | FAILURE | PARTIAL | UNKNOWN | AMBIGUOUS
  observation-required/retry/redrive/repair disposition

RedrivePlan
  plan_id, original operation/failed attempt refs
  original/current connector/mapping/policy/schema comparisons
  authority/revalidation/materiality/approval results
  selected item set, simulation/idempotency/observation plan

ExternalConflict
  conflict_id, canonical resource/field/effective interval
  candidate source assertions/versions/authorities[]
  expected/external observations, conflict class/status

ExternalConflictResolution
  resolution_id, conflict, strategy/selected authority/value
  reviewer/approval/evidence, correction/writeback/repair refs
  effective/recorded times

ReconciliationComparison
  comparison_id, expected/observed snapshot refs
  authority/freshness/tolerance policy, item refs[]
  PASS | FAIL | PARTIAL | UNKNOWN, repair/status

FieldDiscrepancy
  discrepancy_id, comparison/resource/field
  expected/observed values and presence = PRESENT | ABSENT |
    NOT_APPLICABLE | REDACTED | STALE | UNAVAILABLE
  severity/cause/authority/disposition

ConnectorHealth
  health_id, connection/as-of
  authentication/scope/schema/API status
  latency/error/throttle/queue metrics
  affected capabilities/workflows, incident ref?

EventSubscription
  subscription_id, subscriber/event filter
  tenant/org/population scope
  delivery mode/destination/schema version
  auth/purpose/retry/DLQ/rate/signature policies
  cursor/generation/expiry/status

WebhookDelivery
  delivery_id, subscription/event
  endpoint/signature/key version
  attempt/idempotency/ordering
  request/response hashes and states

WebhookReceipt
  receipt_id, connection/provider event ID
  signature/timestamp/replay/schema results
  immutable payload artifact/hash, mapped subjects/correlation
  processing attempts/status/quarantine

WebhookReplayPlan
  plan_id, original receipt/item set
  target mapping/schema/processor versions
  revalidation/dry-run/dedupe/side-effect suppression policy
  approval/status

ManagedFileTransfer
  transfer_id, connection/partner/direction
  file name/path/manifest/hash/size
  SSH/PGP/certificate refs
  pickup/drop/ack/retry/archive states
  receipt/item refs

ExternalCapacity
  capacity_id, connection/provider
  request/concurrency/token limits
  remaining/reset/retry-after
  queue/predicted completion/throttle metrics

ExternalPermissionDiagnostic
  diagnostic_id, connection/capability/object
  required/configured external scopes/consents[]
  HCM AuthZ/organization/legal/egress composition refs
  read/write feasibility, affected workflow/capability refs

GovernanceCompositionReceipt
  receipt_id, operation/connection/principal refs
  HCM AuthZ + connection scope + provider privilege + legal + egress decisions
  combined result/restrictions/validity/invalidators/digest

AuthorityPolicy
  policy_id, tenant/org/domain/resource/field/operation scope
  connection/source roles and precedence/composition
  effective/recorded interval, conflict/takeover/cutover policy

AuthorityDecision
  decision_id, policy/resource/field/action/as-of
  candidate sources/assertions, selected authority
  epoch/fence/unknowns/conflicts/trace/valid-until

AuthorityHandoffPlan
  plan_id, scope, prior/target authorities
  snapshot/cutover time, writer fences, sync/reconciliation/rollback
  approvals/activation receipt/status
```
