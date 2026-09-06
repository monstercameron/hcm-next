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
	ErrInvalidReview       = errors.New("performance: invalid review")
	ErrReviewerNotFrozen   = errors.New("performance: reviewer is not in the frozen graph for participant")
	ErrReviewCutoffClosed  = errors.New("performance: review collection is closed at the phase cutoff")
	ErrReviewDuplicate     = errors.New("performance: review resubmission must be a new revision")
	ErrReviewRevision      = errors.New("performance: review revision is not the next append-only revision")
	ErrReviewCycleMismatch = errors.New("performance: review is not bound to the frozen cycle revision")
	ErrReviewCollection    = errors.New("performance: invalid review collection")
)

// ReviewKind distinguishes ordinary cycle reviews from 360 feedback while
// sharing the same frozen reviewer graph and append-only submission rules.
type ReviewKind string

const (
	ReviewKindReview      ReviewKind = "REVIEW"
	ReviewKind360Feedback ReviewKind = "360_FEEDBACK"
)

// ReviewState keeps draft, submitted, and correction records distinct. A
// participant-facing view never exposes draft payloads because drafts are not
// accepted into ReviewCollection.
type ReviewState string

const (
	ReviewStateDraft      ReviewState = "DRAFT"
	ReviewStateSubmitted  ReviewState = "SUBMITTED"
	ReviewStateCorrection ReviewState = "CORRECTION"
)

// Review is a submitted opinion bound to one frozen graph and rating scale.
// NarrativeDigest is a digest of narrative text; raw narrative is not a field
// in this domain value.
type Review struct {
	ID                       string
	Kind                     ReviewKind
	State                    ReviewState
	ReviewerID               string
	ParticipantID            string
	CycleID                  string
	CycleRevision            uint64
	GraphRevision            uint64
	GraphDigest              string
	RatingScale              RatingScaleVersionRef
	Rating                   string
	NarrativeDigest          string
	SubmittedAt              values.Instant
	ReviewRevision           uint64
	SupersedesReviewRevision uint64
}

func (r Review) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.ReviewerID) == "" || strings.TrimSpace(r.ParticipantID) == "" || strings.TrimSpace(r.CycleID) == "" {
		return fmt.Errorf("%w: ids are required", ErrInvalidReview)
	}
	if r.Kind != ReviewKindReview && r.Kind != ReviewKind360Feedback {
		return fmt.Errorf("%w: unknown review kind %q", ErrInvalidReview, r.Kind)
	}
	if r.State != ReviewStateSubmitted && r.State != ReviewStateCorrection {
		return fmt.Errorf("%w: only submitted and correction records can be collected", ErrInvalidReview)
	}
	if r.CycleRevision == 0 || r.GraphRevision == 0 || strings.TrimSpace(r.GraphDigest) == "" {
		return fmt.Errorf("%w: cycle and frozen graph binding are required", ErrInvalidReview)
	}
	if err := r.RatingScale.Validate(); err != nil {
		return fmt.Errorf("%w: rating scale: %v", ErrInvalidReview, err)
	}
	if strings.TrimSpace(r.Rating) == "" {
		return fmt.Errorf("%w: rating is required", ErrInvalidReview)
	}
	if !strings.HasPrefix(r.NarrativeDigest, canonicalbytes.DigestAlgorithm+":") || len(r.NarrativeDigest) <= len(canonicalbytes.DigestAlgorithm)+1 {
		return fmt.Errorf("%w: narrative must be a digest, not raw text", ErrInvalidReview)
	}
	if err := r.SubmittedAt.Validate(); err != nil {
		return fmt.Errorf("%w: submitted_at: %v", ErrInvalidReview, err)
	}
	if r.ReviewRevision == 0 {
		return fmt.Errorf("%w: review revision is required", ErrInvalidReview)
	}
	if r.State == ReviewStateSubmitted && r.SupersedesReviewRevision >= r.ReviewRevision {
		return fmt.Errorf("%w: superseded revision must precede current revision", ErrInvalidReview)
	}
	return nil
}

