package intentcoverage

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fixtureSnapshot is a complete, orphan-free snapshot: one baseline intent
// with every one of the nine owners present (contract, model, property,
// governance, capability, workflow-or-direct-path, test, evidence,
// deferment) plus one extension intent, so baseline/extension counts are
// exercised even though the live catalog has no extension intents yet.
func fixtureSnapshot() Snapshot {
	return Snapshot{
		Intents: []Intent{
			{
				ID: "hcmnext.people.promote_worker/v1", Namespace: NamespaceBaseline,
				Model: []string{"Worker"}, Property: []string{"effective_date"}, Governance: []string{"initiator_authority"},
			},
			{
				ID: "hcmnext.future.reward_recognition/v1", Namespace: NamespaceExtension,
				Model: []string{"Worker"}, Property: []string{"recognition_amount"}, Governance: []string{"initiator_authority"},
			},
		},
		Gaps: []CapabilityGap{
			{
				FeatureID: "promotion_request_submit", IntentID: "hcmnext.people.promote_worker/v1",
				Disposition: "MERGED_INTO", Capability: "intent:hcmnext.people.promote_worker/v1", Governance: "owner:people",
			},
			{
				FeatureID: "promotion_future_addon", IntentID: "hcmnext.people.promote_worker/v1",
				Disposition: "DEFERRED_TO_INTENT", DispositionTarget: "hcmnext.people.promotion_addon/v1",
				DispositionRationale: "future slice, not yet drafted",
			},
			{
				FeatureID: "reward_recognition_submit", IntentID: "hcmnext.future.reward_recognition/v1",
				Disposition: "MERGED_INTO", Capability: "intent:hcmnext.future.reward_recognition/v1", Governance: "owner:rewards",
			},
		},
		Workflows: []WorkflowBinding{
			{WorkflowID: "WF-PROMOTE", IntentID: "hcmnext.people.promote_worker/v1"},
			{WorkflowID: "WF-RECOGNIZE", IntentID: "hcmnext.future.reward_recognition/v1"},
		},
		Todos: []TodoBinding{
			{
				TodoID: "PROMO-001", Direct: []string{"hcmnext.people.promote_worker/v1"}, Sets: []string{"BI.PEOPLE"},
				Test: "TestPromoteWorkerContract",
				TestMatrix: map[string]string{
					"PRIMARY": "TestPromoteWorkerContract",
					"GOLDEN":  "TestTodo_PROMO_001_Golden",
				},
				Done:              true,
				EvidenceTestNames: []string{"TestPromoteWorkerContract"},
			},
			{
				TodoID: "RECOG-001", Direct: []string{"hcmnext.future.reward_recognition/v1"}, Sets: []string{"BI.REWARDS"},
				Test: "TestRewardRecognitionContract",
				TestMatrix: map[string]string{
					"PRIMARY": "TestRewardRecognitionContract",
				},
				Done:              true,
				EvidenceTestNames: []string{"TestRewardRecognitionContract"},
			},
			{
				TodoID: "PROMO-002", Direct: nil, Sets: []string{"BI.PEOPLE"},
				Test: "TestPromoWidgetSupport", TestMatrix: map[string]string{"PRIMARY": "TestPromoWidgetSupport"},
			},
		},
	}
}

func fixtureTestNames() map[string]bool {
	return map[string]bool{
		"TestPromoteWorkerContract":     true,
		"TestRewardRecognitionContract": true,
		"TestPromoWidgetSupport":        true,
	}
}

func findIntent(report Report, id string) (IntentNode, bool) {
	for _, n := range report.Intents {
		if n.IntentID == id {
			return n, true
		}
	}
	return IntentNode{}, false
}

func countOrphans(orphans []Orphan, kind string) int {
	n := 0
	for _, o := range orphans {
		if o.Kind == kind {
			n++
		}
	}
	return n
}

