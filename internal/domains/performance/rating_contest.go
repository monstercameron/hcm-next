package performance

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrInvalidContest           = errors.New("performance: invalid rating contest")
	ErrContestWindowClosed      = errors.New("performance: rating contest window is closed")
	ErrContestAlreadyRaised     = errors.New("performance: rating contest already raised")
	ErrContestAfterFinalization = errors.New("performance: rating contest is not allowed after finalization")
	ErrInvalidCorrection        = errors.New("performance: invalid rating correction")
	ErrCorrectionAlreadyDecided = errors.New("performance: rating correction already decided")
	ErrCorrectionReviewer       = errors.New("performance: correction reviewer is not eligible")
	ErrCorrectionSeparation     = errors.New("performance: correction reviewer violates separation of duties")
	ErrCorrectionDecision       = errors.New("performance: invalid correction decision")
	ErrContestOpen              = errors.New("performance: rating contest remains open")
	ErrFinalizationBeforeCutoff = errors.New("performance: rating cannot be finalized before the phase cutoff")
	ErrRatingAlreadyFinalized   = errors.New("performance: rating is already finalized")
	ErrInvalidFinalRating       = errors.New("performance: invalid final rating")
)

// ContestReasonCode is the closed vocabulary for a participant's contest.
type ContestReasonCode string

const (
	ContestReasonEvidence          ContestReasonCode = "EVIDENCE"
	ContestReasonProcess           ContestReasonCode = "PROCESS"
	ContestReasonBias              ContestReasonCode = "BIAS"
	ContestReasonDataQuality       ContestReasonCode = "DATA_QUALITY"
	ContestReasonPolicy            ContestReasonCode = "POLICY"
	RatingContestReasonEvidence                      = ContestReasonEvidence
	RatingContestReasonProcess                       = ContestReasonProcess
	RatingContestReasonBias                          = ContestReasonBias
	RatingContestReasonDataQuality                   = ContestReasonDataQuality
	RatingContestReasonPolicy                        = ContestReasonPolicy
)

type RatingContestReasonCode = ContestReasonCode

func (r ContestReasonCode) Valid() bool {
	switch r {
	case ContestReasonEvidence, ContestReasonProcess, ContestReasonBias, ContestReasonDataQuality, ContestReasonPolicy:
		return true
	default:
		return false
	}
}

// CorrectionReasonCode is a closed vocabulary for a reviewer's correction
// decision. A correction reason is never a substitute for the narrative
// digest or the evidence references that support it.
type CorrectionReasonCode string

const (
	CorrectionReasonEvidence          CorrectionReasonCode = "EVIDENCE"
	CorrectionReasonProcess           CorrectionReasonCode = "PROCESS"
	CorrectionReasonBias              CorrectionReasonCode = "BIAS"
	CorrectionReasonDataQuality       CorrectionReasonCode = "DATA_QUALITY"
	CorrectionReasonPolicy            CorrectionReasonCode = "POLICY"
	RatingCorrectionReasonEvidence                         = CorrectionReasonEvidence
	RatingCorrectionReasonProcess                          = CorrectionReasonProcess
	RatingCorrectionReasonBias                             = CorrectionReasonBias
	RatingCorrectionReasonDataQuality                      = CorrectionReasonDataQuality
	RatingCorrectionReasonPolicy                           = CorrectionReasonPolicy
)

type RatingCorrectionReasonCode = CorrectionReasonCode

func (r CorrectionReasonCode) Valid() bool {
	switch r {
	case CorrectionReasonEvidence, CorrectionReasonProcess, CorrectionReasonBias, CorrectionReasonDataQuality, CorrectionReasonPolicy:
		return true
	default:
		return false
	}
}

type CorrectionDecision string

const (
	CorrectionDecisionUphold  CorrectionDecision = "UPHOLD"
	CorrectionDecisionCorrect CorrectionDecision = "CORRECT"
	CorrectionDecisionRefer   CorrectionDecision = "REFER"
	CorrectionUphold                             = CorrectionDecisionUphold
	CorrectionCorrect                            = CorrectionDecisionCorrect
	CorrectionRefer                              = CorrectionDecisionRefer
)

func (d CorrectionDecision) Valid() bool {
	return d == CorrectionDecisionUphold || d == CorrectionDecisionCorrect || d == CorrectionDecisionRefer
}

// RatingContestWindow is half-open: OpensAt <= RaisedAt < ClosesAt.
type RatingContestWindow struct {
	OpensAt  values.Instant
	ClosesAt values.Instant
}

type ContestWindow = RatingContestWindow

func NewRatingContestWindow(opensAt, closesAt values.Instant) (RatingContestWindow, error) {
	w := RatingContestWindow{OpensAt: opensAt, ClosesAt: closesAt}
	if err := w.Validate(); err != nil {
		return RatingContestWindow{}, err
	}
	return w, nil
}

