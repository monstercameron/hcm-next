package object

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// corruptReplicaByte reaches into the unexported replica record for
// (objectID, location) and flips one byte of its stored envelope bytes,
// returning false if the flip had no effect (so callers, including the
// fuzz oracle, never assert on a no-op mutation).
func corruptReplicaByte(t testing.TB, i *IntegrityStore, objectID string, location ReplicaLocation, index int, flip byte) bool {
	t.Helper()
	i.mu.Lock()
	defer i.mu.Unlock()
	locs := i.replicas[objectID]
	rec, ok := locs[location]
	if !ok || !rec.Present || len(rec.Bytes) == 0 {
		return false
	}
	idx := index % len(rec.Bytes)
	if idx < 0 {
		idx += len(rec.Bytes)
	}
	before := rec.Bytes[idx]
	after := before ^ flip
	if after == before {
		return false
	}
	mutated := append([]byte(nil), rec.Bytes...)
	mutated[idx] = after
	rec.Bytes = mutated
	locs[location] = rec
	return true
}

// dropReplica removes a previously published replica entirely, modelling a
// disk failure or lost copy.
func dropReplica(t testing.TB, i *IntegrityStore, objectID string, location ReplicaLocation) {
	t.Helper()
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.replicas[objectID], location)
}

// truncateReplica shortens a replica's stored bytes, modelling a partial
// write or a copy that never fully landed.
func truncateReplica(t testing.TB, i *IntegrityStore, objectID string, location ReplicaLocation, n int) {
	t.Helper()
	i.mu.Lock()
	defer i.mu.Unlock()
	locs := i.replicas[objectID]
	rec, ok := locs[location]
	if !ok {
		t.Fatalf("truncateReplica: %s/%s not registered", objectID, location)
	}
	if n > len(rec.Bytes) {
		n = len(rec.Bytes)
	}
	rec.Bytes = append([]byte(nil), rec.Bytes[:n]...)
	locs[location] = rec
}

// TestTodo_ARTIFACT_006 is the PRIMARY test: a healthy object verifies
// clean and produces an immutable receipt naming the object, the outcome,
// and when the check ran.
func TestTodo_ARTIFACT_006(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	integ, err := NewIntegrityStore(store)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("payroll bytes for a healthy object")
	if _, err := store.Put(context.Background(), ctxA, "object-1", "text/plain", plaintext); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := integ.PublishReplica("object-1", "primary"); err != nil {
		t.Fatalf("publish primary: %v", err)
	}

	receipt, err := integ.Verify(context.Background(), "object-1", "primary")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if receipt.ObjectID != "object-1" {
		t.Fatalf("receipt.ObjectID = %q, want object-1", receipt.ObjectID)
	}
	if receipt.Outcome != OutcomeVerified {
		t.Fatalf("receipt.Outcome = %q, want %q", receipt.Outcome, OutcomeVerified)
	}
	if receipt.At.IsZero() {
		t.Fatal("receipt.At is zero")
	}
	if integ.Quarantined("object-1") {
		t.Fatal("healthy object is quarantined")
	}

	got, info, err := integ.Get(context.Background(), ctxA, "object-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("get = %q, want %q", got, plaintext)
	}
	if info.ArtifactID != "object-1" {
		t.Fatalf("info.ArtifactID = %q", info.ArtifactID)
	}

	// The receipt log is immutable: mutating a copy returned by Receipts
	// must never affect what is stored internally.
	receipts := integ.Receipts("object-1")
	if len(receipts) == 0 {
		t.Fatal("no receipts recorded")
	}
	receipts[0].Outcome = "TAMPERED"
	receipts[0].Detail = "tampered"
	again := integ.Receipts("object-1")
	if again[0].Outcome == "TAMPERED" {
		t.Fatal("mutating a returned receipt copy changed the stored receipt")
	}
}

