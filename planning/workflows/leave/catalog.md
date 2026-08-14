# Benefits, Time, Leave and Accommodation Workflow Catalog

Rows use the complete [workflow archetypes](../_shared/workflow-archetypes.md).
Medical and accommodation evidence is always compartmented; general workflow
context receives only authorized obligation/status references.

## Benefits

| Intent                      | Arch. | Primary dependencies/data                 | Required step delta                                                     |
| --------------------------- | ----- | ----------------------------------------- | ----------------------------------------------------------------------- |
| DetermineBenefitEligibility | A9    | Employment, Plans, Reg, Time              | pin plan/rule/fact versions and explain inclusion/exclusion             |
| EnrollBenefits              | A2/A8 | Eligibility, Dependents, Payroll, Carrier | enrollment window, elections, evidence, deductions, carrier observation |
| ChangeBenefitElection       | A2    | Life event/window, Payroll                | validate permitted delta and effective date                             |
| CancelBenefitElection       | A3    | Plan, Payroll, Carrier                    | coverage-end/notice/continuation rules                                  |
| ProcessLifeEvent            | A8    | People, Docs, Benefits                    | evidence, event date, window and dependent/election subworkflows        |
| AddDependent                | A1    | Person/Relationship, Docs, Privacy        | identity/link/evidence and plan eligibility                             |
| RemoveDependent             | A3    | Relationship, Benefits                    | distinguish correction, loss of eligibility and voluntary removal       |
| ChangeDependent             | A2/A5 | Person/Relationship                       | route fact corrections to owning domain                                 |
| CalculateBenefitDeduction   | A9    | Plan rates, Payroll, Tax                  | fixed-decimal trace, frequency and effective range                      |
| ContinueBenefits            | A8    | Termination/Leave, Reg, Carrier           | eligibility, notice, election, payment and expiration                   |
| ReconcileBenefitEnrollment  | A9    | Canonical election, Carrier, Payroll      | compare coverage, dependents, dates and deductions                      |
| ExplainBenefitEligibility   | A6/A9 | Plans/Rules/Employment                    | minimum-necessary explanation with rule trace                           |

## Time and scheduling

| Intent               | Arch. | Primary dependencies/data             | Required step delta                                                      |
| -------------------- | ----- | ------------------------------------- | ------------------------------------------------------------------------ |
| RecordTimePunch      | A1    | Device/Identity, Schedule, Location   | trusted device/time, offline sequence, attestation and duplicate control |
| CorrectTimePunch     | A5    | Punches, Approvals, Wage rules        | preserve original, reason/evidence and payroll impact                    |
| SubmitTimecard       | A2    | Punches/allocations/exceptions        | freeze revision and route unresolved exceptions                          |
| ApproveTimecard      | A2    | Manager relationship, Wage rules      | exact revision binding and SoD                                           |
| RejectTimecard       | A3    | Human Work                            | reason and correction task; do not erase submission                      |
| ReopenTimecard       | A4    | Payroll cutoff, Authority             | post-close correction/retro consequences                                 |
| AssignSchedule       | A2    | Worker, Position, Availability, Rules | qualification/availability/rest-period checks                            |
| ChangeSchedule       | A2/A5 | Schedule, Messaging, Wage rules       | notice/predictive scheduling and premium calculation                     |
| PublishSchedule      | A10   | Scheduling, Messaging                 | frozen population/version, delivery and acknowledgement policy           |
| SwapShift            | A8    | two workers, skills, rules            | atomic release/claim, approvals and schedule observation                 |
| OfferShift           | A1/A7 | Open shift, audience                  | eligibility filter, expiry and fair allocation                           |
| ClaimShift           | A7    | Worker, open shift                    | atomic capacity claim and conflict revalidation                          |
| RequestOvertime      | A1    | Schedule/Budget/Wage                  | hours, reason, forecast premium and fatigue rules                        |
| ApproveOvertime      | A2    | Manager/Finance, Wage                 | exact request and no implied time worked                                 |
| AllocateLabor        | A2    | Time, Cost centers/Projects           | allocation totals, crosswalks and accounting impact                      |
| DetectTimeException  | A9    | Punch/schedule/rules                  | deterministic finding with confidence/source completeness                |
| ResolveTimeException | A5/A2 | Finding, evidence                     | correction/waiver/escalation and verification                            |
| CalculateTimeBalance | A9    | Accrual rules, usage, corrections     | unit/rounding/effective-known time trace                                 |

## Leave and accommodation

| Intent                    | Arch. | Primary dependencies/data              | Required step delta                                              |
| ------------------------- | ----- | -------------------------------------- | ---------------------------------------------------------------- |
| RequestLeave              | A1/A2 | Employment, Reg, Forms                 | minimum necessary reason/pattern and protected case creation     |
| DetermineLeaveEligibility | A9    | Service/hours, Programs, Reg/CBA       | composition/concurrency/offset explanation                       |
| ApproveLeave              | A2    | Leave admin/Decision Rights            | distinguish administrative determination from manager preference |
| DenyLeave                 | A3    | Legal/Decision Rights                  | governed reason, notice and review/appeal route                  |
| ExtendLeave               | A2    | Entitlement/evidence                   | recalculate remaining eligibility and downstream effects         |
| ShortenLeave              | A2    | Schedule/Payroll/Benefits              | return readiness and restoration timing                          |
| CancelLeave               | A3    | Leave state/effects                    | before-start cancellation vs active-leave return workflow        |
| StartLeave                | A3/A8 | Payroll/Benefits/Time/Access           | atomic leave activation and effect DAG                           |
| ReturnFromLeave           | A4/A8 | Employment/Schedule/Access             | readiness, restrictions/accommodation and restoration            |
| RequestLeaveEvidence      | A1    | Docs/Privacy/Legal                     | necessity, due date, restricted delivery/visibility              |
| CertifyLeave              | A2    | Evidence, authorized reviewer          | certification period/scope, expiry and contest route             |
| RequestAccommodation      | A1    | Case/Privacy, Forms                    | interactive process and protected evidence compartment           |
| ApproveAccommodation      | A2/A8 | Job/Workplace/Schedule                 | capability restrictions, duration and implementation effects     |
| ModifyAccommodation       | A2    | Case, evidence                         | reassessment, worker participation and no diagnosis disclosure   |
| EvaluateReturnToWork      | A9    | Leave/evidence/job requirements        | readiness result with restricted rationale                       |
| ReconcileLeaveState       | A9    | Leave, Payroll, Benefits, Time, Access | per-domain expected/observed comparisons and repair              |
