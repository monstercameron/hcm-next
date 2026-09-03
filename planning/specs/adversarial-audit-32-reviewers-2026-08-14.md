# Thirty-Two-Reviewer Adversarial Audit — 2026-08-14

## Method boundary and closure policy (added 2026-09-02)

This audit is a frozen input. It is not re-run, extended, or answered with
further contract prose. The rules for using it:

- **No audit pass until P1A executes.** Model-generated review of
  model-generated planning converges on more planning. The next review is of
  running code and its tests.
- **A finding closes in exactly one of three ways:** a test that exercises
  the defect, an explicit `DEFERRED` disposition naming the gate that owns
  it, or `NOT_APPLICABLE` with a one-line reason. "Contract patch required"
  is not a closure; it is a request that must resolve to one of the three.
- **Findings against the legacy TypeScript runtime** (the `LEGACY CUTOVER
BLOCKER` class) are `NOT_APPLICABLE` to the target and are recorded as
  negative tests the Go slice must pass; they are not tracked as open defects.
- **Findings against deferred systems** (agents, messaging beyond one email,
  billing, regulatory, Gate C trust work) are `DEFERRED` to the gate that owns
  the system and do not appear on the P1A/P1B list.
- **The P1A/P1B working list** is the subset of findings that map to a Tier 1
  rule in the workflow-context contract or to a P1A/P1B acceptance bullet in
  the execution plan. Everything else waits.

## Scope and evidence rule

Thirty-two independent GPT-5.6 Luna reviewers examined the HCM Next planning,
schema and legacy implementation corpus. GWC was explicitly excluded. Reviews
covered product viability, intent/workflow/transaction semantics, HCM domains,
security/privacy, tenancy, reliability, operations, economics, accessibility,
developer contracts and conformance. Reviewers used primary official sources when
current external guidance was material.

Raw findings were deduplicated into the issue families below. A finding is not
closed because prose names the desired system. Closure requires the applicable
contract, typed artifact, owner, phase disposition, negative test and evidence.

```text
DEFINED IN TARGET PLAN
  a coherent target contract exists

OPEN IMPLEMENTATION
  the target contract exists but executable evidence does not

CONTRACT PATCH REQUIRED
  the target plan is incomplete, contradictory or unsafe

DEFERRED DOMAIN DEPTH
  the boundary is explicit and no current-product claim is permitted

LEGACY CUTOVER BLOCKER
  existing TypeScript/Node/demo behavior must not survive the Go cutover
```

## Consolidated defect register

