package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/access"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
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
