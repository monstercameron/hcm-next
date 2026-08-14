# Source Authority and External Mastering Contract

The Authority Registry and Resolver determines which system may assert, propose,
write, correct, or observe a business fact for a specific scope and effective
interval. It never uses undocumented last-write-wins behavior.

## Canonical State

```text
AuthorityPolicy
  DRAFT -> VALIDATED -> APPROVED -> ACTIVE -> SUPERSEDED | QUARANTINED

AuthorityHandoffPlan
  PLANNED -> SIMULATED -> APPROVED -> DUAL_OBSERVE -> WRITE_FENCED
          -> CUTOVER -> VERIFIED -> COMPLETE
                        |             |
                        +-> ROLLBACK <-+
```

Objects are `AuthorityPolicy`, `AuthorityScope`, `AuthorityDecision`,
`SourceObservation`, `MasteringResolution`, `AuthorityHandoffPlan`,
`AuthorityFence`, `AuthorityActivationReceipt`, and `AuthorityDispute`.

Scope is explicit over tenant, organization/legal entity, domain, resource type,
resource/field or field set, operation class, source system/connection,
effective interval, recorded interval, and environment. A decision distinguishes:

```text
DOMAIN_FACT_OWNER
PERMITTED_PROPOSER
PERMITTED_WRITER
PERMITTED_CORRECTOR
EXTERNAL_OBSERVER
RECONCILIATION_SOURCE
```

## Resolution and APIs

Precedence is deterministic: exact resource/field/interval, then narrower
organization/legal-entity scope, then domain/tenant default. A policy declares
whether more-specific rules may override or only restrict inherited authority.
Equal-precedence conflicting grants return `AMBIGUOUS_AUTHORITY`; they never pick
by update time.

```text
authority.resolve|explain
authority.observations.record|compare
authority.disputes.open|resolve
authority.policies.propose|validate|approve|publish|quarantine
authority.handoffs.plan|simulate|approve|fence|cutover|verify|rollback|reconcile
authority.snapshots.status
```

Every hot-path decision uses a signed locally applied policy snapshot and records
its fingerprint. Expired or unknown policy fails closed for writes/corrections;
read observations remain labeled with unresolved authority. Future-dated policy
is evaluated at both proposal and execution. Imports and repairs require their
own allowed operation class.

## Handoff, Failure, Security, and Evidence

A handoff inventories source baselines, freezes or fences old/new writers,
dual-observes changes, establishes cutover watermark, activates new authority,
reconciles, then removes the old writer. External fencing that cannot be proven
produces degraded/ambiguous state and blocks dual writing. Rollback restores the
prior signed policy and fences the failed writer; it never deletes intervening
observations.

Policy author, domain owner, security approver, and activation executor are
separate capabilities. Cross-legal-entity or payroll/compensation authority
changes require step-up and dual control. Tenant administrators cannot modify
global provider eligibility; support cannot grant itself authority.

Evidence records input scope/time, candidate policies, precedence trace,
decision/result, snapshot/version, actor/purpose, source observations, dispute,
handoff simulation/approvals/fences/watermarks, activation receipts,
reconciliation, rollback, and correction. Decisions are retained with the
transactions/observations they governed.

## Phase Depth

Gate A implements read/observe authority and explanation for pilot fields. Gate B
implements exact permitted-writer enforcement, future revalidation, incumbent
writeback authority, dispute blocking, and a simulated rollback. General
authority handoff is conformance-only.

Fixtures cover ambiguous overlapping policy, stale snapshot, future ownership,
import, repair, dual-writer attempt, external observation versus domain fact,
handoff fence failure, cutover, and rollback.
