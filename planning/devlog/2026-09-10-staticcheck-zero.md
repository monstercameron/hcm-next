# 2026-09-10 — Staticcheck to zero (U1000 batch-A completion + deprecation migration)

## What

Cleared all 266 `go tool staticcheck ./...` findings so the CI quality
gate (`go run ./tools/quality`: gofmt, vet, staticcheck, i18n/a11y) is
fully green: `quality: PASSED`.

## Batches

- ST1005 (18): lowercase error paths from the schemaflux generator
  (`renderRequired` lowercases the leading type name) + regen of
  `internal/generated/schemaflux/models_generated.go` via
  `go run ./tools/gen/schemaflux/cmd/modelgen`; `TestTodo_MSRC_007_Golden`
  digest updated to the new template digest; one hand-written
  capitalization in `gatea_contracts.go` fixed. No test or doc asserted
  the old strings (verified by search).
- S1016/S1011/S1002/S1001/S1023/S1031/S1009/S1029/S1038/S1039 (mechanical):
  struct conversions, append spreads, `copy`, dropped redundant
  break/nil-checks, `t.Fatal` string, string range.
- ST1008: `ExecuteWithRecovery`/`RecoveryGuard.Run` now return
  `(ClassifiedFailure, error)`; the two call sites (impl + test) updated.
- SA4000 (26): determinism self-checks (`f(x) != f(x)`) rebound through
  variables so both evaluations still execute; the `||`-combined guards
  split into separate checks.
- SA4023 (11): deleted the deprecated always-failing `LoadIntentManifest`/
  `LoadFeatureManifest` stubs (callers use the `*YAML` variants); vacuous
  interface-nil checks became compile-time `var _ I = v` assertions.
- SA4006 (9): mapping NullError paths now return collected diagnostics
  with the error; dead initializers/assignments removed; a genuine
  swallowed error in `expression_eval.go` (negate path dropped `err`)
  now returns it.
- U1000 (52): deleted verified-dead funcs/methods/fields/test helpers
  (package-scoped reference check + interface audit each). One scripted
  deletion over-removed `Input.Validate` (single-line-func span bug);
  restored from git and re-removed only the two one-liners; every other
  hunk audited for the same hazard.
- SA1019 (81): `css.Opacity(n)` migrated to identical-output
  `css.OpacityNum(css.Num(n))` (48 sites); pgx `BeforeAcquire` migrated to
  `PrepareConn` (same destroy-and-retry semantics); `parser.ParseDir`
  replaced with behavior-identical ReadDir+ParseFile loops (no new deps);
  `ProposalBinding.Approved` initializers removed and the field deleted
  (Start never consulted it; WF-RUN-027 fallback tests still pin the
  boundary through ApprovalRef/Superseded); `attribute.Value.Emit` to
  `String`.
- Reasoned `//lint:ignore` (first in repo, each with cause): 5 nil-context
  hardening tests (SA1012), 2 odd-arity panic tests (SA5012), 2 empty-wire
  Cursor assertions (SA9005), parked_continuations wire-compat population
  and pins (SA1019, proto field cannot migrate without a wire change),
  x509 `Subjects` on AddCert-built pools, ecdsa coordinate nil guard.
  A full wire-format migration for parked_continuations is deferred as
  intentional debt (WorkType strings are not recoverable from the typed
  refs).

## Verification

- `go build ./...`, `go vet ./...`, `gofmt -l` clean.
- `go run ./tools/quality`: PASSED (the one local-only gofmt hit was a
  stale Sept-7 lane scratch file under git-ignored `.artifacts/`; formatted
  in place, content untouched — CI never sees it).
- Full `go test -count=1 ./...` before commit (results recorded in the
  commit trailer notes / this log's follow-up).

## Admission retry hardening (00282/00283, 00281 Down)

- 00280 left two defects: single-column primary keys on tenant-scoped
  tables and RLS enabled but not forced. 00282 re-keys both tables to
  (tenant_id, id) and points the receipt foreign key at the budget primary
  key; 00283 adds FORCE ROW LEVEL SECURITY to both. All three Downs refuse
  with P0001 (00281's rollback was rewritten to a refusal) because the
  admissionstore evidence-wall test requires every migration above the
  newest reversible version to be irreversible; my first 00283 draft with a
  real Down failed that test and was corrected.
- Manifest entry added per migration and re-signed with the development
  fixture key; the P1A evidence report was regenerated from the manifest
  with the same fixed date the freshness test pins.
- Verified: `go test -count=1 ./internal/data/admissionstore/
./internal/data/schema/ ./tools/planning/gateevidence/` all pass. The
  rlsparity admission findings are gone; the remaining safety-conformance
  repository-scope findings also fail on pristine c0a28ebe (checked via a
  clean archive copy), so they are pre-existing and need a security review
  before anyone allowlists them — deliberately left red.

## Outbox duplicate-dispatch fence (EVENT_002)

- Two concurrent `Dispatch` calls for the same record could both read
  "not applied" and run the handler. `ConsumerGroup` now registers one
  in-flight dispatch per applied key under the mutex; a second delivery
  waits on the runner's done channel and re-checks fencing afterwards
  instead of running the handler twice.
- Verified: the EVENT_002 race test passes repeatedly (10/10 in the prior
  session's runs, 3/3 re-verified here); the outbox suite re-runs with the
  commit hook.
- The pre-existing TestBreak_RunHaltsWhenLeaseExpiresMidHandler failure was
  a test-harness bug, not a product break: Run and the test's delivery
  polling shared one pgx connection, so the first Begin failed with "conn
  busy" (proven by replicating Run with its return value visible) and Run
  returned before any delivery. Run now gets its own connection via
  pgtest NewConn, the pattern TestOutboxConsumerHandlerIsIdempotentByMessageID
  already documents. No product code changed for this; the assertions are
  untouched.
