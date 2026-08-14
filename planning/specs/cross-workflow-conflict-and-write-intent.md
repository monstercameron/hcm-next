# Cross-Workflow Conflict and Write-Intent Contract

The Conflict Registry owns normalized active write intents and conflict decisions.
Domain services own semantic conflict rules. A preflight result is advisory until
the conflict state is validated and fenced at commit.

## Canonical State

```text
WriteIntent
ReadBaseline
ConflictRule
ConflictSnapshot
ConflictDecision
IntentDependency
Supersession
SerializationLease
ConflictResolution
```

```text
DRAFT -> PREFLIGHTED -> SUBMITTED -> APPROVED -> RESERVED
      -> EXECUTING -> EFFECTIVE -> RELEASED
                  |-> CANCELLED | SUPERSEDED | REPAIR_INTENT
```

An intent binds tenant, workflow/business intent, subject/resources, normalized
field paths, operation semantics, effective half-open ranges, baseline stream/
projection/source versions, domain rule versions, priority/risk, dependencies,
and intended writes/effects.

## APIs and Conflict Semantics

```text
conflicts.intents.register|update|release|cancel
conflicts.query|explain
conflicts.rebase|supersede|resolve
conflicts.serialize.acquire|renew|release
conflicts.validate_at_commit
conflicts.rules.publish|read
```

Field paths use schema-registry canonical identities, not display/JSON names.
Rules explicitly define exact-field, parent/child, semantic alias, aggregate,
resource-capacity, budget, effective-range, and cross-domain dependencies.
Intervals touching at `[a,b)` and `[b,c)` do not overlap unless a domain rule says
transition adjacency conflicts. `MERGEABLE` requires a domain-supplied deterministic
merge proof and new proposal digest; the registry never merges arbitrary writes.

## Race Closure and Failure

Preflight records a `ConflictSnapshot` and expected conflict/domain sequences.
At commit, the Transaction Coordinator locks/checks affected domain streams and
conflict buckets in deterministic order, validates expected sequences, commits
domain events plus intent transition/outbox atomically, then publishes the new
conflict watermark. If co-location is impossible, a fenced `SerializationLease`
must cover the normalized scope through commit and reconciliation.

Two proposals may both preflight successfully; only a commit with current
sequences/fence may succeed. Stale, unavailable, incomplete, or unversioned
conflict state fails closed for material writes. Repair intents declare whether
they supersede, serialize behind, or intentionally conflict with the damaged
transaction under separate repair authority.

Typed decisions are `NO_CONFLICT`, `CONFLICT`, `SERIALIZE_AFTER`, `REBASE_REQUIRED`,
`MERGEABLE_WITH_PROOF`, `SUPERSEDE_ALLOWED`, and `UNKNOWN_FAIL_CLOSED`.

## Security and Evidence

Callers can see only conflicts within authorized subject/org/field scope; an
unauthorized conflict is disclosed as a blocking opaque dependency. Register,
resolve, supersede, force-serialize, and repair are distinct capabilities.
Conflict administrators cannot authorize underlying domain writes.

Evidence stores intent and normalized scope, baselines, snapshot/watermarks,
rules/versions, candidates, decision/explanation, opaque redactions, lease/fence,
commit validation, winning/losing transactions, rebase/merge proof,
supersession/resolution, and release.

Gate A implements conflict query/simulation over incumbent and proposed changes.
Gate B implements durable intent registration, commit-time closure, reservation
integration, and races where two preflights pass but one commit conflicts.
