package performance

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidOutcomeLink      = errors.New("performance: invalid outcome link")
	ErrOutcomeRatingNotFinal   = errors.New("performance: outcome link requires a FINAL rating")
	ErrOutcomeRatingSuperseded = errors.New("performance: outcome link cannot target a superseded rating revision")
	ErrOutcomeLinkNotFound     = errors.New("performance: outcome link not found")
	ErrOutcomeAlreadyUnlinked  = errors.New("performance: outcome link is already unlinked")
	ErrOutcomeLinkDuplicate    = errors.New("performance: duplicate outcome link")
)

// OutcomeKind is the deliberately closed vocabulary of performance outcomes.
// Outcome links are observations or downstream references, not rating facts.
type OutcomeKind string

const (
	OutcomeKindCompensationChangeProposal OutcomeKind = "COMPENSATION_CHANGE_PROPOSAL"
	OutcomeKindPromotionIntent            OutcomeKind = "PROMOTION_INTENT"
	OutcomeKindDevelopmentPlan            OutcomeKind = "DEVELOPMENT_PLAN"
	OutcomeKindRetentionFlag              OutcomeKind = "RETENTION_FLAG"

	// Short aliases keep the vocabulary easy to use at domain boundaries.
	OutcomeCompensationChangeProposal = OutcomeKindCompensationChangeProposal
	OutcomePromotionIntent            = OutcomeKindPromotionIntent
	OutcomeDevelopmentPlan            = OutcomeKindDevelopmentPlan
	OutcomeRetentionFlag              = OutcomeKindRetentionFlag
	OutcomeKindCompensation           = OutcomeKindCompensationChangeProposal
	OutcomeKindPromotion              = OutcomeKindPromotionIntent
	OutcomeKindLearning               = OutcomeKindDevelopmentPlan
	OutcomeKindRetention              = OutcomeKindRetentionFlag
)

func (k OutcomeKind) Valid() bool {
	switch k {
	case OutcomeKindCompensationChangeProposal, OutcomeKindPromotionIntent,
		OutcomeKindDevelopmentPlan, OutcomeKindRetentionFlag:
		return true
	default:
		return false
	}
}

// OutcomeLinkAction distinguishes an initial downstream observation from its
// later append-only withdrawal. UNLINK records retain the original link.
type OutcomeLinkAction string

const (
	OutcomeLinkActionLink   OutcomeLinkAction = "LINK"
	OutcomeLinkActionUnlink OutcomeLinkAction = "UNLINK"
	OutcomeActionLink                         = OutcomeLinkActionLink
	OutcomeActionUnlink                       = OutcomeLinkActionUnlink
)

// RatingRevisionState is carried by a rating reference because FinalRating is
// intentionally a terminal value and does not own a mutable lifecycle field.
type RatingRevisionState string

const (
	RatingRevisionStateProposed   RatingRevisionState = "PROPOSED"
	RatingRevisionStateCalibrated RatingRevisionState = "CALIBRATED"
	RatingRevisionStateFinal      RatingRevisionState = "FINAL"
	RatingRevisionStateSuperseded RatingRevisionState = "SUPERSEDED"

	RatingStateProposed   = RatingRevisionStateProposed
	RatingStateCalibrated = RatingRevisionStateCalibrated
	RatingStateFinal      = RatingRevisionStateFinal
	RatingStateSuperseded = RatingRevisionStateSuperseded
)

func (s RatingRevisionState) Valid() bool {
	switch s {
	case RatingRevisionStateProposed, RatingRevisionStateCalibrated,
		RatingRevisionStateFinal, RatingRevisionStateSuperseded:
		return true
	default:
		return false
	}
}

// RatingReference names the exact rating revision an outcome observes.
// Superseded is retained as a separate bit so callers cannot accidentally
// treat a stale revision as current merely by copying its state string.
type RatingReference struct {
	Digest       string
	Revision     uint64
	State        RatingRevisionState
	Superseded   bool
	SupersededBy string
}

type FinalRatingReference = RatingReference
type OutcomeRatingReference = RatingReference
type FinalRatingRef = RatingReference
type RatingRef = RatingReference

func NewFinalRatingReference(rating FinalRating, revision uint64) (RatingReference, error) {
	if err := rating.Validate(); err != nil {
		return RatingReference{}, fmt.Errorf("%w: final rating: %v", ErrInvalidOutcomeLink, err)
	}
	if revision == 0 {
		revision = 1
	}
	return RatingReference{Digest: rating.CanonicalDigest, Revision: revision, State: RatingRevisionStateFinal}, nil
}

