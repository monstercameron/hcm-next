# Normative Workflow Context Contract

This document makes integration, legal, authorization, human-work and
operational responsibilities explicit at workflow boundaries. It complements
the exploratory [workflow context](workflow-context-layers.md) and
[step catalog](step-types.md). It is a target contract, not implementation
evidence.

## 0. Enforcement tiers

Every rule in this document carries one of three tiers. A rule without a tier
marker is Tier 3. Only Tier 1 rules are acceptance criteria for P1A or P1B;
a Tier 2 or 3 rule cannot block a release.

```text
Tier 1  ENFORCED in P1A/P1B      tested before the release ships
Tier 2  GATE C                   contracted; tested before general authority
Tier 3  RESEARCH                 recorded so a later contract has a starting
                                 point; may be simplified or dropped
```

Tier 1 rules, in full:

- §1 boundary invariant: no input is trusted for arriving.
- §2 typed context: undeclared reads fail; masked/stale/unknown is `UNKNOWN`.
- §3 ingress: receipt persisted before acknowledgement; same provider ID with
  different bytes is an incident; `AMBIGUOUS` correlation never advances
  material work.
- §4 mapping: no floats, no server-local time; `EXTERNAL_MASTER` never
  promotes state without an owner observation; timeout-after-send is
  `AMBIGUOUS`.
- §6 authorization: deny dominates; material unknown fails closed;
  `DataAccessManifest` on every rendered field.
- §7 identity and approvals: server-derived principal; decision binds the
  proposal digest and the rendered-projection digest the server issued
  (see the simplified receipt rule in §7); `SAFE_TO_DECIDE` gate.
- §9 runtime: actual reads/writes/effects are declared subsets; pure nodes
  read no ambient state; cancellation fence checked at claim/prepare/commit.
- §10: the five intent dimensions and the `InterventionPreview` for cancel.
- §12 negative families 1, 4, 7, 8, 10, 15, 16, 17.

Everything else is Tier 2 or Tier 3 as marked in place.

## 1. Boundary invariant

No external event, legal result, authorization result, human decision, agent
output, timer, signal or operator command is trusted merely because it reached
the runtime.

```text
untrusted input
     |
     v
typed receipt/assertion
     |
     v
authority + time + scope + provenance resolution
     |
     v
governance decision + enforceable obligations
     |
     v
proposal-bound operation
     |
     v
boundary-specific revalidation
     |
     v
effect -> observation -> reconciliation -> evidence
```

Every material artifact binds tenant, cell, placement epoch, organization scope,
stable identity, schema/version digest, effective and recorded time, authority,
classification, purpose, correlation and causal predecessors. IDs never replace
tenant-scoped authorization or correlation.

## 2. Typed, scoped context

`WorkflowContext` is a collection of references, not a mutable map:

```text
WorkflowContextRef
  tenant_context_ref
  principal_context_ref
  subject_context_ref
  organization_context_ref
  locale_context_refs[]
  jurisdiction_resolution_refs[]
  source_authority_snapshot_ref
  governance_snapshot_ref
  data_access_manifest_ref
  risk_context_ref
  entitlement_context_ref
  execution_context_ref
  integration_context_ref?
  human_interaction_context_ref?
```

Each referenced artifact declares its schema/digest, issuing authority,
effective interval, `known_at`, `recorded_at`, freshness/watermark, purpose,
classification, invalidators, failure behavior and canonical digest. Nodes
declare exact context paths and maximum age. An undeclared read fails. A masked,
stale or unknown value used by a rule produces `UNKNOWN`; it is not treated as
absent, zero or false.

## 3. External ingress

```text
IngressTrustProfile
  tenant + connection + external account
  auth mode: HMAC | SIGNATURE | OAUTH_SENDER | MTLS | SSH_PGP | NONE
  issuer/audience + key/cert version/validity
  canonical signed-byte profile
  replay/skew/assurance limits
  unsigned disposition: REJECT | QUARANTINE | OBSERVE_ONLY | MANUAL

IntegrationReceipt
  receipt ID + tenant/cell/placement/org
  account/subscription generation
  trust profile + auth result
  provider message/file/delivery ID
  immutable raw artifact/hash + retention
  content type/encoding/compressed and expanded sizes
  received time + trusted-time uncertainty
  schema/parser versions
  signature/replay/malware/DLP results
  dedupe key + prior receipt?
  authority decision/epoch
  source sequence/cursor/watermark
  occurred/effective/recorded times
  processing status
```

