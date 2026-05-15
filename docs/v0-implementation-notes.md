# V0 Implementation Notes

These notes are owned by Workstream 4 and track demo verification, E2E coverage, and cross-stream contract checks for the API-only V0.

## Current Implementation State

This working tree now contains the V0 workflow API routes, database migration/seed runners, deterministic Go executor blocks, and runnable Vitest coverage for the legal-name workflow. The E2E test is implemented in `src/tests/e2e/legal-name-change.e2e.spec.ts` and runs against the workflow service with seeded demo dependencies.

## Demo Contract

The demo must prove this sequence:

```text
WorkflowIntent
  -> WorkflowInstance
  -> workflow transitions
  -> ChangeRequest
  -> ProposedChanges
  -> ApprovalTasks
  -> TransactionPlan
  -> LedgerEvents
  -> Projections
  -> IntegrationOutbox
  -> Timeline
```

Allowed public mutation endpoints:

```text
POST /workflow-intents
POST /workflow-instances/:workflowInstanceId/transitions
POST /documents
```

Read endpoints needed for the demo:

```text
GET /workflow-instances/:workflowInstanceId
GET /workflow-instances/:workflowInstanceId/available-actions
GET /workflow-instances/:workflowInstanceId/timeline
GET /documents/:documentId
GET /tasks?status=pending
```

Optional read-only demo endpoint:

```text
GET /employees/:workerId
```

The employee read endpoint is exposed as a read-only demo projection route through a Next rewrite to `/api/demo/employee-projections/:employeeId`.

## Stable Demo Inputs

Seed aliases:

| Alias                 | Purpose             |
| --------------------- | ------------------- |
| `tenant_demo`         | Tenant              |
| `env_demo`            | Environment         |
| `actor_employee_jane` | Requesting employee |
| `actor_hr_admin`      | HR approver         |
| `actor_system`        | System actor        |
| `emp_123`             | Worker subject      |
| `person_123`          | Person record       |

Happy-path request:

```json
{
  "newLegalName": {
    "first": "Jane",
    "middle": null,
    "last": "Rivera"
  },
  "effectiveAt": "2026-06-01",
  "businessReason": "legal_name_change"
}
```

Required transition envelope:

```json
{
  "transition": "submit_input",
  "idempotencyKey": "idem_demo_submit_input_001",
  "expectedVersion": 1,
  "payload": {}
}
```

## E2E Test Specs

### 1. Happy Path Executes Legal Name Change

Purpose: prove the full platform spine.

Steps:

1. Seed demo tenant, environment, actors, workflow definition, and employee projection.
2. Start `employee.legal_name.change` as `actor_employee_jane` for `emp_123`.
3. Assert workflow state is `collecting_input`, status is `active`, version is `1`, and no change request exists.
4. Submit legal-name input with `expectedVersion: 1`.
5. Assert workflow state is `collecting_evidence`, version is `2`, and a change request exists.
6. Assert proposed changes include `person.legalName.first`, `person.legalName.middle`, and `person.legalName.last`.
7. Create a fake document for the workflow instance.
8. Provide evidence with `expectedVersion: 2`.
9. Assert workflow state is `waiting_approval`, status is `waiting`, and an approval task exists.
10. Fetch pending tasks as `actor_hr_admin`.
11. Approve with `expectedVersion: 3`.
12. Assert workflow state is `approved`, version is `4`, and a transaction plan exists.
13. Execute as `actor_system` or `actor_hr_admin` with `expectedVersion: 4`.
14. Assert workflow state is `executed`, status is `completed`, and version is `5`.
15. Assert `employee_projection.document.person.legalName.last` is `Rivera`.
16. Assert `employee_projection.document.person.displayName` is `Jane Rivera`.
17. Assert `integration_outbox` has one `fake_hris` `updateLegalName` row.
18. Assert timeline includes all required business events in ledger order.

Required ledger events:

```text
WorkflowIntentStarted
WorkflowTransitionSubmitted
ChangeRequestCreated
ProposedChangeCreated
NameChangePreflighted
EvidenceRequested
DocumentCreated
EvidenceProvided
ApprovalTaskCreated
ChangeRequestSubmitted
ApprovalGranted
ChangeRequestApproved
TransactionPlanCreated
TransactionExecutionStarted
PersonLegalNameChanged
EmployeeProjectionUpdated
ExternalWriteRequested
TransactionExecutionCompleted
WorkflowCompleted
WorkflowStateChanged
```

