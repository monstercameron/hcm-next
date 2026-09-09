# Human Capital Management Suite User-Story Corpus

A corpus of one thousand user stories that exercise the workflows in
[`../workflows/catalog.md`](../workflows/catalog.md). Each story is written so
that it can be turned into a test without a second conversation: the
preconditions are concrete, the acceptance criteria are Given/When/Then blocks
with exact expected states, and the failure modes name the typed refusal the
system must produce rather than saying that an error occurs.

Nothing here is implementation evidence. A story describes what the system must
be able to do; it does not assert that anything does it yet. The phase gates in
[the plan](../plan.md) and [the execution plan](../execution-plan.md) continue
to decide what is built.

## Why this exists

Two purposes, and they pull in slightly different directions, which is why both
are stated.

**Aligning the code for correctness.** The repository's contracts are written
from the platform outward: kernel families, lifecycle dimensions, capability
governance, proposals and digests. This corpus comes at the same surface from
the other side, as the sequence of things a person actually tries to do. Where a
story cannot be expressed in the platform's own vocabulary, that is a finding
about the vocabulary, not about the story. Where a story's expected refusal has
no typed code to carry it, that is a gap in the refusal taxonomy. The corpus is
therefore a foil for the contracts, not a restatement of them.

**Benchmarking the system later.** The tier distribution is deliberate. A
benchmark built only from happy paths measures throughput and nothing else; a
benchmark built only from adversarial cases measures a security posture and
tells you nothing about whether ordinary work completes. The corpus mixes them
in known proportions so that a later benchmark can report per-tier results and a
regression in tier 4 is not hidden by a passing tier 1.

A third, quieter use: the corpus is a check on the workflow catalogue itself. A
dimension that produces thin, repetitive stories is a dimension whose workflows
were not thought through.

## Files

```text
README.md                  this file
stories-0001-0100.md       stories in blocks of one hundred, human-readable
...
stories-0901-1000.md
stories.jsonl              one JSON object per story, same fields, machine-readable
quality-report.md          adversarial review rounds, findings, fixes and final distribution
```

The Markdown files and the JSONL are generated from one source, so they cannot
drift: every field the Markdown shows is present in the JSON object with the
same value.

## Schema

Each story carries these fields. In the JSONL they are the object's keys; in the
Markdown they are the headings under each story.

```text
id                            US-0001 .. US-1000, stable, never reused
title                         short, unique across the corpus
persona                       one of the fourteen personas below
workflow_ref                  a WF-<DIMENSION>-<NNN> id from the workflow catalogue
dimension                     derived from the referenced workflow, never restated by hand
complexity_tier               1..5, see the tier definitions below
jurisdiction                  the jurisdiction whose rules the story turns on
preconditions[]               concrete: tenant, roles, existing records, dates, amounts
story                         "As a <persona>, I want <goal>, so that <outcome>"
scenario                      the walk-through: exact decisions, approvals, refusals, evidence
acceptance_criteria[]         Given/When/Then triples, at least three, more for higher tiers
data_read{domain: what}       what the story reads, keyed by owning domain
data_written{domain: what}    what it writes, keyed by owning domain; empty for a pure read
authorization_and_disclosure[] who may see what; what is WITHHELD or DENIED
failure_modes[]               {condition, expected_refusal} with a typed refusal, not "an error"
evidence_expected[]           the evidence or ledger facts the system must leave behind
tags[]                        free vocabulary for slicing the corpus; every
                              tier-5 story and no other carries `adversarial`,
                              so a benchmark can select the adversarial set
                              from either the tier or the tag
```

### Personas

```text
employee                      the subject of most HR facts, and a first-class actor
manager                       the subject's direct manager, an initiator and an approver
HR business partner           the HR generalist who owns the process
payroll administrator         owns pay computation, settlement and its deadlines
benefits administrator        owns plans, elections, carriers and continuation
recruiter                     owns requisitions, candidates and offers
hiring manager                the manager for a role being filled, distinct from manager
compliance officer            owns legal, privacy and investigative decisions
IT or access administrator    owns identity, entitlement and connector operations
finance partner               owns budget authority, cost and commercial decisions
external partner or carrier   a counterparty: carrier, provider, supplier, agency, customer
integration system            a non-human principal acting through a connector
AI agent                      an assistive agent that proposes and never decides
auditor                       reads evidence and tests controls; changes nothing
```

`manager` and `hiring manager` are separate because their authority differs: a
manager has authority over an existing report's terms, a hiring manager has
authority over a vacancy. Several stories turn on exactly that distinction.

### The data domains

`data_read` and `data_written` are keyed by owning domain. Forty-nine domain
keys are used across the corpus and they are normalised at generation time, so a
slice by domain does not split on a synonym:

```text
agent          analytics    asset        benefits     billing      budget
case           compensation compliance   configuration contingent  dataops
documents      events       experience   finance      identity     incident
integration    knowledge    labor        learning     leave        ledger
mobility       onboarding   operations   organization payroll      people
planning       population   position     privacy      program      provenance
provisioning   recruiting   regulatory   safety       sandbox      search
security       settlement   talent       tax          tenant       time
workflow
```

These are data domains rather than the forty-nine HR dimensions the catalogue
uses, and the two lists are not the same thing: a story in the `PAY` dimension
routinely reads `people`, `time` and `regulatory`. That is the point of keeping
them separate.

### How `workflow_ref` is chosen

A story that writes something references the workflow whose trigger matches what
the story does. A story that only reads references the workflow that owns the
artifact being read, because the catalogue has no separate entry for reading a
published artifact and inventing one would double the catalogue without adding
anything. So a manager reading a published pay band references the workflow that
maintains the band catalogue, and a hiring manager reading a job profile
references the workflow that publishes profiles. Where a read-only story's
reference is of this second kind, the story's `data_written` is empty, which is
how the two cases are told apart.

### Complexity tiers

```text
1  single actor, single domain, happy path. No approval, no external effect.
2  approvals, effective dating, one refusal path. Still one primary domain.
3  multi-domain, multi-approver, a correction or a repair.
4  cross-border, concurrent changes, retroactive corrections, integration
   failures, separation-of-duties conflicts.
5  adversarial: privilege escalation attempts, cross-tenant probes, replay,
   forged evidence, data-subject rights during an active case, break-glass,
   migration mid-flight.
```

Target distribution: 20 percent tier 1, 35 percent tier 2, 25 percent tier 3,
15 percent tier 4, 5 percent tier 5. The achieved distribution is reported in
[`quality-report.md`](quality-report.md).

Tier is about the shape of the interaction, not about how hard the code is. A
tier 1 story can be expensive to implement and a tier 5 story can be a single
refusal. What tier 5 means is that the story is written from the position of
someone trying to make the system do something it must not do.

## Vocabulary the stories use

The stories use the repository's own words, not a parallel set:

```text
intent                  a typed, governed request; it has an id and a definition ref
proposal                the immutable thing an approval binds to
proposal digest         the canonical digest that makes the binding exact
approval / rejection    a decision bound to a digest and an authority snapshot
obligation              something that must still happen; it has a responsible party
ledger fact             an append-only record of what became true
evidence ref            a pointer to an artifact, held by digest rather than by value
effective-at            when a fact is true in the business world
known-at                when the system learned it
revision                an immutable version of a fact; corrections append, never overwrite
WITHHELD                the caller may not learn anything, including existence
DENIED                  the field was requested and refused, and is named in the answer
FULL / PARTIAL          whole-subject disclosure states
zero-effect receipt     proof that a read or a simulation wrote nothing
```

Lifecycle language is the kernel's five dimensions and never a single flat
status:

```text
RequestState  ExecutionState  BusinessState  ConsistencyState  ObligationState
```

A story that says an intent "completed" without saying which dimension closed is
underspecified. Two hundred and two of the thousand stories name at least one of
the five dimensions in their scenario or their acceptance criteria, spread
across all forty-nine HR dimensions, all five complexity tiers, every
hundred-story block and all five lifecycle dimensions, each of which is named
between twenty-three and sixty-one times. Those two hundred and two are the
corpus's test for the five-dimension model: an implementation that reports a
single flat status field fails them and passes most of the rest, so a benchmark
that reports per-tier and per-dimension results will see the failure rather than
averaging it away. The remaining seven hundred and ninety-eight stories assert
domain outcomes and are silent about which dimension carried them. That is the
corpus's largest known weakness and it is recorded as such in
[`quality-report.md`](quality-report.md); it is a gap rather than a licence.

## How to use a story

For alignment work, read the failure modes first. The happy path in a story is
usually the least interesting part; the refusals are where the contracts are
tested. If the system cannot produce the named typed refusal, either the
refusal taxonomy is incomplete or the story is wrong, and both are worth
knowing.

For benchmark work, the acceptance criteria are the oracles. They are written to
assert exact visible state, the enabled action set, the semantic result, the
persisted effects and the prohibited disclosure. "Returns 200" and "does not
crash" are never sufficient, which is a rule inherited from
[the user-flow program](../user-flows/README.md).

For catalogue work, `workflow_ref` gives the reverse index: every workflow in
the catalogue should be exercised by several stories at different tiers, and a
workflow with no story is either uninteresting or under-specified.

## What this corpus is not

It is not a test suite. It is not a requirements document. It does not create
scope, and a story marked against a `NEW` workflow is exercising a design
candidate, not a commitment. It also does not encode legal advice: the
jurisdictional detail is there so that a story is concrete enough to test, and a
rule enters the product only through a reviewed rule pack, as
[the state-law research README](../research/state-employment-law/README.md)
already says.