func NewRatingReference(digest string, revision uint64, state RatingRevisionState, superseded bool, supersededBy string) (RatingReference, error) {
	ref := RatingReference{Digest: digest, Revision: revision, State: state, Superseded: superseded, SupersededBy: supersededBy}
	if err := ref.Validate(); err != nil {
		return RatingReference{}, err
	}
	return ref, nil
}

func (r RatingReference) Validate() error {
	if !strings.HasPrefix(r.Digest, canonicalbytes.DigestAlgorithm+":") || len(r.Digest) <= len(canonicalbytes.DigestAlgorithm)+1 {
		return fmt.Errorf("%w: rating digest is required", ErrInvalidOutcomeLink)
	}
	if r.Revision == 0 {
		return fmt.Errorf("%w: rating revision is required", ErrInvalidOutcomeLink)
	}
	if !r.State.Valid() {
		return fmt.Errorf("%w: unknown rating state %q", ErrInvalidOutcomeLink, r.State)
	}
	if r.SupersededBy != "" && !strings.HasPrefix(r.SupersededBy, canonicalbytes.DigestAlgorithm+":") {
		return fmt.Errorf("%w: superseding rating must be a digest", ErrInvalidOutcomeLink)
	}
	return nil
}

// OutcomeLinkRequest is the pure command for creating one LINK record. A
// FinalRating is preferred: it proves the digest and terminal state locally.
// RatingRef supports callers whose read model already holds the exact digest
// and revision metadata. The scalar aliases are accepted for transport mappers.
type OutcomeLinkRequest struct {
	FinalRating      FinalRating
	RatingRef        RatingReference
	RatingDigest     string
	RatingRevision   uint64
	RatingState      RatingRevisionState
	RatingSuperseded bool
	OutcomeKind      OutcomeKind
	OutcomeRef       string
	Kind             OutcomeKind
	Ref              string
	LinkingPrincipal string
	PrincipalID      string
	Principal        string
	EffectiveAt      values.Instant
}

// OutcomeLink is an immutable, digested LINK or UNLINK observation. It never
// embeds or rewrites a review, proposed rating, calibration adjustment,
// contest, or FinalRating; only the exact rating digest is retained.
type OutcomeLink struct {
	RatingRef        RatingReference
	RatingDigest     string
	RatingRevision   uint64
	Action           OutcomeLinkAction
	OutcomeKind      OutcomeKind
	OutcomeRef       string
	Kind             OutcomeKind
	Ref              string
	LinkingPrincipal string
	PrincipalID      string
	Principal        string
	State            OutcomeLinkAction
	EffectiveAt      values.Instant
	Reason           string
	Digest           string
}

func (r OutcomeLinkRequest) normalizedReference() (RatingReference, error) {
	ref := r.RatingRef
	if ref.Digest == "" {
		ref = RatingReference{Digest: r.RatingDigest, Revision: r.RatingRevision, State: r.RatingState, Superseded: r.RatingSuperseded}
	}
	if r.FinalRating.CanonicalDigest != "" {
		if err := r.FinalRating.Validate(); err != nil {
			return RatingReference{}, fmt.Errorf("%w: final rating: %v", ErrOutcomeRatingNotFinal, err)
		}
		if ref.Digest != "" && ref.Digest != r.FinalRating.CanonicalDigest {
			return RatingReference{}, fmt.Errorf("%w: rating digest does not match FinalRating", ErrInvalidOutcomeLink)
		}
		ref.Digest = r.FinalRating.CanonicalDigest
		if ref.Revision == 0 {
			ref.Revision = 1
		}
		if ref.State == "" {
			ref.State = RatingRevisionStateFinal
		}
	}
	if ref.State == "" {
		ref.State = RatingRevisionStateFinal
	}
	if ref.Revision == 0 {
		ref.Revision = 1
	}
	if ref.Superseded || ref.State == RatingRevisionStateSuperseded {
		return RatingReference{}, ErrOutcomeRatingSuperseded
	}
	if err := ref.Validate(); err != nil {
		return RatingReference{}, err
	}
	if ref.State != RatingRevisionStateFinal {
		return RatingReference{}, ErrOutcomeRatingNotFinal
	}
	return ref, nil
}

