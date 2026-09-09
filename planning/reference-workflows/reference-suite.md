# Reference Workflow Integration Suite

Extracted from the Human Capital Management Suite architecture constitution so this contract can evolve independently. The master delivery scope remains governed by [../execution-plan.md](../execution-plan.md).

### 7.5 Reference Workflow Integration Suite

Human Capital Management Suite should use five end-to-end worker-lifecycle workflows as architectural integration tests. They are reference specifications and validation scenarios, not a commitment to implement all five during the initial ChangeOps phase.

| #   | Reference workflow                 | Architectural purpose                                                           |
| --- | ---------------------------------- | ------------------------------------------------------------------------------- |
| 1   | Recruit → Hire → Onboard           | Creates the worker and crosses nearly every foundational domain                 |
| 2   | Promotion + Compensation Change    | Tests proposals, approvals, money, effective dating, decisions, and analytics   |
| 3   | Cross-Org / Cross-Company Transfer | Tests corporate scope, employment, currency, legal, payroll, IAM, and residency |
| 4   | Leave of Absence → Return to Work  | Tests long duration, sensitive data, time, benefits, payroll, and policy change |
| 5   | Termination → Offboarding          | Tests high-risk ordering, irreversible effects, security, and repair            |

Together they exercise:

```text
Recruiting     People Core      Organization     Position
Compensation   Payroll          Benefits         WFM / Time
IAM            Learning         Talent           Communications
Documents      Legal            Privacy          AuthZ
Agents         Workflow Runtime Ledger           Analytics
Billing        Integrations     Repair            Reconciliation
```

#### Preserved Legacy Conformance Fixtures

Four smaller scenario families and two external fakes are retained from the
pre-restructure implementation as focused regression fixtures:

| Fixture               | Contract pressure                                                                                                                                                  |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Legal-name change     | Workflow-native mutation, self-service scope, evidence, self-approval denial, idempotency, optimistic concurrency, deterministic planning, outbox, ledger timeline |
| Headcount requisition | Ordered leadership approvals followed by five parallel tasks, three-of-five quorum, Finance and Medical Director veto, terminal cancellation of remaining tasks    |
| Organization transfer | Current and proposed manager/team/location/cost-center scope, source and destination visibility, relationship-aware authorization                                  |
| Employee termination  | Deterministic preflight, optional non-blocking AI review, high-risk approval, employment mutation, payroll/benefits effects, and full timeline                     |
| Compensation fakes    | Typed external dependency, mapping, timeout, retry, redrive, observation, and reconciliation behavior                                                              |

```text
legal-name fixture ---------> smallest transaction regression
headcount fixture ----------> approval resolution/gate regression
organization transfer -----> graph and scope regression
termination fixture -------> high-risk effect regression
compensation fakes --------> integration/recovery regression
             \                  |                  /
              +-----------------+-----------------+
                                |
                  shared kernel conformance
```

These fixtures do not add Phase 1 product workflows. They remain compact
tests for shared behavior while Promotion + Compensation Change is the only
fully executable Phase 1 reference workflow. Their documented pass status must
be refreshed through executable tests before it is cited as current evidence.

#### Single-Domain and Cross-Domain Contrast Fixtures

Two additional conformance fixtures make the composition boundary explicit:

| Fixture                                               | Purpose                                                                                                                                                                      | Phase depth                |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| [Manager Change](manager-change.md)                   | Smallest useful single-domain authoritative mutation; proves that governance, workflow, effective dating, conflicts, commit, observation, and reconciliation remain required | Design/conformance only    |
| [Promote Into Management](promote-into-management.md) | One parent intent with a multi-domain authoritative core and independently reconciled payroll, IAM, talent, learning, document, and messaging effects                        | Promotion Gate A/B fixture |

`Single-domain` means one domain owns the authoritative business mutation. It
does not mean bypassing the shared kernel. `Cross-domain` means one customer
intent composes separately owned capabilities; it does not authorize a giant
service, shared database writes, or distributed ACID across providers.

The suite must evaluate more than the happy path. Each workflow includes concurrency, future-effective execution, partial external failure, cancellation, correction, policy change, access boundaries, replay, cost, and reconciliation scenarios.

#### Reference Workflow 1: Recruit → Hire → Onboard

The lifecycle begins with approved workforce demand rather than the final Hire action.

