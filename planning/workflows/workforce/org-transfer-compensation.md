# Organization Transfer With Compensation

## Identity and scope

```text
workflow_id: workforce.org_transfer_compensation/v1
legacy_intent: employee.org_transfer_compensation_change
target parent intent: hcmnext.people.transfer_worker
child intents:
  change_assignment, change_manager, change_job_assignment,
  change_cost_center, change_compensation, recalculate_entitlements
kernel_family: ProcessRequest composed from ChangeRequests
subject: Worker + Employment + Assignment + Position
initiator: authorized HR partner
state: EXTRACTED + EXPLORED
```

This workflow is a composition. Organization, manager, job, position, cost center,
compensation and access are separate authoritative facts. The parent proposal
binds their exact child proposals and effect order.

## Dependency inventory

Required synchronous dependencies:

```text
Identity/AuthN, AuthZ and organization scope
People/Employment/Assignment
Organization/Relationship graph
Position/Headcount and occupancy
Job/Reference Data and external crosswalks
Compensation and Workforce Budget Authority
Source Authority and Conflict Registry
Legal/Jurisdiction/Privacy/DLP/Risk
Workflow/Human Work/Approval Resolution
TransactionPlan and multi-stream commit coordinator
```

Required asynchronous dependencies:

```text
HRIS connector + authoritative observation
Payroll/pay-group/cost-center connector + observation
IAM entitlement recalculation/provisioning + observation
Compensation vendor when separately authoritative
Learning/Documents/Messaging obligations when triggered
Reconciliation, Repair, Incident and Provenance
```

Conditional dependencies include immigration, benefits, CBA/works council,
location/tax resolution, equipment, physical access and relocation.

## Features

Effective dating, cross-workflow conflict control, position and budget reservation,
simulation, multi-party approvals, conditional approval requirements, documents,
timers, multi-stream atomic commit, ordered external effects, multidimensional
completion, reconciliation and repair are required. Offline approval is
prohibited; offline draft collection may be allowed for non-sensitive fields.

## Steps

```text
1  resolve worker, active employment/assignment and every current authority
2  collect target org/location/team/cost center/manager/job/position/pay proposal
3  normalize target references and crosswalk external codes
4  read current org graph, assignments, occupancy, comp, budget, payroll and access
5  resolve destination jurisdiction, legal entity, CBA and privacy implications
6  validate target entities, graph invariants, manager eligibility and field scope
7  calculate exact affected resource/field/effective-range conflict footprint
8  simulate all child changes and ordered downstream effects
9  reserve position/headcount and compensation/finance budget with fences
10 construct immutable parent ProposalRevision containing child proposal digests
11 resolve conditional approval requirements from policy/legal/risk
12 collect source manager, destination manager, HRBP, compensation, finance and
   any clinical/legal/works-council decisions; re-evaluate authority at decision
13 generate/collect employment amendment or notice where required
14 wait for acknowledgement/signature and effective date
15 revalidate worker/target entities, reservations, law, authority, cutoffs,
   conflicts, approval bindings, source authority and external capacity
16 atomically end/supersede old assignment relationships and append target
   assignment/manager/job/position/compensation facts plus ledger/projections/outbox
17 dispatch external operations by resource key and causal order
18 observe People/HRIS, Payroll, IAM, Compensation and mandatory obligations
19 reconcile each effect independently
20 release reservations; mark business completion separately from external state
21 create targeted RepairPlan/Incident for drift or unknown outcomes
22 close only after closure policy is satisfied
```

## Data and candidate properties

Input:

```text
worker_ref, employment_ref, current_assignment_ref
target_legal_entity_ref?, target_org_ref, target_team_ref?
target_location_ref, target_cost_center_ref, target_manager_ref
target_job_ref, target_position_ref?
proposed_fte?, proposed_compensation_component_operations[]
effective_range, transfer_reason_code, business_reason
relocation_required?, access_impact_acknowledgement
```

Canonical writes may include assignment revision, organization membership,
manager relationship, position occupancy, cost allocation, job assignment,
compensation component revisions and access-recalculation intent. Runtime data
includes parent/child digests, reservations/fences, expected stream heads,
approval requirements/decisions, legal obligations, payroll cutoff, effect DAG,
external versions, observations and per-domain completion dimensions.

Legacy gaps to reject: fixed actor IDs; a universally required medical-director
approval; direct data writes before a durable atomic commit; sequential vendor
calls without resource ordering/authority fences; `accepted` responses treated as
truth; manual repair without a bounded RepairPlan.
