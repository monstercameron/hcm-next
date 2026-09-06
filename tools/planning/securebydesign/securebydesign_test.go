package securebydesign_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
	"github.com/monstercameron/hcm-next/internal/trust/stepup"
	"github.com/monstercameron/hcm-next/tools/planning/securebydesign"
)

func repoRoot(t testing.TB) string {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for current := working; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("could not find repository root")
		}
	}
}

func fixturePath(t testing.TB) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "tools", "planning", "securebydesign", "testdata", "security-evidence.yaml")
}

func recordFixture(t testing.TB) securebydesign.Record {
	t.Helper()
	fixture, err := securebydesign.LoadFixture(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	record, err := securebydesign.NewRecordFromFixture(1, fixture, securebydesign.DefaultGoals(), []securebydesign.Exception{
		{
			GoalNumber: 3,
			Reason:     "legacy parser migration is completing under compensating review",
			Owner:      "product-security",
			ApprovedBy: "security-governance",
			StartsOn:   "2026-09-01",
			ExpiresOn:  "2026-12-31",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func testPrincipal(t testing.TB) *trust.Principal {
	t.Helper()
	seed := sha256.Sum256([]byte("securebydesign-test-credential"))
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               values.TenantId("acme"),
		Subject:              "securebydesign-test-subject",
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                []string{string(authz.RoleWorkerSelf)},
		Purposes:             []string{authz.PurposeSelfService},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceLow,
		SessionRef:           "securebydesign-test-session",
		IssuedAt:             time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:            time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
		CredentialDigest:     "sha256:" + hex.EncodeToString(seed[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

// TestTodo_SECARCH_011 is the primary acceptance test for the complete
// secure-by-design revision: all seven goals, public disclosure terms,
// computed release metrics, and a digest-bearing revision are present.
func TestTodo_SECARCH_011(t *testing.T) {
	record := recordFixture(t)
	if err := record.Validate(); err != nil {
		t.Fatalf("record validation: %v", err)
	}
	if err := record.VerifyDigest(); err != nil {
		t.Fatalf("record digest: %v", err)
	}
	if len(record.Goals) != 7 || len(record.Trends) != 2 || len(record.Exceptions) != 1 {
		t.Fatalf("record shape = %d goals, %d trends, %d exceptions", len(record.Goals), len(record.Trends), len(record.Exceptions))
	}
	if record.Trends[0].FindingsOpened != 3 || record.Trends[0].FindingsClosed != 2 || record.Trends[0].MedianDays != 5 {
		t.Fatalf("first trend = %+v", record.Trends[0])
	}
	if record.Trends[1].FindingsOpened != 2 || record.Trends[1].FindingsClosed != 1 || record.Trends[1].MedianDays != 4 {
		t.Fatalf("second trend = %+v", record.Trends[1])
	}
}

// TestTodo_SECARCH_011_Golden pins the canonical revision representation.
func TestTodo_SECARCH_011_Golden(t *testing.T) {
	record := recordFixture(t)
	want, err := os.ReadFile(filepath.Join(filepath.Dir(fixturePath(t)), "record.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(record.Canonical()); strings.TrimSpace(string(want)) != got {
		t.Fatalf("canonical record changed:\n got: %s\nwant: %s", got, want)
	}
}

// TestTodo_SECARCH_011_Security proves field-specific refusals, exception
// expiry, and that audit explanations do not disclose protected values.
func TestTodo_SECARCH_011_Security(t *testing.T) {
	record := recordFixture(t)
	if strings.Contains(record.Explain(), "security@example.invalid") || strings.Contains(record.Explain(), "product-security") {
		t.Fatalf("Explain disclosed policy identifiers: %q", record.Explain())
	}
	if err := record.ValidateAt(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expired exception was accepted")
	}
	bad := record
	bad.DisclosurePolicy.Contact = ""
	var invalid securebydesign.InvalidRecord
	if err := bad.Validate(); !errors.As(err, &invalid) {
		t.Fatalf("Validate() error = %T %v, want InvalidRecord", err, err)
	}
	foundField := false
	for _, refusal := range invalid.Refusals {
		if refusal.Field == "vulnerability_disclosure_policy.contact" {
			foundField = true
		}
	}
	if !foundField {
		t.Fatalf("contact refusal did not name its field: %+v", invalid.Refusals)
	}
}

// TestTodo_SECARCH_011_Integration wires the real deny-by-default AuthZ,
// step-up policy, and evidence digest collaborators together in one pure
// package test. No I/O port or fake is involved.
func TestTodo_SECARCH_011_Integration(t *testing.T) {
	principal := testPrincipal(t)
	decision, err := authz.ResolveFields(principal, authz.PurposeAuditReview, []authz.FieldID{authz.FieldBankAccountNumber}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ruling := decision.Rulings[authz.FieldBankAccountNumber]; ruling.Effect != authz.EffectDenied {
		t.Fatalf("bank field ruling = %+v, want deny-by-default", ruling)
	}
	requirement, rule, required := stepup.DefaultObligationPolicy().Select(stepup.ActionApprove, stepup.PurposeHCMOperations, stepup.RiskCritical)
	if !required || rule == "" || requirement.MinAssurance != trust.AssuranceHigh {
		t.Fatalf("step-up selection = %+v, %q, %t", requirement, rule, required)
	}
	digest, err := evidence.ComputeDigest(evidence.Manifest{SchemaVersion: evidence.SchemaVersion, LayoutVersion: evidence.LayoutVersion, CoversFrom: time.Unix(0, 0).UTC(), CoversTo: time.Unix(1, 0).UTC()})
	if err != nil || digest == "" {
		t.Fatalf("audit evidence digest = %q, error = %v", digest, err)
	}
}

// TestTodo_SECARCH_011_Conformance parses the repository's test files and
// rejects any stale goal binding.
func TestTodo_SECARCH_011_Conformance(t *testing.T) {
	record := recordFixture(t)
	names, err := securebydesign.ScanTestNames(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if refusals := record.Conformance(names); len(refusals) != 0 {
		t.Fatalf("goal test conformance refusals: %+v", refusals)
	}
}

// TestTodo_SECARCH_011_Mutation proves the digest gate catches a changed
// revision and cannot be bypassed by changing only the recorded digest.
func TestTodo_SECARCH_011_Mutation(t *testing.T) {
	record := recordFixture(t)
	record.Trends[0].MedianDays++
	if err := record.VerifyDigest(); err == nil {
		t.Fatal("mutated trend passed digest verification")
	}
	record = recordFixture(t)
	record.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := record.VerifyDigest(); err == nil {
		t.Fatal("forged digest passed verification")
	}
}

func TestSecureByDesignDefaultAuthZ(t *testing.T) {
	principal := testPrincipal(t)
	decision, err := authz.ResolveFields(principal, authz.PurposeAuditReview, []authz.FieldID{authz.FieldBankAccountNumber}, nil)
	if err != nil || decision.Rulings[authz.FieldBankAccountNumber].Effect != authz.EffectDenied {
		t.Fatalf("default AuthZ is not deny-by-default: decision=%+v error=%v", decision, err)
	}
}

func TestSecureByDesignMFAAndStepUpAvailable(t *testing.T) {
	requirement, rule, required := stepup.DefaultObligationPolicy().Select(stepup.ActionExport, stepup.PurposeHCMOperations, stepup.RiskElevated)
	if !required || rule == "" || requirement.MinAssurance == trust.AssuranceUnspecified {
		t.Fatalf("default MFA/step-up selection = %+v, %q, %t", requirement, rule, required)
	}
}

func TestSecureByDesignNoDefaultPasswords(t *testing.T) {
	record := recordFixture(t)
	if strings.Contains(strings.ToLower(string(record.Canonical())), `"password":`) {
		t.Fatal("secure-by-design record contains a default-password setting")
	}
}

func TestSecureByDesignSecurityPatchEvidence(t *testing.T) {
	record := recordFixture(t)
	if len(record.Trends) == 0 || record.Trends[0].FindingsOpened == 0 {
		t.Fatal("no release trend evidence for security findings")
	}
}

func TestSecureByDesignDisclosurePolicy(t *testing.T) {
	policy, err := securebydesign.LoadDisclosurePolicyYAML(fixturePath(t))
	if err != nil || policy.Contact == "" || len(policy.Scope) < 3 || policy.Timelines.InitialTriageDays != 5 {
		t.Fatalf("disclosure policy = %+v, error = %v", policy, err)
	}
}

func TestSecureByDesignCVETransparency(t *testing.T) {
	record := recordFixture(t)
	if err := record.VerifyDigest(); err != nil {
		t.Fatal(err)
	}
}

func TestSecureByDesignAuditEvidenceOn(t *testing.T) {
	if evidence.DigestAlgorithm != "sha256" || evidence.DigestProfile == "" || evidence.NewExporter() == nil {
		t.Fatal("ledger evidence capability is not available")
	}
}
