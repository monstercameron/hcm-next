package learning

import (
	"context"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// Environment is the wired, zero-effect, in-memory projection this
// conformance fixture is simulated against. Every field is a declared,
// pinned fact; nothing here reads a clock, a database or a network.
//
// It is deliberately not a domain implementation: CONF-013 proves the
// workflow's own composition (duplicate-enrollment precedence, waiver
// authority, verified-credential-only satisfaction, evidence-expiry
// footprinting, provider reconciliation), not a real LMS, credentialing or
// compliance integration.
type Environment struct {
	RequirementActive         bool
	RequirementAuthorityScope string

	DuplicateEnrollmentDetected bool
	EnrollmentProvider          string

	EvidencePresent    bool
	EvidenceExpiryText string
	EvidenceKind       string

	WaiverPresent bool
	WaiverValid   bool

	// ObserveOutcome and ObserveWatermark control the answer the injected
	// read port gives for the post-decision provider-reconciliation
	// observation.
	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

// GoldenEnvironment is the reference scenario: an active requirement, no
// duplicate enrollment, a far-future verified credential, no waiver and a
// clean post-decision provider-reconciliation observation. It is the
// scenario CONF-013's primary golden walk runs.
func GoldenEnvironment() *Environment {
	return &Environment{
		RequirementActive:           true,
		RequirementAuthorityScope:   "scope-authority:learning-requirement",
		DuplicateEnrollmentDetected: false,
		EnrollmentProvider:          "provider.lms.acme",
		EvidencePresent:             true,
		EvidenceExpiryText:          "2027-06-01",
		EvidenceKind:                EvidenceKindVerifiedCredential,
		WaiverPresent:               false,
		WaiverValid:                 false,
		ObserveOutcome:              workflow.OutcomePass,
		ObserveWatermark:            "lms.completion.projection.stream_head@42",
	}
}

// DuplicateEnrollmentEnvironment reports a duplicate enrollment against an
// otherwise golden, valid-evidence scenario. It proves duplicate enrollment
// blocks ahead of everything else, even evidence that would otherwise
// satisfy the requirement.
func DuplicateEnrollmentEnvironment() *Environment {
	e := GoldenEnvironment()
	e.DuplicateEnrollmentDetected = true
	return e
}

// NoEvidenceEnvironment reports no accessible evidence. It is CONF-013's
// "inaccessible content" RED case: whether evidence was never submitted or
// exists but cannot be read, the fixture reports the same evidence_present
// = false, which must resolve to UNSATISFIED_NO_EVIDENCE, never a false
// pass.
func NoEvidenceEnvironment() *Environment {
	e := GoldenEnvironment()
	e.EvidencePresent = false
	e.EvidenceExpiryText = "2026-12-01"
	return e
}

// ExpiredEnvironment reports verified-credential evidence whose expiry date
// is already before the pinned effective date.
func ExpiredEnvironment() *Environment {
	e := GoldenEnvironment()
	e.EvidenceExpiryText = "2026-11-26"
	return e
}

// ExpiringEnvironment reports verified-credential evidence whose expiry
// date falls inside the fixed expiring-soon window, but has not yet
// expired.
func ExpiringEnvironment() *Environment {
	e := GoldenEnvironment()
	e.EvidenceExpiryText = "2026-12-11"
	return e
}

// SelfClaimEnvironment reports otherwise-valid evidence, but of kind
// SELF_CLAIM rather than VERIFIED_CREDENTIAL. It proves the REFACTOR clause:
// a skill claim never alone satisfies the requirement.
func SelfClaimEnvironment() *Environment {
	e := GoldenEnvironment()
	e.EvidenceKind = EvidenceKindSelfClaim
	return e
}

// ValidWaiverEnvironment reports a present, valid waiver. It resolves the
// requirement as exempt regardless of the evidence state.
func ValidWaiverEnvironment() *Environment {
	e := GoldenEnvironment()
	e.WaiverPresent = true
	e.WaiverValid = true
	return e
}

// InvalidWaiverEnvironment reports a present but invalid waiver against
// otherwise-golden, valid evidence. It proves an invalid waiver is never
// silently treated as a valid exemption.
func InvalidWaiverEnvironment() *Environment {
	e := GoldenEnvironment()
	e.WaiverPresent = true
	e.WaiverValid = false
	return e
}

// DegradedObservationEnvironment answers the post-decision
// provider-reconciliation observation with FAIL, which must route to the
// bounded repair terminal rather than being folded into a consistent
// completion.
func DegradedObservationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomeFail
	e.ObserveWatermark = ""
	return e
}

// AmbiguousObservationEnvironment answers the post-decision
// provider-reconciliation observation with UNKNOWN, CONF-013's "ambiguous
// provider completion" RED case: an observation the interpreter cannot
// reconcile must route to the unknown terminal, never be folded into
// SATISFIED.
func AmbiguousObservationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomeUnknown
	e.ObserveWatermark = ""
	return e
}

