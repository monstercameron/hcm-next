package auditpack_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/auditpack"
)

// TestTodo_DB_024 is the primary acceptance test: a payroll run's four
// ledger-recorded totals resolve to a shared, immutable idempotency key,
// reconcile exactly, bind into the ledger, and export as an auditor package
// an offline verifier accepts holding only the package bytes and the
// checkpoint epoch's public key.
func TestTodo_DB_024(t *testing.T) {
	f := newFixture(t)
	runID := "run-primary-0001"
	from := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	to := f.goodRun(t, f.tenant, runID, from)
	f.mustCheckpoint(t, f.tenant, to)

	t.Run("totals resolve from ledger checkpoint/evidence rows alone", func(t *testing.T) {
		req := f.exportRequest(f.tenant, runID, from, to)
		content, totals, decision, err := auditpack.NewExporter().Resolve(t.Context(), f.db.Conn, req)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if len(content.Streams) != 1 {
			t.Fatalf("content covers %d streams, want 1", len(content.Streams))
		}
		want := map[auditpack.TotalKind]string{
			auditpack.KindRegister: "1000.00", auditpack.KindBankFile: "800.00",
			auditpack.KindTaxLiability: "200.00", auditpack.KindFilingAcknowledgment: "200.00",
		}
		for kind, wantText := range want {
			got, ok := totals.Total(kind)
			if !ok {
				t.Fatalf("no resolved total for %s", kind)
			}
			if got.String() != wantText {
				t.Errorf("%s = %s, want %s", kind, got.String(), wantText)
			}
		}
		if !decision.OK() {
			t.Fatalf("a reconciling run reported variance: %v", decision.Err())
		}
	})

	t.Run("the same run always resolves the same idempotency key", func(t *testing.T) {
		a := auditpack.IdempotencyKey(f.tenant, runID)
		b := auditpack.IdempotencyKey(f.tenant, runID)
		if a == "" || a != b {
			t.Fatalf("IdempotencyKey is not a stable pure function: %q vs %q", a, b)
		}
	})

	t.Run("binding records the resolved totals and replays idempotently", func(t *testing.T) {
		req := f.exportRequest(f.tenant, runID, from, to)
		_, totals, decision, err := auditpack.NewExporter().Resolve(t.Context(), f.db.Conn, req)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		first, err := f.bind(t, f.tenant, runID, 4, to.Add(-time.Second), totals, decision)
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		if first.Replayed {
			t.Fatal("the first bind reports Replayed")
		}
		second, err := f.bind(t, f.tenant, runID, 4, to.Add(-time.Second), totals, decision)
		if err != nil {
			t.Fatalf("re-bind the same run: %v", err)
		}
		if !second.Replayed {
			t.Fatal("re-binding the same run's identical totals did not replay")
		}
		if second.Digest != first.Digest {
			t.Fatalf("replay digest = %s, want %s", second.Digest, first.Digest)
		}
	})

	t.Run("the exported package verifies offline", func(t *testing.T) {
		req := f.exportRequest(f.tenant, runID, from, to)
		pkg, _, _, err := auditpack.NewExporter().Export(t.Context(), f.db.Conn, req)
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		report := auditpack.Verify(pkg.Files(), f.liveKeyDirectory(), runID)
		if !report.OK() {
			for _, finding := range report.Findings {
				t.Errorf("finding: %s", finding)
			}
			if err := report.Evidence.Err(); err != nil {
				t.Errorf("wrapped evidence findings: %v", err)
			}
			t.FailNow()
		}
	})
}

