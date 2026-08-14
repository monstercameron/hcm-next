# Compensation Domain Contract

The Compensation Domain owns compensation semantics and calculations. Workflow
coordinates a compensation change; it does not calculate salary, construct
compensation events, or write compensation storage.

## Canonical Model and Authority

```text
CompensationPackage
   +-- CompensationComponent[]
          type: BASE | HOURLY | ALLOWANCE | BONUS_TARGET | EQUITY_TARGET | OTHER
          value: money | rate | percentage | units
          pay_basis / frequency / period
          effective interval
          eligibility and source
   +-- PayBandReference
   +-- CalculationContext
```

Objects are `CompensationPackage`, `PackageRevision`, `CompensationComponent`,
`PayBasis`, `PayFrequency`, `ProrationRule`, `AnnualizationRule`, `BandPosition`,
`CompensationProposal`, `PayrollImpactPlan`, and `CompensationCorrection`.
Currencies, pay bands, frequencies, and component codes are governed references;
the package and its effective-dated values are domain facts.

SourceAuthority is declared by component, employment/legal entity, organization,
and interval. When an incumbent is authoritative, HCM Next records proposed
transactions and external observations rather than claiming the observed amount
as its own domain fact.

Budget checks use an explicit `BudgetAuthorityRef` from the [Workforce Budget
Authority Contract](workforce-budget-authority.md). Compensation does not infer
that valid Position capacity or a successful band check authorizes spend.

## Numeric and Temporal Rules

- Money and rates use fixed-precision decimal with schema-declared scale and
  rounding; binary floating point is forbidden.
- Every amount carries currency. No implicit tenant or worker currency exists.
- Pay basis and frequency are mandatory for rate/period amounts.
- Annualization records formula/version, periods, standard hours, FTE, currency,
  rounding points, and result. Display annualization is distinct from payroll
  payable amount.
- Proration is explicit by effective interval, work calendar, pay period, FTE,
  and rule version.
- Percentage components declare their base and stacking/order rule.
- Pay-band comparison records band/version, location/job/level scope, currency/FX
  version where conversion is necessary, and whether it is advisory or blocking.
- Retroactivity produces a `PayrollImpactPlan`; it never silently edits prior
  payroll or YTD accumulators.
- Corrections append superseding effective-dated assertions and preserve the
  originally approved/executed values.

## Lifecycle, APIs, and Events

```text
PROPOSED -> SIMULATED -> APPROVED -> SCHEDULED -> EFFECTIVE
                         |                         |
                         +-> CANCELLED             +-> CORRECTED
```

```text
compensation.read|timeline
compensation.simulate_change
compensation.band.read|evaluate
compensation.proposal.validate
compensation.change.execute
compensation.future.cancel
compensation.correct
compensation.payroll_impact.simulate|read
compensation.explain
```

Events include `CompensationChangeProposed`, `CompensationSimulated`,
`CompensationChangeScheduled`, `WorkerCompensationChanged`,
`CompensationFutureChangeCancelled`, `CompensationCorrectionRecorded`, and
`PayrollImpactPlanned`.

The domain returns validated planned appends for compensation, worker/employment
summary, transaction, decision references, and outbox. The Multi-Stream
Transaction Coordinator checks expected sequences and commits co-located streams
atomically. External payroll effects occur after commit through Integration and
are observed/reconciled.

## Failure, Security, and Evidence

Typed failures distinguish missing basis/currency, invalid component, band or
policy violation, illegal precision, stale package/band/FX/rule version,
overlapping future component, budget/reservation conflict, source-authority
denial, payroll-impact uncertainty, AuthZ/legal block, and reapproval requirement.

Compensation is a distinct high-sensitivity data domain. Read, simulate, propose,
approve, execute, correct, bulk, and export are separate capabilities. Field
visibility is enforced at repositories, domain response, search candidates,
analytics, agent/tool gateway, and egress. Requester/approver/executor separation
and step-up apply according to risk.

Evidence binds package and component revisions, exact decimal inputs/results,
formula/rule/band/FX/reference versions, source baseline and stream sequence,
budget/reservation, legal/AuthZ decisions, proposal canonical digest, approvals,
transaction/events, payroll impact, external observation, reconciliation, and
correction.

## Phase Depth and Acceptance

Gate A implements authorized current/future reads, band evaluation, deterministic
raise simulation, annualization explanation, and incumbent observation. Gate B
implements only base-pay and configured bonus-target changes required by the
pilot, with one currency/pay-basis family unless scope is exchanged. Payroll
calculation, retro pay execution, equity administration, and multi-country
compensation are conformance/deferred.

Golden cases cover salary and hourly bases, FTE/proration, rounding boundaries,
currency mismatch, band change, future overlap, stale proposal, retro impact,
field masking, correction, external partial failure, and deterministic replay.

All implementation is Go with Protobuf contracts, grpcbridge edges, GWC views,
and SchemaFlux-compiled calculation/conformance artifacts.
