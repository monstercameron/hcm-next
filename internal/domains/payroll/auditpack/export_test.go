package auditpack_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/payroll/auditpack"
)

// TestExportRefusesAWindowNoSignedEpochCovers proves the export path never
// builds a package over ledger state no checkpoint has attested to yet: a
// window running past the last signed epoch is refused by the wrapped
// evidence read itself.
func TestExportRefusesAWindowNoSignedEpochCovers(t *testing.T) {
	f := newFixture(t)
	runID := "run-export-nocheckpoint-1"
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := f.goodRun(t, f.tenant, runID, from)
	f.mustCheckpoint(t, f.tenant, to)

	// The checkpoint stops at to; a window running past it is a report, not
	// evidence.
	req := f.exportRequest(f.tenant, runID, from, to.Add(time.Hour))
	_, _, _, err := auditpack.NewExporter().Export(t.Context(), f.db.Conn, req)
	if err == nil {
		t.Fatal("exporting over a window past the last signed epoch succeeded")
	}
	if _, ok := errors.AsType[evidence.ErrIncompleteEpochCoverage](err); !ok {
		t.Fatalf("export over an unattested window = %v, want evidence.ErrIncompleteEpochCoverage", err)
	}
}

// TestExportResolveSeparatesReadingFromBuilding proves Resolve lets a caller
// inspect a run's totals and variance decision without ever building package
// bytes, which matters for a caller that wants to decide whether a release
// should proceed before paying for the export.
func TestExportResolveSeparatesReadingFromBuilding(t *testing.T) {
	f := newFixture(t)
	runID := "run-export-resolve-1"
	from := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	to := f.goodRun(t, f.tenant, runID, from)
	f.mustCheckpoint(t, f.tenant, to)

	req := f.exportRequest(f.tenant, runID, from, to)
	content, totals, decision, err := auditpack.NewExporter().Resolve(t.Context(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !decision.OK() {
		t.Fatalf("resolve reported variance for a reconciling run: %v", decision.Err())
	}
	// Resolve alone must not have produced any package bytes; Build is a
	// distinct, explicit step over what Resolve already read.
	pkg, err := auditpack.Build(content, totals, decision)
	if err != nil {
		t.Fatalf("build over resolve's own content: %v", err)
	}
	if pkg.Manifest.RunID != runID {
		t.Fatalf("built package names run %s, want %s", pkg.Manifest.RunID, runID)
	}
}

// TestExportRefusesAnUnreconcilingRun proves the release gate at the export
// boundary itself: Export never returns a package for a run whose totals do
// not reconcile, even though the wrapped ledger read and resolution both
// succeed.
func TestExportRefusesAnUnreconcilingRun(t *testing.T) {
	f := newFixture(t)
	runID := "run-export-variance-1"
	from := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	f.ensureStream(t, f.tenant, runID)
	f.appendLine(t, f.tenant, runID, auditpack.KindRegister, "1000.00", 0, from)
	f.appendLine(t, f.tenant, runID, auditpack.KindBankFile, "800.00", 1, from.Add(time.Minute))
	f.appendLine(t, f.tenant, runID, auditpack.KindTaxLiability, "200.00", 2, from.Add(2*time.Minute))
	f.appendLine(t, f.tenant, runID, auditpack.KindFilingAcknowledgment, "1.00", 3, from.Add(3*time.Minute))
	to := from.Add(4 * time.Minute)
	f.mustCheckpoint(t, f.tenant, to)

	req := f.exportRequest(f.tenant, runID, from, to)
	_, _, _, err := auditpack.NewExporter().Export(t.Context(), f.db.Conn, req)
	if err == nil {
		t.Fatal("Export produced a package for a run with an unexplained variance")
	}
	if _, ok := err.(auditpack.ErrVarianceUnexplained); !ok {
		t.Fatalf("Export refusal = %v, want ErrVarianceUnexplained", err)
	}
}
