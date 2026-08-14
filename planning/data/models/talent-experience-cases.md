# Recruiting, Talent, Learning, Experience, Cases and Safety Entities

## Recruiting

```text
Requisition
  requisition_id, headcount/position/job/org refs
  hiring manager/recruiter refs
  openings/FTE/location/employment type
  compensation range/budget refs
  justification/priority/target dates
  approval/publication/status/effective interval

RequisitionRevision
  revision_id, requisition/headcount/budget/position refs
  requested/approved openings and FTE units
  job/org/location/terms/hiring-team snapshot
  approval certificate/proposal digest, effective interval
  correction/supersession, authority/version/digest

RequisitionOpening
  opening_id, requisition revision/position reservation refs
  quantity/FTE unit, requested/approved/filled/closed disposition
  budget/headcount authorization, target date, lifecycle

OpeningAllocation
  allocation_id, opening/application/offer/position refs
  reserved/consumed/released quantity, fence/idempotency
  effective interval, transaction/correction refs

JobPosting
  posting_id, requisition/job profile
  channel/locale/jurisdiction variants[]
  title/description/requirements/range
  accessibility/accommodation/contact content
  published/unpublished times/status
  external posting refs[]

JobPostingRevision
  revision_id, posting/requisition revision refs
  content variant refs[], audience/eligibility/legal requirements
  effective interval, approval/publication/retirement state, digest

PostingContentVariant
  variant_id, posting revision/channel/locale/jurisdiction
  title/description/range/accommodation/accessibility content
  template/source/artifact hashes, legal/content approvals

PostingPublication
  publication_id, posting revision/channel/connection
  external ID/URL, planned/published/expires/unpublished times
  provider acknowledgement/observation/reconciliation refs, status

PostingPublicationAttempt
  attempt_id, publication/effect ref, idempotency/mapping versions
  request/response/receipt hashes, ambiguity/retry/redrive state

Candidate
  candidate_id, person ref? + candidate identity claims
  source/referral/talent-pool refs
  contact/privacy/consent/preference refs
  lifecycle, duplicate-resolution refs

Application
  application_id, candidate/requisition/posting refs
  submitted profile/resume/artifact refs
  source/channel, submitted_at
  stage/status/disposition revisions[]
  consent/notice/accommodation refs

ApplicationRevision
  revision_id, application/candidate/requisition opening refs
  candidate-profile/resume/artifact/source snapshots
  stage/status/disposition, referral/channel attribution
  effective/known/recorded times, authority, correction/digest

ApplicationDisposition
  disposition_id, application revision
  advance/reject/withdraw/duplicate/offer/fill outcome
  standardized reason + restricted detail, actor/authority/evidence
  notice/retention/downstream effects, effective time

RecruitingPipeline
  pipeline_id, version, requisition/job/org scope
  stage refs/transition graph/guards/tasks/evidence/SLAs
  active-instance migration policy, publication/effective interval

ApplicationStageTransitionDecision
  decision_id, application/from/to stage
  exact pipeline/rule/input revision digests
  guard/evidence/task results, actor/authority/override
  effective/recorded times

CandidateProfile
  profile_id, candidate
  employment/education/skill/credential claims[]
  resume/document refs
  self-identification compartment refs?
  source/provenance/freshness

RecruitingStage
  stage_id, pipeline/version
  code/name/order, entry/exit criteria
  required tasks/assessments/approvals

ApplicationStageTransition
  transition_id, application
  from/to stage, reason
  actor/decision/evidence/effective time

ScreeningAssessment
  assessment_id, application/type
  definition/version, inputs/evidence
  scorer human/agent/service
  result/score/threshold/explanation
  accommodations/bias/validation refs

ScreeningDefinition
  definition_id, type/version, purpose/allowed uses
  input schema/rubric/threshold/accommodation policy
  human-review/override/fairness/validation requirements
  provider/model/rule eligibility, effective interval

ScreeningRun
  run_id, application/definition/input snapshot refs
  provider/model/rule versions, scores/confidence/explanation
  accommodation/fairness/human review/override refs
  result/unknowns, executed/recorded times, digest

Interview
  interview_id, application/stage
  schedule/timezone/channel/location
  participant/panel refs[]
  accommodation/interpreter refs
  status/communications

InterviewParticipant
  participant_id, interview/person/principal ref
  role, authority/conflict/recusal/consent
  availability/timezone, historical visibility, status

InterviewEvent
  event_id, interview, schedule revision
  SCHEDULED | RESCHEDULED | CANCELLED | NO_SHOW | COMPLETED
  reason/actor, calendar/provider observation refs
  occurred/recorded times, notification/acknowledgement evidence

InterviewFeedback
  feedback_id, interview/interviewer
  question/rubric version, ratings/comments
  submitted/locked times, visibility/classification
  conflict/recusal refs

BackgroundCheck
  check_id, candidate/application/provider
  package/purpose/jurisdictions
  disclosure/authorization refs
  requested/completed/status
  protected report artifact/result refs
  adverse-action obligation refs

BackgroundCheckAuthorization
  authorization_id, candidate/check/purpose/jurisdiction
  disclosure/document version, standalone consent/signature
  identity assurance, authorized package/scope, expiry/revocation

BackgroundCheckRun
  run_id, check/provider/package/version
  request/attempt/external ID, report artifact/digest
  result scope/status/authority, completed/received times

BackgroundCheckDispute
  dispute_id, run/candidate, disputed items/evidence
  provider/consumer responses, deadlines/status/resolution

AdverseActionNotice
  notice_id, check/run/application, PRE_ADVERSE | FINAL_ADVERSE
  legal/template/version, report/rights refs
  delivery/acknowledgement/deadline/response refs, status

CandidateSelectionDecision
  decision_id, application/requisition
  selected/not-selected/waitlist
  criteria/rubric/evidence/comparator refs
  human/authority/approval/fairness refs

SelectionDecisionRevision
  revision_id, selection/requisition opening
  candidate/comparator population/evidence snapshots
  rubric/criteria/fairness versions, rationale/result
  decision-maker authority/COI/SoD/approval refs
  effective/recorded times, correction/supersession

Offer
  offer_id, application/requisition
  proposal revision, job/position/employment refs
  compensation/benefit/start/location terms
  conditions/contingencies/expiration
  document/signature/acceptance refs
  status/correction/rescission

OfferRevision
  revision_id, offer/application/opening/legal-entity/position refs
  employment/compensation-package/benefit/location/start terms
  conditions/deadlines/jurisdiction/document-template refs
  proposal/canonical terms digest, approval/signature state
  effective/known/recorded times, supersession/invalidations

OfferDeliveryAttempt
  attempt_id, offer revision/recipient/channel/provider
  rendered content/document hash, identity/delivery requirements
  request/receipt/observation/ambiguity refs, status

OfferResponse
  response_id, exact offer revision/terms digest
  ACCEPT | DECLINE | WITHDRAW, candidate identity/session/signature
  reason?, responded_at, validity/invalidation/supersession

OfferRescission
  rescission_id, offer revision, authority/reason/legal checks
  notice/delivery evidence, effective time
  downstream cancellation/compensation/repair refs

OfferCondition
  condition_id, offer, type
  requirement/evidence/deadline
  satisfied/waived/failed state and authority

TalentPool
  pool_id, purpose/criteria/owner
  candidate membership refs[]
  consent/retention/effective interval

RecruitingTransactionPlan
  plan_id, intent/proposal/requisition/application/candidate refs
  typed read/write/conflict/reservation manifests
  posting/ATS/message/document/background-check effects
  consent/legal/approval/data-access/deadline requirements
  compensation/repair/observation/reconciliation paths, digest
```

