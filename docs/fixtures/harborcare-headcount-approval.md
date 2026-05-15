# HarborCare Headcount Approval Fixtures

These aliases are the canonical fixture names for the
`position.headcount_requisition.approval` E2E and UX-contract tests.

The scenario is one new Senior Registered Nurse position for Cambridge Nursing.
Use these HarborCare seed records instead of generic demo-company names.

| Alias                                      | Value                    | Meaning                                      |
| ------------------------------------------ | ------------------------ | -------------------------------------------- |
| `tenant_demo`                              | HarborCare Medical Group | Demo tenant                                  |
| `env_demo`                                 | HarborCare Development   | Demo environment                             |
| `actor_hr_admin`                           | Grace Kim                | Headcount requester                          |
| `actor_manager_morgan`                     | Morgan Lee               | Leadership approver 1                        |
| `actor_emp_461`                            | Sofia Rossi              | Leadership approver 2                        |
| `actor_finance_admin`                      | Avery Chen               | Finance async approver and veto holder       |
| `actor_hr_admin_riley`                     | Riley Patel              | HRBP async approver                          |
| `actor_comp_admin`                         | Jordan Rivera            | Compensation async approver                  |
| `actor_emp_920`                            | Daniel Cho               | Medical Director async approver, veto holder |
| `actor_emp_930`                            | Priya Nair               | Clinic Ops async approver                    |
| `actor_system`                             | HCM Next System          | System executor                              |
| `leadership_chain_gate`                    | sequential gate          | Ordered leadership approval gate             |
| `cross_functional_gate`                    | parallel gate            | Five-actor async quorum gate                 |
| `position_req_senior_rn_cambridge_nursing` | position subject         | New position request subject                 |

## Headcount Request

| Field                    | Value                                                             |
| ------------------------ | ----------------------------------------------------------------- |
| Department               | `Clinical Care`                                                   |
| Team                     | `Cambridge Nursing`                                               |
| Location                 | `Cambridge Clinic`                                                |
| Location org unit key    | `location_cambridge_clinic`                                       |
| Cost center              | `CLN-CAM`                                                         |
| Cost center org unit key | `cost_center_cln_cam`                                             |
| Job code                 | `CLN-RN3`                                                         |
| Title                    | `Senior Registered Nurse`                                         |
| Level                    | `P3`                                                              |
| Requested FTE            | `1`                                                               |
| Target start date        | `2026-07-01`                                                      |
| Salary range             | `98000` to `116000`                                               |
| Business justification   | Cambridge Nursing evening triage volume requires Senior RN cover. |

## Approval Contract

The sequential gate opens only Morgan Lee's task first. Sofia Rossi receives a
task only after Morgan approves.

The parallel gate opens five tasks at once after the leadership gate passes.
Finance, HRBP, Compensation, Medical Director, and Clinic Ops may decide in any
order. The gate passes when any three approvals are recorded unless Finance or
the Medical Director rejects, because both are veto holders. When the gate
passes or fails, remaining pending tasks are canceled and ledgered.
