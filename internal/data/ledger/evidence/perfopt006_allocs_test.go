//go:build !race

// This file carries the PERFOPT-006 allocation ceiling only, behind `!race`.
//
// testing.AllocsPerRun counts allocations made by the running binary, and the
// race detector's instrumentation adds its own: on Linux CI the untagged
// version of this test measured 175 allocations against a 156 ceiling derived
// from an uninstrumented benchmark, so it was measuring the detector rather
// than evidence.Build. .github/workflows/tests.yml already states the rule
// this follows -- wall-clock and allocation budgets are measured outside the
// race detector, "whose instrumentation changes timing and allocations".
//
// `!race` resolves under the default build context, so this file still runs on
// every ordinary `go test` -- including the coverage sweep -- and
// tools/policy/racepolicy still sees a resolving test file for this package.

package evidence_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
)

func TestTodo_PERFOPT_006(t *testing.T) {
	content := perfEvidenceContent(t, 10)
	const maxAllocs = 156 // pre-change benchmark: 209 allocs/op; ceiling is 25% lower
	got := testing.AllocsPerRun(10, func() {
		if _, err := evidence.Build(content); err != nil {
			t.Fatal(err)
		}
	})
	if got > maxAllocs {
		t.Fatalf("allocations = %v, want <= %d", got, maxAllocs)
	}
}