// TestTodo_ARTIFACT_006_Fault proves each RED condition independently: bit
// rot, generation swap, a missing replica, and a partial replica must each
// end quarantined/unavailable, and Get must fail rather than serve suspect
// bytes.
func TestTodo_ARTIFACT_006_Fault(t *testing.T) {
	t.Run("BitRot", func(t *testing.T) {
		m, ctxA, _, _ := sealedFixture(t)
		store, _ := NewSealedObjectStore(m)
		integ, _ := NewIntegrityStore(store)
		if _, err := store.Put(context.Background(), ctxA, "obj", "text/plain", []byte("bit rot victim")); err != nil {
			t.Fatal(err)
		}
		if _, err := integ.PublishReplica("obj", "primary"); err != nil {
			t.Fatal(err)
		}
		if !corruptReplicaByte(t, integ, "obj", "primary", 5, 0xFF) {
			t.Fatal("corruption had no effect")
		}

		receipt, err := integ.Verify(context.Background(), "obj", "primary")
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if receipt.Outcome != OutcomeBitRot {
			t.Fatalf("outcome = %q, want %q", receipt.Outcome, OutcomeBitRot)
		}
		if !integ.Quarantined("obj") {
			t.Fatal("bit-rotted object was not quarantined")
		}
		if _, _, err := integ.Get(context.Background(), ctxA, "obj"); !errors.Is(err, ErrQuarantined) || !errors.Is(err, ErrBitRot) {
			t.Fatalf("get on bit-rotted object = %v, want ErrQuarantined+ErrBitRot", err)
		}
	})

	t.Run("GenerationSwap", func(t *testing.T) {
		m, ctxA, _, _ := sealedFixture(t)
		store, _ := NewSealedObjectStore(m)
		integ, _ := NewIntegrityStore(store)
		if _, err := store.Put(context.Background(), ctxA, "obj", "text/plain", []byte("generation one")); err != nil {
			t.Fatal(err)
		}
		if _, err := integ.PublishReplica("obj", "primary"); err != nil {
			t.Fatal(err)
		}
		// Advance the object to generation 2 without ever republishing the
		// replica: "primary" is now a perfectly valid, self-consistent
		// envelope for generation 1 — its own digest matches its own
		// bytes — but it is stale relative to the object's current
		// generation. A digest check alone would wrongly call this
		// healthy; identity plus generation must both match.
		newInfo, err := store.PutNewGeneration(context.Background(), ctxA, "obj", "text/plain", []byte("generation two"))
		if err != nil {
			t.Fatalf("put new generation: %v", err)
		}
		if newInfo.Generation != 2 {
			t.Fatalf("generation = %d, want 2", newInfo.Generation)
		}

		receipt, err := integ.Verify(context.Background(), "obj", "primary")
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if receipt.Outcome != OutcomeGenerationSwap {
			t.Fatalf("outcome = %q, want %q", receipt.Outcome, OutcomeGenerationSwap)
		}
		if !integ.Quarantined("obj") {
			t.Fatal("generation-swapped object was not quarantined")
		}
		if _, _, err := integ.Get(context.Background(), ctxA, "obj"); !errors.Is(err, ErrQuarantined) || !errors.Is(err, ErrGenerationSwap) {
			t.Fatalf("get on generation-swapped object = %v, want ErrQuarantined+ErrGenerationSwap", err)
		}
	})

	t.Run("MissingReplica", func(t *testing.T) {
		m, ctxA, _, _ := sealedFixture(t)
		store, _ := NewSealedObjectStore(m)
		integ, _ := NewIntegrityStore(store)
		if _, err := store.Put(context.Background(), ctxA, "obj", "text/plain", []byte("will go missing")); err != nil {
			t.Fatal(err)
		}
		if _, err := integ.PublishReplica("obj", "primary"); err != nil {
			t.Fatal(err)
		}
		dropReplica(t, integ, "obj", "primary")

		receipt, err := integ.Verify(context.Background(), "obj", "primary")
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if receipt.Outcome != OutcomeMissingReplica {
			t.Fatalf("outcome = %q, want %q", receipt.Outcome, OutcomeMissingReplica)
		}
		if !integ.Quarantined("obj") {
			t.Fatal("object with a missing replica was not quarantined")
		}
		if _, _, err := integ.Get(context.Background(), ctxA, "obj"); !errors.Is(err, ErrQuarantined) || !errors.Is(err, ErrReplicaMissing) {
			t.Fatalf("get on missing-replica object = %v, want ErrQuarantined+ErrReplicaMissing", err)
		}
	})

	t.Run("PartialReplica", func(t *testing.T) {
		m, ctxA, _, _ := sealedFixture(t)
		store, _ := NewSealedObjectStore(m)
		integ, _ := NewIntegrityStore(store)
		if _, err := store.Put(context.Background(), ctxA, "obj", "text/plain", []byte("a fairly long payload so truncation is unambiguous")); err != nil {
			t.Fatal(err)
		}
		info, err := integ.PublishReplica("obj", "primary")
		if err != nil {
			t.Fatal(err)
		}
		_ = info
		truncateReplica(t, integ, "obj", "primary", 10)

		receipt, err := integ.Verify(context.Background(), "obj", "primary")
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if receipt.Outcome != OutcomePartialReplica {
			t.Fatalf("outcome = %q, want %q", receipt.Outcome, OutcomePartialReplica)
		}
		if !integ.Quarantined("obj") {
			t.Fatal("object with a partial replica was not quarantined")
		}
		if _, _, err := integ.Get(context.Background(), ctxA, "obj"); !errors.Is(err, ErrQuarantined) || !errors.Is(err, ErrReplicaPartial) {
			t.Fatalf("get on partial-replica object = %v, want ErrQuarantined+ErrReplicaPartial", err)
		}
	})
}

