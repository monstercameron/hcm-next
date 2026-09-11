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
	ErrInvalidCalibrationRule       = errors.New("performance: invalid calibration rule")
	ErrInvalidCalibrationSession    = errors.New("performance: invalid calibration session")
	ErrInvalidCalibrationAdjustment = errors.New("performance: invalid calibration adjustment")
	ErrCalibrationReviewer          = errors.New("performance: adjuster is not a session reviewer")
	ErrCalibrationParticipant       = errors.New("performance: participant is not in calibration cohort")
	ErrCalibrationFromMismatch      = errors.New("performance: adjustment from rating does not match current rating")
	ErrCalibrationCapExceeded       = errors.New("performance: adjustment exceeds calibration cap")
	ErrCalibrationSeparation        = errors.New("performance: participant's manager cannot be the sole adjuster")
	ErrCalibrationDuplicate         = errors.New("performance: duplicate calibration adjustment")
)

// CalibrationReasonCode is a closed reason vocabulary. A reason is evidence
// for why a human adjustment occurred, not a free-form narrative field.
type CalibrationReasonCode string

const (
	CalibrationReasonEvidence    CalibrationReasonCode = "EVIDENCE"
	CalibrationReasonConsistency CalibrationReasonCode = "CONSISTENCY"
	CalibrationReasonScope       CalibrationReasonCode = "SCOPE"
	CalibrationReasonPolicy      CalibrationReasonCode = "POLICY"
	CalibrationReasonDataQuality CalibrationReasonCode = "DATA_QUALITY"
	CalibrationEvidence          CalibrationReasonCode = CalibrationReasonEvidence
	CalibrationConsistency       CalibrationReasonCode = CalibrationReasonConsistency
	CalibrationScope             CalibrationReasonCode = CalibrationReasonScope
)

func (r CalibrationReasonCode) Valid() bool {
	switch r {
	case CalibrationReasonEvidence, CalibrationReasonConsistency, CalibrationReasonScope, CalibrationReasonPolicy, CalibrationReasonDataQuality:
		return true
	default:
		return false
	}
}

// CalibrationRule governs the allowed immutable adjustment events.
type CalibrationRule struct {
	ID                 string
	Version            string
	Digest             string
	AdjustmentCap      values.Decimal
	AllowedReasonCodes []CalibrationReasonCode
}

type CalibrationPolicy = CalibrationRule

func NewCalibrationRule(id, version string, adjustmentCap values.Decimal, reasons []CalibrationReasonCode) (CalibrationRule, error) {
	rule := CalibrationRule{ID: id, Version: version, AdjustmentCap: adjustmentCap, AllowedReasonCodes: append([]CalibrationReasonCode(nil), reasons...)}
	return rule.withDigest()
}

func (r CalibrationRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("%w: id and version are required", ErrInvalidCalibrationRule)
	}
	if err := r.AdjustmentCap.Validate(); err != nil || r.AdjustmentCap.Sign() <= 0 {
		return fmt.Errorf("%w: adjustment cap must be a positive decimal", ErrInvalidCalibrationRule)
	}
	seen := make(map[CalibrationReasonCode]struct{}, len(r.AllowedReasonCodes))
	for _, reason := range r.AllowedReasonCodes {
		if !reason.Valid() {
			return fmt.Errorf("%w: unknown reason code %q", ErrInvalidCalibrationRule, reason)
		}
		if _, exists := seen[reason]; exists {
			return fmt.Errorf("%w: duplicate reason code %q", ErrInvalidCalibrationRule, reason)
		}
		seen[reason] = struct{}{}
	}
	if r.Digest != "" && r.Digest != r.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidCalibrationRule)
	}
	return nil
}

func (r CalibrationRule) allowsReason(reason CalibrationReasonCode) bool {
	if len(r.AllowedReasonCodes) == 0 {
		return reason.Valid()
	}
	for _, allowed := range r.AllowedReasonCodes {
		if allowed == reason {
			return true
		}
	}
	return false
}

