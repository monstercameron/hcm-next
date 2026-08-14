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

## A11 Regulated filing or irreversible submission

```text
resolve jurisdiction, filing authority, period and deadline
-> freeze governed source population and rule/data releases
-> calculate and validate filing package
-> resolve exceptions and collect exact-package approval/signature
-> revalidate authority, deadline, credentials and package digest
-> submit once behind an irreversible-effect checkpoint
-> observe acknowledgement/acceptance/rejection
-> reconcile obligation; amend/correct through a successor filing intent
```

## A12 Case, investigation or protected service matter

```text
classify intake and disclosure mode
-> create case revision, participants, compartments, SLA and retention/hold policy
-> triage conflicts, jurisdiction, safeguarding and immediate obligations
-> assign governed human work and collect minimized evidence
-> perform interviews/analysis/decisions with provenance
-> create separately governed mutation/filing/remedy child intents
-> communicate permitted outcomes
-> resolve/close with appeal and reopen policy; preserve chronology
```

## A13 Communication, notice or acknowledgement

```text
resolve purpose, audience/population and permitted fields
-> freeze recipient and content/template/rule versions
-> apply preference, legal-delivery, localization, accessibility and DLP policy
-> approve high-risk/bulk/legal content when required
-> render per recipient and dispatch idempotently
-> observe delivery/read/acknowledgement without equating transport acceptance to receipt
-> retry/fallback/escalate and reconcile every recipient
```

## A14 External integration or synchronization operation

```text
resolve connector, connection, authority, schema and mapping revisions
-> validate scope, credentials, classification, ordering and provider limits
-> simulate canonical request/expected observation
-> journal an idempotent operation and revalidate at dispatch
-> dispatch through a dumb provider adapter
-> record attempts and external observations
-> reconcile intended/authoritative/observed state
-> redrive, quarantine or create RepairPlan without fabricating success
```

## A15 Workforce access, identity or entitlement change

```text
resolve worker identity graph, lifecycle facts and expected-access policy
-> calculate requested versus expected accounts/entitlements/devices/physical access
-> apply AuthZ, SoD, risk, step-up and approval/certification policy
-> freeze proposal and revalidate worker/role/manager/risk at effect time
-> commit expected access state and ordered provider effects
-> observe actual access independently
-> reconcile drift and repair/revoke under urgency policy
```

## A16 Document, form, signature or evidence lifecycle

```text
resolve purpose, subject, template/schema, classification and retention/hold policy
-> collect/generate content through restricted artifact references
-> scan, validate, classify, extract/redact and bind provenance
-> approve/review where required
-> render/sign/acknowledge under identity-assurance ceremony
-> store immutable artifact revision and evidence receipt
-> deliver or expose by minimum-necessary policy
-> expire/supersede/correct without overwriting history
```

## A17 Triggered or scheduled intent activation

```text
resolve trigger/schedule definition and target capability
-> authenticate origin and validate event/time/calendar version
-> deduplicate firing and evaluate admission/governance
-> create typed BusinessIntent with causation/correlation evidence
-> execute the target intent's own recipe
-> record trigger outcome separately from target outcome
-> retry/quarantine trigger without duplicating target effects
```

## A18 Reconciliation, repair or governed operation

```text
detect finding from authoritative expectation and fresh observations
-> classify mismatch, ambiguity, severity and ownership
-> diagnose evidence-backed cause
-> generate and simulate immutable RepairPlan/operation proposal
-> authorize, approve and revalidate current state
-> execute bounded corrective actions idempotently
-> reobserve and reconcile
-> close only on verified outcome; escalate/quarantine otherwise
```

## A19 Configuration, definition or publication lifecycle

```text
create immutable draft revision
-> validate schema, dependencies, security and compatibility
-> simulate impact against frozen representative fixtures/populations
-> review and approve exact digest
-> publish/promote/future-activate through progressive rollout
-> monitor behavior and compare expected outcomes
-> pause/rollback/quarantine/supersede without mutating published history
```

## A20 Plan, scenario or lifecycle composition

```text
resolve baseline, population, assumptions, requirements and authority
-> build immutable scenario/plan and dependency graph
-> simulate cost, risk, capacity, legal obligations and child-intent outcomes
-> compare alternatives and approve exact selected revision
-> compile bounded, ordered child BusinessIntents
-> execute/observe each child independently
-> compare plan versus actual and replan/repair without hiding partial outcomes
```

## D1 Governed direct capability (no durable workflow)

```text
authenticate purpose and scope
-> resolve pinned facts/rules/data and completeness
-> authorize fields/population/output
-> execute deterministic read/calculation/validation
-> return typed result, uncertainty, exclusions and trace
-> record access/calculation evidence
```

`D1` is an intentional non-workflow disposition. A durable instance is introduced
only when waits, human work, external effects, retries, deadlines or multi-step
obligations actually require it.
