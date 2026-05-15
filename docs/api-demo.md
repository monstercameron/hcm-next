# HCM Next V0 API Demo

This guide is the expected happy-path demo for the V0 workflow:

```text
employee.legal_name.change
```

The public mutation surface stays workflow-native:

```text
POST /workflow-intents
POST /workflow-instances/:workflowInstanceId/transitions
POST /documents
```

Do not add or demo direct lifecycle mutation endpoints such as `POST /employees/:id/legal-name`, `POST /change-requests/:id/approve`, or `POST /transaction-plans/:id/run`.

## Current Repo Status

This checkout includes the V0 workflow API routes, deterministic Go executor blocks, Postgres migrations/seeds, and executable Vitest coverage for the API demo path. The documented public paths are served by the framework-free Node API in `src/api`.

## Prerequisites

Expected local services:

```text
Postgres:    localhost:5432
Node API:    http://localhost:3000
Go executor: http://localhost:7001
```

Expected setup commands:

```bash
npm install
npm run db:migrate
npm run db:seed
npm run dev
```

In a second terminal:

```bash
cd src/blocks/go
go run ./cmd/executor
```

The root `db:migrate` and `db:seed` scripts run the TypeScript migration and seed runners. They require a local Postgres database. The default database is `hcm_next`; the HarborCare demo seed is designed for database-per-organization isolation and can be run against `hcm_next_harborcare` by setting `DATABASE_URL` before running the same migration and seed commands.

## Demo Seed Aliases

Use these stable aliases in examples, tests, and seed data:

| Alias                 | Meaning                     |
| --------------------- | --------------------------- |
| `tenant_demo`         | HarborCare demo tenant      |
| `env_demo`            | HarborCare demo environment |
| `actor_employee_jane` | Employee actor              |
| `actor_hr_admin`      | HR approver actor           |
| `actor_system`        | System actor                |
| `emp_123`             | Worker subject              |
| `person_123`          | Person linked to `emp_123`  |

The HarborCare seed contains a roughly 50-person clinic network. The primary self-service employee projection starts as:

Org-transfer E2E tests use the extended fixture aliases in
[docs/fixtures/harborcare-org-transfer.md](fixtures/harborcare-org-transfer.md),
including Boston Main Clinic, Cambridge Clinic, Cambridge Nursing, `CLN-BOS`,
`CLN-CAM`, Morgan Lee (`emp_456`), and Sofia Rossi (`emp_461`).

```json
{
  "employeeId": "emp_123",
  "person": {
    "personId": "person_123",
    "legalName": {
      "first": "Jane",
      "middle": null,
      "last": "Doe"
    },
    "displayName": "Jane Doe"
  }
}
```

After execution, it should read:

```json
{
  "employeeId": "emp_123",
  "person": {
    "personId": "person_123",
    "legalName": {
      "first": "Jane",
      "middle": null,
      "last": "Rivera"
    },
    "displayName": "Jane Rivera"
  }
}
```

## Required Headers

Every curl below uses:

```text
content-type: application/json
x-demo-actor-id: actor_employee_jane | actor_hr_admin | actor_system
x-correlation-id: demo-legal-name-001
```

Set local shell variables:

```bash
export API_BASE="http://localhost:3000"
export CORRELATION_ID="demo-legal-name-001"
export EMPLOYEE_ACTOR="actor_employee_jane"
export HR_ACTOR="actor_hr_admin"
export SYSTEM_ACTOR="actor_system"
```

## 1. Start Workflow Intent

Employee starts their own legal name change request.

```bash
curl -sS -X POST "$API_BASE/workflow-intents" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $EMPLOYEE_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID" \
  -d '{
    "intent": "employee.legal_name.change",
    "subject": {
      "type": "worker",
      "id": "emp_123"
    }
  }'
```

Expected response:

