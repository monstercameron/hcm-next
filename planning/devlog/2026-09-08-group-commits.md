# Devlog 2026-09-08 — lane-work group commits

## What this entry covers

The shared worktree held 337 changed files from parallel lanes (plus my own
evidence-repair edits). Per the user's "commit the code in groups" instruction
I landed them as area groups through the pre-commit hook, updated the
CHANGELOG markers, repaired the planning-corpus traceability evidence, and
recorded what stayed red and why. AGENTS.md group order: policy, data,
domain, trust, operations, transport, ui, docs.

## Commits

- policy: `185f252f` — governance/legal, gate tooling, architecture
  definitions, CI decomposition gate, lint configs.
- data: `2133aa41` — stores, retry budgets, migrations 00262–00280,
  NewestReversibleVersion fix.
- domain: `2dbc1db6` — lane domain logic, intent and transaction planes.
- trust: `f895257c` — agent-security gateway (AGENT-002).
- operations A: `d790d98c` — application serve wiring, admission, worker.
- operations B: `a97b9cd6` — workflow runtime, engines, platform roles.
- transport: `13f9f402` — journey handler (repairs HEAD build break),
  regenerated contract.
- ui: `6ec0e1a8` — WEB-041–043 matrix suites.
- docs: this commit — changelog, todos evidence repair, devlogs.

## Defects found while committing

1. Stale duplicate risk (cleared): three untracked
   `internal/humanwork/productui/web04[123]_*_test.go` files re-declared the
   `TestTodo_WEB_041/042/043` suites. They turned out to be additive (the
   committed coverage lives under different names), `go vet` is clean, and
   all 12 suites pass — they ship in the ui commit.
2. Fabricated benchmark names in WEB-030/WEB-031 evidence (fixed):
   `BenchmarkResolvedCanonicalHref` and `BenchmarkHistoryLedgerIDValidation`
   never existed in any commit (`git log -S` finds them only in the docs
   commits that cite them). Replaced with the real benchmarks
   (`BenchmarkProductResolvedRouteCanonicalize`,
   `BenchmarkBrowserStateStorageBoundary`) and re-measured numbers on
   2026-09-08 (20061 ns/op / 37785 B/op / 136 allocs/op;
   150.0 ns/op / 0 B/op / 0 allocs/op).
3. Systematically wrong matrix names in five lane evidence lines (fixed):
   `TestTodo_<ID>_Property`, `_Golden`, … expands to
   `TestTodo_<ID>_Property_Golden`, which does not exist. All 35 real
   `TestTodo_<ID>_{Property,Golden,Race,Fault,Security,Conformance,Mutation}`
   tests exist and their packages pass; evidence now uses the brace form.
   TAXPROFILE-002's prose form fixed the same way.
4. Missing Evidence fields on 188 done todos (fixed): WEB-041–240 (except
   WEB-121, which had one) and ADMIN-007 cited no Evidence field, so the
   traceability gate flagged them. Added dated lines naming only tests
   verified present by scan; every PASS claim backed by a suite run in this
   session (full productui suite, productclient, six lane domain packages,
   both workspace packages for UX-002).
5. TEST MATRIX over-promises (reported, not fixed): WEB-056–064 and
   WEB-193–204 declare `SECURITY=` tests that do not exist; WEB-229–236
   declare `SECURITY=`/`INTEGRATION=`/`FAULT=` tests that do not exist.
   Evidence cites the existing subset; the missing variants are a coverage
   gap for the owning lanes.
6. Irreversible-tip rollback red (fixed at source): new migrations 00279
   and 00280 deliberately refuse Down (durable evidence, covered by
   `TestMigrationDownRefusesToEraseBudgetEvidence`), but `TestTodo_DB_001`
   and `TestTodo_FX_003_{Integration,Fault}` rolled back the stack tip and
   went red. Added `migrations.NewestReversibleVersion()` plus a unit test;
   the three tests now cycle down/up at the newest reversible version on a
   fresh schema. Failure surfaced by the staged covergate gate, which
   correctly blocked the data commit twice before the fix.
7. Load flake (observed, not a product bug):
   `TestOutboxConsumerHandlerIsIdempotentByMessageID` failed once under a
   full-package run (465s wall clock, 3s delivery deadline exceeded) and
   passes alone (15s) and in an earlier full run. Re-ran the data commit;
   killed the never-healthy Docker Desktop backend to free the box.
8. PG deadline flakes under parallel load (observed, tests green alone):
   `TestTodo_ADMISSION_002_ComposedRetryFactoryRevalidatesPersistedBudgetBeforeStart`
   and `TestTodo_DB_EDGE_003_...RechecksApproval` exceed internal context
   deadlines only when the staged gate runs many PG packages at once
   (~100s vs ~15s alone, both pass in isolation). Split the operations
   group in two to halve gate parallelism rather than re-rolling.