| ID      | Severity | Defect family                                                                                                                     | Classification                       | Required closure                                                                                                                |
| ------- | -------- | --------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| A32-001 | Critical | Caller-selected demo actor and tenant can impersonate privileged principals                                                       | Legacy cutover blocker               | Verified human identity/session context; no demo header outside isolated test mode; cross-tenant negative tests                 |
| A32-002 | Critical | ID-only repositories, single-column foreign keys and absent RLS permit cross-tenant object/reference paths                        | Contract patch + legacy blocker      | Tenant-scoped APIs, composite tenant foreign keys, fail-closed database tenant context/RLS and direct-database isolation tests  |
| A32-003 | Critical | Go executor and east-west calls trust body identity over unauthenticated HTTP                                                     | Legacy cutover blocker               | Workload identity, mTLS/service tokens, server-derived authority, audience/tenant/cell binding and replay limits                |
| A32-004 | Critical | External effects occur before durable local commit and operation evidence                                                         | Legacy cutover blocker               | Atomic local commit of intent/plan/domain/ledger/projection/outbox before dispatch; ambiguous outcomes remain unknown           |
| A32-005 | Critical | No multi-stream commit coordinator, stream-head validation or projection CAS                                                      | Open implementation                  | Deterministic stream locking, expected heads, one database transaction, commit/abort receipts and ambiguity resolution          |
| A32-006 | Critical | Idempotency has check-then-act races, payload-blind replay and unrecoverable started attempts                                     | Contract patch + legacy blocker      | Stable semantic effect identity, canonical request digest, atomic claim/CAS, leases/fences and crash recovery                   |
| A32-007 | High     | Effect idempotency is scoped to principal, allowing a new worker/repair actor to duplicate the same effect                        | Contract patch required              | Bind effect identity to transaction/node/operation; record principal as authority evidence, not uniqueness scope                |
| A32-008 | High     | Ledger hashes are nullable/caller-controlled and no per-stream hash/sequence head exists                                          | Open implementation                  | Coordinator-computed canonical digest chain, non-null profile/algorithm IDs, verification incidents and signed epochs           |
| A32-009 | Critical | Durable workflow timers, signals, leases, retries, child cancellation and poison-work ownership are not executable                | Open implementation                  | Durable scheduler primitives, bounded retry budgets, owned quarantine and restart/duplicate tests                               |
| A32-010 | High     | Workflow graphs may silently choose the first route, accept unreachable nodes/cycles and treat visit-limit warnings as completion | Legacy cutover blocker               | Compiler errors for invalid graphs; explicit default route; bounded loops; execution-limit failure/quarantine                   |
| A32-011 | High     | Canonical intent schema lacks immutable proposal, approval, execution, cancellation, supersession, correction and closure records | Contract patch required              | Add typed immutable records and references with actor, reason, exact digest, control snapshots, times and evidence              |
| A32-012 | High     | Independent intent lifecycle fields have no compatibility/transition lattice                                                      | Contract patch required              | Versioned family-specific compatibility matrix enforced at publication, command, projection, migration and repair               |
| A32-013 | High     | Approval aggregate state cannot prove requirement-level votes, quorum, authority, invalidation or exact proposal binding          | Contract patch required              | Canonical ApprovalRequirement/Decision/Binding messages and exact proposal/control snapshot revalidation                        |
| A32-014 | Critical | Source authority, identity resolution, person/worker/employment separation and mastering have no executable gate                  | Open implementation                  | Authority decisions/fences plus canonical identities, external links, merge/separate/correction lineage and ambiguity states    |
| A32-015 | High     | Effective-time endpoint and timezone semantics vary by domain                                                                     | Contract patch required              | One shared half-open `EffectiveTimeRange`, local-date/instant rules, normalization and boundary conformance vectors             |
| A32-016 | High     | Data validation, quality, invariants and reconciliation are named but not independently executable                                | Open implementation                  | Separate typed evaluators with PASS/FAIL/UNKNOWN/PARTIAL, watermarks, phase gates, findings and remediation                     |
| A32-017 | High     | Repair is bulk bookkeeping rather than a bounded, approved and verified RepairPlan                                                | Open implementation                  | Immutable diagnosis/repair/action/approval/verification records, SoD, targeted redrive and post-repair reconciliation           |
| A32-018 | Critical | No authoritative root Go module, generated Protobuf services or reproducible Go-only release command exists                       | Open implementation                  | Root Go toolchain/module, pinned generators, deterministic build/test/SBOM/provenance and legacy runtime exclusion              |
| A32-019 | Critical | Current SchemaFlux sibling is an LLM operations library, not the deterministic HCM schema compiler claimed by the plan            | Contract patch + implementation gate | Add/prove a pure-Go offline compiler or HCM adapter with strict YAML, descriptor resolution and deterministic emit              |
| A32-020 | High     | grpcbridge supports a narrower transport/authentication boundary than the constitution implies                                    | Contract patch required              | Qualify supported transport, authenticated context propagation, secure defaults, origin/reconnect/limit tests                   |
| A32-021 | Critical | Fresh database migration path contains incompatible initial schemas and lacks checksum/locking/dirty-state controls               | Legacy cutover blocker               | One migration manifest/runner, advisory lock, checksums, clean-database CI and expand/contract/backfill policy                  |
| A32-022 | Critical | Signed control bundles can be replayed or rolled back below a revoked safe state                                                  | Contract patch required              | Monotonic activation epoch/floor, validity interval, revocation set, parent link and dual-approved rollback receipt             |
| A32-023 | Critical | Classification/DLP/egress is not bound to accepted content, proposal approval or actual outbound calls                            | Contract patch + implementation gate | Typed label/DLP snapshots in proposal digests; destination trust; fail-closed propagation and egress receipts                   |
| A32-024 | Critical | Rights, retention, legal holds, copy inventory and deletion verification have no executable path                                  | Open implementation                  | Subject-rights and records lifecycles spanning canonical, derived, external, backup and restored copies                         |
| A32-025 | Critical | Mutating AI tools rely on prompt compliance; client can forge system/tool history and employee search bypasses AuthZ              | Legacy cutover blocker               | Server-owned conversation history, taint, tool gateway, exact confirmation token, field filtering and write kill switch         |
| A32-026 | High     | Messaging is labeled Phase 1 implementation without schema, Go service, provider adapter or delivery tests                        | False maturity                       | Downgrade until a bounded inbox/intent/delivery/signal slice exists; never equate provider acceptance with receipt              |
| A32-027 | High     | Regulatory computation/reporting/content rows are labeled defined without focused typed contracts                                 | False maturity                       | Mark partial/deferred; add jurisdiction assertions, composition programs, deterministic calculation/report/obligation contracts |
| A32-028 | Critical | Legacy termination and compensation blocks embed unsafe legal/money shortcuts                                                     | Legacy cutover blocker               | Remove universal notice/final-pay/COBRA assumptions; use fixed-decimal money and unresolved/manual-review outcomes              |
| A32-029 | High     | WFM, Benefits/Leave and Recruiting/Onboarding are product nouns without domain operating contracts                                | Deferred domain depth                | Add explicit deferred contracts/owners before claims; hide demo eligibility/coverage states from operational surfaces           |
| A32-030 | High     | Human Work, Forms, Rules, Cases, Documents and E-signature are broader in prose than executable evidence                          | False maturity/open implementation   | Bound Gate B to implemented approval/form/rule slice; add CAS, claim leases, typed submissions, quarantine and evidence         |
| A32-031 | Critical | Logical tenant placement/cell epoch is absent despite being a Phase 1 isolation primitive                                         | Contract patch + implementation gate | Add placement/residency/isolation profile and signed epoch to request/event/outbox/workload contexts                            |
| A32-032 | Critical | No canonical overload/admission/backpressure/retry-budget plane protects critical work                                            | Contract patch + implementation gate | WorkloadContext, admission decision, tenant fairness, P0 reservations, load shedding, drain and bounded fan-out                 |
| A32-033 | High     | SLO, incident, backup/restore and manual continuity contracts lack executable Gate B evidence                                     | Open implementation                  | Pilot service/recovery profiles, incident/advisory path, continuity drill and measured restore/reconciliation evidence          |
| A32-034 | High     | Supply-chain, trusted-time and crypto-agility claims lack exact operational profiles                                              | Contract patch + implementation gate | SLSA v1.2 provenance, complete SBOM, admission verification, NTS/quorum/uncertainty, crypto inventory/migration profiles        |
| A32-035 | Critical | Raw prompts, payloads, debug data and arbitrary log details can become telemetry/AI egress                                        | Legacy cutover blocker               | Allowlisted telemetry privacy gateway, payload limits, typed redaction, cumulative AI budgets and completeness health           |
| A32-036 | High     | Intelligence, metrics, secure retrieval, process mining and digital twin are absent from coverage tracking                        | Contract patch required              | Add explicit deferred responsibilities/owners and remove current-product implication until executable                           |
| A32-037 | High     | Causal language is used for lineage and process variation without an evaluated causal design                                      | Contract patch required              | Use TRIGGERED/DERIVED_FROM/ASSOCIATED_WITH; reserve causal claims for governed estimates with uncertainty and assumptions       |
| A32-038 | High     | Commercial entitlements, metering, provider cost, billing evidence and unit economics have no focused owner contract              | Contract patch required              | Add minimal fixed-price pilot commercial operations now; defer variable rating/tax/payment until dedicated gates                |
| A32-039 | Critical | Configuration publish/import can bypass guardrails and is not an atomic environment promotion                                     | Legacy cutover blocker               | Draft-by-default, authoritative guardrail/simulation/SoD in one publish transaction, signed target binding manifest             |
| A32-040 | High     | Tenant onboarding/import/migration/sandbox/exit lacks customer/source lineage and cutover evidence                                | Contract patch + implementation gate | Partner onboarding plan, staged import/crosswalk/completeness, masked sandbox and rehearsed export/delete runbook               |
| A32-041 | High     | WCAG/localization promises lack criterion/process/locale/AT evidence and conflict with deferred document/translation systems      | Contract patch required              | Criterion-to-test matrix, canonical locale/content rules, accessible document evidence and human alternative route              |
| A32-042 | Critical | Offline approval and shared-device persistence are underspecified                                                                 | Contract patch required              | High-risk offline is read/draft only; device-bound storage/wipe; online revalidation for submit/approve/sign/commit             |
| A32-043 | Critical | Gate B allows write authority before minimal support/JIT diagnostics/incident communication/exit readiness                        | Phase-gate contradiction             | Require PilotOperationsPack, named on-call, advisory route, continuity/repair runbooks and exit rehearsal before Gate B         |
| A32-044 | High     | Product qualification can duplicate incumbent capability or prove only one-system governance                                      | Product-gate gap                     | Require incumbent edition/topology assessment, one independently owned downstream effect and stop/reselect thresholds           |
| A32-045 | High     | Pilot economics omit customer labor, ongoing support and bespoke connector cost                                                   | Product-gate gap                     | Partner labor plan, cost-to-serve, contribution margin and numeric stop/continue thresholds                                     |
| A32-046 | High     | API/SDK/webhook/error/pagination/version contracts are ad hoc and no generated service surface exists                             | Contract patch + implementation gate | Canonical Protobuf services, generated clients, stable errors, bounded pagination/streaming and app identity                    |
| A32-047 | Critical | Reference workflow tests use canned executors and permit missing effects to pass as completed                                     | Conformance blocker                  | Real Go/DB/connector fakes, exact effect assertions, failure injection, restart/race/load/restore and orphan matrix             |
| A32-048 | High     | The 530-candidate intent count, phase vocabulary and documentation-integrity claims are not mechanically reproducible             | False maturity                       | Check in source manifest/digest, normalize phase enums, generate trace/index and enforce link/schema/format checks              |
| A32-049 | High     | Outbox operations lack per-resource causal ordering, conditional external version and authority-handoff fencing                   | Contract patch required              | External resource key/sequence, expected vendor version, ordering class, authority fingerprint/epoch and pre-send revalidation  |
| A32-050 | High     | Cross-tenant content-addressed deduplication can defeat deletion or delete another tenant's artifact                              | Contract patch required              | Prohibit sensitive cross-tenant dedup or use tenant-keyed wrappers/reference counts with delete/preserve/restore tests          |

