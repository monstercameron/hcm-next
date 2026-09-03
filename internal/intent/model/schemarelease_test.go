package model_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

func baseRelease() model.SchemaRelease {
	return model.SchemaRelease{
		ReleaseRef:         "hcmnext.people.v1.Employment@v3",
		ArtifactDigest:     "sha256:aaaa",
		CompatibilityClass: model.CompatBackward,
		HasMigration:       true,
		State:              model.ReleaseDraft,
	}
}

// TestTodo_MODEL_017 is the PRIMARY test for the schema release lifecycle.
//
// RED: release rejects unresolved consumers, incompatible change, missing
// migration or publication without approval.
//
// GREEN: draft -> validated -> approved -> active -> superseded/retired
// retains consumer adoption and rollback evidence.
func TestTodo_MODEL_017(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		t.Run("unresolved consumers block retirement", func(t *testing.T) {
			rel := baseRelease()
			rel.State = model.ReleaseSuperseded
			rel.UnresolvedConsumers = []string{"consumer.payroll_export/v1"}
			if _, _, err := model.AdvanceRelease(rel, model.ReleaseRetired, instant(2026, time.January, 1), "gov:1"); !errors.Is(err, model.ErrInvalidSchemaRelease) {
				t.Fatalf("retired with unresolved consumers: %v", err)
			}
		})

		t.Run("incompatible change with no migration", func(t *testing.T) {
			rel := baseRelease()
			rel.State = model.ReleaseApproved
			rel.CompatibilityClass = model.CompatBreaking
			rel.HasMigration = false
			if _, _, err := model.AdvanceRelease(rel, model.ReleaseActive, instant(2026, time.January, 1), "gov:1"); !errors.Is(err, model.ErrInvalidSchemaRelease) {
				t.Fatalf("activated a BREAKING change with no migration: %v", err)
			}
		})

		t.Run("publication without approval", func(t *testing.T) {
			rel := baseRelease()
			rel.State = model.ReleaseApproved
			if _, _, err := model.AdvanceRelease(rel, model.ReleaseActive, instant(2026, time.January, 1), ""); !errors.Is(err, model.ErrInvalidSchemaRelease) {
				t.Fatalf("published with no governance decision: %v", err)
			}
		})

		t.Run("approval without governance", func(t *testing.T) {
			rel := baseRelease()
			rel.State = model.ReleaseValidated
			if _, _, err := model.AdvanceRelease(rel, model.ReleaseApproved, instant(2026, time.January, 1), ""); !errors.Is(err, model.ErrInvalidSchemaRelease) {
				t.Fatalf("approved with no governance decision: %v", err)
			}
		})

		t.Run("undeclared transition", func(t *testing.T) {
			rel := baseRelease()
			rel.State = model.ReleaseDraft
			if _, _, err := model.AdvanceRelease(rel, model.ReleaseActive, instant(2026, time.January, 1), "gov:1"); !errors.Is(err, model.ErrUnknownReleaseTransition) {
				t.Fatalf("jumped DRAFT->ACTIVE directly: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		rel := baseRelease()
		at := instant(2026, time.January, 1)
		var err error
		var rec model.ReleaseTransition

		rel, rec, err = model.AdvanceRelease(rel, model.ReleaseValidated, at, "")
		if err != nil {
			t.Fatalf("DRAFT->VALIDATED: %v", err)
		}
		if rec.From != model.ReleaseDraft || rec.To != model.ReleaseValidated {
			t.Fatalf("transition record = %+v", rec)
		}

		rel, _, err = model.AdvanceRelease(rel, model.ReleaseApproved, at, "gov:approve-1")
		if err != nil {
			t.Fatalf("VALIDATED->APPROVED: %v", err)
		}

		rel, _, err = model.AdvanceRelease(rel, model.ReleaseActive, at, "gov:publish-1")
		if err != nil {
			t.Fatalf("APPROVED->ACTIVE: %v", err)
		}
		if rel.State != model.ReleaseActive {
			t.Fatalf("state = %s, want ACTIVE", rel.State)
		}

		rel, _, err = model.AdvanceRelease(rel, model.ReleaseSuperseded, at, "")
		if err != nil {
			t.Fatalf("ACTIVE->SUPERSEDED: %v", err)
		}

		rel, rec, err = model.AdvanceRelease(rel, model.ReleaseRetired, at, "gov:retire-1")
		if err != nil {
			t.Fatalf("SUPERSEDED->RETIRED: %v", err)
		}
		if rel.State != model.ReleaseRetired {
			t.Fatalf("final state = %s, want RETIRED", rel.State)
		}
		if rec.RetentionClass == "" {
			t.Fatalf("retirement transition carries no retention class")
		}
		if rec.GovernanceDecisionRef == "" {
			t.Fatalf("retirement transition carries no rollback/governance evidence")
		}
	})
}

// TestTodo_MODEL_017_Property asserts the compiled release profile itself is
// well-formed: every state is reachable, RETIRED is the only terminal state,
// and every transition carries a retention class.
func TestTodo_MODEL_017_Property(t *testing.T) {
	profile := model.ReleaseLifecycleProfile()
	if err := profile.Validate(); err != nil {
		t.Fatalf("release profile does not validate: %v", err)
	}
	if len(profile.Terminal) != 1 || profile.Terminal[0] != model.ReleaseRetired {
		t.Fatalf("terminal states = %v, want exactly [RETIRED]", profile.Terminal)
	}
	for _, tr := range profile.Transitions {
		if tr.RetentionClass == "" {
			t.Fatalf("transition %s->%s carries no retention class", tr.From, tr.To)
		}
	}
}

// TestTodo_MODEL_017_Golden pins the compiled release lifecycle profile.
func TestTodo_MODEL_017_Golden(t *testing.T) {
	profile := model.ReleaseLifecycleProfile()
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
	goldenJSON(t, "model_017_release_profile.json", d)
}

// TestTodo_MODEL_017_Race advances independent copies of a release
// concurrently. AdvanceRelease is a pure function over its arguments, so
// concurrent callers must never observe a torn or shared result.
func TestTodo_MODEL_017_Race(t *testing.T) {
	rel := baseRelease()
	at := instant(2026, time.January, 1)
	var wg sync.WaitGroup
	results := make([]model.SchemaRelease, 16)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			next, _, err := model.AdvanceRelease(rel, model.ReleaseValidated, at, "")
			if err != nil {
				t.Errorf("concurrent advance %d: %v", i, err)
				return
			}
			results[i] = next
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r.State != model.ReleaseValidated {
			t.Fatalf("result %d state = %s, want VALIDATED", i, r.State)
		}
	}
	if rel.State != model.ReleaseDraft {
		t.Fatalf("the original release value was mutated by a concurrent call: %s", rel.State)
	}
}

// TestTodo_MODEL_017_Fault proves a rejected transition leaves the release
// value completely unchanged: a caller can always retry from the same state.
func TestTodo_MODEL_017_Fault(t *testing.T) {
	rel := baseRelease()
	rel.State = model.ReleaseApproved
	before := rel
	if _, _, err := model.AdvanceRelease(rel, model.ReleaseActive, instant(2026, time.January, 1), ""); err == nil {
		t.Fatalf("expected publication without approval to fail")
	}
	if !reflect.DeepEqual(rel, before) {
		t.Fatalf("a failed AdvanceRelease call mutated its input: got %+v, want %+v", rel, before)
	}
}

// FuzzTodo_MODEL_017 fuzzes state transitions: AdvanceRelease must only ever
// succeed for an edge the compiled profile actually declares.
func FuzzTodo_MODEL_017(f *testing.F) {
	states := []string{"DRAFT", "VALIDATED", "APPROVED", "ACTIVE", "SUPERSEDED", "RETIRED", "BOGUS"}
	f.Add(0, 1, true, true)
	f.Add(2, 3, false, true)
	f.Add(5, 0, true, false)
	f.Fuzz(func(t *testing.T, fromIdx, toIdx int, hasGovernance, hasMigration bool) {
		from := states[((fromIdx%len(states))+len(states))%len(states)]
		to := states[((toIdx%len(states))+len(states))%len(states)]
		rel := baseRelease()
		rel.State = lifecycle.StateID(from)
		rel.CompatibilityClass = model.CompatBackward
		rel.HasMigration = hasMigration
		gov := ""
		if hasGovernance {
			gov = "gov:1"
		}
		profile := model.ReleaseLifecycleProfile()
		declared := map[string]bool{}
		for _, s := range profile.States {
			declared[string(s)] = true
		}
		_, _, err := model.AdvanceRelease(rel, lifecycle.StateID(to), instant(2026, time.January, 1), gov)
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
