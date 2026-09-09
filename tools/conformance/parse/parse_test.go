package parse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/internal/reporoot"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/model"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

func testVocab(t *testing.T) (*vocab.Vocabulary, string) {
	t.Helper()
	root, err := reporoot.Find()
	if err != nil {
		t.Fatalf("reporoot.Find: %v", err)
	}
	v, err := vocab.Load(filepath.Join(root, "planning", "specs", "workflow-runtime.md"))
	if err != nil {
		t.Fatalf("vocab.Load: %v", err)
	}
	return v, root
}

func mustReadRef(t *testing.T, root, name string) []byte {
	t.Helper()
	path := filepath.Join(root, "planning", "reference-workflows", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestParseManagerChange(t *testing.T) {
	v, root := testVocab(t)
	data := mustReadRef(t, root, "manager-change.md")

	doc, err := Parse("planning/reference-workflows/manager-change.md", data, v)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Workflows) != 1 {
		t.Fatalf("len(Workflows) = %d, want 1", len(doc.Workflows))
	}
	wf := doc.Workflows[0]

	if wf.ID != "manager-change" {
		t.Errorf("ID = %q, want %q", wf.ID, "manager-change")
	}
	if wf.Kind != model.KindDocument {
		t.Errorf("Kind = %q, want %q", wf.Kind, model.KindDocument)
	}
	if len(wf.Scenarios) != 10 {
		t.Errorf("len(Scenarios) = %d, want 10: %+v", len(wf.Scenarios), wf.Scenarios)
	}
	for i, sc := range wf.Scenarios {
		if sc.Index != i+1 {
			t.Errorf("Scenarios[%d].Index = %d, want %d", i, sc.Index, i+1)
		}
	}

	wantKeys := map[string]bool{"RequestState": true, "ExecutionState": true, "BusinessState": true, "ConsistencyState": true, "ObligationState": true}
	if len(wf.ExpectedOutcomes) != 5 {
		t.Fatalf("len(ExpectedOutcomes) = %d, want 5: %+v", len(wf.ExpectedOutcomes), wf.ExpectedOutcomes)
	}
	for _, kv := range wf.ExpectedOutcomes {
		if !wantKeys[kv.Key] {
			t.Errorf("unexpected completion dimension key %q", kv.Key)
		}
	}

	if !containsString(wf.Capabilities, "organization.relationship.manager_change.validate") {
		t.Errorf("Capabilities = %v, want it to contain organization.relationship.manager_change.validate", wf.Capabilities)
	}
	if !containsString(wf.Capabilities, "people.manager.change") {
		t.Errorf("Capabilities = %v, want it to contain people.manager.change", wf.Capabilities)
	}

	if !hasPrimitiveHit(wf.PrimitiveHits, "WAIT") {
		t.Errorf("PrimitiveHits = %+v, want a WAIT hit", wf.PrimitiveHits)
	}

	if len(wf.Diagrams) == 0 {
		t.Error("Diagrams is empty, want at least one fenced block")
	}
	if len(wf.Steps) < 5 {
		t.Errorf("len(Steps) = %d, want at least 5", len(wf.Steps))
	}
}

func TestParsePromoteIntoManagement(t *testing.T) {
	v, root := testVocab(t)
	data := mustReadRef(t, root, "promote-into-management.md")

	doc, err := Parse("planning/reference-workflows/promote-into-management.md", data, v)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Workflows) != 1 {
		t.Fatalf("len(Workflows) = %d, want 1", len(doc.Workflows))
	}
	wf := doc.Workflows[0]

	if wf.ID != "promote-into-management" {
		t.Errorf("ID = %q, want %q", wf.ID, "promote-into-management")
	}
	if len(wf.Scenarios) != 12 {
		t.Errorf("len(Scenarios) = %d, want 12: %+v", len(wf.Scenarios), wf.Scenarios)
	}

	if !containsString(wf.Capabilities, "positions.reserve") {
		t.Errorf("Capabilities = %v, want it to contain positions.reserve", wf.Capabilities)
	}
	if !containsString(wf.Capabilities, "rewards.compensation.simulate") {
		t.Errorf("Capabilities = %v, want it to contain rewards.compensation.simulate", wf.Capabilities)
	}

	if !hasPrimitiveHit(wf.PrimitiveHits, "OBSERVE") {
		t.Errorf("PrimitiveHits = %+v, want an OBSERVE hit", wf.PrimitiveHits)
	}
	if !hasPrimitiveHit(wf.PrimitiveHits, "PARALLEL") {
		t.Errorf("PrimitiveHits = %+v, want a PARALLEL hit", wf.PrimitiveHits)
	}
	if !hasPrimitiveHit(wf.PrimitiveHits, "JOIN") {
		t.Errorf("PrimitiveHits = %+v, want a JOIN hit", wf.PrimitiveHits)
	}

	if len(wf.Preconditions) == 0 {
		t.Error("Preconditions is empty, want the Execution-time revalidation diagram's lines")
	}
	if len(wf.ExpectedOutcomes) != 5 {
		t.Errorf("len(ExpectedOutcomes) = %d, want 5: %+v", len(wf.ExpectedOutcomes), wf.ExpectedOutcomes)
	}
}