func (r OutcomeLinkRequest) Validate() error {
	if _, err := r.normalizedReference(); err != nil {
		return err
	}
	kind := r.OutcomeKind
	if kind == "" {
		kind = r.Kind
	}
	if !kind.Valid() || (r.OutcomeKind != "" && r.Kind != "" && r.OutcomeKind != r.Kind) {
		return fmt.Errorf("%w: unknown outcome kind %q", ErrInvalidOutcomeLink, r.OutcomeKind)
	}
	ref := strings.TrimSpace(r.OutcomeRef)
	if ref == "" {
		ref = strings.TrimSpace(r.Ref)
	}
	if ref == "" || (r.OutcomeRef != "" && r.Ref != "" && r.OutcomeRef != r.Ref) {
		return fmt.Errorf("%w: outcome reference is required", ErrInvalidOutcomeLink)
	}
	principal := strings.TrimSpace(r.LinkingPrincipal)
	if principal == "" {
		principal = strings.TrimSpace(r.PrincipalID)
	}
	if principal == "" {
		principal = strings.TrimSpace(r.Principal)
	}
	if principal == "" || (r.LinkingPrincipal != "" && r.PrincipalID != "" && r.LinkingPrincipal != r.PrincipalID) || (r.LinkingPrincipal != "" && r.Principal != "" && r.LinkingPrincipal != r.Principal) {
		return fmt.Errorf("%w: linking principal is required and must be unambiguous", ErrInvalidOutcomeLink)
	}
	if err := r.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective instant: %v", ErrInvalidOutcomeLink, err)
	}
	return nil
}

func (r OutcomeLinkRequest) link() (OutcomeLink, error) {
	if err := r.Validate(); err != nil {
		return OutcomeLink{}, err
	}
	ref, err := r.normalizedReference()
	if err != nil {
		return OutcomeLink{}, err
	}
	principal := strings.TrimSpace(r.LinkingPrincipal)
	if principal == "" {
		principal = strings.TrimSpace(r.PrincipalID)
	}
	kind := r.OutcomeKind
	if kind == "" {
		kind = r.Kind
	}
	outcomeRef := strings.TrimSpace(r.OutcomeRef)
	if outcomeRef == "" {
		outcomeRef = strings.TrimSpace(r.Ref)
	}
	link := OutcomeLink{RatingRef: ref, RatingDigest: ref.Digest, RatingRevision: ref.Revision,
		Action: OutcomeLinkActionLink, State: OutcomeLinkActionLink, OutcomeKind: kind, Kind: kind, OutcomeRef: outcomeRef, Ref: outcomeRef,
		LinkingPrincipal: principal, PrincipalID: principal, Principal: principal, EffectiveAt: r.EffectiveAt}
	return link.withDigest()
}

func (l OutcomeLink) canonical() []byte {
	if l.RatingRef.Validate() != nil || l.RatingRef.State != RatingRevisionStateFinal || l.RatingRef.Superseded || !l.OutcomeKind.Valid() || strings.TrimSpace(l.OutcomeRef) == "" || l.EffectiveAt.Validate() != nil {
		return nil
	}
	principal := l.LinkingPrincipal
	if principal == "" {
		principal = l.PrincipalID
	}
	w := canonicalbytes.New("hcmnext.domains.performance.OutcomeLink", 1).
		String("rating_digest", l.RatingRef.Digest).Int("rating_revision", int64(l.RatingRef.Revision)).
		String("rating_state", string(l.RatingRef.State)).Bool("rating_superseded", l.RatingRef.Superseded).
		String("action", string(l.Action)).String("outcome_kind", string(l.OutcomeKind)).
		String("outcome_ref", l.OutcomeRef).String("linking_principal", principal).
		Value("effective_at", l.EffectiveAt).String("reason", l.Reason)
	raw, writeErr := w.Bytes()
	if writeErr != nil {
		return nil
	}
	return raw
}

func (l OutcomeLink) computedDigest() string { return canonicalbytes.Digest(l.canonical()) }

