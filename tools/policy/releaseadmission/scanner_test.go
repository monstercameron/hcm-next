package releaseadmission_test

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/releaseadmission"
)

func scannerStatement(t *testing.T) (provenance.Statement, string) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 21
	private := ed25519.NewKeyFromSeed(seed)
	statement := provenance.Statement{
		SchemaVersion: provenance.SchemaVersion,
		PredicateType: provenance.PredicateType,
		GeneratedAt:   "2026-09-05T00:00:00Z",
		Subjects:      []provenance.Subject{{Name: "hcmnext", SHA256: strings.Repeat("a", 64)}},
		Builder:       provenance.Builder{ID: provenance.BuilderID},
		Source:        provenance.SourceRef{Repository: provenance.RootModulePath, Ref: "main", Commit: "commit"},
		BuildConfig:   provenance.BuildConfig{GoVersion: "go1.26.3", GOOS: "windows", GOARCH: "arm64", ConfigDigest: "config"},
		SBOM:          provenance.SBOMReference{Path: "sbom.json", SHA256: strings.Repeat("b", 64)},
	}
	signed, err := provenance.SignStatement(private, statement, "test")
	if err != nil {
		t.Fatalf("SignStatement: %v", err)
	}
	return signed, signed.Signature.PublicKey
}

func scannerRecords(t *testing.T) []releaseadmission.ScannerEvidence {
	t.Helper()
	records := make([]releaseadmission.ScannerEvidence, 0, 4)
	for _, kind := range releaseadmission.RequiredScannerKinds() {
		record, err := releaseadmission.NewScannerEvidence(kind, "scanner", "1.2.3", strings.Repeat("c", 64), nil)
		if err != nil {
			t.Fatalf("NewScannerEvidence(%s): %v", kind, err)
		}
		records = append(records, record)
	}
	return records
}

func scannerPolicy(statement provenance.Statement, publicKey string, records []releaseadmission.ScannerEvidence) releaseadmission.Policy {
	return releaseadmission.Policy{
		TrustedPublicKeys:      map[string]bool{publicKey: true},
		PinnedPublicKeys:       []string{publicKey},
		AllowedBuilders:        map[string]bool{provenance.BuilderID: true},
		SBOMDigest:             statement.SBOM.SHA256,
		RequireScannerEvidence: true,
		ScannerEvidence:        records,
	}
}

func TestTodo_SECARCH_007(t *testing.T) {
	statement, publicKey := scannerStatement(t)
	decision := releaseadmission.Evaluate(scannerPolicy(statement, publicKey, scannerRecords(t)), statement)
	if !decision.Admitted || decision.Status != releaseadmission.StatusAdmit {
		t.Fatalf("decision = %+v, want ADMIT", decision)
	}
	if decision.ScannerEvidenceDigest == "" || decision.Digest == "" {
		t.Fatalf("decision lacks scanner evidence digest: %+v", decision)
	}
}

func TestTodo_SECARCH_007_Golden(t *testing.T) {
	record, err := releaseadmission.NewScannerEvidence(releaseadmission.ScannerSAST, "semgrep", "1.0.0", strings.Repeat("d", 64), []releaseadmission.ScannerFinding{{ID: "rule-1", Severity: releaseadmission.SeverityLow, Triage: releaseadmission.TriageResolved}})
	if err != nil {
		t.Fatalf("NewScannerEvidence: %v", err)
	}
	if err := record.Validate(time.Now().UTC()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if record.Digest == "" || !strings.Contains(record.Explain(), "scanner evidence SAST") {
		t.Fatalf("record = %+v, explain = %q", record, record.Explain())
	}
	if strings.Contains(record.Explain(), "rule-1") {
		t.Fatalf("Explain leaked finding identifier: %q", record.Explain())
	}
}

func TestTodo_SECARCH_007_Security(t *testing.T) {
	statement, publicKey := scannerStatement(t)
	records := scannerRecords(t)
	records[0], _ = releaseadmission.NewScannerEvidence(releaseadmission.ScannerSAST, "scanner", "1.2.3", strings.Repeat("c", 64), []releaseadmission.ScannerFinding{{ID: "secret-token-raw-value", Severity: releaseadmission.SeverityHigh, Triage: releaseadmission.TriageOpen}})
	decision := releaseadmission.Evaluate(scannerPolicy(statement, publicKey, records), statement)
	if decision.Admitted || !strings.Contains(strings.Join(decision.Reasons, "\n"), "scanner_evidence.sast.findings") {
		t.Fatalf("decision = %+v, want typed scanner finding refusal", decision)
	}
	if strings.Contains(decision.Explain(), "secret-token-raw-value") {
		t.Fatalf("decision Explain leaked finding identifier: %q", decision.Explain())
	}
}

func TestTodo_SECARCH_007_Integration(t *testing.T) {
	statement, publicKey := scannerStatement(t)
	records := scannerRecords(t)
	records[1], _ = releaseadmission.NewScannerEvidence(releaseadmission.ScannerSecret, "secret-scanner", "4.5.6", strings.Repeat("e", 64), []releaseadmission.ScannerFinding{{ID: "finding-1", Severity: releaseadmission.SeverityCritical, Triage: releaseadmission.TriageExcepted, Exception: &releaseadmission.ScannerException{Reviewer: "security-reviewer", ExpiresAt: time.Now().UTC().Add(time.Hour)}}})
	decision := releaseadmission.Evaluate(scannerPolicy(statement, publicKey, records), statement)
	if !decision.Admitted {
		t.Fatalf("reviewed time-bounded exception did not admit: %+v", decision)
	}
}

func TestTodo_SECARCH_007_Mutation(t *testing.T) {
	records := scannerRecords(t)
	records[0].Digest = strings.Repeat("f", 64)
	statement, publicKey := scannerStatement(t)
	decision := releaseadmission.Evaluate(scannerPolicy(statement, publicKey, records), statement)
	if decision.Admitted || !strings.Contains(strings.Join(decision.Reasons, "\n"), "scanner_evidence.sast") {
		t.Fatalf("tampered scanner record decision = %+v, want rejection", decision)
	}
}
