package authoritygate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/planning/todoregistry"
)

func fullRecord() Record {
	return Record{
		Gate:        GateA,
		Decision:    DecisionProceed,
		ProposalRef: "proposal:p1a-observation/v3",
		EvidenceRefs: map[EvidenceClass][]string{
			ClassSecurity:       {"authoritygate.go"},
			ClassPrivacy:        {"authoritygate.go"},
			ClassCorrectness:    {"authoritygate.go"},
			ClassReconciliation: {"authoritygate.go"},
			ClassRecovery:       {"authoritygate.go"},
			ClassGovernance:     {"authoritygate.go"},
			ClassContinuity:     {"authoritygate.go"},
		},
		Signers:  []Signer{{Name: "A. Owner", Role: "product-lead"}},
		Date:     "2026-09-03",
		ReviewBy: "2026-12-03",
	}
}

// TestAuthorityGateRejectsIncompleteEvidence is the primary red/green test
// for GOV-010.
func TestAuthorityGateRejectsIncompleteEvidence(t *testing.T) {
	t.Run("RED: every missing evidence class is reported, not just the first", func(t *testing.T) {
		r := fullRecord()
		r.EvidenceRefs = map[EvidenceClass][]string{
			ClassSecurity: {"authoritygate.go"}, // only one of seven classes present
		}
		missing := MissingEvidenceClasses(r)
		want := []EvidenceClass{ClassPrivacy, ClassCorrectness, ClassReconciliation, ClassRecovery, ClassGovernance, ClassContinuity}
		if len(missing) != len(want) {
			t.Fatalf("MissingEvidenceClasses = %v, want %v", missing, want)
		}
		for i, c := range want {
			if missing[i] != c {
				t.Errorf("MissingEvidenceClasses[%d] = %s, want %s", i, missing[i], c)
			}
		}

		violations := ValidateRecord(r)
		gotClasses := 0
		for _, v := range violations {
			if v.Kind == "missing_evidence" {
				gotClasses++
			}
		}
		if gotClasses != len(want) {
			t.Errorf("ValidateRecord reported %d missing_evidence violations, want %d: %v", gotClasses, len(want), violations)
		}
	})

	t.Run("RED: a record with every evidence class empty reports all seven", func(t *testing.T) {
		r := fullRecord()
		r.EvidenceRefs = map[EvidenceClass][]string{}
		missing := MissingEvidenceClasses(r)
		if len(missing) != 7 {
			t.Fatalf("expected all 7 classes missing, got %v", missing)
		}
	})

	t.Run("GREEN: a fully evidenced, signed, proposal-bound record with a PROCEED decision validates clean", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "authoritygate.go"), "package authoritygate\n")
		r := fullRecord()
		if v := ValidateRecord(r); len(v) != 0 {
			t.Errorf("ValidateRecord: unexpected violations %v", v)
		}
		if v := CheckEvidencePaths(r, dir); len(v) != 0 {
			t.Errorf("CheckEvidencePaths: unexpected violations %v", v)
		}
	})

	for _, d := range []Decision{DecisionProceed, DecisionRemediate, DecisionNarrow, DecisionReselectWedge, DecisionStop} {
		t.Run("GREEN: "+string(d)+" is an accepted decision", func(t *testing.T) {
			r := fullRecord()
			r.Decision = d
			if v := ValidateRecord(r); len(v) != 0 {
				t.Errorf("ValidateRecord(%s): unexpected violations %v", d, v)
			}
		})
	}

	t.Run("GREEN: a decision outside the five-member enum is rejected", func(t *testing.T) {
		r := fullRecord()
		r.Decision = "APPROVED" // not one of the five
		violations := ValidateRecord(r)
		if !hasKind(violations, "invalid_decision") {
			t.Errorf("expected invalid_decision violation, got %v", violations)
		}
	})

	t.Run("GREEN: an unsigned decision is rejected", func(t *testing.T) {
		r := fullRecord()
		r.Signers = nil
		if !hasKind(ValidateRecord(r), "unsigned") {
			t.Errorf("expected unsigned violation for zero signers")
		}
		r2 := fullRecord()
		r2.Signers = []Signer{{Name: "", Role: "product-lead"}}
		if !hasKind(ValidateRecord(r2), "unsigned") {
			t.Errorf("expected unsigned violation for a signer missing a name")
		}
	})

	t.Run("GREEN: a decision with no proposal binding is rejected", func(t *testing.T) {
		r := fullRecord()
		r.ProposalRef = ""
		if !hasKind(ValidateRecord(r), "missing_proposal_ref") {
			t.Errorf("expected missing_proposal_ref violation")
		}
	})

	t.Run("GREEN: an unknown gate is rejected", func(t *testing.T) {
		r := fullRecord()
		r.Gate = "GATE_X"
		if !hasKind(ValidateRecord(r), "invalid_gate") {
			t.Errorf("expected invalid_gate violation")
		}
	})

	t.Run("undecided gate referenced by a ticked GATE_* todo is rejected", func(t *testing.T) {
		todos := []todoregistry.Todo{
			{ID: "X-001", Phase: "GATE_A", Done: true},
			{ID: "X-002", Phase: "GATE_B", Done: false}, // not ticked: does not require a decision
			{ID: "X-003", Phase: "P0", Done: true},      // not a gate phase: irrelevant
		}
		violations := CheckUndecidedGates(todos, nil)
		if len(violations) != 1 || violations[0].Gate != GateA || violations[0].Kind != "undecided" {
			t.Fatalf("expected exactly one undecided GATE_A violation, got %v", violations)
		}
	})

	t.Run("a gate with a valid decision record is not undecided even with ticked todos", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-001", Phase: "GATE_A", Done: true}}
		records := []Record{{Gate: GateA, Decision: DecisionRemediate}}
		if v := CheckUndecidedGates(todos, records); len(v) != 0 {
			t.Errorf("expected zero violations, got %v", v)
		}
	})

	t.Run("a gate decided with an invalid decision value still counts as undecided", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-001", Phase: "GATE_A", Done: true}}
		records := []Record{{Gate: GateA, Decision: "MAYBE"}}
		if v := CheckUndecidedGates(todos, records); len(v) != 1 {
			t.Errorf("expected one undecided violation for a garbage decision value, got %v", v)
		}
	})

	t.Run("a gate with zero ticked todos requires no decision", func(t *testing.T) {
		todos := []todoregistry.Todo{{ID: "X-001", Phase: "GATE_C", Done: false}}
		if v := CheckUndecidedGates(todos, nil); len(v) != 0 {
			t.Errorf("expected zero violations for an undecided gate with no completed work, got %v", v)
		}
	})

	t.Run("real corpus: no unallowed authority-gate gaps", func(t *testing.T) {
		root := filepath.Join("..", "..", "..")
		content, err := os.ReadFile(filepath.Join(root, "planning", "todos.md"))
		if err != nil {
			t.Fatalf("read todos.md: %v", err)
		}
		todos, parseErrs := todoregistry.ParseTodos(string(content))
		if len(parseErrs) > 0 {
			t.Fatalf("ParseTodos returned errors: %v", parseErrs)
		}

		defectsPath := filepath.Join(root, "definitions", "planning", "known-defects.yaml")
		records, err := Load(filepath.Join(root, "definitions", "planning", "authority-gate-decision.yaml"))
		if err != nil {
			t.Fatalf("Load authority-gate-decision.yaml: %v", err)
		}
		knownGaps, err := LoadKnownGaps(defectsPath)
		if err != nil {
			t.Fatalf("LoadKnownGaps: %v", err)
		}
		allowed := make(map[string]bool, len(knownGaps))
		for _, g := range knownGaps {
			if g.Owner == "" || g.Expiry == "" {
				t.Errorf("known-defects.yaml authority-gate entry %s/%s has no owner/expiry", g.Gate, g.Kind)
				continue
			}
			expiry, err := time.Parse("2006-01-02", g.Expiry)
			if err != nil {
				t.Errorf("known-defects.yaml authority-gate entry %s/%s has an unparseable expiry %q: %v", g.Gate, g.Kind, g.Expiry, err)
				continue
			}
			if time.Now().After(expiry) {
				t.Errorf("known-defects.yaml authority-gate entry %s/%s expired on %s; renew or resolve it", g.Gate, g.Kind, g.Expiry)
				continue
			}
			allowed[string(g.Gate)+"|"+g.Kind] = true
		}

		var unresolved []Violation
		for _, r := range records {
			for _, v := range ValidateRecord(r) {
				if !allowed[string(v.Gate)+"|"+v.Kind] {
					unresolved = append(unresolved, v)
				}
			}
			for _, v := range CheckEvidencePaths(r, root) {
				if !allowed[string(v.Gate)+"|"+v.Kind] {
					unresolved = append(unresolved, v)
				}
			}
		}
		for _, v := range CheckUndecidedGates(todos, records) {
			if !allowed[string(v.Gate)+"|"+v.Kind] {
				unresolved = append(unresolved, v)
			}
		}

		if len(unresolved) != 0 {
			t.Errorf("found %d unallowed authority-gate violation(s) in the real corpus:", len(unresolved))
			for _, v := range unresolved {
				t.Errorf("  %s", v)
			}
		}
	})
}

