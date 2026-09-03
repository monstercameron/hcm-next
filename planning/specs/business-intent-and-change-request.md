# Business Intent and Change Request Kernel Contract

The Business Transaction Service owns platform intent identity, subtype
registration, proposal revisions, lifecycle coordination, supersession,
cancellation, and closure evidence. It does not own domain facts or workflow
runtime state.

## General Kernel and Subtypes

```text
BusinessIntent
  +-- ChangeRequest          may mutate domain state or cause external effects,
  |                          directly or through explicitly bound child intents
  +-- CalculationRequest     deterministic, pure computation; no mutation
  +-- AnalyticalRequest      governed read, explanation, or inference; no mutation
```

Three families are the whole kernel. What distinguishes them is one question:
may this intent cause a material mutation or effect? Everything else that used
to be a family is an attribute of a `ChangeRequest` definition:

| Former family    | Now expressed as                                                                      |
| ---------------- | ------------------------------------------------------------------------------------- |
| `ProcessRequest` | `ChangeRequest` whose compiled plan has child intents and no single central write     |
| `FilingRequest`  | `ChangeRequest` with `IRREVERSIBLE_EXTERNAL_MUTATION` side effect and correction rule |
| `BatchOperation` | `ChangeRequest` with a declared `population_scope` and per-item child intents         |
| `Case`           | Not an intent. A Case is a domain aggregate that owns linked child intents            |

A family is added only when a funded domain proves that an attribute cannot
express the distinction. `Case` and `FilingRequest` are reserved names for that
event; they are not runtime classes today.

`HCMChangeRequest` is the first `ChangeRequest` subtype, not the universal object
for payroll runs, time punches, cases, filings, or analytics.

### Layer budget

One intent passes through at most these layers, in this order, and P1A uses
only the first, second, and last:

```text
IntentInstance            what is wanted, by whom, for which subjects
  -> capability call      one governed operation, when no workflow is needed
  -> compiled workflow    only when approval, waiting, or ordering is required
  -> domain command       produces validated planned appends
  -> TransactionPlan      one atomic local commit
  -> evidence             ledger, observation, reconciliation
```

No additional coordination layer may be inserted between these without a
scope exchange.

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

## Five Lifecycle Dimensions

An intent carries exactly five state dimensions. Each answers one question that
the others cannot:

```text
RequestState       where is the request itself?
  DRAFT | PREFLIGHTED | SIMULATED | SUBMITTED | APPROVED | REJECTED
  | WITHDRAWN | CANCELLED | SUPERSEDED | CLOSED | REOPENED

ExecutionState     what has the runtime done?
  NOT_PLANNED | SCHEDULED | REVALIDATING | EXECUTING | COMMITTED
  | BLOCKED | REPAIR_REQUIRED

BusinessState      did the business outcome happen?
  NOT_STARTED | IN_PROGRESS | COMPLETED | NOT_ACHIEVED | CORRECTED | UNKNOWN

ConsistencyState   does observed external state agree with intent?
  NOT_APPLICABLE | PENDING_OBSERVATION | CONSISTENT | DEGRADED
  | REPAIRING | UNKNOWN

ObligationState    are attached obligations discharged?
  NOT_APPLICABLE | PENDING | SATISFIED | OVERDUE | WAIVED | UNKNOWN
```

No single `status` substitutes for these five. Runtime commit, business
completion, external consistency, and obligations may legitimately differ, and
the product renders all five.

Things that were once separate dimensions and are now records or projections:

| Former dimension      | Where it lives now                                                                   |
| --------------------- | ------------------------------------------------------------------------------------ |
| `ProposalState`       | Folded into `RequestState`; each `ProposalRevision` remains its own immutable record |
| `ApprovalState`       | Folded into `RequestState`; each `ApprovalBinding` remains its own record            |
| `ReconciliationState` | Merged into `ConsistencyState` (`REPAIRING` is the reconciliation-in-progress value) |
| `ClosureState`        | `CLOSED` and `REOPENED` are `RequestState` values; `ClosureRecord` holds evidence    |
| `OperationalState`    | A workflow-instance and incident projection, not an intent property                  |
| `OutcomeState`        | An Intelligence outcome record referenced by `ClosureRecord.outcome_tracking_ref`    |

Independence does not mean every tuple is legal, but legality is governed by a
short fixed rule set, not a per-definition lattice:

- `CANCELLED`, `SUPERSEDED`, `REJECTED`, or `WITHDRAWN` cannot enter `EXECUTING`;
- `APPROVED` requires one valid requirement-level `ApprovalBinding` for the
  current proposal revision unless the definition declares approval not required;
- a new material proposal revision invalidates every `ApprovalBinding` whose
  `approved_proposal_digest` differs from the current material digest (see the
  materiality rule below);
- `COMMITTED` requires a `CommitReceipt`; `REPAIR_REQUIRED` requires a linked
  RepairPlan or incident;
- `CLOSED` requires `ObligationState` in `{SATISFIED, WAIVED, NOT_APPLICABLE}` and
  `ConsistencyState` not in `{PENDING_OBSERVATION, REPAIRING}` unless the
  definition's closure policy explicitly permits closing with an open repair;
- `UNSPECIFIED` is invalid for persisted active instances.

These rules are enforced on every command transition and checked on projection
rebuild, replay, and repair. A projection may not repair an illegal tuple by
silently selecting a preferred status.

### Materiality rule for control snapshots

A `ProposalRevision` records control snapshot digests (policy bundle, capability
registry, reference data, classification taxonomy, DLP decision, and so on) as
evidence of the context in which it was simulated. Those digests are **not**
part of the material proposal digest and their change does **not** by itself
invalidate approvals. Instead:

```text
control snapshot changed
        |
        v
revalidate the proposal under the new snapshot
        |
        +-- material result unchanged -> approval stands; record revalidation
        +-- material result changed   -> new revision; approvals invalidated
        +-- mandatory deny appears    -> execution BLOCKED; approvals invalidated
```

"Material result" means the typed planned writes and effects, subjects,
effective time, required approvals, reservations, and source-authority
decisions for the written fields. The exact field list is the `PROPOSAL`
canonicalization profile in the [Canonical Envelope and Digest
Contract](canonical-envelope-and-digest.md). This keeps a tenant-wide policy
republish from invalidating every pending approval in a compensation cycle
while still guaranteeing that nothing executes under a stale decision.

Normal Promotion sequence, with the `RequestState` / `ExecutionState` values
in brackets:

```text
Draft intent                                   [DRAFT / NOT_PLANNED]
  -> Preflight                                 [PREFLIGHTED]
  -> Deterministic simulation                  [SIMULATED]
  -> Material proposal revision + CanonicalDigest
  -> Submit exact digest                       [SUBMITTED]
  -> Resolve/collect approvals                 [APPROVED]
  -> Schedule                                  [APPROVED / SCHEDULED]
  -> Execution-time revalidation               [APPROVED / REVALIDATING]
       same material digest -> Execute         [APPROVED / EXECUTING -> COMMITTED]
       material difference  -> new revision    [SIMULATED / NOT_PLANNED]
  -> Observe/reconcile/repair                  [ConsistencyState moves]
  -> Close when obligations permit             [CLOSED]
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

P1A implements `RequestState` through `SIMULATED` and `SUBMITTED` without
execution; `ExecutionState` stays `NOT_PLANNED`. P1B adds `APPROVED`,
`SCHEDULED`, `REVALIDATING`, `EXECUTING`, `COMMITTED`, reconciliation, repair,
and `CLOSED` for Promotion only.
