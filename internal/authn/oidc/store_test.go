package oidc_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
)

func TestMemoryStateStore_PutTake(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := oidc.NewMemoryStateStore()

	pending := oidc.PendingAuthorization{
		Tenant: tenantAcme, IssuerURL: issuerAcme,
		ClientID: clientIDAcme, RedirectURI: redirectURI,
		State: "state-1", Nonce: "nonce-1", CodeChallengeDigest: "digest-1",
		CreatedAt: baseTime, ExpiresAt: baseTime.Add(10 * time.Minute),
	}

	if err := store.Put(ctx, pending); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, found, err := store.Take(ctx, "state-1")
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if !found {
		t.Fatalf("Take: found = false, want true")
	}
	if got != pending {
		t.Fatalf("Take returned %+v, want %+v", got, pending)
	}

	// Take is single-use: a second Take for the same state finds nothing,
	// which is exactly the replay guard [oidc.Flow.HandleCallback] relies
	// on.
	_, found, err = store.Take(ctx, "state-1")
	if err != nil {
		t.Fatalf("second Take: %v", err)
	}
	if found {
		t.Fatalf("second Take: found = true, want false (state must be single-use)")
	}
}

func TestMemoryStateStore_TakeUnknownState(t *testing.T) {
	t.Parallel()
	store := oidc.NewMemoryStateStore()
	_, found, err := store.Take(context.Background(), "never-issued")
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if found {
		t.Fatalf("Take unknown state: found = true, want false")
	}
}

func TestMemoryStateStore_PutRefusesDuplicateState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := oidc.NewMemoryStateStore()
	pending := oidc.PendingAuthorization{State: "dup-state", CreatedAt: baseTime, ExpiresAt: baseTime}
	if err := store.Put(ctx, pending); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	if err := store.Put(ctx, pending); err == nil {
		t.Fatalf("second Put with the same state: got nil error, want an error")
	}
}