func (r Review) canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.performance.Review", 1).
		String("id", r.ID).
		String("kind", string(r.Kind)).
		String("state", string(r.State)).
		String("reviewer_id", r.ReviewerID).
		String("participant_id", r.ParticipantID).
		String("cycle_id", r.CycleID).
		Int("cycle_revision", int64(r.CycleRevision)).
		Int("graph_revision", int64(r.GraphRevision)).
		String("graph_digest", r.GraphDigest).
		Value("rating_scale", r.RatingScale).
		String("rating", r.Rating).
		String("narrative_digest", r.NarrativeDigest).
		Value("submitted_at", r.SubmittedAt).
		Int("review_revision", int64(r.ReviewRevision)).
		Int("supersedes_review_revision", int64(r.SupersedesReviewRevision))
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ReviewCollectionPolicy identifies which reviewer relationships are
// anonymous to the participant. The policy is pinned into the collection
// digest alongside the frozen graph digest.
type ReviewCollectionPolicy struct {
	AnonymousRelationships []ReviewerRelationshipKind
	RatingScale            RatingScaleVersionRef
}

func (p ReviewCollectionPolicy) validate() error {
	if err := p.RatingScale.Validate(); err != nil {
		return fmt.Errorf("%w: rating scale: %v", ErrReviewCollection, err)
	}
	seen := make(map[ReviewerRelationshipKind]struct{}, len(p.AnonymousRelationships))
	for _, relationship := range p.AnonymousRelationships {
		if !relationship.Valid() {
			return fmt.Errorf("%w: anonymous relationship %q is invalid", ErrReviewCollection, relationship)
		}
		if _, exists := seen[relationship]; exists {
			return fmt.Errorf("%w: duplicate anonymous relationship %q", ErrReviewCollection, relationship)
		}
		seen[relationship] = struct{}{}
	}
	return nil
}

func (p ReviewCollectionPolicy) anonymous(relationship ReviewerRelationshipKind) bool {
	for _, candidate := range p.AnonymousRelationships {
		if candidate == relationship {
			return true
		}
	}
	return false
}

// ReviewCollection is an immutable-by-value append-only set for one frozen
// cycle revision. Submit returns a detached successor value.
type ReviewCollection struct {
	Graph           FrozenParticipantReviewerGraph
	PhaseCutoff     values.Instant
	Policy          ReviewCollectionPolicy
	Reviews         []Review
	CanonicalDigest string
}

func NewReviewCollection(graph FrozenParticipantReviewerGraph, phaseCutoff values.Instant, policy ReviewCollectionPolicy) (ReviewCollection, error) {
	if err := graph.Validate(); err != nil {
		return ReviewCollection{}, err
	}
	if err := phaseCutoff.Validate(); err != nil {
		return ReviewCollection{}, fmt.Errorf("%w: phase cutoff: %v", ErrReviewCollection, err)
	}
	if err := policy.validate(); err != nil {
		return ReviewCollection{}, err
	}
	c := ReviewCollection{Graph: graph, PhaseCutoff: phaseCutoff, Policy: policy, Reviews: []Review{}}
	return c.withDigest()
}

func (c ReviewCollection) validate() error {
	if err := c.Graph.Validate(); err != nil {
		return err
	}
	if err := c.PhaseCutoff.Validate(); err != nil {
		return fmt.Errorf("%w: phase cutoff: %v", ErrReviewCollection, err)
	}
	if err := c.Policy.validate(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(c.Reviews))
	for _, review := range c.Reviews {
		if err := review.Validate(); err != nil {
			return err
		}
		if err := c.validateReviewBinding(review); err != nil {
			return err
		}
		key := reviewKey(review)
		if _, exists := seen[key+fmt.Sprintf("\x00%d", review.ReviewRevision)]; exists {
			return ErrReviewDuplicate
		}
		seen[key+fmt.Sprintf("\x00%d", review.ReviewRevision)] = struct{}{}
	}
	if c.CanonicalDigest == "" || c.CanonicalDigest != c.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrReviewCollection)
	}
	return nil
}

func reviewKey(review Review) string {
	return review.ReviewerID + "\x00" + review.ParticipantID + "\x00" + review.CycleID + fmt.Sprintf("\x00%d", review.CycleRevision)
}

