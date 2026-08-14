# Rewards, Payroll, Tax, Benefits, Time and Leave Entities

## Compensation and rewards

```text
CompensationPackage
  package_id, worker/employment/assignment refs
  component refs[], currency/frequency summary
  effective interval, authority, lifecycle

CompensationComponent
  component_id, package ref
  type = BASE_PAY | BONUS_TARGET | ALLOWANCE | COMMISSION | EQUITY |
         DIFFERENTIAL | STIPEND | ONE_TIME
  amount/rate/percentage/units
  currency/frequency/basis
  proration/annualization/rounding/taxability refs
  earning/deduction code refs?
  eligibility/vesting/condition refs[]
  effective interval, correction/supersession

CompensationGrade
  grade_id, code/name, job/level refs[]
  progression/order, currency/localization
  effective interval

CompensationBand
  band_id, grade/job/location/jurisdiction refs
  minimum/midpoint/maximum Money
  frequency/basis, market/source/version
  effective interval

CompensationChangeProposal
  proposal_id, worker/package refs
  current/proposed component refs
  reason, effective date, market/equity context
  band position/compliance/budget simulations

CompensationSimulation
  simulation_id, proposal/input snapshot
  current/proposed totals and annualized values
  band/compa-ratio/range penetration
  payroll/tax/benefit/budget effects
  warnings/unknowns/rule versions

RewardGrant
  grant_id, worker/ref, type
  amount/units/percentage, currency
  grant/vesting/performance conditions
  approval/document/external-provider refs
  effective interval, status

MeritCycle
  cycle_id, population snapshot
  performance period, budget pools
  guidelines/eligibility/rules
  worksheet/proposal refs[], approval/status

MeritWorksheet
  worksheet_id, cycle/manager/org refs
  worker line refs[], budget totals
  submission/approval/version/status

MeritRecommendation
  recommendation_id, worksheet/worker refs
  current/proposed components
  guideline exception, rationale
  validation/approval/outcome refs

CompensationBudget
  budget_id, org/cost-center/cycle/period
  amount/currency, approved/reserved/consumed
  owner, source authority, effective interval

BudgetReservation
  reservation_id, budget/proposal ref
  amount/currency, owner/fence/expiry/status
  consumption/release refs

PayEquityAnalysis
  analysis_id, definition/population/source watermark
  compensation dimensions, comparator methodology
  control variables, cohort/privacy thresholds
  results/confidence/limitations/remediation refs

CompensationPackageRevision
  revision_id, package/worker/employment/assignment refs
  component revision refs[], pay basis/frequency/currency
  annualization/proration/rounding/FX rule refs
  effective/known/recorded times, authority, correction/supersession
  canonical digest

PayBasis
  basis_id, SALARY | HOURLY | DAILY | PIECE | COMMISSION | MIXED
  unit, standard hours/calendar, precision/rounding
  annualization/proration rule refs, effective interval/version

CompensationComponentValue
  value_id, component revision ref
  exactly one of Money | Rate | Percentage | Quantity | FormulaRef
  currency/unit/frequency/basis, scale/rounding
  source authority, effective interval

CompensationBandRevision
  revision_id, band/grade/job/location/jurisdiction refs
  minimum/midpoint/maximum, currency/frequency/basis
  FX/source/market/rounding refs, effective interval
  overlap/ordering invariant receipt, authority/version

BandPositionResult
  result_id, worker/package/band revision refs
  annualized comparable amount, compa-ratio/range penetration
  BELOW | WITHIN | ABOVE | UNKNOWN, assumptions/trace
  calculation/rule/FX versions, evaluated_at

CompensationCorrection
  correction_id, target package/component assertions[]
  original/corrected values and effective intervals
  reason/source evidence, impacted payroll periods/runs
  retro/tax/deduction/accounting/statement impact plan refs
  approval/transaction/reconciliation refs

CompensationPayrollImpactPlan
  plan_id, proposal/correction refs
  target pay groups/periods, earning/deduction mappings
  cutoff/retro/off-cycle decisions, expected deltas
  rule/version/authority, effect/observation/repair refs

MeritPopulationSnapshot
  snapshot_id, cycle/source query, worker/assignment refs[]
  eligibility inputs/outputs, authority watermarks
  effective/known-at, exclusions/unknowns, digest

MeritCycleFinalization
  finalization_id, cycle/revision
  approved recommendation refs[], budget control totals
  exception/waiver refs[], approval certificate
  compensation transaction batch/effect refs, finalized_at
```

