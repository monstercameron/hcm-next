package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PlanParticipant is one stream or store the plan touches.
type PlanParticipant struct {
	ParticipantID string
	StreamID      string
	StorageClass  string

	// Local marks a participant inside the single local ACID commit boundary.
	// A non-local participant becomes a durable effect, never part of the
	// local transaction.
	Local bool
}

// PlannedRead is one baseline read the plan depends on.
type PlannedRead struct {
	ResourceKey      values.ResourceKey
	ExpectedRevision values.RevisionToken
}

// PlannedAppend is one domain event the plan would append.
type PlannedAppend struct {
	StreamID         string
	ExpectedSequence uint64
	EventType        string
	PayloadDigest    string
}

// ProjectionMutation is one critical projection the commit would apply inside
// the same local transaction.
type ProjectionMutation struct {
	ProjectionID string
	ResourceKey  values.ResourceKey
	Operation    string
}

// OutboxEffect is one external effect the plan would enqueue. External calls
// never happen inside the local transaction; they are outbox records executed
// afterwards.
type OutboxEffect struct {
	EffectID       string
	DestinationRef string
	IdempotencyKey string
	Reversibility  string
}

// CommitPrecondition is one condition prepare would re-check at commit time.
type CommitPrecondition struct {
	Kind string
	Ref  string
}

// ReservationBinding binds a scarce-resource hold into the plan.
type ReservationBinding struct {
	ReservationID string
	Expiry        values.Instant
}

// PostCommitObservation is one observation the plan declares for an effect, so
// that no effect can be emitted with nothing watching it.
type PostCommitObservation struct {
	EffectID       string
	ObservationRef string
	Deadline       values.Instant
}

// CompensationBinding is how one effect is compensated or repaired.
type CompensationBinding struct {
	EffectID     string
	Strategy     string
	RepairPlanID string
}

// GovernanceSnapshot is the governance decision set the plan compiles under. It
// is an input struct rather than an engine: the kernel records the decisions a
// governance engine produced, it does not make them.
type GovernanceSnapshot struct {
	SnapshotDigest string
	AuthZDecision  string
	LegalDecision  string
	PolicyDecision string
	RiskDecision   string

	// MandatoryDeny records a deny that no revalidation can clear. A plan
	// carrying one is compiled as BLOCKED rather than READY.
	MandatoryDeny bool
}

// Validate rejects a governance snapshot with an undecided dimension.
func (g GovernanceSnapshot) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"governance.snapshot_digest", g.SnapshotDigest},
		{"governance.authz_decision", g.AuthZDecision},
		{"governance.legal_decision", g.LegalDecision},
		{"governance.policy_decision", g.PolicyDecision},
		{"governance.risk_decision", g.RiskDecision},
	} {
		if req.value == "" {
			return newError("Validate", req.field, ErrInvalidPlan, "governance decision is absent")
		}
	}
	return nil
}

// ConflictSnapshot is the conflict fence the plan compiles under.
type ConflictSnapshot struct {
	SnapshotDigest string
	FenceToken     string
	FootprintRef   string
}

// Validate rejects a conflict snapshot with no fence.
func (c ConflictSnapshot) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"conflict.snapshot_digest", c.SnapshotDigest},
		{"conflict.fence_token", c.FenceToken},
		{"conflict.footprint_ref", c.FootprintRef},
	} {
		if req.value == "" {
			return newError("Validate", req.field, ErrInvalidPlan, "conflict fence is absent")
		}
	}
	return nil
}

// PlanStatus is the compiled plan's state. Gate A stops at
// GOVERNANCE_VALIDATED or BLOCKED; nothing here can reach COMMITTING.
type PlanStatus uint8

// PlanStatus values.
const (
	PlanUnspecified PlanStatus = iota
	PlanGovernanceValidated
	PlanBlocked
)

var planStatusNames = map[PlanStatus]string{
	PlanUnspecified:         "UNSPECIFIED",
	PlanGovernanceValidated: "GOVERNANCE_VALIDATED",
	PlanBlocked:             "BLOCKED",
}

func (s PlanStatus) String() string { return enumName(planStatusNames, s, "PlanStatus") }

// PlanInput is everything plan compilation consumes.
type PlanInput struct {
	Proposal   ProposalRevision
	Definition Definition
	Mode       Mode

	Governance GovernanceSnapshot
	Conflict   ConflictSnapshot

	Participants        []PlanParticipant
	Reads               []PlannedRead
	Appends             []PlannedAppend
	ProjectionMutations []ProjectionMutation
	Effects             []OutboxEffect
	Preconditions       []CommitPrecondition
	Reservations        []ReservationBinding
	Observations        []PostCommitObservation
	Compensations       []CompensationBinding

	// IdempotencyRecordRef names the idempotency record the commit would
	// finalize. Without it a retry cannot be distinguished from a second
	// request.
	IdempotencyRecordRef string

	// ApprovalRequirementIDs are the approvals the plan requires.
	ApprovalRequirementIDs []string

	// RevalidationRuleRefs are the checks prepare would rerun.
	RevalidationRuleRefs []string

	// ExpiresAt bounds how long the compiled plan may be considered current.
	ExpiresAt values.Instant
}

