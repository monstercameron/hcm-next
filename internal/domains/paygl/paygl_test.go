package paygl

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/labor"
	"github.com/monstercameron/hcm-next/internal/domains/payroll"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func glDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func glInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2027, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "calendar.us", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func glRun(t *testing.T) payroll.PayrollRun {
	t.Helper()
	run, err := payroll.NewPayrollRun("run-gl", "monthly",
		payroll.PeriodRef{ID: "period", Version: "v1", Digest: "sha256:period"},
		payroll.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "v1", Digest: "sha256:population"}, "sha256:inputs")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Calculate("sha256:calculation")
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func glDimension() labor.Dimension {
	return labor.Dimension{Kind: labor.DimensionCostCenter, Value: "cc-1", Version: "v1"}
}

func glRule(t *testing.T) AccountingRule {
	t.Helper()
	rule, err := NewAccountingRule(AccountingRule{
		ID: "salary", Version: "v1", Effective: glInterval(t), ComponentKind: ComponentEarning, ComponentCode: "SALARY",
		Dimension: glDimension(), DebitAccount: "6000", CreditAccount: "2100", Currency: "USD",
		Rounding: values.RoundingExactRequired, SuspensePolicy: SuspensePolicyReject,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func glLine(t *testing.T, id, amount string) CalculatedLine {
	t.Helper()
	return CalculatedLine{ID: id, ComponentKind: ComponentEarning, ComponentCode: "SALARY", Amount: glDecimal(t, amount), Currency: "USD", Dimension: glDimension()}
}

// TestTodo_PAYGL_001 is the primary acceptance case: complete versioned
// mapping rules derive an exact and balanced debit/credit posting set.
func TestTodo_PAYGL_001(t *testing.T) {
	rule := glRule(t)
	derivation, err := DerivePostings(glRun(t), []AccountingRule{rule}, []CalculatedLine{glLine(t, "line-1", "100.00")})
	if err != nil {
		t.Fatal(err)
	}
	if !derivation.Balanced || len(derivation.Postings) != 2 || derivation.TotalDebits.String() != "100.00" || derivation.TotalCredits.String() != "100.00" || derivation.Digest == "" {
		t.Fatalf("derivation = %+v", derivation)
	}
}

// TestTodo_PAYGL_001_Property proves balance for every non-negative exact
// line amount in a representative table.
func TestTodo_PAYGL_001_Property(t *testing.T) {
	for _, amount := range []string{"0.01", "1.00", "99.99", "1000000.00"} {
		derivation, err := DerivePostings(glRun(t), []AccountingRule{glRule(t)}, []CalculatedLine{glLine(t, "line-"+amount, amount)})
		if err != nil {
			t.Fatalf("amount %s: %v", amount, err)
		}
		if !derivation.TotalDebits.Equal(derivation.TotalCredits) {
			t.Fatalf("amount %s unbalanced", amount)
		}
	}
}

// TestTodo_PAYGL_001_Conformance proves incomplete rules and missing mappings
// fail with the named conformance refusal before any publication exists.
func TestTodo_PAYGL_001_Conformance(t *testing.T) {
	bad := glRule(t)
	bad.CreditAccount = ""
	bad.CreditAccountRef = ""
	if _, err := NewAccountingRule(bad); !errors.Is(err, ErrInvalidAccountingRule) {
		t.Fatalf("incomplete rule = %v", err)
	}
	if _, err := DerivePostings(glRun(t), []AccountingRule{glRule(t)}, []CalculatedLine{{ID: "missing", ComponentKind: ComponentTax, ComponentCode: "FED", Amount: glDecimal(t, "1.00"), Currency: "USD", Dimension: glDimension()}}); !errors.Is(err, ErrPostingRejected) || !errors.Is(err, ErrNoAccountingRule) {
		t.Fatalf("missing mapping = %v", err)
	}
}

// TestTodo_PAYGL_001_Mutation proves version and mapping changes receive new
// digests and same-version successors are refused.
func TestTodo_PAYGL_001_Mutation(t *testing.T) {
	rule := glRule(t)
	next, err := rule.NewVersion("v2", glInterval(t))
	if err != nil {
		t.Fatal(err)
	}
	if next.CanonicalDigest == rule.CanonicalDigest || next.Supersedes != rule.Version {
		t.Fatalf("next = %+v", next)
	}
	if _, err := rule.Successor(rule); !errors.Is(err, ErrAccountingRuleConflict) {
		t.Fatalf("same-version successor = %v", err)
	}
	changed := rule
	changed.DebitAccount = "6100"
	changed.CanonicalDigest = ""
	if changed.computedDigest() == rule.CanonicalDigest {
		t.Fatal("mapping mutation reused digest")
	}
}

// TestTodo_PAYGL_001_Explain keeps the audit narrative useful without making
// it the programmatic contract.
func TestTodo_PAYGL_001_Explain(t *testing.T) {
	d, err := DerivePostings(glRun(t), []AccountingRule{glRule(t)}, []CalculatedLine{glLine(t, "line-1", "100.00")})
	if err != nil {
		t.Fatal(err)
	}
	if got := Explain(d); !strings.Contains(got, "run-gl") || !strings.Contains(got, "balanced true") {
		t.Fatalf("Explain = %q", got)
	}
}
