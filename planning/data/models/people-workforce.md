# People, Identity, Employment and Organization Entities

## Party and identity

```text
Person
  person_id
  lifecycle_state
  preferred_name_ref
  legal_name_refs[]
  demographic_fact_refs[]
  contact_point_refs[]
  address_refs[]
  identity_link_refs[]
  privacy/communication preference refs[]
  preferred locale/accessibility profile refs

PersonNameRevision
  name_id, person_ref, name_type = LEGAL | PREFERRED | FORMER | ALIAS
  structured_name, usage jurisdictions[]
  evidence_refs[], verification_state
  effective_interval, authority, correction/supersession

PersonDemographicFact
  fact_id, person_ref, fact_type
  typed value/artifact ref
  self_identified/verified/source state
  purpose/classification/compartment
  effective interval, correction/supersession

ContactPoint
  contact_point_id, person_or_relationship_ref
  channel, endpoint revisions[]
  verification/ownership/purpose states
  lifecycle

ContactPointRevision
  contact_point_ref, normalized value/provider ref
  business/personal, purposes[], locale
  verification state/evidence
  effective interval, authority, revision

Address
  address_id, owner_ref, address_type
  revisions[], lifecycle

AddressRevision
  address_ref, postal address, geo/location ref?
  validation/normalization/geocode evidence
  effective interval, authority, revision

IdentityClaim
  claim_id, typed subject ref?, claim type/namespace
  raw-value artifact ref?, normalized hash, normalization version
  issuer organization/account/source, assertion vs observation kind
  assurance, evidence/proof refs, allowed purposes
  asserted/issued/effective/known/received/recorded times
  validity/revocation/supersession refs, status

IdentityMatchCandidate
  match_id, incoming claim set, candidate person ref
  exact/fuzzy/semantic features[], score/confidence
  contradictions[], model/rule versions, explanation
  disposition = POSSIBLE | REJECTED | CONFIRMED

IdentityResolutionCase
  case_id, incoming subject/claim-set snapshot/digest
  candidate-set snapshot, search scope/partitions/watermarks
  match-candidate refs[], explicit non-match/contradiction refs[]
  review queue/assignee/SoD, status/deadline
  decision/merge/separation refs, audit/evidence manifest

DoNotMergeConstraint
  constraint_id, person/claim/external-link refs[]
  scope, reason/evidence/authority, confidence
  effective interval, review/expiry, override policy

IdentityResolutionDecision
  decision_id, claim/match refs
  result = CREATE | LINK | MERGE | SEPARATE | UNRESOLVED
  actor/authority/evidence, decided_at
  affected canonical/external links

PersonMerge
  merge_id, survivor_person_ref, merged_person_refs[]
  field/relationship disposition map
  conflict decisions, redirect refs
  effective/recorded times, authority/evidence

PersonMergePlan
  plan_id, source/survivor refs, source revision/watermark snapshot
  per-fact/per-relationship/per-external-ID disposition map
  conflict/unknown list, do-not-merge checks, impact simulation
  downstream rekey/redirect/correction/repair steps
  proposal/approval binding, idempotency/fence, canonical digest

PersonSeparation
  separation_id, prior_merge_ref
  resulting person refs[], claim/relationship redistribution
  downstream correction/notification/repair refs

PersonSeparationPlan
  plan_id, prior merge/current graph snapshot
  new/surviving identities and claim/fact/relationship redistribution
  ambiguous ownership queue, historical lineage preservation
  external relink/correction/repair plan, proposal/approval/fence

FormerIdentifierRedirect
  redirect_id, prior canonical/external identifier
  current entity ref, redirect reason = MERGE | SEPARATION | REKEY
  effective interval, reuse prohibition/expiry policy
  source operation, authority, resolution status

PersonRole
  role_id, person_ref
  role_type = CANDIDATE | WORKER | CONTRACTOR | DEPENDENT | ALUMNUS |
              FORMER_WORKER | REPRESENTATIVE | APPLICANT
  role-context ref, effective interval, authority, lifecycle

RepresentationGrant
  grant_id, represented person/representative principal-or-person refs
  relationship type, permitted intents/capabilities/fields/purposes
  jurisdiction/legal basis/evidence, effective interval
  verification/step-up/revocation/delegation policy

CandidateWorkerConversion
  conversion_id, candidate/application/offer/person refs
  target worker/employment refs, field disposition/copy manifest
  consent/purpose/retention changes, external identifier links
  identity resolution decision, transaction/evidence refs

ExternalIdentityLink
  link_id, canonical entity ref, external object ref
  match basis/assurance, effective interval
  source authority/crosswalk release
  UNIQUE | AMBIGUOUS | CONFLICTING | RETIRED | REUSED_ID

EmergencyContactRelationship
  relationship_id, subject person ref
  contact person ref? or supplied identity snapshot
  relationship code, priority, endpoints[]
  verification/consent, effective interval, authority

DependentRelationship
  relationship_id, worker/person/dependent person refs
  relationship type, dependency/tax/benefit attributes
  evidence/verification, effective interval, jurisdiction

SharedPartyDataGrant
  grant_id, source owner/consumer role-or-relationship refs
  allowed contact/address/identity fact refs or field selectors
  purpose/legal basis, copy-vs-reference semantics
  effective interval, revocation/retention/propagation policy
```

