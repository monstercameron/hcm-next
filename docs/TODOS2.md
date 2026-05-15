# TODO: Dynamic Sync/Async Approval Gates

## Target Capability

Build reusable approval-gate orchestration that supports both ordered and unordered approvals:

```text
sync approval = ordered, one approver or approver stage at a time
async approval = unordered, many approvers open at once
approval gate = the pass/fail rule that decides whether the workflow advances
```

The first workflow using this capability is:

```text
position.headcount_requisition.approval
```

Scenario:

```text
HarborCare requests one new Senior Registered Nurse position for Cambridge Nursing.
The request first moves through a user-defined leadership chain in order.
Then Finance, HRBP, Compensation, Medical Director, and Clinic Ops review in parallel.
The async gate passes with 3 of 5 approvals unless a veto holder rejects.
The workflow proves dynamic approver resolution, quorum math, rejection handling, stale task protection, UX contract shape, ledger events, and idempotency.
```

## Design Intent

- Keep the public mutation surface workflow-native: `POST /workflow-intents` and `POST /workflow-instances/:id/transitions`.
- Separate approver resolution from approval-gate evaluation.
- Snapshot resolved approvers when a gate opens so audit history remains stable if org structure changes later.
- Reuse existing `approval_tasks` where possible, adding `approval_groups` as the gate-level durable record.
- Do not hard-code one workflow's approval chain into the generic runtime.
- Keep all pass/fail decisions auditable through ledger events.
- Keep workflow-level `expectedVersion` for major transitions, but add task-level decision version semantics so parallel approvers do not fight over stale workflow versions.

## Parallel Workstreams

| Workstream                         | Owner Scope                                                                                                                                  | Primary Files                                                                                                                                                                           |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Config, Constants, and Schema   | Workflow intent, workflow states, ledger events, approval-gate config types, headcount workflow config, config validation tests              | `src/platform/foundation/constants/*`, `src/workflows/shared/workflow-config.ts`, `src/workflows/configs/*`, `src/workflows/shared/workflow-config.test.ts`, `docs/workflow-schemas.md` |
| 2. Data Store and Gate Engine      | `approval_groups` records/repository/migration, pure approval-gate evaluator, task version metadata, unit tests                              | `src/platform/data-store/*`, `src/workflows/shared/approval-gate.ts`, `src/workflows/shared/approval-gate.test.ts`, migrations                                                          |
| 3. Runtime and Approver Resolution | Headcount workflow handlers, sync gate opening/advancement, async gate opening/joining, resolver semantics, permission checks, ledger events | `src/workflows/headcount-requisition/*`, `src/workflows/legal-name-change/service.ts`, `src/workflows/shared/*`                                                                         |
| 4. E2E, UX Contract, and Docs      | HarborCare fixtures, full E2E, negative-path tests, API contract checks, docs/demo updates                                                   | `src/tests/e2e/*`, `docs/api-demo.md`, `docs/fixtures/*`, `docs/TODOS2.md`                                                                                                              |

## Workstream 1: Config, Constants, and Schema

Goal: define a reusable approval-gate config shape and a headcount requisition workflow that exercises ordered and unordered approvals.

### Constants

- [x] Add workflow intent constant `position.headcount_requisition.approval`.
- [x] Add workflow states:
  - [x] `waiting_sync_approval`
  - [x] `waiting_async_approval`
  - [x] `approval_gate_passed`
  - [x] `approval_gate_failed`
  - [x] `waiting_approval_repair`
- [x] Add permission constants:
  - [x] `position.headcount.request`
  - [x] `position.headcount.leadership_approve`
  - [x] `position.headcount.finance_approve`
  - [x] `position.headcount.hrbp_approve`
  - [x] `position.headcount.compensation_approve`
  - [x] `position.headcount.medical_director_approve`
  - [x] `position.headcount.clinic_ops_approve`
  - [x] `position.headcount.execute`
  - [x] `position.headcount.view_restricted_summary`
- [x] Add ledger event constants:
  - [x] `HeadcountRequisitionSubmitted`
  - [x] `ApprovalGateOpened`
  - [x] `ApprovalGateTaskCreated`
  - [x] `ApprovalGateTaskDecided`
  - [x] `ApprovalGatePassed`
  - [x] `ApprovalGateFailed`
  - [x] `ApprovalGateTaskCanceled`
  - [x] `HeadcountRequisitionApproved`
  - [x] `HeadcountRequisitionRejected`
  - [x] `HeadcountRequisitionExecuted`