9. Scheduler exactly-once race (held back, pre-existing flake):
   `TestTodo_SVC_004_Race` demands exactly-once dispatch across two
   racing replicas and fails intermittently on clean HEAD too (1/3) as
   well as with the lane's claim-helper hunk (2/3), so the race predates
   the change. `scheduler.go` stays uncommitted for owner investigation;
   the Claim helper itself already landed with the data group and the
   old Transition call-site keeps the tree consistent.

## Decisions taken

- The P1A manifest (`definitions/planning/gates/p1a-manifest.yaml`) lags 19
  migration files but is Ed25519-signed with "do not hand-edit without
  re-signing", and I hold no key. Left untouched; the key holder must
  re-sign after the data commit. `tools/planning/gateevidence` Go changes
  stay uncommitted for the same reason (their staged-gate suite compares
  manifest against the live FS).
- `tools/planning/traceability` Go changes stay uncommitted: 5 remaining
  corpus orphans (CLIENT-001/002, UX-006/007/008) cite tests that never
  existed anywhere in history. Writing tests for others' old todos would
  invent coverage; reopening their ticks would rewrite others' closure.
  Orphan count went 261 (HEAD) → 5 (worktree) without either.
- `test/workspace` has two failures (UX_004_Browser, live-cell CSP subtest)
  asserting the WASM enhancement that commit 60ca21b9 deliberately disabled
  for security (fixture rebuilds the form sans CSRF). The tests went stale
  because the package could not build on HEAD (transport references an
  uncommitted handler). Not mine to resolve silently: re-enabling would
  reopen the CSRF hole; rewriting the assertions needs a product decision.
- Per-commit hook strategy: the full hook passed on the complete worktree
  before grouping; each group commit runs the hook (which re-runs the full
  suite), with one final full-tree hook re-run on the clean tree at the end.

## Verified

- `sh .husky/pre-commit` exit 0 on the full worktree (format, lint, unit,
  coverage, drift, API, substrate, race, nested tests, build).
- `go test -count=1 ./internal/humanwork/productui/` PASS (30.5s).
- `go test -count=1 ./tools/uxqual/productclient/` PASS.
- kernel/values + domains fx/merit/mobility/service/taxprofile PASS.
- `go test -count=1 -run 'TestTodo_UX_002'
./internal/humanwork/workspace/ ./test/workspace/` PASS.
- `go test -count=1 ./tools/planning/intentmanifests/` PASS.
- Traceability orphans: 261 on clean HEAD → 5 in worktree, all five
  pre-existing untestable ticks.

## Left partial / needs a decision

- P1A manifest re-sign (key holder).
- gateevidence + traceability Go files uncommitted (blocked on the above).
- 5 ancient orphans (CLIENT-001/002, UX-006/007/008): no tests exist.
- 2 stale test/workspace assertions vs the deliberate no-enhancement
  fallback (product/security decision).
- Missing matrix test variants for WEB-056–064, WEB-193–204, WEB-229–236.
- Docker-in-WSL still needs `sudo apt-get install -y uidmap iptables
slirp4netns` from the user; Postgres runs unprivileged from ~/pgroot.

## OBS-016 correlation (2026-09-08, uncommitted)

- RED: `hcmotel.ExemplarsOf` undefined; `correlation` package absent.
- GREEN: new `internal/platform/telemetry/correlation` (`Correlation`,
  `Join`/`JoinScope`/`JoinTicket`, `CheckMetricLabel`, `LogFields`,
  context helpers) with the six-test OBS-016 matrix; `effect.dispatch.
duration` histogram in the Go catalog + YAML mirror + OTel adapter
  instrument + `RecordEffectDispatchLatency` + `ExemplarsOf`, with two
  exemplar tests proving real-SDK trace linkage and the unsampled-empty
  case. Catalog growth flowed into `DefaultRequiredSignals` by
  construction; OBS-002 fixtures updated to emit all six instruments.
- Verification: `go build ./...`, `go vet
./internal/platform/telemetry/...`, `go test -count=1
./internal/platform/telemetry/...` all PASS.
- STAGED, commit blocked on environment (this devlog + CHANGELOG +
  todos staged in the same atomic set): correlation pkg (3 files), otel
  metrics + tests, catalog YAML, regenerated archdoc testdata,
  substratecoverage ownership row. `git commit` runs but the husky hook
  cannot start: WSL interop is down in this shell (Windows binaries fail
  with Exec format error; binfmt_misc lacks WSLInterop; uid 1000 so no
  repair). Retry from a Windows shell. `otel/obs013_test.go` Golden
  content and the `gofmt -w internal/data/jobs/obs013_test.go`
  worktree-only normalization stay uncommitted (prior sessions' work).
