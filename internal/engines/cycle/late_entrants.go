// CYCLE-006: decide late-entrant admissions and removals against an
// already-frozen PopulationBinding (CYCLE-005) as explicit, typed,
// reasoned events. DecideMembershipChange never rewrites the binding it is
// evaluated against and never applies a change on the caller's behalf: it
// always returns one immutable, digested MembershipChangeDecision naming a
// disposition -- include, exclude, defer or review -- plus the effective
// time and downstream recalculation obligations that disposition implies.
// A request that arrives after the phase's cutoff is never silently
// dropped (it decides Defer or Review, never nothing) and never silently
// joins (it is only Included when the active phase's declared operations
// say so).
package cycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OperationAdmitMember and OperationRemoveMember are the declared-operation
// tokens (see Phase.AllowedOperations / CompiledPhase.AllowedOperations)
// that let an ordinary, before-cutoff admission or removal request actually
// take effect. OperationAdmitLateEntrant and OperationRemoveLateEntrant
// separately gate the same requests once they arrive at or after the
// phase's cutoff: late handling is never implied by the ordinary
// operation, it must be declared for itself.
const (
	OperationAdmitMember       = "ADMIT_MEMBER"
	OperationRemoveMember      = "REMOVE_MEMBER"
	OperationAdmitLateEntrant  = "ADMIT_LATE_ENTRANT"
	OperationRemoveLateEntrant = "REMOVE_LATE_ENTRANT"
)

var (
	ErrMembershipChangeKind      = errors.New("cycle: membership change requires a valid kind")
	ErrMembershipChangeSubject   = errors.New("cycle: membership change requires a subject reference")
	ErrMembershipChangeReason    = errors.New("cycle: membership change requires an explicit reason")
	ErrMembershipChangeTime      = errors.New("cycle: membership change requires a requested-at and an effective-at instant")
	ErrMembershipChangeBinding   = errors.New("cycle: membership change decision requires a bound population digest")
	ErrMembershipChangeDecidedAt = errors.New("cycle: membership change decision requires a decided-at instant")
)

// PopulationChangeKind identifies whether a membership change event is an
// admission (a subject newly enters the bound population) or a removal (a
// subject leaves it).
type PopulationChangeKind string

const (
	PopulationChangeAdmission PopulationChangeKind = "ADMISSION"
	PopulationChangeRemoval   PopulationChangeKind = "REMOVAL"
)

func (k PopulationChangeKind) valid() bool {
	return k == PopulationChangeAdmission || k == PopulationChangeRemoval
}

// MembershipChangeRequest is one typed, reasoned request to admit or remove
// a subject against an already-bound population. A request is never
// applied by simply constructing it: DecideMembershipChange must be called,
// and it always returns an explicit disposition rather than mutating any
// binding.
type MembershipChangeRequest struct {
	Kind        PopulationChangeKind
	SubjectRef  string
	Reason      string
	RequestedAt time.Time
	EffectiveAt time.Time
}

// Validate reports whether the request is complete enough to decide.
func (r MembershipChangeRequest) Validate() error {
	if !r.Kind.valid() {
		return ErrMembershipChangeKind
	}
	if strings.TrimSpace(r.SubjectRef) == "" {
		return ErrMembershipChangeSubject
	}
	if strings.TrimSpace(r.Reason) == "" {
		return ErrMembershipChangeReason
	}
	if r.RequestedAt.IsZero() || r.EffectiveAt.IsZero() {
		return ErrMembershipChangeTime
	}
	return nil
}

// Disposition is the exhaustive outcome a membership-change policy can
// produce. A policy that cannot confidently decide returns
// DispositionReview; it never defaults to inclusion or exclusion.
type Disposition string

const (
	// DispositionInclude admits (or, for a removal, actually excludes) the
	// subject with immediate effect.
	DispositionInclude Disposition = "INCLUDE"
	// DispositionExclude actually removes the subject with immediate
	// effect.
	DispositionExclude Disposition = "EXCLUDE"
	// DispositionDefer holds the request for the next cycle: it neither
	// applies now nor is discarded.
	DispositionDefer Disposition = "DEFER"
	// DispositionReview holds the request for a human decision because the
	// active phase does not declare the operation this request would need.
	DispositionReview Disposition = "REVIEW"
)

// MembershipChangeDecision is the immutable record of how one
// MembershipChangeRequest was resolved against one PopulationBinding's
// active compiled phase and cutoff: a typed disposition, the effective time
// it actually applies at, and the recalculation obligations it hands
// downstream. It is evidence alongside the frozen binding, never a rewrite
// of it.
type MembershipChangeDecision struct {
	Request       MembershipChangeRequest
	BindingDigest string
	Cutoff        time.Time
	Disposition   Disposition
	// EffectiveAt is the instant the disposition actually applies at. It is
	// the zero time for DispositionDefer and DispositionReview: neither
	// takes effect against this binding, so there is no effective instant
	// to report yet.
	EffectiveAt              time.Time
	RecalculationObligations []string
	// Rule names the deciding rule (ARCH-GO-009's required Explain-shaped
	// output).
	Rule      string
	DecidedAt time.Time
	Digest    string
}

