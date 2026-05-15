# HCM Next

HCM Next is being built as a workflow-native HCM platform. Public mutations advance governed workflow intents and transitions instead of directly mutating employee records.

V0 focuses on one API-only workflow:

```text
employee.legal_name.change
```

Workflow Admin v0 now adds the JSON-first admin path for workflow configs:

```text
workflow template/raw config
  -> draft
  -> validate / preview / simulate
  -> publish immutable DB version
  -> runtime instance pins version
  -> debug / repair / rollback
```

The demo proves:

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

## Architecture Summary

HCM Next uses:

```text
Node/TypeScript control plane
+ Go execution plane
+ Postgres ledger/projection store
```

Node owns APIs, permissions, workflow orchestration, persistence, timeline reads, and integration outbox writes. Go owns deterministic workflow blocks such as legal-name preflight validation and transaction plan construction. Postgres records business facts through ledger events and exposes read models through projections.

## Project Layout

See [project-layout.md](project-layout.md) for the current repository map.
See [workflow-schemas.md](workflow-schemas.md) for the evolving workflow schema and graph model.

Short version:

```text
src/api                       Node API control plane
src/platform/foundation       shared constants, errors, Result, context
src/platform/data-store       Postgres, repositories, migrations, seeds
src/platform/workflow-runtime reusable workflow runtime primitives
src/workflows/                workflow-owned orchestration and schemas
src/blocks/go                 Go deterministic block runner
```

## V0 Scope

Included:

- Employee starts their own legal-name change workflow.
- Workflow instance is durable and owns runtime state.
- Employee submits new legal name, effective date, and business reason.
- Go preflight validates deterministic rules.
- Workflow creates a change request and proposed changes.
- Employee creates a fake evidence document and provides it through a transition.
- HR approves or rejects.
- Execution updates `employee_projection`.
- Execution writes a fake HRIS request to `integration_outbox`.
- Timeline reconstructs the lifecycle from ledger events.

Excluded:

- Real payroll or HRIS integration.
- Real object storage.
- Multi-country policy.
- Multiple approvers.
- Visual workflow builder.
- UI.

## Public API Shape

Allowed mutation endpoints:

```text
POST /workflow-intents
POST /workflow-instances/:workflowInstanceId/transitions
POST /documents
```

Read endpoints for V0:

```text
GET /workflow-instances/:workflowInstanceId
GET /workflow-instances/:workflowInstanceId/available-actions
GET /workflow-instances/:workflowInstanceId/timeline
GET /documents/:documentId
GET /tasks?status=pending
```

Workflow Admin endpoints include:

```text
GET  /admin/workflow-families
POST /admin/workflow-drafts
POST /admin/workflow-drafts/:workflowDraftId/publish
POST /admin/workflows/validate-json
POST /admin/workflows/mermaid-preview
GET  /admin/workflow-blocks
POST /admin/workflows/input-mapping-preview
POST /admin/workflows/interaction-preview
POST /admin/workflows/permission-preview
POST /admin/workflows/simulate
POST /admin/workflows/diff
POST /admin/workflows/publish-guardrails
GET  /admin/workflow-instances/:workflowInstanceId/debug
POST /admin/workflow-instances/:workflowInstanceId/repair-actions
```

Timeline supports three views:

```text
GET /workflow-instances/:workflowInstanceId/timeline              # business view
GET /workflow-instances/:workflowInstanceId/timeline?view=audit   # raw ledger
GET /workflow-instances/:workflowInstanceId/timeline?view=debug   # runtime trace
```

Every transition request must include:

```json
{
  "transition": "submit_input",
  "idempotencyKey": "idem_123",
  "expectedVersion": 1,
  "payload": {}
}
```

## Current Repo Status

This checkout includes the V0 API demo implementation: a small Node HTTP API, a Go deterministic executor, shared TypeScript platform code, Postgres migrations/seeds, in-memory demo dependencies for fast local API tests, and E2E coverage for the legal-name workflow spine.

## Project Setup

Package manager: `npm`.

```bash
npm install
```

Start local Postgres:

