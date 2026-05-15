# Data Model And Storage Strategy

## 1. Decision

HCM Next will use **Option 3: functional ledger core with materialized projections**.

The system of record is an immutable, append-only ledger of HCM change events. Product state is generated from that ledger into fast, queryable projections.

```text
ledger_events -> projection functions -> employee/change/org/comp projections
```

This gives us the functional programming model we want without making every UI, API, AI review, or report reduce a raw event stream at runtime.

## 2. Core Principle

```text
Truth = immutable events
State = materialized projections
Workflow = typed function composition plus explicit side effects
```

The ledger answers:

- What happened?
- Who did it?
- Why was it allowed?
- Which workflow caused it?
- Which approvals existed?
- Which AI context was visible?
- Which systems were touched?
- What failed or was repaired?

Projections answer:

- What is true now?
- What will be true on an effective date?
- What should the UI/API/AI see quickly?
- What changed between current and proposed state?

## 3. Why Not A Single Mutable Employee Document

A single flexible employee document is attractive for reads, but it is weak as the authoritative truth for enterprise HCM.

Problems:

- Effective-dated changes become fragile.
- Field-level audit is harder.
- Payroll-sensitive transactions need stronger causality.
- Reorgs and manager changes affect many records.
- Shared objects like jobs, positions, departments, pay bands, and policies do not belong inside one employee document.
- Permissions differ by field and fact.
- Rollback, reconciliation, and simulation need event history.

We still want the convenience of a document-shaped employee record, but as a projection, not as the source of truth.

## 4. Storage Layers

### 4.1 Authoritative Ledger

The ledger is append-only and immutable.

Primary table:

```text
ledger_events
```

Purpose:

- Store every meaningful HCM event.
- Preserve causality across workflows, actors, approvals, AI reviews, and integrations.
- Support replay, audit, simulation, reconciliation, rollback, and point-in-time reconstruction.

Example event types:

- ChangeRequestCreated
- ChangeRequestPreflighted
- ApprovalTaskCreated
- ApprovalGranted
- ApprovalRejected
- TransactionPlanCreated
- EmployeeJobChanged
- EmployeeManagerChanged
- CompensationChanged
- ExternalWriteRequested
- ExternalWriteSucceeded
- ExternalWriteFailed
- ReconciliationCompleted
- ManualRepairCreated
- ManualRepairResolved
- AIChangeReviewGenerated

### 4.2 Materialized Projections

Projection tables are rebuildable read models generated from ledger events.

Initial projections:

```text
employee_projection
change_request_projection
position_projection
org_projection
compensation_projection
approval_task_projection
```

The first implementation can collapse some of these into fewer tables, but the conceptual boundary should stay clear.

Projection uses:

- UI reads
- API reads
- AI context assembly
- Permission-filtered views
- Search indexing
- Transaction simulation
- Reconciliation dashboards
- Operational reporting

### 4.3 Metadata And Configuration Tables

Metadata/configuration is not ledger-only. It needs normal versioned tables because it is queried constantly and governs runtime behavior.

Core metadata tables:

```text
tenant
metadata_object_definitions
metadata_field_definitions
workflow_definitions
workflow_versions
permission_policies
ai_policy_definitions
integration_connections
block_definitions
block_versions
```

Changes to metadata should also emit ledger events, but runtime reads should use versioned metadata tables.

## 5. Recommended Database

Use **Postgres first**.

Why:

- Strong transactions
- Append-only ledger support
- JSONB flexibility
- Indexing
- Constraints where needed
- Point-in-time and effective-date queries
- Mature backup and recovery
- Enterprise operational familiarity
- Good enough scale for v1 and design partners

Optional later systems:

- OpenSearch for full-text search
- Object storage for documents and attachments
- Data warehouse for analytics
- Vector store or pgvector for AI retrieval
- Queue/event bus for projection workers and integrations

## 6. Ledger Event Shape

Baseline event fields:

```text
event_id
tenant_id
event_type
event_version
subject_type
subject_id
occurred_at
effective_at
recorded_at
actor_type
actor_id
actor_role
relationship_context
workflow_instance_id
workflow_definition_version
change_request_id
transaction_plan_id
approval_task_id
block_name
block_version
correlation_id
causation_id
idempotency_key
permission_snapshot
ai_visibility_snapshot
payload
previous_hash
event_hash
```

Notes:

- `occurred_at` is when the action happened.
- `effective_at` is when the HCM fact becomes true.
- `recorded_at` is when the platform persisted the event.
- `payload` is JSONB, but validated by event type and version.
- `previous_hash` and `event_hash` support tamper-evident audit chains.

## 7. Projection Shape

Example `employee_projection`:

```text
tenant_id
employee_id
projection_version
as_of_effective_at
source_event_id
source_event_sequence
document
indexed_fields
created_at
updated_at
```

The `document` column can be JSONB:

```json
{
  "employeeId": "emp_123",
  "person": {
    "displayName": "Jane Doe"
  },
  "employment": {
    "status": "Active",
    "legalEntity": "US-001",
    "startDate": "2024-01-15"
  },
  "job": {
    "title": "Senior Engineer",
    "level": "L5",
    "jobFamily": "Engineering"
  },
  "position": {
    "positionId": "pos_789",
    "departmentId": "dept_platform",
    "costCenter": "cc_420"
  },
  "manager": {
    "employeeId": "emp_456"
  },
  "compensation": {
    "salary": 145000,
    "currency": "USD",
    "payFrequency": "Annual"
  },
  "custom": {}
}
```

Sensitive fields should not be blindly served from this document. Reads still pass through permission filtering.

## 8. Write Path

All meaningful changes flow through a change request and ledger append.

```text
user/API/workflow
  -> create or update change request
  -> preflight validation
  -> approval workflow
  -> transaction plan
  -> append ledger events
  -> update projections
  -> reconcile external systems
  -> append reconciliation events
```

For critical user-facing state, ledger append and projection update should happen in the same database transaction where possible.

For external side effects, use the outbox pattern:

```text
ledger event + outbox row -> integration worker -> external API -> result event
```

## 9. Read Path

Most product reads use projections:

```text
UI/API/AI -> permission-aware query -> projection -> field filtering -> response
```

Audit and investigation reads use the ledger:

```text
audit UI -> ledger_events -> timeline / causality / before-after review
```

Simulation reads use current projections plus proposed events:

```text
current projection + proposed transaction events -> temporary projection -> diff
```

## 10. Effective Dating

Effective dating is first-class.

Events can be:

- Current-dated
- Future-dated
- Retroactive

The projection layer must support:

