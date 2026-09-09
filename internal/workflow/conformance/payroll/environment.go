package payroll

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
	CutoffActive         bool
	DuplicateRunDetected bool
	AlreadySettled       bool
	PopulationWatermark  string

	// ObserveOutcome and ObserveWatermark control the answer the injected
	// read port gives for the provider-settlement observation.
	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

// GoldenEnvironment is the reference scenario: an active cutoff, no
// duplicate run, a run that has not already settled and a clean provider
// settlement observation.
func GoldenEnvironment() *Environment {
	return &Environment{
		CutoffActive:         true,
		DuplicateRunDetected: false,
		AlreadySettled:       false,
		PopulationWatermark:  "payroll.population.stream_head@42",
		ObserveOutcome:       workflow.OutcomePass,
		ObserveWatermark:     "payroll.provider.stream_head@8",
	}
}

// StaleCutoffEnvironment reports a cutoff that is no longer active, which
// the release DECISION must block rather than release against a stale
// population/cutoff snapshot.
func StaleCutoffEnvironment() *Environment {
	e := GoldenEnvironment()
	e.CutoffActive = false
	return e
}

// DuplicateRunEnvironment reports that this run id has already been
// processed once, which must block release rather than release twice.
func DuplicateRunEnvironment() *Environment {
	e := GoldenEnvironment()
	e.DuplicateRunDetected = true
	return e
}

// AlreadySettledEnvironment reports a run that has already settled. It is
// the environment both the reversal-intent path (paired with
// reversal_requested) and the invalid-request path (without it) are built
// from.
func AlreadySettledEnvironment() *Environment {
	e := GoldenEnvironment()
	e.AlreadySettled = true
	return e
}

// RejectedSettlementEnvironment answers the provider-settlement observation
// with FAIL, which must route to the rejected/degraded repair terminal
// rather than being folded into a consistent completion.
func RejectedSettlementEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomeFail
	e.ObserveWatermark = ""
	return e
}

// AmbiguousSettlementEnvironment answers the provider-settlement observation
// with PARTIAL, which must route to a distinct ambiguous/degraded repair
// terminal, never the same terminal a rejected settlement reaches and never
// success.
func AmbiguousSettlementEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomePartial
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
		{CapReadPopulationAndCutoff, "payroll", e.handleReadPopulationAndCutoff},
		// The settlement observation runs through the injected ReadPort, not
		// this capability handler; the capability exists only because a
		// StepObserve node must bind one.
		{CapObserveProviderSettlement, "payroll.provider", e.handleObserveProviderSettlementUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("payroll: publish %s: %w", entry.id, err)
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
		return simulate.CapabilityRequest{}, fmt.Errorf("payroll: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadPopulationAndCutoff(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"cutoff_active":          simulate.NewBool(e.CutoffActive),
			"duplicate_run_detected": simulate.NewBool(e.DuplicateRunDetected),
			"already_settled":        simulate.NewBool(e.AlreadySettled),
			"population_watermark":   simulate.NewString(e.PopulationWatermark),
		},
		Detail: "read population and cutoff at watermark " + e.PopulationWatermark,
	}, nil
}

// handleObserveProviderSettlementUnused exists only to publish
// CapObserveProviderSettlement, so the OBSERVE node has a capability to
// bind. The interpreter never invokes it: OBSERVE dispatches through the
// injected simulate.ReadPort (see [Reads]).
func (e *Environment) handleObserveProviderSettlementUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}
