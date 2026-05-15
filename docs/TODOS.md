# TODO: Heavy Org-Aware RBAC Workflow

## Target Workflow

Build a heavy-duty workflow named:

```text
employee.org_transfer_compensation_change
```

Recommended config file:

```text
src/workflows/configs/employee-org-transfer-compensation-change.workflow.json
```

Recommended E2E file:

```text
src/tests/e2e/org-aware-transfer-rbac.e2e.spec.ts
```

Scenario:

```text
Jane Doe moves from Boston Main Clinic to Cambridge Clinic.
Her manager, primary team, work location, cost center, and compensation change.
The workflow must prove RBAC changes before, during, and after execution.
```

## Design Intent

- Keep employee projections as read models.
- Treat `organization_units`, `organization_relationships`, `worker_assignments`, and `role_bindings` as the durable org-aware RBAC source.
- Use workflow transitions for all mutations.
- Use graph nodes for internal routing and business steps.
- Test the user-facing workflow contract without a GUI by asserting interaction payloads, available actions, masked fields, labels, state names, timeline summaries, and error messages.

## Parallel Workstreams

The work is split so four agents can work simultaneously with minimal file overlap.

| Workstream                            | Owner Scope                                                                                              | Primary Files                                                                                                                           |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Workflow Config and Constants      | Declarative workflow contract, graph shape, constants, config registration                               | `src/workflows/configs/*`, `src/workflows/shared/workflow-config.ts`, foundation constants, config unit tests                           |
| 2. Runtime, Blocks, and Data Mutation | Service handlers, deterministic blocks, transaction planning, ledger/outbox/projection/assignment writes | `src/workflows/legal-name-change/service.ts` or new workflow service files, `src/blocks/go/**`, data-store repositories/types as needed |
| 3. Org-Aware RBAC and Visibility      | Access evaluator, role-binding/assignment scope semantics, masking, timeline filtering                   | `src/workflows/shared/employee-access.ts`, data-store org repositories, RBAC unit tests                                                 |
| 4. E2E, UX Contract, and Docs         | Full workflow E2E, API-facing UX contract tests, fixture aliases, docs updates                           | `src/tests/e2e/**`, docs, test fixtures/helpers                                                                                         |

## Workstream 1: Workflow Config and Constants

Goal: define the workflow as a node-based, schema-first workflow config before runtime logic is implemented.

### Ownership

- Owns workflow config shape and registration.
- Owns new constants needed by the config.
- Owns config validation tests.
- Should not implement service handlers or RBAC evaluator logic.

### Files

- `src/workflows/configs/employee-org-transfer-compensation-change.workflow.json`
- `src/workflows/shared/workflow-config.ts`
- `src/platform/foundation/constants/workflow.constants.ts`
- `src/platform/foundation/constants/permission.constants.ts`
- `src/platform/foundation/constants/ledger-event.constants.ts`
- New or existing config unit test files.
- `docs/workflow-schemas.md` after config shape stabilizes.

### Tasks

- [ ] Add `employee.org_transfer_compensation_change` to shared workflow constants.
- [ ] Add permission constants:
  - [ ] `employee_data_change.org_transfer.request`
  - [ ] `employee_data_change.org_transfer.hr_review`
  - [ ] `employee_data_change.org_transfer.manager_approve`
  - [ ] `employee_data_change.org_transfer.destination_manager_approve`
  - [ ] `employee_data_change.org_transfer.finance_approve`
  - [ ] `employee_data_change.org_transfer.compensation_approve`
  - [ ] `employee_data_change.org_transfer.medical_director_approve`
  - [ ] `employee_data_change.org_transfer.execute`
  - [ ] `employee_data_change.org_transfer.view_restricted_summary`
