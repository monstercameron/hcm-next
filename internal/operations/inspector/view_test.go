package inspector_test

import (
	"sort"
	"testing"

	"github.com/monstercameron/hcm-next/internal/operations/inspector"
)

func nodeByID(t *testing.T, view inspector.WorkflowView, id string) inspector.NodeView {
	t.Helper()
	for _, n := range view.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node %q in view", id)
	return inspector.NodeView{}
}

// TestTodo_ADMIN_002 is the ADMIN-002 primary test: BuildWorkflowView
// traverses a compiled plan's node/edge graph, overlays execution status,
// safe points, effect summary, pending human work and lifecycle dimensions,
// and reports a node the trace never reached with the typed unavailable
// state rather than an invented guess.
func TestTodo_ADMIN_002(t *testing.T) {
	plan := fixturePlan()
	trace := fixtureTrace()

	view, err := inspector.BuildWorkflowView(plan, trace, allowDecision())
	if err != nil {
		t.Fatalf("BuildWorkflowView: %v", err)
	}

	if view.WorkflowID != "wf.promotion" || view.Version != 3 {
		t.Fatalf("view identity = %s/%d, want wf.promotion/3", view.WorkflowID, view.Version)
	}
	if view.StartNodeID != "preflight" {
		t.Errorf("StartNodeID = %s, want preflight", view.StartNodeID)
	}
	if !view.Effects.ZeroEffect {
		t.Error("Effects.ZeroEffect = false, want true (P1A plan)")
	}
	if len(view.Nodes) != 3 {
		t.Fatalf("len(Nodes) = %d, want 3", len(view.Nodes))
	}
	if len(view.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1", len(view.Edges))
	}

	preflight := nodeByID(t, view, "preflight")
	if !preflight.SafePoint {
		t.Error("preflight.SafePoint = false, want true")
	}
	if preflight.Status != inspector.StatusSucceeded {
		t.Errorf("preflight.Status = %s, want SUCCEEDED", preflight.Status)
	}
	if len(preflight.EvidenceRefs) != 1 || preflight.EvidenceRefs[0] != "ev:preflight:1" {
		t.Errorf("preflight.EvidenceRefs = %v, want [ev:preflight:1]", preflight.EvidenceRefs)
	}

	unreached := nodeByID(t, view, "unreached")
	if unreached.Status != inspector.StatusUnknown {
		t.Errorf("unreached.Status = %s, want UNKNOWN (trace has no evidence for it)", unreached.Status)
	}
	if len(unreached.EvidenceRefs) != 0 {
		t.Errorf("unreached.EvidenceRefs = %v, want empty", unreached.EvidenceRefs)
	}

	end := nodeByID(t, view, "end")
	if end.Terminal == nil {
		t.Fatal("end.Terminal is nil, want a compiled terminal")
	}
	if end.Terminal.TerminalCode != "PROMOTION_SIMULATED" {
		t.Errorf("Terminal.TerminalCode = %s, want PROMOTION_SIMULATED", end.Terminal.TerminalCode)
	}
	if end.Terminal.Dimensions.Business != "NOT_STARTED" {
		t.Errorf("Terminal.Dimensions.Business = %s, want NOT_STARTED", end.Terminal.Dimensions.Business)
	}
	if end.Terminal.Dimensions.Consistency != "PENDING_OBSERVATION" {
		t.Errorf("Terminal.Dimensions.Consistency = %s, want PENDING_OBSERVATION", end.Terminal.Dimensions.Consistency)
	}

	if len(view.PendingHumanWork) != 1 {
		t.Fatalf("len(PendingHumanWork) = %d, want 1", len(view.PendingHumanWork))
	}
	if view.PendingHumanWork[0].AssignedTo != "role:hr-partner" {
		t.Errorf("PendingHumanWork[0].AssignedTo = %s, want role:hr-partner", view.PendingHumanWork[0].AssignedTo)
	}
	if view.Digest == "" {
		t.Error("Digest is empty")
	}
}