## Onboarding and offboarding

```text
OnboardingPlan
  plan_id, worker/employment/offer
  start date/location/role
  task/obligation/effect refs[]
  owner, readiness dimensions, status

HireConversion
  conversion_id, candidate/application/offer/person refs
  target worker/employment/assignment refs
  identity-resolution/dedup/source snapshot refs
  field/consent/notice/retention disposition
  idempotency/fence/authority/saga phase
  failure/compensation/repair/evidence refs

OnboardingRequirement
  requirement_id, plan/type
  responsible party, due/dependency
  form/document/evidence/capability refs
  blocking/degradable policy, status

OnboardingReadiness
  readiness_id, plan/as-of
  employment/payroll/benefit/access/equipment/
  schedule/learning/document dimensions
  blockers/unknowns/manual-continuity refs

ReadinessCheck
  check_id, plan/dimension/requirement refs
  READY | BLOCKED | UNKNOWN | WAIVED | STALE | DEGRADED
  criticality/evidence/authority/evaluated-at/valid-until
  dependencies/reason/owner/override/waiver/approver refs

ReadinessSnapshot
  snapshot_id, plan/as-of, check refs[]
  mandatory/degradable/unknown summaries
  start-gate decision, source watermarks/digest

OnboardingTask
  task_id, plan/requirement/work-item refs
  assignee/queue/form/document/deadline/escalation refs
  dependencies/blocking policy/completion evidence/status

OnboardingEffect
  effect_id, plan/requirement/target-domain refs
  expected state/operation/effective time/idempotency
  external operation/observation/reconciliation/repair refs

OnboardingRelationshipAssignment
  assignment_id, plan/worker/related-person refs
  BUDDY | MENTOR | SPONSOR, scope/consent/availability
  effective interval, fallback/reassignment/visibility/status

OffboardingPlan
  plan_id, employment/end fact
  access/equipment/payroll/benefit/document/
  communication/records effect refs
  deadlines/owners/status

TerminationPlan
  plan_id, employment/proposed end revision refs
  restriction/legal/leave/accommodation/CBA/retaliation checks[]
  final-pay/benefit/access/asset/records/notice effect graph
  deadlines/unknowns/reservations/approvals/repair paths

TerminationDecision
  decision_id, exact termination plan/proposal digest
  authority/approval/SoD/legal-review refs
  approve/deny/defer/manual-review result, reason/evidence
  effective time/validity/invalidators

TerminationRestrictionCheck
  check_id, plan/check type, source facts/rules
  PASS | FAIL | UNKNOWN | REVIEW_REQUIRED
  findings/obligations/reviewer/override/evidence/validity

TerminationNotice
  notice_id, plan/recipient/jurisdiction
  legal/template/document versions, required delivery semantics
  delivery/acknowledgement/signature/evidence/deadline refs

OffboardingEffect
  effect_id, plan/domain/target
  required action/effective instant/priority
  capability/external operation refs
  expected/observed/reconciliation state

OffboardingTask
  task_id, plan/work-item/domain/owner refs
  action/dependencies/deadline/irreversibility
  evidence/exception/escalation/completion status

OffboardingChecklistSnapshot
  snapshot_id, plan/as-of, task/effect/obligation refs[]
  satisfied/failed/unknown/waived/unresolved items
  source watermarks/completeness/digest

OffboardingCompletionDecision
  decision_id, plan/checklist snapshot digest
  final pay/benefit/access/asset/records/notice dimensions
  unresolved owner/waiver/repair/incident refs
  authority/result/closed-at

ReinstatementPlan
  plan_id, erroneous end fact/termination refs
  corrective employment/status/assignment append plan
  access/payroll/benefit/schedule/asset restoration effects
  legal/approval/evidence/reconciliation requirements

ReinstatementVerification
  verification_id, plan/corrective transaction refs
  expected/observed domain states[]
  preserved original-history proof, PASS | FAIL | UNKNOWN
  repair/incident refs, verified_at

WorkAuthorizationCheck
  check_id, person/employment/authorization refs
  work type/employer/location/hours/date constraints
  VERIFIED | RESTRICTED | REJECTED | EXPIRED | REVOKED | UNKNOWN
  verifier/issuer/evidence/rule/version/valid-until

WorkAuthorizationReverification
  event_id, authorization/check refs
  due/completed times, trigger, evidence/provider operation
  result/new authorization/refusal/stop-work obligation

EquipmentAssignment
  compatibility name only; canonical entity is AssetAssignment in
  connectivity-access-content.md. No separate aggregate or persistence.
```

