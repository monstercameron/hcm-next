package quarantine

import (
	"errors"
	"sync"
	"testing"
	"time"
)

var quarantineInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func quarantineRequest() Request {
	return Request{
		WorkflowID: "promotion", WorkflowVersion: 17, CompiledPlanDigest: "plan-v17",
		Reason: "INCIDENT_BAD_RULE", EvidenceRef: "incident:123",
		DeclaredBy: "principal:operator", ApprovedBy: "principal:reviewer",
		EffectiveAt: quarantineInstant,
	}
}

func TestTodo_WF_RUN_009(t *testing.T) {
	record, err := Quarantine(quarantineRequest())
	if err != nil {
		t.Fatalf("quarantine: %v", err)
	}
	if record.Action != ActionQuarantine || record.Digest() == "" || record.Event.Digest() == "" {
		t.Fatalf("record = %+v, want a digested quarantine action and event", record)
	}
	if err := record.Verify(); err != nil {
		t.Fatalf("verify quarantine: %v", err)
	}
	if !record.BlocksNewStart("promotion", 17, "plan-v17") || record.BlocksNewStart("promotion", 16, "other") {
		t.Fatal("quarantine did not block only the exact compiled version")
	}
	if CodeOf(record.AdmitStart("promotion", 17, "plan-v17")) != CodeStartQuarantined {
		t.Fatal("quarantine did not provide a typed start refusal")
	}
	dispositions := record.MarkLiveInstances([]LiveInstance{
		{InstanceID: "z", WorkflowID: "promotion", WorkflowVersion: 17, CompiledPlanDigest: "plan-v17", StepID: "review", Stage: "WAITING"},
		{InstanceID: "unrelated", WorkflowID: "promotion", WorkflowVersion: 16, CompiledPlanDigest: "other"},
		{InstanceID: "a", WorkflowID: "promotion", WorkflowVersion: 17, CompiledPlanDigest: "plan-v17", StepID: "review", Stage: "RUNNING"},
	})
	if len(dispositions) != 2 || dispositions[0].Instance.InstanceID != "a" || dispositions[1].Decision != MigrationPreviewRequired {
		t.Fatalf("dispositions = %+v, want sorted migration-preview decisions", dispositions)
	}

	lifted, err := Lift(record, LiftRequest{Reason: "VALIDATED_FIX", EvidenceRef: "validation:456", ReviewedBy: "principal:third-reviewer", EffectiveAt: quarantineInstant.Add(time.Hour)})
	if err != nil {
		t.Fatalf("lift: %v", err)
	}
	if lifted.Action != ActionLift || lifted.Digest() == record.Digest() || lifted.PreviousDigest != record.Digest() {
		t.Fatalf("lift = %+v, want a new linked record", lifted)
	}
	if !record.BlocksNewStart("promotion", 17, "plan-v17") || lifted.BlocksNewStart("promotion", 17, "plan-v17") {
		t.Fatal("lift changed the old record or failed to release the new record")
	}
	if err := lifted.AdmitStart("promotion", 17, "plan-v17"); err != nil {
		t.Fatalf("lifted version remains blocked: %v", err)
	}
}

func TestTodo_WF_RUN_009_Race(t *testing.T) {
	store := NewRegistry()
	const workers = 16
	var wg sync.WaitGroup
	results := make(chan Record, workers)
	errorsSeen := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := store.Quarantine(quarantineRequest())
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- r
		}()
	}
	wg.Wait()
	close(results)
	close(errorsSeen)
	if len(results) != 1 || len(errorsSeen) != workers-1 {
		t.Fatalf("quarantine race produced %d records and %d errors", len(results), len(errorsSeen))
	}
	history := store.History("promotion", 17, "plan-v17")
	if len(history) != 1 || !store.AllowsNewStart("promotion", 17, "other") || store.AllowsNewStart("promotion", 17, "plan-v17") {
		t.Fatalf("store state after race = %+v", history)
	}
}

func TestTodo_WF_RUN_009_Fault(t *testing.T) {
	bad := quarantineRequest()
	bad.ApprovedBy = bad.DeclaredBy
	if _, err := Quarantine(bad); CodeOf(err) != CodeSeparationOfDuties || !errors.Is(err, ErrQuarantine) {
		t.Fatalf("same-person quarantine error = %v, want separation-of-duties refusal", err)
	}
	record, err := Quarantine(quarantineRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lift(record, LiftRequest{Reason: "x", EvidenceRef: "e", ReviewedBy: record.DeclaredBy, EffectiveAt: quarantineInstant}); CodeOf(err) != CodeSeparationOfDuties {
		t.Fatalf("same-person lift error = %v", err)
	}
	if _, err := Lift(record, LiftRequest{Reason: "x", EvidenceRef: "e", ReviewedBy: "third", EffectiveAt: time.Time{}}); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("missing lift instant error = %v", err)
	}
}

func TestTodo_WF_RUN_009_Mutation(t *testing.T) {
	record, err := Quarantine(quarantineRequest())
	if err != nil {
		t.Fatal(err)
	}
	record.Reason = "tampered"
	if CodeOf(record.Verify()) != CodeRecordMutated {
		t.Fatalf("tampered record was accepted")
	}
	record, err = Quarantine(quarantineRequest())
	if err != nil {
		t.Fatal(err)
	}
	record.Event.RecordDigest = "tampered"
	if CodeOf(record.Verify()) != CodeEventMutated {
		t.Fatalf("tampered event was accepted")
	}
}
