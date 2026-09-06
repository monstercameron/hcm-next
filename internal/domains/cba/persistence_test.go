package cba

import (
	"context"
	"errors"
	"testing"
	"time"
)

func persistentAgreement(revision string, retired bool) AgreementRevision {
	return AgreementRevision{
		ID: "row-" + revision, AgreementID: "agreement-1", Revision: revision,
		Version: "2026." + revision, Title: "Engineers", Representative: "union-1", Source: "register",
		EffectiveFrom: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		KnownFrom:     time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Retired: retired,
	}
}

func TestTodo_PERSIST_CBA_001_MemoryStore(t *testing.T) {
	store := NewMemoryStore()
	if err := store.SaveAgreement(context.Background(), "tenant-a", persistentAgreement("1", false), ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAgreement(context.Background(), "tenant-a", persistentAgreement("2", true), "1"); err != nil {
		t.Fatal(err)
	}
	got, err := store.CurrentAgreement(context.Background(), "tenant-a", "agreement-1")
	if err != nil || got.Revision != "2" || !got.Retired {
		t.Fatalf("current=%+v err=%v", got, err)
	}
	history, err := store.ListAgreementRevisions(context.Background(), "tenant-a", "agreement-1")
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	duplicate := store.SaveAgreement(context.Background(), "tenant-a", persistentAgreement("2", true), "2")
	var typed *StoreError
	if !errors.As(duplicate, &typed) || typed.Code != StoreDuplicateCode {
		t.Fatalf("duplicate=%v, want typed duplicate", duplicate)
	}
}

func TestTodo_PERSIST_CBA_001_MemoryStore_TenantIsolation(t *testing.T) {
	store := NewMemoryStore()
	if err := store.SaveAgreement(context.Background(), "tenant-a", persistentAgreement("1", false), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadAgreement(context.Background(), "tenant-b", "agreement-1", "1"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("tenant-b load=%v, want not found", err)
	}
}