Verification covers raw bytes before parsing. Decompression/archive/parsing have
byte, count, depth, expansion and time limits. Batch/file items receive separate
identities and dispositions. Receipt persistence, dedupe reservation and
continuation enqueue are atomic before acknowledgement. Same scoped provider ID
with different bytes is a security incident.

```text
CorrelationDecision
  namespace = tenant + connection + account + connector version + object type
  external ID + canonical subject/resource refs
  crosswalk release/effective interval
  UNIQUE | UNKNOWN | AMBIGUOUS | CONFLICTING | REUSED_ID
  candidates/reasons/assurance/evidence

IngressDisposition
  ASSERTION | OBSERVATION | COMMAND | SIGNAL | QUARANTINED

OrderingDecision
  resource/partition + source sequence/watermark/predecessor
  ACCEPT | BUFFER | STALE | GAP | DUPLICATE | RECONCILE
```

Only unique correlation may advance material work. Signed input is not
automatically a command, signal or fact. Late input preserves event, effective,
received and recorded time and follows an explicit correction/supersession path.

Observations distinguish owner from observer and partial from complete:

```text
ObjectObservation
  source role: DOMAIN_FACT_OWNER | EXTERNAL_OBSERVER | RECONCILIATION_SOURCE
  authority/fingerprint/epoch + canonical resource/link assurance
  completeness: FULL | PARTIAL
  requested/provided fields
  values or digest + source version/watermark/read-as-of
  occurred/effective/recorded/received times
  PASS | FAIL | UNKNOWN | PARTIAL
  promotion: NEVER | PROPOSE_ONLY | PROMOTE_IF_DOMAIN_OWNER
```

Omitted, null, empty, unknown and redacted remain distinct. Absence becomes
deletion only with an explicit tombstone/disable signal proving complete scope,
pagination, watermark and authority.

## 4. Mapping, schema and source authority

Mappings pin source/destination schema digests, canonicalization, crosswalk,
authority, locale/time/money semantics and golden vectors.

```text
FieldMappingResult
  source/destination paths
  presence: ABSENT | NULL | VALUE | REDACTED | UNKNOWN
  transformation ref
  lossiness: NONE | FORMAT | ROUNDING | TRUNCATION | AGGREGATION |
             DROPPED | DEFAULTED | IRREVERSIBLE
  warning/error + reverse mapping?

AuthorityBinding
  tenant/org/resource/field/effective interval
  LOCAL_MASTER | EXTERNAL_MASTER | SHARED_FIELD
  domain fact owner + permitted writer
  authority decision/fingerprint/activation epoch
  promotion/merge rule + cutover watermark?
```

Money uses decimal amount, currency, frequency, basis and rounding policy. Time
uses instant/local-date type, IANA zone, tzdb version and DST gap/fold policy.
Material transforms cannot use floats, server-local time or locale-dependent
parsing.

`LOCAL_MASTER` may append canonical facts. `EXTERNAL_MASTER` commits intent,
effect and outbox evidence only and promotes state after an owner observation.
`SHARED_FIELD` requires a deterministic merge/conflict contract. Operations are
authority-homogeneous by field unless a merge proof exists.

```text
ConnectorOperation
  semantic effect identity + business/workflow/node refs
  tenant/cell/placement/org + connection/account/destination allowlist
  external resource key + causal predecessor/sequence
  authority bindings/fence/cutover
  mapping/schema release + expected external version
  canonical and mapped payload digests
  classification/purpose/egress decision
  idempotency + timeout/retry/deadline
  observation requirement

PLANNED -> QUEUED -> LEASED -> SENDING -> SENT -> PROVIDER_ACCEPTED
  -> OBSERVING -> RECONCILED
  or REJECTED | RETRYABLE | DEAD_LETTER | AMBIGUOUS | REPAIR_REQUIRED
```

