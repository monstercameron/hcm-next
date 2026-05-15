# Workflow Schemas

This is the living design document for HCM Next workflow schema shape. We should keep revising it as the workflow engine becomes more flexible and as real HCM workflows expose more routing, repair, approval, and integration cases.

The current direction is:

```text
Workflow definition
  -> states and user actions
  -> interaction schemas
  -> deterministic block inputs
  -> graph nodes
  -> node outcomes
  -> saga transaction semantics
  -> transaction plan
  -> compensation plan
  -> ledger events
  -> projections
  -> integration outcomes
```

The schema should let a business describe the workflow graph and let the runtime execute it without hardcoding workflow-specific branches in TypeScript.

## Goals

- Model HCM workflows as directed graphs, not as one hardcoded request path.
- Let every important step be represented as a node.
- Let every node emit one of several outcomes.
- Route outcomes to another node, state, status, interaction, and ledger event.
- Keep deterministic business logic inside blocks where possible.
- Keep platform glue generic: permissions, routing, persistence, idempotency, ledger, and integration execution.
- Support both happy paths and repair/failure paths as first-class workflow behavior.
- Support saga-based workflow transactions with compensating actions.
- Make reversibility explicit for every side-effecting step.
- Make generated UX possible from interaction schemas and workflow state.
- Make audit review possible from config plus ledger facts.

## Current Schema Sections

Current workflow config files live in:

```text
src/workflows/configs/*.workflow.json
```

The current top-level shape is:

```json
{
  "intent": "employee.compensation.change",
  "subjectType": "worker",
  "selfServiceStart": false,
  "startActors": ["hr_admin"],
  "interactions": {},
  "states": {},
  "submit": {},
  "approval": {},
  "plan": {},
  "graph": {},
  "saga": {},
  "projection": {},
  "timeline": {}
}
```

## Intent

The workflow intent is the public business command.

Examples:

```text
employee.legal_name.change
employee.emergency_contact.update
employee.contact_info.update
employee.compensation.change
```

The API should start workflows by intent, not by directly invoking a controller:

```http
POST /workflow-intents
```

This keeps the public mutation surface stable while workflow implementation changes behind the config.

## Actors

Current actor controls:

```json
{
  "selfServiceStart": false,
  "startActors": ["hr_admin"]
}
```

Current action actors:

```text
requester
initiator
hr_admin
compensation_admin
hr_admin_or_system
```

Direction:

```text
actor expression -> permission check -> relationship/field scope -> action allowed
```

We should eventually move from fixed actor strings to policy expressions, for example:

```json
{
  "actor": {
    "anyOf": [
      { "role": "hr_admin" },
      { "relationship": "manager_of_subject" },
      { "permission": "employee.compensation.approve" }
    ]
  }
}
```

## Interactions

Interactions describe generated UX surfaces.

Current examples:

```text
form
waiting
ready_to_execute
repair
```

Example:

```json
{
  "vendorDecisionRepair": {
    "type": "repair",
    "title": "Vendor compensation decision needs repair",
    "jsonSchema": {
      "type": "object",
      "required": ["repairAction"],
      "properties": {
        "repairAction": {
          "type": "string",
          "enum": ["revise_compensation", "cancel_request", "manual_review"]
        },
        "notes": { "type": "string" }
      }
    },
    "uiSchema": {
      "layout": "repair_panel",
      "submitLabel": "Record repair action"
    }
  }
}
```

Interaction schemas should stay declarative. The runtime can render or serve them to another UI layer, but the workflow config should describe:

- Required input data.
- Read-only employee context.
- Layout intent.
- Submit action label.
- Repair or review mode.
- Field visibility requirements.

## States And Actions

States describe what actions are available to which actor while the workflow is in a runtime state.

Current shape:

```json
{
  "waiting_approval": {
    "actions": [
      {
        "transition": "approve",
        "label": "Approve",
        "actor": "compensation_admin",
        "handler": "approve"
      },
      {
        "transition": "reject",
        "label": "Reject",
        "actor": "compensation_admin",
        "handler": "reject"
      }
    ]
  }
}
```

States are useful for user-facing runtime control, but they are not enough by themselves. A flexible workflow engine also needs graph nodes because a single state can hide several internal steps.

