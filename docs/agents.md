# Agent Instructions

This repository follows the engineering standards in [docs/code-style.md](code-style.md). Agents must read and follow that document before writing application code.

## Core Rules

- Build the platform as a workflow-native system. Public mutation APIs advance workflow intents/transitions, not direct employee or change-request CRUD mutations.
- Use TypeScript/Node for the control plane and Go for deterministic or performance-sensitive workflow execution blocks.
- Use Postgres as the ledger/projection store. Business state changes must be represented as ledger events.
- Prefer pure functions for validation, permission checks, state transitions, projection reducers, diff generation, and mapping.
- Isolate side effects: database writes, external APIs, AI calls, file/document operations, logging, and integration outbox processing.
- Do not write broad `try/catch` blocks in application logic. Use `Result` objects and approved wrappers such as `fromPromise` / `fromThrowable`.
- Every fallible call must be checked before using the value. No strange error bubbling.
- Use shared constants for workflow states, transition names, statuses, permission keys, ledger event types, route names, and config defaults.
- Use shared error factories and shared error codes. Do not construct ad hoc errors inline.
- Keep variables and names human-readable. Longer names are acceptable when they reduce ambiguity.
- Declare important function-level dependencies, context, and result-shaping variables near the top. Keep short-lived variables near first use.
- Add comments around complex business logic only. Avoid comments that repeat obvious code.
- Add JSDoc for exported functions, workflow handlers, block contracts, permission functions, projection reducers, repository methods, and error mappers.
- Decompose large files and functions. Keep modules focused on one reason to change.
- Use shared folders/packages for generic reusable code.
- Enforce Prettier, ESLint, and TypeScript checks through local precommit hooks.

## Enterprise Defaults

- Default deny for permissions, AI visibility, field access, external capabilities, and workflow transitions.
- Use explicit `RequestContext` / `WorkflowContext` objects. Do not pass tenant, actor, correlation, and permission data as loose arguments.
- Commands mutate state and append ledger events. Queries read projections and timelines. Do not hide writes inside query functions.
- Every command transition must require `idempotencyKey` and `expectedVersion`.
- Transaction boundaries must be explicit in names and implementation.
- Workflow state strings must come from typed constants and transition maps.
- No direct external calls from workflow handlers. Create outbox rows and let integration workers execute side effects.
- No direct SQL in business logic. Use repositories/query modules.
- No silent `null` for actionable failures. Return `Result` with a typed error.
- Logs must be structured and include tenant/request/correlation/workflow context where available.
