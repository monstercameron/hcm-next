# HCM Next Generic Workflow Runtime TODOs

## Non-Negotiable Architecture Rule

TypeScript code must contain zero workflow-specific business logic.

Allowed in TypeScript:

- [ ] Generic workflow graph loading, versioning, and validation.
- [ ] Generic node dispatch and transition orchestration.
- [ ] Generic RBAC/ABAC/ReBAC policy checks based on config.
- [ ] Generic approval, transaction, saga, ledger, projection, outbox, and repair mechanics.
- [ ] Generic UI schema delivery and interaction state.
- [ ] Generic API transport and request/response mapping.

Not allowed in TypeScript:

- [ ] Legal-name-specific business rules.
- [ ] Contact-info-specific business rules.
- [ ] Emergency-contact-specific business rules.
- [ ] Compensation-specific business rules.
- [ ] Org-transfer-specific business rules.
- [ ] Headcount-specific business rules.
- [ ] Integration-specific decision logic.
- [ ] Customer-specific validation, transformation, routing, or transaction planning.

Business behavior belongs in:

- [ ] JSON workflow configs.
- [ ] Go block implementations.
- [ ] Block manifests and schemas.
- [ ] Tenant policy/config data.
- [ ] Integration manifests.

## Work Stream 1: Generic TypeScript Workflow Runtime

- [ ] Create `src/workflows/runtime/` as the only TypeScript workflow execution layer.
- [ ] Add generic public workflow service functions:
  - [ ] `startWorkflowIntent`
  - [ ] `getWorkflowInstance`
  - [ ] `getAvailableActions`
  - [ ] `transitionWorkflow`
  - [ ] `getTimeline`
  - [ ] `getTasks`
  - [ ] `createDocument`
  - [ ] `getDocument`
  - [ ] `getEmployeeProjection`
  - [ ] `listEmployeeProjections`
- [ ] Replace API imports from workflow-specific services with the generic runtime service.
- [ ] Define runtime models:
  - [ ] `RuntimeWorkflowDefinition`
  - [ ] `RuntimeWorkflowVersion`
  - [ ] `RuntimeWorkflowInstance`
  - [ ] `RuntimeWorkflowNode`
  - [ ] `RuntimeWorkflowEdge`
  - [ ] `RuntimeTransition`
  - [ ] `NodeExecutionResult`
  - [ ] `RouteDecision`
  - [ ] `RuntimeTransactionPlan`
  - [ ] `SagaStep`
- [ ] Implement generic start flow:
  - [ ] Resolve workflow config by intent from registry/database.
  - [ ] Validate subject type from config.
  - [ ] Evaluate start permissions from config.
  - [ ] Create workflow instance.
  - [ ] Set initial interaction from config.
  - [ ] Append generic intent-started ledger event.
- [ ] Implement generic transition flow:
  - [ ] Parse canonical transition body.
  - [ ] Enforce idempotency.
  - [ ] Enforce optimistic version checks.
  - [ ] Load pinned workflow config.
  - [ ] Resolve available action from current state/config.
  - [ ] Check generic actor permission rule.
  - [ ] Dispatch to generic handler by node/action type.
  - [ ] Persist state changes and ledger events.
  - [ ] Save transition attempt result.
- [ ] Implement generic node dispatcher:
  - [ ] `ui_form`
  - [ ] `go_block`
  - [ ] `approval_gate`
  - [ ] `external_call`
  - [ ] `transaction_plan`
  - [ ] `transaction_commit`
  - [ ] `wait`
  - [ ] `repair`
  - [ ] `terminal`
- [ ] Remove hardcoded workflow checks from TypeScript:
  - [ ] Remove `isHeadcountRequisitionWorkflowIntent` branches from generic API path.
  - [ ] Remove `isOrgTransferWorkflowIntent` branches from generic API path.
  - [ ] Remove legal-name-specific assumptions from generic API path.
- [ ] Keep TypeScript transitions canonical and generic:
  - [ ] `start`
  - [ ] `submit_input`
  - [ ] `provide_evidence`
  - [ ] `approve`
  - [ ] `reject`
  - [ ] `request_more_info`
  - [ ] `repair`
  - [ ] `execute`
  - [ ] `cancel`
- [ ] Make routing config-driven:
  - [ ] Runtime follows `routeKey` / outcome config returned by nodes.
  - [ ] Runtime never branches on workflow intent.
  - [ ] Runtime never branches on HR-specific fields.
- [ ] Move workflow-specific TypeScript folders toward deletion:
  - [ ] `src/workflows/legal-name-change/service.ts`
  - [ ] `src/workflows/headcount-requisition/service.ts`
  - [ ] `src/workflows/org-transfer/service.ts`
- [ ] Keep only generic shared runtime modules under TypeScript workflow code.

## Work Stream 2: Go Block Programming Model

- [ ] Define the formal Go block contract.
- [ ] Every Go block input must support:
  - [ ] Typed node input.
  - [ ] Workflow context.
  - [ ] Actor context.
  - [ ] Permission snapshot.
  - [ ] Tenant config.
  - [ ] Prior node facts.
