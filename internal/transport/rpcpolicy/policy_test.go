package rpcpolicy

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/transport/manifest"
)

func TestGRPCRetryPolicyCannotReplayNonIdempotentCapabilityEffect(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, method := range m.Methods {
		if method.Hedging != HedgingProhibited {
			t.Fatalf("%s hedging=%s", method.Procedure, method.Hedging)
		}
		if method.Idempotency == manifest.IdempotencyKey && method.Decide("UNAVAILABLE", OutcomeUnknown, 1) != DecisionResolveAmbiguous {
			t.Fatalf("%s replayed an unknown effect", method.Procedure)
		}
	}
}

func TestTodo_RPC_EDGE_001_Property(t *testing.T) {
	p, err := Classify(manifest.EndpointDefinition{EndpointID: "x", GRPCProcedure: "/svc/Read", DeadlineBudgetMillis: 2000, IdempotencyClass: manifest.IdempotencyReadSafe})
	if err != nil {
		t.Fatal(err)
	}
	if p.Deadline.Milliseconds() != 2000 || !p.WaitForReady || p.MaxAttempts != 3 || len(p.RetryableStatusCodes) != 2 {
		t.Fatalf("policy=%+v", p)
	}
	if p.Decide("UNAVAILABLE", OutcomeNotCommitted, 1) != DecisionRetry || p.Decide("UNAVAILABLE", OutcomeNotCommitted, 3) != DecisionReturn {
		t.Fatal("read retry budget is not bounded")
	}
}

func TestTodo_RPC_EDGE_001_Race(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, len(m.Methods))
	for _, method := range m.Methods {
		go func(method MethodPolicy) { done <- method.Validate() }(method)
	}
	for range m.Methods {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_RPC_EDGE_001_Integration(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, procedure := range []string{"/hcmnext.intents.v1.IntentService/CreateIntent", "/hcmnext.registry.v1.RegistryService/ListCapabilities"} {
		p, ok := m.Lookup(procedure)
		if !ok || p.Deadline <= 0 {
			t.Fatalf("lookup %s = %+v, %v", procedure, p, ok)
		}
	}
}

func TestTodo_RPC_EDGE_001_Fault(t *testing.T) {
	p, err := Classify(manifest.EndpointDefinition{EndpointID: "effect", GRPCProcedure: "/svc/Effect", DeadlineBudgetMillis: 1000, IdempotencyClass: manifest.IdempotencyKey})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []Outcome{OutcomeUnknown, OutcomeCommitted} {
		if got := p.Decide("DEADLINE_EXCEEDED", outcome, 1); got != DecisionResolveAmbiguous {
			t.Errorf("outcome %s decision=%s", outcome, got)
		}
	}
}

func TestTodo_RPC_EDGE_001_Security(t *testing.T) {
	p, err := Classify(manifest.EndpointDefinition{EndpointID: "effect", GRPCProcedure: "/svc/Effect", DeadlineBudgetMillis: 1000, IdempotencyClass: manifest.IdempotencyKey})
	if err != nil {
		t.Fatal(err)
	}
	if p.WaitForReady || contains(p.RetryableStatusCodes, "DEADLINE_EXCEEDED") {
		t.Fatalf("unsafe effect policy=%+v", p)
	}
	if got := p.Explain(); got == "" {
		t.Fatal("empty explanation")
	}
}

func TestTodo_RPC_EDGE_001_Conformance(t *testing.T) {
	bad := manifest.EndpointDefinition{EndpointID: "bad", GRPCProcedure: "/svc/Bad", DeadlineBudgetMillis: 0, IdempotencyClass: manifest.IdempotencyReadSafe}
	if _, err := Classify(bad); err == nil {
		t.Fatal("unbounded method accepted")
	}
	if _, err := Classify(manifest.EndpointDefinition{EndpointID: "bad", GRPCProcedure: "/svc/Bad", DeadlineBudgetMillis: 1, IdempotencyClass: manifest.IdempotencyClass("other")}); err == nil {
		t.Fatal("unknown idempotency accepted")
	}
}
