// Package merit owns pure, immutable merit-cycle semantics. A cycle consumes a
// frozen population snapshot, an exact decimal budget, and a versioned
// guideline matrix; it never writes compensation or makes an employment
// decision.
package merit

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this domain vocabulary's contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidPopulation           = errors.New("merit: invalid frozen population")
	ErrInvalidGuideline            = errors.New("merit: invalid guideline matrix")
	ErrInvalidCycle                = errors.New("merit: invalid merit cycle")
	ErrInvalidRecommendation       = errors.New("merit: invalid merit recommendation")
	ErrInvalidAdjustment           = errors.New("merit: invalid calibration adjustment")
	ErrBudgetExceeded              = errors.New("merit: budget pool would be exceeded")
	ErrRecommendationDuplicate     = errors.New("merit: participant already has a recommendation")
	ErrCalibrationSeparation       = errors.New("merit: manager cannot be the sole calibration adjuster")
	ErrStalePopulationFact         = errors.New("merit: recommendation does not use the frozen salary and performance facts")
	ErrCycleTransition             = errors.New("merit: cycle transition is not allowed")
	ErrInvalidRevisionLineage      = errors.New("merit: invalid revision lineage")
	ErrCorrectionAfterFinalization = errors.New("merit: finalized cycle requires a successor correction")
)

// MeritCycleState is the lifecycle of a cycle revision.
type MeritCycleState string

const (
	CycleDraft       MeritCycleState = "DRAFT"
	CycleOpen        MeritCycleState = "OPEN"
	CycleCalibrating MeritCycleState = "CALIBRATING"
	CycleApproved    MeritCycleState = "APPROVED"
	CycleFinalized   MeritCycleState = "FINALIZED"
	DRAFT                            = CycleDraft
	OPEN                             = CycleOpen
	CALIBRATING                      = CycleCalibrating
	APPROVED                         = CycleApproved
	FINALIZED                        = CycleFinalized
)

func (s MeritCycleState) Valid() bool {
	switch s {
	case CycleDraft, CycleOpen, CycleCalibrating, CycleApproved, CycleFinalized:
		return true
	}
	return false
}

// RecommendationState is the closed decision vocabulary required by MERIT-001.
type RecommendationState string

const (
	RecommendationProposed   RecommendationState = "PROPOSED"
	RecommendationAdjusted   RecommendationState = "ADJUSTED"
	RecommendationApproved   RecommendationState = "APPROVED"
	RecommendationRejected   RecommendationState = "REJECTED"
	RecommendationFinalized  RecommendationState = "FINALIZED"
	PROPOSED                                     = RecommendationProposed
	ADJUSTED                                     = RecommendationAdjusted
	APPROVED_RECOMMENDATION                      = RecommendationApproved
	REJECTED_RECOMMENDATION                      = RecommendationRejected
	FINALIZED_RECOMMENDATION                     = RecommendationFinalized
)

func (s RecommendationState) Valid() bool {
	switch s {
	case RecommendationProposed, RecommendationAdjusted, RecommendationApproved, RecommendationRejected, RecommendationFinalized:
		return true
	}
	return false
}

// PopulationMember is a point-in-time, non-live input to a merit cycle.
// Protected attributes are intentionally not represented and therefore cannot
// enter recommendation arithmetic.
type PopulationMember struct {
	ParticipantID     string
	ManagerID         string
	BasePay           values.Money
	PerformanceRating values.Decimal
	BandPosition      values.Decimal
	SalaryRevisionRef string
	PerformanceRef    string
	EffectiveAt       values.Instant
	KnownAt           values.Instant
}

func (m PopulationMember) Validate() error {
	if strings.TrimSpace(m.ParticipantID) == "" {
		return fmt.Errorf("%w: participant_id is required", ErrInvalidPopulation)
	}
	if strings.TrimSpace(m.ManagerID) == "" {
		return fmt.Errorf("%w: manager_id is required", ErrInvalidPopulation)
	}
	if err := m.BasePay.Validate(); err != nil {
		return fmt.Errorf("%w: base_pay: %v", ErrInvalidPopulation, err)
	}
	if m.BasePay.Amount().Sign() < 0 {
		return fmt.Errorf("%w: base_pay must not be negative", ErrInvalidPopulation)
	}
	for name, d := range map[string]values.Decimal{"performance_rating": m.PerformanceRating, "band_position": m.BandPosition} {
		if err := d.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidPopulation, name, err)
		}
	}
	for name, ref := range map[string]string{"salary_revision_ref": m.SalaryRevisionRef, "performance_ref": m.PerformanceRef} {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidPopulation, name)
		}
	}
	if err := m.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective_at: %v", ErrInvalidPopulation, err)
	}
	if err := m.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidPopulation, err)
	}
	return nil
}

func (m PopulationMember) Canonical() []byte {
	if m.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.merit.PopulationMember", schemaVersion).
		String("participant_id", m.ParticipantID).String("manager_id", m.ManagerID).Value("base_pay", m.BasePay).
		Value("performance_rating", m.PerformanceRating).Value("band_position", m.BandPosition).
		String("salary_revision_ref", m.SalaryRevisionRef).String("performance_ref", m.PerformanceRef).
		Value("effective_at", m.EffectiveAt).Value("known_at", m.KnownAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// PopulationSnapshot is frozen at construction and is the only population a
// cycle may use.
type PopulationSnapshot struct {
	SnapshotID      string
	Revision        uint64
	Members         []PopulationMember
	Watermark       values.RevisionToken
	Frozen          bool
	FrozenAt        values.Instant
	CanonicalDigest string
}

// MeritPopulation and Population are compatibility spellings for the frozen
// population record.
type MeritPopulation = PopulationSnapshot
type Population = PopulationSnapshot
type MeritPopulationSnapshot = PopulationSnapshot
type MeritBudget = values.Decimal

func (p PopulationSnapshot) Validate() error {
	if strings.TrimSpace(p.SnapshotID) == "" {
		return fmt.Errorf("%w: snapshot_id is required", ErrInvalidPopulation)
	}
	if p.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidPopulation)
	}
	if !p.Frozen {
		return fmt.Errorf("%w: frozen must be true", ErrInvalidPopulation)
	}
	if !p.Watermark.IsSpecified() {
		return fmt.Errorf("%w: watermark is required", ErrInvalidPopulation)
	}
	if err := p.FrozenAt.Validate(); err != nil {
		return fmt.Errorf("%w: frozen_at: %v", ErrInvalidPopulation, err)
	}
	if len(p.Members) == 0 {
		return fmt.Errorf("%w: members are required", ErrInvalidPopulation)
	}
	seen := make(map[string]struct{}, len(p.Members))
	for _, member := range p.Members {
		if err := member.Validate(); err != nil {
			return err
		}
		if _, exists := seen[member.ParticipantID]; exists {
			return fmt.Errorf("%w: duplicate participant_id %q", ErrInvalidPopulation, member.ParticipantID)
		}
		seen[member.ParticipantID] = struct{}{}
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidPopulation)
	}
	return nil
}