- [ ] Add ledger event constants:
  - [ ] `OrgTransferPreflighted`
  - [ ] `OrgTransferSubmitted`
  - [ ] `SourceManagerApprovalCreated`
  - [ ] `DestinationManagerApprovalCreated`
  - [ ] `FinanceApprovalCreated`
  - [ ] `CompensationApprovalCreated`
  - [ ] `ClinicalPlacementApprovalCreated`
  - [ ] `WorkerAssignmentSuperseded`
  - [ ] `WorkerAssignmentCreated`
  - [ ] `RoleBindingsRecalculated`
  - [ ] `EmployeeOrgProjectionUpdated`
  - [ ] `EmployeeCompensationUpdated`
  - [ ] `OrgTransferExecuted`
- [ ] Register the new workflow config in `src/workflows/shared/workflow-config.ts`.
- [ ] Extend `WorkflowActionActor` with:
  - [ ] `source_manager`
  - [ ] `destination_manager`
  - [ ] `finance_admin`
  - [ ] `medical_director`
  - [ ] `clinic_ops_admin`
  - [ ] `org_transfer_approver`
- [ ] Extend `WorkflowStartActor` if the workflow can be started by more than `hr_admin`.
- [ ] Decide self-start:
  - [ ] If yes, allow employee to request transfer but require HR to complete org fields.
  - [ ] If no, set `selfServiceStart: false` and start with HR.
- [ ] Add `interactions.input` with employee context:
  - [ ] current `organization`
  - [ ] current `job`
  - [ ] current `manager`
  - [ ] current `compensation` only for actors with compensation visibility
  - [ ] active `worker_assignments`
  - [ ] current role-binding summary for access impact
- [ ] Add JSON schema fields:
  - [ ] `targetLocationOrgUnitId`
  - [ ] `targetTeamOrgUnitId`
  - [ ] `targetCostCenterOrgUnitId`
  - [ ] `targetManagerEmployeeId`
  - [ ] `proposedJob`
  - [ ] `proposedCompensation`
  - [ ] `effectiveAt`
  - [ ] `businessReason`
  - [ ] `transferReason`
  - [ ] `accessImpactAcknowledged`
- [ ] Add UI schema metadata:
  - [ ] section order
  - [ ] field labels
  - [ ] read-only current-state fields
  - [ ] warning text keys for access impact
  - [ ] submit label
  - [ ] review labels for each approver
- [ ] Add interactions:
  - [ ] HR intake
  - [ ] source manager approval
  - [ ] destination manager approval
  - [ ] finance cost-center approval
  - [ ] compensation approval
  - [ ] medical director clinical placement approval
  - [ ] ready to execute
  - [ ] repair/rework
  - [ ] completed summary

### Node-Based Graph Tasks

- [ ] Add `collect_transfer_input` interaction node.
- [ ] Add `org_transfer_preflight` block node.
- [ ] Add policy check nodes:
  - [ ] `check_hr_transfer_authority`
  - [ ] `check_source_manager_visibility`
  - [ ] `check_destination_manager_visibility`
  - [ ] `check_finance_cost_center_scope`
  - [ ] `check_compensation_scope`
  - [ ] `check_medical_director_clinical_scope`
- [ ] Add approval nodes:
  - [ ] `source_manager_approval`
  - [ ] `destination_manager_approval`
  - [ ] `finance_approval`
  - [ ] `compensation_approval`
  - [ ] `medical_director_approval`
- [ ] Every approval node must have `approved`, `rejected`, and `request_more_info` outcomes.
- [ ] Use serial approvals for V1.
- [ ] Document future `parallel_split` and `parallel_join` upgrade path.
- [ ] Add `plan_org_transfer_transaction` transaction-plan node.
- [ ] Add assignment write nodes:
  - [ ] `supersede_current_assignments`
  - [ ] `create_target_assignments`
- [ ] Add `recalculate_role_bindings` node.
- [ ] Add integration nodes:
  - [ ] `sync_hris_transfer`
  - [ ] `sync_payroll_cost_center`
  - [ ] `sync_compensation_vendor`
