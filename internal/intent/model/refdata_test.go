package model_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

func baseReferenceRelease() model.ReferenceRelease {
	return model.ReferenceRelease{
		ReleaseRef:     "refdata.job_family/2026a",
		Dataset:        model.DatasetJobFamily,
		ArtifactDigest: "sha256:bbbb",
		State:          model.RefReleaseReleased,
	}
}

// TestTodo_MODEL_018 is the PRIMARY test for governed reference-data
// releases.
//
// RED: simulation rejects unknown/retired job, position, location, currency,
// reason code or unversioned global dataset.
//
// GREEN: pilot references resolve by effective date and return
// release/provenance IDs.
func TestTodo_MODEL_018(t *testing.T) {
	released := baseReferenceRelease()
	draft := baseReferenceRelease()
	draft.ReleaseRef = "refdata.job_family/2026b-draft"
	draft.State = model.RefReleaseDraft

	releases := map[string]model.ReferenceRelease{
		released.ReleaseRef: released,
		draft.ReleaseRef:    draft,
	}

	engV1 := model.ReferenceValue{
		ReleaseRef:    released.ReleaseRef,
		Dataset:       model.DatasetJobFamily,
		Code:          "ENG",
		Effective:     mustInterval(t, 2025, time.January, 1, 2026, time.January, 1),
		Status:        model.StatusActive,
		ProvenanceRef: "prov:eng-v1",
	}
	engV2 := model.ReferenceValue{
		ReleaseRef:    released.ReleaseRef,
		Dataset:       model.DatasetJobFamily,
		Code:          "ENG",
		Effective:     mustOpenInterval(t, 2026, time.January, 1),
		Status:        model.StatusActive,
		ProvenanceRef: "prov:eng-v2",
	}
	retiredRole := model.ReferenceValue{
		ReleaseRef:    released.ReleaseRef,
		Dataset:       model.DatasetJobFamily,
		Code:          "OLD_ROLE",
		Effective:     mustOpenInterval(t, 2020, time.January, 1),
		Status:        model.StatusRetired,
		ProvenanceRef: "prov:old-role",
	}
	futureRole := model.ReferenceValue{
		ReleaseRef:    released.ReleaseRef,
		Dataset:       model.DatasetJobFamily,
		Code:          "FUTURE_ROLE",
		Effective:     mustOpenInterval(t, 2030, time.January, 1),
		Status:        model.StatusActive,
		ProvenanceRef: "prov:future-role",
	}
	draftCode := model.ReferenceValue{
		ReleaseRef:    draft.ReleaseRef,
		Dataset:       model.DatasetJobFamily,
		Code:          "DRAFT_ROLE",
		Effective:     mustOpenInterval(t, 2026, time.January, 1),
		Status:        model.StatusActive,
		ProvenanceRef: "prov:draft-role",
	}

	vals := []model.ReferenceValue{engV1, engV2, retiredRole, futureRole, draftCode}

	t.Run("RED", func(t *testing.T) {
		t.Run("unknown code", func(t *testing.T) {
			_, err := model.ResolveReference(releases, vals, model.DatasetJobFamily, "NOPE", instant(2026, time.June, 1))
			if !errors.Is(err, model.ErrUnknownReferenceValue) {
				t.Fatalf("resolved an unknown code: %v", err)
			}
		})

		t.Run("retired code", func(t *testing.T) {
			_, err := model.ResolveReference(releases, vals, model.DatasetJobFamily, "OLD_ROLE", instant(2026, time.June, 1))
			if !errors.Is(err, model.ErrReferenceValueRetired) {
				t.Fatalf("resolved a retired code: %v", err)
			}
		})

		t.Run("out of effective range", func(t *testing.T) {
			_, err := model.ResolveReference(releases, vals, model.DatasetJobFamily, "FUTURE_ROLE", instant(2026, time.June, 1))
			if !errors.Is(err, model.ErrReferenceValueOutOfEffectiveRange) {
				t.Fatalf("resolved a code outside its effective window: %v", err)
			}
		})

		t.Run("unversioned global dataset", func(t *testing.T) {
			_, err := model.ResolveReference(releases, vals, model.DatasetJobFamily, "DRAFT_ROLE", instant(2026, time.June, 1))
			if !errors.Is(err, model.ErrReferenceDatasetNotReleased) {
				t.Fatalf("resolved a code from an unreleased dataset: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		t.Run("resolves the version effective before the boundary", func(t *testing.T) {
			res, err := model.ResolveReference(releases, vals, model.DatasetJobFamily, "ENG", instant(2025, time.June, 1))
			if err != nil {
				t.Fatalf("resolve ENG at 2025-06-01: %v", err)
			}
			if res.ProvenanceRef != "prov:eng-v1" {
				t.Fatalf("provenance = %s, want prov:eng-v1", res.ProvenanceRef)
			}
			if res.ReleaseRef != released.ReleaseRef {
				t.Fatalf("release = %s, want %s", res.ReleaseRef, released.ReleaseRef)
			}
		})

		t.Run("resolves the version effective after the boundary", func(t *testing.T) {
			res, err := model.ResolveReference(releases, vals, model.DatasetJobFamily, "ENG", instant(2026, time.June, 1))
			if err != nil {
				t.Fatalf("resolve ENG at 2026-06-01: %v", err)
			}
			if res.ProvenanceRef != "prov:eng-v2" {
				t.Fatalf("provenance = %s, want prov:eng-v2", res.ProvenanceRef)
			}
			if res.ReleaseRef != released.ReleaseRef {
				t.Fatalf("release = %s, want %s", res.ReleaseRef, released.ReleaseRef)
			}
		})
	})
}

// TestTodo_MODEL_018_Property asserts the compiled reference-data release
// profile itself is well-formed: every state is reachable, DEPRECATED is the
// only terminal state, and every transition carries a retention class.
func TestTodo_MODEL_018_Property(t *testing.T) {
	profile := model.ReferenceReleaseLifecycleProfile()
	if err := profile.Validate(); err != nil {
		t.Fatalf("reference release profile does not validate: %v", err)
	}
	if len(profile.Terminal) != 1 || profile.Terminal[0] != model.RefReleaseDeprecated {
		t.Fatalf("terminal states = %v, want exactly [DEPRECATED]", profile.Terminal)
	}
	for _, tr := range profile.Transitions {
		if tr.RetentionClass == "" {
			t.Fatalf("transition %s->%s carries no retention class", tr.From, tr.To)
		}
	}
}

// TestTodo_MODEL_018_Golden pins the compiled reference-data release
// lifecycle profile.
func TestTodo_MODEL_018_Golden(t *testing.T) {
	profile := model.ReferenceReleaseLifecycleProfile()
	type rule struct {
		From, To           string
		RequiresGovernance bool
		RetentionClass     string
	}
	type doc struct {
		ID       string
		States   []string
		Initial  string
		Terminal []string
		Rules    []rule
	}
	d := doc{ID: profile.ID, Initial: string(profile.Initial)}
	for _, s := range profile.States {
		d.States = append(d.States, string(s))
	}
	for _, s := range profile.Terminal {
		d.Terminal = append(d.Terminal, string(s))
	}
	for _, tr := range profile.Transitions {
		d.Rules = append(d.Rules, rule{
			From: string(tr.From), To: string(tr.To),
			RequiresGovernance: tr.RequiresGovernance, RetentionClass: tr.RetentionClass,
		})
	}
	goldenJSON(t, "model_018_reference_release_profile.json", d)
}

// TestTodo_MODEL_018_Race advances independent copies of a reference release
// concurrently. AdvanceReferenceRelease is a pure function over its
// arguments, so concurrent callers must never observe a torn or shared
// result.
func TestTodo_MODEL_018_Race(t *testing.T) {
	rel := baseReferenceRelease()
	rel.State = model.RefReleaseDraft
	at := instant(2026, time.January, 1)
	var wg sync.WaitGroup
	results := make([]model.ReferenceRelease, 16)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			next, _, err := model.AdvanceReferenceRelease(rel, model.RefReleaseValidated, at, "")
			if err != nil {
				t.Errorf("concurrent advance %d: %v", i, err)
				return
			}
			results[i] = next
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r.State != model.RefReleaseValidated {
			t.Fatalf("result %d state = %s, want VALIDATED", i, r.State)
		}
	}
	if rel.State != model.RefReleaseDraft {
		t.Fatalf("the original release value was mutated by a concurrent call: %s", rel.State)
	}
}

// FuzzTodo_MODEL_018 fuzzes state transitions: AdvanceReferenceRelease must
// only ever succeed for an edge the compiled profile actually declares.
func FuzzTodo_MODEL_018(f *testing.F) {
	states := []string{"DRAFT", "VALIDATED", "APPROVED", "RELEASED", "DEPRECATED", "BOGUS"}
	f.Add(0, 1, true)
	f.Add(2, 3, false)
	f.Add(4, 0, true)
	f.Fuzz(func(t *testing.T, fromIdx, toIdx int, hasGovernance bool) {
		from := states[((fromIdx%len(states))+len(states))%len(states)]
		to := states[((toIdx%len(states))+len(states))%len(states)]
		rel := baseReferenceRelease()
		rel.State = lifecycle.StateID(from)
		gov := ""
		if hasGovernance {
			gov = "gov:1"
		}
		profile := model.ReferenceReleaseLifecycleProfile()
		declared := map[string]bool{}
		for _, s := range profile.States {
			declared[string(s)] = true
		}
		_, _, err := model.AdvanceReferenceRelease(rel, lifecycle.StateID(to), instant(2026, time.January, 1), gov)
		if !declared[from] {
			if err == nil {
				t.Fatalf("advanced from an undeclared state %q", from)
			}
			return
		}
		if _, ok := profile.Rule(lifecycle.StateID(from), lifecycle.StateID(to)); !ok {
			if err == nil {
				t.Fatalf("advanced %s->%s though the profile declares no such edge", from, to)
			}
		}
	})
}
