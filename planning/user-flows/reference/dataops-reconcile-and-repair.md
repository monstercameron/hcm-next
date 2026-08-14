# User Flow: DataOps Reconcile and Repair

## Record

```text
flow_id: UF-012
primary: HRIS/DataOps specialist
other participants: domain owner, security/privacy reviewer, integration owner, affected business owner
root intents: CompareSystems / DetectDrift / CreateRepairPlan
child intents: SimulateRepair, ApproveRepair, ExecuteRepair, VerifyRepair
archetype: UF-A6 + UF-A4
```

Success means an authorized specialist can move from a bounded mismatch to an
explainable, simulated and verified RepairPlan without direct database edits,
blind provider redrive, authority confusion or recreation of the original
business transaction.

## Main flow

| Stage          | Specialist experience                                                                                                                            | Semantic action and required result                            |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------- |
| Discover       | Open from alert, reconciliation finding, workflow timeline or scoped search; display affected scope without leaking unauthorized subject details | authorized finding/query result with freshness                 |
| Orient         | Show expected, authoritative and observed truth classes, source authority, watermarks, materiality, prior attempts and open incidents            | immutable comparison context                                   |
| Diagnose       | Filter field-level differences and causal candidates; reveal payload artifacts only through separate authorization                               | append diagnosis/finding disposition, never mutate domain fact |
| Plan           | Choose or generate bounded actions with prerequisites, reversibility, credentials, effect ordering and expected post-state                       | immutable RepairPlanRevision                                   |
| Simulate       | Show exact planned local/external changes, untouched fields, risks, cost, approvals and ambiguity behavior                                       | zero authoritative/provider effect                             |
| Review         | Required domain/security/privacy reviewer sees plan digest and only relevant evidence; stale finding disables approval                           | proposal-bound decision receipt                                |
| Execute        | One idempotent action obtains current leases/fences and runs only approved actions; live progress separates logical operation and attempts       | RepairExecution with bounded attempts                          |
| Observe        | Show provider acceptance separately from fresh observed state; ambiguity disables blind retry                                                    | ExternalObservation and freshness/deadline                     |
| Verify         | Compare expected post-state and residual differences; return pass/fail/partial/unknown                                                           | RepairVerification and reconciliation result                   |
| Close/escalate | Close only on policy; otherwise revise plan, open incident or route manual action                                                                | append-only lifecycle and evidence package                     |

## Critical alternatives

- Authority changes between plan and execute: block/replan; never use old connector
  simply because it succeeded in simulation.
- Provider timeout after request: show ambiguous attempt and observation route;
  disable ordinary “retry” until ambiguity policy resolves.
- Difference is a legitimate external-authority change: update expectation/source
  dispute rather than overwrite external state.
- Large affected population: freeze snapshot, show aggregates/samples under
  authorization and execute bounded partitions with per-subject findings.
- Repair partly succeeds: preserve successful actions, show residual plan and
  prevent repeated irreversible effects.
- Direct SQL/manual provider action is unavoidable: create governed manual
  WorkItem with before/after evidence and reconciliation; never mark it automatic.
- Session/JIT access expires while viewing or executing: mask/close sensitive view,
  let already admitted worker follow its lease policy and require new access for
  further intervention.

## Initial TDD seeds

```text
TestRepairWorkbenchSeparatesExpectedAuthoritativeAndObservedValues
TestRepairDiagnosisDoesNotMutateDomainOrExternalState
TestRepairSimulationReturnsExactActionsAndZeroEffects
TestRepairApprovalDisablesWhenFindingPlanOrAuthorityIsStale
TestAmbiguousProviderAttemptDisablesBlindRedrive
TestRepairExecutionCannotExceedApprovedActionGraph
TestRepairPartialSuccessShowsResidualWithoutRepeatingIrreversibleEffect
TestRepairVerificationRequiresFreshObservationAndExactPostcondition
TestRepairBulkViewDoesNotLeakUnauthorizedMembersOrCounts
TestRepairJITExpiryRevokesFurtherInspectionAndPreservesExecutionTruth
```
