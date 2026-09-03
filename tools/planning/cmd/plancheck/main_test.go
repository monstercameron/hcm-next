package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTDDContractCommandAdapterSyntheticGreen proves plancheck wires GOV-017
// to the checker rather than only compiling the library. A complete fixture
// must return a zero exit/error result from the command adapter.
func TestTDDContractCommandAdapterSyntheticGreen(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "planning"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const markdown = "## Fixtures\n\n" +
		"- [ ] `FIXTURE-001` **[P0][LUNA] complete fixture.**\n" +
		"  - **TEST:** `TestFixture`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestFixture; GOLDEN=TestFixtureGolden`.\n" +
		"  - **RED:** returns a typed error for the seeded defect.\n" +
		"  - **GREEN:** returns the exact accepted state.\n" +
		"  - **REFACTOR:** preserves the oracle and rerun scope.\n"
	if err := os.WriteFile(filepath.Join(root, "planning", "todos.md"), []byte(markdown), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := runTDDContract(root); err != nil {
		t.Fatalf("runTDDContract(synthetic green): %v", err)
	}
}

// TestTDDContractCommandAdapterLiveCorpusRemainsNonGreen ensures the live
// command exposes existing findings instead of silently treating incomplete
// planning evidence as success.
func TestTDDContractCommandAdapterLiveCorpusRemainsNonGreen(t *testing.T) {
	root := repositoryRoot(t)
	if err := runTDDContract(root); err == nil {
		t.Fatalf("runTDDContract(%s) unexpectedly accepted the live corpus", root)
	}
}

// TestLegalResearchCheckersRunAgainstRepository verifies the LEGAL-017 and
// LEGAL-018 checkers are wired into plancheck's repository-facing path, not
// merely unit-tested as isolated parsing libraries.
func TestLegalResearchCheckersRunAgainstRepository(t *testing.T) {
	root := repositoryRoot(t)
	if err := runFederalBaseline(root); err != nil {
		t.Fatalf("runFederalBaseline(%s): %v", root, err)
	}
	if err := runResearchQuestions(root); err != nil {
		t.Fatalf("runResearchQuestions(%s): %v", root, err)
	}
}

// TestBacklogAndArchitectureCheckersRunAgainstRepository proves the three
// repository-facing adapters are kept registered and execute their live scans,
// rather than leaving GOV-015, GOV-016, or ARCH-GO-017 as library-only checks.
func TestBacklogAndArchitectureCheckersRunAgainstRepository(t *testing.T) {
	root := repositoryRoot(t)
	if err := runProgress(root); err != nil {
		t.Fatalf("runProgress(%s): %v", root, err)
	}
	// The live corpus deliberately contains unresolved Gate A phase
	// inversions while the plan is being reconciled. A non-nil result is the
	// truthful command outcome; accepting it here would turn this adapter
	// into a false-green path. The dependencygraph package owns the small
	// clean/invalid graph fixtures that exercise both result classes.
	if err := runDependencyGraph(root); err == nil {
		t.Fatalf("runDependencyGraph(%s) unexpectedly accepted the known live graph violations", root)
	}
	if err := runGarbageDrawer(root); err != nil {
		t.Fatalf("runGarbageDrawer(%s): %v", root, err)
	}
}

// TestGateAndBoundaryCheckersRunAgainstRepository verifies the NEXT-002/
// NEXT-003 evidence-closure, GOV-010 authority-gate, GOV-011 coverage-matrix
// and GOV-013 boundary-test adapters are wired into plancheck's
// repository-facing path (not merely unit-tested as isolated libraries) and
// that each one, run against the live corpus, resolves cleanly once known,
// allow-listed gaps are taken into account: runAuthorityGate and
// runCoverageMatrix filter their real-tree findings against
// known-defects.yaml's known_authority_gate_gaps/known_coverage_gaps
// allow-lists exactly as their packages' own real-corpus tests do, so an
// item that is only allow-listed must not fail the command adapter.
func TestGateAndBoundaryCheckersRunAgainstRepository(t *testing.T) {
	root := repositoryRoot(t)
	if err := runP1AEvidence(root, false); err != nil {
		t.Errorf("runP1AEvidence(%s, live=false): %v", root, err)
	}
	if err := runAuthorityGate(root); err != nil {
		t.Errorf("runAuthorityGate(%s): %v", root, err)
	}
	if err := runCoverageMatrix(root); err != nil {
		t.Errorf("runCoverageMatrix(%s): %v", root, err)
	}
	if err := runBoundaryTests(root); err != nil {
		t.Errorf("runBoundaryTests(%s): %v", root, err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "planning", "todos.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root above %s", dir)
		}
		dir = parent
	}
}
