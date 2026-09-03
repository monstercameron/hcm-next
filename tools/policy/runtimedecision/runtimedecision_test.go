package runtimedecision_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/runtimedecision"
)

// recordPath is the real WF-RUN-000 decision record this package guards.
func recordPath(root string) string {
	return filepath.Join(root, "definitions", "runtime", "durable-runtime-decision.yaml")
}

// minimalCompleteYAML is a small, self-contained decision record that
// satisfies every completeness rule Validate enforces, used as the GREEN
// baseline that individual RED cases mutate one field at a time. Its
// EXISTS evidence path points at a file this test creates in a temp repo
// root, not at the real repository.
const minimalCompleteYAML = `
schema_version: 1
todo_id: WF-RUN-000
title: "test record"
status: DECIDED
decision_date: "2026-09-03"
owners:
  - name: someone
    role: owner
review_by: "2026-09-03"
non_negotiables:
  - id: NN1
    description: "ledger outside engine history"
  - id: NN2
    description: "tenant isolation and fencing"
  - id: NN3
    description: "inspectable typed projections"
  - id: NN4
    description: "safe-point pause and version pinning"
candidates:
  - name: "In-house"
    kind: BUILD
    selected: true
    summary: "build it"
    evaluation:
      - non_negotiable: NN1
        result: PASS
        reason: "reason 1"
      - non_negotiable: NN2
        result: PASS
        reason: "reason 2"
      - non_negotiable: NN3
        result: PASS
        reason: "reason 3"
      - non_negotiable: NN4
        result: PENDING
        reason: "reason 4"
        pending_todo: WF-RUN-008
    evidence:
      - description: "evidence file"
        path: "EVIDENCE_FILE.md"
        status: EXISTS
  - name: "Temporal"
    kind: ADOPT
    selected: false
    summary: "adopt it"
    evaluation:
      - non_negotiable: NN1
        result: UNKNOWN
        reason: "not spiked"
      - non_negotiable: NN2
        result: UNKNOWN
        reason: "not spiked"
      - non_negotiable: NN3
        result: UNKNOWN
        reason: "not spiked"
      - non_negotiable: NN4
        result: UNKNOWN
        reason: "not spiked"
    evidence:
      - description: "spike pending"
        status: PENDING
        pending_todo: WF-RUN-000
selected_option:
  candidate: "In-house"
  choice: "BUILD"
  rationale: "cheapest option that already exists"
  signed_by: "test-owner"
  signed_date: "2026-09-03"
rejected_options:
  - candidate: "Temporal"
    reason: "no fixture evidence yet"
consequences:
  - todo: WF-RUN-001
    impact: "build the instance table"
reevaluation_trigger:
  description: "re-run before P1B code"
  gating_todo: WF-RUN-000
rollback_plan: "delete the in-house code"
`

// writeRepoWithRecord creates a temp directory containing go.mod (so it
// looks like a repo root), the evidence file the minimal record's EXISTS
// entry names, and returns the temp root plus the decision record's path.
func writeRepoWithRecord(t *testing.T, yamlContent string) (repoRoot, path string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatalf("writing fixture go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "EVIDENCE_FILE.md"), []byte("evidence\n"), 0o644); err != nil {
		t.Fatalf("writing fixture evidence file: %v", err)
	}
	recPath := filepath.Join(dir, "record.yaml")
	if err := os.WriteFile(recPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("writing fixture record: %v", err)
	}
	return dir, recPath
}

func mustContainFinding(t *testing.T, res runtimedecision.Result, substr string) {
	t.Helper()
	for _, f := range res.Findings {
		if strings.Contains(f, substr) {
			return
		}
	}
	t.Fatalf("expected a finding containing %q, got: %v", substr, res.Findings)
}

