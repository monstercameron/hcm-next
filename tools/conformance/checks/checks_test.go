package checks

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/conformance/internal/reporoot"
	"github.com/monstercameron/hcm-next/tools/conformance/model"
	"github.com/monstercameron/hcm-next/tools/conformance/vocab"
)

func testVocab(t *testing.T) *vocab.Vocabulary {
	t.Helper()
	root, err := reporoot.Find()
	if err != nil {
		t.Fatalf("reporoot.Find: %v", err)
	}
	v, err := vocab.Load(filepath.Join(root, "planning", "specs", "workflow-runtime.md"))
	if err != nil {
		t.Fatalf("vocab.Load: %v", err)
	}
	return v
}

func result(t *testing.T, results []Result, id string) Result {
	t.Helper()
	for _, r := range results {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no result with ID %s in %+v", id, results)
	return Result{}
}

func TestEvaluateIsSortedByID(t *testing.T) {
	v := testVocab(t)
	wf := &model.Workflow{ID: "empty", Kind: model.KindDocument, RawText: "# Empty\n"}
	results := Evaluate(wf, v)
	if len(results) != len(registry) {
		t.Fatalf("len(results) = %d, want %d", len(results), len(registry))
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].ID >= results[i].ID {
			t.Fatalf("results not sorted: %s before %s", results[i-1].ID, results[i].ID)
		}
	}
	for _, r := range results {
		switch r.Status {
		case Pass, Fail, Unknown:
		default:
			t.Errorf("result %s has invalid status %q", r.ID, r.Status)
		}
		if r.Detail == "" {
			t.Errorf("result %s has empty Detail", r.ID)
		}
		if len(r.Refs) == 0 {
			t.Errorf("result %s has no Refs citation", r.ID)
		}
	}
}

func TestCHK001RetiredPrimitiveNodeType(t *testing.T) {
	v := testVocab(t)

	clean := &model.Workflow{RawText: "CAPABILITY people.worker.snapshot\nDECISION raise > 10%?\n"}
	if got := result(t, Evaluate(clean, v), "CHK-001").Status; got != Pass {
		t.Errorf("clean doc: status = %s, want PASS", got)
	}

	dirty := &model.Workflow{RawText: "START\nCHECKPOINT before commit\nEND\n"}
	r := result(t, Evaluate(dirty, v), "CHK-001")
	if r.Status != Fail {
		t.Errorf("dirty doc: status = %s, want FAIL", r.Status)
	}
}

func TestCHK002StructuralPrimitiveGated(t *testing.T) {
	v := testVocab(t)

	none := &model.Workflow{RawText: "no structural mentions here"}
	if got := result(t, Evaluate(none, v), "CHK-002").Status; got != Unknown {
		t.Errorf("no mention: status = %s, want UNKNOWN", got)
	}

	gated := &model.Workflow{RawText: "the fan-out is the post-P1B shape once PARALLEL/JOIN exist."}
	gated.PrimitiveHits = []model.PrimitiveHit{
		{Primitive: "PARALLEL", Context: "the fan-out is the post-P1B shape once PARALLEL/JOIN exist.", Line: 1},
		{Primitive: "JOIN", Context: "the fan-out is the post-P1B shape once PARALLEL/JOIN exist.", Line: 1},
	}
	if got := result(t, Evaluate(gated, v), "CHK-002").Status; got != Pass {
		t.Errorf("gated mention: status = %s, want PASS", got)
	}

	ungated := &model.Workflow{RawText: "the fan-out uses PARALLEL/JOIN here."}
	ungated.PrimitiveHits = []model.PrimitiveHit{
		{Primitive: "PARALLEL", Context: "the fan-out uses PARALLEL/JOIN here.", Line: 1},
	}
	if got := result(t, Evaluate(ungated, v), "CHK-002").Status; got != Fail {
		t.Errorf("ungated mention: status = %s, want FAIL", got)
	}
}

func TestCHK003SafePointConvention(t *testing.T) {
	v := testVocab(t)

	none := &model.Workflow{RawText: "no mention of the concept"}
	if got := result(t, Evaluate(none, v), "CHK-003").Status; got != Unknown {
		t.Errorf("no mention: status = %s, want UNKNOWN", got)
	}

	attr := &model.Workflow{RawText: "CAPABILITY worker.promote.execute   [safe_point before]"}
	if got := result(t, Evaluate(attr, v), "CHK-003").Status; got != Pass {
		t.Errorf("attribute style: status = %s, want PASS", got)
	}

	bare := &model.Workflow{RawText: "a safe point occurs here but is never written as an attribute"}
	if got := result(t, Evaluate(bare, v), "CHK-003").Status; got != Fail {
		t.Errorf("bare mention: status = %s, want FAIL", got)
	}
}

