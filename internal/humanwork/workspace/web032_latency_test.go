//go:build !race

// This file carries the WEB-032 wall-clock budget only, behind `!race`.
//
// The race detector's instrumentation changes both timing and allocation
// counts, so a p95 measured under it says nothing about the product's real
// latency: on Linux CI the untagged version of this test reported p95 5.37ms
// against a 2ms budget purely because it was running inside the detector.
// .github/workflows/tests.yml makes the same distinction for the other
// interaction budgets (internal/humanwork/productui,
// tools/uxqual/render/journey), which use this same `!race` tag.
//
// `!race` resolves under the default build context, so this file still runs on
// every ordinary `go test` — including the coverage sweep — and
// tools/policy/racepolicy still sees a resolving test file for this package.

package workspace

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

func TestTodo_WEB_032_Latency(t *testing.T) {
	manifest, err := BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		t.Fatal(err)
	}
	budget := latencygate.Budget{Name: "asset manifest serialization", P95: 2 * time.Millisecond, Warmups: 3, Samples: 25}
	result, err := latencygate.Measure(budget, func() error {
		if _, err := manifest.CanonicalJSON(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatalf("%v (%s)", err, result)
	}
	t.Logf("%s", result)
}
