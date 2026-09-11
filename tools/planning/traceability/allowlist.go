package traceability

// This file records the reviewed, pre-existing backlog evidence variance
// that the GOV-003 real-corpus test allows so the test can be green today
// without weakening any rule: a completed todo whose ID is not present in
// tsProvenTodos below must still name at least one Test/Fuzz/Benchmark
// function that exists in the repository's *_test.go sources, and any NEW
// orphan fails the test. Every entry here is a genuine live-corpus variance
// as of 2026-09-10: the todo is done and its proof lives in the TypeScript
// vitest suites (which ScanTestNames intentionally does not scan - it only
// walks *_test.go files), with the exact suite file cited per entry and the
// passing run recorded on the todo's own Evidence line.
//
// To retire an entry: add Go test coverage for the todo (or extend the
// scanner to the TS suites), cite the Go test from the todo's Evidence
// line, re-run the governance test, and delete the now-unmatched entry
// below (a stale, unmatched entry does not fail the test - only a NEW,
// unlisted orphan does).
var tsProvenTodos = map[string]string{
	// UX-006: universal/contextual action discovery - proof in
	// src/platform/action-discovery/ux006.test.ts (+ action-discovery.test.ts).
	"UX-006": "src/platform/action-discovery/ux006.test.ts",
	// UX-007: governed Intent Center - proof in
	// src/platform/intent-center/index.test.ts (+ intent-center.test.ts).
	"UX-007": "src/platform/intent-center/index.test.ts",
	// UX-008: cross-channel semantic equivalence - proof in
	// src/platform/channel-parity/channel-parity.test.ts.
	"UX-008": "src/platform/channel-parity/channel-parity.test.ts",
	// CLIENT-001: browser code/policy/storage/cache lifecycle - proof in
	// src/platform/client-lifecycle/lifecycle.test.ts.
	"CLIENT-001": "src/platform/client-lifecycle/lifecycle.test.ts",
	// CLIENT-002: mobile/kiosk/offline device state - proof in
	// src/platform/client-device-state/device-state.test.ts
	// (+ client-002.security.test.ts).
	"CLIENT-002": "src/platform/client-device-state/device-state.test.ts",
}
