# Devlog — 2026-09-05: the Promotion workflow terminates accurately

Running log of the wave that closed every backlog item the compiled
`hcmnext.workflows.promotion.execute` v1 plan depends on to reach a terminal
and bind that terminal back onto its Intent. Written by the orchestrating
session; every result below was re-verified by the orchestrator before it was
recorded (`go test -count=1` on the named packages, windows/arm64, Go 1.26.3,
branch `plan-revision-2026-09-02`). Entries are in the order the work happened.

## 1. Starting point

- HEAD `7d55b67` had the executable Promotion prototype: the prototype plan
  (`promotion_approval@1`) completed end to end, the serve process ran the
  in-process scheduler, and the execute plan had just been compiled
  (`internal/workflow/promotionexec`) with a step runner
  (`internal/platform/execution/promotionsteps`) and a live proof on the dev
  server.
- The gap was everything the execute plan touches durably: the tables its
  steps write, the ledger and transaction semantics its commit relies on, the
  telemetry the runtime emits per advancement, the intent projection the
  terminal must update, and the connector, repair and reconciliation paths
  the non-happy terminals route through. A dependency closure over
  `planning/todos.md` produced 123 todos with a 21-todo frontier.

## 2. How the wave ran

- Codex GPT-5.6 Luna lanes, launched in four dependency waves (A: 18 lanes,
  B: 3 chains, C: 5 chains, D: 4 chains) plus fix lanes for packages whose
  PostgreSQL-backed tests a lane had skipped. Lanes own file roots only;
  definitions, planning, migrations numbering, storage disposition rows and
  ticks stay with the orchestrator.
- Every landed package was re-run by the orchestrator. Lanes reliably misread
  embedded-PostgreSQL archive-lock contention as "database unavailable" and
  skipped their pg suites, so "PASS" in a lane report was never taken as
  evidence.
- Ticks were applied by `verify_lane.py`, which runs gofmt, vet and the
  package tests and checks that every TEST MATRIX name exists before it
  writes the tick. 588 → 657 ticked (registry 1654).

## 3. What landed

- **Tables (migrations 00040–00069).** Effect reconciliation jobs, session
  store, position and budget reservations, attestation, payroll run, pay-GL,
  job architecture, location, tenant placement, balance accumulators, access
  identity, legal evidence, outbox lease fencing, consumer positions and
  dedupe, record copy links, replay snapshots, people/organization/
  compensation write evidence, invariant evaluation, connector operation
  journals, and the intent outcome references. Every table has a store
  package, RLS, a storage-disposition row and per-file tests.
- **Ledger and data.** Stream-head CAS (DATA-002), multi-stream append
  under canonical lock order (LEDGER-003), commit checkpoints, partitions,
  rebuild-to-digest, replay snapshots verified against the ledger, read
  barriers (DATA-021).
- **Transactions.** Plan preparation against heads (TX-003), the commit
  coordinator with idempotent replay and a crash at every boundary (TX-004),
  evidence resolution without guessing (TX-005), immutable corrections
  (TX-007), cancel-before and cancel-after commit under an advisory lock
  (TX-008), the crash-boundary conformance suite (TX-009), per-resource
  causal ordering (TX-010).
- **OpenTelemetry.** Span registry and cardinality rules, boundary
  instrumentation with the six allow-listed attributes, deterministic test
  exporters, panic and error scrubbing, one span per advancement and an
  evidence row per authority decision and terminal.
- **Intent closure.** The terminal's five lifecycle dimensions plus the
  RECON-002 verdict bind back onto the intent as one OutcomeReceipt
  (INTENT-007); closure only under the definition's completion policy
  (INTENT-008); separation of duties on approvals (APPROVAL-008); atomic
  approval election, pre-execution revalidation and rendered-digest binding
  (APPROVAL-003/005/006).
- **Non-happy terminals.** Connector operation journals, lease-time
  revalidation, exactly-once dispatch, UNKNOWN-after-timeout and isolated
  redrive (INTG-011..016); RepairPlan revalidation and execution mode
  (REPAIR-002, WF-RUN-016); reconciliation completion (RECON-002).
- **Wiring.** Root redirect, health listener, timer dataset flags, scheduler
  as an initial process role, `-workflow-plan=execute`, journey stages 7–15.