func TestCHK004CapabilityPresent(t *testing.T) {
	v := testVocab(t)

	empty := &model.Workflow{}
	if got := result(t, Evaluate(empty, v), "CHK-004").Status; got != Fail {
		t.Errorf("no capabilities: status = %s, want FAIL", got)
	}

	withCaps := &model.Workflow{Capabilities: []string{"people.worker.read"}}
	if got := result(t, Evaluate(withCaps, v), "CHK-004").Status; got != Pass {
		t.Errorf("with capabilities: status = %s, want PASS", got)
	}
}

func TestCHK005ApprovalResolverPresence(t *testing.T) {
	v := testVocab(t)

	suite := &model.Workflow{Kind: model.KindSuiteSection}
	if got := result(t, Evaluate(suite, v), "CHK-005").Status; got != Unknown {
		t.Errorf("suite section: status = %s, want UNKNOWN", got)
	}

	noHeading := &model.Workflow{Kind: model.KindDocument, Sections: []model.Section{{Heading: "Execution graph"}}}
	if got := result(t, Evaluate(noHeading, v), "CHK-005").Status; got != Unknown {
		t.Errorf("no approval heading: status = %s, want UNKNOWN", got)
	}

	headingNoResolver := &model.Workflow{Kind: model.KindDocument, Sections: []model.Section{{Heading: "Approval and revalidation"}}}
	if got := result(t, Evaluate(headingNoResolver, v), "CHK-005").Status; got != Fail {
		t.Errorf("heading without resolver: status = %s, want FAIL", got)
	}

	headingWithResolver := &model.Workflow{
		Kind:     model.KindDocument,
		Sections: []model.Section{{Heading: "Approval and revalidation"}},
		Actors:   []string{"HRBPFor(worker.organization)"},
	}
	if got := result(t, Evaluate(headingWithResolver, v), "CHK-005").Status; got != Pass {
		t.Errorf("heading with resolver: status = %s, want PASS", got)
	}
}

func TestCHK006CompletionDimensions(t *testing.T) {
	v := testVocab(t)

	none := &model.Workflow{}
	if got := result(t, Evaluate(none, v), "CHK-006").Status; got != Unknown {
		t.Errorf("no block: status = %s, want UNKNOWN", got)
	}

	full := &model.Workflow{ExpectedOutcomes: []model.KeyValue{
		{Key: "RequestState", Value: "CLOSED"},
		{Key: "ExecutionState", Value: "COMMITTED"},
		{Key: "BusinessState", Value: "COMPLETED"},
		{Key: "ConsistencyState", Value: "CONSISTENT"},
		{Key: "ObligationState", Value: "SATISFIED"},
	}}
	if got := result(t, Evaluate(full, v), "CHK-006").Status; got != Pass {
		t.Errorf("full five: status = %s, want PASS", got)
	}

	missingOne := &model.Workflow{ExpectedOutcomes: []model.KeyValue{
		{Key: "RequestState", Value: "CLOSED"},
		{Key: "ExecutionState", Value: "COMMITTED"},
		{Key: "BusinessState", Value: "COMPLETED"},
		{Key: "ObligationState", Value: "SATISFIED"},
	}}
	if got := result(t, Evaluate(missingOne, v), "CHK-006").Status; got != Fail {
		t.Errorf("missing one: status = %s, want FAIL", got)
	}

	sixth := &model.Workflow{ExpectedOutcomes: []model.KeyValue{
		{Key: "RequestState", Value: "CLOSED"},
		{Key: "ExecutionState", Value: "COMMITTED"},
		{Key: "BusinessState", Value: "COMPLETED"},
		{Key: "ConsistencyState", Value: "CONSISTENT"},
		{Key: "ObligationState", Value: "SATISFIED"},
		{Key: "RuntimeStatus", Value: "COMPLETED"},
	}}
	if got := result(t, Evaluate(sixth, v), "CHK-006").Status; got != Fail {
		t.Errorf("invented sixth dimension: status = %s, want FAIL", got)
	}
}

