# Payroll Correction and Retroactive Pay Repair

## Identity and scope

```text
workflow_id: payroll.correction_and_retro/v1
parent intents: CorrectPayroll, CalculateRetroPay, ExecuteRepair
subjects: Worker, PayrollResult, PayRun, Accounting/Tax obligations
state: REFERENCE + EXPLORED
```

Dependencies: authoritative payroll ledger, People/Compensation/Time/Benefits input
facts, Tax/Regulatory computation, Accounting/Finance, Source Authority,
Transaction/Decision ledger, bitemporal provenance, Human Work/Approvals,
Payments when applicable, Government Reporting, Integration, Reconciliation and
Repair Operations.

## Steps

```text
1 detect discrepancy from worker report, invariant, reconciliation or filing reject
2 freeze/identify affected pay results without rewriting history
3 reconstruct expected facts using effective-at and known-at timelines
4 trace causal transaction/source assertion and classify error ownership
5 calculate gross, tax, deduction, benefit, employer and accounting deltas
6 simulate retro/off-cycle options, rounding, wage bases, YTD and filing impacts
7 identify affected workers/pay periods/authorities and conflict with open payroll
8 build immutable CorrectionProposal and RepairPlan with exact calculation trace
9 collect payroll/controller/tax approvals and SoD-required release decision
10 revalidate source facts, rules, accumulators, open periods and payment status
11 append corrective payroll facts and outbox; never overwrite original result
12 create payment/reversal, accounting and tax/report correction effects in order
13 observe bank/provider/accounting/government acknowledgements as applicable
14 reconcile worker balance, accumulators, accounting and filings
15 issue corrected pay statement and governed explanation
16 close or iterate a new RepairPlan for residual drift
```

Candidate data includes discrepancy assertion, expected/observed values, causal
provenance, affected pay components, original rule-pack/calculator versions,
recalculation versions, fixed-decimal deltas, wage bases/YTD accumulators,
correction type, payment/accounting/filing effects, approval bindings,
observations and worker explanation artifact.
