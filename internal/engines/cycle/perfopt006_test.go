package cycle

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func perfCycleInputs(t testing.TB) (GovernedCycle, PhaseGraph, []CutoffResolution, time.Time) {
	t.Helper()
	c := validCycle()
	c.Phases[0].AllowedOperations = []string{string(OperationClose), "SUBMIT_ADJUSTMENT"}
	r, err := NewRevision(c, "cycle-009", "v1", c.Periods[0].Start, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	governed, err := NewOpenCycle(r)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := CompilePhaseGraph(c)
	if err != nil {
		t.Fatal(err)
	}
	date, err := values.ParseLocalDate("2026-01-15")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return governed, graph, []CutoffResolution{{
		PhaseID: "open", Status: CutoffResolved, EffectiveDate: date,
		Instant: time.Date(2026, 1, 30, 21, 0, 0, 0, time.UTC), Rule: "fixture cutoff pinned by calendar revision calendar.us/1",
	}}, at
}

func perfCycle(t testing.TB, locked bool) []byte {
	t.Helper()
	governed, graph, cutoffs, at := perfCycleInputs(t)
	if locked {
		governed.State = StateLocked
	}
	explanation, err := ExplainCurrentPhase(governed, graph, cutoffs, at)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(explanation)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTodo_PERFOPT_006_Golden(t *testing.T) {
	assertCycleGolden(t, "perfopt006_cycle_open.json", perfCycle(t, false))
	assertCycleGolden(t, "perfopt006_cycle_locked.json", perfCycle(t, true))
}

func TestTodo_PERFOPT_006(t *testing.T) {
	governed, graph, cutoffs, at := perfCycleInputs(t)
	const maxAllocs = 78 // pre-change benchmark: 104 allocs/op; ceiling is 25% lower
	got := testing.AllocsPerRun(10, func() {
		if _, err := ExplainCurrentPhase(governed, graph, cutoffs, at); err != nil {
			t.Fatal(err)
		}
	})
	if got > maxAllocs {
		t.Fatalf("allocations = %v, want <= %d", got, maxAllocs)
	}
}

func BenchmarkTodo_PERFOPT_006(b *testing.B) {
	governed, graph, cutoffs, at := perfCycleInputs(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ExplainCurrentPhase(governed, graph, cutoffs, at); err != nil {
			b.Fatal(err)
		}
	}
}

func assertCycleGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with HCMNEXT_UPDATE_GOLDEN=1)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s differs", path)
	}
}