func (l OutcomeLink) withDigest() (OutcomeLink, error) {
	l.RatingDigest = l.RatingRef.Digest
	l.RatingRevision = l.RatingRef.Revision
	if l.LinkingPrincipal == "" {
		l.LinkingPrincipal = l.PrincipalID
	}
	if l.PrincipalID == "" {
		l.PrincipalID = l.LinkingPrincipal
	}
	if l.Action == "" {
		l.Action = OutcomeLinkActionLink
	}
	if l.State == "" {
		l.State = l.Action
	}
	if l.Kind == "" {
		l.Kind = l.OutcomeKind
	}
	if l.Ref == "" {
		l.Ref = l.OutcomeRef
	}
	if err := l.validateWithoutDigest(); err != nil {
		return OutcomeLink{}, err
	}
	l.Digest = l.computedDigest()
	return l, nil
}

func (l OutcomeLink) validateWithoutDigest() error {
	if err := l.RatingRef.Validate(); err != nil {
		return err
	}
	if l.RatingRef.Superseded || l.RatingRef.State == RatingRevisionStateSuperseded {
		return ErrOutcomeRatingSuperseded
	}
	if l.RatingRef.State != RatingRevisionStateFinal {
		return ErrOutcomeRatingNotFinal
	}
	if l.RatingDigest != "" && l.RatingDigest != l.RatingRef.Digest {
		return fmt.Errorf("%w: rating digest alias mismatch", ErrInvalidOutcomeLink)
	}
	if l.RatingRevision != 0 && l.RatingRevision != l.RatingRef.Revision {
		return fmt.Errorf("%w: rating revision alias mismatch", ErrInvalidOutcomeLink)
	}
	if l.Action != OutcomeLinkActionLink && l.Action != OutcomeLinkActionUnlink {
		return fmt.Errorf("%w: unknown action %q", ErrInvalidOutcomeLink, l.Action)
	}
	if l.State != l.Action {
		return fmt.Errorf("%w: action and state aliases differ", ErrInvalidOutcomeLink)
	}
	if l.Kind != "" && l.Kind != l.OutcomeKind {
		return fmt.Errorf("%w: outcome kind aliases differ", ErrInvalidOutcomeLink)
	}
	if l.Ref != "" && l.Ref != l.OutcomeRef {
		return fmt.Errorf("%w: outcome reference aliases differ", ErrInvalidOutcomeLink)
	}
	if !l.OutcomeKind.Valid() || strings.TrimSpace(l.OutcomeRef) == "" {
		return fmt.Errorf("%w: outcome kind and reference are required", ErrInvalidOutcomeLink)
	}
	if strings.TrimSpace(l.LinkingPrincipal) == "" || (l.PrincipalID != "" && l.PrincipalID != l.LinkingPrincipal) || (l.Principal != "" && l.Principal != l.LinkingPrincipal) {
		return fmt.Errorf("%w: linking principal is required and unambiguous", ErrInvalidOutcomeLink)
	}
	if err := l.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective instant: %v", ErrInvalidOutcomeLink, err)
	}
	if l.Action == OutcomeLinkActionUnlink && strings.TrimSpace(l.Reason) == "" {
		return fmt.Errorf("%w: unlink reason is required", ErrInvalidOutcomeLink)
	}
	return nil
}

func (l OutcomeLink) Validate() error {
	if err := l.validateWithoutDigest(); err != nil {
		return err
	}
	if l.Digest == "" || l.Digest != l.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidOutcomeLink)
	}
	return nil
}

func NewOutcomeLink(input any, args ...any) (OutcomeLink, error) {
	return LinkOutcome(input, args...)
}
func LinkFinalRatingOutcome(rating FinalRating, kind OutcomeKind, outcomeRef, principal string, effectiveAt values.Instant) (OutcomeLink, error) {
	return NewOutcomeLink(OutcomeLinkRequest{FinalRating: rating, OutcomeKind: kind, OutcomeRef: outcomeRef, LinkingPrincipal: principal, EffectiveAt: effectiveAt})
}

