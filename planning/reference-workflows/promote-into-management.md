# Promote Into Management: Cross-Domain Reference Workflow

## Parent intent and ownership model

The customer experiences one business action:

```text
Promote Jane to Engineering Manager on 2026-10-01
  position: ENG-MGR-42
  manager: Susan
  responsibility: Team Phoenix
  base pay: 165,000 USD
  bonus target: 15 percent
```

This is represented as one parent `hcmnext.people.promote_worker/v1` intent with
a management-promotion variant and immutable desired outcome. It does not require
a new runtime class or a `PromoteToManager()` monolith.

```text
Parent ChangeRequest
        |
        +-- People promotion change
        +-- Position reservation/fill change
        +-- Compensation change
        +-- Budget reservation/consumption
        +-- Document and acknowledgement process
        +-- Payroll external effect
        +-- Access recalculation effect
        +-- Talent/Learning child work
        +-- Messaging child intents
        `-- Reconciliation/repair process
```

Each child has its own definition reference, typed payload, idempotency identity,
authority, evidence, and result. The parent proposal binds the ordered child set
and its material digests.

## Preflight and simulation

```text
                         resolve Person/Worker/Employment
                                      |
                                      v
                         immutable ProposalRevision
                                      |
          +---------------------------+---------------------------+
          v                           v                           v
       PEOPLE                      REWARDS                      FINANCE
 job/level/position/org       pay/band/bonus/range       headcount/cost/budget
          |                           |                           |
          +---------------------------+---------------------------+
                                      |
                                      v
                  legal + AuthZ + privacy + entitlement + risk
                                      |
                                      v
                           conflict and dependency scan
                                      |
                                      v
                         deterministic SimulationArtifact
```

The simulation explicitly reports at least:

```text
People       job, level, position, manager, organization, direct reports
Rewards      base pay, band, bonus target, currency and annualized delta
Finance      budget reservation, variable exposure and cost-center attribution
Payroll      earning-profile delta, cutoff and retro expectation
Access       grants, revocations, sensitive manager access and review requirement
Talent       management profile/competency changes
Learning     mandatory manager and jurisdictional training
Regulatory   jurisdiction, notice, consultation and training obligations
Documents    localized promotion letter and acceptance requirement
Messaging    employee, team and operational audience/delivery plans
Risk         aggregate and per-effect risk with required approvals
```

Unknown or unavailable domain results are represented as blocking/conditional
simulation findings. They are never silently omitted from the proposal.

## Reservations, approvals, and acknowledgement

Before approval, the plan declares whether position and budget checks are advisory
or durable reservations. A durable reservation has identity, amount/capacity,
proposal digest, expiry, renewal authority, consumption rule, and release rule.

Representative approval graph:

```text
CurrentManagerOf(Jane)
          |
HRBPFor(Jane)
          |
CompensationPartnerFor(Jane)
          |
raise exceeds customer threshold?
       /       \
     no         yes -> FinancePartnerFor(cost_center)
       \       /
          |
localized promotion document
          |
employee acknowledgement/signature when policy or law requires
```

Customer thresholds and jurisdiction-specific steps are compiled rule/workflow
configuration. Domain calculations and legal determinations remain owned by their
capabilities, not copied into workflow expressions.

## Execution-time revalidation

Immediately before material execution the plan reevaluates:

```text
worker and employment active
target job/level/position valid
position reservation current
budget reservation current and sufficient
manager and Team Phoenix relationship graph current
compensation band/rules current
no overlapping termination, transfer, leave, promotion, or org change conflict
approval authority and separation of duties still valid
document/acknowledgement obligations satisfied
LegalContext and applicable rule versions current
payroll cutoff/effective-date compatibility current
access policy and sensitive-entitlement risk current
```

Material change yields a new proposal revision and invalidates affected approvals.
An explicitly non-material change may proceed only when the definition's versioned
materiality policy proves that classification.

## Authoritative commit boundary

The local atomic core contains only state owned by the same transactional
authority and co-located commit coordinator:

```text
[safe_point]
    |
    v