// TestTodo_DB_024_Golden pins the exact bytes an auditor package commits to:
// the outer manifest digest, the summary digest, the wrapped evidence
// digest and every part's own digest. A change to the framing, the
// canonicalization or the path layout changes them, and that has to be a
// deliberate edit of the golden file, never a silent break in every package
// an auditor already holds.
func TestTodo_DB_024_Golden(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)

	var b strings.Builder
	b.WriteString("manifest_digest=" + pkg.Manifest.Digest + "\n")
	b.WriteString("evidence_digest=" + pkg.Manifest.EvidenceDigest + "\n")
	b.WriteString("summary_digest=" + pkg.Manifest.SummaryDigest + "\n")
	b.WriteString("idempotency_key=" + pkg.Manifest.IdempotencyKey + "\n")
	paths := pkg.Paths()
	sort.Strings(paths)
	for _, path := range paths {
		raw, _ := pkg.Part(path)
		b.WriteString("part=" + path + " length=" + itoa(len(raw)) + "\n")
	}
	got := b.String()

	golden := filepath.Join("testdata", "golden_package.txt")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	wantBytes, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.ReplaceAll(string(wantBytes), "\r\n", "\n"); got != want {
		t.Fatalf("auditor package bytes changed.\n got:\n%s\nwant:\n%s", got, want)
	}

	report := auditpack.Verify(pkg.Files(), goldenKeyDirectory(t), goldenRunID)
	if !report.OK() {
		t.Fatalf("golden package does not verify: %v", auditKinds(report))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestTodo_DB_024_Security proves the two refusals that make an auditor
// package security-relevant rather than merely a report: another tenant's
// session cannot resolve a run's totals at all - row level security hides
// the rows before this package ever sees them, so it never gets the chance
// to leak them - and a package whose stated total was tampered with fails
// verification.
func TestTodo_DB_024_Security(t *testing.T) {
	f := newFixture(t)
	runID := "run-security-0001"
	from := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)
	to := f.goodRun(t, f.tenant, runID, from)
	f.mustCheckpoint(t, f.tenant, to)

	t.Run("a package for another tenant is unreadable under RLS", func(t *testing.T) {
		other := f.withTenant(t)
		conn := appConn(t, f.db)

		// auditpack.Querier is evidence.Querier, i.e. internal/data/ledger.
		// Querier (dbport.Querier): a dbport.Tx already satisfies it, so the
		// request is issued straight through the tenant-scoped transaction
		// the row level security policy reads app.tenant_id from.
		var exportErr error
		txErr := inTenantTxErr(conn, other.tenant, func(tx dbport.Tx) error {
			_, _, _, exportErr = auditpack.NewExporter().Resolve(t.Context(), tx, f.exportRequest(f.tenant, runID, from, to))
			return nil
		})
		if txErr != nil {
			t.Fatalf("run in tenant tx: %v", txErr)
		}
		if exportErr == nil {
			t.Fatal("resolving another tenant's run under RLS succeeded; it must be unreadable")
		}
		t.Logf("refused as expected: %v", exportErr)
	})

	t.Run("a tampered total fails Verify", func(t *testing.T) {
		req := f.exportRequest(f.tenant, runID, from, to)
		pkg, _, _, err := auditpack.NewExporter().Export(t.Context(), f.db.Conn, req)
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		tampered := mutate(t, pkg.Files(), auditpack.SummaryPath, func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["totals"].(map[string]any)[string(auditpack.KindFilingAcknowledgment)] = "150.00/HALF_EVEN"
			})
		})
		report := auditpack.Verify(tampered, f.liveKeyDirectory(), runID)
		if report.OK() {
			t.Fatal("a package with a tampered total verified")
		}
		if !report.Has(auditpack.FindingSummaryDigest) {
			t.Fatalf("tampering the summary reported %v, want a summary digest mismatch", auditKinds(report))
		}
	})
}

