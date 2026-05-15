# Claude Instructions

Follow [docs/code-style.md](code-style.md) for all implementation work in this repository.

## Working Style

- Keep code explicit, typed, and easy to audit.
- Prefer small pure functions before side-effectful functions.
- Make side effects obvious from function names and file placement.
- Avoid broad `try/catch` in business logic. Convert throwing APIs into `Result` with shared wrappers.
- Check every `Result` before using `.value`.
- Use shared constants and shared error factories.
- Write comments only around complex rules, workflow decisions, or non-obvious tradeoffs.
- Add JSDoc for exported APIs and important workflow/block functions.
- Decompose large files into focused modules.
- Keep names readable, even when longer.
- Use Prettier, ESLint, and TypeScript checks before committing.

## Architecture Rules

- Public mutations go through workflow intents and workflow transitions.
- `WorkflowInstance` owns runtime state. `ChangeRequest` owns the business mutation.
- Node/TypeScript owns the control plane, APIs, workflow orchestration, AI orchestration, and external communication.
- Go owns deterministic workflow blocks, validation/calculation/scoring, simulation, and future custom block execution.
- Postgres owns ledger events, projections, workflow state, change requests, approvals, transaction plans, and outbox rows.
- Workflow handlers must not call external systems directly. They create integration outbox work.
- Business mutations must append ledger events.
- Projection reads must be permission-filtered.

## Error Discipline

- Expected failures return `Result`, not thrown exceptions.
- `try/catch` belongs only in system-boundary wrappers: route wrapper, DB wrapper, integration client, AI client, Go executor client, file/document client.
- Use shared error codes and factories from the core error package.
- Public errors need safe user-facing messages.
- Internal logs can include technical detail, but never leak secrets or restricted HR data.

## Enterprise Readiness

- Require `idempotencyKey` and `expectedVersion` for workflow transitions.
- Use typed state machines and transition maps.
- Keep command/query separation.
- Make transaction boundaries explicit.
- Add tests for permissions, transition guards, idempotency, version conflicts, projection reducers, and API result mapping.
- Default to deny for permissions, field visibility, AI context, and external integration capability.
