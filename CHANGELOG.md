# Changelog

## 2026-09-03

- `aef1496` - Moved `internal/kernel/canonical` and `internal/kernel/digest` to `internal/engines/wire/canonical|digest`; updated all import sites, docs, and architecture firewall roles; added `ledger.NewAppenderWithClock` for deterministic clock pinning.
- `a56a251` - Hardened data plane (aggregates, ledger/hashchain/lineage, artifacts, bitemporal, projection/critical mapper, outbox, provenance, tenancy, health); added `dbport`/`pgxadapter` abstraction and `pgtest` isolation/lock helpers plus seed conformance fixtures.
- `9fedc43` - Added legal obligation model, pack definitions/releases, and extract pipeline (matrix, research, states); ported CA/NY rulepacks and seeded 50-state `definitions/legal` packs.
- `ce27a9a` - Rewired intent cell/pgstore/workspace, added `transaction/idempotency` (TX006) and full workflow runtime lanes (frontier, inspect, runtime, version, wait-step) plus `connectivity/observe` fixes.
- `56c1c93` - Added transport cell/OTel middleware, admin gRPC + `hcmctl`, humanwork workitem/workspace handler, dev token minting, and CLI/projector/worker wiring.
- `a862456` - Landed architecture qualifications, operation definitions, capability coverage/P1A manifests, generated admin/wire contracts, migrations, and planning/quality toolchains.
- `7e9d649` - Synced docs and harnesses (README, state research + us-federal, admin proto, serve/workspace conformance harnesses, todos, layout docs).
- `b42414a` - Repaired vet and test lanes: added workflow_continuation/advancement_receipt migrations, fixed stale replay head check, regenerated P1A evidence/golden, and scaffolded missing package unit tests.

## 2026-05-16

- `51fe847` - Aligned the AI chat Playwright fixture with the seeded demo
  employee (`Jane Doe` / `emp_123`) so quick-action prompt assertions match the
  console's default subject context.
- `9e24c2f` - Added Playwright regression coverage for an AI-generated
  termination page using `name`-keyed fields and `$selectedSubject` summaries.
  Updated `WorkflowPageRenderer` to treat field `name` as a stable fallback id,
  and hid the assistant FAB while the panel is open while preserving focus
  restoration on close.
- `c62d4a2` - Restored the workflow action-bar `systemError` import used by
  generated-page submit error mapping after the fetch-boundary refactor.
- `e55f62d` - Fixed the code-style checker so export declarations without a
  module specifier do not crash the AST walk. Added a console HTTP boundary
  helper and routed AI chat/start-workflow fetch calls through it so the
  external-call rule passes without weakening the architectural check.
- `9f99732` - Applied Prettier formatting to the AI assistant backend, console
  runtime, tests, and documentation changes after enabling format enforcement
  in `test:all`.
- `c06dcc5` - Documented the implemented AI assistant UI generation flow in
  `docs/ai-ui-generation.md`, linked it from `docs/plan.md`, and replaced the
  stale TODO inventory with completed four-workstream status plus follow-up
  backlog.
- `b0d640c` - Added GitHub Actions for Node and Go test jobs on push, pull
  request, and manual dispatch. Added pre-commit enforcement for lint-staged,
  code-style checks, Go checks, and `test:all`; added TypeScript and Go style
  checkers; moved shared runtime dependency types out of the API layer; replaced
  high-signal workflow magic literals with typed constants; and gofmt-formatted
  the touched Go block code.
- `4c7bc8a` - Added the console AI assistant surface and generated-page runtime:
  floating `AiChatPanel`, quick-action chips, `useChat`, `useStartWorkflow`,
  assistant-view rendering and route cleanup in `ConsoleShell`, generated page
  form state, subject picker widget, action-bar workflow submit wiring, branded
  panel styles, and unit/Playwright coverage for the assistant and renderer
  paths.
- `fbd5240` - Added the AI chat and workflow UI generation backend:
  `POST /api/ai/chat`, chat tool dispatch for workflow/employee/task actions,
  direct `POST /api/ai/generate-ui`, `AiClient.generatePageDefinition`,
  canonical widget type constraints, OpenAI/null-client implementations,
  workflow config lookup and hashing, UI generation error factories, and API,
  provider, registry, and E2E tests.