func (p PopulationSnapshot) body() []byte {
	members := append([]PopulationMember(nil), p.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].ParticipantID < members[j].ParticipantID })
	w := canonicalbytes.New("hcmnext.domains.merit.PopulationSnapshot", schemaVersion).
		String("snapshot_id", p.SnapshotID).Int("revision", int64(p.Revision)).Value("watermark", p.Watermark).
		Bool("frozen", p.Frozen).Value("frozen_at", p.FrozenAt).Count("members", len(members))
	for _, member := range members {
		w.String("member", canonicalbytes.Digest(member.Canonical()))
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (p PopulationSnapshot) computedDigest() string { return canonicalbytes.Digest(p.body()) }
func (p PopulationSnapshot) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}

func NewPopulationSnapshot(p PopulationSnapshot) (PopulationSnapshot, error) {
	p.Members = append([]PopulationMember(nil), p.Members...)
	if p.CanonicalDigest == "" {
		if err := validatePopulationWithoutDigest(p); err != nil {
			return PopulationSnapshot{}, err
		}
		p.CanonicalDigest = p.computedDigest()
	}
	if err := p.Validate(); err != nil {
		return PopulationSnapshot{}, err
	}
	return p, nil
}

func (p PopulationSnapshot) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.CanonicalDigest, nil
}
func validatePopulationWithoutDigest(p PopulationSnapshot) error {
	copyOf := p
	copyOf.CanonicalDigest = ""
	return copyOf.Validate()
}

// GuidelineRule selects an exact rate interval for a rating and band position.
// Rates are fractions: 0.075 means 7.5 percent.
type GuidelineRule struct {
	RatingMin       values.Decimal
	RatingMax       values.Decimal
	BandPositionMin values.Decimal
	BandPositionMax values.Decimal
	MinimumRate     values.Decimal
	MaximumRate     values.Decimal
}

// GuidelineEntry is a descriptive alias.
type GuidelineEntry = GuidelineRule