func (c ReviewCollection) validateReviewBinding(review Review) error {
	if review.CycleID != c.Graph.CycleID || review.CycleRevision != c.Graph.CycleRevision || review.GraphRevision != c.Graph.GraphRevision || review.GraphDigest != c.Graph.Digest || review.RatingScale != c.cycleRatingScale() {
		return ErrReviewCycleMismatch
	}
	if !review.SubmittedAt.Before(c.PhaseCutoff) {
		return ErrReviewCutoffClosed
	}
	for _, assignment := range c.Graph.Reviewers {
		if assignment.ParticipantID == review.ParticipantID && assignment.ReviewerID == review.ReviewerID {
			return nil
		}
	}
	return ErrReviewerNotFrozen
}

func (c ReviewCollection) cycleRatingScale() RatingScaleVersionRef {
	return c.Policy.RatingScale
}

func (c ReviewCollection) computedDigest() string {
	// The frozen graph carries the cycle binding but not the original cycle
	// value. Review bindings therefore compare their scale using the graph's
	// cycle revision contract through this collection's pinned expectations.
	// The scale is recorded in each review and in the collection digest.
	reviews := append([]Review(nil), c.Reviews...)
	sort.Slice(reviews, func(i, j int) bool {
		if reviewKey(reviews[i]) != reviewKey(reviews[j]) {
			return reviewKey(reviews[i]) < reviewKey(reviews[j])
		}
		return reviews[i].ReviewRevision < reviews[j].ReviewRevision
	})
	anonymous := append([]ReviewerRelationshipKind(nil), c.Policy.AnonymousRelationships...)
	sort.Slice(anonymous, func(i, j int) bool { return anonymous[i] < anonymous[j] })
	w := canonicalbytes.New("hcmnext.domains.performance.ReviewCollection", 1).
		String("cycle_id", c.Graph.CycleID).
		Int("cycle_revision", int64(c.Graph.CycleRevision)).
		Int("graph_revision", int64(c.Graph.GraphRevision)).
		String("graph_digest", c.Graph.Digest).
		Value("rating_scale", c.Policy.RatingScale).
		Value("phase_cutoff", c.PhaseCutoff).
		Count("anonymous_relationships", len(anonymous))
	for _, relationship := range anonymous {
		w.String("anonymous_relationship", string(relationship))
	}
	w.Count("reviews", len(reviews))
	for _, review := range reviews {
		w.Field("review", review.canonical())
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func (c ReviewCollection) withDigest() (ReviewCollection, error) {
	c.CanonicalDigest = c.computedDigest()
	if err := c.validateWithoutDigestCheck(); err != nil {
		return ReviewCollection{}, err
	}
	return c, nil
}

func (c ReviewCollection) validateWithoutDigestCheck() error {
	if err := c.Graph.Validate(); err != nil {
		return err
	}
	if err := c.PhaseCutoff.Validate(); err != nil {
		return fmt.Errorf("%w: phase cutoff: %v", ErrReviewCollection, err)
	}
	return c.Policy.validate()
}

// Submit appends a review as a new immutable revision. The receiver and all
// prior review records remain unchanged.
func (c ReviewCollection) Submit(review Review) (ReviewCollection, error) {
	if err := c.validate(); err != nil {
		return ReviewCollection{}, err
	}
	if err := review.Validate(); err != nil {
		return ReviewCollection{}, err
	}
	if err := c.validateReviewBinding(review); err != nil {
		return ReviewCollection{}, err
	}
	key := reviewKey(review)
	maxRevision := uint64(0)
	for _, prior := range c.Reviews {
		if reviewKey(prior) == key && prior.ReviewRevision > maxRevision {
			maxRevision = prior.ReviewRevision
		}
	}
	if review.ReviewRevision != maxRevision+1 {
		if maxRevision == 0 {
			return ReviewCollection{}, ErrReviewRevision
		}
		return ReviewCollection{}, ErrReviewDuplicate
	}
	if maxRevision == 0 && review.SupersedesReviewRevision != 0 {
		return ReviewCollection{}, ErrReviewRevision
	}
	if maxRevision != 0 && review.SupersedesReviewRevision != maxRevision {
		return ReviewCollection{}, ErrReviewRevision
	}
	next := c
	next.Reviews = append([]Review(nil), c.Reviews...)
	next.Reviews = append(next.Reviews, review)
	sort.Slice(next.Reviews, func(i, j int) bool {
		if reviewKey(next.Reviews[i]) != reviewKey(next.Reviews[j]) {
			return reviewKey(next.Reviews[i]) < reviewKey(next.Reviews[j])
		}
		return next.Reviews[i].ReviewRevision < next.Reviews[j].ReviewRevision
	})
	return next.withDigest()
}

func CollectReview(collection ReviewCollection, review Review) (ReviewCollection, error) {
	return collection.Submit(review)
}

func SubmitReview(collection ReviewCollection, review Review) (ReviewCollection, error) {
	return collection.Submit(review)
}

// ReviewView is the participant-compartment projection. Anonymous reviewer
// identity is represented by an empty ReviewerID, while the review's rating,
// digest, relationship, and trusted submission instant remain visible.
type ReviewView struct {
	ID                      string
	Kind                    ReviewKind
	ParticipantID           string
	ReviewerID              string
	Relationship            ReviewerRelationshipKind
	ReviewerIdentityVisible bool
	Rating                  string
	NarrativeDigest         string
	SubmittedAt             values.Instant
	ReviewRevision          uint64
}

// ParticipantReviews returns only reviews for participantID and redacts
// reviewer identity for relationships configured anonymous.
func (c ReviewCollection) ParticipantReviews(participantID string) ([]ReviewView, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(participantID) == "" {
		return nil, fmt.Errorf("%w: participant id is required", ErrReviewCollection)
	}
	views := make([]ReviewView, 0)
	for _, review := range c.Reviews {
		if review.ParticipantID != participantID {
			continue
		}
		relationship, ok := relationshipFor(c.Graph, review)
		if !ok {
			return nil, ErrReviewerNotFrozen
		}
		anonymous := c.Policy.anonymous(relationship)
		view := ReviewView{
			ID: review.ID, Kind: review.Kind, ParticipantID: review.ParticipantID,
			Relationship: relationship, ReviewerIdentityVisible: !anonymous,
			Rating: review.Rating, NarrativeDigest: review.NarrativeDigest,
			SubmittedAt: review.SubmittedAt, ReviewRevision: review.ReviewRevision,
		}
		if !anonymous {
			view.ReviewerID = review.ReviewerID
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].ID != views[j].ID {
			return views[i].ID < views[j].ID
		}
		return views[i].ReviewRevision < views[j].ReviewRevision
	})
	return views, nil
}

func relationshipFor(graph FrozenParticipantReviewerGraph, review Review) (ReviewerRelationshipKind, bool) {
	for _, assignment := range graph.Reviewers {
		if assignment.ParticipantID == review.ParticipantID && assignment.ReviewerID == review.ReviewerID {
			return assignment.Relationship, true
		}
	}
	return "", false
}

type ReviewCollectionExplanation struct {
	CycleID                string
	CycleRevision          uint64
	GraphRevision          uint64
	GraphDigest            string
	CollectionDigest       string
	ReviewCount            int
	AnonymousRelationships []ReviewerRelationshipKind
}

func (c ReviewCollection) Explain() (ReviewCollectionExplanation, error) {
	if err := c.validate(); err != nil {
		return ReviewCollectionExplanation{}, err
	}
	return ReviewCollectionExplanation{
		CycleID: c.Graph.CycleID, CycleRevision: c.Graph.CycleRevision,
		GraphRevision: c.Graph.GraphRevision, GraphDigest: c.Graph.Digest,
		CollectionDigest: c.CanonicalDigest, ReviewCount: len(c.Reviews),
		AnonymousRelationships: append([]ReviewerRelationshipKind(nil), c.Policy.AnonymousRelationships...),
	}, nil
}

func ExplainReviewCollection(c ReviewCollection) (ReviewCollectionExplanation, error) {
	return c.Explain()
}