```json
{
  "workflowInstance": {
    "id": "wfi_demo_123",
    "intent": "employee.legal_name.change",
    "state": "collecting_input",
    "status": "active",
    "version": 1,
    "subject": {
      "type": "worker",
      "id": "emp_123"
    },
    "changeRequestId": null
  },
  "currentInteraction": {
    "type": "form",
    "title": "Request legal name change",
    "schemaVersion": "v0.1",
    "jsonSchema": {
      "type": "object",
      "required": ["newLegalName", "effectiveAt", "businessReason"]
    }
  },
  "availableActions": [
    {
      "transition": "submit_input",
      "label": "Continue",
      "enabled": true
    },
    {
      "transition": "cancel",
      "label": "Cancel",
      "enabled": true
    }
  ]
}
```

Capture the returned ID:

```bash
export WORKFLOW_INSTANCE_ID="wfi_demo_123"
```

## 2. Read Workflow Instance

```bash
curl -sS "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID" \
  -H "x-demo-actor-id: $EMPLOYEE_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID"
```

Expected state:

```json
{
  "workflowInstance": {
    "id": "wfi_demo_123",
    "state": "collecting_input",
    "status": "active",
    "version": 1
  },
  "changeRequest": null
}
```

## 3. Submit Legal Name Input

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $EMPLOYEE_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID" \
  -d '{
    "transition": "submit_input",
    "idempotencyKey": "idem_demo_submit_input_001",
    "expectedVersion": 1,
    "payload": {
      "newLegalName": {
        "first": "Jane",
        "middle": null,
        "last": "Rivera"
      },
      "effectiveAt": "2026-06-01",
      "businessReason": "legal_name_change"
    }
  }'
```

Expected response:

```json
{
  "workflowInstance": {
    "id": "wfi_demo_123",
    "state": "collecting_evidence",
    "status": "active",
    "version": 2,
    "changeRequestId": "cr_demo_123"
  },
  "currentInteraction": {
    "type": "evidence_upload",
    "title": "Upload legal name change evidence",
    "acceptedDocumentTypes": [
      "marriage_certificate",
      "court_order",
      "government_id",
      "other_legal_document"
    ],
    "maxFiles": 1
  },
  "availableActions": [
    {
      "transition": "provide_evidence",
      "label": "Submit evidence",
      "enabled": true
    },
    {
      "transition": "cancel",
      "label": "Cancel",
      "enabled": true
    }
  ]
}
```

The implementation must create a `ChangeRequest`, proposed legal-name changes, a Go preflight result, and ledger events for `WorkflowTransitionSubmitted`, `ChangeRequestCreated`, `ProposedChangeCreated`, `NameChangePreflighted`, `EvidenceRequested`, and `WorkflowStateChanged`.

## 4. Create Fake Document

```bash
curl -sS -X POST "$API_BASE/documents" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $EMPLOYEE_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID" \
  -d '{
    "purpose": "legal_name_change_evidence",
    "filename": "court-order.pdf",
    "contentType": "application/pdf",
    "classification": "sensitive_person_identity",
    "workflowInstanceId": "'"$WORKFLOW_INSTANCE_ID"'"
  }'
```

Expected response:

```json
{
  "documentId": "doc_demo_123",
  "status": "uploaded",
  "filename": "court-order.pdf",
  "contentType": "application/pdf",
  "classification": "sensitive_person_identity",
  "workflowInstanceId": "wfi_demo_123"
}
```

Capture the returned document ID:

```bash
export DOCUMENT_ID="doc_demo_123"
```

## 5. Provide Evidence

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $EMPLOYEE_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID" \
  -d '{
    "transition": "provide_evidence",
    "idempotencyKey": "idem_demo_provide_evidence_001",
    "expectedVersion": 2,
    "payload": {
      "documentId": "'"$DOCUMENT_ID"'"
    }
  }'
```

Expected response:

```json
{
  "workflowInstance": {
    "id": "wfi_demo_123",
    "state": "waiting_approval",
    "status": "waiting",
    "version": 3,
    "changeRequestId": "cr_demo_123"
  },
  "currentInteraction": {
    "type": "waiting",
    "title": "Waiting for HR approval",
    "description": "Your legal name change request has been submitted."
  },
  "availableActions": [
    {
      "transition": "cancel",
      "label": "Cancel request",
      "enabled": true
    }
  ]
}
```

The implementation must attach the document to the workflow/change request, create a pending HR approval task, and append `EvidenceProvided`, `ApprovalTaskCreated`, `ChangeRequestSubmitted`, and `WorkflowStateChanged`.

