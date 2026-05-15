# HCM Next Generic Workflow Runtime TODOs

## Non-Negotiable Architecture Rule

TypeScript code must contain zero workflow-specific business logic.

Allowed in TypeScript:

- [x] Generic workflow graph loading, versioning, and validation.
- [x] Generic node dispatch and transition orchestration.
- [x] Generic RBAC/ABAC/ReBAC policy checks based on config.
- [x] Generic approval, transaction, ledger, projection, outbox, and repair mechanics.
- [x] Generic UI schema delivery and interaction state.
- [x] Generic API transport and request/response mapping.
- [x] Generic external-write client dispatch by configured `connectionId`.

Not allowed in TypeScript:

- [x] Legal-name-specific business rules.
- [x] Contact-info-specific business rules.
- [x] Emergency-contact-specific business rules.
- [x] Compensation-specific business rules.
- [x] Org-transfer-specific business rules.
- [x] Headcount-specific business rules.
- [x] Integration-specific decision logic.
- [x] Customer-specific validation, transformation, routing, or transaction planning.

Business behavior belongs in:

- [x] JSON workflow configs.
- [x] Go block implementations.
- [x] Block contract schemas and SDK contract types.
- [x] Tenant policy/config data.
- [ ] Integration manifests beyond the current `connectionId` client map.

## Work Stream 1: Generic TypeScript Workflow Runtime

- [x] Create `src/workflows/runtime/` as the only TypeScript workflow execution layer.
- [x] Add generic public workflow service functions:
  - [x] `startWorkflowIntent`
  - [x] `getWorkflowInstance`
  - [x] `getAvailableActions`
  - [x] `transitionWorkflow`
  - [x] `getTimeline`
  - [x] `getTasks`
  - [x] `createDocument`
  - [x] `getDocument`
  - [x] `getEmployeeProjection`
  - [x] `listEmployeeProjections`
- [x] Replace API imports from workflow-specific services with the generic runtime service.
- [x] Define runtime models:
  - [x] `RuntimeWorkflowDefinition`
  - [x] `RuntimeWorkflowVersion`
  - [x] `RuntimeWorkflowInstance`
  - [x] `RuntimeWorkflowNode`
  - [x] `RuntimeWorkflowEdge`
  - [x] `RuntimeTransition`
  - [x] `NodeExecutionResult`
  - [x] `RouteDecision`
  - [x] `RuntimeTransactionPlan`
  - [x] `SagaStep`
- [x] Implement generic start flow:
  - [x] Resolve workflow config by intent from registry/database.
  - [x] Validate subject type from config.
  - [x] Evaluate start permissions from config.
  - [x] Create workflow instance.
  - [x] Set initial interaction from config.
  - [x] Append generic intent-started ledger event.
- [x] Implement generic transition flow:
  - [x] Parse canonical transition body.
  - [x] Enforce idempotency.
  - [x] Enforce optimistic version checks.
  - [x] Load pinned workflow config.
  - [x] Resolve available action from current state/config.
  - [x] Check generic actor permission rule.
  - [x] Dispatch to generic handler by node/action type.
  - [x] Persist state changes and ledger events.
  - [x] Save transition attempt result.
- [x] Implement generic node behavior used by V0 workflows:
  - [x] UI form submission.
  - [x] Go preflight block execution.
  - [x] Go transaction-planning block execution.
  - [x] Approval task nodes.
  - [x] Approval gates.
  - [x] External write routing through configured clients.
  - [x] Transaction commit from block output.
  - [x] Repair states.
  - [x] Terminal completion.
  - [ ] Generic wait/timer nodes.
- [x] Remove hardcoded workflow checks from TypeScript:
  - [x] Remove `isHeadcountRequisitionWorkflowIntent` branches from the active API path.
  - [x] Remove `isOrgTransferWorkflowIntent` branches from the active API path.
  - [x] Remove legal-name-specific assumptions from the active API path.
  - [x] Delete legacy workflow-specific TypeScript service folders.
- [x] Keep TypeScript transitions canonical and generic:
  - [x] `start`
  - [x] `submit_input`
  - [x] `provide_evidence`
  - [x] `approve`
  - [x] `reject`
  - [x] `request_more_info`
  - [x] `repair`
  - [x] `execute`
  - [x] `cancel`
- [x] Make routing config-driven:
  - [x] Runtime follows `routeKey` / outcome config returned by nodes.
  - [x] Runtime never branches on workflow intent.
  - [x] Runtime never branches on HR-specific fields.
- [x] Keep only generic shared runtime modules under TypeScript workflow execution code.

## Work Stream 2: Go Block Programming Model

- [x] Define the formal Go block contract.
- [x] Every Go block input supports:
  - [x] Typed node input.
  - [x] Workflow context.
  - [x] Actor context.
  - [x] Permission snapshot.
  - [x] Tenant config placeholder.
  - [x] Prior node facts placeholder.
