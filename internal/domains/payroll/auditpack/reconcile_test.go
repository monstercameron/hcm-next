package auditpack_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/payroll/auditpack"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func totalsOf(t *testing.T, register, bankFile, taxLiability, filingAck string) auditpack.RunTotals {
	t.Helper()
	return auditpack.RunTotals{
		RunID: "run-1",
		Totals: map[auditpack.TotalKind]values.Decimal{
			auditpack.KindRegister:             decimal(t, register),
			auditpack.KindBankFile:             decimal(t, bankFile),
			auditpack.KindTaxLiability:         decimal(t, taxLiability),
			auditpack.KindFilingAcknowledgment: decimal(t, filingAck),
		},
	}
}

func TestReconcileAcceptsAConsistentRun(t *testing.T) {
	t.Parallel()
	decision, err := auditpack.Reconcile(totalsOf(t, "1000.00", "800.00", "200.00", "200.00"))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !decision.OK() {
		t.Fatalf("a consistent run reported variance: %v", decision.Err())
	}
	if len(decision.Pairs) != 2 {
		t.Fatalf("Reconcile checked %d pairs, want 2", len(decision.Pairs))
	}
}

func TestReconcileNamesTheRegisterVsDisbursedPlusWithheldVariance(t *testing.T) {
	t.Parallel()
	// register 1000 should equal 800 + 200 = 1000; make it 1050 instead.
	decision, err := auditpack.Reconcile(totalsOf(t, "1050.00", "800.00", "200.00", "200.00"))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if decision.OK() {
		t.Fatal("an inconsistent run reported no variance")
	}
	variance, ok := asVariance(decision.Err())
	if !ok {
		t.Fatalf("Err() = %v, want ErrVarianceUnexplained", decision.Err())
	}
	if variance.Pair != "REGISTER_VS_BANK_FILE_PLUS_TAX_LIABILITY" {
		t.Fatalf("variance names pair %q, want REGISTER_VS_BANK_FILE_PLUS_TAX_LIABILITY", variance.Pair)
	}
	if variance.Difference != "50.00" {
		t.Fatalf("variance difference = %s, want 50.00", variance.Difference)
	}
}

func TestReconcileNamesTheTaxLiabilityVsFilingAcknowledgmentVariance(t *testing.T) {
	t.Parallel()
	decision, err := auditpack.Reconcile(totalsOf(t, "1000.00", "800.00", "200.00", "150.00"))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if decision.OK() {
		t.Fatal("an inconsistent run reported no variance")
	}
	variance, ok := asVariance(decision.Err())
	if !ok {
		t.Fatalf("Err() = %v, want ErrVarianceUnexplained", decision.Err())
	}
	if variance.Pair != "TAX_LIABILITY_VS_FILING_ACKNOWLEDGMENT" {
		t.Fatalf("variance names pair %q, want TAX_LIABILITY_VS_FILING_ACKNOWLEDGMENT", variance.Pair)
	}
	if variance.Difference != "50.00" {
		t.Fatalf("variance difference = %s, want 50.00", variance.Difference)
	}
}

func TestReconcileRefusesAMissingTotal(t *testing.T) {
	t.Parallel()
	incomplete := auditpack.RunTotals{RunID: "run-1", Totals: map[auditpack.TotalKind]values.Decimal{
		auditpack.KindRegister: decimal(t, "1.00"),
	}}
	_, err := auditpack.Reconcile(incomplete)
	if _, ok := err.(auditpack.ErrMissingTotal); !ok {
		t.Fatalf("Reconcile over an incomplete run = %v, want ErrMissingTotal", err)
	}
}

func TestReconcileToleratesDifferentDeclaredScales(t *testing.T) {
	t.Parallel()
	wide, err := values.NewDecimal("200.000", 3, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	totals := auditpack.RunTotals{RunID: "run-1", Totals: map[auditpack.TotalKind]values.Decimal{
		auditpack.KindRegister:             decimal(t, "1000.00"),
		auditpack.KindBankFile:             decimal(t, "800.00"),
		auditpack.KindTaxLiability:         wide,
		auditpack.KindFilingAcknowledgment: decimal(t, "200.00"),
	}}
	decision, err := auditpack.Reconcile(totals)
	if err != nil {
		t.Fatalf("reconcile across declared scales: %v", err)
	}
	if !decision.OK() {
		t.Fatalf("numerically equal totals at different scales reported variance: %v", decision.Err())
	}
}
