package workflowdesignjoin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
)

func joinRecord(intent, definition string) workflowdesign.DesignRecord {
	return workflowdesign.DesignRecord{
		Intent:         intent,
		Definition:     definition,
		Disposition:    workflowdesign.DispositionWorkflow,
		Archetype:      "A2",
		DomainProfile:  "people",
		InputBoundary:  "proposal",
		SnapshotPolicy: "authority-snapshot",
		Engines:        workflowdesign.Dimension{Items: []string{"identity-resolution"}},
		HumanWork:      workflowdesign.Dimension{Value: "manager-self-service"},
		Writes:         workflowdesign.Dimension{Value: "edge-replacement"},
		Waits:          workflowdesign.Dimension{Value: "approval-window"},
		Invalidators:   workflowdesign.Dimension{Value: "org-reparent"},
		Reconciliation: workflowdesign.Dimension{Value: "relationship-access"},
		Correction:     workflowdesign.Dimension{Value: "revoke-and-replace"},
		Completion:     "edge-observed",
	}
}

func findingCodes(findings []Finding, definition string) map[string]int {
	codes := make(map[string]int)
	for _, finding := range findings {
		if finding.Definition == definition {
			codes[finding.Code]++
		}
	}
	return codes
}

// TestAcceptedIntentWorkflowDesignJoinIsExactAndTotal is the WF-DISC-006
// primary oracle: one-to-one join over accepted definitions with zero
// missing, duplicate or alias-only records and no other denominator.
func TestAcceptedIntentWorkflowDesignJoinIsExactAndTotal(t *testing.T) {
	clean := []workflowdesign.DesignRecord{
		joinRecord("Alpha", "hcmnext.t.alpha/v1"),
		joinRecord("Beta", "hcmnext.t.beta/v1"),
		joinRecord("Gamma", "hcmnext.t.gamma/v1"),
	}
	join, findings := JoinRecords(
		[]string{"hcmnext.t.alpha/v1", "hcmnext.t.beta/v1", "hcmnext.t.gamma/v1"},
		clean,
	)
	if !join.Complete() || join.Matched != 3 || join.Total != 3 {
		t.Fatalf("clean join = %d/%d complete=%v, want 3/3 true", join.Matched, join.Total, join.Complete())
	}
	if len(findings) > 0 {
		t.Fatalf("clean join raised findings: %+v", findings)
	}
	if join.Digest == "" {
		t.Fatal("clean join carries no digest")
	}

	messy, messyFindings := JoinRecords(
		[]string{"hcmnext.t.alpha/v1", "hcmnext.t.beta/v1", "hcmnext.t.gamma/v1"},
		[]workflowdesign.DesignRecord{
			joinRecord("Alpha", "hcmnext.t.alpha/v1"),
			joinRecord("AlphaAgain", "hcmnext.t.alpha/v1"),
			joinRecord("AliasOnly", ""),
			joinRecord("Rogue", "hcmnext.t.rogue/v1"),
		},
	)
	if messy.Complete() {
		t.Fatal("messy join reports complete")
	}
	if messy.Matched != 0 || messy.Total != 3 {
		t.Fatalf("messy join = %d/%d, want 0/3", messy.Matched, messy.Total)
	}
	if codes := findingCodes(messyFindings, "hcmnext.t.alpha/v1"); codes[DuplicateDesign] == 0 {
		t.Errorf("duplicate definition accepted: %+v", messyFindings)
	}
	if codes := findingCodes(messyFindings, "hcmnext.t.beta/v1"); codes[MissingDesign] == 0 {
		t.Errorf("missing definition accepted: %+v", messyFindings)
	}
	if codes := findingCodes(messyFindings, "hcmnext.t.gamma/v1"); codes[MissingDesign] == 0 {
		t.Errorf("missing definition accepted: %+v", messyFindings)
	}
	alias := false
	for _, finding := range messyFindings {
		if finding.Code == AliasRecord && finding.Intent == "AliasOnly" {
			alias = true
		}
		if finding.Code == AliasRecord && finding.Definition != "" {
			t.Errorf("alias finding carries a definition: %+v", finding)
		}
	}
	if !alias {
		t.Errorf("alias-only record accepted: %+v", messyFindings)
	}
	unknown := false
	for _, finding := range messyFindings {
		if finding.Code == UnknownDefinition && finding.Definition == "hcmnext.t.rogue/v1" {
			unknown = true
		}
	}
	if !unknown {
		t.Errorf("unknown definition accepted: %+v", messyFindings)
	}
	for _, binding := range messy.Bindings {
		if binding.Definition == "hcmnext.t.rogue/v1" {
			t.Errorf("unknown definition joined: %+v", binding)
		}
	}
}