## Payroll

```text
PayrollEnrollment
  enrollment_id, employment/legal-entity/pay-group refs
  payroll system/account/external worker ref
  payment/tax/deduction profile refs
  effective interval, status, authority

PayGroup
  pay_group_id, legal entity/country/payroll establishment
  frequency/calendar/cutoff/currency
  calculation/rule/reporting refs
  effective interval, lifecycle

PayPeriod
  period_id, pay-group ref
  work/start/end/pay dates
  cutoff/close/reopen state and times
  calendar/rule versions

PayrollRun
  run_id, pay-group/period/run type
  population snapshot/watermark
  calculation version/rule sets
  input/result/payment/filing batch refs
  exception/approval/release states
  totals/reconciliation/closure refs

PayrollCalculation
  calculation_id, run/worker/employment refs
  input snapshot, gross/tax/deduction/net totals
  line refs[], accumulator changes[]
  rule/compiler/rounding versions
  trace, status, correction/supersession

PayrollLine
  line_id, calculation ref
  earning/deduction/tax/contribution/garnishment type
  code/ref, units/rate/amount/currency
  taxable/pretax/posttax treatment
  source time/comp/benefit/adjustment refs
  jurisdiction/authority/rule trace

PayrollAccumulator
  accumulator_id, worker/pay-group/authority/type
  period/YTD/lifetime dimensions
  amount/units/currency, wage base
  effective/recorded times, correction lineage

PayrollAdjustment
  adjustment_id, worker/period/line refs
  expected/actual/delta amounts
  cause transaction/assertion refs
  retro periods, simulation/approval/status

RetroPayCalculation
  retro_id, worker/source correction
  affected periods/calculations
  historical rule/input snapshots
  present correction rules, deltas/trace
  tax/deduction/accounting effects

OffCyclePayment
  payment_request_id, worker/legal entity
  reason, earning/deduction lines
  pay date/method/bank instruction ref
  approvals/risk/reconciliation/status

PayStatement
  statement_id, calculation/worker/period
  rendered artifact/version/locale
  delivery/access/retention refs

PaymentInstruction
  instruction_id, payroll run/worker/payee
  amount/currency/value date
  destination token/ref, method
  bank/file/batch/idempotency refs
  status/reversal/reconciliation

PaymentSettlement
  settlement_id, instruction/provider/bank refs
  submitted/accepted/settled/rejected/reversed states
  provider timestamps/receipts
  observed amount/currency, reconciliation

DirectDepositElection
  election_id, worker/payroll enrollment
  tokenized account/routing refs
  allocation amount/percent/priority
  verification/risk/effective interval

DeductionElection
  election_id, worker/payroll enrollment
  deduction type/code/source plan/order
  amount/rate/percentage/limits
  pretax/posttax/tax treatment
  effective interval/status

GarnishmentOrder
  order_id, worker/authority/case refs
  order type/priority, received/effective dates
  calculation limits/exemptions/fees
  protected artifact/compartment
  balance/status/remittance refs

PayrollLedgerEntry
  entry_id, payroll account/worker/legal-entity/run/period refs
  entry type, debit/credit direction, amount/currency/units
  earning/deduction/tax/contribution/payment/accounting dimensions
  effective/known/recorded times, source calculation/transaction
  reversal/correction/predecessor refs, immutable sequence/digest

PayrollCalculationTrace
  trace_id, calculation/worker/run refs
  ordered formula/rule nodes and dependency edges
  input/output values with scale/rounding, accumulator transitions
  jurisdiction/rule-pack/engine/schema versions
  unknowns/warnings, canonical input/output digests

PayrollCorrectionProposal
  proposal_id, discrepancy/causal transaction refs
  expected/observed/source-authority evidence
  affected calculations/periods/YTD/filings/statements/payments
  proposed ledger reversals/replacements, retro simulation
  approval/SoD/risk/effect/reconciliation plan refs

PayrollDiscrepancy
  discrepancy_id, worker/run/period/dimension refs
  expected/observed/delta values, authority/freshness
  severity/materiality, suspected causes, status

AccountingJournal
  journal_id, payroll run/legal entity/ledger refs
  journal line refs[], control totals/currency/period
  accounting policy/mapping version, approval/posting status
  external operation/observation/reconciliation/correction refs

AccountingJournalLine
  line_id, journal/account/cost-center/project refs
  debit/credit, amount/currency, worker/payroll dimensions
  allocation/source-line refs, effective/posting dates

PayrollPopulationSnapshot
  snapshot_id, run/pay-group/period refs
  included/excluded employment refs and reasons
  source watermarks/cutoff/fence, completeness/unknowns, digest

PayrollRunApproval
  approval_id, run revision/input/result/control-total digests
  requirement/certificate refs, approver authority/SoD
  valid-until/invalidators, decision time

PayrollRelease
  release_id, run/approved snapshot ref
  release fence/idempotency, revalidation/control receipts[]
  payment/filing/accounting effect manifests
  released_by/at, ambiguity/closure state

PaymentBatch
  batch_id, run/legal-entity/provider/method/currency
  instruction refs[], count/debit/control totals
  file/artifact/signature/encryption refs
  approval/release/submission/settlement/reversal states

PaymentReturnOrReversal
  record_id, instruction/settlement refs
  return/reversal code, amount/currency/effective date
  provider/bank evidence, worker impact, replacement/payment repair refs

DepositAccountVerification
  verification_id, election/tokenized-account ref
  method, ownership/name-match result, prenote state
  fraud/risk signals, verified/valid-until times, evidence

DeductionArrears
  arrears_id, worker/election/deduction ref
  amount/currency, source periods, priority/limit policy
  recovery schedule, balance ledger, status/correction

GarnishmentCalculation
  calculation_id, order/payroll calculation ref
  disposable earnings, protected minimum, priority, limits/fees
  withheld/remitted/balance results, rule/jurisdiction trace

GarnishmentRemittance
  remittance_id, order/authority/payroll run refs
  amount/currency, due/submitted/accepted dates
  destination/payment/receipt/reconciliation refs
```

