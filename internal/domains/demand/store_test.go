package demand

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_PERSIST_PLANNING_001_DomainMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	signal, err := NewDemandSignal(validSignal(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSignal(context.Background(), "tenant-a", signal, ""); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadSignal(context.Background(), "tenant-a", signal.SignalID, signal.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != signal.CanonicalDigest {
		t.Fatalf("digest = %q, want %q", got.CanonicalDigest, signal.CanonicalDigest)
	}
	if err := store.SaveSignal(context.Background(), "tenant-a", signal, ""); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate = %v, want ErrStoreDuplicate", err)
	}
	if _, err := store.LoadSignal(context.Background(), "tenant-b", signal.SignalID, signal.Version); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant load = %v, want ErrStoreNotFound", err)
	}
}

func TestTodo_PERSIST_PLANNING_001_DomainMemoryStore_Fault(t *testing.T) {
	store := NewMemoryStore()
	signal, err := NewDemandSignal(validSignal(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSignal(context.Background(), "tenant-a", signal, ""); err != nil {
		t.Fatal(err)
	}
	next := signal
	next.Version = "v2"
	next, err = NewDemandSignal(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSignal(context.Background(), "tenant-a", next, "stale"); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("stale save = %v, want ErrStoreStaleCAS", err)
	}
}