- Current state
- As-of state
- Future scheduled state
- Diff between current and proposed future state
- Detection of effective-date conflicts

Example:

```text
CompensationChanged
occurred_at: 2026-05-15
effective_at: 2026-06-01
```

This event is recorded today but does not become active compensation until June 1.

## 11. Functional Programming Fit

This model fits the desired programming style:

```text
state = reduce(events, reducer)
projection = project(ledger)
simulation = project(ledger + proposed_events)
workflow = compose(blocks)
```

Blocks should be small and typed:

```text
Input -> pure-ish function -> Output
```

Side effects are explicit:

- API calls
- Emails
- AI calls
- External writes
- Document generation

Pure blocks are replayable. Effect blocks are recorded and replayed from captured request/response history.

## 12. Consistency Strategy

The ledger is the source of truth.

Projection consistency levels:

- Strong for critical in-request state, where ledger and projection update together.
- Eventual for search, analytics, external reconciliation dashboards, and non-critical denormalized views.

Every projection row should carry:

- Projection version
- Source event sequence
- Last applied event ID
- Rebuild timestamp

This allows the system to detect stale projections.

## 13. Rebuild And Replay

Projection rebuild should be a standard operation, not an emergency-only tool.

Requirements:

- Rebuild projection for one employee.
- Rebuild projection for one change request.
- Rebuild projection for one tenant.
- Rebuild projection from a checkpoint.
- Compare current projection to rebuilt projection.
- Alert on drift.

Projection code must be deterministic for the same event stream and metadata version.

## 14. MVP Object Model Nodes

The MVP object model should support the top HCM transaction workflows without modeling the full HCM universe.

Core object nodes:

```text
Tenant
Environment
User / Actor
Person
Worker
Employment
Job
Position
OrgUnit
Location
CostCenter
ManagerRelationship
Compensation
ChangeRequest
ProposedChange
TransactionPlan
ApprovalTask
WorkflowDefinition
PolicyRule
PermissionPolicy
ExternalReference
IntegrationConnection
LedgerEvent
Projection
```

Relationship shape:

```text
Tenant
  -> Users / Actors
  -> WorkflowDefinitions
  -> PermissionPolicies
  -> IntegrationConnections

Person
  -> Worker
  -> Employment
  -> Position
  -> Job
  -> OrgUnit
  -> CostCenter
  -> Location
  -> ManagerRelationship
  -> Compensation

ChangeRequest
  -> Target Worker
  -> ProposedChanges[]
  -> ApprovalTasks[]
  -> TransactionPlan
  -> LedgerEvents[]
  -> ExternalReferences[]
```

The most important authored object in ChangeOps v1 is `ChangeRequest`, not `Employee`.

The employee record is a projection assembled from the ledger, canonical facts, external references, and tenant metadata.

## 15. Schema Conventions

All persisted records should be tenant-scoped.

Common fields for mutable operational tables:

```text
id
tenant_id
created_at
updated_at
created_by
updated_by
version
status
metadata jsonb
```

Common fields for effective-dated HCM facts:

```text
effective_from
effective_to
effective_sequence
source_event_id
source_system
external_reference_id
```

Common fields for cross-system traceability:

```text
correlation_id
causation_id
idempotency_key
workflow_instance_id
change_request_id
transaction_plan_id
```

JSONB usage:

- Use typed columns for fields required for filtering, joining, permissions, effective dating, and reconciliation.
- Use JSONB for tenant-specific metadata, source payloads, validation results, AI review output, and external system responses.
- JSONB fields must still be validated by metadata definitions or event schemas.

## 16. MVP Object Schemas

These are product schemas, not final database DDL. Implementation can split fields across tables or projections as needed.

### 16.1 Tenant

Purpose: isolates customer data, metadata, permissions, integrations, and workflow definitions.

Fields:

```text
tenant_id
name
slug
status
default_timezone
default_locale
data_region
created_at
updated_at
settings jsonb
```

### 16.2 Environment

Purpose: separates development, sandbox, staging, and production configuration.

Fields:

```text
environment_id
tenant_id
name
type
status
created_at
updated_at
```

Types:

```text
development
sandbox
staging
production
```

### 16.3 User / Actor

Purpose: represents a human user, service account, integration actor, workflow actor, or AI actor.

Fields:

```text
actor_id
tenant_id
actor_type
linked_worker_id
email
display_name
status
roles jsonb
groups jsonb
auth_provider
external_subject
created_at
updated_at
```

Actor types:

```text
human
service_account
integration
workflow
ai_agent
system
```

### 16.4 Person

Purpose: identity anchor for one human. A person may have one or more worker relationships over time.

Fields:

```text
person_id
tenant_id
legal_name jsonb
display_name
preferred_name
work_email
personal_email
phone_numbers jsonb
addresses jsonb
date_of_birth
government_identifiers_ref
status
custom_fields jsonb
created_at
updated_at
```

Notes:

- Sensitive identity fields should be separately classified and permission-filtered.
- Government identifiers should be tokenized or stored in a restricted secure store if included at all.

### 16.5 Worker

Purpose: employee/contractor work identity.

Fields:

```text
worker_id
tenant_id
person_id
worker_number
worker_type
worker_status
primary_employment_id
primary_position_id
primary_manager_worker_id
hire_date
original_hire_date
termination_date
custom_fields jsonb
created_at
updated_at
```

Worker types:

```text
employee
contractor
intern
temporary
seasonal
vendor
external_collaborator
```

### 16.6 Employment

Purpose: legal and lifecycle employment relationship.

Fields:

```text
employment_id
tenant_id
worker_id
legal_entity_id
employment_status
worker_type
employment_type
fte
hire_date
start_date
termination_date
termination_reason
probation_end_date
location_id
pay_group
effective_from
effective_to
source_event_id
custom_fields jsonb
```

Employment statuses:

```text
pending_hire
active
leave
suspended
terminated
retired
inactive
```

### 16.7 Job

Purpose: defines the work profile, title, level, family, and classification.

Fields:

```text
job_id
tenant_id
job_code
title
job_family
job_level
job_grade
classification
exempt_status
pay_band_id
skills jsonb
status
effective_from
effective_to
custom_fields jsonb
```

### 16.8 Position

Purpose: represents a seat in the organization, usually tied to budget, reporting, and headcount.

Fields:

```text
position_id
tenant_id
position_code
job_id
org_unit_id
cost_center_id
location_id
budget_owner_worker_id
incumbent_worker_id
position_status
fte
headcount_type
effective_from
effective_to
source_event_id
custom_fields jsonb
```

Position statuses:

