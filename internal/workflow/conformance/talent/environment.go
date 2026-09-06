package talent

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
//
// It is deliberately not a domain implementation: CONF-012 proves the
// workflow's own composition (distinct FACT/HUMAN_OPINION/MODEL_INFERENCE
// provenance, a footprint that reports but never itself blocks, a decision
// that blocks a stale population, an unauthorized rating author or a
// contested calibration, and a correction that becomes a new revision rather
// than a mutation), not a real Talent, Performance or HRIS integration.
type Environment struct {
	PopulationActive    bool
	RaterID             string
	PopulationWatermark string

	RatingValue string

	InferenceRecommendation string

	CalibrationAdjustedRating string
	// CalibrationStatus is "AGREED" or "CONTESTED".
	CalibrationStatus string

	// ObserveOutcome and ObserveWatermark control the answer the injected
	// read port gives for the post-decision calibration-record observation.
	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

// GoldenEnvironment is the reference scenario: an active population, a
// rating author who is the rater of record, an agreed calibration and a
// clean post-decision observation. It is the scenario CONF-012's primary
// golden walk runs.
func GoldenEnvironment() *Environment {
	return &Environment{
		PopulationActive:          true,
		RaterID:                   "principal:manager-4471",
		PopulationWatermark:       "talent.population.stream_head@42",
		RatingValue:               "EXCEEDS_EXPECTATIONS",
		InferenceRecommendation:   "MODEL_RECOMMENDS_EXCEEDS_EXPECTATIONS",
		CalibrationAdjustedRating: "MEETS_EXPECTATIONS",
		CalibrationStatus:         "AGREED",
		ObserveOutcome:            workflow.OutcomePass,
		ObserveWatermark:          "talent.calibration.stream_head@7",
	}
}

// StalePopulationEnvironment reports an inactive population, which the
// calibration-and-correction DECISION must route to an explicit unknown
// terminal rather than silently proceeding with a stale membership.
func StalePopulationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.PopulationActive = false
	return e
}

// CalibrationConflictEnvironment reports a contested calibration, which the
// DECISION must block rather than accept as though the committee had agreed.
func CalibrationConflictEnvironment() *Environment {
	e := GoldenEnvironment()
	e.CalibrationStatus = "CONTESTED"
	return e
}

// DegradedObservationEnvironment answers the post-decision calibration-record
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
		{CapReadPopulation, "talent.population", e.handleReadPopulation},
		{CapReadRatingClaim, "talent.rating", e.handleReadRatingClaim},
		{CapReadModelInference, "talent.model_inference", e.handleReadModelInference},
		{CapResolveCalibrationCommittee, "talent.calibration", e.handleResolveCalibrationCommittee},
		// The calibration-record observation runs through the injected
		// ReadPort, not this capability handler; the capability exists only
		// because a StepObserve node must bind one.
		{CapObserveCalibrationRecord, "talent.calibration", e.handleObserveCalibrationRecordUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("talent: publish %s: %w", entry.id, err)
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
		return simulate.CapabilityRequest{}, fmt.Errorf("talent: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadPopulation(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"population_active":    simulate.NewBool(e.PopulationActive),
			"rater_id":             simulate.NewBranded("WorkerID", e.RaterID),
			"population_watermark": simulate.NewString(e.PopulationWatermark),
		},
		Detail: "read population snapshot; rater of record " + e.RaterID,
	}, nil
}

func (e *Environment) handleReadRatingClaim(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"rating_value": simulate.NewString(e.RatingValue),
			// The rating claim is a HUMAN_OPINION: a person's judgment, not a
			// verified FACT and not a MODEL_INFERENCE. This is the tag
			// ObligationProvenanceRetention requires be retained, distinct
			// from the other two provenance kinds this workflow reads.
			"rating_provenance": simulate.NewString("HUMAN_OPINION"),
		},
		Detail: "read human rating claim",
	}, nil
}

func (e *Environment) handleReadModelInference(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"inference_recommendation": simulate.NewString(e.InferenceRecommendation),
			// MODEL_INFERENCE: a recommendation, never an authoritative
			// decision. See NodeBuildAssessmentProposal's comment and
			// TestTodo_CONF_012_Security for the structural proof that this
			// node's output never reaches the DECISION's own inputs.
			"inference_provenance": simulate.NewString("MODEL_INFERENCE"),
		},
		Detail: "read model-generated recommendation (advisory only)",
	}, nil
}

func (e *Environment) handleResolveCalibrationCommittee(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"calibration_adjusted_rating": simulate.NewString(e.CalibrationAdjustedRating),
			"calibration_status":          simulate.NewString(e.CalibrationStatus),
		},
		Detail: "resolved calibration committee outcome: " + e.CalibrationStatus,
	}, nil
}

// handleObserveCalibrationRecordUnused exists only to publish
// CapObserveCalibrationRecord, so the OBSERVE node has a capability to bind.
// The interpreter never invokes it: OBSERVE dispatches through the injected
// simulate.ReadPort (see [Reads]).
func (e *Environment) handleObserveCalibrationRecordUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}
