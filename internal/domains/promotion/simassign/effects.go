package simassign

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	enginesnapshot "github.com/monstercameron/hcm-next/internal/engines/snapshot"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// schemaVersion pins the canonical encoding this package digests under. It is
// bumped only when the encoding changes, never when a field's value changes.
const schemaVersion = 1

// Reversibility is the closed vocabulary an effect's undo class is drawn from.
//
// It is the same three tokens the intent control plane persists
// (internal/data/intentcontrol), restated here as a typed value rather than
// imported, because a domain simulation must not depend on a storage adapter.
// [ReversibilityClasses] is the test hook that keeps the two lists identical.
type Reversibility string

// Reversibility classes.
const (
	// Reversible means the effect can be undone by writing a further
	// effective-dated revision in the same authoritative store.
	Reversible Reversibility = "REVERSIBLE"
	// Compensatable means the effect cannot be undone but has a declared
	// compensating action (a release, a credit, a counter-operation).
	Compensatable Reversibility = "COMPENSATABLE"
	// Irreversible means neither: once done it stands, and the only remedy is
	// a repair plan.
	Irreversible Reversibility = "IRREVERSIBLE"
)

// Valid reports whether the class is one of the three declared tokens.
func (r Reversibility) Valid() bool {
	return r == Reversible || r == Compensatable || r == Irreversible
}

// String returns the wire token.
func (r Reversibility) String() string { return string(r) }

// ReversibilityClasses returns the three reversibility classes in declaration
// order, so a caller (or a conformance test) can prove its own list matches.
func ReversibilityClasses() []Reversibility {
	return []Reversibility{Reversible, Compensatable, Irreversible}
}

// EffectKind names one proposed change. The tokens are the participants the
// promote-into-management reference workflow lists inside its commit boundary.
//
// The type is declared here and reused by the compensation and budget half of
// the same simulation (internal/domains/promotion/simcomp), so that PROMO-004
// can take the union of the two effect sets without translating between two
// vocabularies that would then be free to drift apart.
type EffectKind string

// The People, Org and Position effect kinds a promotion implies.
const (
	// EffectAssignmentRevision is the new assignment revision: position, job,
	// grade and organizational unit, effective-dated from the promotion date.
	EffectAssignmentRevision EffectKind = "people.assignment.revision"
	// EffectManagerRelationship is the manager-relationship change.
	EffectManagerRelationship EffectKind = "org.manager_relationship.change"
	// EffectPositionOccupancy is the target position's occupancy transition
	// and the reservation that holds the head for it.
	EffectPositionOccupancy EffectKind = "position.occupancy.transition"
)

// String returns the wire token.
func (k EffectKind) String() string { return string(k) }

// EffectKinds returns the kinds this package can propose, in declaration
// order.
func EffectKinds() []EffectKind {
	return []EffectKind{EffectAssignmentRevision, EffectManagerRelationship, EffectPositionOccupancy}
}

// FieldChange is one field's exact before and after, as canonical text.
//
// Before always comes from the snapshot and After always comes from the
// caller's stated intention; there is no third source, and no code path that
// lets the two swap roles.
type FieldChange struct {
	// Field is the domain field path (e.g. "assignment.job_code").
	Field string
	// Before is the snapshot's canonical text for the field.
	Before string
	// After is the proposed canonical text.
	After string
	// Changed reports whether the promotion actually moves this field.
	Changed bool
	// SourceInput is the snapshot input Before was read from.
	SourceInput string
}

// Canonical encodes one change.
func (c FieldChange) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.simassign.FieldChange", schemaVersion).
		String("field", c.Field).
		String("before", c.Before).
		String("after", c.After).
		Bool("changed", c.Changed).
		String("source_input", c.SourceInput).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ProposedEffect is one typed change the promotion would make, stated in full