// TransactionPlan is an immutable, non-executable plan value.
//
// In Gate A a plan exposes exactly what would happen and nothing that makes it
// happen. There is no Execute, no Commit, no Prepare and no Apply on this type;
// the only thing resembling execution is [TransactionPlan.AuthorizeExecution],
// which always refuses with EXECUTION_PROHIBITED_GATE_A.
type TransactionPlan struct {
	PlanID             string
	IntentID           string
	ProposalRevisionID string

	// ProposalDigest is the exact material digest the plan was compiled from.
	ProposalDigest string

	Mode   Mode
	Status PlanStatus

	Tenant              values.TenantId
	OrganizationScopeID string
	Subjects            []SubjectReference
	EffectiveTime       values.EffectiveInterval

	Participants        []PlanParticipant
	Reads               []PlannedRead
	Appends             []PlannedAppend
	ProjectionMutations []ProjectionMutation
	Effects             []OutboxEffect
	Preconditions       []CommitPrecondition
	Reservations        []ReservationBinding
	Observations        []PostCommitObservation
	Compensations       []CompensationBinding

	IdempotencyRecordRef   string
	ApprovalRequirementIDs []string
	RevalidationRuleRefs   []string

	GovernanceSnapshotDigest string
	ConflictSnapshotDigest   string
	ConflictFenceToken       string

	ExpiresAt values.Instant

	// Digest derives from the plan's canonical semantic content, not from its
	// field layout or the order a compiler happened to append things in.
	Digest string
}

// Executable reports whether the plan may be executed. It is false for every
// plan this package can produce: P1A compiles plans and never runs them.
func (p TransactionPlan) Executable() bool { return false }

// AuthorizeExecution is the only execution-shaped entry point and it always
// refuses. It exists so that a caller reaching for execution gets the exact
// typed refusal EXECUTION_PROHIBITED_GATE_A rather than a missing method it
// might be tempted to add.
func (p TransactionPlan) AuthorizeExecution() error {
	return newError("AuthorizeExecution", "plan_id", ErrExecutionProhibitedGateA,
		"plan %s exposes effects for review only; Gate A grants no write authority", p.PlanID)
}

// CompilePlan compiles an immutable non-executable plan from a proposal
// revision and its governance and conflict context.
//
// Compilation fails when any required declaration is missing: a participant, a
// read baseline, a planned append for a planned write, an effect's outbox
// record, an expected sequence, the idempotency record, an approval
// requirement, a revalidation rule, a compensation for an effect, or the
// post-commit observation that watches it.
func CompilePlan(in PlanInput, ids IDSource) (TransactionPlan, error) {
	if ids == nil {
		ids = UUIDv7Source
	}
	if err := validatePlanInput(in); err != nil {
		return TransactionPlan{}, err
	}
	id, err := ids()
	if err != nil {
		return TransactionPlan{}, err
	}
	status := PlanGovernanceValidated
	if in.Governance.MandatoryDeny {
		status = PlanBlocked
	}
	plan := TransactionPlan{
		PlanID:                   id,
		IntentID:                 in.Proposal.IntentID,
		ProposalRevisionID:       in.Proposal.ProposalRevisionID,
		ProposalDigest:           in.Proposal.MaterialDigest.Digest,
		Mode:                     in.Mode,
		Status:                   status,
		Tenant:                   in.Proposal.Tenant,
		OrganizationScopeID:      in.Proposal.OrganizationScopeID,
		Subjects:                 slices.Clone(in.Proposal.Subjects),
		EffectiveTime:            in.Proposal.EffectiveTime,
		Participants:             slices.Clone(in.Participants),
		Reads:                    slices.Clone(in.Reads),
		Appends:                  slices.Clone(in.Appends),
		ProjectionMutations:      slices.Clone(in.ProjectionMutations),
		Effects:                  slices.Clone(in.Effects),
		Preconditions:            slices.Clone(in.Preconditions),
		Reservations:             slices.Clone(in.Reservations),
		Observations:             slices.Clone(in.Observations),
		Compensations:            slices.Clone(in.Compensations),
		IdempotencyRecordRef:     in.IdempotencyRecordRef,
		ApprovalRequirementIDs:   slices.Clone(in.ApprovalRequirementIDs),
		RevalidationRuleRefs:     slices.Clone(in.RevalidationRuleRefs),
		GovernanceSnapshotDigest: in.Governance.SnapshotDigest,
		ConflictSnapshotDigest:   in.Conflict.SnapshotDigest,
		ConflictFenceToken:       in.Conflict.FenceToken,
		ExpiresAt:                in.ExpiresAt,
	}
	plan.Digest = plan.computeDigest()
	return plan, nil
}

