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
	ErrInvalidRatingRule      = errors.New("performance: invalid rating rule")
	ErrRatingRuleWeight       = errors.New("performance: rating rule has no weight for relationship")
	ErrInvalidRatingInput     = errors.New("performance: invalid review rating input")
	ErrInsufficientReviews    = errors.New("performance: minimum review count is not met")
	ErrInvalidProposedRating  = errors.New("performance: invalid proposed rating")
	ErrInvalidOutlierHandling = errors.New("performance: outlier handling is not declared")
)

// OutlierHandling is a closed set. The trim policy removes one lowest and one
// highest rating after the latest review for each reviewer has been selected.
type OutlierHandling string

const (
	OutlierHandlingNone         OutlierHandling = "NONE"
	OutlierHandlingTrimExtremes OutlierHandling = "TRIM_ONE_EACH_SIDE"
	OutlierNone                 OutlierHandling = OutlierHandlingNone
	OutlierTrimOneEachSide      OutlierHandling = OutlierHandlingTrimExtremes
	OutlierHandlingDropExtremes OutlierHandling = OutlierHandlingTrimExtremes
)

func (h OutlierHandling) Valid() bool {
	return h == OutlierHandlingNone || h == OutlierHandlingTrimExtremes
}

// ProposedRatingRule is the declared, versioned calculation rule. All
// arithmetic is performed with values.Decimal; no floating-point conversion is
// involved. A missing Digest is filled by the constructors and calculation
// boundary so callers may declare a rule as a value literal safely.
type ProposedRatingRule struct {
	ID              string
	Version         string
	Digest          string
	RatingScale     int32
	ResultScale     int32
	Rounding        values.RoundingMode
	MinimumReviews  int
	Weights         map[ReviewerRelationshipKind]values.Decimal
	OutlierHandling OutlierHandling
}

// RatingRule is the shorter spelling for a proposed-rating rule.
type RatingRule = ProposedRatingRule

func NewProposedRatingRule(id, version string, ratingScale, resultScale int32, rounding values.RoundingMode, minimumReviews int, weights map[ReviewerRelationshipKind]values.Decimal, outliers OutlierHandling) (ProposedRatingRule, error) {
	rule := ProposedRatingRule{ID: id, Version: version, RatingScale: ratingScale, ResultScale: resultScale, Rounding: rounding, MinimumReviews: minimumReviews, Weights: copyWeights(weights), OutlierHandling: outliers}
	return rule.withDigest()
}

func NewRatingRule(id, version string, ratingScale, resultScale int32, rounding values.RoundingMode, minimumReviews int, weights map[ReviewerRelationshipKind]values.Decimal, outliers OutlierHandling) (ProposedRatingRule, error) {
	return NewProposedRatingRule(id, version, ratingScale, resultScale, rounding, minimumReviews, weights, outliers)
}

func copyWeights(weights map[ReviewerRelationshipKind]values.Decimal) map[ReviewerRelationshipKind]values.Decimal {
	copyOf := make(map[ReviewerRelationshipKind]values.Decimal, len(weights))
	for relationship, weight := range weights {
		copyOf[relationship] = weight
	}
	return copyOf
}

func (r ProposedRatingRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("%w: id and version are required", ErrInvalidRatingRule)
	}
	if r.RatingScale < 0 || r.RatingScale > values.MaxScale || r.ResultScale < 0 || r.ResultScale > values.MaxScale {
		return fmt.Errorf("%w: decimal scale is outside [0,%d]", ErrInvalidRatingRule, values.MaxScale)
	}
	if _, err := values.NewDecimal("0", 0, r.Rounding); err != nil {
		return fmt.Errorf("%w: rounding: %v", ErrInvalidRatingRule, err)
	}
	if r.MinimumReviews <= 0 {
		return fmt.Errorf("%w: minimum review count must be positive", ErrInvalidRatingRule)
	}
	if !r.OutlierHandling.Valid() {
		return fmt.Errorf("%w: %w %q", ErrInvalidRatingRule, ErrInvalidOutlierHandling, r.OutlierHandling)
	}
	if len(r.Weights) == 0 {
		return fmt.Errorf("%w: at least one relationship weight is required", ErrInvalidRatingRule)
	}
	for relationship, weight := range r.Weights {
		if !relationship.Valid() {
			return fmt.Errorf("%w: unknown relationship %q", ErrInvalidRatingRule, relationship)
		}
		if err := weight.Validate(); err != nil {
			return fmt.Errorf("%w: weight %s: %v", ErrInvalidRatingRule, relationship, err)
		}
		if weight.Sign() <= 0 {
			return fmt.Errorf("%w: weight %s must be positive", ErrInvalidRatingRule, relationship)
		}
		if r.RatingScale+weight.Scale() > values.MaxScale {
			return fmt.Errorf("%w: rating and weight scales exceed decimal limit", ErrInvalidRatingRule)
		}
	}
	if r.Digest != "" && r.Digest != r.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidRatingRule)
	}
	return nil
}