func (g GuidelineRule) Validate() error {
	for name, d := range map[string]values.Decimal{
		"rating_min": g.RatingMin, "rating_max": g.RatingMax, "band_position_min": g.BandPositionMin, "band_position_max": g.BandPositionMax, "minimum_rate": g.MinimumRate, "maximum_rate": g.MaximumRate,
	} {
		if err := d.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidGuideline, name, err)
		}
	}
	if g.RatingMin.Cmp(g.RatingMax) > 0 {
		return fmt.Errorf("%w: rating range is reversed", ErrInvalidGuideline)
	}
	if g.BandPositionMin.Cmp(g.BandPositionMax) > 0 {
		return fmt.Errorf("%w: band_position range is reversed", ErrInvalidGuideline)
	}
	if g.MinimumRate.Sign() < 0 || g.MaximumRate.Sign() < 0 || g.MinimumRate.Cmp(g.MaximumRate) > 0 {
		return fmt.Errorf("%w: rate range is invalid", ErrInvalidGuideline)
	}
	return nil
}
func (g GuidelineRule) Canonical() []byte {
	if g.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.merit.GuidelineRule", schemaVersion).
		Value("rating_min", g.RatingMin).Value("rating_max", g.RatingMax).Value("band_position_min", g.BandPositionMin).Value("band_position_max", g.BandPositionMax).
		Value("minimum_rate", g.MinimumRate).Value("maximum_rate", g.MaximumRate).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// GuidelineMatrix is a versioned, deterministic rule set.
type GuidelineMatrix struct {
	MatrixID        string
	Version         string
	Rules           []GuidelineRule
	CanonicalDigest string
}

func (g GuidelineMatrix) Validate() error {
	if strings.TrimSpace(g.MatrixID) == "" || strings.TrimSpace(g.Version) == "" {
		return fmt.Errorf("%w: matrix_id and version are required", ErrInvalidGuideline)
	}
	if len(g.Rules) == 0 {
		return fmt.Errorf("%w: rules are required", ErrInvalidGuideline)
	}
	for _, rule := range g.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	if g.CanonicalDigest != "" && g.CanonicalDigest != g.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidGuideline)
	}
	return nil
}
func (g GuidelineMatrix) body() []byte {
	rules := append([]GuidelineRule(nil), g.Rules...)
	sort.Slice(rules, func(i, j int) bool { return string(rules[i].Canonical()) < string(rules[j].Canonical()) })
	w := canonicalbytes.New("hcmnext.domains.merit.GuidelineMatrix", schemaVersion).String("matrix_id", g.MatrixID).String("version", g.Version).Count("rules", len(rules))
	for _, rule := range rules {
		w.String("rule", canonicalbytes.Digest(rule.Canonical()))
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (g GuidelineMatrix) computedDigest() string { return canonicalbytes.Digest(g.body()) }
func (g GuidelineMatrix) Canonical() []byte {
	if g.Validate() != nil {
		return nil
	}
	return g.body()
}
func NewGuidelineMatrix(g GuidelineMatrix) (GuidelineMatrix, error) {
	g.Rules = append([]GuidelineRule(nil), g.Rules...)
	if g.CanonicalDigest == "" {
		if err := validateGuidelineWithoutDigest(g); err != nil {
			return GuidelineMatrix{}, err
		}
		g.CanonicalDigest = g.computedDigest()
	}
	if err := g.Validate(); err != nil {
		return GuidelineMatrix{}, err
	}
	return g, nil
}

func (g GuidelineMatrix) Digest() (string, error) {
	if err := g.Validate(); err != nil {
		return "", err
	}
	return g.CanonicalDigest, nil
}
func validateGuidelineWithoutDigest(g GuidelineMatrix) error {
	c := g
	c.CanonicalDigest = ""
	return c.Validate()
}

// Lookup finds the one rule containing rating and band position.
func (g GuidelineMatrix) Lookup(rating, position values.Decimal) (GuidelineRule, error) {
	if err := g.Validate(); err != nil {
		return GuidelineRule{}, err
	}
	if err := rating.Validate(); err != nil {
		return GuidelineRule{}, fmt.Errorf("%w: rating: %v", ErrInvalidRecommendation, err)
	}
	if err := position.Validate(); err != nil {
		return GuidelineRule{}, fmt.Errorf("%w: band_position: %v", ErrInvalidRecommendation, err)
	}
	var found *GuidelineRule
	for _, rule := range g.Rules {
		if rating.Cmp(rule.RatingMin) >= 0 && rating.Cmp(rule.RatingMax) <= 0 && position.Cmp(rule.BandPositionMin) >= 0 && position.Cmp(rule.BandPositionMax) <= 0 {
			if found != nil {
				return GuidelineRule{}, fmt.Errorf("%w: overlapping guideline rules", ErrInvalidGuideline)
			}
			copyOf := rule
			found = &copyOf
		}
	}
	if found == nil {
		return GuidelineRule{}, fmt.Errorf("%w: no guideline for rating and band_position", ErrInvalidRecommendation)
	}
	return *found, nil
}

// MeritRecommendation is an exact, per-participant proposal with the frozen
// salary/rating facts copied into it, so a later live read cannot change it.
type MeritRecommendation struct {
	CycleID                string
	CycleRevision          uint64
	ParticipantID          string
	BasePay                values.Money
	PerformanceRating      values.Decimal
	BandPosition           values.Decimal
	SalaryRevisionRef      string
	PerformanceRef         string
	Rate                   values.Decimal
	Amount                 values.Decimal
	GuidelineDigest        string
	ProposedBy             string
	ApprovedBy             string
	ApprovalReceiptID      string
	ApprovedDigest         string
	EffectRevision         uint64
	approvalVerified       bool
	RejectedBy             string
	RejectionReceiptID     string
	RejectedDigest         string
	rejectionVerified      bool
	verifiedDecisionDigest string
	State                  RecommendationState
	Adjustments            []CalibrationAdjustment
	CanonicalDigest        string
}

func (r MeritRecommendation) Validate() error {
	if strings.TrimSpace(r.CycleID) == "" || r.CycleRevision == 0 || strings.TrimSpace(r.ParticipantID) == "" {
		return fmt.Errorf("%w: cycle and participant binding is required", ErrInvalidRecommendation)
	}
	if err := r.BasePay.Validate(); err != nil {
		return fmt.Errorf("%w: base_pay: %v", ErrInvalidRecommendation, err)
	}
	for name, d := range map[string]values.Decimal{"performance_rating": r.PerformanceRating, "band_position": r.BandPosition, "rate": r.Rate, "amount": r.Amount} {
		if err := d.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidRecommendation, name, err)
		}
	}
	for name, ref := range map[string]string{"salary_revision_ref": r.SalaryRevisionRef, "performance_ref": r.PerformanceRef} {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRecommendation, name)
		}
	}
	if r.Rate.Sign() < 0 || r.Amount.Sign() < 0 {
		return fmt.Errorf("%w: rate and amount must not be negative", ErrInvalidRecommendation)
	}
	if strings.TrimSpace(r.GuidelineDigest) == "" || strings.TrimSpace(r.ProposedBy) == "" {
		return fmt.Errorf("%w: guideline_digest and proposed_by are required", ErrInvalidRecommendation)
	}
	if !r.State.Valid() {
		return fmt.Errorf("%w: state is not declared", ErrInvalidRecommendation)
	}
	approved := r.State == RecommendationApproved || r.State == RecommendationFinalized
	if approved && (!r.approvalVerified || strings.TrimSpace(r.ApprovedBy) == "" || strings.TrimSpace(r.ApprovalReceiptID) == "" || strings.TrimSpace(r.ApprovedDigest) == "" || r.verifiedDecisionDigest != r.decisionPayloadDigest()) {
		return fmt.Errorf("%w: approved states require bound approval evidence", ErrInvalidRecommendation)
	}
	if !approved && (r.ApprovedBy != "" || r.ApprovalReceiptID != "" || r.ApprovedDigest != "") {
		return fmt.Errorf("%w: approval evidence is only valid on approved states", ErrInvalidRecommendation)
	}
	rejected := r.State == RecommendationRejected
	if rejected && (!r.rejectionVerified || strings.TrimSpace(r.RejectedBy) == "" || strings.TrimSpace(r.RejectionReceiptID) == "" || strings.TrimSpace(r.RejectedDigest) == "" || r.Amount.Sign() != 0 || r.Rate.Sign() != 0 || r.verifiedDecisionDigest != r.decisionPayloadDigest()) {
		return fmt.Errorf("%w: rejected state requires bound zero-award evidence", ErrInvalidRecommendation)
	}
	if !rejected && (r.RejectedBy != "" || r.RejectionReceiptID != "" || r.RejectedDigest != "") {
		return fmt.Errorf("%w: rejection evidence is only valid on rejected state", ErrInvalidRecommendation)
	}
	for _, adjustment := range r.Adjustments {
		if err := adjustment.Validate(); err != nil {
			return err
		}
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidRecommendation)
	}
	return nil
}
func (r MeritRecommendation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.merit.MeritRecommendation", schemaVersion).
		String("cycle_id", r.CycleID).Int("cycle_revision", int64(r.CycleRevision)).String("participant_id", r.ParticipantID).Value("base_pay", r.BasePay).
		Value("performance_rating", r.PerformanceRating).Value("band_position", r.BandPosition).String("salary_revision_ref", r.SalaryRevisionRef).String("performance_ref", r.PerformanceRef).Value("rate", r.Rate).Value("amount", r.Amount).
		String("guideline_digest", r.GuidelineDigest).String("proposed_by", r.ProposedBy).String("approved_by", r.ApprovedBy).
		String("approval_receipt_id", r.ApprovalReceiptID).String("approved_digest", r.ApprovedDigest).String("state", string(r.State)).Count("adjustments", len(r.Adjustments))
	w.Int("effect_revision", int64(r.EffectRevision))
	w.String("rejected_by", r.RejectedBy).String("rejection_receipt_id", r.RejectionReceiptID).String("rejected_digest", r.RejectedDigest)
	for _, adjustment := range r.Adjustments {
		w.String("adjustment", adjustment.CanonicalDigest)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r MeritRecommendation) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r MeritRecommendation) decisionPayloadDigest() string {
	w := canonicalbytes.New("hcmnext.domains.merit.MeritDecisionPayload", schemaVersion).
		String("cycle_id", r.CycleID).Int("cycle_revision", int64(r.CycleRevision)).String("participant_id", r.ParticipantID).
		Value("base_pay", r.BasePay).Value("performance_rating", r.PerformanceRating).Value("band_position", r.BandPosition).
		String("salary_revision_ref", r.SalaryRevisionRef).String("performance_ref", r.PerformanceRef).Value("rate", r.Rate).Value("amount", r.Amount).
		String("guideline_digest", r.GuidelineDigest).String("proposed_by", r.ProposedBy).
		String("approved_by", r.ApprovedBy).String("approval_receipt_id", r.ApprovalReceiptID).String("approved_digest", r.ApprovedDigest).
		String("rejected_by", r.RejectedBy).String("rejection_receipt_id", r.RejectionReceiptID).String("rejected_digest", r.RejectedDigest).
		Count("adjustments", len(r.Adjustments))
	for _, adjustment := range r.Adjustments {
		w.String("adjustment", adjustment.CanonicalDigest)
	}
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}
func (r MeritRecommendation) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func NewMeritRecommendation(r MeritRecommendation) (MeritRecommendation, error) {
	r.Adjustments = append([]CalibrationAdjustment(nil), r.Adjustments...)
	for i := range r.Adjustments {
		adjustment, err := NewCalibrationAdjustment(r.Adjustments[i])
		if err != nil {
			return MeritRecommendation{}, err
		}
		r.Adjustments[i] = adjustment
	}
	if r.CanonicalDigest == "" {
		if err := validateRecommendationWithoutDigest(r); err != nil {
			return MeritRecommendation{}, err
		}
		r.CanonicalDigest = r.computedDigest()
	}
	if err := r.Validate(); err != nil {
		return MeritRecommendation{}, err
	}
	return r, nil
}