func TestParseReferenceSuite(t *testing.T) {
	v, root := testVocab(t)
	data := mustReadRef(t, root, "reference-suite.md")

	doc, err := Parse("planning/reference-workflows/reference-suite.md", data, v)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(doc.Workflows) != 6 {
		t.Fatalf("len(Workflows) = %d, want 6 (5 reference workflows + sixth stress test): %+v", len(doc.Workflows), workflowIDs(doc.Workflows))
	}
	for _, wf := range doc.Workflows {
		if wf.Kind != model.KindSuiteSection {
			t.Errorf("Workflow %s Kind = %q, want %q", wf.ID, wf.Kind, model.KindSuiteSection)
		}
	}

	if len(doc.FoundationalQuestions) != 12 {
		t.Errorf("len(FoundationalQuestions) = %d, want 12", len(doc.FoundationalQuestions))
	}
	if len(doc.LegacyFixtures) != 5 {
		t.Errorf("len(LegacyFixtures) = %d, want 5: %+v", len(doc.LegacyFixtures), doc.LegacyFixtures)
	}

	// The "Promotion + Compensation Change" subsection must prove 8 items
	// via its bulleted "must prove" list.
	found := false
	for _, wf := range doc.Workflows {
		if wf.Title == "Reference Workflow 2: Promotion + Compensation Change" {
			found = true
			if len(wf.Scenarios) != 8 {
				t.Errorf("Reference Workflow 2 Scenarios = %d, want 8: %+v", len(wf.Scenarios), wf.Scenarios)
			}
		}
	}
	if !found {
		t.Errorf("did not find 'Reference Workflow 2: Promotion + Compensation Change' among %v", workflowIDs(doc.Workflows))
	}

	// The sixth stress test states its scenarios as one prose sentence, not
	// a bullet list; extraction must not fabricate a scenario list for it.
	for _, wf := range doc.Workflows {
		if wf.ID == "suite-sixth-infrastructure-stress-test-payroll-correction-retroactive-pay-repair" {
			if len(wf.Scenarios) != 0 {
				t.Errorf("sixth stress test Scenarios = %+v, want none (prose sentence, not a list)", wf.Scenarios)
			}
		}
	}
}

func TestParseRejectsEmptyDocument(t *testing.T) {
	v, _ := testVocab(t)
	if _, err := Parse("x.md", []byte("   \n\n  "), v); err == nil {
		t.Fatal("Parse(empty): got nil error, want non-nil")
	}
}

func TestParseRejectsMissingTitle(t *testing.T) {
	v, _ := testVocab(t)
	if _, err := Parse("x.md", []byte("Not a heading\n\nBody text.\n"), v); err == nil {
		t.Fatal("Parse(no title): got nil error, want non-nil")
	}
}

func TestParseFileMissing(t *testing.T) {
	v, _ := testVocab(t)
	if _, err := ParseFile(filepath.Join(t.TempDir(), "missing.md"), "missing.md", v); err == nil {
		t.Fatal("ParseFile(missing): got nil error, want non-nil")
	}
}

func TestIsContentLinePreservesWordsContainingConnectorLetters(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"validate active worker, active proposed manager,", true},
		{"          v", false},
		{"       v               v                       v               v", false},
		{"+---------------+-----------+-----------+---------------+", false},
		{"ACID: relationship event + critical projection + outbox", true},
		{"", false},
		{"   ", false},
		{"QUORUM 2 OF", true},
	}
	for _, c := range cases {
		if got := isContentLine(c.line); got != c.want {
			t.Errorf("isContentLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func hasPrimitiveHit(hits []model.PrimitiveHit, name string) bool {
	for _, h := range hits {
		if h.Primitive == name {
			return true
		}
	}
	return false
}

func workflowIDs(wfs []*model.Workflow) []string {
	ids := make([]string, len(wfs))
	for i, wf := range wfs {
		ids[i] = wf.ID
	}
	return ids
}
