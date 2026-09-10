# Transaction Ledger, Reconciliation, and Repair

Extracted from the Human Capital Management Suite architecture constitution so this contract can evolve independently. The master delivery scope remains governed by [../execution-plan.md](../execution-plan.md).

## 8. Platform Capabilities

### 8.1 Change Request Hub

Provides one place to initiate, track, approve, execute, reconcile, review, and correct employee changes.

### 8.2 Workflow Routing

Routes actions using HCM-specific context such as:

- Role and relationship to the worker
- Organization and legal entity
- Geography and jurisdiction
- Compensation threshold
- Changed fields
- Effective date and payroll cutoff
- Policy requirements
- Delegation and segregation of duties

### 8.3 Transaction Simulation

Simulation should disclose what is known and how it is known. Effects are classified as:

- Deterministic Human Capital Management Suite effects
- Policy-based predictions
- Connector-derived intended payloads
- Estimated external-system outcomes
- Unknown or unmodeled downstream behavior

Every result should show provenance, assumptions, configuration versions, source freshness, confidence, and unresolved conflicts. The product promises transparent impact analysis, not certainty about opaque external automation.

### 8.4 Approval Integrity

Approvals bind to immutable proposal revisions and relevant context. Before execution, the platform reevaluates:

- Proposal identity and content
- Policy and permission validity
- Approver authority
- Worker relationship and jurisdiction
- Current and future-dated employee state
- Conflicting or superseding transactions
- Transaction-plan validity

Stale approval must result in an explicit return to preflight, simulation, or approval.

### 8.5 Transaction Ledger

The ledger records the causal history of the transaction:

- Who requested, reviewed, approved, and executed it
- What proposal was considered
- Which policies and permissions applied
- What AI was permitted to see and produced
- Which systems were touched
- What was attempted and observed
- What failed or conflicted
- What was retried, repaired, reversed, or corrected

The ledger must support historical explanation without treating replay as automatic repetition of external side effects.

Ledger authority is attached to the assertion, not inferred from the fact that an event was recorded:

| Assertion class        | Meaning                                                                                                                 |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `TRANSACTION_FACT`     | Human Capital Management Suite authoritatively records its own proposal, decision, plan, attempt, or transaction result |
| `DOMAIN_FACT`          | Human Capital Management Suite is the configured authority for the asserted domain fact and effective interval          |
| `EXTERNAL_OBSERVATION` | Another configured authority reported a value at an observation time                                                    |
| `CLAIM`                | A human, service, agent, document, or import asserted something not yet promoted to domain truth                        |
| `CORRECTION`           | A later governed assertion corrects, completes, or supersedes an earlier assertion                                      |

For example, `CompensationObserved(amount=145000, source=Workday)` is authoritative evidence that Human Capital Management Suite observed Workday report that value. It becomes authoritative compensation truth only if the source-authority contract says that Workday governed that field and interval, or a governed process promotes the assertion.

```text
immutable event chronology
          ≠
automatic authority over every payload field

authority = assertion class + source-authority policy + effective time
```

### 8.6 Integration and Reconciliation

The platform compares intended state with observed state and distinguishes:

- Delivery failure
- Partial application
- Stale observation
- Unauthorized external mutation
- Legitimate concurrent external change
- Transformation or mapping disagreement

The configured authority and conflict policy determines the response. Accidental last-write-wins behavior is not an acceptable business rule.

### 8.7 Repair and Correction

The product supports:

- Safe retries
- Roll-forward repair
- Compensation transactions
- Manual repair
- Explicit supersession
- Retroactive correction
- Downstream recalculation tasks
- Reconciliation closure

Retroactive changes never erase prior history. They create correction lineage and identify downstream outcomes that must be recalculated, compensated, reviewed, or left to an external authority.

### 8.8 Ledger and Telemetry Separation

The ledger is the authoritative business-event source for the platform. It is not the application logging system.

Human Capital Management Suite operates two connected but separate information planes:

| Business ledger                           | Software telemetry                        |
| ----------------------------------------- | ----------------------------------------- |
| Promotion approved                        | HTTP request completed in 84 milliseconds |
| Worker job changed                        | Database pool waited 11 milliseconds      |
| Payroll write requested                   | Worker process used 64% CPU               |
| Payroll write failed                      | Connector emitted a stack trace           |
| Repair plan created                       | Retry queue latency increased             |
| Repair executed and reconciliation passed | Distributed span completed                |