func hasKind(violations []Violation, kind string) bool {
	for _, v := range violations {
		if v.Kind == kind {
			return true
		}
	}
	return false
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestTodo_GOV_010_Golden pins the exact Violation message format so a
// future refactor cannot silently change diagnostic wording.
func TestTodo_GOV_010_Golden(t *testing.T) {
	v := Violation{Gate: GateB, Kind: "missing_evidence", Reason: "missing security evidence"}
	want := "GATE_B: missing security evidence"
	if got := v.String(); got != want {
		t.Errorf("violation message changed:\n got:  %s\n want: %s", got, want)
	}
}

// TestTodo_GOV_010_Fault exercises malformed input: unparsable YAML, an I/O
// error reading the decision file, and a missing decision file.
func TestTodo_GOV_010_Fault(t *testing.T) {
	t.Run("missing decision file yields zero records, not an error", func(t *testing.T) {
		records, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
		if err != nil {
			t.Fatalf("Load: unexpected error %v", err)
		}
		if records != nil {
			t.Errorf("expected nil records for a missing file, got %v", records)
		}
	})

	t.Run("malformed YAML is a parse error, not a panic or silent empty result", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "authority-gate-decision.yaml")
		mustWrite(t, path, "decisions: [this is not: valid: yaml:")
		if _, err := Load(path); err == nil {
			t.Error("expected a parse error for malformed YAML")
		}
	})

	t.Run("a decision file that is a directory reports an error", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "authority-gate-decision.yaml")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if _, err := Load(sub); err == nil {
			t.Error("expected an error reading a directory as the decision file")
		}
	})

	t.Run("an evidence class present but with an empty path list counts as missing", func(t *testing.T) {
		r := fullRecord()
		r.EvidenceRefs[ClassSecurity] = []string{}
		missing := MissingEvidenceClasses(r)
		if len(missing) != 1 || missing[0] != ClassSecurity {
			t.Errorf("expected only security missing, got %v", missing)
		}
	})

	t.Run("CheckEvidencePaths on an unresolvable repository root reports an error, not a panic", func(t *testing.T) {
		r := fullRecord()
		violations := CheckEvidencePaths(r, string([]byte{0}))
		if len(violations) == 0 {
			t.Error("expected at least one violation for an invalid repository root")
		}
	})
}

