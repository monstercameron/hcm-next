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
- **Fix lanes landed.** The data-plane lane's fixes verified green in all
  eleven packages once I corrected the workforce fixture it had left alone
  (migration 00181 made worker numbers unique per tenant). The policy lane
  re-pinned the capability-binding allowlist and wire-method count, moved
  the transformation engine off the direct decimal import, refreshed the
  race-policy, release-admission, gensources, table-inventory and phase-one
  goldens, quarantined the skipped tests, declared the two detached test
  harnesses, regenerated the generator outputs, and made the TOOL-012 race
  tests skip where no race detector exists. The contact domain now
  normalises Unicode through a kernel helper instead of importing x/text,
  and the dependency roles declare the reservation store's uuid use and the
  east-west transport's manifest parser. What the library firewall still
  reports is the front-end session's: experience/i18n, productui,
  uicomponents, workspace and the uxqual tools importing x/text,
  GoWebComponents and x/net outside their declared roots.
- **One artifact root.** Cam asked for the temp, cache and stale artifacts
  cleared and every build output routed to one place. Fifty-three orphaned
  embedded PostgreSQL processes across twenty-one runtimes were stopped (the
  dev PostgreSQL 17 service kept), their runtime directories and the stale
  Go build and cache directories removed from the user temp folder, and the
  scratch entries at the repository root deleted. `.artifacts/` is now the
  sole root: `bin/` for binaries (`scripts/build.sh`), `lanes/` for lane
  briefs and reports, `coverage/`, `tmp/` for Go temp and test binaries,
  `gocache/` for lane build caches and `pg/` for the embedded PostgreSQL
  cache. The pre-commit hook and the lane launcher export `GOTMPDIR`, `TMP`,
  `TEMP` and `HCMNEXT_TEST_PG_CACHE` to those paths so the leaks stop at the
  source instead of being swept after the fact.

## 10. 2026-09-07: frontend delivery gates

- **WEB-031 browser state.** The production router now stores only a random,
  bounded per-tab history-ledger identifier and index. Credentials, authority,
  business records, configuration truth and authorization-shaped values are
  rejected by the reviewed adapter and recursive AST gate. Native and actual
  `js/wasm` fault suites exercised hostile browser getters and methods; Codex
  browser QA covered reload plus People/My Work back-forward traversal without
  replacing the persistent shell. The validation benchmark was 143.9 ns/op,
  7 B/op and 0 allocs/op.
- **WEB-032 asset integrity.** A deterministic generated manifest now binds
  the exact routable shim and WASM identities and gzip representations with
  SHA-256 and SRI. The bounded parser and handler constructor reject missing,
  stale, extra, orphaned, reordered, malformed, empty, oversized or mutated
  catalogues before serving. Authenticated same-origin loaders integrity-check
  both executable assets, while request handling uses pre-indexed content type,
  ETag and SRI metadata. Deterministic gzip packaging publishes synchronized
  temporary replacements. Windows does not provide a bundle-wide atomic
  replacement for all five files; exact startup validation is therefore the
  fail-closed interruption boundary. The interaction p95 was 1.0004 ms against
  2 ms, and the build/startup benchmark was 1.43-1.51 ms/op, 820,153 B/op and
  1,033 allocs/op outside the request path.
- **Adversarial refinement.** Luna produced each first implementation; Sol
  found and drove fixes for missing bearer propagation, non-routable source
  assets, permissive manifest bounds and canonicalization, duplicate matrix
  cases, and per-render parsing. The orchestrator fixed a shallow-copy alias in
  the final mutation test. Both todos passed their exact seven-test matrices,
  focused package tests, vet and the full repository hook.
- **Internationalization and accessibility.** Every frontend todo is now
  accepted only after the registered-page gate passes all 17 pages in `en-US`,
  `de-DE` and RTL `ar` (51 cases). It verifies document language/direction,
  unresolved keys, ID and ARIA references, accessible control names, keyboard
  semantics, reduced motion and form-error association. This is a deterministic
  regression gate, not a claim that semantic translation or assistive-technology
  conformance is complete.
