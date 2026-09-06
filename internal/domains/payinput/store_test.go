package payinput

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStoreImplementsPayInputStore(t *testing.T) {
	var _ Store = NewMemoryStore()
	if _, err := NewMemoryStore().LoadDefinition(context.Background(), "tenant", "missing", 1); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing definition error = %v", err)
	}
}