func (w RatingContestWindow) Validate() error {
	if err := w.OpensAt.Validate(); err != nil {
		return fmt.Errorf("%w: contest window opens_at: %v", ErrInvalidContest, err)
	}
	if err := w.ClosesAt.Validate(); err != nil {
		return fmt.Errorf("%w: contest window closes_at: %v", ErrInvalidContest, err)
	}
	if !w.OpensAt.Before(w.ClosesAt) {
		return fmt.Errorf("%w: contest window must have a positive duration", ErrInvalidContest)
	}
	return nil
}

func (w RatingContestWindow) contains(at values.Instant) bool {
	return !at.Before(w.OpensAt) && at.Before(w.ClosesAt)
}

func (w RatingContestWindow) Canonical() []byte {
	if w.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.performance.RatingContestWindow", 1).
		Value("opens_at", w.OpensAt).Value("closes_at", w.ClosesAt).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// RatingContest is the participant's append-only challenge. Narrative is
// deliberately represented only by a digest; the domain never stores raw
// contest text.
type RatingContest struct {
	ID                     string
	ParticipantID          string
	CalibratedRatingDigest string
	Reason                 ContestReasonCode
	NarrativeDigest        string
	EvidenceRefs           []EvidenceRef
	RaisedAt               values.Instant
	Digest                 string
}

type Contest = RatingContest

func NewRatingContest(id, participantID string, reason ContestReasonCode, narrativeDigest string, evidenceRefs []EvidenceRef, raisedAt values.Instant) (RatingContest, error) {
	c := RatingContest{ID: id, ParticipantID: participantID, Reason: reason, NarrativeDigest: narrativeDigest, EvidenceRefs: cloneEvidenceRefs(evidenceRefs), RaisedAt: raisedAt}
	return c.withDigest()
}

func NewContest(id, participantID string, reason ContestReasonCode, narrativeDigest string, evidenceRefs []EvidenceRef, raisedAt values.Instant) (RatingContest, error) {
	return NewRatingContest(id, participantID, reason, narrativeDigest, evidenceRefs, raisedAt)
}

func (c RatingContest) Validate() error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.ParticipantID) == "" {
		return fmt.Errorf("%w: id and participant are required", ErrInvalidContest)
	}
	if !c.Reason.Valid() {
		return fmt.Errorf("%w: unknown reason code %q", ErrInvalidContest, c.Reason)
	}
	if !strings.HasPrefix(c.NarrativeDigest, canonicalbytes.DigestAlgorithm+":") || len(c.NarrativeDigest) <= len(canonicalbytes.DigestAlgorithm)+1 {
		return fmt.Errorf("%w: narrative must be a digest", ErrInvalidContest)
	}
	if err := c.RaisedAt.Validate(); err != nil {
		return fmt.Errorf("%w: raised_at: %v", ErrInvalidContest, err)
	}
	if len(c.EvidenceRefs) == 0 {
		return fmt.Errorf("%w: at least one evidence reference is required", ErrInvalidContest)
	}
	if err := validateEvidenceRefs(c.EvidenceRefs); err != nil {
		return fmt.Errorf("%w: evidence: %v", ErrInvalidContest, err)
	}
	if c.Digest == "" || c.Digest != c.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidContest)
	}
	return nil
}