func (r CalibrationRule) body() []byte {
	copyOf := r
	copyOf.Digest = ""
	if err := copyOf.Validate(); err != nil {
		return nil
	}
	reasons := append([]CalibrationReasonCode(nil), r.AllowedReasonCodes...)
	sort.Slice(reasons, func(i, j int) bool { return reasons[i] < reasons[j] })
	w := canonicalbytes.New("hcmnext.domains.performance.CalibrationRule", 1).
		String("id", r.ID).String("version", r.Version).Value("adjustment_cap", r.AdjustmentCap).
		Count("allowed_reasons", len(reasons))
	for _, reason := range reasons {
		w.String("allowed_reason", string(reason))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r CalibrationRule) computedDigest() string {
	body := r.body()
	if body == nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

func (r CalibrationRule) withDigest() (CalibrationRule, error) {
	providedDigest := r.Digest
	r.Digest = ""
	if err := r.Validate(); err != nil {
		return CalibrationRule{}, err
	}
	computed := r.computedDigest()
	if providedDigest != "" && providedDigest != computed {
		return CalibrationRule{}, fmt.Errorf("%w: digest mismatch", ErrInvalidCalibrationRule)
	}
	r.Digest = computed
	return r, nil
}

func (r CalibrationRule) Canonical() []byte { return r.body() }

// CalibrationAdjustment is one immutable, digested decision event.
type CalibrationAdjustment struct {
	ParticipantID string
	From          values.Decimal
	To            values.Decimal
	Reason        CalibrationReasonCode
	AdjusterID    string
	Digest        string
}

func NewCalibrationAdjustment(participantID string, from, to values.Decimal, reason CalibrationReasonCode, adjusterID string) (CalibrationAdjustment, error) {
	adjustment := CalibrationAdjustment{ParticipantID: participantID, From: from, To: to, Reason: reason, AdjusterID: adjusterID}
	return adjustment.withDigest()
}

func (a CalibrationAdjustment) Validate() error {
	if strings.TrimSpace(a.ParticipantID) == "" || strings.TrimSpace(a.AdjusterID) == "" {
		return fmt.Errorf("%w: participant and adjuster are required", ErrInvalidCalibrationAdjustment)
	}
	if err := a.From.Validate(); err != nil {
		return fmt.Errorf("%w: from rating: %v", ErrInvalidCalibrationAdjustment, err)
	}
	if err := a.To.Validate(); err != nil {
		return fmt.Errorf("%w: to rating: %v", ErrInvalidCalibrationAdjustment, err)
	}
	if a.From.Scale() != a.To.Scale() {
		return fmt.Errorf("%w: from and to scales differ", ErrInvalidCalibrationAdjustment)
	}
	if !a.Reason.Valid() {
		return fmt.Errorf("%w: unknown reason code %q", ErrInvalidCalibrationAdjustment, a.Reason)
	}
	if a.Digest == "" || a.Digest != a.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidCalibrationAdjustment)
	}
	return nil
}