### Config Types

- [x] Extend `WorkflowGraphNodeConfig.type` with `approval_gate`.
- [x] Add `WorkflowApprovalGateConfig`.
- [x] Support gate `mode` values:
  - [x] `sequential`
  - [x] `parallel`
- [x] Support approver resolver types:
  - [x] `actor`
  - [x] `role`
  - [x] `manager_chain`
  - [x] `department_lead`
  - [x] `cost_center_owner`
  - [x] `seniority_level`
  - [x] `workflow_field`
- [x] Support pass rule types:
  - [x] `all_required`
  - [x] `quorum`
  - [x] `percentage`
  - [x] `any_one`
  - [x] `weighted`
  - [x] `role_quorum`
  - [x] `composite`
- [x] Support failure policy types:
  - [x] `stop_workflow`
  - [x] `send_to_repair`
  - [x] `continue_until_threshold_impossible`
  - [x] `require_all_responses`
  - [x] `veto_only`
  - [x] `escalate_on_timeout`
- [x] Add config serialization tests for approval-gate nodes.
- [x] Validate every approval-gate node references a known interaction.
- [x] Validate every approval-gate pass/fail outcome points to a real node.
- [x] Validate every approver resolver has the required fields.
- [x] Validate every pass rule has enough numeric configuration.

### Headcount Workflow Config

- [x] Add `src/workflows/configs/position-headcount-requisition.workflow.json`.
- [x] Register the config in `workflow-config.ts`.
- [x] Use `subjectType: "position"`.
- [x] Set start actors to HR admin and clinic ops admin.
- [x] Add intake fields:
  - [x] `department`
  - [x] `team`
  - [x] `location`
  - [x] `costCenter`
  - [x] `jobCode`
  - [x] `title`
  - [x] `level`
  - [x] `requestedFte`
  - [x] `targetStartDate`
  - [x] `salaryRangeMin`
  - [x] `salaryRangeMax`
  - [x] `businessJustification`
  - [x] `selectedLeadershipApprovers`
- [x] Define sync gate `leadership_chain_gate` using selected leadership approvers in order.
- [x] Define async gate `cross_functional_gate` with five approvers.
- [x] Configure async gate as 3 of 5 approvals required.
- [x] Mark Finance and Medical Director as veto holders.
- [x] Add ready-to-execute and completed interactions.
- [x] Add repair/rework interaction for rejected or more-info states.

### Config Unit Tests

- [x] Config is registered by intent.
- [x] Config has one start node.
- [x] Graph includes one sequential gate and one parallel gate.
- [x] Sequential gate uses a workflow-field resolver for user-defined approver order.
- [x] Parallel gate opens all five approver tasks at once.
- [x] Async gate pass rule is quorum 3 of 5.
- [x] Veto holders are declared and stable.
- [x] All action handlers are known.
- [x] All gate node outcomes are lower snake case.
- [x] All ledger event names in config exist in constants.

## Workstream 2: Data Store and Gate Engine

Goal: add the durable approval-group concept and pure pass/fail evaluation logic.

### Data Store

- [x] Add `ApprovalGroupRecord`.
- [x] Add `approvalGroups` map to the in-memory store.
- [x] Add repository methods:
  - [x] `create`
  - [x] `findById`
  - [x] `findByWorkflow`
  - [x] `findActiveByWorkflow`
  - [x] `update`
- [x] Add migration for `approval_groups`.
- [x] Add approval task fields or metadata conventions:
  - [x] `approvalGroupId`
  - [x] `gateNodeId`
  - [x] `sequenceIndex`
  - [x] `weight`
  - [x] `isVetoHolder`
  - [x] `resolvedFrom`
  - [x] `taskVersion`
- [x] Ensure repository reads can find pending tasks by approval group.
- [x] Ensure pending task lookup remains compatible with legacy single-approval workflows.

### Gate Engine