- `2f1a7ac` - Added runtime package dependencies for the AI assistant:
  `dotenv` for API environment loading plus `@tanstack/react-query`,
  `react-markdown`, and `remark-gfm` for the console assistant experience.
- `2d00188` - Enhanced the console app shell with a demo auth session (login screen,
  workspace card, session storage using `fromThrowable` at the UI boundary). Expanded
  `WorkflowPageRenderer` with richer interaction handling, extended `hcm-controls` with
  new field types, updated field registry and widget index tests, and expanded Playwright
  component gallery and UX smoke test coverage.
- `22afae4` - Expanded the brand token system with gap, padding, sizing, shadow, opacity,
  scrollbar, and chart accent tokens. Extended `style-lab-vars` to expose the new tokens
  as CSS variable mappings, added matching `StyleLabRail` sliders for live editing, and
  updated `styles.css` to consume the new variables.
- `816cd8c` - Added vitest test suites for the termination workflow: `ai-review-graph.test.ts`
  covers auto-advance through `ai_review` nodes, `completedNodes` outcome reporting,
  fallback via `outcomes[0]`, and stop-boundary behavior; `termination-workflow-config.test.ts`
  validates config loading, schema validation, AI review node wiring, preflight block
  reference, and start actor requirement.
- `f39a608` - Wired `ai_review` node auto-advance and async AI execution into the workflow
  runtime service. `aiReviewAutomaticRouteKeys` maps every `ai_review` node to `'completed'`
  before graph advance; `executeAiReviewNodes` fires async AI calls for traversed nodes and
  appends `AiChangeReviewGenerated` or `AiChangeReviewFailed` ledger events. AI failures
  never block workflow progression (`failurePolicy: 'continue'`).
- `bfb4269` - Added the employee termination workflow config (`employee.termination`):
  HR-initiated input collection, Go compliance preflight block, `ai_review` node for risk
  assessment, HR director approval, plan-transaction block, projection write, payroll and
  benefits external writes, ledger event recording, and full timeline summaries. Registered
  in the filesystem workflow config registry.
- `5712fb5` - Added Go termination preflight and plan-transaction blocks.
  `system.employee_data.termination.preflight` validates employment status, termination
  type, effective date, and business reason; computes tenure, risk level, statutory notice
  days, COBRA window, and warnings. `system.employee_data.termination.plan_transaction`
  produces an HRIS internal write, `fake_payroll/processFinalPay` and
  `fake_benefits/triggerCobra` external calls, and an `employment.status` projection patch.
  Registered in both executor entry points.
- `92aa225` - Added `ai_review` to the workflow graph node type union and validation
  allowlist. Exported `WorkflowAiReviewNodeConfig` (changeType, visibleFields,
  currentStateTemplate, proposedStateTemplate, failurePolicy).
- `15ed808` - Added foundation constants for the employee termination workflow:
  `TerminationSubmitted`, `TerminationPreflighted`, `TerminationExecuted` ledger event
  types; five termination action permissions; and the `employee.termination` workflow intent.

## 2026-05-15

- `011fdf9` - Marked atomic runtime and field components complete in the TODOS tracker.
- `7afe6cc` - Expanded the console control library with canonical field types, new widget
  categories (ui-widgets, graph-widgets, workflow-widgets), updated field registry and
  shared config, broad style refinements, and added Playwright E2E setup with component
  gallery visual and console UX smoke tests.
- `6e9d830` - Added structured logging throughout the workflow runtime and admin services,
  covering intent start/create, transitions, idempotent replay, external writes, and all
  admin lifecycle operations (import, validate, publish, deprecate, repair actions).
- `2ad6b31` - Added request-scoped logging to the Node API layer: per-request logger
  injected via DI, executor client re-emits Go block logs into the unified stream, and
  HTTP request/completion events logged at appropriate levels.
- `d909276` - Added structured slog logging to the Go executor service with per-request
  child loggers carrying block, requestId, correlationId, workflowInstanceId, and actorId.
