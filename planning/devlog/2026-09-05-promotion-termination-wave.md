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
