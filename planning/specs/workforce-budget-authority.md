# Workforce Budget Authority Contract

“Budget” is not one quantity. This contract distinguishes position/headcount
authorization, compensation-pool allocation, and finance cost authority while
allowing Phase 1 to bind to an incumbent finance/planning system.

## Budget Types and Authority Reference

```text
HEADCOUNT_CAPACITY     positions / heads / FTE
COMPENSATION_POOL      merit/promotion amount by cycle/scope
FINANCE_COST_BUDGET    money by cost center/period/account
```

All three are workforce budgets: quantities the customer's finance or planning
function authorizes for people decisions. Platform execution, AI, and API
allowances are a different thing, owned by the Entitlement, Metering, and
Billing plane; they never appear as a `BudgetAuthorityRef` and a workforce
reservation never consumes them.

Phase 1 binds only `COMPENSATION_POOL` (P1B) and reads `HEADCOUNT_CAPACITY`
through the Position domain. `FINANCE_COST_BUDGET` is a contract with no
Phase 1 consumer.

Every plan uses:

```text
BudgetAuthorityRef
  budget_type
  owner_system / connection
  authority_policy_fingerprint
  scope
  period
  currency?
  unit: HEAD | FTE | MONEY | PERCENT
  baseline_version / external_watermark
  available_quantity
  reservation_id?
  reservation_expiry?
  observation_id
```

The Workforce Budget Service owns a budget only where SourceAuthority assigns it.
Otherwise it owns checks, reservation intents, observations, and reconciliation
against the incumbent Finance/Planning authority.

## State, APIs, and Arithmetic

```text
Budget: DRAFT -> APPROVED -> ACTIVE -> CLOSED -> CORRECTED?
Reservation: REQUESTED -> HELD -> COMMITTED | RELEASED | EXPIRED
             ambiguous external result -> RECONCILIATION_REQUIRED
```

```text
budgets.read|available|explain
budgets.simulate_allocation
budget_reservations.acquire|renew|commit|release|status|reconcile
budgets.adjust|correct
budget_authority.resolve
```

Each budget type declares fixed-precision unit/scale, inclusion rules, period,
rollover, currency, FX purpose/version, rounding points, negative/overdraft
policy, hierarchy allocation, concurrent reservation semantics, and adjustment
authority. Passing headcount capacity never implies compensation or finance
budget, and vice versa.

Co-located authoritative reservation uses the Multi-Stream Transaction Contract.
External reservation uses a fenced idempotent connector operation, records
ambiguous outcomes, and blocks execution until observed/reconciled. Approval does
not extend an expired reservation. Cancellation and failed execution release the
hold idempotently; inability to prove release remains an obligation.

## Failure, Security, and Evidence

Typed failures include wrong type/unit/currency/period, stale baseline, exhausted
allocation, concurrent hold, expired reservation, authority denial, FX/rule
missing, external ambiguity, release ambiguity, closed budget, and correction
requiring reapproval.

Budget read, reserve, approve, adjust, correct, bulk, and export are separate
capabilities with tenant/org/cost-center/field/purpose scope. Requester,
compensation approver, finance approver, and budget adjuster obey declared
separation of duties. Financial detail can be more restricted than Position or
Compensation visibility.

Evidence binds authority, budget/version/watermark, calculation and FX, requested
and available units, competing reservations, hold/expiry, proposal digest,
approvals, commit/release attempts, external observations, reconciliation,
adjustments, and correction.

Gate A reads/simulates design-partner budget observations. Gate B is explicitly
bound either to one incumbent Finance/Planning adapter or one narrow co-located
promotion pool; it does not claim a general budgeting product. Tests cover two
competing reservations, expiry after approval, finance-side change, ambiguous
external hold/release, cancellation release, FX mismatch, and correction.