func (a CalibrationAdjustment) body() []byte {
	if err := a.ValidateWithoutDigest(); err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.performance.CalibrationAdjustment", 1).
		String("participant_id", a.ParticipantID).Value("from", a.From).Value("to", a.To).
		String("reason", string(a.Reason)).String("adjuster_id", a.AdjusterID)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (a CalibrationAdjustment) ValidateWithoutDigest() error {
	// The digest is simply never read here, so there is nothing to blank.
	if strings.TrimSpace(a.ParticipantID) == "" || strings.TrimSpace(a.AdjusterID) == "" {
		return fmt.Errorf("%w: participant and adjuster are required", ErrInvalidCalibrationAdjustment)
	}
	if err := a.From.Validate(); err != nil {
		return fmt.Errorf("%w: from rating: %v", ErrInvalidCalibrationAdjustment, err)
	}
	if err := a.To.Validate(); err != nil {
		return fmt.Errorf("%w: to rating: %v", ErrInvalidCalibrationAdjustment, err)
	}
	if a.From.Scale() != a.To.Scale() || !a.Reason.Valid() {
		return fmt.Errorf("%w: rating scales and reason must be declared", ErrInvalidCalibrationAdjustment)
	}
	return nil
}

func (a CalibrationAdjustment) computedDigest() string {
	body := a.body()
	if body == nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

func (a CalibrationAdjustment) withDigest() (CalibrationAdjustment, error) {
	providedDigest := a.Digest
	a.Digest = ""
	if err := a.ValidateWithoutDigest(); err != nil {
		return CalibrationAdjustment{}, err
	}
	computed := a.computedDigest()
	if providedDigest != "" && providedDigest != computed {
		return CalibrationAdjustment{}, fmt.Errorf("%w: digest mismatch", ErrInvalidCalibrationAdjustment)
	}
	a.Digest = computed
	return a, nil
}

func (a CalibrationAdjustment) Canonical() []byte { return a.body() }

// FinalCalibratedRating retains the proposed value and the complete immutable
// adjustment history used to reach the final value.
type FinalCalibratedRating struct {
	ParticipantID        string
	ProposedRating       ProposedRating
	FinalRating          values.Decimal
	ProposedRatingDigest string
	Adjustments          []CalibrationAdjustment
	SessionDigest        string
	CanonicalDigest      string
}

type CalibratedRating = FinalCalibratedRating

func (r FinalCalibratedRating) Validate() error {
	if strings.TrimSpace(r.ParticipantID) == "" || strings.TrimSpace(r.ProposedRatingDigest) == "" || strings.TrimSpace(r.SessionDigest) == "" {
		return fmt.Errorf("%w: final rating references are required", ErrInvalidCalibrationSession)
	}
	if err := r.ProposedRating.Validate(); err != nil {
		return err
	}
	if err := r.FinalRating.Validate(); err != nil {
		return fmt.Errorf("%w: final rating: %v", ErrInvalidCalibrationSession, err)
	}
	if len(r.Adjustments) > 0 && r.Adjustments[0].From.Scale() != r.ProposedRating.Rating.Scale() {
		return fmt.Errorf("%w: adjustment scale differs from proposed rating", ErrInvalidCalibrationSession)
	}
	for _, adjustment := range r.Adjustments {
		if err := adjustment.Validate(); err != nil {
			return err
		}
	}
	current := r.ProposedRating.Rating
	for _, adjustment := range r.Adjustments {
		if adjustment.ParticipantID != r.ParticipantID || !adjustment.From.Equal(current) {
			return fmt.Errorf("%w: adjustment history does not lead from proposed rating", ErrInvalidCalibrationSession)
		}
		current = adjustment.To
	}
	if !current.Equal(r.FinalRating) {
		return fmt.Errorf("%w: final rating does not match adjustment history", ErrInvalidCalibrationSession)
	}
	if r.CanonicalDigest == "" || r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: final digest mismatch", ErrInvalidCalibrationSession)
	}
	return nil
}

func (r FinalCalibratedRating) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.performance.FinalCalibratedRating", 1).
		String("participant_id", r.ParticipantID).Value("proposed_rating", r.ProposedRating.Rating).
		Value("final", r.FinalRating).String("proposed_digest", r.ProposedRatingDigest).
		String("session_digest", r.SessionDigest).Count("adjustments", len(r.Adjustments))
	for _, adjustment := range r.Adjustments {
		w.String("adjustment", adjustment.Digest)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

type FinalCalibratedRatingExplanation struct {
	ParticipantID        string
	ProposedRating       values.Decimal
	FinalRating          values.Decimal
	ProposedRatingDigest string
	SessionDigest        string
	Adjustments          []CalibrationAdjustment
	Digest               string
}

func (r FinalCalibratedRating) Explain() (FinalCalibratedRatingExplanation, error) {
	if err := r.Validate(); err != nil {
		return FinalCalibratedRatingExplanation{}, err
	}
	return FinalCalibratedRatingExplanation{
		ParticipantID: r.ParticipantID, ProposedRating: r.ProposedRating.Rating, FinalRating: r.FinalRating,
		ProposedRatingDigest: r.ProposedRatingDigest, SessionDigest: r.SessionDigest,
		Adjustments: append([]CalibrationAdjustment(nil), r.Adjustments...), Digest: r.CanonicalDigest,
	}, nil
}

func ExplainFinalCalibratedRating(r FinalCalibratedRating) (FinalCalibratedRatingExplanation, error) {
	return r.Explain()
}

// CalibrationSession is an immutable-by-value cohort and adjustment history.
type CalibrationSession struct {
	SessionID       string
	CycleID         string
	CycleRevision   uint64
	Graph           FrozenParticipantReviewerGraph
	FacilitatorID   string
	ReviewerIDs     []string
	Cohort          []ProposedRating
	Rule            CalibrationRule
	Adjustments     []CalibrationAdjustment
	CanonicalDigest string
}

func NewCalibrationSession(sessionID string, graph FrozenParticipantReviewerGraph, cohort []ProposedRating, facilitatorID string, reviewerIDs []string, rule CalibrationRule) (CalibrationSession, error) {
	normalizedRule, err := rule.withDigest()
	if err != nil {
		return CalibrationSession{}, err
	}
	session := CalibrationSession{SessionID: sessionID, CycleID: graph.CycleID, CycleRevision: graph.CycleRevision, Graph: graph, FacilitatorID: facilitatorID, ReviewerIDs: append([]string(nil), reviewerIDs...), Cohort: cloneProposedRatings(cohort), Rule: normalizedRule}
	sort.Strings(session.ReviewerIDs)
	sort.Slice(session.Cohort, func(i, j int) bool { return session.Cohort[i].ParticipantID < session.Cohort[j].ParticipantID })
	session.CanonicalDigest = session.computedDigest()
	if err := session.Validate(); err != nil {
		return CalibrationSession{}, err
	}
	return session, nil
}

func CreateCalibrationSession(sessionID string, graph FrozenParticipantReviewerGraph, cohort []ProposedRating, facilitatorID string, reviewerIDs []string, rule CalibrationRule) (CalibrationSession, error) {
	return NewCalibrationSession(sessionID, graph, cohort, facilitatorID, reviewerIDs, rule)
}

func cloneProposedRatings(input []ProposedRating) []ProposedRating {
	output := make([]ProposedRating, len(input))
	for i, item := range input {
		output[i] = item
		output[i].Contributions = append([]RatingContribution(nil), item.Contributions...)
		output[i].ExcludedReviewIDs = append([]string(nil), item.ExcludedReviewIDs...)
		output[i].Rule.Weights = copyWeights(item.Rule.Weights)
	}
	return output
}

func (s CalibrationSession) Validate() error {
	if strings.TrimSpace(s.SessionID) == "" || strings.TrimSpace(s.FacilitatorID) == "" || s.CycleRevision == 0 {
		return fmt.Errorf("%w: session, facilitator and cycle are required", ErrInvalidCalibrationSession)
	}
	if err := s.Graph.Validate(); err != nil {
		return fmt.Errorf("%w: graph: %v", ErrInvalidCalibrationSession, err)
	}
	if s.CycleID != s.Graph.CycleID || s.CycleRevision != s.Graph.CycleRevision {
		return fmt.Errorf("%w: cycle binding mismatch", ErrInvalidCalibrationSession)
	}
	if err := s.Rule.Validate(); err != nil {
		return fmt.Errorf("%w: rule: %v", ErrInvalidCalibrationSession, err)
	}
	if len(s.ReviewerIDs) == 0 {
		return fmt.Errorf("%w: at least one reviewer is required", ErrInvalidCalibrationSession)
	}
	reviewers := make(map[string]struct{}, len(s.ReviewerIDs))
	for _, reviewer := range s.ReviewerIDs {
		if strings.TrimSpace(reviewer) == "" || reviewer == s.FacilitatorID {
			return fmt.Errorf("%w: facilitator and reviewers must be distinct", ErrInvalidCalibrationSession)
		}
		if _, exists := reviewers[reviewer]; exists {
			return fmt.Errorf("%w: duplicate reviewer %q", ErrInvalidCalibrationSession, reviewer)
		}
		reviewers[reviewer] = struct{}{}
	}
	if len(s.Cohort) == 0 {
		return fmt.Errorf("%w: cohort is required", ErrInvalidCalibrationSession)
	}
	participants := make(map[string]struct{}, len(s.Cohort))
	for _, proposed := range s.Cohort {
		if err := proposed.Validate(); err != nil {
			return fmt.Errorf("%w: proposed rating: %v", ErrInvalidCalibrationSession, err)
		}
		if proposed.CycleID != s.CycleID || proposed.CycleRevision != s.CycleRevision || proposed.GraphDigest != s.Graph.Digest {
			return fmt.Errorf("%w: proposed rating cycle or graph mismatch", ErrInvalidCalibrationSession)
		}
		if _, exists := participants[proposed.ParticipantID]; exists {
			return fmt.Errorf("%w: duplicate cohort participant %q", ErrInvalidCalibrationSession, proposed.ParticipantID)
		}
		participants[proposed.ParticipantID] = struct{}{}
	}
	for _, adjustment := range s.Adjustments {
		if err := adjustment.Validate(); err != nil {
			return err
		}
		if _, exists := participants[adjustment.ParticipantID]; !exists {
			return ErrCalibrationParticipant
		}
		if _, exists := reviewers[adjustment.AdjusterID]; !exists {
			return ErrCalibrationReviewer
		}
		if !s.Rule.allowsReason(adjustment.Reason) {
			return fmt.Errorf("%w: reason %q is not allowed by rule", ErrInvalidCalibrationRule, adjustment.Reason)
		}
	}
	if err := s.validateAdjustmentHistory(); err != nil {
		return err
	}
	if s.CanonicalDigest == "" || s.CanonicalDigest != s.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidCalibrationSession)
	}
	return nil
}

