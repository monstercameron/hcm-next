package humanwork

import (
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Requirement identifiers the promotion approval graph produces. They are
// exported because a proposal's RequiredApproval entries and, later, one
// WorkItem per decision slot both cite them.
const (
	// RequirementCurrentManager is the reference workflow's first baseline
	// reviewer.
	RequirementCurrentManager = "req.promotion.current_manager/v1"
	// RequirementHRBP is the HR business partner for the worker's organization.
	RequirementHRBP = "req.promotion.hrbp/v1"
	// RequirementCompensationPartner is the compensation partner for the
	// organization.
	RequirementCompensationPartner = "req.promotion.compensation_partner/v1"
	// RequirementFinancePartner is added by the FINANCE_REQUIRED tier.
	RequirementFinancePartner = "req.promotion.finance_partner/v1"
	// RequirementExecutiveCommittee is added by the EXECUTIVE_REQUIRED tier.
	RequirementExecutiveCommittee = "req.promotion.executive_committee/v1"
)

// Roles the promotion approval graph requires. A role name is configuration in
// the directory projection; these constants are this reference derivation's
// choice of names, cited by the requirements it builds.
const (
	RolePeopleManager       = "role.people_manager"
	RoleHRBP                = "role.hrbp"
	RoleCompensationPartner = "role.compensation_partner"
	RoleFinancePartner      = "role.finance_partner"
	RoleExecutiveApprover   = "role.executive_approver"
)

// GovernancePolicy is the versioned configuration that turns an approval tier
// into a requirement set: how long approvers have, how long a decision stays
// fresh, what separation of duties applies, what happens at the deadline, and
// how many executives a committee review needs.
//
// It is a value the caller supplies rather than constants in this file, because
// a threshold, a window and a quorum are all customer configuration; what this
// package owns is the shape of the graph, not the numbers in it.
type GovernancePolicy struct {
	PolicyRef string

	// DecisionWindow is how long after derivation a decision is due.
	DecisionWindow time.Duration
	// DecisionValidity is how long a recorded decision stays current. It runs
	// from the same derivation instant, so it must be at least DecisionWindow.
	DecisionValidity time.Duration

	Separation SeparationConstraints

	// EscalationAction is what happens when DecisionWindow elapses.
	EscalationAction EscalationAction
	EscalationRuleID string
	// FallbackGroupID is the deputy group escalation reassigns to. It is
	// required when EscalationAction is REASSIGN_TO_FALLBACK.
	FallbackGroupID string

	// ExecutiveQuorum is how many distinct executives an EXECUTIVE_REQUIRED
	// review needs.
	ExecutiveQuorum uint32

	// Invalidators are the triggers every requirement in the set declares.
	Invalidators []Invalidator
}

// Validate rejects a governance policy that cannot produce a compilable
// requirement.
func (p GovernancePolicy) Validate() error {
	if p.PolicyRef == "" {
		return newError("Validate", "policy.policy_ref", ErrInvalidRequirement,
			"governance policy has no reference")
	}
	if p.DecisionWindow <= 0 {
		return newError("Validate", "policy.decision_window", ErrInvalidRequirement,
			"decision window must be positive")
	}
	if p.DecisionValidity < p.DecisionWindow {
		return newError("Validate", "policy.decision_validity", ErrInvalidRequirement,
			"decision validity %s is shorter than the decision window %s",
			p.DecisionValidity, p.DecisionWindow)
	}
	if p.EscalationAction == EscalationReassignToFallback && p.FallbackGroupID == "" {
		return newError("Validate", "policy.fallback_group_id", ErrInvalidRequirement,
			"REASSIGN_TO_FALLBACK names no fallback group")
	}
	if p.ExecutiveQuorum == 0 {
		return newError("Validate", "policy.executive_quorum", ErrInvalidRequirement,
			"executive quorum must be at least one")
	}
	return p.Separation.Validate()
}

// DerivationInput is the promotion proposal's governance context: the rules
// engine's tier decision plus the organizational frames the approval graph
// resolves in.
type DerivationInput struct {
	Tier   rules.PromotionApprovalDecision
	Policy GovernancePolicy

	// SubjectWorkerID is the worker being promoted, used as the SUBJECT frame.
	SubjectWorkerID string
	// OrganizationScopeID, LegalEntityID and CostCenterID are the frames the
	// HRBP, compensation, finance and executive terms resolve in.
	OrganizationScopeID string
	LegalEntityID       string
	CostCenterID        string
}

// DeriveRequirements builds the promotion approval graph from the rules
// engine's tier decision plus governance configuration.
//
// The baseline - current manager, HRBP, compensation partner - is the reference
// workflow's constant approval graph and is present at every tier. The tier
// only ever adds: FINANCE_REQUIRED adds the finance partner for the affected
// cost center, EXECUTIVE_REQUIRED adds that plus a committee review with its
// own distinct-principal quorum. UNKNOWN_BLOCKED adds nothing because it states
// nothing; it is returned as ErrTierBlocked rather than being read as an empty
// requirement set, which would be exactly the "missing value resolves to no
// approval required" failure the rules engine refuses to make.
func DeriveRequirements(in DerivationInput, clock Clock) (RequirementSet, error) {
	if clock == nil {
		return RequirementSet{}, newError("DeriveRequirements", "clock", ErrInvalidRequirement,
			"no clock supplied")
	}
	if err := in.Policy.Validate(); err != nil {
		return RequirementSet{}, err
	}
	for _, f := range []struct{ field, value string }{
		{"subject_worker_id", in.SubjectWorkerID},
		{"organization_scope_id", in.OrganizationScopeID},
		{"legal_entity_id", in.LegalEntityID},
		{"cost_center_id", in.CostCenterID},
	} {
		if f.value == "" {
			return RequirementSet{}, newError("DeriveRequirements", f.field, ErrInvalidRequirement,
				"derivation names no %s", f.field)
		}
	}
	if in.Tier.Tier == rules.ApprovalTierUnknownBlocked {
		return RequirementSet{}, newError("DeriveRequirements", "tier", ErrTierBlocked,
			"table %s@%s row %q returned UNKNOWN_BLOCKED",
			in.Tier.TableID, in.Tier.TableVersion, in.Tier.MatchedRowID)
	}

	now := clock()
	if !now.IsSet() {
		return RequirementSet{}, newError("DeriveRequirements", "derived_at", ErrInvalidRequirement,
			"clock returned an unset instant")
	}
	deadline := Deadline{
		DecideBy: values.NewInstant(now.Time().Add(in.Policy.DecisionWindow)),
		Expiry:   values.NewInstant(now.Time().Add(in.Policy.DecisionValidity)),
	}
	source := RequirementSource{
		Tier:                in.Tier.Tier,
		TableID:             in.Tier.TableID,
		TableVersion:        in.Tier.TableVersion,
		TableDigest:         in.Tier.TableDigest,
		MatchedRowID:        in.Tier.MatchedRowID,
		GovernancePolicyRef: in.Policy.PolicyRef,
	}

	orgScope := Scope{Kind: ScopeOrganization, Ref: in.OrganizationScopeID}
	subjectScope := Scope{Kind: ScopeSubject, Ref: in.SubjectWorkerID}
	costScope := Scope{Kind: ScopeCostCenter, Ref: in.CostCenterID}
	legalScope := Scope{Kind: ScopeLegalEntity, Ref: in.LegalEntityID}

	single := Quorum{MinApprovals: 1}
	specs := []RequirementSpec{
		{
			RequirementID:  RequirementCurrentManager,
			Stage:          1,
			Candidates:     Relationship(RelationshipManagerOf, subjectScope),
			AuthorityFloor: []string{RolePeopleManager},
			Quorum:         single,
		},
		{
			RequirementID:  RequirementHRBP,
			Stage:          2,
			Candidates:     Relationship(RelationshipHRBPFor, orgScope),
			AuthorityFloor: []string{RoleHRBP},
			Quorum:         single,
		},
		{
			RequirementID:  RequirementCompensationPartner,
			Stage:          3,
			Candidates:     Relationship(RelationshipCompensationPartnerFor, orgScope),
			AuthorityFloor: []string{RoleCompensationPartner},
			Quorum:         single,
		},
	}
	switch in.Tier.Tier {
	case rules.ApprovalTierStandard:
	case rules.ApprovalTierFinanceRequired:
		specs = append(specs, financeSpec(costScope))
	case rules.ApprovalTierExecutiveRequired:
		specs = append(specs, financeSpec(costScope), RequirementSpec{
			RequirementID: RequirementExecutiveCommittee,
			Stage:         5,
			Candidates: AnyOf(
				Group("group.compensation_committee", legalScope),
				Role(RoleExecutiveApprover, legalScope),
			),
			AuthorityFloor: []string{RoleExecutiveApprover},
			Quorum: Quorum{
				MinApprovals:              in.Policy.ExecutiveQuorum,
				RequireDistinctPrincipals: in.Policy.ExecutiveQuorum > 1,
			},
		})
	default:
		return RequirementSet{}, newError("DeriveRequirements", "tier", ErrInvalidRequirement,
			"approval tier %q is not a declared tier", string(in.Tier.Tier))
	}

	set := RequirementSet{Tier: in.Tier.Tier, DerivedAt: now, Source: source}
	for _, spec := range specs {
		spec.Revision = 1
		spec.Deadline = deadline
		spec.Separation = in.Policy.Separation
		spec.Invalidators = in.Policy.Invalidators
		spec.Source = source
		spec.Escalation = escalationFor(in.Policy, orgScope)
		req, err := Compile(spec)
		if err != nil {
			return RequirementSet{}, err
		}
		set.Requirements = append(set.Requirements, req)
	}
	sort.Slice(set.Requirements, func(i, j int) bool {
		if set.Requirements[i].Stage != set.Requirements[j].Stage {
			return set.Requirements[i].Stage < set.Requirements[j].Stage
		}
		return set.Requirements[i].RequirementID < set.Requirements[j].RequirementID
	})
	return set, nil
}

func financeSpec(costScope Scope) RequirementSpec {
	return RequirementSpec{
		RequirementID:  RequirementFinancePartner,
		Stage:          4,
		Candidates:     Relationship(RelationshipFinancePartnerFor, costScope),
		AuthorityFloor: []string{RoleFinancePartner},
		Quorum:         Quorum{MinApprovals: 1},
	}
}

func escalationFor(p GovernancePolicy, orgScope Scope) EscalationPolicy {
	e := EscalationPolicy{OnDeadline: p.EscalationAction, RuleID: p.EscalationRuleID}
	if p.EscalationAction == EscalationReassignToFallback {
		fb := Group(p.FallbackGroupID, orgScope)
		e.Fallback = &fb
	}
	return e
}