```text
open
filled
frozen
closed
pending
```

### 16.9 OrgUnit

Purpose: department, team, business unit, region, legal hierarchy, or other organizational grouping.

Fields:

```text
org_unit_id
tenant_id
org_unit_code
name
org_unit_type
parent_org_unit_id
legal_entity_id
cost_center_id
hrbp_worker_id
status
effective_from
effective_to
custom_fields jsonb
```

Org unit types:

```text
company
legal_entity
business_unit
division
department
team
region
location_group
custom
```

### 16.10 Location

Purpose: work location and jurisdiction context.

Fields:

```text
location_id
tenant_id
location_code
name
address jsonb
country
region
state
city
timezone
remote_type
status
custom_fields jsonb
```

### 16.11 CostCenter

Purpose: finance and budget routing context.

Fields:

```text
cost_center_id
tenant_id
cost_center_code
name
parent_cost_center_id
finance_owner_actor_id
budget_owner_worker_id
status
effective_from
effective_to
custom_fields jsonb
```

### 16.12 ManagerRelationship

Purpose: reporting relationship for approvals, access control, org charts, and downstream sync.

Fields:

```text
manager_relationship_id
tenant_id
worker_id
manager_worker_id
relationship_type
relationship_status
effective_from
effective_to
source_event_id
custom_fields jsonb
```

Relationship types:

```text
direct
dotted_line
temporary
project
delegated
```

### 16.13 Compensation

Purpose: pay facts used for compensation approvals, payroll export, simulation, and audit.

Fields:

```text
compensation_id
tenant_id
worker_id
position_id
pay_type
amount
currency
pay_frequency
compensation_plan_id
pay_band_id
bonus_target
equity_eligible
reason_code
effective_from
effective_to
payroll_sync_status
source_event_id
custom_fields jsonb
```

Pay types:

```text
salary
hourly
stipend
contract_rate
commission
```

### 16.14 ChangeRequest

Purpose: central ChangeOps object. Represents a proposed HCM mutation.

Fields:

```text
change_request_id
tenant_id
change_type
target_worker_id
requester_actor_id
effective_at
business_reason
status
priority
current_snapshot jsonb
proposed_snapshot jsonb
preflight_result jsonb
ai_review_id
workflow_definition_id
workflow_version_id
transaction_plan_id
submitted_at
approved_at
executed_at
closed_at
created_at
updated_at
```

Statuses:

```text
draft
needs_data
preflighted
submitted
in_approval
approved
rejected
canceled
simulated
executing
executed
reconciling
reconciled
waiting_repair
closed
superseded
failed
```

Change types:

```text
hire
onboarding
job_change
manager_org_change
compensation_change
promotion
leave
employee_data_change
termination
reorg_batch
custom
```

### 16.15 ProposedChange

Purpose: field-level or domain-level proposed mutation inside a change request.

Fields:

```text
proposed_change_id
tenant_id
change_request_id
target_object_type
target_object_id
field_path
current_value jsonb
proposed_value jsonb
effective_at
reason_code
validation_status
risk_level
metadata jsonb
```

Examples:

```text
worker.job.title: "Engineer II" -> "Senior Engineer"
worker.manager.worker_id: "emp_123" -> "emp_456"
compensation.amount: 120000 -> 140000
position.cost_center_id: "cc_old" -> "cc_new"
```

### 16.16 TransactionPlan

Purpose: executable and auditable plan created after preflight and approval.

Fields:

```text
transaction_plan_id
tenant_id
change_request_id
status
plan_version
steps jsonb
internal_writes jsonb
external_writes jsonb
rollback_plan jsonb
compensation_plan jsonb
idempotency_keys jsonb
simulation_result jsonb
execution_result jsonb
reconciliation_result jsonb
created_at
updated_at
```

Step types:

```text
validate
approval_check
append_event
update_projection
external_write
notification
reconciliation
manual_repair
rollback
compensation
```

### 16.17 ApprovalTask

Purpose: assigned human or system approval step.

Fields:

```text
approval_task_id
tenant_id
change_request_id
workflow_instance_id
assignee_actor_id
assignee_role
assignee_relationship
approval_type
status
decision
decision_reason
comments
due_at
delegated_to_actor_id
created_at
decided_at
```

Statuses:

```text
pending
approved
rejected
delegated
expired
canceled
skipped
```

### 16.18 WorkflowDefinition

Purpose: versioned workflow config for routing, validation, UX, approvals, AI review, and transaction planning.

Fields:

```text
workflow_definition_id
tenant_id
name
workflow_type
status
current_version_id
created_at
updated_at
```

Workflow version fields:

```text
workflow_version_id
workflow_definition_id
version_number
graph_definition jsonb
input_schema jsonb
output_schema jsonb
validation_rules jsonb
approval_rules jsonb
ai_review_scope jsonb
published_by
published_at
status
```

### 16.19 PolicyRule

Purpose: tenant-specific business policy used by validation, approval routing, and simulation.

Fields:

```text
policy_rule_id
tenant_id
name
policy_type
scope jsonb
condition jsonb
decision jsonb
severity
status
effective_from
effective_to
created_at
updated_at
```

Policy types:

```text
compensation_threshold
pay_band
approval_route
payroll_cutoff
job_eligibility
manager_change
location_rule
security_rule
custom
```

### 16.20 PermissionPolicy

Purpose: RBAC, relationship access, field permissions, action permissions, and AI visibility.

Fields:

```text
permission_policy_id
tenant_id
name
subject_scope jsonb
resource_scope jsonb
actions jsonb
field_permissions jsonb
relationship_rules jsonb
attribute_rules jsonb
ai_visibility_rules jsonb
effect
priority
status
created_at
updated_at
```

Effects:

```text
allow
deny
require_approval
mask
redact
```

### 16.21 ExternalReference

Purpose: maps HCM Next objects to external systems.

Fields:

```text
external_reference_id
tenant_id
object_type
object_id
external_system
external_object_type
external_object_id
external_url
source_of_truth_rank
last_synced_at
sync_status
metadata jsonb
```

### 16.22 IntegrationConnection

Purpose: connection to UKG, Workday, payroll, identity, finance, ATS, Slack/Teams, SFTP, webhook, or customer API.

Fields:

```text
integration_connection_id
tenant_id
name
system_type
auth_type
secret_ref
base_url
status
capabilities jsonb
rate_limits jsonb
last_health_check_at
created_at
updated_at
```

### 16.23 LedgerEvent

Purpose: immutable source of truth for causality and audit.

Use the shape defined in section 6.

Critical payload categories:

