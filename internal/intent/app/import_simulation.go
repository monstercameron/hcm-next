package app

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/domains/dataops/importing"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var ErrImportProposal = errors.New("app: import proposal is invalid")

// ImportWriteAuthority is trusted server-side context for one proposed field.
// It is deliberately separate from CSV-derived values and simulation claims.
type ImportWriteAuthority struct {
	ResourceKey             values.ResourceKey
	ExpectedRevision        values.RevisionToken
	SourceAuthorityDecision string
}

// ImportRowAuthority binds a simulated row to its genuine intent and resource.
type ImportRowAuthority struct {
	IntentID             string
	ProposalRevision     uint64
	SupersedesRevisionID *string
	Subject              intent.SubjectReference
	Writes               map[string]ImportWriteAuthority
}

// ImportSimulationPins are re-read from authenticated platform state at the
// proposal boundary. They prevent replay of an authentic stale simulation.
type ImportSimulationPins struct {
	BatchDigest      string
	MappingDigest    string
	ValidationDigest string
	SnapshotDigest   string
	ModelDigest      string
}

// ImportProposalRequest contains verified platform context used to mint
// immutable ProposalRevisions. None of these authority fields come from CSV.
type ImportProposalRequest struct {
	Simulation importing.ImportSimulation
	Verifier   importing.Verifier
	Registry   *intent.Registry
	Digester   intent.Digester
	IDs        intent.IDSource
	Clock      intent.Clock

	Tenant              values.TenantId
	OrganizationScopeID string
	LegalEntityID       string
	EffectiveTime       values.EffectiveInterval
	CreatedBy           intent.PrincipalReference
	ControlSnapshots    intent.ControlSnapshots
	TrustedPins         ImportSimulationPins
	Rows                map[string]ImportRowAuthority
	RequiredApprovals   []intent.RequiredApproval
	Obligations         []intent.Obligation
	LegalReviewRequired int
	Purpose             intent.PurposeDecision
	Revalidation        intent.RevalidationPlan
}

