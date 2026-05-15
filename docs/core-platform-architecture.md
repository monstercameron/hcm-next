# Core Platform Architecture

## 1. Decision

HCM Next uses a split architecture:

```text
Node/TypeScript control plane
+ Go execution plane
+ Postgres ledger/projection store
```

Principle:

> Node decides what should happen. Go executes deterministic or performance-sensitive steps. Postgres records what happened.

Second principle:

> Node owns external communication. Go owns block computation.

This keeps the product fast to build while preserving a path to high-performance, deterministic workflow execution.

## 2. Why This Split

The platform has two different kinds of work.

### 2.1 Platform Control Work

This work is latency-sensitive, integration-heavy, product-facing, and JSON/API-heavy.

Best fit:

> Node/TypeScript

Examples:

- Web app
- Public APIs
- Internal APIs
- Webhooks
- Auth/session handling
- Tenant management
- Change request CRUD
- Approval task APIs
- Workflow config APIs
- AI review orchestration
- Integration orchestration
- Outbound API calls
- Projection read APIs
- Admin/config screens
- Developer/admin tooling

### 2.2 Workflow Execution Work

This work benefits from compilation, deterministic contracts, performance, isolation, and small pure-ish functions.

Best fit:

> Go

Examples:

- Workflow step execution
- Built-in compute blocks
- Validation blocks
- Calculation blocks
- Policy evaluation blocks
- Risk scoring blocks
- Mapping/normalization blocks
- Simulation reducers
- Projection reducers, if needed
- Customer custom block runtime
- WASM/native custom block execution

## 3. Runtime Ownership

## 3.1 Node/TypeScript Owns The Control Plane

Node owns:

- HTTP APIs
- API authentication
- Session and user context
- Tenant routing
- Request validation
- Permission orchestration
- Change request lifecycle
- Approval lifecycle
- Workflow definition management
- Workflow graph selection
- Transaction plan creation and persistence
- AI review orchestration
- Integration connection management
- Inbound webhooks
- Outbound API execution
- Integration retries and rate limits
- Projection read APIs
- UI-oriented query shaping
- Audit timeline presentation

Node is the main product platform.

### 3.2 Go Owns The Execution Plane

Go owns:

- Pure/deterministic block execution.
- Performance-sensitive transformation.
- Policy/rule evaluation when compiled blocks are useful.
- Calculation-heavy operations.
- Simulation transforms.
- Customer custom Go block execution.
- WASM/native block runtime, later.
- Optional projection reduction if TypeScript workers become too slow.

Go should not own product UX, external API orchestration, tenant/account management, or broad business CRUD.

## 4. Boundary Rule

Go blocks should not freely call external systems in v1.

Instead:

```text
Go block produces an external call request spec.
Node integration layer executes the external call.
Postgres ledger records the request and result.
```

Why:

- Centralized secrets handling
- Centralized auth
- Centralized rate limiting
- Centralized retries
- Centralized connector behavior
- Centralized audit
- Safer customer custom blocks
- Easier integration testing
- Better failure handling

Go can request a side effect, but Node performs the side effect.

## 5. High-Level System Flow

Example: compensation change.

```text
1. User submits change request in Node web/API.
2. Node validates request shape and actor context.
3. Node loads permission-filtered employee projection from Postgres.
4. Node creates ChangeRequest and ProposedChanges.
5. Node appends ChangeRequestCreated ledger event.
6. Node asks Go executor to run validation/simulation blocks.
7. Go executes deterministic blocks and returns results/events.
8. Node stores preflight/simulation result and appends ledger events.
9. Node creates approval tasks.
10. Approvers act through Node APIs/UI.
11. Node creates final TransactionPlan.
12. Go executes final deterministic transaction planning blocks.
13. Node appends authoritative ledger events.
14. Node updates projections in the same DB transaction where needed.
15. Node integration worker executes external writeback/export.
16. Node appends integration success/failure events.
17. Projection workers update read models.
18. Node shows AI/audit/reconciliation timeline.
```

