# Workflow Context: Integrations, Legal, Authorization and Human Work

This exploratory contract defines how cross-cutting concerns enter a workflow
without becoming hidden behavior inside nodes. It complements the
[step-type catalog](step-types.md).

## Context is resolved, scoped and evidenced

```text
External event / Human intent / Schedule / Agent proposal
                         |
                         v
                  Intake Trust Boundary
                         |
                         v
                   BusinessIntent
                         |
          +--------------+---------------+
          |              |               |
          v              v               v
    Identity/AuthN   Legal context   Source authority
          |              |               |
          +--------------+---------------+
                         v
                 Governance composition
        AuthZ + privacy + risk + entitlement + DLP
                         |
                         v
               Compiled workflow version
                         |
                         v
                 Per-step scoped context
                         |
                         v
              Capability / human / effect
                         |
                         v
             Evidence + observation + repair
```

A workflow instance does not receive one ambient mutable “context bag.” Context is
an immutable or versioned set of typed references. Each node declares the minimum
subset it needs. Context snapshots have authority, freshness, invalidators and
failure behavior.

```text
WorkflowContext
  TenantContext
  PrincipalContext
  SubjectContext
  OrganizationContext
  LocaleContext
  JurisdictionContext
  SourceAuthoritySnapshot
  GovernanceSnapshot
  DataClassificationContext
  RiskContext
  EntitlementContext
  ExecutionContext
  IntegrationContext?
  HumanInteractionContext?
```

Locale selects language/format/accessibility presentation. Jurisdiction determines
legal authority and never comes from locale alone.

## External systems entering the workflow

External data can enter through API commands, webhooks/events, scheduled sync,
managed files, observations or human-attached evidence. Every path terminates at
an ingress adapter before workflow correlation.

```text
External system
      |
      v
Connector ingress / API edge / file gateway
      |
      +-- authenticate source and connection
      +-- bind tenant, cell and organization scope
      +-- validate signature, timestamp, replay window and size
      +-- malware/DLP/quarantine untrusted content
      +-- validate transport schema/version
      +-- persist immutable receipt before acknowledgement
      +-- map external IDs/codes through versioned crosswalk
      +-- normalize to canonical event/command/observation
      |
      v
IntegrationReceipt
      |
      +-- create a new BusinessIntent
      +-- correlate to an existing SIGNAL subscription
      +-- publish an Observation
      +-- quarantine/unmatched-event human work
```

External source authentication does not authorize an HCM action. After mapping,
the resulting service principal still passes capability AuthZ, purpose, legal,
source-authority and risk decisions.

### Required ingress evidence

```text
provider event/message/file ID
connector definition and connection versions
authenticated external principal/certificate/key version
tenant/cell/organization binding
received_at and trusted-time uncertainty
signature/replay/size/malware/DLP results
raw artifact reference/hash under restricted retention
transport schema and canonical mapping versions
canonical subject/resource correlation
dedupe decision and prior receipt reference
processing disposition
```

Unknown subjects, unknown schema versions, ambiguous crosswalks, late/out-of-order
events and unavailable source authority do not silently create or mutate workers.
They route to quarantine, retry or governed human resolution.

## External effects leaving the workflow

Workflows never call vendor APIs directly. A governed capability commits the
authoritative local transaction and durable outbox operation first.

```text
Workflow CAPABILITY
       |
       v
AuthZ/legal/source-authority revalidation
       |
       v
Atomic local commit
  locally owned facts + ledger/projection + outbox
  external-mastered fields: intent/effect evidence only
       |
       v
Integration scheduler
  tenant fairness + vendor capacity + resource ordering
       |
       v
Pre-send revalidation
  authority fence + expected external version + DLP/destination
       |
       v
External operation
       |
       +-- rejected/retryable/dead-letter/ambiguous
       |
       v
OBSERVE authoritative external state
       |
       v
RECONCILE -> complete or RepairPlan/Incident
```

Every operation carries `external_resource_key`, causal predecessor/sequence,
expected external version, source-authority decision/fence, semantic idempotency
key, payload digest, classification/purpose/destination decision and observation
requirement. Provider acceptance is transport evidence only.

Authority mode determines what the local transaction may claim:

```text
LOCAL_MASTER
  commit canonical domain fact and critical projection
  dispatch external replicas afterward

EXTERNAL_MASTER
  commit intent, plan, effect identity and outbox only
  do not publish the proposed value as canonical domain truth
  promote only an authoritative owner observation

SHARED_FIELD
  require an explicit deterministic merge/conflict contract
  implicit dual write is forbidden
```