Timeout after possible send is `AMBIGUOUS`, not failed. Observe by semantic
identity before retry. Redrive is a new governed repair lineage and revalidates
AuthZ, legal, DLP, authority, mapping, destination, version and rate policy.

Authority handoff fences ingress and egress. `DUAL_OBSERVE` permits reads only.
Exactly one writer exists at cutover. Stale queued authority epochs become
`HANDOFF_REVIEW`; they are drained, cancelled, quarantined or replanned, never
silently sent under new policy.

## 5. Legal context (Tier 2, except where noted)

Phase 1 legal context is one versioned rule pack of customer-configured
thresholds and notice obligations, evaluated to `LegalEvaluationStatus` and
bound through `ObligationBinding` (those two structures are Tier 1). The
jurisdiction graph, deadline calculus, temporal legal policy, and counsel
decision model below are Tier 2 and are not Phase 1 dependencies.

Jurisdiction is resolved separately per domain:

```text
JurisdictionAssertion
  role: RESIDENCE | PHYSICAL_WORK | REMOTE_WORK | PAYROLL | TAX | BENEFITS |
        PRIVACY | IMMIGRATION | DATA_PROCESSING | LEGAL_ENTITY
  domain/jurisdiction/activity segment
  effective interval + known-at
  source authority/evidence/confidence
  VERIFIED | ASSERTED | AMBIGUOUS | CONFLICTING | UNKNOWN | STALE

JurisdictionResolution
  action/subjects + assertions
  candidate/selected/rejected authorities
  composition profile/trace + resolver version
  unresolved facts/contradictions
  RESOLVED | AMBIGUOUS | UNKNOWN | CONFLICTING | STALE
  correction/supersession?
```

Residence, locale, employer or payroll country is never a universal fallback.
Mobile work uses effective-dated work-location segments with hours/allocation,
evidence and verification.

```text
LegalEvaluationStatus
  RESOLVED_ALLOW | ALLOW_WITH_OBLIGATIONS | DENY | RESTRICTED |
  JURISDICTION_AMBIGUOUS | RULE_COVERAGE_UNKNOWN |
  RULE_STALE_OR_QUARANTINED | CONTRADICTORY_RULES |
  COUNSEL_REQUIRED | DEPENDENCY_UNAVAILABLE

LegalEffect
  PROHIBIT | REQUIRE_STEP | RESTRICT | CALCULATE | INFORM
  scope + rule/applicability/composition refs + temporal policy + mandatory

ObligationBinding
  legal effect + workflow/node/capability
  NODE | GUARD | FIELD_MASK | DESTINATION_GATE | HUMAN_TASK | TIMER | CHILD_INTENT
  dependencies + owner + satisfaction/evidence + revalidation + non-removable
```

Publication fails if a mandatory effect lacks one reachable enforceable binding,
owner, evidence predicate or satisfaction event. Obligations propagate through
children, parallel branches, compensation and migration. Best-effort/ANY joins
cannot drop mandatory obligations.

Legal restrictions specify affected fields, operations, purposes, destinations,
required transform and enforcement points at read, proposal, commit, pre-send
and observation. Deny dominates; restrictions intersect; an unenforceable or
empty intersection fails closed.

```text
LegalDeadline
  STATUTORY | REGULATORY | CONTRACTUAL | POLICY | SLA
  trigger + clock-start basis/evidence
  due expression + authoritative timezone/tzdb
  calendar/version/cutoff + extension/tolling/grace
  due instant/uncertainty + owner/escalation/late route
  satisfaction: DELIVERED | ACKNOWLEDGED | ACCEPTED | SIGNED | FILED | VERIFIED

TemporalLegalPolicy
  law-as-of basis
  change reaction: PIN | RECOMPUTE | REQUIRE_REVIEW | MIGRATE | BLOCK
  future/retroactive actions + grandfathering
  invalidators/revalidation points
  historical/current rule snapshots
```

