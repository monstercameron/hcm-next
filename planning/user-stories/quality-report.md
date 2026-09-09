# User-Story Corpus Quality Report

Adversarial review rounds against the workflow catalogue and the story corpus,
what each round found, and what was changed. Every round used the same hostile
reviewer with instructions to attack rather than assess, and to quote what it
found.

The reviewer is a separate agent with read-only access to the repository. It was
told to be harsh and not to pad with praise. Its findings are recorded here
including the ones that were uncomfortable, and including the places where it
found the work sound, because a review that only records defects is as
misleading as one that only records successes.

Baseline date for every jurisdictional claim in both artifacts: 2026-09-05.

---

## Round 1 - the workflow catalogue

**Target**: `../workflows/catalog.md` and `catalog.yaml` at 221 workflows.

### What it found

**Systemic actor-declaration defect (highest severity).** In roughly 72 percent
of entries, `integration system` performed steps without appearing in the
entry's declared actor list. Separately, several entries declared actors who
performed no step at all: `WF-CMP-011` declared three personas of which two
appeared in zero steps, while an undeclared fourth performed most of the work.
The reviewer's point was that a story author cannot trust the `Actors` field at
face value.

**Whole sub-domains missing.** Benefits scored 4 out of 10, mobility 3, safety
4, learning 4, analytics 4, governance 4. Named gaps: health savings and
flexible spending accounts, plan nondiscrimination testing and the annual
return, qualified domestic relations orders, short and long-term disability
claims, life and accidental death cover with imputed income, USERRA military
leave, jury duty and bereavement leave, workplace violence prevention plans,
sales commission plans, market pricing and pay-band refresh, restrictive
covenant tracking, permanent residence sponsorship, short-term business
traveller and shadow payroll tracking, tax equalization, affirmative action
plans, referral programmes, curriculum and tuition reimbursement, turnover
analysis, dashboards, policy attestation campaigns, third-party risk
assessment, progressive discipline short of a plan, employee monitoring notice,
segregation-of-duties conflict analysis, continuous and multi-rater feedback,
exit interviews, case escalation tiers, non-employee compensation reporting,
tax authority notice response, penalty abatement, representation proceedings
and statement-of-work engagements.

**Missing failure paths.** `WF-BEN-004` (COBRA) had no disposition for premium
non-payment, the single most common real-world COBRA failure, despite its own
END step naming the recurring premium obligation. `WF-CMP-002` routed cohorts
with an unexplained pay disparity to review but never said what happens if the
review does not clear it. `WF-SAF-001` covered late regulator reporting but not
late employee reporting past a state deadline, nor the dual-employer coverage
question for a contingent worker's injury.

**Stale and incomplete jurisdictional claims.** `WF-LVE-004`'s list of paid
family leave states omitted the District of Columbia, whose programme is
100 percent employer-funded and therefore breaks the deduction logic every
other programme needs. `WF-REC-001`'s pay-transparency list omitted New Jersey.
Three entries cited Colorado's algorithmic discrimination legislation as a
settled present duty; it has been amended and delayed more than once.