## Phase-gate corrections

### Gate A — read-only proof

Gate A cannot pass without:

1. A dated incumbent edition/topology assessment proving a non-duplicative failure class.
2. One HCM source plus one independently owned downstream system/effect, or an
   explicitly narrower single-system product claim.
3. A real consumable handoff plus acknowledgement/observation; proposal approval
   alone is not outcome evidence.
4. A numeric baseline, adoption denominator, bypass taxonomy, customer labor
   estimate, cost-to-serve and stop/reselect thresholds.
5. A pinned root Go/Protobuf/SchemaFlux qualification pipeline. Draft schemas may
   remain, but do not count as compiled contracts.
6. Logical tenant placement, authenticated identity/workload boundaries and a
   read-only recovery/incident path.

### Gate B — first write authority

Gate B additionally requires:

1. Atomic local commit before all external dispatch.
2. Durable operation journal, causal ordering, authority fence, observation,
   reconciliation, ambiguity and repair.
3. Canonical approval bindings and lifecycle compatibility enforcement.
4. Tenant-safe repositories/database constraints and production identity.
5. Pilot Operations Pack: named primary/secondary on-call, support hours, JIT
   diagnostic access, incident/customer advisory route, continuity/redrive/
   rollback procedures and exit rehearsal.
6. Decision notice/explanation/correction/human-review evidence for the selected
   high-impact workflow.