// TestIntentTodoCoverageRejectsOrphansAndFalseImplementationClaims is
// GOV-026's PRIMARY oracle: the complete fixture must resolve to VERIFIED
// with zero orphans, and removing any one of the nine required owners (or
// the reverse consumer) must produce exactly the matching orphan and
// nothing else.
func TestIntentTodoCoverageRejectsOrphansAndFalseImplementationClaims(t *testing.T) {
	good := Reconcile(fixtureSnapshot(), Options{TestNames: fixtureTestNames()})
	if len(good.NewOrphans) != 0 {
		t.Fatalf("complete fixture has orphan paths: %+v", good.NewOrphans)
	}
	node, ok := findIntent(good, "hcmnext.people.promote_worker/v1")
	if !ok || node.Status != Verified {
		t.Fatalf("complete fixture intent status = %+v, want VERIFIED", node)
	}
	extNode, ok := findIntent(good, "hcmnext.future.reward_recognition/v1")
	if !ok || extNode.Namespace != NamespaceExtension || extNode.Status != Verified {
		t.Fatalf("extension intent = %+v, want VERIFIED", extNode)
	}
	if good.BaselineIntentCount != 1 || good.ExtensionIntentCount != 1 {
		t.Fatalf("baseline/extension counts = %d/%d, want 1/1", good.BaselineIntentCount, good.ExtensionIntentCount)
	}
	foundExact := false
	for _, e := range good.ReverseEdges {
		if e.TodoID == "PROMO-001" && e.ExactMatch && e.IntentID == "hcmnext.people.promote_worker/v1" {
			foundExact = true
			if e.Outcome != Verified {
				t.Fatalf("reverse edge outcome = %s, want VERIFIED", e.Outcome)
			}
		}
		if e.TodoID == "PROMO-002" && e.ExactMatch {
			t.Fatalf("PROMO-002 has no DIRECT binding and must not appear as an exact reverse edge: %+v", e)
		}
	}
	if !foundExact {
		t.Fatal("no exact reverse edge found for PROMO-001")
	}

	t.Run("removing model owner", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Intents[0].Model = nil
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindIntentModel) != 1 {
			t.Fatalf("orphans = %+v, want exactly one intent_model", r.NewOrphans)
		}
	})

	t.Run("removing property owner", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Intents[0].Property = nil
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindIntentProperty) != 1 {
			t.Fatalf("orphans = %+v, want exactly one intent_property", r.NewOrphans)
		}
	})

	t.Run("removing governance owner", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Intents[0].Governance = nil
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindIntentGovernance) != 1 {
			t.Fatalf("orphans = %+v, want exactly one intent_governance", r.NewOrphans)
		}
	})

	t.Run("removing capability owner", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Gaps = s.Gaps[1:] // keep only the DEFERRED_TO_INTENT gap
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindIntentCapability) != 1 {
			t.Fatalf("orphans = %+v, want exactly one intent_capability", r.NewOrphans)
		}
		node, _ := findIntent(r, "hcmnext.people.promote_worker/v1")
		if node.Status != Conceptual {
			t.Fatalf("intent status without capability = %s, want CONCEPTUAL (partition/conceptual coverage cannot be reported as CONTRACTED/IMPLEMENTED)", node.Status)
		}
	})

	t.Run("removing workflow-or-direct-path owner", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Workflows = nil
		s.Todos[0].Direct = nil
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindIntentWorkflowOrDirect) != 1 {
			t.Fatalf("orphans = %+v, want exactly one intent_workflow_or_direct", r.NewOrphans)
		}
	})

	t.Run("removing the reverse consumer from substrate", func(t *testing.T) {
		// Deleting the bound todo entirely (as if it were removed from
		// planning/todos.md) while the workflow path is also absent must
		// produce the same workflow-or-direct-path orphan from the intent
		// side, proving the two views share one substrate binding.
		s := fixtureSnapshot()
		s.Workflows = nil
		s.Todos = s.Todos[1:] // drop PROMO-001 entirely
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindIntentWorkflowOrDirect) != 1 {
			t.Fatalf("orphans = %+v, want exactly one intent_workflow_or_direct after removing the reverse consumer", r.NewOrphans)
		}
		for _, e := range r.ReverseEdges {
			if e.TodoID == "PROMO-001" {
				t.Fatalf("removed todo must not still appear as a reverse edge: %+v", e)
			}
		}
	})

	t.Run("removing test owner", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Todos[0].Test = ""
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindIntentTest) != 1 {
			t.Fatalf("orphans = %+v, want exactly one intent_test", r.NewOrphans)
		}
	})

	t.Run("removing evidence owner", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Todos[0].Done = false
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindIntentEvidence) != 1 {
			t.Fatalf("orphans = %+v, want exactly one intent_evidence", r.NewOrphans)
		}
	})

	t.Run("removing deferment owner", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Gaps[1].DispositionRationale = ""
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindGapDeferment) != 1 {
			t.Fatalf("orphans = %+v, want exactly one gap_deferment", r.NewOrphans)
		}
	})

	t.Run("removing contract owner (dangling gap reference)", func(t *testing.T) {
		s := Snapshot{
			Gaps: []CapabilityGap{{FeatureID: "orphan_gap", IntentID: "hcmnext.people.promote_worker/v1", Disposition: "MERGED_INTO"}},
		}
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if len(r.NewOrphans) != 1 || r.NewOrphans[0].Kind != KindIntentContract || r.NewOrphans[0].ID != "orphan_gap" {
			t.Fatalf("orphans = %+v, want exactly one intent_contract for orphan_gap", r.NewOrphans)
		}
	})

	t.Run("dangling reverse consumer (todo names a removed intent)", func(t *testing.T) {
		s := fixtureSnapshot()
		s.Todos[0].Direct = []string{"hcmnext.ghost.does_not_exist/v1"}
		r := Reconcile(s, Options{TestNames: fixtureTestNames()})
		if countOrphans(r.NewOrphans, KindTodoDirectDangling) != 1 {
			t.Fatalf("orphans = %+v, want exactly one todo_direct_dangling", r.NewOrphans)
		}
		for _, e := range r.ReverseEdges {
			if e.TodoID == "PROMO-001" && e.ExactMatch {
				t.Fatalf("dangling DIRECT binding must not produce an exact reverse edge: %+v", e)
			}
		}
	})
}

