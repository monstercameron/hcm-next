package humanwork

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The promotion scenario's principals. They are exported so that a test or a
// downstream lane can name the exact person whose availability, delegation or
// conflict it wants to exercise, rather than reaching into the directory by
// string.
const (
	// PrincipalRequester is the HR partner who raises the promotion.
	PrincipalRequester = "principal:hr-partner-7"
	// PrincipalSubject is the worker being promoted, as a principal.
	PrincipalSubject = "principal:jane"
	// PrincipalManager is Jane's current manager.
	PrincipalManager = "principal:manager-1"
	// PrincipalHRBP is the HR business partner for the organization.
	PrincipalHRBP = "principal:hrbp-2"
	// PrincipalCompensationPartner is the compensation partner.
	PrincipalCompensationPartner = "principal:comp-3"
	// PrincipalFinancePartner is the finance partner for the cost center.
	PrincipalFinancePartner = "principal:finance-4"
	// PrincipalExecutiveA and PrincipalExecutiveB sit on the compensation
	// committee.
	PrincipalExecutiveA = "principal:exec-5"
	// PrincipalExecutiveB is the second committee member.
	PrincipalExecutiveB = "principal:exec-6"
	// PrincipalDeputy is a deputy who holds every baseline authority and is a
	// member of the escalation fallback group.
	PrincipalDeputy = "principal:deputy-9"
	// PrincipalIntern is also in the fallback group but holds no approval
	// authority at all, which is how a fallback that would broaden authority
	// gets proved to be refused rather than assumed not to happen.
	PrincipalIntern = "principal:intern-8"
	// PrincipalDelegate holds a bounded delegation from the manager.
	PrincipalDelegate = "principal:delegate-10"
	// PrincipalExpiredDelegate holds a delegation that has already lapsed.
	PrincipalExpiredDelegate = "principal:delegate-11"
	// PrincipalOverbroadDelegate holds a delegation that does not carry the
	// authority the requirement needs.
	PrincipalOverbroadDelegate = "principal:delegate-12"
)

// The promotion scenario's organizational frames.
const (
	// ScenarioWorkerID is the SUBJECT frame's worker.
	ScenarioWorkerID = "worker:jane"
	// ScenarioOrganizationScopeID is the ORGANIZATION frame.
	ScenarioOrganizationScopeID = "org:acme-eu:engineering"
	// ScenarioLegalEntityID is the LEGAL_ENTITY frame.
	ScenarioLegalEntityID = "legal:acme-eu-gmbh"
	// ScenarioCostCenterID is the COST_CENTER frame.
	ScenarioCostCenterID = "cost-center:eng-platform"
	// ScenarioFallbackGroupID is the deputy group escalation reassigns to.
	ScenarioFallbackGroupID = "group.approval_deputies"
	// ScenarioDirectoryVersion identifies the fixture projection.
	ScenarioDirectoryVersion = "directory.promotion_fixture/2026.1"
	// ScenarioGovernancePolicyRef identifies the fixture governance policy.
	ScenarioGovernancePolicyRef = "policy.promotion_approval_governance/2026.1"
)

// ScenarioAt is the fixed effective time the promotion fixture resolves at. It
// is a parameter of the fixture rather than time.Now so that a resolution and
// everything derived from it replay byte-identically.
func ScenarioAt() values.Instant {
	return values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
}

// ScenarioClock returns a clock pinned to [ScenarioAt].
func ScenarioClock() Clock {
	at := ScenarioAt()
	return func() values.Instant { return at }
}

// PromotionGovernancePolicy returns the fixture's governance configuration:
// a two-business-week decision window, a decision that stays fresh for thirty
// days, the full separation-of-duties set, deadline escalation to the deputy
// group, a two-executive committee quorum, and the invalidator set every
// requirement declares.
func PromotionGovernancePolicy() GovernancePolicy {
	return GovernancePolicy{
		PolicyRef:        ScenarioGovernancePolicyRef,
		DecisionWindow:   10 * 24 * time.Hour,
		DecisionValidity: 30 * 24 * time.Hour,
		Separation: SeparationConstraints{
			RequesterMayNotApprove:      true,
			SubjectMayNotApprove:        true,
			OneRequirementPerPrincipal:  true,
			ForbidRequesterManagerChain: true,
			RuleID:                      "policy.promotion_separation_of_duties/2026.1",
		},
		EscalationAction: EscalationReassignToFallback,
		EscalationRuleID: "policy.promotion_escalation/2026.1",
		FallbackGroupID:  ScenarioFallbackGroupID,
		ExecutiveQuorum:  2,
		Invalidators: []Invalidator{
			{Kind: InvalidatorMaterialProposalChange, RuleID: "policy.invalidate_on_material_change/2026.1"},
			{Kind: InvalidatorAuthorityRevoked, RuleID: "policy.invalidate_on_authority_revoked/2026.1"},
			{Kind: InvalidatorDelegationExpired, RuleID: "policy.invalidate_on_delegation_expiry/2026.1"},
			{Kind: InvalidatorDeadlineExpired, RuleID: "policy.invalidate_on_decision_expiry/2026.1"},
			{Kind: InvalidatorMandatoryDeny, RuleID: "policy.invalidate_on_mandatory_deny/2026.1"},
		},
	}
}

