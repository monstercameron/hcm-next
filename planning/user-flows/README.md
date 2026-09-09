# Human Capital Management Suite User-Flow Design Program

## Purpose

User flows describe how a person discovers, starts, understands, advances,
waits for, corrects and verifies a material HCM outcome. They complement—but do
not replace—the BusinessIntent, workflow and vertical-slice contracts.

```text
UserFlow
  describes the human experience and continuity

BusinessIntent
  describes what the participant is trying to accomplish

Workflow
  coordinates governed execution

VerticalSlice
  resolves the complete technical and business implementation
```

A user flow cannot invent business rules, authority, state transitions or
effects. It references those semantic owners and exposes missing contracts as
findings for later todos and tests.

## Current artifacts

- [Flow archetypes](archetypes.md) define reusable experience shapes.
- [Initial flow catalog](catalog.md) maps cross-persona journeys to intents,
  surfaces, archetypes and detailed references.
- [Promote into management](reference/promote-into-management.md) exercises a
  manager proposal, simulation, approvals, effective-date execution and repair.
- [Medical leave and return](reference/medical-leave-and-return.md) exercises
  employee self-service, restricted evidence, partial replanning and return.
- [Confidential HR case](reference/confidential-hr-case.md) exercises anonymity,
  compartments, human investigation, appeal and non-retaliation.
- [DataOps reconcile and repair](reference/dataops-reconcile-and-repair.md)
  exercises expert diagnosis, simulation, approval, execution and verification.

## UserFlowRecord

Every flow uses this complete record:

```text
UserFlowRecord
  flow_id / version / status / owner
  title / job-to-be-done / success definition
  root and child BusinessIntent references
  workflow and vertical-slice references

  primary participant
  other participants and representation/delegation
  persona, relationship and decision rights
  identity assurance and session assumptions

  entry points and discovery paths
  supported surfaces/channels/devices
  preconditions and unavailable-action behavior
  resume/deep-link/notification paths

  visible facts, provenance and freshness
  masked/hidden/summary-only facts
  requested input versus server-resolved truth
  forms, documents and evidence compartments

  ordered stages[]
    participant goal
    surface/view
    system state and available actions
    input and validation
    capability/intent/workflow transition
    visible result and evidence
    decision/wait/interruption
    error/recovery alternatives

  state presentation matrix
  cancellation/correction/supersession routes
  completion and follow-up behavior

  locale/RTL/name/date/money rules
  accessibility/accommodation/manual route
  responsive/mobile/kiosk/offline behavior
  privacy/safety/non-disclosure requirements

  analytics events and prohibited telemetry
  exact test scenarios and oracles
  open findings / todo links / evidence
  phase and maximal-configuration applicability
```

An empty field is not “not applicable.” `NOT_APPLICABLE` requires a reason and
owner. Unknown values remain visible findings.

## Participant vocabulary

```text
Participant
  HUMAN_SELF
  MANAGER
  HR_SPECIALIST
  REVIEWER_OR_APPROVER
  CANDIDATE_OR_EXTERNAL_PERSON
  DELEGATE_OR_REPRESENTATIVE
  CASE_PARTICIPANT
  HRIS_OR_PAYROLL_OPERATOR
  SUPPORT_OR_INCIDENT_OPERATOR
```

These are interaction roles, not authorization roles. A manager may be an
initiator, reviewer, observer or subject depending on the intent. Current
governance determines what each participant may see and decide.

Assisted use records the actual subject, representative/interpreter, authority,
communication route and evidence separately. “Act as” never silently becomes
impersonation.

## Stage vocabulary

Flows compose a small set of experience stages:

```text
DISCOVER       find a permitted action or receive a task/message
ORIENT         understand scope, prerequisites, authority and consequences
COLLECT        provide requested facts or evidence
VALIDATE       correct field/schema/evidence problems
SIMULATE       preview outcome, obligations, cost and effects
COMPARE        understand current versus proposed state
CONFIRM        acknowledge exact proposal/digest and material warnings
SUBMIT         create or advance a typed BusinessIntent
REVIEW         inspect evidence and make a permitted human decision
WAIT           await time, participant, signal, provider or observation
TRACK          inspect multidimensional progress and safe next actions
REPLAN         review a successor proposal after material change
EXECUTE        observe authorized execution without client-side mutation
RECONCILE      understand expected versus observed outcome
REPAIR         diagnose, simulate, approve and execute bounded correction
COMPLETE       understand closure, open obligations and retained evidence
CORRECT        append a correction or superseding intent
APPEAL         request independent review where applicable
```

Not every flow uses every stage, but omission of `TRACK`, truthful failure,
correction and accessible continuity requires explicit justification.

## Surface vocabulary

```text
ACTION_DISCOVERY
GUIDED_FORM
RECORD_CONTEXT_PANEL
SIMULATION_COMPARE
CONFIRMATION_REVIEW
TASK_DETAIL
APPROVAL_DETAIL
SECURE_INBOX
INTENT_TIMELINE
STATUS_SUMMARY
EVIDENCE_VIEWER
EXCEPTION_WORKBENCH
RECONCILIATION_COMPARE
REPAIR_WORKBENCH
ADMIN_INSPECTOR
```

