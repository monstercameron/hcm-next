# Code Style And Engineering Standards

## 1. Purpose

This document defines the code style for HCM Next.

The goal is enterprise-ready code that is explicit, auditable, testable, and hard to misuse.

Core principle:

> Business logic should read as a sequence of named, checked steps. Side effects should be explicit. Complex rules should be isolated, named, documented, and tested.

## 2. Platform Style

HCM Next uses:

```text
Node/TypeScript control plane
+ Go execution plane
+ Postgres ledger/projection store
```

Rules:

- Node/TypeScript owns the product platform, APIs, workflow orchestration, AI orchestration, external communication, and integration workers.
- Go owns deterministic or performance-sensitive workflow blocks.
- Postgres records what happened through ledger events and materialized projections.

Short version:

> Node decides what should happen. Go executes deterministic or performance-sensitive steps. Postgres records what happened.

## 3. Workflow-Native API Style

The public API must be workflow-native.

Allowed public mutation pattern:

```text
POST /workflow-intents
POST /workflow-instances/:id/transitions
```

Do not expose direct lifecycle mutation APIs such as:

```text
POST /employees/:id/legal-name
POST /change-requests/:id/approve
POST /change-requests/:id/execute
POST /transaction-plans/:id/run
```

Those are internal workflow operations.

Every workflow transition must include:

```json
{
  "transition": "submit_input",
  "idempotencyKey": "idem_123",
  "expectedVersion": 3,
  "payload": {}
}
```

## 4. Result-Based Error Handling

Application code should use `Result`, not exception bubbling, for expected failures.

Use this shape:

```ts
export type Result<T, E = AppError> = { ok: true; value: T } | { ok: false; error: E };

export function ok<T>(value: T): Result<T, never> {
  return { ok: true, value };
}

export function err<E>(error: E): Result<never, E> {
  return { ok: false, error };
}
```

Every `Result` must be checked before using the value:

```ts
const workflowResult = await workflowRepository.findByIntent(input.intent);

if (!workflowResult.ok) {
  return workflowResult;
}

const workflowDefinition = workflowResult.value;
```

## 5. Try/Catch Policy

Do not use broad `try/catch` in business logic.

Allowed `try/catch` locations:

- HTTP route handler wrapper
- DB client/repository wrapper
- integration client wrapper
- AI provider wrapper
- Go executor client wrapper
- file/document storage wrapper
- process/job worker boundary

Use wrappers to convert throwing APIs into `Result`.

```ts
export async function fromPromise<T>(
  operation: () => Promise<T>,
  mapError: (error: unknown) => AppError,
): Promise<Result<T>> {
  try {
    const value = await operation();
    return ok(value);
  } catch (error) {
    return err(mapError(error));
  }
}

export function fromThrowable<T>(
  operation: () => T,
  mapError: (error: unknown) => AppError,
): Result<T> {
  try {
    return ok(operation());
  } catch (error) {
    return err(mapError(error));
  }
}
```

Pure workflow logic should return `Result` directly and should not call `fromPromise` internally unless it is crossing a system boundary.

## 6. Error Taxonomy

Use shared error codes and factories.

Error categories:

- validation errors
- permission errors
- conflict/version errors
- not found errors
- workflow state errors
- idempotency errors
- database errors
- integration errors
- AI provider errors
- Go executor errors
- system errors

Example:

```ts
export const ERROR_CODES = {
  VALIDATION_FAILED: "VALIDATION_FAILED",
  PERMISSION_DENIED: "PERMISSION_DENIED",
  WORKFLOW_NOT_FOUND: "WORKFLOW_NOT_FOUND",
  INVALID_WORKFLOW_TRANSITION: "INVALID_WORKFLOW_TRANSITION",
  VERSION_CONFLICT: "VERSION_CONFLICT",
  IDEMPOTENCY_CONFLICT: "IDEMPOTENCY_CONFLICT",
  DATABASE_ERROR: "DATABASE_ERROR",
  INTEGRATION_ERROR: "INTEGRATION_ERROR",
  GO_EXECUTOR_ERROR: "GO_EXECUTOR_ERROR",
} as const;
```

Error shape:

```ts
export type AppError = {
  code: ErrorCode;
  message: string;
  safeMessage: string;
  details?: Record<string, unknown>;
  cause?: unknown;
};
```

Use factories:

```ts
export function permissionDeniedError(details?: Record<string, unknown>): AppError {
  return {
    code: ERROR_CODES.PERMISSION_DENIED,
    message: "Permission denied.",
    safeMessage: "You do not have permission to perform this action.",
    details,
  };
}
```

Rules:

- Do not construct ad hoc errors inline.
- Do not leak secrets or restricted HR data in public errors.
- Public errors need `safeMessage`.
- Internal logs may include technical details if they are safe.

## 7. Shared Constants

No magic strings for:

- workflow states
- workflow statuses
- transition names
- change request statuses
- approval statuses
- permission keys
- ledger event types
- actor types
- integration statuses
- route names
- config defaults

Example:

```ts
export const WORKFLOW_STATES = {
  COLLECTING_INPUT: "collecting_input",
  COLLECTING_EVIDENCE: "collecting_evidence",
  WAITING_APPROVAL: "waiting_approval",
  APPROVED: "approved",
  EXECUTING: "executing",
  EXECUTED: "executed",
  REJECTED: "rejected",
  CANCELED: "canceled",
  FAILED: "failed",
} as const;

export type WorkflowState = (typeof WORKFLOW_STATES)[keyof typeof WORKFLOW_STATES];
```

Recommended structure:

```text
src/platform/foundation/
  constants/
    workflow.constants.ts
    change-request.constants.ts
    ledger-event.constants.ts
    permission.constants.ts
    integration.constants.ts
    http.constants.ts
    time.constants.ts
    index.ts

  errors/
    app-error.ts
    error-codes.ts
    error-factories.ts
    error-mappers.ts
    index.ts

  result/
    result.ts
    from-promise.ts
    index.ts
```

## 8. Variable And Naming Style

Prefer human-readable names.

Rules:

- Longer names are acceptable when they reduce ambiguity.
- Avoid clever abbreviations.
- Name booleans clearly: `isAllowed`, `hasEvidence`, `canApprove`.
- Name side-effect functions clearly: `appendLedgerEvent`, `saveWorkflowInstance`, `enqueueIntegrationOutbox`.
- Declare important function-level dependencies, context, and return-shaping variables near the top.
- Declare short-lived variables near first use when that keeps scope tight.

Do not force every local variable to the top if it makes scope and ownership less clear.

## 9. Comments And JSDoc

Use comments sparingly.

Add comments around:

- complex business rules
- permission edge cases
- workflow transition decisions
- effective-date logic
- idempotency and concurrency handling
- non-obvious performance decisions

Avoid comments that repeat the code.

Use JSDoc for:

- exported functions
- workflow handlers
- block contracts
- permission functions
- projection reducers
- repository methods
- error mappers
- integration clients

## 10. Pure Functions First

Prefer pure functions for:

- validation
- permission checks
- state transition resolution
- projection reducers
- diff generation
- field filtering
- mapping and normalization
- transaction plan construction
- AI context assembly before provider calls

Side effects should be isolated in dedicated modules.

Side effects include:

- DB writes
- external API calls
- AI calls
- file/document storage
- logging
- queue/outbox writes
- email/notification delivery

## 11. Command/Query Split

Separate writes from reads.

Commands:

- mutate state
- append ledger events
- create outbox rows
- update projections when needed

Queries:

- read projections
- read workflow state
- read ledger timelines
- must not mutate state

No hidden writes in query functions.

## 12. Transaction Boundaries

Every function that writes business state should make the transaction boundary obvious.

Good names:

```text
executeWorkflowTransitionInTransaction
appendLedgerEvent
updateWorkflowInstanceState
createChangeRequestWithProposedChanges
```

Avoid casual DB writes scattered through helper functions.

## 13. Idempotency And Version Checks

Any command endpoint must require an `idempotencyKey`.

Workflow transitions must also require `expectedVersion`.

This prevents:

- double-click corruption
- retry duplication
- stale UI actions
- concurrent execution races
- mobile refresh issues

## 14. Explicit Context Objects

Pass a single context object rather than many loose parameters.

```ts
export type RequestContext = {
  tenantId: string;
  actorId: string;
  roles: string[];
  requestId: string;
  correlationId: string;
};

export type WorkflowContext = RequestContext & {
  workflowInstanceId: string;
  changeRequestId?: string;
  permissionSnapshot: Record<string, unknown>;
};
```

Context should include tenant, actor, request, correlation, and permission information where relevant.

## 15. Repository And External Call Boundaries

Business logic should not contain raw SQL.

Rules:

- Repositories/query modules do SQL.
- Services/workflow handlers call repositories.
- Workflow handlers do not call external systems directly.
- Workflow handlers create outbox rows.
- Integration workers execute external systems.

## 16. Domain Events And Ledger

If it matters to the business, it is a ledger event.

Examples:

- workflow intent started
- transition submitted
- state changed
- change request created
- proposed change created
- evidence provided
- approval granted/rejected
- transaction plan created
- execution started/completed
- projection updated
- external write requested/succeeded/failed
- workflow failed

Do not rely on logs as the business audit trail.

## 17. Typed State Machines

Workflow states and transitions must come from typed constants and transition maps.

Avoid giant unstructured `switch` blocks when a transition map is clearer.

Distinguish:

```text
Action = what the UI offers the actor
Transition = canonical state-machine command
Handler = internal function that processes the transition
Block = deterministic step, often Go-executed
Ledger event = persisted fact of what happened
```

## 18. Route Handler Wrapper

All API handlers should go through one wrapper that handles:

- auth
- context creation
- correlation ID
- result-to-response mapping
- unexpected exception conversion
- structured logging
- duration timing

Route handlers should be thin.

## 19. Structured Logging

Do not use `console.log` in application code.

Structured logs should include where available:

- tenantId
- actorId
- requestId
- correlationId
- workflowInstanceId
- changeRequestId
- transition
- ledgerEventId

## 20. Observability

Add placeholders from day one for:

- metrics
- traces
- duration timings
- error counts
- integration attempts
- Go executor duration
- AI token/cost tracking
- workflow transition counts

Even if V0 is simple, keep the shape enterprise-ready.

## 21. Tests

Require tests for:

- pure workflow functions
- permission checks
- transition guards
- idempotency
- version conflicts
- projection reducers
- API result mapping
- error mappers
- repository behavior around transactions

V0 legal name change tests should cover:

- employee can start own request
- employee cannot start another employee's request
- missing last name fails validation
- unchanged name fails validation
- missing evidence blocks approval
- HR can approve
- employee cannot approve
- rejected workflow cannot execute
- approved workflow can execute
- duplicate transition idempotency works
- wrong `expectedVersion` fails
- projection updates after execution
- timeline shows full causality

## 22. File Size And Complexity

Guidelines:

- Target function length: 40-60 lines.
- Target file length: 250-400 lines unless schema/config.
- Avoid nesting beyond 2-3 levels.
- Prefer guard clauses.
- Split large handlers into guard, validate, execute, and respond steps.
- One module should have one reason to change.

Rewrite complex code instead of adding more conditionals.

## 23. Dependency Direction

Keep dependency direction clear:

```text
shared/core -> domain -> repositories/services -> routes/UI
```

Lower-level shared code must not import app/server code.

Avoid large barrel files that hide dependency direction. Index exports are acceptable for constants and types.

## 24. Configuration

Validate environment/config at startup with a schema.

No loose `process.env.X` reads scattered through application code.

Config should fail fast with safe, actionable errors.

## 25. Security Defaults

Default to deny for:

- permissions
- field visibility
- AI visibility
- workflow transitions
- external integration capability
- custom block capability

Projection reads must be permission-filtered.

AI context must be assembled from permitted fields only.

## 26. Migration And Seed Discipline

Schema changes must use migrations.

Do not rely on implicit schema sync.

Seeds should be:

- deterministic
- safe to rerun
- tenant-scoped
- clearly separated between local/dev/demo data

## 27. Precommit Hooks

Use local precommit hooks for:

- Prettier
- ESLint
- TypeScript checks where fast enough

Recommended tooling:

```text
husky
lint-staged
prettier
eslint
typescript
```

Precommit should run on changed files where possible. CI should run the full suite.

## 28. Performance Rules

- Avoid unnecessary projection rebuilds.
- Avoid loading unfiltered employee documents when only a small slice is needed.
- Do not cache permission-filtered data without including actor, permission version, tenant, and projection version in the cache key.
- Use DB indexes for tenant/status/subject lookups.
- Keep Go blocks small and deterministic.
- Measure before moving Node logic to Go.

## 29. Enterprise Summary

Enterprise-ready style means:

- explicit context
- explicit errors
- explicit transactions
- explicit permissions
- explicit events
- explicit side effects
- typed states
- deterministic pure logic where possible
- centralized constants
- centralized errors
- structured logs
- auditable workflow transitions
