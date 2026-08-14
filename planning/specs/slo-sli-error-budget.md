# SLO, SLI, Error Budget, and Reliability Obligation Contract

The Reliability Management Service owns SLI definitions, SLO versions,
measurement validity, error budgets, burn response, exclusions, and reliability
obligations. Telemetry systems provide observations but cannot redefine success.

## Canonical State

```text
SLIDefinition
SLOVersion
MeasurementWindow
MeasurementResult
ErrorBudget
BurnAlert
MeasurementExclusion
ReliabilityObligation
```

```text
SLI/SLO: DRAFT -> VALIDATED -> APPROVED -> ACTIVE -> SUPERSEDED | RETIRED
Window:  OPEN -> MEASURED -> VALIDATED | UNKNOWN -> CLOSED
Budget:  HEALTHY -> BURNING -> EXHAUSTED -> RECOVERING
```

An SLI fixes source of measurement, event population, `good`, `valid`, `total`
semantics, latency/freshness/completeness calculation, tenant/cell/capability
scope, aggregation, windows, required telemetry health, and version. An SLO fixes
target, window, customer-contract status, consequences, dependencies, and owner.

## APIs and Operating Policy

```text
reliability.sli.propose|validate|publish
reliability.slo.propose|approve|publish|retire
reliability.measure|explain|status
reliability.error_budget.read|forecast
reliability.exclusions.request|approve|expire
reliability.obligations.query|satisfy
reliability.reconcile
```

Missing/dropped/invalid telemetry above the declared bound makes measurement
`UNKNOWN`, never healthy. Numerator, denominator, window, and exclusions are
immutable within an SLO version. Customer and internal objectives are labeled;
internal targets cannot be presented as contractual commitments.

Multi-window burn policy creates typed alerts/obligations and may freeze risky
releases, shed/degrade lower priority work, increase sampling, or require incident
declaration. Maintenance exclusions require preapproval, exact scope/time,
customer-contract compatibility and expiry. Retroactive exclusion is prohibited
except correction of demonstrably invalid measurement under dual review.

## Security and Evidence

SLI authors, service/domain owners, reliability approvers, commercial approvers
for contractual SLOs, exclusion approvers and incident commanders are distinct as
required. Tenant SLO data and infrastructure topology are scoped/redacted.

Evidence includes definitions/versions, queries/collector versions, raw aggregate
references, telemetry completeness, windows/results, exclusions and approvals,
budget calculations/burn alerts, triggered degradation/freeze/incident/
communication, reconciliation and corrections.

Gate A measures read/simulation availability, p95 latency, source/projection
freshness and connector observation completeness. Gate B adds command success/
latency, workflow timer wake-up, external reconciliation, repair latency,
idempotency/duplicate effects, and durability/restore objectives.