// EvidenceExpiry parses the fixture's declared evidence expiry date.
func (e *Environment) EvidenceExpiry() (values.LocalDate, error) {
	return values.ParseLocalDate(e.EvidenceExpiryText)
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
		{CapReadRequirement, "learning.requirement", e.handleReadRequirement},
		{CapReadEnrollmentHistory, "learning.enrollment", e.handleReadEnrollmentHistory},
		{CapReadCredentialEvidence, "learning.evidence", e.handleReadCredentialEvidence},
		{CapReadWaiver, "learning.waiver", e.handleReadWaiver},
		// The provider-reconciliation observation runs through the injected
		// ReadPort, not this capability handler; the capability exists only
		// because a StepObserve node must bind one (see
		// workflow.StepObserve's RequiresCapability contract).
		{CapObserveProviderReconciliation, "learning.provider", e.handleObserveProviderReconciliationUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("learning: publish %s: %w", entry.id, err)
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
		return simulate.CapabilityRequest{}, fmt.Errorf("learning: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadRequirement(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"requirement_active":          simulate.NewBool(e.RequirementActive),
			"requirement_authority_scope": simulate.NewString(e.RequirementAuthorityScope),
		},
		Detail: "read requirement authority scope " + e.RequirementAuthorityScope,
	}, nil
}

func (e *Environment) handleReadEnrollmentHistory(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"duplicate_enrollment_detected": simulate.NewBool(e.DuplicateEnrollmentDetected),
			"enrollment_provider":           simulate.NewString(e.EnrollmentProvider),
		},
		Detail: "read enrollment history from " + e.EnrollmentProvider,
	}, nil
}

func (e *Environment) handleReadCredentialEvidence(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	expiry, err := e.EvidenceExpiry()
	if err != nil {
		return nil, fmt.Errorf("learning: evidence expiry: %w", err)
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"evidence_present":     simulate.NewBool(e.EvidencePresent),
			"evidence_expiry_date": simulate.NewLocalDate(expiry),
			"evidence_kind":        simulate.NewString(e.EvidenceKind),
		},
		Detail: "read credential evidence, present=" + boolText(e.EvidencePresent) + " kind=" + e.EvidenceKind,
	}, nil
}

func (e *Environment) handleReadWaiver(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"waiver_present": simulate.NewBool(e.WaiverPresent),
			"waiver_valid":   simulate.NewBool(e.WaiverValid),
		},
		Detail: "read waiver, present=" + boolText(e.WaiverPresent) + " valid=" + boolText(e.WaiverValid),
	}, nil
}

// handleObserveProviderReconciliationUnused exists only to publish
// CapObserveProviderReconciliation, so a StepObserve node has a capability to
// bind. The interpreter never invokes it: OBSERVE dispatches through the
// injected simulate.ReadPort (see [Reads]).
func (e *Environment) handleObserveProviderReconciliationUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}
