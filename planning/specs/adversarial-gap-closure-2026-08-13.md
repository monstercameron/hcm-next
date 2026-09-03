# Adversarial Gap Analysis and Closure Contract — 2026-08-13

## Method boundary (added 2026-09-02)

This document is a frozen input under the same closure policy as the
[2026-08-14 audit](adversarial-audit-32-reviewers-2026-08-14.md): findings
close by test, explicit deferral, or not-applicable; no further audit passes
run until P1A executes; findings on the legacy runtime or on deferred systems
are not on the P1A/P1B list. Where this document's dispositions say "closed
contractually below," read that as `DEFERRED` to the gate that owns the
system unless a P1A/P1B acceptance bullet names it.

## Scope and method

This review covers the post-extraction planning and schema corpus. It searches for
more than missing components:

```text
omission          required responsibility absent
contradiction     two contracts require incompatible behavior
false maturity    prose or metadata claims more than artifacts prove
unsafe default    failure/unknown state can become authority or success
implementation bug contract cannot be encoded or executed as written
operability gap   no owner, queue, evidence, recovery or support path
scope failure     commitments exceed staffing/time/product evidence
abuse/privacy     design enables misuse, surveillance or unreviewable harm
economic failure cost or complexity can exceed removed customer burden
testability gap   claim has no executable acceptance evidence
drift risk        code, schemas, vendors, models, policies or docs can diverge
```

The review does not claim that no future gap can exist. Closure means a material
issue has an owner, contract, phase decision, failure behavior, evidence, and test;
it does not mean every long-term subsystem is implemented.

## Findings register

| ID      | Severity | Finding                                                                                                                                                                               | Disposition                                                                                                                         |
| ------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| AGA-001 | Critical | Coverage language allowed roughly 48 systems to appear like simultaneous Phase 1 deliverables despite a 7–8 person Gate A                                                             | Closed by the scope-budget gate below; matrix depth is not staffing authority                                                       |
| AGA-002 | High     | The plan called all 530 proposed intent names `CATALOGUED`, but only 14 exist in repository definition sources                                                                        | Corrected: 14 draft definitions, 516 awaiting lossless ingestion                                                                    |
| AGA-003 | High     | Draft intent definitions were marked `CONTRACTED` while referenced Protobuf schemas, capabilities, rules and tests do not resolve                                                     | Corrected with `DRAFT_CONTRACT`; publication rejects it                                                                             |
| AGA-004 | High     | `IntentInstance` omitted execution mode, concurrency revision, recorded/transition time and completion dimensions used by workflow plans                                              | Corrected in the canonical Protobuf source                                                                                          |
| AGA-005 | High     | No unified worker decision-rights/contestability contract existed for adverse or high-impact human/AI-assisted outcomes                                                               | Closed contractually below; Phase 1 implements decision notice/explanation/correction route for its scope                           |
| AGA-006 | High     | Outcome tracking was named but lacked causal-claim, measurement, dispute and anti-surveillance semantics                                                                              | Closed contractually below; broader causal evaluation remains deferred                                                              |
| AGA-007 | High     | Degradation modes existed, but manual continuity could create unaudited shadow truth or duplicate effects                                                                             | Closed contractually below; Phase 1 requires a bounded manual reconciliation drill                                                  |
| AGA-008 | High     | Retry and quarantine existed in several planes without one poison-work/dead-letter ownership contract                                                                                 | Closed contractually below; every exhausted item becomes owned work, incident or governed discard evidence                          |
| AGA-009 | Critical | Go/GWC/grpcbridge/SchemaFlux and Protobuf generation are not yet one reproducible, pinned root build                                                                                  | Open implementation gate; toolchain bootstrap contract below blocks Gate A implementation claims                                    |
| AGA-010 | High     | SchemaFlux compiler responsibilities are architectural claims not yet proven by an executable repository pipeline                                                                     | Open implementation gate; capability assessment and deterministic offline fixture required                                          |
| AGA-011 | High     | Agent/model drift and infrastructure control drift were dispersed rather than tied to promotion/quarantine gates                                                                      | Closed contractually below; implementation remains phase-scoped                                                                     |
| AGA-012 | High     | Named role ownership appeared in prose, but most specifications lacked canonical owner/status/review metadata and no accountable person/on-call record was required before activation | Role metadata closed in `specification-ownership-registry.md`; named staffing remains an activation blocker                         |
| AGA-013 | Medium   | The architecture atlas and master catalog duplicate concepts and can drift despite normative hierarchy                                                                                | Existing documentation-integrity control retained; generated inventory/diff remains an implementation gate                          |
| AGA-014 | Medium   | Customer rollout, training, process adoption and bypass behavior are weaker than technical execution detail                                                                           | Added to Phase 1 operational readiness and pilot scorecard below                                                                    |
| AGA-015 | Medium   | Vendor compatibility can be overstated at vendor-logo level                                                                                                                           | Already closed by connector maturity/version scope; negative certification tests remain required                                    |
| AGA-016 | Medium   | Long-term payroll/WFM breadth can imply parity without domain operating evidence                                                                                                      | Already closed by authority-expansion disposition and company-scale gates                                                           |
| AGA-017 | High     | Charters, coverage rows, specifications, schemas and tests lacked a mandatory crosswalk, so requirements could become orphaned or contradicted without detection                      | Closed contractually by the requirement-traceability gate below; the generated trace and orphan checker remain implementation gates |