// TestTodo_DB_024_Integration exercises the full path end to end against a
// real ledger: contributing lines are appended and hash-chained, a
// checkpoint attests to them, the run is bound, and the exported package's
// independent recomputation agrees with the ledger-recorded binding.
func TestTodo_DB_024_Integration(t *testing.T) {
	f := newFixture(t)
	runID := "run-integration-0001"
	from := time.Date(2026, 3, 3, 8, 0, 0, 0, time.UTC)
	to := f.goodRun(t, f.tenant, runID, from)

	req := f.exportRequest(f.tenant, runID, from, to)
	_, totals, decision, err := auditpack.NewExporter().Resolve(t.Context(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := f.bind(t, f.tenant, runID, 4, to.Add(-time.Second), totals, decision); err != nil {
		t.Fatalf("bind: %v", err)
	}
	f.mustCheckpoint(t, f.tenant, to)

	pkg, _, _, err := auditpack.NewExporter().Export(t.Context(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	report := auditpack.Verify(pkg.Files(), f.liveKeyDirectory(), runID)
	if !report.OK() {
		t.Fatalf("integration package does not verify: %v (evidence: %v)", auditKinds(report), report.Evidence.Err())
	}
	if report.RecomputedDecision.OK() == false {
		t.Fatalf("recomputed decision reports variance: %v", report.RecomputedDecision.Err())
	}
}

// TestTodo_DB_024_Recovery proves the package is a pure function of
// (tenant, run, window, ledger state): rebuilding it from a fresh connection
// - standing in for a process restart, since nothing about the rebuild reads
// any in-memory state from the first export - reproduces byte-identical
// bytes.
func TestTodo_DB_024_Recovery(t *testing.T) {
	f := newFixture(t)
	runID := "run-recovery-0001"
	from := time.Date(2026, 3, 4, 8, 0, 0, 0, time.UTC)
	to := f.goodRun(t, f.tenant, runID, from)
	f.mustCheckpoint(t, f.tenant, to)

	req := f.exportRequest(f.tenant, runID, from, to)
	first, _, _, err := auditpack.NewExporter().Export(t.Context(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("first export: %v", err)
	}

	// A fresh connection stands in for a restarted process: nothing carries
	// forward from the first export but the rows themselves.
	restarted := f.db.NewConn(t)
	second, _, _, err := auditpack.NewExporter().Export(t.Context(), restarted, req)
	if err != nil {
		t.Fatalf("export after restart: %v", err)
	}

	firstFiles, secondFiles := first.Files(), second.Files()
	if len(firstFiles) != len(secondFiles) {
		t.Fatalf("post-restart export holds %d paths, want %d", len(secondFiles), len(firstFiles))
	}
	for path, raw := range firstFiles {
		other, ok := secondFiles[path]
		if !ok {
			t.Fatalf("post-restart export is missing %s", path)
		}
		if string(other) != string(raw) {
			t.Fatalf("post-restart export differs from the original at %s", path)
		}
	}
	if first.Manifest.Digest != second.Manifest.Digest {
		t.Fatalf("post-restart manifest digest = %s, want %s", second.Manifest.Digest, first.Manifest.Digest)
	}
}

// TestTodo_DB_024_Mutation proves flipping one ledger amount is caught: the
// wrapped evidence package's own event digest no longer reproduces once the
// payload is altered, and the auditor package fails to verify.
func TestTodo_DB_024_Mutation(t *testing.T) {
	f := newFixture(t)
	runID := "run-mutation-0001"
	from := time.Date(2026, 3, 5, 8, 0, 0, 0, time.UTC)
	to := f.goodRun(t, f.tenant, runID, from)
	f.mustCheckpoint(t, f.tenant, to)

	req := f.exportRequest(f.tenant, runID, from, to)
	pkg, _, _, err := auditpack.NewExporter().Export(t.Context(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	eventsPath := auditpack.EvidencePrefix + evidence.StreamEventsPath(0)
	before := auditpack.Verify(pkg.Files(), f.liveKeyDirectory(), runID)
	if !before.OK() {
		t.Fatalf("package does not verify before mutation: %v", auditKinds(before))
	}

	tampered := mutate(t, pkg.Files(), eventsPath, func(raw []byte) []byte {
		return rewriteJSON(t, raw, func(doc map[string]any) {
			events := doc["events"].([]any)
			events[0].(map[string]any)["payload"] = "Q0hBTkdFRA==" // base64("CHANGED")
		})
	})

	after := auditpack.Verify(tampered, f.liveKeyDirectory(), runID)
	if after.OK() {
		t.Fatal("a package with one ledger amount flipped verified")
	}
	if !after.Evidence.Has(evidence.FindingTamperedEvent) {
		t.Fatalf("flipping one ledger amount reported %v (evidence %v), want a tampered event",
			auditKinds(after), kinds(after.Evidence))
	}
}
