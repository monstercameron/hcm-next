# Changelog

## 2026-09-06

- Quality gates (this batch) - Pre-commit now enforces format, lint, unit tests and a 70% statement-coverage floor: `tools/quality/covergate` gates every package with staged Go files (and the whole module in CI) against `definitions/toolchain/coverage-gate.yaml`; `AGENTS.md`, a thin `CLAUDE.md`, `.claude/settings.json`, the gate-runner subagent and the vendored Karpathy guidelines skill are tracked.
- `c26f730`, `725ef15`, `e885a59`, `6d81163`, `c2fdf94` (Gate A backend closure) - The last backend Gate A todos landed and are ticked (904): typed ProposeIntoManagement, workflow inspection, long-running operation and health endpoints wired through both edges with a durable operation store (00261) and a fail-closed cursor key; scheduler timer and signal roles; worker messaging delivery role; incumbent capability inventory. Review fixes and architecture-rule repairs (ceremony consumers, cross-plane allowlists, worker capability seam, mobility registry) included.
- `d121105` plus the interaction-gate follow-up - Kept product navigation on a persistent shell, scoped loading and refresh states to unresolved regions, reused baseline route projections, bounded journey workforce previews, pre-indexed People sorting and filtering, made reusable table rendering scale predictably, and shipped compressed cacheable WASM assets. CI now enforces named p95 budgets for loading feedback, every registered leaf page, the persistent shell, a 10,000-worker People interaction, a 1,000-by-12 table and the journey workforce preview.
- `d365555`, `6de21e2`, `cc9ecac` (front-end session) - Role access control, page permissions and organization visibility: migrations 00182/00238/00239 with their stores, journey RPCs, the roles and organization visibility pages, history navigation and login personas.
- `a7d621a`, `3180651`, `66318d1`, `85f7f5f`, `92ea605`, `c000a21` (security test wave) - Every authentication, authorization and security file now has a package test that exercises its exported surface and deny branches (25 lanes, 110 packages, test-referenced symbols 51% -> 67%); twenty-two production defects the new tests caught are fixed (trailing-JSON acceptance in OIDC and workload identity, foreign-tenant edges through trust/authz, unenforced attestor authority, root-as-leaf issuance, caller-supplied step-up and missing tenant binding in garnishment, free-text validation override in pay methods, forged authorization decisions in product-query invalidation, id-keyed connector operations without tenant checks, and others). The authorization batch closed with ticks 871 -> 895; connector operation queue, journal and credential lease tables (00235-00237) gained their PostgreSQL store; storage-disposition, storeboundaries, vulnimpact, layer-graph, P1A manifest and authority-gate corpora refreshed.
- `ab184e2` - Added the company metadata stores and migrations 00070-00131 (asset, benefits, career, CBA, commercial, contact, content registry, CRM, custom objects, employee relations, equity, FX, HR case, identity privacy, incentive, merit, mobility, pay input, pay method, performance, planning, safety, scheduling, skill, subscription, succession, survey, tax profile, trust; attestation responses; reference dataset release and adoption), each with a tenant-isolated store package, disposition row and tests; goose statement markers on plpgsql blocks; regenerated model manifests and the re-signed P1A manifest.
- `64ef19a` - Added the metadata behaviour behind the stores: location normalization and correction, job architecture assignment and publication, custom-object events, capabilities and search, commercial entitlement snapshots, benefit plan years, HR cases, the flat promotion commit command, the canonical byte stream engine and the bounded expression compiler.
- `08cd05c` - Landed session rotation and revocation fan-out (AUTHN-004/009), the attestation lifecycle (ATTEST-004..008), access lifecycle and drift reconciliation (ACCESS-003/004, ARTIFACT-005), entitlement across channels (CROSS-CONF-002), the reference dataset lifecycle (REFDATA-001, CONFIG-003) and the promotion entitlement and sensitive-access gates.
- `3bcf4b7` - Workflow and execution follow-ups: continuation target attempts, the configuration promotion step, platform config promotion and the end-to-end tests that exercise them.
- `ea9067a` - Product workspace, UI component and UX qualification client updates from the front-end session, with their code-style, ESLint and package configuration.
- `14442de` - Gate A wave tables and stores (migrations 00132 onward), the PostgreSQL outbox queue schema and claim protocol (EVENT-001), artifact byte storage, regenerated model manifests and disposition rows.
- `aca251d` - Workflow, integration and DataOps mappings on the shared transformation engine (XFORM-008), commercial pilot evidence (COMM-002/003), experience preferences.
- `a13bb8b` - Privacy processing inventory (PRIV-001), governance decision inputs, snapshots and composition (GOVERN-001/002), retention simulation (RECORDS-DISP-001), masked datasets (MASK-001).
- `5af163d` - Reliability and telemetry policy (OPS-001..005), recovery matrix and immutable backups (RECOVERY-001/002), control-plane distribution and feature evaluation (CP-004..006), IaC selection and provisioning contracts (IAC-003/004/005/013), connector health and idempotency governance (INTG-017/018, MSG-006, IDEMP-001), scheduler and worker roles (SVC-005/010, WF-RUN-005), bounded transports and the connector sandbox (CONN-RT-002/006), design-partner wedge records (WEDGE-001..009), workload envelopes.
- `04aba0e` - Endpoint header, deadline and budget policy with the transport-parity harness (ENDPOINT-006..008), gRPC deadline classification (RPC-EDGE-001), safe versus exact exports (EXPORT-001), AWS SDK qualification (LIB-018), application and command wiring.
- UI and docs commits (this batch) - front-end session workspace updates; devlog section 6, the changelog, planning and definitions updates, and the gitignore block for build and test artifacts that lanes leave in the checkout.
- Docs commit (this entry) - Ticks 657 -> 759 (registry 1666); twelve backfilled todos for code that had none; four stale checkboxes flipped; every evidence line now carries its go test command in the checked form; storage disposition through 00131; capability-coverage and intent-coverage goldens; table governance policy tools (ALIGN-008..015); coverage inventories.

