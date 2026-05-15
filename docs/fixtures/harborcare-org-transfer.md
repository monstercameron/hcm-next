# HarborCare Org Transfer Fixtures

These aliases are the canonical fixture names for the
`employee.org_transfer_compensation_change` E2E and UX-contract tests.

The scenario is Jane Doe moving from Boston Main Clinic to Cambridge Clinic.
Use these HarborCare seed records instead of generic Northstar/demo-company
names.

| Alias                  | Value                    | Meaning                                |
| ---------------------- | ------------------------ | -------------------------------------- |
| `tenant_demo`          | HarborCare Medical Group | Demo tenant                            |
| `env_demo`             | HarborCare Development   | Demo environment                       |
| `actor_hr_admin`       | Grace Kim                | HR initiator                           |
| `actor_employee_jane`  | Jane Doe                 | Source worker actor                    |
| `emp_123`              | Jane Doe                 | Source worker                          |
| `actor_manager_morgan` | Morgan Lee               | Boston source manager actor            |
| `emp_456`              | Morgan Lee               | Boston source manager employee         |
| `actor_emp_461`        | Sofia Rossi              | Cambridge destination manager actor    |
| `emp_461`              | Sofia Rossi              | Cambridge destination manager employee |
| `actor_finance_admin`  | Avery Chen               | Finance approver                       |
| `actor_comp_admin`     | Jordan Rivera            | Compensation approver                  |
| `actor_emp_920`        | Daniel Cho               | Medical director approver              |
| `actor_system`         | HCM Next System          | System executor                        |

## Source Placement

| Alias              | Value                |
| ------------------ | -------------------- |
| Source location    | `Boston Main Clinic` |
| Source team        | `Boston Nursing`     |
| Source cost center | `CLN-BOS`            |
| Source manager     | `emp_456`            |

## Target Placement

| Alias                           | Value                                                      |
| ------------------------------- | ---------------------------------------------------------- |
| Target location                 | `Cambridge Clinic`                                         |
| Target location org unit key    | `location_cambridge_clinic`                                |
| Target team                     | `Cambridge Nursing`                                        |
| Target team org unit key        | `team_clinical_operations_clinical_care_cambridge_nursing` |
| Target cost center              | `CLN-CAM`                                                  |
| Target cost center org unit key | `cost_center_cln_cam`                                      |
| Target manager employee ID      | `emp_461`                                                  |
| Business reason                 | `operational_need`                                         |
| Transfer reason                 | `clinic_staffing_need`                                     |

The finance approver seed is scoped to `CLN-BOS` and `CLN-CAM` so tests can
prove current and target cost-center visibility without using non-HarborCare
fixture names.
