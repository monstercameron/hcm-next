# Legacy Implementation Baseline and Go Cutover Contract

## Status and Purpose

This specification captures useful implemented behavior documented by the
pre-restructure repository. It is a behavior-extraction and regression contract, not the
target architecture and not a fresh certification that every historical check
still passes.

```text
legacy implementation
       |
       +-- preserve business behavior
       +-- preserve regression fixtures
       +-- preserve data lineage
       |
       v
clean Go-only implementation
       |
       v
parity and authority gate
```

## Documented Current Architecture

The older documents describe this running shape:

```text
React/Vite console
       |
       v
framework-free Node/TypeScript API
  API | AuthZ | workflow | persistence | timeline
       |
       +-------- HTTP --------> Go deterministic block executor
       |
       v
PostgreSQL
  ledger | projections | approval tasks | outbox
```

The binding target is:

```text
GoWebComponents workspace
       |
       v
generated web client / grpcbridge
       |
       v
Go capability and workflow services
       |
       +-- Protobuf contracts
       +-- SchemaFlux catalog proof
       +-- deterministic Go domain packages
       |
       v
PostgreSQL canonical transaction core
```

No replacement may silently drop a documented behavior merely because its old
implementation language is being retired.

## Documented Vertical Slices

### Legal-name change

The smallest completed workflow is documented as:

```text
WorkflowIntent
      |
submit legal name + date + reason
      |
Go preflight
      |
ChangeRequest + ProposedChanges
      |
evidence Document
      |
HR approval
      |
TransactionPlan
      |
employee projection update
      +-- fake HRIS outbox request
      |
ledger-derived timeline
```

The public mutation surface was deliberately workflow-native:

```text
POST /workflow-intents
POST /workflow-instances/:workflowInstanceId/transitions
POST /documents
```

Compatibility behavior includes:

- A worker cannot start the self-service flow for another worker.
- A requester cannot approve their own proposal.
- Transition requests include a stable `idempotencyKey` and
  `expectedVersion`.
- A stale version returns `VERSION_CONFLICT`.
- An idempotent replay returns the stored response without duplicate business
  effects.
- Rejected and canceled instances cannot execute.
- Go preflight and planning blocks are side-effect free.
- External synchronization is represented by an outbox operation.
- Business, audit, and debug timeline views are distinct.

### Headcount approval fixture

The HarborCare fixture provides a reusable approval conformance graph:

```text
Headcount request
      |
      v
SEQUENTIAL leadership gate
  Morgan -> Sofia
      |
      v
PARALLEL cross-functional gate
  Finance | HRBP | Compensation | Medical Director | Clinic Ops
      |
      +-- pass after any 3 approvals
      +-- Finance rejection vetoes
      +-- Medical Director rejection vetoes
      +-- cancel remaining tasks after terminal gate result
```

The scenario verifies ordered task creation, asynchronous decision order,
quorum, veto, terminal cancellation, task versioning, and ledgered gate events.

### Organization-transfer fixture

The HarborCare transfer fixture moves Jane Doe from Boston Nursing and
`CLN-BOS` to Cambridge Nursing and `CLN-CAM`, with a source manager and distinct
destination manager.

```text
current placement                 proposed placement

Boston Main Clinic               Cambridge Clinic
Boston Nursing                   Cambridge Nursing
CLN-BOS                          CLN-CAM
manager emp_456                  manager emp_461
       \_____________________________/
                     |
       source + target AuthZ evaluation
```

This fixture must remain available for relationship, organization, current and
proposed scope, cost-center visibility, and transfer conflict tests.

### Generic runtime and additional workflow evidence

The changelog records a later move from workflow-specific TypeScript services
to a generic, configuration-driven runtime. It also records broader tests and
blocks for organization transfer with compensation, headcount requisition, and
employee termination.

```text
checked-in workflow config
        |
generic graph/runtime helpers
        |
deterministic Go blocks
        |
generic transaction outputs
        +-- facts and route key
        +-- validation errors and warnings
        +-- TransactionPlan
        +-- external calls
        +-- projection patches
        +-- ledger facts
```

The termination implementation is documented as HR-initiated input,
deterministic compliance preflight, optional asynchronous AI review, HR director
approval, planning, employment projection change, and payroll/benefits external
effects. The legacy AI review records success or failure and continues rather
than blocking the transaction. Preserve that behavior as a regression case,
but require a current risk decision before adopting the same failure policy for
any high-risk production workflow.

Two simulated third-party services provide useful test seams:

- Compensation market lookup by job, level, and pay zone.
- Compensation decision submission with current/proposed money, effective
  date, worker, change request, and business reason.