func (s CalibrationSession) validateAdjustmentHistory() error {
	seen := make(map[string]struct{}, len(s.Adjustments))
	for _, proposed := range s.Cohort {
		current := proposed.Rating
		adjusters := make(map[string]struct{})
		for _, adjustment := range s.Adjustments {
			if adjustment.ParticipantID != proposed.ParticipantID {
				continue
			}
			if _, exists := seen[adjustment.Digest]; exists {
				return ErrCalibrationDuplicate
			}
			seen[adjustment.Digest] = struct{}{}
			if !adjustment.From.Equal(current) {
				return ErrCalibrationFromMismatch
			}
			delta, err := adjustment.To.Sub(adjustment.From)
			if err != nil {
				return err
			}
			delta, err = delta.Abs()
			if err != nil {
				return err
			}
			if delta.Cmp(s.Rule.AdjustmentCap) > 0 {
				return ErrCalibrationCapExceeded
			}
			adjusters[adjustment.AdjusterID] = struct{}{}
			current = adjustment.To
			if hasSoleManagerAdjuster(s.Graph, []CalibrationAdjustment{adjustment}, proposed.ParticipantID) && len(adjusters) == 1 {
				return ErrCalibrationSeparation
			}
		}
	}
	return nil
}

func (s CalibrationSession) computedDigest() string {
	cohort := append([]ProposedRating(nil), s.Cohort...)
	sort.Slice(cohort, func(i, j int) bool { return cohort[i].ParticipantID < cohort[j].ParticipantID })
	w := canonicalbytes.New("hcmnext.domains.performance.CalibrationSession", 1).
		String("session_id", s.SessionID).String("cycle_id", s.CycleID).Int("cycle_revision", int64(s.CycleRevision)).
		String("graph_digest", s.Graph.Digest).String("facilitator_id", s.FacilitatorID).
		Value("rule", s.Rule).Count("reviewers", len(s.ReviewerIDs))
	for _, reviewer := range s.ReviewerIDs {
		w.String("reviewer", reviewer)
	}
	w.Count("cohort", len(cohort))
	for _, proposed := range cohort {
		w.String("proposed.participant", proposed.ParticipantID).String("proposed.digest", proposed.CanonicalDigest)
	}
	w.Count("adjustments", len(s.Adjustments))
	for _, adjustment := range s.Adjustments {
		w.String("adjustment", adjustment.Digest)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func (s CalibrationSession) currentRating(participantID string) (values.Decimal, []CalibrationAdjustment, error) {
	var proposed *ProposedRating
	for i := range s.Cohort {
		if s.Cohort[i].ParticipantID == participantID {
			proposed = &s.Cohort[i]
			break
		}
	}
	if proposed == nil {
		return values.Decimal{}, nil, ErrCalibrationParticipant
	}
	current := proposed.Rating
	history := make([]CalibrationAdjustment, 0)
	for _, adjustment := range s.Adjustments {
		if adjustment.ParticipantID != participantID {
			continue
		}
		if !adjustment.From.Equal(current) {
			return values.Decimal{}, nil, ErrCalibrationFromMismatch
		}
		current = adjustment.To
		history = append(history, adjustment)
	}
	return current, history, nil
}

func (s CalibrationSession) managerFor(participantID string) string {
	for _, assignment := range s.Graph.Reviewers {
		if assignment.ParticipantID == participantID && assignment.Relationship == ReviewerRelationshipManager {
			return assignment.ReviewerID
		}
	}
	return ""
}

func hasSoleManagerAdjuster(graph FrozenParticipantReviewerGraph, adjustments []CalibrationAdjustment, participantID string) bool {
	manager := ""
	adjusters := make(map[string]struct{})
	for _, assignment := range graph.Reviewers {
		if assignment.ParticipantID == participantID && assignment.Relationship == ReviewerRelationshipManager {
			manager = assignment.ReviewerID
			break
		}
	}
	if manager == "" {
		return false
	}
	for _, adjustment := range adjustments {
		if adjustment.ParticipantID == participantID {
			adjusters[adjustment.AdjusterID] = struct{}{}
		}
	}
	return len(adjusters) == 1 && func() bool { _, ok := adjusters[manager]; return ok }()
}

// AddAdjustment appends one governed decision event and returns a successor
// session; the receiver and its prior event history remain unchanged.
func (s CalibrationSession) AddAdjustment(adjustment CalibrationAdjustment) (CalibrationSession, error) {
	if err := s.Validate(); err != nil {
		return CalibrationSession{}, err
	}
	normalized, err := adjustment.withDigest()
	if err != nil {
		return CalibrationSession{}, err
	}
	if _, _, err := s.currentRating(normalized.ParticipantID); err != nil {
		return CalibrationSession{}, err
	}
	reviewer := false
	for _, candidate := range s.ReviewerIDs {
		if candidate == normalized.AdjusterID {
			reviewer = true
			break
		}
	}
	if !reviewer {
		return CalibrationSession{}, ErrCalibrationReviewer
	}
	if !s.Rule.allowsReason(normalized.Reason) {
		return CalibrationSession{}, fmt.Errorf("%w: reason %q is not allowed by rule", ErrInvalidCalibrationRule, normalized.Reason)
	}
	current, history, err := s.currentRating(normalized.ParticipantID)
	if err != nil {
		return CalibrationSession{}, err
	}
	if !normalized.From.Equal(current) {
		return CalibrationSession{}, ErrCalibrationFromMismatch
	}
	delta, err := normalized.To.Sub(normalized.From)
	if err != nil {
		return CalibrationSession{}, err
	}
	delta, err = delta.Abs()
	if err != nil {
		return CalibrationSession{}, err
	}
	if delta.Cmp(s.Rule.AdjustmentCap) > 0 {
		return CalibrationSession{}, fmt.Errorf("%w: participant=%s cap=%s delta=%s", ErrCalibrationCapExceeded, normalized.ParticipantID, s.Rule.AdjustmentCap.String(), delta.String())
	}
	for _, prior := range s.Adjustments {
		if prior.Digest == normalized.Digest {
			return CalibrationSession{}, ErrCalibrationDuplicate
		}
	}
	history = append(history, normalized)
	if hasSoleManagerAdjuster(s.Graph, history, normalized.ParticipantID) {
		return CalibrationSession{}, ErrCalibrationSeparation
	}
	next := s
	next.Adjustments = append([]CalibrationAdjustment(nil), s.Adjustments...)
	next.Adjustments = append(next.Adjustments, normalized)
	next.CanonicalDigest = next.computedDigest()
	if err := next.Validate(); err != nil {
		return CalibrationSession{}, err
	}
	return next, nil
}

func (s CalibrationSession) Adjust(adjustment CalibrationAdjustment) (CalibrationSession, error) {
	return s.AddAdjustment(adjustment)
}

func (s CalibrationSession) FinalizeParticipant(participantID string) (FinalCalibratedRating, error) {
	if err := s.Validate(); err != nil {
		return FinalCalibratedRating{}, err
	}
	final, history, err := s.currentRating(participantID)
	if err != nil {
		return FinalCalibratedRating{}, err
	}
	var proposed ProposedRating
	for _, item := range s.Cohort {
		if item.ParticipantID == participantID {
			proposed = item
			break
		}
	}
	result := FinalCalibratedRating{ParticipantID: participantID, ProposedRating: proposed, FinalRating: final, ProposedRatingDigest: proposed.CanonicalDigest, Adjustments: append([]CalibrationAdjustment(nil), history...), SessionDigest: s.CanonicalDigest}
	result.CanonicalDigest = result.computedDigest()
	if err := result.Validate(); err != nil {
		return FinalCalibratedRating{}, err
	}
	return result, nil
}

func (s CalibrationSession) Finalize() ([]FinalCalibratedRating, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	results := make([]FinalCalibratedRating, 0, len(s.Cohort))
	for _, proposed := range s.Cohort {
		result, err := s.FinalizeParticipant(proposed.ParticipantID)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

type CalibrationSessionExplanation struct {
	SessionID         string
	CycleID           string
	CycleRevision     uint64
	GraphDigest       string
	FacilitatorID     string
	ReviewerIDs       []string
	RuleID            string
	RuleVersion       string
	RuleDigest        string
	CohortCount       int
	AdjustmentCount   int
	AdjustmentDigests []string
	Digest            string
}

func (s CalibrationSession) Explain() (CalibrationSessionExplanation, error) {
	if err := s.Validate(); err != nil {
		return CalibrationSessionExplanation{}, err
	}
	digests := make([]string, 0, len(s.Adjustments))
	for _, adjustment := range s.Adjustments {
		digests = append(digests, adjustment.Digest)
	}
	return CalibrationSessionExplanation{SessionID: s.SessionID, CycleID: s.CycleID, CycleRevision: s.CycleRevision, GraphDigest: s.Graph.Digest, FacilitatorID: s.FacilitatorID, ReviewerIDs: append([]string(nil), s.ReviewerIDs...), RuleID: s.Rule.ID, RuleVersion: s.Rule.Version, RuleDigest: s.Rule.Digest, CohortCount: len(s.Cohort), AdjustmentCount: len(s.Adjustments), AdjustmentDigests: digests, Digest: s.CanonicalDigest}, nil
}

func ExplainCalibrationSession(s CalibrationSession) (CalibrationSessionExplanation, error) {
	return s.Explain()
}