## Performance and talent

```text
Goal
  goal_id, worker/owner
  title/description/category
  measurement/target/weight
  alignment refs[], period/effective interval
  status/progress/completion evidence

GoalRevision
  revision_id, goal/worker/employment/assignment refs
  baseline/current/target values and units
  measurement source/formula/version, weight/dependencies/alignment
  period/status, owner/manager acknowledgements
  effective/known/recorded times, correction/supersession

GoalProgressEntry
  entry_id, goal revision, value/unit/as-of
  source/evidence/confidence, author/check-in refs
  correction/supersession, recorded_at

PerformanceCycle
  cycle_id, population/period
  templates/stages/deadlines
  eligibility/rules/owner/status
  frozen population/eligibility snapshot, template/form revisions
  evaluator assignments, calibration/visibility/closure policies

PerformanceReview
  review_id, cycle/worker/reviewer relationship
  form/rubric/goal refs
  rating/feedback/calibration refs
  submission/acknowledgement/status
  visibility/classification

PerformanceReviewRevision
  revision_id, review/cycle/worker/reviewer assignment refs
  exact question/section/answer/rating/goal refs
  locked/submitted/acknowledged/refused/appealed state
  field visibility, notice/access, correction/supersession, digest

PerformanceResponse
  response_id, review revision/question path
  typed answer/artifact, author/rater role
  visibility/classification/evidence, correction/supersession

PerformanceRating
  rating_id, review/dimension
  scale/version/value
  rationale/evidence, source/reviewer
  pre/post-calibration refs
  dimension, raw/normalized value, rater role/confidence
  evidence snapshot, allowed-decision-use refs, correction lineage

Feedback
  feedback_id, subject/author
  type/context/content artifact
  requested/unsolicited, visibility/audience
  classification/retention, effective time

CalibrationSession
  session_id, cycle/population
  participants/authority
  pre/post ratings and change reasons
  distribution/constraint/equity refs
  decisions/status/evidence

CalibrationDecision
  decision_id, session/cohort/worker/rating revision refs
  pre/post values, normalization method/constraints
  fairness/SoD/authority/override/rationale/appeal refs
  effective/recorded times, immutable digest

TalentAssessment
  assessment_id, worker/assessor
  dimensions/rubric/version
  observations/evidence/confidence
  effective/as-of/expiry, classification
  assertion class = FACT | HUMAN_OPINION | MODEL_INFERENCE | RECOMMENDATION
  method/instrument/assessor qualification/accommodation refs
  uncertainty/fairness/contestability/allowed-use refs

ReadinessAssessment
  assessment_id, worker/target job/position
  readiness level/horizon
  skill/experience/performance evidence
  gaps/risks/limitations/confidence
  human/agent sources, expiry

SuccessionPlan
  plan_id, target position/job/org
  criticality/risk/horizon
  candidate nomination refs[]
  owner/review/status/effective interval
  revision, target vacancy/risk snapshot, bench-depth/coverage
  emergency/interim rules, confidentiality/review cadence/outcome refs

SuccessionNomination
  nomination_id, plan/worker
  readiness/interest/mobility refs
  rationale/evidence, rank/tier
  nominated/reviewed/expired state

DevelopmentPlan
  plan_id, worker/owner
  target roles/skills/goals
  activity/learning/mentor refs[]
  period/milestones/status

DevelopmentActivity
  activity_id, plan/goal/skill/learning/mentor refs
  owner/funding/dependencies/milestones/deadlines
  accessibility/accommodation/completion evidence/progress/status

CareerPreference
  preference_id, worker
  roles/locations/work arrangements
  mobility/travel/timing constraints
  visibility/consent/effective interval

InternalMobilityAssessment
  assessment_id, worker/opportunity
  eligibility/skills/readiness/preferences
  conflicts/restrictions/unknowns
  recommendation/explanation refs

TalentDecisionUsePolicy
  policy_id, assessment/rating/feedback type
  permitted/prohibited uses across promotion/compensation/termination/
  succession/access/learning, human-review/notice/contest requirements
  scope/effective interval/version

TalentFairnessAssessment
  assessment_id, decision/process/population snapshot refs
  protected-class access authority, methodology/cohort thresholds
  disparate-impact results/limitations/unknowns/mitigations
  reviewer/approval/version/retention

TalentContest
  contest_id, worker/representative/target decision refs
  grounds/evidence/deadline, notice/acknowledgement
  independent reviewer/result/remedy/correction refs

TalentOutcomeLink
  link_id, source goal/review/assessment/plan/decision refs
  later outcome definition/observation refs
  window/confounders/limitations, non-causal flag
```

