# Promotion Into Management

## Identity and scope

```text
workflow_id: rewards.promotion_into_management/v1
parent intent: hcmnext.people.promote_worker
children: job/position/manager/compensation/access/learning/message changes
kernel_family: ProcessRequest composed from ChangeRequests
subject: Worker + EmploymentAssignment + Position + CompensationPackage
state: REFERENCE + EXPLORED
source: planning/reference-workflows/promote-into-management.md
```

Dependencies: People, Organization, Position/Headcount, Job/Reference Data,
Compensation, Budget/Finance, Payroll, IAM, Talent, Learning, Documents,
Communications, Legal/Regulatory, AuthZ, Source Authority, Conflict/Reservation,
Workflow/Human Work, Transaction Coordinator, Integration, Reconciliation and
Repair.

Features: cross-domain simulation, position/budget reservation, conditional
approval thresholds, documents/acknowledgement, effective wait, multi-stream
commit, parallel downstream effects, multidimensional completion and targeted
repair.

## Steps

```text
1 resolve worker, assignment, current job/position/manager/comp and authorities
2 collect target job/level/position/manager/team, compensation and effective date
3 read job/position capacity, org graph, band/budget, payroll, IAM and obligations
4 validate target references, eligibility, graph and position occupancy
5 simulate people, compensation, finance, payroll, access, talent, learning,
  document, messaging, legal and risk effects
6 conflict-check termination/leave/transfer/future comp and reserve position/budget
7 create immutable parent proposal with all child digests and effect graph
8 collect current manager, HRBP, compensation and conditional finance approvals
9 generate localized amendment/letter and collect required acceptance
10 wait until effective date
11 revalidate worker, position, budget, target manager/team, band, payroll cutoff,
   IAM policy, law/rules, conflicts and every approval authority
12 atomically append job/position/manager/compensation changes plus ledger/outbox
13 in parallel dispatch payroll sync, IAM recalculation, talent profile, learning,
   document archive and policy-timed communications
14 join only mandatory effect classes; observe each authority
15 reconcile expected versus observed state per domain
16 mark promotion business-complete even if permitted downstream dimensions are
   degraded; create RepairPlan/Incident and close after policy permits
```

## Candidate data

```text
PromotionProposal
  worker/employment/assignment refs
  current and target job/position/manager/org refs
  direct_report_relationship_changes[]
  compensation_component_operations[]
  effective_range, reason, promotion_type
  position_reservation_ref, budget_reservation_ref
  child_proposal_digests[], obligation_set_ref, effect_graph_ref

SimulationResult
  people_diff, compensation_diff, annual_cost_delta
  payroll_impact, access_grant/revoke plan
  talent/learning/document/message obligations
  conflicts, warnings, risk, cost, external readiness
```

Team membership and direct-report changes require explicit relationship writes;
they cannot be inferred solely from a new title. Manager access is recalculated
from authoritative relationships rather than copied from a static role template.
