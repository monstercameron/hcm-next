package intent

import (
	"slices"

	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// StateAssertion is one field's value before or after the proposed change. The
// value is carried as its canonical text so that a proposal never depends on a
// domain type the kernel does not own.
type StateAssertion struct {
	Subject       SubjectReference
	ResourceKey   values.ResourceKey
	FieldPath     string
	CanonicalText string
}

// PlannedWrite is one typed planned domain write. It is material: changing a
// field, a value, an expected baseline or the authority decision behind it
// creates a new revision and invalidates approvals bound to the old one.
type PlannedWrite struct {
	Subject     SubjectReference
	ResourceKey values.ResourceKey
	FieldPath   string

	CurrentCanonicalText  string
	ProposedCanonicalText string

	// SourceAuthorityDecision is the per-field authority decision. A write with
	// no authority decision is not a proposal, it is a guess.
	SourceAuthorityDecision string

	// ExpectedRevision is the baseline the write assumes.
	ExpectedRevision values.RevisionToken
}

// PlannedEffect is one declared external or internal effect.
type PlannedEffect struct {
	EffectID        string
	Kind            string
	DestinationRef  string
	Reversibility   string
	CompensationRef string
	ObservationRef  string
}

// ChildIntentBinding binds one child intent into a composite proposal. The
// binding is explicit: a parent never silently aliases a child, and parent
// completion cannot overwrite child truth.
type ChildIntentBinding struct {
	Definition          Ref
	ChildIntentID       string
	Ordinal             uint32
	MaterialInputDigest string
}

// Reservation is a scarce-resource hold the proposal depends on.
type Reservation struct {
	ReservationID string
	Kind          string
	Expiry        values.Instant
}

// RequiredApproval is one approval requirement the proposal declares.
type RequiredApproval struct {
	RequirementID        string
	SeparationConstraint string
}

// Obligation is a duty the proposal attaches.
type Obligation struct {
	ObligationID string
	Kind         string
}

// CompensationDeclaration says how an effect is undone or repaired.
type CompensationDeclaration struct {
	EffectID     string
	Strategy     string
	RepairPlanID string
}

// SourceBaseline pins the stream sequence a planned append expects.
type SourceBaseline struct {
	StreamID         string
	ExpectedRevision values.RevisionToken
}

// AttachmentRef is an immutable artifact reference. Content identity is the
// digest; a mutable URL is never content identity.
type AttachmentRef struct {
	ArtifactID  string
	AlgorithmID string
	Digest      string
}

// PurposeDecision records the approved purpose, destination and residency
// decision. Reclassification that changes any of these changes the material
// list and therefore invalidates approval.
type PurposeDecision struct {
	Purpose        string
	RecipientRef   string
	DestinationRef string
	ResidencyRef   string
}

// RevalidationPlan names the rules that re-decide the proposal at execution
// time. It is material: dropping a revalidation rule changes what will be
// checked before anything runs.
type RevalidationPlan struct {
	Rules []string
}

// ProposalRevision is an immutable proposal artifact. Approval binds its
// material digest, which is computed by the PROPOSAL canonicalization profile
// over the material content only.
//
// ControlSnapshots are deliberately outside the material digest: they are
// evidence of the context the proposal was simulated in. A control-snapshot
// change triggers revalidation and invalidates approval only when the material
// result changes or a mandatory deny appears. That is what keeps a tenant-wide
// policy republish from invalidating a thousand pending approvals.
type ProposalRevision struct {
	ProposalRevisionID string
	IntentID           string
	Revision           uint64

	Tenant              values.TenantId
	OrganizationScopeID string
	LegalEntityID       string
	Subjects            []SubjectReference

	EffectiveTime values.EffectiveInterval

	CurrentState  []StateAssertion
	ProposedState []StateAssertion

	Writes            []PlannedWrite
	Effects           []PlannedEffect
	Children          []ChildIntentBinding
	Reservations      []Reservation
	RequiredApprovals []RequiredApproval
	Obligations       []Obligation
	Compensations     []CompensationDeclaration
	SourceBaselines   []SourceBaseline
	Attachments       []AttachmentRef

	Purpose      PurposeDecision
	Cost         *values.Money
	Revalidation RevalidationPlan

	// SupersedesRevisionID links the revision this one replaces.
	SupersedesRevisionID *string

	// ControlSnapshots is revalidated context, not material.
	ControlSnapshots ControlSnapshots

	// CreatedBy and CreatedAt are provenance evidence, not material.
	CreatedBy PrincipalReference
	CreatedAt values.Instant

	// InvalidatorRefs record why earlier approvals were invalidated. Lifecycle
	// bookkeeping, not material.
	InvalidatorRefs []string

	// MaterialDigest is minted by the kernel. A caller may never supply it.
	MaterialDigest digest.Reference
}

// ProposalSpec is the creation request for a revision. It carries no digest
// field at all, which is how a caller-provided digest is made unrepresentable
// rather than merely rejected.
type ProposalSpec struct {
	IntentID string
	Revision uint64

	Tenant              values.TenantId
	OrganizationScopeID string
	LegalEntityID       string
	Subjects            []SubjectReference

	EffectiveTime values.EffectiveInterval

	CurrentState  []StateAssertion
	ProposedState []StateAssertion

	Writes            []PlannedWrite
	Effects           []PlannedEffect
	Children          []ChildIntentBinding
	Reservations      []Reservation
	RequiredApprovals []RequiredApproval
	Obligations       []Obligation
	Compensations     []CompensationDeclaration
	SourceBaselines   []SourceBaseline
	Attachments       []AttachmentRef

	Purpose      PurposeDecision
	Cost         *values.Money
	Revalidation RevalidationPlan

	SupersedesRevisionID *string
	ControlSnapshots     ControlSnapshots
	CreatedBy            PrincipalReference
	InvalidatorRefs      []string

	// DetectedChildRefs are the material child intents the simulation actually
	// observed in the proposal content. Any one not bound in Children is a
	// hidden child intent and the revision is rejected.
	DetectedChildRefs []Ref
}

// NewProposalRevision mints an immutable revision and its material digest.
func NewProposalRevision(spec ProposalSpec, def Definition, d Digester, ids IDSource, clock Clock) (ProposalRevision, error) {
	if d == nil {
		return ProposalRevision{}, newError("NewProposalRevision", "", ErrInvalidProposal,
			"no digester supplied")
	}
	if ids == nil {
		ids = UUIDv7Source
	}
	if clock == nil {
		return ProposalRevision{}, newError("NewProposalRevision", "", ErrInvalidProposal,
			"no clock supplied")
	}
	if err := validateProposalSpec(spec, def); err != nil {
		return ProposalRevision{}, err
	}
	id, err := ids()
	if err != nil {
		return ProposalRevision{}, err
	}
	now := clock()
	if !now.IsSet() {
		return ProposalRevision{}, newError("NewProposalRevision", "created_at", ErrInvalidProposal,
			"clock returned an unset instant")
	}
	rev := ProposalRevision{
		ProposalRevisionID:   id,
		IntentID:             spec.IntentID,
		Revision:             spec.Revision,
		Tenant:               spec.Tenant,
		OrganizationScopeID:  spec.OrganizationScopeID,
		LegalEntityID:        spec.LegalEntityID,
		Subjects:             slices.Clone(spec.Subjects),
		EffectiveTime:        spec.EffectiveTime,
		CurrentState:         slices.Clone(spec.CurrentState),
		ProposedState:        slices.Clone(spec.ProposedState),
		Writes:               slices.Clone(spec.Writes),
		Effects:              slices.Clone(spec.Effects),
		Children:             slices.Clone(spec.Children),
		Reservations:         slices.Clone(spec.Reservations),
		RequiredApprovals:    slices.Clone(spec.RequiredApprovals),
		Obligations:          slices.Clone(spec.Obligations),
		Compensations:        slices.Clone(spec.Compensations),
		SourceBaselines:      slices.Clone(spec.SourceBaselines),
		Attachments:          slices.Clone(spec.Attachments),
		Purpose:              spec.Purpose,
		Cost:                 spec.Cost,
		Revalidation:         RevalidationPlan{Rules: slices.Clone(spec.Revalidation.Rules)},
		SupersedesRevisionID: cloneStringPtr(spec.SupersedesRevisionID),
		ControlSnapshots:     spec.ControlSnapshots,
		CreatedBy:            spec.CreatedBy,
		CreatedAt:            now,
		InvalidatorRefs:      slices.Clone(spec.InvalidatorRefs),
	}
	ref, err := d.ProposalDigest(rev)
	if err != nil {
		return ProposalRevision{}, err
	}
	rev.MaterialDigest = ref
	return rev, nil
}

func validateProposalSpec(spec ProposalSpec, def Definition) error {
	if spec.IntentID == "" {
		return newError("Validate", "intent_id", ErrInvalidProposal, "revision names no intent")
	}
	if spec.Revision == 0 {
		return newError("Validate", "revision", ErrInvalidProposal,
			"revisions are numbered from 1")
	}
	if err := spec.Tenant.Validate(); err != nil {
		return newError("Validate", "tenant_id", ErrInvalidProposal, "%v", err)
	}
	if spec.OrganizationScopeID == "" {
		return newError("Validate", "organization_scope_id", ErrInvalidProposal,
			"revision names no organization scope")
	}
	if len(spec.Subjects) == 0 {
		return newError("Validate", "subjects", ErrInvalidProposal, "revision names no subject")
	}
	for _, s := range spec.Subjects {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if err := spec.EffectiveTime.Validate(); err != nil {
		return newError("Validate", "effective_time", ErrInvalidProposal, "%v", err)
	}
	if err := spec.CreatedBy.Validate(); err != nil {
		return err
	}
	// Control, source and reference versions are what make a revision
	// reproducible; a revision without them cannot be revalidated later.
	if err := spec.ControlSnapshots.Validate(); err != nil {
		return newError("Validate", "control_snapshots", ErrInvalidProposal, "%v", err)
	}
	for _, w := range spec.Writes {
		if w.FieldPath == "" {
			return newError("Validate", "writes.field_path", ErrInvalidProposal,
				"planned write names no field")
		}
		if w.SourceAuthorityDecision == "" {
			return newError("Validate", "writes.source_authority_decision", ErrInvalidProposal,
				"planned write on %q records no source-authority decision", w.FieldPath)
		}
		if !w.ExpectedRevision.IsSpecified() {
			return newError("Validate", "writes.expected_revision", ErrInvalidProposal,
				"planned write on %q pins no baseline revision", w.FieldPath)
		}
	}
	for _, e := range spec.Effects {
		if e.EffectID == "" {
			return newError("Validate", "effects.effect_id", ErrInvalidProposal,
				"declared effect has no id")
		}
		if e.ObservationRef == "" {
			return newError("Validate", "effects.observation_ref", ErrInvalidProposal,
				"effect %q declares no observation", e.EffectID)
		}
		if !slices.ContainsFunc(spec.Compensations, func(c CompensationDeclaration) bool {
			return c.EffectID == e.EffectID
		}) {
			return newError("Validate", "compensations", ErrInvalidProposal,
				"effect %q declares no compensation or repair", e.EffectID)
		}
	}
	for _, r := range spec.Reservations {
		if !r.Expiry.IsSet() {
			return newError("Validate", "reservations.expiry", ErrInvalidProposal,
				"reservation %q never expires", r.ReservationID)
		}
	}
	bound := make(map[Ref]bool, len(spec.Children))
	for _, c := range spec.Children {
		if err := c.Definition.Validate(); err != nil {
			return err
		}
		if c.MaterialInputDigest == "" {
			return newError("Validate", "children.material_input_digest", ErrInvalidProposal,
				"child %s binds no material input digest", c.Definition)
		}
		bound[c.Definition] = true
	}
	for _, detected := range spec.DetectedChildRefs {
		if !bound[detected] {
			return newError("Validate", "children", ErrHiddenChildIntent,
				"%s materially creates %s but the revision does not bind it",
				def.Ref, detected)
		}
	}
	if def.Family != FamilyChangeRequest && len(spec.Writes) > 0 {
		return newError("Validate", "writes", ErrInvalidProposal,
			"%s is a %s and may not plan a domain write", def.Ref, def.Family)
	}
	if def.ZeroEffect() && len(spec.Effects) > 0 {
		return newError("Validate", "effects", ErrInvalidProposal,
			"%s is ZERO_EFFECT in its scheduled release and may not declare an effect", def.Ref)
	}
	return nil
}

// MaterialPayload returns the deterministic material encoding of the revision,
// as the typed payload the PROPOSAL profile digests. It excludes control
// snapshots, creator, creation time and invalidator references: those are
// revalidated context and provenance, not material.
func (p ProposalRevision) MaterialPayload() TypedPayload {
	e := newEnc(materialMagic)
	e.str(p.ProposalRevisionID)
	e.str(p.IntentID)
	e.uvarint(p.Revision)
	e.str(string(p.Tenant))
	e.str(p.OrganizationScopeID)
	e.str(p.LegalEntityID)
	encodeSet(e, p.Subjects, encodeSubject)
	e.canonical(p.EffectiveTime)
	encodeList(e, p.CurrentState, encodeAssertion)
	encodeList(e, p.ProposedState, encodeAssertion)
	encodeList(e, p.Writes, encodeWrite)
	encodeList(e, p.Effects, encodeEffect)
	encodeList(e, p.Children, encodeChild)
	encodeSet(e, p.Reservations, encodeReservation)
	encodeSet(e, p.RequiredApprovals, encodeRequiredApproval)
	encodeSet(e, p.Obligations, encodeObligation)
	encodeSet(e, p.Compensations, encodeCompensation)
	encodeSet(e, p.SourceBaselines, encodeBaseline)
	encodeSet(e, p.Attachments, encodeAttachment)
	e.str(p.Purpose.Purpose).
		str(p.Purpose.RecipientRef).
		str(p.Purpose.DestinationRef).
		str(p.Purpose.ResidencyRef)
	if p.Cost == nil {
		e.uvarint(0)
	} else {
		e.uvarint(1).canonical(*p.Cost)
	}
	encodeList(e, p.Revalidation.Rules, func(sub *enc, r string) { sub.str(r) })
	e.optionalStr(p.SupersedesRevisionID)
	return TypedPayload{Schema: MaterialSchema(), WireBytes: e.bytes()}
}

func encodeSubject(e *enc, s SubjectReference) {
	e.str(s.Kind).str(s.SubjectID).str(s.AuthorityDomain)
}

func encodeAssertion(e *enc, a StateAssertion) {
	encodeSubject(e, a.Subject)
	e.canonical(a.ResourceKey).str(a.FieldPath).str(a.CanonicalText)
}

func encodeWrite(e *enc, w PlannedWrite) {
	encodeSubject(e, w.Subject)
	e.canonical(w.ResourceKey).
		str(w.FieldPath).
		str(w.CurrentCanonicalText).
		str(w.ProposedCanonicalText).
		str(w.SourceAuthorityDecision).
		canonical(w.ExpectedRevision)
}

func encodeEffect(e *enc, x PlannedEffect) {
	e.str(x.EffectID).str(x.Kind).str(x.DestinationRef).
		str(x.Reversibility).str(x.CompensationRef).str(x.ObservationRef)
}

func encodeChild(e *enc, c ChildIntentBinding) {
	e.str(c.Definition.String()).str(c.ChildIntentID).
		uvarint(uint64(c.Ordinal)).str(c.MaterialInputDigest)
}

func encodeReservation(e *enc, r Reservation) {
	e.str(r.ReservationID).str(r.Kind).instant(r.Expiry)
}

func encodeRequiredApproval(e *enc, a RequiredApproval) {
	e.str(a.RequirementID).str(a.SeparationConstraint)
}

func encodeObligation(e *enc, o Obligation) { e.str(o.ObligationID).str(o.Kind) }

func encodeCompensation(e *enc, c CompensationDeclaration) {
	e.str(c.EffectID).str(c.Strategy).str(c.RepairPlanID)
}

func encodeBaseline(e *enc, b SourceBaseline) {
	e.str(b.StreamID).canonical(b.ExpectedRevision)
}

func encodeAttachment(e *enc, a AttachmentRef) {
	e.str(a.ArtifactID).str(a.AlgorithmID).str(a.Digest)
}

// MaterialEqual reports whether two revisions carry the same material content.
// It compares the material encoding rather than the minted digest so that it is
// usable before a digest exists, and it is the only definition of "materially
// unchanged" the kernel has.
func MaterialEqual(a, b ProposalRevision) bool {
	// Revision identity and lineage are part of the material list, so two
	// revisions of the same proposal are compared on everything else: every
	// successor names a different predecessor, and that link alone must not
	// make a revision material (INTENT-006 keeps approvals across immaterial
	// revisions).
	a.ProposalRevisionID, b.ProposalRevisionID = "", ""
	a.Revision, b.Revision = 0, 0
	a.SupersedesRevisionID, b.SupersedesRevisionID = nil, nil
	return string(a.MaterialPayload().WireBytes) == string(b.MaterialPayload().WireBytes)
}

// ProposalLedger is the append-only record of one intent's proposal revisions.
// A revision is superseded by a further append; it is never edited in place,
// and the ledger is what makes that structurally true rather than a convention.
type ProposalLedger struct {
	intentID  string
	revisions []ProposalRevision
}

// NewProposalLedger returns an empty ledger for one intent.
func NewProposalLedger(intentID string) *ProposalLedger {
	return &ProposalLedger{intentID: intentID}
}

// Len returns the number of recorded revisions.
func (l *ProposalLedger) Len() int { return len(l.revisions) }

// Revisions returns a copy of the recorded revisions.
func (l *ProposalLedger) Revisions() []ProposalRevision {
	return append([]ProposalRevision(nil), l.revisions...)
}

// Current returns the latest revision.
func (l *ProposalLedger) Current() (ProposalRevision, bool) {
	if len(l.revisions) == 0 {
		return ProposalRevision{}, false
	}
	return l.revisions[len(l.revisions)-1], true
}

// Append records the next revision. It rejects a revision belonging to a
// different intent, a non-sequential revision number, a revision carrying no
// minted digest, and any attempt to re-record an existing revision id — the
// three shapes an in-place edit would take.
func (l *ProposalLedger) Append(rev ProposalRevision) error {
	if rev.IntentID != l.intentID {
		return newError("Append", "intent_id", ErrProposalImmutable,
			"revision belongs to intent %q, ledger is for %q", rev.IntentID, l.intentID)
	}
	want := uint64(len(l.revisions)) + 1
	if rev.Revision != want {
		return newError("Append", "revision", ErrProposalImmutable,
			"revision %d recorded out of order; next is %d", rev.Revision, want)
	}
	if rev.MaterialDigest.Digest == "" {
		return newError("Append", "material_proposal_digest", ErrInvalidProposal,
			"revision %d carries no minted material digest", rev.Revision)
	}
	for _, existing := range l.revisions {
		if existing.ProposalRevisionID == rev.ProposalRevisionID {
			return newError("Append", "proposal_revision_id", ErrProposalImmutable,
				"revision %q is already recorded", rev.ProposalRevisionID)
		}
	}
	if prev, ok := l.Current(); ok {
		if rev.SupersedesRevisionID == nil || *rev.SupersedesRevisionID != prev.ProposalRevisionID {
			return newError("Append", "supersedes_proposal_revision_id", ErrProposalImmutable,
				"revision %d does not link the revision it supersedes", rev.Revision)
		}
	}
	l.revisions = append(l.revisions, rev)
	return nil
}