Example:

```text
approved state
  -> transaction plan node
  -> third-party decision node
  -> projection write node
  -> terminal node
```

## Graph Model

The graph is the source of truth for node-level routing.

Current shape:

```json
{
  "graph": {
    "startNodeId": "collect_compensation_input",
    "nodes": []
  }
}
```

Each node should be independently addressable:

```json
{
  "nodeId": "vendor_compensation_decision",
  "type": "external_write",
  "title": "Submit compensation decision to vendor",
  "connectionId": "third_party_compensation_decision",
  "operation": "submitCompensationChange",
  "outcomes": []
}
```

## Node Types

Current node types:

```text
interaction
block
approval
transaction_plan
external_write
projection_write
manual_repair
terminal
```

Likely future node types:

```text
data_read
data_write
policy_check
parallel_split
parallel_join
human_task
timer_wait
webhook_wait
notification
document_collection
ai_review
ai_draft
reconciliation_check
compensation_transaction
rollback_or_compensation
subworkflow
```

The node type should define the runtime behavior category, not the exact business meaning. Business meaning belongs in `title`, block names, operation names, and metadata.

## Outcomes

Every node can produce an array of outcomes.

Current outcome shape:

```json
{
  "outcome": "accepted",
  "when": {
    "$source": "externalWriteResponse",
    "path": "status",
    "equals": "accepted"
  },
  "eventType": "ExternalWriteSucceeded",
  "nextNodeId": "apply_compensation_projection"
}
```

Outcome fields:

```text
outcome          stable outcome key
when             optional condition expression
eventType        ledger event to emit
nextNodeId       next graph node
nextState        runtime workflow state
nextStatus       runtime workflow status
nextInteraction  interaction to display after routing
```

The important design rule:

```text
Good, bad, waiting, skipped, repaired, retried, and canceled paths should all be outcomes.
```

They should not be hidden as uncaught errors or workflow-specific TypeScript branches.

## Saga Transactions

Workflow execution should use saga orchestration for multi-step HCM transactions.

The system name:

```text
Saga pattern
```

The HCM Next version:

```text
saga-based workflow transactions with compensating actions
```

Why this matters:

```text
An HCM workflow may update internal projections, call payroll, call identity,
notify finance, write documents, and sync external HCM systems. If step 5 fails,
the workflow must know what happened in steps 1-4 and what can be safely reversed,
retried, reconciled, or repaired.
```

The workflow schema should eventually include a top-level `saga` section:

```json
{
  "saga": {
    "mode": "orchestrated",
    "transactionBoundary": "workflow_instance",
    "failurePolicy": "compensate_then_repair",
    "idempotencyScope": "workflow_instance",
    "compensationOrder": "reverse_completed_steps",
    "reconciliationRequired": true
  }
}
```

Saga fields:

```text
mode                    orchestrated or choreographed; HCM Next should prefer orchestrated
transactionBoundary     workflow_instance, change_request, transaction_plan, or node
failurePolicy           fail_fast, retry_then_repair, compensate_then_repair, compensate_then_fail
idempotencyScope        workflow_instance, transaction_plan, node, external_operation
compensationOrder       reverse_completed_steps or configured_order
reconciliationRequired  whether final state must be verified before close
```

For enterprise HCM, default to:

```text
orchestrated saga
reverse-order compensation
idempotent steps
explicit repair state
ledgered reconciliation
```

## Side Effects And Reversibility

Every side-effecting node should declare reversibility.

Example:

```json
{
  "nodeId": "vendor_compensation_decision",
  "type": "external_write",
  "connectionId": "third_party_compensation_decision",
  "operation": "submitCompensationChange",
  "transaction": {
    "sideEffect": true,
    "idempotencyKey": {
      "$source": "transactionPlan",
      "path": "idempotencyKeys.vendorDecision"
    },
    "reversibility": "compensatable",
    "compensatingNodeId": "void_vendor_compensation_decision",
    "retryPolicy": {
      "maxAttempts": 3,
      "backoff": "exponential",
      "retryOn": ["timeout", "rate_limited", "transient_5xx"]
    },
    "reconciliation": {
      "required": true,
      "expectedStatus": "accepted"
    }
  }
}
```