- [ ] Each integration node has `accepted`, `rejected`, `retryable_failure`, and `dead_letter` outcomes.
- [ ] Add `apply_employee_projection` projection-write node.
- [ ] Add terminal nodes:
  - [ ] `completed`
  - [ ] `rejected`
  - [ ] `canceled`
  - [ ] `failed`
  - [ ] `waiting_repair`

### Unit Tests

- [ ] Config is registered by intent.
- [ ] Config has a graph.
- [ ] Graph has one start node.
- [ ] Every `nextNodeId` points to an existing node.
- [ ] Every non-terminal node has outcomes.
- [ ] Every outcome key is stable and lower_snake_case.
- [ ] Every node has a title.
- [ ] Every state referenced by nodes exists in `states`.
- [ ] Every interaction referenced by outcomes exists in `interactions`.
- [ ] Every action transition has a handler.
- [ ] Every action actor is a known actor key.
- [ ] Every approval node has approve/reject/request_more_info paths.
- [ ] Every external write node has accepted/rejected/retryable failure paths.
- [ ] Every node that emits `eventType` uses a known ledger event constant.
- [ ] Config can be serialized and deserialized without losing graph shape.

## Workstream 2: Runtime, Blocks, and Data Mutation

Goal: make the workflow executable without adding direct employee mutation APIs.

### Ownership

- Owns service handlers, deterministic block contracts, transaction planning, and execution writes.
- Owns ledger and outbox mutation behavior.
- Should not define the whole workflow config or write E2E assertions beyond targeted local tests.

### Files

- Existing workflow service files or new dedicated org-transfer service files.
- `src/workflows/shared/workflow-config.ts` only if runtime types require changes coordinated with Workstream 1.
- `src/blocks/go/**` for preflight and transaction planning blocks.
- `src/platform/data-store/repositories.ts` if assignment/role-binding write repositories are needed.
- `src/platform/data-store/types.ts` only for runtime-only record fields.

### Tasks

- [ ] Add workflow service handlers for this workflow.
- [ ] Keep public mutation surface limited to workflow intents/transitions.
- [ ] Decide whether runtime needs new node types now:
  - [ ] `policy_check`
  - [ ] `data_write`
  - [ ] `parallel_split`
  - [ ] `parallel_join`
  - [ ] `notification`
- [ ] If new node types are added, update:
  - [ ] `WorkflowGraphNodeConfig`
  - [ ] node execution/routing
  - [ ] node routing tests
  - [ ] timeline summaries
- [ ] Implement deterministic preflight block contract:
  - [ ] target org units exist and are active
  - [ ] employee can be transferred
  - [ ] proposed effective date is valid
  - [ ] target manager is active
  - [ ] target cost center belongs to target business unit or explicitly allows cross-charge
- [ ] Implement deterministic transaction plan block contract.
- [ ] Map proposed input to `ProposedChangeRecord` rows for:
  - [ ] organization assignment
  - [ ] manager assignment
  - [ ] cost center assignment
  - [ ] job changes
  - [ ] compensation changes
- [ ] Create approval tasks for each approval stage.
- [ ] Implement assignment execution:
  - [ ] supersede old primary team assignment
  - [ ] create new primary team assignment
  - [ ] supersede old work location assignment
  - [ ] create new work location assignment
  - [ ] supersede old cost center assignment
  - [ ] create new cost center assignment
- [ ] Implement role-binding recalculation:
  - [ ] old source manager direct-report visibility stops after effective date
  - [ ] new manager direct-report visibility starts after effective date
- [ ] Update employee projection:
  - [ ] `organization.location`
  - [ ] `organization.team`
  - [ ] `organization.costCenter`
  - [ ] `manager.employeeId`
  - [ ] `job` fields if included
  - [ ] `compensation` fields if included
- [ ] Add integration outbox rows:
  - [ ] HRIS worker assignment update
  - [ ] payroll cost-center update
  - [ ] compensation vendor update
- [ ] Make execution idempotent:
  - [ ] same transition and idempotency key returns same result
  - [ ] duplicate execution does not create duplicate assignments
  - [ ] duplicate execution does not duplicate role bindings
  - [ ] duplicate execution does not duplicate outbox rows
