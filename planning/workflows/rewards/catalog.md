# Compensation and Rewards Workflow Catalog

Rows use the complete [workflow archetypes](../_shared/workflow-archetypes.md).
All monetary data is fixed-decimal and binds currency, basis, frequency, rounding
and effective range.

| Intent                          | Arch. | Unique dependencies            | Primary data/writes           | Required step delta                                        |
| ------------------------------- | ----- | ------------------------------ | ----------------------------- | ---------------------------------------------------------- |
| SetCompensation                 | A1    | Ppl, Job/Band, Payroll         | initial package/components    | require no overlapping authoritative package               |
| ChangeBasePay                   | A2    | Comp, Budget, Payroll, Reg     | base component revision       | band/minimum wage/retro/payroll simulation                 |
| ChangePayFrequency              | A2    | Comp, Payroll, Reg             | frequency/basis revision      | conversion, cutoff and worker-notice rules                 |
| ChangePayGrade                  | A2    | Job/Grade, Comp, Budget        | grade assignment revision     | band/component eligibility and downstream impact           |
| ChangePayBand                   | A2    | Job/Band, Comp                 | band reference revision       | separate worker pay from range metadata                    |
| ChangeBonusTarget               | A2    | Comp, Budget, Payroll          | target component revision     | percent precision and plan eligibility                     |
| GrantBonus                      | A1/A2 | Comp, Budget, Payroll          | one-time earning award        | award period, approval, tax/payroll effect                 |
| GrantCommission                 | A1/A2 | Comp, Sales source, Payroll    | commission earning            | source calculation evidence and dispute/correction path    |
| GrantEquity                     | A1    | Equity provider, Legal, Tax    | grant proposal/reference      | valuation/country restrictions and provider observation    |
| AdjustAllowance                 | A2    | Comp, Reg, Payroll             | allowance revision            | recurring vs one-time, taxability and eligibility          |
| RemoveAllowance                 | A3    | Comp, Payroll, Reg             | component end                 | notice/contract and payroll cutoff                         |
| PlanMeritIncrease               | A2    | Comp, Performance, Budget      | per-worker proposal           | recommendation is not approval; calibration/equity checks  |
| RunMeritCycle                   | A10   | Comp, Budget, Ppl, Performance | population proposals          | frozen population/watermark, allocation and partial repair |
| SimulateCompensation            | A9    | Comp, Payroll, Budget, Reg     | calculation trace             | no writes/reservations unless separate intent created      |
| EvaluatePayBandPosition         | A9    | Comp, Job/Band, FX             | range/compa result            | pin band/FX/date and explain normalization                 |
| AnalyzePayEquity                | A9    | Comp, Privacy, Metrics         | protected analysis            | cohort/disclosure controls; no unsupported causal claims   |
| ReserveCompensationBudget       | A7    | Budget Authority               | fenced monetary reservation   | currency/FX, expiry and double-spend protection            |
| ReleaseCompensationBudget       | A7    | Budget Authority               | release receipt               | consumed-versus-unused check                               |
| CorrectCompensation             | A5    | Comp, Payroll, Tax             | corrective components         | historical reconstruction and downstream repair            |
| RetroactivelyAdjustCompensation | A5    | Comp, Payroll, Tax, Accounting | retro revisions/deltas        | known-at/effective-at trace and pay-run impact             |
| ExplainCompensation             | A6    | Comp, Provenance, Payroll      | field/calculation explanation | field-level AuthZ and exact decimal/rule provenance        |