## 1. Phase 1 scope-budget and stop gate

Coverage status, architectural importance, and staffed delivery are different:

```text
DEFINED contract
      !=
Phase 1 implementation
      !=
Gate A staffed deliverable
      !=
production-ready general subsystem
```

Every Gate A/B requirement must appear in a delivery manifest:

```text
DeliveryItem
  gate
  concrete artifact/test
  named accountable owner
  contributing people
  estimate and dependency
  required implementation depth
  acceptance evidence
  operations/support owner
  scope displaced if added after approval
```

Gate A remains bounded to:

```text
one paid design-partner problem and numeric baseline/target
one Promotion/Compensation intent family
one incumbent read/observe topology
one GWC operator workspace
one deterministic simulation/proposal path
one source-authority/conflict/AuthZ/governance slice
one intended-versus-observed comparison and repair recommendation
one synthetic fixture plus approved partner fixture
one deploy/recovery/security/telemetry path appropriate to read-only authority
```

An architecture row may impose a constraint without becoming a standalone team or
service. If mandatory work cannot fit the approved people/time envelope, the gate
owner must remove equivalent scope or reapprove staffing and schedule. It may not
silently convert a contract into a mocked production dependency.

Before Gate A starts, the partner agreement must define numeric success, failure,
and stop thresholds. Stop/replan conditions include loss of partner/data access,
no auditable baseline, no authorized lawful dataset, inability to produce a safe
read-only topology, no repeated user workflow, or an estimate outside the approved
envelope without displaced scope.

## 2. Catalog and contract-toolchain bootstrap

The target build is not established until one root Go-native command proves:

```text
pin Go toolchain
  -> pin GWC + grpcbridge + SchemaFlux revisions
  -> resolve compatible shared dependencies
  -> pin Protobuf compiler/plugins and descriptor policy
  -> validate SchemaFlux sources offline and deterministically
  -> generate Go registries/contracts/fixtures/docs
  -> detect dirty generated output
  -> compile
  -> unit + conformance + fuzz tests
  -> SBOM + provenance + signatures
```

Required preconditions:

- GWC, grpcbridge and SchemaFlux have a tested Go-version/dependency compatibility matrix.
- Pseudo-versions and local `replace` directives are prohibited in releases unless the replaced source is vendored/signed and its provenance is retained.
- SchemaFlux demonstrates parse, normalize, relate, validate and emit for the bounded intent/capability catalog without an LLM/network call in the trusted build.
- Protobuf generation is deterministic and pins compiler plus Go plugins.
- Generated code is never hand-edited; source/descriptor/generated digests are linked.
- Unknown or unresolved schemas, capabilities, rules, classifications, owners and tests keep definitions below `CONTRACTED`.
- The 516 remaining intent candidates are ingested losslessly or explicitly rejected; a partition completeness test prevents skipped/duplicate numbers.

Until this passes, the repository may contain contract sources but cannot claim a
working SchemaFlux/Protobuf platform pipeline.

## 3. Workforce decision rights and contestability

High-impact workforce decisions include hiring/rejection, promotion/demotion,
compensation, scheduling, leave, discipline, termination, investigations, access,
fraud/risk findings, predictions, and recommendations materially used by humans.

