package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

func TestNewRegistryCapabilityResolver_ResolvesExactVersionsAndNothingFromNil(t *testing.T) {
	if _, ok := NewRegistryCapabilityResolver(nil).ResolveCapability(context.Background(), capability.Key{ID: "hcmnext.people.explain_worker_state", Version: 1}); ok {
		t.Fatal("nil registry must resolve nothing")
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap registry: %v", err)
	}
	resolver := NewRegistryCapabilityResolver(registry)
	key := capability.Key{ID: "hcmnext.people.explain_worker_state", Version: 1}
	record, ok := resolver.ResolveCapability(context.Background(), key)
	if !ok || record.Digest == "" {
		t.Fatalf("bootstrap capability must resolve to its exact key, got %+v %v", record, ok)
	}
	if _, ok := resolver.ResolveCapability(context.Background(), capability.Key{ID: key.ID, Version: 999}); ok {
		t.Fatal("an unknown version must not resolve")
	}
}
