// Package racepolicy: rationale (TOOL-012).
//
// # Why not just `go test -race`
//
// `-race` is unavailable on this development host (windows/arm64 has no
// race detector support at all — the Go toolchain simply refuses the
// flag). .github/workflows/tests.yml's go-core job already runs
// `go test -race -count=1 ./...` on Linux CI, which is where the actual
// race class gets proven; that step is not this package's job to
// duplicate, and this package's own test suite runs, and must pass,
// without -race, on this host.
//
// What a blind `-race -count=1 ./...` cannot prove on its own is
// *coverage*: it races whatever tests already exist, but says nothing
// about a concurrent package that has no tests at all, or whose only test
// file happens to be excluded by an accidental build constraint (most
// plausibly a well-intentioned but wrong `//go:build race` guard — `-race`
// is a runtime flag, not a build tag, so a file gated on a "race" tag
// never builds under any `go test` invocation, racing or not, anywhere).
// A concurrent package with zero raceable tests passes `go test -race
// ./...` today for the same reason an untested package passes `go test
// ./...`: there is nothing there to fail.
//
// # What this package does instead
//
// FindConcurrentPackages walks the repository with go/ast and declares
// every package that imports "sync" or "sync/atomic", or contains a `go`
// statement, in any non-generated, non-testdata Go file (production or
// test). Evaluate then checks each declared package against a build
// context retargeted at linux/amd64 (linuxBuildContext, in policy.go) —
// the platform the actual `-race` CI job runs on — and asks go/build
// whether any test file resolves for that package under the *default*
// build constraints there (no special tags, matching plain `go test
// -race` with no extra flags). A package with zero resolving test files
// fails the policy: whatever raced today, it was never this package.
//
// # Known live-repository finding (not this lane's file root)
//
// Running Evaluate against this repository as of this package's authoring
// finds exactly one violation:
// internal/authn/federation/registry.go declares a `sync.RWMutex` field and
// ships no _test.go file at all, so no test resolves for it under any
// build context, racing or not. internal/authn/federation is outside
// tools/policy/racepolicy's file root, so this package does not attempt to
// fix it; TestTodo_TOOL_012_Golden pins this exact finding as a golden
// snapshot precisely so it is visible (and so the golden test itself
// forces an update, rather than silently going stale, whenever that gap
// closes or a new one appears elsewhere).
package racepolicy