Timer wakeup, outbox persistence, provider acceptance, delivery,
acknowledgement, acceptance, signature, filing and verification are distinct.
Historical replay uses pinned rules; live authority reevaluates at request,
approval, resume and irreversible effects. Retroactive rules create reassessment
overlays, not rewritten history.

Evidence requirements state schema/facts, acceptable issuers and authorities,
as-of/freshness, signature/attestation, classification, retention/hold, purpose,
privilege and substitution. Satisfaction binds exact artifact/hash, issuer,
times, method, rule/schema versions, redaction and verifier authority. Agent
assertions and transport receipts are not legal evidence unless explicitly
allowed.

Unknown/stale/contradictory legal coverage blocks material effects unless a
narrow, expiring, fully evidenced continuity grant exists. Continuity cannot
override a prohibition. Counsel decisions distinguish interpretation, exception,
waiver, dispute and continuity; are scoped/effective-dated/revocable; and cannot
waive non-waivable rules.

## 6. Authorization and data governance

Authorization repeats at start, read, proposal, task delivery, decision,
execution, external effect, observation and repair.

```text
AuthorizationDecision
  principal/session/workload/delegation
  tenant/cell/placement/org/population
  capability/resource/fields/effective interval
  purpose/channel/risk + current/proposed relationship scopes
  policy/graph/source watermarks
  session/device/step-up assurance
  ALLOW | DENY | RESTRICT | UNKNOWN_FAIL_CLOSED
  restrictions/obligations/invalidators/max-age/expiry
  canonical input/result digests
```

Mandatory deny dominates. Material unknown/stale inputs fail closed.
Resource/population/field/purpose/destination/time restrictions intersect; empty
intersection denies. Obligations union by semantic identity; contradictions
remain explicit. Current and proposed scope authorizations are separate and
proposal-bound.

Every prompt, form, task, message, projection, connector payload, export, debug
view and telemetry item has a `DataAccessManifest`: fields/classes, purpose/lawful
basis, recipients/destinations/regions, masks/transforms, minimization decision,
retention/hold/copy inventory, derived-data inheritance and egress decision.
Derived summaries, embeddings and agent memory inherit sensitivity and purpose
limits. Hidden data cannot leak through counts, snippets, timing, errors or logs.

## 7. Human identity, approvals and work

`PrincipalContext` is server-derived and binds canonical identity equivalence,
tenant/environment, session, issuer/audience, AuthN/identity assurance,
authenticated time, expiry, revocation epoch, device risk, step-up and delegation.
Headers/body actor IDs never establish production identity. High-risk actions
declare assurance/session-age/step-up requirements and revalidate at commit.
Offline high-risk approval, signature and commit are prohibited.

Support/view-as requires customer consent, case, purpose, field/action scope, JIT
grant, step-up, expiry, recording and revoke. Support cannot approve/sign as a
customer.

```text
ApprovalRequirementSnapshot
  requirement/policy/resolver/candidate-set digests
  ONE | ANY | ALL | QUORUM
  denominator/count/veto/reject/abstain policy
  scope/effective-time/deadline/validity/delegation/SoD/step-up

ResolutionSnapshot
  candidate principals + identity equivalence
  person/worker/employment/assignment refs
  resolver path/authority/effective interval
  graph/group/role watermarks + included/excluded/ambiguity
  policy versions + candidate digest

ApprovalDecision
  requirement revision + proposal digest
  actor/session/delegation/authority snapshots
  kind/reason + visible projection/redaction digest
  presentation_receipt_ref + presentation_receipt_digest
  decision-input digest + trusted times/valid-until/idempotency

ApprovalResolutionCertificate
  proposal/requirement digests + decision IDs
  per-requirement counts/denominator + quorum/veto result + evaluated time

ApprovalInvalidation
  decision/certificate + typed cause/source digest
  effective/detected time + actor + monotonic sequence
```

