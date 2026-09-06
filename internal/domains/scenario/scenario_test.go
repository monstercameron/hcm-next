package scenario

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func scenarioHorizon(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2026, time.February, 1)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func baseScenario(t *testing.T) ScenarioRevision {
	t.Helper()
	s, err := NewScenarioRevision(ScenarioRevision{
		ScenarioID: "scenario-1", Revision: 1, Owner: "workforce-planning", Scope: "north-america",
		Horizon: scenarioHorizon(t), BaselineSnapshotRef: "snapshot:2026-01", Author: "planner-1",
		AuthorityDisclaimer: "simulation only; does not mutate authoritative facts", Lifecycle: LifecycleDraft,
		Assumptions: []Assumption{{Key: "headcount.target", Value: DecimalValue(values.MustDecimal("12", 0, values.RoundingHalfEven)), Unit: "HEAD", ProvenanceRefs: []string{"forecast:2026"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTodo_SCENARIO_001(t *testing.T) {
	parent := baseScenario(t)
	child, err := parent.Fork(Assumption{Key: "headcount.target", Value: DecimalValue(values.MustDecimal("14", 0, values.RoundingHalfEven)), Unit: "HEAD", ProvenanceRefs: []string{"plan:change-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if child.Revision != 2 || child.ParentRevision != 1 || child.ParentDigest != parent.CanonicalDigest || child.CanonicalDigest == "" {
		t.Fatalf("fork = %+v", child)
	}
	if parent.Revision != 1 || parent.Assumptions[0].Value.Number.String() != "12" {
		t.Fatal("fork mutated parent")
	}
	if child.Assumptions[0].Value.Number.String() != "14" {
		t.Fatalf("child assumptions = %+v", child.Assumptions)
	}
	if _, err := Explain(child); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_SCENARIO_001_Property(t *testing.T) {
	s := baseScenario(t)
	clone := s.AssumptionsCopy()
	clone[0].ProvenanceRefs[0] = "tampered"
	if s.Assumptions[0].ProvenanceRefs[0] == "tampered" {
		t.Fatal("assumptions copy aliases provenance")
	}
	other := baseScenario(t)
	if s.CanonicalDigest != other.CanonicalDigest {
		t.Fatal("equal scenarios have different digests")
	}
}

func TestTodo_SCENARIO_001_Golden(t *testing.T) {
	s := baseScenario(t)
	if got := s.Canonical(); len(got) == 0 {
		t.Fatal("scenario has no canonical encoding")
	}
	if got, err := s.Digest(); err != nil || got != s.CanonicalDigest {
		t.Fatalf("digest = %q, %v", got, err)
	}
	explanation, err := s.Explain()
	if err != nil || len(explanation.AssumptionKeys) != 1 || explanation.Digest != s.CanonicalDigest {
		t.Fatalf("explanation = %+v, %v", explanation, err)
	}
}

func TestTodo_SCENARIO_001_Mutation(t *testing.T) {
	s := baseScenario(t)
	if _, err := s.Fork(Assumption{Key: "ungrounded", Value: TextValue("guess"), Unit: "TEXT"}); !errors.Is(err, ErrMissingProvenance) {
		t.Fatalf("ungrounded fork error = %v", err)
	}
	bad := s
	bad.CanonicalDigest = "sha256:tampered"
	if err := bad.Validate(); err == nil {
		t.Fatal("tampered digest accepted")
	}
}
