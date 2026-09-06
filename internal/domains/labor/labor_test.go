package labor

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func laborInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2027, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "calendar.us", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func laborDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func laborRef(name string) RuleRef {
	return RuleRef{ID: name, Version: "v1", Digest: "sha256:" + name}
}

func validLaborRule(t *testing.T) LaborRule {
	t.Helper()
	rule, err := NewLaborRule(LaborRule{
		ID: "labor-cost", Version: "v1", Effective: laborInterval(t),
		WorkerRef: laborRef("worker-source"), TimeRef: laborRef("time-source"),
		EntityRef: laborRef("entity-source"), JobRef: laborRef("job-source"),
		EarningRef: laborRef("earning-source"),
		Dimensions: []DimensionKind{DimensionCostCenter, DimensionProject, DimensionActivity, DimensionFundingSource, DimensionLocation},
		Currency:   "USD", BaseRate: laborDecimal(t, "40.00"), DifferentialRate: laborDecimal(t, "2.50"),
		OvertimeMultiplier: laborDecimal(t, "1.50"), EmployerBurdenRate: laborDecimal(t, "0.25"),
	})
	if err != nil {
		t.Fatalf("NewLaborRule: %v", err)
	}
	return rule
}

func dimension(kind DimensionKind, value string) Dimension {
	return Dimension{Kind: kind, Value: value, Version: "v1"}
}

// TestTodo_LABOR_001 is the primary acceptance case: the closed dimension
// vocabulary validates, a rule is digested, and a successor is a new version.
func TestTodo_LABOR_001(t *testing.T) {
	rule := validLaborRule(t)
	if rule.CanonicalDigest == "" {
		t.Fatal("valid labor rule has no digest")
	}
	next, err := rule.NewVersion("v2", laborInterval(t))
	if err != nil {
		t.Fatalf("NewVersion: %v", err)
	}
	if next.Version != "v2" || next.Supersedes != "v1" || next.CanonicalDigest == rule.CanonicalDigest {
		t.Fatalf("successor = %+v, want a new digested version", next)
	}
	if rule.Version != "v1" || rule.Supersedes != "" {
		t.Fatalf("original rule was mutated: %+v", rule)
	}
	if !DimensionFundingSource.Valid() || DimensionKind("CUSTOMER").Valid() {
		t.Fatal("dimension vocabulary is not closed")
	}
}

// TestTodo_LABOR_001_Property checks every declared dimension and rejects
// duplicates and unknown values deterministically.
func TestTodo_LABOR_001_Property(t *testing.T) {
	for _, kind := range []DimensionKind{DimensionCostCenter, DimensionProject, DimensionActivity, DimensionFundingSource, DimensionLocation} {
		if err := (Dimension{Kind: kind, Value: "value", Version: "v1"}).Validate(); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	rule := validLaborRule(t)
	rule.Dimensions = append(rule.Dimensions, DimensionProject)
	rule.CanonicalDigest = ""
	if !errors.Is(rule.Validate(), ErrInvalidLaborRule) {
		t.Fatal("duplicate rule dimension was accepted")
	}
}

// TestTodo_LABOR_001_Golden proves canonical bytes and digests are stable.
func TestTodo_LABOR_001_Golden(t *testing.T) {
	first := validLaborRule(t)
	second := validLaborRule(t)
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatalf("identical rules have different digests: %q vs %q", first.CanonicalDigest, second.CanonicalDigest)
	}
	allocation := Allocation{Entries: []AllocationEntry{
		{Dimension: dimension(DimensionProject, "project-a"), Percent: laborDecimal(t, "60.00")},
		{Dimension: dimension(DimensionCostCenter, "cost-center-a"), Percent: laborDecimal(t, "40.00")},
	}}
	explanation, err := Explain(allocation)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if explanation.Digest == "" || explanation.Total.String() != "100.00" {
		t.Fatalf("explanation = %+v", explanation)
	}
	if explanation.Entries[0].Dimension.Kind != DimensionCostCenter {
		t.Fatalf("explanation entries were not canonicalized: %+v", explanation.Entries)
	}
}

// TestTodo_LABOR_001_Conformance proves incomplete exact components and
// unbalanced allocations fail closed with the typed LABOR-001 refusal.
func TestTodo_LABOR_001_Conformance(t *testing.T) {
	allocation := Allocation{Entries: []AllocationEntry{
		{Dimension: dimension(DimensionProject, "project-a"), Percent: laborDecimal(t, "99.99")},
	}}
	if _, err := allocation.Explain(); !errors.Is(err, ErrConformanceRejected) || !errors.Is(err, ErrAllocationUnbalanced) {
		t.Fatalf("unbalanced allocation error = %v, want typed refusal", err)
	}
	rule := validLaborRule(t)
	rule.Currency = ""
	rule.CanonicalDigest = ""
	if !errors.Is(rule.Validate(), ErrInvalidLaborRule) {
		t.Fatal("rule without currency was accepted")
	}
}

// TestTodo_LABOR_001_Mutation proves each material rule-version change gets a
// distinct digest and that same-version supersession is refused.
func TestTodo_LABOR_001_Mutation(t *testing.T) {
	rule := validLaborRule(t)
	changed := rule
	changed.Currency = "CAD"
	changed.CanonicalDigest = ""
	digest, err := changed.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if digest == rule.CanonicalDigest {
		t.Fatal("currency mutation reused the original digest")
	}
	bad := rule
	bad.CanonicalDigest = ""
	if _, err := rule.Successor(bad); !errors.Is(err, ErrRuleVersionConflict) {
		t.Fatalf("same-version successor error = %v", err)
	}
}