Queued operations retain the authority epoch under which they were planned. An
authority handoff moves stale queued operations to `HANDOFF_REVIEW`; it never
silently sends them under the new authority policy.

## Legal requirements enter as typed decisions and obligations

The workflow engine does not interpret law. It supplies facts to deterministic
legal/regulatory capabilities and consumes typed results.

```text
Business proposal + jurisdiction facts + effective time
                         |
                         v
             Jurisdiction/Legal resolver
                         |
                         v
      Prohibit | Require | Restrict | Calculate | Inform
                         |
                         v
                  ObligationSet
        +----------------+----------------+
        v                v                v
 insert workflow step  constrain step   block publication/execution
```

An obligation contains authority, subject, trigger, due time, required action,
evidence, responsible party, satisfaction condition, risk/penalty class, source
and rule versions, effective interval, composition strategy and dispute/waiver
policy.

### Legal insertion points

```text
definition publication   validate required step classes and rule compatibility
intent preflight          resolve current obligations and prohibitions
simulation                show notices, approvals, calculations, deadlines, cost
approval                  bind legal/rule snapshot into exact proposal digest
timer/signal wait         track changes/expiry and create invalidators
execution revalidation    reevaluate current law, facts and jurisdiction
external effect           enforce filing/destination/evidence restrictions
closure                   prove obligation satisfaction or explicit waiver/dispute
```

Rule changes do not silently rewrite a live instance. Each obligation declares
whether the workflow pins the original interpretation, reevaluates at defined
points, requires review/migration, or blocks execution. Historical calculations
and decisions remain reproducible under their original versions.

`UNKNOWN` legal applicability is fail-closed for irreversible employment,
financial, privacy, filing and access effects unless an explicitly authorized
manual-continuity policy supplies bounded alternative evidence.

## Authorization is layered and repeated

Authentication answers who/what is acting. Authorization is a composed decision
at multiple boundaries:

```text
1 START AUTHORITY
  may this principal create this intent for this subject/scope/purpose?

2 DATA READ AUTHORITY
  which facts/fields/artifacts may each node read?

3 PROPOSAL AUTHORITY
  may the principal propose these exact fields/effective ranges?

4 HUMAN-WORK AUTHORITY
  may this person receive/claim/delegate/view this task?

5 DECISION AUTHORITY
  may this person approve/reject this exact proposal now?

6 EXECUTION AUTHORITY
  may the workflow/service execute this capability now?

7 EFFECT AUTHORITY
  may this workload send this payload to this external destination?

8 OBSERVATION/REPAIR AUTHORITY
  may this actor view drift, author/approve/execute/verify repair?
```

The composed authorization input includes capability, resource, relationships,
organization/population, fields, purpose, context, channel, risk, session/device
assurance, delegation, legal/privacy restrictions, source authority and data
freshness. Deny dominates; restrictions intersect; obligations union; unknown or
contradictory material decisions fail closed.