func TestTodo_WF_DISC_006_Property(t *testing.T) {
	accepted := []string{"hcmnext.t.alpha/v1", "hcmnext.t.beta/v1"}
	t.Run("empty corpus misses everything", func(t *testing.T) {
		join, findings := JoinRecords(accepted, nil)
		if join.Complete() || join.Matched != 0 || join.Total != 2 {
			t.Fatalf("empty join = %d/%d complete=%v", join.Matched, join.Total, join.Complete())
		}
		for _, id := range accepted {
			if findingCodes(findings, id)[MissingDesign] == 0 {
				t.Errorf("%s missing without a finding", id)
			}
		}
	})
	t.Run("unbinding one breaks totality", func(t *testing.T) {
		full := []workflowdesign.DesignRecord{joinRecord("Alpha", "hcmnext.t.alpha/v1"), joinRecord("Beta", "hcmnext.t.beta/v1")}
		whole, _ := JoinRecords(accepted, full)
		part, findings := JoinRecords(accepted, full[:1])
		if whole.Digest == part.Digest {
			t.Fatal("dropped record left the digest unchanged")
		}
		if part.Complete() {
			t.Fatal("partial join reports complete")
		}
		if findingCodes(findings, "hcmnext.t.beta/v1")[MissingDesign] == 0 {
			t.Errorf("unbound definition accepted: %+v", findings)
		}
	})
	t.Run("rebinding to unknown leaves a hole", func(t *testing.T) {
		records := []workflowdesign.DesignRecord{joinRecord("Alpha", "hcmnext.t.alpha/v1"), joinRecord("Beta", "hcmnext.t.rogue/v1")}
		join, findings := JoinRecords(accepted, records)
		if join.Complete() {
			t.Fatal("rebuilt join reports complete")
		}
		if findingCodes(findings, "hcmnext.t.beta/v1")[MissingDesign] == 0 {
			t.Errorf("abandoned definition accepted: %+v", findings)
		}
		if findingCodes(findings, "hcmnext.t.rogue/v1")[UnknownDefinition] == 0 {
			t.Errorf("unknown definition accepted: %+v", findings)
		}
	})
	t.Run("dropping the definition makes an alias", func(t *testing.T) {
		records := []workflowdesign.DesignRecord{joinRecord("Alpha", "hcmnext.t.alpha/v1"), joinRecord("Beta", "")}
		_, findings := JoinRecords(accepted, records)
		alias := false
		for _, finding := range findings {
			if finding.Code == AliasRecord && finding.Intent == "Beta" {
				alias = true
			}
		}
		if !alias {
			t.Errorf("definition-free record accepted: %+v", findings)
		}
		if findingCodes(findings, "hcmnext.t.beta/v1")[MissingDesign] == 0 {
			t.Errorf("unbound definition accepted: %+v", findings)
		}
	})
}

