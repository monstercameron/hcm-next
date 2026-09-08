package calcpolicy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/payroll/calcpolicy"
	"github.com/monstercameron/hcm-next/internal/domains/taxprofile"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func decimal(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("decimal %q: %v", text, err)
	}
	return d
}

func policy(t *testing.T, mode values.RoundingMode) calcpolicy.Policy {
	t.Helper()
	rule := calcpolicy.CalculationRule{Scale: 2, Rounding: mode, Allocation: calcpolicy.AllocationLargestRemainder, Negative: calcpolicy.NegativePreserve, Zero: calcpolicy.ZeroAllow}
	p, err := calcpolicy.NewPolicy(calcpolicy.Policy{
		ID:       "payroll-exact-decimal",
		Version:  "payroll-exact-decimal/v1",
		Revision: 1,
		Rules: map[calcpolicy.CalculationKind]calcpolicy.CalculationRule{
			calcpolicy.KindPayroll: rule, calcpolicy.KindTax: rule,
			calcpolicy.KindDeduction: rule, calcpolicy.KindRate: rule,
		},
		CurrencyRules: map[string]calcpolicy.CurrencyRule{"USD": {Code: "USD", Scale: 2}},
	})
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	return p
}

func TestTodo_SECARCH_015(t *testing.T) {
	p := policy(t, values.RoundingHalfEven)
	in, err := calcpolicy.NewInput(calcpolicy.Input{CalculationID: "gross-1", Kind: calcpolicy.KindPayroll, Currency: "USD", Amount: decimal(t, "10.005", 3), Allocations: []calcpolicy.Allocation{
		{Key: "first", Order: 1, Weight: decimal(t, "1", 0)},
		{Key: "second", Order: 2, Weight: decimal(t, "1", 0)},
	}})
	if err != nil {
		t.Fatalf("input: %v", err)
	}
	first, err := calcpolicy.Calculate(p, in)
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	second, err := calcpolicy.Calculate(p, in)
	if err != nil {
		t.Fatalf("calculate again: %v", err)
	}
	if first.CanonicalDigest == "" || first.CanonicalDigest != second.CanonicalDigest {
		t.Fatalf("receipt digest is not reproducible: %q vs %q", first.CanonicalDigest, second.CanonicalDigest)
	}
	if got := first.Output.Amount.String(); got != "10.00" {
		t.Fatalf("rounded total = %s, want 10.00", got)
	}
	if got := first.Output.Allocations[0].Amount.String() + "," + first.Output.Allocations[1].Amount.String(); got != "5.00,5.00" {
		t.Fatalf("allocation = %s, want 5.00,5.00", got)
	}
	if first.InputDigest == "" || first.OutputDigest == "" || first.PolicyDigest != p.CanonicalDigest {
		t.Fatal("receipt did not bind all calculation digests")
	}
	if _, err := first.Digest(); err != nil {
		t.Fatalf("receipt validation: %v", err)
	}
	if explanation, err := first.Explain(); err != nil || explanation.ReceiptDigest != first.CanonicalDigest {
		t.Fatalf("explain = %+v, err=%v", explanation, err)
	}

	// Properties required by the contract: allocation is exact, quantization is
	// idempotent, and largest-remainder does not depend on input order.
	for _, total := range []string{"0.01", "1.00", "10.00", "99.99"} {
		amount := decimal(t, total, 2)
		candidate := calcpolicy.Input{Kind: calcpolicy.KindPayroll, Currency: "USD", Amount: amount, Allocations: []calcpolicy.Allocation{
			{Key: "a", Order: 20, Weight: decimal(t, "1", 0)}, {Key: "b", Order: 10, Weight: decimal(t, "2", 0)}, {Key: "c", Order: 30, Weight: decimal(t, "3", 0)},
		}}
		got, err := calcpolicy.Calculate(p, candidate)
		if err != nil {
			t.Fatalf("property total %s: %v", total, err)
		}
		var allocated values.Decimal
		for i, item := range got.Output.Allocations {
			if i == 0 {
				allocated = item.Amount
			} else {
				allocated, err = allocated.Add(item.Amount)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		if !allocated.Equal(got.Output.Amount) {
			t.Fatalf("allocation sum %s != total %s", allocated, got.Output.Amount)
		}
		quantized, err := got.Output.Amount.Quantize(2, values.RoundingHalfEven)
		if err != nil || !quantized.Equal(got.Output.Amount) {
			t.Fatalf("quantization was not idempotent: %s, %v", quantized, err)
		}
	}
}

func TestTodo_SECARCH_015_Golden(t *testing.T) {
	p := policy(t, values.RoundingHalfUp)
	input := calcpolicy.Input{Kind: calcpolicy.KindDeduction, Currency: "USD", Amount: decimal(t, "0.01", 2), Allocations: []calcpolicy.Allocation{
		{Key: "z", Order: 2, Weight: decimal(t, "1", 0)}, {Key: "a", Order: 1, Weight: decimal(t, "1", 0)}, {Key: "m", Order: 3, Weight: decimal(t, "1", 0)},
	}}
	got, err := calcpolicy.Calculate(p, input)
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	if got.Output.Allocations[0].Key != "a" || got.Output.Allocations[0].Amount.String() != "0.01" {
		t.Fatalf("tie break = %+v, want a gets residual", got.Output.Allocations)
	}
	if got.Output.Allocations[1].Amount.String() != "0.00" || got.Output.Allocations[2].Amount.String() != "0.00" {
		t.Fatalf("golden allocations = %+v", got.Output.Allocations)
	}
}

func TestTodo_SECARCH_015_Integration(t *testing.T) {
	p := policy(t, values.RoundingHalfEven)
	in, err := calcpolicy.NewInput(calcpolicy.Input{Kind: calcpolicy.KindRate, Currency: "USD", Base: decimal(t, "4000.00", 2), Rate: decimal(t, "0.075", 3)})
	if err != nil {
		t.Fatalf("input constructor: %v", err)
	}
	receipt, err := calcpolicy.Calculate(p, in)
	if err != nil {
		t.Fatalf("real kernel integration: %v", err)
	}
	if receipt.Output.Amount.String() != "300.00" {
		t.Fatalf("rate output = %s, want 300.00", receipt.Output.Amount)
	}
	if receipt.Output.Currency != "USD" || receipt.PolicyVersion != p.Version {
		t.Fatalf("receipt lost typed collaborators: %+v", receipt)
	}
}

func TestTodo_SECARCH_015_Security(t *testing.T) {
	p := policy(t, values.RoundingHalfEven)
	badCurrency := calcpolicy.Input{Kind: calcpolicy.KindPayroll, Currency: "EUR", Amount: decimal(t, "1.00", 2)}
	if _, err := calcpolicy.Calculate(p, badCurrency); !errors.Is(err, calcpolicy.ErrUnknownCurrency) {
		t.Fatalf("currency error = %v, want ErrUnknownCurrency", err)
	}
	var refusal *calcpolicy.FieldError
	if !errors.As(errForNegative(t, p), &refusal) || refusal.Field != "input.amount" {
		t.Fatalf("negative refusal field = %+v", refusal)
	}

	input := calcpolicy.Input{CalculationID: "sensitive-worker-id", Kind: calcpolicy.KindPayroll, Currency: "USD", Amount: decimal(t, "1.00", 2), Allocations: []calcpolicy.Allocation{{Key: "account-number", Order: 1, Weight: decimal(t, "1", 0)}}}
	receipt, err := calcpolicy.Calculate(p, input)
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	explanation, err := calcpolicy.Explain(receipt)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	for _, secret := range []string{"sensitive-worker-id", "account-number"} {
		if strings.Contains(strings.Join([]string{explanation.PolicyVersion, explanation.InputDigest, explanation.OutputDigest, explanation.ReceiptDigest, explanation.Currency}, "|"), secret) {
			t.Fatalf("explanation disclosed %q", secret)
		}
	}
}

func errForNegative(t *testing.T, p calcpolicy.Policy) error {
	t.Helper()
	strict := p
	strict.Rules = map[calcpolicy.CalculationKind]calcpolicy.CalculationRule{}
	for kind, rule := range p.Rules {
		rule.Negative = calcpolicy.NegativeReject
		strict.Rules[kind] = rule
	}
	strict, err := calcpolicy.NewPolicy(strict)
	if err != nil {
		t.Fatal(err)
	}
	_, err = calcpolicy.Calculate(strict, calcpolicy.Input{Kind: calcpolicy.KindPayroll, Currency: "USD", Amount: decimal(t, "-1.00", 2)})
	return err
}

func TestTodo_SECARCH_015_Mutation(t *testing.T) {
	even := policy(t, values.RoundingHalfEven)
	up := policy(t, values.RoundingHalfUp)
	in := calcpolicy.Input{Kind: calcpolicy.KindPayroll, Currency: "USD", Amount: decimal(t, "1.005", 3)}
	evenReceipt, err := calcpolicy.Calculate(even, in)
	if err != nil {
		t.Fatal(err)
	}
	upReceipt, err := calcpolicy.Calculate(up, in)
	if err != nil {
		t.Fatal(err)
	}
	if evenReceipt.Output.Amount.String() == upReceipt.Output.Amount.String() {
		t.Fatal("rounding-policy mutation did not change the boundary result")
	}
	if evenReceipt.CanonicalDigest == upReceipt.CanonicalDigest {
		t.Fatal("policy mutation did not change receipt digest")
	}
	mutated := evenReceipt
	mutated.Output.Amount, err = values.NewDecimal("999.99", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutated.Digest(); err == nil {
		t.Fatal("mutated output passed receipt digest validation")
	}
}

func TestTaxInputRequiresPrivateContentBoundPin(t *testing.T) {
	p := policy(t, values.RoundingHalfEven)
	forged := calcpolicy.Input{Kind: calcpolicy.KindTax, Currency: "USD", Amount: decimal(t, "1.00", 2), TaxSnapshotDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, err := calcpolicy.NewInput(forged); !errors.Is(err, calcpolicy.ErrInvalidInput) {
		t.Fatalf("public constructor accepted forged tax pin: %v", err)
	}
	if _, err := calcpolicy.Calculate(p, forged); !errors.Is(err, calcpolicy.ErrInvalidInput) {
		t.Fatalf("calculation accepted forged tax pin: %v", err)
	}
	omitted := forged
	omitted.TaxSnapshotDigest = ""
	if _, err := calcpolicy.Calculate(p, omitted); !errors.Is(err, calcpolicy.ErrInvalidInput) {
		t.Fatalf("calculation accepted omitted tax pin: %v", err)
	}
	forgedSnapshot := taxprofile.PinnedTaxInputSnapshot{WorkerRef: "worker-1", EmploymentRef: "employment-1", PayGroupRef: "monthly", ProfileRevision: 1, ProfileDigest: forged.TaxSnapshotDigest, FormReleaseDigest: forged.TaxSnapshotDigest, RuleReleaseDigest: forged.TaxSnapshotDigest, Digest: forged.TaxSnapshotDigest}
	if _, err := calcpolicy.NewPinnedTaxInput(forgedSnapshot, forged); !errors.Is(err, taxprofile.ErrSnapshotInvalid) {
		t.Fatalf("pinned constructor accepted caller-built snapshot: %v", err)
	}
}
