# Every-BusinessIntent Vertical-Slice Convergence Plan

## Outcome

Produce one exact, maximally configured, testable vertical-slice contract for
every accepted BusinessIntent and feed all unresolved semantic responsibilities
into the production TDD backlog without duplicating shared engines or expanding
delivery phase accidentally.

## Stage 0 — Recover source truth

1. Check in and digest the original numbered 530-candidate manifest.
2. Reconcile its number, stable identifier, display name and provenance against
   the 14 existing draft definitions and 807 repository vocabulary candidates.
3. Resolve aliases, collisions, extensions and missing names explicitly.
4. Generate exactly one accepted-definition row per numbered baseline entry.

Exit: zero `UNBOUND_SOURCE`, duplicate catalog number, unstable identity or
unreviewed baseline/extension collision.

## Stage 1 — Compile slice skeletons

For every accepted definition, combine `MAX-v1`, domain profile, workflow recipe,
intent delta and definition metadata into a complete `VerticalSliceRecord`.
Unknown owner, entity, property, capability, engine, governance rule, effect,
endpoint or evidence contract becomes a typed finding.

Exit: one slice record and graph digest per accepted definition; no silent
`NOT_APPLICABLE`.

## Stage 2 — Resolve semantic depth

Review slices by domain in this order:

```text
kernel/intent/governance/workflow/transaction/evidence
People + Organization + Position + Compensation promotion slice
Human Work + Messaging + Documents + Integration + Repair
Leave/Return-to-Work
Payroll + Regulatory + Benefits + Time/WFM
Recruiting + Lifecycle + Access
Talent + Experience + Cases + Mobility + Safety + Privacy
DataOps + Analytics + Agents + Operations + Commercial + Tenant + Triggers
```

Each review resolves entity properties, calculation ownership, decision rights,
external authority, correction, completion and maximal configuration
applicability. Shared behavior is extracted only after multiple slices prove the
same semantic contract.

Exit: every reference resolves to one owner or one atomic backlog gap.

## Stage 3 — Generate tests and endpoint dispositions

For each slice:

1. expand positive, boundary, forbidden, pairwise and mandatory high-risk
   configuration scenarios;
2. name exact unit/property/golden/fuzz/race/integration/fault/security/browser/
   conformance/recovery/benchmark/mutation tests that apply;
3. assign endpoint exposure or justified internal/event/schedule-only status;
4. assert returned and persisted state, events, effects, evidence and prohibited
   leakage for every scenario.

Exit: no placeholder oracle and no contracted behavior without channel and test
disposition.

## Stage 4 — Compile and deduplicate backlog gaps

Convert findings into atomic dependency-ordered todos. Deduplicate by semantic
owner and contract; retain reverse edges from the shared todo to every consuming
slice. Domain-specific invariants remain domain todos even when their workflow
shape is shared.

Exit: gap compiler reaches a fixed point: a second run over unchanged inputs
emits zero new todo identities and an identical graph digest.

## Stage 5 — Implement by authority gate

Only slices permitted by `execution-plan.md` become implementation work. Other
slices remain contracted/conformance/deferred records. A phase gate requires the
applicable maximal configuration suite, production limits, operating ownership
and current evidence.

## Reporting

Coverage always reports separate numerators and denominators:

```text
source-bound / 530 baseline slots
slice-draft / source-bound accepted definitions
contracted / slice-draft
implemented / phase-authorized contracted
verified / implemented
vocabulary candidates reconciled / 807 current candidates
```

No aggregate percentage may hide `UNBOUND_SOURCE`, deferred phase depth,
configuration exclusions, failing scenarios or stale evidence.