func (r ProposedRatingRule) body() []byte {
	if r.ValidateWithoutDigest() != nil {
		return nil
	}
	relationships := make([]ReviewerRelationshipKind, 0, len(r.Weights))
	for relationship := range r.Weights {
		relationships = append(relationships, relationship)
	}
	sort.Slice(relationships, func(i, j int) bool { return relationships[i] < relationships[j] })
	w := canonicalbytes.New("hcmnext.domains.performance.ProposedRatingRule", 1).
		String("id", r.ID).String("version", r.Version).
		Int("rating_scale", int64(r.RatingScale)).Int("result_scale", int64(r.ResultScale)).
		String("rounding", r.Rounding.String()).Int("minimum_reviews", int64(r.MinimumReviews)).
		String("outlier_handling", string(r.OutlierHandling)).Count("weights", len(relationships))
	for _, relationship := range relationships {
		w.String("weight.relationship", string(relationship)).Value("weight.value", r.Weights[relationship])
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r ProposedRatingRule) ValidateWithoutDigest() error {
	copyOf := r
	copyOf.Digest = ""
	return copyOf.Validate()
}

func (r ProposedRatingRule) computedDigest() string {
	body := r.body()
	if body == nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

func (r ProposedRatingRule) withDigest() (ProposedRatingRule, error) {
	providedDigest := r.Digest
	r.Digest = ""
	if err := r.ValidateWithoutDigest(); err != nil {
		return ProposedRatingRule{}, err
	}
	computed := r.computedDigest()
	if providedDigest != "" && providedDigest != computed {
		return ProposedRatingRule{}, fmt.Errorf("%w: digest mismatch", ErrInvalidRatingRule)
	}
	r.Digest = computed
	return r, nil
}

// Canonical returns the rule bytes without duplicating the derived digest in
// the value that the digest itself is calculated over.
func (r ProposedRatingRule) Canonical() []byte { return r.body() }

func (r ProposedRatingRule) RuleDigest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// InsufficientReviewsError names the shortfall instead of returning an opaque
// calculation failure.
type InsufficientReviewsError struct {
	ParticipantID string
	Required      int
	Available     int
	Shortfall     int
}

func (e *InsufficientReviewsError) Error() string {
	return fmt.Sprintf("%s: participant=%s required=%d available=%d shortfall=%d", ErrInsufficientReviews, e.ParticipantID, e.Required, e.Available, e.Shortfall)
}

func (e *InsufficientReviewsError) Is(target error) bool { return target == ErrInsufficientReviews }

// RatingContribution is the non-narrative evidence retained by a proposed
// rating. ReviewID and ReviewRevision identify the exact append-only review.
type RatingContribution struct {
	ReviewID       string
	ReviewRevision uint64
	Relationship   ReviewerRelationshipKind
	Rating         values.Decimal
	Weight         values.Decimal
}

type ProposedRatingContribution = RatingContribution

// ProposedRating is an immutable-by-value calculation result for one
// participant. It retains references and weights, never review narrative.
type ProposedRating struct {
	ParticipantID     string
	CycleID           string
	CycleRevision     uint64
	GraphRevision     uint64
	GraphDigest       string
	CollectionDigest  string
	Rule              ProposedRatingRule
	Rating            values.Decimal
	Contributions     []RatingContribution
	ExcludedReviewIDs []string
	CanonicalDigest   string
}

func (p ProposedRating) Validate() error {
	if strings.TrimSpace(p.ParticipantID) == "" || strings.TrimSpace(p.CycleID) == "" || p.CycleRevision == 0 || p.GraphRevision == 0 || strings.TrimSpace(p.GraphDigest) == "" || strings.TrimSpace(p.CollectionDigest) == "" {
		return fmt.Errorf("%w: cycle, graph, collection and participant references are required", ErrInvalidProposedRating)
	}
	if err := p.Rule.Validate(); err != nil {
		return fmt.Errorf("%w: rule: %v", ErrInvalidProposedRating, err)
	}
	if err := p.Rating.Validate(); err != nil {
		return fmt.Errorf("%w: rating: %v", ErrInvalidProposedRating, err)
	}
	if p.Rating.Scale() != p.Rule.ResultScale {
		return fmt.Errorf("%w: result scale does not match rule", ErrInvalidProposedRating)
	}
	if len(p.Contributions) < p.Rule.MinimumReviews {
		return fmt.Errorf("%w: %w", ErrInvalidProposedRating, ErrInsufficientReviews)
	}
	seen := make(map[string]struct{}, len(p.Contributions))
	for _, contribution := range p.Contributions {
		if strings.TrimSpace(contribution.ReviewID) == "" || contribution.ReviewRevision == 0 || !contribution.Relationship.Valid() {
			return fmt.Errorf("%w: contribution reference is incomplete", ErrInvalidProposedRating)
		}
		if err := contribution.Rating.Validate(); err != nil {
			return fmt.Errorf("%w: contribution rating: %v", ErrInvalidProposedRating, err)
		}
		if contribution.Rating.Scale() != p.Rule.RatingScale {
			return fmt.Errorf("%w: contribution rating scale does not match rule", ErrInvalidProposedRating)
		}
		if err := contribution.Weight.Validate(); err != nil || contribution.Weight.Sign() <= 0 {
			return fmt.Errorf("%w: contribution weight is invalid", ErrInvalidProposedRating)
		}
		ruleWeight, ok := p.Rule.Weights[contribution.Relationship]
		if !ok || !contribution.Weight.Equal(ruleWeight) {
			return fmt.Errorf("%w: contribution weight does not match rule", ErrInvalidProposedRating)
		}
		key := fmt.Sprintf("%s\x00%d", contribution.ReviewID, contribution.ReviewRevision)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate contribution %s", ErrInvalidProposedRating, contribution.ReviewID)
		}
		seen[key] = struct{}{}
	}
	if p.CanonicalDigest == "" || p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidProposedRating)
	}
	return nil
}

