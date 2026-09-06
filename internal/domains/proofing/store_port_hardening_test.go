package proofing

import (
	"context"
	"errors"
	"testing"
)

func TestStoreError_FormattingAndUnwrap(t *testing.T) {
	var nilErr *StoreError
	if nilErr.Error() != "proofing: <nil store error>" || nilErr.Unwrap() != nil {
		t.Fatal("nil StoreError behavior changed")
	}
	for _, tc := range []struct {
		code StoreErrorCode
		want error
	}{
		{StoreInvalidCode, ErrStoreInvalid},
		{StoreNotFoundCode, ErrStoreNotFound},
		{StoreDuplicateCode, ErrStoreDuplicate},
		{StoreStaleCASCode, ErrStoreStaleCAS},
		{"OTHER", nil},
	} {
		err := &StoreError{Code: tc.code}
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Fatalf("%s did not unwrap to %v", tc.code, tc.want)
		}
		if tc.want == nil && err.Unwrap() != nil {
			t.Fatalf("unknown code unwrapped to %v", err.Unwrap())
		}
		if err.Error() == "" {
			t.Fatal("StoreError has empty message")
		}
	}
	if (&StoreError{Code: StoreInvalidCode, Detail: "bad tenant"}).Error() != "proofing: INVALID: bad tenant" {
		t.Fatal("StoreError detail formatting changed")
	}
}

func TestMemoryRepository_SessionTenantCASAndHistory(t *testing.T) {
	repo := NewMemoryRepository()
	session := proofingSession(t, AssuranceIAL2, AssuranceIAL2)
	ctx := context.Background()
	if err := repo.SaveSession(ctx, "tenant-a", session, 0); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSession(ctx, "tenant-a", session, 0); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	if _, err := repo.LoadSession(ctx, "tenant-b", session.SessionID, 1); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant load = %v", err)
	}
	if _, err := repo.CurrentSession(ctx, "tenant-a", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing current = %v", err)
	}
	next, err := session.RecordOutcome(OutcomeReviewRequired)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSession(ctx, "tenant-a", next, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSession(ctx, "tenant-a", next, 1); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate successor = %v", err)
	}
	if err := repo.SaveSession(ctx, "tenant-a", session, 1); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("old revision duplicate = %v", err)
	}
	if err := repo.SaveSession(ctx, "tenant-a", next, 99); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate check precedence = %v", err)
	}
	wrong := next
	wrong.Revision = 3
	wrong.SupersedesRevision = 2
	wrong.CanonicalDigest = wrong.computedDigest()
	if err := repo.SaveSession(ctx, "tenant-a", wrong, 99); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("stale CAS = %v", err)
	}
	initialWithParent := session
	initialWithParent.SessionID = "another"
	initialWithParent.SupersedesRevision = 1
	initialWithParent.CanonicalDigest = initialWithParent.computedDigest()
	if err := repo.SaveSession(ctx, "tenant-a", initialWithParent, 0); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("invalid initial lineage = %v", err)
	}
	list, err := repo.ListSessions(ctx, "tenant-a", session.SessionID)
	if err != nil || len(list) != 2 || list[0].Revision != 1 || list[1].Revision != 2 {
		t.Fatalf("session history = %#v, %v", list, err)
	}
	list[0].Purpose = "tampered"
	loaded, err := repo.LoadSession(ctx, "tenant-a", session.SessionID, 1)
	if err != nil || loaded.Purpose == "tampered" {
		t.Fatalf("stored session mutated through list: %+v, %v", loaded, err)
	}
	if _, err := repo.ListSessions(ctx, "tenant-a", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing history = %v", err)
	}
}

func TestMemoryRepository_AuthorizationTenantCASAndHistory(t *testing.T) {
	repo := NewMemoryRepository()
	auth := proofingAuthorization(t)
	ctx := context.Background()
	if err := repo.SaveAuthorization(ctx, "tenant-a", auth, 0); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAuthorization(ctx, "tenant-a", auth, 0); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate auth = %v", err)
	}
	next := auth
	next.Revision = 2
	next.SupersedesRevision = 1
	next.Category = "VOLUNTEER"
	var err error
	next, err = NewWorkAuthorizationEvidence(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAuthorization(ctx, "tenant-a", next, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAuthorization(ctx, "tenant-a", next, 1); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate auth successor = %v", err)
	}
	if _, err := repo.LoadAuthorization(ctx, "tenant-b", auth.EvidenceID, 1); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant auth load = %v", err)
	}
	if _, err := repo.CurrentAuthorization(ctx, "tenant-a", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing auth current = %v", err)
	}
	list, err := repo.ListAuthorizations(ctx, "tenant-a", auth.EvidenceID)
	if err != nil || len(list) != 2 || list[1].Revision != 2 {
		t.Fatalf("auth history = %#v, %v", list, err)
	}
	if _, err := repo.ListAuthorizations(ctx, "tenant-a", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing auth history = %v", err)
	}
	if err := repo.SaveAuthorization(ctx, "", auth, 0); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("empty auth tenant = %v", err)
	}
	if _, err := repo.LoadAuthorization(ctx, "tenant-a", auth.EvidenceID, 0); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("zero auth revision = %v", err)
	}
	var nilRepo *MemoryRepository
	if err := nilRepo.SaveAuthorization(ctx, "tenant-a", auth, 0); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("nil repo = %v", err)
	}
}
