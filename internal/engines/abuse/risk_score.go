package abuse

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

var (
	ErrInvalidWeightingTable      = errors.New("abuse: invalid risk weighting table")
	ErrInvalidRiskWindow          = errors.New("abuse: invalid risk scoring window")
	ErrInvalidRiskFinding         = errors.New("abuse: invalid risk scoring finding")
	ErrFindingPrincipalMismatch   = errors.New("abuse: finding principal does not match assessment principal")
	ErrFindingWeightMissing       = errors.New("abuse: finding category has no declared risk weight")
	ErrInvalidRiskAssessment      = errors.New("abuse: invalid risk assessment")
	ErrInvalidHumanReview         = errors.New("abuse: invalid human review record")
	ErrInvalidConsequenceMarker   = errors.New("abuse: invalid consequence marker")
	ErrHumanReviewRequired        = errors.New("abuse: a human review record is required before attaching a consequence marker")
	ErrReviewAssessmentMismatch   = errors.New("abuse: human review is bound to a different assessment")
	ErrReviewRecordMarkerMismatch = errors.New("abuse: consequence marker is bound to a different review record")
)

// FindingWeight is one declared contribution in a versioned scoring table.
// Confidence is the scorer's bounded confidence in this contribution, not a
// personnel outcome or a statement about the principal.
type FindingWeight struct {
	Category   FindingCategory
	Weight     int
	Confidence int
}

// WeightingTable declares the complete versioned scoring calibration. The
// table is copied and canonically digested by NewWeightingTable or
// NewRiskScorer; callers cannot change the scorer's table through a slice.
type WeightingTable struct {
	ID              string
	Version         string
	Weights         []FindingWeight
	MaxScore        int
	ReviewThreshold int
	Digest          string
}

func (t WeightingTable) Validate() error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Version) == "" {
		return fmt.Errorf("%w: id and version are required", ErrInvalidWeightingTable)
	}
	if t.MaxScore <= 0 || t.ReviewThreshold <= 0 || t.ReviewThreshold > t.MaxScore {
		return fmt.Errorf("%w: score bounds are invalid", ErrInvalidWeightingTable)
	}
	if len(t.Weights) == 0 {
		return fmt.Errorf("%w: at least one finding weight is required", ErrInvalidWeightingTable)
	}
	seen := make(map[FindingCategory]struct{}, len(t.Weights))
	for _, weight := range t.Weights {
		if !findingCategoryValid(weight.Category) {
			return fmt.Errorf("%w: unknown finding category %q", ErrInvalidWeightingTable, weight.Category)
		}
		if weight.Weight < 0 || weight.Weight > t.MaxScore {
			return fmt.Errorf("%w: weight for %s is outside score bounds", ErrInvalidWeightingTable, weight.Category)
		}
		if weight.Confidence < 0 || weight.Confidence > 100 {
			return fmt.Errorf("%w: confidence for %s is outside 0..100", ErrInvalidWeightingTable, weight.Category)
		}
		if _, exists := seen[weight.Category]; exists {
			return fmt.Errorf("%w: duplicate finding category %q", ErrInvalidWeightingTable, weight.Category)
		}
		seen[weight.Category] = struct{}{}
	}
	return nil
}