func validatePlanInput(in PlanInput) error {
	rev := in.Proposal
	if rev.ProposalRevisionID == "" || rev.IntentID == "" {
		return newError("CompilePlan", "proposal", ErrInvalidPlan,
			"plan compiles from an identified proposal revision")
	}
	if rev.MaterialDigest.Digest == "" {
		return newError("CompilePlan", "proposal.material_proposal_digest", ErrInvalidPlan,
			"proposal revision carries no minted material digest")
	}
	if !in.Mode.Valid() {
		return newError("CompilePlan", "mode", ErrModeNotAllowed, "plan declares no execution mode")
	}
	if !in.Definition.AllowsMode(in.Mode) {
		return newError("CompilePlan", "mode", ErrModeNotAllowed,
			"%s does not allow %s", in.Definition.Ref, in.Mode)
	}
	if err := in.Governance.Validate(); err != nil {
		return err
	}
	if err := in.Conflict.Validate(); err != nil {
		return err
	}
	if len(in.Participants) == 0 {
		return newError("CompilePlan", "participants", ErrInvalidPlan,
			"plan declares no participant")
	}
	for _, p := range in.Participants {
		if p.ParticipantID == "" || p.StreamID == "" || p.StorageClass == "" {
			return newError("CompilePlan", "participants", ErrInvalidPlan,
				"participant %q declares no stream or storage class", p.ParticipantID)
		}
	}
	if len(in.Reads) == 0 {
		return newError("CompilePlan", "reads", ErrInvalidPlan,
			"plan declares no read baseline")
	}
	for _, r := range in.Reads {
		if err := r.ResourceKey.Validate(); err != nil {
			return newError("CompilePlan", "reads.resource_key", ErrInvalidPlan, "%v", err)
		}
		if !r.ExpectedRevision.IsSpecified() {
			return newError("CompilePlan", "reads.expected_revision", ErrInvalidPlan,
				"read of %s pins no expected revision", r.ResourceKey)
		}
	}
	streams := map[string]bool{}
	for _, p := range in.Participants {
		streams[p.StreamID] = true
	}
	if len(rev.Writes) > 0 && len(in.Appends) == 0 {
		return newError("CompilePlan", "appends", ErrInvalidPlan,
			"proposal plans %d write(s) but the plan declares no append", len(rev.Writes))
	}
	for _, a := range in.Appends {
		if a.StreamID == "" || a.EventType == "" {
			return newError("CompilePlan", "appends", ErrInvalidPlan,
				"planned append declares no stream or event type")
		}
		if !streams[a.StreamID] {
			return newError("CompilePlan", "appends.stream_id", ErrInvalidPlan,
				"planned append targets stream %q, which is not a declared participant", a.StreamID)
		}
		if a.ExpectedSequence == 0 {
			return newError("CompilePlan", "appends.expected_sequence", ErrInvalidPlan,
				"planned append to %q declares no expected sequence", a.StreamID)
		}
		if a.PayloadDigest == "" {
			return newError("CompilePlan", "appends.payload_digest", ErrInvalidPlan,
				"planned append to %q carries no payload digest", a.StreamID)
		}
	}
	if in.IdempotencyRecordRef == "" {
		return newError("CompilePlan", "idempotency_record_ref", ErrInvalidPlan,
			"plan declares no idempotency record")
	}
	if len(rev.RequiredApprovals) > 0 && len(in.ApprovalRequirementIDs) == 0 {
		return newError("CompilePlan", "approval_requirement_ids", ErrInvalidPlan,
			"proposal requires approval but the plan binds no approval requirement")
	}
	if len(in.RevalidationRuleRefs) == 0 {
		return newError("CompilePlan", "revalidation_rule_refs", ErrInvalidPlan,
			"plan declares no execution-time revalidation")
	}
	// Every declared effect needs a compensation and an observation, and every
	// non-local participant must be an effect rather than a local append.
	for _, e := range in.Effects {
		if e.EffectID == "" || e.DestinationRef == "" {
			return newError("CompilePlan", "effects", ErrInvalidPlan,
				"outbox effect declares no id or destination")
		}
		if e.IdempotencyKey == "" {
			return newError("CompilePlan", "effects.idempotency_key", ErrInvalidPlan,
				"effect %q carries no idempotency key", e.EffectID)
		}
		if !slices.ContainsFunc(in.Compensations, func(c CompensationBinding) bool {
			return c.EffectID == e.EffectID
		}) {
			return newError("CompilePlan", "compensations", ErrInvalidPlan,
				"effect %q declares no compensation", e.EffectID)
		}
		if !slices.ContainsFunc(in.Observations, func(o PostCommitObservation) bool {
			return o.EffectID == e.EffectID
		}) {
			return newError("CompilePlan", "observations", ErrInvalidPlan,
				"effect %q declares no post-commit observation", e.EffectID)
		}
	}
	if in.Definition.ZeroEffect() && len(in.Effects) > 0 {
		return newError("CompilePlan", "effects", ErrInvalidPlan,
			"%s is ZERO_EFFECT in its scheduled release and may not plan an effect",
			in.Definition.Ref)
	}
	if !in.ExpiresAt.IsSet() {
		return newError("CompilePlan", "expires_at", ErrInvalidPlan, "plan never expires")
	}
	return nil
}

