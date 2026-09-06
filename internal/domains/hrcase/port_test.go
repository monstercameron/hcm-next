package hrcase

import (
	"context"
	"errors"
	"testing"
)

func testRequest() HRRequest {
	return HRRequest{
		CaseID: "case-001", Type: "BENEFITS", TypeVersion: "2026-01", Service: "leave",
		Requester: "principal:requester", Subject: "worker:subject", Purpose: "case handling",
		Classification: "CONFIDENTIAL", Retention: "PERMANENT",
		Participants: []Participant{{Role: "REQUESTER", Principal: "principal:requester"}},
	}
}

func testRevision(t *testing.T) CaseRevision {
	t.Helper()
	revision, err := testRequest().Revision()
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}
	return revision
}

// TestHRCaseLifecycleRejectsMissingTypeSubjectPurposeAndIllegalTransition is
// CASE-001's primary contract test.
func TestHRCaseLifecycleRejectsMissingTypeSubjectPurposeAndIllegalTransition(t *testing.T) {
	fields := []struct {
		name string
		edit func(*HRRequest)
	}{
		{"type", func(r *HRRequest) { r.Type = " " }},
		{"version", func(r *HRRequest) { r.TypeVersion = "" }},
		{"service", func(r *HRRequest) { r.Service = "" }},
		{"requester", func(r *HRRequest) { r.Requester = "" }},
		{"subject", func(r *HRRequest) { r.Subject = "" }},
		{"purpose", func(r *HRRequest) { r.Purpose = "" }},
		{"classification", func(r *HRRequest) { r.Classification = "" }},
		{"retention", func(r *HRRequest) { r.Retention = "" }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			r := testRequest()
			field.edit(&r)
			if _, err := NewCase(r); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("NewCase error = %v, want ErrInvalidRequest", err)
			}
		})
	}

	aggregate, err := NewCase(testRequest())
	if err != nil {
		t.Fatalf("NewCase: %v", err)
	}
	result, err := aggregate.Apply(Closed, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("DRAFT -> CLOSED error = %v, want ErrInvalidTransition", err)
	}
	if result.Revision.CaseID != "" || result.Revision.Revision != 0 || len(result.Revision.Participants) != 0 || len(result.Emissions.Events) != 0 || len(result.Emissions.Work) != 0 || len(result.Emissions.Outbox) != 0 {
		t.Fatalf("invalid command emitted result: %+v", result)
	}
}

// TestTodo_CASE_001_Property checks that every successful transition is an
// append-only successor and that the business context is carried forward.
func TestTodo_CASE_001_Property(t *testing.T) {
	aggregate, err := NewCase(testRequest())
	if err != nil {
		t.Fatalf("NewCase: %v", err)
	}
	for _, step := range []struct {
		to          CaseState
		disposition string
	}{
		{Open, ""}, {Waiting, "awaiting employee"}, {Paused, "calendar paused"},
		{Open, "resumed"}, {Resolved, "resolved with answer"}, {Reopened, "appeal requested"},
	} {
		before := aggregate.Current()
		result, err := aggregate.Apply(step.to, step.disposition)
		if err != nil {
			t.Fatalf("Apply %s: %v", step.to, err)
		}
		got := result.Revision
		if got.Revision != before.Revision+1 || got.Previous != before.Revision {
			t.Fatalf("successor lineage = revision %d previous %d, want %d/%d", got.Revision, got.Previous, before.Revision+1, before.Revision)
		}
		if got.CaseID != before.CaseID || got.Definition != before.Definition || got.Requester != before.Requester || got.Subject != before.Subject || got.Purpose != before.Purpose || got.Classification != before.Classification || got.Retention != before.Retention {
			t.Fatal("successor changed immutable case context")
		}
		if len(result.Emissions.Events) != 1 || len(result.Emissions.Work) != 0 || len(result.Emissions.Outbox) != 0 {
			t.Fatalf("success emission shape = %+v", result.Emissions)
		}
	}
}

// TestTodo_CASE_001_Golden pins the closed state vocabulary and its lifecycle
// edge decisions.
func TestTodo_CASE_001_Golden(t *testing.T) {
	states := []CaseState{Draft, Open, Waiting, Paused, Resolved, Closed, Reopened, Appealed}
	for _, state := range states {
		if !validState(state) {
			t.Fatalf("state %q is not valid", state)
		}
	}
	for _, edge := range [][2]CaseState{{Draft, Open}, {Open, Waiting}, {Open, Paused}, {Open, Resolved}, {Resolved, Reopened}, {Resolved, Appealed}, {Appealed, Closed}} {
		if !TransitionAllowed(edge[0], edge[1]) {
			t.Fatalf("expected transition %s -> %s", edge[0], edge[1])
		}
	}
	for _, edge := range [][2]CaseState{{Draft, Closed}, {Closed, Open}, {Closed, Reopened}, {Waiting, Draft}, {Appealed, Open}} {
		if TransitionAllowed(edge[0], edge[1]) {
			t.Fatalf("unexpected transition %s -> %s", edge[0], edge[1])
		}
	}
}

// TestTodo_CASE_001_Security proves terminal history cannot be edited and
// participant data is copied across the append-only boundary.
func TestTodo_CASE_001_Security(t *testing.T) {
	aggregate, err := NewCase(testRequest())
	if err != nil {
		t.Fatalf("NewCase: %v", err)
	}
	if _, err := aggregate.Apply(Open, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := aggregate.Apply(Resolved, "closed with disposition"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := aggregate.Apply(Closed, "final disposition"); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := aggregate.Apply(Reopened, "attempt to edit terminal"); !errors.Is(err, ErrTerminalCase) {
		t.Fatalf("edit terminal error = %v, want ErrTerminalCase", err)
	}
	history := aggregate.Revisions()
	if len(history) != 4 || history[2].Disposition != "closed with disposition" || history[3].Disposition != "final disposition" {
		t.Fatalf("terminal history was overwritten: %+v", history)
	}
	history[0].Participants[0].Principal = "attacker"
	if aggregate.Revisions()[0].Participants[0].Principal == "attacker" {
		t.Fatal("Revisions exposed mutable participant storage")
	}
}

// TestTodo_CASE_001_Conformance proves the memory implementation satisfies
// the additive domain persistence port.
func TestTodo_CASE_001_Conformance(t *testing.T) {
	var _ Store = NewMemoryStore()
	store := NewMemoryStore()
	revision := testRevision(t)
	if err := store.AppendRevision(context.Background(), "tenant-a", revision, 1); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}
	got, err := store.Current(context.Background(), "tenant-a", revision.CaseID)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got.Digest != revision.Digest {
		t.Fatalf("Current digest = %s, want %s", got.Digest, revision.Digest)
	}
}

// TestTodo_CASE_001_Mutation pins duplicate and stale append boundaries.
func TestTodo_CASE_001_Mutation(t *testing.T) {
	store := NewMemoryStore()
	first := testRevision(t)
	if err := store.AppendRevision(context.Background(), "tenant-a", first, 1); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := store.AppendRevision(context.Background(), "tenant-a", first, 1); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate append error = %v, want ErrStoreDuplicate", err)
	}
	next, err := Apply(first, Open, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	err = store.AppendRevision(context.Background(), "tenant-a", next.Revision, 3)
	if !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("skipped sequence error = %v, want ErrStoreStaleCAS", err)
	}
	var typed *StoreError
	if !errors.As(err, &typed) || typed.Code != StoreStaleCASCode {
		t.Fatalf("stale error = %T/%v, want typed %s", err, err, StoreStaleCASCode)
	}
}