- [ ] Enforce `expectedVersion` on every transition.

### Unit Tests

- [ ] Applying transfer creates new active assignment records.
- [ ] Applying transfer supersedes old assignment records.
- [ ] Applying transfer updates employee projection summary.
- [ ] Applying transfer preserves employee identity/contact data.
- [ ] Applying compensation change updates compensation only.
- [ ] Applying manager change updates `manager.employeeId`.
- [ ] Future-effective transfer does not affect current projection until effective date.
- [ ] Same event applied twice is idempotent.
- [ ] Out-of-order stale event is ignored or rejected.
- [ ] Reducer emits structured validation errors for malformed payloads.
- [ ] Plan includes assignment supersede operations.
- [ ] Plan includes assignment create operations.
- [ ] Plan includes role-binding recalculation operations.
- [ ] Plan includes employee projection operation.
- [ ] Plan includes HRIS outbox operation.
- [ ] Plan includes payroll outbox operation when cost center changes.
- [ ] Plan includes compensation vendor outbox operation when comp changes.
- [ ] Plan marks operations with deterministic idempotency keys.
- [ ] Plan refuses incompatible target org unit combinations.
- [ ] Plan refuses finance approval if finance actor lacks target cost-center scope.

## Workstream 3: Org-Aware RBAC and Visibility

Goal: make employee visibility decisions understand org units, worker assignments, scoped role bindings, field groups, workflow-specific summaries, and masking.

### Ownership

- Owns permission/access evaluator behavior.
- Owns masking behavior and actor-specific visibility.
- Owns timeline filtering logic if visibility-aware timeline changes are needed.
- Should not implement workflow execution writes.

### Files

- `src/workflows/shared/employee-access.ts`
- `src/workflows/shared/employee-access.test.ts`
- Data-store org repositories if read helpers are needed.
- Timeline filtering service if actor-specific timeline filtering is implemented.

### Tasks

- [ ] Add org-aware access decision object for debug/audit tests.
- [ ] Load active role bindings for actor.
- [ ] Resolve worker assignments for target employee.
- [ ] Resolve org-unit descendant scopes through `organization_relationships`.
- [ ] Support scope types:
  - [ ] `self`
  - [ ] `direct_reports`
  - [ ] `manager_chain`
  - [ ] `org_unit`
  - [ ] `org_unit_descendants`
  - [ ] `legal_entity`
  - [ ] `location`
  - [ ] `country`
  - [ ] `cost_center`
  - [ ] `project`
  - [ ] `assignment`
  - [ ] `workflow_instance`
  - [ ] `global`
- [ ] Support workflow-specific restricted summary visibility.
- [ ] Ensure denied actors receive actionable permission errors.
- [ ] Ensure system actor can execute but does not gain human read access.
- [ ] Ensure masked fields are masked in both `document` and `indexedFields`.
- [ ] Add timeline view filtering by actor visibility.

### RBAC Requirements

- [ ] Employee can see their own transfer request summary.
- [ ] Employee cannot see approval-only notes unless explicitly shared.
- [ ] Source manager can see current org/job fields for their direct report.
- [ ] Source manager cannot see compensation fields.
- [ ] Destination manager can see limited proposed transfer summary before execution.
- [ ] Destination manager cannot see personal contact, emergency contacts, or compensation before transfer is active.
- [ ] Destination manager gains direct-report visibility after execution.
- [ ] Source manager loses direct-report visibility after execution.
- [ ] HR admin can see profile, contact, employment, organization, job, workflow fields.
- [ ] Finance admin can approve only if scoped to the target cost center.
- [ ] Finance admin cannot see contact or emergency contacts.
- [ ] Compensation admin can see compensation and job/org context.
- [ ] Compensation admin cannot see contact or emergency contacts.
- [ ] Medical director can approve only clinical placement scopes.
- [ ] Clinic ops admin can see operational org placement but not compensation.
- [ ] Timeline view filters details by actor visibility.