## Learning, skills and credentials

```text
Skill
  skill_id, canonical name/aliases
  taxonomy/category/parent refs
  description/proficiency scale
  effective interval/lifecycle

SkillTaxonomyRelease
  release_id, taxonomy/owner/version/locale
  skill revision/relationship refs[], effective interval
  publication/migration/status/digest

SkillRevision
  revision_id, skill/taxonomy release
  canonical name/aliases/definition/category
  parent/child/related/prohibited-equivalence refs
  proficiency scale, effective interval, correction/supersession

SkillEquivalence
  equivalence_id, source/target skill revision refs
  EQUIVALENT | PARTIAL | PREREQUISITE | RELATED
  conversion rule/confidence/authority/evidence/effective interval

ExternalSkillMapping
  mapping_id, skill revision/external system/taxonomy/version/code
  method/confidence/approval/effective interval/status

SkillEvidence
  evidence_id, person/worker/skill
  source = SELF | MANAGER | ASSESSMENT | CREDENTIAL | WORK_PRODUCT
  proficiency/confidence
  artifact/issuer/verification refs
  observed/effective/expiry times

SkillAssessment
  assessment_id, worker/skill
  method/rubric/version
  result/proficiency/confidence
  assessor/evidence/accommodation refs

ProficiencyScale
  scale_id, version, levels/scoring/mapping rules
  locale/effective interval/publication

SkillAssessmentDefinition
  definition_id, skill/dimension/rubric/version
  method/threshold/evaluator/accommodation/retake/appeal rules
  validity/recency/effective interval

WorkerSkillProfile
  profile_id, person/worker/employment/assignment/skill refs
  derived proficiency/qualifying evidence/confidence
  evaluation method/authority/as-of/valid-until/status

SkillRequirement
  requirement_id, job/position/course/task
  skill/proficiency/recency
  mandatory/preferred, evidence rule
  effective interval

SkillGap
  gap_id, worker/target profile
  required/observed proficiency
  evidence freshness/confidence
  gap severity/recommended actions

LearningContent
  content_id, provider/type
  title/description/locale variants
  version/duration/prerequisites
  skill/credential/compliance refs
  accessibility/lifecycle

LearningProvider
  provider_id, type/authority status, connection/account refs
  supported content/credential types, privacy/residency/retention
  lifecycle/status

LearningContentRevision
  revision_id, content/provider/version
  localized title/description, duration/credits/contact hours
  prerequisites/outcomes/accessibility, effective interval
  retirement/supersession, content digest

LearningOffering
  offering_id, content revision/provider/instructor refs
  modality/location/virtual room/timezone/scheduled interval
  capacity/waitlist/deadline/cancellation/status

LearningSession
  session_id, offering, start/end/instructor/location
  attendance requirements/records, status

LearningPath
  path_id, ordered/optional content refs
  prerequisites/completion rules
  target job/skill/population
  effective interval/version

LearningAssignment
  assignment_id, learner/content/path
  assigned_by/reason/requirement
  due date, attempts/progress/status
  waiver/completion refs
  learner kind, source requirement/rule, priority
  assigned/due/grace/effective times, acceptance/cancellation refs

LearningEnrollment
  enrollment_id, assignment/learner/offering
  registration/waitlist/attendance state
  schedule/location/provider refs

LearningCompletion
  completion_id, learner/content/version
  completed_at, score/result
  evidence/provider/verification
  expiration/renewal refs

LearningAttempt
  attempt_id, assignment/enrollment/content revision/offering/session refs
  attempt number, started/completed times, score/pass/attendance
  assessment/provider/evidence refs, status/correction/supersession

LearningRequirement
  requirement_id, COURSE | PATH | SKILL | CREDENTIAL | HOURS | ASSESSMENT
  job/position/role/location/population scope
  mandatory/preferred, evidence/recency/renewal/grace/equivalence rules
  authority/effective interval/version

LearningRequirementEvaluation
  evaluation_id, learner/requirement/input snapshot refs
  qualifying/rejected/missing evidence refs[]
  SATISFIED | UNSATISFIED | EXPIRING | UNKNOWN | EXEMPT
  expiry/recency/waiver results, rule/content versions/trace

LearningRequirementSatisfaction
  satisfaction_id, learner/requirement
  qualifying completion/credential/skill evidence refs[]
  satisfied-at/valid interval/source authority/status/correction

LearningWaiver
  waiver_id, learner/requirement/scope
  reason/authority/approval/evidence/alternate control
  issued/effective/expiry/review times, revocation/status

Credential
  credential_id, person/worker
  credential type/issuer/number token
  issue/valid/expiry dates
  jurisdiction/scope, evidence/verification
  renewal/revocation/status

CredentialType
  type_id, name/category/jurisdiction/acceptable issuers
  validity/renewal/evidence/equivalence rules, effective interval

CredentialVerification
  verification_id, credential/issuer/provider/verifier refs
  method/source assertion/result/confidence
  requested/completed/expires times, evidence/status

CredentialRenewal
  renewal_id, prior/replacement credential refs
  required learning/evidence, due/grace dates
  provider/submission/status/correction refs

CredentialStatusChange
  change_id, credential, prior/new status/reason
  authority/effective/recorded times/evidence

CertificationRequirement
  requirement_id, job/position/location/work type
  credential type, issuer/jurisdiction
  validity/renewal/evidence rules

LearningRecommendation
  recommendation_id, learner/target skill/requirement/job refs
  content/path alternatives, rationale/source gap/evidence
  model/rule/version/confidence/effort/cost
  prerequisites/accessibility/language/legal constraints
  generated/expiry times, human review/accept/decline/outcome refs

LearningProviderOperation
  operation_id, provider/account/learner/target refs
  semantic operation, mapping/payload/idempotency refs
  attempts/provider receipts/expected-observed state
  reconciliation/repair/status

LearningReconciliation
  reconciliation_id, provider/account/learner/scope refs
  expected/observed snapshots, authority/cursor/watermark
  matched/missing/different/unknown items, repair/status
```

