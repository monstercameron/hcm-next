# Business Intent and Change Request Kernel Contract

The Business Transaction Service owns platform intent identity, subtype
registration, proposal revisions, lifecycle coordination, supersession,
cancellation, and closure evidence. It does not own domain facts or workflow
runtime state.

## General Kernel and Subtypes

```text
BusinessIntent
  +-- ChangeRequest          proposes material domain mutation
  +-- ProcessRequest         requests a governed process without immediate write
  +-- CalculationRequest     deterministic calculation/result
  +-- FilingRequest          regulated submission intent
  +-- Case                   governed, evidence-bearing investigation/service matter
  +-- BatchOperation         bounded population operation
  +-- AnalyticalRequest      governed read/analysis; no business mutation
```

`HCMChangeRequest` is the first `ChangeRequest` subtype, not the universal object
for payroll runs, time punches, cases, filings, or analytics.

The semantic intent type is also not the kernel family. `PromoteWorker` is a
versioned intent definition whose kernel family is `ChangeRequest`; it is not a
new runtime class. The governed catalog and the instance model are defined in
[the Business Intent Catalog](business-intent-catalog.md).

Canonical objects are `BusinessIntent`, `IntentSubtype`, `ProposalRevision`,
`ProposalDigest`, `ApprovalBinding`, `ExecutionBinding`, `Supersession`,
`CancellationDecision`, `CorrectionReference`, and `ClosureRecord`.

Conceptual and wire names map exactly:

| Concept                      | Canonical Protobuf representation                                |
| ---------------------------- | ---------------------------------------------------------------- |
| `BusinessIntent` instance    | `hcmnext.intents.v1.IntentInstance`                              |
| `IntentSubtype` registration | `IntentDefinition` plus its `DefinitionReference`                |
| `ProposalDigest`             | `CanonicalDigestReference` bound to intent and proposal revision |
| `Supersession`               | `SupersessionReference`                                          |

`BusinessIntentInstance` and `HCMChangeRequest` are explanatory/product aliases,
not additional wire roots. New APIs and storage use the Protobuf names above.

## Independent State Dimensions

```text
IntentState:          DRAFT | SUBMITTED | ACCEPTED | REJECTED | CANCELLED | SUPERSEDED
ProposalState:        DRAFT | PREFLIGHTED | SIMULATED | SUBMITTED | WITHDRAWN | SUPERSEDED
ApprovalState:        NOT_REQUIRED | PENDING | PARTIAL | APPROVED | REJECTED | INVALIDATED
ExecutionState:       NOT_PLANNED | SCHEDULED | REVALIDATING | EXECUTING | COMPLETED | BLOCKED | REPAIR_REQUIRED
ExternalConsistency: NOT_APPLICABLE | PENDING | CONSISTENT | DEGRADED | UNKNOWN
ReconciliationState: NOT_REQUIRED | PENDING | PASSED | FAILED | REPAIRING
ClosureState:         OPEN | CLOSURE_ELIGIBLE | CLOSED | REOPENED
BusinessState:        NOT_STARTED | IN_PROGRESS | COMPLETED | NOT_ACHIEVED | CORRECTED | UNKNOWN
OperationalState:     NORMAL | DEGRADED | INCIDENT | MANUAL_CONTINUITY | RECOVERING
ObligationState:      NOT_APPLICABLE | PENDING | SATISFIED | OVERDUE | WAIVED | DISPUTED | UNKNOWN
OutcomeState:         NOT_DEFINED | PENDING_OBSERVATION | OBSERVED | INCONCLUSIVE | DISPUTED | EXPIRED
```

No single `status` substitutes for these dimensions. Runtime completion,
business completion, external consistency, obligations, operations and later
outcomes may legitimately differ.

Independence does not mean every tuple is legal. Each intent definition references
a versioned `LifecycleCompatibilityProfile` containing allowed states, transition
preconditions and cross-dimension implications. Universal constraints include:

- `CANCELLED` or `SUPERSEDED` cannot enter a new `EXECUTING` state;
- `APPROVED` requires at least one valid requirement-level approval binding unless
  the definition declares `NOT_REQUIRED`;
- a material proposal revision invalidates every binding whose digest or control
  snapshot no longer matches;
- `CLOSED` requires the definition's obligation and reconciliation closure policy;
- `REPLAY` requires an original execution binding; `REPAIR` requires a RepairPlan;
- `UNSPECIFIED` is invalid for persisted active instances.

The compatibility profile is enforced during definition publication, every
command/CAS transition, projection rebuild, migration, replay and repair. Negative
fixtures cover impossible tuples; projections may not repair an illegal tuple by
silently selecting a preferred status.

Normal Promotion sequence:

```text
Draft intent
  -> Preflight
  -> Deterministic simulation
  -> Material proposal revision + CanonicalDigest
  -> Submit exact digest
  -> Resolve/collect approvals
  -> Schedule
  -> Execution-time revalidation
       same material digest -> Execute
       material difference  -> new revision + invalidate approvals
  -> Observe/reconcile/repair
  -> Close when obligations permit
```

## APIs and Rules

```text
business_intents.create|read|submit|cancel|supersede|close|reopen
proposal_revisions.create|preflight|simulate|submit|withdraw|compare
approval_bindings.read|invalidate
execution_bindings.schedule|status
business_intents.explain
intent_subtypes.validate|publish|deprecate
```

Creation requires tenant, requester/delegation, purpose, subtype/version,
subjects, initial scope, idempotency, classification, and retention class.
Subtypes declare allowed domains, execution modes, state requirements,
cancellation/supersession/correction semantics, evidence, and whether they may
produce a `BusinessTransaction`.

Idempotency is scoped to intent subtype and canonical request. Supersession links
old/new intent and declares whether execution, approvals, reservations, tasks,
and obligations cancel, migrate, or continue. Cancellation is policy-evaluated;
it never kills a process or erases effects. Correction references the earlier
transaction/assertion and starts a governed corrective intent.

## Failure, Security, and Evidence

Typed failures include unknown subtype/schema, duplicate-conflicting request,
invalid transition, missing preflight/simulation, stale/materially changed
proposal, approval invalidation, conflict/reservation failure, cancellation not
permitted, closure obligations incomplete, and authority/classification denial.

Capabilities are tenant/org/subject/field/purpose/risk scoped. Request, approve,
execute, correct, cancel, supersede, and close are distinct authorities with
separation-of-duties rules. The kernel exposes only fields the caller may see and
does not broaden domain access through an intent reference.

Evidence includes intent/subtype/version, all proposal digests and comparisons,
preflight/simulation results, approval bindings/invalidations, workflow and domain
transaction references, cancellation/supersession/correction decisions,
completion dimensions, obligations, closure decision, and retention actions.

Gate A implements Draft through simulated/submitted proposal without execution.
Gate B adds approval, schedule, revalidation, bounded execution, reconciliation,
repair, and closure for Promotion only.
