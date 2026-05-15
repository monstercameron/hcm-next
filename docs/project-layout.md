# Project Layout

HCM Next is organized by runtime boundary and product responsibility.

```text
src/
  api/                  Node API control plane, HTTP server, dependencies.
  blocks/go/            Go block runner and deterministic block implementations.
  platform/
    foundation/         Result, errors, constants, context, logging, shared types.
    data-store/         Postgres client, migrations, seeds, repositories, projections.
    workflow-runtime/   Reusable workflow runtime primitives and services.
  workflows/
    legal-name-change/  Workflow-owned orchestration, permissions, manifest notes.
    shared/             Reusable workflow configuration conventions.
  tests/                E2E and cross-boundary tests.

docs/                   Product, architecture, implementation, and operating notes.
scripts/                Root-level developer and automation scripts.
infra/                  Deployment and local infrastructure assets.
```

## Ownership Rules

- `src/api` should stay thin: HTTP routing, request context, response mapping, and dependency assembly.
- `src/platform/foundation` must not depend on application, storage, or workflow code.
- `src/platform/data-store` owns SQL, migrations, repositories, seeds, projections, and ledger persistence.
- `src/platform/workflow-runtime` owns reusable workflow machinery.
- `src/workflows/*` owns workflow-specific orchestration, permissions, schemas, state graphs, block contracts, and workflow notes.
- `src/blocks/go` owns deterministic or performance-sensitive block execution.