func TestCHK007ScenariosPresent(t *testing.T) {
	v := testVocab(t)

	doc := &model.Workflow{Kind: model.KindDocument}
	if got := result(t, Evaluate(doc, v), "CHK-007").Status; got != Fail {
		t.Errorf("no scenarios (document): status = %s, want FAIL", got)
	}

	suite := &model.Workflow{Kind: model.KindSuiteSection}
	if got := result(t, Evaluate(suite, v), "CHK-007").Status; got != Unknown {
		t.Errorf("no scenarios (suite): status = %s, want UNKNOWN", got)
	}

	withScenarios := &model.Workflow{Scenarios: []model.Scenario{{Index: 1, Description: "Happy path."}}}
	if got := result(t, Evaluate(withScenarios, v), "CHK-007").Status; got != Pass {
		t.Errorf("with scenarios: status = %s, want PASS", got)
	}
}

func TestCHK008DiagramStepsNonEmpty(t *testing.T) {
	v := testVocab(t)

	none := &model.Workflow{}
	if got := result(t, Evaluate(none, v), "CHK-008").Status; got != Unknown {
		t.Errorf("no diagrams: status = %s, want UNKNOWN", got)
	}

	thin := &model.Workflow{Diagrams: []model.Diagram{{Steps: []string{"a", "b"}}}}
	if got := result(t, Evaluate(thin, v), "CHK-008").Status; got != Fail {
		t.Errorf("thin diagram: status = %s, want FAIL", got)
	}

	rich := &model.Workflow{Diagrams: []model.Diagram{{Steps: []string{"a", "b", "c", "d"}}}}
	if got := result(t, Evaluate(rich, v), "CHK-008").Status; got != Pass {
		t.Errorf("rich diagram: status = %s, want PASS", got)
	}
}

func TestCHK009Preconditions(t *testing.T) {
	v := testVocab(t)

	suite := &model.Workflow{Kind: model.KindSuiteSection}
	if got := result(t, Evaluate(suite, v), "CHK-009").Status; got != Unknown {
		t.Errorf("suite section: status = %s, want UNKNOWN", got)
	}

	explicit := &model.Workflow{Kind: model.KindDocument, Preconditions: []string{"worker and employment active"}}
	if got := result(t, Evaluate(explicit, v), "CHK-009").Status; got != Pass {
		t.Errorf("explicit preconditions: status = %s, want PASS", got)
	}

	prose := &model.Workflow{Kind: model.KindDocument, RawText: "the platform reevaluates and revalidates before commit"}
	if got := result(t, Evaluate(prose, v), "CHK-009").Status; got != Pass {
		t.Errorf("prose revalidation: status = %s, want PASS", got)
	}

	none := &model.Workflow{Kind: model.KindDocument, RawText: "nothing about rechecking anything"}
	if got := result(t, Evaluate(none, v), "CHK-009").Status; got != Fail {
		t.Errorf("no revalidation: status = %s, want FAIL", got)
	}
}

func TestCHK010NoMutableGlobalState(t *testing.T) {
	v := testVocab(t)

	clean := &model.Workflow{RawText: "node outputs are immutable typed artifacts"}
	if got := result(t, Evaluate(clean, v), "CHK-010").Status; got != Pass {
		t.Errorf("clean: status = %s, want PASS", got)
	}

	negated := &model.Workflow{RawText: "the runtime does not expose a global mutable map[string]any"}
	if got := result(t, Evaluate(negated, v), "CHK-010").Status; got != Pass {
		t.Errorf("negated mention: status = %s, want PASS", got)
	}

	bad := &model.Workflow{RawText: "workflow state is a map[string]interface{} shared across nodes"}
	if got := result(t, Evaluate(bad, v), "CHK-010").Status; got != Fail {
		t.Errorf("undeclared map[string]: status = %s, want FAIL", got)
	}
}

func TestCHK011AtomicCoreSeparation(t *testing.T) {
	v := testVocab(t)

	noEffects := &model.Workflow{RawText: "worker record has a job title and a manager"}
	if got := result(t, Evaluate(noEffects, v), "CHK-011").Status; got != Unknown {
		t.Errorf("no external effects: status = %s, want UNKNOWN", got)
	}

	separated := &model.Workflow{RawText: "ACID: relationship event + critical projection + outbox\nobserve authoritative payroll state\nreconcile"}
	if got := result(t, Evaluate(separated, v), "CHK-011").Status; got != Pass {
		t.Errorf("separated: status = %s, want PASS", got)
	}

	fused := &model.Workflow{RawText: "payroll effects apply directly during preflight without confirmation"}
	if got := result(t, Evaluate(fused, v), "CHK-011").Status; got != Fail {
		t.Errorf("fused: status = %s, want FAIL", got)
	}
}
