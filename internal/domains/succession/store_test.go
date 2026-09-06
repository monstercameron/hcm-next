package succession

import (
	"context"
	"errors"
	"testing"
)

type successionTenant string

func (t successionTenant) String() string { return string(t) }

func TestSuccessionMemoryStoreImplementsRevisionPort(t *testing.T) {
	store := NewMemorySlateStore()
	tenant := successionTenant("tenant-a")
	role := CriticalRole{
		RoleID: "role-1", Revision: 1, PositionRef: "position-1", JobRevisionRef: "job-1",
		OwnerRef: "owner-1", AuthorityRef: "authority-1", EffectiveAt: successionInstant(), KnownAt: successionInstant(), EvidenceRefs: []string{"evidence-1"},
	}
	if err := store.SaveCriticalRole(context.Background(), tenant, role); err != nil {
		t.Fatal(err)
	}
	got, err := store.CurrentCriticalRole(context.Background(), tenant, role.RoleID)
	if err != nil || got.CanonicalDigest == "" {
		t.Fatalf("CurrentCriticalRole() = %+v, %v", got, err)
	}
	if err := store.SaveCriticalRole(context.Background(), tenant, role); err == nil {
		t.Fatal("duplicate critical-role revision was accepted")
	} else if !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate error = %v, want ErrStoreDuplicate", err)
	}
}