```bash
docker compose up -d postgres
createdb hcm_next
# Optional organization-isolated demo database:
createdb hcm_next_harborcare
```

Run migrations and seeds:

```bash
npm run db:migrate
npm run db:seed
```

Start the Node API:

```bash
npm run dev
```

Start the Go executor in a second terminal:

```bash
cd src/blocks/go
go run ./cmd/executor
```

Expected local ports:

```text
Node API:    http://localhost:3000
Go executor: http://localhost:7001
Postgres:    localhost:5432
```

For the API-only demo and automated tests, the Node process uses seeded in-memory dependencies by default. The Postgres migration and seed scripts are present for the real ledger/projection store. They use `hcm_next` by default; set `DATABASE_URL` to a database such as `hcm_next_harborcare` when running an organization-isolated demo seed.

## Quality Commands

Expected scripts:

```bash
npm run lint
npm run format
npm run format:check
npm run typecheck
npm test
npm run test:go
npm run test:all
npm run build
```

Expected database scripts:

```bash
npm run db:migrate
npm run db:seed
```

## API Demo

Run the legal-name change demo from:

[docs/api-demo.md](api-demo.md)

That doc also includes the Workflow Admin JSON demo path for validation, Mermaid preview, DB publish, runtime pinning, debugger, and repair actions.

The happy path is:

1. `POST /workflow-intents` as `actor_employee_jane`.
2. `submit_input` transition as `actor_employee_jane`.
3. `POST /documents` as `actor_employee_jane`.
4. `provide_evidence` transition as `actor_employee_jane`.
5. `GET /tasks?status=pending` as `actor_hr_admin`.
6. `approve` transition as `actor_hr_admin`.
7. `execute` transition as `actor_system` or `actor_hr_admin`.
8. `GET /workflow-instances/:id/timeline` as HR.
9. Confirm `employee_projection` changed from Jane Doe to Jane Rivera.
10. Confirm `integration_outbox` has a fake HRIS `updateLegalName` request.

## Verification

Workstream 4 verification notes and E2E specs are in:

[docs/v0-implementation-notes.md](v0-implementation-notes.md)

Critical checks:

- Employee cannot start a workflow for another employee.
- Employee cannot approve their own request.
- Wrong `expectedVersion` returns `VERSION_CONFLICT`.
- Duplicate `idempotencyKey` replays the stored response.
- Rejected and canceled workflows cannot execute.
- Timeline is ledger-derived.
- Go blocks are side-effect free.
- External sync is represented as `integration_outbox`.

## Documentation Map

- [plan.md](plan.md)
- [V0TODOS.md](V0TODOS.md)
- [TODOS.md](TODOS.md)
- [TODOS2.md](TODOS2.md)
- [docs/code-style.md](code-style.md)
- [docs/core-platform-architecture.md](core-platform-architecture.md)
- [docs/data-model-storage-strategy.md](data-model-storage-strategy.md)
- [docs/dynamic-ui-branding-strategy.md](dynamic-ui-branding-strategy.md)
- [docs/api-demo.md](api-demo.md)
- [docs/v0-implementation-notes.md](v0-implementation-notes.md)

## Troubleshooting

`npm install` fails:

Confirm Node is at least `20.11.0` and npm is at least `10.0.0`.

API returns `PERMISSION_DENIED`:

Verify `x-demo-actor-id` is one of `actor_employee_jane`, `actor_hr_admin`, or `actor_system`, and that the actor is valid for the current workflow state.

Transition returns `VERSION_CONFLICT`:

Fetch `GET /workflow-instances/:id` and retry with the latest `workflowInstance.version`.

Transition returns `IDEMPOTENCY_CONFLICT`:

The same idempotency key was reused for a different request body. Use a new key for a new action.

Timeline is missing events:

Check that transition handlers append ledger events for every material business fact and that the timeline endpoint reads `ledger_events`.

Projection does not show Jane Rivera:

Confirm the workflow reached `executed`, then check the execution transition appended `PersonLegalNameChanged` and `EmployeeProjectionUpdated`.

Outbox row is missing:

Confirm the Go transaction planning block returned a fake HRIS external call spec and that Node persisted it to `integration_outbox` during execution.