## 4. Defects found by re-verification, fixed by the orchestrator

- **A completed intent could not be read.** The INTENT-007 binding projected
  ExecutionState COMMITTED onto `intent_instance`, and the next read failed
  the lifecycle rule `committed-requires-receipt`. OutcomeReceipt, Instance
  and IntentRecord now carry `CommitReceiptRef` and `RepairRef`; the app
  derives them from the compiled END node qualified by the instance;
  migration 00069 persists them; `LifecycleContext` reads them.
- **Re-approval collided on attempt 1.** Routing back to `approve_manager`
  inserted a second attempt 1 and hit the node-execution primary key.
  `runtime.Advance` now activates a revisited successor at attempt N+1 and
  stamps the attempt on the continuation record; approval completion and
  `advanceOnce` address the open attempt (only on plans that declare a
  cycle, so scripted-executor tests see no new query).
- **The second WAIT never fired.** `workflow_timer` is unique per
  (instance, node, key), so the re-entered WAIT replayed the already-fired
  first promise. Timer identity and key are now activation-qualified
  (`TimerIDForAttempt`, `TimerKey` = `digest|attempt:N`, drift check
  strips the qualifier).
- **Fired timers woke attempt 1.** `timer.Scheduler` resolves the attempt
  through an optional reader that every composition left nil. The serve
  scheduler workload and the fixtures now pass `Attempts: runtime.Store{}`.
- **Disk exhaustion.** PostgreSQL panicked with "No space left on device"
  mid-wave: 1047 orphaned `hcmnext-pg-*` runtime directories and 45 stale
  lane build caches under %TEMP%, plus 2.2 GB of lane caches inside the
  repository root. Cleaned; `pgtest` now sweeps stale runtime directories
  older than two hours with no live postmaster before each start; the
  cache patterns are ignored.
- **Blocked terminal.** `end_blocked` moved from CANCELLED to COMPLETED
  (RUNNING → CANCELLED is an illegal instance transition); golden digest
  `186dcb38…`.
- **Two tests aligned with delivered behaviour.** The otelmw abandoned-stream
  span test now holds the stream for a measurable lifetime; the bootstrap
  double-decide test now expects APPROVAL-008's idempotent replay of an
  identical decision and a refusal of a contradicting one.

## 5. End state

- `test/workflow` green: nine promotion scenarios, PROMO-009 primary /
  golden / integration / fault / security, WF-RUN-016 repair execution.
- `test/acceptance` promotion matrix 24 of 24 PROVEN.
- Serve journey proof (`TestTodo_PROMO_EXEC_SERVE_ExecutePlanJourneyOverPGTest`)
  green; dev server rebuilt and READY at schema 69.
- P1A manifest re-signed over 62 migration files with recorded gaps
  9, 36, 39, 42 and 58–60; serve-graph and architecture goldens regenerated;
  coverage inventories current (16 untested files, all pre-existing).
- Held on purpose: GOVERN-001/002 (PRIV-001), WF-RUN-027 partial, the
  ADMISSION-001 chain behind WEDGE-005/006, and pre-existing policy failures
  in packages this wave did not touch.

## 6. 2026-09-06: company metadata tables, authorization, backfill, Gate A wave

Continuation of the same log, one day later, same verification rule.

- **Wave 37, company metadata and authz (38 Luna lanes).** Twenty-nine
  persistence todos landed one migration each (00070 to 00127, second slots
  released as recorded gaps), plus attestation responses (00128) and
  reference-dataset release and adoption (00130, 00131). Chains landed for
  AUTHN-004/009, ATTEST-004..008, CUSTOM-004..006, LOCATION-002/003,
  JOBARCH-002/003, COMM-001 and CROSS-CONF-002, ACCESS-003/004 with
  ARTIFACT-005, WF-STEP-011 with CONFIG-003 and REFDATA-001, and the
  ALIGN-008..015 table-governance tools. Twenty-two authorization items
  earlier lanes had built but never verified were ticked. Ticks 657 to 743.