- [x] Add pure module `src/workflows/shared/approval-gate.ts`.
- [x] Define `ApprovalGateMode`.
- [x] Define `ApprovalGatePassRule`.
- [x] Define `ApprovalGateFailurePolicy`.
- [x] Define `ApprovalGateDecision`.
- [x] Define `ApprovalGateEvaluationInput`.
- [x] Implement sequential gate evaluation:
  - [x] no decision while current sequence task is pending
  - [x] approve final task passes all-required gate
  - [x] rejection applies configured failure policy
  - [x] request-more-info routes to repair
- [x] Implement parallel quorum evaluation:
  - [x] pass when approvals reach required quorum
  - [x] fail when required quorum is mathematically impossible
  - [x] wait while outcome remains possible
  - [x] cancel remaining pending tasks after pass/fail
- [x] Implement veto evaluation:
  - [x] veto rejection fails immediately
  - [x] non-veto rejection only counts against quorum
- [x] Implement weighted evaluation.
- [x] Implement percentage evaluation.
- [x] Implement any-one evaluation.
- [x] Implement role-quorum evaluation.
- [x] Implement composite evaluation.

### Gate Engine Unit Tests

- [x] Sequential gate opens only first task.
- [x] Sequential gate advances to next task on approval.
- [x] Sequential gate passes after final ordered approval.
- [x] Sequential gate fails on reject when policy is `stop_workflow`.
- [x] Sequential gate routes to repair when policy is `send_to_repair`.
- [x] Parallel 3 of 5 passes on third approval.
- [x] Parallel 3 of 5 does not pass after only two approvals.
- [x] Parallel 3 of 5 fails after three rejections.
- [x] Parallel 3 of 5 waits after two approvals and two rejections.
- [x] Veto rejection fails immediately.
- [x] Weighted rule passes when approval weight threshold is met.
- [x] Percentage rule passes at the configured percentage.
- [x] Any-one rule passes on first approval.
- [x] Role-quorum requires each required role bucket.
- [x] Composite rule requires every child rule.
- [x] Pending tasks are ignored for final counts except for impossibility math.
- [x] Canceled/superseded tasks do not count.
- [x] Duplicate decisions do not change outcome.
- [x] Task version mismatch produces a validation/conflict error.

## Workstream 3: Runtime and Approver Resolution

Goal: make `position.headcount_requisition.approval` executable through the existing workflow command surface.

### Start and Submit

- [x] Let configured workflows without employee context start without loading an employee projection.
- [x] Add headcount service module.
- [x] Route headcount submit transitions from generic `transitionWorkflow`.
- [x] Validate headcount intake fields.
- [x] Validate requested FTE is positive.
- [x] Validate salary range min is less than or equal to max.
- [x] Validate target start date is present.
- [x] Snapshot the submitted request into workflow context.
- [x] Create a change request for the headcount requisition.
- [x] Create proposed change rows for position, cost center, and compensation band.
- [x] Append `HeadcountRequisitionSubmitted`.

### Approver Resolution

- [x] Resolve explicit actor IDs from `selectedLeadershipApprovers`.
- [x] Preserve user-defined order for sync gate.
- [x] Reject duplicate approvers in one sequential chain.
- [x] Resolve finance approver from finance admin role.
- [x] Resolve HRBP approver from HR admin or HRBP role.
- [x] Resolve compensation approver from compensation admin role.
- [x] Resolve medical director approver from medical director or clinical admin role.
- [x] Resolve clinic ops approver from clinic ops admin role.
- [x] Capture resolver metadata in task metadata.
- [x] Deny approval attempts by actors not assigned to that task.

### Sequential Gate Runtime

- [x] Create an approval group for `leadership_chain_gate`.
- [x] Create the first leadership approval task only.
- [x] On approval, mark current task approved.
- [x] Append `ApprovalGateTaskDecided`.
- [x] If more leadership approvers remain, create next task.
- [x] If no approvers remain, mark gate passed.
- [x] Append `ApprovalGatePassed`.
- [x] Open async gate after sync gate passes.
- [x] Reject workflow or route repair based on failure policy.

### Parallel Gate Runtime