Proposal amendment, invalidation, vote insert and aggregate use one serializable
CAS boundary. Identical retry returns the original; conflicting key reuse fails.
Delivery recipient, queue assignee, claim holder and decision authority are
distinct. Empty/ambiguous/stale resolvers fail closed. Candidate churn explicitly
freezes, recomputes or invalidates. Quorum denominator, dedupe and veto semantics
are pinned. Delegation is scoped, bounded, expiring and non-transitive by default;
it cannot expand authority, count twice or bypass SoD across aliases or children.

Every requirement also declares one `ResolutionTimePolicy`:

```text
PIN_AT_CREATION
CURRENT_AT_DECISION
AS_OF_BUSINESS_EFFECTIVE_TIME
RECHECK_AT_DECISION_AND_EXECUTION
```

and one `CandidateChurnPolicy`:

```text
FREEZE_ELIGIBLE_SET
RECOMPUTE_AND_INVALIDATE
RECOMPUTE_PRESERVE_STILL_ELIGIBLE
REQUIRE_MANUAL_REBASE
```

These policies independently govern candidate delivery, vote validity, quorum
denominator and task reassignment. A timer's `RECOMPUTE` recalculates only its
referenced time/calendar value; legal reevaluation is a separate bound
capability, even though both use the same change-reaction vocabulary.

The approver view is rendered by the server from the proposal revision, and
the server records the digest of what it rendered (visible fields, hidden-field
manifest, warnings, effects) keyed by task version. That is the whole receipt
mechanism in Phase 1 (Tier 1): a decision references the task version, and the
server binds the proposal digest and the rendered-projection digest it holds
for that version. A separate client-held `ApprovalPresentationReceipt` token,
session binding, and expiry are Tier 2 and are added only if a threat model
shows the server-side record is insufficient. Material hidden data blocks or
routes to authorized specialist review.

```text
DecisionSafetyStatus
  SAFE_TO_DECIDE
  MATERIAL_UNKNOWN
  MATERIAL_REDACTED_REVIEW_REQUIRED
  INSUFFICIENT_EVIDENCE
```

Only `SAFE_TO_DECIDE` permits the ordinary approval route. Other states bind the
affected fields/reasons and route to a separately authorized reveal, specialist
review, additional evidence or denial without exposing protected values.

Vote submission references the task version. The server verifies that the
requirement revision, proposal digest, and rendered-projection digest it holds
for that version are still current and that the principal still has authority.
A client-supplied digest is never decision evidence. Material re-rendering or
loss of access advances the task version and requires review again.

Human Work uses a revisioned item, candidate/assignment snapshots and a fenced
`ClaimLease`. Completion requires current authority, item CAS, live claim fence,
typed output and semantic completion identity, and emits one signal. Handoff,
abandonment, lease loss, requeue, escalation, quarantine and policy migration are
explicit events/states. Queue order defines deterministic tie-break, aging,
tenant fairness, critical capacity and starvation bound. Legal deadlines never
reset on reassignment. Bulk is per-item authorized, evidenced and resumable.

Forms bind definition, answer schema, conditional/validation rules, locale,
reference, AuthZ/legal/classification and attachment digests. Conditional cycles
are rejected. Hidden-answer and repeat-group semantics are explicit. Drafts are
encrypted/revisioned/expiring and distinct from submissions. Attachments remain
quarantined until hash/type/size/malware/DLP/classification/retention checks pass.
Corrections are append-only diffs and invalidate dependent decisions when needed.

Each participant has language/script/direction, formatting, timezone, fallback,
accessibility and representative/interpreter context. Locale never changes legal
semantics. The full task/form/approval/message path targets WCAG 2.2 AA and
supports governed phone/in-person/postal transcription/read-back routes with
equal deadline treatment and privacy compartmenting.

## 8. Agent participation

Agent data uses a shared taint label set:

```text
CANONICAL_FACT | APPROVED_POLICY | HUMAN_ASSERTION | EXTERNAL_UNTRUSTED |
HOSTILE_SUSPECTED | AGENT_DERIVED | SANITIZED_WITH_LIMITS
```

Taint survives OCR, summary, memory, retrieval, signal, child and tool output
unless a versioned sanitizer emits bounded evidence. A known thread or signed
file does not make content executable instruction.

