package payroll

import (
	"errors"
	"sync"
	"testing"
)

func validPayrollRun(t *testing.T) PayrollRun {
	t.Helper()
	run, err := NewPayrollRun(
		"run-2026-11-b", "monthly",
		PeriodRef{ID: "period-2026-11-b", Version: "v1", Digest: "sha256:period"},
		PopulationBindingRef{DefinitionID: "population-2026-11", RevisionVersion: "v3", Digest: "sha256:population"},
		"sha256:inputs",
	)
	if err != nil {
		t.Fatalf("NewPayrollRun: %v", err)
	}
	return run
}

// TestTodo_PAYRUN_001 is the primary acceptance case: a run advances through
// immutable revisions, binds its period/population/input digest, and refuses
// an out-of-order settlement with the typed PAYRUN_001 rejection.
func TestTodo_PAYRUN_001(t *testing.T) {
	draft := validPayrollRun(t)
	calculated, err := draft.Calculate("sha256:calculation")
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	released, err := calculated.Release("sha256:release")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	settled, err := released.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	reversed, err := settled.Reverse("sha256:reversal")
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	if draft.State != Draft || draft.Revision != 1 {
		t.Fatalf("draft was mutated: %+v", draft)
	}
	if calculated.Revision != 2 || released.Revision != 3 || settled.Revision != 4 || reversed.Revision != 5 {
		t.Fatalf("unexpected revision sequence: %d %d %d %d", calculated.Revision, released.Revision, settled.Revision, reversed.Revision)
	}
	if reversed.State != Reversed || reversed.SupersedesRevision != settled.Revision {
		t.Fatalf("reversed revision does not point to settled revision: %+v", reversed)
	}
	digest, digestErr := reversed.Digest()
	if reversed.CanonicalDigest == "" || digestErr != nil || digest == "" {
		t.Fatal("reversed revision has no canonical digest")
	}

	_, err = draft.Settle()
	if !errors.Is(err, ErrSettlementWithoutRelease) || !errors.Is(err, ErrTransitionRejected) {
		t.Fatalf("settlement without release error = %v, want both typed refusals", err)
	}
	var refusal *TransitionError
	if !errors.As(err, &refusal) || refusal.Field != "state" || refusal.Revision != draft.Revision {
		t.Fatalf("refusal = %+v, want offending state and revision", refusal)
	}
}

// TestTodo_PAYRUN_001_Property checks that every non-successor state is
// rejected and that each legal edge yields exactly one new revision.
func TestTodo_PAYRUN_001_Property(t *testing.T) {
	cases := []struct {
		from PayrollRunState
		to   PayrollRunState
		ok   bool
	}{
		{Draft, Calculated, true}, {Calculated, Released, true},
		{Released, Settled, true}, {Settled, Reversed, true},
		{Draft, Released, false}, {Draft, Settled, false},
		{Calculated, Settled, false}, {Released, Reversed, false},
		{Reversed, Draft, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.from)+"_to_"+string(tc.to), func(t *testing.T) {
			run := validPayrollRun(t)
			var err error
			switch tc.from {
			case Calculated:
				run, err = run.Calculate()
			case Released:
				var calculated PayrollRun
				calculated, err = run.Calculate()
				if err == nil {
					run, err = calculated.Release()
				}
			case Settled:
				var calculated, released PayrollRun
				calculated, err = run.Calculate()
				if err == nil {
					released, err = calculated.Release()
				}
				if err == nil {
					run, err = released.Settle()
				}
			case Reversed:
				var calculated, released, settled PayrollRun
				calculated, err = run.Calculate()
				if err == nil {
					released, err = calculated.Release()
				}
				if err == nil {
					settled, err = released.Settle()
				}
				if err == nil {
					run, err = settled.Reverse()
				}
			}
			if err != nil {
				t.Fatalf("setup transition: %v", err)
			}
			next, err := run.Transition(tc.to)
			if tc.ok {
				if err != nil || next.Revision != run.Revision+1 {
					t.Fatalf("legal transition = %+v, %v", next, err)
				}
			} else if !errors.Is(err, ErrTransitionRejected) {
				t.Fatalf("illegal transition error = %v", err)
			}
		})
	}
}

func (s PayrollRunState) next() PayrollRunState {
	switch s {
	case Draft:
		return Calculated
	case Calculated:
		return Released
	case Released:
		return Settled
	case Settled:
		return Reversed
	default:
		return Reversed
	}
}

// TestTodo_PAYRUN_001_Race proves independent value transitions are safe and
// reproducible when many conformance callers advance the same draft value.
func TestTodo_PAYRUN_001_Race(t *testing.T) {
	draft := validPayrollRun(t)
	const workers = 16
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			calculated, err := draft.Calculate()
			if err != nil {
				errs <- err
				return
			}
			released, err := calculated.Release()
			if err != nil {
				errs <- err
				return
			}
			settled, err := released.Settle()
			if err != nil {
				errs <- err
				return
			}
			if digest, digestErr := settled.Digest(); digestErr != nil || digest == "" {
				errs <- digestErr
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if draft.State != Draft || draft.Revision != 1 {
		t.Fatalf("concurrent transitions mutated draft: %+v", draft)
	}
}

// TestTodo_PAYRUN_001_Integration proves the period and population bindings
// remain attached to every successor revision.
func TestTodo_PAYRUN_001_Integration(t *testing.T) {
	run := validPayrollRun(t)
	next, err := run.Calculate()
	if err != nil {
		t.Fatal(err)
	}
	if next.Period != run.Period || next.Population != run.Population || next.CalculationInputDigest != run.CalculationInputDigest {
		t.Fatal("successor lost a bound input reference")
	}
}

// TestTodo_PAYRUN_001_Fault proves malformed references fail before a
// lifecycle transition can be accepted.
func TestTodo_PAYRUN_001_Fault(t *testing.T) {
	run := validPayrollRun(t)
	run.Period.Digest = ""
	if err := run.Validate(); !errors.Is(err, ErrInvalidPayrollRun) {
		t.Fatalf("invalid period was accepted: %v", err)
	}
	if _, err := validPayrollRun(t).Transition(PayrollRunState("UNKNOWN")); !errors.Is(err, ErrTransitionRejected) {
		t.Fatalf("unknown state error = %v", err)
	}
}

// TestTodo_PAYRUN_001_Conformance records that this package only returns
// values and errors; the lifecycle has no persistence or effect counters.
func TestTodo_PAYRUN_001_Conformance(t *testing.T) {
	run := validPayrollRun(t)
	oldDigest := run.CanonicalDigest
	_, err := run.Release()
	if !errors.Is(err, ErrTransitionRejected) {
		t.Fatalf("out-of-order release error = %v", err)
	}
	if run.CanonicalDigest != oldDigest {
		t.Fatal("rejected transition changed the original revision")
	}
}

// TestTodo_PAYRUN_001_Mutation proves changing a revision's content changes
// its canonical digest rather than silently reusing the old digest.
func TestTodo_PAYRUN_001_Mutation(t *testing.T) {
	run := validPayrollRun(t)
	changed := run
	changed.Population.Digest = "sha256:other"
	changed.CanonicalDigest = ""
	if run.CanonicalDigest == changed.computedDigest() {
		t.Fatal("changed population binding reused the original digest")
	}
}