- [x] Create an approval group for `cross_functional_gate`.
- [x] Create all five async approval tasks at once.
- [x] Expose all pending tasks to their assigned actors.
- [x] Allow approvals in any order.
- [x] Re-evaluate gate after each decision.
- [x] Pass gate at 3 approvals if no veto rejection occurred.
- [x] Fail gate immediately on veto rejection.
- [x] Fail gate when 3 approvals can no longer be reached.
- [x] Cancel remaining pending tasks after gate pass.
- [x] Cancel remaining pending tasks after gate fail.
- [x] Append `ApprovalGateTaskCanceled` for canceled tasks.
- [x] Mark workflow approved after async gate passes.
- [x] Append `HeadcountRequisitionApproved`.

### Execute and Idempotency

- [x] Execute approved headcount requisition as HR admin or system.
- [x] Append `HeadcountRequisitionExecuted`.
- [x] Complete workflow.
- [x] Replay submit with same idempotency key returns cached response.
- [x] Replay approval with same idempotency key returns cached response.
- [x] Replay execute with same idempotency key returns cached response.
- [x] Duplicate approval decision does not double-count toward quorum.
- [x] Wrong workflow version still fails for major transitions.
- [x] Task-level version protects parallel approvals from stale task decisions.

## Workstream 4: E2E, UX Contract, and Docs

Goal: prove the approval-gate capability through a realistic HarborCare workflow and document what was built.

### Fixtures

- [x] Reuse HarborCare seeded actors where possible.
- [x] Add stable fixture aliases for:
  - [x] requester actor
  - [x] leadership approver 1
  - [x] leadership approver 2
  - [x] finance approver
  - [x] HRBP approver
  - [x] compensation approver
  - [x] medical director approver
  - [x] clinic ops approver
- [x] Add stable headcount request fixture.
- [x] Add helpers for transition requests.
- [x] Add helpers for approval task lookup by gate.
- [x] Add helpers for ledger event assertions.

### E2E Happy Path

- [x] Start workflow as HR admin.
- [x] Submit headcount request with two selected leadership approvers.
- [x] Assert only leadership approver 1 gets the first sync task.
- [x] Assert leadership approver 2 has no task yet.
- [x] Approve leadership approver 1.
- [x] Assert leadership approver 2 task is created.
- [x] Approve leadership approver 2.
- [x] Assert sync gate passes.
- [x] Assert async gate opens five tasks at once.
- [x] Approve Finance.
- [x] Approve Compensation.
- [x] Approve Clinic Ops.
- [x] Assert async gate passes at 3 of 5.
- [x] Assert HRBP and Medical Director pending tasks are canceled.
- [x] Execute as system or HR admin.
- [x] Assert workflow completed.
- [x] Assert ledger includes gate opened, task created, task decided, gate passed, task canceled, approved, executed, completed events.
- [x] Assert replay execute does not duplicate ledger events or tasks.

### E2E Negative Paths

- [x] Wrong actor cannot approve leadership task.
- [x] Leadership approver 2 cannot approve before their sequence opens.
- [x] Async approvals can occur in any order.
- [x] Veto holder rejection fails async gate immediately.
- [x] Quorum-impossible path fails when three async reviewers reject.
- [x] Request-more-info routes workflow to repair.
- [x] Missing selected leadership approvers fails validation.
- [x] Duplicate selected leadership approvers fails validation.
- [x] Version conflict response tells client to refetch.
- [x] Task version conflict response tells client to refetch task.

### UX Contract Tests

- [x] Start response includes title, state, status, and next actions.
- [x] Intake interaction labels are human-readable.
- [x] Required fields are present in JSON schema.
- [x] Leadership gate interaction names the current approver.
- [x] Async gate interaction names quorum progress.
- [x] Approval responses include gate progress.
- [x] Failure responses include action, actor, resource, and missing scope.
- [x] Repair interaction gives actionable next choices.
- [x] Completed summary shows final approved headcount request.

### Documentation

- [x] Add approval-gate section to `docs/workflow-schemas.md`.
- [x] Add API demo sequence for headcount requisition.
- [x] Add fixture notes under `docs/fixtures`.
- [x] Update this TODO file as tasks complete.

## Completion Criteria

- [x] `npm run format:check` passes.
- [x] `npm run lint` passes.
- [x] `npm run typecheck` passes.
- [x] `npm run test` passes.
- [x] `npm run test:go` passes.
- [x] `npm run build` passes.
- [x] All tasks in this file are checked only after implementation and tests support them.