## Employee experience, surveys and recognition

```text
Announcement
  announcement_id, purpose/owner
  audience expression/snapshot
  content/template/locale variants
  channels/schedule/expiry
  classification/acknowledgement policy/status
  revision/approval/publication/recall/correction refs
  audience revalidation/per-recipient state/aggregation refs

SurveyDefinition
  survey_id, purpose/owner/version
  question/form refs
  target population/eligibility
  anonymity/confidentiality/small-cohort policies
  schedule/retention/status

SurveyQuestionRevision
  revision_id, survey/form/question
  type/options/validation/branching/requiredness
  locale/accessibility/sensitivity/effective interval/digest

SurveySamplingFrame
  frame_id, survey/eligible population snapshot
  inclusion/exclusion/method, minimum cohort/reidentification risk
  authority/known-at/watermark/digest

SurveyLaunch
  launch_id, survey/population snapshot
  channels/open/close times
  invitation/reminder refs
  response/access-token policy

SurveyInvitation
  invitation_id, launch/recipient-or-anonymous token
  snapshot/channel/expiry/reminder/suppression
  anonymity mode/delivery/consent/status

SurveyLaunchDisposition
  disposition_id, launch
  OPEN | CLOSE | CANCEL | EXTEND | RELAUNCH
  authority/reason/effective time/response consequences

SurveyResponse
  response_id, launch
  respondent token/subject ref if permitted
  answer artifact/digest
  submitted_at, locale/channel
  anonymity/classification/withdrawal status

SurveyResponseSetRevision
  revision_id, response/launch/form render plan
  question-level answer refs/validation
  submission/withdrawal/correction/duplicate evidence
  anonymity token handling, recorded-at/digest

SurveyAggregationPolicy
  policy_id, survey/purpose/viewer scope
  minimum cohort/k-anonymity/suppression/noise/redaction rules
  repeated-query/reidentification/export/support restrictions

SurveyAggregate
  aggregate_id, launch/population/dimension
  count/statistics/suppression/noise state
  source response watermark/policy version/quality/expiry

Recognition
  recognition_id, giver/recipient refs
  program/category/content/value?
  visibility/audience, awarded_at
  approval/tax/payroll refs?

RecognitionProgramRevision
  revision_id, program/category/eligibility/value/visibility rules
  points/budget/tax/payroll/moderation/consent policies
  effective interval/approval/publication/status

RecognitionNomination
  nomination_id, program revision/giver/recipient
  category/content/value/evidence, duplicate/eligibility checks
  approval/moderation/recipient consent/visibility/status

RecognitionValueLedgerEntry
  entry_id, program/nomination/recipient
  GRANT | REDEEM | EXPIRE | REVERSE | CORRECT
  points-or-money/unit/currency/effective time
  tax/payroll/budget/source/predecessor refs

CommunicationPreference
  preference_id, principal/person
  purpose/channel enabled/priority
  quiet hours/timezone/language
  urgency override/effective interval

AccessibilityProfile
  profile_id, person/principal
  presentation/interaction preferences
  assistive technology needs
  authorized representative/interpreter refs
  purpose/visibility/effective interval
```

## HR service, cases and employee relations

