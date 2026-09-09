package indexation

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_LEGAL_TOOL_010(t *testing.T) {
	set, err := LoadFixture("indexation.yaml")
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := set.Digest(); len(got) != 64 {
		t.Fatalf("digest length = %d, want 64", len(got))
	}
	if !strings.Contains(set.Explain(), "indexation set=us-state-wage-indexation") {
		t.Fatalf("Explain = %q", set.Explain())
	}
}

func TestTodo_LEGAL_TOOL_010_Golden(t *testing.T) {
	set, err := LoadFixture("indexation.yaml")
	if err != nil {
		t.Fatal(err)
	}
	beforeAK, _ := values.ParseLocalDate("2026-06-30")
	ak, err := set.Schedule("us-ak-wage-floor", beforeAK)
	if err != nil {
		t.Fatalf("Alaska schedule: %v", err)
	}
	if len(ak.Values) != 3 {
		t.Fatalf("Alaska values = %d, want 3", len(ak.Values))
	}
	wantAK := []string{"15.00 USD", "15.45 USD", "15.90 USD"}
	for i, want := range wantAK {
		if got := ak.Values[i].Amount.String(); got != want {
			t.Errorf("Alaska value %d = %s, want %s", i, got, want)
		}
		if ak.Values[i].Occurrence.EnginePackage != "internal/engines/schedule" {
			t.Errorf("Alaska occurrence engine = %q", ak.Values[i].Occurrence.EnginePackage)
		}
	}
	ct, err := set.Schedule("us-ct-wage-floor", beforeAK)
	if err != nil {
		t.Fatalf("Connecticut schedule: %v", err)
	}
	if len(ct.Values) != 1 || ct.Values[0].Amount.String() != "17.45 USD" {
		t.Fatalf("Connecticut values = %+v, want one 17.45 USD", ct.Values)
	}
	mo, err := set.Schedule("us-mo-wage-floor", beforeAK)
	if err != nil {
		t.Fatalf("Missouri schedule: %v", err)
	}
	if len(mo.Values) != 0 {
		t.Fatalf("Missouri values = %+v, want none after HB 567 removed CPI indexing", mo.Values)
	}
}

func TestTodo_LEGAL_TOOL_010_Conformance(t *testing.T) {
	set, err := LoadFixture("indexation.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range set.Registry {
		if row.Review != ReviewReviewed && row.Review != ReviewUnreviewed {
			t.Fatalf("state %s has invalid review flag %q", row.State, row.Review)
		}
		if strings.TrimSpace(row.StatuteCitation) == "" {
			t.Fatalf("state %s has no citation", row.State)
		}
	}
	param := set.Parameters[0]
	param.IndexFormula = ""
	set.Parameters[0] = param
	err = set.Validate()
	if err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "parameters[0].index_formula") {
		t.Fatalf("invalid formula error = %v, want typed field refusal", err)
	}

	withoutPoint, err := LoadFixture("indexation.yaml")
	if err != nil {
		t.Fatal(err)
	}
	points := withoutPoint.IndexPoints[:0]
	for _, point := range withoutPoint.IndexPoints {
		if point.ParameterID != "us-ak-wage-floor" {
			points = append(points, point)
		}
	}
	withoutPoint.IndexPoints = points
	staleDate, _ := values.ParseLocalDate("2027-08-01")
	if _, err := withoutPoint.Schedule("us-ak-wage-floor", staleDate); err == nil || !errors.Is(err, ErrCoverageUnknown) || !strings.Contains(err.Error(), "next_adjustment_date") {
		t.Fatalf("lapsed index error = %v, want RULE_COVERAGE_UNKNOWN on next_adjustment_date", err)
	}
}
