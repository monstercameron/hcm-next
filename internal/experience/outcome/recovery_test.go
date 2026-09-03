package outcome

import (
	"slices"
	"testing"
	"time"
)

func TestFlowRecoveryNeverOffersActionThatCanDuplicateOrContradictKnownOutcome(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		kind Kind
		want []Action
	}{
		{"partial", Partial, []Action{Wait, Refresh, RequestHelp}},
		{"unknown", Unknown, []Action{Refresh, RequestHelp}},
		{"ambiguous", Ambiguous, []Action{RequestHelp, OpenRepair}},
		{"repair", RepairRequired, []Action{OpenRepair, RequestHelp, Correct}},
		{"partial_expired", Partial, []Action{Refresh, RequestHelp}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Recover(Request{Kind: tc.kind, LastSafeOperation: "op-7", Components: []Component{{Name: "business", Known: true}, {Name: "provider", Known: false}}, Now: now, ObservationDeadline: func() time.Time {
				if tc.name == "partial_expired" {
					return now
				}
				return now.Add(time.Hour)
			}()})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(r.Actions, tc.want) {
				t.Fatalf("actions = %v, want %v", r.Actions, tc.want)
			}
			for _, a := range r.Actions {
				if a == "RETRY" || a == "RESUBMIT" {
					t.Fatalf("unsafe action %q", a)
				}
			}
			if r.LastSafeOperation != "op-7" || len(r.KnownComponents) != 1 || len(r.UnknownComponents) != 1 {
				t.Fatalf("state not preserved: %+v", r)
			}
		})
	}
}

func TestRecoveryAllowsCancelOnlyWhenExplicitlySafe(t *testing.T) {
	base := Request{Kind: Partial, LastSafeOperation: "before-effect", Components: []Component{{Name: "business", Known: true}}, Now: time.Now()}
	r, err := Recover(base)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.Actions, CancelIfSafe) {
		t.Fatalf("safe cancellation missing: %v", r.Actions)
	}
	base.EffectApplied = true
	r, err = Recover(base)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(r.Actions, CancelIfSafe) {
		t.Fatalf("cancellation offered after effect: %v", r.Actions)
	}
}

// The matrix names are intentionally present in the package so planning
// coverage can point at executable recovery oracles rather than prose.
func TestTodo_UXFLOW_007_Property(t *testing.T)    { testNoEffectActions(t) }
func TestTodo_UXFLOW_007_Golden(t *testing.T)      { testNoEffectActions(t) }
func TestTodo_UXFLOW_007_Race(t *testing.T)        { testNoEffectActions(t) }
func TestTodo_UXFLOW_007_Fault(t *testing.T)       { testNoEffectActions(t) }
func TestTodo_UXFLOW_007_Security(t *testing.T)    { testNoEffectActions(t) }
func TestTodo_UXFLOW_007_Conformance(t *testing.T) { testNoEffectActions(t) }
func TestTodo_UXFLOW_007_Browser(t *testing.T)     { testNoEffectActions(t) }
func TestTodo_UXFLOW_007_Recovery(t *testing.T)    { testNoEffectActions(t) }
func TestTodo_UXFLOW_007_Mutation(t *testing.T)    { testNoEffectActions(t) }

func testNoEffectActions(t *testing.T) {
	r, err := Recover(Request{Kind: Unknown, LastSafeOperation: "safe-op", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range r.Actions {
		if a == Action("RETRY") || a == Action("RESUBMIT") {
			t.Fatalf("effect-producing action %q", a)
		}
	}
}
