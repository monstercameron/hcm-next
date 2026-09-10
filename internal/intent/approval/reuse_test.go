package approval_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
)

func reuseDecisions(t *testing.T) ([]approval.ApprovalDecision, string) {
	t.Helper()
	f := mustFixture(t)
	v := f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved)
	d, err := f.Binder.Record(v)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	_ = d
	decisions := f.Binder.Decisions()
	if len(decisions) == 0 {
		t.Fatal("fixture recorded no decisions")
	}
	return decisions, string(f.Current().MaterialDigest.Digest)
}

func TestTodo_REPLAN_002(t *testing.T) {
	prior, priorDigest := reuseDecisions(t)
	receipt := prior[0].Digest()
	policy := approval.ReusePolicy{Version: "reuse-2026.1", RetainPresentationOnly: true}
	// Presentation-only drift retains by reference with the original receipt.
	retained, err := approval.EvaluateReuse(policy, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:next-presentation", Changed: []string{approval.DriftPresentation}})
	if err != nil {
		t.Fatalf("EvaluateReuse: %v", err)
	}
	if len(retained) != len(prior) || retained[0].Verdict != approval.ReuseRetainedByReference {
		t.Fatalf("retained=%+v", retained)
	}
	if retained[0].Decision.Digest() != receipt {
		t.Fatal("retained decision lost its original immutable receipt")
	}
	if retained[0].ApplicabilityLink != "digest:next-presentation" || retained[0].PolicyVersion != "reuse-2026.1" {
		t.Fatalf("retained=%+v", retained[0])
	}
	// Material amount drift requires reapproval.
	reapproval, err := approval.EvaluateReuse(policy, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:next-amount", Changed: []string{approval.DriftAmount}})
	if err != nil {
		t.Fatal(err)
	}
	if reapproval[0].Verdict != approval.ReuseReapprovalRequired {
		t.Fatalf("reapproval=%+v", reapproval[0])
	}
	// Unknown drift reports UNKNOWN, never silent retention.
	unknown, err := approval.EvaluateReuse(policy, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:next-weird", Changed: []string{"vibes"}})
	if err != nil {
		t.Fatal(err)
	}
	if unknown[0].Verdict != approval.ReuseUnknown {
		t.Fatalf("unknown=%+v", unknown[0])
	}
	// RED: hollow policy and links refuse.
	if _, err := approval.EvaluateReuse(approval.ReusePolicy{}, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:x"}); err == nil {
		t.Fatal("versionless policy evaluated")
	}
	if _, err := approval.EvaluateReuse(policy, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: priorDigest}); err == nil {
		t.Fatal("non-advancing link evaluated")
	}
	// No decision is edited in place.
	if prior[0].Digest() != receipt {
		t.Fatal("evaluation rebound the original decision")
	}
}

func TestTodo_REPLAN_002_Property(t *testing.T) {
	prior, priorDigest := reuseDecisions(t)
	lenient := approval.ReusePolicy{Version: "reuse-2026.1", RetainPresentationOnly: true}
	strict := approval.ReusePolicy{Version: "reuse-2026.1", RetainPresentationOnly: false}
	// Verdicts stay inside the closed vocabulary.
	for _, component := range []string{approval.DriftAmount, approval.DriftInterval, approval.DriftObligation, approval.DriftView, approval.DriftPresentation, "unknown-component"} {
		out, err := approval.EvaluateReuse(lenient, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:" + component, Changed: []string{component}})
		if err != nil {
			t.Fatalf("EvaluateReuse(%s): %v", component, err)
		}
		switch out[0].Verdict {
		case approval.ReuseRetainedByReference, approval.ReuseReapprovalRequired, approval.ReuseUnknown:
		default:
			t.Fatalf("verdict %q outside vocabulary", out[0].Verdict)
		}
	}
	// Presentation-only follows the policy both ways.
	presentation := approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:p", Changed: []string{approval.DriftPresentation}}
	kept, err := approval.EvaluateReuse(lenient, prior, presentation)
	if err != nil || kept[0].Verdict != approval.ReuseRetainedByReference {
		t.Fatalf("kept=%+v err=%v", kept, err)
	}
	dropped, err := approval.EvaluateReuse(strict, prior, presentation)
	if err != nil || dropped[0].Verdict != approval.ReuseReapprovalRequired {
		t.Fatalf("dropped=%+v err=%v", dropped, err)
	}
	// Material drift requires reapproval under every policy.
	for _, policy := range []approval.ReusePolicy{lenient, strict} {
		out, err := approval.EvaluateReuse(policy, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:m", Changed: []string{approval.DriftInterval, approval.DriftPresentation}})
		if err != nil || out[0].Verdict != approval.ReuseReapprovalRequired {
			t.Fatalf("material=%+v err=%v", out, err)
		}
	}
	// Empty drift retains vacuously under every policy.
	for _, policy := range []approval.ReusePolicy{lenient, strict} {
		out, err := approval.EvaluateReuse(policy, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:e"})
		if err != nil || out[0].Verdict != approval.ReuseRetainedByReference {
			t.Fatalf("empty=%+v err=%v", out, err)
		}
	}
}

func TestTodo_REPLAN_002_Mutation(t *testing.T) {
	prior, priorDigest := reuseDecisions(t)
	policy := approval.ReusePolicy{Version: "reuse-2026.1", RetainPresentationOnly: true}
	base, err := approval.EvaluateReuse(policy, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:base", Changed: []string{approval.DriftPresentation}})
	if err != nil || base[0].Verdict != approval.ReuseRetainedByReference {
		t.Fatalf("base=%+v err=%v", base, err)
	}
	// Adding one material component flips retention to reapproval.
	flipped, err := approval.EvaluateReuse(policy, prior, approval.SuccessorLink{PriorProposalDigest: priorDigest, NextProposalDigest: "digest:flip", Changed: []string{approval.DriftPresentation, approval.DriftObligation}})
	if err != nil || flipped[0].Verdict != approval.ReuseReapprovalRequired {
		t.Fatalf("flipped=%+v err=%v", flipped, err)
	}
	// The retained receipt never follows the successor: it stays bound to
	// the original decision.
	if flipped[0].Decision.Digest() != base[0].Decision.Digest() {
		t.Fatal("reuse rebound the original proposal binding")
	}
}
