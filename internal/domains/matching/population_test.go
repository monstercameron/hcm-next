package matching

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestTodo_MATCH_002(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	outOfSource := candidateFact(t, "008", "new-york", "90.00", true)
	outOfSource.SourceRef = matchingRef("candidate_source", "999")
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{
		candidateFact(t, "006", "new-york", "90.00", true),
		candidateFact(t, "007", "new-york", "90.00", true),
		outOfSource,
	})
	if err != nil {
		t.Fatal(err)
	}
	population, err := ResolveCandidatePopulation(context.Background(), port, request, func(_ values.EntityRef, fact CandidateFacts) DisclosureDecision {
		if fact.CandidateRef.Id == matchingUUIDBase+"007" {
			return DisclosureDecision{Reason: "scope:worker-withheld", PolicyVersion: "scope/v1", Completeness: CandidateCompletenessComplete}
		}
		return DisclosureDecision{Allowed: true, PolicyVersion: "scope/v1", Completeness: CandidateCompletenessComplete}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := population.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := len(population.Candidates); got != 1 {
		t.Fatalf("authorized candidates = %d, want 1", got)
	}
	if population.ExclusionCounts["scope:worker-withheld"] != 1 || population.ExclusionCounts[string(ExclusionSourceMismatch)] != 1 {
		t.Fatalf("exclusion counts = %#v", population.ExclusionCounts)
	}
	result, err := MatchAgainstPopulation(context.Background(), request, population)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || result.Matches[0].CandidateRef.Id != matchingUUIDBase+"006" {
		t.Fatalf("frozen match result = %+v", result.Matches)
	}
}

func TestTodo_MATCH_002_Property(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	facts := []CandidateFacts{candidateFact(t, "007", "new-york", "90.00", true), candidateFact(t, "006", "new-york", "90.00", true)}
	first, err := FreezeCandidatePopulation(request, facts, map[string]int{"scope:withheld": 2}, CandidateCompletenessPartial)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FreezeCandidatePopulation(request, []CandidateFacts{facts[1], facts[0]}, map[string]int{"scope:withheld": 2}, CandidateCompletenessPartial)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("candidate enumeration order changed the population digest")
	}
	copyOfFacts := first.CandidateFactsList()
	copyOfFacts[0].Location = "mutated"
	if first.Candidates[0].Location == "mutated" {
		t.Fatal("population exposed mutable candidate facts")
	}
	first.Candidates[0].Location = "mutated"
	if !errors.Is(first.Validate(), ErrInvalidCandidatePopulation) {
		t.Fatalf("mutated population validation error = %v", first.Validate())
	}
}

func TestTodo_MATCH_002_Security(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{candidateFact(t, "006", "new-york", "90.00", true), candidateFact(t, "007", "new-york", "90.00", true)})
	if err != nil {
		t.Fatal(err)
	}
	population, err := ResolveCandidatePopulation(context.Background(), port, request, func(_ values.EntityRef, fact CandidateFacts) DisclosureDecision {
		return DisclosureDecision{Allowed: fact.CandidateRef.Id != matchingUUIDBase+"007", Reason: "scope:denied", PolicyVersion: "scope/v1"}
	})
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := population.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explanation.ExcludedCounts["scope:denied"] != 1 || strings.Contains(explanationText(explanation), matchingUUIDBase+"007") {
		t.Fatalf("excluded identity leaked through explanation: %+v", explanation)
	}
	if _, err := MatchAgainstPopulation(context.Background(), request, CandidatePopulation{}); !errors.Is(err, ErrPopulationUnfrozen) {
		t.Fatalf("unfrozen population error = %v", err)
	}
	if _, err := MatchAgainstPopulation(context.Background(), request, population.Supersede()); !errors.Is(err, ErrPopulationSuperseded) {
		t.Fatalf("superseded population error = %v", err)
	}
}

func explanationText(explanation CandidatePopulationExplanation) string {
	return explanation.RequestID + explanation.PopulationDigest + explanation.Authority
}