- **What the re-verification caught.** Two migrations (00108, 00124) wrapped
  plpgsql and DO blocks without goose statement markers and broke every
  PostgreSQL test in the repository until annotated. Five stores failed their
  own suites (tenant fixture shape, a CHECK vocabulary mismatch, refusal
  ordering, a nil slice marshalled as JSON null, an idempotent replay that
  came back empty) and went to fix lanes. Fifty-six tables had to be
  registered in the storage disposition straight from the migrations because
  the lane reports used other formats. Running 38 lanes at once exhausted the
  machine (1186 processes, 135 PostgreSQL backends, `sleep` refusing to fork)
  and four lanes died silently; the cap is now about 20.
- **Backfill.** A review of 618 packages and 190 unmentioned files against
  the backlog produced twelve todos for delivered code that had none
  (TOOL-026, PROMO-010..013, WF-RUN-033, WORK-011, DATA-024/025, ARCH-GO-029,
  CONTACT-003, PROOF-003) and flipped four stale checkboxes (SECARCH-013/014/
  015, RULE-001). The evidence-freshness check wanted every evidence line's
  go test command in backticks; 431 lines were normalized and the tick writer
  fixed. Registry 1666, ticks 759.
- **Organization structure research.** `planning/research/
organization-structure-maximal-2026.md` and the 30-table draft
  `organization-structure-tables.sql` model the worst case (a provider tenant
  serving many clients, each with businesses, shells, joint ventures and
  franchises, co-employment, matrix reporting, cross-entity payroll groups,
  benefits adoption and billing). Not yet sliced into todos.
- **Gate A wave, paused mid-flight.** All 92 open Gate A todos were sliced
  into 28 lanes; 14 had landed green when the wave was paused to commit
  (wedge A/B, governance and privacy, integration and idempotency, commercial,
  operations and incidents, recovery, control plane, IaC A, export and RPC
  policy, transformation migration, endpoint harness, artifacts and
  onboarding, CI/CD and observability). The rest were still running at the
  pause and are committed as they stood.
- **Environment.** Disk fell to 6 GB twice; 121 stale embedded PostgreSQL
  servers were killed and 494 leaked cache directories plus the Go build cache
  removed (154 GB free). `go test` on this host now exits non-zero when it
  cannot unlink its own test binary even though every package prints `ok`;
  the landing driver and the tick verifier judge by result lines.
- **Commits after the pause.** The Gate A work went in as data, domain,
  governance, operations and transport commits (`14442de`, `aca251d`,
  `a13bb8b`, `5af163d`, `04aba0e`), with the UI and docs commits following
  once the front-end session's package compiled again. Lane scratch
  directories that leaked into the checkout (`.gocache*`, `.gotmp-*`,
  `tmp/`, 142 of them) are now ignored and swept before each commit.

## 7. 2026-09-06 (afternoon): authorization batch closed, security test wave

- **Authorization batch closed.** The thirteen backend authorization lanes
  launched earlier today all landed: authorized search and population
  freezing (SEARCH-001/003, PRIV-003), analysis-to-intent authority
  (INTENT-020), headcount and position authority separation (HEADCOUNT-001,
  MODEL-031), step-up on pay-method changes (PAYMETHOD-002), garnishment
  remittance authorization (GARN-005), row-security versus repository-scope
  parity (ALIGN-013), the authorized product-query envelope and bounded
  invalidation (ALIGN-022/023), operator-only freshness, workflow and outbox
  health (ALIGN-048..051), cross-surface noninterference (ALIGN-058/059),
  connector operation queue, journal and credential leases (CONN-RT-003/004,
  migrations 00235-00237), worker roles (SVC-006/009), readiness evidence
  (READINESS-001/002) and Start-time proposal derivation (WF-RUN-027).
  Ticks 871 -> 895.
- **Post-lane review.** A read-only review of the batch found nine real
  defects: a caller-supplied step-up bool and no tenant field in
  garnishment, a free-text validation override on the pay-method release
  gate, an in-memory catalog that never advanced its current destination,
  an invalidation emitter that trusted a caller-built authorization
  decision, id-keyed connector operations with no tenant check and no code
  writing the three new tables, aliased ALIGN-050/051 matrix tests, an
  assertion-free search integration test, a sequential "race" test, and a
  repository-scope parity test that skipped every repository finding. Each
  was folded into the security wave as a required fix and all are closed.