## Tax and regulatory computation

```text
TaxProfile
  tax_profile_id, worker/employment refs
  residence/work/payroll/tax jurisdiction refs[]
  filing status/elections/allowances
  identifiers/treaties/exemptions refs
  effective interval, evidence/authority

TaxElection
  election_id, tax profile/authority
  election type/value, form/document ref
  signed/received/effective dates
  status/correction

TaxabilityDetermination
  determination_id, earning/deduction/benefit ref
  authority/jurisdiction
  taxable wage bases/treatment
  rule/trace/effective interval

TaxCalculation
  calculation_id, payroll calc/authority
  taxable wages, employee/employer amounts
  wage bases, rates/brackets/elections
  YTD inputs/outputs, rounding
  rule pack/engine version/trace

SocialContributionCalculation
  calculation_id, worker/employer/authority/program
  contributory base, employee/employer amounts
  caps/floors/rates, accumulator refs
  rule/version/trace

WorkerClassificationAssessment
  assessment_id, person/engagement/employment refs
  facts/questionnaire/evidence
  applicable tests/rules/jurisdictions
  result/confidence/unknowns/reviewer
  effective interval/reassessment deadline

WageComplianceEvaluation
  evaluation_id, worker/job/location/time/pay refs
  minimum wage/overtime/break/recordkeeping rules
  expected/actual/differences
  obligations/violations/correction refs

GovernmentReportDefinition
  definition_id, authority/jurisdiction/report type
  frequency/period/source dimensions
  field calculation/validation/reconciliation rules
  format/transmission/due/correction rules
  version/effective interval

GovernmentFiling
  filing_id, definition/authority/period
  source watermark, calculation/validation refs
  rendered/machine artifact hashes
  approval/signature/submission refs
  PREPARED | SUBMITTED | ACCEPTED | REJECTED | UNKNOWN | AMENDED

FilingSubmissionAttempt
  attempt_id, filing/destination
  idempotency, payload/request/response hashes
  submitted/received times, receipt/ack refs
  result/ambiguity/retry status

RegulatoryObligation
  specialization of Obligation
  registration/notice/report/training/payment/verification subtype
  authority/jurisdiction/subject
  legal deadline and filing/document refs

RegulatoryChange
  change_id, source authority/citation
  published/enacted/effective/retroactive dates
  affected rule packs/domains/jurisdictions
  interpretation/review/publication status
  impact-analysis refs

JurisdictionContext
  context_id, subject/action/domain/effective-date
  residence/work/employment/payroll/tax/privacy/labor/entity authorities[]
  location/boundary assertions and evidence[]
  CBA/contract/company-policy refs[]
  per-rule-family composition profiles, conflicts/unknowns
  resolver/geospatial/boundary versions, authority epoch, digest

RulePackRelease
  release_id, pack/version/effective interval
  canonical content/dependency-lock/input-schema digests
  signature/provenance/source/citation refs
  conformance/fixture/coverage/peer/counsel validation refs
  rollout/shadow/quarantine/rollback state, publication authority

RuleCompositionTrace
  trace_id, rule family/jurisdiction context
  applicable/rejected rule refs and reasons
  composition strategy and ordered operations
  intermediate/final results, conflicts/unknowns/overrides
  evaluator/version, canonical digest

RegulatoryCalculationTrace
  trace_id, calculation/determination ref
  typed input snapshot with provenance
  ordered formulas/rules/composition/rounding nodes
  citations/source text refs, engine/rule/version digests
  output/obligation refs, explanation-safe projection

WorkerClassificationDetermination
  determination_id, engagement/worker/employment refs
  jurisdiction/test-specific results[]
  fact/evidence snapshot, reviewer/authority
  impacts on tax/payroll/benefits/labor/reporting/access
  effective interval, reassessment trigger/deadline, correction/appeal

WageHourFinding
  finding_id, evaluation/worker/location/time/pay refs
  requirement type, expected/actual/difference
  affected interval/quantity/money, rule/citation/trace
  violation/unknown/waived state, remediation/obligation refs

RegulatoryObligationTransition
  transition_id, obligation ref, from/to state
  trigger/action/evidence/waiver/dispute refs
  responsible party/authority, effective/recorded times

ObligationWaiver
  waiver_id, obligation/ref, requested/granted by
  legal basis/authority, scope/reason/evidence
  effective interval, conditions/review/expiry, appeal

FilingPackage
  package_id, filing/original/amended filing refs
  report-definition/rule/schema versions, reporting period
  source-data watermark/snapshot/control totals
  calculated fields, rendered/machine artifacts and hashes
  approval/signature/submission/acknowledgement/correction refs
  immutable status chronology, retention/hold

GovernmentAcknowledgement
  acknowledgement_id, filing/submission attempt
  authority submission/receipt identifiers
  accepted/rejected/partial/unknown result
  field/error codes, official timestamp/artifact/signature
  response correlation, amendment/repair deadline refs

RegulatoryCoverageManifest
  manifest_id, country/region/domain/product version
  supported rule families/authorities/reports/filings[]
  effective date range, known exclusions/limitations
  validation/certification/support owner, status/digest
```

