# State Employment Law Research (side task, started 2026-09-03)

Research notes on the employment laws and regulations of each US state that can
affect how HCM Next operates: what it must record, notify, retain, disclose,
time, or refuse when it governs a workforce change. One file per state, written
by a research agent from primary and official sources. These are research
inputs for the Legal plane's rule packs and for connector/partner scoping. They
are not legal advice and are not normative contracts; a rule enters the product
only through a reviewed rule pack.

## Queue

| State          | File                | Status       | Researched | Reviewed   |
| -------------- | ------------------- | ------------ | ---------- | ---------- |
| Alabama        | `alabama.md`        | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Alaska         | `alaska.md`         | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Arizona        | `arizona.md`        | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Arkansas       | `arkansas.md`       | REVIEWED     | 2026-09-03 | 2026-09-03 |
| California     | `california.md`     | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Colorado       | `colorado.md`       | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Connecticut    | `connecticut.md`    | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Delaware       | `delaware.md`       | REVIEWED     | 2026-09-03 | 2026-09-03 |
| Florida        | `florida.md`        | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Georgia        | `georgia.md`        | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Hawaii         | `hawaii.md`         | REVIEWED     | 2026-09-03 | 2026-09-03 |
| Idaho          | `idaho.md`          | REVIEWED     | 2026-09-03 | 2026-09-03 |
| Illinois       | `illinois.md`       | REVIEWED     | 2026-09-03 | 2026-09-03 |
| Indiana        | `indiana.md`        | REVIEWED     | 2026-09-03 | 2026-09-03 |
| Iowa           | `iowa.md`           | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Kansas         | `kansas.md`         | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Kentucky       | `kentucky.md`       | DRAFTED      | 2026-09-03 |            |
| Louisiana      | `louisiana.md`      | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Maine          | `maine.md`          | NEEDS_REWORK | 2026-09-03 | 2026-09-03 |
| Maryland       | `maryland.md`       | DRAFTED      | 2026-09-03 |            |
| Massachusetts  | `massachusetts.md`  | DRAFTED      | 2026-09-03 |            |
| Michigan       | `michigan.md`       | DRAFTED      | 2026-09-03 |            |
| Minnesota      | `minnesota.md`      | DRAFTED      | 2026-09-03 |            |
| Mississippi    | `mississippi.md`    | DRAFTED      | 2026-09-03 |            |
| Missouri       | `missouri.md`       | DRAFTED      | 2026-09-03 |            |
| Montana        | `montana.md`        | DRAFTED      | 2026-09-03 |            |
| Nebraska       | `nebraska.md`       | DRAFTED      | 2026-09-03 |            |
| Nevada         | `nevada.md`         | DRAFTED      | 2026-09-03 |            |
| New Hampshire  | `new-hampshire.md`  | DRAFTED      | 2026-09-03 |            |
| New Jersey     | `new-jersey.md`     | DRAFTED      | 2026-09-03 |            |
| New Mexico     | `new-mexico.md`     | DRAFTED      | 2026-09-03 |            |
| New York       | `new-york.md`       | DRAFTED      | 2026-09-03 |            |
| North Carolina | `north-carolina.md` | DRAFTED      | 2026-09-03 |            |
| North Dakota   | `north-dakota.md`   | DRAFTED      | 2026-09-03 |            |
| Ohio           | `ohio.md`           | DRAFTED      | 2026-09-03 |            |
| Oklahoma       | `oklahoma.md`       | DRAFTED      | 2026-09-03 |            |
| Oregon         | `oregon.md`         | DRAFTED      | 2026-09-03 |            |
| Pennsylvania   | `pennsylvania.md`   | DRAFTED      | 2026-09-03 |            |
| Rhode Island   | `rhode-island.md`   | DRAFTED      | 2026-09-03 |            |
| South Carolina | `south-carolina.md` | DRAFTED      | 2026-09-03 |            |
| South Dakota   | `south-dakota.md`   | DRAFTED      | 2026-09-03 |            |
| Tennessee      | `tennessee.md`      | DRAFTED      | 2026-09-03 |            |
| Texas          | `texas.md`          | DRAFTED      | 2026-09-03 |            |
| Utah           | `utah.md`           | IN_PROGRESS  |            |            |
| Vermont        | `vermont.md`        | IN_PROGRESS  |            |            |
| Virginia       | `virginia.md`       | IN_PROGRESS  |            |            |
| Washington     | `washington.md`     | IN_PROGRESS  |            |            |
| West Virginia  | `west-virginia.md`  | QUEUED       |            |            |
| Wisconsin      | `wisconsin.md`      | QUEUED       |            |            |
| Wyoming        | `wyoming.md`        | QUEUED       |            |            |