- **Security test wave.** Cam's next directive was to unit-test every
  authentication, authorization and security file. A per-file inventory
  (symbols declared per file versus symbols any package test references)
  covered 340 files in 110 packages: 7 files had no test reference at all
  and 120 had under half their symbols exercised. Twenty-five Luna lanes,
  one per package cluster (authn core, OIDC, issuer registry and federation,
  trust authz, sessions and step-up, trust core, custody and crypto agility,
  data classification and DLP, attestation and leases, consent and workload,
  the data stores in two lanes, access, tenant, proofing and redaction,
  privacy, abuse, money-moving authorization, operator authorization,
  connectivity, telemetry and time authority, table policy tools, planning
  gates, and the supply-chain tools in two lanes) were told the bar is per
  file, that a test must be able to fail, and that production code changes
  only to fix a defect a new test catches. After the wave: 3 files without a
  test reference (two are the repopath helper, one belongs to the front-end
  session), 56 under half (mostly lint tools outside scope), and referenced
  symbols 51% -> 67%.
- **Defects the new tests caught.** Twenty-two production fixes, each pinned:
  authn CheckDependent could never succeed; OIDC ID tokens and workload
  identity credentials accepted trailing JSON; trust/authz let foreign-tenant
  organization references and edges through; attest did not enforce attestor
  mode authority or minimum assurance; bundle accepted a root as a leaf
  issuer; attestation statements returned the wrong error for a missing
  assurance id; proofing stores aliased their evidence slices; the assurance
  register accepted a nil claim; provenance panicked on malformed keys and
  short signatures and accepted a statement with no source ref; the sbom
  license parser matched "license" as a prefix of "licenses"; plus the nine
  review findings above and further branch fixes in session, custody,
  cryptoagile, secrets, issuerregistry and governance/privacy.
- **Corpus repairs the wave surfaced.** The generated model package
  (`gen/go/hcmnext/model`) was deleted from the checkout twice by something
  outside this session and restored from git both times. The layer-graph
  golden lacked the signals store's workflow edge (landed at `14442de`); the
  vulnimpact golden pinned an older SBOM; the storeboundaries allowlist named
  a resolved pgstore finding while the scheduler's cross-tenant timer sweep
  was unreviewed; the authority gate found 61 completed GATE_C todos with no
  decision record, so known-defects now carries a GATE_C undecided gap; the
  P1A manifest was re-signed over migration 00239; sixteen live tables had
  no storage-disposition row (presentation preferences, signal disposition,
  schema upgrade control, telemetry registry and copy inventory, worker id
  policy, organization visibility, and the front-end session's role access
  and page permission tables).
- **Left for the front-end session.** Migration 00238 grants DELETE on
  worker_access_role_assignment to the application role, which the tenancy
  DB-017 integration test forbids on the data plane; that file belongs to
  the other session and is not edited here. The wave's data-store lanes
  added tests to `internal/data/roleaccessstore` and
  `internal/experience/roleaccess`, both untracked packages of that session;
  the tests pass and are left uncommitted with the packages.
- **Environment.** Twenty concurrent lanes ran cleanly this time; leaked
  `.codex-*` scratch directories at the repo root are now ignored and
  swept. Coverage logs are regenerated by a Python port of the inventory
  script (the PowerShell original is no longer in the tree).
- **Commits.** The security wave went in as data, trust, domain,
  operations, transport, policy and docs commits (`a7d621a`, `3180651`,
  `66318d1`, `85f7f5f`, `92ea605`, `c000a21`, `a40d672`, `d117687`). Cam
  then asked for everything else in the tree to be committed too, so the
  front-end session's work landed in three groups: access roles, page
  permissions and organization visibility stores with migrations 00182,
  00238 and 00239 (`d365555`); the journey RPCs for role access, page
  access and organization visibility (`6de21e2`); and the roles and
  organization visibility pages, history navigation and login personas
  (`cc9ecac`). The DELETE grant in 00238 and the journey-cell bootstrap
  test noted above are committed as they stood and remain that session's
  to resolve. The checkout is clean.

## 8. 2026-09-06 (evening): production UI regressions, job ladders and live Promotion proof

