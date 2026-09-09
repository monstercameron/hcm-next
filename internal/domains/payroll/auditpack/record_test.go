package auditpack_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/auditpack"
)

func TestAppendLineHashChainsIntoTheRunStream(t *testing.T) {
	f := newFixture(t)
	runID := "run-append-1"
	f.ensureStream(t, f.tenant, runID)
	at := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	receipt := f.appendLine(t, f.tenant, runID, auditpack.KindRegister, "1000.00", 0, at)
	if receipt.Sequence != 1 {
		t.Fatalf("first appended line has sequence %d, want 1", receipt.Sequence)
	}
	if receipt.StreamKey != auditpack.StreamKey(runID) {
		t.Fatalf("appended line stream = %s, want %s", receipt.StreamKey, auditpack.StreamKey(runID))
	}

	second := f.appendLine(t, f.tenant, runID, auditpack.KindBankFile, "800.00", 1, at.Add(time.Minute))
	if second.Sequence != 2 {
		t.Fatalf("second appended line has sequence %d, want 2", second.Sequence)
	}
}

func TestBindRefusesAnUnexplainedVariance(t *testing.T) {
	f := newFixture(t)
	runID := "run-bind-refuse-1"
	from := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	f.ensureStream(t, f.tenant, runID)
	f.appendLine(t, f.tenant, runID, auditpack.KindRegister, "1000.00", 0, from)
	f.appendLine(t, f.tenant, runID, auditpack.KindBankFile, "800.00", 1, from.Add(time.Minute))
	f.appendLine(t, f.tenant, runID, auditpack.KindTaxLiability, "200.00", 2, from.Add(2*time.Minute))
	// The filing acknowledgment disagrees with the tax liability: an
	// unexplained variance.
	to := from.Add(4 * time.Minute)
	f.appendLine(t, f.tenant, runID, auditpack.KindFilingAcknowledgment, "150.00", 3, from.Add(3*time.Minute))

	req := f.exportRequest(f.tenant, runID, from, to)
	_, totals, decision, err := auditpack.NewExporter().Resolve(t.Context(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if decision.OK() {
		t.Fatal("a run with mismatched tax liability and filing acknowledgment reported no variance")
	}

	_, bindErr := f.bind(t, f.tenant, runID, 4, to, totals, decision)
	if bindErr == nil {
		t.Fatal("Bind recorded a reconciliation over an unexplained variance")
	}
	if _, ok := bindErr.(auditpack.ErrVarianceUnexplained); !ok {
		t.Fatalf("Bind refusal = %v, want ErrVarianceUnexplained", bindErr)
	}
}

func TestBindRefusesARunThatDoesNotMatchTheRequest(t *testing.T) {
	f := newFixture(t)
	totals := auditpack.RunTotals{Tenant: uuid.New(), RunID: "someone-elses-run"}
	_, err := f.bind(t, f.tenant, "run-mismatch-1", 0, time.Now().UTC(), totals, auditpack.Decision{})
	if err == nil {
		t.Fatal("Bind accepted totals resolved for a different tenant/run")
	}
}
