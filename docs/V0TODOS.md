## Verdict

Your API instinct is **right**: the public API should be workflow-native. The app should not call “preflight,” “approve,” “execute,” or “write employee field” as random procedural endpoints. The app should submit **intents** and **transitions**, and the workflow engine should decide what is legal next.

But for a V0, your plan is still missing one crucial product/architecture concept:

> **A WorkflowInstance must be a first-class persisted object.**

Right now, your flow implies a workflow instance, but your existing data model is centered on `ChangeRequest`, `ApprovalTask`, `TransactionPlan`, ledger events, and projections. That is good, but not enough. `ChangeRequest` is the business mutation. `WorkflowInstance` is the runtime lifecycle that decides what the user can do next.

So the refined model is:

```text
WorkflowIntent
  -> WorkflowInstance
    -> current state
    -> current interaction / UI schema
    -> available actions
    -> transition history
    -> ChangeRequest, when applicable
      -> ProposedChanges
      -> ApprovalTasks
      -> TransactionPlan
      -> LedgerEvents
      -> Projections
      -> IntegrationOutbox
```

For V0, build **one workflow**:

> **Employee Legal Name Change**

This is not the biggest-money workflow. Promotion/compensation is the stronger flagship later because your own plan identifies it as the best ChangeOps workflow for proving compensation, approvals, effective dating, permission filtering, simulation, and reconciliation.
But legal name change is a better **V0 prototype** because it touches almost every platform primitive without dragging you into compensation policy, pay bands, budget, payroll proration, or org simulation.

---

# 1. The V0 workflow you should build

## Workflow name

```text
employee.legal_name.change
```

## Business outcome

A worker’s legal name is changed, with evidence, approval, audit, and optional external sync.

## Why this is the right V0

Your uploaded model already says an employee data change needs current person data, proposed person data, field sensitivity, actor permission, approval requirement, evidence requirement, effective date, and external mappings; it also lists name change as an example and outputs person fact update, document/evidence record, payroll/tax sync if relevant, and audit event.

That makes legal name change a near-perfect V0 because it proves:

| Platform capability         | Proven by legal name change                                    |
| --------------------------- | -------------------------------------------------------------- |
| Intent-based workflow start | Employee starts `employee.legal_name.change`                   |
| Permission resolution       | Employee can request own name change; HR can approve           |
| Generated UI                | Form → evidence upload → approval card → confirmation          |
| Field-level sensitivity     | Legal name and evidence are sensitive                          |
| Evidence handling           | Upload legal document                                          |
| Approval task               | HR verifies evidence                                           |
| ChangeRequest creation      | Proposed mutation becomes formal business object               |
| ProposedChange rows         | `person.legalName.first`, `person.legalName.last`              |
| Go preflight block          | Validate required fields, evidence rules, date rules           |
| TransactionPlan             | Internal projection write + optional external sync             |
| Ledger                      | Everything recorded with actor, causation, permission snapshot |
| Projection                  | Employee/person projection updates                             |
| Audit timeline              | Shows who requested, approved, executed, and what changed      |

It is small enough to build, but real enough to prove the spine.

---

# 2. Scope the V0 brutally

## Include

Build only this:

```text
Employee requests legal name change
→ system asks for evidence
→ employee uploads document
→ HR approves or rejects
→ workflow executes approved change
→ employee projection updates
→ audit timeline shows full lifecycle
→ optional fake external sync writes to integration_outbox
```

## Exclude

Do **not** build these in V0:

| Exclude                              | Why                                                                       |
| ------------------------------------ | ------------------------------------------------------------------------- |
| Preferred name change variant        | Too easy; does not prove evidence/approval                                |
| Payroll real integration             | Use outbox stub only                                                      |
| Tax/government ID updates            | Sensitive and complicated                                                 |
| Multiple countries/jurisdictions     | Hardcode one policy                                                       |
| Multiple approvers                   | One HR approver is enough                                                 |
| Full visual workflow builder         | Use static workflow JSON                                                  |
| Custom Go block SDK                  | Use one built-in Go block                                                 |
| AI review                            | Optional; add after deterministic flow works                              |
| Complex future-dated materialization | Either support immediate only or implement a very minimal scheduled state |

This V0 is not supposed to impress with breadth. It should impress because the architecture is clean.

---

# 3. Refined API surface

Your proposed public API is directionally correct:

```http
POST /workflow-intents
POST /workflow-instances/:id/transitions
GET  /workflow-instances/:id
GET  /workflow-instances/:id/available-actions
```

I would refine it to this V0 API:

```http
POST /workflow-intents
GET  /workflow-instances/:workflowInstanceId
GET  /workflow-instances/:workflowInstanceId/available-actions
GET  /workflow-instances/:workflowInstanceId/timeline
POST /workflow-instances/:workflowInstanceId/transitions

POST /documents
GET  /documents/:documentId
```

The document endpoint is the one missing public surface. Evidence upload is not just a workflow transition. The user needs a way to create/upload a document, scan/classify it, and then attach it through a transition.