## 6. Main Components

### 6.1 Web App

Runtime:

> Node/TypeScript

Responsibilities:

- Manager request form
- HRBP/compensation approval view
- Transaction simulation view
- Audit/review timeline
- Admin configuration screens
- Integration health screens
- Workflow status screens

Recommended framework:

> Next.js + React + TypeScript

### 6.2 API Server

Runtime:

> Node/TypeScript

Responsibilities:

- REST or RPC APIs for the web app
- Public API surface, later
- Auth middleware
- Tenant context resolution
- Request validation
- Permission checks
- Change request commands
- Approval commands
- Projection queries
- Workflow config commands
- Integration config commands

Recommended validation:

> Zod or similar schema library

### 6.3 Workflow Orchestrator

Runtime:

> Node/TypeScript

Responsibilities:

- Select workflow version.
- Load workflow graph.
- Determine next executable steps.
- Create approval tasks.
- Decide when to call Go executor.
- Decide when to enqueue integration outbox work.
- Persist workflow state.
- Append workflow ledger events.

The orchestrator coordinates. It should not bury complex deterministic business logic inside TypeScript handlers when that logic belongs in blocks.

### 6.4 Go Executor

Runtime:

> Go

Responsibilities:

- Execute block calls.
- Validate typed input/output contracts.
- Run deterministic calculations and transformations.
- Return structured outputs.
- Return proposed ledger events.
- Return external call specs, not execute them directly in v1.
- Return logs and metrics.

MVP interface:

> HTTP is acceptable for speed.

Future interface:

> gRPC or queue-based worker protocol.

### 6.5 Integration Layer

Runtime:

> Node/TypeScript

Responsibilities:

- Inbound webhooks
- Outbound API calls
- CSV/SFTP import/export
- REST connector execution
- Secret lookup
- OAuth token refresh
- Rate limiting
- Retry policy
- Circuit breakers
- Integration outbox processing
- Response normalization
- Reconciliation events

The integration layer is latency-sensitive orchestration, not raw throughput compute. Node is a good fit.

### 6.6 AI Review Service

Runtime:

> Node/TypeScript

Responsibilities:

- Assemble permission-filtered context.
- Call AI provider.
- Store AI review output.
- Append AI review ledger events.
- Track model/provider metadata.
- Track token/cost metadata.
- Enforce AI visibility scopes.

AI should read only what the actor and workflow policy allow.

### 6.7 Projection Workers

Initial runtime:

> Node/TypeScript

Possible future runtime:

> Go

Responsibilities:

- Consume ledger events.
- Build or update projections.
- Rebuild projections by tenant, employee, or change request.
- Detect projection drift.
- Update `employee_projection`, `change_request_projection`, `position_projection`, `org_projection`, and `compensation_projection`.

Start in Node for speed. Move hot reducers to Go only when needed.

### 6.8 Postgres

Responsibilities:

- Immutable ledger events.
- Change requests.
- Proposed changes.
- Transaction plans.
- Approval tasks.
- Workflow definitions.
- Permission policies.
- Integration config.
- Outbox.
- Materialized projections.

Postgres is the system of truth for v1.

## 7. Data Flow Contracts

### 7.1 Node To Go Execution Request

Example:

```json
{
  "tenantId": "tenant_123",
  "environmentId": "env_prod",
  "changeRequestId": "cr_123",
  "workflowVersionId": "wfver_2",
  "block": {
    "name": "system.compensation.validate-range",
    "version": "1.0.0"
  },
  "input": {
    "workerId": "emp_123",
    "currentCompensation": {
      "amount": 120000,
      "currency": "USD",
      "payFrequency": "annual"
    },
    "proposedCompensation": {
      "amount": 140000,
      "currency": "USD",
      "payFrequency": "annual"
    },
    "payBand": {
      "min": 110000,
      "mid": 135000,
      "max": 160000
    }
  },
  "context": {
    "actorId": "actor_123",
    "effectiveAt": "2026-06-01T00:00:00Z",
    "permissions": {
      "visibleFields": [
        "worker.job",
        "worker.compensation.amount",
        "worker.compensation.currency"
      ]
    },
    "correlationId": "corr_abc",
    "idempotencyKey": "idem_abc"
  }
}
```