```text
Position needed
  -> requisition created
  -> budget and headcount approval
  -> job published
  -> candidate applies
  -> screen and interviews
  -> candidate selected
  -> offer generated and approved
  -> offer accepted
  -> pre-employment obligations satisfied
  -> Person resolved or created
  -> Worker created
  -> Employment created
  -> compensation established
  -> payroll and benefits initialized
  -> IAM and equipment provisioned
  -> schedule, location, and learning assigned
  -> onboarding reconciled and completed
```

The identity progression is explicit:

```text
Person
  +-- Candidate identities and application history
  +-- Worker identity
  +-- Contractor or contingent identities
  `-- One or more Employment relationships
```

Candidate, Person, Worker, and Employment are not interchangeable. One person may apply repeatedly, work as a contractor, become an employee, leave, return, or hold multiple employments.

A representative event path includes:

```text
PositionOpened
RequisitionCreated
RequisitionApproved
CandidateApplied
CandidateSelected
OfferProposed
OfferApproved
OfferAccepted
PreEmploymentChecksSatisfied
PersonMatchedOrCreated
WorkerCreated
EmploymentCreated
CompensationEstablished
PayrollEnrollmentRequested
IdentityProvisioningRequested
LearningAssigned
OnboardingCompleted
```

An agent may turn an instruction such as “Hire Jennifer into Miami Engineering as Senior Engineer on September 15 at 145,000 USD” into a governed `HirePlan`. It discovers the position, headcount, band, legal entity, work location, approval policy, offer template, legal checks, IAM profile, and learning obligations. It does not create worker rows directly.

This reference workflow must prove:

- Person resolution, uncertain matches, explicit merge, non-merge, and separation
- Position capacity, job sharing, FTE, seasonal occupancy, reservation, and vacancy semantics
- Consistency between accepted offer, worker, employment, compensation, and start date
- Withdrawal, rescission, no-show, delayed start, and pre-start cancellation without erasing history
- Minimal candidate-data conversion and lawful retention of data that should not enter the worker record
- Parallel onboarding with domain-specific completion and reconciliation
- Partial success such as IAM provisioned while payroll enrollment remains blocked
- Duplicate or repeated hire commands do not create duplicate people, employments, identities, usage, or charges

#### Reference Workflow 2: Promotion + Compensation Change

This is the primary reference for proposal, approval, decision, and effective-dated transaction integrity.

```text
Manager intent
  -> promotion eligibility
  -> performance and talent context
  -> position and job validation
  -> compensation simulation
  -> budget validation or reservation
  -> legal and pay-policy evaluation
  -> approvals
  -> employee notice and documents
  -> execution-time revalidation
  -> job + compensation + related state changes
  -> payroll, IAM, learning, and talent effects
  -> reconciliation and outcome tracking
```

A human description such as “Promote Sarah to Staff Engineer with a 12% increase” may imply:

```text
job level        L4 -> L5
base pay         130,000 -> 145,600 USD
pay band         Band 4 -> Band 5
bonus target     10% -> 15%
position         possibly changes
entitlements     may change
talent profile   records the approved progression
```

Agent analysis may combine permitted compensation, band, peer, performance, tenure, skills, criteria, budget, and manager-observation data into a recommendation. The recommendation records evidence, uncertainty, visible inputs, model provenance, required approvals, and policy constraints. It remains separate from the deterministic transaction.

Approval never eliminates execution-time validation:

```text
proposal approved August 12
  -> worker transfers August 20
  -> promotion effective September 1
  -> revalidate current and future state
     job and position validity
     compensation band
     approver authority
     budget and reservation
     legal and notice requirements
     conflicting transactions
  -> execute, return for reapproval, supersede, or reject
```

This reference workflow must prove:

- Immutable proposal revisions and approval binding
- Context and authority invalidation
- Future-dated write-set conflict detection
- Compensation, bonus, band, and payroll effective dating
- Budget validation and reservation expiry
- Legal notice, localized documents, and current-policy revalidation
- Atomic business intent across job and compensation with independently reconciled external effects
- Decision Ledger input snapshots, agent recommendation provenance, human decision, and outcome linkage

#### Reference Workflow 3: Cross-Org / Cross-Company Transfer

The transfer is the broadest structural stress test. A move from a US company to a Colombia company may change company, legal entity, organization, location, manager, currency, payroll, benefits, schedule, access, documents, and legal obligations.

```text
Transfer proposed
  -> source and destination employment analysis
  -> destination position and headcount reservation
  -> LegalContext and transfer obligations
  -> immigration and work-authorization evaluation
  -> compensation and FX plan
  -> payroll and tax transition plan
  -> benefits transition
  -> IAM continuity and entitlement delta
  -> schedule, calendar, learning, and communication plan
  -> approvals and employee agreements
  -> sequenced effective execution
  -> cross-domain reconciliation