// Recommendation is the concise spelling used by callers.
type Recommendation = MeritRecommendation
type MeritRecommendationRevision = MeritRecommendation

func (r MeritRecommendation) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.CanonicalDigest, nil
}
func validateRecommendationWithoutDigest(r MeritRecommendation) error {
	c := r
	c.CanonicalDigest = ""
	return c.Validate()
}

// CalibrationAdjustment is an append-only amount decision. Its reason uses
// PERFORMANCE-005's closed vocabulary and the manager-separation rule below
// matches performance.CalibrationSession.
type CalibrationAdjustment struct {
	ParticipantID   string
	From            values.Decimal
	To              values.Decimal
	Reason          performance.CalibrationReasonCode
	AdjusterID      string
	CanonicalDigest string
}

func (a CalibrationAdjustment) Validate() error {
	if err := a.validateWithoutDigest(); err != nil {
		return err
	}
	if a.CanonicalDigest == "" {
		return fmt.Errorf("%w: canonical_digest is required", ErrInvalidAdjustment)
	}
	if a.CanonicalDigest != a.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidAdjustment)
	}
	return nil
}

func (a CalibrationAdjustment) validateWithoutDigest() error {
	if strings.TrimSpace(a.ParticipantID) == "" || strings.TrimSpace(a.AdjusterID) == "" {
		return fmt.Errorf("%w: participant_id and adjuster_id are required", ErrInvalidAdjustment)
	}
	if err := a.From.Validate(); err != nil {
		return fmt.Errorf("%w: from: %v", ErrInvalidAdjustment, err)
	}
	if err := a.To.Validate(); err != nil {
		return fmt.Errorf("%w: to: %v", ErrInvalidAdjustment, err)
	}
	if a.From.Scale() != a.To.Scale() || !a.Reason.Valid() {
		return fmt.Errorf("%w: from/to scale and closed reason are required", ErrInvalidAdjustment)
	}
	if a.To.Sign() < 0 {
		return fmt.Errorf("%w: to must not be negative", ErrInvalidAdjustment)
	}
	return nil
}
func (a CalibrationAdjustment) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.merit.CalibrationAdjustment", schemaVersion).String("participant_id", a.ParticipantID).Value("from", a.From).Value("to", a.To).String("reason", string(a.Reason)).String("adjuster_id", a.AdjusterID).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (a CalibrationAdjustment) computedDigest() string { return canonicalbytes.Digest(a.body()) }
func (a CalibrationAdjustment) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	return a.body()
}
func NewCalibrationAdjustment(a CalibrationAdjustment) (CalibrationAdjustment, error) {
	providedDigest := a.CanonicalDigest
	if err := validateAdjustmentWithoutDigest(a); err != nil {
		return CalibrationAdjustment{}, err
	}
	a.CanonicalDigest = a.computedDigest()
	if providedDigest != "" && providedDigest != a.CanonicalDigest {
		return CalibrationAdjustment{}, fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidAdjustment)
	}
	if err := a.Validate(); err != nil {
		return CalibrationAdjustment{}, err
	}
	return a, nil
}

func (a CalibrationAdjustment) Digest() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	return a.CanonicalDigest, nil
}
func validateAdjustmentWithoutDigest(a CalibrationAdjustment) error {
	return a.validateWithoutDigest()
}

// MeritCycle is an immutable revision containing recommendations and their
// append-only calibration history.
type MeritCycle struct {
	CycleID         string
	Revision        uint64
	ParentRevision  uint64
	ParentDigest    string
	Population      PopulationSnapshot
	Guidelines      GuidelineMatrix
	Budget          values.Decimal
	Currency        string
	State           MeritCycleState
	Recommendations []MeritRecommendation
	EffectiveAt     values.Instant
	KnownAt         values.Instant
	CanonicalDigest string
}

