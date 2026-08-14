# Governance Decision and Obligation Composition Contract

The Governance Decision Coordinator composes authoritative subdecisions from
AuthZ, Legal/Regulatory, Privacy/DLP, Entitlement, Risk, Source Authority, and
applicable commercial/records policy. It never overrides their source authority.

## Canonical State

```text
GovernanceRequest
GovernanceSubdecision
GovernanceDecision
EffectiveFieldScope
ObligationSet
GovernanceContradiction
ReevaluationSet
GovernanceReceipt
```

Decision states are `ALLOW`, `ALLOW_WITH_OBLIGATIONS`, `DENY`,
`CONTRADICTORY_REQUIREMENTS`, and `UNKNOWN_FAIL_CLOSED`.

Composition rules:

- Any mandatory deny blocks; one authority's allow never cancels another's deny.
- Resource, population, field, data, destination, purpose, time, and autonomy
  restrictions intersect.
- Required approvals, notices, redactions, transformations, residency, step-up,
  logging, retention, review, and reconciliation obligations union and deduplicate
  by typed identity/version/scope.
- Mutually impossible obligations produce `CONTRADICTORY_REQUIREMENTS` with an
  explicit contradiction set; no evaluation order resolves them silently.
- Unknown, stale, timed-out, unsigned, or incomplete mandatory subdecision fails
  closed for material actions.

## APIs, Freshness, and Evidence

```text
governance.evaluate|explain|status
governance.obligations.validate|satisfy|status
governance.revalidate
governance.contradictions.resolve
```

A request binds principal/delegation/session assurance, tenant/org/subject,
capability/version, current/proposed fields, purpose, effective/recorded time,
execution mode, destination, classification, source authority, risk and proposal/
transaction digest. Each subdecision records source service, policy snapshot,
decision time, maximum age, invalidators and evidence reference.

Cached decisions are eligible only when every subdecision permits caching and no
invalidator/watermark changed. Execution uses a new or still-valid bound decision;
proposal-time allow does not imply execution-time allow. Obligation validation
ensures the TransactionPlan/Workflow has implementable owners and typed steps.

The coordinator uses a scoped workload identity and cannot edit source policies.
Contradiction resolution occurs in source authorities or by changing the business
plan, never by coordinator override. Evidence records the request, all subdecision
digests/freshness, composition trace, intersections/unions, contradictions,
effective decision, obligations/owners, invalidators, revalidation and satisfaction.

Gate A implements read/simulation composition. Gate B adds commit-time
revalidation and tests AuthZ-allow/DLP-deny, entitlement-allow/legal-deny,
intersecting masks, stale authority, and impossible obligations.