Reversibility types:

```text
none             no side effect, nothing to reverse
retryable        can safely retry with same idempotency key
reversible       can directly undo the action
compensatable    cannot truly undo, but can apply a correcting transaction
manual_repair    requires human repair
irreversible     cannot be reversed; must be gated before execution
```

HCM examples:

```text
projection write                 reversible or compensatable
future-dated compensation update compensatable
payroll export                   manual_repair or compensatable
identity deactivation            reversible if not finalized
candidate offer email            irreversible, but compensatable with correction notice
document generation              compensatable by voiding/superseding
notification                     irreversible
```

Irreversible nodes need strong gates:

```text
approval complete
simulation complete
policy checks complete
required external checks complete
explicit human confirmation
```

## Compensation Nodes

Compensating actions should be modeled as normal graph nodes.

Example:

```json
{
  "nodeId": "void_vendor_compensation_decision",
  "type": "external_write",
  "title": "Void vendor compensation decision",
  "connectionId": "third_party_compensation_decision",
  "operation": "voidCompensationChange",
  "outcomes": [
    {
      "outcome": "voided",
      "eventType": "CompensatingActionSucceeded",
      "nextNodeId": "repair_vendor_decision"
    },
    {
      "outcome": "void_failed",
      "eventType": "CompensatingActionFailed",
      "nextNodeId": "manual_repair",
      "nextState": "waiting_repair",
      "nextStatus": "waiting_repair"
    }
  ]
}
```

Compensation nodes should record:

```text
which node they compensate
why compensation was triggered
whether compensation succeeded
what state remained afterward
whether manual repair is still required
```

## Failure Policies

A node should be able to declare what happens when it fails.

Example:

```json
{
  "failurePolicy": {
    "onFailure": "retry_then_compensate",
    "retryPolicy": {
      "maxAttempts": 3,
      "backoff": "exponential"
    },
    "afterRetriesExhausted": {
      "runCompensation": true,
      "nextState": "waiting_repair",
      "nextStatus": "waiting_repair",
      "nextInteraction": "vendorDecisionRepair"
    }
  }
}
```

Useful failure policies:

```text
fail_fast
retry
retry_then_repair
retry_then_compensate
compensate_then_repair
compensate_then_fail
manual_repair
ignore_with_warning
```

For HR transactions, `ignore_with_warning` should be rare. Silent partial success is dangerous.

## Idempotency

Every side-effecting node must have an idempotency strategy.

Idempotency prevents duplicate writes when:

```text
the user retries
the API request times out
the worker crashes after the external system accepted the request
the same transition is replayed
an integration returns an ambiguous response
```

Schema direction:

```json
{
  "idempotency": {
    "scope": "node",
    "keyTemplate": "vendor_comp_decision.${changeRequest.changeRequestId}",
    "onDuplicate": "read_existing_result"
  }
}
```

Duplicate behavior options:

```text
read_existing_result
reject_duplicate
allow_if_same_payload
manual_review_if_payload_differs
```

## Reconciliation

Saga completion should not mean "all HTTP calls returned 200." It should mean the internal and external states reconcile.

Schema direction:

```json
{
  "reconciliation": {
    "required": true,
    "checkNodeId": "reconcile_vendor_compensation_decision",
    "expected": {
      "externalStatus": "accepted",
      "workerId": { "$source": "workflow", "path": "subjectId" },
      "amount": { "$source": "proposedChange", "path": "proposedValue.amount" }
    },
    "onMismatch": {
      "nextState": "waiting_repair",
      "nextStatus": "waiting_repair",
      "eventType": "ReconciliationFailed"
    }
  }
}
```

Reconciliation outcomes:

```text
matched
mismatch
not_found
ambiguous
timed_out
manual_review_required
```

## Transaction Lifecycle States

Workflow state and transaction state are related but not identical.

Workflow states are user-facing:

```text
collecting_input
waiting_approval
approved
executed
waiting_repair
canceled
rejected
failed
```

Transaction states should be more execution-specific:

```text
planned
executing
partially_executed
compensating
compensated
reconciling
reconciled
waiting_repair
failed
```

The transaction plan should record completed nodes so compensation can run in the correct order.

Example execution result:

```json
{
  "completedNodes": [
    {
      "nodeId": "vendor_compensation_decision",
      "outcome": "accepted",
      "sideEffect": true,
      "idempotencyKey": "third_party_comp_decision_chg_123",
      "compensatingNodeId": "void_vendor_compensation_decision"
    }
  ],
  "failedNode": {
    "nodeId": "payroll_sync",
    "outcome": "timeout"
  },
  "compensation": {
    "status": "not_started"
  }
}
```

## External Write Routing

The compensation workflow currently models a third-party decision node like this:

```json
{
  "nodeId": "vendor_compensation_decision",
  "type": "external_write",
  "connectionId": "third_party_compensation_decision",
  "operation": "submitCompensationChange",
  "outcomes": [
    {
      "outcome": "accepted",
      "when": {
        "$source": "externalWriteResponse",
        "path": "status",
        "equals": "accepted"
      },
      "eventType": "ExternalWriteSucceeded",
      "nextNodeId": "apply_compensation_projection"
    },
    {
      "outcome": "rejected",
      "when": {
        "$source": "externalWriteResponse",
        "path": "status",
        "equals": "rejected"
      },
      "eventType": "ExternalWriteFailed",
      "nextNodeId": "repair_vendor_decision",
      "nextState": "waiting_repair",
      "nextStatus": "waiting_repair",
      "nextInteraction": "vendorDecisionRepair"
    }
  ]
}
```

This is the model we want to generalize:

```text
external response -> configured outcome condition -> ledger event -> next node/state/status/interaction
```

## Deterministic Blocks

Blocks are small pure-ish deterministic units, currently implemented in Go.

Current block references:

```json
{
  "block": {
    "name": "system.employee_data.compensation.plan_transaction",
    "version": "1.0.0"
  }
}
```

Blocks should:

- Validate inputs.
- Produce structured outputs.
- Propose internal writes.
- Propose projection patches.
- Propose external call requests.
- Avoid owning platform side effects directly.

The platform should:

- Resolve inputs from workflow sources.
- Execute the block.
- Validate output contracts.
- Persist transaction plans.
- Execute permitted side effects.
- Route based on configured outcomes.

## Template Sources

Current template expressions use source plus path:

```json
{
  "$source": "employee",
  "path": "compensation"
}
```

Current sources:

```text
input
employee
workflow
changeRequest
proposedChange
```

Likely future sources:

```text
approvalTask
transactionPlan
externalWriteResponse
ledger
tenantPolicy
actor
relationshipGraph
secretRef
environment
```

## Transaction Planning

Transaction planning should be explicit because HCM changes are high-risk.

A transaction plan can include:

```text
internalWrites
projectionPatches
externalCallRequests
rollbackPlan
compensationPlan
completedNodes
failedNode
compensatingActions
idempotencyKeys
simulationResult
executionResult
reconciliationResult
```

The workflow graph should decide what happens after each transaction step.

Example:

```text
plan transaction
  -> vendor decision accepted
  -> apply projection
  -> create succeeded outbox row
  -> complete workflow
```

Example failure route:

```text
plan transaction
  -> vendor decision rejected
  -> ledger ExternalWriteFailed
  -> no projection write applied
  -> state waiting_repair
  -> show repair interaction
```

Example partial failure route:

```text
plan transaction
  -> internal projection write applied
  -> payroll sync failed
  -> compensate internal projection write
  -> ledger CompensatingActionSucceeded
  -> state waiting_repair
```

## Ledger Events

Workflow configs should declare business timeline events, but the ledger remains the durable fact log.

Timeline config:

```json
{
  "businessEvents": [
    "WorkflowIntentStarted",
    "ChangeRequestCreated",
    "CompensationPreflighted",
    "ApprovalTaskCreated",
    "ApprovalGranted",
    "TransactionPlanCreated",
    "EmployeeCompensationUpdated",
    "ExternalWriteRequested",
    "ExternalWriteSucceeded",
    "ExternalWriteFailed",
    "WorkflowCompleted"
  ]
}
```

Ledger events should include:

- Actor.
- Tenant.
- Workflow instance.
- Change request.
- Transaction plan.
- Subject.
- Effective date.
- Idempotency key.
- Permission snapshot.
- Payload.

