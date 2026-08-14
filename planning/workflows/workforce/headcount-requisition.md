# Position and Headcount Requisition

## Identity and scope

```text
workflow_id: workforce.headcount_requisition/v1
legacy_intent: position.headcount_requisition.approval
target intents:
  hcmnext.workforce.create_headcount_request
  hcmnext.workforce.approve_headcount
  hcmnext.workforce.create_position
kernel_family: ProcessRequest + ChangeRequest
subject: HeadcountPlan/Budget/Position
initiator: manager, HR, workforce planner or operations administrator
state: EXTRACTED + EXPLORED
```

Approval of headcount and creation of a position are distinct transactions. A
request may authorize capacity without immediately creating a position, and a
position may consume a previously approved plan/budget line.

## Dependencies and features

Required: Organization, Job/Reference Data, Position/Headcount, Workforce Budget,
Finance authority, AuthZ, rules/decision tables, approval resolution, Forms/Human
Work, conflict/reservation, workflow, commit coordinator, ledger, external HRIS
connector, observation/reconciliation and repair.

Features: structured intake, simulation, synchronous approval chain, asynchronous
quorum, separation of duties, position/budget reservation, effective/start-date
planning, external creation, reconciliation and repair. Payroll/IAM/recruiting are
not invoked until a position is approved/created or a requisition starts.

## Steps

```text
1 resolve requesting org, workforce plan, budget authority and requester scope
2 collect job, location, cost center, FTE/capacity, target date, justification,
  employment/worker type, compensation range and requested position count
3 validate references, salary range ordering, FTE scale and plan period
4 simulate headcount/budget/cost impact and identify duplicate/open capacity
5 conflict-check freezes, overlapping requests, plan limits and existing positions
6 conditionally reserve plan capacity and budget
7 resolve leadership approval chain from relationships, not selected arbitrary IDs
8 collect ordered leadership decisions bound to exact proposal
9 collect cross-functional HR/Finance/Operations quorum with SoD
10 handle request-more-information by producing a new proposal revision
11 revalidate approvers, plan/budget, freeze state, references and reservations
12 atomically approve headcount and optionally create position revision/outbox
13 if external authority owns positions, submit conditionally with idempotency key
14 observe created position/capacity and reconcile IDs/attributes
15 release or consume reservations; close or RepairPlan
```

## Data and candidate properties

```text
HeadcountRequest
  request_id, requesting_org_ref, plan_ref?, budget_ref?
  job_ref, location_ref, legal_entity_ref, cost_center_ref
  worker_type, employment_type, requested_position_count
  requested_capacity_decimal, capacity_unit
  target_start_local_date, plan_period
  compensation_range {min_decimal,max_decimal,currency,basis}
  justification, replacement_for_position_ref?, priority
  proposal_revision, source_authority

ApprovalPolicyResult
  ordered_requirements[], quorum_requirements[], SoD constraints[]

ExecutionResult
  approved_capacity, reservation_refs[], created_position_refs[]
```

Legacy gaps to reject: floating-point FTE/salary; free-text department/team/
location/cost center where canonical references exist; requester-selected approvers
without policy validation; conflating request approval and position creation;
external `requested` state treated as completion.
