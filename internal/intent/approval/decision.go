package approval

import (
	"sort"
	"sync"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	decisionSchema   = "hcmnext.intent.approval.ApprovalDecision"
	voteSchema       = "hcmnext.intent.approval.Vote"
	assessmentSchema = "hcmnext.intent.approval.MaterialityAssessment"
)

// Outcome is what an approver decided.
type Outcome string

// Outcomes.
const (
	// OutcomeUnspecified is the zero value and is never a legal decision.
	OutcomeUnspecified Outcome = ""
	// OutcomeApproved is an approval.
	OutcomeApproved Outcome = "APPROVED"
	// OutcomeRejected is a rejection. It is a decision like any other: bound,
	// evidenced and immutable, never the absence of one.
	OutcomeRejected Outcome = "REJECTED"
)

// Valid reports whether o is a declared outcome.
func (o Outcome) Valid() bool { return o == OutcomeApproved || o == OutcomeRejected }

// Projection is the server-held record of what was rendered to an approver for
// one requirement at one task version.
//
// APPROVAL-006 will compute Digest from the actual rendering - visible fields,
// hidden-field manifest, warnings, effects. What matters here, and what this
// type exists to make structural, is that the digest is held by the server and
// bound by the server: a vote references a task version, never a digest, so
// there is no field through which a client could supply one.
type Projection struct {
	RequirementID string
	TaskVersion   uint64
	Digest        string
	// Safety is server-owned render state. Only SAFE_TO_DECIDE permits a vote.
	Safety humanwork.ProjectionSafety
}

// ApproverReference identifies who decided and by what route.
type ApproverReference struct {
	PrincipalID          string
	IdentityAssuranceRef string
	SessionRef           string
	// Via and DelegationID must match the candidate record the current
	// resolution holds. A voter claiming a delegated route they do not hold is
	// an authority change, not a formatting difference.
	Via          humanwork.CandidateSource
	DelegationID string
}

// DecisionBinding is the exact context a decision is bound to. Every field is
// filled from the server-held revision, resolution and projection; none of it
// is copied from the vote.
type DecisionBinding struct {
	RequirementID       string
	RequirementRevision uint64

	IntentID           string
	ProposalRevisionID string
	// ProposalDigest is the full canonical digest reference - profile, schema,
	// algorithm, scope binding and hash - not a naked hash. A bare hash cannot
	// be re-derived and therefore cannot be audited.
	ProposalDigest digest.Reference

	// TaskVersion and RenderedProjectionDigest bind what the approver was
	// shown.
	TaskVersion              uint64
	RenderedProjectionDigest string

	// ControlSnapshots is the revalidated context the proposal was simulated
	// in, including the LegalContext digest. It is recorded, not hashed into
	// the proposal digest, which is what lets a policy republish be revalidated
	// rather than invalidating every pending approval.
	ControlSnapshots intent.ControlSnapshots

	// RequirementDigest and ResolutionExpressionDigest cite the compiled
	// requirement and candidate expression the authority check was made
	// against.
	RequirementDigest          string
	ResolutionExpressionDigest string
}

// LegalContextDigest returns the LegalContext reference this decision was bound
// to.
func (b DecisionBinding) LegalContextDigest() string {
	return b.ControlSnapshots.LegalContextDigest
}

// ApprovalDecision is one immutable, proposal-bound decision.
type ApprovalDecision struct {
	DecisionID string
	Binding    DecisionBinding
	Outcome    Outcome
	Approver   ApproverReference
	// AuthorityDecisionRef cites the authorization decision that permitted the
	// approver to act.
	AuthorityDecisionRef string
	Reason               string
	DecidedAt            values.Instant
	// VoteDigest is the canonical digest of the submission. A duplicate
	// identical submission replays the decision this digest already produced.
	VoteDigest string
}