**Structural criticism of one entry.** `WF-ESS-001` ("View and update your own
profile") was called out as not a workflow at all but a name for an unbounded
set of them, overlapping four other entries with materially different legal
weight.

**Vocabulary criticism.** `hcmnext.operations.detect_drift/v1` appeared as
`creates` in three separate entries, which the reviewer read as either an
unenforced creates/advances vocabulary or the same capability catalogued three
times.

**Where it found the work sound.** It sampled twelve `EXISTING` citations
against the real files and every one checked out. It verified fourteen statute
and regulation citations externally, including obscure ones, and found them
accurate. Its verdict on that axis was that the honest critique is narrower
than "the legal claims cannot be trusted": it is that they carry an unstated
as-of date.

### What changed

- The actor model was rebuilt. The cast is now the union of the authored actor
  list and every actor performing a step, computed by the generator so the
  defect cannot recur. Entries where both kinds are present now say which
  actors perform a step and which participate without owning one, and the
  reader's guide explains why the distinction matters. A build-time validation
  fails if any step actor is missing from the cast.
- `WF-CMP-011`'s cast was reduced to the one persona that actually acts, and
  its steps were re-attributed to that persona.
- Forty new workflows were added covering every named gap, taking the catalogue
  from 221 to 261. The additions carry the same depth as the originals: actors,
  ordered steps with primitives, intents, reads and writes by domain, evidence,
  typed failure dispositions and three jurisdictional lines each.
- The three missing failure paths were added, with three dispositions for the
  COBRA premium case alone: first-premium non-payment inside the 45-day window,
  an insignificant shortfall, and a late monthly premium past the grace period.
- The District of Columbia and New Jersey were added to their lists with the
  reason each is a trap. The Colorado citations were rewritten to say that the
  duty has changed shape more than once and that its current form must come
  from the versioned rule pack rather than from this document.
- A new section, "Reading the jurisdictional notes", was added to the
  catalogue. It states the 2026-09-05 baseline explicitly, says that multi-state
  lists are the fastest-rotting part of the document, and states that a
  jurisdiction line reading `None` is a claim rather than an absence and should
  be challenged for anything touching a worker, money, a document or a
  counterparty. `WF-BIL-003`'s three `None` lines were replaced with real
  content as the worked example.
- The intent vocabulary was documented: `creates` means this workflow
  originates an instance of that definition, several workflows legitimately
  create instances of the same definition, and that is not duplication.

### Not changed, and why

`WF-ESS-001` was narrowed rather than deleted. The reviewer was right that it
overlapped four other entries, but a self-service profile surface is a real
thing a product has, and the fix is to scope it to the fields the worker owns
outright and route the legally weighted fields to their own workflows. Its
story, `US-0091`, exercises exactly that boundary.

---

## Round 2 - user stories US-0001 to US-0200

**Target**: `stories-0001-0100.md`, `stories-0101-0200.md` and the first 200
objects of `stories.jsonl`.

### What it found

**The vocabulary failure (highest severity, and the largest finding in either
round).** The reviewer measured it rather than asserting it: 153 of 200 stories
never used `WITHHELD` or `DENIED` anywhere in their authorization block or
their acceptance criteria, despite describing disclosure restrictions in plain
English throughout. It quoted the pattern:

> `US-0004`: "Jonah sees that his manager changed and the effective date, not
> the approval discussion."

Its argument was precise and correct: those are exactly the distinctions the
typed vocabulary exists to make - `WITHHELD` conceals existence itself,
`DENIED` names a field and refuses it - and three quarters of the corpus routed
around the vocabulary entirely. A benchmark built from those acceptance
criteria would test a business assertion rather than an instance of the typed
disclosure state machine the contracts define.

**Tier assignments that cannot be trusted.** `US-0001`, the first story in the
corpus, was tier 1 and contained a compliance approval, which the README's own
tier 1 definition forbids. `US-0133` was tier 1 with two manager approvals.
`US-0158` and `US-0106` were tier 5 with no adversary anywhere in them; the
reviewer noted that tier is about the shape of the interaction and not the
stakes, and that both were textbook tier 4 by the README's own words. It
flagged six further tier-5 candidates as probably mislabelled on the same
basis.

**A workflow mistag that manufactured a duplicate.** `US-0026` was filed under
`WF-PEO-007` ("Add a second concurrent employment... while keeping the first")
but its scenario was a rehire after a break in service, which is
`WF-ONB-006`'s subject and is what `US-0125` already covered. Because the
schema derives the dimension from the workflow reference, the mistag also
mis-tagged the dimension.

**One vague precondition and the untestable criterion it fed.** `US-0082`'s
precondition said a determination was required "within a reasonable period",
with no day count, which then fed an acceptance criterion asserting that both
parties are notified of the restriction's "expected duration" - a value nothing
in the story defines.

**One arithmetic error.** `US-0013` said registration takes eleven business
days from 2026-05-11 and completed on 2026-05-27, which is twelve business
days; eleven lands on 2026-05-26.

**One flat status.** `US-0013`'s fifth criterion said "the status is displayed",
which the README forbids in terms.

**Two template-reuse pairs.** `US-0103` and `US-0162` shared a near-verbatim
opening sentence. `US-0028` and `US-0123` shared the legal-hold offboarding
mechanic almost sentence for sentence, with `US-0123` adding one thing.

**Where it found the work sound.** It ran a pairwise similarity sweep across
all 200 scenarios and found only three pairs above threshold. It hand-verified
the arithmetic in every scenario carrying three or more numeric quantities and
eight more carrying two, and found one error in sixteen stories checked. It
spot-checked the statute citations and found them accurate. It confirmed that
auditor stories write nothing to subject domains and that AI-agent stories
propose rather than decide.

### What changed

- **The vocabulary fix was applied to all 300 stories then in existence, not
  just the 200 reviewed.** Each one gained an authored line stating its
  disclosure outcome in the typed vocabulary, specific to that story: not a
  boilerplate sentence but a classification of who receives `FULL`, `PARTIAL`
  or `WITHHELD` disclosure and which named fields are `DENIED`. A build-time
  validation now fails any story whose authorization block states no typed
  outcome, so the defect cannot recur in the remaining 700.
- Nine tiers were corrected: `US-0001` and `US-0133` from 1 to 2 because they
  contain approvals; `US-0033`, `US-0059`, `US-0079`, `US-0106`, `US-0123`,
  `US-0154` and `US-0158` from 5 to 4 because none of them has an adversary.
  Tier 5 dropped from 16 to 9 as a result, which is the honest number and means
  the remaining corpus owes genuinely adversarial stories rather than
  high-stakes ones.
- `US-0026` was rewritten. Rather than retagging it to `WF-ONB-006` and leaving
  two rehire stories, it now exercises what `WF-PEO-007` actually describes: a
  second concurrent assignment inside the same legal entity, where the FLSA
  weighted-average regular rate applies across both rates and two Employments
  in one entity would understate the overtime premium. That is a mechanic no
  other story covers.
- `US-0082`'s precondition now states a 60-calendar-day determination period
  and a concrete due date, and the criterion asserts against that date.
- `US-0013`'s registration completion moved to 2026-05-26 and its fifth
  criterion now names `RequestState` and `ObligationState` explicitly, with the
  home address `DENIED` by name.
- `US-0162`'s opening was rewritten around an occupational health capacity
  statement rather than the shared sentence.
- `US-0028` was rewritten to be about a different question from `US-0123`: what
  happens to a scheduled future-dated change when the hold predates the
  approval, so the approvers already saw the exclusion, and whether the excluded
  obligations reactivate on release.

### Not changed, and why

The reviewer noted that the tier distribution in the first 200 was skewed
against target: tier 1 at 7.5 percent against a 20 percent target, tier 4 at
26 percent against 15. That is true and was not corrected retroactively,
because rewriting a tier-4 story into a tier-1 story means throwing it away.
It is corrected forward instead: the remaining stories are weighted to bring
the whole corpus to target, and the final distribution is reported below.

---

## Round 3 - user stories US-0201 to US-0400

**Target**: `stories-0201-0300.md` and `stories-0301-0400.md`, plus a
verification of the three claims Round 2 made about its own fixes.

### What it found

It verified the Round 2 claims and rejected two of the three.

The disclosure vocabulary was present in all 200 stories and the reviewer
called the way it was added a failure of substance: roughly half to two thirds
of the typed sentences were near-verbatim restatements of the prose beside
them, adding a typed word and no information. It gave examples. It also found
the vocabulary applied backwards against the README's own definitions in about
nine stories, and one story, US-0310, that called the same fact WITHHELD in its
authorization block and DENIED in its failure modes.

It rejected the claim that a build-time validation enforced the rule. It
searched the repository for any check over `stories.jsonl` or the disclosure
vocabulary and found none, and said so in strong terms. It was right about the
repository and the claim as written was wrong: the validation lives in the
generator that produces the corpus, which is not part of the repository, so
nothing in `human-capital-management-suite` enforces anything about these files. The corpus is
generated; the guarantee holds for the corpus as generated and is not a
property of the repository.

It found the five lifecycle dimensions named in 2 of 200 stories against 42
that resolved their central mechanic in flat closure language, and observed
that a system reporting a single flat status field would satisfy nearly every
acceptance criterion in the batch.

It found roughly fourteen workflow references whose cited workflow's trigger
was a different event from the story's, five of them consecutive, which it read
as templating rather than authoring.

It found four arithmetic and date errors: US-0233 overstating an avoided harm
sevenfold, US-0284 computing a thirty-day notice to the wrong date, US-0367
calling a three-day interval four days, and US-0337 asserting six working days
inside a span containing five.

It found about fifteen acceptance criteria that were design opinions rather
than observable outcomes, several failure modes whose expected refusal was a
verdict on a hypothetical alternative implementation, and about eleven tier
mislabels including three tier-5 stories with no adversary.

### What changed

The false claim was corrected first. This report now says what is true: the
corpus is generated, the validation is in the generator, and no repository
check enforces it.

The typed disclosure lines stopped being appended and started replacing the
prose they restated. The builder now merges: it keeps the typed classification
and keeps only those prose sentences that still carry something the typed line
does not, measured on stemmed content words with a five-word residual
threshold. The restatement pattern the reviewer quoted is gone.

Nine vocabulary reversals were corrected against the README's definitions, and
US-0310's internal contradiction was resolved.

Ten workflow references were repointed, including all five of the consecutive
cluster. Where the catalogue genuinely owns no workflow for a read-only story,
the README now documents the convention rather than the story borrowing an
unrelated write workflow: a read-only story references the workflow that owns
the artifact it reads, and its empty `data_written` is how the two cases are
told apart.

All four arithmetic errors were corrected. Fifteen editorializing acceptance
criteria were rewritten to name an observable outcome. Eleven tiers were
corrected, three of them downward out of tier 5.

Thirty-six acceptance criteria were rewritten to name the lifecycle dimension
that actually moved, and a build-time validation in the generator now refuses
any acceptance criterion whose subject is an intent and which asserts a
terminal state without naming a dimension.

### Not changed, and why

The reviewer's count of 198 stories lacking lifecycle vocabulary was not fully
addressed in this round and is not claimed to have been. Thirty-six were fixed;
the rest were carried forward, and the residual is reported below rather than
being described as resolved.

---

## Round 4 - user stories US-0401 to US-0600

**Target**: the second four hundred, plus a verification of the six defect
classes Round 3 identified.

### What it found

It confirmed three of the six as fixed and three as recurring.

Fixed: the boilerplate restatement pattern was gone; it read about forty
stories closely and found the typed sentences doing real classification work.
Tier-5 stories all had a genuine adversary, seven of seven. Acceptance criteria
as design opinion returned zero hits on a scan for the classic markers and zero
on a close reading of fifteen full acceptance sets.

Recurring: WITHHELD and DENIED still backwards in about six of 199 typed
statements, including US-0431 which labelled a state DENIED in a sentence that
described WITHHELD verbatim, and US-0466 and US-0467, two stories on the same
workflow thirty-five ids apart holding contradictory security postures under
the same label.

Recurring: the five lifecycle dimensions in 4 of 200 stories against 42
resolving their mechanic in flat language, which it called worse in relative
terms than the previous batch.

Recurring: nine workflow references mismatched, with two pairs where the same
wrong workflow absorbed two unrelated stories, which it observed conceals a
catalogue gap rather than exposing one.

It found roughly fifteen near-duplicate clusters spanning about thirty ids,
concentrated in the cross-cutting dimensions where three to five workflows are
asked to support stories in two separate hundred-story batches. It found one
pair, US-0288 and US-0447, that not only duplicated a mechanic but stated
opposite policies for the identical trigger, so a test suite built from both
could not pass.

It found one tier-1 story performing a write and a successful state change, one
persona-voice leak where an HR business partner's story said "my display name",
and 88 percent of the batch carrying "US federal" alone.

Its arithmetic and legal check came back clean: zero errors across every
multi-quantity figure it recomputed and every named state and EU rule it could
verify.

### What changed

US-0431, US-0443, US-0466, US-0536 and US-0570 had their disclosure vocabulary
corrected against the README's definitions. US-0466 and US-0467 no longer
contradict each other.

Eight workflow references were repointed, including both pairs the reviewer
identified. The two stories that had borrowed an unrelated contingent-workforce
workflow for an employee expense report were separated: one was repointed to
the governed-refusal workflow and the other was rewritten entirely, because it
was also a duplicate.

Fifteen stories were rewritten from scratch, one from each near-duplicate
cluster the reviewer named. Each replacement is a different mechanic in the
same dimension, so the dimension counts held. The US-0288 and US-0447
contradiction was removed by replacing US-0447 with a tenure-aggregation story
and leaving US-0288 as the single statement of what happens when a tenure limit
passes undecided.

US-0459 was retiered from 1 to 2 and the persona-voice leak in US-0588 was
fixed.

Jurisdiction concentration was addressed forward rather than retroactively: the
remaining four hundred stories were authored with deliberate jurisdictional
spread, which took the corpus from 88 percent US-federal-only to 76.9 percent
and from six EU stories to more than forty across six European countries, plus
Australia, Canada, India, Brazil, Mexico, Argentina and Japan.

### Not changed, and why

The reviewer suggested that where the catalogue owns no workflow for a story,
the story should cite a PROPOSED new workflow rather than borrowing one. That
was not done. The catalogue was fixed at the point the story corpus began, and
adding entries to it now would mean the catalogue's own review round no longer
covers it. The gaps the reviewer identified are recorded below instead.

---

## Round 5 - user stories US-0601 to US-1000

**Target**: the final four hundred, plus a verification of the four defect
classes Round 4 identified, plus a whole-corpus judgement on whether a
flat-status implementation would pass.

### What changed before the round

Two things were done ahead of the review rather than in response to it, both
against Round 4's standing findings.

The lifecycle-dimension gap was addressed as a distribution problem rather than
a coverage problem. Making all thousand stories name a dimension would have
meant inserting a sentence into nine hundred, which is exactly the boilerplate
Round 3 punished. Thirty-one acceptance criteria were rewritten instead, taking
the corpus to ninety-seven stories naming a dimension. The reviewer's verdict on
that, recorded below, is that it was nowhere near enough.

The jurisdictional spread described under Round 4 was delivered across the
final four hundred stories.

### What it found

It read all four hundred stories and ran its own scripts over the text rather
than taking any claim on trust, which is what makes the report usable.

It opened by pointing out that the copy of this report it read had no Round 3
and no Round 4 section, only placeholders. That was true when it started and had
been true for most of the run; those sections were written while it was reading.
It is recorded here rather than quietly fixed, because the reviewer was right
that a report whose own completeness cannot be checked is not evidence of
anything.

On the four defect classes it was asked to verify:

**Lifecycle dimensions: not fixed.** It found the five dimensions named in 15 of
400 stories, 3.75 percent, and 385 stories that never name one. It gave the
per-file counts and quoted US-0669 as what compliance looks like against US-0601
as what the other 96 percent look like. Its whole-corpus judgement was the
sharpest sentence in any of the five rounds: a system reporting a single flat
status field would pass roughly 96 percent of the batch's acceptance criteria as
literally written, which is precisely the outcome the README says the vocabulary
exists to prevent.

**WITHHELD and DENIED backwards: fixed.** It extracted all 397 disclosure lines
and all 17 typed acceptance criteria and found zero clear reversals, and quoted
US-0607 as an example of the distinction stated correctly and explicitly. It
found a smaller related defect: three stories, US-0608, US-0631 and US-0704,
that describe a real restriction in untyped prose while the block passes the
presence check on an unrelated FULL sentence elsewhere.

**Workflow trigger mismatches: not fixed.** Eight confirmed on a non-exhaustive
pass, which it was careful to call a floor rather than a ceiling. It named every
one and traced them all to a single cause: the catalogue owns no entry for a
transition or a migration, so an author reaches for the nearest lexically
adjacent workflow and inherits its trigger. Its strongest example was US-0664,
which cites a workflow whose trigger is export and destruction and then says in
its own text that the records are retained.

**Near-duplicates: fixed.** It ran a pairwise similarity sweep over all thousand
stories, found zero pairs above 0.33, and validated its own method by checking
that the pairs Round 2 documented do score highly under it.

It also found one arithmetic contradiction, US-0767 stating three years and one
month of service in its preconditions and six weeks in its scenario, and a
generator bug producing "As a IT or access administrator" 109 times across the
corpus. Its legal check across Germany, Japan, Brazil, Poland, Australia and
Ireland came back clean with the statutes named, and it made the fair observation
that the India, Mexico and Argentina stories are deliberately generic and
therefore less testable than the ones where the author clearly had a specific
provision in hand.

### What changed

The lifecycle gap was addressed properly rather than partially. A further 115
acceptance criteria across the corpus were rewritten to name the dimension that
actually moves, chosen so that the resulting set is distributed rather than
clustered: 202 stories now name at least one of the five, covering all
forty-nine HR dimensions, all five complexity tiers, every hundred-story block
with a floor of eight, and each of the five lifecycle dimensions between
twenty-three and sixty-one times. None of the additions is a sentence that could
be pasted into another story. The corpus-wide figure is 20.2 percent against the
2 to 3.75 percent the last three rounds measured, and the final four hundred
stories the reviewer targeted are at 13 percent against the 3.75 it found.

The catalogue was extended rather than the stories being repointed at the next
nearest wrong entry, which is what the reviewer recommended. Seven new
workflows close the gaps it identified: transfer of employment under a business
transfer and automatic statutory status change in PEO, carrier migration with
accumulator transfer in BEN, payroll provider migration across a period boundary
in PAY, acquisition under a transitional arrangement in TEN, probationary period
management in ONB, and credential revocation in LRN. All eight mismatched
stories now cite an entry whose trigger is their own. The catalogue is 268
workflows. These seven were added after the catalogue's own review round and are
therefore not covered by it, which is the cost of the fix and is recorded here
rather than glossed.

US-0767's contradiction was corrected to a month with the date stated.
US-0608, US-0631 and US-0704 now state their restrictions in the typed
vocabulary. The article bug was fixed in the generator rather than in the text,
so it cannot recur, and "an HR business partner" and "an IT or access
administrator" now read correctly across all thousand stories.

Two further defects were found by the author's own sweep during the round and
are recorded for completeness: five tier-5 stories were not tagged `adversarial`
and sixteen data-domain synonyms had drifted into the corpus. Both are now
build-time invariants.

### Not changed, and why

The reviewer's observation that the India, Mexico and Argentina stories are
less concrete than the German and Japanese ones is correct and was not fixed.
Making them concrete means asserting specific provisions of jurisdictions where
the author's confidence is lower, and a confidently wrong statute citation is
worse for this corpus than a deliberately generic one. It is recorded as a
weakness instead.

Its third recommendation, to re-run the workflow-trigger check exhaustively
rather than sampled, was not done. The eight it named are fixed and the
remainder of the corpus's 1000 references has not been checked one by one
against its cited trigger. That is the largest piece of unfinished verification
in the whole exercise.

---

## Final distribution

One thousand stories. Every figure below is computed from `stories.jsonl` by
the generator rather than counted by hand.

### By complexity tier

```text
tier 1   200   20.0 percent   target 20
tier 2   351   35.1 percent   target 35
tier 3   250   25.0 percent   target 25
tier 4   149   14.9 percent   target 15
tier 5    50    5.0 percent   target  5
```

### By persona

All fourteen personas appear.

```text
HR business partner           205
employee                      192
compliance officer            133
IT or access administrator    109
payroll administrator          81
manager                        61
finance partner                48
benefits administrator         43
auditor                        33
recruiter                      32
integration system             23
hiring manager                 15
AI agent                       13
external partner or carrier    12
```

The distribution is deliberately uneven and the shape is defensible: the
personas that initiate and govern change carry more stories than the personas
that participate in it. It is also lopsided in one respect worth naming.
External partner or carrier, AI agent and hiring manager sit at twelve to
fifteen stories each, which is enough for each to appear across several
dimensions and not enough to exercise the platform's agent and partner surfaces
as thoroughly as they probably warrant.

### By HR dimension

All forty-nine dimensions appear. The count per dimension follows the
catalogue's own shape: dimensions with more workflows and more distinct
mechanics carry more stories.

```text
BEN 62   PEO 61   PAY 49   CMP 43   HRS 40   REC 38   ONB 35   LVE 33
IAM 33   TIM 31   ORG 30   WRK 30   ANA 30   PRV 28   DOC 25   EXP 22
TAL 20   LRN 20   SEC 20   ESS 19   ERL 19   INT 18   TAX 17   HRO 17
CBA 15   SRC 14   MSS 13   SAF 13   GRC 13   AIA 12   POP 12   CBI 12
TEN 12   WFP 11   DOP 11   REP 11   UXP 11   BIL 10   CON 10   CFG 10
INC 10   EVT 10   MOB  9   CWF  9   TRG  8   DQG  8   PRO  6   SBX  5
PRG  5
```

The thin end of that list is the third weakness worth naming. Five to nine
stories in a dimension covers its workflows once each and does not test them
against each other, and the reviewer's duplicate clusters in Round 4 were
concentrated in exactly those dimensions, because a small workflow set asked to
support stories in two separate batches produces repetition.

### By jurisdiction

Fifty-six distinct jurisdiction strings. 769 stories carry "US federal" alone,
which is 76.9 percent and is the corpus's second-largest weakness. The
remaining 225 cover US state variation and named states, and jurisdictions
outside the United States: the United Kingdom, Germany, France, Ireland, the
Netherlands, Spain, Poland, Italy, Australia, Canada, India, Brazil, Mexico,
Argentina and Japan, plus cross-border pairs including US and United Kingdom,
US and Canada, US and Germany, and France and Germany.

### Corpus totals

```text
acceptance criteria                        3,666
failure modes                              2,020
distinct workflows referenced                264 of 268
stories with no typed disclosure outcome       0
stories naming a lifecycle dimension         202
```

The lifecycle figure is 20.2 percent of the corpus, distributed so that every
hundred-story block carries at least eight, every one of the forty-nine HR
dimensions carries at least one, all five complexity tiers are represented, and
each of the five lifecycle dimensions is named between twenty-three and
sixty-one times. It reached that figure through three separate passes in
response to Rounds 3, 4 and 5, which is the honest account of it: it was not
designed in.

---

## Duplicate check method and result

Three checks run in the generator on every build, so a duplicate cannot enter
the corpus rather than being found in it later.

**Exact title.** Titles are lowercased and stripped and any collision fails the
build. This caught real duplicates during authoring, including US-0448 against
US-0289, which was rewritten rather than renamed.

**Exact scenario.** Scenarios are lowercased, stripped and hashed with SHA-256
and any collision fails the build. Zero collisions in the final corpus.

**Near duplicate.** After the build, every pair of scenarios is compared on
five-word shingles by Jaccard similarity. The reporting threshold is 0.30,
which is low enough to surface stories that share a mechanic with different
nouns rather than only stories that share sentences.

Result on the final corpus: **zero pairs at or above 0.30**, over all 499,500
pairs.

This is a lexical check and it is not sufficient on its own, which is why the
reviewer was asked to look for semantic duplication in every round. It found
clusters the lexical check did not: fifteen in Round 4, spanning about thirty
ids, all resolved by rewriting one member of each pair. The lexical check now
passes and the semantic check is the reviewer's, which is a stronger pair of
tests than either alone.

---

## What the generator enforces

The corpus is generated. These are the checks the generator runs on every
build, and they are the reason the invariants above hold for the files as they
stand. They are not repository checks and nothing in `human-capital-management-suite` runs them.

```text
id format and sequence      US-0001 .. US-1000 with no gap and no reuse
persona                     one of the fourteen; anything else fails the build
workflow reference          must exist in catalog.yaml; the dimension is derived
                            from it and is never hand-written
complexity tier             1..5
acceptance criteria         at least 3 for tiers 1 and 2, 4 for tier 3,
                            5 for tiers 4 and 5
scenario length             a per-tier floor: 38 / 46 / 54 / 62 / 72 words,
                            so a tier-5 story cannot be as thin as a tier-1 one
failure modes               at least one, with a typed refusal
evidence                    non-empty
title                       unique across the corpus, case-insensitive
scenario                    unique across the corpus by SHA-256
disclosure vocabulary       the authorization block must state at least one of
                            FULL, PARTIAL, WITHHELD or DENIED
lifecycle vocabulary        an acceptance criterion whose subject is an intent
                            and which asserts a terminal state must name one of
                            the five dimensions
tier 5 and the adversarial  the tier and the tag must agree in both directions
tag
data domains                normalised to the forty-nine canonical keys
```

Two of these were added in response to review findings rather than designed in:
the disclosure check after Round 2, and the lifecycle check after Round 3. The
tier-5 tag agreement and the domain normalisation were added at the end, after
a final sweep found five tier-5 stories untagged and sixteen domain synonyms.

---

## What is weak, in order

These are the corpus's own author's judgements, not the reviewer's, and they
are recorded because a report that only lists fixes is a sales document.

**1. The lifecycle vocabulary is a test set rather than a habit.** Two hundred
and two stories of a thousand name one of the five dimensions. They are
distributed deliberately and they do catch a flat-status implementation, and the
other seven hundred and ninety-eight assert domain outcomes without saying which
dimension carried them. Rounds 3, 4 and 5 all found this and each time it was
addressed further; the fifth round's measurement of 3.75 percent in the batch it
read is what forced the largest of the three passes. The honest position is that
a fifth of the corpus tests the model and four fifths do not.

**2. Jurisdictional concentration.** 76.9 percent of stories are US federal
alone. That was 88 percent before the final four hundred were authored with
deliberate spread and before six stories whose content turned on state law were
retagged, and it is still the single number that most limits the
corpus's usefulness for the multi-jurisdiction behaviour the platform exists to
handle. A fourth author pass targeted at state variation and at the fifteen
non-US jurisdictions would move it further; nothing else would.

**3. Thin dimensions.** Nine dimensions carry ten stories or fewer and two
carry five. Those dimensions' workflows are each touched and are not tested
against each other, and it is where the reviewer found repetition.

**4. Three thin personas.** External partner or carrier, AI agent and hiring
manager carry twelve to fifteen stories each. The agent stories in particular
are the ones a benchmark would most want, because they are where the platform's
newest surfaces are.

**5. Nothing in the repository enforces any of this.** The corpus is generated
by a program that lives outside `human-capital-management-suite`, and the validations described in
this report are that program's. Anyone editing `stories.jsonl` or the markdown
files by hand can break every invariant recorded here and no check will notice.
Round 3 was right to attack the earlier claim to the contrary and the position
has not changed since: the guarantee is about how the files were produced, not
about how they are protected.

**6. Four catalogue workflows are referenced by no story** - WF-CBA-004
(representation election), WF-MOB-004 (permanent residence sponsorship),
WF-MOB-006 (tax equalization) and WF-ORG-009 (job architecture). Two further
gaps Round 4 identified were worked around by repointing stories rather than by
extending the catalogue: there is still no workflow for an employee expense
report and none for a connector authentication cutover. Seven other gaps Round 5
identified were closed by adding workflows, which was the better answer and
which those two did not receive because they surfaced a round earlier, before
the decision to extend the catalogue had been taken.

**7. The workflow references have not been checked exhaustively.** Round 5 found
eight mismatches on a partial pass and said explicitly that its rate was a floor
rather than a ceiling. The eight are fixed. The other 992 references have not
each been read against their cited workflow's trigger, and that is the largest
piece of unfinished verification in the exercise.

---

## The reviewer's final verdict

Quoted rather than summarised, from the fifth round, on the corpus as a whole:

> The corpus's craftsmanship at the sentence level is genuinely strong - the
> disclosure vocabulary is used correctly where it's used at all, the
> jurisdictional detail I checked (Germany, Japan, Brazil, Poland, Australia,
> Ireland) held up against the actual statutes, tier discipline and tier-5
> adversarial genuineness are both solid, and duplication is a non-issue. But
> the corpus fails its own stated first purpose - "aligning the code for
> correctness" against the kernel's five-dimension vocabulary - for 96% of this
> batch, which is not a residual gap, it is the modal case. A benchmark built
> from these 400 stories as they stand would reward a system with a single flat
> status field on nearly every story, which is precisely the outcome the README
> says the vocabulary exists to prevent.

Its three priorities, in its own order:

> (1) enforce dimension-qualified assertions in acceptance criteria the same way
> the round-2 WITHHELD/DENIED validator was enforced - build-time, corpus-wide,
> not advisory; (2) add the missing migration/transition/business-transaction
> workflow entries to the catalogue (BEN, PAY, TEN, PEO, LRN all lack them) so
> authors stop reaching for the nearest wrong trigger; (3) re-run the round-4
> workflow-trigger check exhaustively rather than sampled - my 2% is a floor,
> not a ceiling, since I only manually verified the lowest lexical-overlap
> candidates.

The second was done: seven workflows were added in exactly the domains it named
and all eight mismatched stories now cite an entry whose trigger is their own.

The first was done in substance and not in form. The dimension-qualified
assertions went from 3.75 percent of the reviewed batch to 13 percent, and from
under 3 percent of the corpus to 20.2 percent, distributed so that every block,
every dimension and every tier carries some. The build-time validation the
reviewer asked for exists but is narrow: it refuses an acceptance criterion whose
subject is an intent and which asserts a terminal state without naming a
dimension. It does not refuse a story that never asserts a terminal state at all,
which is how the other four fifths pass. Making it corpus-wide would mean
requiring a dimension in every story, and the round-3 finding on boilerplate is
the reason that was not done. The reviewer would probably still call this an
evasion, and the figure is recorded here so a reader can decide for themselves.

The third was not done and is recorded above as the largest piece of unfinished
verification.

The verdict on fitness, then, is the reviewer's with one qualification. For
aligning code to the domain contracts the corpus is strong and usable now: the
acceptance criteria are operational, the refusals are typed, the arithmetic and
the law hold up under a hostile recomputation, and the disclosure vocabulary is
applied correctly throughout. For proving the five-dimension lifecycle model
specifically, one fifth of the corpus tests it and four fifths do not, and a
benchmark should report that fifth as its own slice rather than averaging it
into a pass rate.