- **Responsive, authority-aware product UI (`2cf44c6`).** The product shell
  and its page compositions now omit actions the resolved page/action grant
  does not allow, while an active role with no configured organization policy
  fails closed to its own unit. The People directory owns sort, filter,
  pagination and page-size changes as component state: a refresh keeps the
  shell and scroll position mounted, scopes `aria-busy` and progress motion to
  the directory, and does not jump focus to the page heading. Responsive table
  actions, loading proxies, localized exact-money rendering and action-link
  refusal states were hardened across Home, Work, People, Person, Insights and
  Studio. The WASM router preserves focus for component-only query changes,
  resets the compact navigation rail correctly and focuses actionable journey
  refusals. `TestFrontendE2EPersonaLoginAndEveryProductRoute` exercises all
  registered product routes through the real HTTP handler for the four local
  personas; component, authorization, sort/performance, loading and navigation
  regressions sit with their owners. `npm run test:frontend` passes.
- **Immutable job architecture and governed ladder edges (`83afa89`).** A
  `PromotionPathRevision` now pins source and target job-profile revisions and
  validates UPWARD, LATERAL and CROSS_FAMILY graph semantics against a published
  architecture revision. Base-pay bounds use exact `values.Percentage` decimal
  fractions; the edge references a versioned compensation policy and benefit
  eligibility rules and cannot mutate benefit elections. The HarborCare
  fixture publishes People Operations, Engineering and Clinical families,
  ranked profiles, exact pay bands and the HRBP2 -> HRBP3 and SWE3 -> MGR1
  paths. `WorkforceOptions` exposes exact placement tuples and source-specific
  path options over the Journey gRPC contract. The client offers only the
  current profile's published next roles, fills the dependent grade, explains
  salary and benefit rules, and refuses an unsupported target before RPC; both
  proposal entry points repeat the exact server-side path/pay guard. Regression
  tests cover invalid graph shapes, missing policy versions, exact lower/upper
  pay bounds, unrelated but payable roles, cross-worker draft leakage and
  intermediate approvals that must not claim a terminal ledger write.
- **Due effective-date completion in local development (`b27f374`).** A live
  run found that `local-dev` enabled the execute plan and durable timer adapter
  but left its in-process scheduler disabled. The result was a truthful but
  indefinitely parked `WAITING_EFFECTIVE_DATE` instance even when the date was
  already due. The profile now enables the bounded scheduler with the executable
  plan; the standard profile remains opt-in. The same live instance was then
  fired and resumed from its durable timer, completed revalidation and effect
  observations, reached `end_complete` and wrote one governed outcome. The
  stale workflow refusal fixture was also moved from deprecated caller flags to
  durable approval/supersession fact ports.
- **Live evidence.** Intent
  `01a078ec-f38b-7458-bfa9-e0f800a5f72b`, instance
  `804e2abb-0595-5530-83b8-93e241f80905`, OPS-HRBP2/P2 -> OPS-HRBP3/P3,
  USD 93,000.00 -> USD 98,000.00 (+5.4%), effective 2026-09-06. The Codex
  browser showed both durable approvals, `wait_effective_date` SUCCEEDED,
  instance version 62/COMPLETED and exactly one
  `hcmnext.workflow.PromotionOutcome/v2` ledger row.
- **Verification.** `npm run check:code-style`, `npm run check:go`,
  `npm run test:frontend`, `go test ./internal/domains/jobarch
./internal/domains/fixtures`, `go test ./internal/intent/app
./internal/transport/journey ./internal/humanwork/workspace`, `go test
./tools/uxqual/journeyclient ./internal/humanwork/productui`, `go test
./test/workflow -count=1` and `npm run check:driftgate` pass. A broad
  `go test ./internal/application` remains red only in the composition-root
  policy test for concurrently edited `cmd/worker/roles.go` and
  `internal/domains/mobility/persistence.go`; the focused local-profile config
  tests pass, and neither unrelated file was changed in this group.
- **Backlog accounting.** `JOBARCH-004` and `PROMO-009` are complete with
  evidence. Persistent promotion-path rows and assignment profile pins remain
  `PERSIST-JOBARCH-002`; the authorized administration surface remains
  `UX-JOBARCH-001`; benefit eligibility execution remains `BEN-003`.

## 9. 2026-09-06 (night): persistent-route performance and an executable UI latency gate

- **Route and component performance (`d121105`).** Product navigation now
  replaces only the feature outlet under a persistent shell. Warm transitions
  reuse already-authorized baseline projections, refresh only the datasets a
  route requires and keep useful content mounted while a response is pending.
  People records carry an immutable normalized search/sort index, the generic
  data table precomputes its column map, and the Journey operational page caps
  its mounted workforce preview while retaining an explicitly selected worker.
  The full directory remains available through software navigation to People.
  Static WASM assets have deterministic gzip representations, ETags and cache
  revalidation; the compressed journey module is about 6.4 MB rather than the
  roughly 29.5 MB uncompressed transfer.
