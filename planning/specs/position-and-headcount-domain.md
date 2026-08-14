# Position and Headcount Domain Contract

Positions are temporal business resources, not reference codes. This domain owns
authorized capacity, occupancy, vacancy, headcount budget, workforce-plan links,
and their effective-dated corrections. Job, level, location, cost center, and pay
grade remain governed reference data referenced by a position version.

## Authority and Boundaries

```text
Workforce plan / budget authority
              |
              v
       Position Domain
 create / revise / reserve / fill / vacate / close
              |
       ACID ledger + projection
              |
    +---------+----------+
    v                    v
Workflow             Reconciliation
coordinates          compares external HCM/finance
```

The Position Domain owns canonical position state only when source-authority
policy assigns that field/time interval to HCM Next. Otherwise it owns proposals,
transactions, and external observations while reconciling the incumbent source.
Master Data owns referenced job/location/cost-center concepts. Workflow may call
position capabilities but cannot mutate position tables or emit position events.

## Canonical State

```text
Position
  position_id, tenant_id, owning_org, legal_entity, status

PositionVersion
  effective_interval, job_ref, level_ref, location_ref, cost_center_ref,
  worker_type, pay_grade_ref?, attributes

CapacityInterval
  effective_interval, capacity_fte, capacity_heads, overfill_policy

Occupancy
  occupancy_id, employment_id, effective_interval, fte, occupancy_type,
  source_transaction, status

Vacancy
  derived interval and available FTE/heads; never independently edited

HeadcountBudget
  BudgetAuthorityRef to a HEADCOUNT_CAPACITY budget; no local implicit budget

WorkforcePlan
  scenario/version, planned positions/capacity, approval and publication state

PositionReservation
  proposal/workflow, interval, reserved FTE/heads/budget, expiry, fencing token
```

Position lifecycle:

```text
DRAFT -> OPEN -> RESERVED? -> PARTIALLY_FILLED | FILLED
                        |             |
                        +-> VACANT <--+
                               |
                         FROZEN -> CLOSED

Correction appends a new effective-dated assertion; it never erases occupancy.
```

`PositionReportsToPosition` is owned by the [Organization and Relationship
Domain](organization-and-relationship-domain.md). Position projections may expose
the typed relationship and graph watermark for convenience, but PositionVersion
does not create a second authoritative manager hierarchy.

Occupancy may be primary, job-share, temporary, acting, matrix/project, or
planned. Only types declared by the position's occupancy policy consume capacity.

## Arithmetic and Invariants

For every effective instant:

```text
consumed_fte = sum(active capacity-consuming occupancies.fte)
reserved_fte = sum(active fenced reservations.fte)
available_fte = capacity_fte - consumed_fte - reserved_fte

consumed_heads <= capacity_heads unless an approved overfill applies
consumed_fte   <= capacity_fte   unless an approved overfill applies
```

- FTE is fixed-precision decimal with declared scale and rounding; never float.
- Effective intervals use half-open `[from,to)` semantics.
- One employment cannot hold overlapping primary occupancy where policy forbids
  it; multiple employments and job shares remain independently identifiable.
- Manager-position edges cannot create prohibited organization cycles.
- Closing a position requires zero future occupancy/reservations or an approved
  migration/cancellation plan.
- Job/location/cost-center retirement is blocked or migrated through dependency
  impact; no dangling reference is allowed.
- Budget availability and position capacity are separate checks. Passing one
  never implies the other.
- All budget checks and reservations use the [Workforce Budget Authority
  Contract](workforce-budget-authority.md); Position owns capacity, not a generic
  finance or compensation budget.
- Vacancy is a temporal derivation. A position with 0.4 available FTE is not
  necessarily an available whole-head vacancy.

## Commands, Queries, and Events

```text
position.create
position.revise
position.reserve
position.reservation.renew|release
position.fill
position.vacate
position.freeze|unfreeze
position.close
position.correct

position.read
position.capacity.at
position.occupancy.timeline
position.vacancies.query
position.budget.check
position.conflicts.query
position.explain
```

Material events include `PositionCreated`, `PositionRevised`,
`PositionCapacityChanged`, `PositionReserved`, `ReservationExpired`,
`PositionFilled`, `PositionVacated`, `PositionOverfillApproved`,
`PositionFrozen`, `PositionClosed`, and `PositionCorrected`.

Each command supplies expected position sequence, affected field/effective range,
idempotency key, proposal/approval hash when applicable, principal and purpose,
reference-data fingerprints, and source-authority context. The position stream,
employment/worker transaction streams, budget reservation, and outbox commit
atomically through the Multi-Stream Transaction Contract when co-located.

## Failure, Security, and Evidence

- Capacity, budget, overlapping reservation, stale reference version, authority,
  or effective-date conflicts return typed failures and current conflicting state.
- Cross-shard/domain effects use a plan and reconciliation; they are not described
  as atomic.
- Authorization evaluates capability, owning organization, proposed destination
  organization, fields, population, purpose, legal entity, and risk.
- Compensation values are not stored merely because a position references a pay
  grade. Workforce-plan and budget visibility can be more restrictive than
  general organization visibility.
- Bulk position operations require separate capability, population bounds,
  simulation, dual approval, and resumable execution.

Evidence includes prior/current versions, calculations, reference versions,
budget result, conflicts, reservation token, proposal and approval binding,
AuthZ/legal decisions, transaction/ledger IDs, external observations,
reconciliation, corrections, and retained explanation.

## Phase 1 Depth

Phase 1 implements only the position read/capacity/reservation checks needed to
validate Promotion and Compensation Change. The incumbent remains authoritative
unless the pilot contract explicitly grants position authority. Position creation,
workforce planning, job shares, overfill execution, and budget ownership are
conformance fixtures, not pilot product promises.

Acceptance requires:

1. A promotion to an invalid/closed/incompatible position is blocked.
2. Competing reservations produce one winner and an explainable conflict.
3. A future position revision invalidates or reapproval-routes a stale proposal.
4. Position and reference-data ownership remain separate in code and schemas.
5. Reconciliation distinguishes HCM Next transaction truth from incumbent
   external observation.

## Go-Only Realization

The domain, APIs, projections, simulation, and tests are Go. Protobuf is the
canonical contract; grpcbridge exposes approved web/API access; GWC renders admin
views; SchemaFlux compiles reference/dependency and conformance artifacts.
