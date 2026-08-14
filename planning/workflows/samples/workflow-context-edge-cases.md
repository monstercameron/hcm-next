# Workflow Context Edge-Case Samples

These samples backfill integration, legal, AuthZ and human-work paths that were
absent from the original primitive-coverage suite. Each sample is a negative
conformance scenario: success is the safe handling of ambiguity or change, not
merely reaching `END`.

## 1. Source-authority handoff with a queued effect

```text
START transfer_991
  -> CAPABILITY authority.resolve(compensation)
     epoch=12, EXTERNAL_MASTER=Workday
  -> CAPABILITY integration.operation.plan
     operation=op_44, queued_authority_epoch=12
  -> WAIT connector capacity

AUTHORITY HANDOFF
  Workday -> HCM Next, epoch=13, cutover watermark=wd_881

WAKE op_44
  -> CAPABILITY integration.pre_send.revalidate
  -> DECISION queued epoch == active epoch?
       no
       -> TASK authority handoff review
       -> DECISION drain | cancel | quarantine | replan
       -> replan creates op_45 under epoch=13
       -> op_44 can never become SENT
       -> WAIT/OBSERVE op_45 when replanned
       -> RECONCILE op_45 or create RepairPlan
  -> END only after op_45 is reconciled or represented by a detached obligation
```

Required evidence: both authority decisions, handoff/fence, cutover watermark,
old and new operation lineage, reviewer authority and disposition. `DUAL_OBSERVE`
cannot send either operation.

## 2. Legal change during a durable wait

```text
START termination notice
  -> CAPABILITY jurisdiction.resolve
  -> RULE legal.evaluate under RuleSet R17
  -> APPROVAL bound to proposal + R17 + obligation set O4
  -> WAIT until effective date

RULE CHANGE
  R18 becomes effective before execution

WAKE
  -> CAPABILITY legal.revalidate
  -> DECISION TemporalLegalPolicy
       PIN            -> historical calculation retains R17
       RECOMPUTE      -> current legal capability evaluates R18
       REQUIRE_REVIEW -> invalidate approval, create counsel/HR task
       MIGRATE    -> compile migration + new proposal revision
       BLOCK      -> block material effect
  -> no path rewrites R17 evidence
```

Future-effective rules do not apply early. Retroactive rules create assessment/
correction obligations. An injunction after send makes the external outcome
unknown and starts reconciliation; it does not fabricate cancellation.
`PIN` preserves the historical calculation or interpretation only; it cannot
bypass a current prohibition or mandatory obligation found by live revalidation.

## 3. Approver authority changes after task assignment

```text
APPROVAL ManagerOf(worker)
  -> ResolutionSnapshot candidates=[Alice], graph watermark=71
  -> TASK delivered to Alice

MANAGER CHANGE
  current manager becomes Bob, graph watermark=72

Alice submits APPROVE
  -> authenticate current session + step-up
  -> re-resolve relationship
  -> DECISION requirement time policy
       PIN_AT_CREATION            -> apply only if policy explicitly permits
       CURRENT_AT_DECISION        -> Alice denied; task invalidated
       RECHECK_AT_DECISION_AND_EXECUTION -> Alice denied; Bob gets new task revision
  -> prior task/vote remains historical, never transferred
```

Variants must cover Alice's termination/rehire, acting-manager ambiguity,
delegation expiry, requester/delegate SoD, nested-group duplicate identity,
empty `ALL`, quorum denominator churn and one veto after quorum approvals.

## 4. Provider accepted but did not apply

```text
CAPABILITY access.entitlement.grant
  -> atomic local transaction + outbox op_90
  -> integration send
  -> provider HTTP 202
  -> state PROVIDER_ACCEPTED, not complete
  -> WAIT observation
  -> OBSERVE authoritative external state: entitlement absent
  -> RECONCILE expected != observed
  -> IntentState=COMMITTED
  -> CanonicalEntitlementState remains unchanged under EXTERNAL_MASTER
  -> ExternalConsistency=DEGRADED
  -> ReconciliationState=REPAIR_REQUIRED
  -> TASK/RepairPlan targeted to op_90
```

If the request times out after possible acceptance, state is `AMBIGUOUS` and the
runtime probes by semantic/idempotency identity before retry. Repair never reruns
successful sibling operations.

## 5. Bulk approval with one changed subject

```text
BulkPlan population snapshot=W88, 10,000 proposal digests
  -> per-item validation/AuthZ/legal/conflict
  -> human reviews homogeneous low-risk group

Before submit:
  subject 4,187 receives new compensation proposal digest

Bulk submit
  -> per-item CAS + authority revalidation
  -> authorized operator receives permitted-success count + one visible exception
  -> restricted viewers receive only a non-inferential partial status
  -> changed item is INVALIDATED without disclosing its identity to unauthorized viewers
  -> aggregate PARTIAL, not all-approved
  -> changed item requires a new presentation receipt and decision
```

