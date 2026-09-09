package snapshot_test

import (
	"errors"
	"strings"
	"testing"

	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	enginesnapshot "github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestSpecificationDeclaresEveryInputExactlyOnce proves the declared input
// contract and the declared input list cannot drift apart: a name added to one
// and not the other would make the completeness verdict silently ignore an
// input the snapshot binds, or demand one it never reads.
func TestSpecificationDeclaresEveryInputExactlyOnce(t *testing.T) {
	t.Parallel()
	spec := promosnapshot.Specification()
	if len(spec.Inputs) != len(promosnapshot.InputNames()) {
		t.Fatalf("the specification declares %d input(s), the package declares %d",
			len(spec.Inputs), len(promosnapshot.InputNames()))
	}
	declared := map[string]bool{}
	for _, name := range promosnapshot.InputNames() {
		declared[name] = true
	}
	for _, requirement := range spec.Inputs {
		if !declared[requirement.Name] {
			t.Fatalf("the specification requires undeclared input %q", requirement.Name)
		}
		delete(declared, requirement.Name)
	}
	if len(declared) != 0 {
		t.Fatalf("the specification omits declared input(s): %v", declared)
	}
}

// TestSpecificationConditionsTheVacancyOnCapacity pins the one conditional
// requirement: the vacancy date matters only when the target position has no
// head available.
func TestSpecificationConditionsTheVacancyOnCapacity(t *testing.T) {
	t.Parallel()
	for _, requirement := range promosnapshot.Specification().Inputs {
		if requirement.Name != promosnapshot.InputTargetPositionVacancy {
			if requirement.Policy != enginesnapshot.InputRequired {
				t.Fatalf("input %s has policy %s, want REQUIRED", requirement.Name, requirement.Policy)
			}
			if requirement.Condition != nil {
				t.Fatalf("required input %s carries a condition", requirement.Name)
			}
			continue
		}
		if requirement.Policy != enginesnapshot.InputConditional {
			t.Fatalf("the vacancy input has policy %s, want CONDITIONAL", requirement.Policy)
		}
		if requirement.Condition == nil {
			t.Fatal("the conditional vacancy input declares no condition")
		}
		if requirement.Condition.InputName != promosnapshot.InputTargetPositionCapacity {
			t.Fatalf("the vacancy condition reads %q, want the capacity input", requirement.Condition.InputName)
		}
		if requirement.Condition.ExpectedValue != promosnapshot.CapacityExhausted {
			t.Fatalf("the vacancy condition activates on %q, want %q",
				requirement.Condition.ExpectedValue, promosnapshot.CapacityExhausted)
		}
	}
	if promosnapshot.CapacityExhausted == promosnapshot.CapacityAvailable {
		t.Fatal("the two capacity observation tokens are the same string")
	}
}

// TestReadersRefuseAnUnconfiguredPort proves an absent reader is refused by
// name rather than turned into a missing input: "nobody asked" and "the record
// says nothing" must never look the same.
func TestReadersRefuseAnUnconfiguredPort(t *testing.T) {
	t.Parallel()
	full := newHarness(t).readers()
	for _, tc := range []struct {
		name    string
		clear   func(*promosnapshot.Readers)
		mention string
	}{
		{"worker", func(r *promosnapshot.Readers) { r.Worker = nil }, "worker facts"},
		{"org", func(r *promosnapshot.Readers) { r.Org = nil }, "organization facts"},
		{"position", func(r *promosnapshot.Readers) { r.Position = nil }, "position facts"},
		{"compensation", func(r *promosnapshot.Readers) { r.Compensation = nil }, "compensation facts"},
		{"bands", func(r *promosnapshot.Readers) { r.Bands = nil }, "pay band catalog"},
		{"budget", func(r *promosnapshot.Readers) { r.Budget = nil }, "budget facts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			readers := full
			tc.clear(&readers)
			err := readers.Validate()
			if !errors.Is(err, promosnapshot.ErrReaderMissing) {
				t.Fatalf("Validate returned %v, want ErrReaderMissing", err)
			}
			if got := err.Error(); !strings.Contains(got, tc.mention) {
				t.Fatalf("the refusal %q does not name the %s port", got, tc.mention)
			}
		})
	}
	if err := full.Validate(); err != nil {
		t.Fatalf("a fully composed reader set was refused: %v", err)
	}
}

// TestBudgetQueryValidate covers the one port request this package defines.
func TestBudgetQueryValidate(t *testing.T) {
	t.Parallel()
	valid := promosnapshot.BudgetQuery{
		Tenant: "harborcare-demo",
		Scope:  fixtureBudgetScope,
		Period: fixtureBudgetPeriod,
		AsOf:   mustInstant(t, fixtureKnownAtText),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a complete budget query was refused: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*promosnapshot.BudgetQuery)
	}{
		{"no tenant", func(q *promosnapshot.BudgetQuery) { q.Tenant = "" }},
		{"no scope", func(q *promosnapshot.BudgetQuery) { q.Scope = "" }},
		{"no period", func(q *promosnapshot.BudgetQuery) { q.Period = "" }},
		{"no as-of", func(q *promosnapshot.BudgetQuery) { q.AsOf = values.Instant{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			broken := valid
			tc.mutate(&broken)
			if err := broken.Validate(); !errors.Is(err, promosnapshot.ErrRequestInvalid) {
				t.Fatalf("Validate returned %v, want ErrRequestInvalid", err)
			}
		})
	}
}
