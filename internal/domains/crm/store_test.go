package crm

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func crmRevisionNumber(t *testing.T, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision("crm", sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func TestMemoryStore_PoolRevisionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	pool := validPool(t)
	if err := store.PutPool(ctx, "tenant-1", pool); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPool(ctx, "tenant-1", pool.PoolID.Id, 1)
	if err != nil || got.Purpose != pool.Purpose {
		t.Fatalf("GetPool()=(%+v, %v), want saved revision", got, err)
	}
	pool.Revision = crmRevisionNumber(t, 2)
	pool.Purpose = "succession recruiting"
	if err := store.PutPool(ctx, "tenant-1", pool, 1); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListPoolVersions(ctx, "tenant-1", pool.PoolID.Id)
	if err != nil || len(versions) != 2 || versions[0].Purpose == versions[1].Purpose {
		t.Fatalf("ListPoolVersions()=(%+v, %v), want ordered immutable revisions", versions, err)
	}
	if err := store.PutPool(ctx, "tenant-1", pool, 2); !errors.Is(err, ErrPoolDuplicate) {
		t.Fatalf("duplicate error=%v, want ErrPoolDuplicate", err)
	}
	pool.Revision = crmRevisionNumber(t, 3)
	if err := store.PutPool(ctx, "tenant-1", pool, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale CAS error=%v, want ErrVersionConflict", err)
	}
	if _, err := store.GetPool(ctx, "tenant-2", pool.PoolID.Id, 1); !errors.Is(err, ErrPoolNotFound) {
		t.Fatalf("cross-tenant read error=%v, want ErrPoolNotFound", err)
	}
}

func TestMemoryStore_MembershipRevisionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	membership := validMembership(t)
	if err := store.PutMembership(ctx, "tenant-1", membership); storeErrorCode(err) != StoreReferenceNotFoundCode {
		t.Fatalf("missing pool error=%v, want reference-not-found", err)
	}
	if err := store.PutPool(ctx, "tenant-1", validPool(t)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutMembership(ctx, "tenant-1", membership); err != nil {
		t.Fatal(err)
	}
	membership.Revision = crmRevisionNumber(t, 2)
	membership.Purpose = "future leadership recruiting"
	if err := store.PutMembership(ctx, "tenant-1", membership, 1); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetMembership(ctx, "tenant-1", membership.MembershipID.Id, 2)
	if err != nil || got.Purpose != membership.Purpose {
		t.Fatalf("GetMembership()=(%+v, %v), want second revision", got, err)
	}
	versions, err := store.ListMembershipVersions(ctx, "tenant-1", membership.MembershipID.Id)
	if err != nil || len(versions) != 2 {
		t.Fatalf("ListMembershipVersions()=(%+v, %v), want two revisions", versions, err)
	}
	if err := store.PutMembership(ctx, "tenant-1", membership, 2); storeErrorCode(err) != StoreDuplicateCode || !errors.Is(err, ErrMembershipDuplicate) {
		t.Fatalf("duplicate error=%v, want membership duplicate", err)
	}
	if _, err := store.GetMembership(ctx, "tenant-2", membership.MembershipID.Id, 1); !errors.Is(err, ErrMembershipNotFound) {
		t.Fatalf("cross-tenant read error=%v, want ErrMembershipNotFound", err)
	}
}

func TestMemoryStore_Refusals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewMemoryStore()
	if err := store.PutPool(ctx, "tenant-1", validPool(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error=%v", err)
	}
	if err := store.PutPool(context.Background(), "tenant-2", validPool(t)); storeErrorCode(err) != StoreInvalidCode {
		t.Fatalf("tenant mismatch error=%v", err)
	}
	if err := store.PutPool(context.Background(), "tenant-1", validPool(t), 0, 1); storeErrorCode(err) != StoreInvalidCode {
		t.Fatalf("multiple CAS values error=%v", err)
	}
	var nilStore *MemoryStore
	if _, err := nilStore.GetPool(context.Background(), "tenant-1", "pool", 1); storeErrorCode(err) != StoreInvalidCode {
		t.Fatalf("nil store error=%v", err)
	}
	if _, err := store.ListPoolVersions(context.Background(), "tenant-1", "missing"); !errors.Is(err, ErrPoolNotFound) {
		t.Fatalf("missing pool error=%v", err)
	}
	if _, err := store.ListMembershipVersions(context.Background(), "tenant-1", "missing"); !errors.Is(err, ErrMembershipNotFound) {
		t.Fatalf("missing membership error=%v", err)
	}
}

func storeErrorCode(err error) StoreErrorCode {
	var storeErr *StoreError
	if errors.As(err, &storeErr) {
		return storeErr.Code
	}
	return ""
}
