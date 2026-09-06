package benefits

import (
	"context"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// Environment is the wired, zero-effect, in-memory projection this
// conformance fixture is simulated against. Every field is a declared,
// pinned fact; nothing here reads a clock, a database or a network.
type Environment struct {
	FactDisputed            bool
	EnrollmentWindowExpired bool
	LifeEventDuplicate      bool
	ElectionOverlapDetected bool
	EligibilityWatermark    string

	// CarrierOutcome/CarrierWatermark and DeductionOutcome/DeductionWatermark
	// independently control the two OBSERVE nodes' answers, which is what
	// makes it checkable that carrier and payroll-deduction reconciliation
	// never collapse into one shared outcome.
	CarrierOutcome     workflow.Outcome
	CarrierWatermark   string
	DeductionOutcome   workflow.Outcome
	DeductionWatermark string
}

// GoldenEnvironment is the reference scenario: no disputed fact, an open
// enrollment window, no duplicate life event, no overlapping election and
// clean carrier/deduction reconciliation.
func GoldenEnvironment() *Environment {
	return &Environment{
		FactDisputed:            false,
		EnrollmentWindowExpired: false,
		LifeEventDuplicate:      false,
		ElectionOverlapDetected: false,
		EligibilityWatermark:    "benefits.eligibility.stream_head@11",
		CarrierOutcome:          workflow.OutcomePass,
		CarrierWatermark:        "benefits.carrier.stream_head@5",
		DeductionOutcome:        workflow.OutcomePass,
		DeductionWatermark:      "payroll.deduction.stream_head@5",
	}
}

// DisputedFactEnvironment reports a disputed eligibility fact, which the
// eligibility DECISION must block rather than silently proceed past.
func DisputedFactEnvironment() *Environment {
	e := GoldenEnvironment()
	e.FactDisputed = true
	return e
}

// ExpiredWindowEnvironment reports an enrollment window that has already
// expired.
func ExpiredWindowEnvironment() *Environment {
	e := GoldenEnvironment()
	e.EnrollmentWindowExpired = true
	return e
}

// DuplicateLifeEventEnvironment reports that this life event has already
// been processed once.
func DuplicateLifeEventEnvironment() *Environment {
	e := GoldenEnvironment()
	e.LifeEventDuplicate = true
	return e
}

// OverlappingElectionEnvironment reports an election that overlaps an
// existing one.
func OverlappingElectionEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ElectionOverlapDetected = true
	return e
}

// DegradedCarrierEnvironment answers the carrier-reconciliation observation
// with FAIL, which must route to the carrier's own degraded repair terminal
// without ever touching the deduction obligation, which this failure never
// examined.
func DegradedCarrierEnvironment() *Environment {
	e := GoldenEnvironment()
	e.CarrierOutcome = workflow.OutcomeFail
	e.CarrierWatermark = ""
	return e
}

// DegradedDeductionEnvironment answers the payroll-deduction reconciliation
// observation with PARTIAL after a clean carrier reconciliation, which must
// route to the deduction's own degraded repair terminal without ever
// touching the carrier obligation, which this failure never examined.
func DegradedDeductionEnvironment() *Environment {
	e := GoldenEnvironment()
	e.DeductionOutcome = workflow.OutcomePartial
	e.DeductionWatermark = ""
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
		{CapReadEligibilityFacts, "benefits", e.handleReadEligibilityFacts},
		// The two reconciliation observations run through the injected
		// ReadPort, not these capability handlers; the capabilities exist
		// only because their StepObserve nodes must bind one.
		{CapObserveCarrierReconciliation, "benefits.carrier", e.handleObserveUnused},
		{CapObserveDeductionReconciliation, "payroll.deduction", e.handleObserveUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("benefits: publish %s: %w", entry.id, err)
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
		return simulate.CapabilityRequest{}, fmt.Errorf("benefits: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadEligibilityFacts(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"fact_disputed":             simulate.NewBool(e.FactDisputed),
			"enrollment_window_expired": simulate.NewBool(e.EnrollmentWindowExpired),
			"life_event_duplicate":      simulate.NewBool(e.LifeEventDuplicate),
			"election_overlap_detected": simulate.NewBool(e.ElectionOverlapDetected),
			"eligibility_watermark":     simulate.NewString(e.EligibilityWatermark),
		},
		Detail: "read eligibility facts at watermark " + e.EligibilityWatermark,
	}, nil
}

// handleObserveUnused exists only to publish the two reconciliation
// capabilities, so each StepObserve node has a capability to bind. The
// interpreter never invokes it: OBSERVE dispatches through the injected
// simulate.ReadPort (see [Reads]).
func (e *Environment) handleObserveUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}
