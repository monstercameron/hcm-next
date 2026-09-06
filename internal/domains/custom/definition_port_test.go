package custom

import (
	"context"
	"testing"
)

// TestTodo_CUSTOM_004_DefinitionPort verifies the additive persistence seam
// can consume the existing tenant-aware custom definition store contract.
func TestTodo_CUSTOM_004_DefinitionPort(t *testing.T) {
	store := NewMemoryStore()
	if err := store.SaveObjectDefinition(context.Background(), "tenant-a", testDefinition(), 0); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDefinition(context.Background(), store, "tenant-a", "Vehicle", "tenant.fleet", 1)
	if err != nil || loaded.Digest() != testDefinition().Digest() {
		t.Fatalf("loaded definition=%+v err=%v", loaded, err)
	}
}
