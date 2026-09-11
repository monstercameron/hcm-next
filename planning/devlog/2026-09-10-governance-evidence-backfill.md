# 2026-09-10 — Governance evidence backfill (GOV-017 / GOV-003)

## What

Backfilled missing Evidence lines for 43 completed todos so the two
planning-governance gates pass without new allowlist entries:

- GOV-017 (`TestTodoTDDContractCompleteness/RealCorpus` in
  `tools/planning/todogovernance`): 38 unallowlisted findings cleared —
  24 todos with no Evidence line (AVAIL-003, BAL-012/013, INTENT-019,
  LEAVE-003..011/014/017, LEGAL-003/004/006, MSG-009/013, REPLAN-002/004,
  WF-DISC-011, WORK-007/008/009), OBS-013 (evidence did not name its
  TEST), and WEB-024/027..036 (evidence named the tests but reported no
  `go test` result).
- GOV-003 (`TestRequirementTraceabilityRejectsOrphans` in
  `tools/planning/traceability`): 30 orphans cleared — 25 by the same
  evidence (each new line names at least one `Test*` function verified
  to exist in `*_test.go`), plus 5 TypeScript-proven todos
  (UX-006/007/008, CLIENT-001/002) covered by a new reviewed allowlist.

## How verified (no new code, only evidence prose)

- Every cited Go test was observed PASS by name (`-run` targeted runs)
  and every cited package suite PASS via
  `go test -count=1 <pkg>` on windows/arm64 (Go 1.26.3).
- Every cited vitest suite was observed green via `npx vitest run`
  (action-discovery 12, intent-center 22, channel-parity 13,
  client-lifecycle 17, client-device-state 22 tests PASS).
- `go test -count=1 ./tools/planning/todogovernance/ ./tools/planning/traceability/`
  PASS; `gofmt`/`go vet` clean on the touched tools.

## New gate mechanism

`tools/planning/traceability/allowlist.go` adds `tsProvenTodos`: the five
done todos whose proof lives in the TS vitest suites (which
`ScanTestNames` intentionally does not scan — Go `*_test.go` only), each
mapped to its suite file. The real-corpus subtest filters exactly these
IDs; any new orphan still fails. Mirrors the existing
`gov017EvidenceAllowlist` pattern (stale entries are harmless). Retire an
entry by adding Go coverage and citing it from the todo's Evidence line.

## Notes

- A `DISC-011` string in an early orphan count was a substring artifact
  of `WF-DISC-011`, not a real todo.
- UX-006/007/008 and CLIENT-001/002 keep their (now stale)
  `gov017EvidenceAllowlist` rows; stale rows do not fail that test.
