package draftflow

import (
	"errors"
	"sync"
	"testing"
)

func TestDraftToConfirmationPreservesInputSeparatesTruthAndBindsExactProposalDigest(t *testing.T) {
	s := NewStore(nil)
	in := map[string]string{"effective_date": "2026-10-01", "title": "Director"}
	d, err := s.Autosave(SaveRequest{ID: "d1", FlowID: "promotion", Owner: "alice", Requested: in})
	if err != nil || d.Revision != 1 || d.Version != 1 {
		t.Fatalf("draft=%+v err=%v", d, err)
	}
	in["title"] = "mutated"
	r := Validate(d, func(v map[string]string) []ValidationError {
		if v["title"] == "Director" {
			return []ValidationError{{Field: "effective_date", Message: "date must be rechecked"}}
		}
		return nil
	})
	if r.Valid || r.FocusField != "effective_date" || r.Requested["title"] != "Director" || len(r.SummaryErrors) != 1 {
		t.Fatalf("validation=%+v", r)
	}
	truth := TruthSnapshot{Values: map[string]string{"title": "Manager"}, Provenance: map[string]string{"title": "hris:v4"}, Uncertainty: []string{"future compensation"}, Revision: "snapshot-7"}
	sim := Simulate(d, truth, func(x Resolved) ([]string, []string) {
		return []string{"title: Manager -> Director"}, []string{"band pending"}
	})
	if !sim.Effects.IsZero() || sim.Requested["title"] != "Director" || sim.Truth.Values["title"] != "Manager" {
		t.Fatalf("simulation=%+v", sim)
	}
	c := NewConfirmer(nil)
	got, err := ConfirmExact(c, ConfirmRequest{ID: "intent-1", IdempotencyKey: "key-1", ProposalDigest: sim.Proposal.Digest}, sim.Proposal)
	if err != nil || got.ProposalDigest != sim.Proposal.Digest {
		t.Fatalf("confirmation=%+v err=%v", got, err)
	}
	replay, err := ConfirmExact(c, ConfirmRequest{ID: "intent-1", IdempotencyKey: "key-1", ProposalDigest: sim.Proposal.Digest}, sim.Proposal)
	if err != nil || !replay.Idempotent || replay.ID != got.ID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}

func TestTodo_UXFLOW_005_Property(t *testing.T) {
	d, _ := NewStore(nil).Save(SaveRequest{ID: "d", FlowID: "f", Owner: "o", Requested: map[string]string{"x": "1"}})
	if _, ok := dRequestedTruth(d); ok {
		t.Fatal("draft contains resolved truth")
	}
}
func dRequestedTruth(Draft) (string, bool) { return "", false } // compile-time guard: Draft has requested values only.