func (c MeritCycle) Validate() error {
	if strings.TrimSpace(c.CycleID) == "" || c.Revision == 0 {
		return fmt.Errorf("%w: cycle_id and revision are required", ErrInvalidCycle)
	}
	if c.Revision == 1 && (c.ParentRevision != 0 || c.ParentDigest != "") {
		return fmt.Errorf("%w: first revision cannot have a parent", ErrInvalidCycle)
	}
	if c.Revision > 1 && (c.ParentRevision == 0 || c.ParentRevision >= c.Revision || strings.TrimSpace(c.ParentDigest) == "") {
		return fmt.Errorf("%w: successor requires an earlier parent revision and digest", ErrInvalidCycle)
	}
	if err := c.Population.Validate(); err != nil {
		return fmt.Errorf("%w: population: %v", ErrInvalidCycle, err)
	}
	if err := c.Guidelines.Validate(); err != nil {
		return fmt.Errorf("%w: guidelines: %v", ErrInvalidCycle, err)
	}
	if err := c.Budget.Validate(); err != nil {
		return fmt.Errorf("%w: budget: %v", ErrInvalidCycle, err)
	}
	if c.Budget.Sign() < 0 {
		return fmt.Errorf("%w: budget must not be negative", ErrInvalidCycle)
	}
	if strings.TrimSpace(c.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidCycle)
	}
	if !c.State.Valid() {
		return fmt.Errorf("%w: state is not declared", ErrInvalidCycle)
	}
	if err := c.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective_at: %v", ErrInvalidCycle, err)
	}
	if err := c.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidCycle, err)
	}
	memberIDs := make(map[string]struct{}, len(c.Population.Members))
	for _, m := range c.Population.Members {
		memberIDs[m.ParticipantID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(c.Recommendations))
	total := zeroLike(c.Budget)
	for _, rec := range c.Recommendations {
		if err := rec.Validate(); err != nil {
			return fmt.Errorf("%w: recommendation: %v", ErrInvalidCycle, err)
		}
		if rec.CycleID != c.CycleID {
			return fmt.Errorf("%w: recommendation cycle binding mismatch", ErrInvalidCycle)
		}
		if _, ok := memberIDs[rec.ParticipantID]; !ok {
			return fmt.Errorf("%w: recommendation participant is outside frozen population", ErrInvalidCycle)
		}
		if _, ok := seen[rec.ParticipantID]; ok {
			return ErrRecommendationDuplicate
		}
		seen[rec.ParticipantID] = struct{}{}
		if rec.BasePay.Currency() != c.Currency {
			return fmt.Errorf("%w: recommendation currency mismatch", ErrInvalidCycle)
		}
		if rec.Amount.Scale() != c.Budget.Scale() {
			return fmt.Errorf("%w: recommendation amount scale differs from budget", ErrInvalidCycle)
		}
		var err error
		total, err = total.Add(rec.Amount)
		if err != nil {
			return fmt.Errorf("%w: budget total: %v", ErrInvalidCycle, err)
		}
	}
	if total.Cmp(c.Budget) > 0 {
		return ErrBudgetExceeded
	}
	if c.CanonicalDigest != "" && c.CanonicalDigest != c.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidCycle)
	}
	return nil
}

func zeroLike(d values.Decimal) values.Decimal {
	return values.MustDecimal(strings.Repeat("0", int(d.Scale())+1), d.Scale(), d.Rounding())
}
func (c MeritCycle) body() []byte {
	recs := append([]MeritRecommendation(nil), c.Recommendations...)
	sort.Slice(recs, func(i, j int) bool { return recs[i].ParticipantID < recs[j].ParticipantID })
	w := canonicalbytes.New("hcmnext.domains.merit.MeritCycle", schemaVersion).String("cycle_id", c.CycleID).Int("revision", int64(c.Revision)).Int("parent_revision", int64(c.ParentRevision)).String("parent_digest", c.ParentDigest).Value("population", c.Population).Value("guidelines", c.Guidelines).Value("budget", c.Budget).String("currency", c.Currency).String("state", string(c.State)).Value("effective_at", c.EffectiveAt).Value("known_at", c.KnownAt).Count("recommendations", len(recs))
	for _, rec := range recs {
		w.String("recommendation", rec.CanonicalDigest)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (c MeritCycle) computedDigest() string { return canonicalbytes.Digest(c.body()) }
func (c MeritCycle) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	return c.body()
}
func NewMeritCycle(c MeritCycle) (MeritCycle, error) {
	c.Recommendations = append([]MeritRecommendation(nil), c.Recommendations...)
	if c.CanonicalDigest == "" {
		if err := validateCycleWithoutDigest(c); err != nil {
			return MeritCycle{}, err
		}
		c.CanonicalDigest = c.computedDigest()
	}
	if err := c.Validate(); err != nil {
		return MeritCycle{}, err
	}
	return c, nil
}

// MeritCycleRevision is an explicit revision spelling.
type MeritCycleRevision = MeritCycle

func (c MeritCycle) Digest() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return c.CanonicalDigest, nil
}
func validateCycleWithoutDigest(c MeritCycle) error {
	d := c
	d.CanonicalDigest = ""
	return d.Validate()
}

