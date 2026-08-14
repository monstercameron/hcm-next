# Identity Resolution and Entity-Linkage Contract

The Identity Resolution Service owns person-candidate matching, do-not-merge,
canonical linkage, merge planning, and separation repair. The People Domain
consumes final resolutions; it does not implement a second matcher.

## Canonical State

```text
IdentityClaim
MatchCandidate
ResolutionCase
DoNotMergeConstraint
ExternalIdentityLink
MergePlan
SeparationPlan
FormerIdentifierRedirect
ResolutionDecision
```

```text
claim -> NORMALIZED -> CANDIDATES_FOUND
                       +-> AUTO_RESOLVED (only below configured risk)
                       +-> REVIEW_REQUIRED -> RESOLVED | NO_MATCH | DO_NOT_MERGE

MergePlan: DRAFT -> SIMULATED -> APPROVED -> FENCED -> EXECUTED
           -> VERIFIED | REPAIR_REQUIRED
SeparationPlan follows the same lifecycle and never erases the original merge.
```

## Capabilities and Matching

```text
identity_resolution.claims.submit|normalize
identity_resolution.candidates.search
identity_resolution.resolve|explain
identity_resolution.constraints.create|revoke
identity_resolution.links.create|correct|end
identity_resolution.merges.plan|simulate|approve|execute|verify
identity_resolution.separations.plan|simulate|approve|execute|verify
identity_resolution.redirects.resolve
```

Match signals are versioned and purpose-limited: verified identifiers, names,
addresses, contact points, dates, employment/candidate/external IDs, and governed
provider observations. Candidate results reveal the minimum fields needed for a
decision; fuzzy scores do not grant access to candidate records. Confidence
thresholds are tenant/domain/risk specific. No-match and do-not-merge are durable
decisions, not absence of a row.

## Merge and Separation Semantics

Every plan enumerates affected Person/Worker/Candidate/Employment identities,
external links, foreign references, assignments/positions, cases/documents/legal
holds, communications/inbox endpoints, accounts/AuthZ relationships, decisions,
analytics/semantic indexes, connectors, and retained former IDs. It classifies
each effect as relink, alias, preserve separate, reconcile external, regenerate
derived state, notify, or manual repair.

Execution fences concurrent identity writes, atomically commits co-located
identity aliases/redirects and ledger evidence, then runs idempotent downstream
repair and reconciliation. Former IDs permanently resolve through authorized
redirects without exposing the merged identity to unauthorized callers.
Separation creates new canonical linkage and reassigns facts through an approved
plan; it cannot pretend the false merge never occurred.

## Failure, Security, and Evidence

Ambiguous candidates, protected do-not-merge, conflicting verified identifiers,
active legal hold, unenumerated consumer, stale source, external-link ambiguity,
or failed fence blocks execution. Auto-merge is prohibited for high-risk or
cross-tenant cases. Merge author, reviewer, executor, and separation approver are
distinct for sensitive cases; step-up and dual control apply.

Claims and candidates are highly restricted PII. Search is tenant/purpose scoped,
field minimized, rate limited, and abuse monitored. Matching models/rules and
their bias/false-match performance are versioned and explainable. Evidence stores
claims as protected references, normalized values/hashes where appropriate,
candidate set, signals/scores, constraints, human rationale, plan/effects,
approvals, transactions, redirects, consumer receipts, reconciliation, and repair.

Gate A supports exact existing-worker lookup and externally mapped IDs only.
Fuzzy candidate resolution, auto-resolution, merge, and separation are Hire/import
conformance fixtures and deferred implementations.
