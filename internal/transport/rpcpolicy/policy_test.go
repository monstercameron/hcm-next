package rpcpolicy

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
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

func TestRPCPolicyMethodValidationAndDecisions(t *testing.T) {
	valid := MethodPolicy{Procedure: "/svc/Read", Deadline: time.Second, WaitForReady: true, Idempotency: manifest.IdempotencyReadSafe, RetryableStatusCodes: []string{"RESOURCE_EXHAUSTED", "UNAVAILABLE"}, MaxAttempts: 2, Hedging: HedgingProhibited}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*MethodPolicy)
	}{
		{"missing procedure", func(p *MethodPolicy) { p.Procedure = "" }},
		{"missing deadline", func(p *MethodPolicy) { p.Deadline = 0 }},
		{"invalid idempotency", func(p *MethodPolicy) { p.Idempotency = "other" }},
		{"zero attempts", func(p *MethodPolicy) { p.MaxAttempts = 0 }},
		{"invalid hedging", func(p *MethodPolicy) { p.Hedging = "ALLOW" }},
		{"empty retry status", func(p *MethodPolicy) { p.RetryableStatusCodes = []string{""} }},
		{"duplicate retry status", func(p *MethodPolicy) { p.RetryableStatusCodes = []string{"UNAVAILABLE", "UNAVAILABLE"} }},
		{"read not ready", func(p *MethodPolicy) { p.WaitForReady = false }},
		{"effect ready", func(p *MethodPolicy) { p.Idempotency = manifest.IdempotencyKey; p.WaitForReady = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			tc.mutate(&p)
			if p.Idempotency == manifest.IdempotencyKey && tc.name == "effect ready" {
				// The mutation deliberately reaches the effect-bearing wait-for-ready branch.
			}
			if err := p.Validate(); err == nil {
				t.Fatalf("Validate accepted %s", tc.name)
			}
		})
	}
	effect := valid
	effect.Idempotency, effect.WaitForReady, effect.MaxAttempts, effect.RetryableStatusCodes = manifest.IdempotencyKey, false, 2, []string{"UNAVAILABLE"}
	if got := effect.Decide("OK", OutcomeUnknown, 1); got != DecisionReturn {
		t.Fatalf("OK decision=%s", got)
	}
	if got := effect.Decide("UNAVAILABLE", OutcomeCommitted, 1); got != DecisionResolveAmbiguous {
		t.Fatalf("committed effect decision=%s", got)
	}
	if got := effect.Decide("UNAVAILABLE", OutcomeNotCommitted, 1); got != DecisionRetry || effect.Decide("UNAVAILABLE", OutcomeNotCommitted, 2) != DecisionReturn {
		t.Fatalf("bounded effect retry behavior=%s", got)
	}
	if got := valid.Decide("RESOURCE_EXHAUSTED", OutcomeNotCommitted, 1); got != DecisionRetry || valid.Decide("OTHER", OutcomeNotCommitted, 1) != DecisionReturn || valid.Decide("UNAVAILABLE", OutcomeNotCommitted, 2) != DecisionReturn {
		t.Fatalf("read decision=%s", got)
	}
	if got := valid.Decide("UNAVAILABLE", OutcomeNotCommitted, 0); got != DecisionReturn {
		t.Fatalf("zero attempt decision=%s", got)
	}
}

func TestRPCPolicyManifestConstructionLookupAndErrors(t *testing.T) {
	if _, err := BuildFromEndpointManifest(nil); err == nil {
		t.Fatal("nil endpoint manifest accepted")
	}
	if _, err := BuildFromEndpointManifest(&manifest.EndpointManifest{}); err == nil {
		t.Fatal("empty endpoint manifest accepted")
	}
	bad := &manifest.EndpointManifest{Endpoints: []manifest.EndpointDefinition{{EndpointID: "bad", GRPCProcedure: "/svc/Bad", DeadlineBudgetMillis: 10, IdempotencyClass: "BROKEN"}}}
	if _, err := BuildFromEndpointManifest(bad); err == nil {
		t.Fatal("invalid endpoint classification accepted")
	}
	m, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup("/missing"); ok || m.Explain() == "" || Explain(m) != m.Explain() {
		t.Fatalf("lookup/explain mismatch missing=%v explain=%q", ok, m.Explain())
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"bad version", func(m *Manifest) { m.Version = 0 }},
		{"empty methods", func(m *Manifest) { m.Methods = nil }},
		{"duplicate method", func(m *Manifest) { m.Methods = append(m.Methods, m.Methods[0]) }},
		{"invalid method", func(m *Manifest) { m.Methods[0].Procedure = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyManifest := m
			copyManifest.Methods = append([]MethodPolicy(nil), m.Methods...)
			tc.mutate(&copyManifest)
			if err := copyManifest.Validate(); err == nil {
				t.Fatalf("Validate accepted %s", tc.name)
			}
		})
	}
}
