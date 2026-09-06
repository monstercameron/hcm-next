# HR Workflow Catalogue

A breadth-first catalogue of the HR workflows an HCM control plane has to
be able to govern, written so that each entry can be turned into a workflow
definition, a conformance fixture or a user story without re-deriving its
actors, decisions, evidence and jurisdictional exposure.

This file is generated together with [`catalog.yaml`](catalog.yaml); the
YAML carries every field this document shows and is the machine-readable
form. Nothing here is implementation evidence. An entry marked `NEW` is a
design candidate, not a commitment, and the phase gates in
[the plan](../plan.md) and [the execution plan](../execution-plan.md)
continue to decide what is actually built.

## How to read an entry

```text
id                 WF-<DIMENSION>-<NNN>, stable and never reused
status             EXISTING  a compiled Go definition, a drafted intent
                             descriptor or an individual deep-dive workflow
                             document already covers it
                   PARTIAL   only archetype-level catalog rows, feature-intake
                             entries, specs or plan sections cover it
                   NEW       nothing in the repository covers it
archetype          the reusable shape from _shared/workflow-archetypes.md
complexity tier    1 single actor/single domain .. 5 adversarial/cross-cutting
intents            REAL      one of the fourteen drafted definitions
                   PROPOSED  a name chosen to the hcmnext.<domain>.<verb_noun>
                             convention that no definition file yet owns
                   creates   this workflow originates an instance of that
                             definition. Several workflows create instances
                             of the same definition; that is ordinary, and
                             it does not mean the definition is duplicated.
                   advances  it moves an instance another workflow created
                   reads     it consumes the result without changing it
actors             the full cast. An actor either performs a step or
                   participates without owning one (a subject who is
                   notified, an approver whose decision is a step's
                   condition, an auditor who reads the result). Where
                   both kinds are present the entry says which is which,
                   because a cast that hides the distinction is unusable
                   for working out who is accountable for a step.
steps              ordered, each carrying its runtime primitive and its actor
failure and repair a condition and the typed disposition it must produce
```

Step primitives are the workflow runtime vocabulary in
[`_engine/step-types.md`](_engine/step-types.md). Dispositions use the
kernel's own refusal and routing vocabulary: `REJECT`, `DENY`,
`WITHHELD`, `FAIL_CLOSED`, `REPLAN`, `REPLAN_AND_REAPPROVE`, `BLOCKED`,
`UNKNOWN`, `REPAIR_PLAN`. Lifecycle language is the five dimensions
(`RequestState`, `ExecutionState`, `BusinessState`, `ConsistencyState`,
`ObligationState`), never a single flat status.

## Reading the jurisdictional notes

Every entry carries three jurisdictional lines: US federal, US state
variation, and international. They exist so an entry is concrete enough to
turn into a test, and they are research inputs on the same footing as
[the state-law research](../research/state-employment-law/README.md): not
legal advice, and normative only once a reviewed rule pack carries them.

Two properties of these notes matter more than their content.

**They have an as-of date.** This catalogue was written against sources as
of 2026-09-05. Employment law moves every legislative session, and the
multi-state lists here are the fastest-rotting part of the document. A list
of states with a given duty is a snapshot, not a contract; the product
resolves the question from a versioned rule pack per jurisdiction, and an
entry that says otherwise is wrong. Where a rule has been repeatedly
amended or delayed, the entry says so rather than naming a date.

**A line saying `None` is a claim, not an absence.** It asserts that no
employment-law duty attaches to that workflow in that jurisdiction class.
For genuinely internal machinery, such as claiming a work item or
compiling a workflow definition, that is true. For anything touching a
worker, money, a document or a counterparty, `None` should be read as an
unexamined conclusion and challenged.

## Relationship to what already exists

- The six domain catalogs under this directory map roughly 198 HR-domain
  intents at archetype depth. That is a mapping, not a definition, so an
  entry whose only cover is such a row is marked `PARTIAL` and cites it.
- The nine compiled conformance workflows under
  `internal/workflow/conformance` are real, compiled definitions. Entries
  they cover are `EXISTING` and cite the `definition.go`. So are the
  twelve individual deep-dive documents under this directory and the
  reference workflows and user flows they pair with.
- The status is derived from the citations by one rule applied to every
  entry, so it is evidence-based rather than an author's judgement. The
  rule is in the generator and is restated above.
- The fourteen drafted intents in
  `definitions/governance/intent-conformance-descriptors.yaml` are the only
  `REAL` intent ids used here.
- The 49 feature groups and 253 features in
  `definitions/governance/feature-intent-intake.yaml` supplied the
  dimension partition and much of the naming.

## Dimensions

| Code  | Dimension                                 | Kind   | Workflows | EXISTING | PARTIAL |     NEW |
| ----- | ----------------------------------------- | ------ | --------: | -------: | ------: | ------: |
| `PEO` | People and core HR                        | DOMAIN |        14 |        6 |       6 |       2 |
| `ORG` | Organization design, jobs and positions   | DOMAIN |         9 |        1 |       7 |       1 |
| `WFP` | Workforce planning and headcount          | CROSS  |         5 |        2 |       1 |       2 |
| `CMP` | Compensation                              | DOMAIN |        14 |        4 |       7 |       3 |
| `PAY` | Payroll                                   | DOMAIN |        11 |        5 |       0 |       6 |
| `TAX` | Tax and regulatory                        | DOMAIN |         6 |        2 |       2 |       2 |
| `BEN` | Benefits                                  | DOMAIN |        13 |        3 |       1 |       9 |
| `TIM` | Time, attendance and scheduling           | DOMAIN |         6 |        4 |       2 |       0 |
| `LVE` | Leave and absence                         | DOMAIN |         8 |        3 |       0 |       5 |
| `REC` | Recruiting and ATS                        | DOMAIN |         8 |        0 |       6 |       2 |
| `ONB` | Onboarding and offboarding                | DOMAIN |         8 |        3 |       2 |       3 |
| `IAM` | Identity and access                       | DOMAIN |         7 |        2 |       2 |       3 |
| `TAL` | Talent and performance                    | DOMAIN |         6 |        3 |       1 |       2 |
| `LRN` | Learning and skills                       | DOMAIN |         6 |        3 |       0 |       3 |
| `EXP` | Employee experience and communications    | DOMAIN |         4 |        3 |       1 |       0 |
| `HRS` | HR service delivery and cases             | DOMAIN |         5 |        3 |       1 |       1 |
| `ERL` | Employee relations and investigations     | DOMAIN |         5 |        4 |       0 |       1 |
| `MOB` | Global mobility and immigration           | DOMAIN |         6 |        2 |       0 |       4 |
| `SAF` | Safety and workplace                      | DOMAIN |         6 |        0 |       0 |       6 |
| `PRV` | Privacy and data governance               | DOMAIN |         6 |        1 |       2 |       3 |
| `DOC` | Documents, forms and signatures           | DOMAIN |         4 |        0 |       2 |       2 |
| `WRK` | Workflow and human work                   | DOMAIN |         4 |        2 |       2 |       0 |
| `INT` | Integration and external systems          | DOMAIN |         4 |        0 |       3 |       1 |
| `DOP` | DataOps and administration                | DOMAIN |         4 |        2 |       2 |       0 |
| `SEC` | Authorization, security and identity      | DOMAIN |         5 |        0 |       2 |       3 |
| `ANA` | Reporting and analytics                   | DOMAIN |         5 |        1 |       1 |       3 |
| `AIA` | Semantic and agent intelligence           | DOMAIN |         3 |        1 |       1 |       1 |
| `REP` | Reconciliation, repair and operations     | DOMAIN |         4 |        1 |       2 |       1 |
| `BIL` | Billing and commercial                    | DOMAIN |         4 |        0 |       1 |       3 |
| `TEN` | Tenant and platform administration        | DOMAIN |         5 |        0 |       1 |       4 |
| `TRG` | System and trigger-driven                 | DOMAIN |         4 |        0 |       2 |       2 |
| `ESS` | Employee self-service                     | CROSS  |         6 |        0 |       3 |       3 |
| `MSS` | Manager self-service                      | CROSS  |         4 |        0 |       3 |       1 |
| `HRO` | HR operations and administration console  | CROSS  |         4 |        0 |       3 |       1 |
| `UXP` | Universal experience patterns             | CROSS  |         4 |        0 |       2 |       2 |
| `CON` | Connectivity and integration hub          | CROSS  |         4 |        1 |       1 |       2 |
| `DQG` | Data quality and lineage governance       | CROSS  |         3 |        0 |       2 |       1 |
| `CFG` | Configuration, rules and policies         | CROSS  |         4 |        1 |       1 |       2 |
| `POP` | Population segmentation and batch         | CROSS  |         4 |        0 |       3 |       1 |
| `SRC` | Search and discovery                      | CROSS  |         4 |        1 |       1 |       2 |
| `INC` | Incident management                       | CROSS  |         4 |        0 |       1 |       3 |
| `EVT` | Events, scheduling and automation         | CROSS  |         3 |        0 |       1 |       2 |
| `SBX` | Sandbox, testing and isolation            | CROSS  |         3 |        1 |       1 |       1 |
| `GRC` | Governance, compliance and audit          | CROSS  |         5 |        0 |       2 |       3 |
| `PRG` | Program and portfolio management          | CROSS  |         3 |        0 |       0 |       3 |
| `PRO` | Provisioning lifecycle management         | CROSS  |         3 |        0 |       2 |       1 |
| `CBI` | Cross-domain business capabilities        | CROSS  |         3 |        0 |       3 |       0 |
| `CBA` | Labor relations and collective bargaining | CROSS  |         4 |        2 |       0 |       2 |
| `CWF` | Contingent workforce                      | CROSS  |         4 |        0 |       1 |       3 |
|       | **Total**                                 |        |   **268** |   **67** |  **90** | **111** |

Tier distribution: tier 1 = 5, tier 2 = 62, tier 3 = 107, tier 4 = 76, tier 5 = 18.

---

## PEO. People and core HR

14 workflows.

### WF-PEO-001. Create a person from verified identity claims

- **Status**: `EXISTING` - planning/workflows/people/catalog.md row CreatePerson (archetype A1); planning/workflows/discovery-backlog.md People and employment
- **Archetype**: `A1` | **Complexity tier**: 2
- **Actors**: HR business partner, compliance officer, integration system
- **Trigger**: An HR business partner or an inbound onboarding feed asserts identity claims for a person who has no Person record.
- **Preconditions**:
  - Tenant and organization scope resolved for the initiating principal.
  - Identity resolution policy version pinned for the request.
  - A processing purpose exists that permits creating a Person from these claim types.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the claim set against the Person index and return candidate matches with scores and the policy's uncertainty band
  2. `DECISION` (HR business partner) route NO_MATCH to creation, POSSIBLE_MATCH to human resolution, and CONFIRMED_MATCH to a refusal that names the existing person only if the caller may know it exists
  3. `TASK` (HR business partner) human identity resolution when the score falls inside the uncertainty band; the resolver records which claims they compared
  4. `TRANSFORM` (HR business partner) build the immutable Person proposal with the normalized claim set and its provenance
  5. `APPROVAL` (compliance officer) compliance approval when any claim is a government identifier or a protected characteristic
  6. `CAPABILITY` (integration system) commit the Person aggregate and its first identity-claim revisions
  7. `OBSERVE` (integration system) confirm the Person is retrievable at the recorded-at watermark before closing
  8. `END` (integration system) close with ObligationState PENDING while the identity-proof retention obligation is open
- **Intents**:
  - `hcmnext.people.create_person/v1` (PROPOSED, creates)
  - `hcmnext.people.resolve_person_identity/v1` (PROPOSED, creates)
- **Reads**:
  - `identity`: match policy version and uncertainty band
  - `people`: Person index, existing identity claims and their source authority
  - `privacy`: processing purpose, consent state and classification floor for each claim type
- **Writes**:
  - `people`: Person aggregate, identity-claim revisions, match decision record
  - `privacy`: processing activity entry naming the purpose and the claim classes used
- **Evidence required**:
  - Canonical request digest of the claim set.
  - Match evidence: candidates considered, scores, policy version, and the human resolver's decision if one was taken.
  - Ledger fact for the Person creation carrying effective-at and known-at.
- **Failure and repair**:
  - Two concurrent creations assert overlapping claim sets -> REJECT the second on the idempotency scope and return the first Person id only when the caller is authorized to know it; otherwise WITHHELD
  - Match score falls inside the uncertainty band and no resolver acts before the task deadline -> BLOCKED with ObligationState PENDING; never auto-merge above the policy's uncertainty threshold
  - A government identifier is supplied without a permitting purpose -> FAIL_CLOSED with a typed purpose refusal that does not echo the identifier back
- **Jurisdiction**:
  - US federal: No federal statute governs the record itself, but IRCA Form I-9 and IRS Form W-4 collection depend on it, so the claim set must support both without becoming their system of record.
  - US state variation: California Civil Code 1798.81.5 and the CPRA employee-data provisions make Social Security numbers and driver licence numbers a security-controlled class; Illinois BIPA forbids taking a biometric claim without prior written release; New York and Colorado add their own breach-notification duties on the same claim classes.
  - International: Under UK and EU GDPR the claim set is personal data processed under Article 6(1)(b) and (c); national identifiers fall under Article 87, so the identifier classes permitted vary by member state.

### WF-PEO-002. Merge two person records that describe the same human

- **Status**: `PARTIAL` - planning/workflows/people/catalog.md row MergePersonRecords (archetype A5)
- **Archetype**: `A5` | **Complexity tier**: 4
- **Actors**: HR business partner, compliance officer, auditor, IT or access administrator, integration system
  - performing a step: HR business partner, compliance officer, integration system
  - participating without owning a step: auditor, IT or access administrator
- **Trigger**: Identity resolution or a data-quality finding reports that two Person aggregates describe one human.
- **Preconditions**:
  - Both Person aggregates are visible to the initiator in the same organization scope.
  - Neither Person is under a legal hold that forbids restructuring its references.
  - A separation plan template exists so the merge is reversible.
- **Steps**:
  1. `CAPABILITY` (integration system) enumerate every referencing aggregate across all domains for both persons
  2. `TRANSFORM` (HR business partner) compute the survivor and loser assignment and the redirect map for every reference
  3. `CAPABILITY` (HR business partner) simulate the merged graph and report the fields where the two records disagree
  4. `DECISION` (HR business partner) route to dual review when the disagreement set touches pay, tax, benefits or access
  5. `APPROVAL` (HR business partner) two independent HR business partner approvals bound to the same proposal digest; the proposer may not be either approver
  6. `APPROVAL` (compliance officer) compliance approval that the retained record satisfies the tenant's records schedule
  7. `CAPABILITY` (integration system) commit the merge as one multi-stream transaction with redirects, never as a delete
  8. `OBSERVE` (integration system) confirm downstream systems resolved the redirect for payroll, benefits and access
  9. `END` (integration system) close only ConsistencyState once every observed system reports the survivor
- **Intents**:
  - `hcmnext.people.merge_person_records/v1` (PROPOSED, creates)
  - `hcmnext.people.separate_person_records/v1` (PROPOSED, creates)
  - `hcmnext.work.approve_proposal/v1` (REAL, advances)
- **Reads**:
  - `access`: entitlements derived from either record
  - `benefits`: enrollments and dependents on both records
  - `payroll`: open payments and year-to-date accumulators on both records
  - `people`: both Person aggregates, all claims, both worker and employment chains
- **Writes**:
  - `dataops`: reference-repair batch for every domain that held a stale reference
  - `people`: survivor Person, redirect records for the loser, merge decision record
- **Evidence required**:
  - Full pre-merge snapshot of both records so the separation plan can reconstruct them.
  - Both approval certificates bound to the identical proposal digest.
  - Ledger facts for each redirected reference, not one aggregate fact.
- **Failure and repair**:
  - A referencing domain rejects the redirect after the local commit -> ConsistencyState DRIFT and a bounded REPAIR_PLAN naming exactly the references that did not move; the merge is never rolled back silently
  - The two persons have conflicting year-to-date payroll accumulators -> REJECT before commit; the payroll correction must be its own intent with its own approvals
  - A legal hold is placed on either record while the proposal waits for approval -> REPLAN_AND_REAPPROVE, because the retention constraint changed after the approvers saw the proposal
- **Jurisdiction**:
  - US federal: Merging records that carry different Social Security numbers has IRS reporting consequences: Forms W-2 already filed under the loser identifier are not corrected by the merge and require a separate Form W-2c.
  - US state variation: California Labor Code 1198.5 gives the employee a 30-day inspection right over the personnel file, so the merged file must remain producible; state record-retention schedules differ on how long the loser's pre-merge snapshot is kept.
  - International: GDPR Article 5(1)(d) accuracy supports the merge, but Article 17 erasure requests against the loser record must not destroy the audit evidence the merge itself depends on.

### WF-PEO-003. Change a worker's direct manager

- **Status**: `EXISTING` - definitions/governance/intent-conformance-descriptors.yaml descriptor hcmnext.people.change_manager; planning/workflows/people/manager-change.md; planning/reference-workflows/manager-change.md
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: manager, HR business partner, employee, IT or access administrator, integration system
  - performing a step: manager, HR business partner, IT or access administrator, integration system
  - participating without owning a step: employee
- **Trigger**: A manager or HR business partner proposes a new direct manager for a worker with an effective date.
- **Preconditions**:
  - The worker has exactly one active primary assignment at the effective date.
  - The incoming manager has an active employment in a scope the initiator may read.
  - No unresolved future-dated manager change already exists for the same interval.
- **Steps**:
  1. `CAPABILITY` (HR business partner) read the authoritative worker state and the organization graph at the effective date
  2. `RULE` (HR business partner) validate that the new edge introduces no cycle and does not violate the exclusive primary-manager cardinality
  3. `TRANSFORM` (HR business partner) build the immutable proposal binding worker, outgoing manager, incoming manager and effective-at
  4. `DECISION` (HR business partner) route to REJECT on a circular or invalid graph, otherwise to approvals
  5. `APPROVAL` (manager) outgoing manager and incoming manager both approve, bound to the proposal digest
  6. `WAIT` (IT or access administrator) hold until the effective date, re-reading the delegation chain on wake
  7. `CHECKPOINT` (HR business partner) revalidate that both managers are still active and still hold their scopes
  8. `CAPABILITY` (integration system) commit the manager relationship revision as an effective-dated fact
  9. `SIGNAL` (IT or access administrator) notify the employee and recalculate relationship-derived approval routing and access
- **Intents**:
  - `hcmnext.people.change_manager/v1` (REAL, creates)
  - `hcmnext.work.approve_proposal/v1` (REAL, advances)
  - `hcmnext.access.recalculate_entitlement/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: entitlements that derive from the manager relationship
  - `organization`: org graph, exclusivity and cycle constraints at the effective date
  - `people`: worker state, current manager edge, assignment identity
- **Writes**:
  - `access`: entitlement recalculation intent scoped to the effective date
  - `people`: manager relationship revision with effective-at and known-at
- **Evidence required**:
  - Request digest binding the exact edge change.
  - Both approval decisions with their authority snapshots.
  - Conflict-resolution record if another manager change touched the same interval.
- **Failure and repair**:
  - An unauthorized initiator or a stale delegation chain -> REJECT; the descriptor's negative policy names this exactly
  - The proposed edge closes a cycle in the reporting graph -> REPLAN with the cycle path named, so the proposer can pick a different parent
  - The outgoing manager terminates between approval and the effective date -> REPLAN_AND_REAPPROVE; the approval bound to the old authority snapshot is invalidated
- **Jurisdiction**:
  - US federal: No federal notice duty attaches to a reporting-line change on its own, but if the change alters FLSA exempt duties it can change overtime eligibility and must trigger a classification review.
  - US state variation: California Labor Code 2810.5 requires written notice within seven days only when pay, pay basis or payday changes, so a pure manager change usually carries no notice duty; New York Labor Law 195(1) is read more broadly by some employers and their configured rule pack may add a notice obligation.
  - International: In Germany and the Netherlands a works council may have consultation rights when a reporting change is part of an organizational restructure rather than an isolated edge.

### WF-PEO-004. Correct an employment fact retroactively

- **Status**: `PARTIAL` - planning/workflows/people/catalog.md row RetroactivelyCorrectEmployment (archetype A5)
- **Archetype**: `A5` | **Complexity tier**: 4
- **Actors**: HR business partner, payroll administrator, auditor, compliance officer, integration system
  - performing a step: HR business partner, payroll administrator, compliance officer, integration system
  - participating without owning a step: auditor
- **Trigger**: An audit, a DataOps diff or an employee complaint shows that an employment fact was wrong for a past interval.
- **Preconditions**:
  - The interval to correct is closed and its downstream payroll periods are identified.
  - The corrective authority is the field owner, not a generic administrator.
  - The original fact and its provenance are still retrievable.
- **Steps**:
  1. `CAPABILITY` (HR business partner) reconstruct the employment state as known at the original recorded-at and as it is known now
  2. `TRANSFORM` (HR business partner) build the corrective revision that appends rather than overwrites, with its own known-at
  3. `CAPABILITY` (payroll administrator) simulate the downstream impact on payroll, benefits eligibility, leave entitlement and government reporting
  4. `DECISION` (payroll administrator) route to a payroll retro child intent when any closed pay period is affected
  5. `APPROVAL` (HR business partner) HR business partner and payroll administrator both approve, with separation of duties enforced against the proposer
  6. `CAPABILITY` (integration system) commit the correction and the bound child intents in one transaction plan
  7. `OBSERVE` (integration system) confirm the payroll provider accepted the retro adjustment
  8. `END` (compliance officer) close BusinessState while ObligationState stays PENDING for any amended filing
- **Intents**:
  - `hcmnext.people.correct_employment/v1` (PROPOSED, creates)
  - `hcmnext.payroll.adjust_retroactive/v1` (PROPOSED, creates)
  - `hcmnext.intelligence.explain_transaction/v1` (REAL, reads)
- **Reads**:
  - `payroll`: closed pay periods, accumulators and prior pay statements
  - `people`: employment revision chain with effective-at and known-at
  - `regulatory`: filings already submitted that used the wrong fact
- **Writes**:
  - `payroll`: retro adjustment lines attributed to the corrected interval
  - `people`: corrective employment revision, superseding the erroneous one without deleting it
  - `regulatory`: amended-filing obligation
- **Evidence required**:
  - Bitemporal trace showing both the original and the corrected view.
  - Explanation artifact naming which downstream figures moved and by how much.
  - Obligation record for every amended government filing the correction requires.
- **Failure and repair**:
  - The correction would change a figure on a filed government return -> the local commit proceeds but ObligationState stays PENDING until the amended filing is submitted; never close the intent on the local write alone
  - A projection rebuild is running against the same interval -> REPLAN after the rebuild watermark passes, so the corrected fact is not overwritten by a stale replay
  - The proposer is also the only available approver -> FAIL_CLOSED on separation of duties rather than allowing self-approval
- **Jurisdiction**:
  - US federal: FLSA 29 CFR 516 requires three years of payroll records and two years of the records that explain the wage computation, so the pre-correction state must survive the correction; an IRS Form W-2c is required when a corrected fact changes reported wages.
  - US state variation: California Labor Code 226 penalties attach per employee per defective wage statement, so a retroactive correction that invalidates past statements creates exposure that the workflow should surface rather than hide; New York requires six years of payroll record retention, twice the federal floor.
  - International: In the EU, correcting personal data engages GDPR Article 16 rectification, and the controller must notify recipients under Article 19 unless it proves impossible or disproportionate.

### WF-PEO-005. Change a worker's legal name with evidence

- **Status**: `EXISTING` - planning/workflows/people/legal-name-change.md; planning/workflows/people/catalog.md row ChangeLegalName
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: employee, HR business partner, payroll administrator, external partner or carrier, integration system
  - performing a step: employee, HR business partner, integration system
  - participating without owning a step: payroll administrator, external partner or carrier
- **Trigger**: An employee submits a legal name change with supporting documentary evidence.
- **Preconditions**:
  - The employee has an active employment and a self-service session at the required assurance level.
  - An evidence compartment exists that can hold the document without exposing it to the manager.
  - The downstream authorities that master the name are enumerated.
- **Steps**:
  1. `TASK` (employee) the employee submits the new legal name and uploads the supporting document
  2. `CAPABILITY` (integration system) scan and classify the document, then place it in a restricted evidence compartment
  3. `TASK` (HR business partner) an HR business partner verifies the document against the claimed name without copying its contents into the general record
  4. `DECISION` (HR business partner) route to REJECT when the document does not support the claimed name, with a reason that does not disclose the document to the manager
  5. `CAPABILITY` (integration system) commit the legal name revision while leaving the preferred name untouched
  6. `PARALLEL` (integration system) dispatch name updates to payroll, benefits carriers, the identity provider and the badge system in declared order
  7. `OBSERVE` (integration system) collect an authoritative observation from each downstream authority
  8. `JOIN` (HR business partner) close ConsistencyState only when every authority has confirmed or been recorded as drifted
- **Intents**:
  - `hcmnext.people.change_legal_name/v1` (PROPOSED, creates)
  - `hcmnext.people.explain_worker_state/v1` (REAL, reads)
- **Reads**:
  - `documents`: evidence artifact metadata and its classification
  - `integration`: connector health and ordering constraints for each downstream authority
  - `people`: current legal name revision, preferred name, identity claims
- **Writes**:
  - `documents`: evidence reference in a restricted compartment
  - `integration`: connector operations and their observations
  - `people`: legal name revision with effective-at
- **Evidence required**:
  - Digest of the uploaded document, never its bytes, in the request evidence.
  - Verification decision naming who compared what.
  - One observation per downstream authority, so a partial rollout is visible.
- **Failure and repair**:
  - The payroll provider accepts the change but the benefits carrier rejects it -> ConsistencyState DRIFT with a REPAIR_PLAN scoped to the carrier alone; the local name is not reverted
  - The manager attempts to open the evidence compartment -> DENY with a named refusal; the manager sees that a name change occurred but not the document
  - The employee submits a second name change while the first is still dispatching -> REJECT the second on the conflict footprint until the first settles or is cancelled
- **Jurisdiction**:
  - US federal: A name change requires a corrected Form I-9 entry in Section 3 or its current equivalent, and the Social Security Administration must show the new name before Form W-2 reporting uses it.
  - US state variation: California Labor Code 2810.5 does not require notice for a name change; several states treat the supporting document (a court order or marriage certificate) as a record with its own retention and confidentiality rules distinct from the personnel file.
  - International: In the UK a deed poll is sufficient evidence and no state registry confirmation exists, so the observation step has no authoritative external confirmation and must record UNKNOWN rather than inventing one.

### WF-PEO-006. Convert a contingent worker to an employee

- **Status**: `PARTIAL` - planning/workflows/people/catalog.md row ConvertWorkerType (archetype A8)
- **Archetype**: `A8` | **Complexity tier**: 4
- **Actors**: hiring manager, HR business partner, payroll administrator, compliance officer, finance partner, integration system, IT or access administrator
  - performing a step: hiring manager, HR business partner, compliance officer, finance partner, integration system, IT or access administrator
  - participating without owning a step: payroll administrator
- **Trigger**: A hiring manager proposes converting a contingent worker to a direct employee on a chosen date.
- **Preconditions**:
  - The contingent engagement has a defined end date or a termination-for-conversion clause.
  - A funded position exists in the destination organization.
  - A worker classification determination has been run for the destination jurisdiction.
- **Steps**:
  1. `CAPABILITY` (HR business partner) read the contingent engagement, the vendor contract terms and the tenure history
  2. `RULE` (compliance officer) run the worker classification determination for the destination jurisdiction and record the test applied
  3. `CAPABILITY` (finance partner) reserve the destination position and the compensation budget
  4. `TRANSFORM` (HR business partner) build the composite proposal: end the engagement, create the employment, create the assignment, set compensation
  5. `APPROVAL` (hiring manager) hiring manager, HR business partner and finance partner approve the bound child set
  6. `APPROVAL` (compliance officer) compliance approval when the vendor contract carries a conversion fee or a no-hire clause
  7. `WAIT` (integration system) hold to the conversion date so the engagement end and the employment start abut without a gap
  8. `CHECKPOINT` (finance partner) revalidate the position reservation, the budget reservation and the classification result
  9. `CAPABILITY` (integration system) commit the exit and start effects as one bound effect graph
  10. `SIGNAL` (IT or access administrator) trigger onboarding, benefits eligibility and access recalculation as child intents
- **Intents**:
  - `hcmnext.people.convert_worker_type/v1` (PROPOSED, creates)
  - `hcmnext.rewards.reserve_compensation_budget/v1` (REAL, creates)
  - `hcmnext.regulatory.determine_worker_classification/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: available compensation budget in the destination cost centre
  - `people`: contingent engagement, tenure, vendor relationship
  - `position`: destination position capacity and reservation state
  - `regulatory`: classification rule pack for the destination jurisdiction
- **Writes**:
  - `benefits`: eligibility evaluation triggered by the new employment
  - `compensation`: initial compensation package
  - `people`: engagement end, new Employment and Assignment
- **Evidence required**:
  - Classification determination with the exact test and rule pack version applied.
  - Both reservations with their expiry and consumption records.
  - Evidence that the engagement end and the employment start form one contiguous interval.
- **Failure and repair**:
  - The classification determination returns contractor for the destination jurisdiction -> FAIL_CLOSED; the conversion cannot proceed on an inconsistent determination and the finding is recorded as a compliance obligation
  - The vendor's no-hire clause has not expired -> BLOCKED pending legal review; the workflow surfaces the clause rather than treating it as a soft warning
  - The budget reservation expires while the proposal waits for the conversion date -> REPLAN with a fresh reservation; a lapsed reservation must never be silently renewed
- **Jurisdiction**:
  - US federal: The IRS common-law test and FLSA economic-reality test can disagree, so the workflow records which test each determination used; misclassification exposure runs to back taxes under IRC 3509 and back wages under FLSA.
  - US state variation: California AB 5 applies the ABC test, and prong B (work outside the usual course of business) is the one most conversions fail, so a California determination is materially stricter than the federal one; New Jersey and Massachusetts apply their own ABC variants.
  - International: In the UK the IR35 off-payroll rules put the status determination duty on the client for medium and large businesses, and a status determination statement must be issued to the worker.

### WF-PEO-007. Add a second concurrent employment for one worker

- **Status**: `PARTIAL` - planning/workflows/people/catalog.md row AddEmploymentRelationship
- **Archetype**: `A1` | **Complexity tier**: 3
- **Actors**: HR business partner, payroll administrator, manager, compliance officer, integration system
  - performing a step: HR business partner, payroll administrator, compliance officer, integration system
  - participating without owning a step: manager
- **Trigger**: A worker takes a second role in a different legal entity or on a different employment terms set while keeping the first.
- **Preconditions**:
  - The worker has an active primary employment.
  - The tenant's policy permits concurrent employments and defines which one is primary.
  - Both legal entities are in scopes the initiator may read.
- **Steps**:
  1. `CAPABILITY` (HR business partner) read both legal entities, their pay groups and their working-time rules
  2. `RULE` (compliance officer) evaluate the combined scheduled hours against the applicable working-time and overtime rules
  3. `DECISION` (compliance officer) route to REJECT when the combined hours breach a hard working-time limit rather than warning
  4. `TRANSFORM` (HR business partner) build the second employment proposal and declare which employment is primary for benefits and tax
  5. `APPROVAL` (payroll administrator) both managers approve, and payroll approves the aggregation rule for tax withholding
  6. `CAPABILITY` (integration system) commit the second Employment and its Assignment
  7. `OBSERVE` (integration system) confirm the payroll provider created the second employment without collapsing it into the first
  8. `END` (payroll administrator) close with an open obligation to review the aggregation at the next tax year boundary
- **Intents**:
  - `hcmnext.people.add_employment_relationship/v1` (PROPOSED, creates)
  - `hcmnext.payroll.enroll_worker/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: existing employments, primary designation policy
  - `regulatory`: working-time rule pack for both jurisdictions
  - `time`: scheduled hours on the existing employment
- **Writes**:
  - `payroll`: second payroll enrollment with an explicit aggregation rule
  - `people`: second Employment and Assignment, primary designation revision
- **Evidence required**:
  - Working-time evaluation with the rule pack version and the combined-hours computation.
  - Payroll aggregation decision, because withholding on two employments is a choice, not a default.
  - Both managers' approval certificates.
- **Failure and repair**:
  - The payroll provider merges the two employments into one record -> ConsistencyState DRIFT and a REPAIR_PLAN; a merged record silently under-withholds and must not be treated as success
  - The combined hours breach a jurisdictional maximum -> REJECT with the exact limit and the computed total named
  - The primary designation is ambiguous because both employments are full time -> BLOCKED pending an explicit human designation; the system does not guess which employment carries benefits
- **Jurisdiction**:
  - US federal: FLSA requires aggregating hours across employments with the same employer for overtime purposes, so two employments in the same legal entity are not two independent overtime calculations; joint employer analysis applies when the entities are related.
  - US state variation: California requires daily overtime after eight hours, which makes concurrent employments in California far more likely to breach a limit than in a weekly-overtime state; New York's spread-of-hours pay adds a further exposure.
  - International: The EU Working Time Directive caps average weekly working time at 48 hours across all employments with the same employer, and several member states have not permitted individual opt-outs.

### WF-PEO-008. Explain a worker's state at a chosen point in time

- **Status**: `EXISTING` - definitions/governance/intent-conformance-descriptors.yaml descriptor hcmnext.people.explain_worker_state; internal/domains/dataops history and drift surfaces
- **Archetype**: `A6` | **Complexity tier**: 2
- **Actors**: HR business partner, auditor, employee, AI agent, integration system
  - performing a step: integration system
  - participating without owning a step: HR business partner, auditor, employee, AI agent
- **Trigger**: A reader asks what was true about a worker at a given effective date, as it was known at a given time.
- **Preconditions**:
  - The caller has a purpose that permits the read.
  - The requested effective-at and known-at are both supplied; neither defaults silently to now.
  - The source authority snapshot for each field is resolvable.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the caller's authorization per field, not per record
  2. `TRANSFORM` (integration system) project the worker state at the requested effective-at as known at the requested known-at
  3. `DECISION` (integration system) for each denied field, return DENIED by name so a refusal is not mistaken for absence; for a subject the caller may not know exists, return WITHHELD
  4. `CAPABILITY` (integration system) attach source authority and provenance to every disclosed field
  5. `END` (integration system) complete with zero writes and zero effects, and a zero-effect receipt proving it
- **Intents**:
  - `hcmnext.people.explain_worker_state/v1` (REAL, creates)
- **Reads**:
  - `people`: worker, employment and assignment revisions across both time axes
  - `privacy`: field-level classification and the caller's purpose
  - `provenance`: source authority and freshness for each field
- **Writes**:
- **Evidence required**:
  - Request digest including both time coordinates.
  - Denied-access evidence naming which fields were refused and under which policy version.
  - Zero-effect receipt.
- **Failure and repair**:
  - The caller lacks read authorization for the subject -> DENY_WITH_NO_DISCLOSURE; the response must not reveal that the worker exists
  - The requested effective-at is in the future beyond the last approved change -> RETURN_UNCERTAINTY rather than projecting an unapproved future state as fact
  - A field's source authority snapshot is older than its freshness policy -> return the field with an explicit staleness marker, never silently as current
- **Jurisdiction**:
  - US federal: No federal statute compels this read, but it is the evidence an employer produces under an FLSA or EEOC investigation, so the bitemporal answer must be reproducible.
  - US state variation: California Labor Code 1198.5 gives a 30-day personnel-file inspection right and 226(b) a wage-statement inspection right, which this workflow serves; Illinois, Massachusetts, Connecticut and about twenty other states have their own inspection statutes with different windows.
  - International: GDPR Article 15 subject access requires the same projection plus the processing purposes and recipients, so the international variant returns a superset of the US one.

### WF-PEO-009. Schedule and later cancel a future-dated worker change

- **Status**: `PARTIAL` - planning/workflows/people/catalog.md rows FutureDateWorkerChange and CancelFutureWorkerChange
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: manager, HR business partner, employee, finance partner, integration system
  - performing a step: manager, HR business partner, finance partner, integration system
  - participating without owning a step: employee
- **Trigger**: An approved change is bound to a future effective date and is then cancelled before it activates.
- **Preconditions**:
  - An approved proposal exists with an effective date in the future.
  - The cancellation window policy for the change type is resolved.
  - Dependent proposals and reservations that reference the change are enumerable.
- **Steps**:
  1. `CAPABILITY` (integration system) register the effective-date activation trigger against the approved proposal
  2. `WAIT` (integration system) hold until the effective date while recording every invalidating event that arrives
  3. `TASK` (manager) the initiator submits a cancellation with a reason before the activation fires
  4. `CAPABILITY` (finance partner) enumerate dependent proposals, reservations and downstream child intents
  5. `DECISION` (HR business partner) route to REJECT when the change has already crossed an irreversible boundary such as a dispatched payroll effect
  6. `APPROVAL` (HR business partner) the original approvers acknowledge the cancellation when the change had reached APPROVED
  7. `CAPABILITY` (integration system) record a cancellation decision that supersedes without deleting, and release every reservation
  8. `SIGNAL` (HR business partner) notify everyone who was told the change would happen, including the employee
- **Intents**:
  - `hcmnext.work.reject_proposal/v1` (REAL, advances)
  - `hcmnext.people.cancel_future_change/v1` (PROPOSED, creates)
  - `hcmnext.rewards.release_compensation_budget/v1` (REAL, creates)
- **Reads**:
  - `budget`: reservations held against the proposal
  - `people`: the pending proposal and its approval bindings
  - `workflow`: timer registration and any dependent instances
- **Writes**:
  - `budget`: reservation release receipts
  - `people`: cancellation decision record and supersession reference
- **Evidence required**:
  - Cancellation decision with its reason and its actor.
  - Release receipt for every reservation, distinguishing released from already consumed.
  - The original proposal remains retrievable; cancellation is append-only.
- **Failure and repair**:
  - The activation timer fires while the cancellation is in flight -> the cancellation loses; the change activates and the correction becomes a separate corrective intent, which is honest about what happened
  - A dependent proposal was already approved on the assumption this change would land -> BLOCKED until the dependent is replanned; cancelling silently would leave the dependent bound to a fact that will never exist
  - A reservation was already consumed by a partially executed child -> REPAIR_PLAN rather than a release, because the money or capacity has moved
- **Jurisdiction**:
  - US federal: If the cancelled change was a scheduled pay reduction, several federal contexts treat advance notice as having been given, so the cancellation notice matters as much as the original.
  - US state variation: Missouri, Nevada, North Carolina and several other states require advance written notice before a wage reduction takes effect; cancelling the reduction must retract that notice or the next reduction inherits a stale one.
  - International: Where a works council was consulted on the original change, cancelling it may itself require notification under the applicable consultation agreement.

### WF-PEO-010. Reinstate a worker after an erroneous termination

- **Status**: `EXISTING` - planning/workflows/people/catalog.md row ReinstateEmployment (archetype A4); internal/workflow/conformance/termination/definition.go terminal TERMINATION_REQUIRES_REINSTATEMENT_INTENT
- **Archetype**: `A4` | **Complexity tier**: 4
- **Actors**: HR business partner, payroll administrator, IT or access administrator, benefits administrator, employee, integration system
  - performing a step: HR business partner, payroll administrator, benefits administrator, integration system
  - participating without owning a step: IT or access administrator, employee
- **Trigger**: A termination is found to have been executed in error and the worker must be restored as if it had not happened.
- **Preconditions**:
  - The termination transaction and every effect it dispatched are enumerable.
  - The gap interval between the termination date and today is known.
  - The worker consents to reinstatement where the tenant's policy requires it.
- **Steps**:
  1. `CAPABILITY` (HR business partner) enumerate every effect the termination dispatched: payroll final pay, benefits termination, access revocation, equipment recovery
  2. `DECISION` (HR business partner) distinguish reinstatement, which restores the original employment, from rehire, which creates a new one; the choice changes seniority, benefits and vesting
  3. `CAPABILITY` (payroll administrator) simulate the restoration including back pay, benefits continuity and the access grants to recreate
  4. `APPROVAL` (HR business partner) HR business partner, payroll administrator and benefits administrator approve their own streams
  5. `CAPABILITY` (integration system) commit the reinstatement as a correction to the original employment, not a new one
  6. `PARALLEL` (integration system) dispatch back pay, benefits reinstatement with carrier retro, and access re-provisioning
  7. `OBSERVE` (integration system) observe each downstream authority and record which ones could not restore the original identifiers
  8. `END` (benefits administrator) close only the dimensions that settled; carrier retro commonly stays PENDING_OBSERVATION for weeks
- **Intents**:
  - `hcmnext.people.reinstate_employment/v1` (PROPOSED, creates)
  - `hcmnext.payroll.adjust_retroactive/v1` (PROPOSED, creates)
  - `hcmnext.benefits.continue_coverage/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: revoked entitlements and the accounts that were disabled or deleted
  - `benefits`: terminated enrollments and any COBRA election already offered
  - `payroll`: final pay already issued and its tax treatment
  - `people`: terminated employment, termination reason and its evidence
- **Writes**:
  - `access`: re-provisioning intents
  - `benefits`: reinstated enrollments with retroactive carrier effective dates
  - `payroll`: back pay and reversal of the final-pay tax treatment
  - `people`: corrective employment revision restoring continuity
- **Evidence required**:
  - The original termination evidence, unaltered.
  - Reinstatement decision naming why the termination was erroneous.
  - One observation per downstream authority, including the ones that created new identifiers instead of restoring old ones.
- **Failure and repair**:
  - The identity provider deleted rather than disabled the account -> the account cannot be restored; a new identity is provisioned and the drift is recorded as a permanent divergence with a REPAIR_PLAN for the entitlement mapping
  - A COBRA election notice was already sent -> the notice must be retracted in writing and the retraction retained; the workflow raises this as a mandatory obligation rather than a message
  - Final pay was already issued and taxed as a termination payment -> REPAIR_PLAN through payroll; the tax treatment is corrected by an adjustment, never by deleting the original payment
- **Jurisdiction**:
  - US federal: COBRA notice duties under ERISA are triggered by the qualifying event, so an erroneous termination creates a real notice that must be retracted; back pay is wages for FICA and FUTA purposes in the year paid, not the year earned.
  - US state variation: California Labor Code 201 to 203 waiting-time penalties may already have accrued on the erroneous final pay, and the reinstatement does not automatically extinguish them; the workflow surfaces the exposure to the payroll administrator.
  - International: In the UK an erroneous dismissal that is withdrawn may still count as a dismissal for unfair-dismissal purposes unless the employee agrees to the withdrawal, so consent is a required precondition rather than a courtesy.

### WF-PEO-011. Record a change of work location that crosses a tax jurisdiction

- **Status**: `PARTIAL` - planning/workflows/people/catalog.md row ChangeWorkLocation (archetypes A2 and A8)
- **Archetype**: `A8` | **Complexity tier**: 4
- **Actors**: employee, manager, payroll administrator, compliance officer, finance partner, integration system
  - performing a step: payroll administrator, compliance officer, integration system
  - participating without owning a step: employee, manager, finance partner
- **Trigger**: An employee moves their primary work location to an address in a different state or country.
- **Preconditions**:
  - The new address is normalized and geocoded to a jurisdiction.
  - The employer has or can obtain registration in the destination jurisdiction.
  - The effective date is known and does not fall inside a locked pay period.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) normalize the address and resolve the work and residence jurisdictions separately
  2. `RULE` (compliance officer) evaluate employer registration, nexus and permanent-establishment exposure in the destination
  3. `DECISION` (compliance officer) route to BLOCKED when the employer is not registered and registration is a precondition, not a follow-up
  4. `CAPABILITY` (payroll administrator) simulate the tax profile change, the minimum wage floor, the overtime rule change and the leave entitlement change
  5. `APPROVAL` (compliance officer) manager, payroll administrator and finance partner approve; compliance approves the nexus position
  6. `WAIT` (payroll administrator) hold to the effective date, which must align with a pay period boundary where the destination requires it
  7. `CAPABILITY` (integration system) commit the location revision and the derived tax profile revision as separate owned facts
  8. `SIGNAL` (compliance officer) issue the destination jurisdiction's required new-hire or change notices
  9. `OBSERVE` (integration system) confirm the payroll provider applied the new withholding from the correct period
- **Intents**:
  - `hcmnext.people.change_work_location/v1` (PROPOSED, creates)
  - `hcmnext.regulatory.resolve_jurisdiction/v1` (PROPOSED, creates)
  - `hcmnext.payroll.change_tax_profile/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: current tax profile, pay group and period calendar
  - `people`: current work and residence addresses and their effective intervals
  - `regulatory`: registration status, nexus rules and rule packs for both jurisdictions
  - `time`: schedule and overtime rule set in force
- **Writes**:
  - `payroll`: tax profile revision and possible pay group change
  - `people`: work location revision
  - `regulatory`: registration obligation and notice obligations
- **Evidence required**:
  - Jurisdiction resolution with the rule pack version and the address normalization result.
  - Nexus and permanent-establishment assessment.
  - Copies of the notices issued, with their delivery evidence.
- **Failure and repair**:
  - The employer has no registration in the destination state -> BLOCKED with a registration obligation; withholding in an unregistered state creates a liability the workflow refuses to create silently
  - The employee moved weeks before telling anyone -> the effective date is backdated, which forces a retroactive payroll correction child intent and an amended-withholding obligation
  - The destination is a country where the employer has no entity -> REJECT the location change and route to the mobility workflow, which handles employer-of-record and permanent-establishment questions properly
- **Jurisdiction**:
  - US federal: Federal income tax withholding follows the work location, and the employer must obtain a new Form W-4 state equivalent; multi-state work weeks require allocation rather than a single jurisdiction.
  - US state variation: California applies its wage-hour law to work performed in California regardless of where the employer sits; New York, New Jersey, Connecticut, Delaware, Nebraska and Pennsylvania apply convenience-of-the-employer rules that can tax a remote worker in the employer's state as well as the employee's; Washington, Texas and Florida have no income tax but still have their own paid-leave or reporting duties.
  - International: Moving to an EU member state can create a permanent establishment for the employer and shifts social security under Regulation 883/2004; an A1 certificate is required for a posting rather than a relocation.

### WF-PEO-012. Change an emergency contact and its disclosure scope

- **Status**: `EXISTING` - planning/workflows/people/emergency-contact-update.md; planning/workflows/people/catalog.md row ChangeEmergencyContact
- **Archetype**: `A2` | **Complexity tier**: 1
- **Actors**: employee, HR business partner, integration system
- **Trigger**: An employee adds, replaces or removes an emergency contact through self-service.
- **Preconditions**:
  - The employee has an authenticated self-service session.
  - The tenant's policy states who may read an emergency contact.
  - The contact's relationship type vocabulary is closed.
- **Steps**:
  1. `TASK` (employee) the employee enters the contact's name, relationship and contact points
  2. `RULE` (employee) validate the relationship against the closed vocabulary and keep the emergency-contact role separate from dependent and beneficiary roles
  3. `CAPABILITY` (integration system) commit the contact relationship revision with the disclosure scope the policy assigns
  4. `DECISION` (HR business partner) when the same person is already a benefits dependent, do not silently link the two roles; they have different consent and disclosure rules
  5. `END` (employee) close immediately; there is no approval and no external effect
- **Intents**:
  - `hcmnext.people.change_emergency_contact/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: existing emergency contact relationships
  - `privacy`: disclosure scope policy for third-party contact data
- **Writes**:
  - `people`: emergency contact relationship revision
- **Evidence required**:
  - Request digest.
  - Ledger fact recording who changed the contact and when.
  - No document evidence is required, and none should be demanded.
- **Failure and repair**:
  - The employee lists a person who is also a benefits dependent -> the two roles stay separate records; a shared identity must not cause the dependent's benefits data to become visible to whoever may read emergency contacts
  - A manager requests the emergency contact list for their team -> DENY unless the tenant policy grants it; the default is that only HR and a safety responder may read it
  - The contact point fails format validation -> REJECT the field with a correctable error and preserve the rest of the entry
- **Jurisdiction**:
  - US federal: No federal statute requires collecting an emergency contact; because it is third-party personal data collected without that party's consent, it should be minimized rather than enriched.
  - US state variation: State breach-notification statutes generally cover the contact's name plus a contact point only when combined with a protected identifier, so the record should never carry the contact's Social Security number or date of birth.
  - International: Under GDPR the contact is a data subject who was never asked; Article 14 transparency duties apply in principle, which is why the collected field set stays minimal and the retention is tied to the employment.

### WF-PEO-013. Transfer employment to another employer under a business transfer

- **Status**: `NEW`
- **Archetype**: `A14` | **Complexity tier**: 4
- **Actors**: HR business partner, compliance officer, employee, external partner or carrier, finance partner
  - performing a step: HR business partner, compliance officer, external partner or carrier, finance partner
  - participating without owning a step: employee
- **Trigger**: A business, a service contract or an entity moves to another employer and the workers attached to it move with it.
- **Preconditions**:
  - The transfer happens by agreement or by operation of law, and which of the two decides what the workers may be offered.
  - Continuous service, accrued entitlements and existing terms travel with the worker unless the governing rule says otherwise.
  - The transferor's own obligations to the transferring workers do not all end at the transfer.
- **Steps**:
  1. `CAPABILITY` (HR business partner) determine the transferring population from the business or contract being transferred rather than from a list
  2. `RULE` (compliance officer) resolve the governing regime and what it preserves: terms, continuous service, accrued entitlements and collective arrangements
  3. `TASK` (HR business partner) assemble the employee liability information the transferee is entitled to, scoped to the transferring population
  4. `APPROVAL` (compliance officer) compliance approves the scope of what transfers and what the transferor retains
  5. `PARALLEL` (compliance officer) run the information and consultation obligations on both parties against their own deadlines
  6. `CAPABILITY` (external partner or carrier) create the receiving employment records with the preserved service dates and terms
  7. `OBSERVE` (HR business partner) confirm the transferee holds each transferring worker before the transferor's records are closed
  8. `END` (finance partner) close the transferring employments at the transfer date; the transferor's surviving obligations remain open with their own owners
- **Intents**:
  - `hcmnext.people.transfer_employment_to_successor/v1` (PROPOSED, creates)
  - `hcmnext.people.preserve_continuous_service/v1` (PROPOSED, advances)
- **Reads**:
  - `commercial`: the transfer agreement and its data schedule
  - `leave`: accrued entitlements travelling with the worker
  - `people`: the transferring population, their terms and their continuous service
  - `regulatory`: the governing transfer regime and its information and consultation duties
- **Writes**:
  - `people`: the closed transferring employments and the receiving records
  - `regulatory`: the information and consultation obligations and their discharge
- **Evidence required**:
  - The population determined from the business transferred rather than from a list.
  - The employee liability information as provided, scoped to the transferring group.
  - Per-worker confirmation that the transferee holds them before the transferor closes.
  - The transferor's surviving obligations with their post-transfer owners.
- **Failure and repair**:
  - A worker objects to the transfer -> the governing regime decides the effect on their employment; treating an objection as a resignation by default is the common and costly error
  - The transferee proposes a variation shortly after the transfer -> it is assessed against the restrictions the regime places on transfer-connected variations rather than as an ordinary change
  - The transferor's records are closed before the transferee confirms receipt -> workers exist at neither employer; confirmation precedes closure
  - The transferor deletes its records at the transfer -> its own surviving obligations have no records behind them and its retention schedule rather than the transfer governs
- **Jurisdiction**:
  - US federal: No single federal regime; a business transfer is governed by the asset or share agreement, with successor employer rules for wage base continuity and separate obligations under WARN where the transaction produces an employment loss.
  - US state variation: Several states impose successor liability for unpaid wages and for unemployment experience rating, and a few require notice on a change of control.
  - International: The EU Acquired Rights Directive and its national implementations, including TUPE in the United Kingdom, transfer employment automatically with terms and continuous service preserved and restrict transfer-connected variations; information and consultation duties fall on both parties and carry their own penalties.

### WF-PEO-014. Apply a change of employment status that the law makes automatic

- **Status**: `NEW`
- **Archetype**: `A3` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, employee, payroll administrator
- **Trigger**: A statutory threshold is crossed and the employment relationship changes by operation of law rather than by anyone deciding.
- **Preconditions**:
  - The change happens whether or not the employer records it, so the record can only be right or wrong about it.
  - It is not a correction of an error and it is not a decision; treating it as either produces the wrong effective date.
  - Continuous service and accrued rights are unaffected by the change of status itself.
- **Steps**:
  1. `OBSERVE` (compliance officer) detect the threshold crossing from the facts that drive it rather than from a calendar reminder
  2. `RULE` (compliance officer) resolve the statutory basis, its effective date and what it changes
  3. `CAPABILITY` (HR business partner) append the status change at the statutory effective date with its basis cited
  4. `CAPABILITY` (payroll administrator) re-derive the terms, entitlements and payroll treatment that follow from the new status
  5. `DOCUMENT` (employee) notify the worker of the change, its basis and its effect on their terms
  6. `END` (HR business partner) close on the notification; the employment interval is continuous across the change
- **Intents**:
  - `hcmnext.people.apply_statutory_status_change/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: the terms that follow from the status
  - `people`: the employment history and the facts that drive the threshold
  - `regulatory`: the statutory rule, its threshold and its effective date
- **Writes**:
  - `payroll`: the re-derived treatment
  - `people`: the status change with its statutory basis and continuous interval
- **Evidence required**:
  - The facts driving the threshold and the date it was crossed.
  - The statutory basis cited on the change.
  - The continuous employment interval across the change.
  - The notification to the worker with its date.
- **Failure and repair**:
  - The change is recorded as a new hire -> continuous service resets and the accrued rights the rule was written to protect are destroyed
  - The change waits for a decision -> the law has already made it and the record disagrees with the legal position from the threshold date
  - The threshold is crossed and never detected -> the record is wrong from that date and the exposure accrues silently until an audit or a claim finds it
- **Jurisdiction**:
  - US federal: No general federal analogue; the nearest cases are the ACA full-time determination and the common-law employee tests, which change obligations rather than the relationship itself.
  - US state variation: A handful of states convert or restrict repeated fixed-term or temporary arrangements, and several deem a relationship one of employment once conduct-based tests are met.
  - International: Widespread in the EU: Spain converts chained fixed-term contracts to indefinite at cumulative-duration thresholds, and Germany, France, Italy and the Netherlands each convert or restrict successive fixed-term contracts on their own thresholds.

---

## ORG. Organization design, jobs and positions

9 workflows.

### WF-ORG-001. Define a job and publish its profile

- **Status**: `PARTIAL` - planning/workflows/workforce/catalog.md row CreateJob
- **Archetype**: `A1` | **Complexity tier**: 2
- **Actors**: HR business partner, compliance officer, finance partner
- **Trigger**: Compensation or HR defines a new job with a family, level and profile.
- **Preconditions**:
  - The job family and level scheme are published and versioned.
  - A pay band exists or is created in the same proposal.
  - The FLSA exemption analysis inputs are available.
- **Steps**:
  1. `TASK` (HR business partner) author the job profile, duties and required qualifications
  2. `RULE` (compliance officer) run the exemption test against the duties and the proposed salary basis
  3. `DECISION` (compliance officer) route to compliance review when the exemption result is non-exempt but the pay basis is salaried
  4. `APPROVAL` (finance partner) compensation and finance approve the band assignment
  5. `CAPABILITY` (HR business partner) publish the job as an immutable revision with an effective interval
  6. `END` (HR business partner) close; the job is a control artifact, so no worker facts change
- **Intents**:
  - `hcmnext.organization.define_job/v1` (PROPOSED, creates)
  - `hcmnext.rewards.evaluate_pay_band_position/v1` (REAL, reads)
- **Reads**:
  - `compensation`: pay band catalog and its version
  - `organization`: job family, level scheme, existing job profiles
  - `regulatory`: exemption rule pack
- **Writes**:
  - `compensation`: band-to-job binding
  - `organization`: job revision and its published profile
- **Evidence required**:
  - Exemption determination with the duties tested and the rule pack version.
  - Approval certificates for the band assignment.
  - Publication record with the effective interval.
- **Failure and repair**:
  - The exemption result contradicts the intended pay basis -> BLOCKED pending compliance sign-off; a salaried non-exempt job is legal but must be a deliberate decision
  - A job with the same code already exists in an overlapping interval -> REJECT on the uniqueness invariant
  - The band catalog version changes between simulation and publication -> REPLAN so the published binding cites a live band
- **Jurisdiction**:
  - US federal: FLSA 29 CFR 541 sets the duties tests and the salary threshold; a job profile is the employer's primary evidence when an exemption is challenged.
  - US state variation: Colorado requires job postings to carry compensation ranges, which makes the band binding a publication input rather than an internal one; California Labor Code 432.3 requires pay scales on postings for employers with 15 or more employees; Washington and New York have parallel duties.
  - International: EU pay transparency Directive 2023/970 requires job categories to rest on objective, gender-neutral criteria and gives applicants a right to pay information before interview.

### WF-ORG-002. Create a position and reserve it against headcount

- **Status**: `EXISTING` - planning/workflows/workforce/catalog.md rows CreatePosition and ReservePosition; planning/workflows/workforce/headcount-requisition.md
- **Archetype**: `A7` | **Complexity tier**: 3
- **Actors**: hiring manager, HR business partner, finance partner, integration system
  - performing a step: hiring manager, finance partner, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A hiring manager needs an approved slot before a requisition can be opened.
- **Preconditions**:
  - A headcount plan exists for the fiscal period with unconsumed lines.
  - The organization unit and cost centre are resolvable.
  - No headcount freeze is in force for the unit.
- **Steps**:
  1. `CAPABILITY` (finance partner) read the headcount plan and the freeze state for the unit
  2. `DECISION` (finance partner) route to REJECT when a freeze covers the unit and the request carries no exception grant
  3. `TRANSFORM` (hiring manager) build the position proposal with FTE, job, location, and its cost attribution
  4. `APPROVAL` (finance partner) the unit's manager chain approves to the level the amount requires, then finance approves the headcount consumption
  5. `CAPABILITY` (integration system) create the position and take a durable headcount reservation with an expiry
  6. `WAIT` (integration system) hold the reservation until it is consumed by a filled position or it expires
  7. `COMPENSATE` (finance partner) release the reservation on expiry and notify the requester before it lapses
- **Intents**:
  - `hcmnext.organization.create_position/v1` (PROPOSED, creates)
  - `hcmnext.organization.reserve_position/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: headcount plan lines, freezes and exception grants
  - `organization`: unit hierarchy and cost centre mapping
  - `position`: existing positions and their occupancy
- **Writes**:
  - `budget`: headcount consumption record with expiry
  - `position`: position aggregate and its reservation
- **Evidence required**:
  - Reservation identity with amount, expiry, renewal authority and consumption rule.
  - Approval chain with the threshold each approver satisfied.
  - Freeze evaluation, including the exception grant if one was used.
- **Failure and repair**:
  - Two requisitions try to consume the same headcount line -> the second REJECTs on the reservation's double-spend guard; capacity is fenced, not advisory
  - The reservation expires while the requisition is still open -> the requisition is BLOCKED and must obtain a new reservation; a lapsed reservation is never silently renewed
  - A freeze is declared after the reservation is taken -> the reservation survives but new consumption is blocked, and the freeze decision names which reservations it grandfathered
- **Jurisdiction**:
  - US federal: No federal statute governs internal headcount, but WARN Act aggregation counts positions rather than requisitions when a later reduction is planned.
  - US state variation: State-level pay transparency laws attach to the posting, not the position, so a created position carries no disclosure duty until a requisition is published.
  - International: In jurisdictions with works council codetermination, creating positions in bulk as part of a restructure can trigger consultation before any requisition is opened.

### WF-ORG-003. Reorganize a unit and move its workers

- **Status**: `PARTIAL` - planning/workflows/workforce/catalog.md rows MoveOrganization and ReorganizeWorkforce
- **Archetype**: `A8` | **Complexity tier**: 5
- **Actors**: HR business partner, manager, finance partner, IT or access administrator, compliance officer, integration system
  - performing a step: HR business partner, finance partner, IT or access administrator, compliance officer, integration system
  - participating without owning a step: manager
- **Trigger**: A restructure moves an organization unit and every worker under it to a new parent on one effective date.
- **Preconditions**:
  - The full subtree and its worker population are enumerable at the effective date.
  - The destination parent exists and has capacity.
  - A population snapshot can be frozen so the change set does not drift while approvals run.
- **Steps**:
  1. `CAPABILITY` (HR business partner) freeze an immutable population snapshot of the subtree at a watermark
  2. `CAPABILITY` (HR business partner) simulate the graph change, the derived manager edges, the cost centre reattribution and the access recalculation
  3. `RULE` (HR business partner) validate that no cycle is created and that matrix edges are not silently promoted to parent edges
  4. `DECISION` (compliance officer) route to a consultation subworkflow where a jurisdiction requires works council or union notice
  5. `APPROVAL` (finance partner) both the source and destination leadership chains approve, and finance approves the cost reattribution
  6. `WAIT` (integration system) hold to the effective date; the snapshot's staleness is re-evaluated on wake
  7. `CHECKPOINT` (HR business partner) replan for every worker who left, transferred or was terminated since the snapshot
  8. `CAPABILITY` (integration system) commit the graph move and the per-worker assignment revisions as one transaction plan
  9. `PARALLEL` (IT or access administrator) dispatch access recalculation, payroll cost centre changes and manager notifications
  10. `OBSERVE` (integration system) reconcile the observed org graph and the observed entitlement set against the plan
- **Intents**:
  - `hcmnext.organization.reorganize_workforce/v1` (PROPOSED, creates)
  - `hcmnext.access.recalculate_entitlement/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: entitlements derived from unit membership
  - `budget`: cost centre mapping and its owners
  - `organization`: subtree, edges, unit attributes
  - `people`: every assignment in the population snapshot
- **Writes**:
  - `access`: recalculation intents scoped to the effective date
  - `organization`: unit parent revision and unit attribute revisions
  - `people`: assignment organization revisions per worker
- **Evidence required**:
  - The frozen population snapshot with its watermark.
  - Per-worker child results, so a partial failure names exactly who did not move.
  - Consultation evidence where a jurisdiction required it.
- **Failure and repair**:
  - Some workers commit and others fail -> no false rollback; the transaction reports per-worker outcomes and opens a REPAIR_PLAN for the failures
  - A worker in the snapshot was terminated before the effective date -> that child is skipped with a recorded reason, not silently dropped
  - Access recalculation removes an entitlement the worker still needs -> the drift is observed and a bounded repair restores it; the reorganization is not reversed for one entitlement
- **Jurisdiction**:
  - US federal: If the reorganization is a cover for a reduction in force, WARN Act 60-day notice duties attach at 100 or more employees and 50 or more affected at a single site.
  - US state variation: California, New York, New Jersey and Illinois operate mini-WARN statutes with lower thresholds and longer notice periods; New Jersey requires severance as a matter of statute.
  - International: In Germany a Betriebsanderung triggers works council consultation and possibly a Sozialplan before implementation; in France a CSE consultation is mandatory and its absence can void the measure.

### WF-ORG-004. Close a position and account for its occupant

- **Status**: `PARTIAL` - planning/workflows/workforce/catalog.md rows ClosePosition and VacatePosition
- **Archetype**: `A3` | **Complexity tier**: 3
- **Actors**: HR business partner, finance partner, manager, integration system
  - performing a step: HR business partner, finance partner, integration system
  - participating without owning a step: manager
- **Trigger**: A position is being eliminated, whether it is vacant or occupied.
- **Preconditions**:
  - The position's occupancy state at the effective date is known.
  - The headcount line the position consumed is identified.
  - If occupied, a destination or a termination path exists for the occupant.
- **Steps**:
  1. `CAPABILITY` (finance partner) read the position, its occupancy and the reservation or headcount line it consumed
  2. `DECISION` (HR business partner) route a vacant position to simple closure and an occupied position to a redeployment or termination subworkflow first
  3. `APPROVAL` (finance partner) the manager chain and finance approve the headcount release
  4. `CAPABILITY` (integration system) end the position with an effective date and release the headcount line
  5. `OBSERVE` (integration system) confirm the finance system reflects the released headcount
  6. `END` (HR business partner) close, leaving the position historically retrievable rather than deleted
- **Intents**:
  - `hcmnext.organization.close_position/v1` (PROPOSED, creates)
  - `hcmnext.organization.release_position_reservation/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: headcount line and its consumption
  - `people`: the occupant's assignment if any
  - `position`: position, occupancy history, reservation state
- **Writes**:
  - `budget`: headcount release receipt
  - `position`: position end revision
- **Evidence required**:
  - Occupancy state at the effective date.
  - Release receipt distinguishing an unused line from a consumed one.
  - Link to the redeployment or termination intent when the position was occupied.
- **Failure and repair**:
  - The position is occupied and no destination exists -> REJECT; closing an occupied position without resolving the occupant creates an orphaned worker
  - A pending requisition still references the position -> BLOCKED until the requisition is cancelled or retargeted
  - The headcount line was already consumed by a different position -> the release records zero recovered capacity rather than crediting the plan twice
- **Jurisdiction**:
  - US federal: Position elimination is the usual predicate for a WARN-triggering event, so the count of closures in a rolling 90-day window is itself a reportable figure.
  - US state variation: State mini-WARN thresholds count affected employees, not positions, so a closure of vacant positions does not count while a closure of occupied ones does.
  - International: In many EU jurisdictions the redundancy is attached to the post rather than the person, and selection criteria must be objective and documented before the closure takes effect.

### WF-ORG-005. Split one organization unit into two

- **Status**: `PARTIAL` - planning/workflows/workforce/catalog.md row SplitOrganization
- **Archetype**: `A8` | **Complexity tier**: 4
- **Actors**: HR business partner, manager, finance partner, IT or access administrator, integration system
- **Trigger**: A unit grows too large and is divided, with its workers, positions and budget allocated between the successors.
- **Preconditions**:
  - Every worker, position and open requisition under the unit is enumerable.
  - An allocation rule assigns each item to exactly one successor.
  - Both successor units have named managers.
- **Steps**:
  1. `CAPABILITY` (HR business partner) enumerate the unit's workers, positions, requisitions and budget lines
  2. `TRANSFORM` (HR business partner) apply the allocation rule and produce an explicit assignment for every item, with no item unassigned
  3. `DECISION` (manager) route to human resolution for every item the rule could not assign; the split does not proceed with an unassigned remainder
  4. `APPROVAL` (finance partner) both successor managers and finance approve the allocation
  5. `CAPABILITY` (integration system) create the successor units and commit the per-item reassignments
  6. `PARALLEL` (IT or access administrator) recalculate access, reattribute cost centres, and retarget open requisitions
  7. `OBSERVE` (HR business partner) verify the original unit has no residual members before closing it
- **Intents**:
  - `hcmnext.organization.split_organization/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: budget lines attributed to the unit
  - `organization`: unit, subtree and edges
  - `people`: assignments under the unit
  - `position`: positions and open requisitions
- **Writes**:
  - `budget`: split budget attributions
  - `organization`: two successor units and the source unit's end revision
  - `people`: per-worker assignment revisions
- **Evidence required**:
  - The complete item inventory with its allocation decision.
  - Evidence that the residual set is empty.
  - Approval certificates from both successor managers.
- **Failure and repair**:
  - An item is left unassigned -> REJECT before commit; an unassigned worker or budget line is a silent orphan
  - A worker transfers out of the unit during approval -> REPLAN the allocation for that worker only
  - Both successors claim the same position -> REJECT on the allocation invariant with the conflicting item named
- **Jurisdiction**:
  - US federal: No federal duty attaches to a split by itself, but the resulting units may cross the FLSA enterprise-coverage or FMLA 50-employee-within-75-miles thresholds, which changes entitlements.
  - US state variation: State pay-data reporting, such as California Government Code 12999, is filed by establishment, so a split that creates a new establishment changes the reporting shape.
  - International: In codetermined jurisdictions a split can change works council constituency and may require a new election or a transitional mandate.

### WF-ORG-006. Change a position's FTE and reconcile the occupant's terms

- **Status**: `PARTIAL` - planning/workflows/workforce/catalog.md row ChangePositionFTE
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: manager, HR business partner, payroll administrator, benefits administrator, integration system
- **Trigger**: A position moves between full time and part time, changing the occupant's hours.
- **Preconditions**:
  - The position has an occupant whose scheduled hours derive from the FTE.
  - The benefits eligibility rules that key on hours are resolvable.
  - The effective date does not fall inside a locked pay period.
- **Steps**:
  1. `CAPABILITY` (HR business partner) read the position FTE, the occupant's schedule and the derived pay and benefits state
  2. `CAPABILITY` (benefits administrator) simulate the pay change, the benefits eligibility change and the leave accrual change
  3. `DECISION` (benefits administrator) route to a benefits loss-of-eligibility subworkflow when the new hours drop below the plan threshold
  4. `APPROVAL` (manager) manager and HR approve; the employee acknowledges where the change is a material term of employment
  5. `WAIT` (payroll administrator) hold to a pay period boundary
  6. `CAPABILITY` (integration system) commit the FTE revision, the schedule revision and the pay basis revision as bound children
  7. `OBSERVE` (payroll administrator) confirm payroll applied the proration from the correct period
- **Intents**:
  - `hcmnext.organization.change_position_fte/v1` (PROPOSED, creates)
  - `hcmnext.rewards.change_base_pay/v1` (REAL, creates)
  - `hcmnext.benefits.determine_eligibility/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: plan eligibility thresholds
  - `compensation`: pay basis and annualization rule
  - `position`: FTE and its history
  - `time`: current schedule and accrual rules
- **Writes**:
  - `benefits`: eligibility re-evaluation
  - `compensation`: pay basis revision
  - `people`: assignment schedule revision
  - `position`: FTE revision
- **Evidence required**:
  - Simulation showing the pay, benefits and accrual deltas before approval.
  - Employee acknowledgement where the jurisdiction treats hours as a material term.
  - Benefits eligibility result with the measurement method named.
- **Failure and repair**:
  - The reduction drops the occupant below the ACA full-time threshold mid stability period -> the stability period rule governs; coverage is not dropped early and the workflow records the obligation to continue it
  - The employee does not acknowledge in a jurisdiction requiring consent -> BLOCKED; the change is not committed on the employer's word alone
  - Payroll applies the proration a period late -> ConsistencyState DRIFT and a retro adjustment repair
- **Jurisdiction**:
  - US federal: The ACA employer mandate measures full-time status over a measurement period and locks it for a stability period, so an FTE cut does not immediately end coverage; ERISA notice duties attach to any resulting loss.
  - US state variation: California, Oregon, Washington and several cities have predictive-scheduling laws that require advance notice and sometimes premium pay for hours reductions in covered industries; New York's call-in pay rules add further exposure.
  - International: In much of the EU a change to contracted hours is a variation of contract requiring the employee's agreement in writing, not a unilateral employer act.

### WF-ORG-007. Analyze the impact of a proposed org change before proposing it

- **Status**: `PARTIAL` - planning/workflows/workforce/catalog.md row AnalyzeOrgImpact
- **Archetype**: `A9` | **Complexity tier**: 2
- **Actors**: HR business partner, manager, finance partner, AI agent, integration system
  - performing a step: HR business partner, AI agent, integration system
  - participating without owning a step: manager, finance partner
- **Trigger**: Someone asks what a hypothetical structural change would do before any proposal exists.
- **Preconditions**:
  - The hypothetical is expressed as a scenario, not as a proposal.
  - The caller's read authorization covers the population in scope.
  - A pinned watermark is chosen so the answer is reproducible.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the population and pin the graph watermark
  2. `TRANSFORM` (integration system) compute the structural, cost, span-of-control and access deltas the change would produce
  3. `DECISION` (integration system) withhold per-worker compensation detail from a caller who may see the structure but not the pay
  4. `CAPABILITY` (AI agent) return the analysis with its assumptions, its watermark and an explicit statement that it creates nothing
  5. `END` (integration system) complete with a zero-effect receipt
- **Intents**:
  - `hcmnext.analytics.analyze_org_impact/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: aggregate cost, subject to field authorization
  - `organization`: graph at the pinned watermark
  - `people`: assignments in scope
- **Writes**:
- **Evidence required**:
  - Pinned watermark and assumption set.
  - Denied-field record for any figure the caller could not see.
  - Zero-effect receipt.
- **Failure and repair**:
  - The caller may see headcount but not compensation -> compensation aggregates are returned as DENIED by name, not omitted, so the caller knows the answer is partial
  - The population is small enough that an aggregate discloses an individual's pay -> suppress the aggregate under the tenant's small-cell rule and say so
  - The graph watermark advances mid analysis -> the analysis stays pinned and reports its watermark; it does not silently mix two graph versions
- **Jurisdiction**:
  - US federal: None; an analysis creates no employment consequence. If its output is used to select people for a reduction, the selection itself becomes evidence under Title VII and the ADEA.
  - US state variation: State pay-data reporting obligations mean aggregate pay analyses may become discoverable; the tenant's retention class should reflect that.
  - International: Under GDPR Article 22 the analysis must not by itself decide anything about an individual; it is decision support and the record should say so.

### WF-ORG-008. Freeze headcount across a set of units

- **Status**: `PARTIAL` - planning/workflows/workforce/catalog.md row FreezeHeadcount
- **Archetype**: `A19` | **Complexity tier**: 2
- **Actors**: finance partner, HR business partner, hiring manager, integration system
  - performing a step: finance partner, hiring manager, integration system
  - participating without owning a step: HR business partner
- **Trigger**: Finance declares a hiring freeze covering a set of organization units for an interval.
- **Preconditions**:
  - The unit set and the interval are explicit.
  - The exception-grant authority is named.
  - Existing reservations and open requisitions are enumerable.
- **Steps**:
  1. `CAPABILITY` (finance partner) enumerate open requisitions and live reservations in the affected units
  2. `DECISION` (finance partner) decide explicitly whether existing reservations are grandfathered or cancelled; the freeze must not leave this implicit
  3. `APPROVAL` (finance partner) the finance authority approves the freeze and its exception rule
  4. `CAPABILITY` (integration system) publish the freeze as a versioned control artifact with its interval and scope
  5. `SIGNAL` (hiring manager) notify every hiring manager with an affected requisition, naming the disposition of their specific requisition
  6. `END` (finance partner) close; the freeze then acts as a precondition on downstream intents
- **Intents**:
  - `hcmnext.workforce.freeze_headcount/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: headcount plans, reservations and open requisitions
  - `organization`: unit subtree resolution for the freeze scope
- **Writes**:
  - `budget`: freeze control artifact with scope, interval and exception authority
- **Evidence required**:
  - The grandfathering decision, per requisition.
  - Publication record with the version the downstream checks will cite.
  - Notification delivery evidence per hiring manager.
- **Failure and repair**:
  - A requisition is approved between the freeze decision and its publication -> the freeze's effective-at governs; the requisition is caught and its manager is told which rule caught it
  - An exception is granted by someone without the named authority -> REJECT the exception and record the attempt
  - The freeze scope resolves to a subtree that changed after publication -> the freeze pins the subtree at its publication watermark and re-resolution is a new version
- **Jurisdiction**:
  - US federal: A freeze is not a reduction in force, but a freeze that runs into layoffs affects the WARN aggregation window because the affected-employee count is measured over rolling 90-day periods.
  - US state variation: No state statute governs hiring freezes, but state pay-transparency laws still apply to any posting that remains live during the freeze, so stale postings are an exposure.
  - International: In jurisdictions with collective agreements, a freeze can breach a staffing commitment and should be checked against the applicable agreement before publication.

### WF-ORG-009. Maintain the job architecture and its levelling framework

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, finance partner, manager, integration system
- **Trigger**: The level scheme, its families and its criteria are revised, which repositions every job bound to them.
- **Preconditions**:
  - The framework is versioned and every job binds a specific version.
  - A level's criteria are objective and documented, because they drive pay and progression.
  - Repositioning a job repositions the workers in it.
- **Steps**:
  1. `TASK` (HR business partner) author the framework revision with its level criteria and its family definitions
  2. `CAPABILITY` (HR business partner) map every existing job to the new framework and produce the set whose level changes
  3. `CAPABILITY` (finance partner) evaluate the pay and band impact for every worker in a repositioned job
  4. `DECISION` (compliance officer) a downward reposition is a material change requiring its own notice and approval, not a framework side effect
  5. `APPROVAL` (compliance officer) compensation, finance and compliance approve the framework version and its transition plan
  6. `CAPABILITY` (integration system) publish the framework version and bind jobs to it with an effective date
  7. `END` (manager) close with the repositioning obligations open per affected worker
- **Intents**:
  - `hcmnext.organization.publish_job_architecture/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: band bindings and worker positions in band
  - `organization`: jobs, families, current level scheme
  - `people`: workers in each affected job
- **Writes**:
  - `compensation`: band impact per affected worker
  - `organization`: framework version and job-to-level bindings
- **Evidence required**:
  - Level criteria as published, since they are the objective basis a pay transparency regime asks for.
  - The repositioned job set and, for each, the affected workers.
  - Per-worker downward reposition handled as its own change with its own notice.
- **Failure and repair**:
  - A job's level drops and the worker's pay is now above the new band maximum -> the worker is not cut; the over-band position is recorded with a decision on how it is managed, and cutting pay to fit a framework is a separate, notice-bearing act
  - The framework is republished without repositioning jobs -> the jobs stay bound to the old version, which is legitimate, but the divergence is visible rather than silent
  - Level criteria are subjective -> REJECT at publication; a levelling framework that cannot state why a job is at a level cannot support a pay transparency answer
- **Jurisdiction**:
  - US federal: The Equal Pay Act permits pay differences based on a bona fide factor other than sex, and a documented levelling framework is the usual evidence for one; an undocumented framework is not.
  - US state variation: Colorado, California, Washington and New York all require posted ranges tied to a level, so an unpublishable framework becomes a posting problem; Illinois pay equity registration asks for the basis of pay differences.
  - International: The EU Pay Transparency Directive requires pay structures resting on objective, gender-neutral criteria and gives workers the right to know the criteria for progression, which makes the framework a compliance artifact rather than an internal convenience.

---

## WFP. Workforce planning and headcount

5 workflows.

### WF-WFP-001. Build a workforce plan for a fiscal period

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 2 feature workforce_plan_create
- **Archetype**: `A20` | **Complexity tier**: 3
- **Actors**: finance partner, HR business partner, manager, integration system
  - performing a step: finance partner, manager, integration system
  - participating without owning a step: HR business partner
- **Trigger**: Finance and HR build the headcount and cost plan for the next fiscal period.
- **Preconditions**:
  - The current population and its run-rate cost are computable at a pinned watermark.
  - The organization hierarchy for the plan period is settled.
  - Attrition and merit assumptions are declared as named assumptions, not embedded constants.
- **Steps**:
  1. `CAPABILITY` (finance partner) snapshot the current population, its cost and its structure at a pinned watermark
  2. `TRANSFORM` (finance partner) apply the declared assumptions to produce headcount lines per unit per period
  3. `TASK` (manager) unit managers review and adjust their own lines within the envelope they are given
  4. `DECISION` (finance partner) route any unit that exceeds its envelope to an exception approval rather than silently rebalancing
  5. `APPROVAL` (finance partner) the finance authority approves the consolidated plan
  6. `CAPABILITY` (integration system) publish the plan as a versioned artifact that downstream reservations consume
  7. `END` (finance partner) close; the plan is a control artifact and creates no worker facts
- **Intents**:
  - `hcmnext.workforce.create_workforce_plan/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: run-rate cost by unit
  - `organization`: hierarchy for the plan period
  - `people`: current population and its assignments
- **Writes**:
  - `budget`: workforce plan with headcount lines and their cost envelopes
- **Evidence required**:
  - Pinned snapshot watermark and the named assumption set.
  - Per-unit manager input and the exceptions approved.
  - Publication version that reservations will cite.
- **Failure and repair**:
  - A manager submits lines after the consolidation deadline -> the late input is recorded as a plan amendment with its own approval, not folded silently into the approved version
  - The population snapshot is stale by the time the plan publishes -> the plan states its watermark; downstream variance reporting compares against it rather than pretending it was current
  - Two units claim the same shared position -> REJECT on the plan invariant with both claims named
- **Jurisdiction**:
  - US federal: None directly; the plan is internal. Its outputs feed decisions that are governed, and the assumption set becomes evidence if a later reduction is challenged as pretextual.
  - US state variation: State pay-data reporting cycles determine when the population snapshot is most defensible; planning against a snapshot that differs from the filed one invites reconciliation questions.
  - International: Where a works council has information rights over staffing plans, the published plan version is what is shared, which is a reason to keep the assumption set explicit.

### WF-WFP-002. Reconcile plan to actual headcount and explain the variance

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: finance partner, HR business partner, auditor
  - performing a step: finance partner, auditor
  - participating without owning a step: HR business partner
- **Trigger**: At period close, actual headcount and cost are compared against the approved plan.
- **Preconditions**:
  - An approved, versioned plan exists for the period.
  - Actuals are computable at the period-end watermark.
  - The mapping from plan lines to actual positions is defined.
- **Steps**:
  1. `CAPABILITY` (finance partner) read the approved plan version and the actual population at period end
  2. `TRANSFORM` (finance partner) attribute each variance to a cause: unfilled requisition, unplanned hire, attrition, transfer in or out, or compensation drift
  3. `DECISION` (finance partner) flag any variance whose cause cannot be attributed rather than assigning it to a residual bucket
  4. `CAPABILITY` (auditor) produce the variance report with links to the intents that caused each movement
  5. `END` (finance partner) complete with zero writes
- **Intents**:
  - `hcmnext.analytics.reconcile_workforce_plan/v1` (PROPOSED, creates)
  - `hcmnext.intelligence.explain_transaction/v1` (REAL, reads)
- **Reads**:
  - `budget`: approved plan version and its lines
  - `compensation`: actual cost including in-period changes
  - `people`: actual assignments at period end
- **Writes**:
- **Evidence required**:
  - Plan version and actual watermark, both pinned.
  - Per-variance attribution linked to the originating intent id.
  - The unattributed residual, stated rather than hidden.
- **Failure and repair**:
  - A movement cannot be attributed to any intent -> report it as unattributed and open a data-quality finding; a residual bucket that absorbs unknowns destroys the audit trail
  - The plan version was amended mid period -> reconcile against both versions and show which amendment moved which line
  - The caller may see headcount but not cost -> return cost variances as DENIED by name
- **Jurisdiction**:
  - US federal: None directly, though the variance report is often the document that explains a reduction's business rationale in later litigation.
  - US state variation: Nothing state-specific, but reductions concentrated in one establishment change state WARN exposure, so the attribution should carry the work location.
  - International: In the EU, headcount variance by establishment feeds collective-redundancy thresholds, which are counted per establishment rather than per employer.

### WF-WFP-003. Model a workforce scenario without touching authoritative facts

- **Status**: `EXISTING` - internal/domains/scenario package: immutable descriptive scenario revisions
- **Archetype**: `A20` | **Complexity tier**: 3
- **Actors**: finance partner, HR business partner, AI agent, integration system
  - performing a step: finance partner, HR business partner, integration system
  - participating without owning a step: AI agent
- **Trigger**: A planner explores an alternative structure or cost path as a named scenario.
- **Preconditions**:
  - The scenario declares its baseline watermark and its typed assumptions.
  - The scenario lifecycle state permits editing.
  - The caller may read the baseline population.
- **Steps**:
  1. `CAPABILITY` (finance partner) create the scenario revision with its baseline reference and typed assumptions
  2. `TRANSFORM` (integration system) compute the scenario outcome without writing any domain fact
  3. `TASK` (finance partner) the planner iterates, each iteration producing a new immutable scenario revision
  4. `DECISION` (HR business partner) refuse any request to promote a scenario directly into authoritative state; promotion is a separate governed intent
  5. `END` (finance partner) close the scenario in a terminal lifecycle state with its lineage intact
- **Intents**:
  - `hcmnext.planning.model_scenario/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: baseline cost
  - `organization`: baseline graph
  - `people`: baseline population
- **Writes**:
  - `scenario`: scenario revisions and their assumption sets
- **Evidence required**:
  - Baseline watermark and lineage between revisions.
  - Assumption values as typed values, not free text.
  - Explicit statement that no domain fact was written.
- **Failure and repair**:
  - A caller tries to read a scenario figure as if it were authoritative -> the read returns the scenario's DERIVED_PREVIEW classification, which downstream consumers must refuse to treat as fact
  - The baseline moves after the scenario is built -> the scenario keeps its pinned baseline and reports that it is now historical
  - A scenario carries per-person pay for a population the caller cannot see individually -> the scenario is refused at creation rather than produced and then masked
- **Jurisdiction**:
  - US federal: None; a scenario has no employment effect. If it names individuals as candidates for elimination, it becomes discoverable and its retention class should reflect that.
  - US state variation: Nothing state-specific.
  - International: GDPR data minimization argues for pseudonymized scenarios where individual identity is not needed for the model.

### WF-WFP-004. Approve a headcount request against a plan line

- **Status**: `EXISTING` - planning/workflows/workforce/headcount-requisition.md
- **Archetype**: `A7` | **Complexity tier**: 2
- **Actors**: hiring manager, finance partner, HR business partner, integration system
  - performing a step: hiring manager, finance partner, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A manager requests headcount that a plan line must fund.
- **Preconditions**:
  - A published plan exists with an unconsumed line matching the request's unit and job family.
  - The approval thresholds for the requested cost are configured.
  - No freeze covers the unit or an exception grant exists.
- **Steps**:
  1. `CAPABILITY` (finance partner) match the request to a plan line and compute the remaining capacity
  2. `DECISION` (finance partner) route to REJECT when no matching line exists, rather than creating one implicitly
  3. `APPROVAL` (finance partner) the manager chain approves to the threshold, then finance approves the consumption
  4. `CAPABILITY` (integration system) consume the plan line and issue a reservation bound to the request
  5. `SIGNAL` (hiring manager) notify the requester with the reservation's expiry
  6. `END` (finance partner) close with the reservation open as an obligation until it is consumed or released
- **Intents**:
  - `hcmnext.workforce.approve_headcount/v1` (PROPOSED, creates)
  - `hcmnext.work.approve_proposal/v1` (REAL, advances)
- **Reads**:
  - `budget`: plan lines, remaining capacity, freeze state
  - `organization`: requesting unit and its chain
- **Writes**:
  - `budget`: line consumption and reservation
- **Evidence required**:
  - Line match evidence showing which line funded the request.
  - Approval chain with thresholds.
  - Reservation with expiry and release rule.
- **Failure and repair**:
  - Concurrent requests exhaust the line -> the second REJECTs against the remaining capacity computed under the reservation fence, not against a cached figure
  - The requested cost exceeds the line's envelope -> BLOCKED pending an exception approval at the higher threshold
  - The plan is amended mid approval -> REPLAN_AND_REAPPROVE against the amended line
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None directly; the downstream posting inherits state pay-transparency duties.
  - International: None directly.

### WF-WFP-005. Detect and resolve span-of-control violations

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 2
- **Actors**: HR business partner, manager, AI agent, integration system
  - performing a step: HR business partner, manager, integration system
  - participating without owning a step: AI agent
- **Trigger**: A policy sets minimum and maximum direct reports; the system finds managers outside the band.
- **Preconditions**:
  - A span-of-control policy is published with its thresholds and exemptions.
  - The organization graph is readable at a pinned watermark.
  - Exemption grants are recorded rather than assumed.
- **Steps**:
  1. `CAPABILITY` (integration system) evaluate every manager's direct report count at the pinned watermark
  2. `RULE` (integration system) apply the policy thresholds and subtract recorded exemptions
  3. `DECISION` (HR business partner) separate structural violations from transient ones caused by a pending manager change
  4. `TASK` (manager) route each remaining violation to the responsible leader with the specific count and threshold
  5. `END` (HR business partner) close as an analysis; remediation is a separate manager change or reorganization intent
- **Intents**:
  - `hcmnext.analytics.evaluate_span_of_control/v1` (PROPOSED, creates)
- **Reads**:
  - `organization`: graph and manager edges
  - `people`: assignments and their effective intervals
- **Writes**:
- **Evidence required**:
  - Pinned watermark and policy version.
  - Per-violation record naming the manager, the count and the threshold breached.
  - Exemption references applied.
- **Failure and repair**:
  - A pending future-dated manager change would resolve the violation -> report it as transient with the resolving intent named, not as an open violation
  - A manager's reports span organization scopes the caller cannot see -> return the count as DENIED rather than a partial count that reads as complete
  - The policy has no published version -> FAIL_CLOSED; an unversioned threshold cannot produce a defensible finding
- **Jurisdiction**:
  - US federal: None.
  - US state variation: None.
  - International: In codetermined jurisdictions, span thresholds that drive restructures may need consultation before they are enforced.

---

## CMP. Compensation

14 workflows.

### WF-CMP-001. Change a worker's base pay

- **Status**: `EXISTING` - definitions/governance/intent-conformance-descriptors.yaml descriptor hcmnext.rewards.change_base_pay; planning/workflows/rewards/catalog.md row ChangeBasePay; planning/workflows/rewards/compensation-change.md
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: manager, HR business partner, payroll administrator, finance partner, integration system
  - performing a step: HR business partner, payroll administrator, finance partner, integration system
  - participating without owning a step: manager
- **Trigger**: A manager proposes a base pay change for a direct report with an effective date.
- **Preconditions**:
  - The worker has an active compensation package with a base component.
  - A pay band is bound to the worker's job and level at the effective date.
  - A compensation budget exists for the increase amount.
- **Steps**:
  1. `CAPABILITY` (HR business partner) read the current package, the band and the budget authority
  2. `CAPABILITY` (HR business partner) evaluate the proposed amount against the band and compute compa-ratio and range penetration
  3. `CAPABILITY` (payroll administrator) simulate the payroll delta, the annualized cost and any retro exposure
  4. `DECISION` (HR business partner) route above-band or above-threshold proposals to the higher approval tier
  5. `CAPABILITY` (finance partner) reserve the compensation budget against the proposal digest
  6. `APPROVAL` (finance partner) manager, HR business partner and, above threshold, finance approve the bound proposal
  7. `WAIT` (payroll administrator) hold to the effective date
  8. `CHECKPOINT` (HR business partner) revalidate the band, the budget reservation and the worker's active state
  9. `CAPABILITY` (integration system) commit the base component revision and consume the reservation
  10. `OBSERVE` (payroll administrator) confirm payroll applied the new rate from the correct period
- **Intents**:
  - `hcmnext.rewards.change_base_pay/v1` (REAL, creates)
  - `hcmnext.rewards.evaluate_pay_band_position/v1` (REAL, reads)
  - `hcmnext.rewards.simulate_compensation/v1` (REAL, reads)
  - `hcmnext.rewards.reserve_compensation_budget/v1` (REAL, creates)
- **Reads**:
  - `budget`: available authority in the cost centre
  - `compensation`: current package, components, band binding
  - `payroll`: pay group calendar and cutoff
  - `regulatory`: minimum wage floor for the work jurisdiction
- **Writes**:
  - `budget`: reservation and its consumption
  - `compensation`: base component revision with effective-at
- **Evidence required**:
  - Band evaluation with the band version and FX rate if the currencies differ.
  - Reservation identity, amount and consumption record.
  - Approval certificates bound to the proposal digest.
- **Failure and repair**:
  - The new rate falls below the jurisdiction's minimum wage -> REJECT; a minimum wage floor is a hard invariant, not a warning
  - The budget reservation expires before the effective date -> REPLAN with a fresh reservation and reapproval
  - Payroll processes the period before the commit lands -> the change becomes a retro adjustment child intent; the workflow does not pretend the period was correct
- **Jurisdiction**:
  - US federal: FLSA requires the new rate to meet the federal minimum and, for exempt workers, the salary basis threshold in 29 CFR 541.600; a cut below it converts the worker to non-exempt.
  - US state variation: California Labor Code 2810.5 requires written notice of a pay change within seven days; several states, including Missouri, Nevada and North Carolina, require advance notice before a reduction takes effect; California, Washington and Colorado also apply local minimum wage floors above the state figure.
  - International: In the EU a pay reduction is normally a contract variation requiring agreement; the EU Pay Transparency Directive gives the worker a right to know the criteria used for pay progression.

### WF-CMP-002. Run an annual merit cycle across a population

- **Status**: `PARTIAL` - planning/workflows/rewards/catalog.md row RunMeritCycle (archetype A10)
- **Archetype**: `A10` | **Complexity tier**: 5
- **Actors**: HR business partner, manager, finance partner, compliance officer, payroll administrator, integration system
- **Trigger**: The compensation team opens a merit cycle for a frozen population with a budget envelope per unit.
- **Preconditions**:
  - The eligible population is definable by rule and can be frozen at a watermark.
  - Budget envelopes are allocated per unit and are fenced.
  - Performance ratings for the cycle are final or explicitly excluded from the model.
- **Steps**:
  1. `CAPABILITY` (HR business partner) freeze the eligible population snapshot and record the eligibility rule version
  2. `CAPABILITY` (finance partner) allocate budget envelopes per unit and fence them as reservations
  3. `TASK` (manager) managers enter recommendations on worksheets bounded by their envelope
  4. `RULE` (HR business partner) validate each recommendation against band, minimum wage, envelope and the tenant's equity guardrails
  5. `CAPABILITY` (compliance officer) run a pay equity analysis over the proposed outcomes before approval, not after
  6. `DECISION` (compliance officer) route cohorts with an unexplained disparity to review rather than approving the cycle wholesale
  7. `APPROVAL` (finance partner) unit leadership approves its worksheet; finance approves the consolidated envelope consumption
  8. `WAIT` (payroll administrator) hold to the common effective date
  9. `CHECKPOINT` (HR business partner) replan for every worker who left, transferred or changed job since the freeze
  10. `CAPABILITY` (integration system) commit one child base pay change per worker, each independently addressable
  11. `OBSERVE` (HR business partner) reconcile the committed population against the frozen one and report every difference
- **Intents**:
  - `hcmnext.rewards.run_merit_cycle/v1` (PROPOSED, creates)
  - `hcmnext.rewards.change_base_pay/v1` (REAL, creates)
  - `hcmnext.rewards.reserve_compensation_budget/v1` (REAL, creates)
- **Reads**:
  - `budget`: envelope allocations
  - `compensation`: current packages and band positions
  - `people`: frozen population and its assignments
  - `talent`: performance ratings if the model uses them
- **Writes**:
  - `analytics`: pay equity analysis result retained with the cycle
  - `budget`: envelope consumption per unit
  - `compensation`: one base component revision per included worker
- **Evidence required**:
  - The frozen population with its watermark and eligibility rule version.
  - Per-worker recommendation, its approver and its guardrail results.
  - The pre-approval equity analysis, retained under its own classification.
- **Failure and repair**:
  - A worker in the frozen population terminates before the effective date -> that child is skipped with a recorded reason; the parent does not fail
  - Some children commit and others fail -> no false rollback; per-worker outcomes are reported and the failures get a bounded repair
  - A manager's recommendations exceed the envelope -> the worksheet is REJECTED at submission with the overage named, not silently prorated
  - The pre-approval equity analysis reports an unexplained disparity in a cohort and the review does not clear it -> the cycle is BLOCKED for that cohort's worksheets only, with the cohort named and the analysis attached; the remaining worksheets proceed and a blocked worksheet requires either a documented remediation or a REPLAN with corrected recommendations before it can be approved
  - An approver attempts to approve the whole cycle in order to clear a blocked cohort -> REJECT; the block is at the worksheet level and a cycle-level approval cannot absorb it
- **Jurisdiction**:
  - US federal: Title VII, the Equal Pay Act and the ADEA all attach to merit outcomes; running the equity analysis before approval is what makes the cycle defensible rather than a discovery liability.
  - US state variation: California Government Code 12999 pay-data reporting and Illinois Equal Pay Act registration certificates both consume the cycle's outputs; Colorado requires promotional-opportunity notice, which a merit-plus-promotion cycle must satisfy.
  - International: The EU Pay Transparency Directive requires reporting gender pay gaps and a joint pay assessment when an unexplained gap exceeds five percent, which makes the pre-approval analysis a legal input rather than a courtesy.

### WF-CMP-003. Grant a one-time bonus outside a cycle

- **Status**: `PARTIAL` - planning/workflows/rewards/catalog.md row GrantBonus
- **Archetype**: `A1` | **Complexity tier**: 2
- **Actors**: manager, HR business partner, payroll administrator, finance partner, integration system
- **Trigger**: A manager awards a spot bonus for a specific contribution.
- **Preconditions**:
  - A bonus plan or discretionary authority covers the award type.
  - Budget authority exists in the awarding cost centre.
  - The payment period and its tax treatment are resolvable.
- **Steps**:
  1. `CAPABILITY` (HR business partner) read the plan eligibility and the available discretionary budget
  2. `TRANSFORM` (manager) build the award proposal with amount, currency, award period and reason
  3. `APPROVAL` (finance partner) the manager chain approves to the threshold and finance approves the budget consumption
  4. `CAPABILITY` (integration system) commit the one-time earning and bind it to a specific pay period
  5. `OBSERVE` (payroll administrator) confirm payroll paid it in the intended period with the intended supplemental tax treatment
  6. `END` (payroll administrator) close once the payment is observed, not when it is dispatched
- **Intents**:
  - `hcmnext.rewards.grant_bonus/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: discretionary budget balance
  - `compensation`: plan eligibility and prior awards in the period
  - `payroll`: period calendar and supplemental withholding rule
- **Writes**:
  - `compensation`: one-time earning award
  - `payroll`: earning line bound to a pay period
- **Evidence required**:
  - Award reason and approval chain.
  - Budget consumption record.
  - Payment observation with the tax treatment applied.
- **Failure and repair**:
  - The award pushes the worker over a plan cap for the period -> REJECT with the cap and the year-to-date figure named
  - Payroll pays it in the wrong period -> ConsistencyState DRIFT; the correction is a payroll adjustment, not a re-issue of the award
  - The manager awards to their own record -> FAIL_CLOSED on separation of duties
- **Jurisdiction**:
  - US federal: A non-discretionary bonus must be included in the FLSA regular rate for overtime, which retroactively changes overtime owed in the bonus period; a truly discretionary bonus is excluded, and the distinction is a documented determination, not a label.
  - US state variation: California requires percentage-of-earnings bonuses to be allocated across the earning period for overtime recalculation, which differs from the federal weighted-average method.
  - International: In several EU jurisdictions a repeated discretionary bonus can become a contractual entitlement through custom and practice, so the plan's discretionary language matters.

### WF-CMP-004. Analyze pay equity across a protected cohort

- **Status**: `PARTIAL` - planning/workflows/rewards/catalog.md row AnalyzePayEquity
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: compliance officer, HR business partner, auditor, AI agent, integration system
  - performing a step: compliance officer, auditor, integration system
  - participating without owning a step: HR business partner, AI agent
- **Trigger**: Compliance runs a pay equity analysis over a defined cohort and control set.
- **Preconditions**:
  - The cohort definition and the control variables are declared before the run.
  - A small-cell suppression rule is configured.
  - The caller's purpose permits processing protected characteristics.
- **Steps**:
  1. `CAPABILITY` (compliance officer) resolve the cohort and verify the purpose permits the protected attributes involved
  2. `TRANSFORM` (integration system) run the model with the declared controls and produce residual gaps with confidence intervals
  3. `DECISION` (compliance officer) suppress any cell below the small-cell threshold and say that it was suppressed
  4. `CAPABILITY` (compliance officer) return the result under legal privilege classification where the tenant has designated it so
  5. `END` (auditor) complete with zero writes and a retained analysis artifact
- **Intents**:
  - `hcmnext.analytics.analyze_pay_equity/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: pay for the cohort
  - `people`: protected characteristics under a permitting purpose
  - `talent`: performance and tenure controls
- **Writes**:
  - `analytics`: analysis result with its model version and control set
- **Evidence required**:
  - Declared cohort, controls and model version, fixed before the run.
  - Suppression record for every small cell.
  - Purpose and authorization evidence for the protected attributes.
- **Failure and repair**:
  - The caller lacks a purpose covering protected attributes -> DENY before any computation; the refusal must not reveal the cohort size
  - A control variable is itself correlated with the protected characteristic -> the result carries the warning; the system does not present a controlled gap as a causal finding
  - A cell falls below the suppression threshold -> suppress and disclose the suppression rather than returning a figure that identifies an individual
- **Jurisdiction**:
  - US federal: The Equal Pay Act, Title VII and the ADEA all bear on the result; conducting the analysis under attorney-client privilege is a common and legitimate design, which is why the classification is a first-class field.
  - US state variation: California Government Code 12999 requires annual pay data reporting by establishment, job category, race, ethnicity and sex; Illinois requires an equal pay registration certificate; Massachusetts gives a safe harbour for employers who conduct a self-evaluation.
  - International: The EU Pay Transparency Directive mandates gender pay gap reporting and a joint pay assessment with worker representatives when an unexplained gap exceeds five percent and is not remedied within six months.

### WF-CMP-005. Reserve and later release compensation budget

- **Status**: `EXISTING` - definitions/governance/intent-conformance-descriptors.yaml descriptors hcmnext.rewards.reserve_compensation_budget and release_compensation_budget; internal/domains/budget package
- **Archetype**: `A7` | **Complexity tier**: 2
- **Actors**: finance partner, HR business partner, integration system
  - performing a step: finance partner, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A pending compensation proposal fences budget so a second proposal cannot spend it.
- **Preconditions**:
  - A budget authority observation exists with a freshness stamp.
  - The reservation currency and the budget currency are the same or an FX rate is pinned.
  - The proposal digest the reservation binds is immutable.
- **Steps**:
  1. `CAPABILITY` (finance partner) read the budget authority observation and its freshness
  2. `DECISION` (finance partner) refuse when the observation is older than the freshness policy rather than reserving against a stale figure
  3. `CAPABILITY` (integration system) create the reservation bound to the proposal digest with an amount, an expiry and a release rule
  4. `WAIT` (integration system) hold until consumption, cancellation or expiry
  5. `COMPENSATE` (finance partner) release the reservation and record whether it was unused, partly consumed or fully consumed
- **Intents**:
  - `hcmnext.rewards.reserve_compensation_budget/v1` (REAL, creates)
  - `hcmnext.rewards.release_compensation_budget/v1` (REAL, creates)
- **Reads**:
  - `budget`: authority observation, existing reservations, available quantity
- **Writes**:
  - `budget`: reservation record and its release receipt
- **Evidence required**:
  - Reservation identity with amount, currency, FX rate, expiry and the digest it binds.
  - Freshness stamp of the authority observation used.
  - Release receipt distinguishing unused from consumed.
- **Failure and repair**:
  - Two reservations race for the last available amount -> the second REJECTs; the guard is the reservation fence, not a read-then-write check
  - The release is requested for an already-consumed reservation -> REJECT and route to a repair, because the money has moved
  - The authority observation is stale -> FAIL_CLOSED; an observation is evidence of what an incumbent system reported, and it never grants spending authority by itself
- **Jurisdiction**:
  - US federal: None; budget is internal control.
  - US state variation: None.
  - International: None, though currency controls in some jurisdictions constrain when a reservation can be held in a foreign currency.

### WF-CMP-006. Correct a compensation error discovered after payroll

- **Status**: `PARTIAL` - planning/workflows/rewards/catalog.md rows CorrectCompensation and RetroactivelyAdjustCompensation
- **Archetype**: `A5` | **Complexity tier**: 4
- **Actors**: payroll administrator, HR business partner, employee, finance partner, integration system
  - performing a step: payroll administrator, HR business partner, finance partner, integration system
  - participating without owning a step: employee
- **Trigger**: A worker was paid at the wrong rate for several closed periods.
- **Preconditions**:
  - The erroneous interval and the correct rate are both established.
  - The affected pay periods and their filings are enumerable.
  - The direction of the correction, underpayment or overpayment, is known.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) reconstruct what was paid and what should have been paid across the interval
  2. `TRANSFORM` (payroll administrator) build the corrective compensation revision and the retro payroll delta per period
  3. `DECISION` (HR business partner) route an overpayment to the recovery path, which has consent and notice requirements a correction does not
  4. `APPROVAL` (finance partner) HR and payroll approve; finance approves the cost
  5. `CAPABILITY` (integration system) commit the corrective revision and the bound retro adjustment
  6. `OBSERVE` (payroll administrator) confirm the provider applied the retro and that year-to-date accumulators moved
  7. `END` (payroll administrator) close BusinessState while any amended filing obligation stays PENDING
- **Intents**:
  - `hcmnext.rewards.correct_compensation/v1` (PROPOSED, creates)
  - `hcmnext.payroll.adjust_retroactive/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: the erroneous component revisions and their provenance
  - `payroll`: closed periods, statements issued, accumulators
  - `regulatory`: filings already submitted
- **Writes**:
  - `compensation`: corrective component revision
  - `payroll`: retro adjustment lines per period
  - `regulatory`: amended filing obligation
- **Evidence required**:
  - Period-by-period computation of the delta.
  - Overpayment recovery consent where the jurisdiction requires it.
  - Amended filing obligation with its deadline.
- **Failure and repair**:
  - The worker disputes the overpayment -> the recovery is BLOCKED pending resolution; the employer does not deduct unilaterally where consent is required
  - The correction crosses a tax year boundary -> the prior year is corrected by an amended return, not by adjusting the current year's accumulators
  - The provider applies the retro to the wrong periods -> ConsistencyState DRIFT and a bounded repair per period
- **Jurisdiction**:
  - US federal: FLSA back wages are due for underpayments with potential liquidated damages; IRS rules require Form W-2c for a prior year and permit current-year adjustment only within the same year.
  - US state variation: California Labor Code 221 forbids most deductions to recover overpayments without written authorization, and 203 waiting-time penalties can attach if a final pay was short; New York requires a specific notice and a capped recovery schedule under 12 NYCRR 195.
  - International: In the UK, recovery of an overpayment is permitted by statute but a long-standing overpayment can raise an estoppel defence; several EU states require worker agreement.

### WF-CMP-007. Grant equity and observe the provider

- **Status**: `PARTIAL` - planning/workflows/rewards/catalog.md row GrantEquity
- **Archetype**: `A14` | **Complexity tier**: 4
- **Actors**: HR business partner, finance partner, compliance officer, external partner or carrier, integration system
  - performing a step: finance partner, compliance officer, external partner or carrier, integration system
  - participating without owning a step: HR business partner
- **Trigger**: An equity grant is awarded and must be created at the external equity administrator.
- **Preconditions**:
  - A grant plan, its pool and its available shares are readable.
  - The worker's country of residence and its equity restrictions are resolved.
  - The equity administrator connector is healthy.
- **Steps**:
  1. `CAPABILITY` (compliance officer) read the plan, the pool balance and the country restriction set
  2. `DECISION` (compliance officer) refuse or restructure the grant where the country prohibits the instrument, rather than granting and hoping
  3. `APPROVAL` (finance partner) the compensation committee delegate approves and finance approves the dilution impact
  4. `CAPABILITY` (integration system) commit the grant proposal locally with its vesting schedule
  5. `CAPABILITY` (external partner or carrier) dispatch the grant creation to the equity administrator with an idempotency key
  6. `OBSERVE` (integration system) obtain the administrator's grant identifier and reconcile the share count and grant date
  7. `END` (finance partner) close ConsistencyState only on the administrator's confirmation
- **Intents**:
  - `hcmnext.rewards.grant_equity/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: plan, pool balance, prior grants
  - `people`: country of residence and tax residency
  - `regulatory`: securities and tax restrictions per country
- **Writes**:
  - `compensation`: grant record with vesting schedule and provider reference
  - `integration`: connector operation and its observation
- **Evidence required**:
  - Country restriction evaluation with its rule version.
  - Approval from the delegated committee authority.
  - Provider grant identifier and the reconciled share count.
- **Failure and repair**:
  - The administrator creates the grant but returns no identifier -> ConsistencyState UNKNOWN; the workflow does not retry blindly and opens an investigation, because a duplicate grant is a securities problem
  - The grant date at the administrator differs from the approved one -> DRIFT with a repair; the grant date drives valuation and tax
  - The pool has insufficient shares -> REJECT at simulation; the pool is a fenced resource
- **Jurisdiction**:
  - US federal: Section 409A and, for incentive stock options, IRC 422 constrain the grant date, exercise price and holding periods; a mis-dated grant creates immediate tax exposure for the employee.
  - US state variation: State securities exemptions vary, and California Corporations Code 25102(o) has its own conditions for employee plans.
  - International: In France, qualified free-share plans have statutory holding periods; in India and China, exchange control rules constrain remittance of proceeds, so the grant may be restructured to cash-settled.

### WF-CMP-008. Change pay frequency and reconcile the transition period

- **Status**: `PARTIAL` - planning/workflows/rewards/catalog.md row ChangePayFrequency
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: payroll administrator, HR business partner, employee, compliance officer, integration system
  - performing a step: payroll administrator, HR business partner, compliance officer, integration system
  - participating without owning a step: employee
- **Trigger**: A population moves from semi-monthly to biweekly pay, or the reverse.
- **Preconditions**:
  - The current and target period calendars are both published.
  - The transition period and any bridging payment are computed.
  - Notice requirements for the jurisdiction are resolved.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) compute the transition calendar and the per-worker cash-flow gap
  2. `RULE` (compliance officer) evaluate the jurisdiction's pay frequency minimum and its notice period
  3. `DECISION` (payroll administrator) route to a bridging payment where the transition creates a gap longer than the jurisdiction permits
  4. `APPROVAL` (HR business partner) payroll, HR and finance approve the transition plan and the bridging cost
  5. `SIGNAL` (compliance officer) issue the required advance notice to every affected worker and record delivery
  6. `WAIT` (compliance officer) hold for the statutory notice period before the first changed period
  7. `CAPABILITY` (integration system) commit the pay frequency revision and the pay group change
  8. `OBSERVE` (payroll administrator) confirm the first period under the new calendar paid the expected amounts
- **Intents**:
  - `hcmnext.rewards.change_pay_frequency/v1` (PROPOSED, creates)
  - `hcmnext.payroll.change_pay_group/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: pay basis and annualization
  - `payroll`: current and target period calendars, accumulators
  - `regulatory`: pay frequency and notice rule pack
- **Writes**:
  - `compensation`: pay frequency revision
  - `payroll`: pay group assignment and bridging payment
- **Evidence required**:
  - Transition calendar with the per-worker gap computed.
  - Notice delivery evidence per worker.
  - Bridging payment records where issued.
- **Failure and repair**:
  - A worker's gap exceeds the statutory maximum interval between paydays -> BLOCKED until a bridging payment is included; the transition is not a defence to a late-payment claim
  - Notice is not delivered to some workers -> the change is BLOCKED for those workers only, and the population splits rather than proceeding wholesale
  - Accumulators do not map cleanly across the calendar change -> DRIFT and a reconciliation repair before the next filing
- **Jurisdiction**:
  - US federal: FLSA has no pay frequency rule, so this is entirely state law; the federal constraint is only that overtime is computed on the workweek regardless of the pay period.
  - US state variation: California Labor Code 204 requires at least semi-monthly payment on designated paydays; New York Labor Law 191 requires weekly payment for manual workers, which makes a biweekly transition unlawful for that class; Connecticut, Massachusetts, Vermont and others impose weekly or biweekly floors with waiver procedures.
  - International: In most of the EU, monthly pay is standard and a frequency change is a contract variation requiring notice or agreement.

### WF-CMP-009. Explain how a worker's pay was computed

- **Status**: `EXISTING` - planning/workflows/rewards/catalog.md row ExplainCompensation; internal/domains/compensation read projection with DISCLOSURE FULL, PARTIAL and WITHHELD
- **Archetype**: `A6` | **Complexity tier**: 2
- **Actors**: employee, manager, HR business partner, auditor, integration system
  - performing a step: integration system
  - participating without owning a step: employee, manager, HR business partner, auditor
- **Trigger**: A worker or an approver asks why their pay is what it is.
- **Preconditions**:
  - The caller's field-level authorization is resolvable.
  - The effective date of the explanation is supplied.
  - The rule and band versions that produced the figure are retained.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve field-level authorization; a manager may see base pay but not a garnishment
  2. `TRANSFORM` (integration system) assemble the component chain, the band position, and the rule versions that produced each figure
  3. `DECISION` (integration system) mark every unauthorized field DENIED by name and return WITHHELD for a subject the caller may not know exists
  4. `END` (integration system) complete with zero writes and a zero-effect receipt
- **Intents**:
  - `hcmnext.rewards.explain_compensation/v1` (PROPOSED, creates)
  - `hcmnext.rewards.evaluate_pay_band_position/v1` (REAL, reads)
- **Reads**:
  - `compensation`: components, their revisions and provenance
  - `payroll`: the pay statement lines that consumed them, subject to authorization
  - `privacy`: field classification
- **Writes**:
- **Evidence required**:
  - Field-level disclosure result, including the denied set by name.
  - Rule and band versions pinned to the answer.
  - Zero-effect receipt.
- **Failure and repair**:
  - A manager requests a report's garnishment detail -> DENIED by name, so the manager learns a deduction class exists but not its content, which is what the closed field vocabulary in the compensation package already enforces
  - The band version that produced the figure has been retired -> the answer cites the retired version rather than re-evaluating against the current one
  - The caller may not know the subject exists -> WITHHELD, with no distinction between an unauthorized subject and a nonexistent one
- **Jurisdiction**:
  - US federal: The FLSA record-keeping rules make the computation reproducible a legal requirement, not a convenience.
  - US state variation: California Labor Code 226 requires an itemized wage statement with nine specific elements and a three-year retention; 226(b) gives an inspection right that this workflow serves.
  - International: The EU Pay Transparency Directive gives workers a right to information on the criteria used to determine pay levels and progression, which this explanation is the natural vehicle for.

### WF-CMP-010. Adjust an allowance and evaluate its taxability

- **Status**: `PARTIAL` - planning/workflows/rewards/catalog.md rows AdjustAllowance and RemoveAllowance
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: HR business partner, payroll administrator, employee, integration system
  - performing a step: HR business partner, payroll administrator, integration system
  - participating without owning a step: employee
- **Trigger**: A recurring allowance such as a car, housing or remote-work stipend is changed or ended.
- **Preconditions**:
  - The allowance component and its plan rules are readable.
  - The taxability determination for the allowance type and jurisdiction is available.
  - The notice or contract requirements for reducing it are known.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) read the allowance component, its plan and its current taxability treatment
  2. `RULE` (payroll administrator) re-evaluate taxability for the new amount and the current jurisdiction
  3. `DECISION` (HR business partner) route a reduction or removal through the notice path that a pay reduction requires
  4. `APPROVAL` (payroll administrator) manager and HR approve; payroll approves the tax treatment
  5. `CAPABILITY` (integration system) commit the allowance revision with its effective date
  6. `OBSERVE` (payroll administrator) confirm payroll applied the new amount and the correct taxable or non-taxable split
- **Intents**:
  - `hcmnext.rewards.adjust_allowance/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: allowance component and plan
  - `people`: work location driving the applicable rules
  - `regulatory`: taxability rules for the allowance type
- **Writes**:
  - `compensation`: allowance component revision
  - `payroll`: earning line with its taxability flag
- **Evidence required**:
  - Taxability determination with its rule version.
  - Notice evidence where the change is a reduction.
  - Payroll observation confirming the taxable split.
- **Failure and repair**:
  - The allowance was treated as non-taxable and the new determination says otherwise -> the change is committed and a separate correction intent handles the prior periods; the two are not merged
  - Removal happens without the required notice -> BLOCKED until notice is issued and its period elapses
  - The employee has already incurred the expense the allowance covers -> the effective date is moved forward or a final payment is included; the workflow surfaces this rather than cutting mid month
- **Jurisdiction**:
  - US federal: An accountable plan under IRC 62(c) keeps a reimbursement non-taxable only if it meets substantiation and return-of-excess rules; a flat stipend usually fails them and is wages.
  - US state variation: California Labor Code 2802 requires reimbursement of necessary business expenses, including a reasonable share of home internet and phone for remote workers, so removing a remote stipend can create a reimbursement obligation instead; Illinois has a parallel statute.
  - International: Several EU states have fixed non-taxable home-working allowances; exceeding the statutory amount makes the excess taxable rather than the whole allowance.

### WF-CMP-011. Simulate a compensation change without creating it

- **Status**: `EXISTING` - definitions/governance/intent-conformance-descriptors.yaml descriptor hcmnext.rewards.simulate_compensation; internal/domains/rewards package
- **Archetype**: `A9` | **Complexity tier**: 1
- **Actors**: manager
- **Trigger**: A manager wants to see the effect of a hypothetical pay change before proposing it.
- **Preconditions**:
  - The caller may read the worker's compensation.
  - A band version and an FX rate, if needed, are pinned.
  - The simulation is version-pinned so the same inputs give the same answer.
- **Steps**:
  1. `CAPABILITY` (manager) read the current package under the caller's field authorization
  2. `TRANSFORM` (manager) compute the proposed package, the band position, the annualized delta and the budget impact
  3. `DECISION` (manager) return DERIVED_PREVIEW classification so no consumer mistakes the result for state
  4. `END` (manager) complete with zero writes, zero reservations and a zero-effect receipt
- **Intents**:
  - `hcmnext.rewards.simulate_compensation/v1` (REAL, creates)
  - `hcmnext.rewards.evaluate_pay_band_position/v1` (REAL, reads)
- **Reads**:
  - `budget`: available authority as an observation only
  - `compensation`: current package and band
- **Writes**:
- **Evidence required**:
  - Pinned band and FX versions.
  - Zero-effect receipt proving no reservation was taken.
  - Input digest so the same simulation is reproducible.
- **Failure and repair**:
  - A caller expects the simulation to hold budget -> it does not; taking a reservation is a separate CHANGE_REQUEST, and the receipt proves the simulation held nothing
  - The band version changes between two simulations -> the two answers differ legitimately; each cites its version
  - The caller may not see the current package -> DENY before computing, so the simulation cannot be used to infer a hidden salary
- **Jurisdiction**:
  - US federal: None; a simulation has no employment effect.
  - US state variation: None.
  - International: None, though under GDPR the simulation is still processing and must sit under a declared purpose.

### WF-CMP-012. Administer a sales commission plan through a period

- **Status**: `NEW`
- **Archetype**: `A10` | **Complexity tier**: 4
- **Actors**: payroll administrator, manager, finance partner, employee
- **Trigger**: A commissioned worker's earnings are computed from a plan against attributed transactions.
- **Preconditions**:
  - The plan document, its rates, its accelerators and its clawback terms are versioned and signed.
  - The transaction source is the system of record for bookings, not a spreadsheet.
  - The dispute window and its process are defined before the period closes.
- **Steps**:
  1. `CAPABILITY` (finance partner) attribute transactions to the worker under the plan's crediting rules
  2. `TRANSFORM` (payroll administrator) compute attainment, apply rates and accelerators, and produce the earning with its calculation trace
  3. `SIGNAL` (employee) publish the statement to the worker with the underlying transactions before payment
  4. `WAIT` (manager) hold the dispute window open
  5. `TASK` (manager) resolve disputes per transaction rather than per statement
  6. `APPROVAL` (finance partner) finance approves the period total
  7. `CAPABILITY` (payroll administrator) pay the commission and record any draw recovery or clawback obligation
  8. `END` (finance partner) close the period while clawback obligations for reversed transactions remain open
- **Intents**:
  - `hcmnext.rewards.administer_commission_plan/v1` (PROPOSED, creates)
  - `hcmnext.rewards.grant_commission/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: plan version, quota, prior attainment, draw balance
  - `crm`: attributed transactions and their status
  - `payroll`: prior commission payments and recoveries
- **Writes**:
  - `commercial`: clawback obligations on reversed transactions
  - `compensation`: commission earning with its calculation trace
  - `payroll`: commission lines and any draw recovery
- **Evidence required**:
  - Calculation trace from transaction to payment, so a dispute can be resolved on evidence.
  - The statement as published to the worker, with its dispute window.
  - Clawback obligations with their trigger and their recovery terms.
- **Failure and repair**:
  - A transaction is reversed after the commission was paid -> the clawback obligation activates, but recovery follows the jurisdiction's deduction rules rather than being netted from the next payment automatically
  - The plan is amended mid period -> the prior period keeps its version and the amendment applies prospectively; a retroactive plan change to reduce earned commission is unlawful in several states
  - A commission is non-discretionary and the worker also works overtime -> the commission must be included in the FLSA regular rate for the period it was earned, which retroactively changes overtime owed
- **Jurisdiction**:
  - US federal: A non-discretionary commission is included in the FLSA regular rate and requires recomputation of overtime for the earning period; the retail and service establishment exemption in 29 USC 207(i) is narrow and frequently misapplied.
  - US state variation: California Labor Code 2751 requires a signed written commission agreement and a copy to the employee; several states, including Illinois, Massachusetts, New York and Maryland, treat earned commission as wages whose forfeiture on termination is void, and treble damages are available in Massachusetts.
  - International: In the EU, commission earned during statutory annual leave must be included in holiday pay following the Lock v British Gas line of cases, which changes the holiday pay calculation for every commissioned worker.

### WF-CMP-013. Refresh pay bands from market data

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: finance partner, HR business partner, compliance officer, external partner or carrier, integration system
- **Trigger**: Market survey data is ingested and the pay band catalog is repriced for the next cycle.
- **Preconditions**:
  - The survey source, its effective date and its licence terms are recorded.
  - The job-to-survey-benchmark mapping is explicit and reviewable.
  - The impact of the new bands on current workers is computed before publication.
- **Steps**:
  1. `CAPABILITY` (external partner or carrier) ingest the survey data with its source, effective date and licence terms
  2. `TASK` (HR business partner) map each internal job to its survey benchmark and record the match quality
  3. `TRANSFORM` (finance partner) compute proposed band minimums, midpoints and maximums by geography
  4. `CAPABILITY` (HR business partner) evaluate every current worker against the proposed bands and produce the below-minimum population
  5. `DECISION` (compliance officer) a worker falling below a new band minimum is a compensation obligation with a deadline, not an informational finding
  6. `APPROVAL` (finance partner) finance and compensation approve the new band version and the remediation budget
  7. `CAPABILITY` (integration system) publish the band version with its effective date
  8. `END` (HR business partner) close with the below-minimum remediation as an open obligation
- **Intents**:
  - `hcmnext.rewards.refresh_pay_bands/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: current bands, worker positions in band, job-to-benchmark mapping
  - `organization`: job catalog and levels
  - `people`: worker locations for geographic differentials
- **Writes**:
  - `budget`: remediation cost estimate
  - `compensation`: new band version and the below-minimum population
- **Evidence required**:
  - Survey source, its effective date and the licence terms that constrain redistribution.
  - Per-job benchmark match with its quality rating.
  - The below-minimum population with its remediation cost and deadline.
- **Failure and repair**:
  - A job has no credible survey match -> the band is set by internal alignment and the absence of a market match is recorded, rather than a weak match being presented as market data
  - Publishing the new bands puts workers below the minimum -> the remediation obligation opens with a deadline; publishing a band a current worker sits below and doing nothing is the pattern that produces pay equity claims
  - Survey data is redistributed beyond the licence terms -> REJECT at export; survey licences commonly forbid disclosing participant-level data, and a pay transparency posting built from it can breach them
- **Jurisdiction**:
  - US federal: Survey data participation and use are contractual rather than statutory, but antitrust law limits information exchange between competitors on compensation, and the Department of Justice has treated wage-fixing and no-poach agreements as criminal.
  - US state variation: State pay transparency laws consume the band output directly: a posted range must be a good-faith range, so a band that nobody is paid within is itself evidence of bad faith. Colorado, California, Washington and New York all reach this.
  - International: The EU Pay Transparency Directive requires pay structures to rest on objective, gender-neutral criteria, which makes the benchmark mapping and its match quality part of the compliance record rather than an internal working note.

### WF-CMP-014. Track restrictive covenants against the jurisdictions that void them

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: compliance officer, HR business partner, manager, employee
  - performing a step: compliance officer, HR business partner, employee
  - participating without owning a step: manager
- **Trigger**: Non-compete, non-solicit and confidentiality terms must be tracked per worker and per jurisdiction.
- **Preconditions**:
  - Each covenant is bound to the agreement version and the worker's work location when signed.
  - The enforceability rules are jurisdiction-scoped and change frequently.
  - A covenant that becomes void must be identifiable, and the worker may need to be told.
- **Steps**:
  1. `CAPABILITY` (HR business partner) record each covenant with its agreement version, its scope, its duration and the work location at signature
  2. `RULE` (compliance officer) evaluate enforceability against the current rule pack for the worker's jurisdiction and compensation level
  3. `DECISION` (compliance officer) flag a covenant that is void or unenforceable in the worker's jurisdiction rather than leaving it nominally in force
  4. `SIGNAL` (employee) issue the notice the jurisdiction requires where a void clause must be disclosed to the worker
  5. `WAIT` (compliance officer) hold for the covenant's duration, re-evaluating when the worker relocates or the rule pack changes
  6. `END` (compliance officer) close on expiry with the enforceability history retained
- **Intents**:
  - `hcmnext.people.track_restrictive_covenant/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: compensation level where the jurisdiction sets a threshold
  - `documents`: the signed agreement and its version
  - `people`: work location at signature and at each re-evaluation
  - `regulatory`: enforceability rule pack per jurisdiction
- **Writes**:
  - `documents`: any required notice and its delivery evidence
  - `people`: covenant records and their enforceability determinations
- **Evidence required**:
  - The agreement version each covenant binds to.
  - Enforceability determination per jurisdiction with its rule version and the date evaluated.
  - Notice delivery where a void clause must be disclosed.
- **Failure and repair**:
  - A worker relocates to a jurisdiction where the covenant is void -> the determination changes and the covenant is marked unenforceable in the new location while remaining historically recorded; enforcing it anyway is the exposure
  - A covenant is enforced against a worker in a jurisdiction that voids it -> FAIL_CLOSED at the enforcement step, because attempting to enforce a void non-compete is itself actionable in several states
  - The rule pack changes and a previously enforceable covenant becomes void -> every affected covenant is re-evaluated and the notice obligation, where one exists, opens with its deadline
- **Jurisdiction**:
  - US federal: The Federal Trade Commission's attempt at a national non-compete ban was set aside in litigation, so enforceability remains state law; the National Labor Relations Board has separately treated overbroad non-competes for non-supervisory employees as an unfair labor practice.
  - US state variation: California voids non-competes almost entirely under Business and Professions Code 16600 and 16600.1 and requires notice to current and former employees whose agreements contain a void clause; Minnesota, Oklahoma and North Dakota also void them broadly; Colorado, Illinois, Washington, Oregon, Maine, Maryland, Virginia, Nevada and others impose compensation thresholds, notice periods or garden-leave requirements that differ in every case.
  - International: In the UK a restraint is enforceable only so far as it protects a legitimate interest and goes no wider than necessary; several EU states require the employer to pay compensation for the restraint period, which makes an unpaid covenant simply void.

---

## PAY. Payroll

11 workflows.

### WF-PAY-001. Run a payroll cycle from calculation to settlement

- **Status**: `EXISTING` - internal/workflow/conformance/payroll/definition.go workflow hcmnext.workflows.payroll_run_calculation_release_settlement; planning/workflows/payroll/payroll-correction-retro.md
- **Archetype**: `A10` | **Complexity tier**: 5
- **Actors**: payroll administrator, finance partner, compliance officer, integration system, external partner or carrier
  - performing a step: payroll administrator, finance partner, integration system, external partner or carrier
  - participating without owning a step: compliance officer
- **Trigger**: The payroll calendar reaches a run date for a pay group.
- **Preconditions**:
  - The pay group's period is open and its cutoff has passed.
  - Time, absence and compensation inputs for the period are locked.
  - No prior run for the same period is in a non-terminal state.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) open the run and freeze the input population and its watermarks
  2. `DECISION` (payroll administrator) REJECT a duplicate run for the same period rather than producing a second set of statements
  3. `CAPABILITY` (integration system) calculate gross to net per worker, producing lines and accumulators
  4. `CAPABILITY` (payroll administrator) run the pre-release checks: negative net, missing tax profile, out-of-band variance against the prior period
  5. `APPROVAL` (finance partner) payroll and finance approve the run totals bound to the calculation digest
  6. `CAPABILITY` (integration system) release the run and generate pay statements
  7. `CAPABILITY` (external partner or carrier) dispatch payment instructions in the declared rail order with idempotency keys
  8. `OBSERVE` (integration system) observe settlement per instruction; provider acknowledgement is not evidence of settled funds
  9. `END` (payroll administrator) close BusinessState on release, ConsistencyState only when settlement is observed
- **Intents**:
  - `hcmnext.payroll.run_payroll/v1` (PROPOSED, creates)
  - `hcmnext.payroll.calculate_payroll/v1` (PROPOSED, creates)
  - `hcmnext.payroll.release_payroll/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: deduction obligations
  - `compensation`: effective components at the period dates
  - `regulatory`: tax rule pack versions for every jurisdiction in the population
  - `time`: locked timecards and absence for the period
- **Writes**:
  - `paygl`: general ledger postings
  - `payroll`: run, calculation lines, accumulators, pay statements
  - `settlement`: payment instructions and their state
- **Evidence required**:
  - Frozen input watermark set, one per contributing domain.
  - Calculation digest the approval binds.
  - Per-instruction settlement observation, separately from the release.
- **Failure and repair**:
  - A second run is submitted for the same period -> PAYROLL_DUPLICATE_RUN_BLOCKED; the terminal exists in the conformance definition
  - An input domain's cutoff was reopened after the freeze -> PAYROLL_STALE_CUTOFF_BLOCKED; the run does not silently include late input
  - The run is released and then found wrong -> PAYROLL_REQUIRES_REVERSAL_INTENT; a released run is corrected by a reversal, never by re-running
  - Settlement is rejected or ambiguous -> PAYROLL_SETTLEMENT_REJECTED_DEGRADED or PAYROLL_SETTLEMENT_AMBIGUOUS_DEGRADED with a bounded repair; an ambiguous settlement is never retried blindly
- **Jurisdiction**:
  - US federal: FLSA requires payment on the regular payday for the period covered; IRS deposit schedules under Circular E are triggered by the payment date, and a late deposit incurs penalties independent of whether the worker was paid.
  - US state variation: California Labor Code 204 sets payday timing and 226 sets the wage statement content; New York Labor Law 191 sets frequency by worker class; Massachusetts requires payment within six or seven days of the period end depending on the workweek.
  - International: In the EU, payslip content is set by national law and, in several states, by collective agreement; SEPA credit transfers settle on a different rhythm than US ACH, which changes what an acknowledgement means.

### WF-PAY-002. Reverse a released payroll run

- **Status**: `EXISTING` - internal/workflow/conformance/payroll/definition.go terminal PAYROLL_REQUIRES_REVERSAL_INTENT
- **Archetype**: `A5` | **Complexity tier**: 5
- **Actors**: payroll administrator, finance partner, compliance officer, employee, integration system
  - performing a step: payroll administrator, compliance officer, employee, integration system
  - participating without owning a step: finance partner
- **Trigger**: A released run is found to be materially wrong before or after funds settle.
- **Preconditions**:
  - The run is released and its statements have been issued.
  - The settlement state of every instruction is known.
  - A reversal authority approves at the level the amount requires.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) classify each payment instruction as unsettled, settled or ambiguous
  2. `DECISION` (payroll administrator) route unsettled instructions to recall and settled ones to recovery, which are different legal acts
  3. `APPROVAL` (compliance officer) payroll, finance and compliance approve the reversal and its recovery plan
  4. `CAPABILITY` (integration system) issue the reversal, producing negative lines and corrected accumulators rather than deleting the original
  5. `SIGNAL` (employee) notify every affected worker with the corrected statement and the recovery terms
  6. `OBSERVE` (integration system) observe each recall and each recovery separately
  7. `END` (payroll administrator) close only the instructions that resolved; the rest stay PENDING_OBSERVATION
- **Intents**:
  - `hcmnext.payroll.reverse_payroll_run/v1` (PROPOSED, creates)
  - `hcmnext.operations.create_repair_plan/v1` (REAL, advances)
- **Reads**:
  - `payroll`: run, lines, statements, accumulators
  - `regulatory`: deposits already made against the run
  - `settlement`: instruction states per rail
- **Writes**:
  - `payroll`: reversal lines and corrected accumulators
  - `regulatory`: amended deposit or filing obligation
  - `settlement`: recall and recovery instructions
- **Evidence required**:
  - Per-instruction settlement classification at the moment of reversal.
  - Recovery consent where the jurisdiction requires it.
  - Corrected statements, retained alongside the originals.
- **Failure and repair**:
  - A recall succeeds for some workers and fails for others -> no false rollback; per-worker outcomes are reported and the failures become individual recovery cases
  - A worker has already spent the funds -> recovery follows the jurisdiction's deduction rules; the system does not schedule an unlawful deduction
  - A tax deposit was already made on the reversed amount -> an amended deposit or a credit against the next deposit is required and stays an open obligation
- **Jurisdiction**:
  - US federal: IRS rules permit adjusting an overpayment within the same year via Form 941-X, but a prior-year overpayment requires Form W-2c and, for the employee share, repayment before a correction can be claimed.
  - US state variation: California Labor Code 221 forbids self-help deductions to recover an overpayment; New York's 12 NYCRR 195 sets a notice, timing and frequency cap on recovery deductions.
  - International: In the UK an overpayment recovery is permitted under the Employment Rights Act 1996 section 14, which is an unusual carve-out from the general prohibition on deductions.

### WF-PAY-003. Process an off-cycle payment

- **Status**: `NEW`
- **Archetype**: `A1` | **Complexity tier**: 3
- **Actors**: payroll administrator, HR business partner, finance partner, integration system
  - performing a step: payroll administrator, finance partner, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A worker must be paid outside the regular cycle, for a termination, a correction or a missed payment.
- **Preconditions**:
  - The reason code for the off-cycle is one the tenant's policy permits.
  - The payment rail and its cut-off for same-day or next-day settlement are known.
  - The tax treatment for the payment type is determined.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) compute the payment, its withholding and its accumulator effect
  2. `DECISION` (payroll administrator) route final-pay off-cycles through the jurisdiction's timing rule, which may be immediate
  3. `APPROVAL` (finance partner) payroll and finance approve; a second approver is required above the tenant's threshold
  4. `CAPABILITY` (integration system) dispatch on the chosen rail with an idempotency key
  5. `OBSERVE` (payroll administrator) observe settlement and reconcile the accumulator effect into the next regular run
- **Intents**:
  - `hcmnext.payroll.issue_off_cycle_payment/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: the amount's source component
  - `payroll`: accumulators, prior payments, tax profile
  - `settlement`: rail availability and cut-off times
- **Writes**:
  - `payroll`: off-cycle payment lines and accumulators
  - `settlement`: payment instruction
- **Evidence required**:
  - Reason code and its policy authorization.
  - Rail selection and its cut-off evidence, because a missed cut-off changes the legal payment date.
  - Reconciliation into the next regular run.
- **Failure and repair**:
  - The rail cut-off passes while approvals run -> the payment date moves and the workflow recomputes whether the new date still satisfies the statutory deadline; it does not report success on a missed deadline
  - A duplicate off-cycle is submitted for the same reason and worker -> REJECT on the idempotency scope
  - Settlement is returned by the bank -> the instruction moves to a returned state and a repair reissues it; the original is not marked settled
- **Jurisdiction**:
  - US federal: Federal law sets no off-cycle timing rule; the deposit obligation follows the payment date, so a same-day payment can accelerate a deposit deadline.
  - US state variation: California Labor Code 201 requires final wages immediately on discharge and 202 within 72 hours on resignation, which makes the off-cycle a statutory deadline rather than a convenience; Massachusetts requires final pay on the discharge day; Texas allows six days.
  - International: In the EU, off-cycle payments are unusual and often require a payslip of their own; SEPA instant transfers make same-day settlement feasible where ACH would not.

### WF-PAY-004. Set up and apply a wage garnishment

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 4
- **Actors**: payroll administrator, compliance officer, employee, external partner or carrier, integration system
- **Trigger**: A court or agency order requires withholding from a worker's wages.
- **Preconditions**:
  - The order document is received and its issuing authority verified.
  - The order type, its priority class and its arrears are recorded.
  - The worker's disposable earnings basis is computable.
- **Steps**:
  1. `TASK` (payroll administrator) record the order, its authority, its amount or formula and its effective dates
  2. `RULE` (compliance officer) compute the protected amount and the maximum withholding under the applicable federal and state caps
  3. `DECISION` (compliance officer) order multiple concurrent garnishments by statutory priority rather than by arrival
  4. `CAPABILITY` (integration system) commit the garnishment obligation with its priority and its cap
  5. `SIGNAL` (employee) issue the worker the notice the jurisdiction requires and record its delivery
  6. `CAPABILITY` (external partner or carrier) apply the deduction each period and remit to the payee
  7. `OBSERVE` (payroll administrator) observe remittance and track the balance until the order is satisfied or released
- **Intents**:
  - `hcmnext.payroll.apply_garnishment/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: the order artifact in a restricted compartment
  - `payroll`: disposable earnings, existing garnishments and their priorities
  - `regulatory`: federal and state garnishment caps
- **Writes**:
  - `payroll`: garnishment obligation, per-period deductions
  - `settlement`: remittance instructions to the payee
- **Evidence required**:
  - The order document with its authority verified, held in a restricted compartment.
  - Cap computation showing the disposable earnings basis and the limit applied.
  - Remittance evidence per period and the running balance.
- **Failure and repair**:
  - Concurrent orders exceed the aggregate cap -> apply in statutory priority and record the unsatisfied balance; never exceed the cap to satisfy a later order
  - The worker's manager requests the garnishment detail -> DENIED by name; a garnishment is outside the manager-visible compensation vocabulary
  - The payee's remittance address is stale -> the deduction is taken but the remittance is BLOCKED and escalated; the funds are held, not returned to the worker
- **Jurisdiction**:
  - US federal: The Consumer Credit Protection Act Title III caps ordinary garnishment at 25 percent of disposable earnings or the amount above 30 times the federal minimum wage, whichever is less; child support under the CCPA can reach 50 to 65 percent, and federal tax levies follow IRS Publication 1494 exemption tables instead.
  - US state variation: Texas and Pennsylvania prohibit garnishment for most consumer debts entirely; North Carolina and South Carolina restrict it sharply; California, New York and Illinois each set lower caps than the federal one, and the lower cap governs.
  - International: In the UK, attachment of earnings orders have their own protected-earnings mechanics; several EU states require the employer to reply to the court within a fixed window or become liable for the debt.

### WF-PAY-005. Enroll a worker in payroll for the first time

- **Status**: `EXISTING` - planning/workflows/discovery-backlog.md rewards adjacency EnrollWorkerInPayroll
- **Archetype**: `A1` | **Complexity tier**: 2
- **Actors**: payroll administrator, HR business partner, employee, integration system
  - performing a step: payroll administrator, employee, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A new employment requires a payroll enrollment before the first period.
- **Preconditions**:
  - The employment, its legal entity and its work jurisdiction are committed.
  - The worker has supplied withholding elections and a payment method.
  - The pay group matching the jurisdiction and frequency exists.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) resolve the pay group from the legal entity, jurisdiction and frequency
  2. `TASK` (employee) the worker submits withholding elections and a payment method through self-service
  3. `RULE` (payroll administrator) validate the elections against the jurisdiction's current forms and default to the statutory fallback where an election is missing
  4. `CAPABILITY` (integration system) commit the enrollment and dispatch it to the payroll provider
  5. `OBSERVE` (integration system) confirm the provider created the worker and returned its own identifier
  6. `END` (payroll administrator) close only when the provider identifier is bound; an unbound worker will not be paid
- **Intents**:
  - `hcmnext.payroll.enroll_worker/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: pay group calendar
  - `people`: employment, legal entity, work location
  - `regulatory`: withholding forms and defaults for the jurisdiction
- **Writes**:
  - `integration`: provider worker identifier
  - `payroll`: enrollment, tax profile, payment method reference
- **Evidence required**:
  - Withholding elections with their form version.
  - Payment method verification, holding no raw bank detail in the domain record.
  - Provider identifier binding.
- **Failure and repair**:
  - The worker supplies no withholding election before the first run -> apply the statutory default and record that it was a default, so the worker is paid rather than blocked
  - The provider rejects the enrollment for a data mismatch -> BLOCKED with the exact field named; the first run must not proceed with an unenrolled worker
  - A duplicate enrollment is created for the same employment -> REJECT on the idempotency scope; two enrollments mean two payments
- **Jurisdiction**:
  - US federal: Form W-4 governs federal withholding and the employer must apply the default single-with-no-adjustments treatment if none is filed; Form I-9 completion is a separate deadline of three business days from the start date.
  - US state variation: Most states have their own withholding certificate; California DE 4, New York IT-2104 and others differ from the federal form, and eight states have no income tax and therefore no certificate.
  - International: In the EU, tax codes come from the authority rather than the worker: HMRC issues a UK tax code and a missing P45 triggers an emergency code, which is a different mechanic from a US default.

### WF-PAY-006. Reconcile the payroll ledger to the general ledger

- **Status**: `EXISTING` - internal/domains/paygl package; internal/workflow/conformance/payroll/definition.go
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: payroll administrator, finance partner, auditor
- **Trigger**: After a run, payroll totals must agree with the general ledger postings.
- **Preconditions**:
  - The run is released and its postings are generated.
  - The general ledger period is open.
  - A reconciliation policy defines what a material difference is.
- **Steps**:
  1. `CAPABILITY` (finance partner) read the run totals by account and the posted general ledger entries
  2. `TRANSFORM` (finance partner) compare by account, cost centre and period, producing typed differences
  3. `DECISION` (finance partner) classify each difference as timing, mapping, rounding or unexplained
  4. `CAPABILITY` (payroll administrator) produce a repair plan for mapping and unexplained differences
  5. `APPROVAL` (finance partner) finance approves any posting correction
  6. `END` (auditor) close ConsistencyState only when unexplained differences are zero
- **Intents**:
  - `hcmnext.operations.detect_drift/v1` (REAL, creates)
  - `hcmnext.operations.create_repair_plan/v1` (REAL, creates)
- **Reads**:
  - `budget`: cost centre attribution
  - `paygl`: posted entries and the account mapping version
  - `payroll`: run totals by account
- **Writes**:
  - `operations`: drift findings and repair plan
  - `paygl`: correcting entries after approval
- **Evidence required**:
  - Difference list with its classification, not a single net figure.
  - Account mapping version used.
  - Approval for each correcting entry.
- **Failure and repair**:
  - A difference is classified as timing but persists into the next period -> it is reclassified as unexplained and escalated; a timing difference that does not clear is a mapping error
  - The ledger period closes before the repair executes -> the correction posts to the open period with a reference to the original, never by reopening a closed period silently
  - Rounding differences accumulate above the materiality threshold -> the rounding profile itself becomes the finding
- **Jurisdiction**:
  - US federal: Sarbanes-Oxley internal control requirements make this reconciliation an auditable control for public filers; the classification detail is what an auditor tests.
  - US state variation: None state-specific.
  - International: IFRS and local GAAP differences in accrual timing can create legitimate persistent differences, which is why the classification vocabulary must include a policy-difference class.

### WF-PAY-007. Compute and issue final pay on termination

- **Status**: `EXISTING` - planning/workflows/discovery-backlog.md rewards adjacency CalculateFinalPay; internal/workflow/conformance/termination/definition.go
- **Archetype**: `A3` | **Complexity tier**: 4
- **Actors**: payroll administrator, HR business partner, employee, finance partner, compliance officer, integration system
  - performing a step: payroll administrator, finance partner, compliance officer, integration system
  - participating without owning a step: HR business partner, employee
- **Trigger**: A termination requires final wages, accrued leave payout and any severance, by a statutory deadline.
- **Preconditions**:
  - The termination date and its type, voluntary or involuntary, are committed.
  - Accrued and unused leave balances are final.
  - The jurisdiction's final pay deadline is resolved before the payment is scheduled.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) compute earned wages to the termination date, accrued leave payout, and any severance or notice pay
  2. `RULE` (compliance officer) resolve the jurisdiction's final pay deadline and whether accrued leave must be paid out
  3. `DECISION` (payroll administrator) route to an immediate off-cycle when the deadline falls before the next regular run
  4. `APPROVAL` (finance partner) HR and payroll approve; finance approves severance
  5. `CAPABILITY` (integration system) dispatch the payment and generate the final statement
  6. `OBSERVE` (payroll administrator) observe settlement against the statutory deadline and record whether it was met
  7. `END` (compliance officer) close with any waiting-time penalty exposure recorded as an open obligation, not hidden
- **Intents**:
  - `hcmnext.payroll.calculate_final_pay/v1` (PROPOSED, creates)
  - `hcmnext.payroll.issue_off_cycle_payment/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: severance terms
  - `leave`: accrued and unused balances and their payout rules
  - `regulatory`: final pay deadline and leave payout rules
  - `time`: final timecard and unpaid hours
- **Writes**:
  - `leave`: balance payout and closure
  - `payroll`: final pay lines and statement
  - `settlement`: payment instruction with the deadline attached
- **Evidence required**:
  - Deadline computation with the rule version.
  - Leave payout basis, per policy and per statute.
  - Settlement observation against the deadline.
- **Failure and repair**:
  - The deadline passes before settlement -> the miss is recorded as an obligation with its exposure computed; the workflow does not silently close as successful
  - Final timecard hours arrive after the payment -> a supplemental payment is issued and the original stands; the first payment is not reversed
  - A company loan or equipment charge is deducted -> BLOCKED unless the jurisdiction permits the deduction with the consent on file
- **Jurisdiction**:
  - US federal: FLSA requires payment for all hours worked but sets no final-pay deadline; the deadline is entirely state law. Accrued vacation is not a federal wage.
  - US state variation: California Labor Code 201 to 203 requires immediate payment on discharge, treats accrued vacation as wages that cannot be forfeited, and imposes waiting-time penalties of up to 30 days of wages; Massachusetts requires payment on the discharge date; Texas allows six calendar days; Georgia and Florida have no statutory deadline and default to the next regular payday.
  - International: In much of the EU, notice pay and accrued holiday payout are statutory minimums set by the Working Time Directive as implemented nationally, and holiday cannot generally be forfeited.

### WF-PAY-008. Handle a returned or failed direct deposit

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 3
- **Actors**: payroll administrator, employee, external partner or carrier, integration system
  - performing a step: payroll administrator, integration system
  - participating without owning a step: employee, external partner or carrier
- **Trigger**: A bank returns a payment because the account is closed or the details are wrong.
- **Preconditions**:
  - The original payment instruction and its settlement state are retrievable.
  - The return reason code from the rail is captured.
  - A fallback payment method policy exists.
- **Steps**:
  1. `OBSERVE` (integration system) receive the return and record its reason code against the original instruction
  2. `DECISION` (payroll administrator) distinguish a return, where funds came back, from a rejection at submission, where they never left
  3. `TASK` (payroll administrator) contact the worker for corrected details through a channel that does not disclose the amount to a third party
  4. `CAPABILITY` (integration system) invalidate the stale payment method so the next run does not reuse it
  5. `CAPABILITY` (integration system) reissue on a corrected method or the fallback rail with a new idempotency key
  6. `OBSERVE` (payroll administrator) observe the reissue and close only on settlement
- **Intents**:
  - `hcmnext.payroll.reissue_payment/v1` (PROPOSED, creates)
  - `hcmnext.operations.create_repair_plan/v1` (REAL, advances)
- **Reads**:
  - `payroll`: the run the payment belonged to
  - `people`: worker contact points
  - `settlement`: original instruction, rail return codes
- **Writes**:
  - `payroll`: payment state on the original line
  - `settlement`: reissue instruction, invalidated payment method
- **Evidence required**:
  - Return reason code, verbatim from the rail.
  - Method invalidation record so the failure is not repeated next period.
  - Reissue settlement observation.
- **Failure and repair**:
  - The return arrives after the next run already used the same method -> both payments fail; the method invalidation must be immediate on the return, not deferred to a batch
  - The worker cannot be reached -> the funds are held in a suspense state with an escheatment clock started, not returned to the employer's operating account silently
  - A duplicate reissue is dispatched -> the idempotency key prevents it; a double payment becomes a recovery case, which is worse than a delay
- **Jurisdiction**:
  - US federal: Unclaimed wages become subject to state escheatment after a dormancy period, so a permanently unreachable worker's funds have a legal destination.
  - US state variation: California requires unclaimed wages to be reported to the state after three years; New York uses three years for wages as well but with different reporting cycles; Delaware's dormancy rules are the most aggressive and apply to many employers by incorporation.
  - International: SEPA returns carry ISO 20022 reason codes with different semantics from NACHA return codes, so a shared vocabulary must map rather than merge them.

### WF-PAY-009. Report non-employee compensation to a contractor and the authority

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 3
- **Actors**: payroll administrator, finance partner, external partner or carrier, compliance officer
- **Trigger**: Payments to a non-employee must be tracked to the reporting threshold and reported annually.
- **Preconditions**:
  - The payee's taxpayer identification and its certification are on file before the first payment.
  - The payment classification, services against goods, decides whether it is reportable.
  - Backup withholding applies where the certification is missing or fails matching.
- **Steps**:
  1. `TASK` (finance partner) collect the payee certification before the first payment
  2. `DECISION` (payroll administrator) apply backup withholding where the certification is absent or the identification fails matching
  3. `CAPABILITY` (finance partner) accumulate reportable payments per payee per year with their classification
  4. `CAPABILITY` (compliance officer) produce and furnish the payee statement and transmit the return by their separate deadlines
  5. `OBSERVE` (external partner or carrier) record the authority's acceptance and any matching notice
  6. `END` (compliance officer) close on acceptance; a matching notice reopens the payee's certification
- **Intents**:
  - `hcmnext.payroll.report_non_employee_compensation/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: payments to the payee and their classification
  - `people`: the payee's certification and identification
  - `regulatory`: reporting thresholds, form versions and deadlines
- **Writes**:
  - `payroll`: backup withholding where applied
  - `regulatory`: payee statements, the transmitted return and its acceptance
- **Evidence required**:
  - The payee certification with its date, preceding the first payment.
  - Per-payee accumulation with the classification that made each payment reportable.
  - Furnishing and filing evidence against their separate deadlines.
- **Failure and repair**:
  - The certification is missing at the first payment -> backup withholding applies from that payment; collecting the certification later does not retroactively excuse it
  - The identification fails matching -> the authority's notice starts a solicitation process with its own deadlines, and continued payment without it compounds the exposure
  - A payee is reclassified as an employee after the year is reported -> the reporting must be corrected and the employment tax exposure computed; the two filings cannot both stand for the same payments
- **Jurisdiction**:
  - US federal: Form 1099-NEC reports non-employee compensation at or above the statutory threshold with a 31 January furnishing and filing deadline, and Form W-9 certification supports it; backup withholding under IRC 3406 applies where certification is missing or matching fails, and electronic filing thresholds have fallen sharply.
  - US state variation: Many states require their own information return or participate in the combined federal and state filing programme, with different thresholds and deadlines; several states require reporting below the federal threshold.
  - International: No analogue; payments to self-employed contractors in the EU are reported through national systems with entirely different mechanics, and in several states the payer has withholding duties the US does not impose.

### WF-PAY-010. Respond to a tax authority notice

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 4
- **Actors**: payroll administrator, compliance officer, finance partner, external partner or carrier
- **Trigger**: An authority sends a notice about a discrepancy, a penalty or a missing return, with a response deadline.
- **Preconditions**:
  - Notices arrive by post to an address that may not be monitored, so intake is itself a control.
  - The response deadline is short and running from the notice date, not from receipt.
  - Paying a penalty is not the same as agreeing with it.
- **Steps**:
  1. `TASK` (payroll administrator) record the notice with its authority, its notice date, its deadline and its subject period
  2. `CAPABILITY` (payroll administrator) reconstruct the period's figures and compare against what the authority asserts
  3. `DECISION` (compliance officer) classify as agree, disagree with evidence, or partially agree; each has a different response and a different deadline
  4. `APPROVAL` (finance partner) finance approves any payment and compliance approves the response content
  5. `CAPABILITY` (external partner or carrier) transmit the response and any payment, and record the confirmation
  6. `WAIT` (compliance officer) hold for the authority's reply, which commonly takes months
  7. `END` (finance partner) close on resolution; an unresolved notice remains an open obligation with its exposure
- **Intents**:
  - `hcmnext.regulatory.respond_to_authority_notice/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: penalty and interest accruals
  - `payroll`: the period's accumulators and deposits
  - `regulatory`: the notice, prior notices, filings for the subject period
- **Writes**:
  - `paygl`: penalty and interest postings
  - `regulatory`: notice record, response, payment and resolution
- **Evidence required**:
  - The notice with its notice date, which starts the deadline rather than the receipt date.
  - The reconstruction of the period's figures behind the response.
  - The transmitted response and the authority's confirmation.
- **Failure and repair**:
  - The notice is discovered after its deadline -> the intake gap is the finding; the response is filed late with a reasonable cause argument and the exposure is recorded
  - Payment is made without a response -> the payment may be applied to the wrong period or the wrong tax and the notice remains open; paying is not responding
  - The authority's figures are right and an earlier filing was wrong -> an amended return follows as its own obligation, and the root cause is a data quality finding rather than a one-off correction
- **Jurisdiction**:
  - US federal: IRS notices carry deadlines measured from the notice date, and penalties under IRC 6651 and 6656 accrue regardless of whether the notice was received; a reasonable cause abatement request is a separate filing with its own standard.
  - US state variation: State authorities each have their own notice types, deadlines and appeal routes, and several are materially shorter than the federal ones; local jurisdictions in Pennsylvania and Ohio add another layer.
  - International: In the UK, HMRC notices under PAYE have their own appeal windows and a determination becomes final if not appealed in time.

### WF-PAY-011. Migrate payroll to a new provider across a period boundary

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: payroll administrator, external partner or carrier, compliance officer, integration system, employee
  - performing a step: payroll administrator, external partner or carrier, compliance officer, integration system
  - participating without owning a step: employee
- **Trigger**: Payroll processing moves to a new provider and the boundary between the two has to be exact.
- **Preconditions**:
  - Year-to-date figures belong to the system that computed them; the return that reports them is filed by that system's operator.
  - Carry-forward positions such as tax codes, loan positions and starter declarations must migrate; computed accumulators generally should not.
  - The outgoing provider's obligations survive the end of its service.
- **Steps**:
  1. `DECISION` (payroll administrator) choose the boundary, preferring a tax year or a period boundary over a mid-period cut
  2. `RULE` (compliance officer) assign the year-end and correction obligations for periods the outgoing provider ran
  3. `CAPABILITY` (integration system) migrate the carry-forward positions and open the incoming provider with zeroed year-to-date figures where the boundary permits
  4. `CAPABILITY` (payroll administrator) run a parallel period and reconcile the two providers' outputs before the cutover
  5. `APPROVAL` (compliance officer) compliance approves the cutover against the parallel reconciliation
  6. `OBSERVE` (payroll administrator) confirm the first live run against the reconciliation and hold any worker whose carry-forward position is missing
  7. `END` (external partner or carrier) close on the first successful live settlement; the outgoing provider's filing obligations remain open with their own deadlines
- **Intents**:
  - `hcmnext.payroll.migrate_payroll_provider/v1` (PROPOSED, creates)
  - `hcmnext.payroll.reconcile_parallel_run/v1` (PROPOSED, advances)
- **Reads**:
  - `commercial`: both provider arrangements and their term
  - `payroll`: the run history, the accumulators and the carry-forward positions
  - `regulatory`: the filing obligations for the periods each provider ran
- **Writes**:
  - `payroll`: the incoming provider's opening positions and the parallel reconciliation
  - `regulatory`: the assigned filing obligations
- **Evidence required**:
  - The chosen boundary and its rationale.
  - The parallel run reconciliation, per worker rather than in total.
  - The carry-forward positions migrated and the accumulators deliberately not migrated.
  - The outgoing provider's filing obligations contracted beyond the end of its service.
- **Failure and repair**:
  - Year-to-date figures are migrated across a tax year boundary -> a return is filed by a system that did not compute the year and nobody can reconcile it
  - The outgoing contract ends before its filing obligations are discharged -> the obligation lapses with the service and the filing has no owner
  - A worker's carry-forward position is missing at the first live run -> that worker is held rather than defaulted to an emergency basis
  - The parallel run is skipped for schedule reasons -> the first live run is the first comparison and its errors reach workers
- **Jurisdiction**:
  - US federal: Federal deposit and return obligations attach to the employer rather than the provider, and a successor or agent arrangement determines who files; a provider change does not transfer the employer's own liability.
  - US state variation: State registrations, deposit schedules and electronic filing enrolments each have to be repointed, and several states require notice of an agent change before it takes effect.
  - International: Equivalent obligations exist in every jurisdiction with employer withholding; the United Kingdom's real time information regime in particular makes a mid-year provider change materially harder than a tax year boundary.

---

## TAX. Tax and regulatory

6 workflows.

### WF-TAX-001. Resolve the tax jurisdictions that apply to a worker

- **Status**: `EXISTING` - definitions/governance/feature-intent-intake.yaml group 5 feature jurisdiction_resolve; internal/workflow/conformance/transfer/definition.go node resolve_jurisdiction
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: payroll administrator, compliance officer, integration system
- **Trigger**: A worker's work and residence locations must be turned into a set of withholding jurisdictions.
- **Preconditions**:
  - Both work and residence addresses are normalized.
  - The rule pack for the effective date is published.
  - Reciprocity agreements between the two states are known.
- **Steps**:
  1. `CAPABILITY` (integration system) normalize both addresses and resolve them to jurisdiction codes at every level
  2. `RULE` (compliance officer) apply reciprocity, convenience-of-the-employer and local-tax rules to produce the withholding set
  3. `DECISION` (compliance officer) return UNKNOWN rather than a guess when an address resolves ambiguously across a boundary
  4. `CAPABILITY` (payroll administrator) return the jurisdiction set with the rule pack version pinned
  5. `END` (integration system) complete as a calculation with zero writes
- **Intents**:
  - `hcmnext.regulatory.resolve_jurisdiction/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: work and residence addresses with their intervals
  - `regulatory`: jurisdiction hierarchy, reciprocity table, rule pack version
- **Writes**:
- **Evidence required**:
  - Address normalization result and its confidence.
  - Rule pack version and the reciprocity rules applied.
  - Explicit UNKNOWN where the boundary is ambiguous.
- **Failure and repair**:
  - The address straddles a local tax boundary -> UNKNOWN with both candidates named; guessing creates either an over- or under-withholding liability
  - The worker works in several states in one period -> the result is a set with allocation percentages, not a single jurisdiction
  - The rule pack has no version for the effective date -> FAIL_CLOSED; withholding computed against an unversioned rule is indefensible
- **Jurisdiction**:
  - US federal: Federal withholding follows the employer's obligation regardless of state; multi-state work requires allocation, and the employer must track it rather than assume the primary work location.
  - US state variation: Pennsylvania and Ohio have thousands of local income tax jurisdictions resolved by street address, so normalization accuracy is a compliance control; New York, New Jersey, Connecticut, Delaware, Nebraska and Pennsylvania apply convenience-of-the-employer rules; reciprocity agreements between neighbouring states remove residence-state withholding in some pairs only.
  - International: Cross-border work in the EU is governed by bilateral tax treaties and by Regulation 883/2004 for social security, which can place income tax and social security in different countries for the same worker.

### WF-TAX-002. Submit a periodic government filing

- **Status**: `EXISTING` - planning/workflows/\_shared/workflow-archetypes.md A11 Regulated filing or irreversible submission; definitions/governance/feature-intent-intake.yaml group 5 feature government_filing_submit
- **Archetype**: `A11` | **Complexity tier**: 4
- **Actors**: compliance officer, payroll administrator, finance partner, external partner or carrier, integration system
  - performing a step: compliance officer, payroll administrator, external partner or carrier, integration system
  - participating without owning a step: finance partner
- **Trigger**: A quarterly or annual return is due to a tax authority.
- **Preconditions**:
  - The filing period is closed and its figures are final.
  - The filing package format and the authority's transmission channel are configured.
  - The signing authority is identified and available.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) assemble the filing package from the ledger and freeze it as an immutable artifact
  2. `CAPABILITY` (compliance officer) validate the package against the authority's schema and its arithmetic edits
  3. `APPROVAL` (compliance officer) the signing authority approves the exact package digest, not a summary of it
  4. `CAPABILITY` (external partner or carrier) transmit to the authority with an idempotency key and record the submission attempt
  5. `OBSERVE` (integration system) obtain the authority's acknowledgement and its confirmation number
  6. `DECISION` (compliance officer) an acknowledgement of receipt is not acceptance; wait for the acceptance response before closing
  7. `END` (compliance officer) close ObligationState only on acceptance; a rejected filing reopens the obligation with its deadline intact
- **Intents**:
  - `hcmnext.regulatory.submit_government_filing/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: period accumulators and deposits
  - `people`: the population in scope for the filing
  - `regulatory`: filing definition, schema version, deadline
- **Writes**:
  - `regulatory`: filing artifact, submission attempts, acknowledgement and acceptance records
- **Evidence required**:
  - Immutable filing package with its digest.
  - Signing approval bound to that digest.
  - Every submission attempt, including failed ones, with the authority's response.
- **Failure and repair**:
  - The authority rejects the filing on a validation edit -> the obligation stays open with the original deadline; the rejection reason is attached and a corrected package is a new submission attempt, not an edit of the old one
  - The transmission times out with no response -> ConsistencyState UNKNOWN; a blind resubmit risks a duplicate filing, so the workflow queries the authority's status endpoint before retrying
  - Figures change after submission -> an amended filing is a separate intent with its own approval; the submitted package is immutable
- **Jurisdiction**:
  - US federal: IRS Forms 941 quarterly and 940 annually, plus Forms W-2 and W-3 by 31 January, carry penalties under IRC 6651 and 6656 that accrue from the deadline regardless of the reason.
  - US state variation: Every state with income tax has its own withholding return and its own unemployment insurance return with a different deadline and format; some states, such as Pennsylvania and Ohio, add local returns.
  - International: In the UK, PAYE Real Time Information requires a Full Payment Submission on or before each payment date rather than periodically, which is a fundamentally different cadence.

### WF-TAX-003. Determine a worker's classification as employee or contractor

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 5 feature regulatory_compliance_check; planning/research/state-employment-law/california.md AB 5 ABC test
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: compliance officer, HR business partner, finance partner
  - performing a step: compliance officer, HR business partner
  - participating without owning a step: finance partner
- **Trigger**: The engagement terms of a worker must be tested against the applicable classification standard.
- **Preconditions**:
  - The engagement facts are collected as structured answers, not free text.
  - The jurisdictions in scope are resolved.
  - The rule pack encodes each applicable test separately.
- **Steps**:
  1. `TASK` (HR business partner) collect the engagement facts through a structured questionnaire
  2. `RULE` (compliance officer) evaluate each applicable test independently: the IRS common-law test, the FLSA economic-reality test and any state ABC test
  3. `DECISION` (compliance officer) when the tests disagree, the strictest governing result controls the treatment and the disagreement is recorded
  4. `CAPABILITY` (compliance officer) produce the determination with the facts, the tests, the results and the rule versions
  5. `END` (HR business partner) complete as a calculation; changing the engagement is a separate intent
- **Intents**:
  - `hcmnext.regulatory.determine_worker_classification/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: payment structure and exclusivity
  - `people`: engagement terms, control facts, tenure
  - `regulatory`: classification rule packs per jurisdiction
- **Writes**:
  - `regulatory`: classification determination artifact
- **Evidence required**:
  - The structured fact set, so the determination is reproducible.
  - Per-test results with the rule version each used.
  - The disagreement record where tests diverge.
- **Failure and repair**:
  - The facts are incomplete -> return UNKNOWN for the affected test rather than defaulting to contractor, which is the answer that creates liability
  - A determination contradicts an existing engagement -> the determination stands and a remediation obligation is opened; the system does not suppress an inconvenient result
  - The rule pack changes after a determination -> the old determination keeps its version and a re-evaluation is a new artifact
- **Jurisdiction**:
  - US federal: The IRS common-law test weighs behavioral control, financial control and relationship; the FLSA uses an economic-reality test; the two can and do reach different answers on the same facts, and both apply.
  - US state variation: California AB 5 applies the ABC test with prong B as the usual failure point; New Jersey and Massachusetts have their own ABC variants; Texas and Florida largely follow the common-law test, making the same engagement lawful in one state and not another.
  - International: The UK IR35 off-payroll rules put the determination duty on the client and require a status determination statement; the EU platform work directive establishes a rebuttable presumption of employment on specified indicators.

### WF-TAX-004. Register the employer in a new tax jurisdiction

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 4
- **Actors**: compliance officer, payroll administrator, finance partner
  - performing a step: compliance officer, payroll administrator
  - participating without owning a step: finance partner
- **Trigger**: A first worker in a new state or locality requires employer registration before withholding.
- **Preconditions**:
  - The jurisdiction and the tax types requiring registration are identified.
  - The legal entity's registration data is available.
  - The lead time for each registration is known and compared against the first payroll date.
- **Steps**:
  1. `CAPABILITY` (compliance officer) identify every tax type requiring registration in the jurisdiction, including unemployment insurance and local taxes
  2. `DECISION` (compliance officer) compare each registration lead time against the first payroll date and flag those that will not complete in time
  3. `TASK` (compliance officer) submit each registration and record its reference
  4. `WAIT` (compliance officer) hold for the authority's account number, which is a hard dependency for filing
  5. `OBSERVE` (payroll administrator) record each account number and its effective date
  6. `END` (compliance officer) close only when every required account exists; a partial registration leaves an open obligation
- **Intents**:
  - `hcmnext.regulatory.register_employer_jurisdiction/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: first payroll date
  - `people`: the entity and its first worker in the jurisdiction
  - `regulatory`: registration requirements per jurisdiction and tax type
- **Writes**:
  - `regulatory`: registration records and account numbers with their effective dates
- **Evidence required**:
  - Per-tax-type registration reference and its status.
  - Lead time versus first payroll date comparison.
  - Account numbers with the dates from which they are valid.
- **Failure and repair**:
  - The account number does not arrive before the first payroll -> the payroll is BLOCKED or run with a documented pending-registration position that carries a named liability; the choice is explicit
  - The registration is rejected for entity data mismatch -> the exact field is surfaced and the registration is resubmitted; withholding does not proceed on an unregistered account
  - A local jurisdiction is discovered after the first payroll -> a retroactive registration and amended filings become an open obligation with its penalty exposure computed
- **Jurisdiction**:
  - US federal: The employer must have an EIN before any withholding; state registrations are separate and none is inferred from the federal one.
  - US state variation: Registration lead times range from same-day online in some states to several weeks in others; Pennsylvania and Ohio local jurisdictions each require their own registration, and New York requires separate withholding and unemployment registrations.
  - International: In the EU, an employer without a local entity generally cannot register directly and needs either a non-resident employer registration, where the state allows it, or an employer of record.

### WF-TAX-005. Apply a regulatory change when a rule pack version updates

- **Status**: `PARTIAL` - planning/specs/legal-rule-packs-and-state-configuration.md
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: compliance officer, payroll administrator, auditor, integration system
- **Trigger**: A new rule pack version changes a rate, a threshold or a test with an effective date.
- **Preconditions**:
  - The new rule pack version is published with its effective interval.
  - The population affected by the change is computable.
  - The pinning policy for in-flight intents is defined.
- **Steps**:
  1. `CAPABILITY` (compliance officer) compute which in-flight intents and which future-dated changes reference the superseded rule
  2. `DECISION` (compliance officer) apply the pinning policy: pin, reevaluate, or require review, per obligation, rather than one blanket rule
  3. `CAPABILITY` (payroll administrator) reevaluate the affected population and produce the delta
  4. `APPROVAL` (compliance officer) compliance approves the adoption plan and its cutover date
  5. `CAPABILITY` (integration system) activate the new version at its effective date
  6. `OBSERVE` (auditor) verify the first calculation after cutover used the new version
- **Intents**:
  - `hcmnext.regulatory.apply_regulatory_change/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: in-flight runs and future-dated changes
  - `people`: affected population
  - `regulatory`: old and new rule pack versions and their intervals
- **Writes**:
  - `payroll`: recalculated figures where reevaluation applied
  - `regulatory`: adoption plan and activation record
- **Evidence required**:
  - Both rule versions and the diff between them.
  - Per-obligation pinning decision.
  - Post-cutover verification that the new version was actually used.
- **Failure and repair**:
  - An approved proposal was priced under the old rule -> the pinning policy decides; where it says reevaluate, the approval is invalidated and REPLAN_AND_REAPPROVE follows
  - The change is retroactive -> the reevaluation produces retro corrections as child intents rather than silently restating history
  - The new version arrives after its own effective date -> the gap is recorded as a compliance finding with the exposure computed; back-dating the activation would hide it
- **Jurisdiction**:
  - US federal: Federal rate changes such as the Social Security wage base take effect on a fixed calendar and are non-negotiable; a late adoption creates both under-withholding and deposit penalties.
  - US state variation: State minimum wage indexation commonly takes effect on 1 January or 1 July, and local ordinances index on their own schedules, so a single worker can face three floors changing on three dates; California, Washington and Colorado all index annually.
  - International: In the EU, statutory minimum wage and social security ceilings are set nationally with different effective dates; a mid-year change in one country must not disturb pinned calculations in another.

### WF-TAX-006. Contest a penalty assessment and pursue abatement

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 4
- **Actors**: compliance officer, finance partner, payroll administrator, auditor
- **Trigger**: A penalty is assessed and the employer believes it should be abated.
- **Preconditions**:
  - The penalty's basis, its computation and its period are identified.
  - The abatement grounds available, first-time relief or reasonable cause, are distinguished.
  - An abatement request is a filing with its own deadline and its own evidence standard.
- **Steps**:
  1. `CAPABILITY` (compliance officer) identify the penalty, its statutory basis and its computation
  2. `TRANSFORM` (payroll administrator) assemble the facts supporting the abatement ground, which for reasonable cause must show ordinary business care
  3. `DECISION` (compliance officer) choose between administrative first-time relief and a reasonable cause argument; using first-time relief consumes it for a period
  4. `APPROVAL` (finance partner) finance approves the approach and any payment made under protest
  5. `CAPABILITY` (compliance officer) file the abatement request and record its reference
  6. `WAIT` (compliance officer) hold for the determination and pursue appeal if it is denied
  7. `END` (auditor) close with the outcome and, where the penalty stands, the root cause as a separate finding
- **Intents**:
  - `hcmnext.regulatory.contest_penalty/v1` (PROPOSED, creates)
- **Reads**:
  - `operations`: any incident or outage supporting reasonable cause
  - `payroll`: the underlying period and its evidence
  - `regulatory`: the assessment, prior penalties, prior abatements used
- **Writes**:
  - `paygl`: penalty reversal where abated
  - `regulatory`: abatement request, its determination and any appeal
- **Evidence required**:
  - The penalty's statutory basis and computation.
  - The evidence supporting the abatement ground, assembled from the ledger rather than asserted.
  - The determination and, if the penalty stands, the root cause finding.
- **Failure and repair**:
  - First-time relief is used for a small penalty -> it is consumed and unavailable for a larger one in the same window; the choice is recorded as a decision rather than made by whoever files first
  - Reasonable cause rests on a vendor's failure -> reliance on a third party is not automatically reasonable cause, and the argument must show the employer's own ordinary business care
  - The penalty is abated but the underlying process is unchanged -> the root cause finding stays open; an abated penalty with an unfixed cause returns
- **Jurisdiction**:
  - US federal: IRC 6651 and 6656 penalties may be abated for reasonable cause, and the administrative first-time abatement is available once in a rolling window; the request has its own procedure and an appeal route.
  - US state variation: State abatement standards and windows differ and several states have no administrative first-time relief at all.
  - International: In the UK, HMRC penalties may be appealed on reasonable excuse, which is a narrower standard than reasonable cause and specifically excludes reliance on another person unless reasonable care was taken.

---

## BEN. Benefits

13 workflows.

### WF-BEN-001. Run open enrollment for a population

- **Status**: `EXISTING` - internal/workflow/conformance/benefits/definition.go workflow hcmnext.workflows.benefits_eligibility_election_reconciliation; planning/workflows/leave/catalog.md benefits rows
- **Archetype**: `A10` | **Complexity tier**: 5
- **Actors**: benefits administrator, employee, external partner or carrier, compliance officer, integration system
  - performing a step: benefits administrator, employee, external partner or carrier, integration system
  - participating without owning a step: compliance officer
- **Trigger**: The annual enrollment window opens for a plan year.
- **Preconditions**:
  - Plan revisions and rates for the new plan year are published.
  - The eligible population and its dependants are computable.
  - The window's open and close instants are set in a defined time zone.
- **Steps**:
  1. `CAPABILITY` (benefits administrator) freeze the eligible population and evaluate eligibility per plan
  2. `SIGNAL` (employee) notify every eligible employee with their personalized plan set and the window close time
  3. `TASK` (employee) employees make elections, add or remove dependants and supply evidence where required
  4. `RULE` (benefits administrator) validate each election against plan rules, dependant eligibility and any evidence requirement
  5. `WAIT` (benefits administrator) hold until the window closes, applying defaults for non-responders per policy
  6. `CAPABILITY` (integration system) commit the election revisions and compute the per-period deduction obligations
  7. `CAPABILITY` (external partner or carrier) transmit enrollments to each carrier in the declared order
  8. `OBSERVE` (integration system) reconcile the carrier's returned enrollment file against what was sent
  9. `END` (benefits administrator) close BusinessState at window close, ConsistencyState only when every carrier reconciles
- **Intents**:
  - `hcmnext.benefits.run_open_enrollment/v1` (PROPOSED, creates)
  - `hcmnext.benefits.determine_eligibility/v1` (PROPOSED, creates)
  - `hcmnext.benefits.process_election/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: plan revisions, rates, eligibility rules, prior year elections
  - `payroll`: deduction capacity and pay period calendar
  - `people`: population, employment status, work location
- **Writes**:
  - `benefits`: election revisions, dependant records, deduction obligations
  - `integration`: carrier enrollment operations and their observations
  - `payroll`: per-period deduction lines
- **Evidence required**:
  - Frozen population and eligibility results per plan.
  - Each election with its timestamp against the window boundary.
  - Carrier reconciliation showing sent versus accepted, per subscriber.
- **Failure and repair**:
  - An election arrives after the window closes -> BENEFITS_EXPIRED_WINDOW_BLOCKED; a late election needs a qualifying life event, not an exception
  - Two elections overlap for the same plan and interval -> BENEFITS_OVERLAPPING_ELECTION_BLOCKED
  - The carrier's returned file omits subscribers that were sent -> BENEFITS_CARRIER_RECONCILIATION_DEGRADED with a bounded repair per subscriber; a missing subscriber has no coverage and must not be reported as enrolled
  - Payroll deductions do not match the elected plan costs -> BENEFITS_DEDUCTION_RECONCILIATION_DEGRADED
- **Jurisdiction**:
  - US federal: ERISA requires a summary plan description and, for group health plans, a summary of benefits and coverage before enrollment; the ACA employer mandate requires an offer of minimum essential coverage to full-time employees, evidenced on Forms 1094-C and 1095-C.
  - US state variation: State continuation statutes, sometimes called mini-COBRA, apply to employers below the 20-employee COBRA threshold in states including California, New York, Texas and Massachusetts; several states mandate additional covered benefits that change the plan set by work location.
  - International: In the EU, occupational pension and health arrangements are national and often collectively bargained; an annual election window is not a general concept and the workflow must not assume one exists.

### WF-BEN-002. Process a qualifying life event mid year

- **Status**: `EXISTING` - internal/workflow/conformance/benefits/definition.go terminal BENEFITS_DUPLICATE_LIFE_EVENT_BLOCKED; planning/workflows/leave/catalog.md row ProcessLifeEvent
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: employee, benefits administrator, external partner or carrier, integration system
- **Trigger**: A birth, marriage, divorce or loss of other coverage opens a special enrollment window.
- **Preconditions**:
  - The event type is one the plan recognizes.
  - The event date and the reporting deadline are both known.
  - Evidence requirements for the event type are configured.
- **Steps**:
  1. `TASK` (employee) the employee reports the event with its date and supporting evidence
  2. `RULE` (benefits administrator) check the report against the plan's reporting deadline measured from the event date
  3. `DECISION` (benefits administrator) reject a duplicate report of the same event rather than opening a second window
  4. `CAPABILITY` (benefits administrator) open a bounded special enrollment window with the changes the event permits, not the full plan set
  5. `TASK` (employee) the employee elects within the window
  6. `CAPABILITY` (integration system) commit the election with the effective date the event dictates, which may be retroactive to the event
  7. `CAPABILITY` (external partner or carrier) transmit to the carrier with the retroactive effective date and observe acceptance
  8. `END` (benefits administrator) close only when the carrier confirms the retroactive effective date, not just the enrollment
- **Intents**:
  - `hcmnext.benefits.record_life_event/v1` (PROPOSED, creates)
  - `hcmnext.benefits.process_election/v1` (PROPOSED, advances)
- **Reads**:
  - `benefits`: plan rules for the event type, current elections, dependants
  - `documents`: evidence artifacts in a restricted compartment
  - `people`: marital status and dependant relationships
- **Writes**:
  - `benefits`: life event record, election revision with a retroactive effective date
  - `payroll`: deduction adjustment, possibly retroactive
- **Evidence required**:
  - Event date, report date and the deadline computation between them.
  - Evidence reference and its verification decision.
  - Carrier confirmation of the retroactive effective date.
- **Failure and repair**:
  - The report arrives after the deadline -> REJECT with the deadline named; the next opportunity is open enrollment, which the refusal states
  - The same event is reported twice -> BENEFITS_DUPLICATE_LIFE_EVENT_BLOCKED; a second window would let the employee re-elect after seeing claims
  - The carrier accepts the enrollment but with today's effective date -> DRIFT; retroactive coverage is the whole point of the event and a repair must correct the date
- **Jurisdiction**:
  - US federal: Section 125 cafeteria plan rules permit mid-year changes only on specified events and only changes consistent with the event; HIPAA special enrollment rights apply to loss of coverage, marriage, birth and adoption with a 30-day window, or 60 days for Medicaid and CHIP events.
  - US state variation: State-mandated benefits, such as California's paid family leave interaction with dependant coverage, can change what the event permits; several states extend dependant coverage to age 26 or beyond independently of the ACA.
  - International: In the EU, mid-year changes to occupational schemes follow scheme rules rather than a tax-code concept like Section 125, so the deadline logic is scheme-specific.

### WF-BEN-003. Reconcile a carrier file against local enrollment

- **Status**: `EXISTING` - internal/workflow/conformance/benefits/definition.go terminal BENEFITS_CARRIER_RECONCILIATION_DEGRADED
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: benefits administrator, external partner or carrier, auditor, integration system
  - performing a step: benefits administrator, integration system
  - participating without owning a step: external partner or carrier, auditor
- **Trigger**: A periodic carrier file must be compared with local enrollment to find drift.
- **Preconditions**:
  - The carrier file's as-of date and its completeness marker are known.
  - A matching key exists that does not rely on a Social Security number alone.
  - A drift tolerance policy defines what is material.
- **Steps**:
  1. `OBSERVE` (integration system) ingest the carrier file and record its as-of watermark
  2. `TRANSFORM` (benefits administrator) match subscribers and dependants and classify each difference
  3. `DECISION` (benefits administrator) separate timing differences, where the carrier has not processed a recent change, from real drift
  4. `CAPABILITY` (benefits administrator) produce a repair plan per subscriber for real drift
  5. `APPROVAL` (benefits administrator) benefits approves the repair actions that change coverage
  6. `OBSERVE` (integration system) verify the next file shows the repair applied
- **Intents**:
  - `hcmnext.operations.detect_drift/v1` (REAL, creates)
  - `hcmnext.operations.create_repair_plan/v1` (REAL, creates)
  - `hcmnext.operations.simulate_repair/v1` (REAL, reads)
- **Reads**:
  - `benefits`: local elections and dependants
  - `integration`: carrier file, its schema version and its watermark
- **Writes**:
  - `benefits`: corrected elections after approved repair
  - `operations`: drift findings and repair plan
- **Evidence required**:
  - Carrier file watermark and completeness marker.
  - Per-subscriber difference with its classification.
  - Verification in the following file that the repair landed.
- **Failure and repair**:
  - The carrier file is partial and does not say so -> treat the absent subscribers as UNKNOWN rather than as terminated; assuming absence means termination has cancelled real coverage in production systems
  - A subscriber exists at the carrier but not locally -> open an investigation; an unknown enrollment is a billing and privacy problem, not a record to delete
  - A repair is applied but the next file still disagrees -> escalate to an incident; two failed repairs mean the mapping, not the data, is wrong
- **Jurisdiction**:
  - US federal: ERISA fiduciary duties make an unreconciled enrollment a plan administration failure; incorrect COBRA notices flow from bad enrollment data.
  - US state variation: State mini-COBRA and continuation duties depend on accurate termination dates in the carrier's record, so drift on a termination date has direct statutory consequences.
  - International: In the EU, an incorrect enrollment can breach the works council agreement governing the scheme as well as the contract with the provider.

### WF-BEN-004. Administer COBRA continuation after a qualifying event

- **Status**: `NEW`
- **Archetype**: `A13` | **Complexity tier**: 4
- **Actors**: benefits administrator, employee, compliance officer, external partner or carrier, integration system
- **Trigger**: A termination or hours reduction ends coverage and triggers continuation rights.
- **Preconditions**:
  - The qualifying event and its date are committed.
  - The plan is subject to COBRA or a state continuation statute.
  - The notice deadlines are computed from the event date, not the processing date.
- **Steps**:
  1. `CAPABILITY` (compliance officer) classify the qualifying event and compute the notice and election deadlines
  2. `DOCUMENT` (benefits administrator) generate the election notice with the plan-specific rates and deadlines
  3. `SIGNAL` (external partner or carrier) deliver the notice by a method that produces proof of mailing or delivery
  4. `WAIT` (benefits administrator) hold for the election period, tracking the deadline
  5. `TASK` (employee) the qualified beneficiary elects or declines
  6. `CAPABILITY` (integration system) reinstate coverage retroactively to the loss date on a valid election and premium payment
  7. `END` (benefits administrator) close with the premium-payment obligation recurring monthly until the maximum period ends
- **Intents**:
  - `hcmnext.benefits.administer_continuation/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: coverage at the loss date, plan rates, prior elections
  - `people`: the qualifying event and its date
  - `regulatory`: COBRA or state continuation rule pack
- **Writes**:
  - `benefits`: continuation record, election, premium obligations
  - `documents`: the election notice and its delivery evidence
- **Evidence required**:
  - Notice content and its delivery proof, which is the employer's defence.
  - Deadline computation from the event date.
  - Election or decline record with its timestamp.
- **Failure and repair**:
  - The notice is issued late -> the obligation records the lateness with its exposure; statutory penalties accrue per day per beneficiary and the workflow must not hide the miss
  - The beneficiary elects after the deadline but within the grace period rules -> route to a human decision; the plan administrator has discretion the system should not exercise silently
  - The qualifying event was recorded in error, as in an erroneous termination -> the notice must be retracted in writing and the retraction retained
  - The beneficiary elects but does not pay the first premium within the 45-day window -> coverage is retroactively terminated to the loss date and the termination is transmitted to the carrier; claims paid in the interim become a recovery matter, and the workflow never leaves coverage nominally active on an unpaid election
  - A monthly premium arrives short by less than the plan's insignificant-shortfall threshold -> the payment is accepted and the beneficiary is notified with 30 days to cure; a de minimis underpayment is not a lapse
  - A monthly premium is late past the 30-day grace period -> coverage terminates as of the end of the paid-through period, dated to that period end rather than to the discovery date, and the termination notice is issued and retained
- **Jurisdiction**:
  - US federal: COBRA under ERISA and the IRC requires the employer to notify the plan administrator within 30 days and the administrator to notify the beneficiary within 14 days; the beneficiary then has 60 days to elect and 45 days to pay. Penalties run to 100 dollars per day per beneficiary under IRC 4980B.
  - US state variation: State mini-COBRA covers employers under 20 employees in California, New York, Texas, Massachusetts and about 40 other states, with different maximum durations, commonly 18 to 36 months, and sometimes longer for disability.
  - International: The EU has no COBRA analogue; continuation of occupational health cover after employment is governed by scheme rules and, in some states, by statutory sick pay bridging instead.

### WF-BEN-005. Add or remove a benefits dependant with verification

- **Status**: `PARTIAL` - planning/workflows/leave/catalog.md rows AddDependent and RemoveDependent
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: employee, benefits administrator, external partner or carrier, integration system
- **Trigger**: An employee adds a spouse or child, or removes one after a divorce or age-out.
- **Preconditions**:
  - The dependant relationship type is in the plan's covered set.
  - The verification evidence requirement for the type is configured.
  - For a removal, the resulting continuation rights are computable.
- **Steps**:
  1. `TASK` (employee) the employee submits the dependant with the relationship and supporting evidence
  2. `TASK` (benefits administrator) verification of the evidence against the relationship claim, held in a restricted compartment
  3. `DECISION` (benefits administrator) route an age-out removal to an automatic path with advance notice, and a divorce removal to the continuation path
  4. `CAPABILITY` (integration system) commit the dependant revision with its coverage interval
  5. `CAPABILITY` (external partner or carrier) transmit to the carrier and observe acceptance
  6. `END` (benefits administrator) close on carrier confirmation of the dependant, not of the subscriber
- **Intents**:
  - `hcmnext.benefits.manage_dependant/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: plan dependant rules, age limits, current dependants
  - `documents`: verification evidence
  - `people`: relationship records
- **Writes**:
  - `benefits`: dependant record and its coverage interval
  - `integration`: carrier dependant operation
- **Evidence required**:
  - Verification decision with what was compared, not the document contents.
  - Age-out advance notice where the plan requires it.
  - Carrier confirmation naming the dependant.
- **Failure and repair**:
  - Verification evidence is not supplied within the grace period -> the dependant's coverage is BLOCKED prospectively with notice, not terminated retroactively, which would leave unpaid claims
  - The carrier accepts the subscriber but silently drops the dependant -> DRIFT with a repair; the subscriber-level acknowledgement is not evidence of dependant coverage
  - A removal is entered with a past date after claims were paid -> route to a human decision; retroactive dependant removal creates claim recovery against an individual and needs a deliberate act
- **Jurisdiction**:
  - US federal: The ACA requires dependant child coverage to age 26 without regard to student or marital status; Section 125 limits mid-year dependant changes to consistent events.
  - US state variation: Several states extend dependant eligibility past 26 for unmarried dependants, including New Jersey to 31 and Florida to 30, so the age-out date is not uniform.
  - International: In the EU, dependant coverage under occupational schemes is scheme-defined and family definitions vary, particularly for registered partnerships.

### WF-BEN-006. Determine ACA full-time status over a measurement period

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: benefits administrator, compliance officer, payroll administrator
- **Trigger**: Variable-hour workers must be measured to decide whether an offer of coverage is required.
- **Preconditions**:
  - The measurement, administrative and stability periods are configured.
  - Hours of service for the population are complete for the measurement period.
  - The method, monthly or look-back, is declared per class.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) aggregate hours of service per worker across the measurement period, including paid leave
  2. `RULE` (compliance officer) apply the declared method and threshold to classify each worker
  3. `DECISION` (compliance officer) lock the classification for the stability period; a mid-period hours drop does not change it
  4. `SIGNAL` (benefits administrator) trigger an offer of coverage for every newly full-time worker within the administrative period
  5. `END` (benefits administrator) close with the stability period as an open obligation until it ends
- **Intents**:
  - `hcmnext.benefits.determine_aca_status/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: measurement configuration
  - `people`: employment class and start dates
  - `time`: hours of service including paid leave and special unpaid leave
- **Writes**:
  - `benefits`: ACA status per worker with its stability period
  - `regulatory`: offer obligation and its deadline
- **Evidence required**:
  - The hours aggregation with its source periods.
  - Method and threshold applied, per class.
  - Stability period boundaries, which govern later changes.
- **Failure and repair**:
  - Hours data is incomplete for part of the measurement period -> the classification is UNKNOWN and defaults to the safer treatment of offering coverage rather than the cheaper one
  - A worker's hours fall below the threshold during the stability period -> the classification holds; dropping coverage early is a mandate penalty exposure
  - The method changes between periods -> the change is a configuration intent with its own approval, and the prior period keeps its method
- **Jurisdiction**:
  - US federal: The ACA employer shared responsibility rules in IRC 4980H define full time as 30 hours per week or 130 per month; the look-back safe harbour is in 26 CFR 54.4980H-3, and Forms 1094-C and 1095-C report the result.
  - US state variation: Hawaii's Prepaid Health Care Act imposes a 20-hour-per-week coverage duty that is stricter than the ACA and runs alongside it; some states add their own reporting.
  - International: No analogue outside the US; the workflow is US-only and should declare itself so rather than resolving to a null rule pack elsewhere.

### WF-BEN-007. Enroll a worker in a retirement plan and start deferrals

- **Status**: `NEW`
- **Archetype**: `A1` | **Complexity tier**: 3
- **Actors**: benefits administrator, employee, payroll administrator, external partner or carrier, integration system
  - performing a step: benefits administrator, employee, external partner or carrier, integration system
  - participating without owning a step: payroll administrator
- **Trigger**: A worker becomes eligible for a defined contribution plan, with or without automatic enrollment.
- **Preconditions**:
  - The plan's eligibility rule and entry dates are configured.
  - The automatic enrollment default and its opt-out window, if any, are defined.
  - The recordkeeper connector is healthy.
- **Steps**:
  1. `RULE` (benefits administrator) evaluate eligibility and the entry date from service and age rules
  2. `SIGNAL` (employee) issue the automatic enrollment notice with the default rate and the opt-out deadline
  3. `WAIT` (benefits administrator) hold for the opt-out window before the first deferral
  4. `CAPABILITY` (integration system) commit the deferral election, whether elected or defaulted, and start payroll deductions
  5. `CAPABILITY` (external partner or carrier) transmit the enrollment and each contribution file to the recordkeeper
  6. `OBSERVE` (integration system) confirm each contribution was received and allocated, not merely transmitted
- **Intents**:
  - `hcmnext.benefits.enroll_retirement_plan/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: plan eligibility, entry dates, default rates
  - `payroll`: compensation definition used for deferrals
  - `people`: service history and age
- **Writes**:
  - `benefits`: deferral election and its rate
  - `integration`: recordkeeper contribution operations
  - `payroll`: deduction lines and employer match computation
- **Evidence required**:
  - Automatic enrollment notice and its delivery, which is a fiduciary requirement.
  - Election or default record with the applicable rate.
  - Per-contribution remittance confirmation with the deposit date.
- **Failure and repair**:
  - Contributions are withheld but not remitted within the deposit deadline -> an immediate incident; late deposits are a prohibited transaction with a correction program of their own
  - The compensation definition used for deferrals differs from the plan document -> BLOCKED; a mismatch is an operational failure requiring a formal correction
  - The worker opts out after the first deferral -> refund the deferral within the permissible window and record it as a permissible withdrawal, not a distribution
- **Jurisdiction**:
  - US federal: ERISA requires participant contributions to be deposited as soon as they can reasonably be segregated, and no later than the fifteenth business day of the following month, with the DOL treating small plans' seven-business-day safe harbour as the practical rule; SECURE 2.0 mandates automatic enrollment for most new plans.
  - US state variation: State-run auto-IRA programs in California, Illinois, Oregon, Colorado and others require employers without a plan to register and remit, which is a separate obligation from an ERISA plan.
  - International: The UK requires automatic enrolment with statutory minimum contributions and re-enrolment every three years; the mechanics and deadlines are entirely different from the US ones.

### WF-BEN-008. Administer a health savings or flexible spending account

- **Status**: `NEW`
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: benefits administrator, employee, payroll administrator, external partner or carrier
- **Trigger**: A worker elects an account-based benefit with statutory contribution limits and forfeiture rules.
- **Preconditions**:
  - The account type, its annual limit and its plan year are configured.
  - Eligibility for a health savings account depends on being covered by a qualifying high-deductible plan and by nothing disqualifying.
  - The forfeiture or carryover rule for a flexible spending account is declared before elections open.
- **Steps**:
  1. `RULE` (benefits administrator) evaluate eligibility, which for a health savings account depends on the medical plan elected in the same window
  2. `TASK` (employee) the worker elects an annual amount
  3. `RULE` (benefits administrator) test the election against the statutory limit including any catch-up and any prior-employer contributions the worker discloses
  4. `CAPABILITY` (payroll administrator) commit the election and schedule per-period deductions
  5. `CAPABILITY` (external partner or carrier) remit contributions to the custodian each period and observe posting
  6. `WAIT` (benefits administrator) hold to the plan year end and apply the carryover, grace period or forfeiture rule
  7. `END` (benefits administrator) close the plan year with the forfeited or carried amount recorded per worker
- **Intents**:
  - `hcmnext.benefits.administer_account_benefit/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: account type, limits, medical plan election, prior contributions
  - `payroll`: per-period deduction capacity and year-to-date contributions
  - `people`: age for catch-up eligibility
- **Writes**:
  - `benefits`: account election and its limit test
  - `integration`: custodian remittance operations
  - `payroll`: per-period deduction lines
- **Evidence required**:
  - Limit test showing the statutory maximum, the elected amount and any catch-up applied.
  - Per-period remittance with the custodian's posting confirmation.
  - Year-end forfeiture or carryover determination per worker.
- **Failure and repair**:
  - The worker elects a health savings account while enrolled in a general-purpose flexible spending account -> REJECT; the two are mutually disqualifying and committing both creates a taxable excess contribution the worker must unwind personally
  - Contributions exceed the annual limit because a mid-year hire had a prior employer's contributions -> the excess is detected at the limit test and the election is reduced prospectively; an excess already remitted becomes a correction with the custodian, not a silent adjustment
  - A flexible spending account balance is unspent at the plan year end -> the declared carryover or grace period applies and the remainder is forfeited with the amount recorded per worker, because a forfeiture the worker was never told about is the complaint this generates
- **Jurisdiction**:
  - US federal: IRC 223 sets health savings account eligibility and annual limits with a catch-up from age 55; IRC 125 governs flexible spending accounts, the use-or-lose rule and the permitted carryover or grace period, and an excess contribution is subject to an excise tax under IRC 4973.
  - US state variation: State conformity to the federal treatment is not universal: California and New Jersey do not conform on health savings accounts, so contributions are state-taxable there while being federally excluded, which the payroll treatment must reflect per worker location.
  - International: No equivalent exists outside the US; where a tenant operates in both, the account benefit must be scoped to US employments only rather than offered globally.

### WF-BEN-009. Run plan nondiscrimination testing and correct a failure

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 4
- **Actors**: benefits administrator, compliance officer, payroll administrator, external partner or carrier
- **Trigger**: A plan year closes and the retirement and cafeteria plans must be tested for discrimination in favour of highly compensated employees.
- **Preconditions**:
  - The compensation definition the plan document uses is available and is not assumed to be gross pay.
  - The highly compensated and key employee determinations are computed from the prior year's compensation and ownership data.
  - The correction deadlines and their consequences are configured.
- **Steps**:
  1. `CAPABILITY` (benefits administrator) compute the highly compensated and key employee sets from prior-year compensation and ownership
  2. `CAPABILITY` (external partner or carrier) run the applicable tests: deferral and contribution percentage tests for the retirement plan, eligibility and benefits tests for the cafeteria plan, and the top-heavy test
  3. `DECISION` (compliance officer) on a failure, choose a correction method within its deadline: refund excess deferrals, make a qualified nonelective contribution, or both
  4. `APPROVAL` (compliance officer) the plan fiduciary approves the correction method and its cost
  5. `CAPABILITY` (payroll administrator) execute the correction through payroll and the recordkeeper
  6. `END` (compliance officer) close with the test results and the correction retained as plan records
- **Intents**:
  - `hcmnext.benefits.run_nondiscrimination_test/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: deferrals, contributions and eligibility per worker
  - `compensation`: prior-year compensation under the plan's own definition
  - `people`: ownership and officer status for the key employee test
- **Writes**:
  - `benefits`: test results and the correction records
  - `payroll`: refund or contribution lines from the correction
- **Evidence required**:
  - The compensation definition used, taken from the plan document rather than assumed.
  - Per-test result with its numerator, denominator and threshold.
  - The correction method, its deadline and its execution.
- **Failure and repair**:
  - The compensation definition used for testing differs from the plan document -> the whole test is invalid and must be rerun; this is the single most common cause of a failed plan audit
  - A failure is corrected after the 2.5-month deadline -> the excise tax applies and the exposure is recorded as an obligation rather than absorbed
  - The recordkeeper's test and the platform's disagree -> BLOCKED pending reconciliation; two different answers on a discrimination test cannot both be filed
- **Jurisdiction**:
  - US federal: IRC 401(k)(3) and 401(m) set the deferral and contribution percentage tests, IRC 416 the top-heavy test, and IRC 125 the cafeteria plan tests; a failure corrected more than 2.5 months after the plan year end triggers a 10 percent excise tax under IRC 4979, and the results feed the Form 5500 filing.
  - US state variation: None state-specific; these are federal tax-qualification rules.
  - International: No analogue; occupational schemes in the EU are not tested this way, so the workflow is US-only and must declare itself so rather than resolving to an empty rule pack elsewhere.

### WF-BEN-010. Process a domestic relations order against a retirement plan

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 4
- **Actors**: benefits administrator, compliance officer, employee, external partner or carrier
- **Trigger**: A court order divides a worker's retirement benefit with a former spouse or dependant.
- **Preconditions**:
  - The order names the plan, the participant, the alternate payee and the amount or formula.
  - The plan's qualification procedures for such orders are published.
  - An account restriction can be applied while the order is under review.
- **Steps**:
  1. `TASK` (benefits administrator) receive the order and place a restriction on the participant's account while it is reviewed
  2. `SIGNAL` (employee) notify the participant and the alternate payee that the order was received and that a restriction is in place
  3. `TASK` (compliance officer) determine whether the order is qualified against the plan's published procedures
  4. `DECISION` (compliance officer) reject an order that fails qualification with the specific defect named, so it can be corrected and resubmitted
  5. `CAPABILITY` (external partner or carrier) instruct the recordkeeper to segregate the alternate payee's share
  6. `END` (benefits administrator) close and release the restriction; an indefinite restriction is itself a fiduciary breach
- **Intents**:
  - `hcmnext.benefits.process_domestic_relations_order/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: plan balances, vesting, prior orders
  - `documents`: the order in a restricted compartment
  - `people`: participant identity and the alternate payee's identity
- **Writes**:
  - `benefits`: qualification determination, account restriction, segregation instruction
  - `documents`: notices to both parties
- **Evidence required**:
  - The order held in a restricted compartment, accessible to the plan administrator function only.
  - Notices to both the participant and the alternate payee with their dates.
  - The qualification determination with the plan procedures applied and the restriction's start and end.
- **Failure and repair**:
  - The restriction is left in place after the determination -> an incident; a restriction with no end date deprives the participant of access and is a fiduciary breach in its own right
  - The order is ambiguous about the valuation date -> REJECT as not qualified with the specific defect named; a plan administrator interpreting an ambiguous order substitutes its judgement for the court's
  - The participant's manager requests the order -> DENIED; a domestic relations order is among the most sensitive documents in the system and its existence is not a management fact
- **Jurisdiction**:
  - US federal: ERISA 206(d)(3) and IRC 414(p) define a qualified domestic relations order and require the plan to have written procedures, to notify both parties, and to determine qualification within a reasonable period; the anti-alienation rule otherwise prohibits assigning benefits.
  - US state variation: State domestic relations law produces the order, and community property states, including California, Texas, Arizona and Washington, generate materially different division formulas than equitable distribution states.
  - International: No analogue; pension sharing on divorce in the UK and the EU follows entirely different mechanics and does not use this workflow.

### WF-BEN-011. Administer a short-term or long-term disability claim

- **Status**: `NEW`
- **Archetype**: `A14` | **Complexity tier**: 4
- **Actors**: benefits administrator, employee, external partner or carrier, payroll administrator
- **Trigger**: A worker is unable to work and claims income replacement under a disability policy.
- **Preconditions**:
  - The policy, its elimination period, its benefit percentage and its offsets are configured.
  - The claim is adjudicated by the carrier, not by the employer.
  - The interaction with protected leave and with any state programme is defined.
- **Steps**:
  1. `TASK` (employee) the worker files with the carrier and the employer supplies the employment and earnings verification
  2. `WAIT` (benefits administrator) hold through the elimination period, during which sick leave or paid time off may apply
  3. `OBSERVE` (external partner or carrier) record the carrier's determination and the benefit amount
  4. `RULE` (payroll administrator) compute offsets against any state programme, social security award or employer top-up
  5. `CAPABILITY` (payroll administrator) coordinate payroll: stop or reduce wages, continue benefit deductions, and apply the correct tax treatment
  6. `DECISION` (benefits administrator) keep the claim decision separate from the leave decision; a denied claim does not end job protection
  7. `END` (benefits administrator) close the payroll coordination while the claim and any protected leave continue on their own clocks
- **Intents**:
  - `hcmnext.benefits.administer_disability_claim/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: policy terms, elimination period, offsets
  - `leave`: concurrent protected leave and its entitlement
  - `payroll`: earnings history for the benefit calculation, deduction capacity
- **Writes**:
  - `benefits`: claim reference and its determination
  - `payroll`: wage stop or reduction, deduction handling, tax treatment
- **Evidence required**:
  - The employment and earnings verification the employer supplied, which is the figure the benefit rests on.
  - The carrier determination and the offset computation.
  - The separation of the claim decision from the leave decision, recorded explicitly.
- **Failure and repair**:
  - The carrier denies the claim -> job protection continues on its own statute; the workflow must not end protected leave because the money stopped
  - The premium was employer-paid and the benefit is treated as non-taxable -> REJECT that treatment; an employer-paid premium generally makes the benefit taxable, and the tax treatment follows who paid the premium and how
  - The worker receives both the employer top-up and a state benefit exceeding their regular wage -> cap at the regular wage and record the cap, because overpayment can disqualify the state claim
- **Jurisdiction**:
  - US federal: There is no federal disability income mandate; the tax treatment follows IRC 104 and 105, and an employer-paid premium generally makes the benefit taxable while a post-tax employee-paid premium makes it non-taxable.
  - US state variation: California, New York, New Jersey, Rhode Island and Hawaii operate statutory short-term disability programmes that a private policy must coordinate with rather than duplicate; the coordination rules differ in each.
  - International: In the EU, income replacement during sickness comes through statutory sick pay and social insurance with employer top-up rules set nationally, so a private policy is supplemental rather than primary.

### WF-BEN-012. Administer life and accidental death coverage including imputed income

- **Status**: `NEW`
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: benefits administrator, employee, payroll administrator, external partner or carrier, integration system
- **Trigger**: A worker elects employer-paid and supplemental life cover, and beneficiaries must be recorded.
- **Preconditions**:
  - The employer-paid coverage amount and the supplemental options are configured.
  - The evidence of insurability threshold is known.
  - Beneficiary designations are the worker's own and are not inferred from marital status.
- **Steps**:
  1. `TASK` (employee) the worker elects supplemental coverage and names beneficiaries with their shares
  2. `RULE` (benefits administrator) require evidence of insurability above the guaranteed issue amount and hold that portion pending the carrier's decision
  3. `RULE` (payroll administrator) compute imputed income on employer-paid coverage above the excludable amount using the published age-banded table
  4. `CAPABILITY` (integration system) commit the elections, the pending amount and the imputed income to payroll
  5. `OBSERVE` (external partner or carrier) record the carrier's insurability decision and activate or decline the held portion
  6. `END` (benefits administrator) close with the beneficiary designations retained and the imputed income recurring per period
- **Intents**:
  - `hcmnext.benefits.administer_life_coverage/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: coverage options, guaranteed issue thresholds, prior elections
  - `payroll`: imputed income capacity
  - `people`: age for the imputed income table and for age-banded rates
- **Writes**:
  - `benefits`: coverage elections and beneficiary designations
  - `payroll`: imputed income lines per period
- **Evidence required**:
  - Beneficiary designations with their shares totalling exactly one hundred percent.
  - The imputed income computation with the table version and the worker's age band.
  - The insurability decision and the date the held coverage activated.
- **Failure and repair**:
  - Beneficiary shares do not total one hundred percent -> REJECT rather than normalizing; a beneficiary dispute after a death is the worst possible place to discover a rounding decision
  - Coverage above the guaranteed issue amount is treated as active before the carrier decides -> REJECT; the worker would believe they are covered when they are not, which is the most consequential error this workflow can make
  - Imputed income is omitted for employer-paid coverage above the excludable amount -> the omission is a payroll error correctable through retro adjustment, and the exposure is recorded
- **Jurisdiction**:
  - US federal: IRC 79 excludes the first 50,000 dollars of employer-provided group term life from income and requires imputed income on the excess computed from the Table I age-banded rates; ERISA governs the plan and beneficiary designation disputes are decided under the plan document.
  - US state variation: Community property states can give a spouse an interest in a policy regardless of the designation, and several states have revocation-on-divorce statutes that override a stale designation.
  - International: In the EU, death-in-service cover is usually part of an occupational scheme with nomination rather than binding designation, and the tax treatment is national.

### WF-BEN-013. Migrate a plan to a new carrier and transfer its accumulators

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: benefits administrator, external partner or carrier, employee, finance partner, integration system
  - performing a step: benefits administrator, employee, finance partner, integration system
  - participating without owning a step: external partner or carrier
- **Trigger**: A plan moves to a new carrier and the enrolled population, their coverage and their year-to-date accumulators must move with it.
- **Preconditions**:
  - A carrier change does not reset a plan year, so deductibles and out-of-pocket accumulations carry.
  - The outgoing carrier's runout period continues after the change and claims for prior service dates go to it.
  - Confirmation has to be per record; an aggregate count hides the record that did not arrive.
- **Steps**:
  1. `CAPABILITY` (benefits administrator) assemble the enrolled population with coverage tiers, dependants and year-to-date accumulators as at the change date
  2. `APPROVAL` (finance partner) finance approves the contract and compliance approves the data scope transferred
  3. `CAPABILITY` (integration system) transmit the enrolments and accumulators to the incoming carrier
  4. `OBSERVE` (integration system) confirm receipt per record rather than in aggregate, and hold any record the carrier cannot match
  5. `TASK` (employee) communicate the change, the new identifiers and the runout arrangements to subscribers
  6. `CAPABILITY` (benefits administrator) reconcile the two carriers' positions after the first billing cycle under the new contract
  7. `END` (benefits administrator) close on the reconciliation; held records remain open with their own owners
- **Intents**:
  - `hcmnext.benefits.migrate_plan_carrier/v1` (PROPOSED, creates)
  - `hcmnext.benefits.transfer_plan_accumulators/v1` (PROPOSED, advances)
- **Reads**:
  - `benefits`: the enrolments, tiers, dependants and accumulators
  - `commercial`: both carrier contracts and their runout terms
  - `people`: the subscriber and dependant records the enrolments rest on
- **Writes**:
  - `benefits`: the migrated enrolments and transferred accumulators
  - `regulatory`: the subscriber communications and their delivery
- **Evidence required**:
  - The population and accumulators as at the change date.
  - Per-record receipt confirmation from the incoming carrier.
  - The held records the carrier could not match, with their reasons.
  - The runout period as communicated to subscribers.
- **Failure and repair**:
  - Accumulators reset at the change -> families meet their deductible twice in one plan year and the error surfaces as a claim dispute
  - Receipt is confirmed in aggregate -> a missing record is invisible until a claim is denied
  - A record cannot be matched by the incoming carrier -> it is held rather than enrolled with a zero accumulator, because a zero is a claim about the year rather than an absence of one
  - A discrepancy is found between the two carriers' accumulator figures -> it is resolved against the tenant's own claims data rather than by accepting either carrier's figure
- **Jurisdiction**:
  - US federal: ERISA plan documents govern the change and a mid-year carrier change is a plan amendment for disclosure purposes; the summary of material modifications and the summary plan description both follow from it.
  - US state variation: State insurance law governs the contracts themselves and several states impose continuity-of-care and runout requirements the employer's own arrangements must accommodate.
  - International: No analogue in the same form; European occupational schemes change providers under national supervisory regimes with member consultation duties the US does not impose.

---

## TIM. Time, attendance and scheduling

6 workflows.

### WF-TIM-001. Record a time punch and bridge it to payroll

- **Status**: `EXISTING` - internal/workflow/conformance/time/definition.go workflow hcmnext.workflows.time_punch_timecard_payroll_bridge
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: employee, manager, payroll administrator, integration system
- **Trigger**: A worker clocks in or out at a device, an app or a kiosk.
- **Preconditions**:
  - The worker has an active assignment with a schedule or an open-shift policy.
  - The device or client is registered and its clock skew is measurable.
  - The pay period is open.
- **Steps**:
  1. `CAPABILITY` (employee) accept the punch with its device identity, its local time and its offset
  2. `RULE` (integration system) evaluate clock skew, duplicate detection, DST fold, offline replay and location plausibility
  3. `DECISION` (manager) route a suspicious punch to review rather than rejecting it; a rejected punch is unpaid time
  4. `CAPABILITY` (integration system) commit the punch as an immutable fact and derive the timecard entry
  5. `OBSERVE` (payroll administrator) confirm the payroll bridge ingested the entry for the correct period
  6. `END` (payroll administrator) close only when the bridge confirms ingestion
- **Intents**:
  - `hcmnext.time.record_punch/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: assignment and work location
  - `regulatory`: meal, rest and rounding rules for the jurisdiction
  - `time`: schedule, prior punches, rounding and rule configuration
- **Writes**:
  - `payroll`: hours available to the bridge
  - `time`: punch fact and derived timecard entry
- **Evidence required**:
  - Device identity and measured clock skew.
  - The raw punch, retained separately from any rounded or adjusted value.
  - Bridge ingestion confirmation.
- **Failure and repair**:
  - The device clock is skewed -> TIME_PUNCH_REVIEW_CLOCK_SKEW; the punch is held for review with both the device and server times recorded
  - A punch replays from an offline device after the period locks -> TIME_PUNCH_REJECTED_POST_LOCK_EDIT or TIME_PUNCH_REVIEW_OFFLINE_REPLAY depending on the lock state; the raw punch is retained either way
  - The same punch arrives twice -> TIME_PUNCH_DUPLICATE on the idempotency scope
  - Location evidence is implausible -> TIME_PUNCH_REVIEW_SPOOF_SUSPECTED; the accusation is a review item, never an automatic denial of pay
- **Jurisdiction**:
  - US federal: FLSA requires payment for all hours suffered or permitted to be worked; the Portal-to-Portal Act de minimis doctrine has been narrowed, and 29 CFR 785.48 permits rounding only if it is neutral over time.
  - US state variation: California rejects rounding that systematically disadvantages employees and requires premium pay for missed meal and rest periods under Labor Code 226.7; Oregon and Washington have their own meal-period mechanics; Illinois BIPA makes a fingerprint or face-scan punch a biometric collection requiring prior written consent and a retention schedule.
  - International: In the EU, the CJEU decision in CCOO v Deutsche Bank requires an objective, reliable and accessible record of daily working time, which makes punch retention a legal requirement rather than an operational choice.

### WF-TIM-002. Submit and approve a timecard for a period

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 7 feature timecard_submit
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: employee, manager, payroll administrator, integration system
- **Trigger**: At period end a worker submits their timecard and a manager approves it.
- **Preconditions**:
  - All punches and absences for the period are recorded.
  - Exceptions are either resolved or explicitly accepted.
  - The submission deadline precedes the payroll cutoff.
- **Steps**:
  1. `CAPABILITY` (integration system) compute the timecard totals, premiums and exceptions
  2. `TASK` (employee) the employee reviews, attests and submits
  3. `DECISION` (employee) block submission while an unresolved exception affects pay, and allow it where the exception is informational
  4. `APPROVAL` (manager) the manager approves the exact totals; an edit by the manager reopens the attestation
  5. `CAPABILITY` (payroll administrator) lock the timecard and release it to the payroll bridge
  6. `END` (payroll administrator) close on lock; later changes require a reopen intent
- **Intents**:
  - `hcmnext.time.submit_timecard/v1` (PROPOSED, creates)
  - `hcmnext.work.approve_proposal/v1` (REAL, advances)
- **Reads**:
  - `leave`: approved absence for the period
  - `regulatory`: attestation requirements
  - `time`: punches, absences, schedule, premium rules
- **Writes**:
  - `time`: timecard revision, attestation record, lock state
- **Evidence required**:
  - Employee attestation with what was attested to.
  - Manager approval bound to the exact totals.
  - Every edit with its actor, because an unattested manager edit is an FLSA risk.
- **Failure and repair**:
  - The manager edits after the employee attests -> TIME_PUNCH_REJECTED_STALE_REOPEN unless the employee re-attests; an edited card the employee never saw is the classic wage claim
  - The deadline passes with no submission -> the card auto-submits with an unattested marker and the manager approves; the worker is paid and the missing attestation is the exception
  - The card is reopened after the payroll lock -> REJECT; the correction is a retro adjustment in the next period
- **Jurisdiction**:
  - US federal: FLSA record-keeping under 29 CFR 516.2 requires the hours record; an employer that edits hours without the employee's knowledge loses the presumption its records are accurate.
  - US state variation: California requires the wage statement to show hours and rates and imposes penalties per defective statement; several states require the employee to be able to inspect the underlying time record.
  - International: Under the EU working time recording duty, the record must be objective and reliable, which argues against manager-only edits with no employee visibility.

### WF-TIM-003. Publish a schedule and handle a shift swap

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 7 features schedule_manage and shift_swap_approve
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: manager, employee, compliance officer, integration system
- **Trigger**: A manager publishes a schedule and two workers then agree to swap shifts.
- **Preconditions**:
  - The schedule period and the advance notice requirement are configured.
  - Both workers are qualified for each other's shift.
  - Overtime and rest-period implications are computable before approval.
- **Steps**:
  1. `CAPABILITY` (manager) publish the schedule and record the publication instant against the notice requirement
  2. `TASK` (employee) worker A offers a shift and worker B accepts
  3. `RULE` (compliance officer) validate qualifications, rest periods, overtime exposure and any predictive scheduling premium the swap triggers
  4. `DECISION` (manager) route to manager approval when the swap creates overtime or a premium, and auto-approve when it does not
  5. `CAPABILITY` (integration system) commit both shift assignment revisions atomically; a half-completed swap leaves a shift uncovered
  6. `SIGNAL` (employee) notify both workers and update the published schedule
- **Intents**:
  - `hcmnext.time.publish_schedule/v1` (PROPOSED, creates)
  - `hcmnext.time.swap_shift/v1` (PROPOSED, creates)
- **Reads**:
  - `qualification`: certifications required by each shift
  - `regulatory`: predictive scheduling and rest rules
  - `time`: schedule, shifts, worked hours to date
- **Writes**:
  - `payroll`: premium exposure where the swap triggers one
  - `time`: shift assignment revisions for both workers
- **Evidence required**:
  - Publication instant against the notice requirement.
  - Both workers' consent to the swap, since an employer-imposed change has different consequences.
  - Premium determination with its rule version.
- **Failure and repair**:
  - The swap creates a rest-period violation -> REJECT with the specific rule and interval named
  - Only one side of the swap commits -> atomic commit prevents it; if a downstream system applies one side only, the drift is a repair with an uncovered shift as a named risk
  - The swap happens after the notice window closes -> an employee-initiated swap generally does not trigger predictive scheduling premiums, but the workflow records who initiated it because that fact is what decides
- **Jurisdiction**:
  - US federal: FLSA counts hours in the workweek regardless of who arranged the shift; a swap that pushes a worker over 40 hours creates overtime the employer owes.
  - US state variation: Predictive scheduling ordinances in San Francisco, Seattle, New York City, Philadelphia, Chicago and Oregon statewide require advance notice and premium pay for employer-initiated changes, with employee-initiated swaps usually exempt; the initiator field is therefore legally load-bearing.
  - International: In the EU the Working Time Directive's 11-hour daily rest is a hard floor a swap cannot breach, and many collective agreements add stricter limits.

### WF-TIM-004. Detect and resolve time exceptions before payroll

- **Status**: `EXISTING` - planning/workflows/discovery-backlog.md time rows DetectTimeException and ResolveTimeException
- **Archetype**: `A18` | **Complexity tier**: 3
- **Actors**: manager, employee, payroll administrator, integration system
  - performing a step: manager, payroll administrator, integration system
  - participating without owning a step: employee
- **Trigger**: Missing punches, unapproved overtime and missed meal periods must be cleared before the payroll cutoff.
- **Preconditions**:
  - Exception rules are configured with their severity and their pay impact.
  - The cutoff date is known and the exception queue is prioritized against it.
  - The resolution paths for each exception type are defined.
- **Steps**:
  1. `CAPABILITY` (integration system) evaluate the period's records against the exception rules
  2. `TRANSFORM` (payroll administrator) prioritize by pay impact and by proximity to the cutoff
  3. `TASK` (manager) the manager resolves each exception with a reason, and the employee confirms where the resolution changes their pay
  4. `DECISION` (payroll administrator) apply the jurisdiction's default where an exception is unresolved at cutoff, and record that a default was applied
  5. `CAPABILITY` (integration system) commit the resolutions and release the period
  6. `END` (payroll administrator) close with unresolved exceptions listed rather than absorbed
- **Intents**:
  - `hcmnext.time.resolve_exception/v1` (PROPOSED, creates)
- **Reads**:
  - `leave`: absences that explain a missing punch
  - `regulatory`: meal, rest and overtime rules
  - `time`: punches, schedule, exception rules
- **Writes**:
  - `payroll`: hours and premiums released to the run
  - `time`: exception resolutions with their reasons
- **Evidence required**:
  - Each exception with its rule, its pay impact and its resolution.
  - Employee confirmation where the resolution reduces pay.
  - The defaults applied at cutoff, listed explicitly.
- **Failure and repair**:
  - A missing clock-out is defaulted to the scheduled end -> permitted only where the policy says so, and the default is recorded so an employee claim can be tested against it
  - A missed meal period is resolved as waived -> requires the waiver on file for that jurisdiction; without it the premium is owed and the resolution is REJECTED
  - Exceptions remain unresolved at cutoff -> the run proceeds with defaults and the unresolved list becomes an obligation reviewed in the next period
- **Jurisdiction**:
  - US federal: Under the FLSA the employer must pay for hours it knows or should know were worked; an unresolved missing punch resolved downward is the fact pattern behind many collective actions.
  - US state variation: California requires a one-hour premium for each missed meal and each missed rest period, capped at one of each per day, and Labor Code 512 waivers are valid only in defined circumstances; Oregon and Washington have their own premium mechanics.
  - International: In the EU, a missed rest break is a working time breach with regulatory rather than premium-pay consequences, so the resolution vocabulary differs by region.

### WF-TIM-005. Allocate labor cost across projects and cost centres

- **Status**: `EXISTING` - internal/domains/labor package: closed labor-cost dimension vocabulary
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: payroll administrator, finance partner, manager, integration system
- **Trigger**: Worked hours must be attributed to projects, cost centres or grants for costing and billing.
- **Preconditions**:
  - The allocation dimensions and their valid values are published.
  - Hours are final for the period.
  - The allocation must total exactly one hundred percent per worker per period.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) read the period's hours and the worker's default allocation
  2. `TASK` (manager) the worker or manager overrides the allocation where the work differed from the default
  3. `RULE` (finance partner) validate that every dimension value is in the published vocabulary and that allocations sum exactly
  4. `CAPABILITY` (integration system) commit the allocation and produce the labor cost postings
  5. `OBSERVE` (finance partner) reconcile the postings against the payroll totals
- **Intents**:
  - `hcmnext.time.calculate_labor_allocation/v1` (PROPOSED, creates)
- **Reads**:
  - `labor`: dimension vocabulary and its version
  - `payroll`: period cost to allocate
  - `time`: hours by day and by activity
- **Writes**:
  - `labor`: allocation records per worker per period
  - `paygl`: labor cost postings
- **Evidence required**:
  - Allocation with its dimension vocabulary version.
  - Exact-sum validation result.
  - Reconciliation between allocated cost and payroll cost.
- **Failure and repair**:
  - An allocation uses a dimension value that has since been retired -> REJECT with the retired value named; an opaque string is not a valid dimension
  - Allocations do not sum to one hundred percent -> REJECT rather than normalizing, which would silently move cost
  - A grant's allowable cost rules exclude a component of the pay -> the component is allocated to the default centre and the exclusion is recorded, not netted away
- **Jurisdiction**:
  - US federal: For federal grant recipients, 2 CFR 200.430 requires that charges to federal awards be based on records that accurately reflect the work performed, which makes the allocation an auditable record rather than an estimate.
  - US state variation: State grant and prevailing wage programs, including Davis-Bacon-covered work under state analogues, require certified payroll with allocation detail per project.
  - International: In the EU, grant-funded research under Horizon programmes requires time recording per action with declarations that mirror the same exact-sum requirement.

### WF-TIM-006. Correct a locked timecard after payroll

- **Status**: `EXISTING` - planning/workflows/discovery-backlog.md time row CorrectTimePunch; internal/workflow/conformance/time/definition.go terminal TIME_PUNCH_REJECTED_POST_LOCK_EDIT
- **Archetype**: `A5` | **Complexity tier**: 3
- **Actors**: employee, manager, payroll administrator, integration system
- **Trigger**: A worker reports that hours were wrong in a period that has already been paid.
- **Preconditions**:
  - The period is locked and its run is released.
  - The claimed correct hours are supported by evidence or by the manager's attestation.
  - The retro adjustment path is available.
- **Steps**:
  1. `TASK` (employee) the worker submits the correction claim with the specific dates and hours
  2. `TASK` (manager) the manager reviews and attests to the corrected hours
  3. `DECISION` (payroll administrator) REJECT an edit to the locked timecard itself and route to an adjustment that references it
  4. `CAPABILITY` (integration system) commit an adjustment record that leaves the original timecard intact
  5. `CAPABILITY` (payroll administrator) create the retro payroll child intent for the delta
  6. `OBSERVE` (payroll administrator) confirm the retro paid in the next run and that the accumulators moved
- **Intents**:
  - `hcmnext.time.correct_locked_period/v1` (PROPOSED, creates)
  - `hcmnext.payroll.adjust_retroactive/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: the released run and its statements
  - `time`: locked timecard, punches, exception history
- **Writes**:
  - `payroll`: retro lines in the next run
  - `time`: adjustment record referencing the locked card
- **Evidence required**:
  - The original locked timecard, unaltered.
  - The manager's attestation to the corrected hours.
  - The retro payment observation.
- **Failure and repair**:
  - The correction spans a prior tax year -> the retro is paid in the current year and reported in the year paid; the prior year's statement is not restated
  - The manager disputes the claimed hours -> the dispute is a case, not a silent denial; the worker gets a decision with a reason
  - Multiple corrections stack for the same period -> each is a separate adjustment referencing the same locked card; they are not merged into one revised card
- **Jurisdiction**:
  - US federal: FLSA back wages plus potential liquidated damages attach to unpaid hours; the two-year statute extends to three for willful violations, which makes the attestation trail material.
  - US state variation: California allows a three-year claim window extended to four under the unfair competition statute, and waiting-time penalties can attach if the corrected hours were part of a final pay; New York allows six years.
  - International: In the UK, unlawful deduction from wages claims run from the last in a series of deductions with a two-year backstop, so a stacked correction has a different limitation shape.

---

## LVE. Leave and absence

8 workflows.

### WF-LVE-001. Request, certify and start a medical leave

- **Status**: `EXISTING` - planning/workflows/leave/leave-return-to-work.md; planning/workflows/leave/catalog.md rows RequestLeave, CertifyLeave and StartLeave; planning/user-flows/reference/medical-leave-and-return.md
- **Archetype**: `A12` | **Complexity tier**: 4
- **Actors**: employee, manager, HR business partner, benefits administrator, integration system
- **Trigger**: An employee requests leave for their own serious health condition.
- **Preconditions**:
  - The employee's eligibility inputs, tenure and hours, are computable at the leave start.
  - A protected evidence compartment exists so the manager never sees the medical certification.
  - The applicable leave programs and their interaction rules are resolved.
- **Steps**:
  1. `TASK` (employee) the employee submits the request with dates and a reason category, not a diagnosis
  2. `RULE` (HR business partner) evaluate eligibility and entitlement across every applicable program and compute how they run concurrently
  3. `DOCUMENT` (HR business partner) issue the eligibility and rights notice and the certification request with its statutory deadline
  4. `WAIT` (HR business partner) hold for the certification, tracking the deadline
  5. `TASK` (HR business partner) the certification is received into the protected compartment and reviewed by HR only
  6. `DECISION` (HR business partner) route an incomplete certification to a cure period rather than to a denial
  7. `CAPABILITY` (integration system) approve the leave, commit the absence interval and set the entitlement consumption
  8. `PARALLEL` (benefits administrator) adjust pay, continue benefits with the employee's premium share, suspend access as policy requires
  9. `SIGNAL` (manager) notify the manager of the absence dates and the return date only, never the reason
- **Intents**:
  - `hcmnext.leave.request_leave/v1` (PROPOSED, creates)
  - `hcmnext.leave.determine_eligibility/v1` (PROPOSED, creates)
  - `hcmnext.leave.certify_leave/v1` (PROPOSED, advances)
- **Reads**:
  - `benefits`: coverage and premium share during leave
  - `leave`: programs, entitlements, prior usage in the leave year
  - `people`: tenure, hours of service, worksite headcount within 75 miles
  - `regulatory`: federal, state and local leave rule packs
- **Writes**:
  - `benefits`: continuation record
  - `leave`: leave case, entitlement consumption, absence intervals
  - `payroll`: pay treatment for the absence
- **Evidence required**:
  - Eligibility and rights notice with its issue date, because the deadline is statutory.
  - Certification held in a compartment with an access log.
  - Concurrency determination showing which programs ran together.
- **Failure and repair**:
  - The certification is incomplete or insufficient -> issue a cure notice with the statutory period; a denial without a cure period is itself a violation
  - The manager attempts to read the certification -> DENY with a named refusal; the manager sees dates and a return date only
  - The employee is ineligible for the federal program but eligible for a state one -> the leave proceeds under the state program with its own entitlement, and the notice states which program applies
- **Jurisdiction**:
  - US federal: FMLA requires 12 months of service, 1,250 hours and a 50-employee-within-75-miles worksite test; the eligibility notice is due within five business days and the certification deadline is 15 calendar days under 29 CFR 825.305. Medical information must be kept in a file separate from the personnel file under the ADA.
  - US state variation: California's CFRA has a five-employee threshold and does not require the 1,250 hours test, and runs concurrently with FMLA except for pregnancy disability, which stacks; New York, New Jersey, Massachusetts, Washington, Colorado, Connecticut, Oregon and others operate paid family and medical leave programs with their own contribution, eligibility and benefit mechanics.
  - International: In the UK, statutory sick pay and family leaves have entirely separate qualification rules; in Germany, Entgeltfortzahlung provides six weeks of employer-paid sick pay before statutory sickness benefit begins.

### WF-LVE-002. Evaluate and manage a return to work with restrictions

- **Status**: `EXISTING` - planning/workflows/leave/leave-return-to-work.md; planning/workflows/leave/catalog.md row EvaluateReturnToWork
- **Archetype**: `A12` | **Complexity tier**: 4
- **Actors**: employee, HR business partner, manager, compliance officer, integration system
  - performing a step: HR business partner, manager, compliance officer, integration system
  - participating without owning a step: employee
- **Trigger**: A worker's clinician clears them to return with restrictions.
- **Preconditions**:
  - An active leave case exists with an expected return date.
  - The essential functions of the role are documented.
  - An interactive process path is available.
- **Steps**:
  1. `TASK` (HR business partner) the return-to-work certification with restrictions is received into the protected compartment
  2. `TRANSFORM` (HR business partner) compare the restrictions against the documented essential functions
  3. `DECISION` (compliance officer) route to the interactive accommodation process when a restriction touches an essential function
  4. `TASK` (HR business partner) the interactive process with the employee, recording each option considered and why it was accepted or rejected
  5. `APPROVAL` (compliance officer) the accommodation decision is approved by an authority independent of the manager
  6. `CAPABILITY` (integration system) commit the work restriction, the accommodation and the return date
  7. `SIGNAL` (manager) tell the manager the restrictions in functional terms only, without the medical basis
  8. `END` (HR business partner) close the leave case with the accommodation as an open obligation with a review date
- **Intents**:
  - `hcmnext.leave.evaluate_return_to_work/v1` (PROPOSED, creates)
  - `hcmnext.leave.request_accommodation/v1` (PROPOSED, creates)
- **Reads**:
  - `leave`: leave case, entitlement remaining
  - `people`: job profile and its essential functions
  - `talent`: performance expectations that a restriction affects
- **Writes**:
  - `leave`: work restriction record, accommodation decision, return date
  - `people`: assignment adjustments the accommodation requires
- **Evidence required**:
  - Each accommodation option considered, with the reason it was accepted or rejected.
  - The functional restriction as told to the manager, separate from the medical certification.
  - Review date for the accommodation.
- **Failure and repair**:
  - No accommodation is possible without undue hardship -> the hardship analysis is recorded with its cost and operational basis; an undocumented undue hardship claim is the weakest position an employer can take
  - The manager requests the medical basis -> DENY; the manager receives functional restrictions only
  - The employee refuses the offered accommodation -> the refusal is recorded and the process continues; a refusal does not automatically end the obligation
- **Jurisdiction**:
  - US federal: The ADA requires an interactive process and a reasonable accommodation absent undue hardship; medical information must be kept confidential and separate under 29 CFR 1630.14. FMLA requires restoration to the same or an equivalent position.
  - US state variation: California's FEHA imposes a broader accommodation duty than the ADA, covers more conditions, and makes the interactive process itself an independently actionable duty; New York City's Human Rights Law is broader still and requires a cooperative dialogue with written conclusion.
  - International: The EU Equal Treatment Framework Directive requires reasonable accommodation for disability; several member states add a statutory phased-return mechanism, such as the German Wiedereingliederung.

### WF-LVE-003. Track intermittent leave usage against an entitlement

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 4
- **Actors**: employee, manager, HR business partner, payroll administrator, integration system
  - performing a step: employee, HR business partner, payroll administrator, integration system
  - participating without owning a step: manager
- **Trigger**: An approved intermittent leave is drawn down in increments over months.
- **Preconditions**:
  - An approved intermittent leave case exists with a certified frequency and duration.
  - The entitlement is expressed in the same unit as the increments used.
  - The leave year method is configured.
- **Steps**:
  1. `TASK` (employee) the employee reports each absence increment, or the manager records it from the time system
  2. `RULE` (HR business partner) test each increment against the certified frequency and duration and against the remaining entitlement
  3. `DECISION` (HR business partner) flag a pattern that exceeds the certification for recertification rather than denying the increment
  4. `CAPABILITY` (integration system) commit the increment, decrement the entitlement and mark the absence as protected
  5. `SIGNAL` (payroll administrator) tell payroll which increments are protected so no attendance penalty attaches
  6. `END` (HR business partner) close only when the entitlement is exhausted or the leave year rolls
- **Intents**:
  - `hcmnext.leave.track_intermittent_usage/v1` (PROPOSED, creates)
- **Reads**:
  - `leave`: certification, entitlement remaining, prior increments
  - `regulatory`: leave year method and minimum increment rules
  - `time`: absence records for the increments
- **Writes**:
  - `leave`: increment records and entitlement consumption
  - `payroll`: pay treatment per increment
  - `time`: absence marked protected
- **Evidence required**:
  - Each increment with its date, its duration and its entitlement effect.
  - Recertification requests and their responses.
  - The protected marking that shields the absence from attendance policy.
- **Failure and repair**:
  - An increment exceeds the certified frequency -> request recertification with the pattern described; denying the increment first invites an interference claim
  - The entitlement is exhausted mid absence -> the remainder is unprotected and the workflow says so explicitly, so the manager applies the ordinary attendance policy knowingly
  - An attendance point is assessed against a protected increment -> the point is reversed automatically and the incident is logged; unreversed points are the most common FMLA interference finding
- **Jurisdiction**:
  - US federal: FMLA permits intermittent leave for a serious health condition and requires the employer to track it in increments no greater than the shortest period used for other leave, under 29 CFR 825.205; the leave year method must be applied uniformly.
  - US state variation: State paid leave programs often pay in daily or weekly increments even where the job protection is hourly, which creates two different ledgers for the same absence; California, Washington and Massachusetts differ on this.
  - International: In the EU, intermittent sickness absence is usually a matter of statutory sick pay rules and occupational health referral rather than an entitlement ledger.

### WF-LVE-004. Administer a state paid family leave claim alongside job protection

- **Status**: `NEW`
- **Archetype**: `A14` | **Complexity tier**: 4
- **Actors**: employee, HR business partner, payroll administrator, external partner or carrier, integration system
  - performing a step: HR business partner, payroll administrator, external partner or carrier, integration system
  - participating without owning a step: employee
- **Trigger**: A worker claims wage replacement from a state programme while taking protected leave.
- **Preconditions**:
  - The state programme applies to the work location and the worker has contributed.
  - The claim channel is either the state agency or a private plan carrier.
  - The interaction with employer-paid leave, topping up or offsetting, is configured.
- **Steps**:
  1. `CAPABILITY` (HR business partner) determine the applicable programme from the work location and the leave reason
  2. `TASK` (external partner or carrier) the employee files with the agency or carrier; the employer responds to the verification request
  3. `RULE` (payroll administrator) compute the interaction between the state benefit, employer paid leave and any accrued balance
  4. `DECISION` (HR business partner) refuse to require the employee to exhaust accrued leave where the state forbids it
  5. `OBSERVE` (integration system) record the agency's determination and its benefit amount
  6. `CAPABILITY` (payroll administrator) adjust payroll for the offset or top-up and continue benefits
  7. `END` (HR business partner) close when the claim period ends; the job protection may outlast the benefit
- **Intents**:
  - `hcmnext.leave.administer_paid_family_leave/v1` (PROPOSED, creates)
- **Reads**:
  - `leave`: leave case, protected interval
  - `payroll`: wage history for the benefit calculation, accrued balances
  - `regulatory`: state programme rules and offset policy
- **Writes**:
  - `leave`: benefit determination recorded against the case
  - `payroll`: offset or top-up lines
- **Evidence required**:
  - Employer verification response to the agency, with its date.
  - Benefit determination and amount from the agency or carrier.
  - The offset computation, so the employee can see why their net changed.
- **Failure and repair**:
  - The agency denies the claim -> job protection continues if it rests on a separate statute; the workflow must not end the protected leave because the money stopped
  - The employer requires accrued leave to be used where the state forbids it -> REJECT the configuration at evaluation time with the statute named
  - The benefit and the employer top-up together exceed the worker's regular wage -> cap at the regular wage and record the cap, because over-payment can disqualify the claim
- **Jurisdiction**:
  - US federal: There is no federal paid leave programme; FMLA provides unpaid job protection only, and the interaction between paid state benefits and FMLA runs through 29 CFR 825.207.
  - US state variation: California SDI and PFL, New York PFL, New Jersey FLI and TDI, Rhode Island TCI, Washington PFML, Massachusetts PFML, Connecticut, Colorado, Oregon, Maryland, Delaware, Minnesota and Maine each have separate contribution rates, benefit formulas, waiting periods and rules on whether accrued leave may be required or coordinated; the differences are material, not cosmetic. The District of Columbia is the outlier a fifty-state rule pack misses: its Universal Paid Leave programme is funded entirely by an employer payroll tax with no employee contribution, so the deduction and offset logic every state programme needs does not apply there.
  - International: Statutory sickness and parental benefits in the EU are paid by social insurance with employer top-up rules set nationally and often by collective agreement.

### WF-LVE-005. Handle a leave that exhausts entitlement without a return

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 5
- **Actors**: HR business partner, compliance officer, manager, employee
  - performing a step: HR business partner, compliance officer, employee
  - participating without owning a step: manager
- **Trigger**: Protected leave runs out and the worker is still unable to return.
- **Preconditions**:
  - The entitlement is exhausted and the exhaustion date is documented.
  - Whether any further leave is available as an accommodation is an open question.
  - The separation path, if reached, has its own approvals.
- **Steps**:
  1. `CAPABILITY` (HR business partner) confirm the exhaustion date across every applicable programme, not just the federal one
  2. `SIGNAL` (employee) notify the employee in writing that protection has ended and what options remain
  3. `TASK` (compliance officer) run the interactive process to consider additional leave as a reasonable accommodation
  4. `DECISION` (compliance officer) an inflexible maximum leave policy is not a defence; each case is individually assessed
  5. `APPROVAL` (compliance officer) any separation requires HR, compliance and legal approval with the accommodation analysis attached
  6. `SUBWORKFLOW` (HR business partner) route an approved separation to the termination workflow, which carries its own obligations
  7. `END` (compliance officer) close with the accommodation analysis retained regardless of the outcome
- **Intents**:
  - `hcmnext.leave.handle_entitlement_exhaustion/v1` (PROPOSED, creates)
  - `hcmnext.people.end_employment/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: coverage and its continuation trigger
  - `leave`: entitlements consumed across programmes, certifications
  - `people`: employment status and tenure
- **Writes**:
  - `leave`: exhaustion record and accommodation analysis
  - `people`: separation, if approved, through the termination workflow
- **Evidence required**:
  - Exhaustion computation per programme with its dates.
  - The individualized accommodation analysis, including any undue hardship reasoning.
  - The written notice to the employee and its delivery.
- **Failure and repair**:
  - The employer applies an automatic termination at exhaustion -> FAIL_CLOSED; automatic separation at the end of leave is the single most litigated ADA failure and the workflow refuses to encode it
  - Further leave is granted as an accommodation with no end date -> BLOCKED; an indefinite leave is not a reasonable accommodation, and the workflow requires a defined review date instead
  - The worker's condition qualifies for a state programme the employer overlooked -> the exhaustion date recomputes and any separation already in flight is REPLANNED
- **Jurisdiction**:
  - US federal: The EEOC's position is that leave beyond FMLA can be a reasonable accommodation and that inflexible maximum-leave policies violate the ADA; the analysis must be individualized and documented.
  - US state variation: California FEHA and New York City's Human Rights Law both require more than the ADA baseline and treat the failure to engage as an independent violation; Massachusetts and Washington add their own duties.
  - International: In the UK, dismissal for long-term sickness requires a fair procedure including up-to-date medical evidence and consideration of adjustments, or the dismissal is unfair regardless of the absence length.

### WF-LVE-006. Accrue, carry over and pay out a leave balance

- **Status**: `EXISTING` - internal/domains/balance package; planning/workflows/leave/catalog.md row CalculateTimeBalance
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: payroll administrator, employee, HR business partner, integration system
- **Trigger**: Accrual runs, a carryover cap applies at year end, and a balance is paid out on separation.
- **Preconditions**:
  - The accrual rule, its cap and its carryover policy are configured per jurisdiction.
  - The plan year boundary and its time zone are defined.
  - Whether the balance is a wage on separation is determined by jurisdiction.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) compute accrual for the period from hours worked or from a flat grant
  2. `RULE` (payroll administrator) apply the accrual cap and the carryover rule at the year boundary
  3. `DECISION` (HR business partner) refuse a use-it-or-lose-it forfeiture where the jurisdiction treats the balance as earned wages
  4. `CAPABILITY` (integration system) commit the balance revision with its effective date
  5. `SIGNAL` (employee) show the employee the balance and the carryover consequence before the boundary, not after
  6. `END` (payroll administrator) on separation, route the balance to the final pay workflow where payout is required
- **Intents**:
  - `hcmnext.leave.calculate_balance/v1` (PROPOSED, creates)
- **Reads**:
  - `leave`: accrual rules, prior balance, usage
  - `regulatory`: forfeiture and payout rules by jurisdiction
  - `time`: hours worked driving accrual
- **Writes**:
  - `leave`: balance revision, carryover or forfeiture record
  - `payroll`: payout line on separation where required
- **Evidence required**:
  - Accrual computation with its rule version.
  - Carryover or forfeiture decision with the jurisdiction rule cited.
  - Advance notice to the employee before a forfeiture boundary.
- **Failure and repair**:
  - A forfeiture is attempted in a jurisdiction that forbids it -> REJECT with the statute named; the balance carries over instead
  - Accrual is computed on hours that were later corrected -> the balance recomputes and the delta is an adjustment, not a restatement
  - The plan year boundary crosses a time zone -> the boundary uses the tenant's declared zone and the decision records it, because an hour's difference can forfeit a day
- **Jurisdiction**:
  - US federal: The FLSA does not require paid leave and does not regulate accrual; the entire subject is state and contract law.
  - US state variation: California treats accrued vacation as earned wages that cannot be forfeited, permits a reasonable accrual cap, and requires payout on separation; Colorado reached the same conclusion in Nieto v Clark's Market; Massachusetts, Illinois, Nebraska, North Dakota and Rhode Island require payout; Texas, Georgia and Florida leave it to policy; separately, state and local paid sick leave laws in over twenty jurisdictions mandate accrual rates and carryover independent of vacation.
  - International: The EU Working Time Directive guarantees four weeks of paid annual leave that generally cannot be replaced by payment except on termination, and CJEU case law limits carryover forfeiture where the worker was unable to take the leave.

### WF-LVE-007. Administer military leave and reemployment rights

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 4
- **Actors**: employee, HR business partner, payroll administrator, benefits administrator
- **Trigger**: A worker leaves for uniformed service and later returns with statutory reemployment rights.
- **Preconditions**:
  - Advance notice from the worker is not required to be in writing and its absence does not forfeit the rights.
  - There is no tenure or hours threshold, unlike ordinary protected leave.
  - The cumulative service limit and the reemployment application deadlines are computed from the service length.
- **Steps**:
  1. `TASK` (employee) the worker gives notice of service; the notice may be oral and its form does not gate the rights
  2. `RULE` (HR business partner) compute the cumulative service used against the five-year limit and its exceptions
  3. `CAPABILITY` (benefits administrator) commit the leave, continue health coverage on the statutory terms and record the pension service that will be credited on return
  4. `WAIT` (HR business partner) hold for the service period; the reemployment application deadline scales with its length
  5. `TASK` (employee) the worker applies for reemployment within the applicable deadline
  6. `DECISION` (HR business partner) reemploy into the position the worker would have attained had service not intervened, not merely the position they left
  7. `CAPABILITY` (payroll administrator) restore pension service, seniority and the pay progression that would have accrued
  8. `END` (HR business partner) close with the discharge-protection period running from the reemployment date
- **Intents**:
  - `hcmnext.leave.administer_military_leave/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: health coverage continuation terms and pension service crediting
  - `compensation`: the pay progression that would have applied
  - `people`: service history, prior military leave, position progression
- **Writes**:
  - `benefits`: restored pension service
  - `compensation`: restored pay progression
  - `leave`: military leave record and cumulative service used
  - `people`: reemployment into the escalator position
- **Evidence required**:
  - Notice record, including that an oral notice was accepted.
  - Cumulative service computation against the five-year limit.
  - The escalator determination showing the position the worker would have attained.
  - The discharge-protection period and its expiry.
- **Failure and repair**:
  - The worker is reemployed into the position they left rather than the one they would have attained -> REJECT; the escalator principle is the core of the statute and reinstatement to the old position is a violation
  - The worker is dismissed without cause inside the discharge-protection period -> FAIL_CLOSED; the protection period runs from 180 days to one year depending on service length and the dismissal requires cause
  - The worker gave no written notice -> the rights stand; requiring a written notice as a condition is itself unlawful
- **Jurisdiction**:
  - US federal: USERRA gives reemployment rights with no minimum tenure or hours, a cumulative five-year service limit with substantial exceptions, an escalator principle for the position and seniority, health coverage continuation for up to 24 months at COBRA-like rates, pension service crediting, and discharge protection for 180 days or one year after reemployment depending on service length.
  - US state variation: Many states supplement USERRA for state National Guard service under state call-up, and several give broader rights or cover service USERRA does not reach.
  - International: No analogue; reserve service protections in the EU are national and much narrower.

### WF-LVE-008. Administer jury duty, bereavement and voting leave

- **Status**: `NEW`
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: employee, manager, payroll administrator, compliance officer
- **Trigger**: A worker takes a short statutory or policy leave whose pay treatment and evidence rules differ by jurisdiction.
- **Preconditions**:
  - The leave type maps to a jurisdiction rule rather than to a single company policy.
  - The pay treatment, paid or unpaid, is determined by the rule and not by the manager.
  - The evidence the employer may require is limited by statute in several jurisdictions.
- **Steps**:
  1. `TASK` (employee) the worker requests the leave with its dates and its type
  2. `RULE` (compliance officer) resolve the applicable rule from the work jurisdiction and the leave type
  3. `DECISION` (compliance officer) refuse to require evidence the jurisdiction does not permit the employer to demand
  4. `APPROVAL` (manager) the manager acknowledges rather than approves where the leave is a statutory right
  5. `CAPABILITY` (payroll administrator) commit the absence with its pay treatment and shield it from attendance policy
  6. `END` (manager) close; the absence is protected and cannot generate an attendance penalty
- **Intents**:
  - `hcmnext.leave.administer_statutory_short_leave/v1` (PROPOSED, creates)
- **Reads**:
  - `leave`: leave types, jurisdiction rules, prior usage
  - `people`: work location driving the applicable rule
  - `time`: schedule for the requested dates
- **Writes**:
  - `leave`: absence record with its pay treatment and protected marking
  - `payroll`: paid or unpaid treatment per the rule
- **Evidence required**:
  - The rule resolution with its jurisdiction and version.
  - The manager's acknowledgement, distinguished from an approval.
  - The protected marking that shields the absence from attendance policy.
- **Failure and repair**:
  - A manager declines a statutory jury duty leave -> REJECT the decline; the leave is a right and the manager's action is an acknowledgement, not a decision
  - An attendance point is assessed against a protected absence -> the point is reversed automatically and the incident is logged
  - The employer demands proof of a family member's death beyond what the jurisdiction permits -> the requirement is refused at configuration; over-collection here is both unlawful in some states and gratuitously cruel
- **Jurisdiction**:
  - US federal: No federal jury duty, bereavement or voting leave exists for private employers; the Jury Systems Improvement Act protects federal jurors from discharge, and there is no federal pay requirement.
  - US state variation: Jury duty pay is required in Alabama, Colorado, Connecticut, Georgia, Louisiana, Massachusetts, Nebraska, New York, Tennessee and the District of Columbia on varying terms; bereavement leave is mandated in Oregon, Illinois, California, Maryland, Washington and Colorado with different qualifying relationships and durations; paid voting leave is required in about thirty states with different notice and hour thresholds. A single national policy is wrong in most of them.
  - International: In the EU, compassionate and civic leave entitlements are national and often collectively bargained, and several states provide statutory paid bereavement that no US state matches in length.

---

## REC. Recruiting and ATS

8 workflows.

### WF-REC-001. Open a requisition and publish a job posting

- **Status**: `PARTIAL` - planning/workflows/lifecycle/catalog.md rows CreateRequisition and PublishJobPosting
- **Archetype**: `A1` | **Complexity tier**: 2
- **Actors**: hiring manager, recruiter, HR business partner, finance partner, integration system
  - performing a step: recruiter, HR business partner, finance partner, integration system
  - participating without owning a step: hiring manager
- **Trigger**: A hiring manager opens a requisition against approved headcount and it is posted.
- **Preconditions**:
  - A headcount reservation or plan line funds the requisition.
  - The job profile and its pay band are published.
  - The posting jurisdictions and their disclosure requirements are resolved.
- **Steps**:
  1. `CAPABILITY` (recruiter) bind the requisition to the funding reservation and the job profile
  2. `RULE` (HR business partner) assemble the pay range and any required disclosures for every jurisdiction the posting will reach
  3. `DECISION` (HR business partner) REJECT publication in a jurisdiction whose required disclosure is missing rather than posting without it
  4. `APPROVAL` (finance partner) the hiring manager chain and finance approve the requisition and its band
  5. `CAPABILITY` (recruiter) publish the posting to each board with its jurisdiction-specific content
  6. `OBSERVE` (integration system) confirm each board accepted the posting and record its live URL and date
- **Intents**:
  - `hcmnext.recruiting.create_requisition/v1` (PROPOSED, creates)
  - `hcmnext.recruiting.publish_posting/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: pay band for the level and location
  - `organization`: job profile, level, essential functions
  - `position`: the position and its reservation
  - `regulatory`: posting disclosure rules per jurisdiction
- **Writes**:
  - `integration`: board publication operations
  - `recruiting`: requisition and posting records
- **Evidence required**:
  - The exact posted content per jurisdiction, retained for the statutory period.
  - Approval chain for the requisition and its band.
  - Publication confirmation with the live date, which starts several statutory clocks.
- **Failure and repair**:
  - A board strips the pay range from the posted content -> DRIFT; the posting is non-compliant as published and must be corrected or pulled, and the observation is what detects it
  - The requisition outlives its headcount reservation -> the posting is BLOCKED from renewal until refunded
  - The same role is posted with different ranges in different jurisdictions -> permitted where the ranges reflect location, but the difference is recorded with its basis, since an unexplained difference is evidence in a pay equity claim
- **Jurisdiction**:
  - US federal: Federal contractors must include EEO taglines and, under OFCCP rules, list openings with the state employment service; the ADA constrains how essential functions are described.
  - US state variation: Colorado, California, Washington, New York State, New York City, New Jersey, Hawaii, Maryland, Illinois, Vermont and Minnesota require pay ranges in postings, with different scopes, employer-size thresholds and effective dates; Colorado also requires notice of promotional opportunities to existing employees; several jurisdictions ban salary history questions entirely. The list changes every legislative session, which is why the posting content is assembled from a versioned rule pack per jurisdiction rather than from a maintained list in this document.
  - International: The EU Pay Transparency Directive requires applicants to receive information on the initial pay level or its range before the interview and bans asking about pay history.

### WF-REC-002. Screen an application and record a decision

- **Status**: `PARTIAL` - planning/workflows/lifecycle/catalog.md rows CandidateApplication and ScreenCandidate
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: recruiter, hiring manager, compliance officer, AI agent
  - performing a step: recruiter, compliance officer, AI agent
  - participating without owning a step: hiring manager
- **Trigger**: An application arrives and is screened against the requisition's criteria.
- **Preconditions**:
  - The screening criteria are declared before applications open.
  - Any automated screening tool is registered with its bias audit.
  - Candidate consent and privacy notice are recorded at application.
- **Steps**:
  1. `CAPABILITY` (recruiter) record the application with its consent and its source
  2. `AGENT` (AI agent) an assistive screen ranks against the declared criteria and produces reasons, never a decision
  3. `TASK` (recruiter) a human recruiter decides advance or reject and records the reason against the declared criteria
  4. `DECISION` (compliance officer) refuse to record a reason outside the declared criteria set
  5. `SIGNAL` (recruiter) notify the candidate of the outcome within the tenant's service commitment
  6. `END` (compliance officer) close with the decision and its reason retained for the statutory period
- **Intents**:
  - `hcmnext.recruiting.screen_candidate/v1` (PROPOSED, creates)
- **Reads**:
  - `intelligence`: model version and its audit reference where a tool was used
  - `privacy`: candidate consent and processing purpose
  - `recruiting`: application, requisition criteria, prior stage decisions
- **Writes**:
  - `intelligence`: model output retained as decision support, not as the decision
  - `recruiting`: stage decision with its reason
- **Evidence required**:
  - Declared criteria version at the time of screening.
  - Human decision-maker identity and the reason recorded.
  - Model version and bias audit reference where an automated tool contributed.
- **Failure and repair**:
  - The automated tool's output is recorded as the decision -> FAIL_CLOSED; the tool advises and a human decides, which is both the platform principle and the law in a growing number of jurisdictions
  - A reason is recorded that is not in the declared set -> REJECT; free-text reasons are what make adverse impact analysis impossible
  - A candidate requests the basis of the decision -> the disclosure follows the jurisdiction; the record must be able to answer it
- **Jurisdiction**:
  - US federal: Title VII adverse impact analysis under the Uniform Guidelines on Employee Selection Procedures requires applicant flow data by race, sex and ethnicity; the EEOC has stated that AI screening tools are covered by the same standards.
  - US state variation: New York City Local Law 144 requires an annual independent bias audit of automated employment decision tools plus candidate notice; Illinois regulates AI video interview analysis; Maryland restricts facial recognition in interviews; California's proposed and enacted automated decision rules add further duties.
  - International: The EU AI Act classifies employment screening as high risk with conformity assessment, logging and human oversight duties; GDPR Article 22 restricts solely automated decisions with legal or similarly significant effects.

### WF-REC-003. Order a background check and apply adverse action

- **Status**: `PARTIAL` - planning/workflows/lifecycle/catalog.md row RunBackgroundCheck
- **Archetype**: `A14` | **Complexity tier**: 4
- **Actors**: recruiter, compliance officer, external partner or carrier, hiring manager, integration system
  - performing a step: recruiter, compliance officer, external partner or carrier, integration system
  - participating without owning a step: hiring manager
- **Trigger**: A conditional offer is made and a background check is ordered through a consumer reporting agency.
- **Preconditions**:
  - A standalone written disclosure and authorization are obtained from the candidate.
  - The check's scope is limited to what the role requires.
  - The adverse action process and its waiting period are configured.
- **Steps**:
  1. `DOCUMENT` (recruiter) obtain the standalone disclosure and the candidate's written authorization
  2. `CAPABILITY` (external partner or carrier) order the check with the scope the role justifies
  3. `OBSERVE` (integration system) receive the report into a restricted compartment
  4. `DECISION` (compliance officer) route any potentially disqualifying finding to the individualized assessment, not to an automatic rejection
  5. `DOCUMENT` (compliance officer) issue the pre-adverse action notice with a copy of the report and the summary of rights
  6. `WAIT` (compliance officer) hold the statutory waiting period so the candidate can dispute
  7. `TASK` (compliance officer) consider any dispute or explanation the candidate provides
  8. `DOCUMENT` (compliance officer) issue the final adverse action notice if the decision stands
  9. `END` (compliance officer) close with the full notice trail retained
- **Intents**:
  - `hcmnext.recruiting.order_background_check/v1` (PROPOSED, creates)
  - `hcmnext.recruiting.apply_adverse_action/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: disclosure, authorization, the report itself in a compartment
  - `recruiting`: candidate, offer, role requirements
  - `regulatory`: ban-the-box and fair chance rules for the jurisdiction
- **Writes**:
  - `documents`: pre-adverse and final adverse action notices with delivery evidence
  - `recruiting`: check result reference and the decision
- **Evidence required**:
  - Standalone disclosure and authorization, not buried in an application form.
  - The individualized assessment, showing the nature of the offence, the time elapsed and the role's requirements.
  - Both notices with their dates and the waiting period between them.
- **Failure and repair**:
  - The hiring manager rejects before the waiting period ends -> BLOCKED; the pre-adverse notice exists precisely to allow a dispute, and skipping it is the most common FCRA class claim
  - The report is returned with a record that belongs to a different person -> the dispute path applies and the decision is suspended; the workflow does not proceed on an unresolved identity conflict
  - The role is in a jurisdiction that forbids asking about the record type returned -> the finding is suppressed from the decision and the suppression is recorded
- **Jurisdiction**:
  - US federal: FCRA requires a standalone disclosure, written authorization, a pre-adverse action notice with the report and the CFPB summary of rights, a reasonable waiting period, and a final adverse action notice; statutory damages plus attorney fees make procedural defects expensive.
  - US state variation: California's Fair Chance Act requires a conditional offer first, an individualized assessment, a five-business-day response period and a second five-day period after a dispute; New York City's Fair Chance Act has its own sequence and a Fair Chance Act notice form; over 35 states and 150 localities have ban-the-box rules with different timing.
  - International: In the UK, DBS checks are limited by the level of check the role justifies and by filtering rules on spent convictions; in the EU, criminal record processing is restricted under GDPR Article 10 to circumstances authorized by law.

### WF-REC-004. Extend, negotiate and sign an offer

- **Status**: `PARTIAL` - planning/workflows/lifecycle/catalog.md rows CreateOffer, ApproveOffer, SendOffer and AcceptOffer
- **Archetype**: `A16` | **Complexity tier**: 3
- **Actors**: recruiter, hiring manager, finance partner, compliance officer, integration system
  - performing a step: recruiter, finance partner, compliance officer, integration system
  - participating without owning a step: hiring manager
- **Trigger**: A selected candidate receives an offer that is negotiated and then signed.
- **Preconditions**:
  - The compensation is within the posted range or an exception is approved.
  - The offer's conditions, such as background check and work authorization, are explicit.
  - The signature ceremony and its evidence requirements are configured.
- **Steps**:
  1. `TRANSFORM` (recruiter) build the offer with compensation, start date, conditions and the localized letter
  2. `RULE` (compliance officer) validate the compensation against the posted range and the internal equity guardrails
  3. `APPROVAL` (finance partner) hiring manager and finance approve; compliance approves any deviation from the posted range
  4. `DOCUMENT` (recruiter) generate the localized offer letter with the jurisdiction's required content
  5. `SIGNAL` (recruiter) send the offer with an expiry
  6. `TASK` (recruiter) the candidate accepts, declines or counters; a counter produces a new offer revision, not an edit
  7. `CAPABILITY` (integration system) record the signature with its ceremony evidence
  8. `END` (recruiter) close on acceptance, leaving the conditions as open obligations until each clears
- **Intents**:
  - `hcmnext.recruiting.create_offer/v1` (PROPOSED, creates)
  - `hcmnext.recruiting.accept_offer/v1` (PROPOSED, advances)
- **Reads**:
  - `compensation`: band, internal equity comparison
  - `recruiting`: requisition, posting range, candidate record
  - `regulatory`: required offer content for the jurisdiction
- **Writes**:
  - `documents`: signed letter and its signature proof
  - `recruiting`: offer revisions, acceptance, conditions
- **Evidence required**:
  - Every offer revision, so a negotiation history exists.
  - Signature ceremony evidence including the signer's authentication.
  - The posted range comparison and any approved deviation.
- **Failure and repair**:
  - The final compensation is outside the posted range -> BLOCKED pending an approved exception; a range that is not honoured is evidence against the employer in a transparency claim
  - The candidate signs after the expiry -> the offer is expired; a new revision must be issued rather than treating the late signature as acceptance
  - A condition fails after acceptance -> the offer is rescinded through its own governed path with notice, not by deleting the acceptance
- **Jurisdiction**:
  - US federal: An offer letter is not a contract in most US jurisdictions but at-will language must be present and consistent; IRCA requires work authorization verification within three business days of the start date, not before the offer.
  - US state variation: California Labor Code 2810.5 requires a wage notice at hire with nine specific items; New York Labor Law 195(1) requires a written notice with pay rate, basis and payday, in English and the employee's primary language where a template exists; several jurisdictions ban salary history questions during negotiation.
  - International: In the EU, Directive 2019/1152 on transparent and predictable working conditions requires core terms in writing within seven days and the remainder within a month; in Germany the Nachweisgesetz makes omissions directly sanctionable.

### WF-REC-005. Convert an accepted candidate into a worker

- **Status**: `PARTIAL` - planning/workflows/lifecycle/catalog.md row ConvertCandidateToWorker
- **Archetype**: `A8` | **Complexity tier**: 3
- **Actors**: recruiter, HR business partner, IT or access administrator, payroll administrator, integration system
  - performing a step: HR business partner, IT or access administrator, payroll administrator, integration system
  - participating without owning a step: recruiter
- **Trigger**: An accepted offer becomes a Person, Worker, Employment and Assignment before the start date.
- **Preconditions**:
  - All offer conditions have cleared.
  - The start date is far enough ahead for provisioning lead times.
  - The candidate's identity resolves against any existing Person record, such as a former employee.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the candidate against the Person index so a rehire reuses the existing Person
  2. `TRANSFORM` (HR business partner) build the composite proposal: Person or link, Worker, Employment, Assignment, compensation
  3. `DECISION` (HR business partner) route a matched former employee to the rehire path, which preserves service history
  4. `APPROVAL` (payroll administrator) HR approves the conversion; payroll approves the pay group assignment
  5. `CAPABILITY` (integration system) commit the bound child set as one transaction plan
  6. `SIGNAL` (IT or access administrator) trigger onboarding, access provisioning and payroll enrollment as child intents
  7. `END` (HR business partner) close on commit; readiness is the onboarding workflow's concern
- **Intents**:
  - `hcmnext.recruiting.convert_candidate/v1` (PROPOSED, creates)
  - `hcmnext.people.create_employment/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: Person index for match resolution
  - `position`: the position the requisition reserved
  - `recruiting`: candidate, accepted offer, conditions
- **Writes**:
  - `compensation`: initial package
  - `people`: Person link or creation, Worker, Employment, Assignment
  - `position`: occupancy and reservation consumption
- **Evidence required**:
  - Identity match evidence, so a rehire is not created as a stranger.
  - The bound child set and the order it committed in.
  - Position reservation consumption.
- **Failure and repair**:
  - The candidate matches a former employee with an unresolved obligation, such as unreturned equipment -> the conversion proceeds and the obligation is carried forward, visible to HR; it is not silently cleared
  - The start date passes before conversion -> the start date is corrected forward with a recorded reason; a retroactive start creates back pay and tax exposure
  - The position was filled by someone else in the interim -> REJECT; the reservation is a fence and a filled position cannot be occupied twice
- **Jurisdiction**:
  - US federal: Form I-9 must be completed by the third business day after the start date; E-Verify, where required, has its own three-day window from the I-9 completion.
  - US state variation: E-Verify is mandatory for all employers in Alabama, Arizona, Georgia, Mississippi, North Carolina, South Carolina, Tennessee and Utah with varying employee-count thresholds, and for public contractors in many more; California restricts employer use of E-Verify beyond federal requirements.
  - International: In the EU, right-to-work checks vary by member state; the UK requires a check before employment begins to establish a statutory excuse against illegal working penalties.

### WF-REC-006. Schedule interviews and collect structured feedback

- **Status**: `PARTIAL` - planning/workflows/lifecycle/catalog.md rows ScheduleInterview and RecordInterviewFeedback
- **Archetype**: `A13` | **Complexity tier**: 2
- **Actors**: recruiter, hiring manager, employee, compliance officer
- **Trigger**: An interview loop is scheduled and each interviewer submits structured feedback.
- **Preconditions**:
  - The interview plan defines who assesses which competency.
  - Accommodation requests are collected before scheduling.
  - The feedback form is structured against the declared criteria.
- **Steps**:
  1. `TASK` (recruiter) collect any accommodation request and arrange it before scheduling
  2. `CAPABILITY` (recruiter) schedule the loop against interviewer availability and candidate constraints
  3. `TASK` (employee) each interviewer submits structured feedback against their assigned competencies
  4. `DECISION` (recruiter) hide other interviewers' feedback until submission, so assessments stay independent
  5. `TASK` (hiring manager) the hiring manager makes the advance decision with the assembled feedback
  6. `END` (compliance officer) close with the feedback retained for the statutory record period
- **Intents**:
  - `hcmnext.recruiting.schedule_interview/v1` (PROPOSED, creates)
  - `hcmnext.recruiting.record_feedback/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: interviewer availability and scope
  - `privacy`: accommodation request in a restricted compartment
  - `recruiting`: candidate, requisition criteria, interview plan
- **Writes**:
  - `documents`: recorded sessions where consent was obtained
  - `recruiting`: interview schedule and feedback records
- **Evidence required**:
  - Structured feedback tied to declared competencies.
  - Accommodation request and how it was met, held separately from the assessment.
  - The independence control, showing feedback was not visible before submission.
- **Failure and repair**:
  - An interviewer submits free-text feedback outside the competency structure -> the submission is accepted but flagged; unstructured feedback is the weakest evidence in a discrimination claim and the workflow surfaces the risk
  - A recording is made without consent in a two-party consent jurisdiction -> REJECT the recording and delete it; the interview itself stands
  - The accommodation was requested but not arranged -> BLOCK the interview and reschedule; proceeding is an independent violation
- **Jurisdiction**:
  - US federal: Title VII and the ADA apply to the interview itself; the ADA forbids disability-related inquiries before a conditional offer, and interview notes are discoverable.
  - US state variation: California, Illinois, Florida, Pennsylvania, Washington and several other states require all-party consent to record; Illinois' AI Video Interview Act requires notice, explanation and consent before AI analysis, plus deletion on request; Maryland requires consent for facial recognition.
  - International: The EU AI Act treats emotion recognition in the workplace as prohibited; GDPR requires a lawful basis and a retention limit for interview notes, and several member states cap retention at six months absent consent.

### WF-REC-007. Maintain an affirmative action plan and its applicant data

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 4
- **Actors**: compliance officer, recruiter, HR business partner, auditor
  - performing a step: compliance officer, recruiter, auditor
  - participating without owning a step: HR business partner
- **Trigger**: A federal contractor must maintain plans, analyse availability and utilization, and retain applicant data.
- **Preconditions**:
  - The applicant definition is the regulatory one and not the recruiter's working definition.
  - Self-identification is voluntary, solicited at the right stage, and stored apart from the selection record.
  - The plan year, its availability analysis and its placement goals are versioned.
- **Steps**:
  1. `CAPABILITY` (recruiter) classify every expression of interest against the regulatory applicant definition
  2. `TASK` (recruiter) solicit voluntary self-identification at the correct stages and store it apart from the selection record
  3. `TRANSFORM` (compliance officer) compute availability, utilization and any placement goals per job group
  4. `CAPABILITY` (compliance officer) run adverse impact analysis over the selection stages
  5. `DECISION` (compliance officer) a placement goal is a goal, not a quota; a selection decision made to meet it is unlawful and the workflow must not present it as an action
  6. `END` (auditor) close the plan year with the plan, the analyses and the applicant data retained for the statutory period
- **Intents**:
  - `hcmnext.recruiting.maintain_affirmative_action_plan/v1` (PROPOSED, creates)
- **Reads**:
  - `organization`: job groups and their availability data
  - `people`: self-identification data under a permitting purpose
  - `recruiting`: applicants, stages, selection decisions
- **Writes**:
  - `governance`: the plan, its availability and utilization analyses and its adverse impact findings
  - `recruiting`: applicant classification and retained data
- **Evidence required**:
  - The applicant classification per expression of interest, since who counts as an applicant decides every downstream ratio.
  - Self-identification stored separately from the selection record, with its own access control.
  - Adverse impact analysis per stage, retained under its classification.
- **Failure and repair**:
  - Self-identification data is visible to the selection decision-maker -> DENIED; the separation is the whole point and a leak here converts the compliance programme into evidence against the employer
  - A recruiter is instructed to select a candidate to meet a placement goal -> the workflow has no such action; a goal drives outreach, not selection, and presenting it otherwise invites a reverse discrimination claim
  - An expression of interest is excluded from the applicant count on a discretionary basis -> the exclusion must map to the regulatory definition and its basis is recorded; discretionary exclusion is how impact ratios get quietly improved
- **Jurisdiction**:
  - US federal: Executive Order 11246 and its implementing regulations, together with Section 503 and VEVRAA, impose plan, analysis and record retention duties on covered federal contractors; the Uniform Guidelines govern adverse impact analysis and the four-fifths rule is a rule of thumb rather than a legal threshold. The scope and content of these obligations have been subject to significant executive change and the current requirements should be read from the rule pack.
  - US state variation: California and Illinois pay data reporting overlaps but does not align with federal EEO-1 categories, so one dataset cannot serve both without a documented mapping.
  - International: The EU does not use affirmative action plans of this shape; positive action is permitted within narrow limits and quota-based selection is generally unlawful.

### WF-REC-008. Run an employee referral programme and pay its awards

- **Status**: `NEW`
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: employee, recruiter, payroll administrator, finance partner
- **Trigger**: A worker refers a candidate and an award becomes payable on a defined milestone.
- **Preconditions**:
  - The programme rules, its eligibility, its award amounts and its milestones are published before referrals are made.
  - The referral must precede the candidate's application to count.
  - The award is wages when paid, not a gift.
- **Steps**:
  1. `TASK` (employee) the worker submits a referral, which is timestamped before any application
  2. `CAPABILITY` (recruiter) link the referral to the application if one arrives, and record the ordering
  3. `WAIT` (recruiter) hold to the award milestone, commonly the hire date plus a retention period
  4. `RULE` (recruiter) test eligibility: the referrer is still employed, is not the hiring manager, and is in an eligible role
  5. `CAPABILITY` (payroll administrator) pay the award through payroll with supplemental withholding
  6. `END` (finance partner) close with the referral chain and the award retained
- **Intents**:
  - `hcmnext.recruiting.pay_referral_award/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: programme award amounts
  - `people`: referrer employment status at the milestone
  - `recruiting`: referrals, applications, hire outcomes
- **Writes**:
  - `payroll`: award payment with supplemental withholding
  - `recruiting`: referral link and award determination
- **Evidence required**:
  - The referral timestamp relative to the application, which decides eligibility.
  - The milestone evaluation with the referrer's status at that date.
  - The award payment with its tax treatment.
- **Failure and repair**:
  - The referral is submitted after the candidate applied -> REJECT with the two timestamps shown; retro-fitting a referral to an existing pipeline candidate is the most common abuse
  - The referrer leaves before the milestone -> the programme rules decide; whichever way they decide, the rule must be published in advance rather than applied at payment time
  - The award is paid as a gift card to avoid payroll -> REJECT; it is wages regardless of its form and paying it outside payroll creates a withholding failure
- **Jurisdiction**:
  - US federal: A referral award is supplemental wages subject to withholding and is included in the FLSA regular rate unless it qualifies as a discretionary bonus, which a published programme with defined amounts does not.
  - US state variation: None state-specific beyond ordinary wage payment rules.
  - International: In several EU states a repeated referral award can become a contractual entitlement through custom and practice.

---

## ONB. Onboarding and offboarding

8 workflows.

### WF-ONB-001. Run an onboarding plan to day-one readiness

- **Status**: `EXISTING` - planning/workflows/lifecycle/catalog.md rows StartOnboarding through CompleteOnboarding; planning/workflows/lifecycle/recruit-hire-onboard.md
- **Archetype**: `A20` | **Complexity tier**: 3
- **Actors**: HR business partner, employee, IT or access administrator, manager, payroll administrator
  - performing a step: HR business partner, employee, IT or access administrator, manager
  - participating without owning a step: payroll administrator
- **Trigger**: A converted candidate has a start date and a readiness plan is generated from role, location and legal requirements.
- **Preconditions**:
  - The employment and assignment are committed with a start date.
  - The requirement catalog for the role, location and legal entity is resolvable.
  - Each requirement names its owner and its deadline relative to the start date.
- **Steps**:
  1. `CAPABILITY` (HR business partner) generate the requirement set from role, location, entity and jurisdiction
  2. `PARALLEL` (IT or access administrator) dispatch each requirement to its owner: forms to the employee, access to IT, equipment to facilities, payroll enrollment to payroll
  3. `TASK` (employee) the employee completes identity, tax and policy requirements
  4. `WAIT` (HR business partner) hold for each requirement with its own deadline and reminder cadence
  5. `DECISION` (HR business partner) distinguish a blocking requirement, such as work authorization, from a non-blocking one, such as an optional benefit election
  6. `CAPABILITY` (HR business partner) compute readiness as the set of blocking requirements satisfied, not as a percentage
  7. `SIGNAL` (manager) tell the manager which specific requirements are outstanding, without disclosing their content
  8. `END` (HR business partner) close only when every blocking requirement is satisfied; the rest remain open obligations
- **Intents**:
  - `hcmnext.onboarding.create_plan/v1` (PROPOSED, creates)
  - `hcmnext.onboarding.verify_readiness/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: entitlement templates for the role
  - `learning`: mandatory training for the role and jurisdiction
  - `people`: employment, assignment, start date, location
  - `regulatory`: jurisdictional requirements at hire
- **Writes**:
  - `access`: provisioning intents
  - `documents`: completed forms and their evidence
  - `onboarding`: plan, requirements, readiness state
- **Evidence required**:
  - Requirement set with its generating rule version.
  - Per-requirement completion with its actor and timestamp.
  - Readiness determination naming the blocking set.
- **Failure and repair**:
  - Work authorization is not verified by the third business day -> the employment is BLOCKED from continuing; this is a hard legal deadline, not a reminder
  - Equipment does not arrive before the start date -> readiness reports NOT_READY with the specific gap; a green dashboard with no laptop is the failure mode this replaces
  - The start date moves -> every deadline recomputes from the new date and requirements already completed are preserved rather than reissued
- **Jurisdiction**:
  - US federal: Form I-9 section 1 by the first day of work and section 2 by the third business day; Form W-4 before the first payroll; OSHA requires certain safety training before the worker performs covered tasks.
  - US state variation: State new-hire reporting to the state directory is due within 20 days, or less in some states; state-specific wage notices such as California's 2810.5 and New York's 195(1) must be delivered at hire; mandatory harassment prevention training has deadlines in California, New York, Illinois, Connecticut, Delaware and Maine.
  - International: In the EU, Directive 2019/1152 requires core employment terms in writing within seven days of the first day, and a right-to-work check must generally precede the start rather than follow it.

### WF-ONB-002. Verify work authorization and complete the employment eligibility record

- **Status**: `PARTIAL` - planning/workflows/lifecycle/catalog.md row CollectWorkAuthorization
- **Archetype**: `A16` | **Complexity tier**: 3
- **Actors**: HR business partner, employee, compliance officer
- **Trigger**: A new employee must present documents establishing identity and work authorization.
- **Preconditions**:
  - The start date is set and the three-business-day clock is computable.
  - The document list the employee may choose from is presented without steering.
  - A remote or authorized-representative process exists where the employee is not on site.
- **Steps**:
  1. `TASK` (employee) the employee completes section 1 no later than the first day
  2. `TASK` (HR business partner) the employer examines the presented documents and completes section 2 within three business days
  3. `DECISION` (compliance officer) refuse to specify which documents the employee must present; document abuse is its own violation
  4. `CAPABILITY` (compliance officer) submit to the electronic verification service where the jurisdiction or contract requires it
  5. `OBSERVE` (compliance officer) record the verification result and route a tentative non-confirmation to the contest process
  6. `END` (compliance officer) close with the record retained on its own schedule, separate from the personnel file
- **Intents**:
  - `hcmnext.onboarding.verify_work_authorization/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: presented document metadata, never a stored copy unless the policy requires it uniformly
  - `people`: employment start date and worker identity
  - `regulatory`: verification requirements for the state and any federal contract
- **Writes**:
  - `onboarding`: eligibility record with its completion dates
  - `regulatory`: verification case reference and result
- **Evidence required**:
  - Section completion dates, which are the audited facts.
  - Verification case reference and its result.
  - Reverification schedule for documents with an expiry.
- **Failure and repair**:
  - A tentative non-confirmation is returned -> the employee must be notified and given the chance to contest; no adverse action may be taken while the contest is open
  - Documents expire during employment -> reverification is scheduled before expiry; working past an expired authorization is a knowing violation
  - The employer stores copies for some employees and not others -> the inconsistency is itself a discrimination risk, so the policy is enforced uniformly by the system rather than left to each administrator
- **Jurisdiction**:
  - US federal: IRCA requires Form I-9 completion on the stated timeline; document abuse and citizenship discrimination are enforced by the Department of Justice IER, and paperwork violations carry per-form penalties.
  - US state variation: E-Verify is mandatory statewide in Alabama, Arizona, Georgia, Mississippi, North Carolina, South Carolina, Tennessee and Utah, and for public contractors in many other states; California Labor Code 1019.2 restricts voluntary E-Verify use and requires notice of any inspection.
  - International: The UK requires a right-to-work check before employment starts, with a digital check for eVisa holders, and a follow-up check before any time-limited permission expires.

### WF-ONB-003. Assign and later recover equipment

- **Status**: `EXISTING` - planning/workflows/lifecycle/catalog.md rows AssignEquipment and RecoverEquipment; internal/domains/asset package: custody receipts
- **Archetype**: `A15` | **Complexity tier**: 2
- **Actors**: IT or access administrator, employee, manager, integration system
  - performing a step: IT or access administrator, employee, integration system
  - participating without owning a step: manager
- **Trigger**: Equipment is issued at hire and must be returned at exit.
- **Preconditions**:
  - The asset inventory is authoritative and the item is in stock.
  - The custody receipt mechanism produces independently attributable evidence.
  - A shipping path exists for remote workers with a tracked return.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) reserve the asset and generate the custody handoff
  2. `TASK` (employee) the employee acknowledges receipt, producing the handoff receipt
  3. `CAPABILITY` (integration system) record custody with the asset's identifiers and its condition
  4. `WAIT` (IT or access administrator) hold custody until a return event, whether exit, upgrade or loss
  5. `TASK` (IT or access administrator) the return is received and inspected; the employee's report of return is not itself completion
  6. `END` (IT or access administrator) close only on the inspected return receipt; an unreturned asset stays an open obligation
- **Intents**:
  - `hcmnext.asset.assign_equipment/v1` (PROPOSED, creates)
  - `hcmnext.asset.recover_equipment/v1` (PROPOSED, creates)
- **Reads**:
  - `asset`: inventory, custody history, condition
  - `people`: worker location and employment status
- **Writes**:
  - `asset`: custody revision and receipts
  - `people`: outstanding obligation on separation
- **Evidence required**:
  - Handoff receipt with the asset identifiers.
  - Return receipt from the receiving party, not from the employee.
  - Condition assessment at both ends.
- **Failure and repair**:
  - The employee reports the item returned but it is not received -> the obligation stays open; the domain deliberately treats a self-reported return as insufficient
  - The asset is lost -> a loss record with its data-security consequence, which may itself be a breach assessment trigger
  - A deduction for unreturned equipment is proposed from final pay -> BLOCKED unless the jurisdiction permits it with the signed authorization on file
- **Jurisdiction**:
  - US federal: No federal statute governs equipment, but a device holding personal data makes an unreturned asset a potential breach event under state notification laws.
  - US state variation: California Labor Code 221 and 224 forbid deducting equipment cost from wages without narrow written authorization, and never below minimum wage; New York and Illinois similarly restrict; Texas allows more with written consent.
  - International: Under GDPR, an unreturned device holding personal data is a potential personal data breach requiring a 72-hour assessment, which is why the loss record has a privacy branch.

### WF-ONB-004. Execute an offboarding plan on termination

- **Status**: `EXISTING` - internal/workflow/conformance/termination/definition.go workflow hcmnext.workflows.termination_offboarding; planning/workflows/lifecycle/termination-offboarding.md
- **Archetype**: `A3` | **Complexity tier**: 5
- **Actors**: HR business partner, manager, IT or access administrator, payroll administrator, compliance officer, integration system
  - performing a step: HR business partner, IT or access administrator, payroll administrator, compliance officer, integration system
  - participating without owning a step: manager
- **Trigger**: An approved termination triggers the coordinated wind-down of pay, benefits, access and equipment.
- **Preconditions**:
  - The termination is approved with a date and a reason code.
  - No legal hold forbids the deprovisioning the plan includes.
  - The separation-of-duties rule prevents the terminating manager from also approving their own conflict.
- **Steps**:
  1. `CAPABILITY` (HR business partner) read the termination decision, the legal hold state and the obligations attached to the worker
  2. `DECISION` (compliance officer) block when a legal hold covers the worker's data and the plan would delete it
  3. `DECISION` (compliance officer) block when separation of duties is violated, for instance when the initiator is the subject
  4. `TRANSFORM` (HR business partner) build the exit effect graph: final pay, benefits end and continuation, access revocation, equipment recovery, knowledge transfer
  5. `APPROVAL` (payroll administrator) HR and the manager chain approve; payroll approves the final pay computation
  6. `WAIT` (IT or access administrator) hold to the termination instant, which for access is often earlier than for pay
  7. `PARALLEL` (IT or access administrator) dispatch each effect in its declared order; access revocation precedes the exit conversation in involuntary cases
  8. `OBSERVE` (integration system) observe each downstream authority and report which effects settled
  9. `END` (HR business partner) close BusinessState on the termination date, with continuation and equipment obligations still open
- **Intents**:
  - `hcmnext.people.end_employment/v1` (PROPOSED, creates)
  - `hcmnext.payroll.calculate_final_pay/v1` (PROPOSED, creates)
  - `hcmnext.access.revoke_all/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: every entitlement and account
  - `benefits`: coverage and continuation triggers
  - `payroll`: final pay inputs
  - `people`: employment, assignment, obligations
  - `privacy`: legal holds and retention duties
- **Writes**:
  - `access`: revocation operations and their observations
  - `benefits`: coverage end and continuation record
  - `payroll`: final pay and its deadline
  - `people`: employment end revision
- **Evidence required**:
  - Legal hold evaluation before any deletion.
  - Separation of duties evaluation.
  - Per-effect observation, so a partial offboarding is visible rather than reported as complete.
- **Failure and repair**:
  - A legal hold covers the worker -> TERMINATION_LEGAL_HOLD_BLOCKED; the data-affecting effects are suppressed and the rest proceed
  - The initiator is the subject or otherwise conflicted -> TERMINATION_SOD_VIOLATION_BLOCKED
  - The termination is later found erroneous -> TERMINATION_REQUIRES_REINSTATEMENT_INTENT; there is no undo, only a governed reinstatement
  - Some effects settle and others do not -> TERMINATION_SIMULATION_DEGRADED in simulation, or a bounded repair in execution; no false rollback
- **Jurisdiction**:
  - US federal: COBRA notice duties, final pay obligations, and the WARN Act if the termination is part of a covered event; the ADEA's OWBPA requires specific disclosures and a 21 or 45 day consideration period plus a 7 day revocation period for any release signed by a worker over 40.
  - US state variation: Final pay deadlines vary sharply: immediate in California on discharge, six days in Texas, next regular payday in Georgia and Florida; several states require a written statement of the reason for separation on request, including Missouri's service letter statute.
  - International: In most of the EU, dismissal requires cause or a statutory process, notice periods are longer, and works council consultation may be mandatory; access revocation before notice is served can itself be unlawful.

### WF-ONB-005. Run a knowledge and responsibility handover before exit

- **Status**: `NEW`
- **Archetype**: `A13` | **Complexity tier**: 2
- **Actors**: manager, employee, HR business partner, integration system
- **Trigger**: A departing worker's responsibilities, approvals and delegations must be reassigned.
- **Preconditions**:
  - The worker's active approval roles, delegations and owned records are enumerable.
  - A destination owner exists for each item or the item is explicitly retired.
  - The exit date is known.
- **Steps**:
  1. `CAPABILITY` (HR business partner) enumerate approval roles, delegations, owned cases, owned documents and system ownerships
  2. `TASK` (manager) the manager assigns each item to a successor or marks it retired
  3. `DECISION` (HR business partner) refuse to complete while any item is unassigned; an orphaned approval role silently blocks other workflows
  4. `CAPABILITY` (integration system) commit the reassignments with their effective dates
  5. `SIGNAL` (employee) notify each successor of what they now own
  6. `END` (manager) close when the unassigned set is empty
- **Intents**:
  - `hcmnext.people.transfer_responsibilities/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: owned artifacts and their retention
  - `hrcase`: owned cases
  - `work`: approval roles, delegations, open work items
- **Writes**:
  - `hrcase`: case reassignments
  - `work`: reassigned roles and delegations
- **Evidence required**:
  - The complete item inventory at the point of exit.
  - Per-item disposition with its successor.
  - Confirmation that the unassigned set is empty.
- **Failure and repair**:
  - An approval role has no eligible successor in scope -> escalate to the next level rather than leaving it unassigned; an unassigned approver is the most common cause of stalled workflows after a departure
  - An open case is confidential and the successor lacks the compartment -> the case is reassigned to a qualified owner outside the ordinary chain, and the compartment is not widened
  - The exit happens before the handover completes -> the items are escalated to the manager automatically and the incomplete handover is recorded
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None directly.
  - International: In codetermined jurisdictions, transferring a works council role is governed by the council's own rules, not by the employer's handover.

### WF-ONB-006. Rehire a former employee and restore service history

- **Status**: `PARTIAL` - planning/workflows/lifecycle/catalog.md row ReinstateWorker; planning/workflows/people/catalog.md row RehireWorker
- **Archetype**: `A4` | **Complexity tier**: 3
- **Actors**: HR business partner, recruiter, benefits administrator, payroll administrator, integration system
  - performing a step: HR business partner, benefits administrator, integration system
  - participating without owning a step: recruiter, payroll administrator
- **Trigger**: A former employee is hired again and prior service may affect benefits, leave and vesting.
- **Preconditions**:
  - The former Person and Worker records are matched with acceptable confidence.
  - The break-in-service rules for each affected plan are configured.
  - Any rehire eligibility restriction from the prior separation is checked.
- **Steps**:
  1. `CAPABILITY` (HR business partner) match the candidate to the former Person and read the prior separation reason and its restrictions
  2. `DECISION` (HR business partner) block where a not-eligible-for-rehire marker exists, and require an override with its own approval
  3. `RULE` (benefits administrator) apply the break-in-service rules per plan: retirement vesting, leave eligibility, benefits waiting period, seniority
  4. `CAPABILITY` (integration system) create a new Employment under the existing Person and Worker
  5. `CAPABILITY` (benefits administrator) restore or reset each plan's service credit according to its own rule, not a single global rule
  6. `END` (benefits administrator) close with the service credit decisions recorded per plan
- **Intents**:
  - `hcmnext.people.rehire_worker/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: break-in-service rules per plan
  - `leave`: prior entitlement usage and its recency
  - `people`: prior employments, separation reasons, restrictions
- **Writes**:
  - `benefits`: service credit decisions per plan
  - `leave`: eligibility computed from combined service where the rule allows
  - `people`: new Employment under the existing Person
- **Evidence required**:
  - Identity match evidence.
  - Per-plan service credit decision with its rule.
  - Any rehire restriction and the override that cleared it.
- **Failure and repair**:
  - Prior service is credited for one plan and not another -> correct; each plan's rule governs, and a single global answer would be wrong for at least one plan
  - The former record is under a legal hold from an old investigation -> the rehire proceeds but the hold is preserved and the investigation record is not merged into the new employment's visibility
  - The match confidence is below the threshold -> create a new Person rather than guessing; a wrong merge is far more expensive than a duplicate
- **Jurisdiction**:
  - US federal: FMLA counts prior service toward the 12-month requirement if the break is under seven years, with longer allowances for military service; retirement plan break-in-service rules under ERISA use the rule of parity.
  - US state variation: State paid leave programmes count contribution history rather than employer service, so a rehire may qualify immediately regardless of the employer's records.
  - International: In the EU, continuity of employment for redundancy and unfair dismissal purposes can survive a short break, and in the UK a break of less than a week generally preserves continuity.

### WF-ONB-007. Conduct an exit interview and feed attrition analysis

- **Status**: `NEW`
- **Archetype**: `A13` | **Complexity tier**: 2
- **Actors**: HR business partner, employee, manager
- **Trigger**: A departing worker is invited to an exit interview whose content must not become a weapon.
- **Preconditions**:
  - Participation is voluntary and refusal has no consequence.
  - The manager does not see attributed responses about themselves.
  - The aggregate feeds analysis; the individual record has its own retention.
- **Steps**:
  1. `SIGNAL` (employee) invite the departing worker, stating that participation is voluntary and how the responses will be used
  2. `TASK` (employee) the worker responds, or declines with no consequence recorded
  3. `DECISION` (HR business partner) route any response alleging misconduct or unlawful treatment to the case intake immediately rather than into an analytics pipeline
  4. `RULE` (HR business partner) suppress attributed responses from the subject manager's view and release only aggregates above the threshold
  5. `END` (manager) close with the individual record retained under its own class and the aggregate available
- **Intents**:
  - `hcmnext.experience.conduct_exit_interview/v1` (PROPOSED, creates)
- **Reads**:
  - `experience`: prior exit responses for the aggregate
  - `people`: the departing worker, their manager and their tenure
- **Writes**:
  - `experience`: exit interview record and its aggregate contribution
- **Evidence required**:
  - The voluntariness statement as shown to the worker.
  - Any misconduct allegation routed to case intake, with its routing timestamp.
  - Suppression of attributed content from the subject manager.
- **Failure and repair**:
  - A response alleges harassment -> it is a report and the employer's duty to investigate starts from its receipt; treating it as survey data is the failure that turns a resolvable complaint into litigation
  - A manager requests attributed exit responses about themselves -> DENIED where the responses were collected on a confidential basis
  - The interview is presented as mandatory -> REJECT; a mandatory exit interview produces responses nobody should rely on
- **Jurisdiction**:
  - US federal: An exit interview that surfaces a harassment or safety allegation triggers the employer's duty to act from the date of receipt regardless of the channel.
  - US state variation: State whistleblower and anti-retaliation statutes attach protection from the date of the report, which makes the routing timestamp legally significant.
  - International: Under GDPR the exit response is personal data about both the leaver and anyone they name, and the named person has their own access rights.

### WF-ONB-008. Manage a probationary period to a confirmation decision

- **Status**: `NEW`
- **Archetype**: `A5` | **Complexity tier**: 2
- **Actors**: manager, HR business partner, employee, compliance officer
- **Trigger**: A worker is engaged with a probationary period that must end in a decision rather than by inertia.
- **Preconditions**:
  - The period is defined in the contract and may be capped or qualified by statute, and the two are separate facts.
  - Absence during the period may extend it, which means the end date is computed rather than fixed.
  - A period that passes with no decision produces a confirmation by default, which is a decision nobody made.
- **Steps**:
  1. `CAPABILITY` (HR business partner) compute the probation end date from its terms and any qualifying absence
  2. `RULE` (compliance officer) resolve any statutory cap or qualification separately from the contractual period
  3. `TASK` (manager) conduct and record the review before the end date rather than after it
  4. `DECISION` (manager) confirm, extend within the permitted limit, or begin a separate exit process
  5. `APPROVAL` (HR business partner) HR approves any extension against the policy limit and its recorded basis
  6. `DOCUMENT` (employee) notify the worker of the outcome and any recomputed date
  7. `END` (HR business partner) close on the decision; a period passing undecided confirms by default and the default is recorded as a decision with its basis
- **Intents**:
  - `hcmnext.people.manage_probationary_period/v1` (PROPOSED, creates)
  - `hcmnext.people.confirm_probation/v1` (PROPOSED, advances)
- **Reads**:
  - `leave`: absence that extends the period
  - `people`: the probation terms, the start date and the service computation
  - `regulatory`: any statutory cap or qualification on the period
- **Writes**:
  - `people`: the recomputed end date, the decision and any extension
  - `talent`: the review record behind the decision
- **Evidence required**:
  - The computed end date with the absence that moved it.
  - The review conducted before the end date, with its date.
  - The decision with its basis, including a default confirmation recorded as one.
  - Any extension against the policy limit.
- **Failure and repair**:
  - The end date passes with no decision -> confirmation happens by inertia; the default is applied and recorded as a decision so the record does not imply a review that did not happen
  - A second extension is sought beyond the policy limit -> it is refused with the limit and the prior extension named, and the decisions that remain open are offered
  - The contractual period exceeds a statutory cap on shortened notice -> both are recorded and the notice computation applies the cap, because they govern different questions
  - Qualifying absence is not applied to the end date -> the period ends early by the length of the absence and the assessment covers less than it was meant to
- **Jurisdiction**:
  - US federal: At-will employment makes a probationary period a management practice rather than a legal status in most of the United States, and describing it as one has been read as creating an expectation of continued employment.
  - US state variation: Montana's wrongful discharge statute makes the probationary period legally significant, and several states' unemployment rules treat separations during a stated probationary period differently.
  - International: Probationary periods are statutory in most of Europe with caps on length and on the shortened notice they permit: Germany caps the shortened notice at six months, France sets maxima by category with limited renewal, and the Netherlands voids a probationary clause in short fixed-term contracts entirely.

---

## IAM. Identity and access

7 workflows.

### WF-IAM-001. Provision workforce access from a role template

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 11 features workforce_identity_create and access_entitlement_grant
- **Archetype**: `A15` | **Complexity tier**: 3
- **Actors**: IT or access administrator, manager, HR business partner, integration system
  - performing a step: IT or access administrator, manager, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A new or changed assignment drives an entitlement calculation and a set of grants.
- **Preconditions**:
  - The role-to-entitlement mapping is published and versioned.
  - Every target application's connector is healthy.
  - The worker's identity exists in the identity provider or is created as part of the flow.
- **Steps**:
  1. `CAPABILITY` (integration system) compute the entitlement set from role, organization, location and assignment attributes
  2. `DECISION` (manager) route entitlements above the sensitivity threshold to explicit approval instead of automatic grant
  3. `APPROVAL` (IT or access administrator) the manager approves the sensitive subset; the application owner approves where the application requires it
  4. `CAPABILITY` (integration system) dispatch grant operations per application in the declared order
  5. `OBSERVE` (integration system) observe each application and record what was actually granted, not what was requested
  6. `END` (IT or access administrator) close ConsistencyState only when the observed set equals the computed set
- **Intents**:
  - `hcmnext.access.calculate_entitlement/v1` (PROPOSED, creates)
  - `hcmnext.access.grant_entitlement/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: mapping version, existing grants, application inventory
  - `people`: assignment, role, organization, location
  - `qualification`: certifications an entitlement requires
- **Writes**:
  - `access`: entitlement calculation, grants and their observations
  - `integration`: per-application operations
- **Evidence required**:
  - Entitlement calculation with the mapping version.
  - Approval for each sensitive grant.
  - Per-application observation of the granted state.
- **Failure and repair**:
  - An application grants more than was requested -> DRIFT with a repair that revokes the excess; over-provisioning is the finding an access review would otherwise catch months later
  - An application is unreachable -> the grant stays PENDING with the worker unable to work in that system; readiness reports the gap rather than reporting success
  - A required qualification has lapsed -> the grant is refused with the qualification named, because the entitlement's precondition is not satisfied
- **Jurisdiction**:
  - US federal: SOX requires access controls over financial systems for public filers; HIPAA requires role-based access to protected health information with minimum necessary scope.
  - US state variation: State data-security statutes, including New York's SHIELD Act and Massachusetts 201 CMR 17, require reasonable access controls, which makes an unreconciled grant a compliance gap rather than an operational one.
  - International: The EU NIS2 Directive imposes access control and accountability duties on covered entities, and GDPR Article 32 requires access to be limited to what the purpose requires.

### WF-IAM-002. Run an access review campaign and act on its results

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 11 feature access_review_campaign_run
- **Archetype**: `A10` | **Complexity tier**: 4
- **Actors**: IT or access administrator, manager, auditor, compliance officer, integration system
- **Trigger**: A periodic certification requires reviewers to confirm or revoke each entitlement.
- **Preconditions**:
  - The review scope, its population and its reviewer assignment rule are defined.
  - The entitlement inventory is observed rather than assumed from the mapping.
  - The campaign deadline and its escalation path are set.
- **Steps**:
  1. `CAPABILITY` (integration system) collect the observed entitlement inventory from every in-scope application
  2. `TRANSFORM` (compliance officer) assign each item to a reviewer and detect items whose reviewer is the item's own holder
  3. `DECISION` (compliance officer) reassign self-review items to an independent reviewer rather than accepting a self-certification
  4. `TASK` (manager) reviewers certify or revoke each item with a reason
  5. `WAIT` (IT or access administrator) hold to the deadline with escalation for non-responders
  6. `DECISION` (compliance officer) apply the non-response policy, which is usually revoke rather than retain
  7. `CAPABILITY` (integration system) execute the revocations as governed intents and observe each one
  8. `END` (auditor) close with the certification evidence package assembled for the auditor
- **Intents**:
  - `hcmnext.access.run_review_campaign/v1` (PROPOSED, creates)
  - `hcmnext.access.revoke_entitlement/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: observed entitlements, grant provenance, prior campaign results
  - `organization`: scope boundaries for reviewer assignment
  - `people`: reviewer relationships and their currency
- **Writes**:
  - `access`: certification decisions and revocations
  - `operations`: evidence package for the campaign
- **Evidence required**:
  - The observed inventory with its collection watermark, not the mapping's expectation.
  - Per-item decision with its reviewer and reason.
  - The non-response disposition applied at deadline.
- **Failure and repair**:
  - An entitlement exists with no identifiable owner -> it is revoked by default and the orphan is recorded; an unowned entitlement cannot be certified
  - A reviewer certifies every item in one action without opening them -> the campaign records the bulk action; blanket certification is a known audit finding and must be visible, not hidden
  - A revocation fails at the application -> the item stays uncertified and open; a failed revocation is not a completed review
- **Jurisdiction**:
  - US federal: SOX 404 makes periodic access certification a standard control for public filers; HIPAA and PCI DSS both require periodic access review with documented results.
  - US state variation: State insurance and financial regulators impose their own access review cadences on licensed entities.
  - International: The EU DORA regulation requires financial entities to review access rights regularly, and NIS2 requires equivalent controls for covered sectors.

### WF-IAM-003. Detect and repair access drift against the entitlement model

- **Status**: `EXISTING` - definitions/governance/feature-intent-intake.yaml group 11 feature access_drift_detect; definitions/governance/intent-conformance-descriptors.yaml descriptor hcmnext.operations.detect_drift
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
  - performing a step: IT or access administrator, compliance officer, integration system
  - participating without owning a step: auditor
- **Trigger**: Observed access in a target system differs from what the model says it should be.
- **Preconditions**:
  - The observation is fresh enough to be meaningful.
  - The model's expected set is computable at the same effective instant.
  - A repair authority exists for each application.
- **Steps**:
  1. `OBSERVE` (integration system) collect the actual entitlement state from the application with a watermark
  2. `TRANSFORM` (compliance officer) compare against the expected set at the same effective instant and classify each difference
  3. `DECISION` (compliance officer) separate an expected grant not yet applied, a lag, from an unauthorized grant, a real drift
  4. `CAPABILITY` (IT or access administrator) create a repair plan naming each action and its risk
  5. `CAPABILITY` (IT or access administrator) simulate the repair to show what it would change
  6. `APPROVAL` (IT or access administrator) the application owner approves the repair actions
  7. `OBSERVE` (integration system) verify the repair landed in a later observation, not from the operation's return code
- **Intents**:
  - `hcmnext.operations.detect_drift/v1` (REAL, creates)
  - `hcmnext.operations.create_repair_plan/v1` (REAL, creates)
  - `hcmnext.operations.simulate_repair/v1` (REAL, creates)
- **Reads**:
  - `access`: expected entitlement set and its mapping version
  - `integration`: observed state and its watermark
- **Writes**:
  - `access`: corrected grants after approved repair
  - `operations`: drift findings, repair plan, repair execution records
- **Evidence required**:
  - Observation watermark and the expected set's effective instant, which must match.
  - Per-difference classification.
  - Post-repair verification observation.
- **Failure and repair**:
  - An unauthorized privileged grant is found -> escalate to a security incident rather than repairing quietly; the question of how it was granted matters more than the removal
  - The observation is stale -> report UNKNOWN rather than drift; comparing a fresh model to a stale observation manufactures findings
  - The repair would remove access the worker is actively using -> the plan states the operational impact and the approver decides; the system does not optimize for tidiness over the business
- **Jurisdiction**:
  - US federal: SOX and HIPAA both treat unauthorized access as a reportable control failure; the discovery date starts a breach assessment clock in some frameworks.
  - US state variation: State breach notification laws are triggered by unauthorized acquisition of personal information, so an unauthorized grant to a system holding it starts a jurisdiction-specific assessment.
  - International: GDPR Article 33 requires notification of a personal data breach within 72 hours of awareness, and unauthorized internal access counts.

### WF-IAM-004. Grant time-bound emergency access under break-glass

- **Status**: `NEW`
- **Archetype**: `A15` | **Complexity tier**: 5
- **Actors**: IT or access administrator, compliance officer, auditor, manager, integration system
  - performing a step: IT or access administrator, compliance officer, auditor, integration system
  - participating without owning a step: manager
- **Trigger**: An incident requires access beyond the normal entitlement model for a bounded period.
- **Preconditions**:
  - A break-glass policy defines who may request, who may approve and what the maximum duration is.
  - The grant is technically time-bound rather than reliant on a manual revocation.
  - Enhanced logging is enabled for the duration.
- **Steps**:
  1. `TASK` (IT or access administrator) the requester states the incident, the specific access needed and the duration
  2. `APPROVAL` (compliance officer) an approver independent of the requester approves, with a second approver above a sensitivity threshold
  3. `CAPABILITY` (integration system) grant with a hard expiry enforced by the target system, not by a scheduled job
  4. `SIGNAL` (compliance officer) notify the security team and the data owner that break-glass is active
  5. `OBSERVE` (auditor) capture the session activity at a higher fidelity than normal
  6. `WAIT` (integration system) hold to expiry
  7. `CAPABILITY` (compliance officer) verify the grant expired and was not silently extended
  8. `END` (auditor) close with a mandatory post-use review within the policy's window
- **Intents**:
  - `hcmnext.access.grant_break_glass/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: the requested entitlement and its normal approval path
  - `operations`: the incident the request cites
  - `privacy`: the data classes the access reaches
- **Writes**:
  - `access`: time-bound grant and its expiry
  - `operations`: enhanced activity log and the post-use review
- **Evidence required**:
  - Request with its stated incident and justification.
  - Independent approval, with the separation from the requester proven.
  - Session activity log and the post-use review conclusion.
- **Failure and repair**:
  - The grant is requested and self-approved -> FAIL_CLOSED; break-glass without independent approval is indistinguishable from privilege escalation
  - The expiry passes and access persists -> an immediate incident; the enforcement must be in the target system, and a failure of that enforcement is a control failure
  - The post-use review does not happen within the window -> the obligation escalates; an unreviewed break-glass use is the audit finding
- **Jurisdiction**:
  - US federal: SOX and HIPAA both expect emergency access procedures with documented justification and subsequent review; HIPAA specifically requires an emergency access procedure as a required implementation specification.
  - US state variation: State data-security statutes require reasonable safeguards, and an unreviewed break-glass path is a clear gap under New York's SHIELD Act and Massachusetts 201 CMR 17.
  - International: DORA requires financial entities to log and review privileged access; the EU AI Act's logging duties apply where the accessed system is a high-risk AI system.

### WF-IAM-005. Revoke all access on an immediate involuntary termination

- **Status**: `EXISTING` - internal/workflow/conformance/termination/definition.go; definitions/governance/feature-intent-intake.yaml group 10 feature access_revocation_manage
- **Archetype**: `A15` | **Complexity tier**: 4
- **Actors**: IT or access administrator, HR business partner, manager, compliance officer, integration system
  - performing a step: IT or access administrator, HR business partner, compliance officer, integration system
  - participating without owning a step: manager
- **Trigger**: An involuntary termination requires simultaneous revocation across every system at a precise instant.
- **Preconditions**:
  - The revocation instant is decided and is often before the worker is told.
  - Every account and entitlement, including non-federated and personal-device access, is enumerable.
  - Data preservation duties are evaluated before any deletion.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) enumerate every account, session, token, device and physical credential
  2. `DECISION` (compliance officer) preserve rather than delete where a legal hold or an investigation applies
  3. `WAIT` (HR business partner) hold to the exact revocation instant coordinated with the HR conversation
  4. `PARALLEL` (IT or access administrator) revoke sessions first, then credentials, then entitlements, then physical access
  5. `OBSERVE` (integration system) observe each system and identify any that did not revoke
  6. `CAPABILITY` (compliance officer) escalate unrevoked access to an incident with a manual containment step
  7. `END` (IT or access administrator) close only when the observed access set is empty or each exception is explained
- **Intents**:
  - `hcmnext.access.revoke_all/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: all accounts, entitlements, sessions and credentials
  - `asset`: devices in custody
  - `privacy`: legal holds and preservation duties
- **Writes**:
  - `access`: revocation operations and their observations
  - `operations`: incident record for any unrevoked access
- **Evidence required**:
  - Enumeration completeness evidence, since an unenumerated system cannot be revoked.
  - Per-system revocation observation with its timestamp.
  - Preservation decisions where deletion was suppressed.
- **Failure and repair**:
  - A system reports success but a later observation shows an active session -> an incident; session invalidation and credential revocation are different operations and both are required
  - A mailbox is deleted while under legal hold -> FAIL_CLOSED before the operation; spoliation is worse than lingering access
  - Access via a personal device or a shared credential is discovered -> the shared credential is rotated for everyone, which the plan must include rather than treating it as out of scope
- **Jurisdiction**:
  - US federal: The Computer Fraud and Abuse Act makes post-termination access potentially criminal, but that is no substitute for revoking it; SOX and HIPAA both require timely termination of access.
  - US state variation: State breach notification duties attach if a former worker accesses personal information after separation, which makes the observation step the control that matters.
  - International: Under GDPR, continued access by a former employee is a personal data breach with a 72-hour assessment clock from awareness.

### WF-IAM-006. Request, approve and time-limit a delegation of authority

- **Status**: `NEW`
- **Archetype**: `A15` | **Complexity tier**: 3
- **Actors**: manager, employee, compliance officer, HR business partner, integration system
  - performing a step: manager, compliance officer, integration system
  - participating without owning a step: employee, HR business partner
- **Trigger**: A manager delegates their approval authority while absent.
- **Preconditions**:
  - The delegable authorities are explicitly enumerable; not all authority is delegable.
  - The delegate is in a scope that may hold the authority.
  - The delegation has a defined interval.
- **Steps**:
  1. `TASK` (manager) the delegator selects which authorities to delegate and to whom, for a defined interval
  2. `RULE` (compliance officer) validate that each selected authority is delegable and that the delegate is eligible
  3. `DECISION` (compliance officer) refuse to delegate an authority whose policy marks it personal, such as an investigation disposition
  4. `CAPABILITY` (integration system) commit the delegation with its interval and its authority list
  5. `WAIT` (integration system) hold to the interval end
  6. `CAPABILITY` (compliance officer) expire the delegation and revalidate any approval made under it that has not yet executed
- **Intents**:
  - `hcmnext.security.grant_delegation/v1` (PROPOSED, creates)
- **Reads**:
  - `organization`: scope boundaries
  - `people`: the delegate's scope and eligibility
  - `work`: the delegator's authorities and their delegability
- **Writes**:
  - `security`: delegation record with its interval and authority list
  - `work`: delegation chain applied to approvals
- **Evidence required**:
  - The delegation chain recorded on every decision made under it.
  - Delegability evaluation per authority.
  - Expiry and any revalidation it triggered.
- **Failure and repair**:
  - An approval is made under a delegation that has since expired -> the approval is invalidated at revalidation; the descriptor's stale-delegation refusal names this case exactly
  - The delegate delegates onward -> REJECT unless the policy permits sub-delegation, and record the chain depth
  - The delegator returns early -> the delegation can be revoked, and approvals already made under it stand unless independently invalidated
- **Jurisdiction**:
  - US federal: SOX requires that delegated authority be documented and bounded; an undocumented delegation undermines the control it substitutes for.
  - US state variation: None state-specific.
  - International: In codetermined jurisdictions, some employer duties toward the works council cannot be delegated below a defined level.

### WF-IAM-007. Detect and resolve a segregation-of-duties conflict

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: compliance officer, IT or access administrator, manager, auditor, integration system
- **Trigger**: A worker holds two entitlements that together allow an unchecked transaction.
- **Preconditions**:
  - The conflict rules are expressed as entitlement pairs or sets with a stated risk.
  - Detection runs against observed access rather than against the role model.
  - A conflict has three resolutions: remove one side, add a compensating control, or accept with an owner and an expiry.
- **Steps**:
  1. `CAPABILITY` (integration system) collect the observed entitlement set across applications
  2. `RULE` (compliance officer) evaluate the conflict rules and produce each violation with its risk and the two sides
  3. `DECISION` (manager) route each violation to removal, compensating control, or a time-bound acceptance; an unowned acceptance is not one of the options
  4. `APPROVAL` (compliance officer) the risk owner approves any acceptance with its expiry and its compensating control
  5. `CAPABILITY` (IT or access administrator) execute removals through the ordinary entitlement revocation path
  6. `END` (auditor) close with the accepted set carrying owners and expiries
- **Intents**:
  - `hcmnext.access.detect_duty_conflict/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: observed entitlements and the conflict rule set
  - `governance`: prior acceptances and their expiries
  - `people`: the holders and their managers
- **Writes**:
  - `access`: violations, removals and accepted conflicts with their compensating controls
- **Evidence required**:
  - The observed inventory the detection ran against, with its watermark.
  - Per-violation record naming both sides and the risk.
  - Acceptances with owner, expiry and compensating control.
- **Failure and repair**:
  - A conflict is accepted with no expiry -> REJECT; a permanent acceptance is an unmanaged risk wearing the language of management
  - Detection runs against the role model rather than observed access -> the result understates by exactly the amount of drift, which is the population that matters most
  - Removing one side breaks a business process with no alternative -> the compensating control route applies and its adequacy is the approver's decision, recorded
- **Jurisdiction**:
  - US federal: SOX 404 makes segregation of duties over financial processes a standard control; a conflict without a compensating control is a deficiency an auditor will report.
  - US state variation: State regulators for licensed entities impose their own duty-segregation expectations.
  - International: DORA and NIS2 both expect access governance including conflict detection for covered entities.

---

## TAL. Talent and performance

6 workflows.

### WF-TAL-001. Run a performance review cycle to final ratings

- **Status**: `EXISTING` - internal/workflow/conformance/talent/definition.go workflow hcmnext.workflows.talent_performance_calibration
- **Archetype**: `A10` | **Complexity tier**: 4
- **Actors**: HR business partner, manager, employee, compliance officer, integration system
- **Trigger**: An annual review cycle opens for a frozen population and produces final ratings.
- **Preconditions**:
  - The cycle population is definable and can be frozen at a watermark.
  - The rating scale and its distribution guidance are published.
  - Every reviewer's authority to rate their assigned subjects is verifiable.
- **Steps**:
  1. `CAPABILITY` (HR business partner) freeze the population and resolve reviewer assignments
  2. `TASK` (employee) employees submit self-assessments where the cycle includes them
  3. `TASK` (manager) managers draft ratings and narratives against the published criteria
  4. `DECISION` (compliance officer) reject a rating submitted by someone outside the subject's authority chain
  5. `SUBWORKFLOW` (HR business partner) run calibration for the units the policy requires
  6. `APPROVAL` (manager) the second-level manager approves the calibrated ratings
  7. `CAPABILITY` (integration system) commit the ratings as immutable revisions and release them to employees
  8. `END` (HR business partner) close with the ratings available to downstream compensation and talent decisions as governed reads
- **Intents**:
  - `hcmnext.talent.run_review_cycle/v1` (PROPOSED, creates)
  - `hcmnext.talent.assign_rating/v1` (PROPOSED, creates)
- **Reads**:
  - `organization`: unit boundaries for calibration groups
  - `people`: population, assignments, manager relationships at the freeze
  - `talent`: goals, prior ratings, rating scale version
- **Writes**:
  - `talent`: rating revisions, narratives, calibration outcomes
- **Evidence required**:
  - Frozen population with its watermark and eligibility rule.
  - Rating authority evidence per reviewer.
  - Calibration session record showing which ratings moved and why.
- **Failure and repair**:
  - A reviewer rates a subject outside their authority -> TALENT_CALIBRATION_UNAUTHORIZED_RATING_BLOCKED
  - The population changes materially after the freeze -> TALENT_CALIBRATION_STALE_POPULATION_UNKNOWN; the cycle replans rather than rating people who left
  - A rating is changed after release -> TALENT_CALIBRATION_REVISION_CREATED; the original stands and a revision supersedes it
- **Jurisdiction**:
  - US federal: Ratings are evidence in ADEA, Title VII and ADA claims; a rating that changes shortly before a termination without a recorded reason is a common pretext finding.
  - US state variation: California and Illinois pay data reporting connect ratings to pay outcomes; several states give employees a right to inspect performance documents in their personnel file.
  - International: In much of the EU, performance ratings used for dismissal must follow a documented, proportionate procedure, and in Germany a works council may have codetermination rights over the rating scheme itself.

### WF-TAL-002. Calibrate ratings across a population

- **Status**: `EXISTING` - internal/workflow/conformance/talent/definition.go
- **Archetype**: `A10` | **Complexity tier**: 4
- **Actors**: HR business partner, manager, compliance officer, integration system
- **Trigger**: A calibration session compares proposed ratings across managers and adjusts them.
- **Preconditions**:
  - The session's population and participants are defined.
  - Each participant's visibility is scoped to the population under discussion.
  - The distribution guidance is guidance, not a hard quota, unless the policy says otherwise.
- **Steps**:
  1. `CAPABILITY` (HR business partner) assemble the proposed ratings and the distribution against guidance
  2. `DECISION` (compliance officer) restrict each participant's view to the subjects they may see; a calibration room is not a general disclosure
  3. `TASK` (manager) participants discuss and propose changes, each change carrying a stated reason
  4. `RULE` (compliance officer) detect a conflict where a participant is also a subject in the same session
  5. `APPROVAL` (HR business partner) the session owner approves the final set
  6. `CAPABILITY` (integration system) commit each changed rating as a revision citing the session
- **Intents**:
  - `hcmnext.talent.conduct_calibration/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: subjects and participants and their relationships
  - `talent`: proposed ratings, distribution, prior year outcomes
- **Writes**:
  - `talent`: calibrated ratings and the session record
- **Evidence required**:
  - Every change with its reason and the participant who proposed it.
  - The conflict evaluation for participants who are also subjects.
  - The pre and post distributions.
- **Failure and repair**:
  - A participant is a subject in the same session -> TALENT_CALIBRATION_CONFLICT_BLOCKED; the participant is excluded for that subject
  - A change has no recorded reason -> REJECT; an unexplained downward adjustment is the least defensible artifact in a discrimination claim
  - The distribution guidance is enforced as a hard quota -> permitted only where the policy declares it; forced distribution has been the basis of age discrimination class actions and the system records which mode was used
- **Jurisdiction**:
  - US federal: Forced ranking systems have produced significant ADEA class litigation; the reason record is the employer's defence.
  - US state variation: None state-specific, though state personnel file inspection rights can reach the calibration notes.
  - International: In codetermined jurisdictions the calibration method may require works council agreement before use.

### WF-TAL-003. Set and track goals through a period

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 12 feature goal_set_and_track
- **Archetype**: `A2` | **Complexity tier**: 1
- **Actors**: employee, manager
- **Trigger**: A worker and manager agree goals and update progress through the period.
- **Preconditions**:
  - The goal framework and its period are configured.
  - The worker has an active assignment for the period.
  - Alignment to a parent goal is optional but recorded when present.
- **Steps**:
  1. `TASK` (employee) the employee drafts goals and the manager reviews them
  2. `APPROVAL` (manager) the manager approves the goal set for the period
  3. `TASK` (employee) either party updates progress during the period, each update an immutable revision
  4. `DECISION` (manager) a mid-period goal change is a new revision with a reason, not an edit of the original
  5. `END` (manager) close at period end with the goal set frozen for the review cycle
- **Intents**:
  - `hcmnext.talent.manage_goals/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: assignment for the period
  - `talent`: goal framework, parent goals, prior period goals
- **Writes**:
  - `talent`: goal revisions and progress updates
- **Evidence required**:
  - The approved goal set at the start of the period.
  - Every revision with its reason and actor.
  - The frozen set used by the review cycle.
- **Failure and repair**:
  - Goals are changed after the review period closes -> REJECT; retroactive goal changes destroy the review's evidentiary value
  - A goal is deleted -> goals are superseded, not deleted, so the history of what was asked remains
  - The manager changes a goal without the employee seeing it -> the change is committed but the employee is notified; a silent change is the pattern that produces disputes
- **Jurisdiction**:
  - US federal: None directly; goals become evidence in performance disputes.
  - US state variation: None.
  - International: In some EU jurisdictions, variable pay tied to goals requires the goals to be set at the start of the period, and a failure to set them can entitle the worker to the full target.

### WF-TAL-004. Nominate and assess a successor for a critical role

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: HR business partner, manager, compliance officer, integration system
- **Trigger**: A succession plan for a critical position records candidates and their readiness.
- **Preconditions**:
  - The critical role set is defined by policy, not by opinion.
  - Readiness criteria are declared before assessment.
  - The confidentiality of the plan is configured, since a nomination is not an entitlement.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the critical positions and their current incumbents
  2. `TASK` (manager) leaders nominate candidates against the declared readiness criteria
  3. `RULE` (compliance officer) check the nomination pool for adverse impact before the plan is finalized
  4. `DECISION` (HR business partner) keep the plan compartmented; a nominee who learns of their nomination has an expectation the plan does not create
  5. `CAPABILITY` (integration system) commit the plan revision with its readiness assessments
  6. `END` (HR business partner) close with a review date; a stale succession plan is worse than none
- **Intents**:
  - `hcmnext.talent.manage_succession/v1` (PROPOSED, creates)
- **Reads**:
  - `organization`: critical position designation
  - `people`: incumbents and their tenure
  - `talent`: readiness assessments, performance history, career preferences
- **Writes**:
  - `talent`: succession plan revision and nomination records
- **Evidence required**:
  - Declared readiness criteria and their version.
  - Adverse impact check over the nomination pool.
  - Compartment membership for the plan.
- **Failure and repair**:
  - A nominee requests to see their own nomination status -> the disclosure follows the tenant policy; where the plan is confidential the answer is WITHHELD, and that answer is consistent for everyone
  - The pool shows adverse impact -> the finding is recorded and the plan is reviewed; suppressing it would make the plan itself evidence
  - An incumbent is a nominator for their own succession -> permitted but recorded, since the incumbent has an interest in the outcome
- **Jurisdiction**:
  - US federal: Succession plans are discoverable and have been used to show that promotion decisions were predetermined; the adverse impact check is the mitigation.
  - US state variation: None state-specific.
  - International: Under GDPR, a readiness assessment is personal data the subject can access under Article 15, which constrains how confidential a plan can actually be in the EU.

### WF-TAL-005. Record a career preference and match to internal mobility

- **Status**: `EXISTING` - internal/domains/career and internal/domains/matching packages
- **Archetype**: `A9` | **Complexity tier**: 2
- **Actors**: employee, recruiter, manager, integration system
  - performing a step: employee, recruiter, integration system
  - participating without owning a step: manager
- **Trigger**: A worker states career preferences and the system suggests internal opportunities.
- **Preconditions**:
  - Preferences are the worker's own assertion, held under their control.
  - The matcher is descriptive and never assigns anyone to anything.
  - The visibility of preferences to the current manager is a configured choice.
- **Steps**:
  1. `TASK` (employee) the employee records target roles, locations and development objectives
  2. `CAPABILITY` (integration system) match preferences and qualifications against open requisitions, producing ranked recommendations with reasons
  3. `DECISION` (employee) withhold the preference record from the current manager where the policy protects it, since a stated intent to move can be held against a worker
  4. `SIGNAL` (recruiter) notify the employee of matches; the recruiter sees only workers who opted into visibility
  5. `END` (employee) complete as a recommendation; applying is a separate act by the worker
- **Intents**:
  - `hcmnext.talent.record_career_preference/v1` (PROPOSED, creates)
  - `hcmnext.talent.match_internal_opportunity/v1` (PROPOSED, creates)
- **Reads**:
  - `career`: preference revisions
  - `qualification`: held credentials and skills
  - `recruiting`: open requisitions and their criteria
- **Writes**:
  - `career`: preference revisions
  - `matching`: recommendation set with its reasons
- **Evidence required**:
  - Preference revisions with their author.
  - Match reasons, since an unexplained ranking is not actionable.
  - The opt-in state controlling recruiter visibility.
- **Failure and repair**:
  - A manager requests their reports' preferences -> DENIED where the policy protects them; the refusal is uniform so its presence does not itself signal anything
  - The matcher recommends a role the worker is not qualified for -> the recommendation carries the unmet constraints; the matcher describes rather than decides
  - A recommendation is treated as an assignment -> FAIL_CLOSED; the matching domain has no capability to assign anyone
- **Jurisdiction**:
  - US federal: Internal mobility is subject to the same anti-discrimination law as external hiring; a matcher that systematically under-recommends a protected group is an adverse impact problem.
  - US state variation: Colorado requires notice of promotional opportunities to all employees, which constrains a purely recommendation-driven internal market.
  - International: The EU AI Act treats internal promotion decision support as high risk with the same obligations as external screening.

### WF-TAL-006. Collect continuous and multi-rater feedback outside the review cycle

- **Status**: `NEW`
- **Archetype**: `A13` | **Complexity tier**: 2
- **Actors**: employee, manager, HR business partner
- **Trigger**: Feedback is requested from peers and stakeholders between formal cycles.
- **Preconditions**:
  - The requester, the raters and the visibility model are explicit before requests go out.
  - Whether feedback is attributed or anonymous is declared and then honoured.
  - Anonymous feedback with a small rater set is not anonymous in practice.
- **Steps**:
  1. `TASK` (employee) the worker or their manager selects raters and the visibility model
  2. `SIGNAL` (employee) issue requests with the visibility model stated to each rater before they write
  3. `TASK` (employee) raters submit against the declared competencies
  4. `RULE` (HR business partner) suppress the aggregate where the rater count falls below the anonymity threshold
  5. `CAPABILITY` (manager) release feedback per the declared visibility model and no wider
  6. `END` (HR business partner) close with the feedback retained under its declared visibility
- **Intents**:
  - `hcmnext.talent.collect_feedback/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: rater and subject relationships
  - `talent`: competency framework, prior feedback, rater relationships
- **Writes**:
  - `talent`: feedback records under their declared visibility
- **Evidence required**:
  - The visibility model as stated to raters before they wrote.
  - Rater count against the anonymity threshold.
  - The release, matching the declared model exactly.
- **Failure and repair**:
  - Fewer than the threshold number of raters respond in an anonymous model -> the aggregate is suppressed and the subject is told it was suppressed; releasing two anonymous comments identifies both authors
  - A manager requests the identity behind anonymous feedback -> DENIED; the promise was made to the rater and breaking it once ends honest feedback permanently
  - Feedback is later used in a formal rating -> permitted only where the visibility model said so; repurposing anonymous developmental feedback into an evaluative decision breaks the terms it was collected under
- **Jurisdiction**:
  - US federal: Feedback used in an evaluative decision becomes discoverable and part of the selection record.
  - US state variation: State personnel file inspection rights can reach feedback filed against an employee, which constrains how anonymous it can really be.
  - International: Under GDPR the subject can access feedback about them, subject to a limited third-party exemption, so an absolute anonymity promise is not deliverable in the EU.

---

## LRN. Learning and skills

6 workflows.

### WF-LRN-001. Assign and satisfy a mandatory compliance training

- **Status**: `EXISTING` - internal/workflow/conformance/learning/definition.go workflow hcmnext.workflows.learning_credential_satisfaction
- **Archetype**: `A13` | **Complexity tier**: 3
- **Actors**: HR business partner, employee, compliance officer, external partner or carrier
- **Trigger**: A role or jurisdiction requires a training that must be completed by a deadline.
- **Preconditions**:
  - The requirement is derived from role, location and jurisdiction rather than assigned by hand.
  - The deadline is computed from the triggering event, such as hire or a rule change.
  - The evidence of completion is verifiable, not self-asserted.
- **Steps**:
  1. `RULE` (compliance officer) derive the requirement from role, location and jurisdiction with its rule version
  2. `CAPABILITY` (employee) assign the learning with its deadline and notify the worker
  3. `WAIT` (HR business partner) hold for completion with escalating reminders
  4. `OBSERVE` (external partner or carrier) receive the completion record from the provider and verify it, rather than accepting a self-claim
  5. `DECISION` (compliance officer) route an unverified claim, an expired credential or an invalid waiver to their own terminals
  6. `END` (compliance officer) close with the credential's expiry registered as a future obligation
- **Intents**:
  - `hcmnext.learning.assign_content/v1` (PROPOSED, creates)
  - `hcmnext.learning.verify_credential/v1` (PROPOSED, creates)
- **Reads**:
  - `learning`: content catalog, prior completions, credential expiry
  - `people`: role, location, hire date
  - `regulatory`: training mandates and their deadlines
- **Writes**:
  - `learning`: assignment, completion, credential record
  - `regulatory`: compliance obligation and its satisfaction
- **Evidence required**:
  - Requirement derivation with its rule version.
  - Provider completion record with its verification.
  - Credential expiry driving the next assignment.
- **Failure and repair**:
  - The worker claims completion with no provider record -> LEARNING_UNSATISFIED_UNVERIFIED_CLAIM
  - A waiver is claimed but its authority is invalid -> LEARNING_UNSATISFIED_INVALID_WAIVER
  - The credential has expired -> LEARNING_UNSATISFIED_EXPIRED; the requirement reopens rather than remaining satisfied
  - The provider is unreachable at the deadline -> LEARNING_SIMULATION_DEGRADED with the obligation still open; an unreachable provider does not satisfy a mandate
- **Jurisdiction**:
  - US federal: OSHA requires documented training for covered hazards before exposure; federal contractor obligations add their own; the record, not the completion, is what an inspection tests.
  - US state variation: California requires two hours of sexual harassment prevention training for supervisors and one hour for others every two years at employers with five or more employees; New York State and City, Illinois, Connecticut, Delaware and Maine each have their own cadence, content and record duties.
  - International: In the EU, health and safety training duties come from Directive 89/391 as implemented nationally, and works councils often have a say in the content.

### WF-LRN-002. Renew an expiring professional credential

- **Status**: `EXISTING` - internal/domains/qualification package: renewal rules and evidence kinds
- **Archetype**: `A17` | **Complexity tier**: 3
- **Actors**: employee, HR business partner, manager, compliance officer, integration system
  - performing a step: employee, manager, compliance officer, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A licence or certification that a role requires is approaching expiry.
- **Preconditions**:
  - The credential, its renewal rule and its expiry are recorded.
  - The lead time for renewal is known and the reminder schedule derives from it.
  - The consequence of lapse for the role is defined.
- **Steps**:
  1. `CAPABILITY` (integration system) detect the approaching expiry and compute the renewal window
  2. `SIGNAL` (employee) notify the worker and their manager at the configured lead times
  3. `TASK` (employee) the worker renews and submits evidence
  4. `OBSERVE` (compliance officer) verify the evidence against the issuing authority where a verification channel exists
  5. `DECISION` (manager) on lapse, suspend the entitlements and assignments the credential gated, rather than continuing silently
  6. `END` (compliance officer) close on verified renewal, or leave the suspension as the open state
- **Intents**:
  - `hcmnext.learning.renew_credential/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: entitlements the credential gates
  - `people`: assignments requiring the credential
  - `qualification`: credential, renewal rule, evidence kind
- **Writes**:
  - `access`: suspension on lapse
  - `qualification`: renewed credential with its new interval
- **Evidence required**:
  - Renewal evidence and its verification source.
  - Notification history at each lead time.
  - Suspension record where lapse occurred.
- **Failure and repair**:
  - The credential lapses while the worker continues in the role -> an immediate suspension and an incident; working unlicensed can void insurance and create personal liability
  - The issuing authority has no verification channel -> record the evidence kind as self-asserted rather than treating an uploaded PDF as verified
  - The renewal completes after the entitlement was suspended -> the entitlement is restored through the ordinary grant path, not by reversing the suspension record
- **Jurisdiction**:
  - US federal: Federal transport, healthcare and financial roles all have credential requirements whose lapse is a regulatory event for the employer, not just the worker.
  - US state variation: State professional licensing boards each set their own renewal cycles and grace periods; a nursing licence lapse in one state does not track another's calendar, which is why multi-state populations need per-jurisdiction rules.
  - International: In the EU, recognition of professional qualifications across member states is governed by Directive 2005/36, so a credential valid in one state may need recognition in another.

### WF-LRN-003. Assess a skill and record the gap against a requirement

- **Status**: `EXISTING` - internal/domains/qualification package: read-only evaluation against held credentials
- **Archetype**: `A9` | **Complexity tier**: 2
- **Actors**: employee, manager, HR business partner, integration system
- **Trigger**: A worker's skills are assessed against a role's requirements to produce a gap.
- **Preconditions**:
  - The skill taxonomy and the requirement set are published and versioned.
  - The assessment method is declared: self, manager, test or evidence-based.
  - The evaluation is descriptive and creates no work.
- **Steps**:
  1. `CAPABILITY` (integration system) read held qualifications and evidence for the worker
  2. `TRANSFORM` (integration system) evaluate each requirement and produce a satisfied, partial or unmet result with its evidence
  3. `DECISION` (HR business partner) distinguish an unmet requirement from an unassessed one; they are not the same and conflating them misleads
  4. `CAPABILITY` (manager) return the gap with its taxonomy version
  5. `END` (employee) complete with zero writes; assigning learning is a separate intent
- **Intents**:
  - `hcmnext.learning.assess_skill_gap/v1` (PROPOSED, creates)
- **Reads**:
  - `learning`: completions that count as evidence
  - `qualification`: held credentials, evidence, requirement definitions
- **Writes**:
- **Evidence required**:
  - Taxonomy and requirement versions.
  - Per-requirement result with the evidence that supported it.
  - The unassessed set, named rather than folded into unmet.
- **Failure and repair**:
  - A requirement has no evidence kind that can satisfy it -> the requirement is reported as unassessable, which is a definition defect rather than a worker gap
  - The taxonomy version changes -> prior assessments keep their version; a re-evaluation is a new artifact
  - A manager uses the gap as the sole basis for an adverse action -> outside this workflow, but the artifact records that it is descriptive evidence and not a decision
- **Jurisdiction**:
  - US federal: None directly; a skills assessment used in selection becomes a selection procedure under the Uniform Guidelines and must be job-related and validated.
  - US state variation: Illinois and Maryland regulate certain assessment technologies in hiring; state professional standards constrain who may assess clinical competence.
  - International: The EU AI Act covers assessment tools used for employment decisions as high risk.

### WF-LRN-004. Design a curriculum and publish a course catalog

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 2
- **Actors**: HR business partner, compliance officer, employee, integration system
- **Trigger**: Learning content is organized into paths with prerequisites and published to an audience.
- **Preconditions**:
  - Each item declares its owner, its audience scope, its prerequisites and its review cadence.
  - A compliance item is distinguished from a development item, because their consequences differ.
  - Retiring an item does not delete the completions recorded against it.
- **Steps**:
  1. `TASK` (HR business partner) author the content items, their prerequisites and their audience scope
  2. `RULE` (HR business partner) validate that prerequisite chains terminate and contain no cycle
  3. `APPROVAL` (compliance officer) compliance approves items that satisfy a legal mandate
  4. `CAPABILITY` (integration system) publish the catalog version with its effective interval
  5. `DECISION` (HR business partner) retire by superseding, never by deleting, so completions against the retired version remain meaningful
  6. `END` (employee) close with the review date registered
- **Intents**:
  - `hcmnext.learning.publish_catalog/v1` (PROPOSED, creates)
- **Reads**:
  - `learning`: existing items, prerequisite graph, prior completions
  - `regulatory`: mandates the items satisfy
- **Writes**:
  - `learning`: catalog version, item revisions, supersession links
- **Evidence required**:
  - Prerequisite graph validation.
  - Compliance approval for mandate-satisfying items.
  - Supersession links from retired items to their successors.
- **Failure and repair**:
  - A prerequisite chain contains a cycle -> REJECT at publication; a worker who can never become eligible is an unfixable support ticket
  - An item is deleted while completions reference it -> REJECT; the completion becomes unexplainable and a compliance record loses its meaning
  - A retired compliance item leaves a mandate uncovered -> the mandate's coverage gap is raised as a finding, because the requirement outlives the content
- **Jurisdiction**:
  - US federal: Training content that satisfies an OSHA or sector mandate must meet that mandate's content and frequency requirements; the catalog is the evidence that it did.
  - US state variation: State harassment prevention training mandates prescribe content, duration and audience in California, New York, Illinois, Connecticut, Delaware and Maine, and a single national course satisfies none of them fully.
  - International: In the EU, works councils commonly have codetermination rights over training programmes, so a catalog change can require agreement.

### WF-LRN-005. Administer a tuition reimbursement request

- **Status**: `NEW`
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: employee, manager, payroll administrator, finance partner
- **Trigger**: A worker requests reimbursement for external study under an educational assistance programme.
- **Preconditions**:
  - The programme, its annual limit, its eligible expenses and its repayment terms are published.
  - Approval precedes enrollment, because a post-hoc request has no business decision left in it.
  - The tax treatment depends on whether the programme qualifies and on the amount.
- **Steps**:
  1. `TASK` (employee) the worker requests approval before enrolling, with the course, cost and its job relatedness
  2. `APPROVAL` (finance partner) the manager approves the job relatedness and finance approves the cost against the annual limit
  3. `DOCUMENT` (employee) issue the repayment agreement where the programme requires continued service
  4. `WAIT` (manager) hold for completion evidence: the grade or certificate the programme requires
  5. `RULE` (payroll administrator) determine the taxable portion above the qualifying exclusion
  6. `CAPABILITY` (payroll administrator) reimburse through payroll with the taxable portion identified
  7. `END` (finance partner) close with the repayment obligation open for its service period
- **Intents**:
  - `hcmnext.learning.reimburse_tuition/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: the department's education budget
  - `compensation`: annual limit consumed
  - `learning`: programme terms, prior reimbursements in the year
- **Writes**:
  - `documents`: repayment agreement
  - `learning`: request, approval and completion records
  - `payroll`: reimbursement with its taxable and non-taxable split
- **Evidence required**:
  - Approval before enrollment, with its date.
  - Completion evidence meeting the programme's own standard.
  - The taxable portion computation and the signed repayment agreement.
- **Failure and repair**:
  - The worker leaves inside the repayment period -> the clawback is computed but its collection follows the jurisdiction's deduction rules; it is not netted from final pay automatically
  - The request arrives after the course is complete -> the programme rules decide, and whichever they say must be published rather than decided case by case
  - The reimbursement exceeds the qualifying exclusion -> the excess is wages and is taxed as such, which the worker is told before the payment rather than discovering on the payslip
- **Jurisdiction**:
  - US federal: IRC 127 excludes up to a statutory annual amount of employer-provided educational assistance from income; job-related education may alternatively qualify as a working condition fringe under IRC 132. The exclusion amount and its scope have been extended and modified repeatedly, so the current figure comes from the rule pack.
  - US state variation: California Labor Code 2802 can require reimbursement of expenses necessary to perform the job, which is a different and broader duty than a discretionary tuition programme; a training repayment agreement that operates as a penalty has been challenged in several states and by the Consumer Financial Protection Bureau.
  - International: In the EU, training the employer requires must generally be provided free and counted as working time under Directive 2019/1152, which makes a repayment agreement for mandatory training unlawful in several member states.

### WF-LRN-006. Handle a revoked credential and reopen its requirement

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: compliance officer, employee, HR business partner, external partner or carrier
- **Trigger**: An issuer withdraws a credential after it was recorded as satisfying a requirement.
- **Preconditions**:
  - The requirement's satisfaction was derived from the credential, so it does not survive it.
  - There is a period during which the requirement was believed met and was not, and any compliance statement covering it is wrong.
  - The revocation is the issuer's act and its reason may not be disclosed to the employer at all.
- **Steps**:
  1. `OBSERVE` (external partner or carrier) receive the revocation from the issuer and record its date and its stated scope
  2. `CAPABILITY` (compliance officer) identify every requirement the credential satisfied and reopen each one
  3. `RULE` (compliance officer) compute a new deadline for each reopened requirement from the revocation rather than from the original assignment
  4. `CAPABILITY` (compliance officer) identify the interval during which each requirement was believed met and mark any compliance statement covering it
  5. `DOCUMENT` (employee) notify the worker of the revocation, its source and what is now required of them
  6. `END` (HR business partner) close when every reopened requirement is satisfied or its own exception is granted
- **Intents**:
  - `hcmnext.learning.handle_credential_revocation/v1` (PROPOSED, creates)
  - `hcmnext.learning.reopen_requirement/v1` (PROPOSED, advances)
- **Reads**:
  - `learning`: the credential, the requirements it satisfied and their completion records
  - `people`: the roles whose requirements the credential met
  - `regulatory`: any compliance statement covering the affected interval
- **Writes**:
  - `learning`: the revoked credential and the reopened requirements
  - `regulatory`: the corrected position for the affected interval
- **Evidence required**:
  - The revocation with its date and the issuer's stated scope.
  - Every requirement reopened, identified from the credential rather than by search.
  - The interval during which each requirement was believed met.
  - The notification to the worker with its date.
- **Failure and repair**:
  - The credential is marked invalid and the requirement stays satisfied -> the compliance position is wrong in exactly the place it matters and a report covering the interval asserts something false
  - The worker is simply reassigned the course -> they repeat training with no explanation and the record shows no revocation, so a later question about the interval has no answer
  - The issuer's reason is not disclosed -> the revocation stands on its own; the employer records what it was told and does not infer a reason it was not given
  - The revocation reaches a role the worker no longer holds -> the requirement is closed as no longer applicable rather than reopened, and the closure records the role change as its basis
- **Jurisdiction**:
  - US federal: Where a licence or certification is a condition of the work, its revocation affects the worker's ability to perform the role and the employer's own obligations under the governing regime; the revocation itself is the issuer's act.
  - US state variation: State licensing boards revoke on their own procedures with their own appeal routes, and several require the employer to be notified directly rather than through the worker.
  - International: Equivalent regimes exist across the EU with mutual recognition complications where the credential was issued in another member state.

---

## EXP. Employee experience and communications

4 workflows.

### WF-EXP-001. Send a targeted communication to a resolved audience

- **Status**: `EXISTING` - internal/domains/audience package; planning/specs/messaging-and-notification-plane.md
- **Archetype**: `A13` | **Complexity tier**: 3
- **Actors**: HR business partner, employee, integration system, compliance officer
  - performing a step: HR business partner, integration system, compliance officer
  - participating without owning a step: employee
- **Trigger**: A message must reach a population defined by a rule rather than a static list.
- **Preconditions**:
  - The audience expression is governed and its evaluation is scoped to what the sender may see.
  - Delivery endpoints have a purpose eligibility, not just a validity.
  - The message classification is declared before the audience is resolved.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the audience from the governed expression and freeze an immutable audience snapshot
  2. `DECISION` (compliance officer) exclude recipients whose endpoint is not eligible for the message's purpose, and report the exclusion count
  3. `APPROVAL` (HR business partner) the message owner approves the exact content and the resolved audience size
  4. `PARALLEL` (integration system) create one child send intent per recipient or per batch partition
  5. `OBSERVE` (integration system) collect per-recipient delivery evidence; the aggregate never hides a per-recipient failure
  6. `END` (HR business partner) close with the failed set named, not summarized away
- **Intents**:
  - `hcmnext.experience.send_bulk_communication/v1` (PROPOSED, creates)
  - `hcmnext.experience.send_message/v1` (PROPOSED, creates)
- **Reads**:
  - `audience`: the governed expression, org units, eligibility
  - `people`: endpoints and their purposes
  - `privacy`: communication preferences and opt-outs
- **Writes**:
  - `experience`: audience snapshot, message intents, delivery records
- **Evidence required**:
  - The immutable audience snapshot with its resolution watermark.
  - Per-recipient delivery evidence.
  - Exclusion record with the reason for each excluded recipient.
- **Failure and repair**:
  - A recipient's only endpoint is ineligible for the purpose -> excluded with a named reason; an urgent safety message may have a different eligibility rule, which the classification selects
  - The audience resolves to more people than the approver saw -> REPLAN_AND_REAPPROVE; the approval binds the snapshot, not the expression
  - A delivery provider reports success but the message bounces later -> the late bounce updates the per-recipient record; the aggregate is recomputed rather than left stale
- **Jurisdiction**:
  - US federal: Certain notices, such as COBRA and WARN, have prescribed delivery methods where email alone is insufficient; the workflow must not treat a delivered email as satisfying a mailed-notice duty.
  - US state variation: State notice requirements vary on permissible electronic delivery, and several require affirmative consent to electronic delivery of wage statements and notices.
  - International: GDPR and the ePrivacy rules constrain unsolicited messaging even to employees for non-employment purposes; works council agreements often govern internal mass communication.

### WF-EXP-002. Launch a survey and analyze responses without re-identifying

- **Status**: `EXISTING` - internal/domains/survey package; internal/domains/pseudonym package
- **Archetype**: `A10` | **Complexity tier**: 4
- **Actors**: HR business partner, employee, compliance officer, auditor, integration system
- **Trigger**: An engagement or pulse survey is launched with a confidentiality promise.
- **Preconditions**:
  - The confidentiality model is declared: anonymous, confidential or attributed.
  - A minimum reporting group size is configured.
  - The pseudonymization boundary is technical, not procedural.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the invited population and issue pseudonymous response tokens
  2. `TASK` (employee) employees respond; the response store holds no direct identifier
  3. `RULE` (compliance officer) enforce the minimum group size on every reporting cut, including intersecting cuts
  4. `DECISION` (compliance officer) refuse a report cut that would identify an individual by combination, not just by group size
  5. `CAPABILITY` (HR business partner) publish results at the permitted granularity
  6. `END` (auditor) close with the response data retained under its own class and the token map destroyed on schedule
- **Intents**:
  - `hcmnext.experience.launch_survey/v1` (PROPOSED, creates)
  - `hcmnext.analytics.analyze_survey/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: demographic attributes used for cuts, under a permitting purpose
  - `privacy`: confidentiality model and retention
  - `survey`: campaign, question bank, invited population
- **Writes**:
  - `survey`: responses under pseudonymous tokens, aggregate results
- **Evidence required**:
  - The declared confidentiality model, shown to respondents before they answer.
  - Suppression record for every cut below the threshold.
  - Token map destruction record.
- **Failure and repair**:
  - A manager requests a cut that would identify one respondent -> DENIED with the suppression rule named; a promise of confidentiality broken once destroys every later survey
  - A free-text comment identifies its author -> the comment is withheld from the manager view and routed to a reviewer under the same confidentiality model
  - An investigation subpoenas the responses -> the legal hold applies and the token map, if it still exists, becomes discoverable; the retention schedule is what limits this exposure
- **Jurisdiction**:
  - US federal: Survey responses about workplace conduct can become evidence in a harassment claim; a confidentiality promise the employer cannot keep is itself a liability.
  - US state variation: State privacy statutes, including the CPRA's employee provisions, give access rights that can reach pseudonymous data if re-identification remains possible.
  - International: Under GDPR, pseudonymized data is still personal data; works councils in Germany and the Netherlands commonly require agreement on survey design and reporting granularity.

### WF-EXP-003. Handle an inbound message into the secure inbox

- **Status**: `PARTIAL` - planning/specs/messaging-and-notification-plane.md
- **Archetype**: `A13` | **Complexity tier**: 2
- **Actors**: employee, HR business partner, integration system
  - performing a step: HR business partner, integration system
  - participating without owning a step: employee
- **Trigger**: A worker replies to a notification or writes into the secure inbox.
- **Preconditions**:
  - The inbound channel is authenticated to the worker, not merely to an email address.
  - Content is treated as untrusted until classified.
  - The thread's classification governs who may read it.
- **Steps**:
  1. `CAPABILITY` (integration system) accept the inbound message and bind it to the authenticated worker and thread
  2. `CAPABILITY` (integration system) scan and classify content and attachments before any human sees them
  3. `DECISION` (integration system) quarantine content that fails the malware or hostile-content check rather than forwarding it
  4. `CAPABILITY` (HR business partner) route the thread to the queue its classification and subject imply
  5. `END` (HR business partner) close the intake; the response is the receiving workflow's concern
- **Intents**:
  - `hcmnext.experience.receive_inbound_message/v1` (PROPOSED, creates)
- **Reads**:
  - `experience`: thread, prior messages, classification
  - `people`: the authenticated sender
  - `privacy`: content classification result
- **Writes**:
  - `experience`: inbound message record and its routing
- **Evidence required**:
  - Sender authentication evidence, since an email from a personal address is not proof of identity.
  - Content scan result.
  - Routing decision with the queue it selected.
- **Failure and repair**:
  - The sender's address matches a worker but the message is unauthenticated -> the message is accepted into a low-trust queue and the worker is asked to confirm through an authenticated channel
  - An attachment fails the malware scan -> quarantined; the sender is told it was rejected and the content is never delivered
  - The message discloses a safety concern in a general queue -> the routing rules escalate on content signals, since a worker in danger will not always pick the right form
- **Jurisdiction**:
  - US federal: None directly, though a message reporting harassment starts the employer's duty to investigate from the date of receipt regardless of the channel used.
  - US state variation: Several state statutes make the employer's notice date the trigger for anti-retaliation protections, which makes the intake timestamp legally significant.
  - International: GDPR requires that unsolicited personal data in an inbound message still be handled under a lawful basis and retention limit.

### WF-EXP-004. Publish an announcement with acknowledgement tracking

- **Status**: `EXISTING` - planning/workflows/samples/bulk-policy-acknowledgement.md
- **Archetype**: `A13` | **Complexity tier**: 2
- **Actors**: HR business partner, employee, compliance officer
- **Trigger**: A policy change requires every affected worker to acknowledge it.
- **Preconditions**:
  - The affected population is resolvable and the acknowledgement is a legal or policy requirement.
  - The document version being acknowledged is immutable.
  - The deadline and the consequence of non-acknowledgement are defined.
- **Steps**:
  1. `CAPABILITY` (HR business partner) freeze the audience and bind the exact document version
  2. `PARALLEL` (employee) issue one acknowledgement task per worker
  3. `TASK` (employee) each worker acknowledges the specific version, not a link that may later change
  4. `WAIT` (HR business partner) hold to the deadline with reminders
  5. `DECISION` (compliance officer) escalate non-acknowledgers to their manager rather than deeming acknowledgement
  6. `END` (compliance officer) close with the acknowledged and unacknowledged sets both named
- **Intents**:
  - `hcmnext.experience.publish_announcement/v1` (PROPOSED, creates)
  - `hcmnext.documents.record_acknowledgement/v1` (PROPOSED, creates)
- **Reads**:
  - `audience`: the affected population
  - `documents`: the policy version and its digest
  - `people`: manager relationships for escalation
- **Writes**:
  - `documents`: acknowledgement records bound to the document digest
  - `experience`: announcement and its delivery evidence
- **Evidence required**:
  - Document digest each acknowledgement binds.
  - Per-worker acknowledgement with its timestamp.
  - The unacknowledged set at the deadline.
- **Failure and repair**:
  - Deemed acknowledgement is applied to non-responders -> REJECT; a deemed acknowledgement has no evidentiary value in the dispute it exists to prevent
  - The document is revised after some acknowledgements -> the revision requires a new round; the earlier acknowledgements bind the earlier digest
  - A worker is on leave and cannot acknowledge -> the task is deferred to return rather than counted as refused
- **Jurisdiction**:
  - US federal: Arbitration agreements, handbooks and safety policies all depend on provable acknowledgement of a specific version; a link-based acknowledgement to mutable content has been held unenforceable.
  - US state variation: California requires certain notices in the employee's primary language; several states require specific acknowledgement for arbitration agreements and non-competes.
  - International: In the EU, changes to core employment terms require written notice within a month under Directive 2019/1152, and works council agreement may be needed for policies affecting working conditions.

---

## HRS. HR service delivery and cases

5 workflows.

### WF-HRS-001. Open, assign and resolve an HR service request

- **Status**: `EXISTING` - planning/workflows/hr-service/catalog.md rows CreateHRRequest through ResolveCase; internal/domains/hrcase package
- **Archetype**: `A12` | **Complexity tier**: 2
- **Actors**: employee, HR business partner, manager, integration system
  - performing a step: employee, HR business partner, integration system
  - participating without owning a step: manager
- **Trigger**: A worker submits a service request that HR must resolve within a service commitment.
- **Preconditions**:
  - The request type is in the published catalog with an owner and an SLA.
  - The requester's identity and scope are resolved.
  - The queue routing rules are configured.
- **Steps**:
  1. `TASK` (employee) the worker submits the request against a published type
  2. `CAPABILITY` (integration system) classify, route and start the service clock
  3. `TASK` (HR business partner) the assignee works the case, adding notes with their own visibility class
  4. `DECISION` (HR business partner) escalate on breach of the service commitment rather than letting the clock run silently
  5. `CAPABILITY` (HR business partner) record the disposition and close
  6. `SIGNAL` (employee) notify the requester with the outcome and their appeal route if one exists
- **Intents**:
  - `hcmnext.cases.open_service_request/v1` (PROPOSED, creates)
  - `hcmnext.cases.resolve_case/v1` (PROPOSED, advances)
- **Reads**:
  - `hrcase`: case definition, queue, SLA clock
  - `knowledge`: articles relevant to the request type
  - `people`: requester and their scope
- **Writes**:
  - `hrcase`: case, notes, assignment, disposition
- **Evidence required**:
  - Case creation timestamp starting the clock.
  - Each note with its visibility class and author.
  - Disposition with its reason and the notification to the requester.
- **Failure and repair**:
  - The case is closed with no notification -> REJECT the closure; an unnotified resolution is indistinguishable from being ignored
  - A note is added at the wrong visibility class -> the class cannot be lowered after the fact without a governed reclassification, since readers may already have seen it
  - The SLA breaches -> escalate and record the breach; the clock is evidence in a service dispute
- **Jurisdiction**:
  - US federal: None directly, though a case reporting harassment or a safety hazard triggers statutory duties independent of the case system's own rules.
  - US state variation: State personnel-file inspection rights can reach HR case notes about the requesting employee, which is a reason to keep investigative notes in a separate compartment.
  - International: Under GDPR, case notes are personal data subject to access requests, with a limited exemption for third-party information.

### WF-HRS-002. Answer an employment verification request

- **Status**: `PARTIAL` - planning/workflows/hr-service/catalog.md row RequestEmploymentVerification
- **Archetype**: `A6` | **Complexity tier**: 2
- **Actors**: external partner or carrier, HR business partner, employee, compliance officer
  - performing a step: HR business partner, employee, compliance officer
  - participating without owning a step: external partner or carrier
- **Trigger**: A lender or prospective employer asks to verify employment and, sometimes, income.
- **Preconditions**:
  - The requester's authority rests on the worker's consent, not on the requester's assertion.
  - The disclosure scope is configured separately for employment, dates, title and income.
  - The jurisdiction's salary-history rules constrain what may be disclosed.
- **Steps**:
  1. `CAPABILITY` (compliance officer) verify the consent artifact and its scope before resolving the subject
  2. `DECISION` (compliance officer) refuse income disclosure where the requester is a prospective employer in a salary-history-ban jurisdiction
  3. `CAPABILITY` (HR business partner) produce the verification limited to the consented scope
  4. `SIGNAL` (employee) notify the worker that a verification was answered and what was disclosed
  5. `END` (compliance officer) complete with the disclosure record retained
- **Intents**:
  - `hcmnext.cases.answer_employment_verification/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: income, only within the consented and lawful scope
  - `people`: employment dates, title, status
  - `privacy`: consent artifact and disclosure policy
- **Writes**:
  - `hrcase`: verification record and what was disclosed
- **Evidence required**:
  - Consent artifact with its scope and expiry.
  - Exactly what was disclosed, field by field.
  - Notification to the worker.
- **Failure and repair**:
  - The requester asks for more than the consent covers -> the excess is DENIED by name; the answer is partial and says so
  - The subject is not an employee and never was -> WITHHELD; confirming non-employment can itself disclose that someone lied on an application, which is not the employer's decision to make
  - A salary-history ban applies -> income is refused with the statute named, even where the worker consented, because the ban binds the discloser in some jurisdictions
- **Jurisdiction**:
  - US federal: The FCRA applies when the verification is provided through a consumer reporting agency, bringing accuracy and dispute duties with it.
  - US state variation: California Labor Code 432.3, New York, Massachusetts, Colorado, Illinois and about twenty other jurisdictions restrict salary history in hiring; some bind only the asker, others also the discloser, which changes whether consent cures it.
  - International: In the EU, a reference is personal data and its accuracy is subject to Article 16 rectification; several member states restrict negative references.

### WF-HRS-003. Escalate a case from an AI assistant to a human

- **Status**: `EXISTING` - planning/workflows/samples/agent-assisted-hr-case-triage.md
- **Archetype**: `A12` | **Complexity tier**: 3
- **Actors**: AI agent, employee, HR business partner, compliance officer
  - performing a step: AI agent, HR business partner, compliance officer
  - participating without owning a step: employee
- **Trigger**: An assistant handling a routine question encounters something it must not answer.
- **Preconditions**:
  - The assistant's competence boundary is declared, not inferred.
  - Escalation triggers include content signals, not only explicit requests.
  - The assistant's authority never exceeds the requesting worker's own.
- **Steps**:
  1. `AGENT` (AI agent) the assistant interprets the request and proposes an answer or an action
  2. `DECISION` (AI agent) escalate on any signal of harassment, safety, medical, legal or compensation content, regardless of confidence
  3. `CAPABILITY` (HR business partner) hand off with the full conversation context to the human queue
  4. `TASK` (HR business partner) the human reviews the assistant's proposal before acting on it
  5. `END` (compliance officer) close with the assistant's contribution recorded as decision support, never as the decision
- **Intents**:
  - `hcmnext.intelligence.assist_case_triage/v1` (PROPOSED, creates)
- **Reads**:
  - `hrcase`: case content and history
  - `intelligence`: agent definition, tool permissions, memory scope
  - `knowledge`: published articles the assistant may cite
- **Writes**:
  - `hrcase`: escalation record and the human's decision
  - `intelligence`: agent execution trace and its tool invocations
- **Evidence required**:
  - The agent's execution trace and every tool it invoked.
  - The escalation trigger that fired.
  - The human decision, distinct from the agent's proposal.
- **Failure and repair**:
  - The assistant answers a legal question with confidence -> the content trigger should have escalated; a missed escalation is an incident, and the trace is what proves it
  - The assistant's tool call would read data the worker cannot see -> DENY at the tool gateway; agent initiation never increases authority
  - The worker asks the assistant to act on someone else's record -> refused on scope, and the attempt is recorded
- **Jurisdiction**:
  - US federal: An assistant that gives incorrect leave or wage advice does not shift liability from the employer; the escalation boundary is a risk control.
  - US state variation: Several states regulate automated decision tools in employment; an assistant that effectively decides a case may fall inside those rules.
  - International: The EU AI Act and GDPR Article 22 both constrain automated handling of matters with significant effects, which is why the human decision is a separate recorded act.

### WF-HRS-004. Publish and retire a knowledge article

- **Status**: `EXISTING` - internal/domains/knowledge package
- **Archetype**: `A19` | **Complexity tier**: 2
- **Actors**: HR business partner, compliance officer, employee, integration system
  - performing a step: HR business partner, compliance officer, integration system
  - participating without owning a step: employee
- **Trigger**: A policy answer is published as a versioned article with an audience and a jurisdiction.
- **Preconditions**:
  - The article declares its owner, authority, audience scope, jurisdiction and review lifecycle.
  - The body is stored as an artifact reference rather than inline.
  - Supersession links exist so older versions become stale without deletion.
- **Steps**:
  1. `TASK` (HR business partner) the owner drafts the article with its citations and its jurisdiction scope
  2. `APPROVAL` (compliance officer) compliance approves articles that state a legal position
  3. `CAPABILITY` (integration system) publish with an effective interval and an audience scope
  4. `WAIT` (HR business partner) hold to the review date
  5. `DECISION` (HR business partner) supersede rather than edit; a reader who acted on the old version must be able to see what it said
  6. `END` (compliance officer) retire with a supersession link, never a deletion
- **Intents**:
  - `hcmnext.knowledge.publish_article/v1` (PROPOSED, creates)
- **Reads**:
  - `knowledge`: existing articles, supersession chains, citations
  - `privacy`: audience scope and classification
- **Writes**:
  - `knowledge`: article revision, publication record, supersession link
- **Evidence required**:
  - Citations supporting any legal statement.
  - Approval where the article states a legal position.
  - Supersession chain from the retired version.
- **Failure and repair**:
  - An article is edited in place after publication -> REJECT; the edit becomes a new version, since the assistant and the employees cited the old one
  - An article's jurisdiction scope is wrong and workers relied on it -> the correction is published and the reliance period is identifiable from the effective intervals
  - An article has no review date -> REJECT at publication; an unreviewed policy article is how stale legal advice persists
- **Jurisdiction**:
  - US federal: An employer's published policy statement can create contractual or estoppel exposure; the version history is what limits the claim to what was actually said.
  - US state variation: State-specific policy content, such as paid sick leave rules, differs enough that a single national article is usually wrong somewhere, which is why jurisdiction scope is mandatory.
  - International: In the EU, works council agreement may be required before a policy affecting working conditions is published.

### WF-HRS-005. Escalate a case through service tiers against its clock

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 2
- **Actors**: HR business partner, employee, manager, compliance officer, integration system
- **Trigger**: A case is not resolved at its current tier and must move up before its commitment is breached.
- **Preconditions**:
  - Each tier has a scope, an owner and a response and resolution commitment.
  - The clock pauses only for defined reasons, and pausing is an act with an actor.
  - Escalation moves ownership; it does not merely notify someone.
- **Steps**:
  1. `CAPABILITY` (integration system) track the case against its tier commitments
  2. `DECISION` (HR business partner) pause the clock only for a defined reason, recorded with its actor
  3. `SIGNAL` (manager) escalate at the threshold, transferring ownership rather than copying someone in
  4. `TASK` (HR business partner) the receiving tier acknowledges ownership
  5. `DECISION` (compliance officer) escalate a case touching harassment, safety or legal risk immediately regardless of its clock
  6. `END` (employee) close with the clock history including every pause and its reason
- **Intents**:
  - `hcmnext.cases.escalate_case/v1` (PROPOSED, creates)
- **Reads**:
  - `hrcase`: case, tier definitions, clock state, prior escalations
  - `people`: the escalation chain
- **Writes**:
  - `hrcase`: escalation record, ownership transfer, clock history
- **Evidence required**:
  - Clock history with every pause, its reason and its actor.
  - Ownership transfer with the receiving tier's acknowledgement.
  - Content-based escalations, distinguished from clock-based ones.
- **Failure and repair**:
  - The clock is paused with no reason -> REJECT; an unexplained pause is how a commitment is met on paper and missed in fact
  - The escalation notifies without transferring ownership -> the case has two nominal owners and therefore none; the transfer is the escalation
  - A case is escalated on content but the receiving tier lacks the compartment -> route to a tier that holds it rather than widening the compartment
- **Jurisdiction**:
  - US federal: A case reporting harassment or a safety hazard triggers statutory duties from receipt, which is why content escalation overrides the clock.
  - US state variation: State anti-retaliation protections attach from the report date rather than from the escalation date.
  - International: The EU Whistleblower Directive requires acknowledgement within seven days and feedback within three months for reports in its scope, which is a harder commitment than most service tiers.

---

## ERL. Employee relations and investigations

5 workflows.

### WF-ERL-001. Receive a concern report and open an investigation

- **Status**: `EXISTING` - internal/workflow/conformance/hrcase/definition.go workflow hcmnext.workflows.hr_case_investigation_disposition; planning/user-flows/reference/confidential-hr-case.md
- **Archetype**: `A12` | **Complexity tier**: 5
- **Actors**: employee, compliance officer, HR business partner, auditor
  - performing a step: employee, compliance officer
  - participating without owning a step: HR business partner, auditor
- **Trigger**: A worker reports conduct that may require investigation, possibly anonymously.
- **Preconditions**:
  - The intake supports anonymity without breaking the ability to follow up.
  - A matter wall exists so the subject's chain cannot read the case.
  - The investigator assignment excludes anyone in the reporting chain of either party.
- **Steps**:
  1. `TASK` (employee) the reporter submits the concern, choosing named or anonymous intake
  2. `CAPABILITY` (compliance officer) create the case in a compartment and start the anti-retaliation watch on the reporter
  3. `RULE` (compliance officer) assign an investigator, excluding anyone with a conflict or in either party's chain
  4. `DECISION` (compliance officer) block a participant who is not authorized for the compartment from being added
  5. `TASK` (compliance officer) the investigator collects evidence and conducts interviews, each with its own record
  6. `APPROVAL` (compliance officer) the disposition is approved by an authority independent of the investigator
  7. `SIGNAL` (employee) notify the reporter of the outcome at the level the policy permits
  8. `END` (compliance officer) close with the compartment intact and the retaliation watch continuing
- **Intents**:
  - `hcmnext.cases.open_investigation/v1` (PROPOSED, creates)
  - `hcmnext.cases.record_finding/v1` (PROPOSED, advances)
- **Reads**:
  - `hrcase`: case, participants, compartment membership
  - `people`: reporting chains for conflict detection
  - `privacy`: legal hold and privilege classification
- **Writes**:
  - `documents`: evidence artifacts under the compartment
  - `hrcase`: case, interviews, findings, disposition
- **Evidence required**:
  - Compartment membership with every access logged.
  - Investigator conflict evaluation.
  - Each interview and finding as its own record with its author.
- **Failure and repair**:
  - A participant outside the compartment is added -> HRCASE_UNAUTHORIZED_PARTICIPANT_BLOCKED
  - The assigned investigator has a conflict -> HRCASE_INVESTIGATOR_CONFLICT_BLOCKED
  - A read crosses the matter wall -> HRCASE_MATTER_WALL_BREACH_BLOCKED; the attempt is recorded and the reader is not told what they missed
  - A legal hold is applied while the case is disposing -> HRCASE_LEGAL_HOLD_RACE_BLOCKED
- **Jurisdiction**:
  - US federal: Title VII imposes a duty to investigate and to take prompt remedial action; anti-retaliation protection attaches from the date of the report, which is why the watch starts at intake. Sarbanes-Oxley and Dodd-Frank protect certain whistleblowers with their own procedures.
  - US state variation: California requires an investigation to be prompt, thorough and impartial and makes the failure independently actionable; New York's disclosure rules limit non-disclosure terms in harassment settlements; several states have their own whistleblower statutes with shorter deadlines.
  - International: The EU Whistleblower Directive requires internal reporting channels, acknowledgement within seven days, feedback within three months, and strong confidentiality for the reporter's identity.

### WF-ERL-002. Dispose an investigation and take disciplinary action

- **Status**: `EXISTING` - internal/workflow/conformance/hrcase/definition.go terminals HRCASE_SIMULATION_PENDING_DISPOSITION and HRCASE_ALREADY_DISPOSED_INVALID
- **Archetype**: `A12` | **Complexity tier**: 5
- **Actors**: compliance officer, HR business partner, manager, employee
  - performing a step: compliance officer, HR business partner, employee
  - participating without owning a step: manager
- **Trigger**: An investigation concludes and a disciplinary outcome follows.
- **Preconditions**:
  - The findings are recorded and the disposition authority is identified.
  - The subject has had an opportunity to respond where policy or law requires it.
  - Any consequent employment change is a separate governed intent.
- **Steps**:
  1. `CAPABILITY` (compliance officer) assemble the findings and the evidence they rest on
  2. `TASK` (employee) the subject responds to the findings where the process requires it
  3. `APPROVAL` (compliance officer) the disposition authority approves the outcome, bound to the findings digest
  4. `DECISION` (compliance officer) reject a second disposition on an already-disposed case; reopening is a distinct act
  5. `SUBWORKFLOW` (HR business partner) route any employment consequence to its own workflow with its own approvals
  6. `SIGNAL` (employee) communicate the outcome to the subject and, at the permitted level, to the reporter
  7. `END` (compliance officer) close with the appeal window open where one exists
- **Intents**:
  - `hcmnext.cases.dispose_case/v1` (PROPOSED, creates)
  - `hcmnext.cases.take_disciplinary_action/v1` (PROPOSED, creates)
- **Reads**:
  - `hrcase`: findings, evidence, participants, prior discipline
  - `people`: employment and its terms
  - `privacy`: what may be told to the reporter
- **Writes**:
  - `hrcase`: disposition and disciplinary record
  - `people`: consequent employment change through its own intent
- **Evidence required**:
  - Findings digest the approval binds.
  - The subject's response, or the record that they declined.
  - Consistency comparison against prior similar dispositions, since inconsistent discipline is the usual discrimination proof.
- **Failure and repair**:
  - The case is already disposed -> HRCASE_ALREADY_DISPOSED_INVALID; a second disposition would create two contradictory truths
  - An appeal is filed -> HRCASE_APPEAL_REQUIRES_REOPEN; the appeal reopens rather than amending the closed disposition
  - The disciplinary outcome differs sharply from prior similar cases -> the difference is surfaced to the approver with the comparison, because unexplained inconsistency is the evidence a claimant needs
- **Jurisdiction**:
  - US federal: Title VII requires prompt and appropriate remedial action; the NLRA protects concerted activity, so discipline for a complaint made with coworkers can be an unfair labor practice. Weingarten rights entitle a union member to representation at an investigatory interview.
  - US state variation: Montana is the only state without at-will employment and requires good cause after a probationary period; several states require a written reason for separation on request; California limits non-disclosure terms in harassment matters.
  - International: In most EU states, dismissal for misconduct requires a documented procedure with a hearing and a right to be accompanied; skipping it makes the dismissal unfair regardless of the underlying conduct.

### WF-ERL-003. Apply and release a legal hold

- **Status**: `EXISTING` - planning/specs/records-management-and-disposition.md; planning/workflows/discovery-backlog.md rows ApplyLegalHold and ReleaseLegalHold
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: compliance officer, IT or access administrator, auditor, integration system
  - performing a step: compliance officer, IT or access administrator, integration system
  - participating without owning a step: auditor
- **Trigger**: Litigation or an investigation requires preservation of records that would otherwise be deleted.
- **Preconditions**:
  - The hold's scope is defined by custodian, data class and interval, not by a vague description.
  - Every system holding in-scope data can enforce or report the hold.
  - The release authority is separate from the applying authority.
- **Steps**:
  1. `CAPABILITY` (compliance officer) resolve the custodians and the data classes in scope
  2. `CAPABILITY` (IT or access administrator) apply the hold to every system, suppressing deletion and disposition
  3. `OBSERVE` (integration system) confirm each system reports the hold as active, rather than assuming it applied
  4. `SIGNAL` (compliance officer) issue preservation notices to custodians and record their acknowledgement
  5. `WAIT` (compliance officer) hold until the matter concludes
  6. `APPROVAL` (compliance officer) an authority independent of the applier approves the release
  7. `CAPABILITY` (integration system) release the hold and resume the ordinary retention schedule
- **Intents**:
  - `hcmnext.privacy.apply_legal_hold/v1` (PROPOSED, creates)
  - `hcmnext.privacy.release_legal_hold/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: artifacts and their classes
  - `people`: custodians in scope
  - `privacy`: retention schedules, disposition queues
- **Writes**:
  - `privacy`: hold record, per-system enforcement state, custodian notices
- **Evidence required**:
  - Per-system confirmation that the hold is active.
  - Custodian notices with acknowledgements.
  - Release approval, separate from the application.
- **Failure and repair**:
  - A system cannot enforce the hold technically -> a manual preservation control with a named owner, recorded as a compensating control rather than left as a gap
  - Data in scope is deleted by an automated schedule -> an immediate spoliation incident; the retention engine must consult holds before every disposition, not periodically
  - A data subject erasure request covers held data -> the hold prevails and the requester is told the request is refused on legal grounds, which is a permitted refusal
- **Jurisdiction**:
  - US federal: Federal Rule of Civil Procedure 37(e) sanctions for lost electronically stored information can include adverse-inference instructions; the duty to preserve begins when litigation is reasonably anticipated, which is earlier than a filed complaint.
  - US state variation: State e-discovery rules largely mirror the federal ones; state records retention statutes for public employers add their own minimum periods.
  - International: Under GDPR, a legal hold is a legitimate ground to refuse erasure under Article 17(3)(e), but the hold's scope must be documented and proportionate.

### WF-ERL-004. Create and monitor a performance improvement plan

- **Status**: `EXISTING` - planning/workflows/discovery-backlog.md row CreatePerformanceImprovementPlan
- **Archetype**: `A12` | **Complexity tier**: 3
- **Actors**: manager, HR business partner, employee
- **Trigger**: A worker is placed on a plan with objectives, support and a review cadence.
- **Preconditions**:
  - The performance concerns are documented before the plan, not written retrospectively.
  - The objectives are measurable and the support the employer will provide is stated.
  - The consequence of not meeting the plan is stated up front.
- **Steps**:
  1. `TASK` (manager) the manager drafts objectives, support and review dates
  2. `APPROVAL` (HR business partner) HR reviews for consistency with prior similar cases and for any protected-activity proximity
  3. `DECISION` (HR business partner) flag when the plan follows closely after a protected activity such as a leave request or a complaint
  4. `SIGNAL` (employee) deliver the plan to the employee with an acknowledgement, which is not an agreement
  5. `WAIT` (manager) hold through the review cadence, each review its own record
  6. `END` (HR business partner) close with the outcome recorded; a consequent separation is a separate intent
- **Intents**:
  - `hcmnext.cases.create_improvement_plan/v1` (PROPOSED, creates)
- **Reads**:
  - `hrcase`: recent complaints, for the proximity check
  - `leave`: recent protected leave, for the proximity check
  - `talent`: prior ratings and documented concerns
- **Writes**:
  - `hrcase`: improvement plan, review records, outcome
- **Evidence required**:
  - The documented concerns predating the plan.
  - The proximity check result.
  - Each review with its evidence and the employee's response.
- **Failure and repair**:
  - The plan begins within weeks of a protected activity -> the proximity is surfaced to HR and legal; proceeding is permitted but the timing must be justified on the record
  - Reviews are skipped -> the plan's outcome is weakened; the workflow records the missed reviews rather than backfilling them
  - The employee requests an accommodation during the plan -> the accommodation process runs in parallel and the plan objectives are re-examined against the restrictions
- **Jurisdiction**:
  - US federal: ADEA, Title VII and ADA retaliation claims often turn on temporal proximity; the plan is either the employer's best evidence or its worst, depending on whether the concerns predate the protected activity.
  - US state variation: California and New York courts scrutinize plan timing closely; some state statutes give employees a right to respond in writing to documents placed in their personnel file.
  - International: In the UK, a capability procedure with warnings, support and a right to be accompanied is expected before dismissal for poor performance.

### WF-ERL-005. Issue a documented warning under a progressive discipline policy

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 2
- **Actors**: manager, HR business partner, employee
- **Trigger**: A performance or conduct issue warrants a documented step short of a plan or an investigation.
- **Preconditions**:
  - The policy's steps and their sequence are published, and skipping a step requires a recorded reason.
  - The worker has an opportunity to respond and their response is retained.
  - Consistency with prior similar cases is checked before issue.
- **Steps**:
  1. `TASK` (manager) the manager drafts the warning with the specific conduct, its dates and the expected change
  2. `CAPABILITY` (HR business partner) compare against prior similar dispositions and surface any inconsistency to HR
  3. `DECISION` (HR business partner) flag proximity to a recent protected activity such as a complaint or a leave request
  4. `SIGNAL` (employee) deliver the warning and record the worker's acknowledgement, which is not agreement
  5. `TASK` (employee) retain the worker's written response where they provide one
  6. `END` (manager) close with the warning's expiry, if the policy gives one, registered
- **Intents**:
  - `hcmnext.cases.issue_disciplinary_warning/v1` (PROPOSED, creates)
- **Reads**:
  - `hrcase`: prior discipline, similar cases and their outcomes
  - `leave`: recent protected leave for the proximity check
  - `talent`: documented performance concerns
- **Writes**:
  - `hrcase`: warning record, acknowledgement, worker response
- **Evidence required**:
  - The specific conduct, its dates and the expected change, rather than a general characterization.
  - The consistency comparison against prior similar cases.
  - The worker's response, retained alongside the warning.
- **Failure and repair**:
  - A step is skipped with no recorded reason -> the policy's own sequence becomes evidence against the employer; the reason is required at issue, not reconstructed later
  - The warning follows within weeks of a protected activity -> the proximity is surfaced to HR; proceeding is permitted but the timing must be justified on the record
  - The worker refuses to acknowledge -> the refusal is recorded and the warning stands; acknowledgement evidences receipt, not agreement
- **Jurisdiction**:
  - US federal: Discipline is evidence in Title VII, ADEA and ADA claims, and inconsistent discipline for similar conduct is the usual proof of pretext; the NLRA protects concerted activity, so discipline for a complaint raised with coworkers can be an unfair labor practice.
  - US state variation: Several states give employees a right to inspect and to respond in writing to documents in their personnel file, and Montana requires good cause for discharge after a probationary period, which makes the documented sequence materially more important there.
  - International: In the UK, the ACAS code expects a documented process with a right to be accompanied, and a failure to follow it can increase compensation by up to 25 percent.

---

## MOB. Global mobility and immigration

6 workflows.

### WF-MOB-001. Set up an international assignment with dated legs

- **Status**: `EXISTING` - internal/workflow/conformance/mobility/definition.go workflow hcmnext.workflows.mobility_cross_border_privacy
- **Archetype**: `A8` | **Complexity tier**: 5
- **Actors**: HR business partner, compliance officer, payroll administrator, external partner or carrier, finance partner, integration system
  - performing a step: HR business partner, compliance officer, external partner or carrier, integration system
  - participating without owning a step: payroll administrator, finance partner
- **Trigger**: A worker is assigned to work in another country for a defined period.
- **Preconditions**:
  - Each leg of the assignment has explicit start and end dates and a work country.
  - Home and host payroll positions are both determinable.
  - The privacy transfer basis for moving the worker's data to the host is established.
- **Steps**:
  1. `CAPABILITY` (HR business partner) read the assignment legs and validate they do not overlap
  2. `CAPABILITY` (compliance officer) verify work authorization for each host country and its milestone status
  3. `RULE` (compliance officer) assess permanent establishment and tax residency exposure for each leg
  4. `RULE` (compliance officer) assess the privacy transfer basis for host-country processing
  5. `APPROVAL` (compliance officer) home payroll, host payroll, immigration, privacy and tax all approve their own stream
  6. `CAPABILITY` (integration system) commit the assignment with its dated legs and its payroll split
  7. `OBSERVE` (external partner or carrier) observe the immigration vendor and the host payroll registration
  8. `END` (compliance officer) close with renewal reminders and the tax review as open obligations
- **Intents**:
  - `hcmnext.mobility.create_international_assignment/v1` (PROPOSED, creates)
  - `hcmnext.mobility.verify_work_authorization/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: home and host payroll capability
  - `people`: worker, nationality, current assignment
  - `privacy`: transfer mechanism for the host country
  - `regulatory`: immigration status, tax treaty, social security agreement
- **Writes**:
  - `mobility`: assignment legs, authorization records, tax assessment
  - `payroll`: split payroll arrangement
  - `privacy`: transfer assessment
- **Evidence required**:
  - Dated leg evidence, since tax residency turns on day counts.
  - Authorization milestones with their expiry dates.
  - Privacy transfer assessment and its mechanism.
- **Failure and repair**:
  - Legs overlap -> MOBILITY_OVERLAPPING_ASSIGNMENT_BLOCKED; a worker cannot be tax-resident in two places on the same day under the model
  - Work authorization is missing or a milestone is incomplete -> MOBILITY_AUTHORIZATION_BLOCKED or MOBILITY_AUTHORIZATION_MILESTONE_INCOMPLETE
  - The privacy transfer basis does not cover the host -> MOBILITY_PRIVACY_TRANSFER_BLOCKED
  - Permanent establishment impact cannot be determined -> MOBILITY_TAX_PE_IMPACT_UNKNOWN; unknown is reported, never assumed to be nil
- **Jurisdiction**:
  - US federal: US citizens and residents remain taxable on worldwide income with the foreign earned income exclusion and foreign tax credit as relief; totalization agreements with about thirty countries prevent double social security contributions and require a certificate of coverage.
  - US state variation: State tax residency does not end merely because the worker left the country; California, New York and Virginia apply domicile tests that can keep a worker taxable during a foreign assignment.
  - International: Within the EU, Regulation 883/2004 and the A1 certificate govern which state's social security applies; the Posted Workers Directive imposes host-country pay and conditions after set periods; Schengen day counts are separate from tax day counts and both must be tracked.

### WF-MOB-002. Renew an expiring work authorization before it lapses

- **Status**: `EXISTING` - internal/workflow/conformance/mobility/definition.go terminal MOBILITY_AUTHORIZATION_EXPIRING_REVIEW
- **Archetype**: `A17` | **Complexity tier**: 4
- **Actors**: compliance officer, employee, HR business partner, external partner or carrier, integration system
  - performing a step: compliance officer, employee, external partner or carrier, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A visa or permit approaches expiry and the worker's ability to work depends on renewal.
- **Preconditions**:
  - The authorization's expiry and the renewal lead time are recorded.
  - The renewal path and its dependencies, such as a labour market test, are known.
  - The consequence of lapse, which is that the worker cannot lawfully work, is enforced.
- **Steps**:
  1. `CAPABILITY` (integration system) detect the approaching expiry against the renewal lead time
  2. `SIGNAL` (employee) notify the worker, HR and the immigration vendor at the configured lead times
  3. `TASK` (external partner or carrier) the vendor files the renewal and reports milestone progress
  4. `OBSERVE` (integration system) record each milestone and the final decision
  5. `DECISION` (compliance officer) on lapse without renewal, suspend work rather than continuing; unauthorized employment is a knowing violation
  6. `END` (compliance officer) close on the new authorization interval or on the suspension
- **Intents**:
  - `hcmnext.mobility.renew_work_authorization/v1` (PROPOSED, creates)
- **Reads**:
  - `mobility`: authorization, expiry, renewal history
  - `people`: assignment and work location
  - `regulatory`: renewal rules and lead times
- **Writes**:
  - `mobility`: renewal case and milestone records
  - `people`: work suspension if lapse occurs
- **Evidence required**:
  - Milestone timeline with the vendor's evidence.
  - Notification history at each lead time.
  - The suspension record where lapse occurred.
- **Failure and repair**:
  - The renewal is filed but not decided by expiry -> many jurisdictions grant continued work authorization on a timely filing; the rule is jurisdiction-specific and the workflow must apply the right one rather than a general assumption
  - The worker travels during the renewal -> the travel can abandon the application in some categories; the workflow warns rather than silently allowing it
  - The vendor reports success without a document -> UNKNOWN; an authorization without evidence cannot support employment
- **Jurisdiction**:
  - US federal: Continuing to employ a worker after authorization lapses is a knowing violation under IRCA with per-worker penalties; timely-filed H-1B extensions permit up to 240 days of continued work, which is a specific rule and not a general one.
  - US state variation: State E-Verify mandates interact with reverification; California restricts the employer from reverifying earlier than federal law requires.
  - International: In the UK, continued employment after permission expires ends the statutory excuse and exposes the employer to civil penalties and, for knowing employment, criminal liability.

### WF-MOB-003. Administer a relocation package and its tax gross-up

- **Status**: `NEW`
- **Archetype**: `A8` | **Complexity tier**: 4
- **Actors**: HR business partner, payroll administrator, finance partner, external partner or carrier
- **Trigger**: A relocated worker receives benefits with mixed taxability that must be grossed up.
- **Preconditions**:
  - The package components and their taxability by jurisdiction are configured.
  - The gross-up method is declared: flat, inverse or marginal.
  - The repayment agreement, if any, is signed before payment.
- **Steps**:
  1. `CAPABILITY` (payroll administrator) assemble the package components and determine each one's taxability
  2. `TRANSFORM` (payroll administrator) compute the gross-up under the declared method for every affected jurisdiction
  3. `DOCUMENT` (HR business partner) issue the repayment agreement where the package is conditional on continued service
  4. `APPROVAL` (finance partner) finance approves the total cost including the gross-up, which often exceeds the benefit itself
  5. `CAPABILITY` (external partner or carrier) dispatch payments and vendor authorizations
  6. `OBSERVE` (finance partner) reconcile vendor invoices against the authorized package
  7. `END` (HR business partner) close with the repayment clawback as an open obligation for its term
- **Intents**:
  - `hcmnext.mobility.create_relocation_package/v1` (PROPOSED, creates)
- **Reads**:
  - `mobility`: package template, prior relocations
  - `payroll`: withholding rates for the gross-up
  - `regulatory`: taxability of each component by jurisdiction
- **Writes**:
  - `documents`: repayment agreement
  - `mobility`: package record and vendor authorizations
  - `payroll`: taxable benefit lines and gross-up
- **Evidence required**:
  - Per-component taxability determination.
  - Gross-up computation with its method.
  - Signed repayment agreement with its term and schedule.
- **Failure and repair**:
  - The worker leaves within the repayment term -> the clawback is computed but its collection follows the jurisdiction's deduction rules; it is not simply netted from final pay
  - A vendor invoices above the authorized amount -> the excess is held for approval rather than paid; relocation vendor overruns are a common leakage
  - The gross-up under-provides because a local tax was missed -> the shortfall becomes a correction with its own approval, and the worker is told
- **Jurisdiction**:
  - US federal: The Tax Cuts and Jobs Act made nearly all employer-paid moving expenses taxable wages through 2025 and the rules continue to be revisited, so the taxability determination must be version-pinned rather than assumed.
  - US state variation: State conformity to the federal moving expense treatment varies; California, New York, Massachusetts and several others did not conform, so a component can be taxable federally and excludable at state level.
  - International: In the EU, relocation allowances often have statutory tax-free ceilings; exceeding them makes only the excess taxable, which is a different mechanic from the US all-or-nothing treatment.

### WF-MOB-004. Sponsor a permanent residence application

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 5
- **Actors**: compliance officer, employee, HR business partner, external partner or carrier, hiring manager
  - performing a step: compliance officer, HR business partner, external partner or carrier, hiring manager
  - participating without owning a step: employee
- **Trigger**: The employer sponsors a worker for permanent residence, which requires a labour market test before the petition.
- **Preconditions**:
  - The position, its requirements and its wage are fixed before the process starts and cannot be tailored to the individual.
  - The prevailing wage determination is obtained from the labour authority rather than estimated.
  - The recruitment steps and their timing are prescribed and auditable.
- **Steps**:
  1. `CAPABILITY` (external partner or carrier) request the prevailing wage determination for the position and its location
  2. `WAIT` (compliance officer) hold for the determination, which sets the wage floor for the offered position
  3. `TASK` (hiring manager) conduct the prescribed recruitment and record every applicant and the lawful reason each was not selected
  4. `DECISION` (compliance officer) if a minimally qualified and available worker applies, the certification cannot proceed; the recruitment is a real test, not a formality
  5. `CAPABILITY` (external partner or carrier) file the labour certification with the audit file assembled and retained
  6. `WAIT` (compliance officer) hold for certification, then file the immigrant petition and track priority date movement
  7. `END` (HR business partner) close the sponsorship stage; the worker's status and its expiry continue to be tracked separately
- **Intents**:
  - `hcmnext.mobility.sponsor_permanent_residence/v1` (PROPOSED, creates)
- **Reads**:
  - `organization`: the position, its requirements and its location
  - `people`: the worker's current status and its expiry
  - `recruiting`: applicants, their qualifications and the rejection reasons
  - `regulatory`: prevailing wage determination and filing requirements
- **Writes**:
  - `compensation`: the wage floor bound to the position
  - `mobility`: sponsorship case, prevailing wage determination, certification and petition references
  - `recruiting`: the audit file of recruitment and rejections
- **Evidence required**:
  - The prevailing wage determination and its validity period.
  - The complete recruitment record: every applicant, their qualifications and the lawful, job-related reason for each rejection.
  - The audit file retained for the statutory period, assembled at filing rather than reconstructed later.
- **Failure and repair**:
  - A qualified and available applicant responds to the recruitment -> the certification cannot proceed; treating the recruitment as a formality and rejecting a qualified applicant to preserve the sponsorship is fraud
  - The position requirements are written to match only the sponsored worker -> the requirements fail the normal-requirements test and the application is denied; tailoring is the most common substantive denial ground
  - The offered wage falls below the prevailing wage determination -> REJECT; the wage floor is a condition of the certification and paying below it after approval is itself a violation
  - The worker's temporary status expires before the petition is approved -> an extension or a change of status is a separate obligation with its own deadline; the sponsorship does not preserve work authorization by itself
- **Jurisdiction**:
  - US federal: The PERM labour certification process under 20 CFR 656 requires a prevailing wage determination, prescribed recruitment, a test of the labour market, and a retained audit file for five years; the immigrant petition and priority date mechanics then govern how long the worker waits.
  - US state variation: State law does not govern sponsorship, but the recruitment must still comply with state pay transparency posting rules, which can require a range on the very advertisements the federal process prescribes.
  - International: The EU Blue Card Directive and national permanent residence routes have entirely different mechanics, generally without an employer-run labour market test of this shape.

### WF-MOB-005. Track short-term business travellers and their shadow payroll exposure

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 5
- **Actors**: compliance officer, payroll administrator, employee, finance partner, integration system
- **Trigger**: Workers travel across borders for short assignments and accumulate day counts that create tax and payroll obligations.
- **Preconditions**:
  - Travel days are captured from a source that is authoritative enough to defend, not from expense claims alone.
  - The treaty thresholds, the domestic day thresholds and the permanent establishment tests are configured per country pair.
  - The shadow payroll obligation, where it exists, is a reporting obligation and not a second payment.
- **Steps**:
  1. `CAPABILITY` (integration system) ingest travel days per worker per country from the authoritative source
  2. `RULE` (compliance officer) evaluate each country's domestic threshold, the applicable treaty article and the employer's own presence
  3. `DECISION` (compliance officer) distinguish three outcomes: no obligation, a reporting obligation through shadow payroll, and a permanent establishment risk that is a corporate tax question rather than a payroll one
  4. `SIGNAL` (employee) alert the worker and their manager before a threshold is crossed rather than after
  5. `CAPABILITY` (payroll administrator) establish shadow payroll reporting in the host country where the obligation exists
  6. `END` (finance partner) close the period with the day counts, the determinations and the open obligations retained
- **Intents**:
  - `hcmnext.mobility.track_business_traveller/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: home country payroll for the shadow calculation
  - `people`: travel days, work location per day, nationality and residence
  - `regulatory`: domestic thresholds, treaty articles and permanent establishment tests per country pair
- **Writes**:
  - `mobility`: day counts and threshold determinations
  - `payroll`: shadow payroll reporting obligations
  - `regulatory`: permanent establishment risk findings
- **Evidence required**:
  - Day counts with their source, since a count built from expense claims will not survive an audit.
  - Per-country determination with the treaty article and the domestic threshold applied.
  - The alert issued before the threshold was crossed, with its date.
- **Failure and repair**:
  - A threshold is crossed before anyone is alerted -> the obligation arises retroactively, with penalties running from the crossing; the alert exists because retroactive shadow payroll registration is far more expensive than a declined trip
  - Travel days are recorded only from approved expense claims -> the count is incomplete and the determination is unreliable; the gap is recorded as a data quality finding rather than presented as a clean answer
  - The activity performed creates a permanent establishment regardless of day count -> the finding escalates to corporate tax; a payroll workflow cannot resolve a permanent establishment question and must not appear to
- **Jurisdiction**:
  - US federal: US citizens and residents remain taxable on worldwide income; the foreign earned income exclusion has its own physical presence and bona fide residence tests that day counts feed directly.
  - US state variation: US states apply their own day-count thresholds to non-resident workers, and they differ sharply: New York has effectively a one-day threshold for many workers while Illinois, Georgia and others use 30 days; a domestic business traveller can therefore create a state filing obligation on the first day.
  - International: Treaty articles on dependent personal services commonly exempt an employee below 183 days in a twelve-month period provided the employer is not resident in and does not bear the cost in the host state, which means the exemption is lost precisely when a host entity recharges the cost.

### WF-MOB-006. Administer tax equalization for an expatriate assignment

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 5
- **Actors**: payroll administrator, compliance officer, employee, external partner or carrier, finance partner
- **Trigger**: An assignee is kept whole against home-country tax and the employer settles the difference.
- **Preconditions**:
  - The equalization policy, its hypothetical tax basis and its settlement cadence are agreed in writing before departure.
  - The tax provider computes the hypothetical and actual positions; the employer does not.
  - Settlement can run for years after the assignment ends.
- **Steps**:
  1. `TASK` (employee) agree and record the equalization terms and the hypothetical tax basis before departure
  2. `CAPABILITY` (payroll administrator) withhold hypothetical tax from the assignee's pay each period
  3. `CAPABILITY` (finance partner) fund the actual home and host tax liabilities as they fall due
  4. `OBSERVE` (external partner or carrier) receive the provider's annual equalization calculation
  5. `DECISION` (payroll administrator) settle the balance in either direction, including recovering from the assignee where the calculation says so
  6. `WAIT` (compliance officer) hold the settlement obligation open through the trailing years after repatriation
  7. `END` (finance partner) close only when the final trailing settlement is agreed, which is commonly two to three years after the assignment ends
- **Intents**:
  - `hcmnext.mobility.administer_tax_equalization/v1` (PROPOSED, creates)
- **Reads**:
  - `mobility`: assignment terms, equalization policy, prior settlements
  - `payroll`: hypothetical withholding and actual gross
  - `regulatory`: home and host tax positions from the provider
- **Writes**:
  - `mobility`: equalization calculations and open trailing obligations
  - `payroll`: hypothetical tax withholding and settlement payments or recoveries
- **Evidence required**:
  - The written equalization terms agreed before departure, which is what the settlement rests on.
  - Annual calculation from the provider with its inputs.
  - Each settlement in either direction, with the trailing obligation and its expected final year.
- **Failure and repair**:
  - The assignee owes a settlement to the employer and has left the company -> the recovery follows the jurisdiction's deduction rules and, in practice, a contractual claim; a payroll deduction from a former employee is not available in most jurisdictions
  - The equalization terms were never agreed in writing -> the settlement is unenforceable and the exposure is the employer's; the workflow refuses to start an assignment without them
  - Trailing settlements are closed with the assignment -> REJECT; closing the obligation dimension when the assignment ends hides two to three years of real liability
- **Jurisdiction**:
  - US federal: Hypothetical tax is not a tax and has no statutory basis; it is a contractual reduction, and treating it as withholding for reporting purposes is an error. Employer-paid tax is itself compensation and grosses up, which is why the calculation iterates.
  - US state variation: State residency does not end on departure: California, New York and Virginia apply domicile tests that can keep an assignee taxable at state level throughout a foreign assignment.
  - International: Host-country tax equalization can be treated as taxable benefit in kind in several jurisdictions, and social security follows the totalization agreement or the A1 certificate rather than the tax position, so the two can point at different countries for the same worker.

---

## SAF. Safety and workplace

6 workflows.

### WF-SAF-001. Report a workplace injury and file a workers compensation claim

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 4
- **Actors**: employee, manager, HR business partner, external partner or carrier, compliance officer, integration system
  - performing a step: employee, HR business partner, external partner or carrier, compliance officer, integration system
  - participating without owning a step: manager
- **Trigger**: A worker is injured and the employer must record, report and file within statutory deadlines.
- **Preconditions**:
  - The incident intake is available to the worker and the manager without a gatekeeper.
  - The recordability determination is separate from the claim decision.
  - The carrier and the state reporting deadlines are both configured.
- **Steps**:
  1. `TASK` (employee) the incident is reported with date, time, location, task and injury description
  2. `RULE` (compliance officer) determine recordability under the occupational safety rules, which is not the same as claim compensability
  3. `CAPABILITY` (external partner or carrier) file the claim with the carrier or state fund within the deadline
  4. `CAPABILITY` (compliance officer) record on the injury log where recordable, and report immediately where the severity requires it
  5. `SUBWORKFLOW` (HR business partner) route resulting absence to the leave workflow and any restriction to the accommodation workflow
  6. `OBSERVE` (integration system) track the claim to determination and record the outcome
  7. `END` (compliance officer) close with the log entry and the anti-retaliation watch both active
- **Intents**:
  - `hcmnext.safety.report_incident/v1` (PROPOSED, creates)
  - `hcmnext.safety.file_workers_comp_claim/v1` (PROPOSED, creates)
- **Reads**:
  - `leave`: resulting absence
  - `people`: worker, assignment, work location
  - `regulatory`: recordability rules and filing deadlines
  - `safety`: prior incidents, hazard records
- **Writes**:
  - `leave`: absence linked to the incident
  - `privacy`: medical information in a restricted compartment
  - `safety`: incident record, injury log entry, claim reference
- **Evidence required**:
  - Incident record with its report timestamp, which starts several clocks.
  - Recordability determination with its rule.
  - Claim filing confirmation and the carrier's determination.
- **Failure and repair**:
  - A severe injury is not reported to the regulator within the statutory window -> the miss is recorded with its exposure; fatality and hospitalization reporting windows are measured in hours, not days
  - A manager discourages the report -> the intake accepts direct worker reporting without manager approval precisely to prevent this, and any manager suppression is a retaliation matter
  - The claim is denied but the injury is recordable -> both remain true; compensability and recordability are separate determinations and the system does not collapse them
  - The worker reports the injury after the state's employee notice deadline -> the claim is still filed with the late report and its reason recorded; applying the deadline is the carrier's or the state's decision, and an employer that suppresses a late claim rather than filing it creates its own liability
  - The injured worker is a contingent worker supplied by a staffing agency -> the dual-coverage question is resolved before filing: depending on the state, the agency policy, the client policy or both may respond, so the workflow routes the agency as primary filer while the client records the incident on its own log where the borrowed-servant test applies
- **Jurisdiction**:
  - US federal: OSHA requires recording on Forms 300, 300A and 301, reporting a fatality within 8 hours and an inpatient hospitalization, amputation or eye loss within 24 hours, and forbids retaliation for reporting; the anti-retaliation rule also constrains blanket post-incident drug testing.
  - US state variation: Workers compensation is entirely state law: Texas allows employers to opt out of the system entirely, Ohio, North Dakota, Washington and Wyoming operate monopolistic state funds, and first-report-of-injury deadlines range from 24 hours to 30 days; medical provider choice rules differ sharply.
  - International: In the EU, reporting duties come from national implementations of Directive 89/391; RIDDOR in the UK sets its own reportable categories and deadlines.

### WF-SAF-002. Manage a work restriction and modified duty

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 3
- **Actors**: HR business partner, manager, employee, compliance officer, integration system
  - performing a step: HR business partner, manager, compliance officer, integration system
  - participating without owning a step: employee
- **Trigger**: A medical restriction limits what a worker may do and modified duty is arranged.
- **Preconditions**:
  - The restriction is a functional statement, not a diagnosis.
  - The essential functions of the role are documented.
  - The modified duty assignment has an end or review date.
- **Steps**:
  1. `TASK` (HR business partner) the restriction is received into a protected compartment
  2. `TRANSFORM` (HR business partner) map the restriction against the role's essential and marginal functions
  3. `DECISION` (compliance officer) route to the accommodation process where an essential function is affected, and to a temporary modified duty where only marginal ones are
  4. `CAPABILITY` (integration system) commit the restriction and any modified duty with its review date
  5. `SIGNAL` (manager) tell the manager the functional limits only
  6. `WAIT` (HR business partner) hold to the review date and re-evaluate
- **Intents**:
  - `hcmnext.safety.manage_work_restriction/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: role and essential functions
  - `safety`: restriction, its source and its interval
  - `time`: schedule the restriction affects
- **Writes**:
  - `safety`: work restriction record and modified duty assignment
  - `time`: schedule adjustments
- **Evidence required**:
  - The functional restriction as communicated, separate from the medical source.
  - The essential function mapping.
  - Review date and its outcome.
- **Failure and repair**:
  - Modified duty continues indefinitely with no review -> the review date is mandatory; an unreviewed modified duty becomes the new job by practice
  - The manager assigns work outside the restriction -> an incident with safety and liability consequences; the restriction is enforced at assignment, not by memory
  - The worker's restriction changes but the old one is still in force in the schedule -> the schedule is replanned; a stale restriction is as dangerous as none
- **Jurisdiction**:
  - US federal: The ADA governs when a restriction becomes an accommodation question; OSHA's general duty clause applies to assigning work that a known restriction forbids.
  - US state variation: State workers compensation rules on modified duty affect benefit levels: refusing suitable modified duty can end wage replacement in many states, which makes the offer's documentation important.
  - International: In the EU, occupational health services often make the fitness determination and the employer must follow it; in Germany the Wiedereingliederung provides a structured phased return.

### WF-SAF-003. Conduct a safety investigation and track corrective actions

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 3
- **Actors**: compliance officer, manager, employee, auditor
  - performing a step: compliance officer, manager, auditor
  - participating without owning a step: employee
- **Trigger**: A significant incident or near miss requires root cause analysis and corrective action.
- **Preconditions**:
  - The incident record exists with its evidence preserved.
  - The investigation is separate from the claim and from any discipline.
  - Corrective actions have owners and verification steps.
- **Steps**:
  1. `CAPABILITY` (compliance officer) preserve the scene evidence, the equipment state and the witness list
  2. `TASK` (compliance officer) interview witnesses and the affected worker, recording each separately
  3. `TRANSFORM` (compliance officer) identify contributing factors and root causes rather than a single cause
  4. `TASK` (manager) define corrective actions with owners and verification criteria
  5. `WAIT` (auditor) track each action to verified completion
  6. `END` (compliance officer) close only when every action is verified, not when it is assigned
- **Intents**:
  - `hcmnext.safety.conduct_investigation/v1` (PROPOSED, creates)
- **Reads**:
  - `asset`: equipment involved and its maintenance history
  - `people`: witnesses and the affected worker
  - `safety`: incident, prior similar incidents, hazard register
- **Writes**:
  - `safety`: investigation record, findings, corrective actions
- **Evidence required**:
  - Preserved physical and documentary evidence.
  - Each interview as its own record.
  - Corrective action verification, not just assignment.
- **Failure and repair**:
  - Discipline is applied before the investigation concludes -> the investigation's independence is compromised and the discipline becomes evidence of retaliation for reporting
  - The same contributing factor recurs -> the prior corrective action failed and the escalation is to the hazard's owner, not to another action of the same kind
  - Evidence is not preserved before the area is cleared -> the investigation records the gap; reconstructing a scene from memory is weak evidence in an enforcement action
- **Jurisdiction**:
  - US federal: OSHA's general duty clause and the specific standards both make the corrective action, not the investigation, the compliance obligation; the anti-retaliation provision protects the reporter.
  - US state variation: State plan states, including California, Oregon, Washington and Michigan, operate their own programmes with standards at least as effective as federal ones and sometimes stricter, such as California's Injury and Illness Prevention Program requirement.
  - International: EU framework Directive 89/391 requires employers to evaluate risks and to keep a list of occupational accidents; national implementations set the reporting thresholds.

### WF-SAF-004. Run a hazard assessment and assign controls

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 2
- **Actors**: compliance officer, manager, employee
- **Trigger**: A workplace or task is assessed for hazards and controls are assigned by the hierarchy of controls.
- **Preconditions**:
  - The assessment scope is a task or a location, not the organization in general.
  - Controls are ranked: elimination, substitution, engineering, administrative, then personal protective equipment.
  - Worker participation in the assessment is recorded.
- **Steps**:
  1. `TASK` (employee) identify hazards for the scoped task with worker participation
  2. `TRANSFORM` (compliance officer) rate likelihood and severity to produce a risk level
  3. `TASK` (manager) assign controls following the hierarchy, recording why a higher control was rejected
  4. `DECISION` (compliance officer) refuse an assessment whose only control is personal protective equipment for a high-severity hazard without a recorded reason
  5. `END` (compliance officer) close with a reassessment date
- **Intents**:
  - `hcmnext.safety.assess_hazard/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: the workers exposed
  - `safety`: hazard register, prior assessments, incident history
- **Writes**:
  - `safety`: assessment, risk ratings, assigned controls
- **Evidence required**:
  - Worker participation record.
  - The hierarchy reasoning, including rejected higher controls.
  - Reassessment date.
- **Failure and repair**:
  - The assessment is done without the workers who do the task -> the participation gap is recorded; assessments written from a desk miss the actual practice
  - A control is assigned with no owner -> REJECT; an unowned control is not implemented
  - The reassessment date passes -> the assessment is marked stale and the hazard reverts to unassessed rather than remaining nominally controlled
- **Jurisdiction**:
  - US federal: OSHA requires hazard assessment for personal protective equipment under 29 CFR 1910.132 and for specific standards; the general duty clause covers recognized hazards without a specific standard.
  - US state variation: California requires a written Injury and Illness Prevention Program with hazard assessment and correction; several other state plan states have analogous programme requirements.
  - International: The EU framework directive requires documented risk assessment and consultation with workers or their representatives.

### WF-SAF-005. Administer a return from a workers compensation absence

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 4
- **Actors**: HR business partner, payroll administrator, employee, external partner or carrier, integration system
  - performing a step: HR business partner, payroll administrator, external partner or carrier, integration system
  - participating without owning a step: employee
- **Trigger**: A worker on a compensation claim is cleared to return and benefits, pay and duty must coordinate.
- **Preconditions**:
  - The claim's benefit state and the employer's leave state are tracked separately.
  - The return clearance comes from the treating provider through the claim channel.
  - Modified duty availability is a real determination, not a formality.
- **Steps**:
  1. `OBSERVE` (external partner or carrier) receive the return clearance and any restrictions through the claim channel
  2. `CAPABILITY` (HR business partner) determine whether modified duty within the restrictions exists
  3. `DECISION` (HR business partner) record a genuine offer or a genuine unavailability; a sham offer to end benefits is a bad-faith claim
  4. `CAPABILITY` (payroll administrator) coordinate the wage replacement stop with the pay restart so no gap occurs
  5. `CAPABILITY` (integration system) commit the return, the restrictions and the duty assignment
  6. `END` (HR business partner) close the absence while the claim's medical portion may remain open
- **Intents**:
  - `hcmnext.safety.administer_return/v1` (PROPOSED, creates)
  - `hcmnext.leave.evaluate_return_to_work/v1` (PROPOSED, advances)
- **Reads**:
  - `leave`: concurrent protected leave entitlements
  - `payroll`: wage replacement offsets
  - `safety`: claim state, benefit payments, restrictions
- **Writes**:
  - `leave`: absence closure
  - `payroll`: pay restart with the benefit offset ended
  - `safety`: return record and duty assignment
- **Evidence required**:
  - Return clearance from the treating provider.
  - Modified duty offer or documented unavailability.
  - The wage replacement and pay coordination, so no gap or overlap occurs.
- **Failure and repair**:
  - Benefits stop before pay restarts -> the worker has a gap; the coordination step exists to prevent it and a gap is an escalation, not an accounting detail
  - The claim remains open while the absence closes -> correct; a medical claim can outlast a return to work and closing both together would falsify one
  - The worker refuses a genuine modified duty offer -> the refusal is recorded and reported to the carrier; in most states it can end wage replacement, which is why the offer's genuineness matters
- **Jurisdiction**:
  - US federal: FMLA and ADA run concurrently with a compensation absence and have their own restoration and accommodation duties that the claim's closure does not discharge.
  - US state variation: Workers compensation is state law: return-to-work incentives, temporary partial disability formulas and the consequences of refusing modified duty differ sharply, and Texas allows non-subscription entirely.
  - International: In the EU, occupational injury benefits come through social insurance and the return is usually managed through occupational health rather than an insurer.

### WF-SAF-006. Maintain a workplace violence prevention plan

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: compliance officer, manager, employee, HR business partner
- **Trigger**: A jurisdiction requires a written plan, training, an incident log and periodic review.
- **Preconditions**:
  - The plan is written, site-specific and identifies the hazards for the actual work performed.
  - Employee participation in developing the plan is required, not optional.
  - The incident log is separate from the injury log and records threats and near misses, not only injuries.
- **Steps**:
  1. `TASK` (employee) develop the plan with employee participation and record who participated
  2. `CAPABILITY` (compliance officer) publish the plan as a versioned artifact scoped to the sites it covers
  3. `CAPABILITY` (HR business partner) assign the required training and track completion per worker
  4. `TASK` (manager) record every incident, threat and near miss in the violent incident log
  5. `WAIT` (compliance officer) hold to the annual review and to any incident that triggers an earlier one
  6. `END` (compliance officer) close each review cycle with the plan version, the log and the training evidence retained
- **Intents**:
  - `hcmnext.safety.maintain_violence_prevention_plan/v1` (PROPOSED, creates)
- **Reads**:
  - `learning`: training assignments and completions
  - `people`: workers at each covered site
  - `safety`: site hazards, prior incidents, existing plan versions
- **Writes**:
  - `learning`: training assignments per covered worker
  - `safety`: plan version, violent incident log, review records
- **Evidence required**:
  - Employee participation record from plan development.
  - The violent incident log, which records threats and near misses and not only injuries.
  - Training completion per worker, verified rather than self-asserted.
- **Failure and repair**:
  - The plan is a generic template with no site-specific hazards -> REJECT at publication; a generic plan fails the requirement and is worse than none because it evidences the failure
  - An incident is recorded in the injury log only -> the violent incident log is separate and a threat that caused no injury still belongs in it; conflating them loses the near misses the plan exists to prevent
  - The annual review lapses -> the plan is marked stale and the lapse is an obligation with an owner, because an unreviewed plan is the finding an inspection produces
- **Jurisdiction**:
  - US federal: There is no federal workplace violence standard for most industries; OSHA enforces through the general duty clause and has industry-specific guidance for healthcare and late-night retail.
  - US state variation: California Labor Code 6401.9 requires nearly all employers to maintain a written workplace violence prevention plan, provide annual training and keep a violent incident log; New York requires plans for public employers and, separately, retail worker protections; Texas requires plans for healthcare facilities; several other states have sector-specific mandates. This is one of the fastest-moving areas of state safety law.
  - International: EU framework Directive 89/391 requires risk assessment covering psychosocial risks including violence and harassment; ILO Convention 190, where ratified, adds specific duties.

---

## PRV. Privacy and data governance

6 workflows.

### WF-PRV-001. Fulfil a data subject access request

- **Status**: `PARTIAL` - planning/specs/records-management-and-disposition.md; planning/data/models/intent-coverage-matrix.md rows 317 to 333
- **Archetype**: `A6` | **Complexity tier**: 4
- **Actors**: employee, compliance officer, HR business partner, auditor, integration system
  - performing a step: employee, compliance officer, auditor, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A worker or former worker asks for a copy of the personal data held about them.
- **Preconditions**:
  - The requester's identity is verified to the assurance the request warrants.
  - The response deadline is computed from the request receipt, not from triage.
  - Third-party data and privileged material are separable from the response set.
- **Steps**:
  1. `TASK` (compliance officer) the request is received and the requester's identity is verified
  2. `CAPABILITY` (integration system) enumerate every system and data class holding the subject's personal data
  3. `TRANSFORM` (compliance officer) assemble the response, removing third-party personal data and privileged or investigative material
  4. `DECISION` (compliance officer) record every exemption applied by name so a refusal is visible rather than an unexplained absence
  5. `APPROVAL` (compliance officer) legal or compliance approves the response package before release
  6. `CAPABILITY` (employee) deliver through a channel that authenticates the recipient
  7. `END` (auditor) close with the exemption log and the delivery evidence retained
- **Intents**:
  - `hcmnext.privacy.process_data_subject_request/v1` (PROPOSED, creates)
- **Reads**:
  - `hrcase`: case material, subject to exemption
  - `people`: the subject's records across domains
  - `privacy`: processing activities, data inventory, retention schedules
- **Writes**:
  - `privacy`: request record, exemption log, delivery evidence
- **Evidence required**:
  - Identity verification method and its assurance level.
  - Per-exemption record naming the ground and the material withheld.
  - Delivery evidence to an authenticated recipient.
- **Failure and repair**:
  - The response set includes an open investigation the subject is a subject of -> the investigative exemption applies where the law allows; the exemption is recorded and the subject is told a category is withheld
  - A legal hold covers part of the data -> the hold does not block access; it blocks deletion, and conflating the two is a common error
  - The deadline passes -> the miss is recorded with its exposure; extensions are permitted in some regimes but must be notified before the original deadline
- **Jurisdiction**:
  - US federal: There is no general federal employee access right; the FCRA gives a right to a consumer report used in employment, and HIPAA applies to group health plan records held by the plan rather than the employer.
  - US state variation: California's CPRA gives employees access, deletion and correction rights with a 45-day response window extendable to 90; Colorado, Connecticut, Virginia and others exclude employee data from their consumer statutes, so the answer differs sharply by state; separately, about twenty states give personnel file inspection rights with windows from seven to thirty days.
  - International: GDPR Article 15 requires a response within one month, extendable by two, and requires the purposes, recipients, retention periods and the source of the data, not just the data itself.

### WF-PRV-002. Execute a verified deletion under a retention schedule

- **Status**: `PARTIAL` - planning/specs/records-management-and-disposition.md
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: compliance officer, IT or access administrator, auditor, integration system
- **Trigger**: Records reach the end of their retention period and must be destroyed verifiably.
- **Preconditions**:
  - The retention schedule is published with a class per record type.
  - Every copy, including backups and derived planes, is enumerable.
  - Legal holds are consulted before every disposition, not periodically.
- **Steps**:
  1. `CAPABILITY` (compliance officer) identify records eligible for disposition under the schedule
  2. `DECISION` (compliance officer) suppress any record under a legal hold, an open case or an unmet statutory minimum
  3. `APPROVAL` (compliance officer) the records authority approves the disposition batch
  4. `CAPABILITY` (integration system) delete from the authoritative store and every derived plane
  5. `OBSERVE` (IT or access administrator) verify deletion, including in backups whose rotation may lag
  6. `END` (auditor) close with a certificate of destruction listing what was destroyed and what remains pending backup rotation
- **Intents**:
  - `hcmnext.privacy.execute_deletion_plan/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: copy inventory across planes and backups
  - `privacy`: retention schedule, holds, prior dispositions
- **Writes**:
  - `dataops`: deletion operations per copy
  - `privacy`: disposition records and destruction certificate
- **Evidence required**:
  - The copy inventory the deletion was computed against.
  - Hold suppression records.
  - Destruction certificate with the residual backup window stated honestly.
- **Failure and repair**:
  - A copy exists in a system not in the inventory -> the inventory is the control; an unknown copy makes the certificate false, so discovery of one is an incident
  - Backups cannot be selectively deleted -> the certificate states the rotation date after which the copy expires, rather than claiming immediate destruction
  - A hold is applied during the batch -> the affected records are removed from the batch and the batch proceeds for the rest
- **Jurisdiction**:
  - US federal: FLSA requires three years of payroll records, ADEA one year of application records extended during a charge, and IRCA requires the I-9 for three years after hire or one year after separation, whichever is later; deleting before a minimum is a violation in its own right.
  - US state variation: State retention minimums often exceed federal ones: New York requires six years of payroll records, California three years plus specific rules for wage statements and personnel files; several states require longer retention of injury records.
  - International: GDPR requires deletion when the purpose is exhausted, which conflicts with maximal retention; the schedule must therefore be per-jurisdiction rather than a single global maximum.

### WF-PRV-003. Record consent and honour its withdrawal

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: employee, compliance officer, HR business partner, integration system
  - performing a step: employee, compliance officer, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A processing activity relies on consent and the worker later withdraws it.
- **Preconditions**:
  - The activities relying on consent are identified, and those relying on another basis are not.
  - Withdrawal is as easy as giving consent.
  - The downstream effect of withdrawal is computable before it is offered.
- **Steps**:
  1. `CAPABILITY` (compliance officer) record the consent with its scope, its version and its evidence
  2. `TASK` (employee) the worker withdraws consent for a named activity
  3. `TRANSFORM` (compliance officer) compute what stops, what continues under another basis and what data must be deleted
  4. `DECISION` (compliance officer) refuse to continue an activity whose only basis was the withdrawn consent
  5. `CAPABILITY` (integration system) stop the processing and record the withdrawal's effective instant
  6. `SIGNAL` (employee) tell the worker exactly what stopped and what did not, and why
- **Intents**:
  - `hcmnext.privacy.record_consent/v1` (PROPOSED, creates)
  - `hcmnext.privacy.withdraw_consent/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: the subject and the activities touching them
  - `privacy`: processing activities, their bases, consent records
- **Writes**:
  - `privacy`: consent revision, withdrawal record, processing stop
- **Evidence required**:
  - Consent artifact with its scope and version.
  - Withdrawal with its effective instant.
  - The explanation of what continues under a different basis.
- **Failure and repair**:
  - An activity has consent recorded but actually relies on contract or legal obligation -> the basis is corrected; consent in the employment context is often invalid because of the power imbalance, and mislabelling it creates a false withdrawal right
  - Withdrawal would break a legally required processing -> the activity continues under the legal obligation basis and the worker is told which one
  - Historical processing under the consent is questioned -> the withdrawal is prospective; it does not make prior processing unlawful, and the record shows the interval
- **Jurisdiction**:
  - US federal: Consent has limited application to US employment data; the FCRA authorization and biometric consents are the main statutory ones.
  - US state variation: Illinois BIPA requires written release before biometric collection and gives a private right of action with statutory damages, which has produced very large settlements; Texas and Washington have similar statutes without private rights of action.
  - International: GDPR Recital 43 and the Article 29 Working Party guidance treat employee consent as rarely freely given, so an employer relying on consent must be able to show the worker suffered no detriment from refusing.

### WF-PRV-004. Assess a cross-border transfer of worker data

- **Status**: `EXISTING` - internal/workflow/conformance/mobility/definition.go obligation privacy review
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: compliance officer, IT or access administrator, auditor
  - performing a step: compliance officer, auditor
  - participating without owning a step: IT or access administrator
- **Trigger**: Worker personal data will be processed in or accessed from another country.
- **Preconditions**:
  - The origin and destination jurisdictions and the data classes are identified.
  - The available transfer mechanisms are known and their conditions are checkable.
  - Onward transfers to sub-processors are in scope.
- **Steps**:
  1. `CAPABILITY` (compliance officer) map the data classes, the flows and every recipient including sub-processors
  2. `RULE` (compliance officer) select and validate a transfer mechanism for each flow
  3. `DECISION` (compliance officer) block a flow with no valid mechanism rather than proceeding on a best-effort basis
  4. `CAPABILITY` (compliance officer) record the assessment with its supplementary measures
  5. `WAIT` (compliance officer) hold to the reassessment date, since adequacy decisions and mechanisms change
  6. `END` (auditor) close with the reassessment as a standing obligation
- **Intents**:
  - `hcmnext.privacy.assess_data_transfer/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: connector destinations and their hosting regions
  - `privacy`: data classes, flows, recipients, existing mechanisms
- **Writes**:
  - `privacy`: transfer assessment, mechanism record, supplementary measures
- **Evidence required**:
  - Flow map including sub-processors.
  - Mechanism validity evidence with its expiry or reassessment date.
  - Supplementary measures where the mechanism alone is insufficient.
- **Failure and repair**:
  - A flow is discovered that the assessment did not cover -> the flow is suspended and the assessment reopened; an unassessed transfer is the finding regulators act on
  - An adequacy decision is invalidated -> every flow relying on it is reassessed; the mechanism is pinned to a version so the affected set is computable
  - A sub-processor changes hosting region -> the change triggers reassessment automatically rather than being noticed at the next audit
- **Jurisdiction**:
  - US federal: There is no general US restriction on exporting employee data, but sectoral rules such as ITAR and EAR restrict access by foreign nationals to certain technical data, which can constrain who may administer an HR system.
  - US state variation: State laws largely do not restrict export, though some public-sector contracts require in-state data residency.
  - International: GDPR Chapter V requires an adequacy decision, standard contractual clauses with a transfer impact assessment, or another Article 46 mechanism; the Schrems II decision requires assessing the destination's surveillance law, and the EU-US Data Privacy Framework is itself subject to challenge.

### WF-PRV-005. Conduct a data protection impact assessment for a new processing

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: compliance officer, HR business partner, auditor
- **Trigger**: A new HR processing activity is high risk and requires a documented assessment before launch.
- **Preconditions**:
  - The trigger criteria for requiring an assessment are configured.
  - The processing is described in enough detail to be assessed, including any automated decision element.
  - The consultation requirement with the supervisory authority is testable.
- **Steps**:
  1. `CAPABILITY` (compliance officer) test the proposed processing against the assessment trigger criteria
  2. `TASK` (HR business partner) describe the processing, its necessity, its proportionality and its risks to workers
  3. `TRANSFORM` (compliance officer) identify mitigations and compute the residual risk
  4. `DECISION` (compliance officer) block launch where residual risk remains high and no supervisory consultation has occurred
  5. `APPROVAL` (compliance officer) the data protection authority within the tenant approves
  6. `END` (auditor) close with the assessment as a living artifact reviewed when the processing changes
- **Intents**:
  - `hcmnext.privacy.conduct_dpia/v1` (PROPOSED, creates)
- **Reads**:
  - `intelligence`: any model involved and its documentation
  - `privacy`: existing assessments, processing inventory, risk criteria
- **Writes**:
  - `privacy`: assessment artifact with its risks, mitigations and residual risk
- **Evidence required**:
  - Trigger evaluation, so a skipped assessment is a visible decision.
  - Risk register with mitigations and residual risk.
  - Consultation record where required.
- **Failure and repair**:
  - The processing launches without the assessment -> an incident; launching first and assessing later is the specific failure the requirement exists to prevent
  - The processing changes materially after approval -> the assessment is reopened; an assessment pinned to an old design protects nothing
  - Worker representatives were not consulted where required -> the assessment is incomplete and the approval is withheld
- **Jurisdiction**:
  - US federal: No general federal requirement, though sectoral risk assessments exist; the FTC has treated inadequate assessment of automated tools as an unfair practice.
  - US state variation: Colorado and Connecticut require data protection assessments for certain profiling, and California's regulations require risk assessments for significant decisions, including employment ones.
  - International: GDPR Article 35 requires a DPIA for systematic monitoring, large-scale special category processing and automated decisions with legal effects; Article 35(9) requires seeking the views of data subjects or their representatives where appropriate, which in practice means the works council.

### WF-PRV-006. Notify workers before monitoring their electronic activity

- **Status**: `NEW`
- **Archetype**: `A13` | **Complexity tier**: 3
- **Actors**: compliance officer, IT or access administrator, employee
- **Trigger**: The employer monitors email, devices or location and must give the notice the jurisdiction requires.
- **Preconditions**:
  - The monitoring's scope, purpose and retention are documented before it starts.
  - The notice content and its timing are jurisdiction-specific.
  - Monitoring for one purpose does not license using the data for another.
- **Steps**:
  1. `CAPABILITY` (compliance officer) document the monitoring scope, its purpose, its data classes and its retention
  2. `DECISION` (compliance officer) refuse monitoring whose purpose is not among the declared ones; scope creep here is the whole risk
  3. `SIGNAL` (employee) issue the notice in the form the jurisdiction requires and record acknowledgement where required
  4. `CAPABILITY` (IT or access administrator) enable the monitoring only after the notice is delivered
  5. `WAIT` (compliance officer) hold to the review date and re-evaluate proportionality
  6. `END` (compliance officer) close each review cycle with the notice, the acknowledgements and the retention state retained
- **Intents**:
  - `hcmnext.privacy.notify_monitoring/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: the workers in scope and their jurisdictions
  - `privacy`: monitoring purposes, data classes, retention schedule
  - `security`: the monitoring systems and their configuration
- **Writes**:
  - `documents`: the notice and its delivery evidence
  - `privacy`: monitoring declaration and notice records
- **Evidence required**:
  - The declared purposes, fixed before monitoring starts.
  - Notice delivery per worker with its jurisdiction-specific form.
  - The proportionality review at each cycle.
- **Failure and repair**:
  - Monitoring starts before the notice is delivered -> FAIL_CLOSED; in several jurisdictions the notice is a precondition and monitoring without it is unlawful regardless of what it finds
  - Data collected for security is used in a performance decision -> the secondary use is refused; purpose limitation is the control and a security log repurposed for discipline is the classic breach
  - A worker in a jurisdiction requiring consent has not consented -> monitoring is excluded for that worker rather than applied uniformly
- **Jurisdiction**:
  - US federal: The Electronic Communications Privacy Act permits monitoring with consent or in the ordinary course of business, and the Stored Communications Act constrains access to stored communications; there is no general federal notice mandate.
  - US state variation: New York Civil Rights Law 52-c requires written notice at hire and a conspicuous posting before monitoring email, telephone or internet; Connecticut requires prior written notice; Delaware requires notice with acknowledgement; California's CPRA notice-at-collection duties reach employee monitoring data. Two-party consent recording statutes in California, Illinois, Pennsylvania, Washington and others apply separately.
  - International: Under GDPR the monitoring needs a lawful basis, a documented necessity and proportionality assessment, and in Germany and the Netherlands a works council agreement before it can be deployed at all.

---

## DOC. Documents, forms and signatures

4 workflows.

### WF-DOC-001. Generate a localized document from a template

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 20 features document_template_create and form_submission_process
- **Archetype**: `A16` | **Complexity tier**: 2
- **Actors**: HR business partner, employee, compliance officer, integration system
  - performing a step: HR business partner, compliance officer, integration system
  - participating without owning a step: employee
- **Trigger**: An employment document is produced from a versioned template with merged data.
- **Preconditions**:
  - The template version and its jurisdiction and language variants are published.
  - Every merge field resolves from an authorized read.
  - The output is immutable once generated.
- **Steps**:
  1. `CAPABILITY` (HR business partner) select the template variant by jurisdiction and the worker's language
  2. `CAPABILITY` (integration system) resolve merge fields through authorized reads, never from a client-supplied payload
  3. `DECISION` (compliance officer) refuse generation when a required merge field is unauthorized or unresolvable, rather than emitting a blank
  4. `DOCUMENT` (integration system) render the document and seal it with a digest
  5. `END` (compliance officer) close with the template version and the merged values retained alongside the artifact
- **Intents**:
  - `hcmnext.documents.generate_document/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: template versions and variants
  - `people`: merge data under the caller's authorization
  - `regulatory`: required content per jurisdiction
- **Writes**:
  - `documents`: generated artifact with its digest and template version
- **Evidence required**:
  - Template version and variant selected.
  - Merged values, so the document can be reproduced.
  - Artifact digest.
- **Failure and repair**:
  - A required merge field is unauthorized for the generator -> REJECT; emitting a document with a blank where a wage rate should be is worse than refusing
  - No variant exists for the worker's jurisdiction -> REJECT with the gap named; falling back to a generic variant can omit statutorily required content
  - The template changes after generation -> the generated artifact keeps its version; documents are never regenerated in place
- **Jurisdiction**:
  - US federal: Wage notices, arbitration agreements and benefit summaries all have prescribed content; a document missing a required element can be void or penalized regardless of intent.
  - US state variation: California requires notices in the employee's primary language where the Labor Commissioner publishes a template; New York requires dual-language wage notices; Texas has no equivalent, so a single template is wrong across a multi-state population.
  - International: Directive 2019/1152 sets minimum written information across the EU with member-state additions; several states require the local language for the document to be enforceable.

### WF-DOC-002. Collect a signature with a verifiable ceremony

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 20 feature signature_request_send
- **Archetype**: `A16` | **Complexity tier**: 3
- **Actors**: HR business partner, employee, external partner or carrier, compliance officer, integration system
- **Trigger**: A document requires a legally effective signature from one or more parties.
- **Preconditions**:
  - The signature method's legal sufficiency in the jurisdiction is established.
  - The signer's identity is authenticated to the required assurance.
  - The document digest the signature binds is fixed before the ceremony.
- **Steps**:
  1. `CAPABILITY` (HR business partner) seal the document and create the signature request bound to its digest
  2. `SIGNAL` (employee) invite the signer through an authenticated channel
  3. `TASK` (external partner or carrier) the signer reviews and signs, producing ceremony evidence
  4. `DECISION` (compliance officer) reject a signature whose bound digest differs from the document presented
  5. `CAPABILITY` (integration system) record the signature, its proof and the ceremony metadata
  6. `END` (compliance officer) close when every required party has signed; a partially signed document is not executed
- **Intents**:
  - `hcmnext.documents.request_signature/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: document artifact and its digest
  - `people`: signer identity and authentication
  - `regulatory`: signature sufficiency rules
- **Writes**:
  - `documents`: signature records, ceremony evidence, execution state
- **Evidence required**:
  - Document digest bound to each signature.
  - Signer authentication evidence and the ceremony metadata.
  - Consent to electronic signature where the jurisdiction requires it.
- **Failure and repair**:
  - The document is amended after one party signs -> the earlier signature binds the earlier digest and does not carry forward; a fresh ceremony is required
  - The signer's authentication is weaker than the document warrants -> REJECT; a clickwrap signature on an arbitration agreement has been held unenforceable where identity was not established
  - The provider reports signed but returns no proof -> UNKNOWN; a signature without evidence cannot be relied on
- **Jurisdiction**:
  - US federal: ESIGN and UETA make electronic signatures effective, but require the signer's consent to electronic records and the ability to retain a copy; some documents, such as certain wage assignments, are excluded in specific states.
  - US state variation: New York and California courts have voided clickwrap arbitration agreements where the employer could not prove who signed; several states require a wet signature for specific instruments such as a wage deduction authorization.
  - International: The eIDAS Regulation distinguishes simple, advanced and qualified electronic signatures, and some employment documents in Germany still require written form under BGB 126 that an electronic signature does not satisfy.

### WF-DOC-003. Redact a document before disclosure

- **Status**: `NEW`
- **Archetype**: `A16` | **Complexity tier**: 3
- **Actors**: compliance officer, HR business partner, auditor, integration system
  - performing a step: compliance officer, auditor, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A document must be disclosed with third-party or privileged content removed.
- **Preconditions**:
  - The redaction basis is recorded per redaction, not per document.
  - Redaction is destructive in the produced copy, not a display overlay.
  - The unredacted original is preserved under its own controls.
- **Steps**:
  1. `CAPABILITY` (compliance officer) identify content requiring redaction by class and by review
  2. `TASK` (compliance officer) a reviewer confirms each proposed redaction and its basis
  3. `CAPABILITY` (integration system) produce a new artifact with the content removed from the bytes, not hidden
  4. `DECISION` (compliance officer) verify the produced copy contains no recoverable redacted text before release
  5. `END` (auditor) close with a redaction log and both artifacts retained
- **Intents**:
  - `hcmnext.documents.apply_redaction/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: source artifact and its classification
  - `privacy`: redaction bases and third-party data classes
- **Writes**:
  - `documents`: redacted artifact, redaction log, link to the original
- **Evidence required**:
  - Per-redaction basis.
  - Verification that the redacted content is not recoverable.
  - Link between the redacted copy and its original.
- **Failure and repair**:
  - A visual overlay is used instead of removing the bytes -> REJECT; overlay redactions have leaked in high-profile disclosures and the check exists to prevent it
  - A redaction basis is not recorded -> REJECT; an unexplained redaction is challenged and cannot be defended later
  - The original is deleted after redaction -> REJECT; the original may be required for a court's in-camera review
- **Jurisdiction**:
  - US federal: Disclosures in litigation and in response to agency charges routinely require redaction of third-party personal data; over-redaction is itself sanctionable.
  - US state variation: State personnel file inspection statutes commonly permit redaction of third-party information and of records of an ongoing investigation.
  - International: GDPR Article 15(4) says the right to a copy must not adversely affect the rights of others, which is the basis for redacting third-party data from an access response.

### WF-DOC-004. Retain and dispose of a document under its records class

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: compliance officer, HR business partner, auditor, integration system
- **Trigger**: A document is classified on creation and its retention and disposition follow from that class.
- **Preconditions**:
  - Classification happens at creation, not at disposition time.
  - The retention clock's trigger is explicit: creation, employment end, or an event.
  - Disposition consults holds at execution, not at scheduling.
- **Steps**:
  1. `CAPABILITY` (HR business partner) classify the document and bind its retention class and clock trigger at creation
  2. `WAIT` (compliance officer) hold until the retention period elapses from its trigger
  3. `DECISION` (compliance officer) consult holds at the moment of disposition rather than at scheduling
  4. `CAPABILITY` (integration system) dispose and certify, or suppress with the hold reference
  5. `END` (auditor) close each disposition cycle with its certificate
- **Intents**:
  - `hcmnext.documents.dispose_document/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: artifacts, their classes and their clock triggers
  - `privacy`: retention schedule and active holds
- **Writes**:
  - `documents`: disposition records and certificates
  - `privacy`: suppression records where a hold applied
- **Evidence required**:
  - Classification and clock trigger recorded at creation.
  - Hold consultation at the moment of disposition.
  - Disposition certificate or suppression record.
- **Failure and repair**:
  - A document is created with no class -> REJECT at creation; an unclassified document is retained forever by default, which is its own compliance problem
  - A hold is applied between scheduling and execution -> the disposition is suppressed; consulting holds at scheduling only is how spoliation happens
  - The clock trigger is employment end and the employment is later reinstated -> the clock resets and the disposition is cancelled, because the trigger event did not actually occur
- **Jurisdiction**:
  - US federal: Retention minimums come from FLSA, IRCA, ERISA, ADEA and Title VII and differ by document type; disposing before a minimum is a violation independent of any litigation.
  - US state variation: New York requires six years of payroll records against the federal three; California requires three years for personnel files with a separate rule for wage statements; several states impose longer periods for injury records.
  - International: GDPR requires deletion once the purpose is exhausted, which conflicts with maximal retention, so the schedule must be per-jurisdiction rather than a single global maximum.

---

## WRK. Workflow and human work

4 workflows.

### WF-WRK-001. Compile and publish a workflow definition

- **Status**: `EXISTING` - planning/specs/workflow-runtime.md; planning/workflows/\_engine/step-types.md
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
- **Trigger**: A workflow definition is compiled against a capability registry and published for a phase.
- **Preconditions**:
  - Every capability the definition binds is published at the bound version.
  - The definition declares its modes, its terminal profile and its limits.
  - The phase gate for the definition's risk class is satisfied.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) compile the definition against the capability registry for the target phase
  2. `DECISION` (IT or access administrator) refuse compilation when a bound capability version is missing or its mode is not declared
  3. `CAPABILITY` (integration system) validate node limits, edge completeness and terminal completion mappings
  4. `APPROVAL` (compliance officer) the governance authority approves publication for the risk class
  5. `CAPABILITY` (integration system) publish the compiled definition as an immutable version
  6. `END` (auditor) close; running instances stay pinned to the version they started on
- **Intents**:
  - `hcmnext.workflow.compile_definition/v1` (PROPOSED, creates)
  - `hcmnext.workflow.publish_definition/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: phase gates and risk classification
  - `workflow`: definition source, capability registry, prior versions
- **Writes**:
  - `workflow`: compiled definition and its publication record
- **Evidence required**:
  - Compilation result including every resolved capability version.
  - Approval for the risk class.
  - Publication version that instances will pin to.
- **Failure and repair**:
  - A capability is unpublished after the definition is compiled -> running instances keep their pinned version; new instances fail to start with the missing capability named
  - An edge has no route for an outcome the node can produce -> REJECT at compile; an unrouted outcome becomes a stuck instance in production
  - A terminal has no completion mapping for all five lifecycle dimensions -> REJECT; a terminal that leaves a dimension undefined produces an instance nobody can reason about
- **Jurisdiction**:
  - US federal: None directly; the control is internal but supports SOX change-management evidence for public filers.
  - US state variation: None.
  - International: None.

### WF-WRK-002. Resolve an approval requirement and bind the decision

- **Status**: `EXISTING` - definitions/governance/intent-conformance-descriptors.yaml descriptors hcmnext.work.approve_proposal and reject_proposal
- **Archetype**: `A2` | **Complexity tier**: 3
- **Actors**: manager, HR business partner, compliance officer, integration system
- **Trigger**: A proposal requires one or more approvals bound to its exact digest.
- **Preconditions**:
  - The approval requirement declares its resolver expression, quorum and separation-of-duties rule.
  - The authority snapshot at the moment of decision is capturable.
  - The proposal digest is immutable.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the approver set from the requirement's expression at the effective instant
  2. `DECISION` (compliance officer) exclude any resolved approver who is the proposer or otherwise conflicted
  3. `TASK` (manager) each approver reviews the proposal as presented and decides
  4. `CAPABILITY` (integration system) bind each decision to the proposal digest and the approver's authority snapshot
  5. `DECISION` (compliance officer) invalidate every decision when the proposal materially changes
  6. `END` (HR business partner) close when the quorum is met; a rejection ends the proposal rather than pausing it
- **Intents**:
  - `hcmnext.work.approve_proposal/v1` (REAL, creates)
  - `hcmnext.work.reject_proposal/v1` (REAL, creates)
- **Reads**:
  - `organization`: scope boundaries for the resolver
  - `people`: candidate approvers and their authority
  - `work`: approval requirement, resolver expression, quorum
- **Writes**:
  - `work`: approval decisions with their authority snapshots and digest bindings
- **Evidence required**:
  - Resolver output showing who was eligible and why.
  - Each decision bound to the digest with the approver's authority snapshot.
  - Invalidation records where a change reset the approvals.
- **Failure and repair**:
  - The proposal changes after some approvals -> the changed material invalidates them; REPLAN_AND_REAPPROVE, which the change_manager descriptor names as its own negative policy
  - The resolver returns nobody -> FAIL_CLOSED; an unresolvable approval requirement is a configuration defect, not a licence to proceed
  - An approver's authority lapses between decision and execution -> revalidation at the commit boundary invalidates the decision
- **Jurisdiction**:
  - US federal: SOX requires approvals over financial transactions to be attributable and to have functioned; an approval that cannot be tied to what was approved fails that test.
  - US state variation: None state-specific.
  - International: None, though codetermination can add a works council approval as a legally required step rather than an internal control.

### WF-WRK-003. Claim, complete and hand off a work item

- **Status**: `PARTIAL` - planning/specs/human-work-forms-and-rules.md
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: HR business partner, manager, employee, integration system
  - performing a step: HR business partner, integration system
  - participating without owning a step: manager, employee
- **Trigger**: A human task is queued, claimed by a worker, and completed or handed back.
- **Preconditions**:
  - The queue's assignment rule and its eligibility are configured.
  - A claim lease prevents two people working the same item.
  - The item's data access manifest limits what the claimant may see.
- **Steps**:
  1. `CAPABILITY` (integration system) place the item in the queue with its eligibility and priority
  2. `TASK` (HR business partner) an eligible worker claims the item, taking a lease
  3. `DECISION` (integration system) expire the lease and return the item if the claimant goes idle, rather than leaving it locked
  4. `TASK` (HR business partner) the claimant completes the item with a typed result, or hands it off with a reason
  5. `END` (integration system) close the item and advance the waiting instance
- **Intents**:
  - `hcmnext.work.claim_work_item/v1` (PROPOSED, creates)
  - `hcmnext.work.complete_work_item/v1` (PROPOSED, creates)
- **Reads**:
  - `privacy`: data access manifest for the item
  - `work`: queue, item, eligibility, prior claims
- **Writes**:
  - `work`: claim, lease, completion or handoff record
- **Evidence required**:
  - Claim and lease with their timestamps.
  - Completion result as a typed value, not free text.
  - Handoff reason where the item was returned.
- **Failure and repair**:
  - The lease expires while the claimant is still working -> the work is not lost; the claimant reclaims and the double-claim is detected at completion
  - Two claimants complete the same item -> the second completion is REJECTED on the item's idempotency scope
  - The item's data manifest denies a field the claimant needs -> the item is unworkable and is escalated rather than being completed on incomplete information
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-WRK-004. Recover a workflow instance after a runtime failure

- **Status**: `PARTIAL` - planning/specs/workflow-runtime.md; planning/todos.md section 8 durable workflow runtime and intervention
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, auditor, compliance officer, integration system
- **Trigger**: A durable instance is interrupted and must resume without repeating side effects.
- **Preconditions**:
  - Checkpoints exist at every safe point before an external effect.
  - Every external effect carries an idempotency key.
  - The instance's pinned definition version is retrievable.
- **Steps**:
  1. `OBSERVE` (integration system) detect the interrupted instance and read its last checkpoint
  2. `DECISION` (IT or access administrator) determine for each dispatched effect whether it completed, by querying rather than assuming
  3. `CAPABILITY` (integration system) resume from the checkpoint, replaying only steps whose effects are proven not to have occurred
  4. `DECISION` (compliance officer) route an effect whose state cannot be determined to an investigation, never to a blind retry
  5. `END` (auditor) close when the instance reaches a terminal, with the recovery recorded
- **Intents**:
  - `hcmnext.operations.recover_workflow_instance/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: external operation journal and its observations
  - `workflow`: instance state, checkpoints, dispatched effects
- **Writes**:
  - `workflow`: resumed instance state and recovery record
- **Evidence required**:
  - Checkpoint the recovery resumed from.
  - Per-effect determination of whether it had occurred.
  - Recovery record so a repeated failure is visible as a pattern.
- **Failure and repair**:
  - An effect's completion cannot be determined -> the instance moves to UNKNOWN and an investigation opens; a blind retry on a payment or a filing is unacceptable
  - The definition version was retired while the instance was down -> the instance keeps its pinned version; migration to a new version is an explicit, approved act
  - A checkpoint is corrupt -> the instance is quarantined rather than resumed from a guess
- **Jurisdiction**:
  - US federal: None directly, though a duplicated payment or filing caused by a blind retry has direct regulatory consequences.
  - US state variation: None.
  - International: None.

---

## INT. Integration and external systems

4 workflows.

### WF-INT-001. Onboard a connector and take it to governed maturity

- **Status**: `PARTIAL` - planning/specs/integration-platform.md connector maturity ladder
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, external partner or carrier, auditor
- **Trigger**: A new external system is connected and moves from transport adapter to governed connector.
- **Preconditions**:
  - The external system, its authority scope and its data classes are declared.
  - Credentials are held as leases rather than as stored secrets in configuration.
  - The maturity level being claimed is explicit.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) register the external system with its trust profile and data classification
  2. `CAPABILITY` (external partner or carrier) establish the connection with a credential lease and verify reachability
  3. `CAPABILITY` (IT or access administrator) bind the schema mapping and prove it round-trips on fixtures
  4. `DECISION` (compliance officer) refuse to claim a maturity level whose evidence does not exist; a configured adapter is not a certified connector
  5. `APPROVAL` (compliance officer) compliance approves the data classes the connector may carry and the egress path
  6. `END` (auditor) close at the maturity the evidence supports, with the next level as an open item
- **Intents**:
  - `hcmnext.integration.register_external_system/v1` (PROPOSED, creates)
  - `hcmnext.integration.establish_connection/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: system registry, trust profiles, existing connections
  - `privacy`: data classes the connector will carry
  - `security`: credential lease authority and egress policy
- **Writes**:
  - `integration`: system, connection, mapping and maturity records
  - `security`: credential lease
- **Evidence required**:
  - Round-trip fixture results proving the mapping.
  - Credential lease with its rotation policy, never a stored static secret.
  - Maturity claim with the evidence that supports it.
- **Failure and repair**:
  - A credential is supplied as a static value in configuration -> REJECT; a lease with rotation is the contract and a static secret defeats the custody model
  - The mapping round-trips on fixtures but fails on production shapes -> the maturity claim is downgraded and the failure becomes a fixture, which is how the ladder is supposed to work
  - The connector reaches a destination outside the approved egress policy -> BLOCKED at the egress control, not at the connector's own configuration
- **Jurisdiction**:
  - US federal: None directly, though a connector carrying protected health information brings the vendor into HIPAA business associate obligations.
  - US state variation: State data-security statutes require reasonable vendor diligence; New York's SHIELD Act and Massachusetts 201 CMR 17 both make third-party contracts a named control.
  - International: GDPR Article 28 requires a processor contract with specified terms before any transfer, and Article 32 security duties flow to the processor.

### WF-INT-002. Run a synchronization and reconcile what the target actually holds

- **Status**: `PARTIAL` - planning/specs/integration-platform.md
- **Archetype**: `A14` | **Complexity tier**: 4
- **Actors**: integration system, IT or access administrator, auditor
- **Trigger**: A scheduled sync pushes or pulls records and the result must be verified rather than assumed.
- **Preconditions**:
  - The sync's scope, its watermark and its ordering guarantee are declared.
  - Every operation carries an idempotency key.
  - A reconciliation policy defines what counts as agreement.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the change set since the last watermark
  2. `CAPABILITY` (integration system) dispatch operations in the declared order with idempotency keys
  3. `OBSERVE` (integration system) read back the target's state rather than trusting the operation responses
  4. `TRANSFORM` (IT or access administrator) compare expected against observed and classify each difference
  5. `DECISION` (IT or access administrator) advance the watermark only for records confirmed present, not for the whole batch
  6. `END` (auditor) close with unreconciled records carried into the next run rather than dropped
- **Intents**:
  - `hcmnext.integration.execute_sync/v1` (PROPOSED, creates)
  - `hcmnext.operations.detect_drift/v1` (REAL, advances)
- **Reads**:
  - `integration`: connector, mapping, prior watermark, operation journal
  - `people`: source records in scope
- **Writes**:
  - `integration`: operations, observations, new watermark, drift findings
- **Evidence required**:
  - Operation journal with idempotency keys and responses.
  - Read-back observation, distinct from the operation response.
  - Watermark advance record with the confirmed set.
- **Failure and repair**:
  - The target accepts an operation and later loses it -> the read-back detects it and the record stays unreconciled; a watermark advanced on acceptance would have hidden it forever
  - The target rate-limits mid batch -> the batch pauses and resumes from the last confirmed record; it does not restart from the watermark and duplicate work
  - The target's response is ambiguous -> UNKNOWN and an investigation; the idempotency key makes a safe retry possible only where the target honours it
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None, though a sync crossing a border is a transfer requiring its own assessment.

### WF-INT-003. Redrive a failed connector operation batch

- **Status**: `PARTIAL` - planning/specs/integration-platform.md; planning/specs/transaction-ledger-reconciliation-and-repair.md
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
  - performing a step: IT or access administrator, compliance officer, integration system
  - participating without owning a step: auditor
- **Trigger**: A batch of external operations failed and must be reprocessed safely.
- **Preconditions**:
  - Each failed operation's terminal state is known: rejected, timed out or ambiguous.
  - Idempotency keys from the original attempts are retrievable.
  - The redrive authority is separate from the operator who triggered the original run.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) classify each failure as safely retryable, requiring investigation, or permanently failed
  2. `DECISION` (compliance officer) exclude ambiguous operations from the automatic redrive; they need determination first
  3. `CAPABILITY` (IT or access administrator) simulate the redrive and report what it would send
  4. `APPROVAL` (compliance officer) the redrive authority approves the exact operation set
  5. `CAPABILITY` (integration system) reissue with the original idempotency keys
  6. `OBSERVE` (integration system) observe each result and reconcile
- **Intents**:
  - `hcmnext.integration.redrive_operations/v1` (PROPOSED, creates)
  - `hcmnext.operations.simulate_repair/v1` (REAL, reads)
- **Reads**:
  - `integration`: operation journal, failure codes, idempotency keys
  - `operations`: the incident the redrive addresses
- **Writes**:
  - `integration`: reissued operations and their observations
- **Evidence required**:
  - Per-operation failure classification.
  - Simulation output the approval bound to.
  - Reissue results, distinguishing a duplicate-suppressed response from a fresh success.
- **Failure and repair**:
  - The target does not honour idempotency keys -> the redrive is refused for effectful operations; a duplicate payment or a duplicate filing is worse than a delay
  - An operation classified as failed actually succeeded -> the read-back before redrive is what catches it; classification alone is not sufficient evidence
  - The redrive itself fails the same way -> escalate rather than loop; two identical failures mean the cause is not transient
- **Jurisdiction**:
  - US federal: None directly, though a duplicated tax filing or payment has immediate regulatory consequences, which is why ambiguity blocks the automatic path.
  - US state variation: None.
  - International: None.

### WF-INT-004. Receive and verify an inbound webhook event

- **Status**: `NEW`
- **Archetype**: `A14` | **Complexity tier**: 3
- **Actors**: integration system, IT or access administrator, external partner or carrier
  - performing a step: integration system, IT or access administrator
  - participating without owning a step: external partner or carrier
- **Trigger**: An external system notifies of a change and the platform must act on it safely.
- **Preconditions**:
  - The webhook's signature verification and its replay window are configured.
  - The event's claimed subject must be resolvable within the tenant's scope.
  - The event is a notification, not an authoritative fact.
- **Steps**:
  1. `CAPABILITY` (integration system) verify the signature and reject an event outside the replay window
  2. `DECISION` (IT or access administrator) treat the payload as untrusted input; resolve the subject and re-read the authoritative state rather than believing the payload
  3. `CAPABILITY` (integration system) record the event in the trigger inbox with its dedupe key
  4. `CAPABILITY` (integration system) fire the subscribed trigger, which creates an intent under the platform's own governance
  5. `END` (integration system) close the intake; the intent's outcome is its own concern
- **Intents**:
  - `hcmnext.integration.receive_webhook/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: subscription, signing key, replay window, inbox
  - `security`: tenant scope for the claimed subject
- **Writes**:
  - `integration`: inbox entry and trigger firing
- **Evidence required**:
  - Signature verification result.
  - Dedupe key and the duplicate suppression it produced.
  - The authoritative re-read that the action was actually based on.
- **Failure and repair**:
  - The same event is delivered several times -> the dedupe key suppresses the duplicates; providers retry aggressively and at-least-once is the norm
  - The event claims a subject in another tenant -> REJECT and record; a cross-tenant claim in a webhook is a probe, not a mistake
  - The payload disagrees with the authoritative state -> the authoritative state wins; the payload is a hint that something changed, not the change itself
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None, though a webhook carrying personal data across a border is itself a transfer.

---

## DOP. DataOps and administration

4 workflows.

### WF-DOP-001. Stage, validate and commit a bulk data import

- **Status**: `PARTIAL` - planning/specs/hris-admin-dataops.md
- **Archetype**: `A10` | **Complexity tier**: 4
- **Actors**: HR business partner, IT or access administrator, compliance officer, integration system
- **Trigger**: An administrator loads a file of changes that must be validated before any of it commits.
- **Preconditions**:
  - The file's schema and its mapping to domain fields are declared.
  - Validation runs against the same rules the interactive path uses, not a looser set.
  - The commit is all-or-nothing per row, with per-row outcomes reported.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) stage the file and profile it against the declared schema
  2. `CAPABILITY` (integration system) validate every row against domain rules, authority and invariants
  3. `DECISION` (HR business partner) refuse the batch when the error rate exceeds the configured threshold, rather than committing the good rows silently
  4. `CAPABILITY` (HR business partner) simulate the batch and report its aggregate effect before approval
  5. `APPROVAL` (compliance officer) the data authority approves the simulated effect
  6. `CAPABILITY` (integration system) commit as bound child intents, one per row, each independently addressable
  7. `END` (HR business partner) close with per-row outcomes; a partial batch is reported as partial
- **Intents**:
  - `hcmnext.dataops.import_batch/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: staging area, mapping, prior imports
  - `governance`: authority for each row's change type
  - `people`: existing records the rows will change
- **Writes**:
  - `dataops`: staging records, validation results, batch record
  - `people`: committed changes as child intents
- **Evidence required**:
  - Validation results per row with the rule that failed.
  - Simulation of the aggregate effect the approval bound to.
  - Per-row commit outcome.
- **Failure and repair**:
  - A row would bypass an approval the interactive path requires -> REJECT; a bulk path that skips governance is the classic backdoor and the batch is not a licence
  - Some rows commit and others fail -> per-row outcomes are reported; there is no false rollback and no aggregate success claim
  - The file's mapping is wrong and every row validates but means the wrong thing -> the simulation is the control; an approver who sees a plausible aggregate effect is the last line of defence
- **Jurisdiction**:
  - US federal: A bulk change to pay or classification has the same legal consequences as an individual one; the batch does not dilute the notice or approval duties.
  - US state variation: State wage-change notice duties apply per employee, so a bulk pay change generates a bulk notice obligation that the workflow must produce.
  - International: Under GDPR, a bulk change is still processing per data subject and an incorrect batch is an accuracy failure with rectification duties.

### WF-DOP-002. Explain how a field's effective value came to be

- **Status**: `EXISTING` - internal/domains/dataops package: effective-date debugger and cross-system diff
- **Archetype**: `A6` | **Complexity tier**: 3
- **Actors**: HR business partner, auditor, payroll administrator, integration system
  - performing a step: auditor, integration system
  - participating without owning a step: HR business partner, payroll administrator
- **Trigger**: An administrator asks why a field has the value it does at a given date.
- **Preconditions**:
  - The caller's field-level authorization is resolvable.
  - The revision chain and its provenance are retained.
  - The answer distinguishes the domain fact, the external observation and the claim.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the caller's authorization for the field
  2. `TRANSFORM` (integration system) assemble the revision chain with effective-at, known-at, actor and source authority for each link
  3. `DECISION` (integration system) return DENIED for a field the caller may not read and WITHHELD for a subject they may not know exists
  4. `CAPABILITY` (auditor) return the explanation with its watermark
  5. `END` (integration system) complete with zero writes and a zero-effect receipt
- **Intents**:
  - `hcmnext.intelligence.explain_transaction/v1` (REAL, creates)
- **Reads**:
  - `people`: revision chains for the field
  - `privacy`: field classification
  - `provenance`: source authority and actor per revision
- **Writes**:
- **Evidence required**:
  - Revision chain with both time axes per link.
  - Denied and withheld results by name.
  - Zero-effect receipt.
- **Failure and repair**:
  - A revision's actor is a system with no human behind it -> the answer says so; attributing a system change to the last human who touched anything is worse than saying it was a system
  - The chain has a gap because a migration overwrote history -> the gap is disclosed; a smooth-looking chain that hides a migration is a lie
  - The caller may read the current value but not the historical one -> the current value is returned and the history is DENIED, which is a legitimate and different answer
- **Jurisdiction**:
  - US federal: FLSA and IRS record-keeping both require that a figure be explainable; this workflow is how that requirement is met in practice.
  - US state variation: State personnel-file inspection rights sometimes extend to the history of a field, not just its current value.
  - International: GDPR Article 15 requires the source of the data, which this chain provides.

### WF-DOP-003. Compare HCM state against an external system of record

- **Status**: `EXISTING` - internal/domains/dataops package: cross-system diff
- **Archetype**: `A18` | **Complexity tier**: 3
- **Actors**: IT or access administrator, HR business partner, auditor, integration system
  - performing a step: IT or access administrator, auditor, integration system
  - participating without owning a step: HR business partner
- **Trigger**: An administrator asks where the platform and an external system disagree.
- **Preconditions**:
  - Both sides are readable at comparable effective instants.
  - A field mapping and a materiality rule are configured.
  - The comparison is descriptive; it repairs nothing.
- **Steps**:
  1. `CAPABILITY` (integration system) read both sides at the same effective instant with their watermarks
  2. `TRANSFORM` (IT or access administrator) compare mapped fields and classify each difference by materiality
  3. `DECISION` (IT or access administrator) report a timing difference separately from a real disagreement, using the two watermarks
  4. `CAPABILITY` (auditor) return the diff with its authorization applied per field
  5. `END` (IT or access administrator) complete with zero writes; repair is a separate intent
- **Intents**:
  - `hcmnext.operations.detect_drift/v1` (REAL, creates)
- **Reads**:
  - `integration`: external state and its watermark
  - `people`: local authoritative state
  - `privacy`: field classification for the diff output
- **Writes**:
- **Evidence required**:
  - Both watermarks, so a timing difference is distinguishable.
  - Per-field difference with its materiality.
  - Field authorization applied to the output.
- **Failure and repair**:
  - The external system's watermark is much older -> differences are reported as possibly-timing with the lag stated; treating them as drift would generate false repairs
  - A field exists on one side only -> reported as unmapped rather than as a difference; an unmapped field is a mapping gap
  - The caller may not read a field on either side -> the field is DENIED in the diff, not silently excluded, so the caller knows the comparison is partial
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-DOP-004. Promote a configuration package between environments

- **Status**: `PARTIAL` - planning/specs/hris-admin-dataops.md; definitions/governance/feature-intent-intake.yaml group 23 feature configuration_package_promote
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
- **Trigger**: Configuration validated in a sandbox is promoted to production.
- **Preconditions**:
  - The package is immutable and its contents are enumerable.
  - The target environment's current configuration is readable for a diff.
  - The promotion authority is distinct from the authoring authority.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) diff the package against the target environment's current configuration
  2. `CAPABILITY` (IT or access administrator) simulate the promotion's effect on in-flight instances and pinned versions
  3. `DECISION` (compliance officer) block a promotion that would change the rule a running instance is pinned to
  4. `APPROVAL` (compliance officer) the promotion authority approves the exact diff
  5. `CAPABILITY` (integration system) publish into the target with an activation instant
  6. `OBSERVE` (auditor) verify the target reports the new version active
- **Intents**:
  - `hcmnext.dataops.promote_configuration/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: package contents, target configuration, prior promotions
  - `workflow`: in-flight instances and their pinned versions
- **Writes**:
  - `dataops`: promotion record and activation
  - `governance`: new active configuration version
- **Evidence required**:
  - The diff the approval bound to.
  - In-flight instance impact analysis.
  - Activation confirmation from the target.
- **Failure and repair**:
  - The package was authored against a stale target -> the diff shows unexpected changes and the promotion is REPLANNED; promoting a stale package silently reverts other work
  - A rollback is needed -> the rollback is its own promotion of the prior version, with its own approval; there is no unlogged undo
  - The activation partially applies -> the environment reports a mixed version state and the promotion is BLOCKED rather than reported successful
- **Jurisdiction**:
  - US federal: SOX change management requires segregation between authoring and promotion for systems affecting financial reporting.
  - US state variation: None state-specific.
  - International: None.

---

## SEC. Authorization, security and identity

5 workflows.

### WF-SEC-001. Grant time-limited support access to a customer tenant

- **Status**: `PARTIAL` - planning/specs/secrets-key-custody-and-credential-leases.md; definitions/governance/feature-intent-intake.yaml group 24 feature support_jit_grant_request
- **Archetype**: `A15` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, auditor, external partner or carrier, integration system
- **Trigger**: A support engineer needs access to a customer tenant to diagnose an issue.
- **Preconditions**:
  - The customer's policy states whether support access requires their approval.
  - The grant is scoped to the specific issue and is time-bound.
  - Every action taken under the grant is attributable to the individual, not to a shared role.
- **Steps**:
  1. `TASK` (IT or access administrator) the engineer requests access citing the specific case and the scope needed
  2. `APPROVAL` (compliance officer) the internal approver and, where the customer's policy requires, the customer approve
  3. `CAPABILITY` (integration system) issue a time-bound, scope-limited grant attributable to the individual
  4. `OBSERVE` (auditor) record every read and action under the grant
  5. `WAIT` (integration system) hold to expiry
  6. `SIGNAL` (external partner or carrier) give the customer an access report after the grant ends
- **Intents**:
  - `hcmnext.security.grant_support_access/v1` (PROPOSED, creates)
- **Reads**:
  - `security`: support policy, prior grants, scope definitions
  - `tenant`: the customer's own approval policy
- **Writes**:
  - `security`: grant record, activity log, access report
- **Evidence required**:
  - Customer approval where their policy requires it.
  - Per-action activity log attributable to the individual.
  - Access report delivered to the customer.
- **Failure and repair**:
  - The engineer needs more scope mid session -> a new request and approval; scope creep inside an active grant defeats the control
  - The grant is used outside the cited case -> an incident; the activity log is what makes it detectable
  - The customer's approver is unavailable during an outage -> the break-glass path applies with its own heavier approval and mandatory post-use review
- **Jurisdiction**:
  - US federal: Customer contracts and SOC 2 commitments typically require support access controls with customer visibility; HIPAA business associate agreements make unauthorized access a reportable event.
  - US state variation: State breach notification statutes are triggered by unauthorized access to personal information, including by a vendor's staff.
  - International: GDPR Article 28 requires the processor to ensure persons authorized to process are bound by confidentiality and act only on instructions; an unapproved support access breaches that.

### WF-SEC-002. Rotate a credential lease before expiry

- **Status**: `PARTIAL` - planning/specs/secrets-key-custody-and-credential-leases.md
- **Archetype**: `A17` | **Complexity tier**: 3
- **Actors**: IT or access administrator, integration system, auditor
- **Trigger**: A connector credential approaches expiry and must be rotated without an outage.
- **Preconditions**:
  - The lease's expiry and its rotation lead time are recorded.
  - The target supports overlapping credentials or a defined cutover.
  - In-flight operations using the old credential are identifiable.
- **Steps**:
  1. `CAPABILITY` (integration system) detect the approaching expiry and request a new credential
  2. `CAPABILITY` (IT or access administrator) verify the new credential against the target before retiring the old one
  3. `DECISION` (IT or access administrator) refuse to retire the old credential while operations are still using it
  4. `CAPABILITY` (integration system) cut over and retire the old lease
  5. `OBSERVE` (auditor) confirm operations succeed on the new credential before closing
- **Intents**:
  - `hcmnext.security.rotate_credential/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: in-flight operations using the credential
  - `security`: lease, expiry, rotation policy
- **Writes**:
  - `security`: new lease and the retired one's record
- **Evidence required**:
  - Verification of the new credential before cutover.
  - In-flight operation drain evidence.
  - Retirement record for the old lease.
- **Failure and repair**:
  - The new credential fails verification -> the rotation is BLOCKED and the old credential's expiry becomes an incident with a deadline
  - The target does not support overlapping credentials -> a maintenance window is required and the plan says so rather than attempting a seamless cutover that will fail
  - The old credential is retired while operations are in flight -> those operations fail; the drain check exists to prevent it and its absence is a defect
- **Jurisdiction**:
  - US federal: PCI DSS and several sector rules require periodic credential rotation with documented evidence.
  - US state variation: State data-security statutes require reasonable safeguards, of which credential lifecycle is one.
  - International: DORA requires financial entities to manage cryptographic keys and credentials over their lifecycle with documented procedures.

### WF-SEC-003. Investigate and contain a suspected credential compromise

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 5
- **Actors**: IT or access administrator, compliance officer, auditor, employee
- **Trigger**: Signals suggest a worker's credentials or session have been taken over.
- **Preconditions**:
  - Session, device and behavioural signals are available.
  - Containment actions are separable from investigation actions.
  - The affected data classes determine the notification duties.
- **Steps**:
  1. `OBSERVE` (IT or access administrator) collect the signals and the sessions in question
  2. `DECISION` (IT or access administrator) contain first: invalidate sessions and require re-authentication, before deciding whether it was real
  3. `CAPABILITY` (auditor) preserve logs and session evidence before anything is rotated or deleted
  4. `TASK` (employee) verify with the worker through an out-of-band channel
  5. `RULE` (compliance officer) assess whether personal data was accessed and which notification clocks start
  6. `CAPABILITY` (IT or access administrator) rotate credentials and re-provision
  7. `END` (compliance officer) close with the breach assessment recorded whether or not notification was required
- **Intents**:
  - `hcmnext.security.investigate_compromise/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: what the compromised identity could reach
  - `privacy`: data classes within that reach
  - `security`: sessions, devices, authentication history
- **Writes**:
  - `access`: invalidated sessions and rotated credentials
  - `security`: incident record, containment actions, breach assessment
- **Evidence required**:
  - Preserved logs from before containment.
  - Out-of-band verification with the worker.
  - Breach assessment with its conclusion and its reasoning, even when the conclusion is that no notification is required.
- **Failure and repair**:
  - Containment destroys the evidence needed to scope the incident -> preservation precedes rotation in the step order for exactly this reason
  - The signals were a false positive -> the containment stands and the worker re-authenticates; a false positive costs minutes and a missed compromise costs far more
  - Personal data was accessed -> the notification clocks start from the point of awareness, and awareness is the detection timestamp rather than the conclusion timestamp
- **Jurisdiction**:
  - US federal: There is no general federal breach statute for employee data outside the sectoral ones; HIPAA requires notification within 60 days and the SEC requires material cybersecurity incident disclosure by public filers within four business days of a materiality determination.
  - US state variation: All fifty states have breach notification statutes with different definitions of personal information, different deadlines, and different attorney-general reporting thresholds; a single incident can require dozens of separate notifications.
  - International: GDPR Article 33 requires supervisory authority notification within 72 hours of awareness and Article 34 requires notifying affected individuals where the risk is high.

### WF-SEC-004. Register a principal and establish its authentication

- **Status**: `NEW`
- **Archetype**: `A15` | **Complexity tier**: 3
- **Actors**: IT or access administrator, employee, compliance officer
- **Trigger**: A human or workload principal is registered and bound to an authentication method.
- **Preconditions**:
  - A principal is distinct from a person: one person may hold several principals.
  - The authentication assurance required is derived from what the principal may do.
  - A workload principal has no interactive path and no password.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) create the principal bound to its person or workload, with its type recorded
  2. `RULE` (compliance officer) derive the required authentication assurance from the principal's maximum authority
  3. `TASK` (employee) enroll the authentication factors at the required assurance
  4. `DECISION` (IT or access administrator) refuse an interactive credential on a workload principal and refuse a shared credential on any principal
  5. `END` (compliance officer) close with the principal active and its assurance recorded
- **Intents**:
  - `hcmnext.security.register_principal/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: the maximum authority the principal may hold
  - `people`: the person behind a human principal
  - `security`: existing principals, assurance policy, factor types
- **Writes**:
  - `security`: principal record, factor enrollments and assurance level
- **Evidence required**:
  - The principal type and its binding to a person or a workload.
  - The assurance derivation from the maximum authority.
  - Factor enrollment evidence.
- **Failure and repair**:
  - A shared credential is requested for a team -> REJECT; a shared credential destroys attribution, which every other control in the platform depends on
  - A workload principal is given an interactive password -> REJECT; a workload with an interactive path is a human account wearing a service label
  - A principal's authority later exceeds its assurance level -> the grant is refused until the assurance is raised, rather than the assurance being quietly ignored
- **Jurisdiction**:
  - US federal: None directly, though attribution to an individual is a precondition for every SOX and HIPAA access control.
  - US state variation: State data-security statutes require reasonable authentication controls.
  - International: DORA and NIS2 both require strong authentication for privileged access in covered entities.

### WF-SEC-005. Recover an account after a lost authentication factor

- **Status**: `NEW`
- **Archetype**: `A15` | **Complexity tier**: 4
- **Actors**: employee, IT or access administrator, compliance officer
- **Trigger**: A worker loses their factor and needs to regain access without the recovery path becoming the attack.
- **Preconditions**:
  - The recovery path's assurance is at least as strong as the factor it replaces.
  - Recovery is not self-service for principals holding sensitive authority.
  - Every recovery is recorded and the worker is notified through an independent channel.
- **Steps**:
  1. `TASK` (employee) the worker initiates recovery and is identity-proofed to the required assurance
  2. `DECISION` (compliance officer) route a principal with sensitive authority to an assisted path with a verifier, not to self-service
  3. `CAPABILITY` (IT or access administrator) issue a temporary credential with a short expiry and force enrollment of a new factor
  4. `SIGNAL` (employee) notify the worker through a channel independent of the one being recovered
  5. `OBSERVE` (IT or access administrator) confirm the new factor is enrolled and the temporary credential is expired
  6. `END` (compliance officer) close with the recovery recorded and available for pattern analysis
- **Intents**:
  - `hcmnext.security.recover_account/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: the authority the principal holds
  - `people`: the worker's independent contact points
  - `security`: principal, factors, prior recoveries, assurance policy
- **Writes**:
  - `security`: recovery record, temporary credential, new factor enrollment
- **Evidence required**:
  - Identity proofing method and its assurance.
  - Notification through an independent channel.
  - Temporary credential expiry and the new factor enrollment.
- **Failure and repair**:
  - The recovery path is weaker than the factor it replaces -> REJECT that design; the recovery path becomes the account's real security level
  - Repeated recoveries occur on one principal -> the pattern raises a security review; account recovery is a standard account takeover route
  - The independent notification channel is the one being recovered -> REJECT; a notification to the compromised channel warns nobody
- **Jurisdiction**:
  - US federal: None directly, though an account takeover reaching personal information starts breach assessment clocks.
  - US state variation: State breach notification statutes are triggered by unauthorized acquisition, which an account takeover can constitute.
  - International: GDPR Article 32 requires appropriate security including identity verification before granting access.

---

## ANA. Reporting and analytics

5 workflows.

### WF-ANA-001. Define, schedule and run a governed report

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 25 features report_define_and_schedule
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: HR business partner, auditor, compliance officer, integration system
- **Trigger**: A recurring report is defined, authorized and delivered on a schedule.
- **Preconditions**:
  - The report's field set, its population and its recipients are declared.
  - Authorization is evaluated per run against the recipient, not once at definition time.
  - A small-cell suppression rule applies where the output is aggregate.
- **Steps**:
  1. `CAPABILITY` (HR business partner) define the report with its fields, population and recipients
  2. `APPROVAL` (compliance officer) the data authority approves the field set and the recipient list
  3. `WAIT` (integration system) hold to the schedule
  4. `CAPABILITY` (integration system) evaluate authorization for each recipient at run time and produce per-recipient outputs
  5. `DECISION` (compliance officer) suppress rows and cells the recipient may not see rather than failing the whole run
  6. `END` (auditor) close with the delivery evidence and the suppression record per recipient
- **Intents**:
  - `hcmnext.analytics.define_report/v1` (PROPOSED, creates)
  - `hcmnext.analytics.run_report/v1` (PROPOSED, creates)
- **Reads**:
  - `analytics`: report definition, dataset, watermark
  - `people`: recipient authorization
  - `privacy`: field classification and suppression rules
- **Writes**:
  - `analytics`: report run, per-recipient outputs, delivery records
- **Evidence required**:
  - Per-run authorization evaluation per recipient.
  - Suppression record showing what each recipient did not see.
  - Dataset watermark, so two recipients' copies are comparable.
- **Failure and repair**:
  - A recipient's authorization narrows between runs -> the next run reflects it; an authorization evaluated once at definition time is how stale access persists in reporting
  - The population is small enough to identify individuals -> suppress and record; aggregate reports are the most common accidental disclosure channel
  - A recipient leaves the organization -> delivery stops at the next run and the change is recorded; a scheduled report to a departed employee is a live exposure
- **Jurisdiction**:
  - US federal: EEO-1 and VETS-4212 reporting are mandatory for covered employers with prescribed formats; the underlying data must be reproducible.
  - US state variation: California Government Code 12999 pay data reporting and Illinois equal pay registration each have their own formats and deadlines; several states require establishment-level breakdowns that a national report does not produce.
  - International: The EU Pay Transparency Directive introduces mandatory gender pay gap reporting with thresholds phased by employer size.

### WF-ANA-002. Publish a metric definition and observe it over time

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 2
- **Actors**: HR business partner, finance partner, auditor, integration system
- **Trigger**: A workforce metric is defined once and observed consistently across periods.
- **Preconditions**:
  - The metric's formula, its population and its period are versioned.
  - The observation stores the inputs, not only the result.
  - A definition change creates a new version rather than restating history.
- **Steps**:
  1. `TASK` (HR business partner) define the metric with its formula, population rule and period
  2. `APPROVAL` (finance partner) the metric owner and finance approve the definition
  3. `CAPABILITY` (integration system) observe the metric each period, storing the inputs and the watermark
  4. `DECISION` (HR business partner) on a definition change, publish a new version; do not restate prior observations
  5. `END` (auditor) close each observation as an immutable fact
- **Intents**:
  - `hcmnext.analytics.define_metric/v1` (PROPOSED, creates)
- **Reads**:
  - `analytics`: metric definitions and prior observations
  - `people`: population the metric measures
- **Writes**:
  - `analytics`: metric definition versions and period observations
- **Evidence required**:
  - Formula version with each observation.
  - Stored inputs, so an observation can be recomputed.
  - Version boundary where a definition changed.
- **Failure and repair**:
  - A metric is redefined and history is restated -> REJECT; a restated trend line that nobody can reconcile is how metrics lose credibility
  - An observation's inputs are no longer retrievable -> the observation stands but is marked unverifiable
  - Two teams define the same-named metric differently -> the naming collision is rejected at definition; two metrics with one name is worse than two names
- **Jurisdiction**:
  - US federal: None directly, though metrics published to investors by public filers become subject to disclosure controls.
  - US state variation: None.
  - International: The EU Pay Transparency Directive prescribes specific gap metrics, so a tenant-defined metric cannot substitute for the statutory one.

### WF-ANA-003. Produce an evidence package for an audit

- **Status**: `EXISTING` - internal/ledger/evidence.go: manifest, parts, findings and verification
- **Archetype**: `A6` | **Complexity tier**: 4
- **Actors**: auditor, compliance officer, IT or access administrator, integration system
  - performing a step: auditor, compliance officer, integration system
  - participating without owning a step: IT or access administrator
- **Trigger**: An auditor requests reproducible evidence for a set of transactions or controls.
- **Preconditions**:
  - The scope of the request is expressed as a query over the ledger, not as an ad hoc extract.
  - The package's integrity can be verified independently.
  - Tenant isolation is enforced in the package itself.
- **Steps**:
  1. `CAPABILITY` (auditor) resolve the scope and assemble the evidence parts with their digests
  2. `CAPABILITY` (integration system) build the manifest and seal the package
  3. `CAPABILITY` (auditor) verify the package: every listed part present, no unlisted part, digests match, chain intact, head attested
  4. `DECISION` (compliance officer) refuse to release a package whose verification reports a tenant leak or a broken chain
  5. `END` (auditor) close with the verification report attached to the package
- **Intents**:
  - `hcmnext.operations.build_evidence_package/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: artifacts referenced by the events
  - `ledger`: events, chain, epoch attestations
  - `privacy`: classification limiting what may be included
- **Writes**:
  - `operations`: evidence package, manifest and verification report
- **Evidence required**:
  - Manifest with a digest per part.
  - Verification findings, including the absence of findings.
  - Chain and epoch coverage proof.
- **Failure and repair**:
  - A part is missing from the package but listed in the manifest -> the verification reports a missing part and the package is not released
  - An unlisted part is present -> reported as an unlisted part; extra content is as much a defect as missing content
  - A digest does not match -> tampering or corruption; either way the package is not evidence until it is explained
- **Jurisdiction**:
  - US federal: Federal Rules of Evidence 901 and 902(13) allow authentication of records by a certification of a process producing an accurate result, which is precisely what the verification report provides.
  - US state variation: State e-discovery rules mirror the federal authentication approach.
  - International: In EU proceedings, eIDAS qualified timestamps and seals provide a comparable presumption of integrity.

### WF-ANA-004. Analyse turnover and its drivers for a population

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: HR business partner, manager, compliance officer, AI agent, integration system
- **Trigger**: Leadership asks why people are leaving a unit and what distinguishes leavers from stayers.
- **Preconditions**:
  - The cohort, the period and the turnover definition, voluntary or total, are declared before the analysis.
  - Protected characteristics may only be included under a permitting purpose.
  - The output is descriptive; it does not identify individuals as flight risks to their managers.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the cohort and pin the period watermark
  2. `TRANSFORM` (integration system) compute turnover by the declared definition and compare leaver and stayer distributions
  3. `DECISION` (compliance officer) suppress any cut below the small-cell threshold and refuse to return an individual-level flight risk score to a manager
  4. `CAPABILITY` (AI agent) return the analysis with its assumptions, its model version and its limitations
  5. `END` (manager) complete with zero writes
- **Intents**:
  - `hcmnext.analytics.analyze_turnover/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: pay position, subject to authorization
  - `people`: population, terminations, tenure
  - `talent`: ratings and engagement signals where a purpose permits
- **Writes**:
  - `analytics`: analysis result with its model version and controls
- **Evidence required**:
  - The turnover definition used, since voluntary and total turnover give different answers.
  - Suppression record for small cells.
  - An explicit statement of what the analysis does not establish, because correlation in an exit cohort is routinely read as causation.
- **Failure and repair**:
  - A manager requests per-person attrition risk scores for their team -> DENIED; an individual flight-risk score handed to a manager changes how that person is treated and is the clearest example of an analysis becoming a decision
  - A cut identifies an individual by combination of attributes -> suppressed under the small-cell rule with the suppression disclosed
  - Protected characteristics are included with no permitting purpose -> DENY before computation; the refusal must not reveal the cohort size
- **Jurisdiction**:
  - US federal: Turnover analysis touching protected characteristics engages Title VII and the ADEA if it drives decisions; the analysis itself is discoverable and its retention class should reflect that.
  - US state variation: State pay data reporting consumes some of the same cuts, and a turnover analysis inconsistent with a filed report invites questions.
  - International: Under GDPR Article 22 the analysis must not by itself decide anything about an individual, and profiling for retention purposes requires a lawful basis and transparency.

### WF-ANA-005. Design and publish a dashboard with per-viewer authorization

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, manager, integration system
- **Trigger**: A dashboard is built once and viewed by people with different authorization.
- **Preconditions**:
  - Authorization is evaluated per viewer at render time, not baked in at design time.
  - Every tile declares its field set and its suppression rule.
  - A viewer who can see fewer fields sees a smaller dashboard, not a broken one.
- **Steps**:
  1. `TASK` (HR business partner) design the tiles, their field sets and their suppression rules
  2. `APPROVAL` (compliance officer) the data authority approves the field sets
  3. `CAPABILITY` (integration system) render per viewer, evaluating authorization tile by tile
  4. `DECISION` (compliance officer) a tile the viewer may not see renders as DENIED by name rather than being hidden, so the viewer knows the picture is partial
  5. `END` (manager) close each render with its per-viewer disclosure record
- **Intents**:
  - `hcmnext.analytics.publish_dashboard/v1` (PROPOSED, creates)
- **Reads**:
  - `analytics`: dashboard definition, tile field sets, dataset watermark
  - `privacy`: field classification and per-viewer authorization
- **Writes**:
  - `analytics`: dashboard version and per-render disclosure records
- **Evidence required**:
  - Per-tile field set as approved.
  - Per-viewer disclosure result including the denied tiles.
  - Dataset watermark, so two viewers' numbers are comparable.
- **Failure and repair**:
  - A tile is hidden rather than marked denied -> the viewer draws conclusions from an incomplete picture without knowing it is incomplete
  - Two viewers see different totals for the same tile -> correct where their authorization differs, and the disclosure record explains it; a shared total that ignores authorization is the leak
  - A viewer's authorization narrows between renders -> the next render reflects it; authorization evaluated once at publication is how stale access persists
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None directly.
  - International: GDPR data minimization argues for tiles that carry the least data supporting the decision the viewer makes.

---

## AIA. Semantic and agent intelligence

3 workflows.

### WF-AIA-001. Let an agent propose a change a human then approves

- **Status**: `PARTIAL` - planning/plan.md section 5.14 Agents Interpret; Deterministic Services Execute; planning/todos.md section 18 bounded agent and intelligence safety
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: AI agent, manager, HR business partner, compliance officer, integration system
  - performing a step: AI agent, manager, compliance officer, integration system
  - participating without owning a step: HR business partner
- **Trigger**: An agent interprets a request and produces a proposal that a human must approve before anything happens.
- **Preconditions**:
  - The agent's tool set and its authority ceiling are declared.
  - The agent's authority never exceeds the requesting principal's own.
  - Every tool invocation is logged with its inputs and outputs.
- **Steps**:
  1. `AGENT` (AI agent) the agent interprets the request and calls read tools within the requester's authority
  2. `DECISION` (compliance officer) deny any tool call that would read or write beyond the requester's own authority
  3. `TRANSFORM` (AI agent) the agent assembles a typed proposal; it never assembles a free-form command
  4. `APPROVAL` (manager) a human approves the exact proposal, seeing what the agent saw
  5. `CAPABILITY` (integration system) execute through the ordinary governed path, with the agent recorded as the proposer
  6. `END` (compliance officer) close with the agent's trace retained as evidence of how the proposal arose
- **Intents**:
  - `hcmnext.intelligence.propose_change/v1` (PROPOSED, creates)
  - `hcmnext.work.approve_proposal/v1` (REAL, advances)
- **Reads**:
  - `governance`: authority ceiling for agent-initiated intents
  - `intelligence`: agent definition, tool permissions, memory scope
  - `people`: whatever the requester may read
- **Writes**:
  - `intelligence`: agent execution trace and tool invocation log
  - `work`: the proposal the agent produced
- **Evidence required**:
  - Complete tool invocation trace with inputs and outputs.
  - The authority ceiling evaluated per call.
  - Human approval bound to the proposal digest.
- **Failure and repair**:
  - The agent's proposal cites data the approver cannot see -> the proposal is refused rather than shown with holes; an approver who cannot see the basis cannot approve it
  - The agent attempts an action outside its declared tool set -> DENY at the tool gateway and record it; an undeclared tool call is a containment failure
  - The agent's output is treated as the decision -> FAIL_CLOSED; agent initiation never increases authority and never substitutes for the approval
- **Jurisdiction**:
  - US federal: EEOC guidance treats AI-assisted employment decisions as covered by Title VII and the ADA regardless of the tool's vendor.
  - US state variation: New York City Local Law 144 requires bias audits and notice for automated employment decision tools; Colorado's AI Act imposes duties on deployers of high-risk systems from its effective date; Illinois and Maryland regulate specific technologies.
  - International: The EU AI Act classifies employment-related AI as high risk with human oversight, logging, transparency and conformity duties; GDPR Article 22 restricts solely automated decisions with significant effects.

### WF-AIA-002. Ground an agent answer in cited knowledge and refuse otherwise

- **Status**: `EXISTING` - internal/domains/knowledge package: citations and audience scope
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: AI agent, employee, HR business partner, integration system
- **Trigger**: A worker asks a policy question and the assistant must answer only from published, in-scope knowledge.
- **Preconditions**:
  - Only published articles within the worker's audience scope and jurisdiction are retrievable.
  - Every assertion in the answer carries a citation.
  - An uncited answer is refused rather than generated.
- **Steps**:
  1. `CAPABILITY` (integration system) retrieve candidate articles scoped to the worker's jurisdiction and audience
  2. `AGENT` (AI agent) compose an answer, citing the article version behind each assertion
  3. `DECISION` (AI agent) refuse to answer when no in-scope article supports the question, and route to a human
  4. `SIGNAL` (employee) return the answer with its citations so the worker can check it
  5. `END` (HR business partner) close with the retrieval set and the citations retained
- **Intents**:
  - `hcmnext.intelligence.answer_policy_question/v1` (PROPOSED, creates)
- **Reads**:
  - `knowledge`: published articles, their versions and audience scopes
  - `people`: the worker's jurisdiction and scope
- **Writes**:
  - `intelligence`: answer record with its citations and retrieval set
- **Evidence required**:
  - Retrieval set, so an answer's basis is reconstructible.
  - Citation per assertion with the article version.
  - Refusal record where no article supported the question.
- **Failure and repair**:
  - An article exists but is outside the worker's jurisdiction -> it is not retrieved; a correct answer for the wrong state is worse than a refusal
  - The agent generates a plausible answer with no citation -> the answer is suppressed; an uncited policy answer is exactly the failure mode this design exists to prevent
  - The cited article is later corrected -> the answer record keeps the version it cited, so the reliance interval is identifiable
- **Jurisdiction**:
  - US federal: Incorrect leave or wage guidance does not shift liability from the employer; the citation trail limits the exposure to what was actually published.
  - US state variation: State-specific policy differences are the main source of wrong answers, which is why jurisdiction scoping is a hard filter rather than a ranking signal.
  - International: The EU AI Act's transparency duties require the worker to know they are interacting with an AI system.

### WF-AIA-003. Record and act on an AI incident

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: compliance officer, IT or access administrator, auditor
- **Trigger**: An agent produced a harmful, wrong or unauthorized output and it must be handled as an incident.
- **Preconditions**:
  - Agent traces are retained long enough to investigate.
  - The affected decisions and populations are identifiable from the trace.
  - A model or prompt rollback path exists.
- **Steps**:
  1. `OBSERVE` (compliance officer) detect or receive the report and preserve the trace
  2. `TRANSFORM` (IT or access administrator) identify every decision the same agent version touched in the affected window
  3. `DECISION` (compliance officer) suspend the agent version rather than patching it in place while the investigation runs
  4. `TASK` (compliance officer) review the affected decisions and correct those that were wrong
  5. `CAPABILITY` (auditor) record the incident with its cause, its blast radius and its remediation
  6. `END` (compliance officer) close only when every affected decision is reviewed
- **Intents**:
  - `hcmnext.intelligence.record_ai_incident/v1` (PROPOSED, creates)
- **Reads**:
  - `intelligence`: agent version, traces, affected executions
  - `work`: decisions the agent contributed to
- **Writes**:
  - `intelligence`: incident record, suspension, remediation
  - `work`: corrected decisions where required
- **Evidence required**:
  - Preserved traces from before the suspension.
  - The blast radius: every decision the agent version touched.
  - Per-decision review outcome.
- **Failure and repair**:
  - The trace retention window is shorter than the detection lag -> the blast radius cannot be computed and the incident is recorded as unbounded, which is itself a finding about retention
  - The agent version is patched before the trace is preserved -> evidence is lost; suspension precedes patching in the step order for that reason
  - An affected decision has already had employment consequences -> the correction runs through the ordinary corrective intent with notice to the worker
- **Jurisdiction**:
  - US federal: The EEOC and FTC have both signalled that a vendor's tool does not shield the employer; an unremediated AI incident affecting employment decisions is an enforcement risk.
  - US state variation: Colorado's AI Act requires deployers to notify the attorney general of discovered algorithmic discrimination within a set period; New York City requires published bias audit results.
  - International: The EU AI Act requires providers and deployers of high-risk systems to report serious incidents to the market surveillance authority.

---

## REP. Reconciliation, repair and operations

4 workflows.

### WF-REP-001. Diagnose drift and build a bounded repair plan

- **Status**: `EXISTING` - definitions/governance/intent-conformance-descriptors.yaml descriptors detect_drift, create_repair_plan and simulate_repair; internal/domains/repair package
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
- **Trigger**: Expected and observed state disagree and a correction must be planned before it is executed.
- **Preconditions**:
  - The drift finding names the resource, the expected value and the observed value.
  - The observation's freshness is within policy.
  - The plan is a recommendation with no execution capability of its own.
- **Steps**:
  1. `CAPABILITY` (integration system) produce the drift finding with both sides and their watermarks
  2. `TRANSFORM` (IT or access administrator) diagnose the likely cause and enumerate the actions that would reconcile it
  3. `CAPABILITY` (IT or access administrator) simulate the plan and report exactly what each action would change
  4. `DECISION` (compliance officer) refuse to plan where the cause is unknown; a repair against an unknown cause tends to recreate the drift
  5. `APPROVAL` (compliance officer) the resource owner approves the plan bound to the simulation digest
  6. `END` (auditor) close the planning intent; execution is a separate governed intent
- **Intents**:
  - `hcmnext.operations.detect_drift/v1` (REAL, creates)
  - `hcmnext.operations.create_repair_plan/v1` (REAL, creates)
  - `hcmnext.operations.simulate_repair/v1` (REAL, creates)
- **Reads**:
  - `integration`: observations and their watermarks
  - `operations`: drift findings, prior repairs, resource ownership
- **Writes**:
  - `operations`: repair plan with its actions, risks and simulation
- **Evidence required**:
  - Both watermarks behind the finding.
  - Diagnosis with its cause, or an explicit unknown.
  - Simulation output the approval bound to.
- **Failure and repair**:
  - The plan is executed without approval -> impossible by construction; the planning package has no execute function, not behind a flag and not behind a policy check
  - The simulation and the eventual execution disagree -> the execution is BLOCKED and the plan is regenerated against current state
  - The same drift recurs after repair -> the cause diagnosis was wrong; the second occurrence escalates to an incident rather than a second repair
- **Jurisdiction**:
  - US federal: None directly; the drift's subject matter determines the exposure, whether that is pay, access or a filing.
  - US state variation: None.
  - International: None.

### WF-REP-002. Execute an approved repair and verify the outcome

- **Status**: `PARTIAL` - planning/specs/transaction-ledger-reconciliation-and-repair.md
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
  - performing a step: IT or access administrator, auditor, integration system
  - participating without owning a step: compliance officer
- **Trigger**: An approved repair plan is carried out and its effect confirmed.
- **Preconditions**:
  - The plan is approved and its simulation digest is bound.
  - The current state still matches the state the plan was built against.
  - Each action is independently observable.
- **Steps**:
  1. `CHECKPOINT` (IT or access administrator) revalidate that the state still matches the plan's assumptions
  2. `DECISION` (IT or access administrator) abort and replan when the state has moved
  3. `CAPABILITY` (integration system) execute each action in the declared order
  4. `OBSERVE` (integration system) verify each action's effect by reading back, not by the return code
  5. `END` (auditor) close ConsistencyState only when every action verifies; a partly verified repair stays open
- **Intents**:
  - `hcmnext.operations.execute_repair/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: target systems for each action
  - `operations`: approved plan, its simulation, current state
- **Writes**:
  - `operations`: repair execution records and verification observations
- **Evidence required**:
  - Revalidation result before execution.
  - Per-action read-back verification.
  - The residual set where verification failed.
- **Failure and repair**:
  - An action succeeds but the read-back disagrees -> the action is recorded as unverified and the drift persists; a return code is not evidence
  - The state moved between approval and execution -> abort and replan; executing a stale plan is how a repair becomes a second incident
  - Half the actions verify -> no false rollback; the verified half stands and the rest reopen as a new finding
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-REP-003. Quarantine poisoned work and drain it safely

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 5
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
- **Trigger**: A class of work items or messages is failing repeatedly and threatens the queue.
- **Preconditions**:
  - A poison-detection rule exists based on attempt count and failure signature.
  - Quarantined work is preserved, not discarded.
  - The drain path requires a human decision per class.
- **Steps**:
  1. `OBSERVE` (integration system) detect repeated failures with the same signature
  2. `CAPABILITY` (IT or access administrator) quarantine the affected items, preserving their payloads and their history
  3. `SIGNAL` (compliance officer) raise an incident naming the class, the count and the affected tenants
  4. `TASK` (IT or access administrator) diagnose the class and decide: fix and replay, transform and replay, or discard with a recorded reason
  5. `APPROVAL` (compliance officer) discard requires approval; it is the only path that loses work
  6. `CAPABILITY` (integration system) drain the quarantine under the chosen disposition
  7. `END` (auditor) close when the quarantine is empty and the detection rule is tuned
- **Intents**:
  - `hcmnext.operations.quarantine_work/v1` (PROPOSED, creates)
- **Reads**:
  - `operations`: queue state, failure signatures, prior quarantines
  - `workflow`: instances blocked behind the poisoned items
- **Writes**:
  - `operations`: quarantine records and drain dispositions
- **Evidence required**:
  - Preserved payloads and their failure history.
  - Per-class disposition with its reason.
  - Approval for any discard.
- **Failure and repair**:
  - Items are discarded to clear the queue -> REJECT without approval; silently dropping work is how a payroll batch loses a worker
  - The quarantine grows faster than it drains -> escalate; the detection rule is catching a systemic failure and the upstream must be stopped
  - A quarantined item is replayed and fails identically -> it returns to quarantine with an incremented attempt count and the class disposition is revisited
- **Jurisdiction**:
  - US federal: None directly, though quarantined payroll or filing work has statutory deadlines that keep running.
  - US state variation: None.
  - International: None.

### WF-REP-004. Run a recovery test and prove the restore works

- **Status**: `PARTIAL` - planning/specs/operations-production.md via planning/data/models/operations-production.md; planning/todos.md section 16 operations, assurance, overload and recovery
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, auditor, compliance officer
- **Trigger**: A backup is restored into an isolated environment and verified against the recovery contract.
- **Preconditions**:
  - A recovery contract states the objective time and the objective point.
  - The restore target is isolated so it cannot affect production.
  - Verification is by content, not by the restore job's exit status.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) select the backup set and restore into an isolated environment
  2. `CAPABILITY` (auditor) verify content: ledger chain intact, row counts, referential integrity, tenant isolation
  3. `TRANSFORM` (IT or access administrator) measure the achieved recovery time and point against the contract
  4. `DECISION` (compliance officer) record a failed test as a failure; a restore that took longer than the objective did not meet the contract
  5. `CAPABILITY` (IT or access administrator) destroy the restored environment and its data
  6. `END` (auditor) close with the measured figures, not with a pass or fail alone
- **Intents**:
  - `hcmnext.operations.run_recovery_test/v1` (PROPOSED, creates)
- **Reads**:
  - `ledger`: chain integrity for verification
  - `operations`: backup sets, recovery contract, prior test results
- **Writes**:
  - `operations`: restore test record with its measured objectives and findings
- **Evidence required**:
  - Measured recovery time and point.
  - Content verification findings, including tenant isolation.
  - Destruction record for the restored environment.
- **Failure and repair**:
  - The restore succeeds but the ledger chain is broken -> a failure; a restored database that cannot prove its own integrity is not a recovery
  - Production data is restored into an environment with wider access -> an immediate incident; a restore is a copy and inherits every obligation of the original
  - The test is skipped because production is stable -> the contract's test cadence is the control; an untested backup is a hypothesis
- **Jurisdiction**:
  - US federal: SOC 2 and ISO 27001 both require tested recovery; a public filer's disclosure controls depend on it.
  - US state variation: State data-security statutes require reasonable safeguards including the ability to restore.
  - International: DORA requires financial entities to test their recovery plans and to report severe incidents; GDPR Article 32(1)(c) explicitly requires the ability to restore availability in a timely manner.

---

## BIL. Billing and commercial

4 workflows.

### WF-BIL-001. Meter usage and produce an invoice at semantic boundaries

- **Status**: `PARTIAL` - planning/plan.md section 5.16 Bill at Semantic Boundaries; Meter Technical Work Precisely
- **Archetype**: `A10` | **Complexity tier**: 3
- **Actors**: finance partner, external partner or carrier, auditor, integration system
- **Trigger**: Usage is metered per intent and rated into an invoice for a billing period.
- **Preconditions**:
  - The rate card and the commercial entitlement are versioned and effective-dated.
  - Usage events carry the semantic boundary they belong to, not just a technical count.
  - Disputes are anticipated, so the invoice must be explainable to a line.
- **Steps**:
  1. `CAPABILITY` (integration system) collect usage events for the period with their semantic boundaries
  2. `TRANSFORM` (finance partner) rate the usage against the effective rate card and the entitlement
  3. `DECISION` (finance partner) exclude usage that the entitlement covers rather than billing and crediting it
  4. `APPROVAL` (finance partner) finance approves the invoice before issue
  5. `CAPABILITY` (external partner or carrier) issue the invoice with per-line traceability to the underlying intents
  6. `END` (auditor) close with the dispute window open
- **Intents**:
  - `hcmnext.commercial.record_usage/v1` (PROPOSED, creates)
  - `hcmnext.commercial.generate_invoice/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: rate card, entitlement, prior invoices
  - `operations`: usage events and their semantic boundaries
- **Writes**:
  - `commercial`: rated usage, invoice and its lines
- **Evidence required**:
  - Rate card version applied.
  - Per-line traceability from the invoice to the intents behind it.
  - Entitlement coverage applied before billing, not as a credit.
- **Failure and repair**:
  - A line cannot be traced to underlying intents -> the line is held; an unexplainable charge is a dispute waiting to happen
  - The rate card changes mid period -> the effective-dated rate applies per event, not per invoice
  - Usage is recorded against a retired entitlement -> the event is flagged rather than billed at list price
- **Jurisdiction**:
  - US federal: Revenue recognition under ASC 606 requires the performance obligation to be identifiable, which is why billing at semantic boundaries matters beyond fairness.
  - US state variation: State sales and use tax treatment of software services varies; the invoice must carry enough detail to source each line.
  - International: EU VAT place-of-supply rules for digital services depend on the customer's location and status, which the invoice must record.

### WF-BIL-002. Handle a billing dispute and issue an adjustment

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 3
- **Actors**: external partner or carrier, finance partner, auditor, integration system
- **Trigger**: A customer disputes an invoice line and the charge must be explained or credited.
- **Preconditions**:
  - The disputed line traces to its underlying events.
  - The dispute window and the payment obligation during dispute are defined.
  - The adjustment authority and its threshold are configured.
- **Steps**:
  1. `TASK` (external partner or carrier) the customer raises the dispute against specific lines
  2. `CAPABILITY` (finance partner) produce the line's supporting detail from the usage events
  3. `DECISION` (finance partner) uphold or credit; a partial credit is recorded as such rather than as a full one
  4. `APPROVAL` (finance partner) the adjustment authority approves the credit at its threshold
  5. `CAPABILITY` (integration system) issue the adjustment as a new document referencing the original invoice
  6. `END` (auditor) close with the dispute outcome recorded and the pattern available for analysis
- **Intents**:
  - `hcmnext.commercial.resolve_dispute/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: invoice, lines, usage events, prior disputes
  - `operations`: the intents behind the disputed usage
- **Writes**:
  - `commercial`: dispute record and adjustment document
- **Evidence required**:
  - Line detail produced to the customer.
  - Adjustment approval at the right threshold.
  - The original invoice, unaltered.
- **Failure and repair**:
  - The original invoice is edited instead of adjusted -> REJECT; an amended invoice with no trace is an audit finding and a revenue recognition problem
  - The same line type is disputed repeatedly -> the pattern is surfaced; a recurring dispute means the metering or the contract is wrong
  - The dispute is raised after the window -> the window is enforced but the underlying question is still answered, since refusing to explain a charge is not a defence
- **Jurisdiction**:
  - US federal: ASC 606 requires variable consideration such as expected credits to be estimated; a pattern of credits changes revenue recognition.
  - US state variation: State consumer protection statutes rarely reach business-to-business billing, but unfair practice claims are possible.
  - International: EU consumer rules do not apply to business customers, but VAT credit notes have their own formal requirements.

### WF-BIL-003. Grant and enforce a commercial entitlement

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 2
- **Actors**: finance partner, IT or access administrator, external partner or carrier, integration system
- **Trigger**: A contract grants a customer the right to use specific capabilities up to specific limits.
- **Preconditions**:
  - The entitlement is versioned and effective-dated against the contract.
  - Enforcement happens at the capability boundary, not at the user interface.
  - Exceeding a limit has a declared behaviour: block, meter as overage or warn.
- **Steps**:
  1. `CAPABILITY` (finance partner) issue the entitlement with its capability set, limits and interval
  2. `DECISION` (finance partner) declare the over-limit behaviour per capability rather than applying one global rule
  3. `CAPABILITY` (integration system) enforce at the capability boundary for every invocation
  4. `OBSERVE` (IT or access administrator) record consumption against the limits
  5. `SIGNAL` (external partner or carrier) notify the customer before a hard limit blocks their work
- **Intents**:
  - `hcmnext.commercial.grant_entitlement/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: contract, prior entitlements, consumption history
  - `governance`: capability registry
- **Writes**:
  - `commercial`: entitlement record and consumption
  - `governance`: capability availability per tenant
- **Evidence required**:
  - Entitlement version tied to the contract.
  - Per-invocation enforcement, not per-session.
  - Consumption record and the pre-limit notification.
- **Failure and repair**:
  - A capability is used through an API path that skips the check -> the enforcement is at the capability, so there is no such path; a check only in the interface is the defect this design avoids
  - A hard limit blocks a payroll run -> the over-limit behaviour for a critical capability should be overage rather than block, which is why the behaviour is declared per capability
  - The entitlement expires mid contract term -> the mismatch between the entitlement interval and the contract term is a data quality finding, caught before it blocks anyone
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None, though a hard block on a capability required for a statutory filing would create a customer compliance failure with contractual consequences.

### WF-BIL-004. Attribute platform cost to a tenant and a capability

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: finance partner, IT or access administrator, auditor, integration system
  - performing a step: finance partner, auditor, integration system
  - participating without owning a step: IT or access administrator
- **Trigger**: Infrastructure and model costs must be attributed to the tenants and capabilities that caused them.
- **Preconditions**:
  - Every metered unit carries a tenant and a capability identity.
  - Shared cost has a declared allocation rule rather than an implicit one.
  - Attribution is reconcilable against the total bill.
- **Steps**:
  1. `CAPABILITY` (integration system) collect metered units with their tenant and capability tags
  2. `TRANSFORM` (finance partner) allocate shared cost by the declared rule
  3. `DECISION` (finance partner) reconcile the attributed total against the actual bill and report the unattributed residual
  4. `CAPABILITY` (auditor) publish the attribution for margin analysis
  5. `END` (finance partner) complete as an analysis with zero writes to commercial records
- **Intents**:
  - `hcmnext.commercial.attribute_cost/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: the actual vendor bills
  - `operations`: metered units and their tags
- **Writes**:
  - `analytics`: attribution result with its residual
- **Evidence required**:
  - Allocation rule version for shared cost.
  - Reconciliation against the actual bill.
  - The unattributed residual, stated.
- **Failure and repair**:
  - Untagged usage exists -> it lands in the residual and the tagging gap is a finding; silently spreading it across tenants makes every margin figure wrong
  - The allocation rule changes -> prior periods keep their rule; restating cost history destroys trend comparability
  - A single tenant dominates a shared resource -> the allocation rule's fairness becomes a commercial question, which the attribution surfaces rather than hides
- **Jurisdiction**:
  - US federal: None.
  - US state variation: None.
  - International: None.

---

## TEN. Tenant and platform administration

5 workflows.

### WF-TEN-001. Provision a new tenant and its environments

- **Status**: `PARTIAL` - planning/specs/platform-plane-model.md; planning/todos.md section 14 tenant lifecycle
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: IT or access administrator, compliance officer, finance partner, integration system
- **Trigger**: A new customer tenant is created with its environments, placement and entitlements.
- **Preconditions**:
  - The data residency requirement is known before placement is chosen.
  - The commercial entitlement is issued before the tenant can consume metered capabilities.
  - The bootstrap administrator identity is established through a verified channel.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) resolve placement from the residency requirement and the available cells
  2. `DECISION` (compliance officer) refuse a placement that violates the stated residency, even if capacity is available
  3. `CAPABILITY` (integration system) create the tenant, its production and sandbox environments and its key ring
  4. `CAPABILITY` (finance partner) issue the commercial entitlement and the feature flag set
  5. `TASK` (IT or access administrator) establish the bootstrap administrator through a verified out-of-band channel
  6. `END` (compliance officer) close with the tenant isolated and its placement recorded
- **Intents**:
  - `hcmnext.tenant.provision_tenant/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: the contracted entitlement
  - `security`: key ring and bootstrap identity policy
  - `tenant`: cell capacity, residency rules, placement policy
- **Writes**:
  - `commercial`: entitlement record
  - `tenant`: tenant, environments, placement, key ring
- **Evidence required**:
  - Placement decision with its residency basis.
  - Key ring creation with its custody.
  - Bootstrap identity establishment through a verified channel.
- **Failure and repair**:
  - The bootstrap identity is established by email alone -> REJECT; the first administrator is the root of the tenant's entire trust chain
  - Placement is chosen for capacity over residency -> FAIL_CLOSED; residency is a contractual and often legal commitment
  - Two tenants are created for the same customer by mistake -> both exist and must be reconciled deliberately; merging tenants is far harder than merging records
- **Jurisdiction**:
  - US federal: None directly, though a tenant holding protected health information triggers business associate obligations from the moment data lands.
  - US state variation: Some state and public-sector contracts require in-state data residency.
  - International: GDPR Chapter V and national residency requirements in some member states make placement a legal decision rather than an operational one.

### WF-TEN-002. Reset a sandbox from production with masking

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
- **Trigger**: A sandbox is refreshed from production data for testing.
- **Preconditions**:
  - The masking policy is defined per field class, not per table.
  - Sandbox access is narrower than production, or the copy is a privacy incident.
  - The reset destroys the prior sandbox contents verifiably.
- **Steps**:
  1. `APPROVAL` (compliance officer) compliance approves the copy and the masking policy version
  2. `CAPABILITY` (integration system) extract, mask and load into the sandbox
  3. `DECISION` (compliance officer) verify masking by sampling the loaded data rather than trusting the transform
  4. `CAPABILITY` (IT or access administrator) narrow sandbox access to the approved list
  5. `OBSERVE` (auditor) confirm no unmasked field survived
  6. `END` (auditor) close with the masking verification report
- **Intents**:
  - `hcmnext.tenant.reset_sandbox/v1` (PROPOSED, creates)
- **Reads**:
  - `privacy`: field classification driving the masking
  - `tenant`: sandbox state, masking policy
- **Writes**:
  - `tenant`: reset record and masking verification
- **Evidence required**:
  - Masking policy version applied.
  - Sampling verification of the loaded data.
  - Sandbox access list after the reset.
- **Failure and repair**:
  - A field class is added to production without a masking rule -> the reset is BLOCKED; an unmasked new field is how production personal data reaches a test environment
  - A developer needs unmasked data to reproduce a defect -> that is a support access grant against production, not a sandbox copy
  - The prior sandbox contents are not destroyed -> the old copy persists with the old access list, which is a second exposure
- **Jurisdiction**:
  - US federal: HIPAA's minimum necessary rule and the security rule both apply to test copies of protected health information.
  - US state variation: State breach notification statutes treat a test environment copy as the same personal information as production.
  - International: GDPR requires a lawful basis and purpose limitation for test processing; pseudonymization is expected under Article 25 data protection by design.

### WF-TEN-003. Relocate a tenant between cells without data loss

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 5
- **Actors**: IT or access administrator, compliance officer, external partner or carrier, auditor, integration system
- **Trigger**: A tenant must move to a different cell for capacity, residency or isolation reasons.
- **Preconditions**:
  - The destination satisfies the residency requirement.
  - A cutover plan exists with a declared read-only window.
  - Rollback is possible until a declared point of no return.
- **Steps**:
  1. `CAPABILITY` (compliance officer) verify the destination's residency and capacity
  2. `CAPABILITY` (IT or access administrator) replicate to the destination and verify content equivalence
  3. `SIGNAL` (external partner or carrier) notify the customer of the read-only window
  4. `CAPABILITY` (integration system) freeze writes, drain in-flight work, and cut over
  5. `OBSERVE` (auditor) verify the destination is serving and the source is quiesced
  6. `DECISION` (IT or access administrator) past the declared point of no return, roll forward only; before it, roll back cleanly
  7. `END` (compliance officer) close with the source's data destroyed on schedule
- **Intents**:
  - `hcmnext.tenant.relocate_tenant/v1` (PROPOSED, creates)
- **Reads**:
  - `tenant`: placement, capacity, residency rules
  - `workflow`: in-flight instances to drain
- **Writes**:
  - `tenant`: new placement and the relocation record
- **Evidence required**:
  - Content equivalence verification before cutover.
  - In-flight drain evidence.
  - Source destruction record.
- **Failure and repair**:
  - In-flight work is not drained -> instances resume in the source after cutover and produce split-brain state; the drain is the control
  - Content equivalence fails on a subset -> the cutover is BLOCKED; migrating most of a tenant is not migrating a tenant
  - The source is left running after cutover -> a second live copy accumulating divergent state, which is the worst outcome; quiescing the source is verified, not assumed
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: Public-sector contracts with in-state residency terms make placement a contractual matter.
  - International: GDPR Chapter V applies if the destination cell is outside the origin's region; the relocation is itself a transfer requiring a mechanism.

### WF-TEN-004. Execute a tenant exit and hand back the data

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: compliance officer, IT or access administrator, external partner or carrier, auditor
- **Trigger**: A customer leaves and their data must be exported and then destroyed on schedule.
- **Preconditions**:
  - The exit plan states what is exported, in what format, and when destruction occurs.
  - The export is verifiable by the customer, not just produced.
  - Legal holds and retained obligations survive the exit.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) assemble the export package in the contracted format with its manifest
  2. `CAPABILITY` (auditor) verify the package: completeness, integrity and tenant isolation
  3. `SIGNAL` (external partner or carrier) deliver to the customer and obtain confirmation of receipt and readability
  4. `WAIT` (compliance officer) hold for the contracted retention period before destruction
  5. `DECISION` (compliance officer) suppress destruction of anything under a legal hold or a retained statutory obligation
  6. `CAPABILITY` (IT or access administrator) destroy and certify
- **Intents**:
  - `hcmnext.tenant.execute_exit/v1` (PROPOSED, creates)
- **Reads**:
  - `privacy`: holds and retained obligations
  - `tenant`: all tenant data, exit contract terms
- **Writes**:
  - `tenant`: export package, destruction certificate
- **Evidence required**:
  - Export manifest and integrity verification.
  - Customer confirmation of readability, not just of delivery.
  - Destruction certificate with any retained exceptions named.
- **Failure and repair**:
  - The customer cannot read the export -> the exit is not complete; a delivered but unusable export is a portability failure
  - A legal hold covers part of the data -> that part is retained past the exit with a named basis and the customer is told
  - Backups outlive the destruction date -> the certificate states the rotation window rather than claiming immediate destruction
- **Jurisdiction**:
  - US federal: None generally, though HIPAA business associate agreements require return or destruction of protected health information at termination.
  - US state variation: State breach notification duties continue for retained data after the relationship ends.
  - International: GDPR Article 28(3)(g) requires the processor to delete or return personal data at the end of the service, at the controller's choice.

### WF-TEN-005. Integrate an acquired company under a transitional arrangement

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, HR business partner, external partner or carrier, finance partner
  - performing a step: IT or access administrator, compliance officer, HR business partner, finance partner
  - participating without owning a step: external partner or carrier
- **Trigger**: A company is acquired and its people processes must keep running while integration waits on obligations that cannot be compressed.
- **Preconditions**:
  - Consultation, information and assessment obligations run on their own timetables and are not negotiable against an integration date.
  - The acquired company keeps operating in the interval, which means somebody has to run its payroll, its benefits and its access.
  - The transitional arrangement itself moves data between two controllers and needs its own basis.
- **Steps**:
  1. `CAPABILITY` (compliance officer) enumerate the obligations that gate the migration and their own timetables
  2. `DECISION` (HR business partner) decide what runs where during the interval, and record the arrangement rather than letting it emerge
  3. `APPROVAL` (compliance officer) compliance approves the transitional arrangement's data flows and their basis
  4. `WAIT` (compliance officer) hold the migration until the gating obligations discharge, with the wait itself visible and owned
  5. `CAPABILITY` (IT or access administrator) execute the migration from the completion of the gating obligations rather than from a date fixed before them
  6. `OBSERVE` (IT or access administrator) confirm the migrated population and close the transitional arrangement
  7. `END` (finance partner) close on the arrangement's termination; its own data flows stop and their records are retained
- **Intents**:
  - `hcmnext.tenant.establish_transitional_services/v1` (PROPOSED, creates)
  - `hcmnext.tenant.migrate_acquired_population/v1` (PROPOSED, advances)
- **Reads**:
  - `privacy`: the controllership during the interval and the arrangement's data flows
  - `regulatory`: the gating obligations and their timetables
  - `tenant`: the acquisition, the acquired estate and the migration plan
- **Writes**:
  - `privacy`: the assessed flows and the controllership record
  - `tenant`: the transitional arrangement and the scheduled migration
- **Evidence required**:
  - The enumerated gating obligations with their owners and timetables.
  - The transitional arrangement with its assessed data flows.
  - The migration scheduled from the obligations' completion rather than from a fixed date.
  - The arrangement's termination and its retained records.
- **Failure and repair**:
  - The gating obligations are compressed to meet an integration date -> a legal problem is created to solve an operational one and the obligation is the one that is enforceable
  - The transitional arrangement's flows are assumed covered by the acquisition -> data moves between two controllers with no assessed basis
  - A data subject exercises a right during the interval -> the request is routed to the controller that actually holds the data rather than to the acquirer by default
  - The arrangement runs on past its intended end -> the extension is a recorded decision with its own cost rather than an omission
- **Jurisdiction**:
  - US federal: No single federal regime; the obligations are assembled from the transaction structure, WARN where employment losses arise, and the successor rules for payroll and benefits continuity.
  - US state variation: State notice, successor liability and unemployment experience transfer rules each apply on their own terms and several require filings within days of closing.
  - International: In the EU the works council information and consultation duties gate integration in practice, and in France and Germany in particular they cannot be compressed to a commercial timetable; the transitional arrangement is itself a processing relationship requiring a basis.

---

## TRG. System and trigger-driven

4 workflows.

### WF-TRG-001. Fire a scheduled effective-date activation

- **Status**: `PARTIAL` - planning/data/models/intent-coverage-matrix.md rows 513 to 530 system and trigger-driven
- **Archetype**: `A17` | **Complexity tier**: 3
- **Actors**: integration system, HR business partner, auditor
- **Trigger**: An approved future-dated change reaches its effective date and must activate.
- **Preconditions**:
  - The activation is registered against the approved proposal, not against a calendar reminder.
  - The revalidation rule for the change type is declared.
  - The time zone the effective date is expressed in is explicit.
- **Steps**:
  1. `WAIT` (integration system) hold until the effective instant in the declared time zone
  2. `CHECKPOINT` (HR business partner) revalidate every mutable assumption: authority, eligibility, reservations, rule versions
  3. `DECISION` (HR business partner) route to REPLAN_AND_REAPPROVE when a material fact changed since approval
  4. `CAPABILITY` (integration system) commit the change and dispatch its effects
  5. `OBSERVE` (integration system) confirm downstream systems applied it from the correct date
  6. `END` (auditor) close per dimension; the local commit does not close external consistency
- **Intents**:
  - `hcmnext.system.activate_effective_date/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: authority and rule versions at the activation instant
  - `workflow`: the registered activation and its proposal
- **Writes**:
  - `people`: the activated change as an effective-dated fact
  - `workflow`: activation record
- **Evidence required**:
  - Revalidation result at the activation instant.
  - Any invalidating event that arrived during the wait.
  - Downstream observation of the applied date.
- **Failure and repair**:
  - The approver's authority lapsed during the wait -> the approval is invalidated and the change does not activate; the descriptor's stale-delegation refusal covers this
  - The activation fires during a system outage -> it is retried from the durable timer with the original effective date preserved; a late activation is not a changed effective date
  - The time zone is ambiguous -> REJECT at registration; an effective date without a zone can be a day wrong, which for pay is a real error
- **Jurisdiction**:
  - US federal: None directly; the underlying change carries its own obligations.
  - US state variation: Effective dates that must align to pay period boundaries vary by state pay frequency rules.
  - International: In jurisdictions with notice periods, an activation before the notice expires is unlawful regardless of the approval.

### WF-TRG-002. Fire a business deadline event and escalate

- **Status**: `NEW`
- **Archetype**: `A17` | **Complexity tier**: 2
- **Actors**: integration system, HR business partner, compliance officer
- **Trigger**: A statutory or policy deadline approaches or passes and the system must act.
- **Preconditions**:
  - The deadline is computed from a legal or policy rule, not entered by hand.
  - The escalation path and its recipients are configured.
  - Passing the deadline creates a recorded exposure, not just a reminder.
- **Steps**:
  1. `CAPABILITY` (integration system) compute the deadline from the triggering event and the applicable rule
  2. `WAIT` (integration system) hold with escalating notifications at the configured lead times
  3. `DECISION` (compliance officer) on the deadline passing, record the miss with its computed exposure rather than continuing to remind
  4. `SIGNAL` (HR business partner) escalate to the accountable owner and their manager
  5. `END` (compliance officer) close on satisfaction or on the miss being recorded as an obligation
- **Intents**:
  - `hcmnext.system.fire_business_deadline/v1` (PROPOSED, creates)
- **Reads**:
  - `regulatory`: the rule computing the deadline
  - `workflow`: the obligation the deadline attaches to
- **Writes**:
  - `workflow`: deadline events, escalations and miss records
- **Evidence required**:
  - Deadline computation with its rule version.
  - Escalation delivery evidence.
  - Miss record with the exposure computed.
- **Failure and repair**:
  - The deadline is missed and the reminder simply repeats -> REJECT that design; a reminder loop hides an accrued liability, and the miss must become a tracked obligation
  - The rule changes and the deadline moves -> the deadline recomputes and the affected parties are told which rule moved it
  - The obligation is satisfied but the system is not told -> the deadline fires falsely; satisfaction must be observable rather than self-reported
- **Jurisdiction**:
  - US federal: COBRA, FMLA, FCRA and I-9 all have deadlines whose miss carries per-day or per-instance penalties; recording the miss is what allows the exposure to be quantified and mitigated.
  - US state variation: State final-pay deadlines carry waiting-time penalties that accrue daily, which makes the miss record materially valuable.
  - International: GDPR's one-month access deadline and 72-hour breach deadline both start from events the system observes.

### WF-TRG-003. Process an inbound trigger from an external system safely

- **Status**: `PARTIAL` - planning/data/models/intent-coverage-matrix.md rows 513 to 530 TriggerInbox and TriggerPayload
- **Archetype**: `A17` | **Complexity tier**: 3
- **Actors**: integration system, IT or access administrator, compliance officer
  - performing a step: integration system, IT or access administrator
  - participating without owning a step: compliance officer
- **Trigger**: An external signal enters the trigger inbox and may create a governed intent.
- **Preconditions**:
  - The trigger definition declares which intent it may create and under whose authority.
  - The payload is untrusted and never becomes the intent's authoritative facts.
  - Delivery is at least once, so idempotency is mandatory.
- **Steps**:
  1. `CAPABILITY` (integration system) accept the payload into the inbox with its dedupe key
  2. `DECISION` (integration system) suppress duplicates on the dedupe key before any processing
  3. `CAPABILITY` (integration system) resolve the subject and read authoritative state; the payload only says where to look
  4. `CAPABILITY` (integration system) create the declared intent under the declared principal
  5. `END` (IT or access administrator) close the firing; the intent proceeds under its own governance
- **Intents**:
  - `hcmnext.system.process_trigger/v1` (PROPOSED, creates)
- **Reads**:
  - `security`: the declared principal's authority
  - `workflow`: trigger definition, subscription, inbox
- **Writes**:
  - `workflow`: inbox entry, firing record, created intent
- **Evidence required**:
  - Dedupe key and duplicate suppression.
  - The authoritative read the intent was actually based on.
  - The principal whose authority the intent used.
- **Failure and repair**:
  - The payload asserts a fact the platform does not hold -> the fact is ignored; a trigger is a notification and never a source of truth
  - The payload's subject is in another tenant -> REJECT and record; this is the shape of a cross-tenant probe
  - The trigger fires for a subject that no longer exists -> the firing closes with a recorded no-op rather than creating an intent against nothing
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-TRG-004. Handle a threshold event on a monitored value

- **Status**: `NEW`
- **Archetype**: `A17` | **Complexity tier**: 2
- **Actors**: integration system, HR business partner, finance partner
- **Trigger**: A monitored value crosses a configured threshold and an action or a notification follows.
- **Preconditions**:
  - The threshold, its direction and its hysteresis are configured.
  - Crossing is evaluated against a settled value, not a mid-computation one.
  - Repeated crossings do not produce repeated actions.
- **Steps**:
  1. `OBSERVE` (integration system) evaluate the monitored value at its settled watermark
  2. `DECISION` (integration system) apply hysteresis so a value oscillating around the threshold does not fire repeatedly
  3. `CAPABILITY` (HR business partner) fire the configured action or notification once per crossing
  4. `SIGNAL` (finance partner) notify the accountable owner with the value and the threshold
  5. `END` (HR business partner) close on the reverse crossing or on acknowledgement
- **Intents**:
  - `hcmnext.system.fire_threshold_event/v1` (PROPOSED, creates)
- **Reads**:
  - `analytics`: the monitored value and its watermark
  - `workflow`: threshold configuration and prior firings
- **Writes**:
  - `workflow`: threshold event records
- **Evidence required**:
  - The settled value and its watermark at the crossing.
  - Hysteresis application.
  - One firing per crossing, provably.
- **Failure and repair**:
  - The value is read mid computation -> the crossing is spurious; using the settled watermark is what prevents it
  - The value oscillates -> hysteresis suppresses the repeats and the oscillation itself becomes the finding
  - The threshold is changed while a crossing is active -> the active crossing keeps its threshold version and a new evaluation begins
- **Jurisdiction**:
  - US federal: None.
  - US state variation: None.
  - International: None.

---

## ESS. Employee self-service

6 workflows.

### WF-ESS-001. View and update your own profile

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 31 Employee Self-Service Portal feature profile_view_update
- **Archetype**: `A2` | **Complexity tier**: 1
- **Actors**: employee, integration system
- **Trigger**: A worker opens their profile and edits the fields they own.
- **Preconditions**:
  - The worker is authenticated at the assurance the field set requires.
  - Each field declares whether the worker owns it or merely sees it.
  - Fields the worker may see but not change are visibly read-only rather than absent.
- **Steps**:
  1. `CAPABILITY` (employee) resolve which fields the worker may read and which they may change
  2. `TASK` (employee) the worker edits an owned field and submits
  3. `RULE` (employee) validate the field against its domain rules, including normalization
  4. `CAPABILITY` (integration system) commit the revision with the worker as the actor
  5. `END` (employee) close immediately for fields with no approval requirement
- **Intents**:
  - `hcmnext.people.change_personal_details/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: the worker's own record
  - `privacy`: field ownership and classification
- **Writes**:
  - `people`: field revisions attributed to the worker
- **Evidence required**:
  - Request digest.
  - Ledger fact naming the worker as the actor.
  - The field ownership evaluation that permitted the change.
- **Failure and repair**:
  - The worker edits a field they do not own, such as their job title -> the field is read-only in the surface and DENIED at the capability; both layers refuse
  - A change triggers a downstream consequence the worker did not expect, such as an address change affecting tax -> the consequence is shown before submission rather than discovered on the next payslip
  - Validation fails -> the correctable error is returned with the rest of the input preserved
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: Address changes can move tax jurisdiction, so a self-service address edit is not always a low-risk field.
  - International: GDPR Article 16 rectification is best served by letting the worker correct their own data directly.

### WF-ESS-002. View a pay statement and understand a change

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 31 Employee Self-Service Portal feature pay_statement_view
- **Archetype**: `A6` | **Complexity tier**: 1
- **Actors**: employee, integration system
- **Trigger**: A worker opens their payslip and asks why net pay differs from last period.
- **Preconditions**:
  - The statement is immutable once issued.
  - The comparison is against a specific prior statement, not an average.
  - Every line traces to the component or deduction that produced it.
- **Steps**:
  1. `CAPABILITY` (employee) retrieve the statement and the chosen comparison statement
  2. `TRANSFORM` (integration system) compute the line-level delta and attribute each one to its cause
  3. `DECISION` (integration system) attribute a change to a specific intent where one exists, and to a rule change where it does not
  4. `END` (employee) complete with zero writes
- **Intents**:
  - `hcmnext.rewards.explain_compensation/v1` (PROPOSED, creates)
  - `hcmnext.intelligence.explain_transaction/v1` (REAL, reads)
- **Reads**:
  - `compensation`: component revisions in the interval
  - `payroll`: both statements and their lines
  - `regulatory`: rate changes in the interval
- **Writes**:
- **Evidence required**:
  - Both statement digests.
  - Per-line attribution to an intent or a rule version.
  - Zero-effect receipt.
- **Failure and repair**:
  - A delta cannot be attributed -> it is reported as unattributed rather than absorbed into a rounding line; an unexplained net change is the top payroll support ticket
  - The statement predates a data migration -> the answer says the history is truncated rather than presenting a partial comparison as complete
  - The worker asks about a deduction another party controls, such as a garnishment -> the line is shown because it is their own statement, with the detail the jurisdiction permits
- **Jurisdiction**:
  - US federal: FLSA and IRS rules make the statement's underlying computation reproducible a requirement; showing it to the worker is the natural consequence.
  - US state variation: California Labor Code 226 prescribes nine elements and a three-year retention with an inspection right; New York, Illinois and others have their own content lists.
  - International: EU payslip content is set nationally and often by collective agreement; the itemization requirement is generally stricter than the US federal baseline.

### WF-ESS-003. Submit a time-off request from self-service

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 31 Employee Self-Service Portal feature leave_request_self_submit
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: employee, manager, integration system
- **Trigger**: A worker requests vacation days and the manager approves.
- **Preconditions**:
  - The worker's balance and its accrual-to-date are computable at the requested dates.
  - The team's coverage rules, if any, are evaluated as advice rather than a hard block.
  - The request type maps to a leave programme, not to a free-text reason.
- **Steps**:
  1. `CAPABILITY` (employee) show the balance at the requested dates, including scheduled future absences
  2. `TASK` (employee) the worker selects dates and a request type
  3. `RULE` (employee) validate against balance, blackout periods and any notice requirement
  4. `APPROVAL` (manager) the manager approves or declines with a reason
  5. `CAPABILITY` (integration system) commit the absence and decrement the balance
  6. `SIGNAL` (employee) notify the worker and update the team calendar at the permitted granularity
- **Intents**:
  - `hcmnext.leave.request_leave/v1` (PROPOSED, creates)
- **Reads**:
  - `leave`: balance, accrual, prior absences, blackout periods
  - `time`: schedule for the requested dates
- **Writes**:
  - `leave`: absence record and balance decrement
- **Evidence required**:
  - Balance as shown at request time, since it may change before approval.
  - The manager's decision and reason.
  - The team calendar entry's granularity.
- **Failure and repair**:
  - The balance changes between request and approval -> the approval revalidates; approving into a negative balance must be a deliberate act, not an accident
  - The request is for a protected leave type submitted through the vacation path -> it is rerouted to the protected leave workflow, since the evidence and confidentiality rules differ
  - The manager does not respond before the requested dates -> the escalation path applies; silence is not a denial and the worker needs an answer
- **Jurisdiction**:
  - US federal: Vacation is not federally regulated, but a request that is really protected leave triggers FMLA notice duties from the moment the employer has enough information to know.
  - US state variation: State and local paid sick leave laws often forbid requiring advance notice for unforeseeable use and forbid attendance penalties, so the request path must distinguish sick from vacation.
  - International: In the EU, statutory annual leave cannot generally be refused indefinitely and unused leave may carry over where the worker was prevented from taking it.

### WF-ESS-004. Update a payment method and verify it

- **Status**: `NEW`
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: employee, payroll administrator, integration system
- **Trigger**: A worker changes their direct deposit details.
- **Preconditions**:
  - The domain never holds raw bank credentials; a tokenized reference is used.
  - A verification step precedes the first payment on the new method.
  - A change close to a payroll cutoff has a defined effective period.
- **Steps**:
  1. `TASK` (employee) the worker enters the new method through a tokenizing channel
  2. `CAPABILITY` (integration system) verify the method through the rail's verification mechanism
  3. `DECISION` (payroll administrator) apply the change from the next period whose cutoff has not passed, and tell the worker which one
  4. `SIGNAL` (employee) notify the worker's registered contact point that the method changed, as a fraud control
  5. `CAPABILITY` (integration system) commit the method reference with its effective period
- **Intents**:
  - `hcmnext.payroll.change_payment_method/v1` (PROPOSED, creates)
- **Reads**:
  - `payroll`: current method reference, period calendar
  - `people`: the worker's verified contact points
- **Writes**:
  - `payroll`: new method token and its effective period
- **Evidence required**:
  - Verification result from the rail.
  - Out-of-band notification of the change.
  - Effective period, so the worker knows which payslip is affected.
- **Failure and repair**:
  - The change is made by an attacker who has taken over the session -> the out-of-band notification is the detection control; payroll diversion fraud is common and the notification is not optional
  - Verification fails -> the old method stays active; an unverified method must never receive a payment
  - The change is made after cutoff -> the current period pays to the old method and the worker is told, rather than the payment failing
- **Jurisdiction**:
  - US federal: The FTC and FBI have both flagged payroll diversion as a common business email compromise pattern; the notification control is the recognized mitigation.
  - US state variation: Several states restrict mandatory direct deposit and require a paper option; the workflow must not remove the alternative.
  - International: In the EU, SEPA mandates and the payment services rules govern verification; strong customer authentication concepts inform the out-of-band notification.

### WF-ESS-005. Access your own records as a former employee

- **Status**: `NEW`
- **Archetype**: `A6` | **Complexity tier**: 3
- **Actors**: employee, HR business partner, compliance officer
- **Trigger**: A departed worker needs a pay statement, an employment letter or their personnel file.
- **Preconditions**:
  - Former-employee access has its own identity assurance, since the corporate identity is gone.
  - The retention schedule determines what still exists.
  - The statutory inspection right, where one exists, sets the deadline.
- **Steps**:
  1. `TASK` (employee) the former worker authenticates through a channel that does not depend on the revoked corporate identity
  2. `CAPABILITY` (HR business partner) resolve what remains under the retention schedule
  3. `DECISION` (compliance officer) state clearly when a record has been destroyed under schedule rather than implying it never existed
  4. `CAPABILITY` (HR business partner) produce the records, redacted where third-party data appears
  5. `END` (compliance officer) close within the statutory window where one applies
- **Intents**:
  - `hcmnext.people.export_worker_record/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: the former worker's retained records
  - `privacy`: retention state and third-party redaction rules
- **Writes**:
  - `privacy`: access request record and what was produced
- **Evidence required**:
  - Identity assurance for a non-employee requester.
  - The retention determination for anything not produced.
  - Redaction log.
- **Failure and repair**:
  - The requester cannot be authenticated -> the request is refused with a route to an identity-proofing channel; disclosing a personnel file to the wrong person is worse than a delay
  - The records were destroyed on schedule -> say so with the schedule cited; an ambiguous non-answer looks like concealment
  - The request arrives during litigation -> the legal hold applies and the request is routed through counsel rather than answered routinely
- **Jurisdiction**:
  - US federal: No general federal right, but a former employee remains entitled to a copy of a consumer report used in an adverse action under the FCRA.
  - US state variation: California Labor Code 1198.5 extends the personnel file inspection right to former employees for three years after separation; Illinois, Massachusetts, Connecticut and others have their own windows and some exclude former employees entirely.
  - International: GDPR Article 15 applies regardless of whether the employment ended, limited only by what is still retained.

### WF-ESS-006. Update an address and see its downstream consequences first

- **Status**: `NEW`
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: employee, payroll administrator, integration system
- **Trigger**: A worker changes their home address, which may move tax jurisdiction, benefits eligibility and pay rules.
- **Preconditions**:
  - Home address and work address are separate fields with separate consequences.
  - The consequences are computed and shown before the change is submitted.
  - A move that changes work jurisdiction routes to the work location workflow, which has approvals this one does not.
- **Steps**:
  1. `TASK` (employee) the worker enters the new address
  2. `CAPABILITY` (payroll administrator) normalize the address and resolve its jurisdictions
  3. `DECISION` (payroll administrator) if the work location also moves, route to the work location change workflow rather than committing here
  4. `CAPABILITY` (employee) show the consequences: withholding change, benefits network change, leave entitlement change
  5. `CAPABILITY` (integration system) commit the home address revision with its effective date
  6. `END` (payroll administrator) close with the tax profile change created as a bound child where the residence jurisdiction moved
- **Intents**:
  - `hcmnext.people.change_address/v1` (PROPOSED, creates)
- **Reads**:
  - `benefits`: network availability at the new address
  - `people`: current home and work addresses
  - `regulatory`: residence jurisdiction rules
- **Writes**:
  - `payroll`: residence tax profile change as a bound child
  - `people`: home address revision
- **Evidence required**:
  - Address normalization result and its confidence.
  - The consequences as shown to the worker before submission.
  - The bound tax profile child where the residence jurisdiction moved.
- **Failure and repair**:
  - The worker is actually relocating their work location too -> the change routes to the work location workflow, which requires registration checks and approvals a home address change does not
  - The address does not normalize -> the change is accepted with the raw address and a normalization finding, rather than being blocked; a worker with an unusual address must still be able to update it
  - The move is backdated -> a retroactive withholding correction becomes a bound child rather than being absorbed silently
- **Jurisdiction**:
  - US federal: Residence affects withholding in states with a residence tax and in reciprocity pairs; the employer must obtain a new state withholding certificate where the residence state changes.
  - US state variation: New York, New Jersey, Connecticut, Pennsylvania, Delaware and Nebraska apply convenience-of-the-employer rules that can tax a remote worker in the employer's state as well as the residence state; local jurisdictions in Pennsylvania and Ohio resolve by street address.
  - International: In the EU a residence move across a border changes social security under Regulation 883/2004 and may change the taxing state under the applicable treaty.

---

## MSS. Manager self-service

4 workflows.

### WF-MSS-001. See a team view scoped to what a manager may know

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 32 Manager Dashboard Actions features team_view and direct_report_manage
- **Archetype**: `A6` | **Complexity tier**: 2
- **Actors**: manager, employee, integration system
  - performing a step: manager, integration system
  - participating without owning a step: employee
- **Trigger**: A manager opens their team and sees each report's status.
- **Preconditions**:
  - The manager's scope is derived from the relationship graph, not from a static list.
  - Field-level authorization differs per report and per field.
  - Absence reasons are not disclosed even when absence dates are.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the manager's direct and indirect reports at the current instant
  2. `CAPABILITY` (integration system) apply field-level authorization per report
  3. `DECISION` (integration system) show absence dates without reasons, and show DENIED for fields the manager may not read
  4. `END` (manager) complete with zero writes and a per-field disclosure record
- **Intents**:
  - `hcmnext.people.explain_worker_state/v1` (REAL, reads)
- **Reads**:
  - `organization`: relationship graph and scope
  - `people`: reports, assignments, absence dates
  - `privacy`: field classification
- **Writes**:
- **Evidence required**:
  - Scope resolution showing which relationships granted the view.
  - Per-field disclosure result including the denied set.
  - Zero-effect receipt.
- **Failure and repair**:
  - A report is on protected leave -> the dates appear and the reason is DENIED; the manager needs to plan coverage without learning a medical fact
  - A report has a matrix relationship to another manager -> the dotted-line manager sees a narrower field set; a matrix edge is not a parent edge
  - A report is also the manager's subject in an open investigation -> the matter wall applies and the investigation is invisible in the team view
- **Jurisdiction**:
  - US federal: ADA and FMLA confidentiality both require that medical information not flow to the manager; the interface is where this is usually breached.
  - US state variation: State medical privacy statutes, including California's CMIA, add duties beyond the federal baseline.
  - International: GDPR data minimization means the manager view should carry the least data that supports the management task.

### WF-MSS-002. Act on a pending approval from the manager queue

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 32 Manager Dashboard Actions feature approval_pending_view
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: manager, HR business partner, integration system
  - performing a step: manager, integration system
  - participating without owning a step: HR business partner
- **Trigger**: A manager reviews and decides the approvals waiting on them.
- **Preconditions**:
  - Each item shows the exact proposal, not a summary that could differ from it.
  - The manager's authority for each item is revalidated at decision time.
  - Delegated items are visibly delegated.
- **Steps**:
  1. `CAPABILITY` (manager) resolve the manager's pending items including those delegated to them
  2. `CAPABILITY` (integration system) present each item's exact proposal content and its digest
  3. `TASK` (manager) the manager approves or rejects with a reason where the policy requires one
  4. `DECISION` (integration system) revalidate authority at decision time, not at queue time
  5. `CAPABILITY` (integration system) bind the decision to the digest and advance the waiting instance
- **Intents**:
  - `hcmnext.work.approve_proposal/v1` (REAL, creates)
  - `hcmnext.work.reject_proposal/v1` (REAL, creates)
- **Reads**:
  - `people`: the manager's authority and delegations
  - `work`: pending items, their proposals and digests
- **Writes**:
  - `work`: approval decisions with their authority snapshots
- **Evidence required**:
  - The digest shown to the approver, matching what they decided on.
  - Authority revalidation at decision time.
  - Delegation chain where the item came through a delegation.
- **Failure and repair**:
  - The proposal changed while sitting in the queue -> the item is refreshed and the manager sees the new version; approving a stale summary is the failure mode this prevents
  - The manager's authority lapsed -> the decision is refused with the reason; the queue is not authority
  - Bulk approval is used across many items -> permitted where the policy allows it, and recorded as a bulk action so its scrutiny level is visible
- **Jurisdiction**:
  - US federal: SOX approval controls require the approver to have seen what they approved; a summary-only queue undermines the control.
  - US state variation: None state-specific.
  - International: None.

### WF-MSS-003. Initiate a change for a direct report

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 32 Manager Dashboard Actions feature promotion_request_submit
- **Archetype**: `A2` | **Complexity tier**: 2
- **Actors**: manager, HR business partner
- **Trigger**: A manager starts a promotion, pay change or transfer for a report.
- **Preconditions**:
  - The manager may see the fields the change touches; otherwise they cannot propose it meaningfully.
  - The simulation runs before the proposal is submitted, not after.
  - The approval chain is shown before submission so the manager knows who will see it.
- **Steps**:
  1. `CAPABILITY` (manager) resolve which change types the manager may initiate for this report
  2. `CAPABILITY` (manager) simulate the change and show its effects, its cost and its approval chain
  3. `DECISION` (HR business partner) refuse to submit when the simulation reports a blocking finding
  4. `TASK` (manager) the manager submits the proposal
  5. `END` (HR business partner) hand off to the change's own workflow
- **Intents**:
  - `hcmnext.people.promote_worker/v1` (REAL, creates)
  - `hcmnext.rewards.simulate_compensation/v1` (REAL, reads)
- **Reads**:
  - `compensation`: band and budget, subject to the manager's authorization
  - `people`: the report's current state
  - `position`: target position availability
- **Writes**:
  - `work`: the submitted proposal
- **Evidence required**:
  - Simulation output the manager saw before submitting.
  - The approval chain as displayed.
  - The manager's authorization to initiate this change type.
- **Failure and repair**:
  - The manager cannot see the report's current pay -> they can still propose a change expressed as a delta, with the resulting absolute figure DENIED to them; whether the tenant allows this is a policy choice the system must support either way
  - The simulation shows a band breach -> submission is blocked pending the exception path, so the manager learns before the approvers do
  - The manager submits and then goes on leave -> the proposal is owned by the intent, not by the manager's session, and the approval chain continues
- **Jurisdiction**:
  - US federal: None directly; the underlying change carries its obligations.
  - US state variation: State pay transparency laws may require that an internal promotion's range be disclosed to the employee.
  - International: The EU Pay Transparency Directive gives workers a right to know the criteria for pay progression, which a promotion proposal implicitly exercises.

### WF-MSS-004. Plan team coverage around absences without seeing their reasons

- **Status**: `NEW`
- **Archetype**: `A6` | **Complexity tier**: 2
- **Actors**: manager, employee, HR business partner
- **Trigger**: A manager schedules work around known absences whose reasons they may not see.
- **Preconditions**:
  - Absence dates are visible to the manager; absence reasons are not, for protected types.
  - A protected absence is marked as such without disclosing which protection applies.
  - Coverage planning must not require the manager to ask the worker why.
- **Steps**:
  1. `CAPABILITY` (manager) resolve the team's absences over the planning horizon with dates only
  2. `DECISION` (HR business partner) return the reason as DENIED for protected absence types, uniformly, so the denial itself does not identify the protected ones
  3. `TASK` (manager) the manager plans coverage from dates and expected return
  4. `END` (employee) complete with zero writes and the disclosure record retained
- **Intents**:
  - `hcmnext.people.explain_worker_state/v1` (REAL, reads)
- **Reads**:
  - `leave`: absence intervals and expected return dates
  - `privacy`: absence reason classification
  - `time`: schedule and coverage requirements
- **Writes**:
- **Evidence required**:
  - Per-absence disclosure result, with reasons denied uniformly.
  - Zero-effect receipt.
  - The uniformity check: the denial pattern must not distinguish protected from unprotected absences.
- **Failure and repair**:
  - Reasons are denied only for protected absences and shown for others -> the denial itself identifies who is on protected leave; the denial must be uniform across the absence types the policy protects
  - The manager asks the worker directly why they are absent -> outside the system's control, but the surface must not create the need by presenting an obvious gap
  - An expected return date is unknown -> shown as unknown rather than estimated; a fabricated return date is worse for planning than an honest gap
- **Jurisdiction**:
  - US federal: The ADA and FMLA both require medical information to be kept from the manager; the interface is where this is usually breached.
  - US state variation: State medical privacy statutes, including California's Confidentiality of Medical Information Act, add duties beyond the federal baseline.
  - International: Under GDPR, health data is special category data and the manager has no lawful basis for the reason, only for the dates.

---

## HRO. HR operations and administration console

4 workflows.

### WF-HRO-001. Execute a bulk operation with per-record governance

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 33 HR Administration Console feature bulk_operation_execute
- **Archetype**: `A10` | **Complexity tier**: 4
- **Actors**: HR business partner, compliance officer, finance partner, integration system
- **Trigger**: An administrator applies the same change to many workers at once.
- **Preconditions**:
  - The population is defined by a rule and frozen as a snapshot.
  - Each record's change runs the same governance the individual path runs.
  - The aggregate approval does not replace per-record authority checks.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve and freeze the population snapshot
  2. `CAPABILITY` (HR business partner) simulate the change per record and aggregate the effect
  3. `DECISION` (compliance officer) exclude records whose individual governance would refuse, and report them rather than failing the batch
  4. `APPROVAL` (finance partner) the authority approves the aggregate effect and the excluded set
  5. `CAPABILITY` (integration system) commit as bound child intents, one per record
  6. `END` (HR business partner) close with per-record outcomes; the parent never overwrites a child's truth
- **Intents**:
  - `hcmnext.dataops.execute_bulk_operation/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: aggregate cost where the change has one
  - `governance`: per-record authority and rules
  - `people`: the population and its current state
- **Writes**:
  - `dataops`: batch record with per-record outcomes
  - `people`: per-record changes as child intents
- **Evidence required**:
  - Frozen population and its watermark.
  - Per-record governance evaluation.
  - Per-record outcome, not an aggregate success.
- **Failure and repair**:
  - A record's individual governance would refuse -> it is excluded and named; a bulk path that overrides individual governance is a backdoor
  - Some children fail after others commit -> no false rollback; the outcomes are reported per record
  - The population drifts between freeze and commit -> records that left are skipped with a reason and records that joined are not silently included
- **Jurisdiction**:
  - US federal: A bulk pay or classification change carries the same per-employee notice and approval duties as an individual one.
  - US state variation: State wage-change notice requirements are per employee, so a bulk change produces a bulk notice obligation.
  - International: Collective changes in the EU can trigger consultation thresholds that individual changes do not.

### WF-HRO-002. Review the audit trail for a subject or a period

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 33 HR Administration Console feature audit_trail_review
- **Archetype**: `A6` | **Complexity tier**: 2
- **Actors**: auditor, compliance officer, HR business partner, integration system
  - performing a step: auditor, compliance officer, integration system
  - participating without owning a step: HR business partner
- **Trigger**: An auditor reviews everything that happened to a subject or in a window.
- **Preconditions**:
  - The trail is the ledger, not an application log.
  - Access to the trail is itself recorded.
  - The trail's completeness for the window is provable.
- **Steps**:
  1. `CAPABILITY` (auditor) resolve the scope and read the ledger events for it
  2. `CAPABILITY` (auditor) verify chain integrity and epoch coverage for the window
  3. `DECISION` (compliance officer) apply field authorization to the trail output; the trail is not a bypass around disclosure rules
  4. `CAPABILITY` (integration system) record the auditor's own access to the trail
  5. `END` (auditor) complete with the integrity verification attached
- **Intents**:
  - `hcmnext.intelligence.explain_transaction/v1` (REAL, reads)
- **Reads**:
  - `ledger`: events, chain, epoch attestations
  - `privacy`: classification of the fields inside the events
- **Writes**:
  - `security`: the auditor's own access record
- **Evidence required**:
  - Chain and epoch coverage verification for the window.
  - Field authorization applied to the output.
  - The auditor's access, itself logged.
- **Failure and repair**:
  - The trail has a gap -> the gap is reported with its boundaries; a trail that silently omits is worse than one that admits
  - The auditor requests a subject they may not see -> WITHHELD, consistently with every other read path
  - Someone attempts to alter the trail -> the chain verification fails and the alteration is detectable, which is the point of the chain
- **Jurisdiction**:
  - US federal: Federal Rules of Evidence 902(13) allow self-authentication of records generated by a verified process, which the chain verification supports.
  - US state variation: State recordkeeping statutes set minimum periods the trail must span.
  - International: GDPR Article 5(2) accountability requires the controller to demonstrate compliance, which the trail is the primary means of doing.

### WF-HRO-003. Configure a tenant policy and publish it

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 33 HR Administration Console feature policy_configuration
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, auditor, integration system
- **Trigger**: An administrator changes a tenant policy such as an approval threshold or a leave rule.
- **Preconditions**:
  - Policies are versioned control artifacts with effective intervals.
  - The in-flight impact is computable before publication.
  - The publication authority is separate from the authoring one.
- **Steps**:
  1. `TASK` (HR business partner) author the policy change with its effective date
  2. `CAPABILITY` (compliance officer) compute which in-flight intents and future-dated changes the new version would affect
  3. `DECISION` (compliance officer) declare the pinning behaviour: pin existing, reevaluate, or require review
  4. `APPROVAL` (compliance officer) the publication authority approves
  5. `CAPABILITY` (integration system) publish with its effective instant
  6. `END` (auditor) close; downstream intents cite the version they evaluated against
- **Intents**:
  - `hcmnext.configuration.publish_policy/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: current policy versions and their consumers
  - `workflow`: in-flight instances referencing the policy
- **Writes**:
  - `governance`: new policy version and its activation
- **Evidence required**:
  - The in-flight impact analysis.
  - The pinning decision.
  - Publication record that downstream evaluations cite.
- **Failure and repair**:
  - A policy is edited in place -> REJECT; an intent that cited version 3 must still be explainable after version 4 exists
  - The effective date is in the past -> permitted only with an explicit retroactive flag and an impact analysis, since it changes how past decisions are judged
  - Two policies conflict at the same instant -> REJECT at publication with both named; conflicting policy is worse than a missing one
- **Jurisdiction**:
  - US federal: SOX change management applies where the policy affects financial controls.
  - US state variation: A policy that reduces an entitlement may require advance notice in some states; changing a vacation accrual policy retroactively is unlawful where accrued vacation is a wage.
  - International: In codetermined jurisdictions, works council agreement may be a precondition to publication rather than a courtesy.

### WF-HRO-004. Generate a compliance report for a filing deadline

- **Status**: `NEW`
- **Archetype**: `A11` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, auditor, integration system
- **Trigger**: A recurring statutory report must be produced from the population as of a prescribed date.
- **Preconditions**:
  - The report's population, its as-of date and its category definitions come from the statute, not from tenant preference.
  - The category mapping between internal fields and statutory categories is explicit.
  - The filed figures must be reproducible after the fact.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the population at the prescribed as-of date
  2. `TRANSFORM` (compliance officer) map internal fields to the statutory categories with the mapping version recorded
  3. `DECISION` (compliance officer) report an unmappable record explicitly rather than assigning it to a residual category
  4. `APPROVAL` (compliance officer) compliance approves the figures before filing
  5. `CAPABILITY` (integration system) produce the filing package and freeze it
  6. `END` (auditor) close with the package, its mapping version and its population snapshot retained
- **Intents**:
  - `hcmnext.governance.generate_compliance_report/v1` (PROPOSED, creates)
- **Reads**:
  - `compensation`: pay data where the report requires it
  - `people`: population at the prescribed as-of date
  - `regulatory`: report definition, category schema and deadline
- **Writes**:
  - `governance`: filing package, mapping version, population snapshot
- **Evidence required**:
  - The population snapshot at the statutory as-of date.
  - The category mapping version, so the figures are reproducible.
  - The unmappable set, named rather than absorbed.
- **Failure and repair**:
  - A worker's self-identified category has no statutory equivalent -> reported in the category the statute prescribes for that case and the mapping decision is recorded; silently reassigning is how a filing becomes indefensible
  - The as-of date is chosen for convenience rather than from the statute -> REJECT; the population is prescribed and a different snapshot produces a different filing
  - Figures change after filing -> an amended filing is a separate obligation; the filed package is immutable
- **Jurisdiction**:
  - US federal: EEO-1 Component 1 is filed by covered employers from a snapshot in a prescribed workforce period, and VETS-4212 has its own population and deadline; the category definitions are prescribed and not negotiable.
  - US state variation: California pay data reporting under Government Code 12999 uses establishment-level cuts and its own snapshot period; Illinois equal pay registration has a different cycle again, so one dataset serves neither without a documented mapping.
  - International: The EU Pay Transparency Directive introduces its own reporting with thresholds phased by employer size and its own metric definitions.

---

## UXP. Universal experience patterns

4 workflows.

### WF-UXP-001. Provide an accessible and assisted route for any governed action

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 34 Universal Experience Patterns feature accessibility_accommodate; planning/user-flows/README.md maximal configuration
- **Archetype**: `A13` | **Complexity tier**: 3
- **Actors**: employee, HR business partner, compliance officer
- **Trigger**: A worker cannot use the standard digital surface and needs an equivalent route.
- **Preconditions**:
  - Every governed action has a declared assisted or manual route.
  - An assisted action records the actual subject and the representative separately.
  - Acting on someone's behalf never silently becomes impersonation.
- **Steps**:
  1. `TASK` (employee) the worker requests assistance or an alternative format
  2. `CAPABILITY` (HR business partner) resolve the assisted route for the specific action
  3. `TASK` (HR business partner) the representative performs the action, with the subject, the representative and the authority all recorded
  4. `DECISION` (compliance officer) refuse an assisted action where the representative's authority is not evidenced
  5. `END` (compliance officer) close with the same evidence the self-service route would have produced, plus the representation record
- **Intents**:
  - `hcmnext.experience.perform_assisted_action/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: the authority evidencing the representation
  - `people`: the subject and the representative
- **Writes**:
  - `work`: the action, with subject and representative recorded separately
- **Evidence required**:
  - Representation authority evidence.
  - Subject and representative recorded as distinct actors.
  - Equivalent evidence to the self-service route.
- **Failure and repair**:
  - The representative's action is recorded as the subject's own -> REJECT; impersonation destroys attribution and is the specific failure the design forbids
  - No assisted route exists for an action -> the gap is a finding; an action with no accessible route is an accessibility failure, not an edge case
  - The subject later disputes the action -> the representation record is what resolves it
- **Jurisdiction**:
  - US federal: Title I of the ADA requires reasonable accommodation in the terms and conditions of employment, which includes access to HR systems; Section 508 applies to federal agencies and contractors.
  - US state variation: California's Unruh Act and several state statutes reach beyond the ADA; state web accessibility litigation is active.
  - International: The European Accessibility Act extends accessibility duties to services including employment portals from its 2025 application date.

### WF-UXP-002. Resume an interrupted action across devices

- **Status**: `NEW`
- **Archetype**: `A13` | **Complexity tier**: 2
- **Actors**: employee, manager, integration system
  - performing a step: employee, integration system
  - participating without owning a step: manager
- **Trigger**: A worker starts a submission, loses connectivity or switches device, and resumes.
- **Preconditions**:
  - Drafts are saved server-side with a version, not only in the browser.
  - The draft's staleness against the underlying facts is detectable.
  - The resume path does not silently resubmit.
- **Steps**:
  1. `CAPABILITY` (employee) save the draft server-side with its version and its underlying fact watermark
  2. `TASK` (employee) the worker resumes on another device
  3. `DECISION` (integration system) detect that an underlying fact changed and prompt rather than submitting against a stale basis
  4. `TASK` (employee) the worker reviews and submits
  5. `CAPABILITY` (integration system) submit once; a duplicate submit is suppressed by the idempotency key
- **Intents**:
  - `hcmnext.experience.resume_draft/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: the underlying facts the draft assumed
  - `work`: draft, its version and its fact watermark
- **Writes**:
  - `work`: draft revisions and the eventual submission
- **Evidence required**:
  - Draft version and its watermark.
  - Staleness detection result.
  - Idempotency key preventing a duplicate submission.
- **Failure and repair**:
  - Two devices edit the same draft -> the version conflict is surfaced and the worker chooses; last-write-wins loses work invisibly
  - The worker double-submits -> the idempotency key suppresses the second and the response is the first's result, not an error
  - The draft is resumed months later -> the staleness check refuses and the draft is restarted rather than submitted against facts that have all moved
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-UXP-003. Serve a governed surface in the worker's language and locale

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 34 feature multi_language_support
- **Archetype**: `A13` | **Complexity tier**: 2
- **Actors**: employee, HR business partner, compliance officer, integration system
- **Trigger**: A worker uses the platform in a language and locale that affect names, dates and money.
- **Preconditions**:
  - The locale is a worker attribute, not a browser guess.
  - Fallback is explicit and visible where a translation is missing.
  - Legally required content has a jurisdiction-specific translation, not a machine one.
- **Steps**:
  1. `CAPABILITY` (employee) resolve the worker's locale from their record with the browser only as a hint
  2. `CAPABILITY` (integration system) render with locale-correct name order, date format, number format and currency
  3. `DECISION` (compliance officer) for legally required content, refuse to render an unreviewed machine translation and fall back to the reviewed source with a notice
  4. `END` (HR business partner) close with the locale and any fallback recorded
- **Intents**:
  - `hcmnext.experience.render_localized_surface/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: translated content and its review state
  - `people`: locale preference and jurisdiction
- **Writes**:
- **Evidence required**:
  - Locale resolution source.
  - Fallback record where a translation was missing.
  - Review state of any legally required translation.
- **Failure and repair**:
  - A wage notice is machine translated -> REJECT; several jurisdictions require the notice in the worker's language and an unreviewed translation can void it
  - A name does not fit the assumed given-family structure -> the name is stored and rendered as the worker entered it; imposing a structure corrupts the record
  - A currency is rendered in the viewer's locale rather than the transaction's -> REJECT; pay is denominated in its own currency and reformatting it is a correctness bug, not a preference
- **Jurisdiction**:
  - US federal: None federally.
  - US state variation: California requires wage notices in the language the employer uses to communicate; New York requires dual-language notices where the Department of Labor publishes a template.
  - International: Several member states require employment documents in the local language for enforceability; a French contract in English can be unenforceable against the employee.

### WF-UXP-004. Present a governed refusal the user can act on

- **Status**: `NEW`
- **Archetype**: `A13` | **Complexity tier**: 3
- **Actors**: employee, manager, compliance officer, integration system
  - performing a step: employee, compliance officer, integration system
  - participating without owning a step: manager
- **Trigger**: An action is refused and the refusal must be understandable without disclosing what it protects.
- **Preconditions**:
  - Every refusal has a typed code, not a generic error.
  - The message discloses nothing the caller may not know.
  - Where an escalation or appeal exists, it is offered.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the refusal's typed code and its disclosure class
  2. `DECISION` (compliance officer) select the message variant the caller's authorization permits
  3. `SIGNAL` (employee) present the refusal with its escalation route where one exists
  4. `END` (compliance officer) close with the refusal recorded, including which variant was shown
- **Intents**:
  - `hcmnext.experience.present_refusal/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: the refusal code and its disclosure class
  - `people`: the caller's authorization
- **Writes**:
  - `governance`: refusal record with the variant shown
- **Evidence required**:
  - Typed refusal code.
  - The disclosure variant selected and why.
  - Escalation route offered, if any.
- **Failure and repair**:
  - Two different refusals produce distinguishable messages that let a caller infer a protected fact -> the variants are designed so that WITHHELD looks identical whether the subject exists or not
  - A refusal offers no next step -> acceptable only where no step exists; a dead end with no explanation is what drives users to work around the system
  - A refusal is logged without the variant shown -> the record is incomplete; a later dispute turns on what the user was actually told
- **Jurisdiction**:
  - US federal: None directly, though a refusal to disclose under a statutory access right must state the exemption relied on.
  - US state variation: State personnel-file statutes generally require a reason for a refusal to produce.
  - International: GDPR Article 12(4) requires the controller to inform the data subject of the reasons for not acting and of their right to complain.

---

## CON. Connectivity and integration hub

4 workflows.

### WF-CON-001. Monitor connector health and act before a failure lands

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 35 Connectivity Integration Hub feature connector_health_monitor
- **Archetype**: `A18` | **Complexity tier**: 3
- **Actors**: IT or access administrator, integration system, auditor
- **Trigger**: A connector's health signals degrade and dependent workflows must be protected.
- **Preconditions**:
  - Health is measured by successful governed operations, not only by reachability.
  - Dependent workflows are enumerable from the connector.
  - A degraded connector has a declared behaviour: queue, fail fast or route to manual.
- **Steps**:
  1. `OBSERVE` (integration system) collect health signals: latency, error rate, credential expiry, schema drift
  2. `DECISION` (IT or access administrator) classify the state as healthy, degraded or failed against the declared thresholds
  3. `SIGNAL` (IT or access administrator) notify the owners of every dependent workflow, naming what will happen to their work
  4. `CAPABILITY` (integration system) apply the declared degraded behaviour
  5. `END` (auditor) close on recovery, with the degraded interval recorded for the dependent workflows
- **Intents**:
  - `hcmnext.integration.monitor_connector_health/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: operation history, credential expiry, schema versions
  - `workflow`: dependent instances
- **Writes**:
  - `integration`: health state and its interval
  - `operations`: notifications to dependent owners
- **Evidence required**:
  - Health classification with the thresholds applied.
  - The dependent workflow list at the time of degradation.
  - The degraded interval, so later drift can be attributed to it.
- **Failure and repair**:
  - The connector is reachable but returns wrong data -> reachability is not health; schema drift detection is what catches this and it must be part of the signal set
  - A degraded connector silently queues indefinitely -> the queue depth is itself a threshold; unbounded queuing turns a connector outage into a data loss event
  - Recovery is declared on one successful call -> the recovery threshold requires sustained success, since a single success after an outage is common and misleading
- **Jurisdiction**:
  - US federal: None directly, though a degraded payroll or filing connector has statutory deadline consequences.
  - US state variation: None.
  - International: None.

### WF-CON-002. Handle a breaking schema change from an external partner

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 4
- **Actors**: IT or access administrator, external partner or carrier, compliance officer, integration system
  - performing a step: IT or access administrator, compliance officer, integration system
  - participating without owning a step: external partner or carrier
- **Trigger**: A partner changes their API or file format and the mapping breaks.
- **Preconditions**:
  - The current mapping version and its fixtures are retained.
  - The partner's change notice, if any, has a lead time.
  - The blast radius across dependent workflows is computable.
- **Steps**:
  1. `OBSERVE` (integration system) detect the change through validation failures or the partner's notice
  2. `TRANSFORM` (IT or access administrator) diff the old and new schemas and identify the affected fields
  3. `DECISION` (IT or access administrator) pause the affected operations rather than letting them fail record by record
  4. `TASK` (IT or access administrator) update the mapping and prove it on both old and new fixtures
  5. `APPROVAL` (compliance officer) compliance approves any change in the data classes crossing the boundary
  6. `CAPABILITY` (integration system) publish the new mapping version and resume, replaying the paused work
- **Intents**:
  - `hcmnext.integration.update_schema_mapping/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: mapping versions, fixtures, paused operations
  - `privacy`: data classes in the changed fields
- **Writes**:
  - `integration`: new mapping version and the replayed operations
- **Evidence required**:
  - Schema diff and the affected field list.
  - Fixture results for both schema versions.
  - The paused work and its replay outcome.
- **Failure and repair**:
  - The partner changes without notice -> the validation failure is the detection; the pause prevents a partial migration where some records use each format
  - The new schema drops a field the platform depends on -> the dependency becomes a gap that must be filled another way, and the affected workflows are told rather than silently producing nulls
  - The new schema adds a field carrying a new data class -> the privacy approval gates it; a connector quietly starting to receive health data is a real incident
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: A new data class crossing a border re-opens the transfer assessment under GDPR Chapter V.

### WF-CON-003. Diagnose a failing integration end to end

- **Status**: `EXISTING` - definitions/governance/feature-intent-intake.yaml group 35 feature integration_troubleshoot; internal/connectivity/diagnostics package
- **Archetype**: `A18` | **Complexity tier**: 3
- **Actors**: IT or access administrator, external partner or carrier, HR business partner, integration system
  - performing a step: IT or access administrator, external partner or carrier, integration system
  - participating without owning a step: HR business partner
- **Trigger**: An integration is failing and the cause must be isolated between credential, permission, schema and target.
- **Preconditions**:
  - Each diagnostic check returns a typed finding, not a free-text message.
  - An unknown result is distinguishable from a failure.
  - The diagnostic never mutates the target.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) run the ordered checks: reachability, authentication, permission, schema, sample operation
  2. `DECISION` (IT or access administrator) stop at the first decisive failure rather than reporting every downstream symptom
  3. `CAPABILITY` (integration system) return typed findings such as AUTHENTICATION_FAILED or PERMISSION_DENIED with an unknown where a check could not run
  4. `SIGNAL` (external partner or carrier) hand the finding to the accountable party, which is often the partner rather than the platform
  5. `END` (IT or access administrator) complete with zero mutations to the target
- **Intents**:
  - `hcmnext.integration.diagnose_connector/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: connection, credential state, schema version, recent operations
- **Writes**:
- **Evidence required**:
  - Ordered check results with their typed codes.
  - Explicit unknowns where a check could not run.
  - Zero-mutation evidence.
- **Failure and repair**:
  - The credential is valid but the permission is insufficient -> PERMISSION_DENIED is distinct from AUTHENTICATION_FAILED, and conflating them sends the ticket to the wrong team
  - A check cannot run because an earlier one failed -> the later check reports unknown rather than failed; an unknown is not evidence of a problem
  - The diagnostic writes a test record -> REJECT that design; a diagnostic that mutates the target creates the data it was meant to explain
- **Jurisdiction**:
  - US federal: None.
  - US state variation: None.
  - International: None.

### WF-CON-004. Publish and manage an outbound webhook subscription

- **Status**: `NEW`
- **Archetype**: `A14` | **Complexity tier**: 3
- **Actors**: IT or access administrator, external partner or carrier, compliance officer, integration system
- **Trigger**: A customer subscribes to platform events delivered to their endpoint.
- **Preconditions**:
  - The subscription declares its event types, its endpoint and its data classes.
  - Delivery is at least once with signing and a retry policy.
  - The endpoint's ownership is verified before any data flows.
- **Steps**:
  1. `TASK` (external partner or carrier) the customer registers the endpoint and the event types
  2. `CAPABILITY` (IT or access administrator) verify endpoint ownership through a challenge before enabling delivery
  3. `APPROVAL` (compliance officer) compliance approves the data classes leaving the platform to that endpoint
  4. `CAPABILITY` (integration system) deliver signed events with retries and a dead-letter path
  5. `DECISION` (IT or access administrator) disable a subscription whose endpoint fails persistently rather than retrying indefinitely
  6. `END` (external partner or carrier) close with the dead-lettered events retrievable
- **Intents**:
  - `hcmnext.integration.manage_webhook_subscription/v1` (PROPOSED, creates)
- **Reads**:
  - `integration`: subscription, endpoint, delivery history
  - `privacy`: data classes in the event payloads
- **Writes**:
  - `integration`: subscription record, delivery attempts, dead letters
- **Evidence required**:
  - Endpoint ownership verification.
  - Data class approval for the egress.
  - Delivery attempts including the dead-lettered set.
- **Failure and repair**:
  - The endpoint is taken over by a third party -> the ownership challenge is repeated periodically; a one-time verification ages badly
  - The endpoint fails for days -> the subscription disables and the events dead-letter; indefinite retry against a dead endpoint is an amplification risk
  - An event carries a data class the approval did not cover -> the event is suppressed and the gap is a finding; payload drift is how connectors start leaking
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: State breach notification duties can attach if events carrying personal information reach an unintended endpoint.
  - International: Every outbound event to a foreign endpoint is a transfer requiring a mechanism under GDPR Chapter V.

---

## DQG. Data quality and lineage governance

3 workflows.

### WF-DQG-001. Define and evaluate a data quality invariant

- **Status**: `PARTIAL` - planning/specs/data-quality-and-invariant-evaluation.md; definitions/governance/feature-intent-intake.yaml group 36 DataOps Quality Governance feature data_quality_rule_define
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, auditor, integration system
- **Trigger**: A rule states something that must always be true and the system evaluates it continuously.
- **Preconditions**:
  - The invariant is expressed over domain facts, not over storage.
  - Its severity states whether a violation blocks writes or raises a finding.
  - The evaluation is reproducible at a pinned watermark.
- **Steps**:
  1. `TASK` (HR business partner) define the invariant, its scope, its severity and its owner
  2. `APPROVAL` (compliance officer) the data authority approves a blocking invariant, since it will refuse writes
  3. `CAPABILITY` (integration system) evaluate the invariant across the scope at a pinned watermark
  4. `DECISION` (compliance officer) route blocking violations to refusal at the write path and advisory ones to a finding queue
  5. `END` (auditor) close each evaluation as an immutable result with its watermark
- **Intents**:
  - `hcmnext.dataops.define_quality_rule/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: invariant definitions and prior evaluations
  - `people`: the facts in scope
- **Writes**:
  - `dataops`: invariant version and evaluation results
- **Evidence required**:
  - Invariant version and the watermark evaluated against.
  - Violation list with the specific records.
  - Approval for any blocking severity.
- **Failure and repair**:
  - A newly published blocking invariant is violated by existing data -> the invariant blocks new violations but does not retroactively refuse reads; the existing violations become a remediation backlog
  - An invariant contradicts another -> REJECT at publication with both named
  - An invariant is expressed over a projection rather than the authoritative facts -> REJECT; a projection can be rebuilt and an invariant over it proves nothing
- **Jurisdiction**:
  - US federal: Record accuracy duties under FLSA and IRS rules are what most invariants encode.
  - US state variation: State recordkeeping rules impose their own accuracy requirements, particularly on wage statements.
  - International: GDPR Article 5(1)(d) accuracy is a principle the invariant framework operationalizes.

### WF-DQG-002. Trace the lineage of a derived figure

- **Status**: `PARTIAL` - planning/specs/provenance-graph-and-lineage.md; definitions/governance/feature-intent-intake.yaml group 36 DataOps Quality Governance feature data_lineage_trace
- **Archetype**: `A6` | **Complexity tier**: 3
- **Actors**: auditor, HR business partner, finance partner, integration system
  - performing a step: auditor, finance partner, integration system
  - participating without owning a step: HR business partner
- **Trigger**: Someone asks which sources and rules produced a reported figure.
- **Preconditions**:
  - Every derived value records the inputs and the rule version that produced it.
  - The lineage graph is queryable backwards from the output.
  - Authorization applies along the lineage, not just at the endpoint.
- **Steps**:
  1. `CAPABILITY` (auditor) resolve the output node and walk its lineage backwards
  2. `DECISION` (integration system) apply authorization at each node; an unauthorized input is DENIED by name rather than omitted from the graph
  3. `CAPABILITY` (finance partner) return the lineage with rule versions and source watermarks
  4. `END` (auditor) complete with zero writes
- **Intents**:
  - `hcmnext.intelligence.explain_transaction/v1` (REAL, reads)
- **Reads**:
  - `analytics`: the derived figure and its computation
  - `privacy`: classification of the inputs
  - `provenance`: lineage nodes and edges
- **Writes**:
- **Evidence required**:
  - Lineage graph with rule versions per edge.
  - Denied nodes named rather than pruned.
  - Source watermarks.
- **Failure and repair**:
  - A node's rule version has been retired -> the lineage cites the retired version; re-evaluating against the current rule would give a different figure and a false explanation
  - The lineage crosses into a tenant the caller may not see -> the edge is DENIED and the graph is reported as incomplete, which is honest
  - A node has no recorded inputs -> the gap is shown; a derived figure with no lineage is an untrustworthy figure and hiding that helps nobody
- **Jurisdiction**:
  - US federal: Sarbanes-Oxley and audit standards both require that reported figures be traceable to their sources.
  - US state variation: State pay-data reporting requires the filer to be able to explain its numbers on request.
  - International: GDPR Article 15 requires the source of personal data, which lineage provides.

### WF-DQG-003. Remediate a data quality finding at scale

- **Status**: `NEW`
- **Archetype**: `A10` | **Complexity tier**: 3
- **Actors**: HR business partner, IT or access administrator, compliance officer, integration system, auditor
- **Trigger**: A quality evaluation produced many violations that must be corrected.
- **Preconditions**:
  - The violations share a cause that can be addressed once.
  - Each correction runs the owning domain's governance.
  - The remediation is measurable against the invariant that produced it.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) group the findings by cause rather than by record
  2. `TASK` (HR business partner) the domain owner chooses a correction per cause
  3. `CAPABILITY` (HR business partner) simulate the corrections and report the aggregate effect
  4. `APPROVAL` (compliance officer) the data authority approves per cause, not per record
  5. `CAPABILITY` (integration system) execute as child intents through the owning domain
  6. `OBSERVE` (auditor) re-evaluate the invariant and report the residual
- **Intents**:
  - `hcmnext.dataops.remediate_findings/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: findings, their causes and prior remediations
  - `people`: the records in violation
- **Writes**:
  - `dataops`: remediation batch and its residual
  - `people`: corrections through the owning domain
- **Evidence required**:
  - Cause grouping, since a per-record approach hides the systemic problem.
  - Simulation of the aggregate effect.
  - Post-remediation re-evaluation with the residual named.
- **Failure and repair**:
  - A correction is applied directly to storage rather than through the domain -> REJECT; a direct write bypasses every rule and produces the next generation of findings
  - The residual does not shrink -> the cause diagnosis was wrong and the remediation is stopped rather than repeated
  - A correction changes a figure on a filed return -> the amended filing obligation opens, exactly as an individual correction would
- **Jurisdiction**:
  - US federal: None directly beyond the underlying records' own duties.
  - US state variation: Correcting historical wage data may require amended wage statements in states that regulate their content.
  - International: GDPR Article 19 requires notifying recipients of rectifications unless disproportionate.

---

## CFG. Configuration, rules and policies

4 workflows.

### WF-CFG-001. Author, test and publish a business rule

- **Status**: `EXISTING` - internal/domains/rules via planning/data/models/rules-and-decisions.md; definitions/governance/feature-intent-intake.yaml group 37 Configuration Rules Policies feature rule_define_test
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, auditor, integration system
- **Trigger**: A decision rule is authored, tested against fixtures, and published for use by workflows.
- **Preconditions**:
  - The rule is deterministic and its inputs are typed.
  - A fixture set covers the boundary cases before publication.
  - The rule's version is what decisions will cite, not its name.
- **Steps**:
  1. `TASK` (HR business partner) author the rule with its typed inputs and its decision table
  2. `CAPABILITY` (integration system) evaluate the rule against the fixture set and report coverage
  3. `DECISION` (compliance officer) refuse publication where a fixture is unaccounted for or two rows overlap
  4. `APPROVAL` (compliance officer) the rule owner approves the version
  5. `CAPABILITY` (integration system) publish with an effective interval
  6. `END` (auditor) close; decisions cite the version they evaluated
- **Intents**:
  - `hcmnext.configuration.publish_rule/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: fixture sets
  - `governance`: existing rule versions and their consumers
- **Writes**:
  - `governance`: rule version and its publication
- **Evidence required**:
  - Fixture coverage report.
  - Overlap and completeness checks on the decision table.
  - Publication version cited by every later decision.
- **Failure and repair**:
  - Two decision rows overlap -> REJECT; an ambiguous rule produces non-reproducible decisions
  - An input is untyped or free-text -> REJECT; a rule taking a free-text input cannot be tested
  - A published rule is edited -> REJECT; a new version is required, since decisions already cite the old one
- **Jurisdiction**:
  - US federal: A rule encoding a legal test must be traceable to its source; an unversioned rule cannot support a compliance defence.
  - US state variation: State-specific rules must be scoped by jurisdiction, since a single rule applied nationally will be wrong somewhere.
  - International: In codetermined jurisdictions, rules governing working conditions may require works council agreement.

### WF-CFG-002. Roll back a configuration change safely

- **Status**: `NEW`
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
- **Trigger**: A published configuration is causing harm and must be reverted.
- **Preconditions**:
  - The prior version is retrievable and still valid.
  - Decisions made under the bad version are identifiable.
  - A rollback is a new publication, not an undo.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) identify every decision made under the version being rolled back
  2. `DECISION` (compliance officer) decide whether those decisions are reversed, reviewed or left standing; a rollback does not answer this by itself
  3. `APPROVAL` (compliance officer) the publication authority approves the rollback and the decision disposition
  4. `CAPABILITY` (integration system) publish the prior version as a new version with a new effective instant
  5. `OBSERVE` (auditor) confirm the environment reports the reverted version active
  6. `END` (compliance officer) close with the affected decisions' disposition recorded
- **Intents**:
  - `hcmnext.configuration.rollback_configuration/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: version history and the affected decisions
  - `workflow`: in-flight instances pinned to the bad version
- **Writes**:
  - `governance`: the rollback publication
  - `work`: dispositions for the affected decisions
- **Evidence required**:
  - The affected decision set.
  - The disposition decision for those decisions.
  - The rollback publication, recorded as a forward-moving version.
- **Failure and repair**:
  - The rollback is treated as erasing the bad version -> REJECT; the version and its decisions remain in history, and pretending otherwise breaks every citation
  - Instances are pinned to the bad version -> they keep their pin unless migrated deliberately; a rollback does not retroactively repin running work
  - The prior version is itself invalid under a rule that has since changed -> the rollback is BLOCKED and a corrected forward version is required instead
- **Jurisdiction**:
  - US federal: SOX change management treats a rollback as a change requiring the same controls as the original.
  - US state variation: None state-specific.
  - International: None.

### WF-CFG-003. Manage a decision table with jurisdiction scoping

- **Status**: `PARTIAL` - planning/specs/legal-rule-packs-and-state-configuration.md; definitions/governance/feature-intent-intake.yaml group 37 feature decision_table_manage
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: compliance officer, HR business partner, auditor, integration system
- **Trigger**: A decision table encodes jurisdiction-specific outcomes and must stay complete as jurisdictions are added.
- **Preconditions**:
  - Every row declares the jurisdictions it applies to.
  - An unmatched jurisdiction produces an explicit unknown, not a default.
  - Adding a jurisdiction to the tenant surfaces the tables that do not cover it.
- **Steps**:
  1. `CAPABILITY` (compliance officer) evaluate table coverage against the tenant's active jurisdictions
  2. `DECISION` (compliance officer) report every jurisdiction with no matching row as a coverage gap rather than letting a default absorb it
  3. `TASK` (HR business partner) author the missing rows with their citations
  4. `APPROVAL` (compliance officer) compliance approves the new rows
  5. `CAPABILITY` (integration system) publish the new table version
  6. `END` (auditor) close with the coverage report attached
- **Intents**:
  - `hcmnext.configuration.manage_decision_table/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: table versions, rows, jurisdiction coverage
  - `tenant`: active jurisdictions from worker locations
- **Writes**:
  - `governance`: new table version and its coverage report
- **Evidence required**:
  - Coverage report against active jurisdictions.
  - Citations behind each new row.
  - Approval for the version.
- **Failure and repair**:
  - A default row silently covers a jurisdiction with different law -> REJECT the default for legally scoped tables; a default is how a California worker gets Texas treatment
  - A worker moves to a jurisdiction the table does not cover -> the evaluation returns unknown and blocks the dependent decision rather than guessing
  - Two rows match one jurisdiction -> REJECT at publication; ambiguity in a legal rule is not resolvable at runtime
- **Jurisdiction**:
  - US federal: A rule encoding a federal standard applies everywhere, but almost every HR rule has a stricter state or local variant somewhere.
  - US state variation: Minimum wage, overtime, leave, final pay, notice and deduction rules all vary; a table with a national default is wrong in a predictable set of states.
  - International: EU member state implementations of the same directive differ enough that a European default is equally unsafe.

### WF-CFG-004. Validate a configuration change against live data

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: compliance officer, HR business partner, auditor
- **Trigger**: A proposed configuration change is tested against the actual population before publication.
- **Preconditions**:
  - The validation runs against production data in a read-only mode.
  - The comparison is between the current and proposed rule outcomes per subject.
  - Subjects whose outcome changes are enumerable.
- **Steps**:
  1. `CAPABILITY` (compliance officer) evaluate both the current and the proposed configuration over the population
  2. `TRANSFORM` (compliance officer) produce the set of subjects whose outcome changes, with the direction of change
  3. `DECISION` (HR business partner) flag any change that reduces an entitlement, since those carry notice duties the author may not have considered
  4. `CAPABILITY` (auditor) return the impact report with zero writes
  5. `END` (compliance officer) complete; publication remains a separate approved act
- **Intents**:
  - `hcmnext.configuration.validate_configuration/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: current and proposed configuration
  - `people`: the population the configuration governs
- **Writes**:
- **Evidence required**:
  - Both configuration versions evaluated.
  - The changed-outcome set with directions.
  - Zero-effect receipt.
- **Failure and repair**:
  - The proposed change affects nobody -> worth knowing; a configuration change with no effect is usually a mistake about scope
  - The change reduces entitlements for a subset -> the notice obligation is surfaced before publication rather than discovered afterwards
  - The population is too large to evaluate exhaustively -> a sampled evaluation is permitted but must be labelled as sampled with its confidence
- **Jurisdiction**:
  - US federal: None directly; the affected entitlements carry their own duties.
  - US state variation: Reducing an accrual or an entitlement can require advance notice or be prohibited retroactively in several states.
  - International: In codetermined jurisdictions a change to working conditions may require agreement before publication.

---

## POP. Population segmentation and batch

4 workflows.

### WF-POP-001. Define a population by rule and snapshot it

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 38 Population Segmentation Batch features population_define_query and population_snapshot_create
- **Archetype**: `A9` | **Complexity tier**: 2
- **Actors**: HR business partner, compliance officer, finance partner, integration system
- **Trigger**: A cohort is defined by a governed expression and frozen for a downstream action.
- **Preconditions**:
  - The expression is evaluated within the caller's authorization scope.
  - The snapshot is immutable and carries its watermark.
  - The snapshot's size is visible before it is used.
- **Steps**:
  1. `CAPABILITY` (HR business partner) evaluate the expression within the caller's scope
  2. `DECISION` (compliance officer) report the count of subjects excluded by authorization, without naming them
  3. `CAPABILITY` (integration system) freeze the snapshot with its watermark and expression version
  4. `END` (finance partner) close; consuming the snapshot is a separate intent
- **Intents**:
  - `hcmnext.dataops.define_population/v1` (PROPOSED, creates)
- **Reads**:
  - `organization`: scope boundaries
  - `people`: subjects matching the expression
  - `privacy`: authorization limiting the resolvable set
- **Writes**:
  - `dataops`: population snapshot with its watermark and expression version
- **Evidence required**:
  - Expression version and its watermark.
  - Authorization exclusion count.
  - Immutable membership list.
- **Failure and repair**:
  - The expression resolves differently for two callers -> correct and expected; each snapshot records whose authorization produced it
  - The snapshot is used weeks later -> the staleness is visible from the watermark and the consumer decides; the snapshot does not silently refresh
  - An excluded subject would have changed the aggregate materially -> the exclusion count is what tells the caller their view is partial
- **Jurisdiction**:
  - US federal: A population used to select people for a reduction becomes evidence; the expression is the selection criterion.
  - US state variation: State pay-data reporting populations are defined by statute, so a tenant-defined population cannot substitute.
  - International: EU collective redundancy thresholds are counted per establishment, which constrains how a population may be defined for that purpose.

### WF-POP-002. Monitor a batch execution and handle its errors

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 38 Population Segmentation Batch features batch_execution_monitor and batch_error_handle
- **Archetype**: `A10` | **Complexity tier**: 3
- **Actors**: IT or access administrator, HR business partner, auditor, integration system
- **Trigger**: A long-running batch is observed and its failures are triaged without stopping the whole run.
- **Preconditions**:
  - Per-item outcomes are recorded as the batch runs, not only at the end.
  - The failure classes are distinguishable.
  - A stop condition exists based on error rate.
- **Steps**:
  1. `OBSERVE` (integration system) track per-item outcomes as the batch progresses
  2. `DECISION` (IT or access administrator) stop the batch when the error rate crosses the threshold, rather than completing with a mostly-failed result
  3. `TRANSFORM` (HR business partner) group failures by class for triage
  4. `TASK` (HR business partner) decide per class: retry, correct and retry, or abandon with a reason
  5. `END` (auditor) close with per-item outcomes and the abandoned set named
- **Intents**:
  - `hcmnext.dataops.monitor_batch/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: batch, items, outcomes, error classes
- **Writes**:
  - `dataops`: per-item outcomes and class dispositions
- **Evidence required**:
  - Per-item outcome as it occurred, not reconstructed after.
  - Error class grouping.
  - The abandoned set with its reason.
- **Failure and repair**:
  - The batch completes with a high failure rate and reports success -> REJECT that reporting; the stop condition exists to prevent it and the aggregate must never mask the per-item truth
  - A retry succeeds for some and fails for others -> the outcomes are per item, not per retry attempt
  - The batch is cancelled mid run -> committed items stay committed; cancellation stops future work rather than reversing past work
- **Jurisdiction**:
  - US federal: None directly beyond the underlying changes' duties.
  - US state variation: None.
  - International: None.

### WF-POP-003. Define a batch action bound to a population

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 38 feature batch_action_define
- **Archetype**: `A10` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, integration system
- **Trigger**: An action is defined once and bound to a population snapshot for execution.
- **Preconditions**:
  - The action type declares which per-record governance applies.
  - The binding is to a snapshot, not to a live query.
  - The action's parameters are the same for every record or are per-record from the snapshot.
- **Steps**:
  1. `CAPABILITY` (HR business partner) bind the action type to the population snapshot
  2. `CAPABILITY` (HR business partner) resolve per-record parameters from the snapshot where the action varies
  3. `DECISION` (compliance officer) refuse an action bound to a live query rather than a snapshot
  4. `CAPABILITY` (integration system) produce the executable batch with its per-record child specifications
  5. `END` (HR business partner) close the definition; execution is a separate approved intent
- **Intents**:
  - `hcmnext.dataops.define_batch_action/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: population snapshot, action type definitions
  - `governance`: per-record governance for the action type
- **Writes**:
  - `dataops`: batch definition with per-record child specifications
- **Evidence required**:
  - Snapshot binding, not a query binding.
  - Per-record parameters as resolved.
  - The governance the action type requires per record.
- **Failure and repair**:
  - The action is bound to a query that would resolve differently at execution -> REJECT; the approver approved a population, not a predicate
  - A per-record parameter is missing for some records -> those records are excluded and named rather than defaulted
  - The action type has no declared per-record governance -> REJECT; a batch action with no governance definition is an unbounded write
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-POP-004. Sample a population for an audit or a quality check

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 2
- **Actors**: auditor, compliance officer, HR business partner
- **Trigger**: A representative sample is drawn for testing rather than examining the whole population.
- **Preconditions**:
  - The sampling method and its seed are recorded so the sample is reproducible.
  - The sample size follows a stated method rather than convenience.
  - The sample is drawn from a frozen population, not a live one.
- **Steps**:
  1. `CAPABILITY` (auditor) freeze the population and record its size
  2. `TRANSFORM` (auditor) draw the sample using the declared method and a recorded seed
  3. `DECISION` (compliance officer) refuse a sample drawn from a live population; reproducibility is the point
  4. `TASK` (HR business partner) test each sampled item and record the result
  5. `TRANSFORM` (auditor) project the findings with their confidence, not as a point estimate
- **Intents**:
  - `hcmnext.analytics.draw_audit_sample/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: the frozen population
  - `governance`: the sampling method configuration
- **Writes**:
  - `analytics`: sample, test results and the projection
- **Evidence required**:
  - Seed and method, so the sample is reproducible.
  - Population size at the freeze.
  - Projection with its confidence interval.
- **Failure and repair**:
  - The sample is redrawn after unfavourable results -> the seed record makes it visible; redrawing until the answer is acceptable is the failure this prevents
  - The projection is presented as a point estimate -> REJECT; a sample-based finding without an interval overstates what was tested
  - An item cannot be tested -> it is recorded as untestable and the sample size adjusts, rather than being quietly replaced
- **Jurisdiction**:
  - US federal: Audit sampling standards govern how a control test's sample supports a conclusion.
  - US state variation: None state-specific.
  - International: None.

---

## SRC. Search and discovery

4 workflows.

### WF-SRC-001. Search for workers without leaking existence

- **Status**: `EXISTING` - internal/data/search package: DENIED before any statement runs, WITHHELD never appears in results; definitions/governance/feature-intent-intake.yaml group 39 Search Discovery Intelligence
- **Archetype**: `A6` | **Complexity tier**: 3
- **Actors**: HR business partner, recruiter, manager, compliance officer, integration system
  - performing a step: compliance officer, integration system
  - participating without owning a step: HR business partner, recruiter, manager
- **Trigger**: A user searches the worker directory and results must respect disclosure boundaries.
- **Preconditions**:
  - The caller's search scope is a first-class grant, not derived from a general read.
  - A subject the caller may not know exists never appears, including in a count.
  - Ranking does not leak information the caller cannot read.
- **Steps**:
  1. `CAPABILITY` (integration system) check the caller's search scope before any query runs
  2. `DECISION` (integration system) return DENIED where the caller has no search scope at all, before touching the index
  3. `CAPABILITY` (integration system) execute the search with the disclosure filter applied inside the query, not after
  4. `DECISION` (integration system) exclude WITHHELD subjects from results and from the result count
  5. `END` (compliance officer) complete with zero writes and the scope evaluation recorded
- **Intents**:
  - `hcmnext.search.find_workers/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: indexed worker attributes
  - `privacy`: disclosure decisions per subject
  - `security`: the caller's search scope
- **Writes**:
- **Evidence required**:
  - Scope evaluation before the query.
  - The disclosure filter applied, so the result set is provably filtered.
  - Zero-effect receipt.
- **Failure and repair**:
  - A caller with no scope searches -> DENIED before any statement runs; running the query and filtering afterwards leaks through timing
  - A withheld subject matches the query -> it does not appear and the count does not include it; a count that includes hidden matches discloses existence
  - A caller infers a subject's existence from a ranking artefact -> the ranking is computed only over disclosable subjects for that caller
- **Jurisdiction**:
  - US federal: Confidential personnel matters, including protected leave and investigations, must not be inferable from a directory search.
  - US state variation: State medical and personnel privacy statutes reach the same conclusion by a different route.
  - International: GDPR data minimization and the confidentiality of investigations under the Whistleblower Directive both require that existence not be leaked.

### WF-SRC-002. Turn a search result into a governed batch action

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 39 Search Discovery Intelligence feature search_to_batch_action
- **Archetype**: `A10` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer
- **Trigger**: A user selects results and applies an action to them all.
- **Preconditions**:
  - The selection is converted into an immutable population snapshot before any action.
  - Each item's individual governance still applies.
  - The action's aggregate effect is simulated before approval.
- **Steps**:
  1. `CAPABILITY` (HR business partner) convert the selection into a frozen population snapshot
  2. `DECISION` (compliance officer) refuse when the selection includes subjects the caller may see in search but not act on
  3. `SUBWORKFLOW` (HR business partner) hand the snapshot to the bulk operation workflow with its own governance
  4. `END` (HR business partner) close the search-side intent; outcomes belong to the batch
- **Intents**:
  - `hcmnext.dataops.define_population/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: act-on authority per subject, which differs from read authority
  - `people`: the selected subjects
- **Writes**:
  - `dataops`: population snapshot from the selection
- **Evidence required**:
  - The frozen selection.
  - The read-versus-act authority evaluation.
  - Handoff reference to the batch.
- **Failure and repair**:
  - The caller may read a subject but not act on it -> the subject is excluded and named; read and act are different grants and conflating them is a privilege escalation
  - The search results change between selection and action -> the snapshot is what was selected; the search moving on does not change the batch
  - The selection is very large -> the aggregate simulation and its approval threshold scale with it
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-SRC-003. Rank search results without leaking hidden signal

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: HR business partner, recruiter, compliance officer, integration system
  - performing a step: recruiter, compliance officer, integration system
  - participating without owning a step: HR business partner
- **Trigger**: Results are ordered by relevance and the ranking must not encode information the caller cannot read.
- **Preconditions**:
  - Ranking features are declared and each is checked against the caller's read authorization.
  - A feature the caller cannot read is excluded from their ranking, not merely hidden.
  - The ranking is explainable per result.
- **Steps**:
  1. `CAPABILITY` (integration system) resolve the ranking feature set the caller may use
  2. `DECISION` (compliance officer) exclude features the caller cannot read; ranking by a hidden salary would disclose it through order
  3. `TRANSFORM` (integration system) score and order the disclosable results
  4. `CAPABILITY` (recruiter) return per-result ranking reasons
  5. `END` (compliance officer) complete with the feature set used recorded
- **Intents**:
  - `hcmnext.search.rank_results/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: indexed features
  - `privacy`: feature classification and caller authorization
- **Writes**:
- **Evidence required**:
  - The feature set actually used for this caller.
  - Per-result ranking reasons.
  - The excluded feature list.
- **Failure and repair**:
  - A feature is used that the caller cannot read -> the order itself leaks the value; excluding the feature is the only correct treatment
  - Two callers get different orders for the same query -> correct and expected, because their feature sets differ; the recorded feature set explains it
  - Ranking correlates with a protected characteristic -> the correlation is a finding for the model owner; search ranking in a hiring context is a selection procedure
- **Jurisdiction**:
  - US federal: Search ranking used to select candidates is a selection procedure under the Uniform Guidelines if it affects who is considered.
  - US state variation: New York City Local Law 144 reaches tools that substantially assist employment decisions, which a candidate ranker does.
  - International: The EU AI Act covers candidate ranking as high risk.

### WF-SRC-004. Run a semantic search over knowledge with scope enforcement

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: employee, AI agent, compliance officer, integration system
- **Trigger**: A worker searches published knowledge and results are constrained by audience and jurisdiction.
- **Preconditions**:
  - The index carries audience scope and jurisdiction per article.
  - Retrieval filters before scoring, not after.
  - An empty result set is explained rather than presented as nothing existing.
- **Steps**:
  1. `CAPABILITY` (integration system) filter the candidate set by audience scope and jurisdiction before scoring
  2. `TRANSFORM` (integration system) score semantically over the filtered set
  3. `DECISION` (compliance officer) when the filtered set is empty, say that no in-scope article exists rather than returning out-of-scope results
  4. `CAPABILITY` (employee) return results with their article versions
  5. `END` (AI agent) complete with the filter and the retrieval set recorded
- **Intents**:
  - `hcmnext.search.semantic_search/v1` (PROPOSED, creates)
- **Reads**:
  - `knowledge`: published articles, audience scopes, jurisdictions
  - `people`: the searcher's scope and jurisdiction
- **Writes**:
- **Evidence required**:
  - The filter applied before scoring.
  - Retrieval set with article versions.
  - The empty-result explanation where it applies.
- **Failure and repair**:
  - Filtering happens after scoring -> the score can leak which out-of-scope articles exist through result count or latency; filter first
  - An article is relevant but out of jurisdiction -> excluded; a relevant but wrong-jurisdiction policy answer is the most dangerous result the system can return
  - The searcher is an agent acting for a worker -> the worker's scope governs, not the agent's; agent initiation never widens scope
- **Jurisdiction**:
  - US federal: None directly, though wrong-jurisdiction policy guidance creates the employer's own exposure.
  - US state variation: State-specific policy variance is exactly why jurisdiction is a hard filter.
  - International: None.

---

## INC. Incident management

4 workflows.

### WF-INC-001. Detect, classify and communicate a production incident

- **Status**: `PARTIAL` - planning/specs/incident-management.md; definitions/governance/feature-intent-intake.yaml group 40 Operations Incident Management
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, external partner or carrier, auditor
- **Trigger**: A production issue affects customers and must be classified, communicated and resolved.
- **Preconditions**:
  - Severity classification criteria are published and applied consistently.
  - Customer communication obligations attach to severity, not to convenience.
  - The incident record is the source for the postmortem.
- **Steps**:
  1. `OBSERVE` (IT or access administrator) detect through monitoring or a customer report and open the incident
  2. `DECISION` (compliance officer) classify severity against the published criteria, including whether personal data was affected
  3. `SIGNAL` (external partner or carrier) communicate to affected customers within the commitment for that severity
  4. `TASK` (IT or access administrator) mitigate, then resolve, keeping the two distinct
  5. `CAPABILITY` (auditor) record the timeline with the actual times, not the reconstructed ones
  6. `END` (compliance officer) close with a postmortem obligation open for the severity levels that require one
- **Intents**:
  - `hcmnext.operations.manage_incident/v1` (PROPOSED, creates)
- **Reads**:
  - `operations`: monitoring signals, prior incidents, severity criteria
  - `privacy`: data classes affected
  - `tenant`: affected customers and their commitments
- **Writes**:
  - `operations`: incident record, timeline, communications, postmortem obligation
- **Evidence required**:
  - Detection timestamp, which starts the communication and notification clocks.
  - Severity classification with the criteria applied.
  - Communication delivery evidence per customer.
- **Failure and repair**:
  - Personal data was affected -> the breach assessment runs in parallel with its own statutory clocks, which are shorter than most service commitments
  - Mitigation is mistaken for resolution -> the two are recorded separately; a mitigated incident that recurs was never resolved
  - The timeline is reconstructed after the fact -> the reconstruction is marked as such; approximate times in a regulatory notification are a problem of their own
- **Jurisdiction**:
  - US federal: The SEC requires public filers to disclose material cybersecurity incidents within four business days of a materiality determination; HIPAA and sectoral rules add their own.
  - US state variation: All fifty states have breach notification statutes with different triggers and deadlines.
  - International: GDPR Article 33 requires 72-hour notification from awareness; DORA imposes its own reporting windows on financial entities.

### WF-INC-002. Run a postmortem and track its actions to completion

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 2
- **Actors**: IT or access administrator, compliance officer, auditor
- **Trigger**: After an incident, the causes and the actions are documented and tracked.
- **Preconditions**:
  - The postmortem is required by severity, not by choice.
  - Actions have owners and dates and are tracked outside the postmortem document.
  - Repeat incidents link to the prior postmortem.
- **Steps**:
  1. `TASK` (IT or access administrator) document the timeline, the contributing factors and the detection gap
  2. `TRANSFORM` (IT or access administrator) produce actions with owners and due dates
  3. `DECISION` (compliance officer) link to any prior postmortem with the same contributing factor, since a repeat means the prior action failed
  4. `WAIT` (auditor) track each action to completion
  5. `END` (compliance officer) close only when every action is complete or explicitly cancelled with a reason
- **Intents**:
  - `hcmnext.operations.conduct_postmortem/v1` (PROPOSED, creates)
- **Reads**:
  - `operations`: incident record, prior postmortems and their actions
- **Writes**:
  - `operations`: postmortem, actions and their completion state
- **Evidence required**:
  - Detection gap, which is usually the most valuable finding.
  - Actions with owners and dates.
  - Links to prior postmortems with the same factor.
- **Failure and repair**:
  - Actions are recorded but never tracked -> the postmortem is not closed; an untracked action list is documentation theatre
  - The same factor appears a third time -> escalate above the incident process; three occurrences means the process is the problem
  - An action is cancelled -> permitted with a recorded reason and an owner, not by silent expiry
- **Jurisdiction**:
  - US federal: SOC 2 and ISO 27001 both expect incident review and corrective action tracking.
  - US state variation: None state-specific.
  - International: DORA requires financial entities to conduct post-incident reviews and to feed them into their risk management.

### WF-INC-003. Communicate a customer-affecting incident under contract

- **Status**: `NEW`
- **Archetype**: `A13` | **Complexity tier**: 3
- **Actors**: compliance officer, external partner or carrier, IT or access administrator
- **Trigger**: An incident triggers contractual notification duties to specific customers.
- **Preconditions**:
  - Each customer's notification commitment is known and differs.
  - The notification content is approved before sending, not composed under pressure.
  - The notification is delivered to the contractual contact, not to whoever is convenient.
- **Steps**:
  1. `CAPABILITY` (compliance officer) resolve the affected customers and their individual notification commitments
  2. `TASK` (compliance officer) draft the notification from the incident record with legal review
  3. `APPROVAL` (compliance officer) the incident commander and legal approve the content before any send
  4. `CAPABILITY` (external partner or carrier) deliver to each customer's contractual contact and record the delivery
  5. `END` (IT or access administrator) close with per-customer delivery evidence against their own deadline
- **Intents**:
  - `hcmnext.operations.communicate_incident/v1` (PROPOSED, creates)
- **Reads**:
  - `operations`: incident record and its established facts
  - `tenant`: affected customers and their commitments
- **Writes**:
  - `operations`: notification content, approvals and per-customer delivery
- **Evidence required**:
  - Per-customer commitment and the deadline it sets.
  - Approved content, so no unreviewed statement goes out.
  - Delivery evidence against each deadline.
- **Failure and repair**:
  - Facts change after notification -> an update is sent; a correction is better than a silence that becomes a credibility problem
  - A customer's contractual contact is stale -> the delivery fails and escalates; a notification sent to a departed employee is not delivered
  - A deadline is missed -> recorded with the delay; hiding a missed notification deadline compounds the original incident
- **Jurisdiction**:
  - US federal: The SEC's four-business-day materiality disclosure applies to public filers; HIPAA business associates must notify covered entities without unreasonable delay and within 60 days.
  - US state variation: State breach notification statutes set employer and vendor duties separately and can require attorney-general notification above thresholds.
  - International: GDPR Article 33(2) requires a processor to notify the controller without undue delay; DORA sets its own reporting windows.

### WF-INC-004. Apply a kill switch to a failing capability

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 4
- **Actors**: IT or access administrator, compliance officer, auditor, integration system
  - performing a step: IT or access administrator, compliance officer, integration system
  - participating without owning a step: auditor
- **Trigger**: A capability is causing harm and must be disabled quickly without a deployment.
- **Preconditions**:
  - Kill switches exist per capability and their blast radius is known before use.
  - Disabling a capability has a declared effect on in-flight work.
  - The switch's use is itself an approved and recorded act.
- **Steps**:
  1. `DECISION` (IT or access administrator) identify the capability and confirm its blast radius, including which workflows will block
  2. `APPROVAL` (compliance officer) an approver independent of the operator approves, with a break-glass path for severity one
  3. `CAPABILITY` (integration system) disable the capability; in-flight work blocks rather than failing silently
  4. `SIGNAL` (IT or access administrator) notify the owners of every blocked workflow
  5. `WAIT` (IT or access administrator) hold until the fix is verified
  6. `CAPABILITY` (integration system) re-enable and drain the blocked work
- **Intents**:
  - `hcmnext.operations.apply_kill_switch/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: capability registry and dependent workflows
  - `workflow`: in-flight instances that will block
- **Writes**:
  - `governance`: capability disabled state
  - `operations`: switch use record and the blocked work
- **Evidence required**:
  - Blast radius assessment before use.
  - Approval, or the break-glass justification.
  - The blocked work and its eventual drain.
- **Failure and repair**:
  - The capability is on a critical path such as payroll release -> the blast radius assessment says so and the decision becomes a business one rather than an engineering one
  - In-flight work fails rather than blocks -> REJECT that behaviour; a failed intent is much harder to recover than a blocked one
  - The switch is left on after the fix -> the re-enable is tracked as an obligation; a forgotten kill switch is an outage nobody is looking for
- **Jurisdiction**:
  - US federal: None directly, though disabling a capability that produces a statutory filing does not pause the deadline.
  - US state variation: None.
  - International: None.

---

## EVT. Events, scheduling and automation

3 workflows.

### WF-EVT-001. Subscribe to a domain event and act on it

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 42 Events Scheduling Automation features event_definition_create and event_subscription_manage
- **Archetype**: `A17` | **Complexity tier**: 3
- **Actors**: IT or access administrator, HR business partner, compliance officer, integration system
- **Trigger**: A tenant configures an automation that reacts to a domain event.
- **Preconditions**:
  - The event's schema and its delivery guarantee are declared.
  - The subscriber's action runs under a principal with declared authority, not with the event's own.
  - Loops between an event and the action it triggers are detectable.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) register the subscription with its filter and its action principal
  2. `DECISION` (compliance officer) refuse a subscription whose action would emit the event it subscribes to
  3. `OBSERVE` (integration system) receive the event and evaluate the filter
  4. `CAPABILITY` (integration system) create the action intent under the declared principal's authority
  5. `END` (HR business partner) close the subscription firing; the intent's outcome is its own
- **Intents**:
  - `hcmnext.system.manage_event_subscription/v1` (PROPOSED, creates)
- **Reads**:
  - `security`: the action principal and its authority
  - `workflow`: event definitions, subscriptions, prior firings
- **Writes**:
  - `work`: the created action intents
  - `workflow`: subscription record and firing history
- **Evidence required**:
  - Subscription version and its filter.
  - Per-firing record with the event and the created intent.
  - Loop detection evaluation.
- **Failure and repair**:
  - An event storms and the subscription fires thousands of times -> the rate limit applies and the excess is quarantined rather than executed; an automation that amplifies a storm is worse than one that drops
  - The action principal's authority is narrower than the action needs -> the intent is refused with the reason; automations do not get elevated authority
  - Two subscriptions form a loop -> detected at registration where possible and by the loop guard at runtime otherwise
- **Jurisdiction**:
  - US federal: None directly; the created intents carry their own obligations.
  - US state variation: None.
  - International: An automation making decisions about individuals engages GDPR Article 22 if it has significant effects.

### WF-EVT-002. Schedule a recurring job and prove it ran

- **Status**: `NEW`
- **Archetype**: `A17` | **Complexity tier**: 2
- **Actors**: IT or access administrator, auditor, integration system
- **Trigger**: A periodic job runs on a schedule and its execution must be provable, including its non-execution.
- **Preconditions**:
  - The schedule declares its time zone and its behaviour on a missed window.
  - A missed run is recorded, not silently skipped.
  - Overlapping runs are prevented or explicitly permitted.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) register the job with its schedule, time zone and overlap policy
  2. `WAIT` (integration system) hold to each scheduled instant
  3. `DECISION` (IT or access administrator) on a missed window, apply the declared catch-up policy and record the miss
  4. `CAPABILITY` (integration system) execute, preventing overlap where the policy forbids it
  5. `OBSERVE` (auditor) record the outcome, including a zero-work run
- **Intents**:
  - `hcmnext.system.schedule_job/v1` (PROPOSED, creates)
- **Reads**:
  - `workflow`: job definition, schedule, prior runs
- **Writes**:
  - `workflow`: run records including misses and zero-work runs
- **Evidence required**:
  - Every scheduled instant with its outcome, including misses.
  - Overlap prevention evidence.
  - Zero-work runs recorded, so silence is distinguishable from failure.
- **Failure and repair**:
  - A run is missed during an outage -> the miss is recorded and the catch-up policy decides; a silently skipped compliance job is a gap nobody sees
  - A run overlaps the previous one -> prevented by default; two concurrent payroll accrual jobs will double-count
  - The schedule crosses a daylight saving transition -> the declared time zone resolves it, and a doubled or skipped hour is handled by the policy rather than by chance
- **Jurisdiction**:
  - US federal: Jobs producing regulatory filings or deposits inherit those deadlines, so a missed run is a compliance event.
  - US state variation: None state-specific.
  - International: None.

### WF-EVT-003. Design an automation with a human checkpoint

- **Status**: `NEW`
- **Archetype**: `A17` | **Complexity tier**: 3
- **Actors**: HR business partner, compliance officer, manager, integration system
- **Trigger**: A tenant automates a routine sequence but keeps a human decision at the consequential step.
- **Preconditions**:
  - The automation's steps and their authority are declared.
  - The human checkpoint is at the step with the material consequence, not at the end.
  - The automation cannot be edited to remove the checkpoint without approval.
- **Steps**:
  1. `TASK` (HR business partner) design the automation and place the checkpoint
  2. `RULE` (compliance officer) validate that every step with a material consequence is either preceded by a checkpoint or is reversible
  3. `APPROVAL` (compliance officer) compliance approves the automation and its checkpoint placement
  4. `CAPABILITY` (integration system) publish the automation as a versioned artifact
  5. `DECISION` (compliance officer) refuse an edit that removes a checkpoint without a new approval
  6. `END` (manager) close; each execution runs under the published version
- **Intents**:
  - `hcmnext.system.design_automation/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: which consequences require a human decision
  - `workflow`: automation definitions, capability consequences
- **Writes**:
  - `workflow`: automation version and its checkpoint placement
- **Evidence required**:
  - Consequence analysis per step.
  - Checkpoint placement and its approval.
  - Edit history showing that no checkpoint was silently removed.
- **Failure and repair**:
  - A checkpoint is removed in a later version -> the approval requirement catches it; automations degrade toward fewer checkpoints without this control
  - The checkpoint is placed after the irreversible step -> REJECT; a review after a payment has left is not a checkpoint
  - The automation runs faster than a human can review -> the checkpoint queues rather than being skipped; throughput is not a reason to bypass a decision
- **Jurisdiction**:
  - US federal: An automated adverse employment action without human review engages EEOC scrutiny and, in several jurisdictions, statutory duties.
  - US state variation: New York City Local Law 144 reaches automations that substantially assist employment decisions and is in force; several states have enacted or are enacting comparable deployer duties whose current text belongs in the versioned rule pack rather than in this document.
  - International: GDPR Article 22 and the EU AI Act both require meaningful human oversight for decisions with significant effects.

---

## SBX. Sandbox, testing and isolation

3 workflows.

### WF-SBX-001. Generate synthetic test data that never touches production

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 44 Sandbox Testing Isolation feature test_data_generate
- **Archetype**: `A19` | **Complexity tier**: 2
- **Actors**: IT or access administrator, compliance officer, integration system
- **Trigger**: A test environment needs a realistic population without copying real people.
- **Preconditions**:
  - The generator produces referentially consistent records across domains.
  - No generated identifier collides with a real one in a way that could route real traffic.
  - The generated population exercises the edge cases the tests need.
- **Steps**:
  1. `TASK` (IT or access administrator) declare the population shape: sizes, jurisdictions, edge cases required
  2. `CAPABILITY` (integration system) generate consistent records across people, position, compensation and time
  3. `DECISION` (compliance officer) refuse to generate into an environment that also holds production data
  4. `CAPABILITY` (IT or access administrator) load and verify referential integrity
  5. `END` (IT or access administrator) close with the generation seed recorded so the population is reproducible
- **Intents**:
  - `hcmnext.tenant.generate_test_data/v1` (PROPOSED, creates)
- **Reads**:
  - `tenant`: environment classification and its contents
- **Writes**:
  - `tenant`: generated population with its seed
- **Evidence required**:
  - Generation seed, so a failing test is reproducible.
  - Environment classification check.
  - Referential integrity verification.
- **Failure and repair**:
  - The environment already holds production data -> REJECT; mixing synthetic and real records makes every later privacy question unanswerable
  - A generated email address is deliverable -> REJECT; test data that can send real messages has caused real incidents
  - The generated population lacks the edge cases the tests assume -> the tests pass vacuously; the declared shape is what prevents it
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: GDPR data protection by design favours synthetic data over masked production data for testing.

### WF-SBX-002. Execute a conformance scenario against a fixture environment

- **Status**: `EXISTING` - internal/workflow/conformance environment.go and setup.go across the nine packages
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: IT or access administrator, auditor, compliance officer, integration system
- **Trigger**: A reference workflow is walked end to end against canned capabilities to prove its composition.
- **Preconditions**:
  - The environment's capabilities are fixtures, not real integrations.
  - The expected terminal and its five lifecycle dimensions are declared before the run.
  - The walk asserts the exact path, not merely that it completed.
- **Steps**:
  1. `CAPABILITY` (IT or access administrator) build the fixture environment with its canned capability handlers
  2. `CAPABILITY` (integration system) compile the definition against the fixture registry
  3. `CAPABILITY` (integration system) walk the workflow in simulate mode
  4. `DECISION` (auditor) assert the exact node path and the exact completion mapping, not just the terminal code
  5. `END` (compliance officer) close with the walk recorded as conformance evidence
- **Intents**:
  - `hcmnext.operations.run_conformance_scenario/v1` (PROPOSED, creates)
- **Reads**:
  - `dataops`: fixture data
  - `workflow`: definition, capability registry, expected walk
- **Writes**:
  - `operations`: conformance run record and its assertions
- **Evidence required**:
  - The exact node path taken.
  - The five lifecycle dimensions at the terminal.
  - The capability versions the fixture registry published.
- **Failure and repair**:
  - The walk reaches the right terminal by the wrong path -> the assertion fails; a terminal code alone does not prove the composition
  - A fixture capability is mistaken for a real one -> the environment declares its capabilities as conformance fixtures, which is why they are namespaced separately
  - The definition compiles but a capability version is missing at run time -> the run fails at compile rather than mid walk
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-SBX-003. Analyze a test run and quarantine a flaky result

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 2
- **Actors**: IT or access administrator, auditor, integration system
- **Trigger**: A test suite result is analyzed and unreliable tests are separated from real failures.
- **Preconditions**:
  - Each test's history is retained so flakiness is measurable.
  - A quarantined test still runs but does not gate.
  - Quarantine has an expiry so it does not become permanent.
- **Steps**:
  1. `CAPABILITY` (integration system) collect the run's results and compare against each test's history
  2. `DECISION` (IT or access administrator) classify a failure as new, known-failing or flaky using the history rather than the run alone
  3. `CAPABILITY` (IT or access administrator) quarantine a flaky test with an owner and an expiry
  4. `DECISION` (auditor) refuse to quarantine a test that has never passed; that is a real failure
  5. `END` (IT or access administrator) close with the gating result computed from the non-quarantined set
- **Intents**:
  - `hcmnext.operations.analyze_test_run/v1` (PROPOSED, creates)
- **Reads**:
  - `operations`: test history, run results, quarantine list
- **Writes**:
  - `operations`: classification, quarantine records with owners and expiries
- **Evidence required**:
  - Per-test history behind each classification.
  - Quarantine owner and expiry.
  - The gating computation and what it excluded.
- **Failure and repair**:
  - A real regression is classified as flaky -> the history is the guard; a test that passed consistently and now fails is not flaky
  - A quarantine expires with no action -> the test un-quarantines and gates again, which forces the decision
  - The quarantine list grows steadily -> the trend is itself the finding; a large quarantine means the suite no longer gates anything
- **Jurisdiction**:
  - US federal: None.
  - US state variation: None.
  - International: None.

---

## GRC. Governance, compliance and audit

5 workflows.

### WF-GRC-001. Define a compliance control and assess it

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 45 Governance Compliance Audit features compliance_control_define and compliance_assessment_run
- **Archetype**: `A19` | **Complexity tier**: 3
- **Actors**: compliance officer, auditor, IT or access administrator, integration system
  - performing a step: compliance officer, auditor, integration system
  - participating without owning a step: IT or access administrator
- **Trigger**: A control is defined against a requirement and assessed for design and operating effectiveness.
- **Preconditions**:
  - The control maps to a specific requirement, not to a framework in general.
  - Evidence for the control is produced by the system, not assembled by hand.
  - Design effectiveness and operating effectiveness are assessed separately.
- **Steps**:
  1. `TASK` (compliance officer) define the control, its requirement mapping, its owner and its evidence source
  2. `CAPABILITY` (integration system) collect the evidence the control's source produces
  3. `TASK` (auditor) assess design effectiveness: would the control work if it operated
  4. `TASK` (auditor) assess operating effectiveness: did it actually operate through the period
  5. `DECISION` (compliance officer) record a deficiency where either assessment fails, rather than a single pass or fail
  6. `END` (compliance officer) close with deficiencies tracked as their own obligations
- **Intents**:
  - `hcmnext.governance.assess_control/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: control definitions, requirement mappings, prior assessments
  - `ledger`: evidence the control's source produces
- **Writes**:
  - `governance`: control assessment with design and operating results and deficiencies
- **Evidence required**:
  - Evidence produced by the system rather than asserted.
  - Separate design and operating conclusions.
  - Deficiency records with owners.
- **Failure and repair**:
  - Evidence is assembled by hand for the assessment -> the control's evidence source is the finding; a control whose evidence is manual is fragile
  - The control operated but only because someone remembered -> operating effectiveness passes and design effectiveness fails, which is the honest result
  - A deficiency is recorded and never remediated -> it carries into the next assessment with its age, which is what makes it visible
- **Jurisdiction**:
  - US federal: SOX 404 requires management's assessment of internal control over financial reporting; the design and operating distinction comes from the audit standards.
  - US state variation: State regulators for licensed entities impose their own control frameworks.
  - International: DORA and NIS2 both require documented control frameworks with periodic assessment for covered entities.

### WF-GRC-002. Produce a compliance report for a regulator or a customer

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 45 Governance Compliance Audit feature compliance_report_publish
- **Archetype**: `A11` | **Complexity tier**: 3
- **Actors**: compliance officer, auditor, external partner or carrier
- **Trigger**: A periodic attestation or report is produced for an external party.
- **Preconditions**:
  - The report's scope, period and framework are declared.
  - Every assertion traces to control evidence.
  - The signing authority is identified and the package is immutable at signature.
- **Steps**:
  1. `CAPABILITY` (compliance officer) assemble the report from control assessments and their evidence
  2. `DECISION` (compliance officer) refuse to assert a control that has an open deficiency without disclosing it
  3. `APPROVAL` (compliance officer) the signing authority approves the exact package digest
  4. `CAPABILITY` (external partner or carrier) publish or transmit to the recipient
  5. `END` (auditor) close with the package and its signature retained
- **Intents**:
  - `hcmnext.governance.publish_compliance_report/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: control assessments, deficiencies, framework mapping
  - `ledger`: underlying evidence
- **Writes**:
  - `governance`: report package, signature, transmission record
- **Evidence required**:
  - Per-assertion evidence trace.
  - Deficiency disclosure where one is open.
  - Signature bound to the package digest.
- **Failure and repair**:
  - A deficiency is omitted -> REJECT; an undisclosed known deficiency in a signed attestation is the most serious failure in this workflow
  - The report is amended after signature -> a new package with a new signature; the original stands
  - Evidence for an assertion cannot be produced -> the assertion is qualified rather than made, and the qualification is visible
- **Jurisdiction**:
  - US federal: Sarbanes-Oxley 302 and 906 certifications carry personal liability for signatories; SOC 2 reports carry auditor rather than management liability but the underlying assertions are management's.
  - US state variation: State regulators for licensed entities require their own filings with their own deadlines.
  - International: DORA requires financial entities to report their ICT risk management framework; the EU AI Act requires conformity documentation for high-risk systems.

### WF-GRC-003. Map a new regulation to existing controls and find the gaps

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: compliance officer, auditor, HR business partner
- **Trigger**: A new obligation arrives and the existing control set is assessed against it.
- **Preconditions**:
  - The obligation is decomposed into testable requirements.
  - Existing controls are mapped by what they actually test, not by their titles.
  - Gaps become tracked obligations with owners and dates.
- **Steps**:
  1. `TASK` (compliance officer) decompose the regulation into discrete testable requirements
  2. `TRANSFORM` (compliance officer) map each requirement to existing controls by their evidence, not their names
  3. `DECISION` (auditor) record a partial mapping as a gap rather than as coverage
  4. `TASK` (HR business partner) assign each gap an owner and a target date
  5. `END` (compliance officer) close the mapping; each gap continues as its own obligation
- **Intents**:
  - `hcmnext.governance.map_regulation/v1` (PROPOSED, creates)
- **Reads**:
  - `governance`: control inventory, existing mappings, evidence sources
- **Writes**:
  - `governance`: regulation decomposition, mapping and gap register
- **Evidence required**:
  - Requirement decomposition with citations.
  - Mapping evidence, not title matching.
  - Gap register with owners and dates.
- **Failure and repair**:
  - A control's title suggests coverage its evidence does not support -> the evidence-based mapping is what catches it; title matching produces false comfort
  - The regulation's effective date precedes the gap closure -> the exposure interval is computed and recorded rather than hidden
  - A requirement has no possible control -> escalate; some obligations require a product change rather than a control
- **Jurisdiction**:
  - US federal: New federal rules arrive with effective dates that do not wait for readiness; the gap register with dates is what makes the exposure quantifiable.
  - US state variation: State legislative sessions produce a steady stream of new employment obligations with short lead times, particularly in California, New York, Colorado, Illinois and Washington.
  - International: The EU AI Act, the Pay Transparency Directive, DORA and NIS2 all phase in over several years with different application dates.

### WF-GRC-004. Run a policy attestation campaign

- **Status**: `NEW`
- **Archetype**: `A10` | **Complexity tier**: 3
- **Actors**: compliance officer, employee, manager, auditor
- **Trigger**: Every worker in scope must attest to a specific version of a policy by a deadline.
- **Preconditions**:
  - The policy version is immutable and the attestation binds its digest.
  - The population is frozen at campaign launch.
  - Non-attestation has a defined consequence that is not deemed attestation.
- **Steps**:
  1. `CAPABILITY` (compliance officer) freeze the in-scope population and bind the exact policy version
  2. `PARALLEL` (employee) issue one attestation task per worker
  3. `TASK` (employee) each worker attests to the specific version, having been able to read it
  4. `WAIT` (manager) hold to the deadline with escalation to managers
  5. `DECISION` (compliance officer) escalate non-attesters rather than deeming attestation
  6. `END` (auditor) close with the attested and unattested sets both named and retained
- **Intents**:
  - `hcmnext.governance.run_attestation_campaign/v1` (PROPOSED, creates)
- **Reads**:
  - `documents`: the policy version and its digest
  - `governance`: prior campaigns and their outcomes
  - `people`: the in-scope population and manager relationships
- **Writes**:
  - `documents`: attestation records bound to the policy digest
  - `governance`: campaign record with both sets
- **Evidence required**:
  - Policy digest each attestation binds.
  - Per-worker attestation with its timestamp and the time spent, where the policy requires meaningful review.
  - The unattested set at the deadline with the escalation record.
- **Failure and repair**:
  - Deemed attestation is applied to non-responders -> REJECT; a deemed attestation has no evidentiary value in the dispute it exists to prevent
  - The policy is revised mid campaign -> the campaign is REPLANNED against the new version; attestations to the old version bind the old digest and do not carry forward
  - A worker is on leave and cannot attest -> the task defers to their return rather than counting as a refusal
- **Jurisdiction**:
  - US federal: Arbitration agreements, codes of conduct and safety policies all depend on provable attestation to a specific version; a link-based attestation to mutable content has been held unenforceable.
  - US state variation: California requires certain notices in the employee's primary language, and several states require specific acknowledgement mechanics for arbitration agreements.
  - International: In the EU, changes to core employment terms require written notice within a month, and works council agreement may be needed before a policy affecting working conditions is imposed.

### WF-GRC-005. Assess a third party before it processes worker data

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: compliance officer, IT or access administrator, external partner or carrier, finance partner
- **Trigger**: A vendor will handle worker personal data and must be assessed before any data flows.
- **Preconditions**:
  - The data classes, the volume and the destination are known before the assessment starts.
  - The assessment covers security, privacy, resilience and, where relevant, sub-processors.
  - A contract with the required terms precedes any transfer, not the reverse.
- **Steps**:
  1. `CAPABILITY` (compliance officer) classify the data the vendor will handle and the flows involved
  2. `TASK` (IT or access administrator) run the security and privacy assessment appropriate to the classification, not a single generic questionnaire
  3. `DECISION` (compliance officer) block onboarding where a required contractual term or control is absent, rather than accepting a remediation promise
  4. `TASK` (external partner or carrier) execute the processing agreement with its sub-processor and audit terms
  5. `CAPABILITY` (compliance officer) record the assessment, its residual risk and its reassessment date
  6. `WAIT` (finance partner) hold to the reassessment date or to a material change in the vendor's posture
- **Intents**:
  - `hcmnext.governance.assess_third_party/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: the contract and its terms
  - `integration`: the connector and its destination
  - `privacy`: data classes, flows, existing assessments
- **Writes**:
  - `governance`: assessment, residual risk, reassessment obligation
  - `privacy`: processing agreement reference
- **Evidence required**:
  - The data classification the assessment was scoped to.
  - The executed processing agreement with its date, preceding the first transfer.
  - Residual risk and the reassessment date.
- **Failure and repair**:
  - Data flows before the agreement is executed -> an incident; the transfer is unlawful in several regimes and the vendor's obligations are unenforceable
  - The vendor adds a sub-processor without notice -> the assessment reopens automatically; sub-processor drift is how an assessed vendor becomes an unassessed chain
  - A control gap is accepted on a remediation promise -> permitted only with a dated obligation and an owner; an undated acceptance is how gaps become permanent
- **Jurisdiction**:
  - US federal: HIPAA requires a business associate agreement before protected health information is disclosed; the Gramm-Leach-Bliley safeguards rule and sector regulators impose vendor diligence duties.
  - US state variation: New York's SHIELD Act and Massachusetts 201 CMR 17 both require reasonable third-party contractual safeguards; several states impose vendor notification duties on breach.
  - International: GDPR Article 28 requires a processor contract with prescribed terms and prior authorization for sub-processors; DORA imposes a register of information and specific contractual terms for critical ICT providers to financial entities.

---

## PRG. Program and portfolio management

3 workflows.

### WF-PRG-001. Charter an HR program and track it to an outcome

- **Status**: `NEW`
- **Archetype**: `A20` | **Complexity tier**: 2
- **Actors**: HR business partner, finance partner, manager, integration system
- **Trigger**: A multi-quarter HR initiative is chartered with an outcome measure and tracked.
- **Preconditions**:
  - The outcome measure is defined before the program starts, using the metric definition workflow.
  - The baseline is measured before any intervention.
  - The program's changes to worker terms run through their own governed intents.
- **Steps**:
  1. `TASK` (HR business partner) charter the program with its outcome measure, baseline and sponsor
  2. `CAPABILITY` (integration system) measure the baseline before any intervention
  3. `APPROVAL` (finance partner) the sponsor and finance approve the charter and its budget
  4. `WAIT` (manager) track progress against milestones through the program period
  5. `CAPABILITY` (integration system) measure the outcome against the baseline using the same metric version
  6. `END` (HR business partner) close with the outcome recorded whether or not it was achieved
- **Intents**:
  - `hcmnext.program.charter_program/v1` (PROPOSED, creates)
  - `hcmnext.analytics.define_metric/v1` (PROPOSED, creates)
- **Reads**:
  - `analytics`: metric definitions and baseline observations
  - `budget`: program budget
  - `people`: the population the program affects
- **Writes**:
  - `program`: charter, milestones, outcome measurement
- **Evidence required**:
  - Baseline measured before intervention, not reconstructed after.
  - The metric version used for both baseline and outcome.
  - The outcome recorded regardless of direction.
- **Failure and repair**:
  - The metric definition changes mid program -> both measurements use the charter's pinned version; changing the ruler mid measurement is the most common way programs claim false success
  - The baseline is measured after the program starts -> the measurement is marked as a partial baseline and the claim is weakened accordingly
  - The outcome is worse than the baseline -> recorded as measured; a program register that only contains successes is not a register
- **Jurisdiction**:
  - US federal: None directly; the underlying changes carry their obligations.
  - US state variation: None.
  - International: Programs affecting working conditions may require works council involvement from the charter stage.

### WF-PRG-002. Prioritize a portfolio against a constrained capacity

- **Status**: `NEW`
- **Archetype**: `A20` | **Complexity tier**: 2
- **Actors**: finance partner, HR business partner, manager
  - performing a step: finance partner, HR business partner
  - participating without owning a step: manager
- **Trigger**: More initiatives are proposed than can be funded and a prioritization is recorded.
- **Preconditions**:
  - The scoring criteria are declared before the proposals are scored.
  - Capacity is a hard constraint expressed in a real unit.
  - The unfunded set is recorded, not discarded.
- **Steps**:
  1. `CAPABILITY` (HR business partner) collect proposals with their cost, capacity demand and expected outcome
  2. `TRANSFORM` (finance partner) score against the declared criteria and rank
  3. `DECISION` (finance partner) record the cut line and the unfunded set explicitly
  4. `APPROVAL` (finance partner) the portfolio authority approves the funded set
  5. `END` (HR business partner) close; funded proposals become chartered programs through their own intent
- **Intents**:
  - `hcmnext.program.prioritize_portfolio/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: available funding and capacity
  - `program`: proposals, prior portfolio decisions
- **Writes**:
  - `program`: portfolio decision with the funded and unfunded sets
- **Evidence required**:
  - Scoring criteria fixed before scoring.
  - The full ranked list including the unfunded.
  - Capacity constraint in a real unit.
- **Failure and repair**:
  - Criteria are adjusted after seeing the scores -> the change is recorded as a new scoring round; silently retuning to get a preferred answer is the failure mode
  - A proposal is funded outside the ranking -> permitted as an override with a recorded reason and approver
  - Capacity is expressed as a vague notion of bandwidth -> REJECT; an unmeasurable constraint makes the whole exercise theatre
- **Jurisdiction**:
  - US federal: None.
  - US state variation: None.
  - International: None.

### WF-PRG-003. Measure a program outcome against its baseline honestly

- **Status**: `NEW`
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: HR business partner, finance partner, auditor, integration system
- **Trigger**: A program's claimed outcome is tested against the baseline with confounders acknowledged.
- **Preconditions**:
  - The metric version is the one the charter pinned.
  - Known confounders in the period are enumerated before the result is read.
  - A null or negative result is reportable.
- **Steps**:
  1. `CAPABILITY` (integration system) measure the outcome using the charter's pinned metric version
  2. `TRANSFORM` (finance partner) compare against the baseline and compute the change with its uncertainty
  3. `DECISION` (auditor) enumerate confounders in the period and state which the measurement cannot separate
  4. `CAPABILITY` (HR business partner) publish the result including the confounders and the uncertainty
  5. `END` (auditor) close with the result recorded regardless of direction
- **Intents**:
  - `hcmnext.program.measure_outcome/v1` (PROPOSED, creates)
- **Reads**:
  - `analytics`: baseline, outcome, metric version
  - `people`: the population and its changes during the period
- **Writes**:
  - `program`: outcome measurement with confounders and uncertainty
- **Evidence required**:
  - Pinned metric version used for both measurements.
  - Confounder enumeration.
  - Uncertainty stated with the change.
- **Failure and repair**:
  - The population changed substantially during the period -> a confounder that may dominate the effect; stating it is the difference between analysis and advocacy
  - The result is negative -> published as measured; suppressing it makes the whole program register worthless
  - The measurement is taken early because the trend looks good -> the charter's measurement date governs, and an early read is labelled as interim
- **Jurisdiction**:
  - US federal: None.
  - US state variation: None.
  - International: None.

---

## PRO. Provisioning lifecycle management

3 workflows.

### WF-PRO-001. Request and track provisioning of a resource

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 48 Provisioning Lifecycle Management features resource_provision_request and provisioning_task_track
- **Archetype**: `A15` | **Complexity tier**: 2
- **Actors**: employee, IT or access administrator, manager, integration system
- **Trigger**: A worker requests a tool, a licence or a resource that has a cost and an approval.
- **Preconditions**:
  - The catalog entry declares its cost, its owner and its approval requirement.
  - Licence availability is a fenced resource, not an assumption.
  - The request's fulfilment is observable.
- **Steps**:
  1. `TASK` (employee) the worker requests from the catalog
  2. `APPROVAL` (manager) the manager approves the cost and the resource owner approves the entitlement
  3. `CAPABILITY` (IT or access administrator) reserve a licence seat if the resource is seat-limited
  4. `CAPABILITY` (integration system) provision through the target system
  5. `OBSERVE` (integration system) confirm the resource is usable, not merely that the operation returned
  6. `END` (IT or access administrator) close with the seat consumption and the deprovisioning trigger registered
- **Intents**:
  - `hcmnext.access.request_resource/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: catalog, seat availability, existing grants
  - `budget`: cost centre for the charge
  - `people`: the requester's role and eligibility
- **Writes**:
  - `access`: grant and seat consumption
  - `budget`: recurring cost attribution
- **Evidence required**:
  - Seat reservation and consumption.
  - Both approvals with their thresholds.
  - Fulfilment observation.
- **Failure and repair**:
  - No seats are available -> the request queues against the licence pool rather than provisioning and failing later
  - The resource is provisioned but unusable -> the observation catches it; a returned operation is not a working tool
  - The worker leaves and the seat is not reclaimed -> the deprovisioning trigger registered at grant time is what prevents the licence leak
- **Jurisdiction**:
  - US federal: None directly, though software licence compliance is a contractual exposure with real audit consequences.
  - US state variation: None.
  - International: None.

### WF-PRO-002. Deprovision resources when eligibility ends

- **Status**: `NEW`
- **Archetype**: `A15` | **Complexity tier**: 3
- **Actors**: IT or access administrator, manager, finance partner, integration system
- **Trigger**: A role change or departure ends eligibility for resources that continue to cost money.
- **Preconditions**:
  - Every grant carries the condition that made it eligible.
  - Eligibility is re-evaluated on the events that could end it, not only at departure.
  - Reclaimed seats return to the pool.
- **Steps**:
  1. `OBSERVE` (integration system) detect the eligibility-ending event: departure, role change, project end
  2. `CAPABILITY` (IT or access administrator) re-evaluate every grant whose condition referenced the changed fact
  3. `DECISION` (manager) notify the holder before revoking a resource they are actively using, unless the event is a departure
  4. `CAPABILITY` (integration system) revoke and return the seat to the pool
  5. `OBSERVE` (finance partner) confirm the seat is available and the charge has stopped
- **Intents**:
  - `hcmnext.access.deprovision_resource/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: grants and their eligibility conditions
  - `budget`: recurring charges
  - `people`: the changed fact
- **Writes**:
  - `access`: revocations and seat returns
  - `budget`: charge cessation
- **Evidence required**:
  - The eligibility condition each grant carried.
  - Notification before a non-departure revocation.
  - Seat return and charge cessation confirmation.
- **Failure and repair**:
  - A grant has no recorded eligibility condition -> it can never be automatically reclaimed; the gap is a finding and the grant is reviewed manually
  - The seat is returned but the vendor still bills -> the charge cessation observation catches it; vendors often bill to the term end and the finance record must reflect that
  - A worker loses access mid task -> the notification exists precisely for this; a departure is the one case where notice is not given first
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

### WF-PRO-003. Verify readiness before a milestone that depends on it

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 48 feature readiness_verification_conduct
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: HR business partner, manager, IT or access administrator, integration system
- **Trigger**: A milestone such as a start date or a system go-live depends on a set of prerequisites being genuinely satisfied.
- **Preconditions**:
  - Readiness is the set of blocking prerequisites satisfied, not a percentage.
  - Each prerequisite's satisfaction is observed, not self-reported.
  - A partial readiness names exactly what is missing.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the prerequisite set for the milestone
  2. `OBSERVE` (integration system) verify each prerequisite from its owning system rather than from a checklist
  3. `DECISION` (manager) report NOT_READY with the specific gaps rather than a completion percentage
  4. `SIGNAL` (IT or access administrator) escalate each gap to its owner with the milestone date
  5. `END` (HR business partner) close as READY only when the blocking set is empty
- **Intents**:
  - `hcmnext.onboarding.verify_readiness/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: provisioning state
  - `asset`: equipment custody state
  - `onboarding`: prerequisite definitions and their owners
- **Writes**:
  - `onboarding`: readiness determination with the blocking set
- **Evidence required**:
  - Per-prerequisite observation from its owning system.
  - The blocking set, named.
  - Escalation delivery per gap.
- **Failure and repair**:
  - A prerequisite is marked complete by its owner but not observable -> it counts as unverified; self-reported completion is what produces a first day with no laptop
  - A non-blocking prerequisite is missing -> readiness is still true and the item remains an open obligation
  - The milestone arrives NOT_READY -> the milestone proceeds or moves as a business decision, made with the specific gaps in front of the decider
- **Jurisdiction**:
  - US federal: Work authorization and mandatory safety training are legally blocking prerequisites, not soft ones.
  - US state variation: State-mandated notices at hire are blocking in the sense that their absence is a violation from day one.
  - International: In the EU, the written statement of terms is due within seven days of the first day, making it a near-term blocking item.

---

## CBI. Cross-domain business capabilities

3 workflows.

### WF-CBI-001. Submit and track a cross-domain change request

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 49 Higher-Order Business Capabilities feature change_request_submit_and_track; planning/plan.md section 6.2 HCM Change Request
- **Archetype**: `A20` | **Complexity tier**: 3
- **Actors**: manager, HR business partner, employee, auditor, integration system
  - performing a step: manager, HR business partner, auditor, integration system
  - participating without owning a step: employee
- **Trigger**: A business change spans several domains and is tracked as one request with bound children.
- **Preconditions**:
  - The parent binds the exact child set, their versions and their execution order.
  - Child truth is never overwritten by the parent's summary.
  - The tracked state is five-dimensional, not a single status.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the change type and its required child set
  2. `CAPABILITY` (HR business partner) simulate every child and assemble the parent proposal
  3. `APPROVAL` (manager) approvals resolve against the parent's bound digest
  4. `CAPABILITY` (integration system) execute children in the declared order, each independently addressable
  5. `OBSERVE` (auditor) track each child's own five dimensions
  6. `END` (HR business partner) close only the dimensions that are genuinely closed across all children
- **Intents**:
  - `hcmnext.people.promote_worker/v1` (REAL, creates)
  - `hcmnext.work.approve_proposal/v1` (REAL, advances)
- **Reads**:
  - `governance`: authority for each child type
  - `people`: the subject and every domain the change touches
- **Writes**:
  - `people`: the committed changes per domain
  - `work`: parent proposal and its bound children
- **Evidence required**:
  - The bound child set with versions and order.
  - Per-child outcome and lifecycle dimensions.
  - The parent's own closure, computed from children rather than asserted.
- **Failure and repair**:
  - One child fails after others commit -> no false rollback; the parent reports partial and a repair addresses the failed child
  - The parent is closed while a child's consistency is pending -> REJECT; parent completion cannot overwrite child truth
  - A child's authority differs from the parent's -> each child is authorized independently; a parent approval does not grant child authority
- **Jurisdiction**:
  - US federal: The obligations belong to the children: notice, filing, benefits and access duties each attach where they arise.
  - US state variation: State notice duties attach per change type, so one parent can produce several distinct notice obligations.
  - International: In codetermined jurisdictions a composite change may trigger consultation on some children and not others.

### WF-CBI-002. Apply cross-domain governance to a proposed change

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 49 Higher-Order Business Capabilities feature cross_domain_governance_apply
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: compliance officer, HR business partner, finance partner, integration system
  - performing a step: compliance officer, HR business partner, integration system
  - participating without owning a step: finance partner
- **Trigger**: A proposal is evaluated against authorization, legal, privacy, risk, entitlement and budget together.
- **Preconditions**:
  - Each governance kind is evaluated by its own owner, not by a single monolithic check.
  - A denial from any kind is a denial, and the reason discloses only what the caller may know.
  - The governance snapshot is pinned to the proposal.
- **Steps**:
  1. `CAPABILITY` (compliance officer) evaluate authorization, legal, privacy, risk, entitlement and budget in parallel
  2. `JOIN` (compliance officer) collect all decisions; a single denial is decisive but every result is recorded
  3. `DECISION` (compliance officer) compose obligations from the passing decisions, since a pass can still carry conditions
  4. `CAPABILITY` (integration system) bind the governance snapshot to the proposal digest
  5. `END` (HR business partner) close the evaluation; the proposal proceeds or is refused with its typed reason
- **Intents**:
  - `hcmnext.governance.evaluate_change/v1` (PROPOSED, creates)
- **Reads**:
  - `budget`: authority for the cost
  - `governance`: policy versions for each kind
  - `people`: the subject and the initiator
- **Writes**:
  - `governance`: decision set, composed obligations and the snapshot digest
- **Evidence required**:
  - Per-kind decision with its policy version.
  - Composed obligations from passing decisions.
  - Snapshot digest bound to the proposal.
- **Failure and repair**:
  - One kind is unavailable -> FAIL_CLOSED for that kind; an unavailable legal evaluation is not a pass
  - A pass carries an obligation nobody owns -> REJECT; an obligation without a responsible party is not an obligation
  - The denial reason would disclose something the caller may not know -> the reason is generalized to a non-disclosing form, which the explain_worker_state descriptor already establishes as the pattern
- **Jurisdiction**:
  - US federal: The composed obligations are frequently the statutory ones: notice, filing, training and retention.
  - US state variation: State legal evaluation must be jurisdiction-scoped, so a single legal decision for a multi-state population is wrong.
  - International: GDPR and EU labour law both contribute obligations that a US-only evaluation would miss entirely.

### WF-CBI-003. Simulate and validate a proposal before committing to it

- **Status**: `PARTIAL` - definitions/governance/feature-intent-intake.yaml group 49 feature proposal_simulate_and_validate; planning/plan.md section 5.3 Approval Must Bind to an Immutable Proposal
- **Archetype**: `A9` | **Complexity tier**: 3
- **Actors**: manager, HR business partner, finance partner, integration system
  - performing a step: HR business partner, finance partner, integration system
  - participating without owning a step: manager
- **Trigger**: A change is simulated to produce the immutable proposal that approvals will bind to.
- **Preconditions**:
  - The simulation is deterministic and version-pinned.
  - Every unresolvable domain result appears as a blocking or conditional finding rather than being omitted.
  - The proposal digest is what approvals bind.
- **Steps**:
  1. `CAPABILITY` (HR business partner) run each domain's simulation and collect its result or its finding
  2. `DECISION` (HR business partner) represent an unavailable domain result as a blocking finding; never omit it silently
  3. `TRANSFORM` (integration system) assemble the immutable proposal with its material inputs and expected effects
  4. `CAPABILITY` (integration system) compute the canonical digest under the published canonicalization profile
  5. `END` (finance partner) close the simulation; the proposal is now the thing that gets approved
- **Intents**:
  - `hcmnext.rewards.simulate_compensation/v1` (REAL, reads)
  - `hcmnext.people.promote_worker/v1` (REAL, advances)
- **Reads**:
  - `budget`: authority observations
  - `governance`: rule versions pinned into the simulation
  - `people`: current state across every affected domain
- **Writes**:
  - `work`: immutable proposal revision and its canonical digest
- **Evidence required**:
  - Every domain result or finding, with none omitted.
  - Pinned rule and schema versions.
  - The canonical digest, computed by recomputation rather than trusted from a caller.
- **Failure and repair**:
  - A domain is unavailable -> a blocking finding; a proposal that silently omits payroll impact is worse than no proposal
  - The same inputs produce a different digest on a second run -> the canonicalization profile is defective, which is a platform bug of the first order
  - A caller supplies a digest -> it is recomputed and compared; a supplied digest is never trusted
- **Jurisdiction**:
  - US federal: None directly.
  - US state variation: None.
  - International: None.

---

## CBA. Labor relations and collective bargaining

4 workflows.

### WF-CBA-001. Apply a collective agreement to a proposed change

- **Status**: `EXISTING` - internal/domains/cba package: agreement, unit, membership and applicability semantics
- **Archetype**: `A9` | **Complexity tier**: 4
- **Actors**: HR business partner, compliance officer, manager
  - performing a step: HR business partner, compliance officer
  - participating without owning a step: manager
- **Trigger**: A change to a worker covered by a collective agreement must respect its terms.
- **Preconditions**:
  - The worker's bargaining unit membership is effective-dated and resolvable.
  - The agreement is an immutable revision consumers evaluate as a pinned view.
  - The agreement's terms can be stricter than statute and then govern.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the worker's unit membership at the effective date
  2. `CAPABILITY` (compliance officer) pin the applicable agreement revision
  3. `RULE` (compliance officer) evaluate the proposed change against the agreement's terms, including seniority, notice and pay rules
  4. `DECISION` (compliance officer) block a change the agreement forbids, and require the notice or consultation it mandates
  5. `END` (HR business partner) close the evaluation; the change proceeds through its own workflow with the agreement's obligations attached
- **Intents**:
  - `hcmnext.cba.evaluate_applicability/v1` (PROPOSED, creates)
- **Reads**:
  - `cba`: agreement revisions, units, membership
  - `compensation`: the agreement's pay schedule
  - `people`: the worker's classification and seniority
- **Writes**:
  - `cba`: applicability determination and its obligations
- **Evidence required**:
  - Membership at the effective date.
  - Agreement revision pinned to the evaluation.
  - The obligations the agreement imposes on the change.
- **Failure and repair**:
  - The worker's membership changed during the proposal's life -> the evaluation is redone at revalidation; membership is effective-dated for exactly this reason
  - The agreement is silent on the change -> silence is not permission where the agreement has a management rights clause limitation; the evaluation returns UNKNOWN and routes to labour relations
  - The statute and the agreement disagree -> the more favourable term to the worker generally governs, and the evaluation names both
- **Jurisdiction**:
  - US federal: The NLRA requires bargaining over mandatory subjects including wages, hours and terms of employment; a unilateral change to a mandatory subject during an agreement's term is an unfair labor practice.
  - US state variation: State public-sector bargaining laws vary widely: some states forbid public-sector bargaining entirely while others mandate it; right-to-work statutes in about half the states affect membership and dues.
  - International: In the EU, collective agreements are often erga omnes within a sector and bind employers who never signed them; in Germany and the Nordics they set the effective floor rather than the statutory minimum.

### WF-CBA-002. Administer union dues and remittance

- **Status**: `NEW`
- **Archetype**: `A14` | **Complexity tier**: 3
- **Actors**: payroll administrator, external partner or carrier, employee, compliance officer, integration system
  - performing a step: payroll administrator, external partner or carrier, compliance officer, integration system
  - participating without owning a step: employee
- **Trigger**: Dues are deducted under a valid authorization and remitted to the union.
- **Preconditions**:
  - A signed dues authorization exists with its revocation terms.
  - The deduction is calculated per the agreement, which may be a percentage rather than a flat amount.
  - Remittance carries a member list, not just a total.
- **Steps**:
  1. `CAPABILITY` (compliance officer) verify the authorization and its revocation window
  2. `CAPABILITY` (payroll administrator) compute the deduction per the agreement's formula
  3. `CAPABILITY` (external partner or carrier) deduct in the period and remit with the member detail
  4. `OBSERVE` (integration system) confirm the remittance was received and reconciled by the union
  5. `DECISION` (compliance officer) on revocation, stop the deduction from the period the revocation window permits, not immediately
- **Intents**:
  - `hcmnext.cba.administer_dues/v1` (PROPOSED, creates)
- **Reads**:
  - `cba`: agreement dues terms, membership
  - `documents`: the authorization artifact
  - `payroll`: earnings basis for a percentage calculation
- **Writes**:
  - `payroll`: dues deduction lines
  - `settlement`: remittance with member detail
- **Evidence required**:
  - Signed authorization with its revocation terms.
  - Deduction computation per period.
  - Remittance reconciliation with the union.
- **Failure and repair**:
  - A worker revokes outside the permitted window -> the deduction continues until the window opens; the revocation is recorded with its effective date
  - The remittance total and the member detail disagree -> the remittance is held; a union cannot credit members from a total alone
  - A deduction is taken with no valid authorization -> an immediate correction and refund; an unauthorized dues deduction is an unfair labor practice and a wage claim
- **Jurisdiction**:
  - US federal: Section 302 of the Labor Management Relations Act requires a written, revocable dues authorization; Janus v AFSCME bars dues deduction from non-consenting public employees.
  - US state variation: Right-to-work states forbid requiring membership or fees as a condition of employment; the states differ on the mechanics of revocation windows and several have legislated maintenance-of-membership limits.
  - International: In much of the EU, dues are usually collected by the union directly rather than through payroll, so the deduction workflow may be inapplicable.

### WF-CBA-003. Run a grievance through the contractual steps

- **Status**: `EXISTING` - planning/workflows/discovery-backlog.md row ResolveGrievance; internal/domains/cba package
- **Archetype**: `A12` | **Complexity tier**: 4
- **Actors**: employee, HR business partner, compliance officer, external partner or carrier
- **Trigger**: A represented worker files a grievance and it proceeds through the agreement's steps.
- **Preconditions**:
  - The agreement's step sequence and its time limits are configured.
  - Missing a step's deadline has a defined consequence for each side.
  - The union representative's role at each step is recorded.
- **Steps**:
  1. `TASK` (employee) the grievance is filed at the step the agreement specifies
  2. `CAPABILITY` (compliance officer) compute each step's response deadline from the agreement
  3. `TASK` (HR business partner) the employer responds at each step within its deadline
  4. `DECISION` (compliance officer) apply the agreement's consequence for a missed deadline, which may be deemed granted or deemed advanced
  5. `SUBWORKFLOW` (external partner or carrier) route an unresolved grievance to arbitration where the agreement provides it
  6. `END` (HR business partner) close with the outcome and any remedy as its own governed change
- **Intents**:
  - `hcmnext.cba.process_grievance/v1` (PROPOSED, creates)
- **Reads**:
  - `cba`: agreement grievance procedure and time limits
  - `hrcase`: the grievance and its history
  - `people`: the worker and their representative
- **Writes**:
  - `cba`: grievance record, step responses, outcome
  - `hrcase`: case linkage
- **Evidence required**:
  - Each step's filing and response with their timestamps against the deadlines.
  - The representative's participation.
  - The remedy, if any, and the intent that implements it.
- **Failure and repair**:
  - The employer misses a step deadline -> the agreement's consequence applies automatically; deemed-granted clauses are real and the system must not require someone to notice
  - The remedy requires back pay -> it runs through the payroll correction workflow with its own approvals rather than being paid ad hoc
  - The grievance overlaps an investigation of the same facts -> both proceed; a grievance is not a substitute for an investigation and neither disposition binds the other
- **Jurisdiction**:
  - US federal: The NLRA protects the right to file grievances; retaliation for filing is an unfair labor practice, and Weingarten rights attach to investigatory interviews.
  - US state variation: Public-sector bargaining and grievance rights are creatures of state law and differ enormously; some states provide no grievance right at all for public employees.
  - International: In the EU, grievance procedures are commonly set by collective agreement or by statute, and works councils have their own separate consultation channel.

### WF-CBA-004. Support a representation election or a decertification petition

- **Status**: `NEW`
- **Archetype**: `A12` | **Complexity tier**: 5
- **Actors**: compliance officer, HR business partner, manager, employee
- **Trigger**: A petition is filed and the employer must produce a voter list and stay inside the rules.
- **Preconditions**:
  - The bargaining unit's scope determines who is on the list, and the list has a short statutory deadline.
  - Manager conduct during the period is legally constrained in ways ordinary management is not.
  - The employer's own communications are speech with specific limits.
- **Steps**:
  1. `CAPABILITY` (HR business partner) resolve the proposed unit and produce the voter list in the prescribed format and deadline
  2. `DECISION` (compliance officer) refuse to include or exclude workers on any basis other than the unit definition
  3. `TASK` (manager) brief managers on the conduct limits, and record the briefing
  4. `SIGNAL` (compliance officer) issue employer communications only after compliance review
  5. `WAIT` (compliance officer) hold to the election and record the result
  6. `END` (employee) close with the list, the briefings and the communications retained, because they are the record if an objection is filed
- **Intents**:
  - `hcmnext.cba.support_representation_proceeding/v1` (PROPOSED, creates)
- **Reads**:
  - `cba`: existing units and agreements
  - `organization`: supervisory status determinations
  - `people`: workers in the proposed unit, their classifications and contact details
- **Writes**:
  - `cba`: voter list, briefing records, communication log, election result
- **Evidence required**:
  - The voter list as produced, with its deadline and the unit definition it was built from.
  - Manager briefing records, which are the employer's defence against an objection based on supervisory conduct.
  - Every employer communication with its review and its date.
- **Failure and repair**:
  - The voter list is late or incomplete -> the failure can invalidate the election or produce a bargaining order; the deadline is short and the list format is prescribed
  - A manager interrogates or surveils workers about the petition -> an unfair labor practice; the briefing record is what shows the employer instructed otherwise, and the conduct is still attributable
  - A worker's contact details are disclosed beyond the list's prescribed contents -> the disclosure is itself a privacy problem, and the list contents are prescribed rather than discretionary
- **Jurisdiction**:
  - US federal: The National Labor Relations Act governs representation proceedings; the employer must furnish a voter list including specified contact details within a short deadline, and interference, restraint or coercion is an unfair labor practice. Election rules and their timelines have been revised repeatedly and the current procedure should be read from the rule pack.
  - US state variation: Public sector representation is state law and varies from mandatory bargaining to outright prohibition; right-to-work statutes affect membership and dues but not representation itself.
  - International: In the EU, works council elections follow national law with their own lists, timetables and employer duties, and the employer's role is usually more administrative and less adversarial.

---

## CWF. Contingent workforce

4 workflows.

### WF-CWF-001. Engage a contingent worker through a supplier

- **Status**: `PARTIAL` - planning/plan.md section 3.9 product families, contingent workforce
- **Archetype**: `A1` | **Complexity tier**: 3
- **Actors**: hiring manager, HR business partner, finance partner, external partner or carrier, compliance officer, integration system
  - performing a step: HR business partner, finance partner, compliance officer, integration system
  - participating without owning a step: hiring manager, external partner or carrier
- **Trigger**: A non-employee worker is engaged through a staffing supplier with a rate and a term.
- **Preconditions**:
  - The supplier contract and its rate card are current.
  - The classification determination for the engagement has been run.
  - The co-employment risk controls for the jurisdiction are known.
- **Steps**:
  1. `CAPABILITY` (finance partner) resolve the supplier, its contract and the applicable rate
  2. `RULE` (compliance officer) run the classification and co-employment risk evaluation
  3. `DECISION` (compliance officer) refuse an engagement whose terms would make the platform's client a joint employer where that is unacceptable
  4. `APPROVAL` (finance partner) the hiring manager and finance approve the rate and the term
  5. `CAPABILITY` (integration system) create the contingent worker record and its engagement, distinct from an Employment
  6. `SIGNAL` (HR business partner) trigger the limited access provisioning that a non-employee receives
- **Intents**:
  - `hcmnext.people.create_contingent_engagement/v1` (PROPOSED, creates)
  - `hcmnext.regulatory.determine_worker_classification/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: supplier contract and rate card
  - `people`: existing person records for a returning contractor
  - `regulatory`: classification and co-employment rules
- **Writes**:
  - `access`: limited entitlement set
  - `people`: contingent worker and engagement records
- **Evidence required**:
  - Classification determination.
  - Co-employment risk evaluation with its controls.
  - Engagement terms and the supplier reference.
- **Failure and repair**:
  - The engagement is structured like employment -> the classification refuses and the engagement is restructured or converted; proceeding creates back tax and benefit exposure
  - The contingent worker is given employee-equivalent access and supervision -> the co-employment controls flag it; the risk is created by conduct, not by the contract's label
  - The engagement exceeds the tenure limit the policy sets -> the conversion or termination decision is forced rather than allowed to drift
- **Jurisdiction**:
  - US federal: The IRS common-law test and the FLSA economic-reality test both apply; joint employer analysis under the NLRA has swung repeatedly and the current standard should be a versioned rule rather than a constant.
  - US state variation: California AB 5 and its ABC test, plus New Jersey and Massachusetts variants, make many supplier engagements employment as a matter of state law regardless of the contract.
  - International: The UK IR35 rules place the status determination duty on the client; the EU Platform Work Directive establishes a presumption of employment on specified indicators.

### WF-CWF-002. Track contingent tenure and force a conversion decision

- **Status**: `NEW`
- **Archetype**: `A17` | **Complexity tier**: 3
- **Actors**: HR business partner, hiring manager, compliance officer, integration system
- **Trigger**: A contingent engagement approaches a tenure limit that policy or law imposes.
- **Preconditions**:
  - The tenure clock's start, its pauses and its limit are defined.
  - The decision options are convert, end or extend with an approved exception.
  - Drifting past the limit is not one of the options.
- **Steps**:
  1. `CAPABILITY` (integration system) compute tenure against the limit, including any breaks that reset or pause it
  2. `SIGNAL` (hiring manager) notify the hiring manager and HR at the configured lead time
  3. `TASK` (hiring manager) the manager chooses convert, end or request an exception
  4. `APPROVAL` (compliance officer) an exception requires compliance approval with its risk assessment
  5. `DECISION` (compliance officer) on the limit passing with no decision, end the engagement by default rather than continuing
  6. `END` (HR business partner) close with the decision recorded
- **Intents**:
  - `hcmnext.people.evaluate_contingent_tenure/v1` (PROPOSED, creates)
- **Reads**:
  - `people`: engagement history, breaks, prior engagements with the same person
  - `regulatory`: tenure-based classification risk
- **Writes**:
  - `people`: tenure evaluation and the decision
- **Evidence required**:
  - Tenure computation including prior engagements.
  - Notification at the lead time.
  - The decision or the default end.
- **Failure and repair**:
  - Prior engagements are not counted -> the tenure computation must span engagements for the same person, or the limit is trivially evaded and the risk it manages is real
  - An exception is granted repeatedly -> the pattern is surfaced; a permanent exception is a classification finding
  - The engagement continues past the limit with no decision -> the default end fires; drifting is the behaviour that produces misclassification liability
- **Jurisdiction**:
  - US federal: Long tenure is a factor in the common-law control test and in benefit plan eligibility; the Vizcaino v Microsoft line of cases turned on long-tenured contractors being excluded from plans they qualified for.
  - US state variation: State ABC tests do not have tenure limits as such, but long tenure makes prong B harder to sustain.
  - International: In the EU, fixed-term work directives limit successive fixed-term contracts and can convert them to indefinite employment by operation of law after a set number or duration.

### WF-CWF-003. Reconcile supplier invoices against approved contingent time

- **Status**: `NEW`
- **Archetype**: `A18` | **Complexity tier**: 3
- **Actors**: finance partner, hiring manager, external partner or carrier, integration system
- **Trigger**: A staffing supplier invoices for hours that must match approved time and the contracted rate.
- **Preconditions**:
  - Contingent time is approved by the hiring manager before it is invoiceable.
  - The rate on the invoice must match the engagement's contracted rate.
  - Disputes are per line, not per invoice.
- **Steps**:
  1. `OBSERVE` (integration system) receive the invoice and match lines to approved time and engagements
  2. `TRANSFORM` (finance partner) compare hours and rates per line and classify each difference
  3. `DECISION` (finance partner) hold disputed lines and approve the rest rather than holding the whole invoice
  4. `TASK` (external partner or carrier) resolve disputed lines with the supplier
  5. `CAPABILITY` (finance partner) approve payment for the reconciled lines
  6. `END` (hiring manager) close with the dispute pattern available for supplier performance review
- **Intents**:
  - `hcmnext.commercial.reconcile_supplier_invoice/v1` (PROPOSED, creates)
- **Reads**:
  - `commercial`: engagement rate and contract terms
  - `people`: the engagement each line refers to
  - `time`: approved contingent hours
- **Writes**:
  - `commercial`: reconciliation result, approved lines, disputes
- **Evidence required**:
  - Per-line match against approved time.
  - Rate comparison against the contract.
  - Dispute record per line.
- **Failure and repair**:
  - The invoice includes hours never approved -> the line is disputed and held; paying unapproved contingent hours is a common and large leakage
  - The rate exceeds the contract -> disputed with the contracted rate cited; rate creep between renewals is usually invisible without this check
  - A worker on the invoice has no engagement record -> the line is disputed and the shadow engagement becomes a compliance finding, since an unrecorded worker on site is a real risk
- **Jurisdiction**:
  - US federal: Unrecorded contingent workers on site create joint employer, safety and immigration exposure that the invoice reconciliation is often the first place to surface.
  - US state variation: State ABC tests can make an unrecorded supplier worker the client's employee by operation of law.
  - International: The EU Temporary Agency Work Directive requires equal treatment on basic conditions after a qualifying period, which depends on accurate engagement records.

### WF-CWF-004. Engage a services supplier under a statement of work

- **Status**: `NEW`
- **Archetype**: `A1` | **Complexity tier**: 3
- **Actors**: finance partner, hiring manager, compliance officer, external partner or carrier
- **Trigger**: Work is bought as a deliverable from a firm rather than as a person's time.
- **Preconditions**:
  - The statement of work defines deliverables, acceptance criteria and a fixed or milestone price.
  - The supplier controls how the work is done; direction from the client is what converts it into staff augmentation.
  - Supplier personnel are not given employee-equivalent supervision or access by default.
- **Steps**:
  1. `TASK` (finance partner) author the statement of work with deliverables, acceptance criteria and price
  2. `RULE` (compliance officer) test the arrangement against the co-employment and classification indicators before signature
  3. `DECISION` (compliance officer) refuse an arrangement that pays for time under a deliverable label; that is staff augmentation and belongs in the contingent engagement path
  4. `CAPABILITY` (external partner or carrier) provision the narrow access the deliverable requires, scoped and time-bound
  5. `TASK` (hiring manager) accept deliverables against the stated criteria before payment
  6. `END` (finance partner) close on final acceptance with the access revoked
- **Intents**:
  - `hcmnext.commercial.engage_services_supplier/v1` (PROPOSED, creates)
- **Reads**:
  - `access`: entitlement templates for supplier personnel
  - `commercial`: supplier contract, prior statements of work
  - `regulatory`: co-employment and classification indicators
- **Writes**:
  - `access`: scoped, time-bound grants for supplier personnel
  - `commercial`: statement of work, acceptance records, payments
- **Evidence required**:
  - The statement of work with its deliverables and acceptance criteria.
  - The classification and co-employment evaluation at signature.
  - Acceptance records preceding each payment.
- **Failure and repair**:
  - The engagement pays by the hour with no deliverable -> REJECT and route to the contingent engagement path; a time-and-materials arrangement mislabelled as a deliverable is the pattern regulators look for
  - Client managers direct the supplier's personnel day to day -> the co-employment indicators trip and the arrangement is re-evaluated; the risk is created by conduct, not by the contract's words
  - Supplier personnel receive standing employee-equivalent access -> the access is refused; a scoped, time-bound grant is what a deliverable requires
- **Jurisdiction**:
  - US federal: The IRS and FLSA tests apply to the individuals performing the work, and a services contract does not immunize the client if the conduct looks like employment; joint employer analysis under the NLRA reaches the same conduct.
  - US state variation: California AB 5 has a business-to-business exemption with a long list of conditions, all of which must be met; New Jersey and Massachusetts apply their own tests, and failing them makes the supplier's personnel the client's employees by operation of law.
  - International: The UK IR35 rules apply to individuals working through intermediaries and put the status determination on the client; the EU Platform Work Directive presumes employment on specified indicators.