// and executed by nobody.
//
// It carries what a plan needs to be compiled from it and what a reviewer
// needs to judge it: the participant it touches, its reversibility class, the
// compensation and post-commit observation intent.CompilePlan demands of every
// effect, the baseline revision it assumes, and the exact snapshot inputs it
// was derived from.
type ProposedEffect struct {
	// EffectID is the deterministic identity of this effect within one
	// promotion.
	EffectID string
	// Kind is the typed effect kind.
	Kind EffectKind
	// Participant is the stream or store the effect touches.
	Participant string
	// DestinationRef addresses the effect's target.
	DestinationRef string
	// StorageClass names the participant's storage class.
	StorageClass string
	// Local reports whether the effect falls inside the single local ACID
	// commit boundary. It is derived from the authority class of the inputs
	// the effect was computed from: a NATIVE_STATE input is locally
	// authoritative, an EXTERNAL_OBSERVATION one is not.
	Local bool
	// Reversibility is the effect's undo class.
	Reversibility Reversibility
	// CompensationRef is the compensating action that undoes or repairs the
	// effect. Every effect declares one, whether or not it is local.
	CompensationRef string
	// CompensationStrategy is how that action is applied.
	CompensationStrategy string
	// ObservationRef is the post-commit observation that watches the effect.
	ObservationRef string
	// IdempotencyKey is the deterministic semantic identity a retry reuses.
	IdempotencyKey string
	// Subject is the material subject the effect is about.
	Subject intent.SubjectReference
	// ResourceKey addresses the effect within its owning domain.
	ResourceKey values.ResourceKey
	// Effective is the business interval the effect applies over.
	Effective values.EffectiveInterval
	// ExpectedRevision is the baseline the effect assumes: the watermark of
	// the snapshot input that supplied its "before".
	ExpectedRevision values.RevisionToken
	// AuthorityDecision is the per-field source-authority decision the write
	// is proposed under.
	AuthorityDecision string
	// Changes are the exact field movements, in declaration order.
	Changes []FieldChange
	// DerivedFrom names every snapshot input the effect was computed from,
	// sorted, so no effect can appear without saying what it read.
	DerivedFrom []string
}

// Canonical returns the effect's canonical byte encoding.
func (e ProposedEffect) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.promotion.simassign.ProposedEffect", schemaVersion).
		String("effect_id", e.EffectID).
		String("kind", string(e.Kind)).
		String("participant", e.Participant).
		String("destination_ref", e.DestinationRef).
		String("storage_class", e.StorageClass).
		Bool("local", e.Local).
		String("reversibility", string(e.Reversibility)).
		String("compensation_ref", e.CompensationRef).
		String("compensation_strategy", e.CompensationStrategy).
		String("observation_ref", e.ObservationRef).
		String("idempotency_key", e.IdempotencyKey).
		String("subject.kind", e.Subject.Kind).
		String("subject.id", e.Subject.SubjectID).
		String("subject.authority", e.Subject.AuthorityDomain).
		Value("resource_key", e.ResourceKey).
		Value("effective", e.Effective).
		Value("expected_revision", e.ExpectedRevision).
		String("authority_decision", e.AuthorityDecision).
		Count("changes", len(e.Changes))
	for _, c := range e.Changes {
		w.Field("change", c.Canonical())
	}
	w.SortedStrings("derived_from", e.DerivedFrom)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate reports whether the effect is fully stated. It is the contract
// every constructor in this package satisfies before an effect reaches a
// result, so a caller never has to test for a half-built effect.
func (e ProposedEffect) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"effect_id", e.EffectID},
		{"kind", string(e.Kind)},
		{"participant", e.Participant},
		{"destination_ref", e.DestinationRef},
		{"storage_class", e.StorageClass},
		{"compensation_ref", e.CompensationRef},
		{"compensation_strategy", e.CompensationStrategy},
		{"observation_ref", e.ObservationRef},
		{"idempotency_key", e.IdempotencyKey},
		{"authority_decision", e.AuthorityDecision},
	} {
		if strings.TrimSpace(req.value) == "" {
			return fmt.Errorf("%w: effect %q declares no %s", ErrEffectIncomplete, e.EffectID, req.field)
		}
	}
	if !e.Reversibility.Valid() {
		return fmt.Errorf("%w: effect %q declares reversibility %q", ErrEffectIncomplete, e.EffectID, e.Reversibility)
	}
	if err := e.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: effect %q subject: %w", ErrEffectIncomplete, e.EffectID, err)
	}
	if err := e.ResourceKey.Validate(); err != nil {
		return fmt.Errorf("%w: effect %q resource key: %w", ErrEffectIncomplete, e.EffectID, err)
	}
	if err := e.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effect %q effective time: %w", ErrEffectIncomplete, e.EffectID, err)
	}
	if !e.ExpectedRevision.IsSpecified() {
		return fmt.Errorf("%w: effect %q pins no baseline revision", ErrEffectIncomplete, e.EffectID)
	}
	if len(e.DerivedFrom) == 0 {
		return fmt.Errorf("%w: effect %q names no snapshot input", ErrEffectIncomplete, e.EffectID)
	}
	if len(e.Changes) == 0 {
		return fmt.Errorf("%w: effect %q states no change", ErrEffectIncomplete, e.EffectID)
	}
	return nil
}