// Propose calculates an exact amount from the frozen base pay and a guideline
// rate, then returns a successor cycle. The receiver remains unchanged.
func (c MeritCycle) Propose(participantID string, rate values.Decimal, proposedBy string) (MeritCycle, MeritRecommendation, error) {
	if err := c.Validate(); err != nil {
		return MeritCycle{}, MeritRecommendation{}, err
	}
	if c.State == CycleFinalized {
		return MeritCycle{}, MeritRecommendation{}, ErrCycleTransition
	}
	member, ok := c.member(participantID)
	if !ok {
		return MeritCycle{}, MeritRecommendation{}, fmt.Errorf("%w: participant is outside frozen population", ErrInvalidRecommendation)
	}
	if err := rate.Validate(); err != nil {
		return MeritCycle{}, MeritRecommendation{}, fmt.Errorf("%w: rate: %v", ErrInvalidRecommendation, err)
	}
	rule, err := c.Guidelines.Lookup(member.PerformanceRating, member.BandPosition)
	if err != nil {
		return MeritCycle{}, MeritRecommendation{}, err
	}
	if rate.Cmp(rule.MinimumRate) < 0 || rate.Cmp(rule.MaximumRate) > 0 {
		return MeritCycle{}, MeritRecommendation{}, fmt.Errorf("%w: rate is outside guideline range", ErrInvalidRecommendation)
	}
	amount, err := member.BasePay.MulDecimal(rate, c.Budget.Scale(), c.Budget.Rounding())
	if err != nil {
		return MeritCycle{}, MeritRecommendation{}, err
	}
	if amount.Currency() != c.Currency {
		return MeritCycle{}, MeritRecommendation{}, fmt.Errorf("%w: base pay currency differs from cycle", ErrInvalidRecommendation)
	}
	rec, err := NewMeritRecommendation(MeritRecommendation{CycleID: c.CycleID, CycleRevision: c.Revision, ParticipantID: participantID, BasePay: member.BasePay, PerformanceRating: member.PerformanceRating, BandPosition: member.BandPosition, SalaryRevisionRef: member.SalaryRevisionRef, PerformanceRef: member.PerformanceRef, Rate: rate, Amount: amount.Amount(), GuidelineDigest: c.Guidelines.CanonicalDigest, ProposedBy: proposedBy, State: RecommendationProposed})
	if err != nil {
		return MeritCycle{}, MeritRecommendation{}, err
	}
	next, err := c.appendRecommendation(rec)
	return next, rec, err
}

// AddRecommendation appends an already calculated, frozen recommendation.
func (c MeritCycle) AddRecommendation(rec MeritRecommendation) (MeritCycle, error) {
	if err := c.Validate(); err != nil {
		return MeritCycle{}, err
	}
	if err := rec.Validate(); err != nil {
		return MeritCycle{}, err
	}
	member, ok := c.member(rec.ParticipantID)
	if !ok {
		return MeritCycle{}, fmt.Errorf("%w: participant is outside frozen population", ErrInvalidRecommendation)
	}
	if rec.CycleID != c.CycleID || rec.CycleRevision != c.Revision || !rec.BasePay.Amount().Equal(member.BasePay.Amount()) || rec.BasePay.Currency() != member.BasePay.Currency() || !rec.PerformanceRating.Equal(member.PerformanceRating) || !rec.BandPosition.Equal(member.BandPosition) || rec.SalaryRevisionRef != member.SalaryRevisionRef || rec.PerformanceRef != member.PerformanceRef {
		return MeritCycle{}, ErrStalePopulationFact
	}
	rule, err := c.Guidelines.Lookup(member.PerformanceRating, member.BandPosition)
	if err != nil {
		return MeritCycle{}, err
	}
	if rec.Rate.Cmp(rule.MinimumRate) < 0 || rec.Rate.Cmp(rule.MaximumRate) > 0 {
		return MeritCycle{}, fmt.Errorf("%w: rate is outside guideline range", ErrInvalidRecommendation)
	}
	expected, err := member.BasePay.MulDecimal(rec.Rate, c.Budget.Scale(), c.Budget.Rounding())
	if err != nil || !expected.Amount().Equal(rec.Amount) {
		return MeritCycle{}, ErrStalePopulationFact
	}
	return c.appendRecommendation(rec)
}

func (c MeritCycle) appendRecommendation(rec MeritRecommendation) (MeritCycle, error) {
	for _, prior := range c.Recommendations {
		if prior.ParticipantID == rec.ParticipantID {
			return MeritCycle{}, ErrRecommendationDuplicate
		}
	}
	next := c
	next.Revision, next.ParentRevision, next.ParentDigest = c.Revision+1, c.Revision, c.CanonicalDigest
	next.Recommendations = append(append([]MeritRecommendation(nil), c.Recommendations...), rec)
	next.CanonicalDigest = ""
	if next.State == CycleDraft {
		next.State = CycleOpen
	}
	return NewMeritCycle(next)
}

// Calibrate appends a decision to one recommendation and applies no silent
// rewrite. A manager is rejected as the only adjuster, per PERFORMANCE-005.
func (c MeritCycle) Calibrate(participantID string, adjustment CalibrationAdjustment) (MeritCycle, error) {
	if err := c.Validate(); err != nil {
		return MeritCycle{}, err
	}
	if c.State == CycleFinalized {
		return MeritCycle{}, ErrCorrectionAfterFinalization
	}
	recIndex := -1
	for i := range c.Recommendations {
		if c.Recommendations[i].ParticipantID == participantID {
			recIndex = i
			break
		}
	}
	if recIndex < 0 {
		return MeritCycle{}, fmt.Errorf("%w: participant has no recommendation", ErrInvalidAdjustment)
	}
	normalized, err := NewCalibrationAdjustment(adjustment)
	if err != nil {
		return MeritCycle{}, err
	}
	rec := c.Recommendations[recIndex]
	current := rec.Amount
	for _, prior := range rec.Adjustments {
		if prior.ParticipantID != participantID || !prior.From.Equal(current) {
			return MeritCycle{}, fmt.Errorf("%w: adjustment history does not lead from recommendation", ErrInvalidAdjustment)
		}
		current = prior.To
	}
	if !normalized.From.Equal(current) {
		return MeritCycle{}, fmt.Errorf("%w: from amount does not match current recommendation", ErrInvalidAdjustment)
	}
	member, _ := c.member(participantID)
	if normalized.AdjusterID == member.ManagerID {
		return MeritCycle{}, ErrCalibrationSeparation
	}
	for _, prior := range rec.Adjustments {
		if prior.CanonicalDigest == normalized.CanonicalDigest {
			return MeritCycle{}, fmt.Errorf("%w: duplicate adjustment", ErrInvalidAdjustment)
		}
	}
	rec.Adjustments = append(append([]CalibrationAdjustment(nil), rec.Adjustments...), normalized)
	rec.Amount = normalized.To
	rec.State = RecommendationAdjusted
	rec.CanonicalDigest = ""
	rec, err = NewMeritRecommendation(rec)
	if err != nil {
		return MeritCycle{}, err
	}
	next := c
	next.Revision, next.ParentRevision, next.ParentDigest = c.Revision+1, c.Revision, c.CanonicalDigest
	next.Recommendations = append([]MeritRecommendation(nil), c.Recommendations...)
	next.Recommendations[recIndex] = rec
	next.State = CycleCalibrating
	next.CanonicalDigest = ""
	return NewMeritCycle(next)
}