- [x] Every Go block output supports:
  - [x] `facts`
  - [x] `routeKey`
  - [x] `validationErrors`
  - [x] `warnings`
  - [x] `transactionPlan`
  - [x] `externalCalls`
  - [x] `projectionPatches`
  - [x] `ledgerFacts`
- [x] Add block categories to the contract/docs:
  - [x] validation block
  - [x] preflight block
  - [x] transform block
  - [x] policy block
  - [x] integration mapping block
  - [x] transaction planning block
  - [x] reconciliation block
  - [x] compensation/rollback block placeholder
- [x] Move deterministic workflow-specific logic into Go blocks:
  - [x] legal name change
  - [x] emergency contact update
  - [x] contact info update
  - [x] compensation change
  - [x] org transfer
  - [x] headcount requisition demo planning
- [x] Add or update Go tests for each migrated block.
- [x] Add a local block contract test harness.
- [x] Add customer block SDK skeleton:
  - [x] block contract aliases
  - [x] contract docs
  - [x] example compile/test path through `go test ./...`
  - [ ] full generated block template
- [ ] Add block manifest format:
  - [ ] block name
  - [ ] semantic version
  - [ ] input schema
  - [ ] output schema
  - [ ] required permissions
  - [ ] allowed external capabilities
  - [ ] deterministic/effectful classification
- [ ] Add customer block safety placeholders:
  - [ ] static scan placeholder
  - [ ] dependency audit placeholder
  - [ ] compile check command wrapper
  - [ ] contract test check command wrapper

## Work Stream 3: Workflow Schema And Config Migration

- [x] Redesign workflow JSON schema around graph execution.
- [x] Every workflow config defines:
  - [x] metadata.
  - [x] subject type.
  - [x] start permissions.
  - [x] nodes.
  - [x] edges.
  - [x] UI schemas.
  - [x] block refs.
  - [x] approval gates where needed.
  - [x] transaction behavior.
  - [x] failure behavior.
  - [x] reversal/compensation behavior placeholders.
  - [x] ledger event mapping.
- [x] Edges route by generic `routeKey`, not TypeScript conditions.
- [x] Add schema support for:
  - [x] one input to one output.
  - [x] one input to many possible outcomes.
  - [x] approval-gate parallelism.
  - [x] repair loops.
  - [x] rollback/compensation path metadata.
  - [ ] generic graph joins outside approval gates.
- [x] Convert existing workflows fully to graph configs:
  - [x] legal name change.
  - [x] emergency contact update.
  - [x] contact info update.
  - [x] compensation change.
  - [x] org transfer.
  - [x] headcount requisition.
- [x] Ensure configs reference Go block refs for workflow-specific behavior.
- [x] Ensure configs map ledger events generically.
- [x] Ensure configs map UI/interactions generically.
- [x] Ensure configs map approval behavior generically.
- [x] Add config validation tests.
- [ ] Add config snapshot tests.
- [x] Add a contract test proving a new workflow can be added without TypeScript service edits.

## Work Stream 4: Enterprise Transaction, Security, And Demo Readiness

- [x] Make transaction plans, rollback plans, compensation plans, and repair state part of the runtime records.
- [ ] Make full saga/compensation execution a first-class runtime engine.
- [x] Each workflow execution records:
  - [x] attempted step.
  - [x] completed step.
  - [x] failed external step.
  - [ ] executed compensating step.
  - [x] manual repair state/task path.
  - [x] final reconciliation/outbox status placeholder.
- [x] Make ledger event emission generic and config/block-mapped.
- [x] Ensure projections are updated only from transaction plans.
- [x] Ensure TypeScript never directly mutates business state outside generic transaction mechanics.
- [x] Make RBAC/ABAC/ReBAC generic:
  - [x] actor permissions.
  - [x] field visibility.
  - [x] relationship access.
  - [x] org scope.
  - [x] approval authority.
- [ ] Ensure Go blocks receive only permission-filtered context.
- [x] Add generic approval-gate runtime:
  - [x] sequential approvals.
  - [x] parallel approvals.
  - [x] quorum approvals.
  - [x] veto holders.
  - [ ] escalation timers.
  - [x] repair after rejection/more-info.
- [x] Add generic integration outbox runtime.
- [x] Add generic third-party API simulation path for demos.
- [x] Add e2e coverage proving each workflow runs through generic runtime:
  - [x] legal name change.
  - [x] emergency contact update.
  - [x] contact info update.
  - [x] compensation change.
  - [x] org transfer.
  - [x] headcount requisition.
- [x] Add e2e coverage proving a new workflow requires zero TypeScript changes.
- [ ] Add final API demo script.

## Final Verification

- [x] `npm run format:check`
- [x] `npm run typecheck`
- [x] `npm run lint`
- [x] `npm run test`
- [x] `npm run test:go`
- [x] `npm run build`
- [x] Manual review confirms active TypeScript workflow execution has no workflow-specific business branches.
