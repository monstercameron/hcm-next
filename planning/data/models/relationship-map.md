# Cross-Domain Relationship Map

This map makes aggregate ownership and cross-domain references explicit.

```text
Person
  +-- Worker
  |     +-- Employment
  |     |     +-- Assignment ---- Job
  |     |     |       |           +-- JobFamily / JobLevel / JobProfile
  |     |     |       +-- Position ---- OrganizationUnit / Location
  |     |     |       +-- WorkerRelationship ---- Person/Worker/Org
  |     |     +-- EmploymentContract / CollectiveAgreement
  |     |     +-- PayrollEnrollment ---- PayGroup ---- PayrollRun
  |     |     +-- BenefitElection ---- BenefitPlan
  |     |     +-- LeaveCase / AccommodationCase
  |     +-- WorkforceIdentity ---- DigitalAccount ---- AccessGrant
  |     +-- Timecard / Schedule / Learning / Talent
  +-- Candidate ---- Application ---- Requisition ---- Position/Headcount
  +-- DependentRelationship ---- BenefitElection
  +-- ContactPoint / Address / IdentityClaim

BusinessIntent
  +-- ProposalRevision
  +-- WorkflowInstance
  +-- ApprovalCertificate / WorkItem
  +-- BusinessTransaction ---- LedgerEvent / OutboxEffect
  +-- Observation ---- ReconciliationResult ---- RepairPlan
  +-- ClosureRecord ---- OutcomeObservation
```

## Ownership boundaries

| Entity family                   | Owning domain/plane          | Other domains reference; they do not mutate directly           |
| ------------------------------- | ---------------------------- | -------------------------------------------------------------- |
| Person and identity linkage     | People / Identity Resolution | Recruiting, Benefits, Cases, Access, Privacy                   |
| Worker/Employment/Assignment    | People                       | Payroll, Benefits, Time, Talent, Access                        |
| Organization/Job/Position       | Workforce                    | People, Recruiting, Rewards, Analytics                         |
| Compensation                    | Rewards                      | Payroll consumes governed components                           |
| Payroll results                 | Payroll                      | Rewards/People observe; accounting/reporting consume           |
| Benefit elections               | Benefits                     | Payroll consumes deductions; Leave triggers eligibility        |
| Time facts                      | Time/WFM                     | Payroll consumes approved payable time                         |
| Leave/Accommodation             | Leave                        | Payroll/Benefits/Time/Access receive minimum necessary effects |
| Candidate/Application           | Recruiting                   | People receives selected/verified conversion inputs            |
| Talent/Learning                 | Talent                       | People/Access consume verified completion/credential facts     |
| WorkforceIdentity/AccessGrant   | Access                       | People facts drive entitlement calculations                    |
| Case/Investigation              | Case Management              | Child intents perform authorized domain changes                |
| Document/Form/Signature         | Content plane                | Domains reference immutable artifacts/evidence                 |
| Message/Conversation            | Messaging                    | Workflows create intent; messaging owns delivery               |
| Connector operation/observation | Integration                  | Domains own semantic intent and expected state                 |
| Rule/Obligation/Jurisdiction    | Regulatory/Governance        | Workflows bind and enforce results                             |
| Intent/Workflow/Approval        | Kernel/Workflow              | Domains own facts; kernel owns coordination                    |
| Ledger/Provenance/Repair        | Data/Assurance               | All domains emit evidence through contracts                    |

## Relationship semantics

Every edge declares:

```text
relationship identity and type
source and target tenant-scoped refs
owning authority
effective interval and known-at
cardinality and overlap policy
primary/priority/precedence if meaningful
classification/purpose constraints
source graph/version/watermark
correction/supersession lineage
```

No foreign key implies business precedence. Examples:

- A Person may have simultaneous Candidate, Worker and Dependent roles.
- A Worker may have multiple concurrent Employments.
- An Employment may have multiple Assignments, only according to an explicit
  primary/overlap policy.
- Manager, HRBP and approval relationships are independent edges and may resolve
  differently by assignment, organization, purpose and time.
- Position occupancy is a capacity allocation relationship, not a `worker_id`
  column on Position.
- External identity/crosswalk links never confer domain authority.

## Write composition example: PromoteWorker

```text
PromoteWorker intent
  reads Worker + Employment + Assignment + Job + Position
  reads CompensationPackage/Band + Budget
  reads Jurisdiction/Rules + Access/Talent/Learning policies
  proposes:
    AssignmentRevision
    PositionOccupancy changes
    WorkerRelationship revisions
    CompensationComponent revisions
  reserves Position + CompensationBudget
  obtains ApprovalCertificate + document/acceptance evidence
  commits locally owned facts through BusinessTransaction
  emits Payroll/Access/Learning/Message effects
  observes/reconciles each external authority
  closes with multidimensional ClosureRecord
```

The workflow composes these aggregates; it does not become their owner.
