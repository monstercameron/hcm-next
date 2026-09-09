package simcontract_test

import (
	"context"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
)

// TestTodo_PROMO_004_Race is PROMO-004's RACE matrix test: a burst of
// concurrent [simcontract.Persist.Store] calls for one identical digest must
// all succeed, must all return the same stored artifact, and must persist it
// exactly once.
func TestTodo_PROMO_004_Race(t *testing.T) {
	result, err := simcontract.Assemble(promotionFixtureInput(t))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	store := simcontract.NewMemoryStore()

	const concurrency = 64
	var wg sync.WaitGroup
	stored := make([]simcontract.SimulationResult, concurrency)
	already := make([]bool, concurrency)
	errs := make([]error, concurrency)

	ctx := context.Background()
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			stored[i], already[i], errs[i] = store.Store(ctx, result)
		}(i)
	}
	wg.Wait()

	firstStored := 0
	for i := 0; i < concurrency; i++ {
		if errs[i] != nil {
			t.Fatalf("Store[%d]: %v", i, errs[i])
		}
		if stored[i].Digest != result.Digest {
			t.Fatalf("Store[%d] digest = %s, want %s", i, stored[i].Digest, result.Digest)
		}
		if !already[i] {
			firstStored++
		}
	}
	if firstStored != 1 {
		t.Fatalf("%d of %d concurrent Store calls reported the first insert, want exactly 1", firstStored, concurrency)
	}
	if store.Len() != 1 {
		t.Fatalf("store recorded %d artifacts after a concurrent burst of one digest, want 1", store.Len())
	}

	// Concurrent Load calls see the one recorded artifact too.
	wg.Add(concurrency)
	loaded := make([]simcontract.SimulationResult, concurrency)
	found := make([]bool, concurrency)
	loadErrs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			loaded[i], found[i], loadErrs[i] = store.Load(ctx, result.Digest)
		}(i)
	}
	wg.Wait()
	for i := 0; i < concurrency; i++ {
		if loadErrs[i] != nil {
			t.Fatalf("Load[%d]: %v", i, loadErrs[i])
		}
		if !found[i] {
			t.Fatalf("Load[%d] did not find the stored artifact", i)
		}
		if loaded[i].Digest != result.Digest {
			t.Fatalf("Load[%d] digest = %s, want %s", i, loaded[i].Digest, result.Digest)
		}
	}
}