- **WEB-033 production CSP.** Product, journey, legacy workspace, login,
  refusal, redirect and SSR documents now share one typed deny-by-default
  policy builder. Sol's adversarial pass removed the initial draft's CSP2
  same-origin script bypass and origin-wide connection allowance: executable
  sources are the exact hash-pinned loader, SRI-verified blob shim and narrow
  WASM compilation token, while network access is limited to the canonical
  host's asset prefix and exact gRPC tunnel path. Malformed hosts and hashes,
  inline event/style attributes, redirects, errors and cached responses have
  regression coverage. DataTable column widths use a closed CSS class contract
  instead of runtime style attributes. The exact rebuilt Go/WASM bundle logged
  in through the Codex browser, mounted, navigated Home to People and returned
  via in-app history with no browser diagnostics. The 51-case i18n/accessibility
  gate passed; policy construction measured p95 513.5 us under 2 ms and
  11.7-12.1 us/op in the three-policy benchmark. Trusted Types remains an
  explicit upstream blocker: GWC v5's private serialized-subtree fast paths
  write `template.innerHTML`, so enforcement requires a named `TrustedHTML`
  policy or removal of those sinks before it can be enabled safely.

## 11. 2026-09-07: backend performance wave

- **Where the time actually went.** The 32 existing benchmarks are all
  sub-millisecond in-process work, so the baseline run was not where the
  cost was. Reading the request and storage paths found it: every pool
  acquire ran three hygiene round trips plus one `set_config` per runtime
  parameter, and every `Pool.Query` acquires; the edge middleware and the
  connect interceptor each ran the credential verifier; twenty-two stores
  issued one statement per row inside loops, including the ledger
  multi-stream append; and a textual scan found 218 foreign keys with no
  supporting index.
- **Six atomic todos, six Luna lanes.** `PERFOPT-001` to `006` were written
  with the benchmark that proves each change and the behaviour that must not
  move, then implemented on disjoint file roots. Every lane was reviewed by
  reading the diff, not the report.
- **What the review changed.** The regex lane had aliased `regexp.MustCompile`
  into package variables so its own AST checker would pass; the checker now
  reports aliases and exempts only lines marked `regexhoist:dynamic`, the
  aliases are gone, and the quality FORMAT rule (evaluated per record) got a
  bounded pattern cache. The batch lane had copied the 21-column
  `ledger_event` insert into the multi-stream path; both appends now build
  their statements through one `planEvent`, and the repeated failed-index
  arithmetic became `dbport.FailedStatement`. Its receipt golden was
  hand-typed because embedded PostgreSQL will not start under the lane
  sandbox (restricted-token error 87); it was regenerated from the real
  server. The allocation lane hashed strings through `unsafe`; a reused
  scratch buffer does the same without it, and its zero-padded path
  formatter now matches `%04d` for every input. The pool lane found that
  PostgreSQL refuses `DISCARD ALL` inside the implicit transaction of a
  multi-statement message and spelled out its components, but omitted
  `SET SESSION AUTHORIZATION DEFAULT`; it is back, pinned by a spec test.
- **Proof of no behaviour change.** The goldens for the envelope, cycle
  explanation, evidence package and legal extractor were run against the
  pre-change code in a throwaway HEAD worktree and pass unchanged. The
  existing ledger, checkpoint, intentcontrol, outbox and pgxadapter suites
  pass against the embedded server; the checkpoint failures seen while four
  embedded servers ran at once did not reproduce alone.
- **Numbers.** Acquire and release 139,966 to 40,367 ns/op; `QueryRow`
  170,290 to 91,901 ns/op; edge request 50,456 to 34,751 ns/op; statements
  per three-stream ten-event append 51 to 33; allocs/op 12 to 8 (envelope),
  209 to 152 (evidence), 104 to 71 (cycle); 65 indexes added, 22 foreign
  keys left unindexed by recorded decision.
