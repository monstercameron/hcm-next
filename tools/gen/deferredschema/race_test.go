package deferredschema

import (
	"sync"
	"testing"
)

// TestTodo_DB_016_Race proves Generate and Validate are safe to call from
// many goroutines at once and always agree: every goroutine computes the
// same preview digest and the same validation report, so a caller (a future
// CI job validating this package alongside other generators) can shard the
// work without risking a torn or nondeterministic result. The project runs
// without -race (see the lane's standing rules), so this is a logical
// determinism proof rather than a data-race detector run.
func TestTodo_DB_016_Race(t *testing.T) {
	const workers = 16
	domains := Domains()
	root := testRepoRoot(t)

	digests := make([]string, workers)
	reportsClean := make([]bool, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			set, err := Generate()
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = set.Digest()
			report, err := Validate(root, domains, set)
			if err != nil {
				errs[i] = err
				return
			}
			reportsClean[i] = report.Empty()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	first := digests[0]
	for i, d := range digests {
		if d != first {
			t.Fatalf("worker %d digest %s != worker 0 digest %s (nondeterministic under concurrency)", i, d, first)
		}
		if !reportsClean[i] {
			t.Fatalf("worker %d produced a non-empty validation report", i)
		}
	}
}
