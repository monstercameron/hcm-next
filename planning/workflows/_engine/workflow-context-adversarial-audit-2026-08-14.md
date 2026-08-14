# Workflow Context Adversarial Audit — 2026-08-14

This register records 32 adversarial reviews of how integrations, legal rules,
authorization and humans participate in workflows. Reviews 1–30 produced the
first patch. Reviews 31–32 are the post-patch challenge round.

Status meanings:

```text
CONTRACTED  normative requirement and negative-test family now exist
SAMPLED     a concrete adversarial workflow sample now exists
DEFERRED    safe boundary is explicit; implementation is not claimed
OPEN        undispositioned research defect
```

## First-pass review register

|   # | Review lens           | Highest-risk finding                                                                                                       | Disposition                                                          |
| --: | --------------------- | -------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
|   1 | Ingress trust         | Receipt scope, raw-byte verification, replay, ordering, correlation and file semantics were incomplete                     | CONTRACTED §§3–4; SAMPLED ingress quarantine                         |
|   2 | Egress effects        | Timeout-after-send, provider acceptance, leases/fences and unsafe redrive could create duplicate/false effects             | CONTRACTED §4; SAMPLED accepted-not-applied                          |
|   3 | Mapping/contracts     | Lossiness, presence, crosswalk ambiguity, schema drift, pagination and external-ID reuse lacked closed semantics           | CONTRACTED §4                                                        |
|   4 | Source authority      | Samples committed canonical local facts even when an incumbent remained authoritative                                      | CONTRACTED §4; base context patched; SAMPLED handoff                 |
|   5 | Legal obligations     | Jurisdiction, obligation-to-node binding, legal deadlines, evidence and counsel authority were prose-only                  | CONTRACTED §5                                                        |
|   6 | Legal evolution       | Approval expiry/revocation and future/retroactive/injunction behavior lacked live-boundary rules                           | CONTRACTED §5; SAMPLED rule change                                   |
|   7 | Jurisdiction/time     | Multi-location work, DST, known/effective time and late facts lacked one typed model                                       | CONTRACTED §§2,5                                                     |
|   8 | Legal evidence        | Signature/identity proofing is deferred while workflows appeared to require evidence-grade acceptance                      | CONTRACTED §§5,7; DEFERRED to manual/provider gate until implemented |
|   9 | Human AuthN           | Caller-selected identity, stale sessions, missing step-up and authority races are incompatible with target approval safety | CONTRACTED §7; implementation remains open                           |
|  10 | AuthZ composition     | Unknown/stale decisions and restriction/obligation precedence were not executable                                          | CONTRACTED §6                                                        |
|  11 | Field/purpose/DLP     | Coarse action authority did not prove minimum fields, decision-safe masking, derived-data policy or egress                 | CONTRACTED §6; SAMPLED masked material field                         |
|  12 | Approver resolution   | Resolver AST, candidate identity, churn, effective time, quorum and external candidates were incomplete                    | CONTRACTED §7; SAMPLED authority change                              |
|  13 | Delegation/SoD        | Transitive delegation, identity aliases, quorum duplication and parent-child SoD could bypass independence                 | CONTRACTED §7                                                        |
|  14 | Approval binding      | Requirement snapshots, visible projection, aggregate certificate, invalidation and vote/amendment atomicity were missing   | CONTRACTED §7                                                        |
|  15 | Human queues          | Claim leases, handoffs, SLA clocks, priority/fairness, bulk and abandonment were underspecified                            | CONTRACTED §7                                                        |
|  16 | Approval ergonomics   | Risk/uncertainty/hidden content, RFI, mobile and bulk fatigue needed evidence-bearing presentation                         | CONTRACTED §7                                                        |
|  17 | Accessibility/i18n    | Participant locale, approved translation, WCAG, accommodations and manual parity were not machine-bound                    | CONTRACTED §7; SAMPLED accessible notice                             |
|  18 | Messaging identity    | Endpoint/sender/reply identity, mandatory preference precedence, sensitive fallback and bulk TOCTOU were incomplete        | CONTRACTED §§3,6,7; sample covers notice satisfaction                |
|  19 | Forms/evidence        | Conditional/repeat/draft/stale/attachment/offline/correction semantics lacked enforceable artifacts                        | CONTRACTED §7                                                        |
|  20 | Intervention UX       | Cancellation/retry could be falsely terminal or accepted no-op without impact preview and safe-point evidence              | CONTRACTED §10                                                       |
|  21 | Agent security        | Taint, hostile derivatives, tool gateway, discovery leakage, approval laundering and containment were prose                | CONTRACTED §8                                                        |
|  22 | Compiler/context      | Type algebra, ambient dependencies, obligation propagation, effect attestation and runtime drift were not closed           | CONTRACTED §9                                                        |
|  23 | Completion/failure    | Runtime could equate transport submission with business completion and lose partial-operation state                        | CONTRACTED §§4,10; SAMPLED accepted-not-applied                      |
|  24 | Temporal conflict     | Multi-stream fencing, exact-time races, reservations, cancellation and bulk concurrency lacked linearization rules         | CONTRACTED §9                                                        |
|  25 | Privacy/records       | Purpose, rights, retention/hold/delete propagation and transfer evidence remained descriptive                              | CONTRACTED §§5–7,10–12; full implementation deferred                 |
|  26 | Tenant/support        | Timers/signals/correlation and support/JIT diagnostics did not consistently bind tenant/cell/placement                     | CONTRACTED §§2,6,7,11                                                |
|  27 | Overload              | Criticality, admission, fairness, retry budgets, storm control and drain were vocabulary without wire contracts            | CONTRACTED §11                                                       |
|  28 | Offline/mobile        | Device/clock/location trust, draft queue, conflicts, wipe and replay needed explicit policy                                | CONTRACTED §7; SAMPLED shared-device denial                          |
|  29 | Reference conformance | Happy-path samples lacked authority handoff, legal change, candidate churn, bulk invalidation and ambiguous effects        | SAMPLED nine cross-cutting cases                                     |
|  30 | Admin/explainability  | Debugger needed causal order, evidence completeness, DLP/JIT, bounded replay and signed exports                            | CONTRACTED §10                                                       |