// TestTodo_GOV_010_Security proves an evidence ref cannot be used to read
// outside the repository the decision governs: an absolute path or a
// parent-relative escape is rejected without ever being stat'd, and cannot
// be smuggled past validation as a legitimate evidence file.
func TestTodo_GOV_010_Security(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	mustWrite(t, outside, "not evidence")

	cases := []struct {
		name string
		path string
	}{
		{"absolute path", outside},
		{"parent-relative escape", "../secret.txt"},
		{"nested parent-relative escape", "sub/../../secret.txt"},
		{"bare parent segment", ".."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := fullRecord()
			r.EvidenceRefs = map[EvidenceClass][]string{ClassSecurity: {tc.path}}
			violations := CheckEvidencePaths(r, dir)
			if len(violations) != 1 || violations[0].Kind != "evidence_path_escape" {
				t.Fatalf("expected exactly one evidence_path_escape violation for %q, got %v", tc.path, violations)
			}
			if strings.Contains(violations[0].Reason, "not evidence") {
				t.Errorf("violation message should not echo file content read from disk: %s", violations[0].Reason)
			}
		})
	}

	t.Run("a path that merely contains .. as part of a longer segment name is not an escape", func(t *testing.T) {
		mustWrite(t, filepath.Join(dir, "release..notes.txt"), "fine")
		r := fullRecord()
		r.EvidenceRefs = map[EvidenceClass][]string{ClassSecurity: {"release..notes.txt"}}
		if v := CheckEvidencePaths(r, dir); len(v) != 0 {
			t.Errorf("expected zero violations for a legitimate filename containing '..', got %v", v)
		}
	})
}