func (c RatingContest) body() []byte {
	if err := c.validateWithoutDigest(); err != nil {
		return nil
	}
	evidence := sortedEvidenceRefs(c.EvidenceRefs)
	w := canonicalbytes.New("hcmnext.domains.performance.RatingContest", 1).
		String("id", c.ID).String("participant_id", c.ParticipantID).
		String("calibrated_rating_digest", c.CalibratedRatingDigest).
		String("reason", string(c.Reason)).String("narrative_digest", c.NarrativeDigest).
		Value("raised_at", c.RaisedAt).Count("evidence", len(evidence))
	for _, ref := range evidence {
		w.Value("evidence_ref", ref)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (c RatingContest) validateWithoutDigest() error {
	copyOf := c
	copyOf.Digest = ""
	if strings.TrimSpace(copyOf.ID) == "" || strings.TrimSpace(copyOf.ParticipantID) == "" || !copyOf.Reason.Valid() {
		return ErrInvalidContest
	}
	if !strings.HasPrefix(copyOf.NarrativeDigest, canonicalbytes.DigestAlgorithm+":") || len(copyOf.NarrativeDigest) <= len(canonicalbytes.DigestAlgorithm)+1 {
		return ErrInvalidContest
	}
	if err := copyOf.RaisedAt.Validate(); err != nil {
		return err
	}
	if len(copyOf.EvidenceRefs) == 0 {
		return ErrInvalidContest
	}
	return validateEvidenceRefs(copyOf.EvidenceRefs)
}

func (c RatingContest) computedDigest() string { return canonicalbytes.Digest(c.body()) }

func (c RatingContest) withDigest() (RatingContest, error) {
	provided := c.Digest
	c.Digest = ""
	if err := c.validateWithoutDigest(); err != nil {
		return RatingContest{}, fmt.Errorf("%w: %v", ErrInvalidContest, err)
	}
	c.Digest = c.computedDigest()
	if provided != "" && provided != c.Digest {
		return RatingContest{}, fmt.Errorf("%w: digest mismatch", ErrInvalidContest)
	}
	return c, nil
}

func (c RatingContest) Canonical() []byte { return c.body() }

// RatingCorrection records the independent review path. REFER intentionally
// has no corrected rating because it leaves the contest unresolved.
type RatingCorrection struct {
	ContestDigest   string
	ReviewerID      string
	Decision        CorrectionDecision
	CorrectedRating values.Decimal
	Reason          CorrectionReasonCode
	EvidenceRefs    []EvidenceRef
	DecidedAt       values.Instant
	Digest          string
}

type Correction = RatingCorrection

func NewRatingCorrection(contestDigest, reviewerID string, decision CorrectionDecision, correctedRating values.Decimal, reason CorrectionReasonCode, evidenceRefs []EvidenceRef, decidedAt values.Instant) (RatingCorrection, error) {
	c := RatingCorrection{ContestDigest: contestDigest, ReviewerID: reviewerID, Decision: decision, CorrectedRating: correctedRating, Reason: reason, EvidenceRefs: cloneEvidenceRefs(evidenceRefs), DecidedAt: decidedAt}
	return c.withDigest()
}

func NewCorrection(contestDigest, reviewerID string, decision CorrectionDecision, correctedRating values.Decimal, reason CorrectionReasonCode, evidenceRefs []EvidenceRef, decidedAt values.Instant) (RatingCorrection, error) {
	return NewRatingCorrection(contestDigest, reviewerID, decision, correctedRating, reason, evidenceRefs, decidedAt)
}

func (c RatingCorrection) Validate() error {
	if err := c.validateWithoutDigest(); err != nil {
		return err
	}
	if c.Digest == "" || c.Digest != c.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidCorrection)
	}
	return nil
}

func (c RatingCorrection) validateWithoutDigest() error {
	if strings.TrimSpace(c.ContestDigest) == "" || strings.TrimSpace(c.ReviewerID) == "" {
		return fmt.Errorf("%w: contest and reviewer are required", ErrInvalidCorrection)
	}
	if !c.Decision.Valid() {
		return fmt.Errorf("%w: unknown decision %q", ErrInvalidCorrection, c.Decision)
	}
	if !c.Reason.Valid() {
		return fmt.Errorf("%w: unknown reason code %q", ErrInvalidCorrection, c.Reason)
	}
	if err := c.DecidedAt.Validate(); err != nil {
		return fmt.Errorf("%w: decided_at: %v", ErrInvalidCorrection, err)
	}
	if len(c.EvidenceRefs) == 0 {
		return fmt.Errorf("%w: at least one evidence reference is required", ErrInvalidCorrection)
	}
	if err := validateEvidenceRefs(c.EvidenceRefs); err != nil {
		return fmt.Errorf("%w: evidence: %v", ErrInvalidCorrection, err)
	}
	if c.Decision == CorrectionDecisionCorrect {
		if err := c.CorrectedRating.Validate(); err != nil {
			return fmt.Errorf("%w: corrected rating: %v", ErrInvalidCorrection, err)
		}
	}
	return nil
}

