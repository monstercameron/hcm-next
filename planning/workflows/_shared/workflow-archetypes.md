# HR Workflow Archetypes

An archetype supplies the complete generic phase sequence. A catalog row is a
complete exploratory workflow description only when it names an archetype and
states its unique reads, writes, dependencies and step deltas.

## A1 Create governed record

```text
discover authority/reference context
-> collect typed fields and evidence
-> normalize/validate/quality/invariant checks
-> simulate new record, collisions and downstream effects
-> approve/reserve when policy requires
-> revalidate uniqueness, authority and references
-> atomic create + ledger + projection + outbox
-> external effects -> observe -> reconcile -> close/repair
```

## A2 Revise effective-dated fact

```text
resolve current fact and expected version
-> collect proposed revision/reason/effective range
-> validate + conflict-check overlapping future changes
-> simulate diff/obligations/effects
-> exact-digest approval when required
-> wait -> revalidate mutable assumptions
-> atomic end/supersede + append revision + evidence/outbox
-> observe -> reconcile -> close/repair
```

## A3 End or suspend relationship

```text
resolve active relationship/dependents
-> collect reason/effective boundary/evidence
-> determine cancellation and irreversible boundaries
-> simulate downstream closures/restoration obligations
-> approve -> wait -> revalidate
-> atomic end/suspend + dependent facts/outbox
-> observe downstream state -> reconcile -> close/repair
```

## A4 Reactivate, reinstate or rehire

```text
resolve historical relationship and why it ended
-> distinguish correction, reinstatement, reactivation and new relationship
-> validate eligibility/identity/authority/reference changes
-> simulate restoration versus new identifiers/effects
-> approve -> revalidate
-> append restoration/new facts without erasing prior history
-> reprovision/observe/reconcile -> close/repair
```

## A5 Correct historical fact

```text
resolve prior assertion and known-at/effective-at timelines
-> collect correction reason/evidence
-> reconstruct expected historical state and affected set
-> simulate downstream retro/projection/report effects
-> approve with SoD -> revalidate
-> append corrective fact; never overwrite history
-> replay/recalculate/redrive affected representations
-> observe/reconcile/verify correction -> close
```

## A6 Explain, reconstruct, compare or export

```text
authenticate purpose and resolve field/population scope
-> resolve as-of/known-at/source-authority context
-> query authoritative facts + provenance + corrections
-> apply field/row/artifact authorization and DLP
-> calculate explanation/comparison/export deterministically
-> label unknown/redacted/incomplete results
-> record access/export evidence and deliver safely
```

## A7 Reserve or release resource

```text
resolve scarce resource, authority, quantity and current fence
-> validate availability/budget/freeze/conflicts
-> simulate contention and expiry
-> approve if required
-> atomically create/renew/release fenced reservation
-> reconcile reservation with external authority
-> consume, expire or repair without double allocation
```

## A8 Composite organizational change

```text
resolve affected graph and authoritative resources
-> collect desired target structure
-> calculate affected population and dependency graph
-> validate graph/legal/budget/access invariants
-> simulate child proposals and migration sequence
-> collect conditional approvals/consultations
-> reserve/freeze -> revalidate
-> execute atomic local batches and ordered external effects
-> observe/reconcile every child and repair partial state
```

## A9 Governed calculation or analysis

```text
resolve purpose, population, fact/rule/metric versions
-> validate completeness and authorization
-> calculate deterministically or execute governed analysis plan
-> produce trace, uncertainty, exclusions and protected output
-> record evidence; optionally create a separate proposed BusinessIntent
```

## A10 Cycle, batch or campaign

```text
define population/query and frozen source watermark
-> build per-subject proposals/calculations
-> validate, conflict-check and estimate cost/capacity
-> approve batch/cycle policy and individual exceptions
-> execute resumable partitions with per-item idempotency
-> observe/reconcile item and aggregate results
-> repair failures without rerunning successful items
```
