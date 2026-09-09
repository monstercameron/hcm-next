package synctestkit_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/synctestkit"
)

// TestTodo_TOOL_021_Conformance proves the recorded qualification verdict
// in definitions/toolchain/synctest-qualification.yaml actually conforms
// to this package's test suite: the verdict is QUALIFIED, the declared
// scope names both what is covered and what is explicitly excluded (so
// the manifest can never be read as claiming database/network coverage
// it does not have), and the evidence list set-equals the five tests this
// package actually defines for TOOL-021's test matrix.
func TestTodo_TOOL_021_Conformance(t *testing.T) {
	root := repoRoot(t)
	q, err := synctestkit.LoadQualification(filepath.Join(root, "definitions", "toolchain", "synctest-qualification.yaml"))
	if err != nil {
		t.Fatalf("LoadQualification: %v", err)
	}

	if q.Tool != "testing/synctest" {
		t.Errorf("tool = %q, want %q", q.Tool, "testing/synctest")
	}
	if q.Verdict != "QUALIFIED" {
		t.Fatalf("verdict = %q, want QUALIFIED", q.Verdict)
	}
	if q.QualifiedGoVersion == "" {
		t.Error("qualified_go_version is empty")
	}

	if len(q.Scope.Covers) == 0 {
		t.Error("scope.covers is empty")
	}
	if len(q.Scope.Excludes) == 0 {
		t.Fatal("scope.excludes is empty: a qualification with no declared exclusion reads as covering everything")
	}
	for _, mustExclude := range []string{"PostgreSQL", "network"} {
		found := false
		for _, e := range q.Scope.Excludes {
			if strings.Contains(e, mustExclude) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("scope.excludes does not mention %q; the qualification must not be readable as covering it", mustExclude)
		}
	}

	if len(q.Routing) == 0 {
		t.Error("routing is empty: excluded scope must name where it actually gets validated")
	}

	wantTests := map[string]bool{
		"TestSynctestQualificationAdvancesTimersAndDetectsQuiescenceWithoutSleep": false,
		"TestTodo_TOOL_021_Property":    false,
		"TestTodo_TOOL_021_Race":        false,
		"TestTodo_TOOL_021_Fault":       false,
		"TestTodo_TOOL_021_Conformance": false,
	}
	for _, ev := range q.Evidence {
		if _, ok := wantTests[ev.Test]; !ok {
			t.Errorf("evidence lists unexpected test %q", ev.Test)
			continue
		}
		wantTests[ev.Test] = true
		if ev.Package != "tools/quality/synctestkit" {
			t.Errorf("%s: evidence package = %q, want %q", ev.Test, ev.Package, "tools/quality/synctestkit")
		}
	}
	for name, found := range wantTests {
		if !found {
			t.Errorf("evidence is missing test %q", name)
		}
	}

	const wantCommand = "go test -count=1 ./tools/quality/synctestkit/..."
	if q.Command != wantCommand {
		t.Errorf("command = %q, want %q", q.Command, wantCommand)
	}
}
