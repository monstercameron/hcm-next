package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
)

func TestDispositionValid(t *testing.T) {
	for _, d := range []Disposition{DispositionCompleted, DispositionRetry, DispositionAbandoned} {
		if !d.Valid() {
			t.Errorf("%q is a declared disposition but Valid reports false", d)
		}
	}
	for _, d := range []Disposition{"", "DONE", "completed", "OK"} {
		if d.Valid() {
			t.Errorf("%q is not a declared disposition but Valid reports true", d)
		}
	}
}

// TestDispositionSettleState pins the whole mapping from what a dispatcher
// decided to the durable row state it settles into. A retry must land back on
// READY without a completion instant, because a row carrying one is terminal
// by migration 00026's own CHECK constraint.
func TestDispositionSettleState(t *testing.T) {
	for _, tc := range []struct {
		disposition Disposition
		state       string
		terminal    bool
	}{
		{DispositionCompleted, runtimestate.ReadyDone, true},
		{DispositionRetry, runtimestate.ReadyReady, false},
		{DispositionAbandoned, runtimestate.ReadyCancelled, true},
	} {
		state, terminal, err := tc.disposition.settleState()
		if err != nil {
			t.Fatalf("%s: settleState: %v", tc.disposition, err)
		}
		if state != tc.state || terminal != tc.terminal {
			t.Errorf("%s settles into %s/terminal=%v, want %s/terminal=%v",
				tc.disposition, state, terminal, tc.state, tc.terminal)
		}
	}
}

func TestDispositionSettleStateRefusesAnUndeclaredDisposition(t *testing.T) {
	_, _, err := Disposition("FORGET_ABOUT_IT").settleState()
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
}

func TestDispatcherFuncAdaptsAFunction(t *testing.T) {
	var seen Work
	want := Work{Row: runtimestate.ReadyWork{NodeID: "wait_effective_date", Attempt: 3}}
	f := DispatcherFunc(func(_ context.Context, work Work) (Disposition, error) {
		seen = work
		return DispositionCompleted, nil
	})
	got, err := f.Dispatch(context.Background(), want)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got != DispositionCompleted {
		t.Fatalf("disposition = %q, want COMPLETED", got)
	}
	if seen.Row.NodeID != want.Row.NodeID || seen.Row.Attempt != want.Row.Attempt {
		t.Fatalf("dispatcher saw %+v, want %+v", seen.Row, want.Row)
	}
}