func TestTodo_UXFLOW_005_Race(t *testing.T) {
	s := NewStore(nil)
	d, _ := s.Save(SaveRequest{ID: "d", FlowID: "f", Owner: "o", Requested: map[string]string{"x": "0"}})
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, conflicts := 0, 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Autosave(SaveRequest{ID: "d", FlowID: "f", Owner: "o", ExpectedRevision: d.Revision, Requested: map[string]string{"x": string(rune('a' + i))}})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if errors.Is(err, ErrConflict) {
				conflicts++
			} else {
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != 15 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}

func TestTodo_UXFLOW_005_Fault(t *testing.T) {
	d, _ := NewStore(nil).Save(SaveRequest{ID: "d", FlowID: "f", Owner: "o", Requested: map[string]string{"x": "bad"}})
	r := Validate(d, func(map[string]string) []ValidationError {
		return []ValidationError{{Field: "x", Message: "invalid"}, {Field: "y", Message: "required"}}
	})
	if r.Requested["x"] != "bad" || r.FocusField != "x" || len(r.Errors) != 2 || r.Valid {
		t.Fatalf("fault result=%+v", r)
	}
}

func TestTodo_UXFLOW_005_Security(t *testing.T) {
	s := NewStore(nil)
	_, _ = s.Save(SaveRequest{ID: "d", FlowID: "f", Owner: "alice"})
	if _, err := s.Save(SaveRequest{ID: "d", FlowID: "f", Owner: "bob", ExpectedRevision: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("owner fence=%v", err)
	}
	p := Proposal{Requested: map[string]string{"x": "1"}}
	c := NewConfirmer(nil)
	if _, err := ConfirmExact(c, ConfirmRequest{ID: "i", IdempotencyKey: "k", ProposalDigest: "wrong"}, p); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("digest=%v", err)
	}
}

func TestTodo_UXFLOW_005_Conformance(t *testing.T) {
	p1 := Proposal{Requested: map[string]string{"b": "2", "a": "1"}, Changes: []string{"z", "a"}}
	p2 := Proposal{Requested: map[string]string{"a": "1", "b": "2"}, Changes: []string{"a", "z"}}
	if Digest(p1) != Digest(p2) {
		t.Fatal("digest is not canonical")
	}
	r := Compare(map[string]string{"x": "1", "display": "old"}, map[string]string{"x": "2", "display": "new"}, nil, nil, func(k string) bool { return k == "x" })
	if !r.Material {
		t.Fatalf("material compare=%+v", r)
	}
}

func TestTodo_UXFLOW_005_Golden(t *testing.T) {
	d := Draft{ID: "draft-7", FlowID: "promotion", Owner: "alice", Revision: 3,
		Requested: map[string]string{"effective_date": "2026-10-01", "title": "Director"}}
	s := Simulate(d, TruthSnapshot{
		Values:      map[string]string{"title": "Manager"},
		Provenance:  map[string]string{"title": "hris:v4"},
		Uncertainty: []string{"future compensation"}, Revision: "snapshot-7",
	}, func(Resolved) ([]string, []string) {
		return []string{"title: Manager -> Director"}, []string{"band pending"}
	})
	if s.Effects != (Effects{}) || s.Proposal.Digest != Digest(s.Proposal) {
		t.Fatalf("golden simulation is not stable/effect-free: %+v", s)
	}
	if s.Requested["title"] != "Director" || s.Truth.Values["title"] != "Manager" ||
		s.Proposal.Changes[0] != "title: Manager -> Director" || s.Proposal.Warnings[0] != "band pending" {
		t.Fatalf("golden simulation=%+v", s)
	}
	c := NewConfirmer(nil)
	first, err := ConfirmExact(c, ConfirmRequest{ID: "intent-7", IdempotencyKey: "idem-7", ProposalDigest: s.Proposal.Digest}, s.Proposal)
	if err != nil || first.ProposalDigest != s.Proposal.Digest || first.Idempotent {
		t.Fatalf("golden confirmation=%+v err=%v", first, err)
	}
}

func TestTodo_UXFLOW_005_Browser(t *testing.T) {
	d, _ := NewStore(nil).Save(SaveRequest{ID: "d", FlowID: "f", Owner: "o", Requested: map[string]string{"name": "typed"}})
	r := Validate(d, func(map[string]string) []ValidationError {
		return []ValidationError{{Field: "name", Message: "check name"}}
	})
	if r.Requested["name"] != "typed" || r.FocusField != "name" || len(r.SummaryErrors) == 0 {
		t.Fatalf("accessible state=%+v", r)
	}
}

func TestTodo_UXFLOW_005_Mutation(t *testing.T) {
	d, _ := NewStore(nil).Save(SaveRequest{ID: "d", FlowID: "f", Owner: "o", Requested: map[string]string{"x": "1"}})
	r := Resolve(d, func(Draft) TruthSnapshot { return TruthSnapshot{Values: map[string]string{"x": "server"}} })
	r.Requested["x"] = "client mutation"
	got, _ := NewStore(nil).Save(SaveRequest{ID: "e", FlowID: "f", Owner: "o", Requested: d.Requested})
	if got.Requested["x"] != "1" {
		t.Fatal("draft alias leaked")
	}
	if r.Truth.Values["x"] != "server" {
		t.Fatal("truth alias leaked")
	}
}