- **Executable interaction budgets (this commit).** The reusable
  `tools/uxqual/latencygate` package measures a warmed corpus with the
  nearest-rank p95 and produces actionable p50/p95/max/sample diagnostics on a
  breach. CI runs the wall-clock gate outside race instrumentation. Budgets are
  loading feedback <=16 ms; every registered leaf page <=50 ms; persistent
  shell <=50 ms; filtering, sorting, paginating and rendering 100 rows from
  10,000 indexed workers <=100 ms; a generic 1,000-by-12 table <=75 ms; and the
  bounded Journey workforce preview from 10,000 workers <=16 ms. Network and
  database SLOs remain separately measured boundaries rather than being hidden
  inside client-compute numbers.
- **Observed evidence.** Nine repeated large-directory runs reported p95
  between 40.8 and 75.5 ms, including a run during concurrent repository work.
  Across the complete repeated gate the slowest leaf
  stayed below 26 ms, the persistent shell below 18.1 ms, the 12,000-cell table
  below 19.5 ms, the loading proxy below 0.6 ms and the Journey preview below
  1.6 ms. This leaves headroom while retaining the 100 ms immediate-response
  ceiling for the most expensive local interaction.
- **Verification.** Three complete latency-gate repetitions and five additional
  10,000-worker repetitions pass. `go test -count=1 ./internal/humanwork/...
./tools/uxqual/... ./cmd/frontenddev ./cmd/hcmnext` passes. The percentile
  engine has independent tests for nearest-rank calculation, corpus immutability,
  operation failures, statistically weak sample sets and diagnostic budget
  failures. The local Windows/ARM64 host cannot run `go test -race` because CGO
  is unavailable; the timed suites carry `!race`, and Linux CI retains the
  repository's separate race correctness gate.

## 8. 2026-09-06 (evening): Gate A backend closure

- **Seven lanes, seven ticks.** The last backend Gate A todos went out as
  one lane each: WEDGE-003 (incumbent capability inventory), EP-PROMO-001,
  EP-WF-001, EP-OPS-001, EP-HEALTH-001 (the typed endpoints), SVC-005
  (scheduler timer and signal roles) and SVC-010 (worker messaging
  delivery). All landed and are ticked; backlog 895 -> 904. UX-003 and
  A11Y-001 are the only Gate A entries left and both belong to the
  front-end session. The signed Gate A decision record itself is still a
  human step; known-defects carries the undecided gap.
- **Regression caught.** SVC-005's tick failed because WF-RUN-027 (landed
  earlier today) made proposal and approval fact ports mandatory at Start,
  and the scheduler's SVC-004 fixture still started instances with
  caller-asserted flags. The fixture now supplies memory facts bound to the
  proposal's material digest; every other Start consumer (scheduler,
  worker, execute, test/workflow) was already green.
- **Review findings, fixed.** A read-only review of the seven lanes found
  that the operations endpoint had no production store at all (only the
  in-memory test double, so every deployed call would return unavailable),
  that workflow inspection over HTTP had no reader wired, that the cursor
  signing key fell back to a checked-in literal, that five matrix tests
  were aliases of one projection test, that the race tests ran
  sequentially, and that the durable delivery store had only two
  input-validation tests. Two fix lanes closed all of it: a PostgreSQL
  operation store on migration 00261 threaded through both edges, the HTTP
  edge given the workflow reader, a fail-closed cursor key sourced from
  configuration, real paging, forgery, replay and concurrency tests,
  embedded-PostgreSQL tests for delivery claims, receipts and replay, a
  serve-level test that the signal role needs the queue lease, and the old
  disclosing bootstrap health handler deleted.
- **Architecture rules the wave surfaced.** ARCH-GO-022 had three
  pre-existing unallowlisted cross-plane imports (execute -> messaging,
  migrate/artifacts and promotionexec -> humanwork); they are allowlisted
  with rationale. ARCH-GO-027 flagged four one-method interfaces with no
  consumer; each gained a real consumer with its own test rather than an
  exception. The composition-root rule flagged the worker importing
  internal/capability directly and the mobility store keeping state in a
  package-level registry; both fixed. The front-end session's
  workspace/assets.go carries the same registry pattern and is left to that
  session.