### 2. Employee Cannot Start Another Employee Request

Steps:

1. Start `employee.legal_name.change` as `actor_employee_jane`.
2. Use subject worker ID `emp_999`.
3. Assert HTTP `403`.
4. Assert error code is `PERMISSION_DENIED`.
5. Assert no workflow instance or change request is created.

### 3. Missing Last Name Fails Validation

Steps:

1. Start a workflow as `actor_employee_jane`.
2. Submit input with `newLegalName.last` missing or blank.
3. Assert HTTP `400`.
4. Assert error code is `VALIDATION_FAILED`.
5. Assert workflow remains in `collecting_input`.
6. Assert no change request is created.

### 4. Unchanged Name Fails Validation

Steps:

1. Start a workflow as `actor_employee_jane`.
2. Submit input with Jane Doe as both current and proposed name.
3. Assert HTTP `400`.
4. Assert error code is `VALIDATION_FAILED`.
5. Assert preflight returns an unchanged-name error.
6. Assert workflow remains in `collecting_input`.

### 5. Employee Cannot Approve Own Request

Steps:

1. Move a workflow to `waiting_approval`.
2. Submit `approve` as `actor_employee_jane`.
3. Assert HTTP `403`.
4. Assert error code is `PERMISSION_DENIED`.
5. Assert approval task remains `pending`.
6. Assert workflow remains in `waiting_approval`.

### 6. Non-HR Actor Has No Approve Action

Steps:

1. Move a workflow to `waiting_approval`.
2. Fetch available actions as `actor_employee_jane`.
3. Assert the response excludes `approve`, `reject`, and `request_more_info`.
4. Assert employee-visible actions are limited to allowed requester actions such as `cancel`.

### 7. Reject Path Terminates Workflow

Steps:

1. Move a workflow to `waiting_approval`.
2. Submit `reject` as `actor_hr_admin` with an approval task ID and reason.
3. Assert workflow state is `rejected`.
4. Assert workflow status is terminal.
5. Assert change request status is `rejected`.
6. Assert rejected workflow cannot execute.
7. Assert ledger includes `ApprovalRejected`, `ChangeRequestRejected`, `WorkflowStateChanged`, and `WorkflowCompleted`.

### 8. Request More Info Returns To Evidence

Steps:

1. Move a workflow to `waiting_approval`.
2. Submit `request_more_info` as `actor_hr_admin` with an approval task ID and comment.
3. Assert workflow state is `collecting_evidence`.
4. Assert change request status is `needs_data`.
5. Assert ledger includes `MoreInformationRequested` and `WorkflowStateChanged`.
6. Assert employee can provide a new document through `provide_evidence`.

### 9. Canceled Workflow Cannot Execute

Steps:

1. Start a workflow.
2. Submit `cancel` before approval.
3. Assert workflow state is `canceled`.
4. Submit `execute`.
5. Assert HTTP `409`.
6. Assert error code is `INVALID_WORKFLOW_TRANSITION`.
7. Assert employee projection is unchanged.

### 10. Rejected Workflow Cannot Execute

Steps:

1. Reject a workflow from `waiting_approval`.
2. Submit `execute`.
3. Assert HTTP `409`.
4. Assert error code is `INVALID_WORKFLOW_TRANSITION`.
5. Assert no transaction execution events are appended.

### 11. Duplicate Idempotency Key Returns Same Response

Steps:

1. Submit a valid transition with idempotency key `idem_replay_001`.
2. Repeat the exact same transition request.
3. Assert HTTP `200`.
4. Assert response matches the first response.
5. Assert no duplicate ledger events, approval tasks, transaction plans, projection updates, or outbox rows are created.
6. Repeat with the same key but different payload.
7. Assert HTTP `409` and error code `IDEMPOTENCY_CONFLICT`.

### 12. Wrong Expected Version Fails

Steps:

1. Move a workflow to `waiting_approval` at version `3`.
2. Submit `approve` with `expectedVersion: 2`.
3. Assert HTTP `409`.
4. Assert error code is `VERSION_CONFLICT`.
5. Assert response includes actual version.
6. Assert no approval state changes are persisted.

### 13. Timeline Views Show The Right Level Of Detail

Steps:

1. Complete a happy-path workflow.
2. Fetch default timeline as HR.
3. Assert `view` is `business`.
4. Assert `totalLedgerEventCount` reports the raw ledger count.
5. Assert business events exclude `WorkflowTransitionSubmitted` and `WorkflowStateChanged`.
6. Assert business entries include actor, event type, occurred time, summary, and safe payload excerpt.
7. Fetch `?view=audit`.
8. Assert audit events include the full raw ledger stream in ledger order.
9. Fetch `?view=debug`.
10. Assert debug events include transition submissions, state changes, and transaction execution boundaries.
11. Assert evidence details are permission-filtered for non-HR actors.
12. Assert no timeline data is read from logs.

### 14. Forced Executor Failure Moves Workflow To Failed

Implementation fixture options:

- Configure the Node executor client with a test-only fake response for `system.employee_data.legal_name.plan_transaction@1.0.0`.
- Or run the Go executor with a test-only block input that returns an execution error.

Steps:

1. Move a workflow to `approved`.
2. Force the transaction planning or execution block to fail.
3. Submit `execute`.
4. Assert workflow state is `failed`.
5. Assert `GET /workflow-instances/:id` returns state `failed`.
6. Assert failed transition attempt is recorded.
7. Assert ledger includes `WorkflowFailed`.
8. Assert employee projection is unchanged.
9. Assert no successful outbox row is created.

## Contract Consistency Checklist

- [x] Public mutations are limited to `POST /workflow-intents`, `POST /workflow-instances/:id/transitions`, and `POST /documents`.
- [x] Every transition request requires `transition`, `idempotencyKey`, `expectedVersion`, and `payload`.
- [x] Every transition checks actor permissions before mutating business state.
- [x] Every transition checks state-machine legality before calling handlers.
- [x] Every material mutation appends at least one ledger event.
- [x] Timeline reads from `ledger_events`, not logs.
- [x] Go executor blocks do not call external APIs.
- [x] Go executor external effects are returned as request specs.
- [x] Node writes fake external sync requests to `integration_outbox`.
- [x] Application and business code use `Result` for expected failures.
- [x] Broad `try/catch` only appears in approved wrappers or process boundaries.
- [x] Workflow states and transitions use shared typed constants.
- [x] Errors use shared error codes and factories.
- [x] Read endpoints permission-filter sensitive evidence and employee data.

## Demo Readiness Checks

Current status in this checkout:

| Check                                      | Status | Notes                                               |
| ------------------------------------------ | ------ | --------------------------------------------------- |
| Fresh dependency install                   | Passed | `npm install` completed.                            |
| Migration runner available                 | Passed | `npm run db:migrate` reaches the TypeScript runner. |
| Seed runner available                      | Passed | `npm run db:seed` reaches the TypeScript runner.    |
| Node API builds                            | Passed | `npm run build` completed.                          |
| Go executor tests                          | Passed | `npm run test:go` completed.                        |
| Happy path demo completes                  | Passed | Covered by Vitest service-level E2E.                |
| Timeline readable                          | Passed | E2E asserts required ledger event types.            |
| Projection changes Jane Doe to Jane Rivera | Passed | E2E asserts projection update.                      |
| Integration outbox row exists              | Passed | E2E asserts one fake HRIS outbox row.               |
| Lint passes                                | Passed | `npm run lint` completed.                           |
| Typecheck passes                           | Passed | `npm run typecheck` completed.                      |
| Tests pass                                 | Passed | `npm test` and `npm run test:go` completed.         |

Local Postgres migration/seed execution still requires a running database named `hcm_next`, as documented in the README.

## Latest Local Verification

Commands attempted in this pass:

```bash
npm test
npm run typecheck
cd src/blocks/go && go test ./...
```

Results:

- `npm test` passed.
- `npm run typecheck` passed.
- `npm run lint` passed.
- `npm run build` passed.
- `npm run test:go` passed.

## Suggested Test Harness Shape

Preferred E2E harness once the Node app exists:

```text
src/tests/e2e/legal-name-change.e2e.spec.ts
src/tests/e2e/fixtures/demo-seed.ts
src/tests/e2e/fixtures/http-client.ts
src/tests/e2e/fixtures/db-assertions.ts
```

Recommended test runner:

```text
vitest or jest for API-level tests
testcontainers or a local docker compose Postgres for database-backed E2E
```

Required fixture behavior:

- Reset database state before each test file or test case.
- Run migrations and seeds through the same commands documented for local development.
- Use stable demo aliases for actors and worker IDs.
- Make HTTP calls against the actual API server, not route functions directly, for E2E coverage.
- Assert database side effects through repository/query helpers when available.
- Avoid direct SQL in application code; direct SQL in test assertions is acceptable if no query helper exists yet.