7. Accessibility/localization evidence for the complete process and a non-digital
   accommodation route.
8. Restore, restart, concurrent-submit, duplicate-effect, partial-external-success,
   missing-connector and false-completion tests.

## Claim rules

- A diagram, intent name, widget or catalog row is not an implemented capability.
- `DEFINED` requires a focused contract meeting the coverage-matrix definition;
  architecture-atlas prose alone is `PARTIAL` or `DEFERRED`.
- Legacy TypeScript/demo behavior is evidence to migrate or reject, never target
  certification.
- “Vendor-agnostic” is an architecture hypothesis until a second independently
  configured connector topology reproduces the proof.
- “Cross-system” requires at least two independently owned authoritative/effect
  boundaries.
- Provider acceptance is not delivery, business completion, external consistency
  or reconciliation.
- Causal vocabulary requires a governed causal evaluation design.

## Residual implementation blockers

This audit patches governing expectations, not the missing product. The root Go
build, SchemaFlux compiler adapter, generated contracts, durable runtime, identity,
tenant isolation, commit coordinator, integration journal, quality/reconciliation,
repair, telemetry, support and recovery evidence remain open implementation work.

## Ownership and review

- Accountable role: Architecture Council
- Required reviewers: Product, HCM Domain, Security, Privacy, Reliability,
  Accessibility, Support/Implementation and Commercial Operations
- Status: audit complete; contract patches required; implementation blockers open
- Review trigger: every phase gate, authority expansion, material incident and
  addition of a product-domain implementation claim