func (c RatingCorrection) body() []byte {
	if err := c.validateWithoutDigest(); err != nil {
		return nil
	}
	evidence := sortedEvidenceRefs(c.EvidenceRefs)
	w := canonicalbytes.New("hcmnext.domains.performance.RatingCorrection", 1).
		String("contest_digest", c.ContestDigest).String("reviewer_id", c.ReviewerID).
		String("decision", string(c.Decision)).String("reason", string(c.Reason)).
		Value("decided_at", c.DecidedAt).Count("evidence", len(evidence))
	if c.Decision == CorrectionDecisionCorrect {
		w.Value("corrected_rating", c.CorrectedRating)
	}
	for _, ref := range evidence {
		w.Value("evidence_ref", ref)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (c RatingCorrection) computedDigest() string { return canonicalbytes.Digest(c.body()) }

func (c RatingCorrection) withDigest() (RatingCorrection, error) {
	provided := c.Digest
	c.Digest = ""
	if err := c.validateWithoutDigest(); err != nil {
		return RatingCorrection{}, err
	}
	c.Digest = c.computedDigest()
	if provided != "" && provided != c.Digest {
		return RatingCorrection{}, fmt.Errorf("%w: digest mismatch", ErrInvalidCorrection)
	}
	return c, nil
}

func (c RatingCorrection) Canonical() []byte { return c.body() }

type RatingEventKind string

const (
	RatingEventContestRaised     RatingEventKind = "CONTEST_RAISED"
	RatingEventCorrectionDecided RatingEventKind = "CORRECTION_DECIDED"
	RatingEventFinalized         RatingEventKind = "FINALIZED"
	ContestRaisedEvent                           = RatingEventContestRaised
	CorrectionDecidedEvent                       = RatingEventCorrectionDecided
	RatingFinalizedEvent                         = RatingEventFinalized
)

// RatingTransitionEvent is the append-only event for one state transition.
// Its kind is closed and its digest covers the exact transition references.
type RatingTransitionEvent struct {
	Kind             RatingEventKind
	CaseID           string
	ParticipantID    string
	ActorID          string
	At               values.Instant
	ContestDigest    string
	CorrectionDigest string
	Digest           string
}

type RatingEvent = RatingTransitionEvent

func (e RatingTransitionEvent) Validate() error {
	if strings.TrimSpace(e.CaseID) == "" || strings.TrimSpace(e.ParticipantID) == "" || strings.TrimSpace(e.ActorID) == "" {
		return fmt.Errorf("%w: event identity is required", ErrInvalidFinalRating)
	}
	if e.Kind != RatingEventContestRaised && e.Kind != RatingEventCorrectionDecided && e.Kind != RatingEventFinalized {
		return fmt.Errorf("%w: unknown event kind %q", ErrInvalidFinalRating, e.Kind)
	}
	if err := e.At.Validate(); err != nil {
		return fmt.Errorf("%w: event time: %v", ErrInvalidFinalRating, err)
	}
	if e.Digest == "" || e.Digest != e.computedDigest() {
		return fmt.Errorf("%w: event digest mismatch", ErrInvalidFinalRating)
	}
	return nil
}

func (e RatingTransitionEvent) body() []byte {
	if strings.TrimSpace(e.CaseID) == "" || strings.TrimSpace(e.ParticipantID) == "" || strings.TrimSpace(e.ActorID) == "" || e.At.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.performance.RatingTransitionEvent", 1).
		String("kind", string(e.Kind)).String("case_id", e.CaseID).String("participant_id", e.ParticipantID).
		String("actor_id", e.ActorID).Value("at", e.At).
		String("contest_digest", e.ContestDigest).String("correction_digest", e.CorrectionDigest).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (e RatingTransitionEvent) computedDigest() string { return canonicalbytes.Digest(e.body()) }

func newRatingEvent(kind RatingEventKind, caseID, participantID, actorID string, at values.Instant, contestDigest, correctionDigest string) (RatingTransitionEvent, error) {
	e := RatingTransitionEvent{Kind: kind, CaseID: caseID, ParticipantID: participantID, ActorID: actorID, At: at, ContestDigest: contestDigest, CorrectionDigest: correctionDigest}
	e.Digest = e.computedDigest()
	if err := e.Validate(); err != nil {
		return RatingTransitionEvent{}, err
	}
	return e, nil
}

func (e RatingTransitionEvent) Canonical() []byte { return e.body() }

// FinalRating is the immutable terminal record. It retains the proposed and
// calibrated values, the contest and correction (when present), and every
// transition event used to reach finalization.
type FinalRating struct {
	ParticipantID     string
	ProposedRating    ProposedRating
	CalibratedRating  FinalCalibratedRating
	Contest           RatingContest
	Contested         bool
	Correction        RatingCorrection
	CorrectionDecided bool
	Corrected         bool
	FinalRating       values.Decimal
	FinalizedAt       values.Instant
	Events            []RatingTransitionEvent
	CanonicalDigest   string
}

func (r FinalRating) Validate() error {
	if strings.TrimSpace(r.ParticipantID) == "" {
		return fmt.Errorf("%w: participant is required", ErrInvalidFinalRating)
	}
	if err := r.ProposedRating.Validate(); err != nil {
		return fmt.Errorf("%w: proposed rating: %v", ErrInvalidFinalRating, err)
	}
	if err := r.CalibratedRating.Validate(); err != nil {
		return fmt.Errorf("%w: calibrated rating: %v", ErrInvalidFinalRating, err)
	}
	if r.ParticipantID != r.ProposedRating.ParticipantID || r.ParticipantID != r.CalibratedRating.ParticipantID {
		return fmt.Errorf("%w: participant binding mismatch", ErrInvalidFinalRating)
	}
	if err := r.FinalRating.Validate(); err != nil {
		return fmt.Errorf("%w: final value: %v", ErrInvalidFinalRating, err)
	}
	if r.FinalRating.Scale() != r.CalibratedRating.FinalRating.Scale() {
		return fmt.Errorf("%w: final value scale mismatch", ErrInvalidFinalRating)
	}
	if err := r.FinalizedAt.Validate(); err != nil {
		return fmt.Errorf("%w: finalized_at: %v", ErrInvalidFinalRating, err)
	}
	if r.Contested {
		if err := r.Contest.Validate(); err != nil {
			return err
		}
		if r.Contest.ParticipantID != r.ParticipantID {
			return fmt.Errorf("%w: contest participant mismatch", ErrInvalidFinalRating)
		}
	}
	if r.CorrectionDecided {
		if !r.Contested {
			return fmt.Errorf("%w: correction requires a contest", ErrInvalidFinalRating)
		}
		if err := r.Correction.Validate(); err != nil {
			return err
		}
		if r.Correction.ContestDigest != r.Contest.Digest || r.Correction.Decision == CorrectionDecisionRefer {
			return fmt.Errorf("%w: correction does not resolve contest", ErrInvalidFinalRating)
		}
		if r.Corrected != (r.Correction.Decision == CorrectionDecisionCorrect) {
			return fmt.Errorf("%w: correction state mismatch", ErrInvalidFinalRating)
		}
		if r.Correction.Decision == CorrectionDecisionCorrect && !r.FinalRating.Equal(r.Correction.CorrectedRating) {
			return fmt.Errorf("%w: corrected value mismatch", ErrInvalidFinalRating)
		}
	}
	if r.Corrected && !r.CorrectionDecided {
		return fmt.Errorf("%w: corrected value has no correction decision", ErrInvalidFinalRating)
	}
	if !r.Corrected && !r.FinalRating.Equal(r.CalibratedRating.FinalRating) {
		return fmt.Errorf("%w: uncorrected value differs from calibrated rating", ErrInvalidFinalRating)
	}
	for _, event := range r.Events {
		if err := event.Validate(); err != nil {
			return err
		}
	}
	if r.CanonicalDigest == "" || r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidFinalRating)
	}
	return nil
}

func (r FinalRating) body() []byte {
	if strings.TrimSpace(r.ParticipantID) == "" || r.ProposedRating.CanonicalDigest == "" || r.CalibratedRating.CanonicalDigest == "" || r.FinalRating.Validate() != nil || r.FinalizedAt.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.performance.FinalRating", 1).
		String("participant_id", r.ParticipantID).String("proposed_digest", r.ProposedRating.CanonicalDigest).
		String("calibrated_digest", r.CalibratedRating.CanonicalDigest).Bool("contested", r.Contested).
		Bool("corrected", r.Corrected).Value("final_rating", r.FinalRating).Value("finalized_at", r.FinalizedAt)
	if r.Contested {
		w.String("contest_digest", r.Contest.Digest)
	}
	if r.CorrectionDecided {
		w.String("correction_digest", r.Correction.Digest)
	}
	w.Count("events", len(r.Events))
	for _, event := range r.Events {
		w.String("event", event.Digest)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r FinalRating) computedDigest() string { return canonicalbytes.Digest(r.body()) }

func (r FinalRating) Explain() (FinalRatingExplanation, error) {
	if err := r.Validate(); err != nil {
		return FinalRatingExplanation{}, err
	}
	events := make([]string, 0, len(r.Events))
	for _, event := range r.Events {
		events = append(events, event.Digest)
	}
	return FinalRatingExplanation{ParticipantID: r.ParticipantID, ProposedRating: r.ProposedRating.Rating, CalibratedRating: r.CalibratedRating.FinalRating, FinalRating: r.FinalRating, Contested: r.Contested, Corrected: r.Corrected, ContestDigest: contestDigest(r.Contested, r.Contest), CorrectionDigest: correctionDigest(r.CorrectionDecided, r.Correction), EventDigests: events, Digest: r.CanonicalDigest}, nil
}

type FinalRatingExplanation struct {
	ParticipantID    string
	ProposedRating   values.Decimal
	CalibratedRating values.Decimal
	FinalRating      values.Decimal
	Contested        bool
	Corrected        bool
	ContestDigest    string
	CorrectionDigest string
	EventDigests     []string
	Digest           string
}

func ExplainFinalRating(r FinalRating) (FinalRatingExplanation, error) { return r.Explain() }

// RatingCase is an immutable-by-value contest/correction state machine. The
// manager ID is retained as an authority binding because a calibrated rating
// intentionally does not expose the frozen reviewer graph.
type RatingCase struct {
	CaseID               string
	ParticipantID        string
	ParticipantManagerID string
	CalibratedRating     FinalCalibratedRating
	ContestWindow        RatingContestWindow
	PhaseCutoff          values.Instant
	Contest              RatingContest
	Contested            bool
	Correction           RatingCorrection
	CorrectionDecided    bool
	Corrected            bool
	Finalized            bool
	FinalRating          FinalRating
	Events               []RatingTransitionEvent
	CanonicalDigest      string
}

type RatingResolution = RatingCase
type RatingFinalization = RatingCase

// managerID is variadic for compatibility with callers that have already
// bound the manager in a separate authority record. The value is mandatory
// when a case is created so the correction separation rule is enforceable.
func NewRatingCase(caseID string, calibrated FinalCalibratedRating, window RatingContestWindow, phaseCutoff values.Instant, managerID ...string) (RatingCase, error) {
	manager := ""
	if len(managerID) > 0 {
		manager = strings.TrimSpace(managerID[0])
	}
	c := RatingCase{CaseID: caseID, ParticipantID: calibrated.ParticipantID, ParticipantManagerID: manager, CalibratedRating: calibrated, ContestWindow: window, PhaseCutoff: phaseCutoff, Events: []RatingTransitionEvent{}}
	c.CanonicalDigest = c.computedDigest()
	if err := c.Validate(); err != nil {
		return RatingCase{}, err
	}
	return c, nil
}

func NewRatingResolution(caseID string, calibrated FinalCalibratedRating, window RatingContestWindow, phaseCutoff values.Instant, managerID ...string) (RatingResolution, error) {
	return NewRatingCase(caseID, calibrated, window, phaseCutoff, managerID...)
}

func NewRatingFinalization(caseID string, calibrated FinalCalibratedRating, window RatingContestWindow, phaseCutoff values.Instant, managerID ...string) (RatingFinalization, error) {
	return NewRatingCase(caseID, calibrated, window, phaseCutoff, managerID...)
}

func NewRatingCaseFromSession(caseID string, session CalibrationSession, participantID string, window RatingContestWindow, phaseCutoff values.Instant) (RatingCase, error) {
	calibrated, err := session.FinalizeParticipant(participantID)
	if err != nil {
		return RatingCase{}, err
	}
	return NewRatingCase(caseID, calibrated, window, phaseCutoff, session.managerFor(participantID))
}

func (c RatingCase) Validate() error {
	if strings.TrimSpace(c.CaseID) == "" || strings.TrimSpace(c.ParticipantID) == "" || strings.TrimSpace(c.ParticipantManagerID) == "" {
		return fmt.Errorf("%w: case, participant and manager are required", ErrInvalidFinalRating)
	}
	if err := c.CalibratedRating.Validate(); err != nil {
		return fmt.Errorf("%w: calibrated rating: %v", ErrInvalidFinalRating, err)
	}
	if c.CalibratedRating.ParticipantID != c.ParticipantID {
		return fmt.Errorf("%w: calibrated participant mismatch", ErrInvalidFinalRating)
	}
	if err := c.ContestWindow.Validate(); err != nil {
		return err
	}
	if err := c.PhaseCutoff.Validate(); err != nil {
		return fmt.Errorf("%w: phase cutoff: %v", ErrInvalidFinalRating, err)
	}
	if c.PhaseCutoff.Before(c.ContestWindow.ClosesAt) {
		return fmt.Errorf("%w: phase cutoff precedes contest close", ErrInvalidFinalRating)
	}
	if c.Contested {
		if err := c.Contest.Validate(); err != nil {
			return err
		}
		if c.Contest.ParticipantID != c.ParticipantID || c.Contest.CalibratedRatingDigest != c.CalibratedRating.CanonicalDigest {
			return fmt.Errorf("%w: contest binding mismatch", ErrInvalidFinalRating)
		}
	}
	if c.CorrectionDecided {
		if !c.Contested {
			return fmt.Errorf("%w: correction requires contest", ErrInvalidFinalRating)
		}
		if err := c.Correction.Validate(); err != nil {
			return err
		}
		if c.Correction.ContestDigest != c.Contest.Digest {
			return fmt.Errorf("%w: correction does not resolve contest", ErrInvalidFinalRating)
		}
		if c.Corrected != (c.Correction.Decision == CorrectionDecisionCorrect) {
			return fmt.Errorf("%w: correction state mismatch", ErrInvalidFinalRating)
		}
	}
	if c.Finalized {
		if err := c.FinalRating.Validate(); err != nil {
			return err
		}
		if c.FinalRating.ParticipantID != c.ParticipantID {
			return fmt.Errorf("%w: final participant mismatch", ErrInvalidFinalRating)
		}
	}
	for _, event := range c.Events {
		if err := event.Validate(); err != nil {
			return err
		}
		if event.CaseID != c.CaseID || event.ParticipantID != c.ParticipantID {
			return fmt.Errorf("%w: event binding mismatch", ErrInvalidFinalRating)
		}
	}
	if c.CanonicalDigest == "" || c.CanonicalDigest != c.computedDigest() {
		return fmt.Errorf("%w: case digest mismatch", ErrInvalidFinalRating)
	}
	return nil
}

func (c RatingCase) body() []byte {
	if strings.TrimSpace(c.CaseID) == "" || strings.TrimSpace(c.ParticipantID) == "" || c.CalibratedRating.CanonicalDigest == "" || c.ContestWindow.Canonical() == nil || c.PhaseCutoff.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.performance.RatingCase", 1).
		String("case_id", c.CaseID).String("participant_id", c.ParticipantID).String("manager_id", c.ParticipantManagerID).
		String("calibrated_digest", c.CalibratedRating.CanonicalDigest).Value("contest_window", c.ContestWindow).
		Value("phase_cutoff", c.PhaseCutoff).Bool("contested", c.Contested).Bool("corrected", c.Corrected).Bool("finalized", c.Finalized)
	if c.Contested {
		w.String("contest", c.Contest.Digest)
	}
	if c.CorrectionDecided {
		w.String("correction", c.Correction.Digest)
	}
	if c.Finalized {
		w.String("final", c.FinalRating.CanonicalDigest)
	}
	w.Count("events", len(c.Events))
	for _, event := range c.Events {
		w.String("event", event.Digest)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (c RatingCase) computedDigest() string { return canonicalbytes.Digest(c.body()) }

func (c RatingCase) RaiseContest(contest RatingContest) (RatingCase, error) {
	if err := c.Validate(); err != nil {
		return RatingCase{}, err
	}
	if c.Finalized {
		return RatingCase{}, ErrContestAfterFinalization
	}
	if c.Contested {
		return RatingCase{}, ErrContestAlreadyRaised
	}
	if contest.ParticipantID != c.ParticipantID || !c.ContestWindow.contains(contest.RaisedAt) {
		return RatingCase{}, ErrContestWindowClosed
	}
	contest.CalibratedRatingDigest = c.CalibratedRating.CanonicalDigest
	// The standalone constructor cannot know the calibrated digest. Rebind the
	// participant submission to this case before deriving its authoritative
	// digest.
	contest.Digest = ""
	normalized, err := contest.withDigest()
	if err != nil {
		return RatingCase{}, err
	}
	event, err := newRatingEvent(RatingEventContestRaised, c.CaseID, c.ParticipantID, c.ParticipantID, normalized.RaisedAt, normalized.Digest, "")
	if err != nil {
		return RatingCase{}, err
	}
	next := c
	next.Contest = normalized
	next.Contested = true
	next.Events = append([]RatingTransitionEvent(nil), c.Events...)
	next.Events = append(next.Events, event)
	next.CanonicalDigest = next.computedDigest()
	if err := next.Validate(); err != nil {
		return RatingCase{}, err
	}
	return next, nil
}

func RaiseContest(c RatingCase, contest RatingContest) (RatingCase, error) {
	return c.RaiseContest(contest)
}

func (c RatingCase) DecideCorrection(correction RatingCorrection) (RatingCase, error) {
	if err := c.Validate(); err != nil {
		return RatingCase{}, err
	}
	if c.Finalized {
		return RatingCase{}, ErrRatingAlreadyFinalized
	}
	if !c.Contested {
		return RatingCase{}, ErrContestOpen
	}
	if c.CorrectionDecided {
		return RatingCase{}, ErrCorrectionAlreadyDecided
	}
	correction.ContestDigest = c.Contest.Digest
	// As with contests, the aggregate supplies the authoritative parent digest
	// and then derives the transition record's digest from the bound value.
	correction.Digest = ""
	normalized, err := correction.withDigest()
	if err != nil {
		return RatingCase{}, err
	}
	if normalized.DecidedAt.Before(c.Contest.RaisedAt) || normalized.DecidedAt.After(c.PhaseCutoff) {
		return RatingCase{}, ErrInvalidCorrection
	}
	if normalized.ReviewerID == c.ParticipantManagerID || normalized.ReviewerID == c.ParticipantID {
		return RatingCase{}, ErrCorrectionSeparation
	}
	for _, adjustment := range c.CalibratedRating.Adjustments {
		if adjustment.AdjusterID == normalized.ReviewerID {
			return RatingCase{}, ErrCorrectionSeparation
		}
	}
	if normalized.Decision == CorrectionDecisionCorrect {
		if normalized.CorrectedRating.Scale() != c.CalibratedRating.FinalRating.Scale() {
			return RatingCase{}, ErrInvalidCorrection
		}
	}
	event, err := newRatingEvent(RatingEventCorrectionDecided, c.CaseID, c.ParticipantID, normalized.ReviewerID, normalized.DecidedAt, c.Contest.Digest, normalized.Digest)
	if err != nil {
		return RatingCase{}, err
	}
	next := c
	next.Correction = normalized
	next.CorrectionDecided = true
	next.Corrected = normalized.Decision == CorrectionDecisionCorrect
	next.Events = append([]RatingTransitionEvent(nil), c.Events...)
	next.Events = append(next.Events, event)
	next.CanonicalDigest = next.computedDigest()
	if err := next.Validate(); err != nil {
		return RatingCase{}, err
	}
	return next, nil
}

func DecideCorrection(c RatingCase, correction RatingCorrection) (RatingCase, error) {
	return c.DecideCorrection(correction)
}

func (c RatingCase) finalize(at values.Instant) (RatingCase, FinalRating, error) {
	if err := c.Validate(); err != nil {
		return RatingCase{}, FinalRating{}, err
	}
	if c.Finalized {
		return RatingCase{}, FinalRating{}, ErrRatingAlreadyFinalized
	}
	if err := at.Validate(); err != nil {
		return RatingCase{}, FinalRating{}, fmt.Errorf("%w: finalization time: %v", ErrInvalidFinalRating, err)
	}
	if at.Before(c.PhaseCutoff) {
		return RatingCase{}, FinalRating{}, ErrFinalizationBeforeCutoff
	}
	if c.Contested && (!c.CorrectionDecided || c.Correction.Decision == CorrectionDecisionRefer) {
		return RatingCase{}, FinalRating{}, ErrContestOpen
	}
	finalValue := c.CalibratedRating.FinalRating
	if c.Corrected && c.Correction.Decision == CorrectionDecisionCorrect {
		finalValue = c.Correction.CorrectedRating
	}
	final := FinalRating{ParticipantID: c.ParticipantID, ProposedRating: c.CalibratedRating.ProposedRating, CalibratedRating: c.CalibratedRating, Contest: c.Contest, Contested: c.Contested, Correction: c.Correction, CorrectionDecided: c.CorrectionDecided, Corrected: c.Corrected, FinalRating: finalValue, FinalizedAt: at, Events: append([]RatingTransitionEvent(nil), c.Events...)}
	event, err := newRatingEvent(RatingEventFinalized, c.CaseID, c.ParticipantID, c.ParticipantManagerID, at, contestDigest(c.Contested, c.Contest), correctionDigest(c.CorrectionDecided, c.Correction))
	if err != nil {
		return RatingCase{}, FinalRating{}, err
	}
	final.Events = append(final.Events, event)
	final.CanonicalDigest = final.computedDigest()
	if err := final.Validate(); err != nil {
		return RatingCase{}, FinalRating{}, err
	}
	next := c
	next.Finalized = true
	next.FinalRating = final
	next.Events = append([]RatingTransitionEvent(nil), final.Events...)
	next.CanonicalDigest = next.computedDigest()
	if err := next.Validate(); err != nil {
		return RatingCase{}, FinalRating{}, err
	}
	return next, final, nil
}

func (c RatingCase) Finalize(at values.Instant) (FinalRating, error) {
	_, final, err := c.finalize(at)
	return final, err
}

func FinalizeRatingCase(c RatingCase, at values.Instant) (RatingCase, FinalRating, error) {
	return c.finalize(at)
}

func (r FinalRating) RaiseContest(contest RatingContest) (FinalRating, error) {
	return FinalRating{}, ErrContestAfterFinalization
}

func contestDigest(present bool, contest RatingContest) string {
	if !present {
		return ""
	}
	return contest.Digest
}

func correctionDigest(present bool, correction RatingCorrection) string {
	if !present {
		return ""
	}
	return correction.Digest
}

type RatingCaseExplanation struct {
	CaseID               string
	ParticipantID        string
	ParticipantManagerID string
	ContestWindow        RatingContestWindow
	PhaseCutoff          values.Instant
	Contested            bool
	Corrected            bool
	Finalized            bool
	ContestDigest        string
	CorrectionDigest     string
	FinalDigest          string
	EventDigests         []string
	Digest               string
}

func (c RatingCase) Explain() (RatingCaseExplanation, error) {
	if err := c.Validate(); err != nil {
		return RatingCaseExplanation{}, err
	}
	events := make([]string, 0, len(c.Events))
	for _, event := range c.Events {
		events = append(events, event.Digest)
	}
	finalDigest := ""
	if c.Finalized {
		finalDigest = c.FinalRating.CanonicalDigest
	}
	return RatingCaseExplanation{CaseID: c.CaseID, ParticipantID: c.ParticipantID, ParticipantManagerID: c.ParticipantManagerID, ContestWindow: c.ContestWindow, PhaseCutoff: c.PhaseCutoff, Contested: c.Contested, Corrected: c.Corrected, Finalized: c.Finalized, ContestDigest: contestDigest(c.Contested, c.Contest), CorrectionDigest: correctionDigest(c.CorrectionDecided, c.Correction), FinalDigest: finalDigest, EventDigests: events, Digest: c.CanonicalDigest}, nil
}

func ExplainRatingCase(c RatingCase) (RatingCaseExplanation, error) { return c.Explain() }

func cloneEvidenceRefs(input []EvidenceRef) []EvidenceRef {
	return append([]EvidenceRef(nil), input...)
}

func validateEvidenceRefs(refs []EvidenceRef) error {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return err
		}
		key := ref.ID + "\x00" + ref.Version + "\x00" + ref.Digest
		if _, exists := seen[key]; exists {
			return errors.New("duplicate evidence reference")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func sortedEvidenceRefs(input []EvidenceRef) []EvidenceRef {
	refs := cloneEvidenceRefs(input)
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ID != refs[j].ID {
			return refs[i].ID < refs[j].ID
		}
		if refs[i].Version != refs[j].Version {
			return refs[i].Version < refs[j].Version
		}
		return refs[i].Digest < refs[j].Digest
	})
	return refs
}