// PromotionInputStandard is a small, in-band, funded raise: the baseline
// reviewers are sufficient.
func PromotionInputStandard() rules.PromotionApprovalInput {
	return rules.PromotionApprovalInput{
		IncreasePercent: values.MustDecimal("5.0000", rules.IncreasePercentScale, values.RoundingHalfEven),
		BandPosition:    rules.BandPositionInBand,
		BudgetAuthority: rules.BudgetAuthoritySufficient,
	}
}

// PromotionInputFinance crosses the finance threshold.
func PromotionInputFinance() rules.PromotionApprovalInput {
	in := PromotionInputStandard()
	in.IncreasePercent = values.MustDecimal("12.0000", rules.IncreasePercentScale, values.RoundingHalfEven)
	return in
}

// PromotionInputExecutive crosses the executive threshold.
func PromotionInputExecutive() rules.PromotionApprovalInput {
	in := PromotionInputStandard()
	in.IncreasePercent = values.MustDecimal("25.0000", rules.IncreasePercentScale, values.RoundingHalfEven)
	return in
}

// PromotionInputUnresolved leaves the budget authority explicitly unknown, so
// the table returns UNKNOWN_BLOCKED.
func PromotionInputUnresolved() rules.PromotionApprovalInput {
	in := PromotionInputStandard()
	in.BudgetAuthority = rules.BudgetAuthorityUnknown
	return in
}

// PromotionScenario is the assembled in-memory promotion fixture: the
// directory projection, the derived requirement set, and the resolution input
// they are resolved with.
type PromotionScenario struct {
	Directory    *MemoryDirectory
	Policy       GovernancePolicy
	Tier         rules.PromotionApprovalDecision
	Requirements RequirementSet
	Resolution   ResolutionInput
	Clock        Clock
}

// NewPromotionScenario evaluates the real promotion threshold table for in,
// derives the requirement set from the tier it returns, and seeds a directory
// that satisfies the whole graph.
//
// The tier is not passed in as a constant: the fixture runs the same rules
// engine production would, so a change to the table's thresholds shows up here
// as a changed requirement set rather than as a fixture that quietly disagrees
// with the engine.
func NewPromotionScenario(in rules.PromotionApprovalInput) (PromotionScenario, error) {
	tier, err := rules.EvaluatePromotionApproval(rules.PromotionApprovalThresholdTable(), in)
	if err != nil {
		return PromotionScenario{}, err
	}
	policy := PromotionGovernancePolicy()
	clock := ScenarioClock()
	set, err := DeriveRequirements(DerivationInput{
		Tier:                tier,
		Policy:              policy,
		SubjectWorkerID:     ScenarioWorkerID,
		OrganizationScopeID: ScenarioOrganizationScopeID,
		LegalEntityID:       ScenarioLegalEntityID,
		CostCenterID:        ScenarioCostCenterID,
	}, clock)
	if err != nil {
		return PromotionScenario{}, err
	}
	return PromotionScenario{
		Directory:    PromotionDirectory(),
		Policy:       policy,
		Tier:         tier,
		Requirements: set,
		Resolution: ResolutionInput{
			RequesterPrincipalID: PrincipalRequester,
			SubjectPrincipalIDs:  []string{PrincipalSubject},
			EffectiveAt:          ScenarioAt(),
			ClaimedBy:            map[string]string{},
		},
		Clock: clock,
	}, nil
}