Candidate, worker, contractor, dependent, alumnus and former worker are roles or
relationships around `Person`; they are not mutually exclusive Person states.

## Worker and employment

```text
Worker
  worker_id, person_ref, worker_number?
  worker_type, lifecycle status
  employment_refs[], workforce identity ref?
  original hire/most recent hire/service date refs

Employment
  employment_id, worker_ref, legal_entity_ref
  employment type/classification
  employment agreement/contract refs[]
  start/end/suspension/status revision refs[]
  payroll/benefits/work-authorization refs[]
  primary assignment ref?

EmploymentStatusRevision
  revision_id, employment_ref
  status = PENDING | ACTIVE | SUSPENDED | LEAVE | ENDED | REINSTATED
  reason code + protected detail ref?
  effective interval, authority, evidence

EmploymentStartFact
  employment_ref, planned/actual start
  continuous-service/seniority dates
  probation/trial interval?
  hire/rehire/conversion source refs

EmploymentEndFact
  employment_ref, notice/last-work/effective/end dates
  termination type/reason code
  protected detail ref?, eligibility/reinstatement refs
  final-pay/benefit/access/records obligations[]

EmploymentClassification
  classification_id, employment_ref
  employee/contractor category
  full/part-time/seasonal/temporary status
  exempt/overtime/tax/labor classifications[]
  rule evaluation/evidence, effective interval

EmploymentContract
  contract_id, employment_ref
  contract type/jurisdiction/language
  terms artifact/version/signature refs
  start/end/renewal/notice terms
  compensation/benefit/CBA refs[]

Assignment
  assignment_id, employment_ref
  revision refs[], lifecycle

AssignmentRevision
  assignment_ref, assignment type, primary flag
  job/position/org/location refs
  manager relationship refs[]
  cost center/pay group/work schedule refs?
  FTE/allocation, remote-work arrangement ref?
  effective interval, authority, correction

WorkLocationSegment
  segment_id, assignment_ref, location ref?
  remote/onsite/mobile indicator
  effective interval, hours/allocation
  source/evidence/verification

RemoteWorkArrangement
  arrangement_id, assignment/employment refs
  allowed work locations/jurisdictions
  onsite/remote pattern, schedule constraints
  equipment/expense/security requirements
  approval/evidence/effective interval

ServicePeriod
  service_period_id, worker/employment ref
  start/end, credited service, break reason
  seniority/benefit/leave/payroll treatment

WorkerStatus
  status_id, worker ref, governed status code
  derived-from employment/assignment facts
  effective interval, derivation/version

WorkerLifecycleTransition
  transition_id, worker_ref, from/to state
  intent/transaction/evidence refs, reason
  effective/known/recorded times, authority, guard receipts[]

EmploymentAmendment
  amendment_id, employment/contract refs
  amendment type, changed term paths, prior/new value digests
  effective interval, notice/consultation/signature refs
  legal/rule/approval refs, correction/supersession

EmploymentRelationshipTransition
  transition_id, source worker/employment/assignment refs
  target worker/employment/assignment refs
  type = REHIRE | REINSTATE | CONVERT | ENTITY_TRANSFER |
         ADD_CONCURRENT_EMPLOYMENT | TYPE_CHANGE
  continuity/service/benefit/payroll/identity disposition
  source/target authority, proposal/transaction/evidence refs

WorkerStateSnapshot
  snapshot_id, worker/person/employment/assignment refs
  field/value/authority tuples[], effective_at, known_at
  source watermarks/schema/reference versions, completeness/unknowns
  canonical digest, generated_at

WorkerTimelineEntry
  entry_id, worker/resource/field-path refs
  assertion/event/transaction/correction refs
  effective/known/recorded times, authority, resulting value digest

WorkerExportManifest
  export_id, worker/subject refs, requester/purpose/legal basis
  included/excluded/redacted field manifests
  effective/known-at query, schema/format/version
  artifact/digest/encryption/delivery/expiry/retention refs

EmploymentInvariantReceipt
  receipt_id, proposed transaction and affected refs
  single-primary-assignment/overlap/classification/legal-entity checks[]
  temporal/manager/payroll/benefit/work-authorization checks[]
  pass/fail/unknown, violations[], rule/version/digest
```

