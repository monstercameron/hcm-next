# Adversarial Business Model Audit — 2026-08-14

## Scope and method

Thirty-two independent `gpt-5.6-luna` adversarial passes reviewed the entity
catalog against the workflow research and the 530-number BusinessIntent
vocabulary. Reviewers were instructed to find negative cases, missing entities,
unsafe implicit assumptions, temporal/authority/correction gaps, aggregate
boundary errors, privacy failures, external ambiguity, and incomplete repair
semantics. They did not decide product priority or imply Phase 1 implementation.

The passes covered:

|   # | Adversarial focus                                                 |
| --: | ----------------------------------------------------------------- |
|   1 | Intent/transaction/workflow/governance/evidence kernel            |
|   2 | Person identity resolution, merge and separation                  |
|   3 | Worker, employment, assignment and temporal history               |
|   4 | Organization, job, position, capacity and headcount               |
|   5 | Compensation, merit, budgets and pay equity                       |
|   6 | Payroll, retro, correction, payment and accounting                |
|   7 | Tax, jurisdiction, regulatory rules and government filing         |
|   8 | Benefits, elections, dependents, continuation and carriers        |
|   9 | Time, attendance, schedules, overtime and labor allocation        |
|  10 | Leave, accommodation and return to work                           |
|  11 | Recruiting, screening, interviews, checks and offers              |
|  12 | Onboarding, termination, offboarding and reinstatement            |
|  13 | Workforce IAM, devices, assets and physical access                |
|  14 | Goals, performance, talent, succession and mobility               |
|  15 | Learning, skills, credentials and certifications                  |
|  16 | Messaging, surveys, announcements and recognition                 |
|  17 | HR service catalog, cases and policy answers                      |
|  18 | Employee relations, investigations, discipline and legal hold     |
|  19 | Global mobility, immigration, tax residence and PE risk           |
|  20 | Safety, injury, workers compensation and reporting                |
|  21 | Privacy, DSAR, consent, transfers, retention and deletion         |
|  22 | Documents, forms, extraction, redaction and signatures            |
|  23 | Workflow runtime, human work and approvals                        |
|  24 | Connectors, schemas, mappings, DataOps and configuration          |
|  25 | AuthN/AuthZ, delegation, secrets, keys, DLP and zero trust        |
|  26 | Reports, metrics, analytics, disclosure and explanation           |
|  27 | Agents, RAG, taint, models, tools, memory and AI incidents        |
|  28 | Reconciliation, repair, incidents, cells, overload and recovery   |
|  29 | Billing, tenant lifecycle, sandboxing and triggers                |
|  30 | Holistic coverage across all 530 numeric intents                  |
|  31 | Schema integrity, wire types, ownership and aggregate consistency |
|  32 | Final holistic conceptual coverage and negative dimensions        |

## Refinement results

Material findings were incorporated into the catalog rather than left only in
review notes. The refinements include:

```text
IntentResult and exact result revisions
CapabilityDefinition/Version/Invocation and implementation authority routing
Multi-stream participants, stream heads, commit ambiguity and receipts
Field-level authority sources/bindings and authority handoff fencing
Typed reservation and assertion-correction protocols
Explicit evaluation composition across law, policy, CBA and contracts
Durable workflow attempts/interventions/migrations/replay and human-work clocks
Person merge/separation plans, redirects, roles and representation
Temporal employment transitions, snapshots, timelines and export manifests
Position revisions/capacity conservation/reorganization dispositions
Payroll ledger/correction/accounting/payment/retro traces
Jurisdiction contexts, rule releases, filing packages and coverage manifests
Entitlement ledgers for benefits, time and leave
Recruiting revisions, external publication, screening and offer evidence
IAM desired state, operation attempts, physical access and device custody
Case compartments, privilege, custody, appeals and non-retaliation
Mobility legs/workdays, immigration applications and tax-residence evidence
Document byte/artifact lineage, quarantine, redaction proof and signer resolution
Messaging per-recipient evidence, requirements, fallback and survey anonymity
DataOps staging/cells, schema/reference/configuration publication and rollback
Agent trust/taint, retrieval, provider eligibility, memory and tool validation
Production cells, overload/retry budgets, recovery, telemetry and supply chain
Concrete wire primitives for Protobuf, Go and SchemaFlux
Initial aggregate/lifecycle registries and coverage-checker rejection rules
Explicit consistency boundaries and aggregate-to-lifecycle assignments
```

The final holistic reviewer found two remaining conceptual omissions:
`IntentResult` and `OrganizationImpactAnalysis`. Both were added. A repeat pass
then returned `CLEAN`. The final schema-integrity reviewer identified capability,
wire, transaction-participant, authority, reservation, correction, lifecycle and
alias gaps. Repeat passes then found missing lifecycle assignments and several
non-canonical entity names in those assignments. Reusable lifecycle policies,
an exhaustive aggregate lifecycle registry and canonical entity references were
added. The final schema-integrity repeat pass returned `CLEAN`.

## Closure status

```text
Conceptual domain breadth:              CLOSED for exploratory modeling
Novel material conceptual gaps:        none reported by both final rechecks
Exact 530-name source ingestion:        PENDING
Exact property-level intent bindings:   PENDING
Machine-readable SchemaFlux registries: PENDING
Coverage checker and generated report:  PENDING
Runtime/database implementation:        OUT OF SCOPE for this research pass
```

The pending items are deliberately not hidden behind a “complete” claim. The
catalog can conceptually represent all numbered intent families, but
`530/530 VERIFIED` is forbidden until the artifacts specified in
[Registry and coverage contracts](registry-and-coverage-contracts.md) are
instantiated and checked.

## Negative dimensions required by every future binding

```text
missing, unknown, stale, redacted and unavailable input
concurrent conflict and stale baseline
authorization deny/restrict and legal prohibition/obligation
approval invalidation and approver authority churn
cancellation before and after irreversible effects
duplicate, replay, retry exhaustion and ambiguous commit
external partial success and authority disagreement
observation lag, degraded consistency and repair verification
correction, supersession, appeal and dispute
retention, legal hold, DLP, egress and tenant boundary
effective-date, known-at, source-clock and policy-version evolution
provider, region, cell, identity, key and trusted-time outage
accessibility, representation, delegation and human escalation
```