For V0, `POST /documents` can be fake or local-file-backed, but the domain concept needs to exist.

---

# 4. Critical API rule

Every transition request needs these fields:

```json
{
  "transition": "submit_input",
  "idempotencyKey": "idem_123",
  "expectedVersion": 3,
  "payload": {}
}
```

Do not skip `idempotencyKey` and `expectedVersion`.

Without them, double-clicks, retries, mobile refreshes, and race conditions will corrupt your workflow.

## Transition request shape

```json
{
  "transition": "provide_evidence",
  "idempotencyKey": "wf_wfi_123_transition_004",
  "expectedVersion": 4,
  "payload": {
    "documentId": "doc_123"
  }
}
```

## Transition response shape

```json
{
  "workflowInstance": {
    "id": "wfi_123",
    "status": "waiting_approval",
    "state": "hr_review",
    "version": 5,
    "subject": {
      "type": "worker",
      "id": "emp_123"
    },
    "changeRequestId": "cr_123"
  },
  "currentInteraction": {
    "type": "waiting",
    "title": "Waiting for HR approval",
    "description": "Your legal name change has been submitted for review."
  },
  "availableActions": [
    {
      "transition": "cancel",
      "label": "Cancel request",
      "requiresPayload": true
    }
  ],
  "timeline": [
    {
      "eventType": "EvidenceProvided",
      "occurredAt": "2026-05-15T14:20:00Z"
    }
  ]
}
```

---

# 5. The missing object: `workflow_instances`

You need this table.

Your current architecture already has `workflow_definitions`, `workflow_versions`, `change_requests`, `approval_tasks`, `transaction_plans`, ledger events, and projections. The ledger/projection strategy is strong: truth lives in immutable events, while product state is read through projections.
But the workflow runtime still needs a durable current-state record.

## Add this table

```sql
CREATE TABLE workflow_instances (
  workflow_instance_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid REFERENCES environments(environment_id),

  workflow_definition_id uuid NOT NULL REFERENCES workflow_definitions(workflow_definition_id),
  workflow_version_id uuid NOT NULL REFERENCES workflow_versions(workflow_version_id),

  intent text NOT NULL,
  subject_type text NOT NULL,
  subject_id text NOT NULL,

  status text NOT NULL DEFAULT 'active',
  state text NOT NULL,

  change_request_id uuid REFERENCES change_requests(change_request_id),

  requester_actor_id uuid NOT NULL REFERENCES actors(actor_id),
  current_interaction jsonb NOT NULL DEFAULT '{}'::jsonb,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,

  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  canceled_at timestamptz,
  failed_at timestamptz,

  version integer NOT NULL DEFAULT 1,
  correlation_id text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_instances_status_check
    CHECK (status IN (
      'active',
      'waiting',
      'completed',
      'rejected',
      'canceled',
      'failed',
      'superseded',
      'waiting_repair'
    ))
);

CREATE INDEX workflow_instances_subject_idx
  ON workflow_instances (tenant_id, subject_type, subject_id, created_at DESC);

CREATE INDEX workflow_instances_status_idx
  ON workflow_instances (tenant_id, status, updated_at DESC);

CREATE INDEX workflow_instances_change_request_idx
  ON workflow_instances (tenant_id, change_request_id);
```

`ChangeRequest.status` tells you the business mutation status.

`WorkflowInstance.state` tells you what step the runtime is in.

They are related, but not the same thing.

---

# 6. V0 state machine

Use this state machine:

```text
intent_started
  -> collecting_input
  -> collecting_evidence
  -> preflighted
  -> waiting_approval
  -> approved
  -> executing
  -> executed
  -> closed

Alternate paths:
  collecting_input -> canceled
  collecting_evidence -> canceled
  waiting_approval -> rejected
  waiting_approval -> needs_more_info
  executing -> failed
  failed -> waiting_repair
  waiting_repair -> executed
```

For V0, you can simplify:

```text
collecting_input
collecting_evidence
waiting_approval
approved
executing
executed
rejected
canceled
failed
```

Do **not** make every state a separate public endpoint. States are internal. Public clients only submit transitions.

---

# 7. V0 transition map

## 7.1 Start intent

```http
POST /workflow-intents
```

Request:

```json
{
  "intent": "employee.legal_name.change",
  "subject": {
    "type": "worker",
    "id": "emp_123"
  }
}
```

Engine does:

```text
Authenticate actor
Resolve tenant
Check actor can initiate this intent
Resolve workflow definition/version
Load permission-filtered employee projection
Create WorkflowInstance
Append WorkflowIntentStarted ledger event
Generate first UI schema
Return available actions
```

Response:

```json
{
  "workflowInstance": {
    "id": "wfi_123",
    "intent": "employee.legal_name.change",
    "state": "collecting_input",
    "status": "active",
    "version": 1,
    "subject": {
      "type": "worker",
      "id": "emp_123"
    }
  },
  "currentInteraction": {
    "type": "form",
    "schemaVersion": "2026-05-15.1",
    "title": "Request legal name change",
    "jsonSchema": {
      "type": "object",
      "required": ["newLegalName", "effectiveAt", "businessReason"],
      "properties": {
        "newLegalName": {
          "type": "object",
          "required": ["first", "last"],
          "properties": {
            "first": { "type": "string", "minLength": 1 },
            "middle": { "type": "string" },
            "last": { "type": "string", "minLength": 1 }
          }
        },
        "effectiveAt": {
          "type": "string",
          "format": "date"
        },
        "businessReason": {
          "type": "string",
          "enum": ["marriage", "divorce", "legal_name_change", "correction", "other"]
        }
      }
    },
    "uiSchema": {
      "layout": "wizard",
      "submitLabel": "Continue"
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

---

## 7.2 Submit input

```http
POST /workflow-instances/wfi_123/transitions
```

Request:

```json
{
  "transition": "submit_input",
  "idempotencyKey": "idem_wfi_123_submit_001",
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
}
```

Engine does:

```text
Check transition is allowed from collecting_input
Validate expectedVersion
Validate payload against current interaction schema
Load current legal name from permission-filtered projection
Create ChangeRequest
Create ProposedChange rows
Call Go preflight block
Store preflight result
Append ledger events
Move workflow state to collecting_evidence
Generate evidence upload UI
```

Ledger events:

```text
WorkflowTransitionSubmitted
ChangeRequestCreated
ProposedChangeCreated
NameChangePreflighted
EvidenceRequested
WorkflowStateChanged
```

Response:

```json
{
  "workflowInstance": {
    "id": "wfi_123",
    "state": "collecting_evidence",
    "status": "active",
    "version": 2,
    "changeRequestId": "cr_123"
  },
  "currentInteraction": {
    "type": "evidence_upload",
    "title": "Upload legal name change evidence",
    "description": "Upload a marriage certificate, court order, updated government ID, or other approved legal document.",
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
      "label": "Cancel request",
      "enabled": true
    }
  ]
}
```

---

## 7.3 Upload document

This happens outside the workflow transition.

```http
POST /documents
```

Request:

```json
{
  "purpose": "legal_name_change_evidence",
  "filename": "court-order.pdf",
  "contentType": "application/pdf",
  "classification": "sensitive_person_identity",
  "workflowInstanceId": "wfi_123"
}
```

Response:

```json
{
  "documentId": "doc_123",
  "uploadUrl": "https://example.local/upload/doc_123",
  "status": "pending_upload"
}
```

For local V0, skip real object storage and just return `documentId`.

But keep the shape because later you will need object storage, malware scan, document classification, retention rules, and permission filtering.

---

## 7.4 Provide evidence

```http
POST /workflow-instances/wfi_123/transitions
```

Request:

```json
{
  "transition": "provide_evidence",
  "idempotencyKey": "idem_wfi_123_evidence_001",
  "expectedVersion": 2,
  "payload": {
    "documentId": "doc_123"
  }
}
```

Engine does:

```text
Check transition allowed from collecting_evidence
Check document belongs to tenant
Check document is attached to same workflow or actor
Check document classification is allowed
Check document scan status is clean or skipped in local V0
Attach evidence to ChangeRequest
Create HR approval task
Move state to waiting_approval
Append ledger events
Generate waiting UI for requester
Generate approval UI for HR approver
```

Ledger events:

```text
EvidenceProvided
ApprovalTaskCreated
ChangeRequestSubmitted
WorkflowStateChanged
```

Response to requester:

```json
{
  "workflowInstance": {
    "id": "wfi_123",
    "state": "waiting_approval",
    "status": "waiting",
    "version": 3
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

---

## 7.5 HR gets available actions

```http
GET /workflow-instances/wfi_123/available-actions
```

Response for HR approver:

```json
{
  "workflowInstanceId": "wfi_123",
  "state": "waiting_approval",
  "version": 3,
  "actions": [
    {
      "transition": "approve",
      "label": "Approve",
      "enabled": true,
      "taskId": "task_123",
      "inputSchema": {
        "type": "object",
        "properties": {
          "comment": { "type": "string" }
        }
      }
    },
    {
      "transition": "reject",
      "label": "Reject",
      "enabled": true,
      "taskId": "task_123",
      "inputSchema": {
        "type": "object",
        "required": ["reason"],
        "properties": {
          "reason": { "type": "string" }
        }
      }
    },
    {
      "transition": "request_more_info",
      "label": "Request more information",
      "enabled": true,
      "taskId": "task_123"
    }
  ]
}
```

Response for non-HR actor:

```json
{
  "workflowInstanceId": "wfi_123",
  "state": "waiting_approval",
  "version": 3,
  "actions": []
}
```

Important: `available-actions` is actor-specific. The same workflow has different actions depending on who is asking.

Your access model already points in this direction: access depends on role permissions, attributes, relationship to record, workflow state, field sensitivity, tenant policy, and AI policy.

---

## 7.6 Approve

```http
POST /workflow-instances/wfi_123/transitions
```

Request:

```json
{
  "transition": "approve",
  "idempotencyKey": "idem_wfi_123_approve_001",
  "expectedVersion": 3,
  "payload": {
    "approvalTaskId": "task_123",
    "comment": "Evidence verified."
  }
}
```

Engine does:

```text
Check actor owns approval task or has HR override
Mark ApprovalTask approved
Move ChangeRequest to approved
Create TransactionPlan
Optionally run Go transaction planning block
Move workflow state to approved
Append ledger events
Return execute action or auto-execute
```

Ledger events:

```text
ApprovalGranted
ChangeRequestApproved
TransactionPlanCreated
WorkflowStateChanged
```

Response:

```json
{
  "workflowInstance": {
    "id": "wfi_123",
    "state": "approved",
    "status": "active",
    "version": 4
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

For V0, manual `execute` is fine because it demonstrates the transition model.

For the actual product, you will probably auto-execute after final approval when policy allows it.

---

## 7.7 Execute

```http
POST /workflow-instances/wfi_123/transitions
```

Request:

```json
{
  "transition": "execute",
  "idempotencyKey": "idem_wfi_123_execute_001",
  "expectedVersion": 4,
  "payload": {}
}
```

Engine does:

```text
Acquire DB lock on workflow instance/change request
Check state is approved
Check all required approvals are complete
Check evidence exists
Check idempotency
Append PersonLegalNameChanged or PersonLegalNameChangeScheduled
Update employee projection if effective now
Update change request projection
Mark transaction plan executed
Create integration_outbox row if fake external sync enabled
Move workflow state to executed
Append final ledger events
```

Ledger events:

```text
TransactionExecutionStarted
PersonLegalNameChanged
EmployeeProjectionUpdated
ExternalWriteRequested
TransactionExecutionCompleted
WorkflowCompleted
```

Response:

```json
{
  "workflowInstance": {
    "id": "wfi_123",
    "state": "executed",
    "status": "completed",
    "version": 5
  },
  "currentInteraction": {
    "type": "confirmation",
    "title": "Legal name change completed",
    "description": "Jane Doe is now Jane Rivera."
  },
  "availableActions": []
}
```

---

# 8. The effective date trap

Your example includes:

```json
"effectiveAt": "2026-06-01"
```

Do not accept `effectiveAt` and then ignore it.

For V0, pick one of these two paths:

## Option A — easiest V0

Only allow immediate changes:

```text
effectiveAt must be today
```

This is simplest. It lets you prove the workflow without building scheduled materialization.

## Option B — better architecture V0

Support future-dated events minimally:

```text
If effectiveAt <= now:
  append PersonLegalNameChanged
  update current employee projection

If effectiveAt > now:
  append PersonLegalNameChangeScheduled
  store pending scheduled change in projection
  do not change current legalName yet
```

Then `employee_projection.document` can include:

```json
{
  "person": {
    "legalName": {
      "first": "Jane",
      "last": "Doe"
    },
    "scheduledChanges": [
      {
        "type": "legal_name_change",
        "effectiveAt": "2026-06-01",
        "legalName": {
          "first": "Jane",
          "last": "Rivera"
        },
        "workflowInstanceId": "wfi_123",
        "changeRequestId": "cr_123"
      }
    ]
  }
}
```

For V0, I would choose **Option B** because effective dating is one of the core reasons the ledger/projection architecture matters. Your data strategy explicitly treats effective dating as first-class and distinguishes recorded time from effective time.

---

# 9. What is missing from your current plan

## Missing 1: WorkflowInstance persistence

You need `workflow_instances`. Without it, `GET /workflow-instances/:id` has nothing authoritative to read except a reconstructed ledger or a ChangeRequest approximation.

## Missing 2: Transition idempotency

Every transition needs:

```text
idempotencyKey
expectedVersion
actor context
transition receipt
```

Add either a table or enforce through ledger indexes.

Recommended table:

```sql
CREATE TABLE workflow_transition_attempts (
  workflow_transition_attempt_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workflow_instance_id uuid NOT NULL REFERENCES workflow_instances(workflow_instance_id),

  transition text NOT NULL,
  idempotency_key text NOT NULL,
  expected_version integer,
  actor_id uuid REFERENCES actors(actor_id),

  request_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  response_payload jsonb,
  status text NOT NULL DEFAULT 'processing',
  error_code text,
  error_message text,

  created_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,

  CONSTRAINT workflow_transition_attempts_unique
    UNIQUE (tenant_id, workflow_instance_id, idempotency_key)
);
```

## Missing 3: Actor-specific available actions

`available-actions` cannot be global. It must depend on:

```text
actor
tenant
roles
relationship to worker
workflow state
field sensitivity
approval task assignment
delegation
permission policies
```

A requester, HR approver, payroll admin, and tenant admin should all see different actions.

## Missing 4: Current interaction snapshot

The generated UI should be versioned and persisted.

Why? Because the form the user submitted must be auditable. If the workflow definition changes tomorrow, you still need to know what schema the user saw today.

Add this field to `workflow_instances`:

```sql
current_interaction jsonb NOT NULL DEFAULT '{}'::jsonb
```

For deeper history, log interaction snapshots in the ledger.

## Missing 5: Document/evidence model

You need at least:

```sql
CREATE TABLE documents (
  document_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),

  owner_actor_id uuid REFERENCES actors(actor_id),
  subject_type text,
  subject_id text,

  purpose text NOT NULL,
  classification text NOT NULL,
  filename text NOT NULL,
  content_type text,
  storage_uri text,

  status text NOT NULL DEFAULT 'created',
  scan_status text NOT NULL DEFAULT 'not_scanned',

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT documents_status_check
    CHECK (status IN ('created', 'uploaded', 'attached', 'deleted')),
  CONSTRAINT documents_scan_status_check
    CHECK (scan_status IN ('not_scanned', 'pending', 'clean', 'failed', 'infected', 'skipped'))
);
```

And:

```sql
CREATE TABLE workflow_instance_documents (
  workflow_instance_document_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workflow_instance_id uuid NOT NULL REFERENCES workflow_instances(workflow_instance_id),
  change_request_id uuid REFERENCES change_requests(change_request_id),
  document_id uuid NOT NULL REFERENCES documents(document_id),

  relationship_type text NOT NULL,
  attached_by uuid REFERENCES actors(actor_id),
  attached_at timestamptz NOT NULL DEFAULT now(),

  metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);
```

## Missing 6: Task inbox API

For V0, HR needs to find approvals.

Add:

```http
GET /tasks?status=pending
GET /tasks/:taskId
```

Or:

```http
GET /approval-tasks?assignee=me&status=pending
```

This is not a workflow mutation API. It is a read API.

## Missing 7: Reject and request-more-info paths

Your flow has approve, but V0 needs at least:

```text
reject
request_more_info
cancel
```

Otherwise the workflow only works on the happy path, which is not enough to prove the engine.

## Missing 8: Execution locking

The `execute` transition must lock the workflow/change request.

Otherwise two requests can execute the same change.

In Postgres:

```sql
SELECT *
FROM workflow_instances
WHERE workflow_instance_id = $1
FOR UPDATE;
```

Then check:

```text
state = approved
version = expectedVersion
not already executed
idempotencyKey unused
```

## Missing 9: Read-only audit timeline

Add:

```http
GET /workflow-instances/:id/timeline
```

This should read from ledger events, not random logs.

Your plan already treats ledger events as the source for actor, timestamp, record, field changes, workflow instance, approval chain, AI involvement, integrations, permission context, before/after state, business reason, and source system.

## Missing 10: Failure states

You need to model failure as business state, not backend logs.

Your architecture docs already call this out: workflows and transactions need explicit failure states such as failed permission check, failed approval, failed external call, failed rollback, waiting for manual repair, and resolved manually/automatically.

For V0, implement these:

```text
failed_permission_check
failed_validation
failed_execution
waiting_repair
```

---

# 10. Minimal V0 data model

Use these tables.

## Already in your plan

```text
tenants
environments
actors
ledger_events
change_requests
proposed_changes
transaction_plans
approval_tasks
employee_projection
change_request_projection
workflow_definitions
workflow_versions
permission_policies
integration_outbox
```

## Add for this V0

```text
workflow_instances
workflow_transition_attempts
documents
workflow_instance_documents
```

That is enough.

Do not add person fact tables yet. Use `employee_projection.document.person`.

---

# 11. Minimal employee projection for V0

Seed one worker:

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
    "displayName": "Jane Doe",
    "preferredName": null,
    "workEmail": "jane.doe@example.com"
  },
  "employment": {
    "status": "active",
    "legalEntity": "US-001"
  },
  "manager": {
    "employeeId": "emp_456"
  },
  "custom": {}
}
```

After immediate execution:

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
    "displayName": "Jane Rivera",
    "preferredName": null,
    "workEmail": "jane.doe@example.com"
  }
}
```

---

# 12. Minimal workflow definition

For V0, define workflow in JSON. Do not build a workflow canvas.

```json
{
  "workflowType": "employee_data_change",
  "intent": "employee.legal_name.change",
  "version": 1,
  "initialState": "collecting_input",
  "states": {
    "collecting_input": {
      "interaction": "legal_name_form",
      "transitions": {
        "submit_input": {
          "requiresPermission": "employee_data_change.legal_name.request",
          "handler": "submitLegalNameInput",
          "next": "collecting_evidence"
        },
        "cancel": {
          "handler": "cancelWorkflow",
          "next": "canceled"
        }
      }
    },
    "collecting_evidence": {
      "interaction": "legal_name_evidence_upload",
      "transitions": {
        "provide_evidence": {
          "requiresPermission": "employee_data_change.legal_name.provide_evidence",
          "handler": "provideLegalNameEvidence",
          "next": "waiting_approval"
        },
        "cancel": {
          "handler": "cancelWorkflow",
          "next": "canceled"
        }
      }
    },
    "waiting_approval": {
      "interaction": "hr_approval_card",
      "transitions": {
        "approve": {
          "requiresPermission": "employee_data_change.legal_name.approve",
          "requiresApprovalTask": true,
          "handler": "approveLegalNameChange",
          "next": "approved"
        },
        "reject": {
          "requiresPermission": "employee_data_change.legal_name.reject",
          "requiresApprovalTask": true,
          "handler": "rejectLegalNameChange",
          "next": "rejected"
        },
        "request_more_info": {
          "requiresPermission": "employee_data_change.legal_name.request_more_info",
          "requiresApprovalTask": true,
          "handler": "requestMoreInfo",
          "next": "collecting_evidence"
        }
      }
    },
    "approved": {
      "interaction": "ready_to_execute",
      "transitions": {
        "execute": {
          "requiresPermission": "employee_data_change.legal_name.execute",
          "handler": "executeLegalNameChange",
          "next": "executed"
        }
      }
    },
    "executed": {
      "terminal": true
    },
    "rejected": {
      "terminal": true
    },
    "canceled": {
      "terminal": true
    },
    "failed": {
      "terminal": false
    }
  }
}
```

This proves the engine without overbuilding Process Studio.

Your master plan says runtime UX should be schema-driven, validated, permission-aware, and cheap to execute; this JSON-first workflow definition aligns with that direction.

---

# 13. Minimal Go executor

Your architecture says Node/TypeScript should own the control plane, Go should own deterministic or performance-sensitive execution, and Postgres should record what happened. It also says Go should not freely call external systems in V1; Go should produce side-effect specs and Node should execute them.

So for V0, build exactly two Go blocks.

## Block 1: `system.employee_data.legal_name.preflight@1.0.0`

Input:

```json
{
  "currentLegalName": {
    "first": "Jane",
    "middle": null,
    "last": "Doe"
  },
  "proposedLegalName": {
    "first": "Jane",
    "middle": null,
    "last": "Rivera"
  },
  "effectiveAt": "2026-06-01",
  "businessReason": "legal_name_change"
}
```

Output:

```json
{
  "valid": true,
  "riskLevel": "low",
  "requiresEvidence": true,
  "requiresApproval": true,
  "warnings": [],
  "errors": []
}
```

Validation rules:

```text
first required
last required
new name must differ from current name
business reason required
effective date required
effective date cannot be more than 180 days in past
evidence required for legal name change
approval required from HR
```

## Block 2: `system.employee_data.legal_name.plan_transaction@1.0.0`

Input:

```json
{
  "changeRequestId": "cr_123",
  "workerId": "emp_123",
  "personId": "person_123",
  "currentLegalName": {
    "first": "Jane",
    "last": "Doe"
  },
  "proposedLegalName": {
    "first": "Jane",
    "last": "Rivera"
  },
  "effectiveAt": "2026-06-01"
}
```

Output:

```json
{
  "internalWrites": [
    {
      "eventType": "PersonLegalNameChanged",
      "subjectType": "worker",
      "subjectId": "emp_123",
      "effectiveAt": "2026-06-01",
      "payload": {
        "personId": "person_123",
        "previousLegalName": {
          "first": "Jane",
          "last": "Doe"
        },
        "newLegalName": {
          "first": "Jane",
          "last": "Rivera"
        }
      }
    }
  ],
  "externalCallRequests": [
    {
      "connectionId": "fake_hris",
      "operation": "updateLegalName",
      "idempotencyKey": "fake_hris_legal_name_cr_123",
      "payload": {
        "workerId": "emp_123",
        "legalName": {
          "first": "Jane",
          "last": "Rivera"
        },
        "effectiveAt": "2026-06-01"
      }
    }
  ]
}
```

For V0, Node can ignore or stub the external request, but the shape should exist.

---

# 14. Ledger events for V0

Minimum event types:

```text
WorkflowIntentStarted
WorkflowTransitionSubmitted
WorkflowStateChanged

