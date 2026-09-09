package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/access"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMemoryStoreImplementsRepository(t *testing.T) {
	var _ access.Repository = new(access.MemoryStore)
	store := new(access.MemoryStore)
	graph, err := store.Snapshot(context.Background(), values.TenantId("tenant-a"))
	if err != nil {
		t.Fatalf("empty snapshot: %v", err)
	}
	if graph.Tenant != "tenant-a" {
		t.Fatalf("snapshot tenant = %q", graph.Tenant)
	}
	if err := store.Add(context.Background(), access.ExternalAccessObservation{}); !errors.Is(err, access.ErrInvalidGraph) {
		t.Fatalf("observation through graph add = %v, want ErrInvalidGraph", err)
	}
}

func TestMemoryStoreHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (new(access.MemoryStore)).Snapshot(ctx, "tenant-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled snapshot = %v, want context.Canceled", err)
	}
}

func TestMemoryStore_AddSnapshotAndDefensiveCopy(t *testing.T) {
	f := validFixture(t, "a")
	store := &access.MemoryStore{}
	ctx := context.Background()
	if err := store.Add(ctx, f.identity); err != nil {
		t.Fatalf("identity add: %v", err)
	}
	if err := store.Add(ctx, &f.account); err != nil {
		t.Fatalf("account pointer add: %v", err)
	}
	if err := store.Add(ctx, f.entitlement); err != nil {
		t.Fatalf("entitlement add: %v", err)
	}
	if err := store.Add(ctx, f.expected); err != nil {
		t.Fatalf("expected add: %v", err)
	}
	pointerStore := &access.MemoryStore{}
	for _, record := range []access.Record{&f.identity, &f.account, &f.entitlement, &f.expected} {
		if err := pointerStore.Add(ctx, record); err != nil {
			t.Fatalf("pointer %T add: %v", record, err)
		}
	}
	got, err := store.Snapshot(ctx, f.identity.Tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Identities) != 1 || len(got.Accounts) != 1 || len(got.Entitlements) != 1 || len(got.Expected) != 1 || got.Digest() == "" {
		t.Fatalf("snapshot = %#v", got)
	}
	digest := got.Digest()
	got.Identities[0].Subject = "mutated-copy"
	got.Accounts = nil
	again, err := store.Snapshot(ctx, f.identity.Tenant)
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest() != digest || again.Identities[0].Subject == "mutated-copy" || len(again.Accounts) != 1 {
		t.Fatalf("snapshot was not defensive: %#v", again)
	}

	other, err := store.Snapshot(ctx, values.TenantId("tenant-other"))
	if err != nil || other.Tenant != "tenant-other" || len(other.Identities) != 0 {
		t.Fatalf("new tenant snapshot = %#v, %v", other, err)
	}
}

func TestMemoryStore_AddRejectsInvalidRecordsWithoutStateChange(t *testing.T) {
	f := validFixture(t, "a")
	store := &access.MemoryStore{}
	if err := store.Add(context.Background(), f.identity); err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(context.Background(), f.identity.Tenant)
	if err != nil {
		t.Fatal(err)
	}
	var nilRecord access.Record
	var nilIdentity *access.WorkforceIdentity
	for _, tc := range []struct {
		name   string
		record access.Record
	}{
		{"nil interface", nilRecord},
		{"typed nil identity", nilIdentity},
		{"observation is not authoritative", f.observation},
		{"unowned account", func() access.Record { a := f.account; a.WorkforceIdentityID = "missing"; return a }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Add(context.Background(), tc.record); !errors.Is(err, access.ErrInvalidGraph) && !errors.Is(err, access.ErrObservationMutation) && !errors.Is(err, access.ErrUnownedReference) {
				t.Fatalf("error = %v", err)
			}
			after, snapshotErr := store.Snapshot(context.Background(), f.identity.Tenant)
			if snapshotErr != nil || after.Digest() != before.Digest() {
				t.Fatalf("failed add changed state: %#v, %v", after, snapshotErr)
			}
		})
	}
}

func TestMemoryStore_ObserveIsSeparateAppendOnlyEvidence(t *testing.T) {
	f := validFixture(t, "a")
	store := &access.MemoryStore{}
	ctx := context.Background()
	if err := store.Observe(ctx, f.observation); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if err := store.Observe(ctx, f.observation); !errors.Is(err, access.ErrDuplicateRecord) {
		t.Fatalf("duplicate observation = %v", err)
	}
	invalid := f.observation
	invalid.ProviderVersion = ""
	if err := store.Observe(ctx, invalid); !errors.Is(err, access.ErrIncompleteRevision) {
		t.Fatalf("invalid observation = %v", err)
	}
	graph, err := store.Snapshot(ctx, f.observation.Tenant)
	if err != nil || len(graph.Observations) != 0 || len(graph.Identities) != 0 {
		t.Fatalf("observation entered authority graph: %#v, %v", graph, err)
	}
}

func TestMemoryStore_AllOperationsRejectNilOrCancelledContext(t *testing.T) {
	f := validFixture(t, "a")
	store := &access.MemoryStore{}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		call func(context.Context) error
	}{
		{"add nil", func(ctx context.Context) error { return store.Add(ctx, f.identity) }},
		{"observe nil", func(ctx context.Context) error { return store.Observe(ctx, f.observation) }},
		{"snapshot nil", func(ctx context.Context) error { _, err := store.Snapshot(ctx, f.identity.Tenant); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(nil); err == nil || err.Error() != "access: nil context" {
				t.Fatalf("nil context error = %v", err)
			}
			if err := tc.call(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled context error = %v", err)
			}
		})
	}
}
