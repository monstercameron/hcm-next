package recovery

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/experience/channelparity"
	"github.com/monstercameron/hcm-next/internal/experience/presentation"
	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
)

func ux005Input() presentation.Input {
	return presentation.Input{Dimensions: lifecycle.Dimensions{
		Request: lifecycle.RequestSubmitted, Execution: lifecycle.ExecutionExecuting,
		Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyConsistent,
		Obligation: lifecycle.ObligationNotApplicable,
	}, Authorization: presentation.AuthorizationAllowed, Freshness: presentation.FreshnessFresh,
		Operational: presentation.OperationalReady, HasResult: true, ExternalOutcomeKnown: true,
		EvidenceRefs: []string{"evidence:attempt-1"}}
}

// TestTodo_UX_005 is the primary state/action contract: each degraded result
// remains visibly distinct and exposes only the actions safe for that state.
func TestTodo_UX_005(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*presentation.Input)
		state   presentation.State
		actions []presentation.Action
	}{
		{"partial", func(i *presentation.Input) { i.Dimensions.Consistency = lifecycle.ConsistencyDegraded }, presentation.StatePartialDegraded, []presentation.Action{presentation.ActionRefresh, presentation.ActionOpenRepair, presentation.ActionRequestHelp}},
		{"ambiguous", func(i *presentation.Input) {
			i.ExternalOutcomeKnown = false
			i.Dimensions.Execution = lifecycle.ExecutionCommitted
		}, presentation.StateUnknownAmbiguous, []presentation.Action{presentation.ActionInvestigate, presentation.ActionRefresh, presentation.ActionRequestHelp}},
		{"repair", func(i *presentation.Input) { i.Dimensions.Execution = lifecycle.ExecutionRepairRequired }, presentation.StateRepairRequired, []presentation.Action{presentation.ActionOpenRepair, presentation.ActionReview, presentation.ActionRequestHelp}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := ux005Input()
			tc.mutate(&in)
			got := presentation.Resolve(in)
			if got.State != tc.state {
				t.Fatalf("state=%s want %s", got.State, tc.state)
			}
			if len(got.Actions) != len(tc.actions) {
				t.Fatalf("actions=%v want %v", got.Actions, tc.actions)
			}
			for n, a := range got.Actions {
				if a.Action != tc.actions[n] || !a.Enabled {
					t.Fatalf("action %d=%+v want enabled %s", n, a, tc.actions[n])
				}
			}
		})
	}
}

// TestTodo_UX_005_Integration proves the projection carries correlation-like
// evidence references and does not allow a forged caller slice to mutate it.
func TestTodo_UX_005_Integration(t *testing.T) {
	in := ux005Input()
	got := presentation.Resolve(in)
	in.EvidenceRefs[0] = "forged"
	if len(got.EvidenceRefs) != 1 || got.EvidenceRefs[0] != "evidence:attempt-1" {
		t.Fatalf("evidence=%v", got.EvidenceRefs)
	}
	if got.State != presentation.StateRunning {
		t.Fatalf("state=%s", got.State)
	}
}

// TestTodo_UX_005_Fault proves an unknown committed outcome is never rendered
// as completion and never offers cancellation.
func TestTodo_UX_005_Fault(t *testing.T) {
	in := ux005Input()
	in.ExternalOutcomeKnown = false
	in.Dimensions.Execution = lifecycle.ExecutionCommitted
	p := presentation.Resolve(in)
	if p.State != presentation.StateUnknownAmbiguous {
		t.Fatalf("state=%s", p.State)
	}
	for _, a := range p.Actions {
		if a.Action == presentation.ActionCancelIfSafe && a.Enabled {
			t.Fatal("ambiguous outcome offered cancel")
		}
	}
}

// TestTodo_UX_005_Browser checks the browser-facing projection disables effects
// when freshness is stale while retaining inspection/recovery affordances.
func TestTodo_UX_005_Browser(t *testing.T) {
	in := ux005Input()
	in.Freshness = presentation.FreshnessStale
	p := presentation.Resolve(in)
	for _, a := range p.Actions {
		if a.Action == presentation.ActionSubmit && a.Enabled {
			t.Fatal("stale result offered submit")
		}
	}
	if len(p.Actions) == 0 {
		t.Fatal("stale projection removed safe recovery actions")
	}
}

// TestTodo_UX_005_Mutation proves replaying a clicked effect with the same
// logical key returns the original receipt, while changed input is rejected.
func TestTodo_UX_005_Mutation(t *testing.T) {
	e := channelparity.NewExecutor()
	r := channelparity.Request{IntentID: "worker.update", IntentVersion: "1", Subject: "worker-1", Actor: "worker-1", IdempotencyKey: "ux005-click", Simulation: true, StepUp: true, Confirmation: true, WorkflowVersion: "wf-1", Payload: map[string]string{"title": "Senior Analyst"}}
	one, err := e.Apply(r, channelparity.Desktop)
	if err != nil {
		t.Fatal(err)
	}
	two, err := e.Apply(r, channelparity.Mobile)
	if err != nil {
		t.Fatal(err)
	}
	if one.Outcome.IntentDigest != two.Outcome.IntentDigest {
		t.Fatal("duplicate click changed logical transaction")
	}
	r.Payload["title"] = "Director"
	if _, err = e.Apply(r, channelparity.Desktop); !errors.Is(err, channelparity.ErrDuplicate) {
		t.Fatalf("changed replay err=%v", err)
	}
}