// NewImportProposalRevisions verifies the exact signed simulation bytes and
// converts only CREATE/CHANGE drafts into canonical kernel proposal revisions.
// It has no store or effect port and therefore cannot commit the import.
func NewImportProposalRevisions(req ImportProposalRequest) ([]intent.ProposalRevision, error) {
	if err := importing.VerifyImportSimulation(req.Simulation, req.Verifier); err != nil {
		return nil, fmt.Errorf("%w: simulation signature: %v", ErrImportProposal, err)
	}
	if !req.Simulation.ZeroEffects || req.Simulation.CommittedEffects != 0 || req.Simulation.SideEffectCount != 0 || req.Simulation.EstimatedCost != 0 {
		return nil, fmt.Errorf("%w: simulation is not zero-effect", ErrImportProposal)
	}
	if req.Registry == nil {
		return nil, fmt.Errorf("%w: intent registry is required", ErrImportProposal)
	}
	if req.Simulation.Tenant != req.Tenant.String() || req.Simulation.OrganizationScopeID != req.OrganizationScopeID || req.Simulation.LegalEntityID != req.LegalEntityID || req.Simulation.EffectiveTime != req.EffectiveTime.String() {
		return nil, fmt.Errorf("%w: trusted tenant or effective context does not match the signed simulation", ErrImportProposal)
	}
	pins := req.TrustedPins
	if pins.BatchDigest != req.Simulation.BatchDigest || pins.MappingDigest != req.Simulation.MappingDigest || pins.ValidationDigest != req.Simulation.ValidationDigest || pins.SnapshotDigest != req.Simulation.SnapshotDigest || pins.ModelDigest != req.Simulation.ModelDigest {
		return nil, fmt.Errorf("%w: authenticated source pins do not match the signed simulation", ErrImportProposal)
	}
	if req.Simulation.ApprovalRequired != len(req.RequiredApprovals) || req.Simulation.LegalReviewRequired != req.LegalReviewRequired || !sameObligationIDs(req.Simulation.Obligations, req.Obligations) {
		return nil, fmt.Errorf("%w: proposal governance does not match the signed simulation", ErrImportProposal)
	}

	revisions := make([]intent.ProposalRevision, 0, req.Simulation.Creates+req.Simulation.Changes)
	intentIDs := make(map[string]struct{}, req.Simulation.Creates+req.Simulation.Changes)
	revisionIDs := make(map[string]struct{}, req.Simulation.Creates+req.Simulation.Changes)
	for _, draft := range req.Simulation.Drafts {
		if draft.Status != "CREATE" && draft.Status != "CHANGE" {
			continue
		}
		ref, err := intent.ParseRef(draft.IntentType)
		if err != nil {
			return nil, fmt.Errorf("%w: row %s names an invalid intent definition: %v", ErrImportProposal, draft.RowID, err)
		}
		definition, err := req.Registry.ResolveForInstantiation(ref)
		if err != nil || definition.Family != intent.FamilyChangeRequest {
			return nil, fmt.Errorf("%w: row %s does not name a published change-request definition", ErrImportProposal, draft.RowID)
		}
		binding, ok := req.Rows[draft.RowID]
		if !ok || binding.IntentID == "" || binding.ProposalRevision == 0 {
			return nil, fmt.Errorf("%w: row %s has no trusted intent binding", ErrImportProposal, draft.RowID)
		}
		if _, duplicate := intentIDs[binding.IntentID]; duplicate {
			return nil, fmt.Errorf("%w: intent %s is bound to more than one row", ErrImportProposal, binding.IntentID)
		}
		intentIDs[binding.IntentID] = struct{}{}
		if binding.Subject.Kind != draft.SubjectKind || binding.Subject.SubjectID != draft.SubjectID || binding.Subject.AuthorityDomain != draft.AuthorityDomain {
			return nil, fmt.Errorf("%w: row %s subject does not match trusted authority", ErrImportProposal, draft.RowID)
		}

		spec := intent.ProposalSpec{
			IntentID: binding.IntentID, Revision: binding.ProposalRevision, Tenant: req.Tenant,
			OrganizationScopeID: req.OrganizationScopeID, LegalEntityID: req.LegalEntityID,
			Subjects: []intent.SubjectReference{binding.Subject}, EffectiveTime: req.EffectiveTime,
			RequiredApprovals: append([]intent.RequiredApproval(nil), req.RequiredApprovals...),
			Obligations:       append([]intent.Obligation(nil), req.Obligations...), Purpose: req.Purpose,
			Revalidation:     intent.RevalidationPlan{Rules: append([]string(nil), req.Revalidation.Rules...)},
			ControlSnapshots: req.ControlSnapshots, CreatedBy: req.CreatedBy,
			SupersedesRevisionID: binding.SupersedesRevisionID,
		}
		for _, write := range draft.Writes {
			authority, exists := binding.Writes[write.Property]
			if !exists || write.AuthorityRef == "" || authority.SourceAuthorityDecision != write.AuthorityRef || !authority.ExpectedRevision.IsSpecified() || authority.ExpectedRevision.String() != write.BaselineRevision || authority.ResourceKey.Validate() != nil || authority.ResourceKey.Tenant != req.Tenant || authority.ResourceKey.String() != write.ResourceKey {
				return nil, fmt.Errorf("%w: row %s field %s lacks trusted resource, authority, or revision", ErrImportProposal, draft.RowID, write.Property)
			}
			planned := intent.PlannedWrite{Subject: binding.Subject, ResourceKey: authority.ResourceKey,
				FieldPath: write.Property, ProposedCanonicalText: write.Proposed,
				SourceAuthorityDecision: authority.SourceAuthorityDecision, ExpectedRevision: authority.ExpectedRevision}
			proposed := intent.StateAssertion{Subject: binding.Subject, ResourceKey: authority.ResourceKey, FieldPath: write.Property, CanonicalText: write.Proposed}
			spec.Writes = append(spec.Writes, planned)
			spec.ProposedState = append(spec.ProposedState, proposed)
			if write.CurrentPresent {
				planned.CurrentCanonicalText = write.Current
				spec.Writes[len(spec.Writes)-1] = planned
				spec.CurrentState = append(spec.CurrentState, intent.StateAssertion{Subject: binding.Subject, ResourceKey: authority.ResourceKey, FieldPath: write.Property, CanonicalText: write.Current})
			}
		}
		rev, err := intent.NewProposalRevision(spec, definition, req.Digester, req.IDs, req.Clock)
		if err != nil {
			return nil, fmt.Errorf("%w: row %s: %v", ErrImportProposal, draft.RowID, err)
		}
		if _, duplicate := revisionIDs[rev.ProposalRevisionID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate proposal revision id %s", ErrImportProposal, rev.ProposalRevisionID)
		}
		revisionIDs[rev.ProposalRevisionID] = struct{}{}
		revisions = append(revisions, rev)
	}
	if len(revisions) != req.Simulation.Creates+req.Simulation.Changes {
		return nil, fmt.Errorf("%w: simulation counts do not partition proposal drafts", ErrImportProposal)
	}
	return revisions, nil
}

func sameObligationIDs(signed []string, trusted []intent.Obligation) bool {
	ids := make([]string, len(trusted))
	for i := range trusted {
		ids[i] = trusted[i].ObligationID
	}
	sort.Strings(ids)
	return len(signed) == len(ids) && equalStrings(signed, ids)
}

func equalStrings(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