// TestTodo_ARTIFACT_006_Integration proves repair end to end: one corrupt
// replica plus one verified replica repairs from the verified one, keeps
// identity unchanged, and leaves an immutable receipt behind. It then
// proves repair REFUSES when no verified copy exists rather than guessing.
func TestTodo_ARTIFACT_006_Integration(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	integ, err := NewIntegrityStore(store)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("must survive repair intact")
	if _, err := store.Put(context.Background(), ctxA, "obj", "text/plain", plaintext); err != nil {
		t.Fatal(err)
	}
	if _, err := integ.PublishReplica("obj", "primary"); err != nil {
		t.Fatal(err)
	}
	if _, err := integ.PublishReplica("obj", "backup"); err != nil {
		t.Fatal(err)
	}
	if !corruptReplicaByte(t, integ, "obj", "primary", 3, 0x11) {
		t.Fatal("corruption had no effect")
	}

	// Quarantined before repair.
	if _, _, err := integ.Get(context.Background(), ctxA, "obj"); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("get before repair = %v, want ErrQuarantined", err)
	}

	repairReceipt, err := integ.Repair(context.Background(), ctxA, "obj", "backup")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !repairReceipt.Repaired {
		t.Fatal("repair receipt does not report Repaired=true")
	}
	if repairReceipt.ObjectID != "obj" {
		t.Fatalf("repair receipt object id = %q, want obj (identity must be retained)", repairReceipt.ObjectID)
	}
	if integ.Quarantined("obj") {
		t.Fatal("object still quarantined after repair")
	}

	got, _, err := integ.Get(context.Background(), ctxA, "obj")
	if err != nil {
		t.Fatalf("get after repair: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("get after repair = %q, want %q", got, plaintext)
	}

	receipts := integ.Receipts("obj")
	foundRepairReceipt := false
	for _, r := range receipts {
		if r.Repaired {
			foundRepairReceipt = true
		}
	}
	if !foundRepairReceipt {
		t.Fatal("no immutable repair receipt found in the receipt log")
	}

	// Now corrupt every registered replica so no verified copy exists.
	// Repair must refuse rather than guess from whatever bytes are
	// available.
	if !corruptReplicaByte(t, integ, "obj", "primary", 7, 0x22) {
		t.Fatal("corruption of primary had no effect")
	}
	if !corruptReplicaByte(t, integ, "obj", "backup", 7, 0x22) {
		t.Fatal("corruption of backup had no effect")
	}
	if _, err := integ.Repair(context.Background(), ctxA, "obj", "backup"); !errors.Is(err, ErrNoVerifiedReplica) {
		t.Fatalf("repair from a corrupt replica = %v, want ErrNoVerifiedReplica", err)
	}
	if _, err := integ.RepairAny(context.Background(), ctxA, "obj"); !errors.Is(err, ErrNoVerifiedReplica) {
		t.Fatalf("RepairAny with no verified replica = %v, want ErrNoVerifiedReplica", err)
	}
	if !integ.Quarantined("obj") {
		t.Fatal("object should remain quarantined when repair refuses")
	}
	if _, _, err := integ.Get(context.Background(), ctxA, "obj"); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("get after refused repair = %v, want ErrQuarantined", err)
	}
}

