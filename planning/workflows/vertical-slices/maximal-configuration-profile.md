# MAX-v1 Maximal BusinessIntent Configuration Profile

## Contract

Every vertical slice evaluates every axis below. The result for an axis is a
supported value set, a prohibited value set, or `NOT_APPLICABLE(reason, owner)`.
Ambient defaults and “typical customer” assumptions are invalid.

| Axis                   | Values and boundaries that must be considered                                                                                                                                              |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Initiator              | human self-service, manager, HR/admin, operator/support, agent, partner app, internal service, integration, schedule, rule, system event, delegated/represented actor                      |
| Channel                | native gRPC, grpcbridge HTTP/browser, GWC desktop/mobile/kiosk/accessibility-assisted, CLI/admin, agent tool, event, schedule, file/batch; internal-only/no endpoint                       |
| Tenant topology        | single/multi organization, legal entity and country; shared/dedicated cell; sandbox; tenant move/failover; suspended/closing tenant                                                        |
| Subject/population     | single subject, multiple relationships, frozen cohort, dynamic cohort, empty/one/max population, late entrant/removal, duplicates and partial membership                                   |
| Authority              | `HCM_NEXT`, external master, field-split/hybrid, authority handoff, disputed, stale, unavailable and unknown source                                                                        |
| Identity/assurance     | workforce/customer/workload identity; assurance levels; step-up; delegation; representation; recusal; JIT/support; break-glass; expired/revoked session                                    |
| Authorization          | tenant/org/relationship/population/field/action/purpose scope; allow, deny, hidden, partial, obligation/restriction, stale graph and policy outage                                         |
| Legal/policy           | zero/one/multiple jurisdictions, country/CBA/industry/customer packs, conflicting rules, mandatory floor, uncertainty, future/retro rule and policy outage                                 |
| Privacy/classification | public/internal/personal/sensitive/highly restricted/medical/privileged; compartment, minimization, consent/authority, residency, DLP, anonymized/pseudonymous/confidential actor          |
| Temporal               | immediate, future, retroactive, interval, open-ended, cycle/cutoff, business calendar, timezone/DST/tzdb change, effective-at versus known-at, late/early signal                           |
| Input epistemics       | present, absent, explicit null, unknown, partial, stale, redacted, conflicting, unverified claim, external observation, corrected/superseded fact                                          |
| Configuration/version  | current/future/old-compatible/incompatible/quarantined workflow, rule, schema, mapping, form, template, pack, model and reference-data revisions                                           |
| Human decisions        | none, one, sequential/parallel/quorum, unanimous/threshold, delegation, escalation, abstain/recuse, timeout, contest/appeal, SoD and reapproval after material change                      |
| Workflow topology      | direct capability, immediate workflow, long-running wait/signal, parallel/join, child/subworkflow, case, cycle, batch, filing, human continuity and manual fallback                        |
| Calculation/engine     | deterministic success, conditional, unknown/partial, exact decimal/rounding, caps/floors, currency/FX, population, eligibility, balance, qualification, matching, scenario and explanation |
| Transaction            | no write, one/multi-stream local write, reservation, optimistic conflict, serialization retry, idempotent replay, ambiguous commit, correction and cross-boundary partial success          |
| External effects       | none, API/webhook/file/message/document/signature/payment/filing/device/human delivery; one/multiple providers; ordered dependencies; reversible/irreversible/ambiguous effect             |
| Failure/outage         | caller cancellation, worker/process crash, database/object/cache/queue/control-plane/key/identity/provider/network/cell outage, timeout, overload, poison input and recovery restart       |
| Observation            | fresh/stale/partial/contradictory/unavailable observation, polling/webhook/file/manual evidence, source confidence and observation deadline                                                |
| Reconciliation/repair  | pass, mismatch, partial, unknown, late recovery, repairable/manual/irreversible, repair plan staleness, failed repair and verified postcondition                                           |
| Lifecycle              | draft, proposal revision, approval invalidation, submit, pause/resume, cancel before/after boundary, supersede, extend/shorten/reopen, correct/retro-correct, compensate and close         |
| Completion             | runtime, business, approval, obligation, external consistency, reconciliation, operational, closure and outcome dimensions independently complete/degraded/open                            |
| Records/evidence       | retention classes, legal hold, evidence package, hash/signature verification, export/redaction, destruction/anonymization, key loss/rotation and offline verification                      |
| Experience             | locale/fallback/RTL, name/address conventions, accessibility modes, assisted/non-digital route, notification preference/suppression, deadline continuity and safe error language           |
| Scale/performance      | zero/one/maximum payload, subjects, nodes, decisions, artifacts, effects and history; hot tenant/resource, fairness, backpressure, SLO and cost budget                                     |

## Mandatory high-risk interaction scenarios

Pairwise coverage is insufficient for these interactions:

1. delegated or agent initiator × sensitive field × external effect;
2. future/retro effective time × rule/configuration change × stale approval;
3. concurrent proposal × scarce reservation × ambiguous commit;
4. multiple jurisdictions/CBA × unknown fact × irreversible filing/payment;
5. confidential actor/medical evidence × human delegation × analytics/export;
6. batch/population change × partial provider success × targeted repair;
7. authority handoff × external outage × correction/supersession;
8. cancellation × irreversible effect × late observation;
9. tenant failover × duplicate trigger/retry × idempotency retention;
10. legal hold/retention × correction/crypto-erasure × evidence verification;
11. accessibility/manual continuity × deadline × identity assurance;
12. maximal scale × overload × fairness × recovery.

Each applicable slice gets an exact fixture and prohibited/expected state for
these scenarios. An intent cannot reach `SLICE_VERIFIED` using happy-path and
pairwise evidence alone when a mandatory interaction applies.

## Phase-aware use

Later-phase values remain design/test obligations without becoming Phase 1
implementation scope. The slice records `CONFORMANCE_ONLY`, `DEFERRED` or `OUT`
per value while retaining the gap and its owner. Phase depth changes only through
the execution plan and authority gates.