Network location grants no implicit service authority. Human, workload, connector
and agent identities have distinct credentials, audiences and allowed
capabilities, consistent with the resource-focused zero-trust model in
[NIST SP 800-207](https://csrc.nist.gov/pubs/sp/800/207/final) and its
[cloud-native application access model](https://csrc.nist.gov/pubs/sp/800/207/a/final).

## Approval resolution and authority are separate

```text
ApprovalRequirement
  resolver expression
  cardinality/quorum/veto
  scope and effective-as-of policy
  proposal digest
  deadline/escalation/delegation
  separation-of-duties constraints
  step-up/session requirement
  invalidators

Task creation
  resolver -> candidate recipients

Decision submission
  authenticate current actor
  -> check task access
  -> re-resolve/revalidate actual decision authority
  -> verify exact proposal digest and current invalidators
  -> atomically record immutable vote/decision
```

Receiving a task is not proof of authority. Named recipients, roles,
relationships, groups, any/all/quorum and dynamic capability resolvers require
fallback, unavailability and effective-time behavior. A principal cannot approve
their own request or two supposedly independent requirements unless policy
explicitly permits it.

## Human approval ergonomics

A high-risk approval view must answer, without log inspection:

```text
What am I being asked to decide?
Who and what will be affected?
What changes from current to proposed?
When does it become effective?
Why is my approval required and what authority do I have?
Which information is hidden and why?
What legal/policy obligations and warnings apply?
What downstream effects, cost, reversibility and failure modes exist?
What changed since an earlier review?
What happens if I approve, reject, request information or do nothing?
```

### Approval interaction contract

```text
summary first, details progressively disclosed
current/proposed diff with units, dates and source freshness
plain-language reason plus authoritative policy/rule links
material warnings separated from informational notices
exact proposal revision/digest visibly identified
approve, reject and request-information actions equally reachable
reason requirements explained before submission
review/confirm/correct step for legal, financial and destructive actions
clear success, pending, invalidated and duplicate-submission feedback
deadline, timezone, escalation and delegation state
accessible keyboard/screen-reader/focus/status behavior
localized content without changing jurisdiction semantics
non-digital/accommodation route where required
```

WCAG 2.2 requires error-prevention support for legal, financial and data-changing
transactions through reversibility, checking or review/confirmation; HCM Next
applies that principle to the complete approval process, not only its submit
button. See [WCAG 2.2 SC 3.3.4](https://www.w3.org/WAI/WCAG22/Understanding/error-prevention-legal-financial-data.html).

### Cognitive and organizational ergonomics

- Do not overload one approval with unrelated decisions; split requirements while
  retaining the parent proposal.
- Do not use dark patterns, preselected approval, misleading color-only status or
  asymmetric confirmation.
- Collapse unchanged information; highlight material changes and uncertainty.
- Explain abbreviations, currency/frequency/timezone and local-date semantics.
- Provide workload-aware queues, batching only for genuinely equivalent low-risk
  items, save/draft where safe, and explicit interruption recovery.
- Never allow bulk approval to hide per-subject exceptions, conflicts or changed
  proposal digests.
- Delegation shows delegator, scope, period and whether the delegate may further
  delegate; sensitive tasks may prohibit delegation.
- Rejection and request-more-information preserve dignity, privacy and a clear
  correction path.

## Human work, integration and legal interaction

```text
External event says background check completed
      -> ingress verifies/correlates SIGNAL
      -> legal RULE says human adjudication required
      -> TASK created in restricted queue
      -> reviewer sees permitted result, obligation and decision criteria
      -> decision authority revalidated
      -> typed submission resumes workflow
      -> external adverse-action/document/message obligations execute
      -> delivery/response observed and reconciled
```

No cross-cutting plane may bypass another: a legally required action still needs
AuthZ; an authorized action may still be prohibited by law; an approved action
must still pass execution-time authority and source-version checks; an external
event cannot force a state change solely because it was signed.

## Failure and degraded operation matrix

| Dependency failure                | Default workflow behavior                                            |
| --------------------------------- | -------------------------------------------------------------------- |
| Identity/session uncertainty      | do not accept high-risk human decision; preserve draft/task          |
| AuthZ unavailable/stale           | fail closed for reads/effects; queue only already accepted safe work |
| Legal applicability unknown       | block material effect; create specialist work/obligation             |
| Integration ingress invalid       | reject/quarantine; do not correlate signal                           |
| External destination unavailable  | durable queue with deadline/priority; no false completion            |
| Approver unavailable              | delegation/fallback/escalation policy; never silently auto-approve   |
| Proposal changed                  | invalidate prior decisions and show material diff                    |
| Reference/jurisdiction changed    | re-resolve according to pinned/review/recalculate policy             |
| Human submits twice               | atomically replay original decision or report proposal invalidation  |
| Accessibility channel unavailable | preserve deadline and route accommodation/manual continuity          |

## Required workflow evidence

```text
intake/authentication/integration receipt
context and source-authority snapshots
legal decisions, obligations and versioned sources
AuthZ decisions/restrictions/obligations at each material boundary
human task assignment/claim/delegation/escalation/completion
approval requirement, exact proposal binding and immutable votes
transaction/effect/observation/reconciliation/repair references
content/template/locale/accessibility and delivery evidence
completion dimensions and unresolved obligations
```

## Open research edges

The adversarial review must test at least:

```text
late and out-of-order external events
source-authority handoff with queued operations
legal change while waiting for approval/effective date
manager/role change after task assignment
delegation loops and SoD across subworkflows
bulk approval with one changed subject
offline/shared-device approval attempts
masked fields that materially affect an approver's decision
external provider accepts but does not apply an effect
manual continuity during identity/legal/integration outage
accessibility and localization across a complete multi-step process
```
