package contact

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestStorePortMemoryStorePreservesCASAndDigestOnlyHistory(t *testing.T) {
	tenant := values.TenantId("tenant-memory")
	subject := values.EntityRef{Tenant: tenant, Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
	endpoint, err := NewContactEndpointRevision(subject, "00000000-0000-4000-8000-000000000002", EndpointEmail, "Person@example.com", "recovery", 1, "profile")
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.PutEndpointRevision(ctx, tenant, endpoint); err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.PutEndpointRevision(ctx, tenant, endpoint)); got != StoreDuplicateCode {
		t.Fatalf("duplicate endpoint code = %q", got)
	}
	challenge, _, err := IssueContactChallenge("00000000-0000-4000-8000-000000000003", subject, endpoint, "recovery", "123456", time.Unix(100, 0), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutChallenge(ctx, tenant, challenge); err != nil {
		t.Fatal(err)
	}
	updated, _, err := challenge.Respond("wrong", time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.PutChallenge(ctx, tenant, updated, "sha256:stale")); got != StoreStaleCASCode {
		t.Fatalf("stale challenge code = %q", got)
	}
	if err := store.PutChallenge(ctx, tenant, updated, challenge.CanonicalDigest); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetChallenge(ctx, tenant, challenge.ChallengeID)
	if err != nil || len(loaded.Events) != 2 {
		t.Fatalf("loaded challenge = %+v, err=%v", loaded, err)
	}
	if errors.Is(err, ErrStoreDatabase) {
		t.Fatal("memory store reported a database failure")
	}
}
