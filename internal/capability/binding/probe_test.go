package binding

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

func TestProbeResultTypesCoversEveryPublishedCapability(t *testing.T) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	results := ProbeResultTypes(context.Background(), registry)
	if len(results) != len(registry.List()) {
		t.Fatalf("probed %d capabilities, registry publishes %d", len(results), len(registry.List()))
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].Capability.String() >= results[i].Capability.String() {
			t.Errorf("probe results are not sorted at %d", i)
		}
	}
}

// TestProbeDetectsATypedHandler proves the probe's positive case: it is not
// simply reporting "untyped" for everything.
func TestProbeDetectsATypedHandler(t *testing.T) {
	type typedAnswer struct{ Worker string }

	registry := capability.NewRegistry()
	def := capability.Definition{
		ID: "fixture.typed", Version: 1, OwnerDomain: "people",
		RequestSchema:  capability.SchemaRef{SchemaID: "req", Version: 1, ProtobufFullName: "fixture.Req"},
		ResponseSchema: capability.SchemaRef{SchemaID: "res", Version: 1, ProtobufFullName: "fixture.Res"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "err", Version: 1, ProtobufFullName: "fixture.Err"},
		EffectClass:    capability.EffectReadOnly, RiskClass: "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "scope:people.read", LegalBasisRef: "legal.fixture",
		EntitlementRef: "entitlement.fixture", SLOClassRef: "slo.fixture", TestRef: "conformance:fixture",
	}
	if err := registry.Register(def, func(context.Context, any) (any, error) {
		return typedAnswer{Worker: "w-1"}, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	results := ProbeResultTypes(context.Background(), registry)
	if len(results) != 1 {
		t.Fatalf("probed %d capabilities, want 1", len(results))
	}
	r := results[0]
	if !r.Invoked {
		t.Fatalf("a read-only capability was refused: %s", r.RefusalCode)
	}
	if r.Untyped {
		t.Errorf("a struct result was reported untyped (type %q)", r.ResultType)
	}
	if r.ResultType != "binding.typedAnswer" {
		t.Errorf("ResultType = %q, want binding.typedAnswer", r.ResultType)
	}
	if got := UntypedResultCapabilities(results); len(got) != 0 {
		t.Errorf("UntypedResultCapabilities = %v, want empty", got)
	}
}

// TestProbeRecordsRefusalsAndHandlerErrors rather than hiding them: a
// capability that cannot be invoked is a different finding from one that
// answers untyped, and conflating them would hide both.
func TestProbeRecordsRefusalsAndHandlerErrors(t *testing.T) {
	base := capability.Definition{
		Version: 1, OwnerDomain: "people",
		RequestSchema:  capability.SchemaRef{SchemaID: "req", Version: 1, ProtobufFullName: "fixture.Req"},
		ResponseSchema: capability.SchemaRef{SchemaID: "res", Version: 1, ProtobufFullName: "fixture.Res"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "err", Version: 1, ProtobufFullName: "fixture.Err"},
		RiskClass:      "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef: "scope:people.read", LegalBasisRef: "legal.fixture",
		EntitlementRef: "entitlement.fixture", SLOClassRef: "slo.fixture", TestRef: "conformance:fixture",
	}

	registry := capability.NewRegistry()
	failing := base
	failing.ID = "fixture.failing"
	failing.EffectClass = capability.EffectReadOnly
	if err := registry.Register(failing, func(context.Context, any) (any, error) {
		return nil, errors.New("the handler needs a real payload")
	}); err != nil {
		t.Fatalf("Register failing: %v", err)
	}
	writing := base
	writing.ID = "fixture.writing"
	writing.EffectClass = capability.EffectInternalMutation
	if err := registry.Register(writing, func(context.Context, any) (any, error) {
		t.Fatal("the gateway invoked a write-effect handler; P1A forbids it")
		return nil, nil
	}); err != nil {
		t.Fatalf("Register writing: %v", err)
	}

	byID := map[string]ProbeResult{}
	for _, r := range ProbeResultTypes(context.Background(), registry) {
		byID[r.Capability.ID] = r
	}

	if got := byID["fixture.failing"]; !got.Invoked || got.HandlerError == "" {
		t.Errorf("a failing handler was not recorded as invoked-with-error: %+v", got)
	}
	if got := byID["fixture.writing"]; got.Invoked || got.RefusalCode != capability.CodeWriteEffectRefusedP1A {
		t.Errorf("a write-effect capability was not refused before the handler: %+v", got)
	}
}

func TestDiscardSinkIsHarmless(t *testing.T) {
	id, err := discardSink{}.RecordInvocation(context.Background(), capability.InvocationEvidence{})
	if err != nil {
		t.Fatalf("the probe's evidence sink failed: %v", err)
	}
	if id == "" {
		t.Error("the probe's evidence sink returned an empty id; the gateway would treat it as unrecorded")
	}
}

func TestUntypedResultCapabilitiesIsSortedAndFiltered(t *testing.T) {
	results := []ProbeResult{
		{Capability: capability.Key{ID: "z", Version: 1}, Untyped: true},
		{Capability: capability.Key{ID: "a", Version: 1}, Untyped: true},
		{Capability: capability.Key{ID: "m", Version: 1}, Untyped: false},
	}
	got := UntypedResultCapabilities(results)
	if len(got) != 2 || got[0] != "a/v1" || got[1] != "z/v1" {
		t.Errorf("UntypedResultCapabilities = %v, want [a/v1 z/v1]", got)
	}
}