## 6. HR Reads Pending Tasks

```bash
curl -sS "$API_BASE/tasks?status=pending" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID"
```

Expected response:

```json
{
  "tasks": [
    {
      "approvalTaskId": "task_demo_123",
      "workflowInstanceId": "wfi_demo_123",
      "changeRequestId": "cr_demo_123",
      "status": "pending",
      "taskType": "hr_legal_name_review",
      "subject": {
        "type": "worker",
        "id": "emp_123"
      },
      "summary": {
        "currentDisplayName": "Jane Doe",
        "proposedDisplayName": "Jane Rivera",
        "effectiveAt": "2026-06-01"
      }
    }
  ]
}
```

Capture the returned approval task ID:

```bash
export APPROVAL_TASK_ID="task_demo_123"
```

## 7. HR Reads Available Actions

```bash
curl -sS "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/available-actions" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID"
```

Expected response:

```json
{
  "workflowInstanceId": "wfi_demo_123",
  "state": "waiting_approval",
  "version": 3,
  "actions": [
    {
      "transition": "approve",
      "label": "Approve",
      "enabled": true,
      "taskId": "task_demo_123"
    },
    {
      "transition": "reject",
      "label": "Reject",
      "enabled": true,
      "taskId": "task_demo_123"
    },
    {
      "transition": "request_more_info",
      "label": "Request more information",
      "enabled": true,
      "taskId": "task_demo_123"
    }
  ]
}
```

The same request as the employee must not include HR-only actions:

```bash
curl -sS "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/available-actions" \
  -H "x-demo-actor-id: $EMPLOYEE_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID"
```

Expected response:

```json
{
  "workflowInstanceId": "wfi_demo_123",
  "state": "waiting_approval",
  "version": 3,
  "actions": [
    {
      "transition": "cancel",
      "label": "Cancel request",
      "enabled": true
    }
  ]
}
```

## 8. HR Approves

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID" \
  -d '{
    "transition": "approve",
    "idempotencyKey": "idem_demo_approve_001",
    "expectedVersion": 3,
    "payload": {
      "approvalTaskId": "'"$APPROVAL_TASK_ID"'",
      "comment": "Evidence verified."
    }
  }'
```

Expected response:

```json
{
  "workflowInstance": {
    "id": "wfi_demo_123",
    "state": "approved",
    "status": "active",
    "version": 4,
    "changeRequestId": "cr_demo_123"
  },
  "availableActions": [
    {
      "transition": "execute",
      "label": "Execute change",
      "enabled": true
    }
  ]
}
```

The implementation must mark the approval task approved, mark the change request approved, create a transaction plan, and append `ApprovalGranted`, `ChangeRequestApproved`, `TransactionPlanCreated`, and `WorkflowStateChanged`.

## 9. Execute Approved Change

For V0, HR or system may execute an approved workflow. This example uses the system actor.

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $SYSTEM_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID" \
  -d '{
    "transition": "execute",
    "idempotencyKey": "idem_demo_execute_001",
    "expectedVersion": 4,
    "payload": {}
  }'
```

Expected response:

```json
{
  "workflowInstance": {
    "id": "wfi_demo_123",
    "state": "executed",
    "status": "completed",
    "version": 5,
    "changeRequestId": "cr_demo_123"
  },
  "currentInteraction": {
    "type": "completed",
    "title": "Legal name change completed"
  },
  "availableActions": []
}
```

The implementation must append `TransactionExecutionStarted`, `PersonLegalNameChanged`, `EmployeeProjectionUpdated`, `ExternalWriteRequested`, `TransactionExecutionCompleted`, `WorkflowCompleted`, and `WorkflowStateChanged`. It must update `employee_projection` and create one fake HRIS row in `integration_outbox`.

## 10. Read Timeline

The default timeline is the business view. It is intentionally smaller than the raw audit ledger.

```bash
curl -sS "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/timeline" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID"
```

Expected response:

```json
{
  "workflowInstanceId": "wfi_demo_123",
  "view": "business",
  "totalLedgerEventCount": 26,
  "events": [
    {
      "eventType": "WorkflowIntentStarted",
      "actorId": "actor_employee_jane",
      "summary": "Legal name change workflow started."
    },
    {
      "eventType": "ChangeRequestCreated",
      "actorId": "actor_employee_jane",
      "summary": "Legal name change request submitted."
    },
    {
      "eventType": "NameChangePreflighted",
      "actorId": "actor_employee_jane",
      "summary": "Preflight completed with low risk."
    },
    {
      "eventType": "EvidenceProvided",
      "actorId": "actor_employee_jane",
      "summary": "Evidence was provided for HR review."
    },
    {
      "eventType": "ApprovalTaskCreated",
      "actorId": "actor_employee_jane",
      "summary": "HR approval task created."
    },
    {
      "eventType": "ApprovalGranted",
      "actorId": "actor_hr_admin",
      "summary": "HR approved the legal name change."
    },
    {
      "eventType": "TransactionPlanCreated",
      "actorId": "actor_hr_admin",
      "summary": "Transaction plan created."
    },
    {
      "eventType": "PersonLegalNameChanged",
      "actorId": "actor_hr_admin",
      "summary": "Legal name changed in the employee projection."
    },
    {
      "eventType": "ExternalWriteRequested",
      "actorId": "actor_hr_admin",
      "summary": "External HRIS sync was queued."
    },
    {
      "eventType": "WorkflowCompleted",
      "actorId": "actor_hr_admin",
      "summary": "Workflow completed."
    }
  ]
}
```

Timeline reads must come from `ledger_events`, not logs.

Additional timeline views:

```bash
curl -sS "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/timeline?view=audit" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID"
```

`view=audit` returns the full append-only ledger event stream, including runtime bookkeeping like `WorkflowTransitionSubmitted` and `WorkflowStateChanged`.

```bash
curl -sS "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/timeline?view=debug" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID"
```

`view=debug` returns only runtime/debug events such as transition submissions, state changes, and transaction execution boundaries.

## 11. Verify Projection And Outbox

If a read-only demo projection endpoint is exposed, prefer curl:

```bash
curl -sS "$API_BASE/employees/emp_123" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID"
```

Expected projection excerpt:

```json
{
  "employeeId": "emp_123",
  "person": {
    "personId": "person_123",
    "legalName": {
      "first": "Jane",
      "middle": null,
      "last": "Rivera"
    },
    "displayName": "Jane Rivera"
  }
}
```

If no read endpoint exists yet, verify through Postgres:

```sql
select
  document #>> '{person,legalName,first}' as first_name,
  document #>> '{person,legalName,last}' as last_name,
  document #>> '{person,displayName}' as display_name
from employee_projection
where employee_id = 'emp_123';
```

Expected:

```text
first_name | last_name | display_name
Jane       | Rivera    | Jane Rivera
```

Verify fake external sync:

```sql
select
  destination,
  operation,
  idempotency_key,
  status,
  request_payload
from integration_outbox
where request_payload ->> 'workerId' = 'emp_123'
order by created_at desc
limit 1;
```

Expected:

```text
destination: fake_hris
operation: updateLegalName
idempotency_key: fake_hris_legal_name_cr_demo_123
status: pending
```

## Failure Response Examples

### Unauthorized Start

Employee Jane cannot start a legal name change for another worker.

```bash
curl -sS -X POST "$API_BASE/workflow-intents" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $EMPLOYEE_ACTOR" \
  -H "x-correlation-id: demo-permission-denied-001" \
  -d '{
    "intent": "employee.legal_name.change",
    "subject": {
      "type": "worker",
      "id": "emp_999"
    }
  }'
```

Expected HTTP status: `403`

```json
{
  "error": {
    "code": "PERMISSION_DENIED",
    "safeMessage": "You do not have permission to perform this action."
  }
}
```

### Employee Cannot Approve Own Request

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $EMPLOYEE_ACTOR" \
  -H "x-correlation-id: demo-permission-denied-002" \
  -d '{
    "transition": "approve",
    "idempotencyKey": "idem_demo_employee_approve_denied_001",
    "expectedVersion": 3,
    "payload": {
      "approvalTaskId": "'"$APPROVAL_TASK_ID"'",
      "comment": "I approve my own request."
    }
  }'