## Canonical Multi-Approval Example

The canonical heavy graph example is:

```text
employee.org_transfer_compensation_change
```

Its E2E fixture path is Jane Doe (`emp_123`) transferring from Boston Main
Clinic and Boston Nursing to Cambridge Clinic and Cambridge Nursing, with
target manager Sofia Rossi (`emp_461`) and target cost center `CLN-CAM`. The
stable fixture aliases are documented in
[docs/fixtures/harborcare-org-transfer.md](fixtures/harborcare-org-transfer.md).

The UX contract tests assert the generated surface before a GUI exists:

- HR intake includes current organization context and target org unit fields.
- Approval surfaces are ordered source manager, destination manager, finance,
  compensation, then medical director for clinical placement.
- Rejection and request-more-info actions require a reason.
- Timeline summaries must be user-facing history, not raw event-name dumps.

Saga-specific ledger events we likely need:

```text
SagaStarted
SagaStepStarted
SagaStepSucceeded
SagaStepFailed
CompensationStarted
CompensatingActionStarted
CompensatingActionSucceeded
CompensatingActionFailed
SagaCompensated
ReconciliationStarted
ReconciliationSucceeded
ReconciliationFailed
RepairRequested
RepairCompleted
```

## Repair Semantics

Repair is not just failure. It is a modeled state where the business can decide the next legal action.

Examples:

```text
waiting_repair
  -> revise input
  -> rerun preflight
  -> retry external write
  -> cancel request
  -> manual override with additional approval
```

Current compensation repair is minimal:

```text
vendor rejected -> waiting_repair -> cancel
```

We need to expand this into real repair actions later.

Repair should know whether compensation already ran:

```text
waiting_repair with no side effects applied
waiting_repair with side effects applied but compensated
waiting_repair with side effects applied and compensation failed
waiting_repair with ambiguous external state
```

Those are very different operational situations.

## Schema Evolution Rules

Use these rules when revising workflow schemas:

- Add node types only when a real workflow needs a new execution category.
- Keep workflow-specific business code in blocks, not TypeScript orchestration.
- Make every non-happy path an explicit outcome.
- Declare reversibility for every side-effecting node.
- Require idempotency keys for every side-effecting node.
- Prefer compensation over destructive rollback for HCM records.
- Prefer structured outcome conditions over string-matched special cases.
- Keep ledger event names stable once tests or docs depend on them.
- Keep interaction schemas separate from graph routing.
- Do not let projection writes happen before mandatory approval or external gates.
- Treat retry, repair, and compensation as first-class states.
- Version workflow configs when compatibility matters.

## Open Design Questions

- Should each workflow instance store the current graph node separately from state?
- Should graph execution be single-step per transition or support automatic node chains?
- How should parallel branches be represented?
- How should joins know when enough paths have completed?
- Should outcome conditions support richer boolean expressions?
- Should repair actions be modeled as normal transitions, graph outcomes, or both?
- How do we express field-level permissions in generated interaction schemas?
- How do we validate a workflow config before publishing?
- How do we version block contracts and workflow configs together?
- How much of transaction execution should be graph-driven versus plan-driven?
- Does every workflow need a top-level `saga` section, or should saga defaults be inherited from tenant policy?
- Should compensation nodes be authored manually, generated by blocks, or both?
- How do we represent irreversible actions that require explicit final confirmation?
- How do we handle ambiguous external responses where the external system may have succeeded but timed out?
- Should compensation be automatic, human-approved, or policy-dependent per workflow?

## Current Implementation Notes

The compensation workflow currently uses the graph for the third-party decision step. The runtime matches the external response against node outcomes and routes:

```text
status accepted -> ExternalWriteSucceeded -> projection write path
status rejected -> ExternalWriteFailed -> waiting_repair
```

This is an early implementation. The schema is ahead of the runtime in some areas. The next major step is to make the runtime execute graph nodes generically instead of using only selected graph sections during workflow execution.

Saga support is currently conceptual in the schema spec. The runtime already has transaction plans, idempotency keys, rollback plan placeholders, compensation plan placeholders, external write outcomes, and repair routing. The next implementation step is to make side-effecting nodes record completed node history and execute configured compensating nodes when later steps fail.