// ErrEffectIncomplete is returned when a proposed effect is missing a
// declaration a plan would need. Matchable with errors.Is.
var ErrEffectIncomplete = errors.New("promotion: proposed effect is incomplete")

// PlannedWrites projects the effect's changed fields onto the kernel's typed
// planned-write shape. Unchanged fields are deliberately omitted: a write that
// sets a field to what it already holds is not part of the change.
func (e ProposedEffect) PlannedWrites() []intent.PlannedWrite {
	out := make([]intent.PlannedWrite, 0, len(e.Changes))
	for _, c := range e.Changes {
		if !c.Changed {
			continue
		}
		out = append(out, intent.PlannedWrite{
			Subject:                 e.Subject,
			ResourceKey:             e.ResourceKey,
			FieldPath:               c.Field,
			CurrentCanonicalText:    c.Before,
			ProposedCanonicalText:   c.After,
			SourceAuthorityDecision: e.AuthorityDecision,
			ExpectedRevision:        e.ExpectedRevision,
		})
	}
	return out
}

// PlannedEffect projects the effect onto the kernel's declared-effect shape.
func (e ProposedEffect) PlannedEffect() intent.PlannedEffect {
	return intent.PlannedEffect{
		EffectID:        e.EffectID,
		Kind:            string(e.Kind),
		DestinationRef:  e.DestinationRef,
		Reversibility:   string(e.Reversibility),
		CompensationRef: e.CompensationRef,
		ObservationRef:  e.ObservationRef,
	}
}

// OutboxEffect projects the effect onto the plan's outbox shape.
func (e ProposedEffect) OutboxEffect() intent.OutboxEffect {
	return intent.OutboxEffect{
		EffectID:       e.EffectID,
		DestinationRef: e.DestinationRef,
		IdempotencyKey: e.IdempotencyKey,
		Reversibility:  string(e.Reversibility),
	}
}

// Compensation projects the effect's compensating action onto the plan shape.
func (e ProposedEffect) Compensation() intent.CompensationBinding {
	return intent.CompensationBinding{
		EffectID:     e.EffectID,
		Strategy:     e.CompensationStrategy,
		RepairPlanID: e.CompensationRef,
	}
}

// Observation projects the effect's post-commit observation onto the plan
// shape. The deadline is the caller's declared observation deadline, which is
// the one thing about an observation a pure simulation cannot derive.
func (e ProposedEffect) Observation(deadline values.Instant) intent.PostCommitObservation {
	return intent.PostCommitObservation{
		EffectID:       e.EffectID,
		ObservationRef: e.ObservationRef,
		Deadline:       deadline,
	}
}

// Participant projects the effect's participant onto the plan shape.
func (e ProposedEffect) PlanParticipant() intent.PlanParticipant {
	return intent.PlanParticipant{
		ParticipantID: e.Participant,
		StreamID:      e.Participant,
		StorageClass:  e.StorageClass,
		Local:         e.Local,
	}
}

// Reason is the closed vocabulary of refusal causes.
type Reason string