```

Expected HTTP status: `403`

```json
{
  "error": {
    "code": "PERMISSION_DENIED",
    "safeMessage": "You do not have permission to perform this action."
  }
}
```

### Version Conflict

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: demo-version-conflict-001" \
  -d '{
    "transition": "approve",
    "idempotencyKey": "idem_demo_wrong_version_001",
    "expectedVersion": 2,
    "payload": {
      "approvalTaskId": "'"$APPROVAL_TASK_ID"'",
      "comment": "Stale approval attempt."
    }
  }'
```

Expected HTTP status: `409`

```json
{
  "error": {
    "code": "VERSION_CONFLICT",
    "safeMessage": "The workflow changed before this action was submitted.",
    "details": {
      "expectedVersion": 2,
      "actualVersion": 3
    }
  }
}
```

### Idempotent Replay

Repeat the exact approve request with the same idempotency key and body:

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $HR_ACTOR" \
  -H "x-correlation-id: $CORRELATION_ID" \
  -d '{
    "transition": "approve",
    "idempotencyKey": "idem_demo_approve_001",
    "expectedVersion": 3,
    "payload": {
      "approvalTaskId": "'"$APPROVAL_TASK_ID"'",
      "comment": "Evidence verified."
    }
  }'
```

Expected HTTP status: `200`

Expected behavior:

```text
The response body matches the original completed transition response.
No duplicate approval, transaction plan, or ledger events are created.
```

If the same idempotency key is reused with a different request body, expected HTTP status is `409` with `IDEMPOTENCY_CONFLICT`.

## Headcount Requisition Approval Demo

This sequence uses the HarborCare fixture aliases in
`docs/fixtures/harborcare-headcount-approval.md`.

```bash
HEADCOUNT_INTENT="position.headcount_requisition.approval"
HEADCOUNT_SUBJECT="position_req_senior_rn_cambridge_nursing"
REQUESTER_ACTOR="actor_hr_admin"
LEADERSHIP_1_ACTOR="actor_manager_morgan"
LEADERSHIP_2_ACTOR="actor_emp_461"
FINANCE_ACTOR="actor_finance_admin"
COMP_ACTOR="actor_comp_admin"
CLINIC_OPS_ACTOR="actor_emp_930"
```

Start the workflow:

```bash
curl -sS -X POST "$API_BASE/workflow-intents" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $REQUESTER_ACTOR" \
  -d '{
    "intent": "'"$HEADCOUNT_INTENT"'",
    "subjectType": "position",
    "subjectId": "'"$HEADCOUNT_SUBJECT"'"
  }'
```

Submit the Senior RN Cambridge Nursing request:

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $REQUESTER_ACTOR" \
  -d '{
    "transition": "submit_input",
    "idempotencyKey": "idem_demo_headcount_submit_001",
    "expectedVersion": 1,
    "payload": {
      "department": "Clinical Care",
      "team": "Cambridge Nursing",
      "location": "Cambridge Clinic",
      "costCenter": "CLN-CAM",
      "jobCode": "CLN-RN3",
      "title": "Senior Registered Nurse",
      "level": "P3",
      "requestedFte": 1,
      "targetStartDate": "2026-07-01",
      "salaryRangeMin": 98000,
      "salaryRangeMax": 116000,
      "businessJustification": "Cambridge Nursing needs one Senior RN to cover expanded evening triage volume.",
      "selectedLeadershipApprovers": [
        "'"$LEADERSHIP_1_ACTOR"'",
        "'"$LEADERSHIP_2_ACTOR"'"
      ]
    }
  }'
```

Expected behavior:

```text
The workflow moves to waiting_sync_approval.
Only actor_manager_morgan receives the first leadership_chain_gate task.
actor_emp_461 has no leadership task until the first approval is recorded.
```

Approve the sequential leadership chain, then approve any three async tasks:

```bash
# Leadership approver 1 approves the first opened task.
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $LEADERSHIP_1_ACTOR" \
  -d '{
    "transition": "approve",
    "idempotencyKey": "idem_demo_headcount_leadership_1",
    "expectedVersion": 2,
    "payload": {
      "approvalTaskId": "'"$LEADERSHIP_1_TASK_ID"'",
      "taskVersion": 1,
      "comment": "Cambridge staffing need validated."
    }
  }'

# Leadership approver 2 approves the next task.
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: $LEADERSHIP_2_ACTOR" \
  -d '{
    "transition": "approve",
    "idempotencyKey": "idem_demo_headcount_leadership_2",
    "expectedVersion": 3,
    "payload": {
      "approvalTaskId": "'"$LEADERSHIP_2_TASK_ID"'",
      "taskVersion": 1,
      "comment": "Approved for Cambridge clinic coverage."
    }
  }'

# The async gate opens five tasks. Any three approvals pass quorum.
for ACTOR_AND_TASK in \
  "$FINANCE_ACTOR:$FINANCE_TASK_ID" \
  "$COMP_ACTOR:$COMP_TASK_ID" \
  "$CLINIC_OPS_ACTOR:$CLINIC_OPS_TASK_ID"
do
  ACTOR_ID="${ACTOR_AND_TASK%%:*}"
  TASK_ID="${ACTOR_AND_TASK##*:}"
  curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
    -H "content-type: application/json" \
    -H "x-demo-actor-id: $ACTOR_ID" \
    -d '{
      "transition": "approve",
      "idempotencyKey": "idem_demo_headcount_async_'"$ACTOR_ID"'",
      "expectedVersion": '"$CURRENT_WORKFLOW_VERSION"',
      "payload": {
        "approvalTaskId": "'"$TASK_ID"'",
        "taskVersion": 1,
        "comment": "Approved."
      }
    }'
done
```

Expected behavior:

```text
The workflow moves to approved after the third async approval.
The HRBP and Medical Director pending tasks are canceled.
Ledger events include ApprovalGateOpened, ApprovalGateTaskCreated,
ApprovalGateTaskDecided, ApprovalGatePassed, ApprovalGateTaskCanceled, and
HeadcountRequisitionApproved.
```

Execute the approved requisition:

```bash
curl -sS -X POST "$API_BASE/workflow-instances/$WORKFLOW_INSTANCE_ID/transitions" \
  -H "content-type: application/json" \
  -H "x-demo-actor-id: actor_system" \
  -d '{
    "transition": "execute",
    "idempotencyKey": "idem_demo_headcount_execute_001",
    "expectedVersion": '"$CURRENT_WORKFLOW_VERSION"',
    "payload": {}
  }'
```

Expected behavior:

```text
The workflow moves to executed.
Replay with the same execute idempotency key returns the cached response and
does not create duplicate approval tasks or ledger events.
```

## Demo Readiness Checklist

- [x] Dependencies install from a clean checkout.
- [x] Postgres migration and seed runners exist.
- [x] Node API builds and can run on `http://localhost:3000`.
- [x] Go executor starts on `http://localhost:7001`.
- [x] `POST /workflow-intents` starts `employee.legal_name.change`.
- [x] `submit_input` moves the workflow to `collecting_evidence`.
- [x] `POST /documents` returns a fake uploaded document.
- [x] `provide_evidence` moves the workflow to `waiting_approval`.
- [x] HR sees a pending task and approve/reject/request-more-info actions.
- [x] Employee does not see HR approval actions.
- [x] HR approval creates a transaction plan.
- [x] Execute moves the workflow to `executed`.
- [x] Timeline shows the full ledger-derived causality chain.
- [x] `employee_projection` changes from Jane Doe to Jane Rivera.
- [x] `integration_outbox` has a pending fake HRIS `updateLegalName` request.
- [x] Unauthorized employee start returns `PERMISSION_DENIED`.
- [x] Employee self-approval returns `PERMISSION_DENIED`.
- [x] Wrong `expectedVersion` returns `VERSION_CONFLICT`.
- [x] Duplicate idempotency replay returns the stored response.
- [x] Lint passes.
- [x] Typecheck passes.
- [x] Tests pass.

Postgres itself must be started locally before running `npm run db:migrate` and `npm run db:seed`.