Restricted items cannot leak through counts. Mixed risk, hidden exceptions,
different currencies/jurisdictions, stale candidate authority or differing
effect profiles make a group ineligible for bulk approval.

## 6. Offline/shared-device approval attempt

```text
User opens approval on enrolled shared mobile device
  -> encrypted draft/review state may be cached
  -> device goes offline
  -> APPROVE action requested
  -> DECISION risk policy: high-risk approval requires online fresh step-up
  -> preserve draft; do not emit ApprovalDecision
  -> show canonical deadline and accessible manual route

Reconnect
  -> reauthenticate user, device and session
  -> wipe previous-user partition
  -> refresh proposal/policy/authority/form/translation
  -> show material changes
  -> online confirm and submit once
```

Required variants: revoked device/session, failed wipe, stale policy/form,
duplicate replay, two-device conflict, screen-reader flow and non-digital route
that preserves deadline and read-back evidence.

## 7. Material masked field during approval

```text
Proposal includes visible job/level and restricted medical accommodation field
  -> field policy masks medical value for manager
  -> decision-safety evaluator asks whether hidden field can alter this decision
  -> result MATERIAL_UNKNOWN
  -> manager approval cannot proceed as if field were absent
  -> create compartmented specialist requirement
  -> specialist sees minimum necessary field under separate purpose
  -> aggregate certificate records both presentations and decisions
```

The manager never receives the medical field. The specialist does not receive
unrelated compensation data. Hidden-field mutation changes the relevant
presentation/decision digest and invalidates affected approval only.

## 8. Accessible localized legal notice

```text
Legal obligation requires ACKNOWLEDGED notice
  -> resolve recipient language/accessibility/channel profile
  -> render approved translation; locale != jurisdiction
  -> validate WCAG/RTL artifact and exact legal content digest
  -> secure inbox delivery
  -> external email contains attention-only content
  -> recipient requests assisted phone route
  -> authenticated staff reads exact version, captures interpreter/representative
  -> recipient read-back acknowledgement recorded
  -> obligation satisfied by ACKNOWLEDGED, not provider acceptance
```

If the accessibility channel fails, the workflow preserves the legal deadline,
routes manual continuity and records late/extension evidence. Accommodation data
is compartmented from ordinary approvers.

## 9. Integration ingress quarantine and correction

```text
Webhook raw bytes
  -> verify tenant/account/subscription signature
  -> persist receipt + dedupe atomically
  -> schema/malware/DLP
  -> crosswalk returns AMBIGUOUS person candidates
  -> QUARANTINED; no signal and no worker mutation
  -> restricted HumanTask resolves identity with evidence
  -> create new correlation revision
  -> reprocess same immutable receipt under mapping/crosswalk release M9
  -> late effective date routes CorrectionProposal
```

Same provider ID with different payload opens an incident. A gap in source
sequence buffers/reconciles before later events resume a workflow.

## 10. Parallel read-version divergence

```text
PARALLEL fork binds ReadConsistencyVector V12
  branch A reads org graph at V12
  branch B dependency offers legal policy V13
  -> branch B cannot silently mix V13 with V12
  -> JOIN detects incompatible material vector
  -> discard/recompute affected pure result under one new vector
  -> no combined effect until vectors and revalidation agree
```

## 11. Child workflow authority expansion attempt

```text
Parent approval scope: worker Jane, employment fields, purpose=promotion
SUBWORKFLOW IAM child requests all-US-worker access
  -> ChildAuthorityScope intersection is empty/out-of-parent-scope
  -> child start denied
  -> expansion requires new proposal + independently authorized certificate
  -> parent closure verifies actual child used-scope evidence
```

Service identity or a broader child definition cannot supply the missing scope.

## 12. Taint join and bounded sanitizer

```text
AGENT input A: CANONICAL_FACT
AGENT input B: HOSTILE_SUSPECTED + EXTERNAL_UNTRUSTED
  -> joined output labels retain all three + AGENT_DERIVED
  -> schema validation does not remove labels
  -> sanitizer S4 authorizes plain-text display for field summary only
  -> tool arguments and proposal material remain hostile/untrusted
  -> attempted use outside S4 scope is denied/quarantined
```

## 13. Presentation-receipt replay

```text
Server renders proposal P7 for Alice/session S1
  -> PresentationReceipt R7 binds full/visible diff and invalidators
Alice loses field access; proposal remains P7
Client submits APPROVE with copied visible digest but no valid R7
  -> reject
Client replays R7 from another principal/session or after invalidation
  -> reject
Server re-renders permitted current view as R8
  -> only a decision referencing live R8 may enter the vote CAS
```

## Coverage

Together these samples use existing primitive types but exercise cross-cutting
contracts absent from happy-path examples: authority fencing, legal evolution,
approval invalidation, ambiguous external effects, per-item bulk validity,
offline assurance, decision-safe masking, accessible/manual evidence and ingress
quarantine/correction, parallel consistency, child-scope attenuation, taint joins
and presentation-receipt replay.
