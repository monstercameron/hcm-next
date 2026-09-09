# Manager Change: Single-Domain Reference Workflow

## Purpose and scope

Manager Change is the smallest useful proof that a primarily People-domain
mutation still travels through the complete Human Capital Management Suite control path. It is a
design/conformance fixture and does not expand Phase 1 delivery scope.

```text
BusinessIntent
  type: hcmnext.people.change_manager/v1
  family: CHANGE_REQUEST
  worker: Jane Smith
  current manager: Alice
  proposed manager: Bob Jones
  effective date: 2026-09-01
  reason: REORGANIZATION
```

The authoritative mutation is narrow:

```text
People domain

manager relationship
  Alice -> Bob

effective interval
  [2026-09-01, open)
```

Derived hierarchy projections may change, but workflow storage, search, analytics,
and external observations do not become authoritative for the relationship.

## Execution graph

```text
START
  |
  v
people.worker.read + people.employment.read
  |
  v
validate active worker, active proposed manager,
permitted organization scope, and worker != manager
  |
  v
organization.relationship.manager_change.validate
  |     no self-management
  |     no graph cycle
  |     valid effective interval
  v
governance.evaluate
  |     entitlement + AuthZ + purpose + legal/privacy + risk
  v
conflicts.query
  |     transfer | termination | manager change | organization move
  v
simulate exact before/after relationship and derived impact
  |
  v
resolve and collect proposal-bound approval
  |
  v
WAIT until effective date
  |
  v
revalidate facts + authority + proposal + conflict watermark
  |
  v
people.manager.change                       [safe_point before]
  |
  v
ACID: relationship event + critical projection + outbox
  |
  v
observe authoritative People state
  |
  v
reconcile expected Bob == observed Bob
  |
  v
COMPLETE
```

## Exact read, write, and conflict contract

Reads:

```text
worker employment status at requested effective time
current and future manager relationships
proposed manager employment status and management eligibility
organization ancestry and scope
pending write intents affecting employment, manager, or organization
approval resolver inputs and current governance versions
```

Authoritative write set:

```text
relationship://manager/<worker-employment>
  field: manager_principal_or_assignment_ref
  interval: [effective_at, next_change)
```

Required conflicts are explicit:

```text
pending EndEmployment overlapping effective time         CONFLICT
pending ChangeManager on overlapping interval             CONFLICT
pending organization/transfer changing resolver context   REBASE_REQUIRED
pending leave                                              domain-policy decision
unrelated address/contact change                           NO_CONFLICT
```

Two simulations may pass. Commit still validates expected People stream sequence,
relationship stream sequence, normalized conflict watermark, and any reservation
or serialization fence in deterministic order.

## Approval and revalidation

The approval requirement is policy-resolved, not hardcoded. Representative policy:

```text
ONE_OF:
  HRBPFor(worker.organization)

optional customer rule:
  ALL_OF CurrentManagerOf(worker), HRBPFor(worker.organization)
```

Approval binds the material proposal digest containing worker/employment,
old/new manager, effective interval, reason, affected relationship fields, and
derived impact summary. Control snapshots are recorded as revalidated context,
not hashed into the digest. At decision time and execution time the platform
reevaluates approver authority according to the requirement's validity policy.

Execution fails closed or requires a new proposal when Jane or Bob is inactive,
Bob no longer meets manager eligibility, organization scope changed materially,
a cycle would now exist, the proposal/approval is stale, or a conflicting intent
won the commit race.

## Evidence and completion

Business evidence reads as a story:

```text
ManagerChangeProposed
ManagerChangeValidated
ManagerChangeSimulated
ManagerChangeApproved
ManagerChangeScheduled
ManagerChangeRevalidated
ManagerChanged
ManagerChangeObserved
ManagerChangeReconciled
```

Completion requires:

```text
RequestState       CLOSED
ExecutionState     COMMITTED
BusinessState      COMPLETED
ConsistencyState   NOT_APPLICABLE or CONSISTENT
ObligationState    SATISFIED
```

If an incumbent HRIS remains authoritative, the local ledger records the
transaction request and external observation honestly. Business completion then
depends on the governed external write and observing the incumbent report Bob;
Human Capital Management Suite must not promote its own intended projection to domain truth.

## Required conformance scenarios

1. Happy-path future-effective manager change.
2. Worker cannot manage themself.
3. Direct and indirect manager-cycle detection.
4. Proposed manager becomes inactive after approval.
5. HRBP or current-manager authority changes before decision/execution.
6. Competing future manager change wins after both simulations pass.
7. Transfer changes organization scope and forces rebase/reapproval.
8. Duplicate request is idempotently suppressed.
9. Cancellation before effective date releases workflow/conflict state without erasing evidence.
10. External write succeeds but observation is delayed, leaving reconciliation pending rather than falsely complete.