```text
ServiceRequestType
  request_type_id, catalog/version
  name/description/owner
  input form, eligibility, routing/SLA
  workflow/capability/output refs
  classification/effective interval

ServiceCatalog
  catalog_id, tenant/org/owner/version
  service offering refs[], locale/publication/effective interval/status

ServiceOfferingRevision
  revision_id, request type/catalog
  eligibility/input form/routing/SLA/fulfillment/workflow/output bindings
  classification/compartment/owner/effective interval/digest

HRServiceRequest
  request_id, type/requester/subject
  purpose/input artifact
  linked case/workflow/task refs
  priority/SLA/status/result
  form/offering/eligibility/requester-representation refs
  idempotency/duplicate/cancellation/consent/confidentiality/correction refs

ServiceEligibilityDecision
  decision_id, request/offering/input snapshot
  eligible/ineligible/partial/unknown result
  rule/trace/reason/authority/validity/appeal refs

Case
  case_id, case type, tenant/org
  subject/participant refs[]
  owner/queue, purpose
  classification/compartment/privilege
  allegation/request/issue summary ref
  evidence/note/task/workflow/decision refs[]
  SLA/legal deadlines, disposition/status/reopen history

CaseType
  case_type_id, owner/domain/purpose
  lifecycle/participant/assignment/evidence/note/disposition rules
  compartment/privilege/retention/hold/SLA/escalation policies
  effective interval/version

CaseRelationship
  relationship_id, source/target case refs
  PARENT | CHILD | DUPLICATE | MERGED | RELATED | APPEAL_OF
  reason/authority/effective time/visibility

CaseTransition
  transition_id, case/from-to states/reason
  actor/authority/guard/evidence refs, effective/recorded times

CaseParticipant
  participant_id, case/person/principal
  role, representation/relationship
  access/visibility/notification scope
  effective interval
  conflict/recusal/notice/consent/representation refs
  access to historical/current content and sealed evidence

CaseNote
  note_id, case/author
  note type/content artifact
  confidentiality/privilege/audience
  created_at, correction/supersession

CaseEvidence
  evidence_id, case/artifact
  submitter/source/custodian
  relevance/category, classification/privilege
  collection/custody/verification refs

EvidenceCustodyEvent
  event_id, case/evidence/artifact
  COLLECT | RECEIVE | TRANSFER | CHECK_OUT | CHECK_IN | DERIVE | SEAL
  from/to custodian, actor/tool/method
  artifact hash/occurred/recorded times/integrity verification

EvidenceVerification
  verification_id, evidence/artifact
  authenticity/integrity/source/timestamp/completeness checks
  verifier/tool/version/result/confidence/dispute refs

CaseAssignment
  assignment_id, case/owner/queue
  role/scope, effective interval
  handoff/escalation refs
  candidate/queue/availability/authority snapshots
  claim fence/SLA clock/COI/SoD/role distinctions

CaseDisposition
  disposition_id, case
  result/category/rationale
  findings/remedies/child intents[]
  authority/approval/appeal refs
  decided/closed/effective times
  closure checklist/legal-policy basis/unresolved obligations
  notice/hold/appeal/reopen/correction refs

CaseAccessDecision
  decision_id, case/note/evidence/field/requester/purpose
  compartment/need-to-know/privilege/matter-wall/recusal checks
  allow/deny/restrict/unknown, masks/expiry/break-glass evidence

ConfidentialityCompartment
  compartment_id, matter/case/owner
  membership/access/search/export/agent/RAG rules
  effective interval/declassification/review/status

PrivilegeAssertion
  assertion_id, matter/artifact/note/evidence scope
  attorney-client/work-product/other basis, jurisdiction
  asserted-by/evidence/effective interval/waiver/review/status

MatterWall
  wall_id, matter/case, included/excluded principals/groups
  role conflicts/minimum-necessary policy, effective interval/status

PolicyQuestion
  question_id, requester/subject/context
  question artifact, policy/jurisdiction refs
  answer/evidence/citations
  agent/human review, confidence/status
  authoritative source/version/retrieval snapshot/citation spans
  effective/known-at/freshness/advisory-vs-authoritative/caveats

EmploymentVerificationRequest
  request_id, worker/requester/recipient
  permitted fields/purpose/consent-or-authority
  template/artifact/delivery refs
  status/expiry/audit
  verifier identity/proof, exact field allowlist/source snapshot
  legal basis/consent/masking/output hash/delivery evidence
  denial/partial/idempotency/revocation refs

AgentHumanHandoff
  handoff_id, source agent execution/service request
  trigger/confidence/model-tool context/evidence transcript
  target queue/skill/SLA, acknowledgement/status
  prohibited autonomous adverse-action flag

ConcernReport
  report_id, reporter identity or anonymous token
  subject/org/category/description artifact
  channel/received time, retaliation protections
  triage/risk/case refs, confidentiality

AnonymousReportToken
  token_id, report/identity escrow ref
  reply/authentication/reveal authority/expiry
  non-linkable public handle, status

ReporterProtection
  protection_id, report/person-or-token/protected activity
  confidentiality/identity-escrow/non-retaliation rules
  prohibited uses/adverse actions/monitoring owner
  effective interval/exceptions/review/escalation

Investigation
  investigation_id, case
  scope/questions/plan
  investigator refs, participant/evidence/interview refs
  findings/privilege/legal-hold refs
  status/timeline

Allegation
  allegation_id, report/case/subject/reporter refs
  category/policy/rule/factual assertion artifact
  asserted/received/known times, confidentiality/hold
  lifecycle/finding/action/correction refs

InvestigationPlan
  plan_id, investigation/scope revision
  allegation/questions/population/jurisdiction/policy refs
  collection/interview/review plans, deadlines
  investigator authority/independence/COI/approval/status

InvestigatorAssignment
  assignment_id, investigation/principal
  role/scope/authority/workload/COI/recusal
  effective interval/handoff/status

InvestigationInterview
  interview_id, investigation/participant
  interviewer/representative/interpreter refs
  schedule/notice/consent
  notes/transcript/artifact refs
  corrections/acknowledgement/classification
  exact question-set/notice/recording consent/security refs
  transcript-vs-notes/dispute/no-contact/accommodation/language state

InvestigationFinding
  finding_id, investigation/allegation
  substantiated/unsubstantiated/inconclusive
  standard/evidence/rationale
  reviewer/authority/confidence
  corrective-action refs

FindingReview
  review_id, finding/evidence snapshot/standard
  reviewer independence/authority/quorum/SoD
  affirm/modify/reject/remand result/rationale/correction refs

DisciplinaryAction
  action_id, worker/employment/case
  type/reason/policy/legal basis
  effective interval, conditions
  notice/acknowledgement/appeal refs
  approval/status
  finding/policy/CBA/comparator/proportionality refs
  suspension/pay treatment/consultation/stay/monitoring/rescission refs

PerformanceImprovementPlan
  plan_id, worker/manager/case?
  expectations/measures/support
  start/end/checkpoints
  consequence/notice/acknowledgement
  progress/review/outcome/status

PIPRevision
  revision_id, plan/baseline/expectation/support/accommodation refs
  measures/consequence policy/checkpoints
  worker response/notice/appeal/effective interval/digest

PIPCheckpoint
  checkpoint_id, PIP revision/date
  expected/observed measures/evidence/worker response
  manager review/progress/extension/hold/next action/status

Grievance
  grievance_id, complainant/representative
  agreement/policy basis, issue
  steps/deadlines/hearing/evidence
  resolution/appeal/status

GrievanceStep
  step_id, grievance/CBA-or-policy version
  filing/hearing/mediation/arbitration/appeal stage
  deadline/tolling/representation/evidence/decision/remedy/status

MatterLegalHold
  hold_id, matter/case/investigation refs
  scope/custodians/data sources/authority/trigger
  notice/acknowledgement/preservation/partial-release refs
  effective interval/status

ContestRequest
  contest_id, target finding/action/disposition refs
  appellant/representative/grounds/evidence/deadline/stay request
  review assignment/notice/status

ContestDecision
  decision_id, contest/independent reviewer
  AFFIRM | MODIFY | SUPERSEDE | CORRECT | REMAND
  rationale/evidence/remedy/notice/effective time

ERActionEffect
  effect_id, finding/action/remedy/target domain
  planned operation/dependencies/authority/effective interval
  attempt/observation/reconciliation/repair/protection refs
```