// TestTodo_ARTIFACT_006_Race runs real goroutines. Concurrent repair and
// verification over shared state must not double-repair one object or lose
// a receipt. There is no -race detector on this host (windows/arm64), so
// this asserts a concrete, deterministic result: every concurrent repair
// call that succeeds records its own receipt (none lost), the object ends
// up healthy exactly once, and every subsequent Get is consistent.
func TestTodo_ARTIFACT_006_Race(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	integ, err := NewIntegrityStore(store)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("race repaired payload")
	if _, err := store.Put(context.Background(), ctxA, "obj", "text/plain", plaintext); err != nil {
		t.Fatal(err)
	}
	if _, err := integ.PublishReplica("obj", "primary"); err != nil {
		t.Fatal(err)
	}
	if _, err := integ.PublishReplica("obj", "backup"); err != nil {
		t.Fatal(err)
	}
	if !corruptReplicaByte(t, integ, "obj", "primary", 4, 0x33) {
		t.Fatal("corruption had no effect")
	}

	const n = 50
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%5 == 0 {
				_, err := integ.Verify(context.Background(), "obj", "backup")
				errs[i] = err
				return
			}
			_, err := integ.Repair(context.Background(), ctxA, "obj", "backup")
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent call %d: %v", i, err)
		}
	}
	if integ.Quarantined("obj") {
		t.Fatal("object still quarantined after concurrent repairs")
	}

	receipts := integ.Receipts("obj")
	if len(receipts) < n {
		t.Fatalf("receipt log has %d entries, want at least %d (a concurrent call lost its receipt)", len(receipts), n)
	}
	repairedCount := 0
	for _, r := range receipts {
		if r.Repaired {
			repairedCount++
		}
	}
	wantRepairs := 0
	for i := 0; i < n; i++ {
		if i%5 != 0 {
			wantRepairs++
		}
	}
	if repairedCount != wantRepairs {
		t.Fatalf("repaired receipt count = %d, want %d (double-repair or lost receipt)", repairedCount, wantRepairs)
	}

	// Every concurrent reader afterward must observe the exact same
	// correct plaintext; nothing was left half-repaired.
	var rg sync.WaitGroup
	got := make([][]byte, n)
	gotErrs := make([]error, n)
	for i := 0; i < n; i++ {
		rg.Add(1)
		go func(i int) {
			defer rg.Done()
			b, _, err := integ.Get(context.Background(), ctxA, "obj")
			got[i], gotErrs[i] = b, err
		}(i)
	}
	rg.Wait()
	for i := 0; i < n; i++ {
		if gotErrs[i] != nil || string(got[i]) != string(plaintext) {
			t.Fatalf("concurrent get %d = %q, %v; want %q", i, got[i], gotErrs[i], plaintext)
		}
	}
}

// FuzzTodo_ARTIFACT_006's oracle is that arbitrary corruption of stored
// replica bytes must never verify as healthy — never a false clean.
func FuzzTodo_ARTIFACT_006(f *testing.F) {
	f.Add([]byte("payroll audit trail bytes"), 0, byte(1))
	f.Add([]byte("x"), 3, byte(0xFF))
	f.Add([]byte("another representative payload"), -5, byte(0x80))
	f.Fuzz(func(t *testing.T, plaintext []byte, index int, flip byte) {
		if len(plaintext) == 0 {
			plaintext = []byte("non-empty")
		}
		m, ctxA, _, _ := sealedFixture(t)
		store, err := NewSealedObjectStore(m)
		if err != nil {
			t.Fatal(err)
		}
		integ, err := NewIntegrityStore(store)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Put(context.Background(), ctxA, "obj", "text/plain", plaintext); err != nil {
			t.Fatalf("put: %v", err)
		}
		if _, err := integ.PublishReplica("obj", "primary"); err != nil {
			t.Fatalf("publish: %v", err)
		}
		if !corruptReplicaByte(t, integ, "obj", "primary", index, flip) {
			return // no-op mutation; nothing to assert
		}
		receipt, err := integ.Verify(context.Background(), "obj", "primary")
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if receipt.Outcome == OutcomeVerified {
			t.Fatalf("corrupted replica verified as healthy: index=%d flip=%d", index, flip)
		}
	})
}