## Benefits

```text
BenefitProgram
  program_id, type, sponsor/legal entity
  jurisdiction/plan-year, currency
  eligibility/enrollment/contribution rules
  lifecycle/effective interval

BenefitPlan
  plan_id, program/carrier/provider refs
  coverage levels/options/network
  premiums/contributions/deduction mappings
  document/rule/effective interval

BenefitEligibility
  eligibility_id, worker/dependent/plan refs
  input snapshot, eligible/ineligible/unknown result
  eligibility date/window, waiting period
  rule/version/trace/reason

EnrollmentWindow
  window_id, subject/event/program refs
  opens/closes, timezone/calendar
  allowed elections/changes, extension refs

BenefitElection
  election_id, worker/plan/coverage level
  covered dependent refs[]
  contribution/premium/deduction refs
  effective interval, evidence/status/correction

LifeEvent
  event_id, worker/person refs
  type, occurred/effective/reported dates
  evidence/verification
  affected enrollment windows/obligations

BenefitDependent
  relationship ref plus plan-specific eligibility/evidence

BenefitContinuation
  continuation_id, worker/plan/qualifying event
  eligibility/notice/election/payment periods
  beneficiary/dependent refs, status

CarrierEnrollmentOperation
  operation_id, election/carrier/account
  mapped payload/version, external member ref
  effective date, status/observation/reconciliation

BenefitProgramRevision
  revision_id, program/sponsor/jurisdiction/plan-year refs
  eligibility/enrollment/contribution/composition rule refs
  effective interval, publication/authority/correction, digest

BenefitOption
  option_id, plan revision, code/name/coverage tier
  covered service/network/limit/deductible attributes
  dependent categories, eligibility constraints, effective interval

BenefitRateSchedule
  schedule_id, plan/option/tier/population refs
  worker/employer/dependent premium components
  amount/rate/formula/currency/frequency, age/salary/tobacco dimensions
  effective interval, rule/rounding/authority/version

BenefitEligibilityInputSnapshot
  snapshot_id, subject/plan/life-event refs
  employment/status/service/hours/location/classification/dependent facts
  source authorities/watermarks, effective/known-at
  missing/unknown/disputed facts, canonical digest

BenefitEligibilityResult
  result_id, snapshot/program/plan refs
  ELIGIBLE | INELIGIBLE | PARTIAL | PENDING_EVIDENCE | UNKNOWN
  eligible options/tiers/date, waiting period/reasons
  per-rule results, override/appeal refs, valid-until

BenefitElectionRevision
  revision_id, election/worker/window/plan-option-tier refs
  covered-dependent interval refs[], waived/default/elected state
  rate/premium/contribution/deduction results
  submitted/effective/known/recorded times, authority
  evidence/consent/correction/supersession, canonical digest

BenefitElectionTransaction
  transaction_id, worker/window/life-event refs
  exact election revision set, cross-plan constraints
  expected prior revisions, proposal/approval/evidence
  carrier/payroll effects, atomic commit/reconciliation refs

LifeEventDetermination
  determination_id, reported event/evidence refs
  verified event type/date, jurisdiction/rules
  duplicate/conflict checks, affected programs/windows
  approval/appeal/correction, result/trace

ContinuationProgramAccount
  account_id, continuation/program/beneficiary refs
  notice/election/grace/coverage/payment timelines
  premium ledger refs, reinstatement/termination state
  provider/authority observations, reconciliation

BenefitDeductionObligation
  obligation_id, election revision/payroll enrollment refs
  deduction code/amount/rate/frequency/start/end
  arrears/refund/correction policy, source authority
  payroll operation/observation/reconciliation refs

BenefitEnrollmentReconciliation
  reconciliation_id, worker/election/carrier/payroll refs
  item refs[], expected/observed authority/watermarks
  PASS | FAIL | PARTIAL | UNKNOWN, repair/deadline refs

BenefitEnrollmentDifference
  difference_id, reconciliation/plan/option/dependent/field refs
  expected/observed values, severity/cause/disposition
```