The business ledger answers:

- What happened or was intended to happen?
- Who or what caused it?
- Why was it permitted?
- Which business state existed before and after?
- Which systems were affected?
- What failed, conflicted, or drifted?
- What action repaired or corrected the outcome?

Telemetry answers:

- How did the software behave?
- Where was time or capacity consumed?
- Which process, request, dependency, or code path failed?
- Is the technical system healthy?

The two planes connect through shared identifiers:

```text
tenant_id
workflow_instance_id
change_request_id
transaction_plan_id
correlation_id
causation_id
ledger_event_id
trace_id
```

An operator should be able to move from a business event such as `PayrollWriteFailed` to the distributed trace that caused it, and from a technical trace back to the affected worker transaction. Technical detail remains subject to tenant isolation and employee-data permissions.

The governing rule is:

> Every material business mutation, failure, reconciliation result, and repair becomes an immutable business fact. Runtime diagnostics belong in telemetry.

### 8.9 Ledger-Derived Operating Model

The raw ledger is expensive truth, not the normal query surface for product or operational pages. Purpose-built projections provide cheap, current answers.

```text
Authoritative ledger
        │
        ├── HCM projections
        │     worker, employment, pay, schedule, identity
        │
        ├── Operational projections
        │     workflow health, failures, incidents, repair queues
        │
        ├── Reconciliation projections
        │     intended state, observed state, drift, freshness
        │
        ├── Product experiences
        │     timelines, work queues, dashboards, search
        │
        └── Analytics distribution
              warehouse, lake, governed intelligence
```

Representative operational views include:

| Projection                | Primary question                                                       |
| ------------------------- | ---------------------------------------------------------------------- |
| Workflow health           | Which workflows are running, late, retrying, failed, or stuck?         |
| Integration health        | Which connectors are degraded and how large is the backlog?            |
| Reconciliation            | Where do intended and observed states disagree?                        |
| Repair queue              | Which repairs are automatic, awaiting approval, or assigned to humans? |
| Worker health             | Is this worker's cross-domain state internally coherent?               |
| Payroll run health        | Which pay run, calculation, or delivery outcomes require attention?    |
| Timecard exceptions       | Which time records violate policy or await resolution?                 |
| Access reconciliation     | Which worker entitlements disagree with workforce policy?              |
| Candidate workflow        | Where are candidates blocked or inconsistent in the hiring lifecycle?  |
| Onboarding readiness      | What prevents a new worker from being ready to work?                   |
| Benefit enrollment health | Which elections, eligibility decisions, or carrier states disagree?    |

Normal operational pages must not repeatedly aggregate the complete ledger. Each projection declares its source stream, source sequence, version, freshness, and rebuild status so the product can expose when an answer is stale.

### 8.10 Commit, Distribution, and Failure Isolation

The authoritative database commit establishes business truth. Event distribution occurs after that truth is durable.

```text
                    Governed transaction
                            │
                         commit
                            │
              ┌─────────────┴─────────────┐
              ▼                           ▼
       Immutable ledger                Outbox
              │                           │
              └─────────────┬─────────────┘
                            ▼
                    Event distribution
                            │
        ┌───────────┬───────┼─────────┬────────────┐
        ▼           ▼       ▼         ▼            ▼
    Projectors   Detectors  Search  Analytics  Reconciliation
```

Downstream consumers can fail, retry, rebuild, or catch up independently without invalidating the committed HCM transaction. Analytics outages, search rebuilds, delayed alerting, AI indexing failures, and reconciliation backlog must not erase or ambiguously roll back an already committed business fact.

The initial implementation may use Postgres and a transactional outbox. Future distribution technology may change without altering the logical contract:

- A commit establishes truth.
- Distribution is at-least-once and consumers are idempotent.
- Consumers maintain observable positions in their streams.
- Failed consumers can resume or rebuild.
- External side effects remain governed through outbox and reconciliation patterns.

### 8.11 Ordering and Stream Semantics

The suite does not require one global sequence across every tenant and domain. It requires deterministic order wherever business causality matters.

Relevant streams may include:

- Tenant
- Person or worker
- Employment
- Position
- Payroll run
- Benefit enrollment
- Timecard
- Workflow instance
- Integration operation

Each event must identify its subject and stream, its sequence within the relevant stream, its event type and schema version, and its correlation and causation lineage. Events distinguish:

- `occurred_at` — when the originating activity occurred
- `effective_at` — when the business fact applies
- `recorded_at` — when Human Capital Management Suite durably recorded it

A projection records the last source sequence it applied. If the ledger stream is ahead, the difference is measurable projection lag rather than invisible inconsistency.

Ordering scope is part of each domain contract. A payroll run may require different ordering guarantees from a worker profile, timecard, or communication event.

#### Multi-Stream Atomic Transaction Contract

A material business transaction may append to several ordered streams. Per-stream sequencing is insufficient unless all expected heads are validated and all local authoritative appends commit atomically.

```text
TransactionPlan tx_900

expects:
  worker_123       sequence 88
  employment_7     sequence 41
  position_19      sequence 12
  compensation_4   sequence 27

commit in one PostgreSQL transaction:
  append worker_123       sequence 89
  append employment_7     sequence 42
  append position_19      sequence 13
  append compensation_4   sequence 28
  append transaction tx_900 event
  update critical projections
  append outbox records

             ALL OR NOTHING
```

The contract includes:

- A stable transaction identifier and idempotency key whose canonical request
  digest follows the [Canonical Envelope, Serialization, and Digest
  Contract](canonical-envelope-and-digest.md)
- Every stream and expected sequence in the normalized write set
- Deterministic lock acquisition order to avoid deadlocks
- One ACID commit for ledger events, critical local state, and outbox records
- A unique constraint preventing duplicate transaction application
- A typed concurrency conflict when any expected stream head changed
- No distributed database transaction across external systems
- External effects performed afterward through idempotent saga activities and reconciliation

If affected authoritative streams do not share one database transaction boundary, the plan must not pretend to provide atomicity. It must use an explicit coordinator state, durable intent, fencing token, compensation/reconciliation semantics, and a completion model that exposes partial external consistency.

### 8.12 Continuous Integrity and Invariants

Reconciliation is a permanent background capability, not an occasional administrative action. Human Capital Management Suite must continuously test whether derived and external state agrees with authoritative business truth.

```text
Ledger
  ├── normal projection ─────────► Current state
  └── independent replay ────────► Expected state
                                        │
                                        ▼
                                     compare
                              equal ─────┴───── different
                                │                 │
                               healthy           drift
```

Integrity checks should use several tiers:

1. Verification after every sensitive transaction
2. Frequent checks of recently changed entities
3. Random statistical sampling
4. Scheduled partition or tenant scans
5. Full tenant or domain verification on demand

The platform also evaluates domain invariants, not merely workflow completion. Examples include:

- An active worker has a valid active employment relationship.
- A worker cannot manage themselves.
- Position occupancy does not exceed configured capacity.
- Base compensation has a supported currency and effective interval.
- An active paid worker belongs to a valid pay group.
- A terminated worker retains no prohibited privileged access.
- Timecard intervals do not overlap illegally.
- A filled position references the successful candidate and worker relationship.

An invariant violation produces a business event, diagnosis, and governed repair workflow. Invariants become a continuously evaluated proof of system coherence across the Workforce OS.

### 8.13 Business Correction and Derived-State Repair

Human Capital Management Suite must distinguish correction of a business assertion from repair of derived state.

```text
Business correction
    A previously recorded business assertion is incorrect,
    incomplete, or superseded.

    The ledger remains historically correct that the assertion
    occurred. Append a corrective or superseding business fact.

Derived-state repair
    The retained authoritative inputs are correct.
    Rebuild or resynchronize a projection, index, cache, or external system.
```

Operators should not normally repair the platform through direct database edits. A projection mismatch does not justify changing business history, and an incorrect business fact cannot be fixed by silently overwriting a projection.

A derived-state repair lifecycle may be:

```text
Drift detected
  -> Repair case created
  -> Repair plan generated
  -> Approval when required
  -> Projection rebuild or external resync
  -> Verification
  -> Resolution
```

A business correction preserves the original assertion and appends correction lineage:

```text
Worker manager changed: Alice -> Bob
  -> Correction requested
  -> Worker manager corrected: Bob -> Carol
  -> Downstream impact evaluated
  -> Derived and external state reconciled
```

### 8.14 RepairPlan

`RepairPlan` is a first-class sibling of `TransactionPlan`. It converts diagnosis into a governed, inspectable set of corrective actions.

A RepairPlan describes:

- The problem classification and affected subject
- Expected, actual, and unknown state
- Root-cause evidence and causal event path
- Affected workers, systems, and effective periods
- Proposed repair actions and their order
- Risk, reversibility, and potential side effects
- Required approvals and segregation of duties
- Verification and reconciliation criteria
- Repair algorithm and mapping versions

The lifecycle is:

```text
Failure, conflict, drift, or invariant violation
  -> Diagnosis
  -> RepairPlan
  -> Simulation
  -> Approval when required
  -> Repair execution
  -> Reconciliation
  -> Resolution
```

Automatic repair is allowed only when authority is unambiguous, the action is idempotent, the plan cannot overwrite a newer legitimate change, and policy explicitly permits automation.

### 8.15 Conditions, Incidents, and Business Severity

Alerting operates on conditions and incidents, not one notification per failed transaction. A connector outage affecting 50,000 changes should create one evolving incident with an affected population and repair backlog rather than 50,000 independent alerts.

```text
Ledger events + telemetry
          │
       detectors
          │
   incident fingerprint
          │
     ┌────┴────┐
     ▼         ▼
 existing     new
 aggregate   incident
                 │
                 ▼
             alert policy
```

Incident fingerprints may combine tenant, integration, operation, error class, workflow family, failure stage, projection type, or drift type.

Technical severity and business severity are separate dimensions. Incident evaluation considers:

- Technical severity
- Business-process severity
- Compliance and security severity
- Financial exposure
- Number and type of affected workers
- Payroll or benefits impact
- Access and deprovisioning impact
- Effective-date urgency
- Recoverability and backlog growth

For example, a delayed image resize and incorrect payroll deductions may both be technical failures, but they belong to entirely different business priorities.

### 8.16 Causal Graph and Operations Center

Correlation and causation links should power a causal graph rather than only a chronological timeline.

```text
Promotion requested
  -> Promotion approved
  -> Transaction executed
       ├── Job changed
       ├── Manager changed
       └── Compensation changed
              -> Payroll write requested
              -> Payroll write failed
              -> Retry scheduled
              -> Payroll write succeeded
              -> Reconciliation passed
```

This graph allows an operator to ask why a worker's payroll, access, schedule, or employment state is wrong and see the exact business and technical path that produced the outcome.

The Operations Center should be a major product surface, not a hidden administration page. It brings together:

- Cross-suite health
- Active business incidents
- Affected workers and transactions
- Workflow and integration backlogs
- Projection freshness and drift
- Reconciliation status
- Invariant violations
- Automatic and human repair queues
- Causal timelines and technical trace links
- Repair simulation, approval, execution, and verification

This turns the ledger from a persistence choice into a customer-visible capability: Human Capital Management Suite can explain how workforce state came to exist, detect when derived or external systems disagree, and provide a governed path back to consistency.

### 8.17 Storage, Checkpoint, and Reducer Evolution

The logical event model must remain stable as physical storage scales.

```text
Ledger events       = authoritative truth
Snapshots           = replay acceleration
Projections         = rebuildable state
Caches              = disposable state
Search indexes      = disposable state
Telemetry           = operational evidence with separate retention
```

Physical storage may evolve from a shared Postgres ledger into partitions, dedicated tenant infrastructure, change-data capture, event streams, search systems, and analytical storage. The rest of Human Capital Management Suite should continue to use stable logical operations such as append event, read stream, read causal timeline, and replay stream.

Snapshots and checkpoints improve replay performance but never become irrecoverable truth. The system must be able to discard and reconstruct them from authoritative events.

Reducers, event schemas, metadata, policy, and repair algorithms are versioned. Projection changes use shadow rebuilds:

```text
current reducer
      │
      ├── current projection
      │
new reducer
      └── shadow projection
                 │
                 ▼
             compare
          no difference -> promote
          difference    -> investigate
```

This permits safe evolution of derived state across a large, long-lived HCM event history.