```text
change_request_event
approval_event
transaction_event
projection_event
integration_event
ai_review_event
permission_event
failure_event
repair_event
metadata_event
```

### 16.24 Projection

Purpose: rebuildable read model.

Initial projection types:

```text
employee_projection
change_request_projection
position_projection
org_projection
compensation_projection
approval_task_projection
```

## 17. Top 10 HCM Workflows And Required Data

These workflows define the minimum data model coverage for an HCM platform. ChangeOps v1 should start with workflows 3, 4, 5, and 6.

### 17.1 Hire / Convert Candidate To Employee

Primary transaction:

```text
Create worker, employment, job/position assignment, compensation, onboarding state, and external references.
```

Required data:

- Person identity: legal name, preferred name, email, phone, address.
- Candidate reference: ATS ID, application ID, requisition ID, offer ID.
- Worker type: employee, contractor, intern, temporary.
- Legal entity.
- Employment type: full-time, part-time, contract.
- Hire/start date.
- Job and position.
- Manager.
- Org unit, department, location, cost center.
- Compensation and pay group.
- Work authorization status.
- Required documents.
- Background check status, if applicable.
- Offer acceptance status.
- External system references.

Outputs:

- Worker record.
- Employment fact.
- Position assignment.
- Compensation fact.
- Onboarding workflow.
- Payroll setup task/export.
- Identity provisioning task.
- Benefits eligibility event.
- Ledger events.

### 17.2 Onboarding

Primary transaction:

```text
Complete required setup before start date.
```

Required data:

- Worker and employment record.
- Start date.
- Job, position, location.
- Manager.
- Equipment needs.
- System access needs.
- Required forms.
- Required policy acknowledgements.
- Work authorization requirements.
- Payroll setup status.
- Benefits eligibility.
- Orientation/training assignments.
- Task owners and due dates.

Outputs:

- Task checklist.
- Document collection.
- Account provisioning requests.
- Equipment requests.
- Payroll readiness status.
- Benefits enrollment trigger.
- Completion evidence.

### 17.3 Job / Title / Level Change

Primary transaction:

```text
Update job, title, level, and employment classification facts.
```

Required data:

- Current job.
- Proposed job.
- Current title.
- Proposed title.
- Current level/grade.
- Proposed level/grade.
- Job family.
- Employment classification.
- Effective date.
- Reason code.
- Manager.
- Position.
- Department/org unit.
- Policy eligibility.
- Approval rules.

Outputs:

- Job fact change.
- Employment classification change, if needed.
- Position update, if needed.
- Approval record.
- Downstream sync to HCM, payroll, identity, and reporting.

### 17.4 Manager / Org / Cost Center Change

Primary transaction:

```text
Update reporting, org assignment, position, and finance routing facts.
```

Required data:

- Current manager.
- Proposed manager.
- Relationship type: direct, dotted-line, temporary, project.
- Current org unit.
- Proposed org unit.
- Current cost center.
- Proposed cost center.
- Current position.
- Proposed position, if applicable.
- Effective date.
- Budget owner.
- HRBP owner.
- Approval rules.
- Downstream access implications.

Outputs:

- Manager relationship fact.
- Org assignment fact.
- Cost center assignment fact.
- Reporting chain update.
- Access/provisioning impact.
- Finance/reporting sync.

### 17.5 Compensation Change

Primary transaction:

```text
Update salary, hourly rate, pay frequency, bonus, equity eligibility, or other pay facts.
```

Required data:

- Current salary/rate.
- Proposed salary/rate.
- Currency.
- Pay frequency.
- Pay type.
- Effective date.
- Pay group.
- Payroll cutoff calendar.
- Compensation plan.
- Pay band/range.
- Bonus/equity eligibility.
- Internal equity context, permission permitting.
- Budget impact.
- Reason code.
- Approval threshold rules.
- Permission scope.

Outputs:

- Compensation fact.
- Payroll transaction/export.
- Budget impact record.
- Approval history.
- Employee notification, if configured.
- Audit/reconciliation events.

### 17.6 Promotion

Primary transaction:

```text
Coordinated job, level, compensation, position, and approval transaction.
```

Required data:

- Current and proposed job.
- Current and proposed title.
- Current and proposed level.
- Current and proposed compensation.
- Performance context, if permitted.
- Promotion reason.
- Effective date.
- Manager chain.
- Department/org unit.
- Cost center.
- Pay band.
- Budget availability.
- Compensation policy.
- Approval chain.
- Payroll cutoff.

Outputs:

- Job change.
- Level change.
- Compensation change.
- Possible position change.
- Approval package.
- AI change review.
- Payroll and reporting sync.

This is the best flagship workflow for ChangeOps because it combines job, compensation, approvals, effective dating, permission filtering, simulation, and reconciliation.

### 17.7 Leave Of Absence / Time Off

Primary transaction:

```text
Create absence case, time facts, payroll impact, benefits impact, and return-to-work path.
```

Required data:

- Worker.
- Employment status.
- Location/jurisdiction.
- Work schedule.
- Leave type.
- Requested dates.
- Leave balance.
- Accrual rules.
- Eligibility rules.
- Prior leave history.
- Required evidence/documents.
- Manager.
- HR/benefits owner.
- Payroll impact.
- Benefits continuation rules.
- Privacy classification.

Outputs:

- Leave case.
- Time/absence records.
- Payroll instructions.
- Benefits flags.
- Manager coverage tasks.
- Return-to-work workflow.
- Compliance evidence.

This should be later than job/comp changes because privacy and regulation are heavier.

### 17.8 Employee Data Change

Primary transaction:

```text
Update person, contact, tax, work authorization, or other profile facts.
```

Required data:

- Current person data.
- Proposed person data.
- Field sensitivity.
- Actor permission.
- Approval requirement.
- Evidence requirement.
- Effective date, if applicable.
- External system mappings.

Examples:

- Name change.
- Address change.
- Emergency contact.
- Tax info.
- Work authorization.
- Bank/direct deposit, if in scope.
- Preferred name.
- Contact info.

Outputs:

- Person fact update.
- Document/evidence record.
- Payroll/tax sync, if relevant.
- Audit event.

### 17.9 Termination / Offboarding

Primary transaction:

```text
End employment and trigger downstream removal, payroll, legal, benefits, and asset workflows.
```

Required data:

- Worker.
- Employment record.
- Termination date.
- Termination reason.
- Voluntary/involuntary flag.
- Last day worked.
- Final pay requirements.
- Benefits end date.
- Legal hold check.
- Equipment/access list.
- Manager.
- HR approver.
- Payroll cutoff.
- Severance info, if applicable.
- Rehire eligibility.
- Required documents/notices.

