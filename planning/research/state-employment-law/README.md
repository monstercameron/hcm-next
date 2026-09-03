# State Employment Law Research (side task, started 2026-09-03)

Research notes on the employment laws and regulations of each US state that can
affect how HCM Next operates: what it must record, notify, retain, disclose,
time, or refuse when it governs a workforce change. One file per state, written
by a research agent from primary and official sources. These are research
inputs for the Legal plane's rule packs and for connector/partner scoping. They
are not legal advice and are not normative contracts; a rule enters the product
only through a reviewed rule pack.

## Queue

| State          | File                | Status  | Researched | Reviewed |
| -------------- | ------------------- | ------- | ---------- | -------- |
| Alabama        | `alabama.md`        | DRAFTED | 2026-09-03 |          |
| Alaska         | `alaska.md`         | DRAFTED | 2026-09-03 |          |
| Arizona        | `arizona.md`        | DRAFTED | 2026-09-03 |          |
| Arkansas       | `arkansas.md`       | DRAFTED | 2026-09-03 |          |
| California     | `california.md`     | DRAFTED | 2026-09-03 |          |
| Colorado       | `colorado.md`       | DRAFTED | 2026-09-03 |          |
| Connecticut    | `connecticut.md`    | DRAFTED | 2026-09-03 |          |
| Delaware       | `delaware.md`       | QUEUED  |            |          |
| Florida        | `florida.md`        | DRAFTED | 2026-09-03 |          |
| Georgia        | `georgia.md`        | DRAFTED | 2026-09-03 |          |
| Hawaii         | `hawaii.md`         | QUEUED  |            |          |
| Idaho          | `idaho.md`          | QUEUED  |            |          |
| Illinois       | `illinois.md`       | QUEUED  |            |          |
| Indiana        | `indiana.md`        | QUEUED  |            |          |
| Iowa           | `iowa.md`           | QUEUED  |            |          |
| Kansas         | `kansas.md`         | QUEUED  |            |          |
| Kentucky       | `kentucky.md`       | QUEUED  |            |          |
| Louisiana      | `louisiana.md`      | QUEUED  |            |          |
| Maine          | `maine.md`          | QUEUED  |            |          |
| Maryland       | `maryland.md`       | QUEUED  |            |          |
| Massachusetts  | `massachusetts.md`  | QUEUED  |            |          |
| Michigan       | `michigan.md`       | QUEUED  |            |          |
| Minnesota      | `minnesota.md`      | QUEUED  |            |          |
| Mississippi    | `mississippi.md`    | QUEUED  |            |          |
| Missouri       | `missouri.md`       | QUEUED  |            |          |
| Montana        | `montana.md`        | QUEUED  |            |          |
| Nebraska       | `nebraska.md`       | QUEUED  |            |          |
| Nevada         | `nevada.md`         | QUEUED  |            |          |
| New Hampshire  | `new-hampshire.md`  | QUEUED  |            |          |
| New Jersey     | `new-jersey.md`     | QUEUED  |            |          |
| New Mexico     | `new-mexico.md`     | QUEUED  |            |          |
| New York       | `new-york.md`       | QUEUED  |            |          |
| North Carolina | `north-carolina.md` | QUEUED  |            |          |
| North Dakota   | `north-dakota.md`   | QUEUED  |            |          |
| Ohio           | `ohio.md`           | QUEUED  |            |          |
| Oklahoma       | `oklahoma.md`       | QUEUED  |            |          |
| Oregon         | `oregon.md`         | QUEUED  |            |          |
| Pennsylvania   | `pennsylvania.md`   | QUEUED  |            |          |
| Rhode Island   | `rhode-island.md`   | QUEUED  |            |          |
| South Carolina | `south-carolina.md` | QUEUED  |            |          |
| South Dakota   | `south-dakota.md`   | QUEUED  |            |          |
| Tennessee      | `tennessee.md`      | QUEUED  |            |          |
| Texas          | `texas.md`          | QUEUED  |            |          |
| Utah           | `utah.md`           | QUEUED  |            |          |
| Vermont        | `vermont.md`        | QUEUED  |            |          |
| Virginia       | `virginia.md`       | QUEUED  |            |          |
| Washington     | `washington.md`     | QUEUED  |            |          |
| West Virginia  | `west-virginia.md`  | QUEUED  |            |          |
| Wisconsin      | `wisconsin.md`      | QUEUED  |            |          |
| Wyoming        | `wyoming.md`        | QUEUED  |            |          |

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
