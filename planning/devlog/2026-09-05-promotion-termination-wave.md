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