The taint value is a set ordered by subset: `A <= B` when every label in `A` is
also in `B`. Join is set union. Combining `CANONICAL_FACT` with
`HOSTILE_SUSPECTED` retains both labels and applies the most restrictive rule.
Transformations add `AGENT_DERIVED` and retain every input label. A sanitizer
receipt may add `SANITIZED_WITH_LIMITS` and authorize named fields, threat classes,
uses and expiry; it does not erase provenance or remove hostile status outside
that scope. Declassification requires a separate governed release decision.

Agent calls pass a server gateway binding identity/delegation, tenant/cell/
audience, capability/version, nonce, exact arguments/provenance, field/purpose,
risk/effect, fan-out/deadline/budget and output validation. Discovery and invoke
authority are separate and discovery cannot leak counts/snippets/timing.
Schema-valid output still fails for unsupported claims, missing provenance,
hidden-field echo, hostile markup or tainted material values.

Every `AGENT` input/output carries a typed taint manifest. Taint joins are
monotonic. Downgrade is allowed only through a pinned sanitizer profile and
bounded sanitizer receipt; generic schema or business validation cannot remove
taint.

The AI kill switch fences queued/leased writes and defines behavior for model,
tool, post-local-commit, pre-send and in-flight states. Unknown effects reconcile.

## 9. Compiler, conflict and runtime enforcement

The type algebra defines nullability, cardinality, unions, enums, branded IDs,
units/currency, error variants and coercions. Compatibility covers workflow,
capability, context, signal and stored-output schemas.

Compilation proves declared minimal context, resolved dependency closure,
reachable obligation bindings, transitive effects/write sets, observation/repair
for irreversible effects, bounded cycles/fan-out/time/bytes/cost, mutation-free
simulation and content-addressed dependencies. Runtime validates compiled plan,
compiler/build, schema epoch, capability implementation and effect attestation.
Actual reads/writes/effects must be declared subsets; expansion aborts and may
quarantine the version. Pure nodes cannot read ambient clock, locale, environment,
network, flags or mutable cache.

(Tier 2, applies once `PARALLEL` exists.) Every `PARALLEL` fork binds a
`ReadConsistencyVector` containing relevant domain and source watermarks,
stream heads, legal/AuthZ/policy/reference versions and effective/known time.
Branches use that vector or declare weaker consistency plus explicit
reconciliation. `JOIN` verifies compatible vectors; incompatible material reads
cause revalidation/recompute instead of combining effects.

(Tier 2, applies once `SUBWORKFLOW` exists.) Every `SUBWORKFLOW` binds
`ChildAuthorityScope`: tenant/cell/org, subjects and population, capabilities,
fields, purpose, classification, destinations, risk/effect classes and budgets.
Effective scope is the intersection of parent-approved, caller and child-policy
scopes. Expansion requires a separate governed expansion proposal/certificate.
Child closure returns used-scope evidence for parent verification.

Material transactions carry one multi-stream write-set manifest, expected heads,
effective intervals, reservations and deterministic lock keys. Cross-store work
is either one proven transaction or an explicit saga with partial/unknown states.
Reservations have `HELD`, `CONSUMED`, `RELEASE_PENDING`, `RELEASED`, `AMBIGUOUS`,
`EXPIRED` plus owner/fence/recovery.

Cancellation has a monotonic fence checked at claim, prepare, commit and pre-send.
Pre-commit, post-commit/pre-send, in-flight and ambiguous outcomes differ. Bulk
pins population/watermark, stable chunks, reservations and per-item effects and
never reruns success or hides one changed subject.

## 10. Completion, intervention and explainability

Completion dimensions are the intent kernel's five, plus the workflow
instance's own `runtime_status`:

```text
RequestState | ExecutionState | BusinessState | ConsistencyState |
ObligationState        (intent)
runtime_status         (workflow instance)
```

Local commit state is `ExecutionState`; human-interaction state lives on the
WorkItem; operational state is an incident projection; evidence completeness is
recorded on the `ClosureRecord`. None of those is a sixth intent dimension.

