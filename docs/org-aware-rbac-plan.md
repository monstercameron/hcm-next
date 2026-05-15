# Org-Aware RBAC Plan

## Purpose

This document defines the RBAC model for HCM Next after the research pass across standard, international, and non-standard organization structures. The goal is to support normal companies, multinational employers, healthcare networks, nonprofits, cooperatives, franchises, public-sector agencies, matrix teams, and projectized work without hard-coding access around a single department/team/manager tree.

The core design is:

```text
Role = what an actor may do
Org graph = where that role applies
Worker assignment = why a worker belongs to that scope
Relationship = how the actor relates to the worker or unit
Workflow state = whether the assignment/access is proposed, active, expired, or revoked
Field group = which parts of the record are visible
```

Access decisions must answer:

```text
Can actor A perform action X
on resource R
through relationship Z
inside org scope S
for field group F
at time T?
```

## Research Findings

The first research pass covered typical commercial structures:

- Functional organizations: HR, Finance, IT, Operations, Clinical, Sales.
- Divisional organizations: product line, market, customer segment, geography.
- Supervisory organizations: manager/direct-report trees.
- Matrix/project organizations: dotted-line managers, project leads, agile squads.
- Legal/business structures: LLCs, corporations, partnerships, subsidiaries.

The second research pass covered non-standard and international structures:

- Multinational enterprises: subsidiaries, legal employers, country-specific rules, payroll units, value-chain entities.
- Global HCM models: legal entity, business unit, division, department, location, location group, cost center.
- Works councils and unions: representative bodies that are not departments but affect consultation, visibility, and approvals.
- Cooperatives: members, voting rights, ownership, boards, and worker/member relationships.
- Franchises: franchisor, franchisee, outlet, brand, territory, and operating-control relationships.
- Nonprofits: trustee boards, committees, employees, volunteers, patrons, advisors.
- Public sector and universities: agencies, programs, campuses, schools, departments, grants, appointments.
- Projectized work: client assignment, billability, allocations, temporary access, project end dates.

The conclusion is that employee rows cannot own the source-of-truth org model. Employee projections should contain denormalized summaries for fast reads, but the durable model must be an effective-dated org graph plus worker assignments and scoped role bindings.

## Design Principles

- Default deny for all access.
- Database-per-organization remains the hard isolation boundary for customer/demo orgs.
- Tenant IDs remain in every table for query safety and future shared-schema deployments.
- Org placement and access are workflow-managed, effective-dated records.
- Employee projection fields are read-model summaries, not org truth.
- Roles are capability-oriented and small.
- Scopes are data, not role-name suffixes.
- Deny, mask, and redact policies override allow policies.
- Access decisions must be auditable with actor, role binding, scope, field group, workflow, and effective time.
- Loose org intake is valid. Workflows progressively tighten structure.

## Core Objects

### Organization Unit

An organization unit is any durable grouping, legal body, operational unit, governance body, worksite, financial unit, or temporary work container.

Supported unit types include:

```text
enterprise
legal_entity
employing_entity
payroll_unit
business_unit
division
department
team
location
region
cost_center
project
program
clinic
store
franchisee
supplier
joint_venture
board
committee
works_council
union
volunteer_group
member_group
```

Examples:

- `HarborCare Medical Group`
- `HarborCare Medical Group PC`
- `Clinical Operations`
- `Boston Main Clinic`
- `Revenue Cycle`
- `REV-520`
- `Works Council Germany`
- `Trustee Board`
- `Store 218 Franchisee`
- `Patient Portal Rollout`

### Organization Relationship

An organization relationship connects two org units. This is deliberately graph-shaped rather than a single parent pointer.

Supported relationship types include:

```text
part_of
reports_to
owns
controls
employs
operates
funds
governs
represents
franchises
supplies
located_in
allocated_to
```

Examples:

- Clinic is `part_of` Clinical Operations.
- Cost center is `part_of` Finance.
- Franchisee `operates` Store 218.
- Trustee Board `governs` the nonprofit.
- Works Council `represents` employees in Germany.

### Worker Assignment

A worker assignment links a worker to an org unit for a reason and time period.

Assignment types include:

```text
legal_employer
primary_team
work_location
cost_center
project
program
committee
board_seat
volunteer_assignment
member_affiliation
representative_body
```

Every assignment is effective-dated and has lifecycle status:

```text
proposed
pending_approval
active
suspended
expired
revoked
superseded
```

Assignments can include:

- `manager_employee_id`
- `allocation_percent`
- `role_type`
- `source_workflow_instance_id`
- workflow metadata

### Role Binding

A role binding grants a role to an actor within a scope.

Fields:

```text
actor_id
role_key
scope_type
scope_org_unit_id
scope_value
relationship_type
status
effective_start
effective_end
source_workflow_instance_id
metadata
```

Scope types include:

```text
self
direct_reports
manager_chain
org_unit
org_unit_descendants
legal_entity
location
country
cost_center
project
assignment
workflow_instance
global
```

Examples:

```text
role: hr_partner
scope: org_unit_descendants(Boston Main Clinic)
fields: profile, contact, employment
```

```text
role: finance_admin
scope: cost_center(REV-520)
fields: profile, job, organization, compensation
```

```text
role: project_lead
scope: project(Patient Portal Rollout)
relationship: project_assignment
expires: project end date
fields: profile, job, contact
```

## Evaluation Model

The access evaluator should process decisions in this order:

1. Confirm database/tenant isolation.
2. Confirm actor exists and is active.
3. Load active role bindings for the actor at decision time.
4. Load policies for the requested action and field group.
5. Match role binding to policy subject scope.
6. Resolve role binding scope through org graph and worker assignments.
7. Match worker/resource relationship.
8. Apply field-group allow/mask/redact rules.
9. Apply workflow state restrictions.
10. Apply explicit deny rules.
11. Return an auditable decision object.

Decision output should eventually include:

```json
{
  "decision": "allow | deny | mask | redact",
  "actorId": "actor_hr_admin",
  "roleBindingId": "rb_...",
  "policyId": "policy_...",
  "resourceType": "employee",
  "resourceId": "emp_123",
  "action": "employee.profile.view",
  "fieldGroups": ["profile", "employment"],
  "scopeType": "org_unit_descendants",
  "scopeId": "org_boston_main_clinic",
  "relationshipType": "assigned_to",
  "evaluatedAt": "2026-05-15T00:00:00.000Z"
}
```

### HarborCare Org Transfer Decision Example

The `employee.org_transfer_compensation_change` E2E fixture uses Jane Doe
(`emp_123`) moving from Boston Main Clinic to Cambridge Clinic. Before
execution, Morgan Lee (`emp_456`) has direct-report visibility through Jane's
active Boston Nursing assignment. Sofia Rossi (`emp_461`) is the target manager
but does not receive durable direct-report visibility until execution creates
the Cambridge Nursing assignment and recalculates role bindings.

Finance approval uses the target `CLN-CAM` cost-center org unit. The seeded
finance approver (`actor_finance_admin`) is scoped to that cost center for the
approval contract, but contact and emergency-contact fields remain masked.

## Loose Org Intake

The product should allow loose intake. A worker can start with:

```text
legal employer: HarborCare Medical Group PC
location: Boston Main Clinic
function: Clinical
manager: Pending Assignment
department: Pending Assignment
team: Pending Assignment
cost center: Pending Assignment
access status: provisional
```

Then workflows tighten it:

- Assign manager.
- Assign clinic/location.
- Assign department/team.
- Assign cost center.
- Assign legal employer/payroll unit.
- Grant HR, finance, compensation, IT, or compliance access.
- Review access.
- Transfer worker.
- Reorg org unit.
- Expire project assignment.
- Terminate worker and revoke access.

## Database Plan

The initial schema extension adds:

```text
organization_units
organization_relationships
worker_assignments
role_bindings
```

These tables do not replace existing `employee_projection`, `accessGrants`, or `permission_policies` immediately. They introduce the durable model behind future workflow-native access evaluation.

`employee_projection.document.organization` remains a denormalized read summary. It should be regenerated from assignments after the workflow/projector layer is expanded.

## Compatibility Plan

Phase 1:

- Add org graph tables.
- Add TypeScript records.
- Seed HarborCare org units, relationships, assignments, and role bindings.
- Keep existing access grants for current tests and workflow reads.

Phase 2:

- Add an org-aware access evaluator.
- Convert active role bindings into employee access decisions.
- Keep legacy access grants as an adapter.

Phase 3:

- Move seeded employee org fields to looser `Pending Assignment` values.
- Let workflows project approved assignments into employee projection summaries.

Phase 4:

- Add audit decision logs and re-certification workflows.

## HarborCare Seed Direction

HarborCare should include:

- Enterprise unit.
- Legal employer unit.
- Business units: Corporate, Clinical Operations.
- Departments from current seed.
- Teams from current seed.
- Locations: HQ, Boston, Cambridge, Somerville, Quincy, Telehealth.
- Cost centers from current seed.
- Worker assignments for legal employer, primary team, work location, and cost center.
- Role bindings for employee self-service, manager relationships, HR, finance, compensation, clinic operations, medical director, compliance, IT, and executive access.

The current seeded projection may remain structured for demo continuity, but the org graph is the source model for future workflow tightening.