## 2026-09-05

- `829e3a9` - Added the durable stores and migrations 00040-00069 the promotion closure needs (reconciliation jobs, session store, position/budget reservations, attestation, payroll run, pay-GL, job architecture, location, tenant placement, balance accumulators, access identity, legal evidence, outbox lease fence, consumer positions, record copy links, replay snapshots, connector operation journals, intent outcome references); `pgtest` now sweeps orphaned embedded-PostgreSQL runtime directories.
- `89d8c1b` - Landed the transaction plane: plan preparation against stream heads (TX-003), the commit coordinator with idempotent replay and crash-at-every-boundary proofs (TX-004), evidence resolution (TX-005), immutable corrections (TX-007), cancel-before/after commit under an advisory lock (TX-008), the crash-boundary conformance suite (TX-009).
- `5b64e58` - Made the compiled promotion execute plan terminate accurately: revisited nodes activate at attempt N+1 and continuations carry the attempt, approval completion and resume address the open attempt, WAIT timers are activation-qualified in identity and key, OBSERVE retry exhaustion routes to the repair terminal, `end_blocked` completes; SHADOW mode, worker-death recovery, RepairPlan execution mode, telemetry per advancement.
- `ad58d6a` - Bound workflow terminals back onto the Intent as one OutcomeReceipt carrying commit-receipt and repair references (INTENT-007), closure under the completion policy (INTENT-008), atomic approval election, pre-execution revalidation, rendered-digest binding and separation of duties with idempotent replay (APPROVAL-003/005/006/008), journey stages for the execute plan.
- `1352e3d` - Added governed connector operations (journals before dispatch, lease-time revalidation, per-resource causal order, exactly-once dispatch, UNKNOWN after timeout, isolated redrive), reconciliation completion (RECON-002) and RepairPlan revalidation (REPAIR-002).
- `d3c2a53` - Added the bounded people, organization and compensation writes with append-only evidence inside the Promotion local ACID commit (PEOPLE-004, ORG-003, COMP-004, PROMO-005) and the transaction-invariant suite (MODEL-025), plus domain packages for the persisted stores and engine updates.
- `b524715` - Wired serve for the execute plan: root redirect, health listener, timer dataset flags, scheduler as an initial process role, `-workflow-plan=execute`, browser Origin/Host/CSRF policy, journey stage wire values, serve-graph golden.
- `760eb1b` - Product workspace UI, journey client labels and actions for the execute-plan stages, UX qualification tooling; code-style allow-list for the vendored wasm shim, the design demo and the race-policy script.
- `0a56524` - Promotion end-to-end suite (nine scenarios, PROMO-009, WF-RUN-016 repair execution), acceptance matrix 24/24 PROVEN, bootstrap and otelmw tests aligned, architecture golden, gate-evidence signing, policy and planning tools.
- Docs commit (this entry) - Todos 588 -> 657, storage disposition through 00069, P1A manifest re-signed over 62 migration files with recorded gaps, telemetry allow-list rows, process/dependency roles, coverage inventories, workflow catalogue, user stories, security research, devlog `planning/devlog/2026-09-05-promotion-termination-wave.md`.

## 2026-09-03

- `aef1496` - Moved `internal/kernel/canonical` and `internal/kernel/digest` to `internal/engines/wire/canonical|digest`; updated all import sites, docs, and architecture firewall roles; added `ledger.NewAppenderWithClock` for deterministic clock pinning.
- `a56a251` - Hardened data plane (aggregates, ledger/hashchain/lineage, artifacts, bitemporal, projection/critical mapper, outbox, provenance, tenancy, health); added `dbport`/`pgxadapter` abstraction and `pgtest` isolation/lock helpers plus seed conformance fixtures.
- `9fedc43` - Added legal obligation model, pack definitions/releases, and extract pipeline (matrix, research, states); ported CA/NY rulepacks and seeded 50-state `definitions/legal` packs.
- `ce27a9a` - Rewired intent cell/pgstore/workspace, added `transaction/idempotency` (TX006) and full workflow runtime lanes (frontier, inspect, runtime, version, wait-step) plus `connectivity/observe` fixes.
- `56c1c93` - Added transport cell/OTel middleware, admin gRPC + `hcmctl`, humanwork workitem/workspace handler, dev token minting, and CLI/projector/worker wiring.
- `a862456` - Landed architecture qualifications, operation definitions, capability coverage/P1A manifests, generated admin/wire contracts, migrations, and planning/quality toolchains.
- `7e9d649` - Synced docs and harnesses (README, state research + us-federal, admin proto, serve/workspace conformance harnesses, todos, layout docs).
- `b42414a` - Repaired vet and test lanes: added workflow_continuation/advancement_receipt migrations, fixed stale replay head check, regenerated P1A evidence/golden, and scaffolded missing package unit tests.
- `3692405` - Backfilled per-file unit tests to 100% file coverage (577 files, 25k+ lines): dedicated \*\_test.go for every Go source lacking a direct counterpart across internal/, tools/, cmd/, and gen/.
- `b3c277e` - Implemented DB-013: materialized 16 governance/AuthZ/legal/evidence tables (00021), updated storage disposition, added Go governance store with digest/interval validation (cross-tenant delegation and unversioned authority rejected), and landed TestTodo_DB_013 (7 subtests).

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