// LinkOutcome accepts either an OutcomeLinkRequest or the convenient
// (FinalRating, OutcomeKind, outcomeRef, principal, effectiveAt) form.
func LinkOutcome(input any, args ...any) (OutcomeLink, error) {
	switch value := input.(type) {
	case OutcomeLinkRequest:
		if len(args) != 0 {
			return OutcomeLink{}, fmt.Errorf("%w: request cannot have positional arguments", ErrInvalidOutcomeLink)
		}
		return value.link()
	case FinalRating:
		if len(args) != 4 {
			return OutcomeLink{}, fmt.Errorf("%w: final rating link needs kind, reference, principal and effective instant", ErrInvalidOutcomeLink)
		}
		kind, ok := args[0].(OutcomeKind)
		if !ok {
			return OutcomeLink{}, fmt.Errorf("%w: outcome kind type is invalid", ErrInvalidOutcomeLink)
		}
		ref, ok1 := args[1].(string)
		principal, ok2 := args[2].(string)
		at, ok3 := args[3].(values.Instant)
		if !ok1 || !ok2 || !ok3 {
			return OutcomeLink{}, fmt.Errorf("%w: outcome link arguments have invalid types", ErrInvalidOutcomeLink)
		}
		return LinkFinalRatingOutcome(value, kind, ref, principal, at)
	case RatingReference:
		if len(args) != 4 {
			return OutcomeLink{}, fmt.Errorf("%w: rating reference link needs kind, reference, principal and effective instant", ErrInvalidOutcomeLink)
		}
		kind, ok := args[0].(OutcomeKind)
		ref, ok1 := args[1].(string)
		principal, ok2 := args[2].(string)
		at, ok3 := args[3].(values.Instant)
		if !ok || !ok1 || !ok2 || !ok3 {
			return OutcomeLink{}, fmt.Errorf("%w: outcome link arguments have invalid types", ErrInvalidOutcomeLink)
		}
		return NewOutcomeLink(OutcomeLinkRequest{RatingRef: value, OutcomeKind: kind, OutcomeRef: ref, LinkingPrincipal: principal, EffectiveAt: at})
	case RatingCase:
		if !value.Finalized {
			return OutcomeLink{}, ErrOutcomeRatingNotFinal
		}
		return LinkOutcome(value.FinalRating, args...)
	default:
		return OutcomeLink{}, fmt.Errorf("%w: unsupported rating target %T", ErrInvalidOutcomeLink, input)
	}
}

func (l OutcomeLink) Canonical() []byte { return l.canonical() }

type OutcomeLinkExplanation struct {
	RatingDigest     string
	RatingRevision   uint64
	Action           OutcomeLinkAction
	OutcomeKind      OutcomeKind
	OutcomeRef       string
	LinkingPrincipal string
	EffectiveAt      values.Instant
	Reason           string
	Digest           string
}

func (l OutcomeLink) Explain() (OutcomeLinkExplanation, error) {
	if err := l.Validate(); err != nil {
		return OutcomeLinkExplanation{}, err
	}
	return OutcomeLinkExplanation{RatingDigest: l.RatingRef.Digest, RatingRevision: l.RatingRef.Revision,
		Action: l.Action, OutcomeKind: l.OutcomeKind, OutcomeRef: l.OutcomeRef,
		LinkingPrincipal: l.LinkingPrincipal, EffectiveAt: l.EffectiveAt, Reason: l.Reason, Digest: l.Digest}, nil
}

func ExplainOutcomeLink(l OutcomeLink) (OutcomeLinkExplanation, error) { return l.Explain() }

func NewOutcomeUnlink(link OutcomeLink, principal string, effectiveAt values.Instant, reason string) (OutcomeLink, error) {
	if err := link.Validate(); err != nil {
		return OutcomeLink{}, err
	}
	unlinked := OutcomeLink{RatingRef: link.RatingRef, RatingDigest: link.RatingRef.Digest, RatingRevision: link.RatingRef.Revision,
		Action: OutcomeLinkActionUnlink, State: OutcomeLinkActionUnlink, OutcomeKind: link.OutcomeKind, Kind: link.OutcomeKind,
		OutcomeRef: link.OutcomeRef, Ref: link.OutcomeRef, LinkingPrincipal: principal, PrincipalID: principal, Principal: principal,
		EffectiveAt: effectiveAt, Reason: reason}
	return unlinked.withDigest()
}

// OpinionRecord is a digest-only member of the opinion stream. It lets this
// additive boundary preserve the complete source opinion sequence without
// taking ownership of review, proposed-rating, calibration, contest, or final
// rating lifecycle.
type OpinionRecord struct {
	Kind     string
	Digest   string
	Revision uint64
}

func NewOpinionRecord(kind, digest string, revision uint64) (OpinionRecord, error) {
	record := OpinionRecord{Kind: strings.TrimSpace(kind), Digest: digest, Revision: revision}
	if err := record.Validate(); err != nil {
		return OpinionRecord{}, err
	}
	return record, nil
}