- `9bcf5a1` - Added structured logging infrastructure to the platform foundation: extended
  LogContext with a service field and added JSON-to-stderr fallback at async, sync, and
  database transaction exception boundaries.
- `1412922` - Ignored Playwright test artifacts, output directories, and image files.
- `e398414` - Added the dynamic workflow UI console, UI contracts/runtime
  packages, brand token rendering, configurable control and widget libraries,
  style lab, broad HR widget catalog, route menu organization, and frontend UI
  planning docs.
- `8017b40` - Added the workflow admin runtime foundation, including JSON
  workflow draft/publish APIs, validation and preview services, simulation,
  debugger and repair helpers, workflow admin persistence, graph runtime
  support, E2E coverage, and admin/demo documentation.
- `fdf115a` - Extracted shared Go block helpers for strict decoding, date
  validation, risk/fact construction, transaction field validation, and string
  normalization across deterministic blocks.
- `2a8b2e7` - Hardened `.gitignore` coverage for local build artifacts, runtime
  scratch data, editor state, and secret material.
- `ddc16da` - Updated the generic runtime TODOs and workflow schema docs after
  moving the active workflow path to generic runtime execution.
- `71191dd` - Moved workflow E2E coverage to the public API routes and added a
  generic-runtime contract test proving a new workflow intent can run without a
  workflow-specific TypeScript service edit.
- `a2220ba` - Replaced workflow-specific TypeScript services with the generic
  runtime, config-driven workflow schemas, external-write client dispatch, and
  reusable approval, transaction, permission, timeline, and record handling.
- `68968e3` - Added the generic Go block output contract, customer-facing SDK
  aliases, block contract docs, and generic route/transaction fields across
  existing deterministic blocks.
- `2c63cef` - Added the generic workflow runtime TODO plan that captures the
  remaining work to remove workflow-specific TypeScript branches and move
  business behavior into config and Go blocks.
- `0d88740` - Added the position headcount requisition approval workflow,
  HarborCare fixtures, E2E coverage, API demo docs, approval-gate schema docs,
  and completed approval workflow TODOs.
- `434c493` - Added the approval-gate data model and evaluator, including
  `approval_groups`, approval task metadata, migration coverage, permissions,
  ledger events, and unit tests for sync/async gate rules.
- `821bc57` - Extracted reusable workflow runtime helpers for access context,
  JSON field parsing, projection patches, timeline views, ledger events,
  response shaping, and transition attempts.
- `8d45464` - Added workflow registry admin tooling for listing, validating,
  importing, publishing, and resolving checked-in workflow configs.
- `cc313ac` - Updated the changelog after the org-transfer available-action
  fixes.
- `ca3e00f` - Allowed scoped org-transfer approvers to retrieve available
  approval actions without requiring broad workflow visibility.
- `196f8e9` - Fixed the org-transfer E2E available-action helper used by the
  expanded approval-chain test.
- `0df392c` - Updated the changelog after the final org-transfer test assertion
  fix.
- `312c71d` - Tightened the org-transfer E2E approval-task assertion to coerce
  the task identifier through the same string path used by workflow transitions.
- `8ceb7dd` - Updated the changelog after expanding org-transfer E2E coverage.
- `97d8e12` - Expanded org-transfer E2E coverage for the HR-started workflow,
  approval routing, execution, idempotent replay, projection changes, and
  filtered timeline checks.
- `e29c0b0` - Updated the changelog after the org-transfer metadata alignment
  commit.
- `0b37642` - Aligned the org-transfer demo fixture date and approval metadata
  with the workflow configuration used by the org-transfer runtime.
- `b72f7fa` - Started the root changelog and documented the initial logical
  implementation commits.
- `503fcc2` - Updated workflow planning documentation, including the workflow
  schema spec, org-aware RBAC plan, API demo notes, project layout notes, and
  detailed TODO workstreams.
- `ff81534` - Added workflow execution improvements, compensation and org-transfer
  workflow configs, Go execution blocks, simulated third-party compensation APIs,
  employee/RBAC E2E coverage, and V0 legal-name guardrail tests.
- `923b887` - Added the org-aware RBAC data foundation: org units,
  relationships, worker assignments, role bindings, expanded demo seed data, and
  reusable employee access filtering helpers.
