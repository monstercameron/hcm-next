package performance_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/performance"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func proposedDecimal(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("decimal %q: %v", text, err)
	}
	return d
}

func proposedCollection(t *testing.T) performance.ReviewCollection {
	t.Helper()
	cycle, graph := reviewGraph(t)
	collection, err := performance.NewReviewCollection(graph, reviewInstant(t, "2026-10-01T00:00:00Z"), performance.ReviewCollectionPolicy{RatingScale: cycle.RatingScale})
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	for _, item := range []struct{ reviewer, rating string }{{"manager-1", "4"}, {"peer-1", "2"}} {
		review := reviewFor(t, collection, item.reviewer, "participant-1", performance.ReviewKindReview, 1, 0, "2026-09-05T12:00:00Z")
		review.Rating = item.rating
		review.ID = item.reviewer + "-rating"
		var collectErr error
		collection, collectErr = collection.Submit(review)
		if collectErr != nil {
			t.Fatalf("submit %s: %v", item.reviewer, collectErr)
		}
	}
	return collection
}

func proposedRule(t *testing.T, minimum int, outliers performance.OutlierHandling) performance.ProposedRatingRule {
	t.Helper()
	rule, err := performance.NewProposedRatingRule("performance.rating", "v1", 2, 2, values.RoundingHalfEven, minimum, map[performance.ReviewerRelationshipKind]values.Decimal{
		performance.ReviewerRelationshipManager: proposedDecimal(t, "2.00", 2),
		performance.ReviewerRelationshipPeer:    proposedDecimal(t, "1.00", 2),
	}, outliers)
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	return rule
}

func TestTodo_PERFORMANCE_004(t *testing.T) {
	collection := proposedCollection(t)
	rule := proposedRule(t, 2, performance.OutlierHandlingNone)
	proposed, err := performance.CalculateProposedRating(collection, "participant-1", rule)
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	if proposed.Rating.String() != "3.33" || proposed.CanonicalDigest == "" || len(proposed.Contributions) != 2 {
		t.Fatalf("proposed = %+v", proposed)
	}
	explanation, err := proposed.Explain()
	if err != nil || len(explanation.Contributions) != 2 || explanation.Contributions[0].Weight.String() == "" {
		t.Fatalf("explanation = %+v, err=%v", explanation, err)
	}
}

func TestTodo_PERFORMANCE_004_Property(t *testing.T) {
	collection := proposedCollection(t)
	rule := proposedRule(t, 2, performance.OutlierHandlingNone)
	a, err := collection.CalculateProposedRating("participant-1", rule)
	if err != nil {
		t.Fatal(err)
	}
	b, err := collection.CalculateProposedRating("participant-1", rule)
	if err != nil {
		t.Fatal(err)
	}
	if a.CanonicalDigest != b.CanonicalDigest || !a.Rating.Equal(b.Rating) {
		t.Fatalf("identical inputs differ: a=%+v b=%+v", a, b)
	}
	trimmed, err := performance.CalculateProposedRating(collection, "participant-1", proposedRule(t, 2, performance.OutlierHandlingTrimExtremes))
	if err != nil {
		t.Fatal(err)
	}
	if len(trimmed.ExcludedReviewIDs) != 0 || len(trimmed.Contributions) != 2 {
		t.Fatalf("trimmed = %+v", trimmed)
	}
	if _, err := performance.CalculateProposedRating(collection, "participant-1", proposedRule(t, 3, performance.OutlierHandlingTrimExtremes)); !errors.Is(err, performance.ErrInsufficientReviews) {
		t.Fatalf("shortfall error = %v", err)
	}
}
