package attestation

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStore_CASAndHistoryBoundaries(t *testing.T) {
	store := NewMemoryStore()
	stmt := statementFixture()
	if err := store.PutStatement(context.Background(), "tenant-a", stmt, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.PutStatement(context.Background(), "tenant-a", stmt, 0); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale default CAS = %v", err)
	}
	if err := store.PutStatement(context.Background(), "tenant-a", stmt, 1); !errors.Is(err, ErrStatementDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	gap := stmt
	gap.Version = 3
	if err := store.PutStatement(context.Background(), "tenant-a", gap, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("version gap = %v", err)
	}
	if _, err := store.GetStatement(context.Background(), "tenant-a", "missing", 1); !errors.Is(err, ErrStatementNotFound) {
		t.Fatalf("missing statement = %v", err)
	}
	if _, err := store.ListStatementVersions(context.Background(), "tenant-a", "missing"); !errors.Is(err, ErrStatementNotFound) {
		t.Fatalf("missing statement history = %v", err)
	}
	if _, err := store.ListBindings(context.Background(), "tenant-a", "missing"); !errors.Is(err, ErrStatementNotFound) {
		t.Fatalf("missing binding history = %v", err)
	}
	if err := store.PutStatement(context.Background(), "tenant-b", stmt, 0); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListStatementVersions(context.Background(), "tenant-a", stmt.ID)
	if err != nil || len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("statement history = %#v, %v", versions, err)
	}
}

func TestMemoryStore_BindingRevisionAndInputValidation(t *testing.T) {
	store := NewMemoryStore()
	binding, _, _, _ := bindingFixture()
	if err := store.AppendBinding(context.Background(), "tenant-a", binding); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendBinding(context.Background(), "tenant-a", binding); !errors.Is(err, ErrBindingDuplicate) {
		t.Fatalf("duplicate binding = %v", err)
	}
	gap := binding
	gap.BindingVersion = 3
	if err := store.AppendBinding(context.Background(), "tenant-a", gap); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("binding gap = %v", err)
	}
	bad := binding
	bad.StatementID = ""
	if err := store.AppendBinding(context.Background(), "tenant-a", bad); !errors.Is(err, ErrBindingStatementEmpty) {
		t.Fatalf("invalid binding = %v", err)
	}
	if err := store.PutStatement(context.Background(), "", statementFixture()); err == nil {
		t.Fatal("empty tenant accepted")
	}
	if err := store.PutStatement(context.Background(), "tenant-a", statementFixture(), 99); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("wrong expected version = %v", err)
	}
	if err := (*MemoryStore)(nil).PutStatement(context.Background(), "tenant-a", statementFixture()); err == nil {
		t.Fatal("nil store accepted")
	}
}
