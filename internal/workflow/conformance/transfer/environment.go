package transfer

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
//
// It is deliberately not a domain implementation: CONF-003 proves the
// workflow's own composition (separate company authority scopes, a bound
// conflict footprint, an observed post-decision state), not a real People,
// Organization or IAM integration.
type Environment struct {
	SourceEmploymentActive bool
	SourceAuthorityScope   string
	SourcePositionID       string

	DestinationPositionAvailable bool
	DestinationAuthorityScope    string
	DestinationBudgetAvailable   bool

	DestinationJurisdiction string
	PayrollGroup            string
	IAMPolicyRef            string

	// ObserveOutcome and ObserveWatermark control the answer the injected
	// read port gives for the post-decision effective-date-conflict
	// observation.
	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

// GoldenEnvironment is the reference scenario: two distinct, funded, active
// company authority scopes, a resolved jurisdiction and a clean post-decision
// observation. It is the scenario CONF-003's primary golden walk runs.
func GoldenEnvironment() *Environment {
	return &Environment{
		SourceEmploymentActive:       true,
		SourceAuthorityScope:         "scope-authority:company-acme-labs",
		SourcePositionID:             "POS-SRC-201",
		DestinationPositionAvailable: true,
		DestinationAuthorityScope:    "scope-authority:company-acme-cloud",
		DestinationBudgetAvailable:   true,
		DestinationJurisdiction:      "US-CA",
		PayrollGroup:                 "payroll.group.us-ca-w2",
		IAMPolicyRef:                 "iam.policy.acme-cloud/2026.1",
		ObserveOutcome:               workflow.OutcomePass,
		ObserveWatermark:             "workforce.assignment.stream_head@17",
	}
}

// SameScopeEnvironment collapses the source and destination authority scopes
// to one value. It exists to prove the REFACTOR clause: a transfer whose
// source and destination do not actually carry separate company authority is
// blocked rather than silently accepted as a same-company move dressed up as
// a transfer.
func SameScopeEnvironment() *Environment {
	e := GoldenEnvironment()
	e.DestinationAuthorityScope = e.SourceAuthorityScope
	return e
}

// UnavailableDestinationEnvironment reports a destination with no available
// position, which the conflict-footprint transform must turn into a detected
// conflict rather than a false success.
func UnavailableDestinationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.DestinationPositionAvailable = false
	return e
}

// DegradedObservationEnvironment answers the post-decision observation with
// FAIL, which must route to the bounded repair terminal rather than being
// folded into a consistent completion.
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
		{CapReadSourceAuthority, "company.source", e.handleReadSourceAuthority},
		{CapReadDestinationAuthority, "company.destination", e.handleReadDestinationAuthority},
		{CapResolveJurisdiction, "legal", e.handleResolveJurisdiction},
		// The conflict observation runs through the injected ReadPort, not
		// this capability handler; the capability exists only because a
		// StepObserve node must bind one (see workflow.StepObserve's
		// RequiresCapability contract).
		{CapObserveConflict, "operations", e.handleObserveConflictUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("transfer: publish %s: %w", entry.id, err)
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
		return simulate.CapabilityRequest{}, fmt.Errorf("transfer: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadSourceAuthority(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"source_employment_active": simulate.NewBool(e.SourceEmploymentActive),
			"source_authority_scope":   simulate.NewString(e.SourceAuthorityScope),
			"source_position_id":       simulate.NewBranded("PositionID", e.SourcePositionID),
		},
		Detail: "read source employment and authority scope " + e.SourceAuthorityScope,
	}, nil
}

func (e *Environment) handleReadDestinationAuthority(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"destination_position_available": simulate.NewBool(e.DestinationPositionAvailable),
			"destination_authority_scope":    simulate.NewString(e.DestinationAuthorityScope),
			"destination_budget_available":   simulate.NewBool(e.DestinationBudgetAvailable),
		},
		Detail: "read destination authority scope " + e.DestinationAuthorityScope,
	}, nil
}

func (e *Environment) handleResolveJurisdiction(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"destination_jurisdiction": simulate.NewString(e.DestinationJurisdiction),
			"payroll_group":            simulate.NewString(e.PayrollGroup),
			"iam_policy_ref":           simulate.NewString(e.IAMPolicyRef),
		},
		Detail: "resolved destination jurisdiction " + e.DestinationJurisdiction,
	}, nil
}

// handleObserveConflictUnused exists only to publish CapObserveConflict, so a
// StepObserve node has a capability to bind. The interpreter never invokes
// it: OBSERVE dispatches through the injected simulate.ReadPort (see
// [ReadPort]).
func (e *Environment) handleObserveConflictUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}