## Organizations, legal entities and relationships

```text
LegalEntity
  legal_entity_id, registered/legal names
  registration/tax identifiers[]
  incorporation jurisdictions[]
  registered addresses[]
  currencies/calendars/payroll establishments[]
  parent/ownership refs, lifecycle/effective interval

OrganizationUnit
  organization_id, organization type/code/name revisions
  hierarchy relationship refs[]
  legal entity/cost center/location refs?
  leader/HRBP/finance partner relationship refs[]
  lifecycle/effective interval

OrganizationRevision
  organization_ref, code/names/description
  type, attributes, effective interval
  source authority, correction/supersession

OrganizationRelationship
  relationship_id, parent/child organization refs
  relationship type = REPORTING | LEGAL | FINANCIAL | MATRIX | PROJECT
  primary flag, allocation/ownership?
  effective interval, graph version, authority

WorkerRelationship
  relationship_id, subject/related entity refs
  type = PRIMARY_MANAGER | SECONDARY_MANAGER | HRBP | RECRUITER | MENTOR |
         APPROVER | PROJECT_LEAD | UNION_REP | BUDDY | INVESTIGATOR
  assignment/scope refs, priority/precedence
  effective interval, authority, graph version

RelationshipResolution
  resolution_id, expression, subject/scope/as-of
  candidate paths[], selected refs[]
  graph/source watermarks
  RESOLVED | VACANT | AMBIGUOUS | STALE | DISAGREEING
  explanation/invalidators

CollectiveAgreement
  agreement_id, parties/union/legal entity refs
  covered population expression
  jurisdiction, effective interval
  terms/rule-pack/document refs
  precedence/composition policy

WorksCouncilAgreement
  agreement_id, council/entity/org scope
  affected subjects/processes
  consultation/approval/notice requirements
  effective interval, documents/rules
```

## Jobs, positions and workforce planning