- **Environment.** Dev database at schema 261; server, scheduler and worker
  binaries rebuilt.
- **Commits.** Data `c26f730`, domain `725ef15`, operations `e885a59`,
  transport `6d81163`, policy `c2fdf94`, and this docs commit.

## 9. 2026-09-06 (night): quality gates and agent configuration

- **Four gates, one hook.** Cam asked for format, lint, unit tests and a 70%
  coverage floor to hold before every commit. Format and lint were already in
  the husky hook (prettier, eslint, gofmt, go vet, the house code-style rules
  and the policy suites); what was missing was Go unit tests and coverage. A
  new tool, `tools/quality/covergate`, runs `go test -cover` on every package
  holding a staged Go file, parses the result lines (so the Windows unlink
  exit is not a failure), and refuses a failing test, a package with no test
  files, or a package under the floor in
  `definitions/toolchain/coverage-gate.yaml`. Exceptions are exact-path,
  kind-specific, owner-bearing and expiring; a prefix or wildcard is refused.
  `npm run check:coverage:staged` sits in the hook after `check:go`;
  `npm run check:coverage` gates the whole module in CI. Root `go test ./...`
  stays off the local hook because every data package starts an embedded
  PostgreSQL.
- **Agent configuration.** `AGENTS.md` is now the authoritative instruction
  file (who decides what, the gates, the task lifecycle, the Go rules, lane
  ownership, git discipline, environment traps), `CLAUDE.md` a thin pointer
  to it, and `.claude/` carries the permissions (gates and read-only git
  allowed; `--no-verify`, stash, hard reset, amend, rebase and push denied),
  a gate-runner subagent, and the Karpathy guidelines skill vendored verbatim
  from the same pinned commit CodeFlux uses, with the provenance note
  adjusted and the file exempted from prettier so it stays byte-for-byte.
- **Known state.** `tools/quality`'s TOOL-012 race tests fail locally because
  this host has no race detector; CI runs them on Linux. The first
  whole-module coverage run is seeding the exception list for packages that
  are genuinely under the floor today.
- **Baseline.** The first whole-module run measured every package: 175 are
  under the 70% floor today (95 between 60 and 70, 34 between 50 and 60, 21
  between 30 and 50, 25 under 30; heaviest in internal/data, internal/domains
  and tools/policy), none lack tests, and 62 reported failures under the
  load of nine hundred packages at once, which are being rerun with limited
  parallelism to separate real breakage from contention. Every under-floor
  package now carries an exact-path, expiring `below_floor` exception owned
  by the backlog with its measured percentage in the reason, so the gate
  holds the line from here without pretending the baseline is green; each
  exception retires when its package reaches the floor.
- **Corpus repair.** Of the 62 packages that failed under whole-module
  load, 42 failed alone. The planning half was one root cause: the todo
  registry parser only recognised evidence fields dated 2026-09-03, so
  every later tick was invisible to traceability (649 "orphans"), and three
  front-end todos added without TEST MATRIX, REFACTOR, Refs, a declared role
  or existing dependencies broke every planning parser. The parser now
  accepts any dated evidence field and keeps all of them; the entries are
  completed; the registry is regenerated at 1669; 38 evidence lines that
  omitted their primary test name now carry it, two named tests that never
  existed, and 24 front-end ticks that had no evidence line at all carry
  one recorded after running their packages. Definitions caught up too:
  twenty-one internal package roots and the frontenddev command declared,
  two table owners repointed to packages that exist, the capability
  coverage matrix and control crosswalk goldens regenerated, and the parity
  tests now expect the reviewed scheduler timer sweep. Seven front-end ticks
  (UX-002, UX-006, UX-007, UX-008, ADMIN-007, CLIENT-001, CLIENT-002) remain
  without evidence because their named tests do not exist or their package
  fails; those are that session's to close. The data-plane failures
  (provenance and ledger outbox identity conflicts, blank signal keys in
  runtimestate, workforce fixtures, pseudonym custody, jobarch digest,
  commercial fingerprint, pgtest rollback pin, deferred schema preview) and
  the policy-tool pins are in two fix lanes.