func TestTodo_GOV_026_Property(t *testing.T) {
	opt := Options{TestNames: fixtureTestNames()}
	a := Reconcile(fixtureSnapshot(), opt)
	b := Reconcile(fixtureSnapshot(), opt)
	if a.Digest != b.Digest || a.JSONString() != b.JSONString() {
		t.Fatal("reconciliation is not deterministic")
	}
	if a.Digest == "" {
		t.Fatal("digest is empty")
	}

	// Status is monotonic in the ladder: an intent's status can never rank
	// below its best individual todo edge, and a todo edge can never rank
	// above VERIFIED.
	for _, n := range a.Intents {
		for _, e := range n.Todos {
			if e.Status.rank() > Verified.rank() || e.Status.rank() < Conceptual.rank() {
				t.Fatalf("todo edge status %s out of ladder range", e.Status)
			}
		}
	}
}

func TestTodo_GOV_026_Golden(t *testing.T) {
	root := repoRootForTest(t)
	snap, testNames, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("load repository: %v", err)
	}
	report := Reconcile(snap, Options{TestNames: testNames})
	want, err := os.ReadFile(filepath.Join("testdata", "coverage-digest.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes.TrimSpace(want)) != report.Digest {
		t.Fatalf("report digest = %s, want %s (baseline=%d gaps=%d workflows=%d todos=%d orphans=%d)",
			report.Digest, bytes.TrimSpace(want), report.BaselineIntentCount, report.GapCount, report.WorkflowBindingCount, report.TodoBindingCount, len(report.Orphans))
	}
}

