package matching

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const matchingUUIDBase = "00000000-0000-4000-8000-000000000"

func matchingRef(kind values.Kind, n string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: kind, Id: matchingUUIDBase + n}
}

func matchingWindow(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC))
	window, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return window
}

func matchingMoney(t *testing.T, text string) values.Money {
	t.Helper()
	money, err := values.NewMoney(text, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return money
}

func validMatchRequest(t *testing.T) MatchRequest {
	t.Helper()
	request := MatchRequest{
		RequestID:                 "match-request-1",
		Revision:                  1,
		RequesterScope:            matchingRef("organization", "001"),
		TargetRef:                 matchingRef("position", "002"),
		CandidateSourceRef:        matchingRef("candidate_source", "003"),
		RequiredQualificationRefs: []values.EntityRef{matchingRef("qualification", "004")},
		Constraints: []Constraint{
			{Kind: ConstraintLocation, Mode: ConstraintHard, Location: "new-york"},
			{Kind: ConstraintAvailabilityWindow, Mode: ConstraintHard, Window: matchingWindow(t)},
			{Kind: ConstraintCostCeiling, Mode: ConstraintSoft, Weight: 5, Cost: matchingMoney(t, "100.00")},
		},
		Fairness:    FairnessPolicy{PolicyRef: "policy:fairness", Version: "v1"},
		SnapshotRef: matchingRef("snapshot", "005"),
		AsOf:        values.NewInstant(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)),
		Ranking:     RankingPolicy{PolicyRef: "policy:ranking", Version: "v1", ScoreVersion: 1, TieBreak: TieBreakCandidateRef},
		Explanation: ExplanationPolicy{PolicyRef: "policy:explain", Version: "v1", IncludeConstraintReasons: true},
	}
	return request
}

func candidateFact(t *testing.T, id, location, cost string, qualified bool) CandidateFacts {
	t.Helper()
	qualifications := []values.EntityRef(nil)
	if qualified {
		qualifications = []values.EntityRef{matchingRef("qualification", "004")}
	}
	return CandidateFacts{
		CandidateRef: matchingRef("candidate", id), SourceRef: matchingRef("candidate_source", "003"),
		Location: location, Availability: []values.EffectiveInterval{matchingWindow(t)}, Cost: matchingMoney(t, cost),
		QualificationRefs: qualifications,
	}
}

func TestTodo_MATCH_001(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{
		candidateFact(t, "006", "new-york", "90.00", true),
		candidateFact(t, "007", "new-york", "90.00", true),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Match(context.Background(), port, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 || result.Matches[0].Rank != 1 || result.Matches[1].Rank != 2 {
		t.Fatalf("matches = %+v", result.Matches)
	}
	if result.Matches[0].CandidateRef.String() >= result.Matches[1].CandidateRef.String() {
		t.Fatal("equal scores were not tied by candidate reference")
	}
	if !result.Matches[0].Eligible || result.Matches[0].Score.Total != 6 {
		t.Fatalf("match = %+v", result.Matches[0])
	}
	if result.Matches[0].CanonicalDigest == "" || result.CanonicalDigest == "" {
		t.Fatal("matching result was not digested")
	}
	if _, err := Explain(request); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_MATCH_001_Property(t *testing.T) {
	first, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	secondInput := validMatchRequest(t)
	secondInput.Constraints[2], secondInput.Constraints[0] = secondInput.Constraints[0], secondInput.Constraints[2]
	second, err := NewMatchRequest(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("constraint input order changed the request digest")
	}
	first.RequiredQualificationRefs[0] = matchingRef("qualification", "008")
	if second.RequiredQualificationRefs[0].String() == first.RequiredQualificationRefs[0].String() {
		t.Fatal("request constructor aliased qualification refs")
	}
}

func TestTodo_MATCH_001_Golden(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := request.CanonicalDigest; got == "" || len(got) != len("sha256:")+64 {
		t.Fatalf("canonical digest = %q", got)
	}
	if request.Canonical() == nil {
		t.Fatal("valid request has no canonical bytes")
	}
}

func TestTodo_MATCH_001_Conformance(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := request.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Authority == "" || explanation.RankingPolicy == "" {
		t.Fatalf("explanation = %+v", explanation)
	}
}

func TestTodo_MATCH_001_Mutation(t *testing.T) {
	request := validMatchRequest(t)
	request.Constraints[0].Kind = ConstraintKind("UNDECLARED")
	if _, err := NewMatchRequest(request); !errors.Is(err, ErrUnknownConstraint) {
		t.Fatalf("error = %v, want ErrUnknownConstraint", err)
	}
	request = validMatchRequest(t)
	request.Constraints[0].Mode = ConstraintHard
	request.Constraints[0].Location = "other"
	request, err := NewMatchRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{candidateFact(t, "006", "new-york", "90.00", true)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Match(context.Background(), port, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matches[0].Eligible {
		t.Fatal("hard constraint failure remained eligible")
	}
}