func (r OpinionRecord) Validate() error {
	if r.Kind == "" || !strings.HasPrefix(r.Digest, canonicalbytes.DigestAlgorithm+":") || len(r.Digest) <= len(canonicalbytes.DigestAlgorithm)+1 || r.Revision == 0 {
		return fmt.Errorf("%w: opinion kind, digest and positive revision are required", ErrInvalidOutcomeLink)
	}
	return nil
}

func OpinionFromFinalRating(rating FinalRating) (OpinionRecord, error) {
	if err := rating.Validate(); err != nil {
		return OpinionRecord{}, err
	}
	return NewOpinionRecord("FINAL_RATING", rating.CanonicalDigest, 1)
}

// OutcomeHistorySnapshot keeps the opinion and outcome streams separate in
// the read shape. Both slices are returned defensively and retain append order.
type OutcomeHistorySnapshot struct {
	OpinionRecords []OpinionRecord
	OutcomeLinks   []OutcomeLink
}

type PerformanceHistory = OutcomeHistorySnapshot

// OutcomeLinkHistory is an immutable-by-value append-only outcome stream.
// Linking and unlinking return detached successors; no prior opinion or link
// record is ever modified.
type OutcomeLinkHistory struct {
	OpinionRecords  []OpinionRecord
	OutcomeLinks    []OutcomeLink
	CanonicalDigest string
}

type OutcomeHistory = OutcomeLinkHistory

func NewOutcomeLinkHistory(opinions []OpinionRecord) (OutcomeLinkHistory, error) {
	copyOpinions := append([]OpinionRecord(nil), opinions...)
	for _, opinion := range copyOpinions {
		if err := opinion.Validate(); err != nil {
			return OutcomeLinkHistory{}, err
		}
	}
	h := OutcomeLinkHistory{OpinionRecords: copyOpinions, OutcomeLinks: []OutcomeLink{}}
	return h.withDigest()
}

func NewOutcomeHistory(inputs ...any) (OutcomeHistory, error) {
	if len(inputs) == 0 {
		return NewOutcomeLinkHistory(nil)
	}
	if len(inputs) == 1 {
		switch input := inputs[0].(type) {
		case []OpinionRecord:
			return NewOutcomeLinkHistory(input)
		case FinalRating:
			return NewOutcomeHistoryForRating(input)
		case OpinionRecord:
			return NewOutcomeLinkHistory([]OpinionRecord{input})
		default:
			return OutcomeHistory{}, fmt.Errorf("%w: unsupported opinion seed %T", ErrInvalidOutcomeLink, inputs[0])
		}
	}
	opinions := make([]OpinionRecord, 0, len(inputs))
	for _, input := range inputs {
		opinion, ok := input.(OpinionRecord)
		if !ok {
			return OutcomeHistory{}, fmt.Errorf("%w: unsupported opinion seed %T", ErrInvalidOutcomeLink, input)
		}
		opinions = append(opinions, opinion)
	}
	return NewOutcomeLinkHistory(opinions)
}

func NewOutcomeHistoryForRating(rating FinalRating) (OutcomeHistory, error) {
	opinion, err := OpinionFromFinalRating(rating)
	if err != nil {
		return OutcomeHistory{}, err
	}
	return NewOutcomeHistory([]OpinionRecord{opinion})
}

func (h OutcomeLinkHistory) Validate() error {
	for _, opinion := range h.OpinionRecords {
		if err := opinion.Validate(); err != nil {
			return err
		}
	}
	for _, link := range h.OutcomeLinks {
		if err := link.Validate(); err != nil {
			return err
		}
	}
	if h.CanonicalDigest == "" || h.CanonicalDigest != h.computedDigest() {
		return fmt.Errorf("%w: outcome history digest mismatch", ErrInvalidOutcomeLink)
	}
	return nil
}