// ResolveAll resolves every requirement in the scenario's set, in stage order.
//
// It does not invent one-role-per-principal claims: a claim records who
// actually filled a requirement, which is known when a decision lands and not
// when candidates are listed. A caller collecting decisions threads them back
// through ResolutionInput.ClaimedBy.
func (s PromotionScenario) ResolveAll() ([]Resolution, error) {
	out := make([]Resolution, 0, len(s.Requirements.Requirements))
	for _, req := range s.Requirements.Requirements {
		res, err := Resolve(req, s.Resolution, s.Directory, s.Clock)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

// PromotionDirectory returns the fixture org/authority projection: who holds
// which relationship and role, who is available, who manages whom, and the
// three delegations - one valid, one expired, one carrying insufficient
// authority - that the delegation rules are proved against.
func PromotionDirectory() *MemoryDirectory {
	subject := Scope{Kind: ScopeSubject, Ref: ScenarioWorkerID}
	org := Scope{Kind: ScopeOrganization, Ref: ScenarioOrganizationScopeID}
	cost := Scope{Kind: ScopeCostCenter, Ref: ScenarioCostCenterID}
	legal := Scope{Kind: ScopeLegalEntity, Ref: ScenarioLegalEntityID}

	d := NewMemoryDirectory(ScenarioDirectoryVersion)

	d.WithHolders(Relationship(RelationshipManagerOf, subject).term(), PrincipalManager).
		WithHolders(Relationship(RelationshipHRBPFor, org).term(), PrincipalHRBP).
		WithHolders(Relationship(RelationshipCompensationPartnerFor, org).term(), PrincipalCompensationPartner).
		WithHolders(Relationship(RelationshipFinancePartnerFor, cost).term(), PrincipalFinancePartner).
		WithHolders(Group("group.compensation_committee", legal).term(), PrincipalExecutiveA, PrincipalExecutiveB).
		WithHolders(Role(RoleExecutiveApprover, legal).term(), PrincipalExecutiveA, PrincipalExecutiveB).
		WithHolders(Group(ScenarioFallbackGroupID, org).term(), PrincipalDeputy, PrincipalIntern)

	all := []string{RolePeopleManager, RoleHRBP, RoleCompensationPartner, RoleFinancePartner}
	for _, f := range []PrincipalFacts{
		{PrincipalID: PrincipalRequester, Roles: []string{RoleHRBP}},
		{PrincipalID: PrincipalSubject, Roles: nil},
		{PrincipalID: PrincipalManager, Roles: []string{RolePeopleManager}},
		{PrincipalID: PrincipalHRBP, Roles: []string{RoleHRBP}},
		{PrincipalID: PrincipalCompensationPartner, Roles: []string{RoleCompensationPartner}},
		{PrincipalID: PrincipalFinancePartner, Roles: []string{RoleFinancePartner}},
		{PrincipalID: PrincipalExecutiveA, Roles: []string{RoleExecutiveApprover}},
		{PrincipalID: PrincipalExecutiveB, Roles: []string{RoleExecutiveApprover}},
		{PrincipalID: PrincipalDeputy, Roles: all},
		{PrincipalID: PrincipalIntern, Roles: nil},
		{PrincipalID: PrincipalDelegate, Roles: nil},
		{PrincipalID: PrincipalExpiredDelegate, Roles: nil},
		{PrincipalID: PrincipalOverbroadDelegate, Roles: nil},
	} {
		f.Active = true
		f.Available = true
		f.OrganizationScopeID = ScenarioOrganizationScopeID
		f.IdentityAssuranceRef = "assurance.mfa_session/v1"
		d.WithPrincipal(f)
	}

	// The requester deliberately has no manager edge here: seeding one would
	// make the manager-chain constraint fire on the baseline graph and every
	// scenario would silently run on escalation. A test that wants that
	// conflict adds the edge with WithManager.
	d.WithManager(PrincipalSubject, PrincipalManager).
		WithManager(PrincipalManager, PrincipalExecutiveA)

	at := ScenarioAt().Time()
	d.WithDelegation(Delegation{
		DelegationID:          "delegation:manager-holiday",
		FromPrincipalID:       PrincipalManager,
		ToPrincipalID:         PrincipalDelegate,
		AllowedRequirementIDs: []string{RequirementCurrentManager},
		AllowedRoles:          []string{RolePeopleManager},
		Scope:                 subject,
		NotBefore:             values.NewInstant(at.Add(-24 * time.Hour)),
		Expiry:                values.NewInstant(at.Add(14 * 24 * time.Hour)),
		PolicyRef:             "policy.delegation/2026.1",
	}).WithDelegation(Delegation{
		DelegationID:          "delegation:manager-lapsed",
		FromPrincipalID:       PrincipalManager,
		ToPrincipalID:         PrincipalExpiredDelegate,
		AllowedRequirementIDs: []string{RequirementCurrentManager},
		AllowedRoles:          []string{RolePeopleManager},
		Scope:                 subject,
		NotBefore:             values.NewInstant(at.Add(-60 * 24 * time.Hour)),
		Expiry:                values.NewInstant(at.Add(-24 * time.Hour)),
		PolicyRef:             "policy.delegation/2026.1",
	}).WithDelegation(Delegation{
		DelegationID:          "delegation:manager-admin-only",
		FromPrincipalID:       PrincipalManager,
		ToPrincipalID:         PrincipalOverbroadDelegate,
		AllowedRequirementIDs: []string{RequirementCurrentManager},
		AllowedRoles:          []string{"role.calendar_admin"},
		Scope:                 subject,
		NotBefore:             values.NewInstant(at.Add(-24 * time.Hour)),
		Expiry:                values.NewInstant(at.Add(14 * 24 * time.Hour)),
		PolicyRef:             "policy.delegation/2026.1",
	})
	return d
}
