package simcontract_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/promotion/simcontract"
)

// TestPersistRefusesAnInvalidContract proves Store validates before it
// stores: a contract that fails Validate must never reach the map, so a
// caller cannot bypass section validation by going through Persist instead of
// Assemble.
func TestPersistRefusesAnInvalidContract(t *testing.T) {
	result, err := simcontract.Assemble(promotionFixtureInput(t))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	result.Writes = nil // corrupt an already-assembled contract by hand

	store := simcontract.NewMemoryStore()
	if _, _, err := store.Store(context.Background(), result); err == nil {
		t.Fatal("Store accepted a contract with no writes")
	}
	if store.Len() != 0 {
		t.Fatalf("store recorded %d artifacts for a refused Store call, want 0", store.Len())
	}
}

// TestPersistRefusesATamperedDigest proves Store checks the digest against
// the content it is attached to, so a contract whose Digest field was hand-
// edited after Assemble cannot be stored under the forged value.
func TestPersistRefusesATamperedDigest(t *testing.T) {
	result, err := simcontract.Assemble(promotionFixtureInput(t))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	result.Digest = "sha256:not-the-real-digest"

	store := simcontract.NewMemoryStore()
	if _, _, err := store.Store(context.Background(), result); err == nil {
		t.Fatal("Store accepted a contract with a tampered digest")
	}
}

// TestPersistLoadMissingDigest proves Load reports "not found" rather than an
// error for a digest nobody stored.
func TestPersistLoadMissingDigest(t *testing.T) {
	store := simcontract.NewMemoryStore()
	_, ok, err := store.Load(context.Background(), "sha256:never-stored")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ok {
		t.Fatal("Load reported found for a digest nobody stored")
	}
}

// The digest-conflict branch [simcontract.MemoryStore.Store] carries -- two
// artifacts that collide on one digest -- is exercised by
// TestMemoryStoreDigestConflict in persist_internal_test.go, the internal
// white-box counterpart of this file. It has to seed the store's map
// directly: [simcontract.SimulationResult.VerifyDigest] refuses a tampered
// digest (see TestPersistRefusesATamperedDigest above) before Store ever
// reaches the map, so an external caller cannot present two self-consistent
// results that collide without the store's own unexported state being poked
// first.