Outputs:

- Employment termination fact.
- Final pay task/export.
- Benefits termination event.
- Identity/access removal.
- Equipment return task.
- Exit interview.
- Legal/compliance records.
- Audit trail.

### 17.10 Reorganization / Batch Change

Primary transaction:

```text
Apply many org, manager, position, department, and cost center changes together.
```

Required data:

- Source org structure.
- Proposed org structure.
- Affected workers.
- Positions.
- Managers.
- Departments/org units.
- Cost centers.
- Legal entities.
- Effective date.
- Batch reason.
- Approval rules.
- Simulation results.
- Exception list.
- Downstream system impact.

Outputs:

- Many manager/org/position changes.
- Batch transaction plan.
- Exception report.
- Approval package.
- Payroll/finance/identity sync.
- Reconciliation report.
- Audit package.

This is valuable but should follow the single-worker ChangeOps workflows because it requires batch simulation and repair tooling.

## 18. Workflow-To-Object Coverage Matrix

| Workflow                           | Required Core Nodes                                                                                                                     |
| ---------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Hire / Convert                     | Person, Worker, Employment, Job, Position, OrgUnit, Location, CostCenter, ManagerRelationship, Compensation, ExternalReference          |
| Onboarding                         | Worker, Employment, Position, ManagerRelationship, ApprovalTask, WorkflowDefinition, ExternalReference                                  |
| Job / Title / Level Change         | Worker, Employment, Job, Position, OrgUnit, ChangeRequest, ProposedChange, ApprovalTask, TransactionPlan                                |
| Manager / Org / Cost Center Change | Worker, Position, OrgUnit, CostCenter, ManagerRelationship, ChangeRequest, ProposedChange, TransactionPlan                              |
| Compensation Change                | Worker, Employment, Compensation, Position, CostCenter, ChangeRequest, ApprovalTask, TransactionPlan                                    |
| Promotion                          | Worker, Employment, Job, Position, OrgUnit, CostCenter, ManagerRelationship, Compensation, ChangeRequest, ApprovalTask, TransactionPlan |
| Leave / Time Off                   | Worker, Employment, Location, ManagerRelationship, PolicyRule, ChangeRequest, ApprovalTask                                              |
| Employee Data Change               | Person, Worker, PermissionPolicy, ChangeRequest, ProposedChange, ExternalReference                                                      |
| Termination / Offboarding          | Worker, Employment, ManagerRelationship, Compensation, IntegrationConnection, ChangeRequest, TransactionPlan                            |
| Reorganization / Batch Change      | Worker, Position, OrgUnit, CostCenter, ManagerRelationship, ChangeRequest, TransactionPlan, LedgerEvent                                 |

## 19. Concrete V1 Table Schemas

These are the concrete PostgreSQL table shapes for ChangeOps v1.

The schemas are intentionally narrow. They support:

- Functional ledger core.
- Materialized projections.
- Employee change requests.
- Proposed field/domain changes.
- Transaction planning.
- Approval workflows.
- Permission-aware AI review.
- Integration outbox and reconciliation.

These are implementation-ready starting schemas, not the final enterprise schema.

### 19.1 Database Conventions

Required extension:

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

Conventions:

- Primary keys are `uuid` with `gen_random_uuid()`.
- All tenant-owned rows include `tenant_id`.
- Timestamps use `timestamptz`.
- Flexible payloads use `jsonb`.
- Most business status fields are `text` with check constraints in v1.
- Later migrations can replace repeated status strings with PostgreSQL enums if useful.

### 19.2 Tenants

```sql
CREATE TABLE tenants (
  tenant_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  slug text NOT NULL UNIQUE,
  status text NOT NULL DEFAULT 'active',
  default_timezone text NOT NULL DEFAULT 'UTC',
  default_locale text NOT NULL DEFAULT 'en-US',
  data_region text,
  settings jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT tenants_status_check
    CHECK (status IN ('active', 'inactive', 'suspended', 'deleted'))
);
```

### 19.3 Environments

```sql
CREATE TABLE environments (
  environment_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  type text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT environments_type_check
    CHECK (type IN ('development', 'sandbox', 'staging', 'production')),
  CONSTRAINT environments_status_check
    CHECK (status IN ('active', 'inactive', 'deleted')),
  CONSTRAINT environments_tenant_name_unique
    UNIQUE (tenant_id, name)
);

CREATE INDEX environments_tenant_idx ON environments (tenant_id);
```

### 19.4 Actors

Actors represent human users, service accounts, integrations, workflows, AI agents, and system actors.

```sql
CREATE TABLE actors (
  actor_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  actor_type text NOT NULL,
  linked_worker_id text,
  email text,
  display_name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  roles jsonb NOT NULL DEFAULT '[]'::jsonb,
  groups jsonb NOT NULL DEFAULT '[]'::jsonb,
  auth_provider text,
  external_subject text,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT actors_type_check
    CHECK (actor_type IN ('human', 'service_account', 'integration', 'workflow', 'ai_agent', 'system')),
  CONSTRAINT actors_status_check
    CHECK (status IN ('active', 'inactive', 'suspended', 'deleted'))
);

CREATE INDEX actors_tenant_idx ON actors (tenant_id);
CREATE INDEX actors_tenant_email_idx ON actors (tenant_id, email);
CREATE INDEX actors_linked_worker_idx ON actors (tenant_id, linked_worker_id);
```

`linked_worker_id` is text in v1 because worker state is projection-backed. If worker facts become first-class tables, this can become a foreign key.

### 19.5 Ledger Events

The ledger is the authoritative immutable history.

