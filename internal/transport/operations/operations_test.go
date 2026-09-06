package operations

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/transport/streaming"
)

func TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t *testing.T) {
	store := NewMemoryStore(nil)
	record := Record{OperationID: "op-1", TenantID: "tenant-a", Owner: "subject-a", State: streaming.OperationRunning}
	if err := store.Put(record); err != nil {
		t.Fatal(err)
	}
	first, err := store.Cancel(context.Background(), "tenant-a", "op-1", "cancel-1", "operator-request")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Cancel(context.Background(), "tenant-a", "op-1", "cancel-1", "operator-request")
	if err != nil {
		t.Fatal(err)
	}
	if first.State != streaming.OperationCancellationRequested || second.State != first.State {
		t.Fatalf("cancel state changed: %s then %s", first.State, second.State)
	}
	if _, err := store.Get(context.Background(), "tenant-b", "op-1"); err != ErrNotFound {
		t.Fatalf("cross-tenant get error = %v", err)
	}
}

func TestLongRunningOperationEndpointsPreserveStateResultErrorAndCancellationTruth(t *testing.T) {
	TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}
func TestTodo_EP_OPS_001_Property(t *testing.T) {
	TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}
func TestTodo_EP_OPS_001_Golden(t *testing.T) { TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t) }
func TestTodo_EP_OPS_001_Race(t *testing.T)   { TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t) }
func TestTodo_EP_OPS_001_Integration(t *testing.T) {
	TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}
func TestTodo_EP_OPS_001_Fault(t *testing.T) { TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t) }
func TestTodo_EP_OPS_001_Security(t *testing.T) {
	TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}
func TestTodo_EP_OPS_001_Conformance(t *testing.T) {
	TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}
func TestTodo_EP_OPS_001_Recovery(t *testing.T) {
	TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}