Provider submission/acceptance is not observed application. Parents preserve
child partial/unknown/mandatory obligations. Closure binds proposal/plan/execution
and approval digests, all context/control snapshots, local receipt/stream heads,
external attempts/observations, reconciliation, obligations, human evidence,
incidents/repairs, records and evidence statuses. Evidence status is `COMPLETE |
PARTIAL | UNKNOWN | STALE | REDACTED | UNAVAILABLE | AMBIGUOUS`.

Every pause/cancel/skip/override/rewind/retry/redrive/repair/migration/break-glass
first creates an `InterventionPreview` with current state/safe point, affected
subjects/children/effects, irreversible/ambiguous work, predicted dimensions,
authority/SoD/step-up and expected post-state/repair route. The command binds that
digest, state CAS, actor/JIT grant, exact scope, reason, confirmation and expiry.
An advertised command executes and evidences its transition or rejects; accepted
no-ops are forbidden.

The inspector is a causal DAG carrying stream sequence, predecessors, attempt/
effect identity, resource sequence, authority epoch, watermark and ordering
confidence. Explanations include source, legal composition, AuthZ/obligations,
approval/control digests, consulted/unavailable evidence and redactions. Debug,
support, replay and audit export pass normal tenant/org/field/purpose/DLP/JIT and
bounded-query controls.

## 11. Isolation, overload and degraded operation

Timers, signals, leases, checkpoints, caches, outbox and connector cursors bind
tenant/cell/placement epoch. Stale-cell work cannot fire or commit after move.

```text
WorkloadContext
  criticality P0..P4 + classification policy
  queue/node/workflow/external deadlines
  shared attempt/time/cost retry budget
  fan-out/bytes/cost limits
  dependency budgets + degradation policy

AdmissionDecision
  ADMIT | DEFER | SHED | DEGRADE | REJECT
  reason + retry-after + reservation + evidence
```

Queues are bounded. Scheduling defines tenant fairness, bursts/concurrency,
reserved P0 capacity, aging and starvation bounds. Only one layer retries; child
and connector work consume one shared budget. Timer/signal/fan-out/alert storms
are coalesced and capped. Drain stops admission, fences sends/leases, classifies
queued/in-flight work, stops retries, persists unresolved state and resumes with
bounded replay. Every capability declares fail-closed, queue, read-only, verified
stale, disable-AI or manual degradation plus owner/recovery.

## 12. Required negative conformance families

Families 1, 4, 7, 8, 10, 15, 16, and 17 are Tier 1 and gate P1A/P1B. The
rest are Tier 2 and gate general authority.

1. Forged/duplicate/conflicting/late/out-of-order/gapped ingress.
2. Ambiguous crosswalk, lossy mapping, partial scan and false deletion.
3. Authority handoff, dual-writer attempt and stale queued operation.
4. Timeout-after-send, accepted-not-applied and targeted redrive.
5. Multi-location jurisdiction, DST fold/gap, legal change and late fact.
6. Unbound obligation, stale/contradictory rules and invalid counsel scope.
7. Field/purpose/DLP intersection, material masked field and derived leak.
8. Revoked session, stale step-up, shared device and support impersonation.
9. Empty/changed resolver, quorum/veto, delegation loop and SoD alias.
10. Concurrent proposal amendment/vote and duplicate decision.
11. Claim expiry, queue starvation, legal deadline and restricted task.
12. Form branch tamper, stale draft, quarantined attachment and correction race.
13. Accessibility/RTL/manual route with deadline preservation.
14. Prompt injection through every content path, tool forgery and kill race.
15. Ambient dependency, schema drift, undeclared write and simulation effect.
16. Multi-stream/reservation/cancel-effective/bulk races.
17. Child partial/unknown, missing evidence and false closure.
18. Cross-tenant correlation, relocation, debug/support/export leakage.
19. Saturation, noisy tenant, retry/timer storms and drain/restart.
20. Privacy withdrawal/rectification/hold/deletion across all copies.

Research is internally clean only when every finding is contracted and tested,
explicitly deferred behind a safe manual boundary, or recorded as a residual
unknown. This does not assert that the current Go implementation conforms.