```sql
CREATE TABLE ledger_events (
  event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid REFERENCES environments(environment_id),

  event_type text NOT NULL,
  event_version integer NOT NULL DEFAULT 1,
  event_sequence bigint GENERATED ALWAYS AS IDENTITY,

  subject_type text NOT NULL,
  subject_id text NOT NULL,

  occurred_at timestamptz NOT NULL DEFAULT now(),
  effective_at timestamptz,
  recorded_at timestamptz NOT NULL DEFAULT now(),

  actor_type text NOT NULL,
  actor_id uuid REFERENCES actors(actor_id),
  actor_role text,
  relationship_context jsonb NOT NULL DEFAULT '{}'::jsonb,

  workflow_instance_id uuid,
  workflow_definition_id uuid,
  workflow_version_id uuid,
  change_request_id uuid,
  transaction_plan_id uuid,
  approval_task_id uuid,

  block_name text,
  block_version text,

  correlation_id text NOT NULL,
  causation_id uuid,
  idempotency_key text,

  permission_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  ai_visibility_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,

  previous_hash text,
  event_hash text,

  CONSTRAINT ledger_events_actor_type_check
    CHECK (actor_type IN ('human', 'service_account', 'integration', 'workflow', 'ai_agent', 'system')),
  CONSTRAINT ledger_events_payload_object_check
    CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX ledger_events_tenant_sequence_idx
  ON ledger_events (tenant_id, event_sequence);

CREATE INDEX ledger_events_subject_idx
  ON ledger_events (tenant_id, subject_type, subject_id, event_sequence);

CREATE INDEX ledger_events_change_request_idx
  ON ledger_events (tenant_id, change_request_id, event_sequence);

CREATE INDEX ledger_events_correlation_idx
  ON ledger_events (tenant_id, correlation_id);

CREATE UNIQUE INDEX ledger_events_idempotency_unique_idx
  ON ledger_events (tenant_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;
```

Application rule: `ledger_events` is append-only. No update/delete paths except controlled administrative retention tooling, if ever legally required.

### 19.6 Change Requests

Central ChangeOps object.

```sql
CREATE TABLE change_requests (
  change_request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid REFERENCES environments(environment_id),

  change_type text NOT NULL,
  target_worker_id text NOT NULL,
  requester_actor_id uuid NOT NULL REFERENCES actors(actor_id),

  effective_at timestamptz,
  business_reason text,
  status text NOT NULL DEFAULT 'draft',
  priority text NOT NULL DEFAULT 'normal',

  current_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  proposed_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  preflight_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  ai_review jsonb NOT NULL DEFAULT '{}'::jsonb,

  workflow_definition_id uuid,
  workflow_version_id uuid,
  transaction_plan_id uuid,

  submitted_at timestamptz,
  approved_at timestamptz,
  executed_at timestamptz,
  closed_at timestamptz,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid REFERENCES actors(actor_id),
  updated_by uuid REFERENCES actors(actor_id),
  version integer NOT NULL DEFAULT 1,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT change_requests_change_type_check
    CHECK (change_type IN (
      'hire',
      'onboarding',
      'job_change',
      'manager_org_change',
      'compensation_change',
      'promotion',
      'leave',
      'employee_data_change',
      'termination',
      'reorg_batch',
      'custom'
    )),
  CONSTRAINT change_requests_status_check
    CHECK (status IN (
      'draft',
      'needs_data',
      'preflighted',
      'submitted',
      'in_approval',
      'approved',
      'rejected',
      'canceled',
      'simulated',
      'executing',
      'executed',
      'reconciling',
      'reconciled',
      'waiting_repair',
      'closed',
      'superseded',
      'failed'
    )),
  CONSTRAINT change_requests_priority_check
    CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);

CREATE INDEX change_requests_tenant_status_idx
  ON change_requests (tenant_id, status, created_at DESC);

CREATE INDEX change_requests_target_worker_idx
  ON change_requests (tenant_id, target_worker_id, created_at DESC);

CREATE INDEX change_requests_type_idx
  ON change_requests (tenant_id, change_type, created_at DESC);
```

### 19.7 Proposed Changes

Field-level or domain-level mutations inside a change request.

```sql
CREATE TABLE proposed_changes (
  proposed_change_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  change_request_id uuid NOT NULL REFERENCES change_requests(change_request_id) ON DELETE CASCADE,

  target_object_type text NOT NULL,
  target_object_id text NOT NULL,
  field_path text NOT NULL,

  current_value jsonb,
  proposed_value jsonb NOT NULL,
  effective_at timestamptz,

  reason_code text,
  validation_status text NOT NULL DEFAULT 'pending',
  risk_level text NOT NULL DEFAULT 'unknown',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT proposed_changes_validation_status_check
    CHECK (validation_status IN ('pending', 'valid', 'invalid', 'warning', 'skipped')),
  CONSTRAINT proposed_changes_risk_level_check
    CHECK (risk_level IN ('unknown', 'low', 'medium', 'high', 'critical'))
);

CREATE INDEX proposed_changes_request_idx
  ON proposed_changes (tenant_id, change_request_id);

CREATE INDEX proposed_changes_target_idx
  ON proposed_changes (tenant_id, target_object_type, target_object_id);

CREATE INDEX proposed_changes_field_path_idx
  ON proposed_changes (tenant_id, field_path);
```

### 19.8 Transaction Plans

Executable plan for internal writes, external writes, rollback, compensation, simulation, and reconciliation.

```sql
CREATE TABLE transaction_plans (
  transaction_plan_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  change_request_id uuid NOT NULL REFERENCES change_requests(change_request_id) ON DELETE CASCADE,

  status text NOT NULL DEFAULT 'draft',
  plan_version integer NOT NULL DEFAULT 1,

  steps jsonb NOT NULL DEFAULT '[]'::jsonb,
  internal_writes jsonb NOT NULL DEFAULT '[]'::jsonb,
  external_writes jsonb NOT NULL DEFAULT '[]'::jsonb,
  rollback_plan jsonb NOT NULL DEFAULT '{}'::jsonb,
  compensation_plan jsonb NOT NULL DEFAULT '{}'::jsonb,
  idempotency_keys jsonb NOT NULL DEFAULT '{}'::jsonb,
  simulation_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  execution_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  reconciliation_result jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid REFERENCES actors(actor_id),
  updated_by uuid REFERENCES actors(actor_id),

  CONSTRAINT transaction_plans_status_check
    CHECK (status IN ('draft', 'simulated', 'ready', 'executing', 'executed', 'reconciling', 'reconciled', 'failed', 'canceled')),
  CONSTRAINT transaction_plans_steps_array_check
    CHECK (jsonb_typeof(steps) = 'array'),
  CONSTRAINT transaction_plans_internal_writes_array_check
    CHECK (jsonb_typeof(internal_writes) = 'array'),
  CONSTRAINT transaction_plans_external_writes_array_check
    CHECK (jsonb_typeof(external_writes) = 'array')
);

CREATE INDEX transaction_plans_request_idx
  ON transaction_plans (tenant_id, change_request_id);

CREATE INDEX transaction_plans_status_idx
  ON transaction_plans (tenant_id, status, created_at DESC);
```

### 19.9 Approval Tasks

Approval tasks created by workflow routing.

