package cycle

import (
	"errors"
	"testing"
	"time"
)

func cycleWithPhases() BusinessCycle { return validCycle() }

func TestTodo_CYCLE_002(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BusinessCycle)
		want   error
	}{
		{name: "gap", mutate: func(c *BusinessCycle) { c.Phases[0].Start = c.Periods[0].Start.Add(time.Hour) }, want: ErrPhaseGap},
		{name: "overlap", mutate: func(c *BusinessCycle) {
			c.Phases = append(c.Phases, Phase{ID: "close", Name: "CLOSE", Start: c.Periods[0].Start.Add(-time.Hour), End: c.Periods[0].End})
		}, want: ErrPhaseOverlap},
		{name: "unreachable", mutate: func(c *BusinessCycle) {
			c.Phases = append(c.Phases, Phase{ID: "orphan", Name: "ORPHAN", Start: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)})
		}, want: ErrPhaseUnreachable},
		{name: "invalid-cutoff", mutate: func(c *BusinessCycle) { c.Policies.Cutoff = "NOT_A_CUTOFF" }, want: ErrCutoff},
		{name: "ambiguous-boundary", mutate: func(c *BusinessCycle) {}, want: ErrPhaseBoundary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cycleWithPhases()
			if tt.name == "ambiguous-boundary" {
				c.Phases = append(c.Phases, Phase{ID: "open", Name: "OPEN", Start: c.Periods[0].Start, End: c.Periods[0].End})
			}
			tt.mutate(&c)
			if err := ValidatePhaseWindows(c); !errors.Is(err, tt.want) {
				t.Fatalf("ValidatePhaseWindows()=%v, want %v", err, tt.want)
			}
		})
	}
}

func TestTodo_CYCLE_002_Property(t *testing.T) {
	c := cycleWithPhases()
	g, err := CompilePhaseGraph(c)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := g.PhaseAt(c.Periods[0].Start); !ok || got.ID != "open" {
		t.Fatalf("start boundary did not select phase: %+v, %v", got, ok)
	}
	if _, ok := g.PhaseAt(c.Periods[0].End); ok {
		t.Fatal("end boundary must be outside a half-open phase window")
	}
	// Compilation is detached from the source definition.
	c.Phases[0].ID = "changed"
	if g.Phases[0].ID != "open" {
		t.Fatal("compiled graph aliases source phase data")
	}
}

func TestTodo_CYCLE_002_Fault(t *testing.T) {
	c := cycleWithPhases()
	c.Phases = []Phase{
		{ID: "open", Name: "OPEN", Start: c.Periods[0].Start, End: c.Periods[0].Start.Add(time.Hour)},
		{ID: "close", Name: "CLOSE", Start: c.Periods[0].Start.Add(2 * time.Hour), End: c.Periods[0].End},
	}
	if err := ValidatePhaseWindows(c); !errors.Is(err, ErrPhaseGap) {
		t.Fatalf("gap fault returned %v", err)
	}
}

// TestCompiledPhaseGraphCarriesDeclaredOperations verifies CYCLE-002's GREEN
// requirement that the compiled phase graph defines allowed operations (and
// entry/exit conditions, obligations), detached from the source definition,
// and that Explain reports the phase active at an instant with the rule
// that decided it.
func TestCompiledPhaseGraphCarriesDeclaredOperations(t *testing.T) {
	c := cycleWithPhases()
	c.Phases[0].AllowedOperations = []string{OperationRebindPopulation, "SUBMIT_ADJUSTMENT"}
	c.Phases[0].EntryConditions = []string{"prior period closed"}
	c.Phases[0].ExitConditions = []string{"cutoff reached"}
	c.Phases[0].Obligations = []string{"notify payroll"}

	g, err := CompilePhaseGraph(c)
	if err != nil {
		t.Fatal(err)
	}
	got := g.Phases[0]
	if len(got.AllowedOperations) != 2 || got.AllowedOperations[0] != OperationRebindPopulation {
		t.Fatalf("AllowedOperations not carried through compile: %v", got.AllowedOperations)
	}
	if len(got.EntryConditions) != 1 || len(got.ExitConditions) != 1 || len(got.Obligations) != 1 {
		t.Fatalf("declared rule set not carried through compile: %+v", got)
	}

	// Mutating the compiled slice must never reach back into the source.
	got.AllowedOperations[0] = "MUTATED"
	if c.Phases[0].AllowedOperations[0] == "MUTATED" {
		t.Fatal("compiled AllowedOperations aliases source phase data")
	}

	inside := g.Explain(c.Periods[0].Start)
	if !inside.Found || inside.Phase.ID != "open" || inside.Rule == "" {
		t.Fatalf("Explain did not resolve the active phase with a rule: %+v", inside)
	}
	outside := g.Explain(c.Periods[0].End)
	if outside.Found || outside.Rule == "" {
		t.Fatalf("Explain must report no phase (with a rule) past the last window: %+v", outside)
	}
}