ChangeRequestCreated
ProposedChangeCreated
NameChangePreflighted
EvidenceRequested
EvidenceProvided

ApprovalTaskCreated
ApprovalGranted
ApprovalRejected

TransactionPlanCreated
TransactionExecutionStarted
PersonLegalNameChanged
PersonLegalNameChangeScheduled
EmployeeProjectionUpdated
ExternalWriteRequested
ExternalWriteSucceeded
ExternalWriteFailed
TransactionExecutionCompleted

WorkflowCompleted
WorkflowCanceled
WorkflowFailed
```

Do not just log these. Persist them as ledger events.

---

# 15. Internal status mapping

Keep workflow state and business object status aligned but separate.

| Workflow state        | ChangeRequest status   | Meaning                                   |
| --------------------- | ---------------------- | ----------------------------------------- |
| `collecting_input`    | none yet or `draft`    | User is filling the first form            |
| `collecting_evidence` | `needs_data`           | ChangeRequest exists but evidence missing |
| `waiting_approval`    | `in_approval`          | HR task pending                           |
| `approved`            | `approved`             | Ready to execute                          |
| `executing`           | `executing`            | Transaction in progress                   |
| `executed`            | `executed` or `closed` | Internal change applied or scheduled      |
| `rejected`            | `rejected`             | HR rejected                               |
| `canceled`            | `canceled`             | Requester canceled                        |
| `failed`              | `failed`               | System failure                            |
| `waiting_repair`      | `waiting_repair`       | External sync or repair needed            |

---

# 16. Permission model for V0

Hardcode three roles:

```text
employee
hr_admin
system
```

Hardcode these permissions:

```text
employee_data_change.legal_name.request
employee_data_change.legal_name.provide_evidence
employee_data_change.legal_name.cancel_own
employee_data_change.legal_name.approve
employee_data_change.legal_name.reject
employee_data_change.legal_name.execute
employee_data_change.legal_name.view_evidence
```

Policy:

```text
Employee can request own legal name change.
Employee can provide evidence for own legal name change.
Employee can cancel own request before approval.
Employee cannot approve own legal name change.
HR admin can view request and evidence.
HR admin can approve/reject.
System can execute after approval.
```

For field visibility:

| Actor               | Current legal name | Proposed legal name |                    Evidence |
| ------------------- | -----------------: | ------------------: | --------------------------: |
| Requesting employee |                Yes |                 Yes |            Own uploaded doc |
| HR admin            |                Yes |                 Yes |                         Yes |
| Manager             |           No in V0 |            No in V0 |                          No |
| System              |                Yes |                 Yes | Metadata only unless needed |

This keeps field-level sensitivity visible in the prototype without overbuilding the permission engine.

---

# 17. What the UI should show

Build four screens.

## Screen 1: Employee form

Route:

```text
/workflows/:workflowInstanceId
```

Shows generated form from `currentInteraction`.

Fields:

```text
Current legal name, read-only
New first name
New middle name
New last name
Effective date
Business reason
Submit
Cancel
```

## Screen 2: Evidence upload

Shows:

```text
Required evidence explanation
Upload document button
Submit evidence
Cancel
```

## Screen 3: HR approval card

Shows:

```text
Requester
Current legal name
Proposed legal name
Effective date
Business reason
Evidence document metadata/link
Preflight result
Approve
Reject
Request more info
```

## Screen 4: Audit timeline

Shows:

```text
Intent started
Input submitted
Evidence provided
HR approval created
HR approved
Transaction executed
Projection updated
External sync requested/succeeded/failed
```

Your product plan already treats workflow timeline, human-readable change summary, approval history, AI involvement history, external system history, record lineage, and before/after comparisons as core traceability features.

---

# 18. Build order for V0

## Phase 1: Backend skeleton

Build:

```text
Postgres migrations
Tenant seed
Actor seed
Employee projection seed
Workflow definition seed
Ledger append helper
Permission helper
Workflow instance CRUD internals
```

Do not build frontend yet.

## Phase 2: Intent API

Build:

```http
POST /workflow-intents
GET /workflow-instances/:id
GET /workflow-instances/:id/available-actions
```

Acceptance test:

```text
Employee actor can start own legal name change.
Random employee cannot start another worker's legal name change.
HR can see workflow state.
Available actions differ by actor.
```

## Phase 3: Submit input transition

Build:

```http
POST /workflow-instances/:id/transitions
```

Implement only:

```text
submit_input
cancel
```

Acceptance test:

```text
submit_input creates ChangeRequest.
submit_input creates ProposedChange rows.
submit_input appends ledger events.
submit_input moves state to collecting_evidence.
Duplicate idempotencyKey returns same response.
Wrong expectedVersion fails.
```

## Phase 4: Evidence

Build:

```http
POST /documents
POST /workflow-instances/:id/transitions provide_evidence
```

Acceptance test:

```text
provide_evidence attaches document.
provide_evidence creates ApprovalTask.
provide_evidence moves state to waiting_approval.
HR sees approve/reject actions.
Employee does not see approve/reject actions.
```

## Phase 5: Approval

Build:

```text
approve
reject
request_more_info
```

Acceptance test:

```text
Only assigned HR actor can approve.
Approve marks ApprovalTask approved.
Approve creates TransactionPlan.
Reject terminates workflow.
Request more info sends workflow back to collecting_evidence.
```

## Phase 6: Execution

Build:

```text
execute
projection update
transaction ledger events
fake integration_outbox row
```

Acceptance test:

```text
Execute requires approved state.
Execute is idempotent.
Execute updates employee_projection.
Execute appends PersonLegalNameChanged.
Execute creates integration_outbox row.
Execute moves workflow to completed.
```

## Phase 7: Timeline

Build:

```http
GET /workflow-instances/:id/timeline
```

Acceptance test:

```text
Timeline reconstructs full business story from ledger events.
Timeline includes actor, state transition, approvals, document attachment, execution, projection update.
```

## Phase 8: Minimal UI

Build:

```text
Generated form renderer
Evidence upload screen
HR approval screen
Timeline screen
```

This order prevents frontend design from driving broken backend semantics.

---

# 19. The clean V0 architecture flow

```text
User/API
  -> POST /workflow-intents
  -> Workflow Orchestrator
  -> Permission Resolver
  -> WorkflowInstance created
  -> Ledger event appended
  -> Generated UI returned