```sql
CREATE TABLE approval_tasks (
  approval_task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  change_request_id uuid NOT NULL REFERENCES change_requests(change_request_id) ON DELETE CASCADE,

  workflow_instance_id uuid,
  assignee_actor_id uuid REFERENCES actors(actor_id),
  assignee_role text,
  assignee_relationship text,
  approval_type text NOT NULL DEFAULT 'standard',

  status text NOT NULL DEFAULT 'pending',
  decision text,
  decision_reason text,
  comments text,

  due_at timestamptz,
  delegated_to_actor_id uuid REFERENCES actors(actor_id),

  created_at timestamptz NOT NULL DEFAULT now(),
  decided_at timestamptz,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT approval_tasks_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'delegated', 'expired', 'canceled', 'skipped')),
  CONSTRAINT approval_tasks_decision_check
    CHECK (decision IS NULL OR decision IN ('approved', 'rejected', 'delegated', 'skipped'))
);

CREATE INDEX approval_tasks_request_idx
  ON approval_tasks (tenant_id, change_request_id);

CREATE INDEX approval_tasks_assignee_idx
  ON approval_tasks (tenant_id, assignee_actor_id, status, due_at);

CREATE INDEX approval_tasks_status_idx
  ON approval_tasks (tenant_id, status, due_at);
```

### 19.10 Projection Tables

Projection tables are rebuildable read models. They are not the immutable source of truth.

#### 19.10.1 Employee Projection

```sql
CREATE TABLE employee_projection (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  employee_id text NOT NULL,

  projection_version integer NOT NULL DEFAULT 1,
  as_of_effective_at timestamptz,
  source_event_id uuid REFERENCES ledger_events(event_id),
  source_event_sequence bigint,

  document jsonb NOT NULL DEFAULT '{}'::jsonb,
  indexed_fields jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (tenant_id, employee_id)
);

CREATE INDEX employee_projection_updated_idx
  ON employee_projection (tenant_id, updated_at DESC);

CREATE INDEX employee_projection_document_gin_idx
  ON employee_projection USING gin (document);

CREATE INDEX employee_projection_indexed_fields_gin_idx
  ON employee_projection USING gin (indexed_fields);
```

#### 19.10.2 Change Request Projection

```sql
CREATE TABLE change_request_projection (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  change_request_id uuid NOT NULL REFERENCES change_requests(change_request_id) ON DELETE CASCADE,

  projection_version integer NOT NULL DEFAULT 1,
  source_event_id uuid REFERENCES ledger_events(event_id),
  source_event_sequence bigint,

  status text NOT NULL,
  target_worker_id text NOT NULL,
  change_type text NOT NULL,
  effective_at timestamptz,
  summary jsonb NOT NULL DEFAULT '{}'::jsonb,
  document jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (tenant_id, change_request_id)
);

CREATE INDEX change_request_projection_status_idx
  ON change_request_projection (tenant_id, status, updated_at DESC);

CREATE INDEX change_request_projection_worker_idx
  ON change_request_projection (tenant_id, target_worker_id, updated_at DESC);
```

#### 19.10.3 Position Projection

```sql
CREATE TABLE position_projection (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  position_id text NOT NULL,

  projection_version integer NOT NULL DEFAULT 1,
  source_event_id uuid REFERENCES ledger_events(event_id),
  source_event_sequence bigint,

  org_unit_id text,
  cost_center_id text,
  incumbent_worker_id text,
  manager_worker_id text,
  status text,
  document jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (tenant_id, position_id)
);

CREATE INDEX position_projection_org_idx
  ON position_projection (tenant_id, org_unit_id);

CREATE INDEX position_projection_incumbent_idx
  ON position_projection (tenant_id, incumbent_worker_id);
```

#### 19.10.4 Org Projection

```sql
CREATE TABLE org_projection (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  org_unit_id text NOT NULL,

  projection_version integer NOT NULL DEFAULT 1,
  source_event_id uuid REFERENCES ledger_events(event_id),
  source_event_sequence bigint,

  parent_org_unit_id text,
  cost_center_id text,
  name text NOT NULL,
  org_unit_type text,
  status text,
  document jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (tenant_id, org_unit_id)
);

CREATE INDEX org_projection_parent_idx
  ON org_projection (tenant_id, parent_org_unit_id);

CREATE INDEX org_projection_type_idx
  ON org_projection (tenant_id, org_unit_type);
```

#### 19.10.5 Compensation Projection

```sql
CREATE TABLE compensation_projection (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  compensation_id text NOT NULL,

  worker_id text NOT NULL,
  position_id text,
  pay_type text,
  amount numeric(18, 4),
  currency text,
  pay_frequency text,
  effective_from timestamptz,
  effective_to timestamptz,
  payroll_sync_status text,

  projection_version integer NOT NULL DEFAULT 1,
  source_event_id uuid REFERENCES ledger_events(event_id),
  source_event_sequence bigint,
  document jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (tenant_id, compensation_id)
);

CREATE INDEX compensation_projection_worker_idx
  ON compensation_projection (tenant_id, worker_id, effective_from DESC);

CREATE INDEX compensation_projection_effective_idx
  ON compensation_projection (tenant_id, effective_from, effective_to);
```

### 19.11 Workflow Definitions And Versions

```sql
CREATE TABLE workflow_definitions (
  workflow_definition_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  workflow_type text NOT NULL,
  status text NOT NULL DEFAULT 'draft',
  current_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT workflow_definitions_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'archived')),
  CONSTRAINT workflow_definitions_tenant_name_unique
    UNIQUE (tenant_id, name)
);

CREATE TABLE workflow_versions (
  workflow_version_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workflow_definition_id uuid NOT NULL REFERENCES workflow_definitions(workflow_definition_id) ON DELETE CASCADE,

  version_number integer NOT NULL,
  graph_definition jsonb NOT NULL DEFAULT '{}'::jsonb,
  input_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  output_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  validation_rules jsonb NOT NULL DEFAULT '[]'::jsonb,
  approval_rules jsonb NOT NULL DEFAULT '[]'::jsonb,
  ai_review_scope jsonb NOT NULL DEFAULT '{}'::jsonb,

  status text NOT NULL DEFAULT 'draft',
  published_by uuid REFERENCES actors(actor_id),
  published_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_versions_status_check
    CHECK (status IN ('draft', 'published', 'retired')),
  CONSTRAINT workflow_versions_unique
    UNIQUE (workflow_definition_id, version_number)
);

CREATE INDEX workflow_versions_definition_idx
  ON workflow_versions (tenant_id, workflow_definition_id, version_number DESC);
```

### 19.12 Policy Rules