- [ ] Every Go block output must support:
  - [ ] `facts`
  - [ ] `routeKey`
  - [ ] `validationErrors`
  - [ ] `warnings`
  - [ ] `transactionPlan`
  - [ ] `externalCalls`
  - [ ] `projectionPatches`
  - [ ] `ledgerFacts`
- [ ] Add block categories:
  - [ ] validation block
  - [ ] preflight block
  - [ ] transform block
  - [ ] policy block
  - [ ] integration mapping block
  - [ ] transaction planning block
  - [ ] reconciliation block
  - [ ] compensation/rollback block
- [ ] Move deterministic workflow-specific logic into Go blocks:
  - [ ] legal name change
  - [ ] emergency contact update
  - [ ] contact info update
  - [ ] compensation change
  - [ ] org transfer
  - [ ] headcount requisition
- [ ] Add or update Go tests for each migrated block.
- [ ] Add a local block contract test harness.
- [ ] Add block manifest format:
  - [ ] block name
  - [ ] semantic version
  - [ ] input schema
  - [ ] output schema
  - [ ] required permissions
  - [ ] allowed external capabilities
  - [ ] deterministic/effectful classification
- [ ] Add customer block SDK skeleton:
  - [ ] block template
  - [ ] contract docs
  - [ ] example test
  - [ ] compile command
- [ ] Add customer block safety placeholders:
  - [ ] static scan placeholder
  - [ ] dependency audit placeholder
  - [ ] compile check
  - [ ] contract test check

## Work Stream 3: Workflow Schema And Config Migration

- [ ] Redesign workflow JSON schema around graph execution.
- [ ] Every workflow config must define:
  - [ ] metadata.
  - [ ] subject type.
  - [ ] start permissions.
  - [ ] nodes.
  - [ ] edges.
  - [ ] UI schemas.
  - [ ] block refs.
  - [ ] approval gates.
  - [ ] transaction behavior.
  - [ ] failure behavior.
  - [ ] reversal/compensation behavior.
  - [ ] ledger event mapping.
- [ ] Edges route by generic `routeKey`, not TypeScript conditions.
- [ ] Add schema support for:
  - [ ] one input to one output.
  - [ ] one input to many possible outcomes.
  - [ ] parallel branches.
  - [ ] joins.
  - [ ] repair loops.
  - [ ] rollback/compensation paths.
- [ ] Convert existing workflows fully to graph configs:
  - [ ] legal name change.
  - [ ] emergency contact update.
  - [ ] contact info update.
  - [ ] compensation change.
  - [ ] org transfer.
  - [ ] headcount requisition.
- [ ] Ensure configs reference Go block refs for workflow-specific behavior.
- [ ] Ensure configs map ledger events generically.
- [ ] Ensure configs map UI/interactions generically.
- [ ] Ensure configs map approval behavior generically.
- [ ] Add config validation tests.
- [ ] Add config snapshot tests.
- [ ] Add a contract test proving a new workflow can be added without TypeScript service edits.

## Work Stream 4: Enterprise Transaction, Security, And Demo Readiness

- [ ] Make saga/compensation a first-class runtime concept.
- [ ] Each workflow execution records:
  - [ ] attempted step.
  - [ ] completed step.
  - [ ] failed step.
  - [ ] compensating step.
  - [ ] manual repair task.
  - [ ] final reconciliation status.
- [ ] Make ledger event emission generic and config-mapped.
- [ ] Ensure projections are updated only from transaction plans.
- [ ] Ensure TypeScript never directly mutates business state outside generic transaction mechanics.
- [ ] Make RBAC/ABAC/ReBAC generic:
  - [ ] actor permissions.
  - [ ] field visibility.
  - [ ] relationship access.
  - [ ] org scope.
  - [ ] approval authority.
- [ ] Ensure Go blocks receive only permitted context.
- [ ] Add generic approval-gate runtime:
  - [ ] sequential approvals.
  - [ ] parallel approvals.
  - [ ] quorum approvals.
  - [ ] veto holders.
  - [ ] escalation.
  - [ ] repair after rejection/more-info.
- [ ] Add generic integration outbox runtime.
- [ ] Add generic third-party API simulation path for demos.
- [ ] Add e2e coverage proving each workflow runs through generic runtime:
  - [ ] legal name change.
  - [ ] emergency contact update.
  - [ ] contact info update.
  - [ ] compensation change.
  - [ ] org transfer.
  - [ ] headcount requisition.
- [ ] Add e2e coverage proving a new workflow requires zero TypeScript changes.
- [ ] Add final API demo script.

## Final Verification

- [ ] `npm run format:check`
- [ ] `npm run typecheck`
- [ ] `npm run lint`
- [ ] `npm run test`
- [ ] `npm run test:go`
- [ ] `npm run build`
- [ ] Manual review confirms TypeScript has no workflow-specific business branches.