func (t WeightingTable) canonical() []byte {
	if t.Validate() != nil {
		return nil
	}
	weights := append([]FindingWeight(nil), t.Weights...)
	sort.Slice(weights, func(i, j int) bool { return weights[i].Category < weights[j].Category })
	w := canonicalbytes.New("hcmnext.engines.abuse.WeightingTable", 1).
		String("id", t.ID).
		String("version", t.Version).
		Int("max_score", int64(t.MaxScore)).
		Int("review_threshold", int64(t.ReviewThreshold)).
		Count("weights", len(weights))
	for _, weight := range weights {
		w.String("category", string(weight.Category)).
			Int("weight", int64(weight.Weight)).
			Int("confidence", int64(weight.Confidence))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// NewWeightingTable validates and returns a versioned, immutable-by-value
// weighting table.
func NewWeightingTable(id, version string, weights []FindingWeight, maxScore, reviewThreshold int) (WeightingTable, error) {
	table := WeightingTable{
		ID: id, Version: version, Weights: append([]FindingWeight(nil), weights...),
		MaxScore: maxScore, ReviewThreshold: reviewThreshold,
	}
	if err := table.Validate(); err != nil {
		return WeightingTable{}, err
	}
	table.Digest = canonicalbytes.Digest(table.canonical())
	return table, nil
}

// NewRiskWeightingTable is a descriptive constructor alias.
func NewRiskWeightingTable(id, version string, weights []FindingWeight, maxScore, reviewThreshold int) (WeightingTable, error) {
	return NewWeightingTable(id, version, weights, maxScore, reviewThreshold)
}

func (t WeightingTable) weight(category FindingCategory) (FindingWeight, bool) {
	for _, weight := range t.Weights {
		if weight.Category == category {
			return weight, true
		}
	}
	return FindingWeight{}, false
}

func findingCategoryValid(category FindingCategory) bool {
	switch category {
	case FindingBulkExport, FindingBankDetailChange, FindingPayRateChange,
		FindingNetPayRedirect, FindingSelfGrant, FindingRoleEscalation,
		FindingFakeWorker, FindingPrivilegeBurst:
		return true
	default:
		return false
	}
}

// RiskWindow identifies the exact interval for one principal assessment.
type RiskWindow struct {
	Start time.Time
	End   time.Time
}

func (w RiskWindow) Validate() error {
	if w.Start.IsZero() || w.End.IsZero() || !w.Start.Before(w.End) {
		return ErrInvalidRiskWindow
	}
	return nil
}

// FindingRef is the stable, minimized reference retained in an assessment.
// It names the contributing finding and never copies finding evidence or raw
// activity content.
type FindingRef struct {
	DetectorID     string
	DetectorSemver string
	DetectorDigest string
	SignalID       string
	Category       FindingCategory
}

func (r FindingRef) Validate() error {
	if strings.TrimSpace(r.DetectorID) == "" || strings.TrimSpace(r.DetectorSemver) == "" ||
		strings.TrimSpace(r.DetectorDigest) == "" || strings.TrimSpace(r.SignalID) == "" ||
		!findingCategoryValid(r.Category) {
		return ErrInvalidRiskFinding
	}
	return nil
}

func (r FindingRef) key() string {
	return r.DetectorID + "\x00" + r.DetectorSemver + "\x00" + r.DetectorDigest + "\x00" + r.SignalID + "\x00" + string(r.Category)
}

func findingRef(f RiskFinding) FindingRef {
	return FindingRef{
		DetectorID: f.DetectorID, DetectorSemver: f.DetectorSemver,
		DetectorDigest: f.DetectorDigest, SignalID: f.SignalID, Category: f.Category,
	}
}

// RiskAssessment is a bounded, review-oriented score for one principal and
// window. It deliberately has no accusation, misconduct, conclusion, state,
// or employment-action field.
type RiskAssessment struct {
	Principal               string
	WindowStart             time.Time
	WindowEnd               time.Time
	WeightingTableID        string
	WeightingTableVersion   string
	WeightingTableDigest    string
	Score                   int
	ContributingFindingRefs []FindingRef
	Confidence              int
	ReviewRequired          bool
	Digest                  string
}

func (a RiskAssessment) Validate() error {
	if strings.TrimSpace(a.Principal) == "" || a.WindowStart.IsZero() || a.WindowEnd.IsZero() || !a.WindowStart.Before(a.WindowEnd) {
		return ErrInvalidRiskAssessment
	}
	if strings.TrimSpace(a.WeightingTableID) == "" || strings.TrimSpace(a.WeightingTableVersion) == "" || strings.TrimSpace(a.WeightingTableDigest) == "" {
		return ErrInvalidRiskAssessment
	}
	if a.Score < 0 || a.Score > 100 || a.Confidence < 0 || a.Confidence > 100 {
		return ErrInvalidRiskAssessment
	}
	seen := make(map[string]struct{}, len(a.ContributingFindingRefs))
	for _, ref := range a.ContributingFindingRefs {
		if err := ref.Validate(); err != nil {
			return err
		}
		if _, exists := seen[ref.key()]; exists {
			return ErrInvalidRiskAssessment
		}
		seen[ref.key()] = struct{}{}
	}
	if a.Digest == "" || a.Digest != a.computedDigest() {
		return ErrInvalidRiskAssessment
	}
	return nil
}

func (a RiskAssessment) computedDigest() string {
	refs := append([]FindingRef(nil), a.ContributingFindingRefs...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].key() < refs[j].key() })
	w := canonicalbytes.New("hcmnext.engines.abuse.RiskAssessment", 1).
		String("principal", a.Principal).
		String("window_start", a.WindowStart.UTC().Format(time.RFC3339Nano)).
		String("window_end", a.WindowEnd.UTC().Format(time.RFC3339Nano)).
		String("table_id", a.WeightingTableID).
		String("table_version", a.WeightingTableVersion).
		String("table_digest", a.WeightingTableDigest).
		Int("score", int64(a.Score)).
		Int("confidence", int64(a.Confidence)).
		Bool("review_required", a.ReviewRequired).
		Count("finding_refs", len(refs))
	for _, ref := range refs {
		w.String("detector_id", ref.DetectorID).
			String("detector_semver", ref.DetectorSemver).
			String("detector_digest", ref.DetectorDigest).
			String("signal_id", ref.SignalID).
			String("category", string(ref.Category))
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// FindingRefs returns a defensive copy of the contributing references.
func (a RiskAssessment) FindingRefs() []FindingRef {
	return append([]FindingRef(nil), a.ContributingFindingRefs...)
}

// RiskScorer evaluates typed findings using one declared weighting table.
type RiskScorer struct{ table WeightingTable }

func NewRiskScorer(table WeightingTable) (RiskScorer, error) {
	if err := table.Validate(); err != nil {
		return RiskScorer{}, err
	}
	canonical := append([]FindingWeight(nil), table.Weights...)
	table.Weights = canonical
	table.Digest = canonicalbytes.Digest(table.canonical())
	return RiskScorer{table: table}, nil
}

func (s RiskScorer) WeightingTable() WeightingTable {
	table := s.table
	table.Weights = append([]FindingWeight(nil), table.Weights...)
	return table
}

// Score combines findings already resolved for principal and window. The
// caller supplies that scope explicitly because event-level findings may only
// carry opaque evidence references. No persistence or side effect occurs.
func (s RiskScorer) Score(principal string, window RiskWindow, findings []RiskFinding) (RiskAssessment, error) {
	if err := s.table.Validate(); err != nil {
		return RiskAssessment{}, err
	}
	if err := window.Validate(); err != nil || strings.TrimSpace(principal) == "" {
		return RiskAssessment{}, ErrInvalidRiskWindow
	}
	refs := make([]FindingRef, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	total := 0
	confidenceNumerator := 0
	confidenceDenominator := 0
	for _, finding := range findings {
		if strings.TrimSpace(finding.DetectorID) == "" || strings.TrimSpace(finding.DetectorSemver) == "" ||
			strings.TrimSpace(finding.DetectorDigest) == "" || strings.TrimSpace(finding.SignalID) == "" ||
			!findingCategoryValid(finding.Category) || !finding.Severity.Valid() {
			return RiskAssessment{}, fmt.Errorf("%w: finding %q", ErrInvalidRiskFinding, finding.SignalID)
		}
		if finding.Evidence.Principal != "" && finding.Evidence.Principal != principal {
			return RiskAssessment{}, fmt.Errorf("%w: %s", ErrFindingPrincipalMismatch, finding.SignalID)
		}
		weight, ok := s.table.weight(finding.Category)
		if !ok {
			return RiskAssessment{}, fmt.Errorf("%w: %s", ErrFindingWeightMissing, finding.Category)
		}
		ref := findingRef(finding)
		if _, exists := seen[ref.key()]; exists {
			return RiskAssessment{}, fmt.Errorf("%w: duplicate finding %s", ErrInvalidRiskFinding, finding.SignalID)
		}
		seen[ref.key()] = struct{}{}
		refs = append(refs, ref)
		total += weight.Weight
		confidenceNumerator += weight.Weight * weight.Confidence
		confidenceDenominator += weight.Weight
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].key() < refs[j].key() })
	if total > s.table.MaxScore {
		total = s.table.MaxScore
	}
	confidence := 0
	if confidenceDenominator > 0 {
		confidence = confidenceNumerator / confidenceDenominator
	}
	assessment := RiskAssessment{
		Principal: principal, WindowStart: window.Start.UTC(), WindowEnd: window.End.UTC(),
		WeightingTableID: s.table.ID, WeightingTableVersion: s.table.Version, WeightingTableDigest: s.table.Digest,
		Score: total, ContributingFindingRefs: refs, Confidence: confidence,
		ReviewRequired: total >= s.table.ReviewThreshold,
	}
	assessment.Digest = assessment.computedDigest()
	return assessment, nil
}

// ScoreRisk is the package-level scorer form.
func ScoreRisk(s RiskScorer, principal string, window RiskWindow, findings []RiskFinding) (RiskAssessment, error) {
	return s.Score(principal, window, findings)
}

// RiskAssessmentExplanation names only the contributing finding references;
// it does not invent a conclusion or expose a personnel decision.
type RiskAssessmentExplanation struct {
	FindingRefs []FindingRef
}

func (a RiskAssessment) Explain() (RiskAssessmentExplanation, error) {
	if err := a.Validate(); err != nil {
		return RiskAssessmentExplanation{}, err
	}
	return RiskAssessmentExplanation{FindingRefs: a.FindingRefs()}, nil
}

func ExplainRiskAssessment(a RiskAssessment) (RiskAssessmentExplanation, error) {
	return a.Explain()
}

func (s RiskScorer) Explain(a RiskAssessment) (RiskAssessmentExplanation, error) {
	return a.Explain()
}

// ReviewDecision is recorded by a human reviewer before a consequence marker
// may be attached. It is intentionally separate from RiskAssessment.
type ReviewDecision string

const (
	ReviewAcknowledged ReviewDecision = "ACKNOWLEDGED"
	ReviewDismissed    ReviewDecision = "DISMISSED"
	ReviewInconclusive ReviewDecision = "INCONCLUSIVE"
)

func (d ReviewDecision) Valid() bool {
	return d == ReviewAcknowledged || d == ReviewDismissed || d == ReviewInconclusive
}

type HumanReviewRecord struct {
	RecordID         string
	ReviewerID       string
	AssessmentDigest string
	ReviewedAt       time.Time
	Decision         ReviewDecision
}

func (r HumanReviewRecord) Validate() error {
	if strings.TrimSpace(r.RecordID) == "" || strings.TrimSpace(r.ReviewerID) == "" ||
		strings.TrimSpace(r.AssessmentDigest) == "" || r.ReviewedAt.IsZero() || !r.Decision.Valid() {
		return ErrInvalidHumanReview
	}
	return nil
}

// ConsequenceMarker is an opaque marker supplied by a consequence owner
// after human review. This type is never produced by RiskScorer.Score.
type ConsequenceMarker struct {
	MarkerID       string
	Kind           string
	ReviewRecordID string
	AttachedAt     time.Time
}

func (m ConsequenceMarker) Validate() error {
	if strings.TrimSpace(m.MarkerID) == "" || strings.TrimSpace(m.Kind) == "" ||
		strings.TrimSpace(m.ReviewRecordID) == "" || m.AttachedAt.IsZero() {
		return ErrInvalidConsequenceMarker
	}
	return nil
}

type ReviewedConsequence struct {
	AssessmentDigest string
	Review           HumanReviewRecord
	Marker           ConsequenceMarker
}

// AttachConsequenceMarker is the only constructor for a reviewed consequence
// link. A valid human review record, bound to this exact assessment, is
// mandatory; scoring never calls this function.
func AttachConsequenceMarker(assessment RiskAssessment, review HumanReviewRecord, marker ConsequenceMarker) (ReviewedConsequence, error) {
	if err := assessment.Validate(); err != nil {
		return ReviewedConsequence{}, err
	}
	if err := review.Validate(); err != nil {
		return ReviewedConsequence{}, fmt.Errorf("%w: %v", ErrHumanReviewRequired, err)
	}
	if review.AssessmentDigest != assessment.Digest {
		return ReviewedConsequence{}, ErrReviewAssessmentMismatch
	}
	if err := marker.Validate(); err != nil {
		return ReviewedConsequence{}, err
	}
	if marker.ReviewRecordID != review.RecordID {
		return ReviewedConsequence{}, ErrReviewRecordMarkerMismatch
	}
	return ReviewedConsequence{AssessmentDigest: assessment.Digest, Review: review, Marker: marker}, nil
}

// AttachReviewedConsequence is a descriptive alias.
func AttachReviewedConsequence(assessment RiskAssessment, review HumanReviewRecord, marker ConsequenceMarker) (ReviewedConsequence, error) {
	return AttachConsequenceMarker(assessment, review, marker)
}