## Time, attendance and scheduling

```text
TimePunch
  punch_id, worker/assignment
  punch type, event/device time, trusted receipt time
  timezone/tzdb, device/clock/location evidence
  source/device identity, offline sequence
  acceptance = ACCEPTED | REVIEW | REJECTED | DUPLICATE
  correction/supersession refs
  idempotency key, prior punch/hash-chain ref
  clock calibration/trust/skew result, replay/wipe epoch
  shared-device identity/attestation policy refs
  location accuracy/geofence decision/evidence

Timecard
  timecard_id, worker/assignment/period
  punch/time-entry refs[], calculated totals
  exception refs[], submission/approval/lock status
  rule/source versions, correction lineage

TimeEntry
  entry_id, timecard
  start/end/duration, timezone
  earning/labor/schedule/project refs
  source/manual reason, attestation
  derivation/source-punch refs, paid/unpaid break segments[]
  DST fold/gap resolution, overlap/adjacency policy

TimeException
  exception_id, worker/timecard/entry
  type/severity, expected/actual
  rule/evidence, owner/status/resolution

TimeAttestation
  attestation_id, timecard/worker/manager
  statement/version, exact data digest
  principal/session/time/signature evidence

TimeBalance
  balance_id, worker/program/type
  opening/accrual/used/adjustment/closing
  units, as-of, rule/trace/correction

WorkSchedule
  schedule_id, organization/location/worker scope
  timezone, cycle/pattern, version/effective interval
  rule/constraint/demand refs

Shift
  shift_id, schedule/location/job
  start/end/breaks, required skills/headcount
  status, publication/version

ShiftAssignment
  assignment_id, shift/worker/assignment
  status, source/offer/claim/swap refs
  premium/overtime/compliance results

ShiftOffer
  offer_id, shift/eligible population snapshot
  opens/expires, selection policy
  claimant/status/decision evidence

ShiftSwap
  swap_id, offered/requested shifts/workers
  eligibility/compliance/approval
  reservation/status/effective transaction

LaborAllocation
  allocation_id, time entry
  cost center/project/job/location dimensions
  percentage/hours/amount, validation

StaffingDemand
  demand_id, location/org/job/skill/time interval
  required quantity/service level
  source forecast/version/confidence

ScheduleOptimization
  optimization_id, demand/worker availability snapshot
  constraints/objective/cost weights
  proposed schedule, violations/unknowns
  solver/version/approval/publication

TimecardRevision
  revision_id, timecard/worker/assignment/period refs
  immutable punch/entry/exception refs and totals
  worker attestation/manager approval refs
  payroll cutoff/period/run binding, lock/freeze state
  rule/jurisdiction/source versions, canonical digest
  correction/supersession refs, effective/known/recorded times

TimeComplianceCalculation
  calculation_id, timecard revision/worker/location refs
  regular/overtime/premium/break/rest/fatigue results[]
  rule-pack/jurisdiction/version, inputs/trace/rounding
  payroll earning bridge refs, violations/unknowns

TimeExceptionResolution
  resolution_id, exception/finding-input digest
  CORRECT | WAIVE | ESCALATE | NO_ACTION
  source completeness/confidence, rule/jurisdiction versions
  actor/waiver authority/evidence, exact correction refs
  post-resolution validation/reconciliation

TimeBalanceLedgerEntry
  entry_id, worker/program/type
  ACCRUAL | USAGE | CARRYOVER | EXPIRY | ADJUSTMENT | REVERSAL | CORRECTION
  quantity/unit, effective/known/recorded times
  lot/source transaction/predecessor refs
  cap/floor/negative/expiry/rule/rounding versions

WorkerAvailability
  availability_id, worker/assignment
  available/unavailable/preferred intervals, reason/classification
  source/consent, timezone/calendar, effective interval

ScheduleComplianceEvaluation
  evaluation_id, schedule/shift/assignment refs
  availability/skill/leave/holiday/CBA/location snapshots
  rest/fatigue/notice/predictive-scheduling results
  premium/cost impacts, rule/trace/unknowns

ShiftClaimReservation
  reservation_id, offer/shift/claimant refs
  capacity unit, linearization/fencing token, idempotency key
  audience snapshot/selection algorithm/version/rationale
  held/expires/consumed/released times, race disposition

ShiftSwapTransaction
  transaction_id, swap/source/target assignment revisions
  atomic release-and-claim write set, expected revisions/fences
  compliance/approval/observation refs, correction/supersession

OvertimeRequest
  request_id, worker/assignment/interval/hours
  reason, budget/cost-center, forecast premium
  fatigue/rest/compliance result, proposal digest/status

OvertimeApproval
  approval_id, request revision/proposal digest
  approval requirement/certificate, authority/validity
  explicit statement that approval is not evidence time was worked

LaborAllocationRevision
  revision_id, time entry ref, dimension/crosswalk versions
  allocation lines[], sum/precision/rounding residual policy
  accounting posting/observation refs, correction/supersession

TimePayrollBridge
  bridge_id, timecard revision/compliance calculation refs
  target pay group/period/run, earning/premium lines[]
  cutoff/reopen/retro disposition, mapping/rule versions
  external effects/observations/reconciliation/repair refs

OptimizationInputSnapshot
  snapshot_id, demand/worker/availability/skill/schedule refs
  hard/soft constraints and priorities, objective/cost definition
  source watermarks/unknowns, canonical digest

OptimizationResult
  result_id, input snapshot/solver/version/seed
  proposed assignments, objective values, constraint outcomes
  infeasibility/unknown certificate, explanation/limitations
  approval/publication binding, deterministic digest
```

