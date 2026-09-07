package main

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// TestTodo_TOOL_012 is the TOOL-012 primary test. It runs
// tools/quality/testdata/racefixture under `go test -race` in a subprocess
// and asserts the deliberately racy TestRacyIncrement is caught (RED),
// while the correctly-synchronized TestSynchronizedIncrement passes clean
// under the same detector (GREEN).
//
// The Go race detector is not supported on every platform (notably
// windows/arm64, this development machine's own target). Rather than
// hardcode a platform list that will go stale, this test asks `go test
// -race` itself and skips with a clear message when the toolchain reports
// the combination is unsupported; CI (linux/amd64) does support it and
// verifies for real (see .github/workflows/tests.yml).
func TestTodo_TOOL_012(t *testing.T) {
	root := repoRoot(t)
	fixturePattern := "./tools/quality/testdata/racefixture/"

	probe := exec.Command("go", "test", "-race", "-run", "^$", fixturePattern)
	probe.Dir = root
	probeOut, _ := probe.CombinedOutput()
	if raceUnsupported(probeOut) {
		t.Skipf("race detector not supported on %s/%s; TOOL-012 must be verified on race-capable CI (e.g. linux/amd64). go test output:\n%s", runtime.GOOS, runtime.GOARCH, probeOut)
	}

	t.Run("racy test is caught (RED)", func(t *testing.T) {
		cmd := exec.Command("go", "test", "-race", "-run", "^TestRacyIncrement$", "-count=1", fixturePattern)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected `go test -race` to fail on the deliberate race fixture, it passed:\n%s", out)
		}
		if !strings.Contains(string(out), "DATA RACE") {
			t.Errorf("expected race detector output to contain \"DATA RACE\", got:\n%s", out)
		}
	})

	t.Run("synchronized test passes clean under -race (GREEN)", func(t *testing.T) {
		cmd := exec.Command("go", "test", "-race", "-run", "^TestSynchronizedIncrement$", "-count=1", fixturePattern)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("expected the synchronized fixture to pass under -race, got:\n%s", out)
		}
	})
}

func raceUnsupported(output []byte) bool {
	text := strings.ToLower(string(output))
	return strings.Contains(text, "race detector is not supported") ||
		strings.Contains(text, "not supported on") ||
		strings.Contains(text, "-race requires cgo")
}