- **Commits.** `bf4cbcc` regex hoisting and the AST check; `e9b88c0` edge
  single admission plus envelope and cycle allocations; `a42586a` pool
  hygiene and the batch port; `86093e3` the gate reports failure reasons;
  `e77d7f9` the pgtest admin-connection and role-creation fixes together
  with the ledger, intentcontrol and outbox batching (the batching group
  was still staged from a refused attempt when the pgtest group was
  committed, and the combined commit passed the full gates); `2e22af5`
  the foreign-key index audit and migration 00262; `b4c458e` the
  securebydesign scanner skips dot directories (the same fix for the
  traceability scanner waits on that package's real-corpus test, red on
  the front-end session's evidence lines).
- **What the gate refusals taught.** Two failures appeared only while the
  hook ran five database packages at once beside the other session's
  hook: a schema drop overran its 60-second deadline and pgx closed the
  shared admin connection, so every later CREATE SCHEMA in the package
  failed with "conn closed"; and two sessions applying migration 00008
  at once could leave the second without the role the first had not yet
  committed. The helper now reconnects and gives drops five minutes, the
  migration takes a transaction advisory lock, and the gate prints the
  failing test's own lines so the next refusal explains itself.
- **Completed-but-unticked todos.** A scan of the 751 open todos found 38
  whose `TestTodo_<ID>` already existed; 36 (the two `tools/uxqual` ones are
  the front-end session's) had every declared matrix test present, none
  skipped, and passed individually and as full packages, so they are ticked
  with evidence naming the tests, the commit that wrote them and the run.
  The one package failure was environmental: `sbom.ModGraph` ran `go mod
graph` in a temp directory that now lives under the checkout and so
  resolved the repository module; it requires a go.mod at its root.
- **Left open.** The remaining row-at-a-time loops (jobarchstore, budgetstore,
  contentregistrystore, recordsmeta, contactstore, meritstore, workeridstore,
  seed) can move to `dbport.ExecAll` the same way; `transport.Validate`'s
  reflective walk was not profiled; `PERF-002` and `PERF-008` still own the
  end-to-end latency targets and the regression gate.

## 12. 2026-09-07: WEB-034 qualified browser RPC adapter

- **One transport seam.** `journeyclient.RPCAdapter` wraps the generated
  Journey gRPC client surface through `grpc.ClientConnInterface`; it does not
  duplicate workflow, authorization or persistence decisions. The large
  implementation was separated from the thin service composition seam so
  transport qualification remains independently testable.
- **Fail-closed contract.** Startup cross-checks every generated and protobuf
  descriptor. Calls accept only the exact generated request and response
  types for the named method, bounded protobuf bodies, a closed metadata set
  and approved call options. Missing or malformed bearer credentials,
  authority injection, typed nils, path/stream mismatches and expired
  contexts are rejected before dispatch. Short caller deadlines and
  cancellation are preserved, long calls are capped, upstream status details
  survive intact, and adapter-owned stream contexts are released on EOF and
  every error path.
- **Real execution proof.** The native seven-test matrix and full package,
  focused vet, actual js/wasm test binary under Go's Node harness, and the
  17-page x 3-locale i18n/accessibility gate passed. A tunnel integration test
  ran unary and cancellable streaming RPCs through the real GoGRPCBridge
  WebSocket tunnel against embedded PostgreSQL. Windows reported only the
  repository-documented post-PASS test-binary cleanup lock.
- **Browser and performance proof.** The exact rebuilt Go/WASM asset was
  served through the local gateway and manually inspected in the Codex
  browser. Authenticated Organization, Work and People routes loaded live
  authorized data, including 64 people, and the browser emitted no warnings
  or errors. Five latency repetitions passed the 2 ms gate; measured p95 was
  526.8 us and invocation benchmarks were 1.58-3.94 us/op, 970-973 B/op and
  13 allocations.
- **Shared-tree incident.** The full repository hook completed every gate,
  but another agent advanced HEAD before Git could install the planned
  frontend commit. Its concurrent shared-index commit `a42586a` therefore
  contains WEB-034 beside unrelated pgxadapter work. The implementation is
  safely landed; shared history was not rewritten merely to improve grouping.

## 13. 2026-09-07: WEB-035 filtered invalidation client

- **Hints never become product truth.** The client accepts only the bounded
  canonical invalidation protobuf, filters it against the active tenant,
  projection and authorized-subject scope, then invokes the qualified RPC
  refetch seam. Sequence, watermark and revision cursors advance only after
  that authoritative refetch succeeds.
- **Bounded lifecycle.** One live generation owns a closeable stream, bounded
  refresh and observer queues, cancellation and exactly-once shutdown. Failed,
  cancelled and queue-full work remains retryable. Observability is serialized,
  asynchronous, bounded, panic-contained and identifier-free.
- **Real browser boundary.** The js/wasm adapter uses the browser WebSocket API,
  accepts only same-origin `ws`/`wss` URLs and bounded complete text frames,
  fails closed on binary, overflow and socket errors, and releases every JS
  listener and callback. Native and actual Node/WASM matrix tests passed.
- **Qualification.** The seven exact matrix tests, full focused packages, vet,
  five latency repetitions, the registered i18n/accessibility gate and the
  full repository hook passed. Native p95 was 509.6 us and WASM p95 847.1 us
  against 2 ms; the benchmark measured 20.8-27.8 us/op, about 6.7 KB/op and
  70 allocations. Journeys and People were inspected in the Codex browser
  without diagnostic warnings or errors.
- **Honest boundary.** Commit `f29b156` supplies an injectable client and real
  browser adapter. The repository has no canonical product-invalidation
  endpoint, authentication/subprotocol contract or HTTP shadow, so this change
  does not fabricate one or falsely claim a live subscription. WEB-036 owns
  sequence-based reconnect and cursor catch-up over the governed seam.

## 14. 2026-09-07: WEB-036 reconnect and authoritative catch-up

- **Committed progress only.** Reconnect opens from a private read-only local
  checkpoint. A gap invokes the injected authoritative catch-up and admits the
  later hint only after catch-up succeeds; the hint's own refetch must then
  succeed before its sequence, watermark and revisions commit.
- **No borrowed authority.** The checkpoint has no public fields or wire
  encoding. It is not the signed opaque cursor owned by the server streaming
  contract, and the WASM package does not import signing keys or pretend a bare
  sequence authenticates anything. Catch-up/refetch must re-authorize through
  their owning RPC.
- **Bounded recovery.** Exponential backoff and an attempt ceiling bound
  consecutive failed generations. Useful committed progress resets that
  budget; every later reconnect still waits, preventing clean-EOF hot loops.
  Processing is sequential across catch-up and refetch, cancellation interrupts
  open/backoff/receive/cooperative callbacks, and each transport closes once
  before replacement.
- **Adversarial repairs.** The initial pass exposed public forgeable cursor
  fields, exhausted healthy long-lived sessions, raced later hints past pending
  refetches, wrapped callbacks in leakable goroutines, overflowed `sequence+1`
  gap math and published reconnect events nondeterministically. The Sol pass
  corrected each issue and added hostile lifecycle, scope-renewal and cursor
  contradiction coverage.
- **Qualification.** Commit `d225e01` passed the seven exact matrix tests,
  WEB-035 regressions, full focused packages, vet, 86.2% coverage, five latency
  repetitions and the full repository hook. A fresh js/wasm binary ran both
  browser tests under Go's Node runtime. Native p95 was 0-512.5 us against
  2 ms; the root benchmark measured 35.2-35.6 us/op, 10,186-10,188 B/op and
  112 allocations. The unchanged local product remained visually stable in
  the Codex browser with no warning/error diagnostics. No rendered or localized
  output changed, and Windows race execution remained unavailable with CGO off.
- **Still deliberately absent.** The repository has no canonical product
  invalidation endpoint, authentication/subprotocol contract or server-issued
  cursor in that protocol. This closes the injectable recovery contract, not a
  fabricated live subscription.

## 15. 2026-09-07: WEB-037 stable hydrated application shell

- **Stable production boundary.** The product starts by hydrating the existing
  server-rendered GoWebComponents shell rather than deleting it. HistoryRouter
  owns the page outlet while the banner, authorization-resolved navigation,
  main region and live region retain their DOM identities and component state.
- **Runtime repairs.** The initial implementation exposed two browser defects:
  startup cleared SSR markup before mounting, and route factories called
  hook-using renderers outside a component context. The Sol pass replaced the
  destructive mount with `HydrateMount` and added explicit component boundaries
  around both the shell layout and page content.
- **Actual WASM regression coverage.** The compiled js/wasm test seeds and
  hydrates shell markup, navigates Home to People to Settings through the real
  production factories, proves that only the outlet changes, preserves a local
  `UseState` marker, rejects reloads and duplicate announcements, and cancels a
  stale People loader generation.
- **Qualification.** Commit `7570f57` passed the exact four-test matrix, full
  product UI, native and js/wasm vet, actual Node/WASM execution, five latency
  repetitions, benchmark, formatting/diff checks and the full repository hook.
  Stable-shell p95 was 2.036-9.556 ms against 16 ms; the benchmark measured
  0.884-1.266 ms/op, about 744 KB/op and 3,752 allocations. The registered
  17-page by 3-locale i18n/accessibility, RTL and theme gates passed. Manual
  Codex-browser navigation kept the shell visually stable with no warnings or
  errors.

## 16. 2026-09-07: WEB-038 authorized tenant and acting context

- **Exact server-resolved choices.** The switcher receives a bounded catalogue
  of complete tenant and acting-authority pairs. Tenant and authority are never
  separate pickers, so the browser cannot manufacture an unauthorized
  cross-product. Delegation, delegator, expiry and elevated state remain
  explicit presentation facts, not authority inputs.
- **Narrow component boundary.** Adversarial review found that the first result
  type carried an entire replacement `View` through component props. The Sol
  pass replaced it with a bounded receipt and added the props to the recursive
  architecture guard. The injected adapter now privately stages the full view
  behind an opaque non-authoritative reference, verifies the receipt, clears
  old tenant/principal scoped state and adopts the replacement transactionally.
  Failed or panicking commits roll back; stale overlapping exchanges cannot
  commit after a newer generation begins.
- **Fail-closed interaction.** Same-context selection is a no-op. Foreign,
  malformed, duplicated, oversized, partial and stale results are rejected;
  transport and commit errors remain generic. Native disclosure, list and
  button semantics preserve keyboard operation, focus restoration and live
  status while English, German and Arabic/RTL copy uses the shared catalogue.
- **Qualification.** Commit `1346552` passed the exact four-test matrix, full
  product UI, native and js/wasm vet, actual Node/WASM WEB-037/038 execution,
  reflection architecture guard, registered-page i18n/accessibility, theme,
  token, qualification and WCAG gates, scoped formatting/diff checks and the
  full repository hook. Five latency runs measured p95 508.9-538.8 us against
  2 ms; the benchmark measured 90.3-100.5 us/op, 41,958 B/op and 321
  allocations.
- **Visual refinement and honest limit.** Desktop, 390 px and 320 px light and
  dark layouts were inspected in the Codex browser. The review exposed a mobile
  search/panel overlap, which was fixed; diagnostics stayed clean. There is no
  authoritative backend context-exchange RPC yet, so the production shell must
  receive the documented authenticated adapter. No endpoint or browser-side
  authority was invented to make the component appear more complete.

## 17. 2026-09-07: WEB-039 authorization-resolved primary navigation

- **Projection, not browser authority.** The primary and support navigation now
  consume a bounded, versioned resolver result. Once supplied, it remains the
  complete answer across locale changes, favorites, fuzzy search, shell
  shortcuts and registry or role fallbacks. Empty, denied and malformed answers
  render no destination rather than reviving defaults.
- **Canonical and hostile-input boundary.** Validation charges depth, fan-out
  and total-node limits before recursive copying, requires positive versions,
  and accepts only exact registered page, route, label-key and icon
  relationships. Queries, fragments, traversal, foreign routes, control/bidi
  characters, cross-page metadata and unauthorized duplicate favorites fail
  closed. Resolver display strings are replaced with trusted localized registry
  presentation, while destination RPC and page authorization remain separate
  enforcement boundaries.
- **Stable component behavior.** Existing software navigation, grouped
  disclosures, fuzzy menu filtering, favorites, Person-as-People active state,
  SSR/WASM parity and WEB-037 shell identity are preserved. Component props are
  reflection-guarded narrow records and contain neither page-wide views nor
  credentials or authorization decisions.
- **Qualification and refinement.** Commits `04e8ffe` and `e431b76` passed the
  exact five-test matrix, including its named security gate, full product UI,
  native and js/wasm vet, a freshly compiled Node/WASM
  WEB-037/038/039 run, token/qualification/WCAG gates, formatting/diff checks and
  the full repository hook. Five latency runs measured p95 511.3 us-1.7641 ms
  against 5 ms; the benchmark measured 195.5-223.7 us/op, about 104,881 B/op
  and 1,262 allocations. Codex-browser inspection covered desktop, 390 px and
  320 px light/dark layouts. It exposed and fixed ambiguous authorization-empty
  copy and a truncated narrow fallback wordmark; browser diagnostics were clean.