```text
Job
  job_id, code, title/localized titles
  family/profile/level/grade refs
  exempt/classification refs
  skill/qualification/responsibility refs[]
  compensation band refs[], lifecycle/effective interval

JobFamily
  family_id, code/name, parent family ref?
  career track, level framework refs[]
  effective interval, lifecycle

JobProfile
  profile_id, job/family refs
  description/responsibilities
  required/preferred skill and credential refs[]
  physical/work conditions, travel requirements
  document/version/effective interval

JobLevel
  level_id, framework/family ref
  code/title/rank, progression criteria
  management/individual-contributor track
  pay-grade/band refs[], effective interval

Position
  position_id, position code
  job/org/legal-entity/location refs
  capacity and unit, FTE limits
  occupancy/overlap policy
  availability/freeze/lifecycle states
  budget/headcount-plan refs
  effective interval

PositionRevision
  revision_id, position_ref
  job/org/legal-entity/location/budget/headcount refs
  capacity quantity/unit/precision, occupancy policy
  availability/freeze/lifecycle state, effective interval
  source authority, correction/supersession, canonical digest

PositionCapacityInterval
  capacity_id, position_ref, quantity/unit/precision
  available/reserved/occupied quantities
  effective interval, source revision, rounding/conversion policy
  capacity conservation/invariant receipt

PositionOccupancy
  occupancy_id, position/assignment/worker refs
  allocation/FTE, primary flag
  effective interval, reservation/transaction refs

PositionReservation
  reservation_id, position/proposal ref
  reserved capacity, owner/fence/expiry
  HELD | CONSUMED | RELEASE_PENDING | RELEASED | AMBIGUOUS | EXPIRED
  requested/confirmed/released effective times, authority epoch
  conflict key, proposal digest, idempotency key

PositionSplitMerge
  operation_id, source/target positions[]
  capacity allocation map
  occupant/reservation/budget disposition
  effective date, approval/transaction refs

WorkforcePlan
  plan_id, scenario/version, horizon
  organization/legal-entity scope
  assumptions, demand/headcount/budget line refs[]
  status, owner, approval/effective interval

HeadcountPlanLine
  line_id, plan/org/job/location/cost-center refs
  planned positions/FTE/cost
  period, hiring/attrition assumptions
  approved/reserved/consumed quantities

HeadcountRequest
  request_id, plan line/org/job/location refs
  requested count/FTE/date/priority
  justification/business case
  budget/reservation/requisition refs
  approval/status/effective interval

HeadcountFreeze
  freeze_id, scope/population
  prohibited operations/exceptions
  reason, authority, effective interval

ReorganizationPlan
  reorg_id, population/source watermark
  organization/job/position/manager change set
  dependency/impact/simulation refs
  reservations, approvals, batches, status

ReorganizationDisposition
  disposition_id, reorg/source snapshot refs
  source resource, target resource(s), action
  capacity/occupant/reservation/budget/relationship disposition
  dependencies/checkpoints, unresolved questions, approval/repair refs

VacancyProjection
  vacancy_id, position/capacity interval refs
  available FTE/head units, earliest fill date
  blocking reservation/freeze/requisition refs[]
  source watermark, calculated_at, staleness

HeadcountBudgetAuthorization
  authorization_id, plan/line/org/job/location/cost-center refs
  approved quantity/FTE/money/period
  approver authority/certificate, constraints
  reserved/consumed/released projections, expiry/version/fence

OrganizationGraphValidation
  validation_id, graph/source watermark, effective_at
  hierarchy-type-specific roots/cycle/multi-parent/orphan checks[]
  leadership/manager/position consistency checks[]
  PASS | FAIL | UNKNOWN, violations[], rule/version/digest

OrganizationImpactAnalysis
  analysis_id, organization/reorganization/proposal refs
  before/after graph and population snapshots
  affected worker/position/org/manager relationships[]
  capacity/FTE/headcount/comp/payroll/budget effects[]
  IAM/legal/learning/communication/document/integration effects[]
  conflicts/unknowns/risks/obligations
  simulation/read-write-effect manifests
  required approvals, recommended execution/repair plan
  source watermarks/authority/temporal context/digest
```

## Reference and master data

```text
ReferenceDataSet
  dataset_id, domain/name/version
  tenant/global scope, inheritance
  effective interval, publication/lifecycle
  owner/steward/schema/validation refs

ReferenceDataValue
  value_id, dataset ref, code
  localized labels/description
  attributes, parent/alias refs[]
  effective interval, status

ReferenceDataCrosswalk
  crosswalk_id, canonical value ref
  external system/account/object/code
  effective interval, mapping release
  UNIQUE | AMBIGUOUS | CONFLICTING | RETIRED

CostCenter
  cost_center_id, code/name
  finance/legal-entity/organization refs
  owner/manager, currency/budget refs
  hierarchy/effective interval/lifecycle

Location
  location_id, code/localized name
  address/timezone/geofence refs
  legal entity/org refs[]
  worksite/payroll/tax jurisdiction refs[]
  accessibility/capacity attributes
  effective interval/lifecycle

BusinessCalendar
  calendar_id, jurisdiction/org/purpose
  timezone, weekend rules, holidays/closures[]
  observance/cutoff policies, version/effective interval
```
