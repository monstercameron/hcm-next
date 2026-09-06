package taxprofile

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_PERSIST_TAXPROFILE_001_DomainMemoryPort(t *testing.T) {
	store := NewMemoryStore()
	profile := validTaxProfile(t)
	if err := store.SaveProfile(context.Background(), "tenant-a", profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	got, err := store.LoadProfile(context.Background(), "tenant-a", profile.WorkerRef, profile.Revision)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if got.CanonicalDigest != profile.CanonicalDigest || len(got.Registrations) != 1 || len(got.Elections) != 1 || len(got.Exemptions) != 1 {
		t.Fatalf("loaded profile = %+v, want detached equivalent", got)
	}
	if _, err := store.LoadProfile(context.Background(), "tenant-b", profile.WorkerRef, profile.Revision); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant load = %v, want ErrStoreNotFound", err)
	}
}

func TestTodo_PERSIST_TAXPROFILE_001_Fault(t *testing.T) {
	store := NewMemoryStore()
	profile := validTaxProfile(t)
	if err := store.SaveProfile(context.Background(), "tenant-a", profile); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProfile(context.Background(), "tenant-a", profile); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate profile = %v, want ErrStoreDuplicate", err)
	}
	stale := profile
	stale.Revision = 3
	stale.ParentRevision = 2
	stale.ParentDigest = profile.CanonicalDigest
	stale.CanonicalDigest = ""
	if err := store.SaveProfile(context.Background(), "tenant-a", stale); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("stale profile = %v, want ErrStoreStaleCAS", err)
	}
}