### Unit Tests

- [ ] Source manager scope resolves through current active assignments.
- [ ] Destination manager scope is not active until workflow execution.
- [ ] Finance scope resolves through target cost center assignment or proposed assignment.
- [ ] Medical director scope resolves through clinical org descendants.
- [ ] HR global access does not imply compensation access unless field groups include compensation.
- [ ] Compensation admin cannot see contact fields.
- [ ] Role binding effective dates are honored.
- [ ] Revoked/superseded role bindings do not grant access.
- [ ] Proposed assignments do not grant durable visibility before approval.
- [ ] Workflow-specific visibility can expose a restricted summary without exposing full employee projection.
- [ ] Deny/mask rules override allow rules.
- [ ] Indexed fields do not leak masked org, manager, contact, or compensation data.
- [ ] Wrong actor is denied at every approval stage.
- [ ] Role-binding expiration and revocation are tested directly.

## Workstream 4: E2E, UX Contract, Fixtures, and Docs

Goal: prove the whole workflow makes sense from the API/UI-contract perspective.

### Ownership

- Owns full E2E and UX-contract tests.
- Owns seeded fixture aliases used by tests.
- Owns docs updates after implementation.
- Should avoid changing workflow runtime internals except for testability hooks agreed with other workstreams.

### Files

- `src/tests/e2e/org-aware-transfer-rbac.e2e.spec.ts`
- E2E helper files or fixtures.
- Seed aliases if needed.
- `docs/api-demo.md`
- `docs/workflow-schemas.md`
- `docs/org-aware-rbac-plan.md`
- `docs/TODOS.md`

### Fixture Tasks

- [ ] Add stable seeded destination manager actor alias.
- [ ] Add stable seeded target org-unit aliases:
  - [ ] Cambridge Clinic location
  - [ ] Cambridge Nursing or Cambridge Primary Care team
  - [ ] target cost center
  - [ ] target manager employee ID
- [ ] Add test helper for workflow transition requests.
- [ ] Add test helper for actor-specific employee reads.
- [ ] Add test helper for timeline reads.
- [ ] Add test helper for assignment/role-binding assertions.

### E2E Tasks

- [ ] Create `org-aware-transfer-rbac.e2e.spec.ts`.
- [ ] Use `createSeededDemoStore`.
- [ ] Use Jane `emp_123` as source worker.
- [ ] Source manager is Boston manager.
- [ ] Destination manager is Cambridge manager.
- [ ] Target team is Cambridge Nursing or Cambridge Primary Care.
- [ ] Target location is Cambridge Clinic.
- [ ] Target cost center is a cost center finance can approve.
- [ ] Assert pre-workflow visibility:
  - [ ] source manager can list Jane as direct report
  - [ ] destination manager cannot see Jane as direct report
  - [ ] finance cannot see Jane contact
  - [ ] compensation can see compensation only within scope
- [ ] Start workflow as HR.
- [ ] Submit transfer input.
- [ ] Assert current interaction is waiting for first approval.
- [ ] Assert available actions differ by actor:
  - [ ] employee sees no approve action
  - [ ] source manager sees approve/reject/request_more_info at source manager node
  - [ ] wrong manager is denied
  - [ ] finance sees finance action only at finance node
  - [ ] compensation sees comp action only at comp node
- [ ] Approve source manager step.
- [ ] Approve destination manager step.
- [ ] Approve finance step.
- [ ] Approve compensation step.
- [ ] Approve medical director step if target department is clinical.
- [ ] Execute as system or HR admin.
- [ ] Assert final projection:
  - [ ] `organization.location` changed to Cambridge Clinic
  - [ ] `organization.team` changed
  - [ ] `organization.costCenter` changed
  - [ ] `manager.employeeId` changed
  - [ ] `compensation.amount` and `effectiveDate` changed
- [ ] Assert org graph writes:
  - [ ] old primary team assignment is superseded or expired
  - [ ] new primary team assignment is active
  - [ ] old work location assignment is superseded or expired
  - [ ] new work location assignment is active
  - [ ] old cost center assignment is superseded or expired
  - [ ] new cost center assignment is active