// Canonical returns the deterministic byte encoding of the decision.
func (d ApprovalDecision) Canonical() []byte {
	c := d.Binding.ControlSnapshots
	raw, err := canonicalbytes.New(decisionSchema, 1).
		String("decision_id", d.DecisionID).
		String("requirement_id", d.Binding.RequirementID).
		Int("requirement_revision", int64(d.Binding.RequirementRevision)).
		String("intent_id", d.Binding.IntentID).
		String("proposal_revision_id", d.Binding.ProposalRevisionID).
		String("proposal_digest.profile_id", d.Binding.ProposalDigest.ProfileID).
		Int("proposal_digest.profile_version", int64(d.Binding.ProposalDigest.ProfileVersion)).
		String("proposal_digest.schema_id", d.Binding.ProposalDigest.SchemaID).
		Int("proposal_digest.schema_version", int64(d.Binding.ProposalDigest.SchemaVersion)).
		String("proposal_digest.algorithm_id", d.Binding.ProposalDigest.AlgorithmID).
		String("proposal_digest.digest", d.Binding.ProposalDigest.Digest).
		String("proposal_digest.scope_binding", d.Binding.ProposalDigest.ScopeBindingDigest).
		Int("task_version", int64(d.Binding.TaskVersion)).
		String("rendered_projection_digest", d.Binding.RenderedProjectionDigest).
		String("requirement_digest", d.Binding.RequirementDigest).
		String("resolution_expression_digest", d.Binding.ResolutionExpressionDigest).
		String("controls.capability_registry", c.CapabilityRegistryDigest).
		String("controls.policy_bundle", c.PolicyBundleDigest).
		String("controls.legal_context", c.LegalContextDigest).
		String("controls.entitlement", c.EntitlementDigest).
		String("controls.reference_data", c.ReferenceDataDigest).
		String("controls.workflow_definition", c.WorkflowDefinitionDigest).
		String("controls.connector_configuration", c.ConnectorConfigurationDigest).
		String("controls.classification_taxonomy", c.ClassificationTaxonomyDigest).
		String("controls.classification_label_set", c.ClassificationLabelSetDigest).
		String("controls.classification_watermark", c.ClassificationPropagationWatermark).
		String("controls.dlp_decision", c.DLPDecisionDigest).
		String("controls.destination_trust", c.DestinationTrustDigest).
		String("controls.purpose_and_residency", c.PurposeAndResidencyDigest).
		String("outcome", string(d.Outcome)).
		String("approver.principal_id", d.Approver.PrincipalID).
		String("approver.identity_assurance_ref", d.Approver.IdentityAssuranceRef).
		String("approver.session_ref", d.Approver.SessionRef).
		String("approver.via", string(d.Approver.Via)).
		String("approver.delegation_id", d.Approver.DelegationID).
		String("authority_decision_ref", d.AuthorityDecisionRef).
		String("reason", d.Reason).
		Value("decided_at", d.DecidedAt).
		String("vote_digest", d.VoteDigest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical decision encoding.
func (d ApprovalDecision) Digest() string {
	raw := d.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Vote is a submitted decision.
//
// It carries assertions, not bindings. ProposalRevisionID, ProposalDigest,
// ControlSnapshots and TaskVersion say what the voter believes they are
// deciding on; each is checked against the server-held value and any
// disagreement is refused. What the decision records is always the server's
// copy, which is why there is no rendered-projection digest field here at all.
type Vote struct {
	RequirementID string

	ProposalRevisionID string
	ProposalDigest     digest.Reference
	ControlSnapshots   intent.ControlSnapshots
	TaskVersion        uint64

	Approver             ApproverReference
	Outcome              Outcome
	Reason               string
	AuthorityDecisionRef string
}

// canonical returns the deterministic encoding used for replay detection.
func (v Vote) canonical() []byte {
	c := v.ControlSnapshots
	raw, err := canonicalbytes.New(voteSchema, 1).
		String("requirement_id", v.RequirementID).
		String("proposal_revision_id", v.ProposalRevisionID).
		String("proposal_digest", v.ProposalDigest.Digest).
		String("proposal_digest.profile_id", v.ProposalDigest.ProfileID).
		String("proposal_digest.scope_binding", v.ProposalDigest.ScopeBindingDigest).
		Int("task_version", int64(v.TaskVersion)).
		String("controls.capability_registry", c.CapabilityRegistryDigest).
		String("controls.policy_bundle", c.PolicyBundleDigest).
		String("controls.legal_context", c.LegalContextDigest).
		String("controls.entitlement", c.EntitlementDigest).
		String("controls.reference_data", c.ReferenceDataDigest).
		String("controls.classification_taxonomy", c.ClassificationTaxonomyDigest).
		String("controls.dlp_decision", c.DLPDecisionDigest).
		String("approver.principal_id", v.Approver.PrincipalID).
		String("approver.identity_assurance_ref", v.Approver.IdentityAssuranceRef).
		String("approver.session_ref", v.Approver.SessionRef).
		String("approver.via", string(v.Approver.Via)).
		String("approver.delegation_id", v.Approver.DelegationID).
		String("outcome", string(v.Outcome)).
		String("reason", v.Reason).
		String("authority_decision_ref", v.AuthorityDecisionRef).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Binder records decisions against exactly one proposal revision.
//
// It holds the revision, the compiled requirement set, the current resolution
// per requirement and the current rendered projection per requirement. Every
// one of those is server state; the binder's whole job is to refuse anything
// that disagrees with it and to bind its own copy when nothing does.
type Binder struct {
	mu sync.Mutex

	revision    intent.ProposalRevision
	set         humanwork.RequirementSet
	resolutions map[string]humanwork.Resolution
	projections map[string]Projection

	clock intent.Clock
	ids   intent.IDSource

	byVote     map[string]string
	byApprover map[string]string
	decisions  map[string]ApprovalDecision
	order      []string
}

// NewBinder builds a binder for one proposal revision.
//
// Every requirement in the set must have both a resolution that RESOLVED and a
// rendered projection: a requirement nobody can decide, or one nothing was
// rendered for, cannot accept a bound decision, and finding that out at
// construction is better than finding it out from a vote.
func NewBinder(
	revision intent.ProposalRevision,
	set humanwork.RequirementSet,
	resolutions []humanwork.Resolution,
	projections []Projection,
	clock intent.Clock,
	ids intent.IDSource,
) (*Binder, error) {
	if revision.MaterialDigest.Digest == "" {
		return nil, newError("NewBinder", "material_digest", CodeInvalidBinder, ErrInvalidBinder,
			"revision %q carries no minted material digest", revision.ProposalRevisionID)
	}
	if revision.IntentID == "" || revision.ProposalRevisionID == "" {
		return nil, newError("NewBinder", "proposal_revision_id", CodeInvalidBinder, ErrInvalidBinder,
			"revision names no intent or revision id")
	}
	if clock == nil {
		return nil, newError("NewBinder", "clock", CodeInvalidBinder, ErrInvalidBinder,
			"no clock supplied")
	}
	if ids == nil {
		ids = intent.UUIDv7Source
	}
	if len(set.Requirements) == 0 {
		return nil, newError("NewBinder", "requirements", CodeInvalidBinder, ErrInvalidBinder,
			"requirement set is empty")
	}

	b := &Binder{
		revision:    revision,
		set:         set,
		resolutions: make(map[string]humanwork.Resolution, len(resolutions)),
		projections: make(map[string]Projection, len(projections)),
		clock:       clock,
		ids:         ids,
		byVote:      map[string]string{},
		byApprover:  map[string]string{},
		decisions:   map[string]ApprovalDecision{},
	}
	for _, r := range resolutions {
		if _, ok := set.Find(r.RequirementID); !ok {
			return nil, newError("NewBinder", "resolutions", CodeInvalidBinder, ErrInvalidBinder,
				"resolution names requirement %q, which the set does not contain", r.RequirementID)
		}
		b.resolutions[r.RequirementID] = r
	}
	for _, p := range projections {
		if _, ok := set.Find(p.RequirementID); !ok {
			return nil, newError("NewBinder", "projections", CodeInvalidBinder, ErrInvalidBinder,
				"projection names requirement %q, which the set does not contain", p.RequirementID)
		}
		if p.Digest == "" || p.TaskVersion == 0 {
			return nil, newError("NewBinder", "projections", CodeInvalidBinder, ErrInvalidBinder,
				"projection for %q has no digest or task version", p.RequirementID)
		}
		if p.Safety != humanwork.SafetySafeToDecide {
			return nil, newError("NewBinder", "projections.safety", CodeUnsafeProjection, ErrUnsafeProjection,
				"projection for %q is %s", p.RequirementID, p.Safety)
		}
		b.projections[p.RequirementID] = p
	}
	for _, req := range set.Requirements {
		res, ok := b.resolutions[req.RequirementID]
		if !ok {
			return nil, newError("NewBinder", "resolutions", CodeInvalidBinder, ErrInvalidBinder,
				"requirement %q has no resolution", req.RequirementID)
		}
		if res.Outcome != humanwork.OutcomeResolved {
			return nil, newError("NewBinder", "resolutions", CodeInvalidBinder, ErrInvalidBinder,
				"requirement %q resolved %s", req.RequirementID, res.Outcome)
		}
		if _, ok := b.projections[req.RequirementID]; !ok {
			return nil, newError("NewBinder", "projections", CodeInvalidBinder, ErrInvalidBinder,
				"requirement %q has no server-held rendered projection", req.RequirementID)
		}
	}
	return b, nil
}

// Revision returns the bound proposal revision.
func (b *Binder) Revision() intent.ProposalRevision { return b.revision }

// Record validates a vote against the bound context and records the decision.
//
// A resubmission of the identical vote replays the decision it already
// produced, same identifier and all. A different decision from a principal who
// has already decided is a conflict: prior votes are never mutated, and a
// changed mind is a new decision on a new revision.
func (b *Binder) Record(v Vote) (ApprovalDecision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	req, ok := b.set.Find(v.RequirementID)
	if !ok {
		return ApprovalDecision{}, newError("Record", "requirement_id",
			CodeUnknownRequirement, ErrUnknownRequirement,
			"requirement %q is not in the bound set", v.RequirementID)
	}
	if !v.Outcome.Valid() {
		return ApprovalDecision{}, newError("Record", "outcome",
			CodeInvalidProposal, ErrInvalidProposal, "outcome %q is not a decision", string(v.Outcome))
	}
	if v.Reason == "" {
		return ApprovalDecision{}, newError("Record", "reason",
			CodeInvalidProposal, ErrInvalidProposal, "a decision carries a reason")
	}
	if v.AuthorityDecisionRef == "" {
		return ApprovalDecision{}, newError("Record", "authority_decision_ref",
			CodeInvalidProposal, ErrInvalidProposal, "a decision cites the authorization that permitted it")
	}
	if v.Approver.PrincipalID == "" || v.Approver.IdentityAssuranceRef == "" {
		return ApprovalDecision{}, newError("Record", "approver",
			CodeInvalidProposal, ErrInvalidProposal, "the approver is unidentified or unassured")
	}
	if err := b.checkProposal(v); err != nil {
		return ApprovalDecision{}, err
	}
	projection := b.projections[v.RequirementID]
	if v.TaskVersion != projection.TaskVersion {
		return ApprovalDecision{}, newError("Record", "task_version",
			CodeInvalidProposal, ErrInvalidProposal,
			"vote references task version %d, the server rendered %d",
			v.TaskVersion, projection.TaskVersion)
	}
	res := b.resolutions[v.RequirementID]
	candidate, ok := res.Authorizes(v.Approver.PrincipalID)
	if !ok {
		return ApprovalDecision{}, newError("Record", "approver.principal_id",
			CodeAuthorityChanged, ErrAuthorityChanged,
			"%q is not in the current candidate set for %q",
			v.Approver.PrincipalID, v.RequirementID)
	}
	if v.Approver.Via != candidate.Via || v.Approver.DelegationID != candidate.DelegationID {
		return ApprovalDecision{}, newError("Record", "approver.via",
			CodeAuthorityChanged, ErrAuthorityChanged,
			"%q claims route %s/%q but holds %s/%q",
			v.Approver.PrincipalID, v.Approver.Via, v.Approver.DelegationID,
			candidate.Via, candidate.DelegationID)
	}

	raw := v.canonical()
	if raw == nil {
		return ApprovalDecision{}, newError("Record", "vote",
			CodeInvalidProposal, ErrInvalidProposal, "vote could not be canonicalized")
	}
	voteDigest := canonicalbytes.Digest(raw)
	if id, ok := b.byVote[voteDigest]; ok {
		return b.decisions[id], nil
	}
	slot := v.RequirementID + "\x00" + v.Approver.PrincipalID
	if id, ok := b.byApprover[slot]; ok {
		return ApprovalDecision{}, newError("Record", "approver.principal_id",
			CodeConflictingDecision, ErrConflictingDecision,
			"%q already recorded decision %q on %q",
			v.Approver.PrincipalID, id, v.RequirementID)
	}

	id, err := b.ids()
	if err != nil {
		return ApprovalDecision{}, newError("Record", "decision_id",
			CodeInvalidProposal, ErrInvalidProposal, "%v", err)
	}
	now := b.clock()
	if !now.IsSet() {
		return ApprovalDecision{}, newError("Record", "decided_at",
			CodeInvalidProposal, ErrInvalidProposal, "clock returned an unset instant")
	}

	decision := ApprovalDecision{
		DecisionID: id,
		Binding: DecisionBinding{
			RequirementID:       req.RequirementID,
			RequirementRevision: req.Revision,
			IntentID:            b.revision.IntentID,
			ProposalRevisionID:  b.revision.ProposalRevisionID,
			// The server's own digest, resolution and projection - never the
			// voter's assertions, which have already been checked and are now
			// discarded.
			ProposalDigest:             b.revision.MaterialDigest,
			TaskVersion:                projection.TaskVersion,
			RenderedProjectionDigest:   projection.Digest,
			ControlSnapshots:           b.revision.ControlSnapshots,
			RequirementDigest:          res.RequirementDigest,
			ResolutionExpressionDigest: res.ExpressionDigest,
		},
		Outcome:              v.Outcome,
		Approver:             v.Approver,
		AuthorityDecisionRef: v.AuthorityDecisionRef,
		Reason:               v.Reason,
		DecidedAt:            now,
		VoteDigest:           voteDigest,
	}
	b.byVote[voteDigest] = id
	b.byApprover[slot] = id
	b.decisions[id] = decision
	b.order = append(b.order, id)
	return decision, nil
}

// checkProposal refuses a vote whose asserted proposal, digest or control
// context is not the bound one.
func (b *Binder) checkProposal(v Vote) error {
	if v.ProposalRevisionID != b.revision.ProposalRevisionID {
		return newError("Record", "proposal_revision_id",
			CodeInvalidProposal, ErrInvalidProposal,
			"vote names revision %q, the binder holds %q",
			v.ProposalRevisionID, b.revision.ProposalRevisionID)
	}
	want := b.revision.MaterialDigest
	got := v.ProposalDigest
	switch {
	case got.Digest != want.Digest:
		return newError("Record", "proposal_digest",
			CodeInvalidProposal, ErrInvalidProposal,
			"vote names digest %q, the revision's is %q", got.Digest, want.Digest)
	case got.ProfileID != want.ProfileID || got.ProfileVersion != want.ProfileVersion:
		return newError("Record", "proposal_digest.profile",
			CodeInvalidProposal, ErrInvalidProposal,
			"vote names profile %s, the revision's is %s", got.Key(), want.Key())
	case got.SchemaID != want.SchemaID || got.SchemaVersion != want.SchemaVersion:
		return newError("Record", "proposal_digest.schema",
			CodeInvalidProposal, ErrInvalidProposal,
			"vote names schema %s@%d, the revision's is %s@%d",
			got.SchemaID, got.SchemaVersion, want.SchemaID, want.SchemaVersion)
	case got.AlgorithmID != want.AlgorithmID:
		return newError("Record", "proposal_digest.algorithm",
			CodeInvalidProposal, ErrInvalidProposal,
			"vote names algorithm %q, the revision's is %q", got.AlgorithmID, want.AlgorithmID)
	case got.ScopeBindingDigest != want.ScopeBindingDigest:
		// Without this the same hash could be replayed against a different
		// tenant, intent or proposal.
		return newError("Record", "proposal_digest.scope_binding_digest",
			CodeInvalidProposal, ErrInvalidProposal,
			"vote names scope binding %q, the revision's is %q",
			got.ScopeBindingDigest, want.ScopeBindingDigest)
	}
	if v.ControlSnapshots != b.revision.ControlSnapshots {
		return newError("Record", "control_snapshots",
			CodeInvalidProposal, ErrInvalidProposal,
			"vote was cast against a different control context")
	}
	return nil
}

// Decisions returns the recorded decisions in the order they were recorded.
func (b *Binder) Decisions() []ApprovalDecision {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ApprovalDecision, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, b.decisions[id])
	}
	return out
}

// DecisionsFor returns the recorded decisions for one requirement, sorted by
// decision id.
func (b *Binder) DecisionsFor(requirementID string) []ApprovalDecision {
	var out []ApprovalDecision
	for _, d := range b.Decisions() {
		if d.Binding.RequirementID == requirementID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DecisionID < out[j].DecisionID })
	return out
}
