package location

import (
	"context"
	"errors"
	"testing"
)

func TestVerifyWorksiteRevision_AgreesRefusesDriftAndMissing(t *testing.T) {
	w := validWorksite(t)
	store := NewInMemoryWorksiteStore()
	if err := store.Put(context.Background(), w); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := VerifyWorksiteRevision(context.Background(), store, w); err != nil {
		t.Fatalf("stored revision must verify: %v", err)
	}
	draft := w
	draft.Name = "Somewhere Else"
	draft.CanonicalDigest = ""
	changed, err := NewWorksiteRevision(draft)
	if err != nil {
		t.Fatalf("changed revision must still be valid: %v", err)
	}
	if err := VerifyWorksiteRevision(context.Background(), store, changed); !errors.Is(err, ErrWorksiteRevisionDrift) {
		t.Fatalf("changed name must report drift, got %v", err)
	}
	if err := VerifyWorksiteRevision(context.Background(), NewInMemoryWorksiteStore(), w); !errors.Is(err, ErrWorksiteNotFound) {
		t.Fatalf("unknown revision must report not found, got %v", err)
	}
	if err := VerifyWorksiteRevision(context.Background(), nil, w); err == nil {
		t.Fatal("nil reader must be refused")
	}
	if err := VerifyWorksiteRevision(context.Background(), store, WorksiteRevision{}); err == nil {
		t.Fatal("invalid expected revision must be refused before any read")
	}
}