- [ ] Assert role-binding/access changes:
  - [ ] source manager loses post-transfer access to Jane
  - [ ] destination manager gains post-transfer access to Jane
  - [ ] employee retains self-service access
- [ ] Assert ledger/timeline:
  - [ ] every approval event exists
  - [ ] assignment events exist
  - [ ] projection update event exists
  - [ ] execution event exists
- [ ] Assert outbox:
  - [ ] HRIS transfer outbox row exists
  - [ ] payroll cost-center outbox row exists
  - [ ] compensation vendor outbox row exists
- [ ] Assert idempotency:
  - [ ] replay final execute transition returns cached result
  - [ ] replay does not duplicate assignment or outbox rows

### UX Contract Tests Without GUI

Treat API responses as the UI contract.

- [ ] Start intent response has a clear title, state, status, and next actions.
- [ ] Input interaction includes read-only current organization context.
- [ ] Input interaction hides compensation context from non-compensation actors.
- [ ] Field labels are human-readable.
- [ ] Required fields are reflected in JSON schema.
- [ ] Enum values are stable and user-meaningful.
- [ ] Submit button label matches the workflow stage.
- [ ] Waiting states clearly name who/what is next.
- [ ] Approval interactions include concise proposed-change summaries.
- [ ] Approval interactions show only fields the actor can view.
- [ ] Rejection/request-more-info interactions require a reason.
- [ ] Permission errors include action, actor, resource, and missing scope.
- [ ] Version-conflict errors tell client to refetch.
- [ ] Idempotency replay response matches original response.
- [ ] Timeline summaries read like user-facing history, not raw internal event names.
- [ ] Completed summary shows final effective org placement.
- [ ] Repair interaction gives actionable next choices.

### Documentation Tasks

- [ ] Add this workflow to `docs/workflow-schemas.md` as the canonical multi-approval graph example.
- [ ] Add API demo curl sequence after implementation.
- [ ] Add an org-aware RBAC decision example to `docs/org-aware-rbac-plan.md`.
- [ ] Document seed actors and target org units used by the E2E.
- [ ] Document which parts are still compatibility/legacy access grants.

## Cross-Workstream Coordination

- [ ] Workstream 1 defines stable config keys before Workstreams 2 and 4 depend on them.
- [ ] Workstream 3 defines access decision output shape before Workstreams 2 and 4 assert it.
- [ ] Workstream 2 owns mutation behavior; Workstream 4 only asserts it through public APIs and repository state.
- [ ] Workstream 4 owns fixture alias naming; other streams should reuse those names instead of inventing new IDs.
- [ ] Any new graph node type must be agreed by Workstreams 1 and 2 before implementation.
- [ ] Any new ledger event constant must be agreed by Workstreams 1, 2, and 4.

## Things This Ask Gets Right

- [ ] Heavy RBAC should be tested through a realistic workflow, not isolated permission helpers only.
- [ ] Workflow config should lead implementation.
- [ ] Employee visibility must be tested before, during, and after execution.
- [ ] UX quality can be tested now through interaction payloads, labels, available actions, errors, timeline summaries, and field masking.
- [ ] Unit tests should cover sense-making and contract clarity, not just happy-path execution.

## Gaps To Include Before Building

- [ ] Add config validation before runtime execution so malformed node graphs fail early.
- [ ] Add explicit access-decision object for debug/audit tests.
- [ ] Add workflow timeline filtering tests by actor.
- [ ] Add negative tests for wrong actor at every approval stage.
- [ ] Add repair/rework path, not only happy-path approval.
- [ ] Add rollback/compensation behavior for external write failure.
- [ ] Add future-effective transfer behavior.
- [ ] Add direct tests for role-binding expiration and revocation.
- [ ] Add test fixtures for target org units instead of hard-coding names throughout tests.
- [ ] Add a stable seeded destination manager actor alias for readability.