func (p ProposedRating) computedDigest() string {
	contributions := append([]RatingContribution(nil), p.Contributions...)
	sort.Slice(contributions, func(i, j int) bool {
		if contributions[i].ReviewID != contributions[j].ReviewID {
			return contributions[i].ReviewID < contributions[j].ReviewID
		}
		return contributions[i].ReviewRevision < contributions[j].ReviewRevision
	})
	excluded := append([]string(nil), p.ExcludedReviewIDs...)
	sort.Strings(excluded)
	w := canonicalbytes.New("hcmnext.domains.performance.ProposedRating", 1).
		String("participant_id", p.ParticipantID).String("cycle_id", p.CycleID).
		Int("cycle_revision", int64(p.CycleRevision)).Int("graph_revision", int64(p.GraphRevision)).
		String("graph_digest", p.GraphDigest).String("collection_digest", p.CollectionDigest).
		Value("rule", p.Rule).Value("rating", p.Rating).Count("contributions", len(contributions))
	for _, contribution := range contributions {
		w.String("review.id", contribution.ReviewID).Int("review.revision", int64(contribution.ReviewRevision)).
			String("review.relationship", string(contribution.Relationship)).
			Value("review.rating", contribution.Rating).Value("review.weight", contribution.Weight)
	}
	w.Count("excluded_reviews", len(excluded))
	for _, reviewID := range excluded {
		w.String("excluded_review", reviewID)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func (p ProposedRating) ContributionRefs() []RatingContribution {
	return append([]RatingContribution(nil), p.Contributions...)
}

// ProposedRatingExplanation exposes every contributing review reference and
// weight while intentionally providing no narrative field.
type ProposedRatingExplanation struct {
	ParticipantID       string
	CycleID             string
	CycleRevision       uint64
	RuleID              string
	RuleVersion         string
	RuleDigest          string
	Rating              values.Decimal
	ProposedRating      values.Decimal
	MinimumReviewCount  int
	EligibleReviewCount int
	ContributingCount   int
	OutlierHandling     OutlierHandling
	Contributions       []RatingContribution
	ExcludedReviewIDs   []string
	Digest              string
}

func (p ProposedRating) Explain() (ProposedRatingExplanation, error) {
	if err := p.Validate(); err != nil {
		return ProposedRatingExplanation{}, err
	}
	contributions := p.ContributionRefs()
	excluded := append([]string(nil), p.ExcludedReviewIDs...)
	return ProposedRatingExplanation{
		ParticipantID: p.ParticipantID, CycleID: p.CycleID, CycleRevision: p.CycleRevision,
		RuleID: p.Rule.ID, RuleVersion: p.Rule.Version, RuleDigest: p.Rule.Digest,
		Rating: p.Rating, ProposedRating: p.Rating, MinimumReviewCount: p.Rule.MinimumReviews,
		EligibleReviewCount: len(contributions) + len(excluded), ContributingCount: len(contributions),
		OutlierHandling: p.Rule.OutlierHandling, Contributions: contributions, ExcludedReviewIDs: excluded,
		Digest: p.CanonicalDigest,
	}, nil
}

func ExplainProposedRating(p ProposedRating) (ProposedRatingExplanation, error) { return p.Explain() }

type ratingCandidate struct {
	review       Review
	relationship ReviewerRelationshipKind
	rating       values.Decimal
	weight       values.Decimal
}

func latestReviews(collection ReviewCollection, participantID string) ([]Review, error) {
	latest := make(map[string]Review)
	for _, review := range collection.Reviews {
		if review.ParticipantID != participantID {
			continue
		}
		prior, exists := latest[review.ReviewerID]
		if !exists || review.ReviewRevision > prior.ReviewRevision || (review.ReviewRevision == prior.ReviewRevision && review.ID > prior.ID) {
			latest[review.ReviewerID] = review
		}
	}
	reviews := make([]Review, 0, len(latest))
	for _, review := range latest {
		reviews = append(reviews, review)
	}
	sort.Slice(reviews, func(i, j int) bool {
		if reviews[i].ID != reviews[j].ID {
			return reviews[i].ID < reviews[j].ID
		}
		return reviews[i].ReviewRevision < reviews[j].ReviewRevision
	})
	return reviews, nil
}

func applyOutlierHandling(candidates []ratingCandidate, handling OutlierHandling) (selected []ratingCandidate, excluded []string, err error) {
	selected = append([]ratingCandidate(nil), candidates...)
	if handling == OutlierHandlingNone {
		return selected, nil, nil
	}
	if handling != OutlierHandlingTrimExtremes {
		return nil, nil, fmt.Errorf("%w: %q", ErrInvalidOutlierHandling, handling)
	}
	if len(selected) < 3 {
		return selected, nil, nil
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].rating.Cmp(selected[j].rating) != 0 {
			return selected[i].rating.Cmp(selected[j].rating) < 0
		}
		if selected[i].review.ID != selected[j].review.ID {
			return selected[i].review.ID < selected[j].review.ID
		}
		return selected[i].review.ReviewRevision < selected[j].review.ReviewRevision
	})
	if len(selected) >= 1 {
		excluded = append(excluded, selected[0].review.ID)
	}
	if len(selected) >= 2 {
		excluded = append(excluded, selected[len(selected)-1].review.ID)
	}
	return selected[1 : len(selected)-1], excluded, nil
}