// ApprovalReceipt is authority evidence bound to the exact recommendation
// revision presented to an approval system.
type ApprovalReceipt struct {
	ReceiptID            string
	ApproverID           string
	RecommendationDigest string
}

// ApprovalVerifier is supplied by the authorization boundary. Implementations
// must authenticate the receipt; this domain fails closed without one.
type ApprovalVerifier interface {
	VerifyMeritApproval(ApprovalReceipt) error
}

type RejectionReceipt = ApprovalReceipt

type RejectionVerifier interface {
	VerifyMeritRejection(RejectionReceipt) error
}

// StoredDecisionEvidence is the typed persistence envelope for a verified
// decision. Rehydration still requires an adapter verifier; stored flags alone
// never mint a domain seal.
type StoredDecisionEvidence struct {
	Version            uint64
	ApprovedBy         string
	ApprovalReceiptID  string
	ApprovedDigest     string
	RejectedBy         string
	RejectionReceiptID string
	RejectedDigest     string
	EffectRevision     uint64
}

type StoredDecisionVerifier interface {
	VerifyStoredMeritDecision(MeritRecommendation, StoredDecisionEvidence) error
}

func RehydrateMeritRecommendation(r MeritRecommendation, evidence StoredDecisionEvidence, verifier StoredDecisionVerifier) (MeritRecommendation, error) {
	if evidence.Version != 1 || verifier == nil {
		return MeritRecommendation{}, fmt.Errorf("%w: authenticated stored decision evidence is required", ErrInvalidRecommendation)
	}
	if err := verifier.VerifyStoredMeritDecision(r, evidence); err != nil {
		return MeritRecommendation{}, fmt.Errorf("%w: stored decision evidence: %v", ErrInvalidRecommendation, err)
	}
	r.ApprovedBy, r.ApprovalReceiptID, r.ApprovedDigest = evidence.ApprovedBy, evidence.ApprovalReceiptID, evidence.ApprovedDigest
	r.RejectedBy, r.RejectionReceiptID, r.RejectedDigest = evidence.RejectedBy, evidence.RejectionReceiptID, evidence.RejectedDigest
	r.EffectRevision = evidence.EffectRevision
	r.approvalVerified = r.State == RecommendationApproved || r.State == RecommendationFinalized
	r.rejectionVerified = r.State == RecommendationRejected
	r.verifiedDecisionDigest = r.decisionPayloadDigest()
	return NewMeritRecommendation(r)
}

// Reject turns an existing proposal into an explicit, governed no-award
// outcome. The receipt is bound to the proposal that the decision rejected.
func (c MeritCycle) Reject(participantID string, receipt RejectionReceipt, verifier RejectionVerifier) (MeritCycle, error) {
	if err := c.Validate(); err != nil {
		return MeritCycle{}, err
	}
	if verifier == nil || strings.TrimSpace(receipt.ReceiptID) == "" || strings.TrimSpace(receipt.ApproverID) == "" || strings.TrimSpace(receipt.RecommendationDigest) == "" {
		return MeritCycle{}, fmt.Errorf("%w: verified rejection receipt is required", ErrCycleTransition)
	}
	if err := verifier.VerifyMeritRejection(receipt); err != nil {
		return MeritCycle{}, fmt.Errorf("%w: rejection receipt: %v", ErrCycleTransition, err)
	}
	next := c
	next.Recommendations = append([]MeritRecommendation(nil), c.Recommendations...)
	for i := range next.Recommendations {
		r := &next.Recommendations[i]
		if r.ParticipantID != participantID {
			continue
		}
		if r.State != RecommendationProposed && r.State != RecommendationAdjusted {
			return MeritCycle{}, ErrCycleTransition
		}
		if receipt.RecommendationDigest != r.CanonicalDigest {
			return MeritCycle{}, fmt.Errorf("%w: rejection receipt is stale", ErrCycleTransition)
		}
		r.Rate, r.Amount = zeroLike(r.Rate), zeroLike(r.Amount)
		r.State, r.RejectedBy, r.RejectionReceiptID, r.RejectedDigest = RecommendationRejected, receipt.ApproverID, receipt.ReceiptID, receipt.RecommendationDigest
		r.rejectionVerified, r.CanonicalDigest = true, ""
		r.verifiedDecisionDigest = r.decisionPayloadDigest()
		var err error
		*r, err = NewMeritRecommendation(*r)
		if err != nil {
			return MeritCycle{}, err
		}
		next.Revision, next.ParentRevision, next.ParentDigest, next.CanonicalDigest = c.Revision+1, c.Revision, c.CanonicalDigest, ""
		next.State = CycleApproved
		return NewMeritCycle(next)
	}
	return MeritCycle{}, fmt.Errorf("%w: participant has no recommendation", ErrCycleTransition)
}

func (c MeritCycle) Approve(participantID string, receipt ApprovalReceipt, verifier ApprovalVerifier) (MeritCycle, error) {
	if err := c.Validate(); err != nil {
		return MeritCycle{}, err
	}
	if verifier == nil || strings.TrimSpace(receipt.ReceiptID) == "" || strings.TrimSpace(receipt.ApproverID) == "" || strings.TrimSpace(receipt.RecommendationDigest) == "" {
		return MeritCycle{}, fmt.Errorf("%w: verified approval receipt is required", ErrCycleTransition)
	}
	if err := verifier.VerifyMeritApproval(receipt); err != nil {
		return MeritCycle{}, fmt.Errorf("%w: approval receipt: %v", ErrCycleTransition, err)
	}
	next := c
	found := false
	next.Recommendations = append([]MeritRecommendation(nil), c.Recommendations...)
	for i := range next.Recommendations {
		if next.Recommendations[i].ParticipantID == participantID {
			if next.Recommendations[i].State != RecommendationProposed && next.Recommendations[i].State != RecommendationAdjusted {
				return MeritCycle{}, ErrCycleTransition
			}
			if receipt.RecommendationDigest != next.Recommendations[i].CanonicalDigest {
				return MeritCycle{}, fmt.Errorf("%w: approval receipt is stale", ErrCycleTransition)
			}
			next.Recommendations[i].State = RecommendationApproved
			next.Recommendations[i].ApprovedBy = receipt.ApproverID
			next.Recommendations[i].ApprovalReceiptID = receipt.ReceiptID
			next.Recommendations[i].ApprovedDigest = receipt.RecommendationDigest
			next.Recommendations[i].approvalVerified = true
			next.Recommendations[i].verifiedDecisionDigest = next.Recommendations[i].decisionPayloadDigest()
			next.Recommendations[i].CanonicalDigest = ""
			var err error
			next.Recommendations[i], err = NewMeritRecommendation(next.Recommendations[i])
			if err != nil {
				return MeritCycle{}, err
			}
			found = true
			break
		}
	}
	if !found {
		return MeritCycle{}, fmt.Errorf("%w: participant has no recommendation", ErrCycleTransition)
	}
	next.Revision, next.ParentRevision, next.ParentDigest, next.CanonicalDigest = c.Revision+1, c.Revision, c.CanonicalDigest, ""
	next.State = CycleApproved
	return NewMeritCycle(next)
}