func (h OutcomeLinkHistory) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.performance.OutcomeLinkHistory", 1).Count("opinions", len(h.OpinionRecords))
	for _, opinion := range h.OpinionRecords {
		w.String("opinion.kind", opinion.Kind).String("opinion.digest", opinion.Digest).Int("opinion.revision", int64(opinion.Revision))
	}
	w.Count("outcome_links", len(h.OutcomeLinks))
	for _, link := range h.OutcomeLinks {
		w.String("outcome_link", link.Digest)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func (h OutcomeLinkHistory) withDigest() (OutcomeLinkHistory, error) {
	h.CanonicalDigest = h.computedDigest()
	if err := h.ValidateWithoutDigest(); err != nil {
		return OutcomeLinkHistory{}, err
	}
	return h, nil
}

func (h OutcomeLinkHistory) ValidateWithoutDigest() error {
	for _, opinion := range h.OpinionRecords {
		if err := opinion.Validate(); err != nil {
			return err
		}
	}
	for _, link := range h.OutcomeLinks {
		if err := link.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (h OutcomeLinkHistory) LinkOutcome(input any, args ...any) (OutcomeLinkHistory, OutcomeLink, error) {
	if err := h.Validate(); err != nil {
		return OutcomeLinkHistory{}, OutcomeLink{}, err
	}
	link, err := LinkOutcome(input, args...)
	if err != nil {
		return OutcomeLinkHistory{}, OutcomeLink{}, err
	}
	for _, prior := range h.OutcomeLinks {
		if prior.Digest == link.Digest {
			return OutcomeLinkHistory{}, OutcomeLink{}, ErrOutcomeLinkDuplicate
		}
	}
	next := OutcomeLinkHistory{OpinionRecords: append([]OpinionRecord(nil), h.OpinionRecords...), OutcomeLinks: append([]OutcomeLink(nil), h.OutcomeLinks...)}
	next.OutcomeLinks = append(next.OutcomeLinks, link)
	next, err = next.withDigest()
	if err != nil {
		return OutcomeLinkHistory{}, OutcomeLink{}, err
	}
	return next, link, nil
}

type OutcomeUnlinkRequest struct {
	LinkDigest       string
	LinkingPrincipal string
	PrincipalID      string
	EffectiveAt      values.Instant
	Reason           string
}

func (r OutcomeUnlinkRequest) principal() string {
	if strings.TrimSpace(r.LinkingPrincipal) != "" {
		return strings.TrimSpace(r.LinkingPrincipal)
	}
	return strings.TrimSpace(r.PrincipalID)
}

func (h OutcomeLinkHistory) unlinkOutcome(request OutcomeUnlinkRequest) (OutcomeLinkHistory, OutcomeLink, error) {
	if err := h.Validate(); err != nil {
		return OutcomeLinkHistory{}, OutcomeLink{}, err
	}
	if strings.TrimSpace(request.LinkDigest) == "" || request.principal() == "" || strings.TrimSpace(request.Reason) == "" {
		return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: unlink digest, principal and reason are required", ErrInvalidOutcomeLink)
	}
	var target OutcomeLink
	found := false
	for _, link := range h.OutcomeLinks {
		if link.Digest == request.LinkDigest {
			if link.Action == OutcomeLinkActionUnlink {
				return OutcomeLinkHistory{}, OutcomeLink{}, ErrOutcomeAlreadyUnlinked
			}
			target, found = link, true
			break
		}
	}
	if !found {
		return OutcomeLinkHistory{}, OutcomeLink{}, ErrOutcomeLinkNotFound
	}
	if request.LinkingPrincipal != "" && request.PrincipalID != "" && request.LinkingPrincipal != request.PrincipalID {
		return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: unlink principal aliases differ", ErrInvalidOutcomeLink)
	}
	if err := request.EffectiveAt.Validate(); err != nil {
		return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: effective instant: %v", ErrInvalidOutcomeLink, err)
	}
	unlinked := OutcomeLink{RatingRef: target.RatingRef, RatingDigest: target.RatingRef.Digest, RatingRevision: target.RatingRef.Revision,
		Action: OutcomeLinkActionUnlink, OutcomeKind: target.OutcomeKind, OutcomeRef: target.OutcomeRef,
		LinkingPrincipal: request.principal(), PrincipalID: request.principal(), EffectiveAt: request.EffectiveAt, Reason: strings.TrimSpace(request.Reason)}
	unlinked, err := unlinked.withDigest()
	if err != nil {
		return OutcomeLinkHistory{}, OutcomeLink{}, err
	}
	next := OutcomeLinkHistory{OpinionRecords: append([]OpinionRecord(nil), h.OpinionRecords...), OutcomeLinks: append([]OutcomeLink(nil), h.OutcomeLinks...)}
	next.OutcomeLinks = append(next.OutcomeLinks, unlinked)
	next, err = next.withDigest()
	if err != nil {
		return OutcomeLinkHistory{}, OutcomeLink{}, err
	}
	return next, unlinked, nil
}

// UnlinkOutcome accepts either an OutcomeUnlinkRequest or the convenient
// (linkDigest, principal, effectiveAt, reason) form.
func (h OutcomeLinkHistory) UnlinkOutcome(input any, args ...any) (OutcomeLinkHistory, OutcomeLink, error) {
	var request OutcomeUnlinkRequest
	switch value := input.(type) {
	case OutcomeUnlinkRequest:
		if len(args) != 0 {
			return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: request cannot have positional arguments", ErrInvalidOutcomeLink)
		}
		request = value
	case OutcomeLink:
		if len(args) != 3 {
			return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: unlink needs principal, effective instant and reason", ErrInvalidOutcomeLink)
		}
		principal, ok1 := args[0].(string)
		at, ok2 := args[1].(values.Instant)
		reason, ok3 := args[2].(string)
		if !ok1 || !ok2 || !ok3 {
			return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: unlink arguments have invalid types", ErrInvalidOutcomeLink)
		}
		request = OutcomeUnlinkRequest{LinkDigest: value.Digest, LinkingPrincipal: principal, EffectiveAt: at, Reason: reason}
	case string:
		if len(args) != 3 {
			return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: unlink needs principal, effective instant and reason", ErrInvalidOutcomeLink)
		}
		principal, ok1 := args[0].(string)
		at, ok2 := args[1].(values.Instant)
		reason, ok3 := args[2].(string)
		if !ok1 || !ok2 || !ok3 {
			return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: unlink arguments have invalid types", ErrInvalidOutcomeLink)
		}
		request = OutcomeUnlinkRequest{LinkDigest: value, LinkingPrincipal: principal, EffectiveAt: at, Reason: reason}
	default:
		return OutcomeLinkHistory{}, OutcomeLink{}, fmt.Errorf("%w: unsupported unlink target %T", ErrInvalidOutcomeLink, input)
	}
	return h.unlinkOutcome(request)
}

func UnlinkOutcome(history OutcomeLinkHistory, input any, args ...any) (OutcomeLinkHistory, OutcomeLink, error) {
	return history.UnlinkOutcome(input, args...)
}

// History returns two independent ordered streams. Outcome additions and
// withdrawals only append to OutcomeLinks and therefore cannot alter an
// OpinionRecord digest.
func (h OutcomeLinkHistory) History() (OutcomeHistorySnapshot, error) {
	if err := h.Validate(); err != nil {
		return OutcomeHistorySnapshot{}, err
	}
	return OutcomeHistorySnapshot{OpinionRecords: append([]OpinionRecord(nil), h.OpinionRecords...), OutcomeLinks: append([]OutcomeLink(nil), h.OutcomeLinks...)}, nil
}

func (h OutcomeLinkHistory) Opinions() ([]OpinionRecord, error) {
	snapshot, err := h.History()
	return snapshot.OpinionRecords, err
}

func (h OutcomeLinkHistory) Links() ([]OutcomeLink, error) {
	snapshot, err := h.History()
	return snapshot.OutcomeLinks, err
}

type OutcomeHistoryExplanation struct {
	OpinionDigests     []string
	OutcomeLinkDigests []string
	OpinionCount       int
	OutcomeLinkCount   int
	Digest             string
}

func (h OutcomeLinkHistory) Explain() (OutcomeHistoryExplanation, error) {
	if err := h.Validate(); err != nil {
		return OutcomeHistoryExplanation{}, err
	}
	opinionDigests := make([]string, 0, len(h.OpinionRecords))
	for _, opinion := range h.OpinionRecords {
		opinionDigests = append(opinionDigests, opinion.Digest)
	}
	linkDigests := make([]string, 0, len(h.OutcomeLinks))
	for _, link := range h.OutcomeLinks {
		linkDigests = append(linkDigests, link.Digest)
	}
	return OutcomeHistoryExplanation{OpinionDigests: opinionDigests, OutcomeLinkDigests: linkDigests, OpinionCount: len(opinionDigests), OutcomeLinkCount: len(linkDigests), Digest: h.CanonicalDigest}, nil
}

func ExplainOutcomeHistory(h OutcomeLinkHistory) (OutcomeHistoryExplanation, error) {
	return h.Explain()
}

// OutcomeKinds returns the closed vocabulary in stable order.
func OutcomeKinds() []OutcomeKind {
	result := []OutcomeKind{OutcomeKindCompensationChangeProposal, OutcomeKindPromotionIntent, OutcomeKindDevelopmentPlan, OutcomeKindRetentionFlag}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
