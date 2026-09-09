package managerchange

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

type definitionResolver struct{}

func (definitionResolver) Lookup(key capability.Key) (capability.Record, bool) {
	return capability.Record{
		Definition: capability.Definition{
			ID: key.ID, Version: key.Version, OwnerDomain: "people",
			RequestSchema:  capability.SchemaRef{SchemaID: key.ID + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
			ResponseSchema: capability.SchemaRef{SchemaID: key.ID + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
			EffectClass:    capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{"people"}},
			RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:people.read",
			LegalBasisRef: "legal.p1a.observation-only.v1", EntitlementRef: "entitlement.pilot.p1a.v1", SLOClassRef: "slo.interactive.p95-2s.v1", TestRef: "test.managerchange/v1",
		},
		Status: capability.StatusActive, Digest: "sha256:managerchange-fixture-capability",
	}, true
}

func TestReferenceDefinitionCompilesWithReadOnlyCapabilities(t *testing.T) {
	plan, err := workflow.Compile(ReferenceDefinition(), workflow.Options{Phase: workflow.PhaseP1A, Capabilities: definitionResolver{}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !plan.Effects.ZeroEffect || plan.TerminalProfile != workflow.TerminalProfileSimulateOnly {
		t.Fatalf("compiled plan effects/profile = %+v/%s, want zero-effect/SIMULATE_ONLY", plan.Effects, plan.TerminalProfile)
	}
}
