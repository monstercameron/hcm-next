# Transaction Plan and Atomic Commit Coordinator Contract

The Transaction Coordinator owns the immutable executable plan and local atomic
commit protocol. Domain services produce validated planned appends; Workflow
coordinates when to request/execute a plan; neither manufactures another
domain's events.

## Canonical State

```text
TransactionPlan
PlannedRead
PlannedAppend
ProjectionMutation
OutboxEffect
CommitPrecondition
ReservationBinding
CommitReceipt
AbortReceipt
```

```text
DRAFT -> DOMAIN_VALIDATED -> GOVERNANCE_VALIDATED -> APPROVAL_BOUND
      -> RESERVED -> READY -> COMMITTING -> COMMITTED

Before commit: STALE | ABORTED
During/after uncertainty: AMBIGUOUS | REPAIR_REQUIRED
```

The plan binds tenant/cell, business intent and proposal CanonicalDigest, execution
mode, subjects, effective time, planned reads/baselines, domain appends and stream
sequences, critical projection mutations, conflict snapshot/fence, position and
budget reservations, source authority, AuthZ/legal/policy/entitlement/risk
decisions, idempotency record, outbox effects, post-commit observations,
compensation/repair declarations, schema/profile versions, and expiry.

## APIs and Boundaries

```text
transactions.plan|validate|compare|bind_approval|reserve|prepare
transactions.commit|abort|status|explain
transactions.receipts.read|verify
```

Simulation produces a non-executable effect model. Domain validation converts it
to typed planned appends. Governance validation adds current policy decisions.
Approval binds the exact canonical proposal and plan material context. After
binding, any material difference creates a new plan/proposal revision; the plan
is never mutated in place.

`prepare` performs commit-time AuthZ, source authority, conflict, reservation,
expected sequence, effective-time, idempotency, schema/profile, and digest checks.
`commit` owns exactly one local database transaction: append all co-located domain
and transaction events, apply declared critical projections, transition conflict/
reservation state, finalize idempotency, and append outbox records. External calls
are prohibited inside this transaction and execute afterward as sagas.

## Failure, Security, and Evidence

Typed failures include expired/stale plan, digest/approval mismatch, domain or
governance rejection, sequence/conflict/reservation/authority change, unsupported
mixed version, idempotency conflict, database abort, ambiguous commit result, and
post-commit repair requirement. On ambiguous database outcome, the coordinator
queries the transaction/idempotency receipt before any retry.

Plan authoring, domain validation, governance approval, human approval, prepare,
commit, abort, and repair are separately authorized. Commit workers use scoped
short-lived identity and can execute only a signed/hashed plan in the tenant/cell
lease. High-risk plans require step-up/SoD already bound in the proposal.

Evidence retains every plan revision/digest, validators and versions, reads and
sequences, decisions, reservations/fences, prepare result, database transaction
identity, event/projection/outbox IDs, commit/abort receipt, ambiguity resolution,
external saga/reconciliation, and repair. Plan/receipt retention follows the
longest governed business record it explains.

Gate A creates non-executable simulated plans. Gate B implements prepare/commit/
abort for Promotion and tests stale plans, two-writer races, process death before/
after commit, ambiguous acknowledgement, cancellation before commit, and no
external call inside the transaction.
