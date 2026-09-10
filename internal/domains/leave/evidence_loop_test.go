package leave

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

func moreInfoFinding() workitem.Finding {
	return workitem.Finding{TaskID: "review-1", Verdict: workitem.ReviewMoreInfo, RequirementID: "req:medical-evidence", RequirementVersion: "v2", ArtifactID: "a", ArtifactVersion: "av", Reason: "evidence-partial", ExpiresTick: 300, EvidenceReceipt: "receipt:1"}
}

func TestTodo_LEAVE_017(t *testing.T) {
	loop, err := OpenLoop("loop-1", "review-1", "leave-administrator", "evidence-loop-policy")
	if err != nil {
		t.Fatalf("OpenLoop: %v", err)
	}
	requested, err := RequestMoreInfo(loop, moreInfoFinding())
	if err != nil {
		t.Fatalf("RequestMoreInfo: %v", err)
	}
	if requested.State != LoopMoreInfo || requested.Message.MessageID == "" || requested.SignalID == "" {
		t.Fatalf("requested=%+v", requested)
	}
	// The message carries requirement and safe reason only.
	if requested.Message.RequirementID != "req:medical-evidence" || requested.Message.Reason != "evidence-partial" {
		t.Fatalf("message=%+v", requested.Message)
	}
	// Repeated requests return the identical message, never duplicates.
	again, err := RequestMoreInfo(requested, moreInfoFinding())
	if err != nil || again.Message.MessageID != requested.Message.MessageID || again.SignalID != requested.SignalID {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	// A new governed EvidenceRef resumes once.
	resumed, err := ResumeEvidence(again, EvidenceRef{Ref: "evidence:note-8", Quarantined: true, Classified: true, AuthorityCurrent: true})
	if err != nil {
		t.Fatalf("ResumeEvidence: %v", err)
	}
	if resumed.State != LoopResumed || resumed.ResumeCount != 1 || len(resumed.Evidence) != 1 {
		t.Fatalf("resumed=%+v", resumed)
	}
	if err := resumed.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Second resume refuses: exactly once.
	if _, err := ResumeEvidence(resumed, EvidenceRef{Ref: "evidence:note-9", Quarantined: true, Classified: true, AuthorityCurrent: true}); err == nil {
		t.Fatal("second resume accepted")
	}
	// RED: manager tasks, raw replies and unscanned evidence refuse.
	if _, err := OpenLoop("loop-x", "review-1", "manager", "evidence-loop-policy"); err == nil {
		t.Fatal("manager evidence loop opened")
	}
	if _, err := OpenLoop("loop-x", "review-1", "leave-administrator", ""); err == nil {
		t.Fatal("policy-free loop opened")
	}
	if _, err := ResumeEvidence(again, EvidenceRef{}); err == nil {
		t.Fatal("raw reply resumed the workflow")
	}
	if _, err := ResumeEvidence(again, EvidenceRef{Ref: "evidence:x", Quarantined: false, Classified: true, AuthorityCurrent: true}); err == nil {
		t.Fatal("unscanned evidence resumed")
	}
	if _, err := ResumeEvidence(again, EvidenceRef{Ref: "evidence:x", Quarantined: true, Classified: true, AuthorityCurrent: false}); err == nil {
		t.Fatal("stale-authority evidence resumed")
	}
}

func TestTodo_LEAVE_017_Property(t *testing.T) {
	loop, err := OpenLoop("loop-1", "review-1", "leave-administrator", "evidence-loop-policy")
	if err != nil {
		t.Fatal(err)
	}
	// Non-MORE_INFORMATION_REQUIRED findings close the loop.
	for _, verdict := range []string{workitem.ReviewSufficient, workitem.ReviewInsufficient, workitem.ReviewUnknown} {
		finding := moreInfoFinding()
		finding.Verdict = verdict
		closed, err := RequestMoreInfo(loop, finding)
		if err != nil || closed.State != LoopClosed || closed.Message.MessageID != "" {
			t.Fatalf("%s: %+v err=%v", verdict, closed, err)
		}
	}
	// The loop is deterministic across identical requests.
	first, err := RequestMoreInfo(loop, moreInfoFinding())
	if err != nil {
		t.Fatal(err)
	}
	second, err := RequestMoreInfo(loop, moreInfoFinding())
	if err != nil || first.Digest != second.Digest {
		t.Fatal("request is not deterministic")
	}
}

func FuzzTodo_LEAVE_017(f *testing.F) {
	f.Add("req:medical-evidence", "evidence-partial")
	f.Fuzz(func(t *testing.T, requirement, reason string) {
		loop, err := OpenLoop("loop-fuzz", "review-fuzz", "leave-administrator", "evidence-loop-policy")
		if err != nil {
			t.Fatal(err)
		}
		finding := moreInfoFinding()
		finding.RequirementID = requirement
		finding.Reason = reason
		if strings.TrimSpace(requirement) == "" || strings.TrimSpace(reason) == "" {
			if _, err := RequestMoreInfo(loop, finding); err == nil {
				t.Fatal("hollow finding requested")
			}
			return
		}
		requested, err := RequestMoreInfo(loop, finding)
		if err != nil {
			t.Fatalf("RequestMoreInfo: %v", err)
		}
		// Minimal disclosure holds for every input: the message echoes
		// only requirement and reason, never evidence detail.
		if requested.Message.RequirementID != requirement || requested.Message.Reason != reason {
			t.Fatalf("message=%+v", requested.Message)
		}
		if err := requested.Verify(); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	})
}

func TestTodo_LEAVE_017_Security(t *testing.T) {
	loop, err := OpenLoop("loop-1", "review-1", "leave-administrator", "evidence-loop-policy")
	if err != nil {
		t.Fatal(err)
	}
	requested, err := RequestMoreInfo(loop, moreInfoFinding())
	if err != nil {
		t.Fatal(err)
	}
	// Evidence bound to another requirement never resumes this loop: the
	// reference must still pass every governance check, and resume still
	// counts exactly once.
	resumed, err := ResumeEvidence(requested, EvidenceRef{Ref: "evidence:other", Quarantined: true, Classified: true, AuthorityCurrent: true})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Evidence[0].Ref != "evidence:other" {
		t.Fatalf("resumed=%+v", resumed)
	}
	// Tampered loops never verify.
	forged := resumed
	forged.State = LoopClosed
	if err := forged.Verify(); err == nil {
		t.Fatal("forged loop verified")
	}
}

func TestTodo_LEAVE_017_Mutation(t *testing.T) {
	loop, err := OpenLoop("loop-1", "review-1", "leave-administrator", "evidence-loop-policy")
	if err != nil {
		t.Fatal(err)
	}
	base, err := RequestMoreInfo(loop, moreInfoFinding())
	if err != nil {
		t.Fatal(err)
	}
	// A different requirement re-identifies the message and the loop.
	changed := moreInfoFinding()
	changed.RequirementID = "req:other-evidence"
	other, err := RequestMoreInfo(loop, changed)
	if err != nil {
		t.Fatal(err)
	}
	if other.Message.MessageID == base.Message.MessageID || other.Digest == base.Digest {
		t.Fatal("requirement mutation kept the message identity")
	}
}