They should remain deterministic fakes for connector mapping, capacity,
timeout, retry, redrive, external-observation, and reconciliation tests even if
their legacy HTTP implementations are replaced.

## Workflow Administration Behavior to Preserve

The legacy admin surface documents these capabilities:

```text
template/raw JSON
      |
      v
draft -> validate -> preview -> simulate
      |
      v
publish immutable version
      |
      v
instance pins version
      |
      +-- debug
      +-- diff
      +-- repair
      +-- rollback/quarantine policy
```

Useful operations include schema validation, Mermaid preview, block catalog,
input-mapping preview, interaction preview, permission preview, simulation,
diff, publication guardrails, instance debugging, and typed repair actions.
The future Go workflow compiler may change endpoint shape but must preserve the
underlying operator outcomes.

## Legacy Runtime Limits

The older graph runtime documents important limits:

- One graph node is active at a time.
- Automatic nodes execute sequentially until a wait boundary.
- Runtime state and `activeNodeId` are separate.
- True parallel branch execution is not fully implemented.
- Saga and compensation semantics are partial.
- Several node types are forward-looking schema vocabulary rather than proven
  runtime behavior.

These limits are not target requirements. They identify tests needed before
the new durable scheduler claims parallelism, compensation, or migration
support.

## Existing Experience Baseline

Later legacy documents describe more than the earliest API-only V0:

- A schema-driven React/Vite console and workflow page renderer.
- A canonical widget registry, data bindings, checked-in page definitions, and
  brand foundations.
- `/api/ai/chat` with a tool loop and `/api/ai/generate-ui` for generated page
  definitions.
- Permission gating before model invocation and display-safe tool results.
- Accessibility and browser-level UX tests.

Known deferred or incomplete areas include production field redaction, durable
generated-page promotion, response streaming, and a fully separated governed
Agent Runtime.

The GWC implementation must match required semantic page, workflow action,
permission, accessibility, and audit behavior. Pixel parity is not required,
and the historical React surface is not shipped beside it.

## Data and Logging Baseline

The legacy data design already separates:

```text
ledger events        immutable business narrative
projections          current rebuildable reads
integration outbox   durable external intent
runtime tables       current workflow mechanics
telemetry            technical diagnostics
```

Structured logs use correlation, request, tenant, actor, workflow, and change
identifiers. Request/response bodies, raw SQL, secrets, and sensitive user data
are excluded. Business audit facts belong in the ledger rather than logs.

The changelog also documents GitHub Actions for Node and Go, pre-commit
format/lint/style checks, Playwright UX coverage, and separate code-style
enforcement. Extract their acceptance intent into Go-native CI and browser
checks; do not carry Node-based build tooling into the target pipeline.

## Cutover Rules

The cutover is staged so that the rewrite is never the critical path for
customer evidence:

```text
P1A   Go slice runs beside the legacy runtime. Legacy may serve the
      workspace and existing routes; the Go slice owns intent, simulation,
      observation, and evidence. Comparison evidence is collected here.

P1B   Go owns the write path. Legacy is excluded from the release image.
      No Node or TypeScript process is on the production request path.
```

1. Define the semantic capability and Protobuf schema before replacement code.
2. Use the qualified transport edge (grpcbridge or its named fallback) to
   preserve browser/HTTP reach without creating a second business contract.
3. Use the qualified generator (SchemaFlux or plain Protobuf codegen) for the
   selected bounded catalogs.
4. Move deterministic Go blocks into owned Go domain packages instead of
   retaining a permanent process hop solely for historical layout parity.
5. Legacy code is not extended with new behavior. During P1A it may keep
   running as-is beside the Go slice; a Node compatibility service does not
   survive into P1B.
6. Compare ledger events, approval outcomes, projection changes, outbox
   operations, errors, and AuthZ explanations between old and new paths.
7. Use offline comparison, deterministic replay, or simulation before moving
   write authority; the legacy runtime is not required in production after P1B.
8. Prefer open-source, operationally simple dependencies; total operating cost
   and exit path matter more than license price alone.

## Acceptance Evidence

A P1B cutover-ready slice is not complete until it demonstrates:

```text
contract compatibility
       +
legacy scenario parity
       +
new safety invariants
       +
recovery behavior
       +
workspace accessibility (GWC or the Go SSR fallback)
       +
operator debug evidence
       =
Go-owned write path, legacy excluded from release
```

P1A requires only the first two and the comparison evidence. At minimum, rerun
the legal-name, headcount-gate, and organization-transfer fixtures where
relevant. Historical markdown checklists are never substituted for current CI
output.
