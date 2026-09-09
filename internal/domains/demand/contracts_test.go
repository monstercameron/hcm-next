package demand

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_DEMAND_001(t *testing.T) {
	s := validSignal(t)
	if err := s.Validate(); err != nil {
		t.Fatalf("valid demand signal rejected: %v", err)
	}
	r, err := NewCoverageRequirement("coverage-1", s.Scenario, s.Version, []DemandSignal{s})
	if err != nil {
		t.Fatalf("compose requirement: %v", err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("valid coverage requirement rejected: %v", err)
	}
	if len(r.Signals) != 1 {
		t.Fatalf("signals = %d, want 1", len(r.Signals))
	}
}

func TestTodo_DEMAND_001_Property(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*DemandSignal)
	}{
		{"work_interval", func(s *DemandSignal) { s.Work = values.EffectiveInterval{} }},
		{"location", func(s *DemandSignal) { s.Location = "" }},
		{"quantity", func(s *DemandSignal) { s.Quantity = values.Quantity{} }},
		{"unit", func(s *DemandSignal) { s.Unit = "" }},
		{"skill", func(s *DemandSignal) { s.Skill = "" }},
		{"priority", func(s *DemandSignal) { s.Priority = -1 }},
		{"source", func(s *DemandSignal) { s.Source = "" }},
		{"confidence", func(s *DemandSignal) { s.Confidence = values.Decimal{} }},
		{"scenario", func(s *DemandSignal) { s.Scenario = "" }},
		{"version", func(s *DemandSignal) { s.Version = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := validSignal(t)
			tc.mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("incomplete demand signal accepted")
			}
		})
	}
}

func TestTodo_DEMAND_001_Conformance(t *testing.T) {
	s := validSignal(t)
	r, err := NewCoverageRequirement("coverage-1", s.Scenario, s.Version, []DemandSignal{s})
	if err != nil {
		t.Fatal(err)
	}
	r.Signals[0].Scenario = "other-scenario"
	if err := r.Validate(); err == nil {
		t.Fatal("requirement accepted a signal from another scenario/version")
	}
	if _, err := NewCoverageRequirement("coverage-1", s.Scenario, s.Version, nil); err == nil {
		t.Fatal("empty composition accepted")
	}
}

func TestTodo_DEMAND_001_CoverageAndDigest(t *testing.T) {
	s := validSignal(t)
	s.ConfidenceClass = ConfidenceHigh
	coverage, err := NewCoverageRequirement("coverage-1", s.Scenario, s.Version, []DemandSignal{s})
	if err != nil {
		t.Fatal(err)
	}
	supply := []SupplyReference{{Ref: "supply-1", Location: s.Location, Skill: s.Skill, Work: s.Work, Quantity: s.Quantity}}
	result, err := coverage.Evaluate(supply)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Covered || len(result.Shortfalls) != 0 || result.CanonicalDigest == "" {
		t.Fatalf("covered result = %+v", result)
	}
	if got, err := coverage.Digest(); err != nil || got != coverage.CanonicalDigest {
		t.Fatalf("coverage digest = %q, %v", got, err)
	}

	less, err := values.NewQuantity("1", s.Unit, 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	result, err = coverage.Evaluate([]SupplyReference{{Ref: "supply-1", Location: s.Location, Skill: s.Skill, Work: s.Work, Quantity: less}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Covered || len(result.Shortfalls) != 1 || result.Shortfalls[0].Shortfall.Value().String() != "1" {
		t.Fatalf("shortfall result = %+v", result)
	}
	if explanation, err := Explain(coverage); err != nil || explanation.Digest == "" {
		t.Fatalf("Explain = %+v, %v", explanation, err)
	}
}

func TestTodo_DEMAND_001_ConformanceDigest(t *testing.T) {
	s := validSignal(t)
	s.ConfidenceClass = ConfidenceMedium
	coverage, err := NewCoverageRequirement("coverage-1", s.Scenario, s.Version, []DemandSignal{s})
	if err != nil {
		t.Fatal(err)
	}
	bad := coverage
	bad.CanonicalDigest = "sha256:tampered"
	if err := bad.Validate(); err == nil {
		t.Fatal("tampered coverage digest accepted")
	}
	badSignal := s
	badSignal.ConfidenceClass = "UNDECLARED"
	if err := badSignal.Validate(); err == nil {
		t.Fatal("unknown confidence class accepted")
	}
}

func validSignal(t *testing.T) DemandSignal {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.April, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2026, time.April, 2)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := values.NewQuantity("2", "FTE", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return DemandSignal{
		SignalID: "signal-1", Work: interval, Location: "loc-nyc", Quantity: quantity, Unit: "FTE",
		Skill: "nursing", Priority: 1, Source: "forecast", Confidence: values.MustDecimal("0.9", 1, values.RoundingHalfEven),
		Scenario: "base", Version: "v1",
	}
}