func TestTodo_WF_DISC_006_Golden(t *testing.T) {
	join, findings := JoinRecords(
		[]string{"hcmnext.t.alpha/v1", "hcmnext.t.beta/v1"},
		[]workflowdesign.DesignRecord{
			joinRecord("Alpha", "hcmnext.t.alpha/v1"),
			joinRecord("AliasOnly", ""),
		},
	)
	got, err := MarshalJoin(join, findings)
	if err != nil {
		t.Fatal(err)
	}
	const goldenPath = "testdata/golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_WF_DISC_006_Security(t *testing.T) {
	t.Run("display lookalike without a definition is an alias", func(t *testing.T) {
		_, findings := JoinRecords(
			[]string{"hcmnext.t.alpha/v1"},
			[]workflowdesign.DesignRecord{joinRecord("ChangeManager", "")},
		)
		alias := false
		for _, finding := range findings {
			if finding.Code == AliasRecord && finding.Intent == "ChangeManager" {
				alias = true
			}
		}
		if !alias {
			t.Fatalf("display lookalike joined without a definition: %+v", findings)
		}
	})
	t.Run("forged version is unknown", func(t *testing.T) {
		_, findings := JoinRecords(
			[]string{"hcmnext.t.alpha/v1"},
			[]workflowdesign.DesignRecord{joinRecord("Alpha", "hcmnext.t.alpha/v9")},
		)
		if findingCodes(findings, "hcmnext.t.alpha/v1")[MissingDesign] == 0 {
			t.Fatalf("abandoned definition accepted: %+v", findings)
		}
		if findingCodes(findings, "hcmnext.t.alpha/v9")[UnknownDefinition] == 0 {
			t.Fatalf("forged version accepted: %+v", findings)
		}
	})
	t.Run("shared display names never collide", func(t *testing.T) {
		join, findings := JoinRecords(
			[]string{"hcmnext.t.alpha/v1", "hcmnext.t.beta/v1"},
			[]workflowdesign.DesignRecord{joinRecord("Same", "hcmnext.t.alpha/v1"), joinRecord("Same", "hcmnext.t.beta/v1")},
		)
		if !join.Complete() || len(findings) > 0 {
			t.Fatalf("display collision broke the join: %+v complete=%v", findings, join.Complete())
		}
	})
	t.Run("malformed definition cannot bind", func(t *testing.T) {
		_, findings := JoinRecords(
			[]string{"hcmnext.t.alpha/v1"},
			[]workflowdesign.DesignRecord{joinRecord("Alpha", "Alpha")},
		)
		if findingCodes(findings, "hcmnext.t.alpha/v1")[MissingDesign] == 0 {
			t.Fatalf("abandoned definition accepted: %+v", findings)
		}
	})
}

func TestTodo_WF_DISC_006_Conformance(t *testing.T) {
	root := repoRoot(t)
	accepted, records, err := LoadSnapshot(root)
	if err != nil {
		t.Fatalf("load live snapshot: %v", err)
	}
	if len(accepted) != 14 {
		t.Fatalf("accepted definitions = %d, want 14: there is no other denominator", len(accepted))
	}
	first, firstFindings := JoinRecords(accepted, records)
	second, _ := JoinRecords(accepted, records)
	if first.Digest != second.Digest {
		t.Fatal("live join digest not deterministic")
	}
	if !first.Complete() || first.Matched != 14 || first.Total != 14 {
		t.Fatalf("live join = %d/%d complete=%v, want 14/14 true", first.Matched, first.Total, first.Complete())
	}
	if len(firstFindings) > 0 {
		t.Fatalf("live join raised findings: %+v", firstFindings)
	}
	for _, binding := range first.Bindings {
		if binding.Definition == "" || binding.Intent == "" {
			t.Errorf("binding %+v joins on an empty key", binding)
		}
	}
}

func TestTodo_WF_DISC_006_Mutation(t *testing.T) {
	accepted := []string{"hcmnext.t.alpha/v1", "hcmnext.t.beta/v1"}
	base, _ := JoinRecords(accepted, []workflowdesign.DesignRecord{joinRecord("Alpha", "hcmnext.t.alpha/v1"), joinRecord("Beta", "hcmnext.t.beta/v1")})
	t.Run("duplicating a definition breaks exactness", func(t *testing.T) {
		join, findings := JoinRecords(accepted, []workflowdesign.DesignRecord{joinRecord("Alpha", "hcmnext.t.alpha/v1"), joinRecord("AlphaAgain", "hcmnext.t.alpha/v1"), joinRecord("Beta", "hcmnext.t.beta/v1")})
		if join.Complete() {
			t.Fatal("duplicated join reports complete")
		}
		if findingCodes(findings, "hcmnext.t.alpha/v1")[DuplicateDesign] == 0 {
			t.Errorf("duplicate accepted: %+v", findings)
		}
		if join.Digest == base.Digest {
			t.Error("duplicate left the digest unchanged")
		}
	})
	t.Run("renaming the intent keeps the binding", func(t *testing.T) {
		join, findings := JoinRecords(accepted, []workflowdesign.DesignRecord{joinRecord("Renamed", "hcmnext.t.alpha/v1"), joinRecord("Beta", "hcmnext.t.beta/v1")})
		if !join.Complete() || len(findings) > 0 {
			t.Fatalf("display rename broke the join: %+v", findings)
		}
		if join.Bindings[0].Intent != "Beta" && join.Bindings[0].Intent != "Renamed" {
			t.Errorf("bindings lost the rename: %+v", join.Bindings)
		}
	})
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}