```

The person may persist while employments change:

```text
Person Jane
  +-- US Employment       ends September 30
  `-- Colombia Employment begins October 1
```

This is not always one employment updated in place. The `TransferPlan` must resolve the appropriate domain semantics from employment law, contract, customer policy, and legal-entity configuration.

Financial context preserves original and reporting values:

```text
old compensation       145,000 USD
new compensation       560,000,000 COP
reporting equivalent   calculated USD value
FX method              CompensationPlanningRate.v5
rate date and purpose  preserved
```

This reference workflow must prove:

- Person continuity across sequential or simultaneous employments
- Company, legal entity, organization, location, and payroll distinctions
- Parent, source-company, destination-company, and aggregate-only authority
- Currency, FX, rounding, pay-band, and reporting reproducibility
- Localized agreement, notice, calendar, and jurisdiction requirements
- Data residency and cross-border processing restrictions
- Payroll, tax, benefits, IAM, WFM, and learning transition semantics
- Ordered end/start actions without an accidental employment gap or overlap
- Corporate identity continuity while local entitlements are revoked and granted
- Intercompany cost attribution and historical known-versus-effective reporting

This workflow is a candidate for the first major post-V0 architectural expansion because it forces the shared model to mature rather than adding an isolated feature.

#### Reference Workflow 4: Leave of Absence → Return to Work

Leave is the primary test of long-lived policy-sensitive workflow and confidential domain separation.

```text
Leave requested or initiated
  -> eligibility and overlapping-program evaluation
  -> confidential documentation collection
  -> legal entitlement and policy determination
  -> certification, approval, or acknowledgement
  -> schedule and time-balance effects
  -> pay treatment
  -> benefits continuation
  -> manager coverage and minimum-necessary notice
  -> IAM effect if any
  -> leave begins
  -> periodic deadlines and evidence
  -> return-to-work preflight
  -> fitness, accommodation, or modified-schedule workflow if required
  -> payroll, benefits, schedule, and access restoration
  -> reconciliation and close
```

The same case exposes different permitted views:

```text
manager               availability and approved dates
leave administrator   certification and protected-leave details
payroll               pay treatment
benefits               continuation and eligibility
worker                 own case and required actions
agent                  minimum authorized subset for declared purpose
```

Medical documentation, diagnosis, accommodation, and Employee Relations data never become ordinary manager-visible leave fields.

An agent may explain potentially applicable programs and missing information from authorized facts. The Legal Plane resolves actual obligations, lawful processing, notices, deadlines, and human-review requirements.

This reference workflow must prove:

- Sensitive storage and field/domain isolation
- Minimum-necessary and purpose-bound disclosure
- Concurrent and overlapping leave, absence, disability, and accommodation programs
- Schedule, balance, pay, benefit, payroll, and service-credit interactions
- Jurisdiction, business-calendar, rolling-period, and deadline calculations
- Months-long waits, reminders, escalations, and safe-point intervention
- Pinned workflow semantics with current-law and current-authorization revalidation
- Return conditions, modified work, partial return, extension, cancellation, and failure to return
- Restoration and reconciliation rather than treating return as one `leave.end` event

#### Reference Workflow 5: Termination → Offboarding

Termination is a high-risk distributed transaction with several differently timed effects.

```text
Termination intent
  -> reason and risk classification
  -> Legal / Employee Relations review when required
  -> final approval and step-up authentication
  -> final-pay and benefit simulation
  -> access-removal and equipment plan
  -> employee notice and document plan
  -> scheduled effective actions
  -> employment end
  -> payroll finalization
  -> benefits transition
  -> IAM and physical-access removal
  -> schedule and organization updates
  -> equipment and downstream actions
  -> reconciliation, incidents, repair, and retention
```

The `TransactionPlan` supports ordered effective actions:

```text
privileged access removal   16:59 local business time
employment termination      17:00
general access removal      17:00
final payroll               applicable payroll window
benefits transition         applicable statutory or plan date
```

An agent may analyze, prepare, and simulate a `TerminationPlan` under A3 authority. Critical approval and execution may require authorized HR and Legal actors, separation of duties, dual approval, and step-up AuthN.

