package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestTodo_TOOL_013 is the TOOL-013 primary test. It runs FuzzTodo_TOOL_013
// in a bounded (-fuzztime=5s), real fuzzing subprocess against two
// packages that share the same seed corpus (fuzzkit.SeedCorpus):
//
//   - tools/quality/testdata/fuzzdefect, which has one planted defect, must
//     be found by the fuzz run (RED).
//   - tools/quality/fuzzkit, the fixed reference parser, must never panic
//     under the same bounded run (GREEN).
func TestTodo_TOOL_013(t *testing.T) {
	root := repoRoot(t)

	t.Run("seeded malformed inputs find the planted defect", func(t *testing.T) {
		cmd := exec.Command("go", "test", "-run", "^$", "-fuzz", "^FuzzTodo_TOOL_013$",
			"-fuzztime", "5s", "-count=1", "./tools/quality/testdata/fuzzdefect/")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected the fuzz run to find the planted defect in fuzzdefect.ParseEnvelope, it passed:\n%s", out)
		}
		if !strings.Contains(string(out), "FAIL") {
			t.Errorf("expected fuzz output to report FAIL, got:\n%s", out)
		}
		if !strings.Contains(string(out), "index out of range") {
			t.Errorf("expected fuzz output to report the planted index-out-of-range panic, got:\n%s", out)
		}
	})

	t.Run("bounded fuzz run over the fixed parser never panics", func(t *testing.T) {
		cmd := exec.Command("go", "test", "-run", "^$", "-fuzz", "^FuzzTodo_TOOL_013$",
			"-fuzztime", "5s", "-count=1", "./tools/quality/fuzzkit/")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("expected the bounded fuzz run over the fixed parser to pass, got:\n%s", out)
		}
	})
}