// Refusal reasons.
const (
	// ReasonInputWithheld means authorization denied an input the effect
	// needed. The effect is refused rather than computed from a substitute.
	ReasonInputWithheld Reason = "INPUT_WITHHELD"
	// ReasonInputAbsent means the record answered and holds no such fact.
	ReasonInputAbsent Reason = "INPUT_ABSENT"
	// ReasonInputUnknown means the input's presence could not be established.
	ReasonInputUnknown Reason = "INPUT_UNKNOWN"
	// ReasonInputUnparsable means the input was disclosed but its canonical
	// text does not carry the field the effect needs. It is distinct from
	// ABSENT: the input answered, but not this question.
	ReasonInputUnparsable Reason = "INPUT_UNPARSABLE"
	// ReasonManagerCycle means the proposed manager relationship closes a
	// cycle in the management chain.
	ReasonManagerCycle Reason = "MANAGER_CYCLE"
	// ReasonChainDepthExceeded means the proposed chain is deeper than the
	// declared bound.
	ReasonChainDepthExceeded Reason = "MANAGER_CHAIN_DEPTH_EXCEEDED"
	// ReasonPositionAtCapacity means the target position has no head
	// available at the effective date.
	ReasonPositionAtCapacity Reason = "POSITION_AT_CAPACITY"
	// ReasonVacancyAfterEffectiveDate means the target position frees up, but
	// only after the promotion's effective date.
	ReasonVacancyAfterEffectiveDate Reason = "POSITION_VACANCY_AFTER_EFFECTIVE_DATE"
)

// String returns the wire token.
func (r Reason) String() string { return string(r) }

// Refusal is one typed reason a proposed effect was not produced. It names the
// effect kind it blocked and, when the cause was an input, the exact input and
// what the snapshot was able to say about it. It never carries a value: a
// refusal that quoted the input it could not disclose would be a disclosure.
type Refusal struct {
	// Kind is the effect that was refused.
	Kind EffectKind
	// Reason is the closed-vocabulary cause.
	Reason Reason
	// InputName is the snapshot input responsible, when there is one.
	InputName string
	// Availability is what the snapshot could say about that input.
	Availability promosnapshot.Availability
	// Detail is a safe, value-free explanation.
	Detail string
}

// Canonical encodes one refusal.
func (r Refusal) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.simassign.Refusal", schemaVersion).
		String("kind", string(r.Kind)).
		String("reason", string(r.Reason)).
		String("input_name", r.InputName).
		String("availability", string(r.Availability)).
		String("detail", r.Detail).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Error renders the refusal.
func (r Refusal) Error() string {
	if r.InputName == "" {
		return fmt.Sprintf("promotion: %s refused: %s (%s)", r.Kind, r.Reason, r.Detail)
	}
	return fmt.Sprintf("promotion: %s refused: %s on input %s (availability %s): %s",
		r.Kind, r.Reason, r.InputName, r.Availability, r.Detail)
}

// Unwrap makes every refusal match [ErrRefused].
func (r Refusal) Unwrap() error { return ErrRefused }

// ErrRefused is the sentinel every [Refusal] matches with errors.Is.
var ErrRefused = errors.New("promotion: a proposed effect was refused")

// RefusalForInput builds the refusal a non-disclosed input produces for one
// effect kind, mapping the availability onto its own reason so that a denial, a
// known absence and an unestablished presence never collapse into one cause.
func RefusalForInput(kind EffectKind, in promosnapshot.Input) Refusal {
	reason := ReasonInputUnknown
	switch in.Availability {
	case promosnapshot.AvailabilityWithheld:
		reason = ReasonInputWithheld
	case promosnapshot.AvailabilityAbsent:
		reason = ReasonInputAbsent
	}
	detail := in.Reason
	if detail == "" {
		detail = "the snapshot discloses no value for this input"
	}
	return Refusal{
		Kind:         kind,
		Reason:       reason,
		InputName:    in.Name,
		Availability: in.Availability,
		Detail:       detail,
	}
}

// LocalAuthority reports whether an input's authority class makes it part of
// the local commit boundary. NATIVE_STATE is state this system owns;
// everything else is somebody else's record observed from here, and a write
// against it is an external effect no matter how it is drawn.
func LocalAuthority(class enginesnapshot.AuthorityClass) bool {
	return class == enginesnapshot.AuthorityNativeState
}

// DerivedFrom returns the sorted, deduplicated input names an effect cites.
func DerivedFrom(names ...string) []string {
	out := slices.Clone(names)
	slices.Sort(out)
	return slices.Compact(out)
}