// TestDurableRuntimeAdoptionDecisionRecordIsCompleteAndEvidenced is the
// WF-RUN-000 primary test: it proves RED (a synthetic incomplete record,
// missing a required field, an evaluation result, or evidence for its
// selected option, or claiming a nonexistent evidence path, is rejected
// with the offending finding named) and GREEN (the minimal well-formed
// fixture, and the real definitions/runtime/durable-runtime-decision.yaml
// record this package guards, both validate clean).
func TestDurableRuntimeAdoptionDecisionRecordIsCompleteAndEvidenced(t *testing.T) {
	t.Run("GREEN_minimal_fixture_validates", func(t *testing.T) {
		root, path := writeRepoWithRecord(t, minimalCompleteYAML)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if !res.OK {
			t.Fatalf("expected the minimal complete fixture to validate clean, got findings: %v", res.Findings)
		}
	})

	t.Run("GREEN_real_record_validates", func(t *testing.T) {
		root := repopath.RootDir()
		res, err := runtimedecision.ValidateFile(recordPath(root), root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if !res.OK {
			t.Fatalf("definitions/runtime/durable-runtime-decision.yaml is not complete/evidenced: %v", res.Findings)
		}
	})

	t.Run("RED_no_candidates", func(t *testing.T) {
		start := strings.Index(minimalCompleteYAML, "candidates:\n")
		end := strings.Index(minimalCompleteYAML, "selected_option:")
		if start < 0 || end < 0 || end <= start {
			t.Fatalf("test fixture setup: could not locate candidates block")
		}
		mutated := minimalCompleteYAML[:start] + "candidates: []\n" + minimalCompleteYAML[end:]
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record with zero candidates to be rejected")
		}
		mustContainFinding(t, res, "at least one evaluated candidate is required")
	})

	t.Run("RED_missing_non_negotiable_result", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML,
			`      - non_negotiable: NN4
        result: PENDING
        reason: "reason 4"
        pending_todo: WF-RUN-008
`, "", 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record missing a non-negotiable result to be rejected")
		}
		mustContainFinding(t, res, `missing an evaluation result for non-negotiable "NN4"`)
	})

	t.Run("RED_invalid_evaluation_result", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, "result: PASS\n        reason: \"reason 1\"", "result: MAYBE\n        reason: \"reason 1\"", 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record with an invalid evaluation result to be rejected")
		}
		mustContainFinding(t, res, "is not one of PASS/FAIL/PARTIAL/UNKNOWN/PENDING")
	})

	t.Run("RED_no_signed_choice", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `  signed_by: "test-owner"
`, "", 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record with no signed_by to be rejected")
		}
		mustContainFinding(t, res, "selected_option.signed_by is required")
	})

	t.Run("RED_selected_option_missing_choice", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `  choice: "BUILD"
`, "", 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record missing selected_option.choice to be rejected")
		}
		mustContainFinding(t, res, "selected_option.choice is required")
	})

	t.Run("RED_selected_candidate_lacks_evidence", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `    evidence:
      - description: "evidence file"
        path: "EVIDENCE_FILE.md"
        status: EXISTS
`, "", 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record whose selected option lacks evidence to be rejected")
		}
		mustContainFinding(t, res, `selected candidate "In-house" has no evidence entries`)
	})

	t.Run("RED_evidence_path_does_not_exist", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `path: "EVIDENCE_FILE.md"`, `path: "DOES_NOT_EXIST.md"`, 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record claiming a nonexistent evidence path to be rejected")
		}
		mustContainFinding(t, res, `claims path "DOES_NOT_EXIST.md" exists but it does not`)
	})

	t.Run("RED_pending_evidence_without_todo", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `      - description: "spike pending"
        status: PENDING
        pending_todo: WF-RUN-000
`, `      - description: "spike pending"
        status: PENDING
`, 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record with a PENDING evidence item missing pending_todo to be rejected")
		}
		mustContainFinding(t, res, "status PENDING requires pending_todo")
	})

	t.Run("RED_two_candidates_selected", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `    kind: ADOPT
    selected: false`, `    kind: ADOPT
    selected: true`, 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record with two selected candidates to be rejected")
		}
		mustContainFinding(t, res, "exactly one candidate must be marked selected")
	})

	t.Run("RED_rejected_candidate_missing_reason_entry", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `rejected_options:
  - candidate: "Temporal"
    reason: "no fixture evidence yet"
`, "", 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record with an unaccounted-for rejected candidate to be rejected")
		}
		mustContainFinding(t, res, `candidate "Temporal" is not selected but has no matching rejected_options entry`)
	})

	t.Run("RED_missing_reevaluation_trigger", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `reevaluation_trigger:
  description: "re-run before P1B code"
  gating_todo: WF-RUN-000
`, "", 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record missing reevaluation_trigger to be rejected")
		}
		mustContainFinding(t, res, "reevaluation_trigger.description is required")
	})

	t.Run("RED_wrong_non_negotiable_count", func(t *testing.T) {
		mutated := strings.Replace(minimalCompleteYAML, `  - id: NN4
    description: "safe-point pause and version pinning"
`, "", 1)
		root, path := writeRepoWithRecord(t, mutated)
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("ValidateFile: %v", err)
		}
		if res.OK {
			t.Fatalf("expected a record with only 3 non_negotiables to be rejected")
		}
		mustContainFinding(t, res, "non_negotiables must list exactly the 4 non-negotiables")
	})
}