```sql
CREATE TABLE policy_rules (
  policy_rule_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  policy_type text NOT NULL,
  scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  condition jsonb NOT NULL DEFAULT '{}'::jsonb,
  decision jsonb NOT NULL DEFAULT '{}'::jsonb,
  severity text NOT NULL DEFAULT 'info',
  status text NOT NULL DEFAULT 'active',
  effective_from timestamptz,
  effective_to timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT policy_rules_severity_check
    CHECK (severity IN ('info', 'warning', 'blocking', 'critical')),
  CONSTRAINT policy_rules_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'archived'))
);

CREATE INDEX policy_rules_tenant_type_idx
  ON policy_rules (tenant_id, policy_type, status);
```

### 19.13 Permission Policies

```sql
CREATE TABLE permission_policies (
  permission_policy_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,

  subject_scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  resource_scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  actions jsonb NOT NULL DEFAULT '[]'::jsonb,
  field_permissions jsonb NOT NULL DEFAULT '{}'::jsonb,
  relationship_rules jsonb NOT NULL DEFAULT '{}'::jsonb,
  attribute_rules jsonb NOT NULL DEFAULT '{}'::jsonb,
  ai_visibility_rules jsonb NOT NULL DEFAULT '{}'::jsonb,

  effect text NOT NULL DEFAULT 'allow',
  priority integer NOT NULL DEFAULT 100,
  status text NOT NULL DEFAULT 'active',

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT permission_policies_effect_check
    CHECK (effect IN ('allow', 'deny', 'require_approval', 'mask', 'redact')),
  CONSTRAINT permission_policies_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'archived'))
);

CREATE INDEX permission_policies_tenant_status_idx
  ON permission_policies (tenant_id, status, priority);
```

### 19.14 Metadata Field Definitions

Tenant-defined extension fields.

```sql
CREATE TABLE metadata_field_definitions (
  metadata_field_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),

  object_type text NOT NULL,
  field_key text NOT NULL,
  label text NOT NULL,
  data_type text NOT NULL,

  required boolean NOT NULL DEFAULT false,
  sensitive boolean NOT NULL DEFAULT false,
  searchable boolean NOT NULL DEFAULT false,
  effective_dated boolean NOT NULL DEFAULT false,

  validation jsonb NOT NULL DEFAULT '{}'::jsonb,
  default_value jsonb,
  permission_tags jsonb NOT NULL DEFAULT '[]'::jsonb,
  status text NOT NULL DEFAULT 'active',

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT metadata_field_definitions_data_type_check
    CHECK (data_type IN ('string', 'number', 'boolean', 'date', 'datetime', 'enum', 'object', 'array', 'reference', 'money')),
  CONSTRAINT metadata_field_definitions_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'archived')),
  CONSTRAINT metadata_field_definitions_unique
    UNIQUE (tenant_id, object_type, field_key)
);

CREATE INDEX metadata_field_definitions_object_idx
  ON metadata_field_definitions (tenant_id, object_type, status);
```

### 19.15 External References

Maps internal objects to external systems.

```sql
CREATE TABLE external_references (
  external_reference_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),

  object_type text NOT NULL,
  object_id text NOT NULL,
  external_system text NOT NULL,
  external_object_type text NOT NULL,
  external_object_id text NOT NULL,
  external_url text,
  source_of_truth_rank integer,
  last_synced_at timestamptz,
  sync_status text NOT NULL DEFAULT 'unknown',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT external_references_sync_status_check
    CHECK (sync_status IN ('unknown', 'synced', 'stale', 'failed', 'conflict')),
  CONSTRAINT external_references_unique
    UNIQUE (tenant_id, external_system, external_object_type, external_object_id)
);

CREATE INDEX external_references_internal_idx
  ON external_references (tenant_id, object_type, object_id);
```

### 19.16 Integration Connections

```sql
CREATE TABLE integration_connections (
  integration_connection_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),

  name text NOT NULL,
  system_type text NOT NULL,
  auth_type text NOT NULL,
  secret_ref text,
  base_url text,
  status text NOT NULL DEFAULT 'draft',
  capabilities jsonb NOT NULL DEFAULT '{}'::jsonb,
  rate_limits jsonb NOT NULL DEFAULT '{}'::jsonb,
  last_health_check_at timestamptz,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT integration_connections_auth_type_check
    CHECK (auth_type IN ('none', 'api_key', 'oauth2', 'basic', 'saml', 'custom')),
  CONSTRAINT integration_connections_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'failed', 'deleted'))
);

CREATE INDEX integration_connections_tenant_type_idx
  ON integration_connections (tenant_id, system_type, status);
```

### 19.17 Integration Outbox

Outbox rows drive external side effects after ledger append.

```sql
CREATE TABLE integration_outbox (
  outbox_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  integration_connection_id uuid REFERENCES integration_connections(integration_connection_id),

  change_request_id uuid REFERENCES change_requests(change_request_id),
  transaction_plan_id uuid REFERENCES transaction_plans(transaction_plan_id),
  ledger_event_id uuid REFERENCES ledger_events(event_id),

  destination text NOT NULL,
  operation text NOT NULL,
  request_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  response_payload jsonb,

  status text NOT NULL DEFAULT 'pending',
  attempt_count integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 5,
  next_attempt_at timestamptz,
  last_attempt_at timestamptz,
  error_code text,
  error_message text,
  idempotency_key text NOT NULL,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT integration_outbox_status_check
    CHECK (status IN ('pending', 'processing', 'succeeded', 'failed', 'dead_letter', 'canceled')),
  CONSTRAINT integration_outbox_attempts_check
    CHECK (attempt_count >= 0 AND max_attempts > 0),
  CONSTRAINT integration_outbox_idempotency_unique
    UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX integration_outbox_pending_idx
  ON integration_outbox (status, next_attempt_at, created_at)
  WHERE status IN ('pending', 'failed');

CREATE INDEX integration_outbox_request_idx
  ON integration_outbox (tenant_id, change_request_id);
```

### 19.18 Future Fact Tables

Do not create these until projection and query pressure justify them.

```text
person_facts
worker_facts
employment_facts
job_facts
position_facts
org_unit_facts
manager_relationship_facts
compensation_facts
```

The ledger should remain the write authority. Fact tables, if added, are typed projections optimized for queries and constraints.

## 20. Non-Negotiables

- The ledger is append-only.
- No direct mutation of authoritative history.
- Every event has actor, tenant, permission, causation, and correlation context.
- Projections are rebuildable.
- Projection reads are permission-filtered.
- Effective dates are first-class.
- External side effects are idempotent and reconciled.
- AI reads only permitted projection slices.
- Simulation never commits facts until approval/execution.
- Failed transactions produce ledger events, not just logs.