```text
Decision or recommendation
          |
          v
rights resolver by jurisdiction, contract, policy and decision type
          |
   +------+------+------+------+
   v      v      v      v      v
 notice explain access correct human review
          |                     |
          +----------+----------+
                     v
              contest / appeal
                     |
                     v
        affirm | modify | supersede | correct
```

Canonical state:

```text
DecisionRightsContext
DecisionNotice
ExplanationPackage
ContestRequest
ReviewAssignment
ContestDecision
RemedyPlan
NonRetaliationRestriction
RepresentativeOrAccommodation
```

Rules:

- No agent, model, risk score, semantic inference, or fraud detector autonomously performs an adverse employment action.
- The rights resolver states which rights apply; absence of a legal right does not erase customer-policy rights.
- Explanation distinguishes facts, claims, rules, model inference, recommendation, human rationale and unavailable/redacted evidence.
- Contesting does not mutate the original decision. Review affirms, supersedes or appends correction/remedy evidence.
- Reviewer independence, conflicts, deadlines, representation, accessibility, language, confidentiality and non-retaliation are explicit.
- A contest can pause future effects when law/policy requires; irreversible or security-critical effects declare separate emergency behavior.
- Metrics may measure process fairness and error without exposing protected case content or enabling retaliation.

Phase 1 implements notice, visible-input explanation, correction request and a
human review route for Promotion decisions. General appeals/case product depth is
deferred.

## 4. Outcome evaluation and causal-claim safety

An outcome is not proof that a decision caused it.

```text
Intent -> Decision -> Transaction -> Result -> later Observation
                                              |
                                              v
                                      OutcomeEvaluation
```

Every outcome contract declares:

```text
question and success/failure measures
population and observation window
baseline/comparator
source authority and completeness
known confounders and selection effects
privacy/purpose/retention
attribution unit
claim strength
dispute/correction path
```

Claim strength is typed:

```text
DESCRIPTIVE_ONLY
OBSERVED_ASSOCIATION
QUASI_EXPERIMENTAL_ESTIMATE
RANDOMIZED_CAUSAL_ESTIMATE
NOT_EVALUABLE
```

Agents and dashboards may not upgrade association to causation. Small cohorts,
protected traits, sensitive cases and manager-level surveillance require cohort
controls and may be suppressed. Outcome evaluation never grants permission to use
data for a new employment purpose. Subjects and authorized reviewers can dispute
incorrect source facts or methodology; the correction preserves prior evidence.

## 5. Manual continuity and degraded operations

Every critical capability declares both machine degradation and human continuity:

```text
FAIL_CLOSED | QUEUE_FOR_LATER | READ_ONLY | USE_VERIFIED_STALE
DISABLE_AI | MANUAL_CONTINUITY | EMERGENCY_ONLY
```

`MANUAL_CONTINUITY` is a governed mode, not permission to use undocumented
spreadsheets or bypass controls. A continuity package declares scope, activation
authority, start/end time, trusted data watermark, allowed actions, dual control,
numbering/idempotency scheme, evidence capture, secure storage, communication,
re-entry mapping and reconciliation owner.

On recovery:

```text
freeze manual intake
  -> inventory every manual action/artifact
  -> resolve identity and idempotency
  -> simulate canonical re-entry
  -> approve high-risk entries
  -> commit/observe/reconcile
  -> repair conflicts/duplicates
  -> certify closure and destroy temporary copies under records policy
```

Manual actions never receive earlier trusted timestamps merely because they were
performed during an outage. Uncertain time, authority or ordering remains explicit.

## 6. Poison work and dead-letter ownership

Exhausted retries do not disappear into a queue:

```text
retry budget exhausted
       |
       v
QuarantinedWork
  identity + original semantic idempotency key
  tenant/subject/classification
  failure taxonomy and attempts
  ambiguity/side-effect status
  owner + SLA + next action
  payload/artifact reference under least privilege
       |
       +-- redrive after condition/config change
       +-- repair plan
       +-- incident/case
       +-- supersede/cancel
       `-- governed discard with evidence
```

Queues, projectors, workflows, connectors, messages, subscriptions, imports and
batch jobs use the same ownership principle while retaining domain-specific APIs.
Replay uses the original event/effect identity and never invents a new business
event. Poison items contribute to SLO/error budgets and cannot be excluded by
moving them to a dead-letter store.

## 7. Model, policy and infrastructure drift

Three drift families remain separate:

```text
MODEL_DRIFT
  quality, calibration, bias, tool choice, provider behavior, embedding behavior