multi-stream expected sequences
    |
    +-- employment/assignment job and level
    +-- position reservation consumption and occupancy
    +-- manager/organization relationship
    +-- compensation effective interval
    +-- budget reservation consumption (only if locally authoritative/co-located)
    |
    v
ONE ACID COMMIT
    ledger events + critical projections + intent transitions + outbox
```

If Finance or another domain is externally authoritative or cannot participate in
that local commit, its reservation/consumption is an external effect protected by
a fence and reconciliation contract. The architecture must not claim distributed
ACID across payroll, IAM, messaging, learning, document, or SaaS provider APIs.

## Downstream effect graph

```text
                       AUTHORITATIVE CORE COMMITTED
                                   |
       +---------------+-----------+-----------+---------------+
       v               v                       v               v
    PAYROLL           ACCESS                 TALENT          LEARNING
 sync + observe   recalc + provision     profile update     assignments
       |               |                       |               |
       +---------------+-----------+-----------+---------------+
                                   |
                      +------------+------------+
                      v                         v
                  DOCUMENTS                 MESSAGING
               archive evidence       employee/team/operations
                      |                         |
                      +------------+------------+
                                   |
                                   v
                         effect reconciliation
```

In P1B these effects run as sequential `OBSERVE` steps; the fan-out drawn here
is the post-P1B shape once `PARALLEL`/`JOIN` exist. Each effect declares
ordering key, idempotency key, retry budget, deadline, side-effect profile,
expected observation, compensation/repair policy, and criticality.
Team communication must not release before the governed effective point and any
required employee notification/acknowledgement ordering constraint.

## Honest partial completion and repair

Example observation:

```text
People        SUCCESS
Rewards       SUCCESS
Position      SUCCESS
Payroll       SUCCESS
Access        PARTIAL: compensation.direct_reports.read missing
Talent        SUCCESS
Learning      SUCCESS
Documents     SUCCESS
Messaging     employee delivered; team scheduled
```

The parent reports independent dimensions:

```text
RequestState       APPROVED         (closure policy decides when CLOSED)
ExecutionState     COMMITTED
BusinessState      COMPLETED
ConsistencyState   DEGRADED         (RepairPlan and incident linked)
ObligationState    SATISFIED
```

Jane is promoted. The system does not reverse or describe the promotion as failed
merely because one access effect failed. It creates a bounded repair:

```text
AccessDriftDetected
  -> diagnose rejected provisioning operation
  -> RepairPlan bound to expected entitlement and observation
  -> simulate
  -> approve only if IAM repair policy requires it
  -> redrive with original semantic idempotency identity
  -> observe
  -> reconcile
  -> ConsistencyState = CONSISTENT
```

Business closure requires the configured closure policy. It may permit business
completion before external consistency, but it cannot hide open repair, incident,
security, legal, document, or notification obligations.

## Capability composition contract

Representative capabilities are independently owned:

```text
people.worker.read
people.job.change
people.manager.change
positions.reserve
positions.fill
rewards.compensation.simulate
rewards.compensation.change
budget.compensation.reserve
regulation.obligations.resolve
approvals.resolve
documents.generate
documents.signature.request
payroll.worker.sync
access.entitlements.recalculate
learning.assign
communications.send
reconciliation.verify
```

The workflow owns coordination and customer variation. It does not own salary,
position, tax/legal, IAM, messaging, document, payroll, or learning semantics.

## Required conformance scenarios

1. Happy path with exact proposal, reservations, approvals, acknowledgement, commit, effects, and reconciliation.
2. Position or budget reservation expires before execution.
3. Manager/team/organization changes after approval and forces rebase.
4. Legal or compensation policy version changes before effective date.
5. Payroll cutoff makes the requested date invalid or retroactive.
6. Competing transfer, termination, leave, or promotion wins the conflict race.
7. Core commit succeeds and one downstream effect permanently fails.
8. Duplicate outbox delivery never duplicates payroll, access, learning, document, message, usage, or billing effects.
9. Sensitive access expansion requires step-up or additional approval.
10. Team notification is prevented from releasing early.
11. Repair succeeds without rewriting original business history.
12. Analytics/search/semantic systems are unavailable while the business transaction still executes safely.
