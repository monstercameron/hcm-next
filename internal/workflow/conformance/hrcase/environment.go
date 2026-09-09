package hrcase

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
// It is deliberately not a domain implementation: CONF-014 proves the
// workflow's own composition (a relationship-scoped assignment fence, a
// purpose-scoped participant read, a matter-wall/legal-hold gate, an
// investigator-conflict footprint and an observed record-custody state), not
// a real case-management, legal-hold or IAM integration.
type Environment struct {
	CaseActive                 bool
	InvestigatorAuthorityScope string
	SubjectRelationshipScope   string

	ParticipantAuthorized   bool
	ParticipantPurposeScope string

	RetaliationSignalDetected bool

	MatterWallActive bool
	LegalHoldActive  bool

	// ObserveOutcome and ObserveWatermark control the answer the injected
	// read port gives for the post-decision record-custody observation.
	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

// GoldenEnvironment is the reference scenario: an active case, an authorized
// purpose-scoped participant, no retaliation signal, no matter wall or legal
// hold, and a clean post-decision record-custody observation.
func GoldenEnvironment() *Environment {
	return &Environment{
		CaseActive:                 true,
		InvestigatorAuthorityScope: "scope-authority:hrcase-investigator-9",
		SubjectRelationshipScope:   "scope-relationship:hrcase-subject-9",
		ParticipantAuthorized:      true,
		ParticipantPurposeScope:    "scope-purpose:hrcase-case-access",
		RetaliationSignalDetected:  false,
		MatterWallActive:           false,
		LegalHoldActive:            false,
		ObserveOutcome:             workflow.OutcomePass,
		ObserveWatermark:           "hrcase.record.stream_head@12",
	}
}

// UnauthorizedParticipantEnvironment reports a participant who failed the
// purpose-scoped authorization read. It is CONF-014's "unauthorized
// participant" RED case: the case-disposition DECISION must block rather
// than resolve it.
func UnauthorizedParticipantEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ParticipantAuthorized = false
	return e
}

// MatterWallEnvironment reports an active matter wall, which the
// case-disposition DECISION must block rather than silently cross. It is
// CONF-014's "privilege/matter-wall breach" RED case.
func MatterWallEnvironment() *Environment {
	e := GoldenEnvironment()
	e.MatterWallActive = true
	return e
}

// LegalHoldEnvironment reports a legal hold contending with disposition,
// which the case-disposition DECISION must block rather than race past. It
// is CONF-014's "hold race" RED case.
func LegalHoldEnvironment() *Environment {
	e := GoldenEnvironment()
	e.LegalHoldActive = true
	return e
}

// RetaliationSignalEnvironment reports a detected retaliation signal, which
// the case-disposition DECISION must route to a mandatory Employee Relations
// escalation rather than fold into routine disposition. It is CONF-014's
// "retaliation exposure" GREEN concern.
func RetaliationSignalEnvironment() *Environment {
	e := GoldenEnvironment()
	e.RetaliationSignalDetected = true
	return e
}

// AlreadyDisposedEnvironment reports a case that is no longer active. It is
// the environment both the appeal/reopen path (paired with an appeal
// request) and the already-disposed-invalid path (without one) are built
// from.
func AlreadyDisposedEnvironment() *Environment {
	e := GoldenEnvironment()
	e.CaseActive = false
	return e
}

// DegradedObservationEnvironment answers the post-decision record-custody
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
		{CapReadCaseAssignment, "hrcase.assignment", e.handleReadCaseAssignment},
		{CapReadParticipantAuthz, "hrcase.participant", e.handleReadParticipantAuthz},
		{CapReadRetaliationSignal, "hrcase.retaliation", e.handleReadRetaliationSignal},
		{CapResolveMatterWallAndHold, "legal", e.handleResolveMatterWallAndHold},
		// The record-custody observation runs through the injected ReadPort,
		// not this capability handler; the capability exists only because a
		// StepObserve node must bind one (see workflow.StepObserve's
		// RequiresCapability contract).
		{CapObserveRecordCustody, "hrcase.records", e.handleObserveRecordCustodyUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("hrcase: publish %s: %w", entry.id, err)
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
		return simulate.CapabilityRequest{}, fmt.Errorf("hrcase: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadCaseAssignment(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"case_active":                  simulate.NewBool(e.CaseActive),
			"investigator_authority_scope": simulate.NewString(e.InvestigatorAuthorityScope),
			"subject_relationship_scope":   simulate.NewString(e.SubjectRelationshipScope),
		},
		Detail: "read case assignment; investigator scope " + e.InvestigatorAuthorityScope,
	}, nil
}

func (e *Environment) handleReadParticipantAuthz(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"participant_authorized":    simulate.NewBool(e.ParticipantAuthorized),
			"participant_purpose_scope": simulate.NewString(e.ParticipantPurposeScope),
		},
		Detail: "read participant authorization, purpose scope " + e.ParticipantPurposeScope,
	}, nil
}

func (e *Environment) handleReadRetaliationSignal(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"retaliation_signal_detected": simulate.NewBool(e.RetaliationSignalDetected),
		},
		Detail: "read retaliation signal",
	}, nil
}

func (e *Environment) handleResolveMatterWallAndHold(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"matter_wall_active": simulate.NewBool(e.MatterWallActive),
			"legal_hold_active":  simulate.NewBool(e.LegalHoldActive),
		},
		Detail: "resolved matter wall and legal hold",
	}, nil
}

// handleObserveRecordCustodyUnused exists only to publish
// CapObserveRecordCustody, so a StepObserve node has a capability to bind.
// The interpreter never invokes it: OBSERVE dispatches through the injected
// simulate.ReadPort (see [Reads]).
func (e *Environment) handleObserveRecordCustodyUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}
