package racepolicy

import (
	"go/build"
	"path/filepath"
	"sort"
)

// Finding is one concurrent package's race-coverage verdict.
type Finding struct {
	Package ConcurrentPackage
	// HasLinuxRaceableTests is true when go/build, targeting linux/amd64
	// (where TOOL-012's -race CI job actually runs) with the default build
	// context otherwise, resolves at least one test file for this
	// package's directory. Go's race detector is a runtime flag, not a
	// build tag, so any file that resolves under a plain `go test` also
	// compiles, and races, under `go test -race`; a file gated behind a
	// nonstandard tag (e.g. an accidental `//go:build race`, which is not
	// a tag `-race` ever sets) simply never resolves and never runs
	// anywhere, on any CI job — which is exactly the gap this check finds.
	HasLinuxRaceableTests bool
	// Detail explains a false HasLinuxRaceableTests: what go/build reported
	// (no Go files at all, or Go files but zero test files) for this
	// directory under the Linux context.
	Detail string
}

// Violation reports whether this finding fails the policy.
func (f Finding) Violation() bool { return !f.HasLinuxRaceableTests }

// Report is the full policy evaluation over a directory tree.
type Report struct {
	Findings []Finding
}

// Violations returns the subset of Findings that fail the policy, in the
// same (import-path-sorted) order Evaluate produced them.
func (r Report) Violations() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Violation() {
			out = append(out, f)
		}
	}
	return out
}

// linuxBuildContext is a copy of go/build.Default retargeted at
// linux/amd64 — the platform TOOL-012's actual `-race` CI job runs on
// (.github/workflows/tests.yml's go-core job) — regardless of the host
// platform running this policy check itself (this repository's own
// development host is windows/arm64, which has no race detector at all;
// see doc.go).
func linuxBuildContext() build.Context {
	ctx := build.Default
	ctx.GOOS = "linux"
	ctx.GOARCH = "amd64"
	ctx.CgoEnabled = false
	// Match the default build (no `-tags race` or similar): a test file
	// gated behind such a tag is exactly the failure mode this policy
	// exists to catch, so BuildTags is deliberately left empty.
	ctx.BuildTags = nil
	return ctx
}

// packageHasLinuxRaceableTests reports whether dir (an absolute path)
// resolves at least one test file (TestGoFiles or XTestGoFiles) under the
// Linux build context.
func packageHasLinuxRaceableTests(dir string) (bool, string) {
	ctx := linuxBuildContext()
	pkg, err := ctx.ImportDir(dir, 0)
	if err != nil {
		if _, ok := err.(*build.NoGoError); ok {
			return false, "no Go source files resolve for this directory under linux/amd64 (default build context)"
		}
		// build.MultiplePackageError and similar still populate pkg with
		// what it found; fall through and judge on the file lists.
		if pkg == nil {
			return false, "go/build.ImportDir failed: " + err.Error()
		}
	}
	if len(pkg.TestGoFiles) > 0 || len(pkg.XTestGoFiles) > 0 {
		return true, ""
	}
	return false, "Go source files resolve under linux/amd64, but no test file does (either none exist, or the only ones present are excluded by a build constraint)"
}

// Evaluate finds every concurrent package under root (module path
// modulePath) and checks each one against packageHasLinuxRaceableTests.
func Evaluate(root, modulePath string) (Report, error) {
	packages, err := FindConcurrentPackages(root, modulePath)
	if err != nil {
		return Report{}, err
	}

	findings := make([]Finding, 0, len(packages))
	for _, pkg := range packages {
		dir := filepath.Join(root, filepath.FromSlash(pkg.Dir))
		has, detail := packageHasLinuxRaceableTests(dir)
		findings = append(findings, Finding{Package: pkg, HasLinuxRaceableTests: has, Detail: detail})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Package.ImportPath < findings[j].Package.ImportPath })
	return Report{Findings: findings}, nil
}