Business completion is distinct from external consistency:

```text
employment terminated       complete
payroll finalized           complete
benefits transitioned       complete
building access removed     complete
GitHub access removed       failed

ExecutionState        COMMITTED
BusinessState         COMPLETED
ConsistencyState      DEGRADED       (RepairPlan linked)
ObligationState       PENDING        (security incident open)
```

The remaining failure produces a bounded RepairPlan rather than relabeling the entire termination as ambiguously failed.

An erroneous termination is corrected, not deleted:

```text
TerminationExecuted
  -> ErrorDiscovered
  -> TerminationCorrectionApproved
  -> EmploymentReinstated
  -> payroll and benefit repairs
  -> IAM reprovisioning
  -> reconciliation
```

This reference workflow must prove:

- Security-critical ordering and precise effective moments
- High-risk agent, bulk, break-glass, and approval controls
- Final pay, benefit continuation, documents, legal review, and retention
- Irreversible versus compensatable side effects
- Business completion, external consistency, and reconciliation as separate state dimensions
- Security incident creation and repair deadlines
- Reinstatement, correction, and evidence preservation
- Mass-termination blast-radius limits, staged execution, kill switches, and aggregate incidents

#### Sixth Infrastructure Stress Test: Payroll Correction / Retroactive Pay Repair

Payroll correction is not one of the five lifecycle workflows, but it is the mandatory data-integrity and repair torture test.

```text
Discrepancy detected
  -> expected state reconstructed
  -> causal transaction identified
  -> effective versus known-at-the-time state compared
  -> payroll delta calculated
  -> retro-pay, tax, deduction, and accounting effects simulated
  -> approval
  -> corrective payroll and accounting actions
  -> external observations
  -> reconciliation and worker explanation
```

It must prove bitemporal reconstruction, reducer and policy versioning, monetary precision, append-only correction, compensation, tax and deduction dependencies, provider idempotency, repair approval, and complete reconciliation.

#### Shared Platform Primitives Revealed

The suite should converge on reusable operations:

```text
PERSON       resolve / create / merge / separate
EMPLOYMENT   create / modify / suspend / reactivate / end / reinstate
POSITION     create / reserve / fill / vacate / close
CHANGE       propose / validate / simulate / approve / execute / supersede
TIME         effective date / schedule / deadline / business calendar
WORKFLOW     start / transition / pause / resume / override / repair / migrate
AUTHORITY    check / explain / delegate / step up / break glass
LEGAL        resolve context / evaluate / inject obligations
DECISION     record / review / supersede / link outcome
INTEGRATION  request / observe / retry / reconcile
REPAIR       diagnose / plan / simulate / approve / execute / verify
```

If these primitives are stable across all reference workflows, later worker journeys should be compositions rather than unrelated architectural inventions.

#### Foundational Questions the Suite Must Close

The reference suite makes twelve issues explicit acceptance criteria:

1. **Person identity and deduplication** — candidates, contractors, workers, and former workers share a governed human-identity layer.
2. **Temporal Employment** — one person can hold simultaneous and sequential employments without corrupting history.
3. **Position and headcount accounting** — positions carry capacity, FTE, budget, occupancy, reservation, and temporal state.
4. **Cross-workflow conflict control** — affected resources, fields, and effective time ranges detect collisions among promotion, transfer, leave, and other work.
5. **Proposal and approval binding** — approval attaches to an exact immutable proposal and material context.
6. **Execution-time revalidation** — approved work is rechecked against current authority, law, policy, state, budget, and conflicts.
7. **Ordered distributed side effects** — the plan declares parallel, sequential, atomic, reversible, compensatable, and irreversible work.
8. **Multidimensional completion** — business completion, external consistency, reconciliation, and operational incident state are independent.
9. **Sensitive compartments** — medical, Employee Relations, payroll, bank, immigration, investigation, and identity-secret data receive stronger isolation.
10. **Long-lived policy evolution** — pinned workflow behavior coexists with current-law, current-authorization, and critical-state revalidation.
11. **Enterprise identity lifecycle** — human, employment, workforce identity, account, entitlement, and credential lifecycles remain related but distinct.
12. **Outcome model** — intent, recommendation, decision, transaction, result, and later outcome remain linkable without claiming unsupported causality.

The reference suite is complete only when these questions have explicit, testable platform contracts rather than workflow-specific exceptions.