### 7.2 Go Execution Response

Example:

```json
{
  "status": "succeeded",
  "output": {
    "valid": true,
    "warnings": ["Increase is above department average but within pay band."],
    "riskLevel": "medium"
  },
  "proposedEvents": [
    {
      "eventType": "CompensationRangeValidated",
      "subjectType": "change_request",
      "subjectId": "cr_123",
      "payload": {
        "valid": true,
        "riskLevel": "medium"
      }
    }
  ],
  "externalCallRequests": [],
  "logs": [
    {
      "level": "info",
      "message": "Compensation validation passed."
    }
  ],
  "metrics": {
    "durationMs": 12
  }
}
```

### 7.3 External Call Request Spec

Go blocks can produce this, but Node executes it.

```json
{
  "connectionId": "ukg_prod",
  "operation": "updateEmployeeJob",
  "idempotencyKey": "ukg_job_update_cr_123",
  "payload": {
    "workerId": "emp_123",
    "jobCode": "SWE-L5",
    "effectiveDate": "2026-06-01"
  },
  "reconciliation": {
    "expectedExternalObject": "employee_job",
    "expectedFields": {
      "jobCode": "SWE-L5"
    }
  }
}
```

Node turns this into an outbox row, executes it through the integration layer, and appends success/failure ledger events.

## 8. Side-Effect Policy

Block classes:

### 8.1 Pure Blocks

Pure blocks:

- Mapping
- Validation
- Calculation
- Eligibility
- Policy evaluation
- Risk scoring
- Data transformation
- Simulation reducers

Rules:

- No network.
- No DB writes.
- No secrets.
- Deterministic for same input.
- Replayable.

### 8.2 Effect-Request Blocks

Effect-request blocks produce a spec for a side effect.

Examples:

- Request external API write.
- Request notification.
- Request document generation.
- Request e-signature packet.

Rules:

- Go returns the request.
- Node validates and executes the request.
- Ledger records request and result.

### 8.3 Effect Blocks

Direct effect blocks may exist later for trusted system code, but they should be avoided for customer custom blocks.

Examples:

- High-throughput internal projection reducers.
- Trusted first-party sync workers.

Rules:

- Must use capability manifest.
- Must be audited.
- Must be idempotent.
- Must have timeout and memory limits.

## 9. Deployment Shape

MVP services:

```text
web-api            Node/TypeScript
go-executor        Go
postgres           Postgres
worker             Node/TypeScript background worker
```

Optional soon:

```text
redis              queue/cache, if needed
object-storage     documents/attachments, later
search             OpenSearch, later
```

Suggested local ports:

```text
web-api:      3000
go-executor: 7001
postgres:    5432
```

## 10. Monorepo Shape

Recommended repo structure:

```text
src/
  api/
    Node API control plane
  blocks/go/
    Go block executor
  platform/
    data-store/
      migrations, SQL helpers, seed data
    foundation/
      shared TypeScript domain types and schemas
    workflow-runtime/
      workflow graph schemas and examples
  workflows/
    legal-name-change/
      workflow-specific orchestration and block contracts

docs/
  plan and architecture docs
```

Keep it simple at first. Do not split into many services until boundaries are proven.

## 11. Workflow Execution Sequence

### 11.1 Preflight

```text
Node receives draft change request.
Node loads current projection.
Node checks actor permission.
Node calls Go validation/calculation blocks.
Go returns validation output and proposed events.
Node stores preflight_result.
Node appends ledger events.
Node updates change_request_projection.
```

### 11.2 Approval

```text
Node reads workflow config.
Node creates approval tasks.
Approvers act through Node API/UI.
Node appends approval ledger events.
Node updates approval/change projections.
```