// CalculateProposedRating calculates one participant's proposed rating from
// the latest collected review revision for each reviewer.
func CalculateProposedRating(collection ReviewCollection, participantID string, rule ProposedRatingRule) (ProposedRating, error) {
	if _, err := collection.Explain(); err != nil {
		return ProposedRating{}, err
	}
	if strings.TrimSpace(participantID) == "" {
		return ProposedRating{}, fmt.Errorf("%w: participant id is required", ErrInvalidRatingInput)
	}
	normalized, err := rule.withDigest()
	if err != nil {
		return ProposedRating{}, err
	}
	reviews, err := latestReviews(collection, participantID)
	if err != nil {
		return ProposedRating{}, err
	}
	candidates := make([]ratingCandidate, 0, len(reviews))
	for _, review := range reviews {
		relationship, ok := relationshipFor(collection.Graph, review)
		if !ok {
			return ProposedRating{}, ErrReviewerNotFrozen
		}
		weight, ok := normalized.Weights[relationship]
		if !ok {
			return ProposedRating{}, fmt.Errorf("%w: %s", ErrRatingRuleWeight, relationship)
		}
		rating, parseErr := values.NewDecimal(review.Rating, normalized.RatingScale, normalized.Rounding)
		if parseErr != nil {
			return ProposedRating{}, fmt.Errorf("%w: review=%s: %v", ErrInvalidRatingInput, review.ID, parseErr)
		}
		candidates = append(candidates, ratingCandidate{review: review, relationship: relationship, rating: rating, weight: weight})
	}
	selected, excluded, err := applyOutlierHandling(candidates, normalized.OutlierHandling)
	if err != nil {
		return ProposedRating{}, err
	}
	if len(selected) < normalized.MinimumReviews {
		return ProposedRating{}, &InsufficientReviewsError{ParticipantID: participantID, Required: normalized.MinimumReviews, Available: len(selected), Shortfall: normalized.MinimumReviews - len(selected)}
	}
	weightScale := selected[0].weight.Scale()
	for _, candidate := range selected[1:] {
		if candidate.weight.Scale() > weightScale {
			weightScale = candidate.weight.Scale()
		}
	}
	for i := range selected {
		selected[i].weight, err = selected[i].weight.Quantize(weightScale, values.RoundingExactRequired)
		if err != nil {
			return ProposedRating{}, fmt.Errorf("%w: weight scale: %v", ErrInvalidRatingRule, err)
		}
	}
	productScale := normalized.RatingScale + weightScale
	var totalProduct values.Decimal
	var totalWeight values.Decimal
	for i, candidate := range selected {
		product, mulErr := candidate.rating.Mul(candidate.weight, productScale, normalized.Rounding)
		if mulErr != nil {
			return ProposedRating{}, fmt.Errorf("%w: review=%s: %v", ErrInvalidRatingInput, candidate.review.ID, mulErr)
		}
		if i == 0 {
			totalProduct, totalWeight = product, candidate.weight
			continue
		}
		totalProduct, err = totalProduct.Add(product)
		if err != nil {
			return ProposedRating{}, fmt.Errorf("%w: weighted total: %v", ErrInvalidRatingInput, err)
		}
		totalWeight, err = totalWeight.Add(candidate.weight)
		if err != nil {
			return ProposedRating{}, fmt.Errorf("%w: weight total: %v", ErrInvalidRatingInput, err)
		}
	}
	average, err := totalProduct.Div(totalWeight, normalized.ResultScale, normalized.Rounding)
	if err != nil {
		return ProposedRating{}, fmt.Errorf("%w: average: %v", ErrInvalidRatingInput, err)
	}
	contributions := make([]RatingContribution, 0, len(selected))
	for _, candidate := range selected {
		contributions = append(contributions, RatingContribution{ReviewID: candidate.review.ID, ReviewRevision: candidate.review.ReviewRevision, Relationship: candidate.relationship, Rating: candidate.rating, Weight: candidate.weight})
	}
	result := ProposedRating{ParticipantID: participantID, CycleID: collection.Graph.CycleID, CycleRevision: collection.Graph.CycleRevision, GraphRevision: collection.Graph.GraphRevision, GraphDigest: collection.Graph.Digest, CollectionDigest: collection.CanonicalDigest, Rule: normalized, Rating: average, Contributions: contributions, ExcludedReviewIDs: append([]string(nil), excluded...)}
	result.CanonicalDigest = result.computedDigest()
	if err := result.Validate(); err != nil {
		return ProposedRating{}, err
	}
	return result, nil
}

func CalculateRating(collection ReviewCollection, participantID string, rule ProposedRatingRule) (ProposedRating, error) {
	return CalculateProposedRating(collection, participantID, rule)
}

func (c ReviewCollection) CalculateProposedRating(participantID string, rule ProposedRatingRule) (ProposedRating, error) {
	return CalculateProposedRating(c, participantID, rule)
}
