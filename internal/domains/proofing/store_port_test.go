package proofing

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryRepositoryUsesTypedRevisionErrors(t *testing.T) {
	repo := NewMemoryRepository()
	session := proofingSession(t, AssuranceIAL2, AssuranceIAL2)
	if err := repo.SaveSession(context.Background(), "tenant-db", session, 0); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSession(context.Background(), "tenant-db", session, 0); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate error = %v, want ErrStoreDuplicate", err)
	}
	next, err := session.RecordOutcome(OutcomeReviewRequired)
	if err != nil {
		t.Fatal(err)
	}
	staleErr := repo.SaveSession(context.Background(), "tenant-db", next, 99)
	if !errors.Is(staleErr, ErrStoreStaleCAS) {
		t.Fatalf("stale error = %v, want ErrStoreStaleCAS", staleErr)
	}
	var typed *StoreError
	if !errors.As(staleErr, &typed) || typed.Code != StoreStaleCASCode || typed.Expected != 99 || typed.Actual != 1 {
		t.Fatalf("typed stale error = %#v", staleErr)
	}
}