CONTROL_DRIFT
  deployed infrastructure/security configuration differs from approved state

SEMANTIC_DRIFT
  schema, mapping, rule, capability or vendor meaning changed incompatibly
```

Each has versioned baselines, detectors, evidence freshness, severity, affected-
set calculation, quarantine/rollback, false-positive review, incident linkage and
reactivation gates. Missing evaluation telemetry is `UNKNOWN`, not healthy.

An agent/model/provider cannot remain eligible merely because its endpoint is up.
A configuration cannot remain approved merely because deployment succeeded. A
connector cannot remain certified after an unassessed provider semantic change.

## 8. Accountable ownership and adoption readiness

Role names in specifications are insufficient before activation. Every active
capability/workflow/connector/policy/model/control has:

```text
accountable person or rotation
operational owner and on-call route
security/privacy/domain reviewers
support escalation and customer communication owner
SLO and error budget owner
sunset/deprecation owner
last review and next review deadline
```

Phase 1 additionally requires customer process ownership, training, accessible
instructions, change communications, support routing, bypass detection, adoption
measurement and feedback handling. Technical success with persistent spreadsheet,
email or incumbent-UI bypass is a failed adoption outcome, not a shipped success.

## 9. Requirement traceability and orphan detection

Every normative requirement that is eligible for implementation receives a stable
identifier. A generated `RequirementTrace` relates it to all applicable planning
and executable artifacts:

```text
RequirementTrace
  requirement_id
  charter_ids[]
  coverage_responsibility_ids[]
  specification + section
  phase_depth
  accountable_owner
  schema_or_policy_refs[]
  implementation_refs[]
  conformance_test_refs[]
  evidence_refs[]
  supersedes[]
  status
```

The trace checker fails publication or a phase gate for:

- an active requirement with no coverage row, owner, phase depth or acceptance test;
- a Phase 1 requirement with no executable artifact or explicit contract-only disposition;
- a schema/capability/workflow/configuration artifact with no governing requirement;
- a requirement marked complete whose referenced artifact or test does not exist;
- incompatible requirements that share an authority boundary or semantic object;
- a retired requirement whose live implementation, configuration or documentation remains reachable.

Prose counts are not proof. The trace is generated from repository sources, is
reviewed in change control, and records `UNKNOWN` when a relationship cannot be
established. Initially only Gate A requirements must be fully traced; later-phase
requirements may be `PLANNED_UNTRACED`, but that state cannot pass an activation
gate.

## Negative conformance suite

The release suite must include at least:

1. Catalog/source count and unresolved-reference publication failures.
2. Lifecycle dimension compatibility across intent, workflow, ledger and API views.
3. Two successful preflights with only one valid commit.
4. Partial external success with honest business/degraded/repair states.
5. Retry exhaustion producing owned quarantined work and successful original-ID redrive.
6. Manual continuity action re-entry with duplicate/conflict detection.
7. Adverse recommendation blocked from autonomous execution and routed to human review.
8. Contest/review superseding rather than mutating prior evidence.
9. Association presented without causal language and suppressed unsafe cohorts.
10. Model, control and semantic drift triggering correct affected-set quarantine.
11. Missing owner/on-call/review deadline blocking activation.
12. Gate A scope addition rejected without displaced work or gate reapproval.
13. Orphaned or falsely-complete requirement rejected by the generated trace check.

## Residual risks

The following are intentionally not closed by prose:

- The actual SchemaFlux structured-compiler capability and adapter must be proven in code.
- The root Go/Protobuf generation build does not yet exist.
- The 516 unrecorded intent candidates require mechanical ingestion and review.
- Phase 1 success/stop numbers require a real design-partner contract.
- Regulatory, payroll, WFM, case, appeal and outcome engines remain domain-depth investments.
- Named accountable humans and support rotations do not exist until staffing is assigned.
- The generated requirement trace and orphan checker do not yet exist.

These are release or investment gates, not reasons to expand Phase 1 silently.

## Ownership and status

- Owner: Architecture Council with Product, Security, Privacy, Reliability and Domain review
- Status: contract gaps patched; implementation/evidence gaps explicitly open
- Review trigger: before Gate A staffing, after material architecture additions, and after each reference-workflow or incident finding