These are semantic surfaces, not fixed URLs or component implementations. One
surface may render differently on desktop, mobile, kiosk, print or an assisted
route while invoking the same capabilities.

## Requested input versus server truth

Every collection step classifies fields:

```text
REQUESTED
  desired values supplied by the participant

TRUSTED_CONTEXT
  identity, tenant, purpose, session, locale and processing context injected by
  trusted boundaries

SERVER_RESOLVED
  worker state, relationships, authority, eligibility, balance, legal context,
  budget, position capacity, rules and source freshness

DERIVED_PREVIEW
  simulation/calculation results that are not yet authoritative state
```

The browser never asserts server truth. Hidden form fields are still untrusted.

## State presentation matrix

Each flow explicitly designs at least these states:

| State                          | Participant must understand                   | Required behavior                                             |
| ------------------------------ | --------------------------------------------- | ------------------------------------------------------------- |
| Loading                        | what is being resolved                        | bounded progress, cancellation where safe, no stale action    |
| Empty                          | why no result/action exists                   | authorized explanation and next step without existence leak   |
| Draft                          | what is saved versus unsaved                  | safe autosave, version conflict and resume behavior           |
| Validation blocked             | exact correctable problems                    | field and summary errors, focus/announcement, preserved input |
| Governance denied              | action unavailable or prohibited              | non-disclosing reason, permitted escalation/appeal if any     |
| Simulation ready               | expected changes and uncertainty              | provenance, assumptions, effects, approvals and expiry        |
| Submitted/running              | current multidimensional state                | timeline, owner, deadlines, safe cancel/correct actions       |
| Waiting                        | who/what/time is awaited                      | deadline, reminders, alternate route and authority changes    |
| Stale/replan                   | what materially changed                       | changed fields, invalidated decisions and successor proposal  |
| Partial/degraded               | what completed and what did not               | no false rollback; reconciliation and repair route            |
| Unknown/ambiguous              | why truth is not established                  | no blind retry; investigation and observation route           |
| Repair required                | expected versus observed mismatch             | bounded plan, risk, approval and verification                 |
| Completed                      | exact business/external/obligation dimensions | evidence and follow-up without flattening open dimensions     |
| Cancelled/corrected/superseded | what remains historically true                | append-only chronology and active successor link              |

## Maximal configuration

Every flow inherits
[MAX-v1](../workflows/vertical-slices/maximal-configuration-profile.md). The flow
must classify all experience-relevant variants, especially:

- self, manager, specialist, delegate, representative, agent-proposed and
  system-triggered entry;
- desktop, mobile, kiosk, deep link, secure message and assisted/non-digital
  continuity;
- fresh, stale, partial and conflicting source data;
- low/high assurance, session expiry and step-up;
- unauthorized, masked, compartmented and confidential evidence;
- locale fallback, RTL, name/address/date/money conventions;
- keyboard, screen reader, zoom/reflow, reduced motion, cognitive support,
  interpreter and manual accommodation;
- duplicate submit, concurrent edit, back navigation, refresh, network loss,
  offline draft and multi-device resume;
- legal/configuration/authority change while waiting;
- external success, rejection, timeout, ambiguity, partial application and late
  observation;
- cancellation before/after irreversible boundaries, correction, appeal and
  repair;
- individual, population/bulk and production-scale views.

## Flow-to-todo and TDD extraction

Each stage produces machine-readable references and findings:

```text
FlowStage
  -> required PageDefinition/widget/form
  -> BusinessIntent/capability/workflow transition
  -> entity/property/provenance visibility
  -> governance and decision receipt
  -> endpoint and message/deep-link route
  -> state/error/recovery presentation
  -> accessibility/localization/manual-continuity requirement
  -> analytics event and privacy rule
  -> exact browser/API/conformance/security/fault test
  -> unresolved semantic-owner finding
```

Findings are keyed by semantic owner and contract, not by screen. Shared gaps
generate one implementation todo with reverse links to every consuming flow.
Domain-specific outcomes remain domain-owned.

Minimum generated test classes are:

```text
HAPPY_PATH
VALIDATION
AUTHORIZATION_AND_DISCLOSURE
STALE_AND_CONCURRENT
INTERRUPT_AND_RESUME
ACCESSIBILITY_AND_LOCALIZATION
NETWORK_AND_DEPENDENCY_FAILURE
PARTIAL_UNKNOWN_AND_REPAIR
CANCEL_CORRECT_SUPERSEDE
CROSS_CHANNEL_PARITY
TELEMETRY_AND_PROHIBITED_DATA
PRODUCTION_SCALE
```

Every oracle asserts exact visible state, enabled actions, semantic result,
persisted effects and prohibited disclosure/effects. “Renders,” “returns 200” or
“does not crash” is insufficient.

## Maturity

```text
FLOW_CANDIDATE    actor/job/intent mapping exists
FLOW_DRAFT        stages and principal alternatives exist
FLOW_CONTRACTED   all semantic references and exact scenarios resolve
FLOW_IMPLEMENTED  authorized phase surface and mechanics exist
FLOW_VERIFIED     current browser/API/accessibility/fault evidence passes
```

A detailed flow is not proof that the underlying BusinessIntent is implemented.
The maturity reports remain independent and linked.
