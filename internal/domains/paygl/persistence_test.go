package paygl

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_PERSIST_PAYGL_001(t *testing.T) {
	store := NewMemoryStore()
	rule := glRule(t)
	if err := store.PutAccountingRule(context.Background(), "tenant-a", rule); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAccountingRule(context.Background(), "tenant-a", rule.ID, rule.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != rule.CanonicalDigest {
		t.Fatalf("loaded digest = %q, want %q", got.CanonicalDigest, rule.CanonicalDigest)
	}
}

func TestTodo_PERSIST_PAYGL_001_Fault(t *testing.T) {
	store := NewMemoryStore()
	rule := glRule(t)
	if err := store.PutAccountingRule(context.Background(), "tenant-a", rule); err != nil {
		t.Fatal(err)
	}
	err := store.PutAccountingRule(context.Background(), "tenant-a", rule)
	var conflict ConflictError
	if !errors.As(err, &conflict) || conflict.Code != ConflictDuplicateRevision || !errors.Is(err, ErrPersistenceDuplicate) {
		t.Fatalf("duplicate error = %v, want typed duplicate revision", err)
	}
}

func TestTodo_PERSIST_PAYGL_001_Mutation(t *testing.T) {
	store := NewMemoryStore()
	rule := glRule(t)
	if err := store.PutAccountingRule(context.Background(), "tenant-a", rule); err != nil {
		t.Fatal(err)
	}
	next, err := rule.NewVersion("v2", rule.Effective)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutAccountingRule(context.Background(), "tenant-a", next); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetAccountingRule(context.Background(), "tenant-a", rule.ID, rule.Version); err != nil || got.Version != "v1" {
		t.Fatalf("original revision = %+v, err %v", got, err)
	}
}