// DecideMembershipChange decides req against binding, whose currently
// active compiled phase is activePhase and whose phase cutoff is cutoff.
// The decision never mutates binding: a caller that wants an Include or
// Exclude disposition actually reflected in who the cycle is bound to must
// separately rebind (see PopulationBinding.Rebind) when the active phase's
// declared operations allow it.
func DecideMembershipChange(binding PopulationBinding, activePhase CompiledPhase, cutoff time.Time, req MembershipChangeRequest, decidedAt time.Time) (MembershipChangeDecision, error) {
	if err := req.Validate(); err != nil {
		return MembershipChangeDecision{}, err
	}
	if strings.TrimSpace(binding.Digest) == "" {
		return MembershipChangeDecision{}, ErrMembershipChangeBinding
	}
	if decidedAt.IsZero() {
		return MembershipChangeDecision{}, ErrMembershipChangeDecidedAt
	}

	late := !req.EffectiveAt.Before(cutoff)
	var (
		disposition Disposition
		rule        string
		obligations []string
	)

	fmtTime := func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

	switch req.Kind {
	case PopulationChangeAdmission:
		switch {
		case !late && phaseAllows(activePhase, OperationAdmitMember):
			disposition = DispositionInclude
			rule = fmt.Sprintf("admission effective %s is before cutoff %s and phase %q declares %s",
				fmtTime(req.EffectiveAt), fmtTime(cutoff), activePhase.ID, OperationAdmitMember)
			obligations = []string{"RECALCULATE_ELIGIBILITY", "RECALCULATE_BALANCES"}
		case !late:
			disposition = DispositionReview
			rule = fmt.Sprintf("admission effective %s is before cutoff %s but phase %q does not declare %s",
				fmtTime(req.EffectiveAt), fmtTime(cutoff), activePhase.ID, OperationAdmitMember)
		case phaseAllows(activePhase, OperationAdmitLateEntrant):
			disposition = DispositionInclude
			rule = fmt.Sprintf("admission effective %s is at/after cutoff %s but phase %q declares %s",
				fmtTime(req.EffectiveAt), fmtTime(cutoff), activePhase.ID, OperationAdmitLateEntrant)
			obligations = []string{"RECALCULATE_ELIGIBILITY", "RECALCULATE_BALANCES"}
		default:
			disposition = DispositionDefer
			rule = fmt.Sprintf("admission effective %s is at/after cutoff %s and phase %q does not declare %s: deferred to the next cycle rather than dropped",
				fmtTime(req.EffectiveAt), fmtTime(cutoff), activePhase.ID, OperationAdmitLateEntrant)
			obligations = []string{"AWAIT_NEXT_CYCLE"}
		}
	case PopulationChangeRemoval:
		switch {
		case !late && phaseAllows(activePhase, OperationRemoveMember):
			disposition = DispositionExclude
			rule = fmt.Sprintf("removal effective %s is before cutoff %s and phase %q declares %s",
				fmtTime(req.EffectiveAt), fmtTime(cutoff), activePhase.ID, OperationRemoveMember)
			obligations = []string{"RECALCULATE_ELIGIBILITY", "REVERSE_GRANTS"}
		case !late:
			disposition = DispositionReview
			rule = fmt.Sprintf("removal effective %s is before cutoff %s but phase %q does not declare %s",
				fmtTime(req.EffectiveAt), fmtTime(cutoff), activePhase.ID, OperationRemoveMember)
		case phaseAllows(activePhase, OperationRemoveLateEntrant):
			disposition = DispositionExclude
			rule = fmt.Sprintf("removal effective %s is at/after cutoff %s and phase %q declares %s",
				fmtTime(req.EffectiveAt), fmtTime(cutoff), activePhase.ID, OperationRemoveLateEntrant)
			obligations = []string{"RECALCULATE_ELIGIBILITY", "REVERSE_GRANTS"}
		default:
			disposition = DispositionReview
			rule = fmt.Sprintf("removal effective %s is at/after cutoff %s and phase %q does not declare %s: held for review rather than silently excluded or retained",
				fmtTime(req.EffectiveAt), fmtTime(cutoff), activePhase.ID, OperationRemoveLateEntrant)
		}
	}

	effectiveAt := req.EffectiveAt
	if disposition == DispositionDefer || disposition == DispositionReview {
		effectiveAt = time.Time{}
	}

	decision := MembershipChangeDecision{
		Request:                  req,
		BindingDigest:            binding.Digest,
		Cutoff:                   cutoff.UTC(),
		Disposition:              disposition,
		EffectiveAt:              effectiveAt,
		RecalculationObligations: append([]string(nil), obligations...),
		Rule:                     rule,
		DecidedAt:                decidedAt.UTC(),
	}
	decision.Digest = decision.computeDigest()
	return decision, nil
}

func (d MembershipChangeDecision) computeDigest() string {
	raw, _ := json.Marshal(struct {
		Request       MembershipChangeRequest
		BindingDigest string
		Cutoff        time.Time
		Disposition   Disposition
		EffectiveAt   time.Time
		Obligations   []string
		DecidedAt     time.Time
	}{d.Request, d.BindingDigest, d.Cutoff, d.Disposition, d.EffectiveAt, d.RecalculationObligations, d.DecidedAt})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CanonicalDigest returns the decision's own digest.
func (d MembershipChangeDecision) CanonicalDigest() string { return d.Digest }
