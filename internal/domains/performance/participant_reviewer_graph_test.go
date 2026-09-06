package performance_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/performance"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func graphCycle(t *testing.T) performance.PerformanceCycle {
	t.Helper()
	cycle, err := performance.NewPerformanceCycle("cycle-2026", performance.PopulationBindingRef{DefinitionID: "population-2026", RevisionVersion: "v4", Digest: "sha256:population"}, performance.CalendarBindingRef{Ref: "US.gregorian", Version: "v2026", Digest: "sha256:calendar"}, performance.RatingScaleVersionRef{ID: "scale", Version: "v3", Digest: "sha256:scale"})
	if err != nil {
		t.Fatalf("NewPerformanceCycle: %v", err)
	}
	return cycle
}

func graphInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	var instant values.Instant
	if err := instant.UnmarshalText([]byte(text)); err != nil {
		t.Fatalf("instant %q: %v", text, err)
	}
	return instant
}

func graphInputs() ([]performance.ParticipantRef, []performance.ReviewerAssignment) {
	return []performance.ParticipantRef{{ID: "p-1"}, {ID: "p-2"}}, []performance.ReviewerAssignment{
		{ParticipantID: "p-1", ReviewerID: "manager-1", Relationship: performance.ReviewerRelationshipManager},
		{ParticipantID: "p-2", ReviewerID: "manager-2", Relationship: performance.ReviewerRelationshipManager},
	}
}

func TestTodo_PERFORMANCE_002(t *testing.T) {
	participants, reviewers := graphInputs()
	graph, err := performance.FreezeParticipantReviewerGraph(graphCycle(t), participants, reviewers, performance.DefaultReviewerGraphRules(), graphInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("FreezeParticipantReviewerGraph: %v", err)
	}
	if graph.GraphRevision != 1 || graph.CycleRevision != 1 || graph.Digest == "" || len(graph.Participants) != 2 || len(graph.Reviewers) != 2 {
		t.Fatalf("graph = %+v", graph)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if _, err := graph.Explain(); err != nil {
		t.Fatalf("Explain: %v", err)
	}
	participants[0].ID = "tampered"
	if graph.Participants[0].ID == "tampered" {
		t.Fatal("freeze retained caller participant slice")
	}
}

func TestTodo_PERFORMANCE_002_Property(t *testing.T) {
	participants, reviewers := graphInputs()
	cycle := graphCycle(t)
	a, err := performance.FreezeParticipantReviewerGraph(cycle, participants, reviewers, performance.DefaultReviewerGraphRules(), graphInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("freeze A: %v", err)
	}
	b, err := performance.FreezeParticipantReviewerGraph(cycle, participants, reviewers, performance.DefaultReviewerGraphRules(), graphInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("freeze B: %v", err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical graph digests differ: %q vs %q", a.Digest, b.Digest)
	}
	updated := []performance.ReviewerAssignment{
		{ParticipantID: "p-1", ReviewerID: "manager-3", Relationship: performance.ReviewerRelationshipManager},
		{ParticipantID: "p-2", ReviewerID: "manager-2", Relationship: performance.ReviewerRelationshipManager},
	}
	next, err := a.Amend(participants, updated, graphInstant(t, "2026-09-02T00:00:00Z"))
	if err != nil {
		t.Fatalf("Amend: %v", err)
	}
	if next.GraphRevision != 2 || next.SupersedesGraphRevision != 1 || next.Digest == a.Digest {
		t.Fatalf("amended graph = %+v", next)
	}
	if a.GraphRevision != 1 || a.Reviewers[0].ReviewerID == "manager-3" {
		t.Fatal("amendment mutated prior graph")
	}
}

func TestTodo_PERFORMANCE_002_Golden(t *testing.T) {
	participants, reviewers := graphInputs()
	graph, err := performance.FreezeParticipantReviewerGraph(graphCycle(t), participants, reviewers, performance.DefaultReviewerGraphRules(), graphInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	explanation, err := performance.ExplainParticipantReviewerGraph(graph)
	if err != nil {
		t.Fatalf("ExplainParticipantReviewerGraph: %v", err)
	}
	if explanation.ParticipantIDs[0] != "p-1" || explanation.ParticipantIDs[1] != "p-2" || explanation.ReviewerCount != 2 || explanation.Digest != graph.Digest {
		t.Fatalf("golden explanation = %+v", explanation)
	}
}

func TestTodo_PERFORMANCE_002_Security(t *testing.T) {
	cycle := graphCycle(t)
	participants := []performance.ParticipantRef{{ID: "p-1"}, {ID: "p-2"}}
	self := []performance.ReviewerAssignment{{ParticipantID: "p-1", ReviewerID: "p-1", Relationship: performance.ReviewerRelationshipManager}, {ParticipantID: "p-2", ReviewerID: "manager-2", Relationship: performance.ReviewerRelationshipManager}}
	if _, err := performance.FreezeParticipantReviewerGraph(cycle, participants, self, performance.DefaultReviewerGraphRules(), graphInstant(t, "2026-09-01T00:00:00Z")); !errors.Is(err, performance.ErrSelfReviewRejected) {
		t.Fatalf("self-review error = %v", err)
	}
	participantReviewer := []performance.ReviewerAssignment{{ParticipantID: "p-1", ReviewerID: "p-2", Relationship: performance.ReviewerRelationshipPeer}, {ParticipantID: "p-2", ReviewerID: "manager-2", Relationship: performance.ReviewerRelationshipManager}}
	if _, err := performance.FreezeParticipantReviewerGraph(cycle, participants, participantReviewer, performance.DefaultReviewerGraphRules(), graphInstant(t, "2026-09-01T00:00:00Z")); !errors.Is(err, performance.ErrReviewerParticipantRejected) {
		t.Fatalf("reviewer-participant error = %v", err)
	}
	reject := performance.DefaultReviewerGraphRules()
	reject.ReassignmentPolicy = performance.GraphPolicyReject
	baseParticipants, baseReviewers := graphInputs()
	base, err := performance.FreezeParticipantReviewerGraph(cycle, baseParticipants, baseReviewers, reject, graphInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("base freeze: %v", err)
	}
	changed := []performance.ReviewerAssignment{{ParticipantID: "p-1", ReviewerID: "manager-9", Relationship: performance.ReviewerRelationshipManager}, baseReviewers[1]}
	if _, err := base.Amend(baseParticipants, changed, graphInstant(t, "2026-09-02T00:00:00Z")); !errors.Is(err, performance.ErrReassignmentRejected) {
		t.Fatalf("reassignment error = %v", err)
	}
}