## Global mobility, immigration and safety

```text
MobilityCase
  mobility_id, worker/employment
  source/destination assignment/location/entity refs
  move type/dates/dependents
  tax/immigration/payroll/benefit/privacy effects
  vendor/evidence/obligation/status refs

MobilityCaseRevision
  revision_id, case/requester/source-destination snapshots
  proposed move/type/dates/participants, proposal digest
  authority/context/effective/known/recorded times/supersession

MobilityPolicyRevision
  revision_id, policy/assignment types/population rules
  source-destination/approval/cost/clawback/tax/benefit/vendor rules
  effective interval/authority/publication/digest

MobilityPlan
  plan_id, case revision/policy
  source/destination leg/assignment/employment changes
  dependencies/risks/assumptions/unknowns/obligations
  approvals/effects/reconciliation/status

MobilityLeg
  leg_id, case/origin/destination refs
  jurisdiction assertions/start-end/purpose/work-presence/travel/status

MobilityParticipant
  participant_id, case/person/relationship
  traveling/accompanying/destination/arrival
  authorization/benefit/schooling/housing needs
  consent/privacy compartment/status

RelocationPackage
  package_id, mobility/worker
  allowance/service/expense components
  eligibility/repayment/tax treatment
  provider/approval/status

InternationalAssignment
  assignment_id, mobility/home+host employment/entity
  start/end, assignment policy
  compensation/tax equalization/benefit refs
  immigration/housing/schooling effects

InternationalAssignmentRevision
  revision_id, international assignment/home-host employment/entity/assignment refs
  type = SHORT_TERM | LONG_TERM | PERMANENT | COMMUTER | ROTATIONAL |
         REMOTE_CROSS_BORDER | BUSINESS_TRAVEL
  start/end/return, role/manager/location/pay groups
  payroll model = HOME | HOST | SPLIT | SHADOW | EOR
  compensation/benefit/immigration/tax refs, status/supersession

WorkdaySegment
  segment_id, worker/employment/assignment/date
  physical location/work jurisdiction/employer/payroll establishment
  work fraction/source/evidence/confidence/correction

TravelDayRecord
  record_id, worker/date/origin/destination/transit
  purpose/work-performed/evidence/verification/correction

WorkAuthorization
  authorization_id, person/worker/jurisdiction
  authorization/visa type, sponsor
  issue/valid/expiry dates
  restrictions/evidence/provider refs
  renewal/revocation/status
  issuer/sponsor/permit token, employer/location/occupation/hours/travel limits
  verification/restriction/renewal-window events

WorkAuthorizationAssessment
  assessment_id, person/worker/assignment/destination jurisdiction
  activity/entity/start/authorization type
  eligible/ineligible/conditional/unknown result
  restrictions/documents/sponsor/rule/evidence/assumptions/reviewer

ImmigrationProcess
  process_id, worker/case/type
  authority/provider, petition/application refs
  milestone/deadline/document refs
  status/decision/appeal

ImmigrationApplication
  application_id, process/applicant/beneficiary/sponsor
  type/jurisdiction/authority/channel
  submitted/receipt/decision times, decision/status
  artifact/fee/deadline/appeal refs

ImmigrationMilestone
  milestone_id, process/type/required/due/completed times
  responsible party/evidence/escalation/status

ImmigrationSponsor
  sponsor_id, legal entity/jurisdiction/type
  registration/scope/compliance/renewal refs/status

ImmigrationProviderEngagement
  engagement_id, process/provider/external matter
  scope/confidentiality/fees/invoices/communications/status

MobilityTaxAssessment
  assessment_id, worker/mobility
  residence/workday/travel facts
  jurisdictions/treaties/permanent-establishment risks
  payroll/shadow-payroll/equalization recommendations
  rule/version/confidence/review

TaxResidencePeriod
  period_id, person/jurisdiction/basis
  start/end/treaty/statutory-test/day-count
  evidence/authority/confidence/dispute/correction

MobilityTaxFactSet
  fact_set_id, case/assignment/tax year
  workday/travel/residence/comp/equity/benefit/payroll/family refs
  treaty/source/known-at/completeness/digest

MobilityTaxInstruction
  instruction_id, assessment/payroll refs
  withholding/contribution/shadow/split requirements
  jurisdictions/reporting/effective interval/approval/rule refs

PermanentEstablishmentAssessment
  assessment_id, legal entity/worker/case/jurisdiction/period
  workdays/contract authority/revenue/customer/fixed-place/
  dependent-agent/local-management facts
  treaty/rules/risk/mitigations/unknowns/reviewer/confidence/deadline

CrossBorderDataTransferAssessment
  assessment_id, case/source-destination regions
  data categories/purpose/legal basis/mechanism/vendors
  encryption/minimization/retention/access restrictions
  approval/review/expiry/status

SafetyIncident
  incident_id, worker/location/event time
  type/severity/description
  injury/exposure/witness refs
  emergency response/investigation/reporting refs
  restricted compartment/status
  incident category = INJURY | ILLNESS | EXPOSURE | NEAR_MISS | HAZARD |
                      PROPERTY_DAMAGE | VIOLENCE | ENVIRONMENTAL
  activity/shift/equipment/substance/witness refs
  immediate/emergency/medical/scene-preservation actions
  actual/potential severity/likelihood/reportability/uncertainty

SafetyIncidentReport
  report_id, incident/reporter-or-anonymous-token/channel
  received/acknowledged times, reporter protection/triage/urgency
  duplicate correlation/evidence/status

HazardObservation
  observation_id, incident/location/source/hazard type
  affected population/risk/existing controls/evidence/status

SafetyControl
  control_id, hazard/incident
  ENGINEERING | ADMINISTRATIVE | PPE | TRAINING | MAINTENANCE
  owner/due/implementation/effectiveness/residual-risk/status

ExposureAssessment
  assessment_id, incident/person/substance/hazard
  exposure route/quantity/duration/confidence
  monitoring/medical-evidence boundary/risk/follow-up/status

WorkplaceInjury
  injury_id, safety incident/person
  body part/nature (protected coding)
  treatment/restriction/lost-time refs
  workers-comp/privacy/reporting refs
  work-relatedness/causation = UNKNOWN | ALLEGED | SUPPORTED | CONTESTED | DETERMINED
  first-aid/treatment/transport/lost-restricted-day refs
  medical compartment/provider/authority/correction refs

WorkersCompClaim
  claim_id, injury/worker/carrier/authority
  claim number/external refs
  filed/decision/payment/appeal states
  evidence/deadlines/return-to-work refs
  jurisdiction/program/insurer/TPA/account
  compensability/reserve/payment/bill/wage-replacement refs
  notice/correspondence/fraud/duplicate/external observation/reconciliation refs

WorkersCompFinancialEvent
  event_id, claim, INDEMNITY | MEDICAL | EXPENSE | RESERVE |
  REIMBURSEMENT | REVERSAL | RECOVERY
  amount/currency/effective time/authority/payment/accounting/correction refs

SafetyInvestigation
  investigation_id, incident
  team/scope/evidence/root causes
  corrective actions/regulatory refs
  status/closure

RootCauseAssessment
  assessment_id, investigation
  contributing factors/causal relation/evidence/alternatives
  uncertainty/confidence/reviewer/status

SafetyCorrectiveAction
  action_id, incident/investigation
  owner/action/due date
  risk priority/status/evidence
  control/hazard/type/owner/org/dependencies/resources
  risk-reduction target/implementation/effectiveness/residual risk
  escalation/recurrence monitoring/closure

SafetyReportabilityAssessment
  assessment_id, incident/injury/jurisdiction/rule pack
  fact snapshot/unknowns/exclusions/decision/reviewer
  deadline/revalidation/override/trace

SafetyTrainingRequirement
  requirement_id, hazard/control/job/location/jurisdiction
  course revision/audience/due/recurrence/assessment/certification rules

RestrictionClearance
  clearance_id, work restriction/issuer/evidence
  full/partial clearance, effective/review times
  schedule/access/payroll observation/correction refs

GovernmentSafetyReport
  filing specialization linking incident/injury/authority/report definition
  reportability assessment/source snapshot/filing package/submission/
  acknowledgement/amendment/reconciliation refs
```