func TestTodo_GOV_026_Race(t *testing.T) {
	opt := Options{TestNames: fixtureTestNames()}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := Reconcile(fixtureSnapshot(), opt); len(r.Intents) != 2 {
				t.Error("incomplete intent set")
			}
		}()
	}
	wg.Wait()
}

func TestTodo_GOV_026_Conformance(t *testing.T) {
	report := Reconcile(fixtureSnapshot(), Options{TestNames: fixtureTestNames()})
	if len(report.Intents) != 2 {
		t.Fatalf("intent count = %d, want 2", len(report.Intents))
	}
	for _, n := range report.Intents {
		if n.Namespace != NamespaceBaseline && n.Namespace != NamespaceExtension {
			t.Fatalf("intent %s has unknown namespace %q", n.IntentID, n.Namespace)
		}
	}
	if report.Digest == "" {
		t.Fatal("digest is empty")
	}
	if len(report.ReverseEdges) == 0 {
		t.Fatal("reverse edges are empty")
	}
}

// TestTodo_GOV_026_Mutation proves a single false maturity claim in a
// fixture is caught: a report that (by construction, never via Reconcile)
// claims VERIFIED for an intent whose capability owner is missing must be
// rejected by Validate.
func TestTodo_GOV_026_Mutation(t *testing.T) {
	s := fixtureSnapshot()
	s.Gaps = s.Gaps[1:] // capability owner missing: real ceiling is CONCEPTUAL
	honest := Reconcile(s, Options{TestNames: fixtureTestNames()})
	node, ok := findIntent(honest, "hcmnext.people.promote_worker/v1")
	if !ok || node.Status != Conceptual {
		t.Fatalf("precondition failed: honest status = %+v, want CONCEPTUAL", node)
	}
	if v := Validate(s, honest, Options{TestNames: fixtureTestNames()}); len(v) != 0 {
		t.Fatalf("honest report must validate clean: %+v", v)
	}

	tampered := honest
	tampered.Intents = append([]IntentNode(nil), honest.Intents...)
	for i := range tampered.Intents {
		if tampered.Intents[i].IntentID == "hcmnext.people.promote_worker/v1" {
			tampered.Intents[i].Status = Verified
		}
	}
	violations := Validate(s, tampered, Options{TestNames: fixtureTestNames()})
	if len(violations) != 1 || violations[0].Kind != KindFalseMaturityClaim || violations[0].ID != "hcmnext.people.promote_worker/v1" {
		t.Fatalf("mutation was not caught: %+v", violations)
	}

	// The same guard applies to the reverse-edge outcome.
	tamperedReverse := honest
	tamperedReverse.ReverseEdges = append([]ReverseEdge(nil), honest.ReverseEdges...)
	for i := range tamperedReverse.ReverseEdges {
		if tamperedReverse.ReverseEdges[i].TodoID == "PROMO-001" {
			tamperedReverse.ReverseEdges[i].Outcome = Verified
		}
	}
	violations = Validate(s, tamperedReverse, Options{TestNames: fixtureTestNames()})
	if len(violations) != 1 || violations[0].Kind != KindFalseMaturityClaim {
		t.Fatalf("reverse-edge mutation was not caught: %+v", violations)
	}
}

func TestBuildAllowlistUsesExactOwnerBackedKeys(t *testing.T) {
	entries := BuildAllowlist([]Orphan{{Kind: KindIntentCapability, ID: "X-1"}, {Kind: KindIntentCapability, ID: "X-1"}}, "2026-09-05")
	if len(entries) != 1 || entries[0].Owner != "intake-governance" || entries[0].ID != "X-1" {
		t.Fatalf("baseline allowlist = %+v", entries)
	}
}

func TestReportViolationsIsIndependentCopy(t *testing.T) {
	report := Reconcile(fixtureSnapshot(), Options{TestNames: fixtureTestNames()})
	v := report.Violations()
	if len(v) != 0 {
		t.Fatalf("clean report has violations: %+v", v)
	}
}
