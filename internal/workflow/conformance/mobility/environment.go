package mobility

import (
	"context"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// Environment is the pinned, zero-effect projection used by this fixture.
// It contains no clock, database, network or vendor authority.
type Environment struct {
	MobilityActive          bool
	OverlapDetected         bool
	SourceJurisdiction      string
	DestinationJurisdiction string
	HomePayrollGroup        string
	HostPayrollGroup        string
	PayrollModel            string
	LegStartText            string
	LegEndText              string

	AuthorizationValid             bool
	AuthorizationExpiring          bool
	AuthorizationMilestoneComplete bool
	TaxImpactUnknown               bool
	PEImpactUnknown                bool
	PrivacyTransferAllowed         bool

	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

func GoldenEnvironment() *Environment {
	return &Environment{
		MobilityActive: true, OverlapDetected: false,
		SourceJurisdiction: "US-CA", DestinationJurisdiction: "DE-BE",
		HomePayrollGroup: "payroll.us.home", HostPayrollGroup: "payroll.de.host", PayrollModel: "SPLIT",
		LegStartText: "2026-12-01", LegEndText: "2027-05-31",
		AuthorizationValid: true, AuthorizationExpiring: false, AuthorizationMilestoneComplete: true,
		TaxImpactUnknown: false, PEImpactUnknown: false, PrivacyTransferAllowed: true,
		ObserveOutcome: workflow.OutcomePass, ObserveWatermark: "immigration.vendor.stream_head@18",
	}
}

func AuthorizationExpiredEnvironment() *Environment {
	e := GoldenEnvironment()
	e.AuthorizationValid = false
	return e
}
func AuthorizationMilestoneEnvironment() *Environment {
	e := GoldenEnvironment()
	e.AuthorizationMilestoneComplete = false
	return e
}
func ExpiringAuthorizationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.AuthorizationExpiring = true
	return e
}
func OverlappingLegEnvironment() *Environment {
	e := GoldenEnvironment()
	e.OverlapDetected = true
	return e
}
func UnknownTaxPEEnvironment() *Environment {
	e := GoldenEnvironment()
	e.TaxImpactUnknown = true
	return e
}
func ProhibitedPrivacyEnvironment() *Environment {
	e := GoldenEnvironment()
	e.PrivacyTransferAllowed = false
	return e
}
func DegradedObservationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomeFail
	e.ObserveWatermark = ""
	return e
}
func AmbiguousObservationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomeUnknown
	e.ObserveWatermark = ""
	return e
}

func (e *Environment) Registry() (*capability.Registry, error) {
	r := capability.NewRegistry()
	entries := []struct {
		id, domain string
		handler    capability.Handler
	}{
		{CapReadMobilityLegs, "mobility.legs", e.handleReadMobilityLegs},
		{CapReadWorkAuthorization, "mobility.authorization", e.handleReadWorkAuthorization},
		{CapResolveTaxPE, "mobility.tax", e.handleResolveTaxPE},
		{CapResolvePrivacy, "privacy.transfer", e.handleResolvePrivacy},
		{CapObserveImmigration, "mobility.immigration", e.handleObserveImmigrationUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("mobility: publish %s: %w", entry.id, err)
		}
	}
	return r, nil
}

func readOnlyDefinition(id, domain string) capability.Definition {
	schema := func(slot string) capability.SchemaRef {
		return capability.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
	}
	return capability.Definition{ID: id, Version: 1, OwnerDomain: domain, RequestSchema: schema("request"), ResponseSchema: schema("response"), ErrorSchema: schema("error"), EffectClass: capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{domain}}, RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:" + domain + ".read", LegalBasisRef: "legal.p1a.observation-only.v1", EntitlementRef: "entitlement.pilot.p1a.v1", SLOClassRef: "slo.interactive.p95-2s.v1", TestRef: "conformance:" + id + "/v1"}
}

func request(payload any) (simulate.CapabilityRequest, error) {
	req, ok := payload.(simulate.CapabilityRequest)
	if !ok {
		return simulate.CapabilityRequest{}, fmt.Errorf("mobility: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}
func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (e *Environment) handleReadMobilityLegs(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	start, err := parseDate(e.LegStartText)
	if err != nil {
		return nil, err
	}
	end, err := parseDate(e.LegEndText)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{
		"mobility_active": simulate.NewBool(e.MobilityActive), "overlap_detected": simulate.NewBool(e.OverlapDetected),
		"source_jurisdiction": simulate.NewString(e.SourceJurisdiction), "destination_jurisdiction": simulate.NewString(e.DestinationJurisdiction),
		"home_payroll_group": simulate.NewString(e.HomePayrollGroup), "host_payroll_group": simulate.NewString(e.HostPayrollGroup), "payroll_model": simulate.NewString(e.PayrollModel),
		"leg_start": simulate.NewLocalDate(start), "leg_end": simulate.NewLocalDate(end),
	}, Detail: "read dated mobility leg " + e.SourceJurisdiction + " to " + e.DestinationJurisdiction}, nil
}
func (e *Environment) handleReadWorkAuthorization(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"authorization_valid": simulate.NewBool(e.AuthorizationValid), "authorization_expiring": simulate.NewBool(e.AuthorizationExpiring), "authorization_milestone_complete": simulate.NewBool(e.AuthorizationMilestoneComplete)}, Detail: "read authorization milestones"}, nil
}
func (e *Environment) handleResolveTaxPE(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"tax_impact_unknown": simulate.NewBool(e.TaxImpactUnknown), "pe_impact_unknown": simulate.NewBool(e.PEImpactUnknown), "tax_rule_version": simulate.NewString("mobility.tax_pe/2026.1")}, Detail: "resolved dated tax and permanent-establishment impact"}, nil
}
func (e *Environment) handleResolvePrivacy(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"privacy_transfer_allowed": simulate.NewBool(e.PrivacyTransferAllowed), "privacy_mechanism": simulate.NewString("SCC+encryption")}, Detail: "resolved cross-border privacy transfer decision"}, nil
}
func (e *Environment) handleObserveImmigrationUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{}, Detail: "node " + req.NodeID + " is served by the injected read port"}, nil
}