// CanonicalBytes returns the plan's canonical semantic encoding, using the same
// length-framed rules as the proposal material payload so that neither field
// order nor map iteration can change the answer. The plan's own digest is
// excluded: a digest never hashes itself.
func (p TransactionPlan) CanonicalBytes() []byte {
	e := newEnc(planMagic)
	e.str(planSchemaID).uvarint(planSchemaVersionValue)
	e.str(p.IntentID).str(p.ProposalRevisionID).str(p.ProposalDigest)
	e.uvarint(uint64(p.Mode)).uvarint(uint64(p.Status))
	e.str(string(p.Tenant)).str(p.OrganizationScopeID)
	encodeSet(e, p.Subjects, encodeSubject)
	e.canonical(p.EffectiveTime)
	encodeSet(e, p.Participants, func(sub *enc, v PlanParticipant) {
		sub.str(v.ParticipantID).str(v.StreamID).str(v.StorageClass).boolean(v.Local)
	})
	encodeSet(e, p.Reads, func(sub *enc, v PlannedRead) {
		sub.canonical(v.ResourceKey).canonical(v.ExpectedRevision)
	})
	encodeList(e, p.Appends, func(sub *enc, v PlannedAppend) {
		sub.str(v.StreamID).uvarint(v.ExpectedSequence).str(v.EventType).str(v.PayloadDigest)
	})
	encodeSet(e, p.ProjectionMutations, func(sub *enc, v ProjectionMutation) {
		sub.str(v.ProjectionID).canonical(v.ResourceKey).str(v.Operation)
	})
	encodeList(e, p.Effects, func(sub *enc, v OutboxEffect) {
		sub.str(v.EffectID).str(v.DestinationRef).str(v.IdempotencyKey).str(v.Reversibility)
	})
	encodeSet(e, p.Preconditions, func(sub *enc, v CommitPrecondition) {
		sub.str(v.Kind).str(v.Ref)
	})
	encodeSet(e, p.Reservations, func(sub *enc, v ReservationBinding) {
		sub.str(v.ReservationID).instant(v.Expiry)
	})
	encodeSet(e, p.Observations, func(sub *enc, v PostCommitObservation) {
		sub.str(v.EffectID).str(v.ObservationRef).instant(v.Deadline)
	})
	encodeSet(e, p.Compensations, func(sub *enc, v CompensationBinding) {
		sub.str(v.EffectID).str(v.Strategy).str(v.RepairPlanID)
	})
	e.str(p.IdempotencyRecordRef)
	encodeSet(e, p.ApprovalRequirementIDs, func(sub *enc, v string) { sub.str(v) })
	encodeSet(e, p.RevalidationRuleRefs, func(sub *enc, v string) { sub.str(v) })
	e.str(p.GovernanceSnapshotDigest).str(p.ConflictSnapshotDigest).str(p.ConflictFenceToken)
	e.instant(p.ExpiresAt)
	return e.bytes()
}

// computeDigest hashes the canonical semantic content.
func (p TransactionPlan) computeDigest() string {
	sum := sha256.Sum256(p.CanonicalBytes())
	return hex.EncodeToString(sum[:])
}

// VerifyDigest recomputes the plan digest from its canonical content and
// reports whether the recorded digest matches. A plan is never trusted on the
// word of the digest it carries.
func (p TransactionPlan) VerifyDigest() error {
	if got := p.computeDigest(); got != p.Digest {
		return newError("VerifyDigest", "digest", ErrInvalidPlan,
			"plan %s records digest %q but its content hashes to %q", p.PlanID, p.Digest, got)
	}
	return nil
}