Status values: `QUEUED`, `IN_PROGRESS`, `DRAFTED` (agent finished, unreviewed),
`REVIEWED` (orchestrator read it), `NEEDS_REWORK`.

## File template

Every state file uses these sections, in this order, with a **Sources** list
of URLs and the retrieval date. Where a state has no rule on a topic, say so
explicitly ("no state rule; federal FLSA applies") rather than omitting the
section.

1. Summary for HCM Next (5–10 bullets: what this state changes about a
   promotion, base-pay change, manager change, or termination processed
   through the platform)
2. Employment relationship: at-will status and exceptions; required
   written notices at hire and on change of pay or role (wage theft
   prevention notices, pay rate change notice timing)
3. Wages: minimum wage and overtime rules that differ from federal; pay
   frequency; final pay timing on termination and resignation; permitted
   deductions; pay statement content requirements
4. Pay transparency and equity: salary range disclosure in postings or on
   request; salary history bans; pay data reporting; equal pay statutes and
   protected classes
5. Leave and time: paid sick leave, paid family and medical leave, other
   mandated leave, and how they interact with a pay or role change
6. Records and access: payroll and personnel record retention periods,
   employee access to personnel files, record format rules
7. Privacy and data: state privacy law coverage of employee data, biometric
   and monitoring rules, breach notification obligations, data residency
   or processing constraints
8. Hiring and background: ban-the-box, background check limits, drug
   testing, e-verify mandates
9. Separation: mini-WARN acts, severance and notice requirements, non-compete
   and non-solicit enforceability
10. Classification and multi-state: contractor tests, remote-worker rules,
    reciprocity and which state's law applies
11. Implications for P1A/P1B: which of the above must be modeled as a typed
    obligation, notice, timing constraint, or field restriction when HCM Next
    simulates or executes a promotion and base-pay change for a worker in this
    state; open questions
12. Sources: numbered list of URLs (official state agency, statute text,
    or reputable secondary source), each with the retrieval date

Keep each file between 250 and 600 lines. Cite the statute or regulation
section number whenever the source gives one. Mark anything uncertain as
"verify".

## Review log

### 2026-09-03 — batch 1 + batch 2 (18 files)

Reviewer verdict: 6 REVIEWED (Arkansas, Delaware, Hawaii, Idaho, Illinois,
Indiana), 12 NEEDS_REWORK. Rework items, per state:

- Alabama: under 250 lines; workers'-comp retaliation cite conflicts
  (§ 25-5-11 vs § 25-5-11.01, verify § 25-5-11.1); remove the unexplained
  "$10.88/hour" overtime figure.
- Alaska: under 250 lines only; content sound.
- Arizona: under 250 lines; the statewide private-sector ban-the-box claim
  ("A.R.S. § 23-211, effective 2025") is not real, remove; the PUMP Act
  citation in §4 is unrelated to pay transparency, remove.
- California: under 250 lines; minimum-wage cite is Labor Code § 1182.12,
  not § 526; deepen Cal-WARN and privacy (CCPA/CPRA employee data).
- Colorado: under 250 lines; §3 omits the 40-hour weekly overtime trigger
  (COMPS Order: greater of 40/week, 12/day, 12 consecutive).
- Connecticut: state the minimum-wage figure; add CT Paid Leave (PFMLA
  wage replacement, employee payroll contribution); replace the single
  chapter-overview URL with section-specific sources.
- Florida: under 250 lines only; content sound.
- Georgia: shortest file; O.C.G.A. § 50-18-70 (Open Records) does not give
  private employees personnel-file access, remove; §11 is generic.
- Iowa: under 250 lines; Iowa Code ch. 553 is the antitrust chapter, not a
  non-compete statute (common law only); SF 418 (2025) REMOVED gender
  identity from ch. 216, the file says the opposite.
- Kansas: very short; K.S.A. 50-163 non-compete claim is unverified and
  likely wrong (common law); §§5, 7, 11 are stubs.
- Louisiana: under 250 lines; replace the nolasf.org New Orleans citation
  with the Municipal Code; §§6-7 thin.
- Maine: state the actual minimum wage, tip credit, PFMLA replacement rate,
  and the non-compete salary threshold instead of "(verify)" everywhere.

Also flagged by the orchestrator: hawaii.md does not mention the HRS
§ 378-2.4 salary-history ban (2019); massachusetts.md refers to "CTDPA"
(Connecticut's act) in the privacy section; nebraska.md cites § 87-404
(franchise law) for non-competes.

Cross-file pattern to watch: when a state has no covenant statute, drafts
tend to invent a chapter number for non-competes. Every
"non-compete enforceable under [chapter]" claim gets spot-checked.

Normalisation for the rewrite pass: bare template headings, one metadata
line under the H1 (`**State:** X | **Researched:** date | **Status:** ...`),
Sources as numbered markdown links each ending `— retrieved 2026-09-03`.