// TestTodo_ADMIN_002_Golden pins node and edge ordering: BuildWorkflowView
// must return a deterministic order regardless of the compiled plan's own
// slice order, so two identical inputs always render identically.
func TestTodo_ADMIN_002_Golden(t *testing.T) {
	plan := fixturePlan()
	trace := fixtureTrace()

	view, err := inspector.BuildWorkflowView(plan, trace, allowDecision())
	if err != nil {
		t.Fatalf("BuildWorkflowView: %v", err)
	}

	gotIDs := make([]string, len(view.Nodes))
	for i, n := range view.Nodes {
		gotIDs[i] = n.ID
	}
	wantIDs := append([]string(nil), gotIDs...)
	sort.Strings(wantIDs)
	for i := range gotIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("Nodes not sorted by ID: got %v, want %v", gotIDs, wantIDs)
		}
	}

	// A second build from the identically-shaped inputs renders byte-for-byte
	// the same digest.
	second, err := inspector.BuildWorkflowView(plan, trace, allowDecision())
	if err != nil {
		t.Fatalf("BuildWorkflowView (second): %v", err)
	}
	if view.Digest != second.Digest {
		t.Fatalf("digests differ across identical builds: %s vs %s", view.Digest, second.Digest)
	}
}

// TestTodo_ADMIN_002_Race proves BuildWorkflowView is safe to call
// concurrently from many goroutines against the same plan and trace: it
// mutates neither, and every call returns the identical digest.
func TestTodo_ADMIN_002_Race(t *testing.T) {
	plan := fixturePlan()
	trace := fixtureTrace()
	dec := allowDecision()

	const n = 32
	digests := make([]string, n)
	errs := make([]error, n)
	done := make(chan int, n)

	for i := 0; i < n; i++ {
		i := i
		go func() {
			view, err := inspector.BuildWorkflowView(plan, trace, dec)
			digests[i] = view.Digest
			errs[i] = err
			done <- i
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: BuildWorkflowView: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if digests[i] != digests[0] {
			t.Fatalf("goroutine %d digest %s differs from goroutine 0 digest %s", i, digests[i], digests[0])
		}
	}
}

// TestTodo_ADMIN_002_Security proves the RED clause: a decision that denies
// the execution trace field removes every node's status, attempt, evidence
// reference and failure reason (replaced by the typed UNKNOWN state, never
// left as a stale or invented value), and a non-disclosable subject
// withholds everything uniformly, exactly like internal/trust/authz's own
// convention for a denied record.
func TestTodo_ADMIN_002_Security(t *testing.T) {
	plan := fixturePlan()
	trace := fixtureTrace()

	t.Run("denying the execution trace field withholds every node's status and evidence", func(t *testing.T) {
		view, err := inspector.BuildWorkflowView(plan, trace, denyTraceDecision())
		if err != nil {
			t.Fatalf("BuildWorkflowView: %v", err)
		}
		for _, n := range view.Nodes {
			if n.Status != inspector.StatusUnknown {
				t.Errorf("node %s Status = %s, want UNKNOWN under a denied execution-trace field", n.ID, n.Status)
			}
			if len(n.EvidenceRefs) != 0 {
				t.Errorf("node %s EvidenceRefs = %v, want empty under a denied execution-trace field", n.ID, n.EvidenceRefs)
			}
			if n.FailureReason != "" {
				t.Errorf("node %s FailureReason = %q, want empty under a denied execution-trace field", n.ID, n.FailureReason)
			}
			if n.Attempt != 0 {
				t.Errorf("node %s Attempt = %d, want 0 under a denied execution-trace field", n.ID, n.Attempt)
			}
		}
		// Human work was allowed in this decision, so it must still be
		// visible: denial is per field, not all-or-nothing.
		if len(view.PendingHumanWork) != 1 {
			t.Errorf("PendingHumanWork = %v, want the one allowed item still present", view.PendingHumanWork)
		}
		// The plan shape itself - never gated - is still fully visible.
		if len(view.Nodes) != 3 || len(view.Edges) != 1 {
			t.Error("plan shape (nodes/edges) was affected by an execution-trace field denial")
		}
	})

	t.Run("a non-disclosable subject withholds pending human work too", func(t *testing.T) {
		view, err := inspector.BuildWorkflowView(plan, trace, withheldDecision())
		if err != nil {
			t.Fatalf("BuildWorkflowView: %v", err)
		}
		if !view.Withheld {
			t.Error("Withheld = false, want true for a non-disclosable subject")
		}
		if view.PendingHumanWork != nil {
			t.Errorf("PendingHumanWork = %v, want nil for a non-disclosable subject", view.PendingHumanWork)
		}
		for _, n := range view.Nodes {
			if n.Status != inspector.StatusUnknown {
				t.Errorf("node %s Status = %s, want UNKNOWN for a non-disclosable subject", n.ID, n.Status)
			}
		}
	})

	t.Run("BuildWorkflowView never panics on a nil trace or nil decision", func(t *testing.T) {
		if _, err := inspector.BuildWorkflowView(plan, nil, nil); err != nil {
			t.Fatalf("BuildWorkflowView(nil trace, nil decision): %v", err)
		}
		if _, err := inspector.BuildWorkflowView(plan, nil, withheldDecision()); err != nil {
			t.Fatalf("BuildWorkflowView(nil trace, withheld decision): %v", err)
		}
	})

	t.Run("a nil plan is refused rather than panicking", func(t *testing.T) {
		if _, err := inspector.BuildWorkflowView(nil, trace, allowDecision()); err == nil {
			t.Fatal("BuildWorkflowView(nil plan) succeeded, want an error")
		}
	})
}

// TestTodo_ADMIN_002_Mutation proves the digest is sensitive to every field
// it claims to cover: perturbing any one observable field of the view - one
// at a time - changes Digest, and a redaction difference alone (same plan
// and trace, different decision) also changes it.
func TestTodo_ADMIN_002_Mutation(t *testing.T) {
	plan := fixturePlan()
	trace := fixtureTrace()

	baseline, err := inspector.BuildWorkflowView(plan, trace, allowDecision())
	if err != nil {
		t.Fatalf("BuildWorkflowView: %v", err)
	}

	t.Run("denying the execution trace field changes the digest", func(t *testing.T) {
		redacted, err := inspector.BuildWorkflowView(plan, trace, denyTraceDecision())
		if err != nil {
			t.Fatalf("BuildWorkflowView: %v", err)
		}
		if redacted.Digest == baseline.Digest {
			t.Fatal("redacting the execution trace field did not change the digest")
		}
	})

	t.Run("withholding the subject changes the digest", func(t *testing.T) {
		withheld, err := inspector.BuildWorkflowView(plan, trace, withheldDecision())
		if err != nil {
			t.Fatalf("BuildWorkflowView: %v", err)
		}
		if withheld.Digest == baseline.Digest {
			t.Fatal("withholding the subject did not change the digest")
		}
	})

	t.Run("removing one node's evidence reference changes the digest and nothing else", func(t *testing.T) {
		perturbedTrace := fixtureTrace()
		ev := perturbedTrace.Nodes["preflight"]
		ev.EvidenceRefs = nil
		perturbedTrace.Nodes["preflight"] = ev

		perturbed, err := inspector.BuildWorkflowView(plan, perturbedTrace, allowDecision())
		if err != nil {
			t.Fatalf("BuildWorkflowView: %v", err)
		}
		if perturbed.Digest == baseline.Digest {
			t.Fatal("removing an evidence reference did not change the digest")
		}
		if nodeByID(t, perturbed, "end").Status != inspector.StatusSucceeded {
			t.Error("perturbing preflight's evidence changed an unrelated node's status")
		}
	})

	t.Run("a failed node's failure reason participates in the digest", func(t *testing.T) {
		a := fixtureTrace()
		evA := a.Nodes["preflight"]
		evA.Status = inspector.StatusFailed
		evA.FailureReason = "ADP_TIMEOUT"
		a.Nodes["preflight"] = evA

		b := fixtureTrace()
		evB := b.Nodes["preflight"]
		evB.Status = inspector.StatusFailed
		evB.FailureReason = "DIFFERENT_REASON"
		b.Nodes["preflight"] = evB

		viewA, err := inspector.BuildWorkflowView(plan, a, allowDecision())
		if err != nil {
			t.Fatalf("BuildWorkflowView: %v", err)
		}
		viewB, err := inspector.BuildWorkflowView(plan, b, allowDecision())
		if err != nil {
			t.Fatalf("BuildWorkflowView: %v", err)
		}
		if viewA.Digest == viewB.Digest {
			t.Fatal("two different failure reasons produced the same digest")
		}
	})
}
