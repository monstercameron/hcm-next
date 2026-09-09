package performance_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func reviewInstant(t testing.TB, text string) values.Instant {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(at)
}

func reviewGraph(t testing.TB) (performance.PerformanceCycle, performance.FrozenParticipantReviewerGraph) {
	t.Helper()
	cycle, err := performance.NewPerformanceCycle(
		"cycle-2026", performance.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "7", Digest: "sha256:population"},
		performance.CalendarBindingRef{Ref: "calendar", Version: "3", Digest: "sha256:calendar"},
		performance.RatingScaleVersionRef{ID: "scale", Version: "2", Digest: "sha256:scale"},
	)
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	graph, err := performance.FreezeParticipantReviewerGraph(cycle,
		[]performance.ParticipantRef{{ID: "participant-1"}, {ID: "participant-2"}},
		[]performance.ReviewerAssignment{
			{ParticipantID: "participant-1", ReviewerID: "manager-1", Relationship: performance.ReviewerRelationshipManager},
			{ParticipantID: "participant-1", ReviewerID: "peer-1", Relationship: performance.ReviewerRelationshipPeer},
			{ParticipantID: "participant-2", ReviewerID: "manager-2", Relationship: performance.ReviewerRelationshipManager},
		}, performance.DefaultReviewerGraphRules(), reviewInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("reviewer graph: %v", err)
	}
	return cycle, graph
}

func reviewCollection(t testing.TB) performance.ReviewCollection {
	t.Helper()
	cycle, graph := reviewGraph(t)
	collection, err := performance.NewReviewCollection(graph, reviewInstant(t, "2026-10-01T00:00:00Z"), performance.ReviewCollectionPolicy{
		AnonymousRelationships: []performance.ReviewerRelationshipKind{performance.ReviewerRelationshipManager},
		RatingScale:            cycle.RatingScale,
	})
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	return collection
}

func reviewFor(t testing.TB, collection performance.ReviewCollection, reviewer, participant string, kind performance.ReviewKind, revision, supersedes uint64, submitted string) performance.Review {
	t.Helper()
	return performance.Review{
		ID: reviewer + "-" + participant + "-" + string(kind), Kind: kind, State: performance.ReviewStateSubmitted,
		ReviewerID: reviewer, ParticipantID: participant, CycleID: collection.Graph.CycleID,
		CycleRevision: collection.Graph.CycleRevision, GraphRevision: collection.Graph.GraphRevision,
		GraphDigest: collection.Graph.Digest, RatingScale: collection.Policy.RatingScale,
		Rating: "4", NarrativeDigest: canonicalbytes.Digest([]byte("private narrative")),
		SubmittedAt: reviewInstant(t, submitted), ReviewRevision: revision, SupersedesReviewRevision: supersedes,
	}
}

func TestTodo_PERFORMANCE_003(t *testing.T) {
	collection := reviewCollection(t)
	first := reviewFor(t, collection, "manager-1", "participant-1", performance.ReviewKindReview, 1, 0, "2026-09-05T12:00:00Z")
	updated, err := performance.CollectReview(collection, first)
	if err != nil {
		t.Fatalf("collect review: %v", err)
	}
	if len(updated.Reviews) != 1 || updated.CanonicalDigest == collection.CanonicalDigest {
		t.Fatalf("collection did not append review: %+v", updated)
	}
	views, err := updated.ParticipantReviews("participant-1")
	if err != nil {
		t.Fatalf("participant view: %v", err)
	}
	if len(views) != 1 || views[0].ReviewerIdentityVisible || views[0].ReviewerID != "" || views[0].NarrativeDigest == "" {
		t.Fatalf("anonymous participant view leaked identity or omitted digest: %+v", views)
	}
	explanation, err := updated.Explain()
	if err != nil || explanation.ReviewCount != 1 || explanation.CollectionDigest != updated.CanonicalDigest {
		t.Fatalf("explanation = %+v, err=%v", explanation, err)
	}

	resubmission := reviewFor(t, updated, "manager-1", "participant-1", performance.ReviewKindReview, 2, 1, "2026-09-06T12:00:00Z")
	resubmitted, err := updated.Submit(resubmission)
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if len(resubmitted.Reviews) != 2 || resubmitted.Reviews[1].ReviewRevision != 2 {
		t.Fatalf("resubmission history = %+v", resubmitted.Reviews)
	}
	if _, err := updated.Submit(first); !errors.Is(err, performance.ErrReviewDuplicate) {
		t.Fatalf("duplicate error = %v", err)
	}

	feedback := reviewFor(t, resubmitted, "peer-1", "participant-1", performance.ReviewKind360Feedback, 1, 0, "2026-09-07T12:00:00Z")
	withFeedback, err := resubmitted.Submit(feedback)
	if err != nil {
		t.Fatalf("collect 360 feedback: %v", err)
	}
	feedbackViews, err := withFeedback.ParticipantReviews("participant-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(feedbackViews) != 3 {
		t.Fatalf("feedback views = %+v", feedbackViews)
	}
	if feedbackViews[0].ReviewerIdentityVisible == false && feedbackViews[0].Relationship == performance.ReviewerRelationshipPeer {
		t.Fatal("peer identity unexpectedly anonymous")
	}
}

func TestTodo_PERFORMANCE_003_Mutation(t *testing.T) {
	collection := reviewCollection(t)
	review := reviewFor(t, collection, "manager-1", "participant-1", performance.ReviewKindReview, 1, 0, "2026-09-05T12:00:00Z")
	accepted, err := collection.Submit(review)
	if err != nil {
		t.Fatal(err)
	}
	mutant := review
	mutant.Rating = "1"
	mutated, err := accepted.Submit(reviewFor(t, accepted, "manager-1", "participant-1", performance.ReviewKindReview, 2, 1, "2026-09-06T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if accepted.CanonicalDigest == mutated.CanonicalDigest {
		t.Fatal("changed rating did not change append-only digest")
	}
	if accepted.Reviews[0].Rating != "4" || mutant.Rating != "1" {
		t.Fatal("prior review was mutated")
	}

	unauthorized := reviewFor(t, accepted, "not-in-graph", "participant-1", performance.ReviewKind360Feedback, 1, 0, "2026-09-06T12:00:00Z")
	if _, err := accepted.Submit(unauthorized); !errors.Is(err, performance.ErrReviewerNotFrozen) {
		t.Fatalf("unauthorized reviewer error = %v", err)
	}
	closed := reviewFor(t, accepted, "manager-1", "participant-1", performance.ReviewKindReview, 2, 1, "2026-10-01T00:00:00Z")
	if _, err := accepted.Submit(closed); !errors.Is(err, performance.ErrReviewCutoffClosed) {
		t.Fatalf("cutoff error = %v", err)
	}
	raw := reviewFor(t, accepted, "manager-1", "participant-1", performance.ReviewKindReview, 2, 1, "2026-09-06T12:00:00Z")
	raw.NarrativeDigest = "the raw narrative must not be accepted"
	if _, err := accepted.Submit(raw); !errors.Is(err, performance.ErrInvalidReview) {
		t.Fatalf("raw narrative error = %v", err)
	}
}