// Finalize returns a successor cycle only when every recommendation is
// approved or explicitly rejected and the exact total remains within the pool.
func (c MeritCycle) Finalize() (MeritCycle, error) {
	if err := c.Validate(); err != nil {
		return MeritCycle{}, err
	}
	if len(c.Recommendations) == 0 {
		return MeritCycle{}, fmt.Errorf("%w: recommendations are required", ErrCycleTransition)
	}
	if len(c.Recommendations) != len(c.Population.Members) {
		return MeritCycle{}, fmt.Errorf("%w: every frozen-population worker must have an explicit outcome", ErrCycleTransition)
	}
	seen := make(map[string]struct{}, len(c.Recommendations))
	for _, rec := range c.Recommendations {
		seen[rec.ParticipantID] = struct{}{}
		if rec.State != RecommendationApproved && rec.State != RecommendationRejected {
			return MeritCycle{}, fmt.Errorf("%w: every recommendation must be approved or explicitly rejected", ErrCycleTransition)
		}
	}
	for _, member := range c.Population.Members {
		if _, ok := seen[member.ParticipantID]; !ok {
			return MeritCycle{}, fmt.Errorf("%w: worker %q has no explicit outcome", ErrCycleTransition, member.ParticipantID)
		}
	}
	next := c
	next.Revision, next.ParentRevision, next.ParentDigest, next.CanonicalDigest = c.Revision+1, c.Revision, c.CanonicalDigest, ""
	next.State = CycleFinalized
	next.Recommendations = append([]MeritRecommendation(nil), c.Recommendations...)
	for i := range next.Recommendations {
		if next.Recommendations[i].State == RecommendationRejected {
			continue
		}
		next.Recommendations[i].State = RecommendationFinalized
		if next.Recommendations[i].EffectRevision == 0 {
			next.Recommendations[i].EffectRevision = next.Revision
		}
		next.Recommendations[i].CanonicalDigest = ""
		var err error
		next.Recommendations[i], err = NewMeritRecommendation(next.Recommendations[i])
		if err != nil {
			return MeritCycle{}, err
		}
	}
	return NewMeritCycle(next)
}

func (c MeritCycle) member(id string) (PopulationMember, bool) {
	for _, m := range c.Population.Members {
		if m.ParticipantID == id {
			return m, true
		}
	}
	return PopulationMember{}, false
}
func (c MeritCycle) ConsumedBudget() (values.Decimal, error) {
	if err := c.Validate(); err != nil {
		return values.Decimal{}, err
	}
	total := zeroLike(c.Budget)
	for _, rec := range c.Recommendations {
		var err error
		total, err = total.Add(rec.Amount)
		if err != nil {
			return values.Decimal{}, err
		}
	}
	return total, nil
}
func (c MeritCycle) RemainingBudget() (values.Decimal, error) {
	used, err := c.ConsumedBudget()
	if err != nil {
		return values.Decimal{}, err
	}
	return c.Budget.Sub(used)
}

// MeritCycleExplanation is intentionally bounded and does not expose worker
// IDs, salary, ratings, rates, or calibration narratives.
type MeritCycleExplanation struct {
	CycleID             string
	Revision            uint64
	State               MeritCycleState
	PopulationDigest    string
	GuidelineDigest     string
	RecommendationCount int
	ConsumedBudget      values.Decimal
	Budget              values.Decimal
	Digest              string
}

func (c MeritCycle) Explain() (MeritCycleExplanation, error) {
	used, err := c.ConsumedBudget()
	if err != nil {
		return MeritCycleExplanation{}, err
	}
	return MeritCycleExplanation{CycleID: c.CycleID, Revision: c.Revision, State: c.State, PopulationDigest: c.Population.CanonicalDigest, GuidelineDigest: c.Guidelines.CanonicalDigest, RecommendationCount: len(c.Recommendations), ConsumedBudget: used, Budget: c.Budget, Digest: c.CanonicalDigest}, nil
}
func Explain(c MeritCycle) (MeritCycleExplanation, error) { return c.Explain() }

// CycleStore is an in-memory fake port for callers that need current-cycle
// uniqueness without introducing a database dependency.
type CycleStore interface {
	Save(MeritCycle) error
	Current(cycleID string) (MeritCycle, error)
}
type MemoryCycleStore struct {
	mu      sync.RWMutex
	current map[string]MeritCycle
}

func NewMemoryCycleStore() *MemoryCycleStore {
	return &MemoryCycleStore{current: make(map[string]MeritCycle)}
}
func (m *MemoryCycleStore) Save(c MeritCycle) error {
	if err := c.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	prior, ok := m.current[c.CycleID]
	if !ok {
		if c.Revision != 1 {
			return ErrInvalidRevisionLineage
		}
		m.current[c.CycleID] = c
		return nil
	}
	if c.Revision != prior.Revision+1 || c.ParentDigest != prior.CanonicalDigest {
		return ErrInvalidRevisionLineage
	}
	m.current[c.CycleID] = c
	return nil
}
func (m *MemoryCycleStore) Current(id string) (MeritCycle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.current[id]
	if !ok {
		return MeritCycle{}, fmt.Errorf("%w: cycle_id %q", ErrInvalidCycle, id)
	}
	return c, nil
}
