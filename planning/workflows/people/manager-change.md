# Manager Change

## Identity and scope

```text
workflow_id: people.manager_change/v1
target_intent: hcmnext.people.change_manager
kernel_family: ChangeRequest
subject: EmploymentAssignment + ManagerRelationship
initiator: authorized manager or HR partner
state: REFERENCE + EXPLORED
source: planning/reference-workflows/manager-change.md
```

Features: effective dating, graph validation, conflict detection, simulation,
conditional approval, durable wait, execution revalidation, atomic multi-stream
commit, external observation, reconciliation and repair.

Dependencies: People, Organization/Relationship graph, AuthZ, Source Authority,
Conflict Registry, Workflow/Human Work, Transaction Coordinator, Ledger,
Integration/Observation, Access recalculation where manager-derived entitlements
exist, and Messaging for tasks/notices.

## Steps

```text
1 resolve worker/employment/assignment and current primary manager as of effective date
2 collect proposed manager, effective range and reason
3 validate both relationships active/eligible and worker != manager
4 simulate graph edge replacement and manager-derived access/workflow impacts
5 reject self-management, cycles, overlapping primary managers and scope violations
6 conflict-check transfer, termination, leave, org move and future manager changes
7 bind immutable proposal to exact old/new relationship and graph versions
8 resolve conditional HRBP/current-manager approval and collect decisions
9 wait until effective date
10 revalidate actors, relationship eligibility, graph, approval authority and conflicts
11 atomically end old edge, create new edge, append ledger/projection/outbox
12 recalculate downstream approval/access relationships as explicit effects
13 observe authoritative manager relationship; reconcile and repair drift
```

## Candidate properties

```text
ManagerChangeRequest
  worker_ref, employment_ref, assignment_ref
  current_manager_relationship_ref
  proposed_manager_person_ref
  manager_relationship_type = PRIMARY
  effective_range, reason_code, reason_detail?

ManagerRelationshipRevision
  relationship_id, worker_assignment_ref, manager_person_ref
  type, effective_range, source_authority
  revision, supersedes?, correction_of?
```

Runtime data includes graph version, affected closure set, proposal digest,
approval bindings, expected stream heads and reconciliation watermark. The manager
task recipient and authorized approver are independently resolved and revalidated.
