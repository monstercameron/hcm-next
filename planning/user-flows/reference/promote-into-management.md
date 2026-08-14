# User Flow: Promote Into Management

## Record

```text
flow_id: UF-007
primary: manager initiator
other participants: worker subject, current manager, HRBP, compensation and finance reviewers
root intent: PromoteWorker
child intents/effects: change assignment/manager/compensation/position, payroll sync, access recalculation, communications
archetype: UF-A2 + UF-A4 + UF-A6
workflow: ../../workflows/rewards/promotion-into-management.md
phase: Gate A proposal experience; later authoritative execution by phase gate
```

Success means the initiator can propose the desired future state, understand
authoritative current state and simulation, obtain proposal-bound decisions,
track effective-date execution and distinguish completed promotion from degraded
external consistency.

## Main flow

| Stage           | Participant experience                                                                                                                      | Semantic action and required result                                                |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Discover        | Worker action menu shows “Promote into management” only when currently permitted; unavailable action does not disclose hidden policy        | resolve `ActionDescriptor` for worker/context                                      |
| Orient          | Explain required target position/manager/pay, expected reviewers, effective-date behavior, simulation-only status and privacy               | resolve page/action/configuration versions                                         |
| Collect         | Enter desired job, position, manager, compensation, date and reason; show server-resolved current facts separately                          | save immutable `DraftRevision`; never trust hidden current-state fields            |
| Validate        | Inline plus summary errors identify missing/incompatible requested values without claiming final policy outcome                             | schema/reference validation, zero proposal/effect                                  |
| Simulate        | Show current/proposed diff, annualized exposure, position capacity, budget, conflicts, approvals, effects, uncertainty and source freshness | invoke simulation against immutable input snapshot; zero domain/external writes    |
| Confirm         | Initiator reviews exact proposal digest, material warnings, effective date and cancellation boundary                                        | create `ProposalRevision`; record confirmation receipt                             |
| Submit          | One idempotent action starts workflow; repeated click returns same intent/workflow                                                          | `promotion.submit` / start from proposal                                           |
| Track approvals | Timeline shows required decisions, safe aggregate status and deadline; reviewer identities are shown only where permitted                   | consume workflow/work-item state, not client counters                              |
| Wait/revalidate | Effective-date state explains that approval is not execution; material drift presents exact changed subplan and reapproval requirements     | revalidation result `VALID`, `REPLAN_REQUIRED`, `REAPPROVAL_REQUIRED` or `BLOCKED` |
| Execute         | Show admitted/prepared/committed state without offering a client-side “force” mutation                                                      | transaction receipt and authoritative revisions                                    |
| Reconcile       | Payroll/access outcomes display independently as pass, partial, unknown or repair-required                                                  | observation freshness and reconciliation evidence                                  |
| Repair          | Authorized specialist can open linked repair workbench; initiator sees safe status, not privileged provider detail                          | separate RepairPlan and verification                                               |
| Complete        | Summary distinguishes business, external consistency, reconciliation and obligations; evidence export is authorized separately              | completion policy and stable intent timeline                                       |

## Reviewer subflow

The approval detail shows the proposal-bound before/after state, visible evidence,
warnings, freshness, alternatives and decision consequences. Before decision it
revalidates reviewer identity, relationship, delegation, SoD and proposal digest.
Stale proposals disable the decision and link to the successor revision. The
worker subject does not automatically gain access to confidential reviewer notes.

## Critical alternatives

- Position or budget unavailable before submit: preserve draft and provide
  permissible alternatives; do not reserve during simulation.
- Worker/position/pay changed after approval: present a successor diff and which
  decisions remain valid; never silently reuse all approvals.
- Session expires at confirmation: retain draft, discard action token and require
  current reauthentication/step-up.
- Duplicate/multi-device submit: one intent and proposal; other device transitions
  to current status.
- Payroll applies but access is partial: promotion remains committed; show repair
  status rather than “promotion failed.”
- Cancel before irreversible boundary versus after commit: before may cancel;
  after requires a corrective intent.
- Manager initiates for worker outside scope: non-disclosing denial and zero
  worker detail.
- Mobile, screen reader and assisted route must expose the same proposal digest,
  warnings and result semantics.

## Initial TDD seeds

```text
TestPromotionActionDiscoveryDoesNotLeakUnavailableWorkerAction
TestPromotionDraftSeparatesRequestedValuesFromServerTruth
TestPromotionSimulationRendersExactDiffAndProducesZeroEffects
TestPromotionConfirmationBindsExactProposalDigest
TestPromotionDuplicateSubmitReturnsSameIntentAndWorkflow
TestPromotionApprovalDisablesOnProposalOrAuthorityDrift
TestPromotionReplanShowsOnlyMaterialChangedComponents
TestPromotionPartialAccessOutcomeShowsBusinessCompleteAndRepairRequired
TestPromotionFlowKeyboardScreenReaderAndMobileParity
TestPromotionFlowEmitsNoCompensationOrWorkerDataToTelemetry
```