## Cross-cutting corrections made

- Local commit now branches by field-level mastering mode; external-mastered
  proposals are not exposed as canonical domain truth before owner observation.
- Provider acceptance, delivery, acknowledgement, business acceptance, signature,
  filing acceptance and verified external application are distinct states.
- Legal effects must compile to enforceable guards/nodes/field masks/destination
  gates/tasks/timers/children and carry a typed satisfaction predicate.
- Approval is a protocol of requirement, resolution, presentation, vote,
  invalidation and aggregate certificate artifacts.
- Human queue assignment/claim does not imply decision authority.
- Context is least-privilege, versioned and boundary-revalidated rather than one
  start-time snapshot.
- Interventions must execute an evidenced state change or reject; accepted no-op
  behavior is forbidden.
- Closure is multidimensional and declares partial, unknown, stale, redacted,
  unavailable and ambiguous evidence.

## Explicitly deferred implementation boundaries

The research contract is intentionally broader than the next executable slice.
Until the corresponding Go service and conformance evidence exist:

- evidence-grade e-signature/identity-proofing steps use a bounded external/manual
  gate and cannot claim native signature assurance;
- authoritative payroll/regulatory filing remains deferred;
- offline high-risk approval/signature/commit is prohibited;
- agent nodes remain read/analyze/draft-only unless the tool gateway, taint model,
  output validation and kill-switch tests pass;
- cross-region/cell relocation, full privacy disposition and overload guarantees
  are contracts, not current proof.

## Residual-unknown rule

An unresolved issue may not be hidden in prose. It must become one of:

```text
typed UNKNOWN that blocks or routes review
explicit deferred/manual capability boundary
owned research item with affected workflows and safe default
```

The final challenge reviewers below must report `OPEN` only for issues not already
covered by the normative contract, a negative-test family or an explicit deferral.

## Post-patch challenge round

|   # | Reviewer                        | New gap                                                                                                                                                                                                                                                                                                                                         | Patch/disposition      |
| --: | ------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
|  31 | Post-patch contract consistency | Unified change/reaction and resolver/churn enums; enforced signature deferral; clarified external-master, bulk visibility and handoff closure; propagated taint through signal/transform/child; bound external signals to ingress receipts; typed decision safety/legal RULE output; made node context and execution bindings machine-checkable | CONTRACTED and SAMPLED |
|  32 | Holistic final challenge        | Defined taint join/declassification, parallel read-consistency, child authority attenuation/expansion, server-bound presentation receipt and legal PIN limits                                                                                                                                                                                   | CONTRACTED and SAMPLED |