### 11.3 Simulation

```text
Node loads current projection.
Node builds proposed event list.
Go runs simulation reducer.
Go returns proposed projection and diff.
Node stores simulation result.
Node appends simulation ledger event.
```

### 11.4 Execution

```text
Node locks change request for execution.
Node verifies approval and idempotency.
Go runs final deterministic blocks.
Node appends authoritative ledger events.
Node updates critical projections.
Node creates integration_outbox rows.
Node marks transaction as executing/executed internally.
```

### 11.5 Reconciliation

```text
Node worker processes integration_outbox.
Node calls external systems.
Node records success/failure response.
Node appends integration ledger events.
Node updates reconciliation result.
Node creates repair task if needed.
```

## 12. Failure Boundaries

Node owns:

- API validation failures.
- Permission failures.
- Auth failures.
- Integration failures.
- Outbox retries.
- AI provider failures.
- User-facing error states.

Go owns:

- Block input validation failures.
- Block execution failures.
- Deterministic calculation failures.
- Simulation failures.
- Custom block runtime failures.

Postgres owns:

- Transaction atomicity.
- Ledger persistence.
- Projection persistence.
- Idempotency uniqueness.

Every failure that affects business state must produce a ledger event.

## 13. Security Boundaries

### 13.1 Node Security Responsibilities

- Authenticate users.
- Resolve tenant.
- Evaluate permissions.
- Filter projection fields.
- Enforce AI visibility scopes.
- Resolve secrets for integrations.
- Execute external calls.
- Write audit events.

### 13.2 Go Security Responsibilities

- Enforce block input/output schemas.
- Execute only requested block version.
- Enforce runtime limits.
- Avoid undeclared side effects.
- Return structured logs without secrets.
- For custom blocks, enforce capability manifest.

### 13.3 Postgres Security Responsibilities

- Persist tenant-scoped data.
- Enforce basic relational integrity.
- Enforce idempotency constraints.
- Store append-only ledger events.

## 14. Technology Choices

Recommended starting stack:

```text
Node runtime:       Node.js with TypeScript
Web framework:      Next.js
Validation:         Zod
Database:           Postgres
DB access:          Drizzle or Prisma
Go runtime:         Go
Executor protocol:  HTTP first, gRPC later if needed
Queue:              Postgres outbox first, Redis/BullMQ later if needed
AI SDK:             OpenAI SDK or provider abstraction
```

DB access note:

- Drizzle is closer to SQL and migrations.
- Prisma is faster for CRUD-heavy app development.
- For this ledger/projection design, Drizzle or raw SQL migrations may be the better fit.

## 15. Near-Term Build Order

1. Scaffold monorepo.
2. Add Postgres and migrations.
3. Build Node API shell.
4. Build core tables.
5. Build change request CRUD.
6. Build ledger append helper.
7. Build projection read/write helpers.
8. Build Go executor service with one validation block.
9. Connect Node to Go executor.
10. Build promotion/compensation preflight flow.
11. Add approval tasks.
12. Add transaction simulation.
13. Add integration outbox stub.
14. Add AI change review.

## 16. Open Architecture Questions

- Should the first Node API be Next.js route handlers or a separate Fastify service?
- Should the first DB access layer be Drizzle, Prisma, or raw SQL migrations with a thin query layer?
- Should the Go executor be HTTP only for v1 or use gRPC from the start?
- Should projection updates happen synchronously for v1 or through an outbox/worker from day one?
- Should the Go executor return proposed ledger events directly or only typed outputs that Node converts to events?
- How much permission evaluation lives in TypeScript versus Postgres policies?

Current recommendation:

- Next.js route handlers for speed.
- Drizzle or raw SQL migrations for database control.
- HTTP Go executor for v1.
- Synchronous projection updates for critical change request state.
- Outbox/worker for external integrations and non-critical projections.
- Node converts Go outputs into persisted ledger events.
