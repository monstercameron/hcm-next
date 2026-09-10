package workitem

import (
	"sync"
	"testing"
)

func reviewTask() ReviewTask {
	return ReviewTask{
		TaskID: "review-1", PolicyID: "pol-1",
		RequirementID: "req:medical-evidence", RequirementVersion: "v2",
		ArtifactID: "artifact:note-7", ArtifactVersion: "av-3", ArtifactCurrent: "av-3",
		ArtifactQuarantined: true, Scope: []string{"leave-evidence"},
		ReviewerRole: RoleLeaveAdministrator, ExpiresTick: 300,
	}
}

func reviewHarness(t *testing.T) *Reviewer {
	t.Helper()
	reviewer := NewReviewer()
	if err := reviewer.RegisterPolicy(compartmentPolicy()); err != nil {
		t.Fatalf("RegisterPolicy: %v", err)
	}
	return reviewer
}

func TestTodo_WORK_009(t *testing.T) {
	reviewer := reviewHarness(t)
	finding, err := reviewer.Complete(reviewTask(), ReviewSufficient, "evidence-clear", "receipt:review-1", 150)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if finding.Verdict != ReviewSufficient || finding.RequirementVersion != "v2" || finding.ArtifactVersion != "av-3" {
		t.Fatalf("finding=%+v", finding)
	}
	if finding.EvidenceReceipt == "" || finding.Digest == "" || finding.ExpiresTick != 300 {
		t.Fatalf("finding=%+v", finding)
	}
	if err := finding.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: free-form verdicts and reasons refuse.
	if _, err := reviewer.Complete(ReviewTask{TaskID: "review-2", PolicyID: "pol-1", ReviewerRole: RoleLeaveAdministrator, ExpiresTick: 300, ArtifactVersion: "av-3", ArtifactCurrent: "av-3", ArtifactQuarantined: true, RequirementID: "r", RequirementVersion: "v"}, "ELIGIBLE", "evidence-clear", "receipt:x", 150); err == nil {
		t.Fatal("free-form eligibility verdict completed")
	}
	task := reviewTask()
	task.TaskID = "review-3"
	if _, err := reviewer.Complete(task, ReviewSufficient, "patient looks fine, approve", "receipt:x", 150); err == nil {
		t.Fatal("free-form reason completed")
	}
	// Stale and unquarantined artifacts refuse.
	stale := reviewTask()
	stale.TaskID = "review-4"
	stale.ArtifactCurrent = "av-4"
	if _, err := reviewer.Complete(stale, ReviewSufficient, "evidence-clear", "receipt:x", 150); err == nil {
		t.Fatal("stale artifact completed")
	}
	dirty := reviewTask()
	dirty.TaskID = "review-5"
	dirty.ArtifactQuarantined = false
	if _, err := reviewer.Complete(dirty, ReviewSufficient, "evidence-clear", "receipt:x", 150); err == nil {
		t.Fatal("unquarantined artifact completed")
	}
	// Duplicate completion returns the identical finding, never a divergent one.
	again, err := reviewer.Complete(reviewTask(), ReviewSufficient, "evidence-clear", "receipt:review-1", 150)
	if err != nil || again.Digest != finding.Digest {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	if _, err := reviewer.Complete(reviewTask(), ReviewInsufficient, "evidence-contradictory", "receipt:review-1", 150); err == nil {
		t.Fatal("divergent duplicate completed")
	}
}

func TestTodo_WORK_009_Property(t *testing.T) {
	reviewer := reviewHarness(t)
	// Every verdict in the vocabulary completes against a bound task.
	for i, verdict := range []string{ReviewSufficient, ReviewInsufficient, ReviewMoreInfo, ReviewUnknown} {
		task := reviewTask()
		task.TaskID = "prop-" + verdict
		_ = i
		finding, err := reviewer.Complete(task, verdict, "evidence-partial", "receipt:x", 150)
		if err != nil {
			t.Fatalf("Complete(%s): %v", verdict, err)
		}
		if finding.Verdict != verdict {
			t.Fatalf("finding=%+v", finding)
		}
	}
	// Expired tasks never complete.
	reviewer2 := reviewHarness(t)
	if _, err := reviewer2.Complete(reviewTask(), ReviewSufficient, "evidence-clear", "receipt:x", 301); err == nil {
		t.Fatal("expired task completed")
	}
}

func TestTodo_WORK_009_Race(t *testing.T) {
	reviewer := reviewHarness(t)
	const workers = 16
	var wg sync.WaitGroup
	findings := make([]Finding, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			findings[i], errs[i] = reviewer.Complete(reviewTask(), ReviewSufficient, "evidence-clear", "receipt:review-1", 150)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if findings[i].Digest != findings[0].Digest {
			t.Fatalf("worker %d diverged", i)
		}
	}
}

func TestTodo_WORK_009_Security(t *testing.T) {
	reviewer := reviewHarness(t)
	// Managers hold no evidence grant: completion refuses.
	managed := reviewTask()
	managed.TaskID = "sec-1"
	managed.ReviewerRole = RoleManager
	if _, err := reviewer.Complete(managed, ReviewSufficient, "evidence-clear", "receipt:x", 150); err == nil {
		t.Fatal("manager completed a restricted review")
	}
	// Unknown policies refuse.
	reviewer2 := NewReviewer()
	if _, err := reviewer2.Complete(reviewTask(), ReviewSufficient, "evidence-clear", "receipt:x", 150); err == nil {
		t.Fatal("policy-free review completed")
	}
	// Forged findings never verify.
	finding, err := reviewer.Complete(reviewTask(), ReviewSufficient, "evidence-clear", "receipt:review-1", 150)
	if err != nil {
		t.Fatal(err)
	}
	finding.Verdict = ReviewInsufficient
	if err := finding.Verify(); err == nil {
		t.Fatal("forged finding verified")
	}
}

func TestTodo_WORK_009_Mutation(t *testing.T) {
	reviewer := reviewHarness(t)
	base, err := reviewer.Complete(reviewTask(), ReviewSufficient, "evidence-clear", "receipt:review-1", 150)
	if err != nil {
		t.Fatal(err)
	}
	// A different verdict is a different finding with its own seal.
	changed := reviewTask()
	changed.TaskID = "mut-1"
	other, err := reviewer.Complete(changed, ReviewMoreInfo, "evidence-partial", "receipt:mut-1", 150)
	if err != nil {
		t.Fatal(err)
	}
	if other.Digest == base.Digest {
		t.Fatal("verdict mutation kept the finding digest")
	}
	if err := other.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}
