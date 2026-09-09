package termination

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// Environment is the wired, zero-effect, in-memory projection this
// conformance fixture is simulated against. Every field is a declared,
// pinned fact; nothing here reads a clock, a database or a network.
type Environment struct {
	EmploymentActive bool
	CurrentManagerID string
	LegalHoldActive  bool
	Jurisdiction     string

	// ObserveOutcome and ObserveWatermark control the answer the injected
	// read port gives for the P0 access-revocation observation.
	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

// GoldenEnvironment is the reference scenario: an active employment, no
// legal hold, a resolved jurisdiction and a clean P0 access-revocation
// observation.
func GoldenEnvironment() *Environment {
	return &Environment{
		EmploymentActive: true,
		CurrentManagerID: "mgr-001",
		LegalHoldActive:  false,
		Jurisdiction:     "US-CA",
		ObserveOutcome:   workflow.OutcomePass,
		ObserveWatermark: "iam.access.stream_head@9",
	}
}

// LegalHoldEnvironment reports an active legal hold, which the
// approval-and-SoD DECISION must block rather than silently proceed past.
func LegalHoldEnvironment() *Environment {
	e := GoldenEnvironment()
	e.LegalHoldActive = true
	return e
}

// AlreadyEndedEnvironment reports an employment that has already ended. It is
// the environment both the reinstatement path (paired with a cancellation
// request) and the invalid-request path (without one) are built from.
func AlreadyEndedEnvironment() *Environment {
	e := GoldenEnvironment()
	e.EmploymentActive = false
	return e
}

// DegradedObservationEnvironment answers the P0 access-revocation
// observation with FAIL, which must route to the bounded repair terminal
// rather than being folded into a consistent completion.
func DegradedObservationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomeFail
	e.ObserveWatermark = ""
	return e
}

// Registry publishes the fixture capability versions this reference workflow
// binds. Every definition is READ_ONLY: the governed gateway refuses every
// write effect class in P1A, so nothing registered here could mutate even if
// a handler tried.
func (e *Environment) Registry() (*capability.Registry, error) {
	r := capability.NewRegistry()
	entries := []struct {
		id      string
		domain  string
		handler capability.Handler
	}{
		{CapReadEmploymentAuthority, "people", e.handleReadEmploymentAuthority},
		{CapResolveLegalHolds, "legal", e.handleResolveLegalHolds},
		// The access-revocation observation runs through the injected
		// ReadPort, not this capability handler; the capability exists only
		// because a StepObserve node must bind one.
		{CapObserveAccessRevocation, "iam", e.handleObserveAccessRevocationUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("termination: publish %s: %w", entry.id, err)
		}
	}
	return r, nil
}

func readOnlyDefinition(id, domain string) capability.Definition {
	schema := func(slot string) capability.SchemaRef {
		return capability.SchemaRef{
			SchemaID: id + "." + slot + "/v1", Version: 1,
			ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
		}
	}
	return capability.Definition{
		ID: id, Version: 1, OwnerDomain: domain,
		RequestSchema: schema("request"), ResponseSchema: schema("response"), ErrorSchema: schema("error"),
		EffectClass: capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:" + domain + ".read",
		LegalBasisRef: "legal.p1a.observation-only.v1", EntitlementRef: "entitlement.pilot.p1a.v1",
		SLOClassRef: "slo.interactive.p95-2s.v1", TestRef: "conformance:" + id + "/v1",
	}
}

func request(payload any) (simulate.CapabilityRequest, error) {
	req, ok := payload.(simulate.CapabilityRequest)
	if !ok {
		return simulate.CapabilityRequest{}, fmt.Errorf("termination: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadEmploymentAuthority(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"employment_active":      simulate.NewBool(e.EmploymentActive),
			"source_authority_scope": simulate.NewString("scope-authority:acme-lifecycle"),
			"current_manager_id":     simulate.NewBranded("WorkerID", e.CurrentManagerID),
		},
		Detail: "read employment and authority for manager " + e.CurrentManagerID,
	}, nil
}

func (e *Environment) handleResolveLegalHolds(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"legal_hold_active": simulate.NewBool(e.LegalHoldActive),
			"jurisdiction":      simulate.NewString(e.Jurisdiction),
		},
		Detail: "resolved jurisdiction " + e.Jurisdiction,
	}, nil
}

// handleObserveAccessRevocationUnused exists only to publish
// CapObserveAccessRevocation, so the OBSERVE node has a capability to bind.
// The interpreter never invokes it: OBSERVE dispatches through the injected
// simulate.ReadPort (see [Reads]).
func (e *Environment) handleObserveAccessRevocationUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}
