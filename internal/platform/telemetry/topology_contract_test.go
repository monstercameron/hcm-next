package telemetry

import (
	"errors"
	"strings"
	"testing"
)

func TestTodo_OBS_012_ConformancePromotionPath(t *testing.T) {
	r := CanonicalPromotionRegistry()
	if r.Version() != PromotionTopologyVersion {
		t.Fatalf("registry version = %d, want %d", r.Version(), PromotionTopologyVersion)
	}
	for _, name := range []SpanName{
		SpanHTTPServer, SpanGatewayDecision, SpanCapabilityInvoke,
		SpanIntentCreate, SpanWorkflowStart, SpanWorkflowNode,
		SpanApprovalComplete, SpanTimerFire, SpanTimerResume,
		SpanTransactionCommit, SpanLedgerAppend, SpanOutboxEnqueue,
		SpanSchedulerTick,
	} {
		if _, ok := r.Lookup(name); !ok {
			t.Fatalf("missing promotion span %q", name)
		}
	}
	if row, _ := r.Lookup(SpanLedgerAppend); row.Parent != SpanTransactionCommit || !row.DurableBoundary {
		t.Fatalf("ledger row = %+v", row)
	}
	if row, _ := r.Lookup(SpanTimerResume); row.Parent != SpanTimerFire {
		t.Fatalf("timer resume parent = %q", row.Parent)
	}
}

func TestTodo_OBS_012_GoldenRegistry(t *testing.T) {
	explanation := CanonicalPromotionRegistry().ExplainTopology()
	if !strings.Contains(explanation, "hcmnext.gateway.decision<-hcmnext.http.server") ||
		!strings.Contains(explanation, "hcmnext.ledger.append<-hcmnext.transaction.commit") {
		t.Fatalf("topology explanation omitted required edges: %s", explanation)
	}
}

func TestTodo_OBS_012_ConformanceRegistryRejectsUnregisteredEmission(t *testing.T) {
	r := CanonicalPromotionRegistry()
	if err := r.ValidateEmission("/hcmnext.promotion.v1.Promotion/Execute", map[string]string{"logical_operation_id": "op-1"}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(r.ValidateEmission("hcmnext.workflow.node.secret", nil), ErrTopologyEmission) {
		t.Fatal("dynamic span name accepted")
	}
	if !errors.Is(r.ValidateEmission(string(SpanWorkflowNode), map[string]string{"worker": "secret"}), ErrTopologyEmission) {
		t.Fatal("unregistered attribute accepted")
	}
}

func TestTodo_OBS_012_PropertyRegistryCopiesRows(t *testing.T) {
	rows := CanonicalPromotionTopology()
	rows[0].Attributes[0].Key = "mutated"
	row, _ := CanonicalPromotionRegistry().Lookup(SpanHTTPServer)
	if row.Attributes[0].Key == "mutated" {
		t.Fatal("canonical registry leaked mutable attributes")
	}
}

func TestTodo_OBS_012_Race(t *testing.T) {
	r := CanonicalPromotionRegistry()
	ch := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() {
			_, ok := r.Lookup(SpanWorkflowNode)
			if !ok {
				ch <- ErrTopologyEmission
				return
			}
			ch <- nil
		}()
	}
	for i := 0; i < 32; i++ {
		if err := <-ch; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_OBS_012_Mutation(t *testing.T) {
	rows := CanonicalPromotionTopology()
	rows[0].Name = "hcmnext.dynamic.worker"
	if _, ok := CanonicalPromotionRegistry().Lookup(SpanHTTPServer); !ok {
		t.Fatal("mutating a caller copy changed the registry")
	}
}
