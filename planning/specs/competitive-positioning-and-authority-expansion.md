# Competitive Positioning and Authority Expansion

## Purpose and evidence date

This specification prevents the long-term suite vision from obscuring HCM Next's
entry wedge or turning incumbent feature catalogs into an implementation backlog.
Competitive observations are time-sensitive. The baseline below was reviewed on
2026-08-13 against official UKG product, HCM, WFM, and platform announcements.
It must be revalidated at least quarterly and before a material positioning,
roadmap, pricing, or competitive sales claim.

Current evidence indicates that UKG already markets a connected enterprise
portfolio across HR, payroll, workforce management, talent, HR service delivery,
time, scheduling, workforce planning, compliance, analytics, communications, and
AI. UKG also uses `Workforce Operating Platform`, real-time workforce action, and
agentic orchestration language. Therefore those nouns and broad feature categories
are competitor context, not HCM Next differentiation.

Primary evidence:

- [UKG Pro product](https://www.ukg.com/products/ukg-pro)
- [UKG HCM](https://www.ukg.com/products/human-capital-management)
- [UKG Pro Workforce Management](https://www.ukg.com/learn/resources/product-info/ukg-pro-workforce-management-solution-guide)
- [UKG agentic orchestration announcement](https://www.ukg.com/company/newsroom/ukg-adds-agentic-orchestration-its-workforce-operating-platform-connecting-real-time-intelligence-frontline-execution)

Vendor statements establish what the vendor markets, not independent proof of
quality, customer outcome, implementation ease, or architectural internals.

## Positioning decision

```text
Incumbent suite proposition
  Run HR, pay, time, talent, and workforce operations in the suite.

HCM Next entry proposition
  Safely coordinate workforce change across everything you already run.

HCM Next durable proposition
  Turn every material workforce action into a governed, inspectable,
  authority-aware, reconcilable, and repairable transaction.
```

Day-one copy should favor the concrete entry proposition. `Workforce Operating
System` remains a destination architecture and portfolio description, not an
unqualified moat claim or near-term product promise.

## Current reality versus aspiration

Competitive material must preserve this distinction:

| Claim class            | Meaning                                               | Permitted evidence                                                                |
| ---------------------- | ----------------------------------------------------- | --------------------------------------------------------------------------------- |
| `INCUMBENT_CURRENT`    | Publicly available incumbent product claim/capability | Dated primary source plus scope/edition caveat                                    |
| `HCM_NEXT_IMPLEMENTED` | Running product behavior                              | Current automated tests, operating evidence, supported version and customer scope |
| `HCM_NEXT_PILOT`       | Bounded design-partner behavior                       | Named gate, tenant/workflow/connector constraints and known limitations           |
| `HCM_NEXT_CONTRACTED`  | Reviewed architecture contract                        | Specification only; never shown as shipped functionality                          |
| `HCM_NEXT_ASPIRATION`  | End-state hypothesis                                  | Clearly labeled future direction; no comparative score presented as current fact  |

Radar charts, feature matrices, demos, proposals, and agent-generated sales answers
must label claim class and `verified_at`. An aspiration cannot receive the same
visual treatment as a current incumbent capability.

## Table stakes and differentiated thesis

The following are necessary but not sufficient differentiators:

```text
workflow automation
APIs and webhooks
integration marketplace
dashboards and analytics
HR assistant/copilot
agentic orchestration
audit logs
suite breadth
"system of action" or "workforce operating platform" language
```

HCM Next must prove the combined transaction-control thesis:

```text
BusinessIntent
  -> immutable proposal and material digest
  -> source-authority-aware reads and planned writes
  -> cross-workflow/effective-time conflict control
  -> deterministic simulation
  -> exact approval binding
  -> execution-time revalidation
  -> atomic authoritative core
  -> governed external effects
  -> explicit observations
  -> intended-versus-observed reconciliation
  -> bounded repair and complete provenance
```

No single item is the moat. Differentiation exists only if the product delivers
the complete chain across heterogeneous systems with less risk and operating work
than customer-built coordination.

## Coexistence architecture is the wedge

HCM Next must model mixed estates as normal:

```text
                    HCM NEXT
          intent + governance + transaction truth
                         |
       +-----------------+-----------------+
       v                 v                 v
   HCM suite          Payroll/WFM      IAM/Finance/ITSM
 Workday or UKG       UKG or ADP       Entra/Okta/ERP
       |                 |                 |
       +-----------------+-----------------+
                         |
              observe + reconcile + repair
```

An incumbent may be:

```text
system of record for selected fields
execution destination for selected effects
observation source
coexisting product for a domain HCM Next does not intend to own
event producer or governed capability provider
```

Connector definitions must preserve edition, API/version, tenant configuration,
licensed feature, field-level authority, supported operation, consistency model,
rate limit, idempotency behavior, observation method, and reconciliation limits.
`Supports UKG` or another vendor name alone is not a valid compatibility claim.

## Domain disposition register

Every product/domain proposal receives exactly one current disposition per
customer segment and phase:

| Disposition                | Meaning                                                                         |
| -------------------------- | ------------------------------------------------------------------------------- |
| `OBSERVE`                  | Read/compare with authority remaining elsewhere                                 |
| `ORCHESTRATE`              | Govern intent and effects while the incumbent remains authoritative             |
| `OWN_SELECTED_SCOPE`       | HCM Next is authoritative for explicitly named fields/populations/jurisdictions |
| `REPLACE_SELECTED_PRODUCT` | Customer migrates a bounded incumbent function to HCM Next                      |
| `PARTNER_LONG_TERM`        | HCM Next intentionally integrates rather than builds                            |
| `DEFER`                    | Architectural contract only; no funded product work                             |
| `REJECT`                   | Does not fit strategy/economics/risk                                            |

The register is scoped, effective-dated, and evidence-backed. `Payroll`, `WFM`,
or `Benefits` is too broad a scope. Examples are `US salaried base-pay authority`,
`Germany time calculation`, or `UKG WFM schedule observation for hourly workers`.

### Initial default disposition hypotheses

These defaults govern planning until customer evidence and an authority gate
approve a narrower exception:

| Scope                                      | Phase 1 default                    | Medium-term hypothesis                                            | End-state option                                | Explicit constraint                                                        |
| ------------------------------------------ | ---------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------- | -------------------------------------------------------------------------- |
| Person/worker/employment facts             | `OBSERVE`                          | `ORCHESTRATE`, then selected authoritative fields                 | `OWN_SELECTED_SCOPE`                            | Incumbent remains authoritative until field/population cutover is approved |
| Manager/org/position changes               | `ORCHESTRATE`                      | `OWN_SELECTED_SCOPE` where shared-model advantage is proven       | Native People/Workforce domain                  | Must preserve coexistence, effective dating and incumbent reconciliation   |
| Compensation changes                       | `ORCHESTRATE`                      | Selected transaction authority                                    | Native Rewards scope                            | Payroll result and finance budget authority remain separately resolved     |
| Payroll calculation, filing and settlement | `PARTNER_LONG_TERM`                | Observe/reconcile/correct inputs and outputs                      | Native only through separate company-scale gate | No parity claim from integration or calculation prototypes                 |
| Time, attendance and advanced scheduling   | `PARTNER_LONG_TERM`                | Selected orchestration/DataOps                                    | Native only through separate WFM gate           | Hardware, offline, labor-rule and industry depth required                  |
| Benefits administration/carrier operations | `DEFER`                            | Orchestrate life-event effects                                    | Partner or selected native scope                | Carrier, eligibility, deduction and regulatory operations required         |
| Recruiting/ATS                             | `DEFER`                            | Orchestrate hire conversion and identity resolution               | Partner or selected native scope                | Do not duplicate mature ATS merely for suite breadth                       |
| Talent, learning and performance           | `DEFER`                            | Compose promotion/onboarding obligations                          | Selected native capabilities                    | Requires independent paid pull, not only workflow dependency               |
| HR service/case/document operations        | Minimal shared contracts           | Productize repeated Human Work/Case/Document usage                | `OWN_SELECTED_SCOPE`                            | Confidentiality, evidence and delivery semantics gate expansion            |
| Workforce access/IAM lifecycle             | `ORCHESTRATE`                      | Access policy, expected-state and reconciliation ownership        | First-class Access pillar                       | Identity providers remain execution authorities unless explicitly replaced |
| HRIS DataOps                               | Pilot-required operator slices     | Productize cross-system import/diff/lineage/redrive/configuration | First-class horizontal product                  | Must demonstrate independent customer use and willingness to pay           |
| Analytics/intelligence                     | Bounded Promotion evidence         | Governed cross-system semantic/temporal analysis                  | Native Intelligence plane                       | Derived results never silently become domain authority                     |
| Messaging/e-signature                      | Transactional provider integration | Govern intent/evidence and provider routing                       | Connectivity capability                         | Delivery/signature provider transport may remain partnered                 |

This table is a strategy hypothesis, not a customer authority assignment. Each
tenant's Source Authority registry remains the runtime truth.

## Authority absorption gate

Moving a scope from orchestration to ownership requires all of:

```text
1. customer pull and paid expansion evidence
2. shared-model and transaction-control advantage
3. complete domain semantics and correction model
4. regulatory/content/support operating model
5. migration, coexistence, cutover, rollback and tenant-exit design
6. SLO/RPO/RTO, scale, security and support evidence
7. reconciliation against the incumbent through parallel operation
8. acceptable build/operate liability and gross-margin case
9. named authority owner and deprecation/compatibility policy
10. executive gate approving what roadmap work is displaced
```

Failure of a gate keeps the scope at `OBSERVE`, `ORCHESTRATE`, `PARTNER_LONG_TERM`,
or `DEFER`. Drawing a native domain box or adding intent names does not establish
authority readiness.

## Native payroll and WFM posture

Authoritative payroll and mature workforce management are separate company-scale
commitments. They require domain kernels, regulatory content operations, country/
industry coverage, hardware/offline behavior where relevant, correction and filing
operations, year-round support, high-assurance release processes, and long-term
customer trust.

The default roadmap posture is:

```text
Phase 1/2  integrate, observe, reconcile and repair
Phase 3    own transaction truth and selected domain writes
Phase 4+   consider native payroll/WFM scopes only through authority gate
```

HCM Next must not imply parity from architecture diagrams, intent catalogs, or a
`Payroll`/`Workforce` plane label.

## AI and workflow differentiation

AI does not receive its own authority. The defensible claim is compilation into
governed execution:

```text
untrusted/user intent
       |
       v
agent discovers visible typed capabilities
       |
       v
typed BusinessIntent / workflow draft
       |
       v
schema + governance + side-effect + cost + safety validation
       |
       v
simulation + human publication/approval as required
       |
       v
deterministic capability execution
       |
       v
observation + reconciliation + outcome evaluation
```

Marketing may say `agent-generated behavior is compiled into governed execution`
only when the compiler, capability gateway, simulation, approval, deterministic
effects, and reconciliation path exist for the demonstrated scope.

## HRIS DataOps and Access hypotheses

Two adjacent hypotheses deserve explicit validation:

```text
HRIS DataOps
  administer the enterprise workforce-system topology, not one suite:
  import, map, compare, explain, redrive, reconcile and promote configuration

Workforce Access
  connect Person -> Employment -> Position -> Entitlement -> Account/Device/
  Physical Access with lifecycle reconciliation and repair
```

Neither is declared a moat without paid usage and measurable outcomes. Phase 1
may expose operator capabilities required to run the Promotion pilot; broader
productization follows repeated independent customer use.

## Competitive proof scorecard

The product must measure the claimed advantage against the customer's incumbent
process, not a generic feature checklist:

| Proof area              | Required measures                                                                                       |
| ----------------------- | ------------------------------------------------------------------------------------------------------- |
| Transaction safety      | prevented stale approvals/conflicts, unauthorized effects, duplicate effects, and payroll/access errors |
| Cross-system completion | percent effects observed, reconciliation latency, unresolved/degraded duration                          |
| Repair                  | mean time to diagnose/repair, redrive success, manual interventions, repeated failures                  |
| HRIS operations         | mapping/setup effort, cross-system diff time, audit/evidence preparation time                           |
| Change velocity         | lead time to modify/test/promote workflow and connector configuration                                   |
| Adoption                | eligible transactions governed, human completion, bypass rate and reasons                               |
| Economics               | implementation effort, operational labor removed, connector/support cost and gross margin               |
| Trust                   | expansion of granted authority, security exceptions, rollback/exit exercises and customer confidence    |

The pilot fails its strategic thesis if it merely moves tasks into another UI,
adds another integration dependency, or cannot show that removed complexity and
risk exceed the new system's cost and criticality.

## Phase implications

Phase 1 remains one Promotion/Compensation workflow and one design-partner system
topology. It must demonstrate:

```text
coexistence without demanding system-of-record replacement
field/domain source-authority resolution
one governed connector path with observation and repair
proposal/conflict/revalidation correctness beyond ordinary workflow
before/after evidence against the customer's incumbent operating process
credible bypass, rollback, recovery and exit procedures
```

Phase 1 explicitly does not promise UKG Pro feature parity, native global payroll,
deep WFM, broad connector certification, generic agentic orchestration, or a full
suite migration.

## Ownership and review

- Owner: Product Strategy with Architecture and Product Marketing review
- Review cadence: quarterly and before major positioning/roadmap changes
- Evidence owner: Competitive Intelligence/Product Marketing
- Architecture owner: Chief Architect or delegated architecture council
- Status: defined strategic constraint; Phase 1 proof metrics required
