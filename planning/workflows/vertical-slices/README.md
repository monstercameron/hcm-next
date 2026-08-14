# BusinessIntent Vertical-Slice Program

## Objective

Every accepted BusinessIntent must have one reviewable maximal vertical slice
from request boundary through final outcome evidence. The slice answers what must
exist to implement and operate that intent; it does not imply the intent is in
the current delivery phase.

The registers distinguish two populations:

1. `baseline-vertical-slice-register.md` contains exactly the accepted numbered
   slots `1..530`. Fourteen names are currently known as draft contracts and 516
   remain `UNBOUND_SOURCE` until the original immutable candidate manifest is
   checked in.
2. `vocabulary-vertical-slice-register.md` contains every unique material intent
   name currently enumerable from the six detailed workflow catalogs and the
   compact BusinessIntent workflow registry. These are candidate mappings, not
   automatically accepted baseline identities.

The two populations may only be joined by stable catalog identity and reviewed
provenance. Display-name similarity is insufficient.

## Complete vertical-slice record

```text
VerticalSliceRecord
  slice_id
  intent_definition_ref or candidate_name
  source_provenance / source_status
  owner plane/domain/profile
  kernel family
  archetype / execution disposition
  maximal_configuration_profile_ref
  intent-specific applicability and exclusions
  initiators/channels/endpoints
  untrusted request / trusted context
  pre-state and authoritative snapshot
  entity/property reads and freshness
  governance decisions/obligations/restrictions
  reusable engines and domain capabilities
  workflow steps, waits, signals and human decisions
  proposal/read/write/effect/conflict sets
  transaction and irreversible boundary
  authoritative post-state and ledger evidence
  external operations and observations
  reconciliation/completion/repair
  cancellation/correction/supersession/retention
  positive/negative/configuration/fault/security scenarios
  exact endpoints, tests, todos and evidence
  phase/maturity/owner/review status
```

No field is inferred from the display name. `NOT_APPLICABLE` requires a typed
reason and reviewer. Unknown is a blocking state, not an empty collection.

## Slice expansion

Each compact record expands deterministically as:

```text
MAX-v1 global configuration axes
  + domain profile
  + workflow archetype recipe
  + intent-specific delta
  + accepted IntentDefinition contract
  + current entity/engine/capability/endpoint registries
  = ExpandedVerticalSlice
```

The global profile forces every relevant configuration axis to be considered.
The domain profile provides default data, controls, engines and effects. The
archetype provides the full ordered execution spine. The intent delta changes or
adds domain semantics but cannot remove mandatory governance, revalidation,
evidence, failure, correction or reconciliation responsibilities silently.

## Maximal does not mean impossible Cartesian explosion

“Maximal configuration” means every supported configuration value and material
interaction is represented in applicability metadata and testing. It does not
mean executing the full Cartesian product.

The scenario compiler uses:

- one positive vector for every applicable value;
- every boundary and forbidden value;
- pairwise interaction coverage across ordinary axes;
- three-way or explicit scenario coverage for high-risk interactions;
- all mandated concurrency, ambiguity, irreversible-effect, privacy and recovery
  combinations;
- production-scale benchmarks for declared maximums.

An untested combination requires an explicit equivalence proof, not silence.

## Record maturity

```text
UNBOUND_SOURCE       numbered identity/name/source unavailable
CANDIDATE_MAPPED     vocabulary name has profile/archetype/delta source
SLICE_DRAFT          all slice fields exist but referenced contracts have gaps
SLICE_CONTRACTED     references resolve and scenario/todo graph is reviewable
SLICE_IMPLEMENTED    production path and tests exist for authorized phase
SLICE_VERIFIED       current evidence passes maximal applicable configuration
```

`UNBOUND_SOURCE` and `CANDIDATE_MAPPED` are valuable planning states, but neither
may be presented as a contracted or implemented BusinessIntent.

## Output and gap feedback

Expansion produces:

```text
slice manifest + graph digest
configuration applicability matrix
entity/property read-write-effect inventory
governance/human-decision matrix
endpoint and initiator dispositions
scenario/test manifest
atomic missing-contract findings
dependency-ordered todo candidates
```

Findings deduplicate on semantic owner plus missing contract, so 300 intents that
need the same population snapshot create one shared-engine todo and 300 coverage
edges rather than 300 implementations.

## Truthful current coverage

Current planning may report separately:

```text
accepted baseline slots        530
named baseline draft contracts  14
unbound baseline slots         516
repository vocabulary names    807
```

It may not report `530/530 designed` until the signed source manifest is joined
and every baseline slot reaches at least `SLICE_DRAFT` with zero ambiguous alias.
