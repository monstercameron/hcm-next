# Compensation Change

## Identity and scope

```text
workflow_id: rewards.compensation.change/v1
legacy_intent: employee.compensation.change
target intents:
  hcmnext.rewards.change_base_pay
  hcmnext.rewards.change_bonus_target
  hcmnext.rewards.correct_compensation
kernel_family: ChangeRequest
subject: EmploymentAssignment + CompensationPackage
initiator: authorized HR/manager/compensation partner
state: EXTRACTED + EXPLORED
```

The legacy workflow combines base amount, frequency and bonus target. The target
must either model those as one package proposal or compose separate component
intents under a parent transaction; it may not accidentally overwrite omitted
components.

## Features and dependencies

| Feature               | Requirement                                                            |
| --------------------- | ---------------------------------------------------------------------- |
| Fixed-decimal money   | Required with currency, scale, rounding and rate basis                 |
| Effective dating      | Required; corrections and retroactivity are distinct modes             |
| Simulation            | Required for annualized value, band, budget, payroll and retro impact  |
| Approval              | Compensation authority plus conditional finance/manager approval       |
| Reservation           | Conditional budget/pool reservation with expiry and fence              |
| Regulatory            | Conditional wage, pay-transparency, contract/CBA and notice evaluation |
| External effects      | Payroll/HRIS/rewards vendor according to source authority              |
| Sensitive compartment | Compensation fields and explanations are field-filtered                |
| Reconciliation/repair | Required for every authoritative downstream effect                     |

Required dependencies: Compensation domain, Employment/Assignment, Position/Job,
Master/Reference Data, Budget Authority, AuthZ, Legal/Regulatory, Source Authority,
Conflict Registry, workflow/Human Work, commit coordinator, ledger, Payroll impact
calculator, Integration Plane, Documents/Messaging when notice is required,
provenance and reconciliation.

## Steps

| #   | Primitive      | Main work                                                                                                      |
| --- | -------------- | -------------------------------------------------------------------------------------------------------------- |
| 1   | CAPABILITY     | Resolve assignment, current package/components, pay basis, authority and applicable workflow version           |
| 2   | TASK           | Collect component operations, effective range, reason, supporting evidence and funding source                  |
| 3   | CAPABILITY     | Normalize money/rate; reject float, currency/frequency mismatch and ambiguous omitted values                   |
| 4   | PARALLEL       | Read job/band, budget, payroll calendar/YTD, jurisdiction/CBA, pending changes and source authority            |
| 5   | RULE           | Validate minimums, band rules, internal policy, SoD, correction/retro semantics and data quality               |
| 6   | CAPABILITY     | Simulate annualization, compa-ratio, budget consumption, payroll/retro/tax impact, notice and external effects |
| 7   | CAPABILITY     | Register field/effective-range conflicts and conditionally reserve budget                                      |
| 8   | APPROVAL       | Collect exact proposal-bound compensation, manager and conditional finance decisions                           |
| 9   | DOCUMENT/TASK  | Generate and acknowledge/sign notice or amendment when required                                                |
| 10  | WAIT           | Wait for effective date/payroll boundary while retaining invalidators                                          |
| 11  | CAPABILITY     | Revalidate worker status, assignment, band, budget fence, authority, law, cutoff, conflicts and approvals      |
| 12  | CHECKPOINT     | Establish final safe point before mutation                                                                     |
| 13  | CAPABILITY     | Atomically append compensation package/component revisions, budget consumption, ledger, projection and outbox  |
| 14  | CAPABILITY     | Dispatch ordered payroll/HRIS/vendor effects with expected external versions                                   |
| 15  | OBSERVE        | Read authoritative package/payroll input state and reconcile exact decimals/effective interval                 |
| 16  | END/COMPENSATE | Release unused reservation; close consistent dimensions or create RepairPlan                                   |

## Data and candidate properties

```text
CompensationChangeRequest
  employment_ref, assignment_ref, package_ref
  operations[]                    // ADD, REVISE, END, CORRECT
  effective_range
  reason_code, reason_detail?
  funding_source_ref?, budget_ref?
  correction_of?, retroactive_reason?

CompensationComponentProposal
  component_type                  // BASE, BONUS_TARGET, ALLOWANCE, COMMISSION...
  amount_decimal?, rate_decimal?, percent_decimal?
  currency?, frequency, basis, units?
  proration_rule_ref, annualization_rule_ref
  taxability_classification_ref?
  end_condition?
```

Current facts include package/component revisions, FTE, job/grade/band, work
location, legal entity, pay group, payroll calendar, YTD accumulators where needed,
budget availability and source versions. Simulation output includes annualized
old/new/delta, compa-ratio, range position, budget and payroll impacts, warnings,
obligations, conflicts, write set, effect graph and estimated cost.

Legacy gaps to reject:

- JSON `number` is prohibited for money and percentages.
- `annual`, `hourly`, and `monthly` need typed basis/frequency semantics.
- The proposal cannot duplicate `effectiveDate` inside compensation and
  `effectiveAt` without an equality invariant.
- Vendor acceptance is not compensation truth or reconciliation.
- A vendor repair action cannot mutate compensation without a new proposal,
  revalidation and authorization decision.