## Leave, absence and accommodation

```text
LeaveCase
  leave_case_id, worker/employment
  protected compartment, request/event refs
  requested/approved/used interval refs[]
  program entitlement/evidence/deadline refs[]
  payroll/benefit/schedule effects[]
  status, return-to-work ref

LeaveRequest
  request_id, case/worker
  leave type/reason category
  continuous/intermittent/reduced schedule pattern
  requested dates/duration, notice date
  evidence/accommodation/representative refs

LeaveType
  type_id, code/name, reason/protected category
  paid/unpaid semantics, continuous/intermittent/reduced-schedule support
  evidence/privacy/disclosure policy, effective interval/version

LeaveProgram
  program_id, sponsor/authority/jurisdiction
  entitlement/eligibility/pay/concurrency rules
  certification/notice/deadline requirements
  effective interval/version

LeaveEligibility
  eligibility_id, case/program
  fact snapshot, eligible amount/period
  waiting/concurrency/offset result
  rule/version/trace/unknowns

LeaveEligibilityInputSnapshot
  snapshot_id, case/program/worker/employment refs
  service/hours/location/jurisdiction/classification/prior-usage facts
  CBA/policy/payroll/tax facts, source authorities/watermarks
  effective/known-at, completeness/missing/disputed facts, digest

LeaveDetermination
  determination_id, case/request revision refs
  per-program eligibility/composition results[]
  APPROVED | PARTIALLY_APPROVED | DENIED | PENDING_EVIDENCE | UNKNOWN
  approved quantity/interval/pattern, decision reason
  reviewer/authority/approval/appeal refs, effective_at

LeaveProgramComposition
  composition_id, case/program refs[]
  concurrency/stacking/offset/most-protective strategies
  pay/benefit priority, conflicts/unknowns, rule composition trace

LeaveEntitlement
  entitlement_id, worker/program/case
  granted/used/remaining quantity
  period/expiry, concurrency group
  accrual/correction refs

LeaveEntitlementLedgerEntry
  entry_id, entitlement/program/case refs
  GRANT | ACCRUAL | USAGE | OFFSET | EXPIRY | ADJUSTMENT | REVERSAL | CORRECTION
  quantity/unit, effective interval, known/recorded times
  source transaction/occurrence/predecessor refs, reason
  rule/rounding/version, immutable digest

LeaveBalanceSnapshot
  snapshot_id, entitlement/as-of
  opening/activity/remaining quantities, source watermark
  calculation/rule versions, completeness, reconstructable digest

LeavePeriod
  period_id, case/program
  approved start/end/pattern
  actual usage, pay/status treatment
  schedule/timecard/payroll links

LeaveOccurrence
  occurrence_id, period/case/program/employment refs
  planned-or-actual, start/end, quantity/unit, partial-day/hours
  notice/call-in/source/evidence refs, timezone/calendar
  timecard/payroll links, approval/validation/correction/supersession

IntermittentLeavePattern
  pattern_id, case/program
  frequency/minimum increment/maximum occurrence quantity
  notice/approval windows, recurrence end, call-in protocol
  schedule interaction, evidence policy, timezone/calendar/version

LeaveOccurrenceApproval
  approval_id, occurrence revision/proposal digest
  eligibility/remaining balance/notice/evidence checks
  authority/decision/effective time, invalidators

LeaveEvidence
  evidence_id, case/requirement
  protected artifact ref, issuer/type
  received/certified/validity dates
  verifier, sufficiency/status

LeaveCertification
  certification_id, case/evidence set
  certifier/authority, certified limitations/dates
  decision/expiry/renewal refs

AccommodationCase
  accommodation_id, worker/employment
  protected compartment
  request/interactive-process/evidence refs
  restriction/option/decision refs
  review/expiry/status

WorkRestriction
  restriction_id, accommodation/safety/leave case
  functional limitation (minimum necessary)
  prohibited/allowed duties/schedule/location
  effective interval, issuer/evidence

CapabilityRestriction
  restriction_id, case/decision refs
  allowed/prohibited duties/capabilities/schedule/location constraints
  effective interval, minimum-necessary source evidence token
  disclosure profile/audience field masks, review/expiry

MedicalFactBoundary
  boundary_id, case/subject
  opaque protected-compartment/evidence refs only
  existence/verification/validity status
  authorized purposes/audiences, explicit prohibited projections/indexes

AccommodationOption
  option_id, case, proposed adjustment
  feasibility/cost/impact/undue-hardship refs
  confidentiality/implementation requirements

InteractiveAccommodationProcess
  process_id, case/worker/employment refs
  participants/representatives/communication/meeting refs[]
  requested adjustment, essential-function/workplace snapshots
  option/worker-response refs[], good-faith status
  unresolved issues, next review deadline, accessibility/language needs

UndueHardshipAssessment
  assessment_id, case/option
  cost/financial/operational/safety factors[]
  alternatives/accommodations considered[], organization context
  jurisdiction/rule refs, reviewer/authority/rationale
  result/unknowns, effective/known/recorded times

AccommodationDecision
  decision_id, case/option
  approve/deny/modify, rationale
  legal/authority/human-review evidence
  effective interval/review date

ReturnToWorkAssessment
  assessment_id, leave/accommodation/worker
  planned/actual return, readiness facts
  restrictions/accommodations/schedule
  evidence/decision/unknowns

ReturnToWorkPlan
  plan_id, case/assessment refs
  planned/phased return schedule, job/position requirement snapshot
  restriction/accommodation refs, schedule restoration
  payroll/benefit/access restoration effect refs
  manager/HR instructions, worker acknowledgement
  READY | READY_WITH_RESTRICTIONS | NOT_READY | PENDING_EVIDENCE | UNKNOWN
  approval/status/review date

ReturnToWorkEvent
  event_id, plan/case/employment refs
  actual return time, attendance/shift evidence
  restrictions observed/deviations, source authority
  correction/supersession refs

AbsenceEffect
  effect_id, case, target domain
  payroll/benefits/time/schedule/access operation
  expected state/effective interval
  observation/reconciliation status

AbsenceEffectPlan
  plan_id, case/determination refs
  typed effects[] = PAYROLL_EARNING | PAYROLL_DEDUCTION | PAYROLL_TAX |
                    BENEFIT_COVERAGE | BENEFIT_PREMIUM | TIME_ACCRUAL |
                    SCHEDULE | POSITION_CAPACITY | ACCESS | LEARNING |
                    COMMUNICATION | GOVERNMENT_NOTICE
  target operation/dependencies/expected state/effective interval
  reversibility/idempotency/authority/required observation

AbsenceEffectObservation
  observation_id, effect ref, source authority/watermark
  observed state/time/version, completeness/ambiguity
  reconciliation/repair refs

LeaveCaseTransition
  transition_id, case, from/to state, reason
  actor/authority/evidence refs, effective/recorded times

LeaveChangeDisposition
  disposition_id, case/change type = EXTEND | SHORTEN | CANCEL | RETURN
  requested/current phase, new eligibility/evidence requirements
  retro/payroll/benefit/schedule effect consequences
  reversal/repair plan, decision/approval/status

LeaveConflict
  conflict_id, case/other-intent/resource refs
  interval/conflict type, affected domain/severity
  resolution policy, disposition/override evidence

LeaveReservation
  reservation_id, case/proposal digest
  entitlement quantity and payroll/benefit/schedule reservations[]
  fence/expiry, consumption/release refs, status

LeaveAppeal
  appeal_id, determination/case refs
  appellant/representative, grounds/evidence/deadline
  independent reviewer/authority, status/decision/remedy

LeaveProviderOperation
  operation_id, case/provider/connection/external-object refs
  semantic operation, payload/mapping/idempotency digests
  attempts/acknowledgement/ambiguity/redrive refs, status

LeaveExternalObservation
  observation_id, case/provider/source-authority epoch
  external state/value snapshot, watermark/effective/observed time
  completeness/ambiguity, reconciliation/repair refs
```