User/API
  -> POST /workflow-instances/:id/transitions
  -> Transition Guard
  -> Payload Validator
  -> Go Preflight Block
  -> ChangeRequest / ProposedChanges
  -> Ledger events
  -> Next state + UI

User/API
  -> provide_evidence transition
  -> Document attached
  -> ApprovalTask created
  -> Ledger events
  -> HR available actions

HR/API
  -> approve transition
  -> ApprovalTask approved
  -> TransactionPlan created
  -> Ledger events

System/HR/API
  -> execute transition
  -> Lock workflow
  -> Append name-change event
  -> Update projection
  -> Create fake outbox row
  -> Ledger events
  -> Timeline available
```

That is the prototype.

---

# 20. The API should not expose these in V0

Do **not** expose:

```http
POST /change-requests/:id/preflight
POST /change-requests/:id/approve
POST /change-requests/:id/execute
POST /transaction-plans/:id/run
POST /employees/:id/legal-name
```

Those are internal operations.

You may expose read-only debugging endpoints later, but do not let clients mutate lifecycle objects directly.

---

# 21. Better endpoint naming

Your original endpoint names are good. I would make them slightly more explicit:

```http
POST /workflow-intents
GET  /workflow-instances/:id
GET  /workflow-instances/:id/available-actions
POST /workflow-instances/:id/transitions
GET  /workflow-instances/:id/timeline
```

Optional later:

```http
GET /workflow-instances/:id/current-interaction
```

But for V0, include `currentInteraction` in the `GET /workflow-instances/:id` response.

---

# 22. One subtle but important refinement

A transition is not always the same thing as a button.

Use this distinction:

```text
Action = what the UI offers the actor
Transition = the canonical state-machine command submitted to the engine
Handler = internal function that processes the transition
Block = deterministic step, often Go-executed
Ledger event = persisted fact of what happened
```

Example:

```text
UI action: "Submit evidence"
Transition: provide_evidence
Handler: provideLegalNameEvidence()
Go block: system.employee_data.legal_name.validate_evidence@1.0.0
Ledger event: EvidenceProvided
```

This keeps your UI flexible without making the state machine sloppy.

---

# 23. Final V0 implementation checklist

## Backend

```text
[x] tenants
[x] actors
[x] employee_projection seed
[x] workflow_definitions
[x] workflow_versions
[x] workflow_instances
[x] workflow_transition_attempts
[x] change_requests
[x] proposed_changes
[x] documents
[x] workflow_instance_documents
[x] approval_tasks
[x] transaction_plans
[x] ledger_events
[x] integration_outbox
```

## APIs

```text
[x] POST /workflow-intents
[x] GET /workflow-instances/:id
[x] GET /workflow-instances/:id/available-actions
[x] POST /workflow-instances/:id/transitions
[x] GET /workflow-instances/:id/timeline
[x] POST /documents
[x] GET /tasks?status=pending
```

## Transitions

```text
[x] submit_input
[x] provide_evidence
[x] approve
[x] reject
[x] request_more_info
[x] cancel
[x] execute
```

## Go blocks

```text
[x] legal_name.preflight
[x] legal_name.plan_transaction
```

## UI

```text
[x] employee legal-name form
[x] evidence upload screen
[x] HR approval card
[x] audit timeline
```

## Tests

```text
[x] employee can start own request
[x] employee cannot start another employee's request
[x] missing last name fails validation
[x] unchanged name fails validation
[x] missing evidence blocks approval
[x] HR can approve
[x] employee cannot approve
[x] rejected workflow cannot execute
[x] approved workflow can execute
[x] duplicate transition idempotency works
[x] wrong expectedVersion fails
[x] projection updates after execution
[x] timeline shows full causality
```

---

# 24. The refined V0 thesis

The version I would build is:

> **A workflow-native legal name change prototype where every user action enters through a workflow transition, every transition is guarded by permission and state, every business mutation becomes a ChangeRequest, every material change is written to the ledger, every read comes from projection, and every next UI is generated from workflow state.**

That proves the platform spine.

And the killer architectural rule is:

> **The public API never says “mutate HR data.” It says “advance this governed workflow if the actor, state, policy, and evidence allow it.”**

That is the right foundation.
