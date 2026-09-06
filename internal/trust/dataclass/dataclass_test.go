package dataclass

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

func testCustody(t *testing.T) (*Transformer, custody.Context, custody.Handle) {
	t.Helper()
	key := custody.Handle{ID: "lower-environment-key", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	ctx := custody.Context{RequestContext: custody.RequestContext{
		Workload: "fixture-copy", Tenant: "tenant-a", Region: "us-east",
		Purpose: "lower_environment_copy", Destination: "sandbox",
	}}
	provider := custody.NewInMemoryFake(func() time.Time { return time.Unix(100, 0).UTC() })
	if err := provider.Register(key); err != nil {
		t.Fatalf("Register custody key: %v", err)
	}
	transformer, err := New(provider, key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return transformer, ctx, key
}

func TestTodo_SECARCH_009(t *testing.T) {
	policies := Registry()
	if len(policies) != len(authz.FieldRegistry) {
		t.Fatalf("registry fields = %d, want TRUST-010 fields = %d", len(policies), len(authz.FieldRegistry))
	}
	for _, policy := range policies {
		if _, err := ResolveField(policy.Field); err != nil {
			t.Fatalf("ResolveField(%q): %v", policy.Field, err)
		}
		if policy.Sensitivity.restricted() && !policy.LowerEnvironment.Valid() {
			t.Errorf("restricted field %q has invalid lower rule %q", policy.Field, policy.LowerEnvironment)
		}
	}

	transformer, ctx, _ := testCustody(t)
	first, err := transformer.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldBaseSalary, Value: []byte("90000.00"), Environment: EnvironmentSandbox})
	if err != nil {
		t.Fatalf("PrepareCopy: %v", err)
	}
	second, err := transformer.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldBaseSalary, Value: []byte("90000.00"), Environment: EnvironmentSandbox})
	if err != nil {
		t.Fatalf("second PrepareCopy: %v", err)
	}
	if string(first.Value) == "90000.00" || first.Digest == "" || first.Digest != second.Digest {
		t.Fatalf("fixture is not deterministic and sanitized: %#v %#v", first, second)
	}
	if first.PolicyReceipt.FixtureDigest != first.Digest {
		t.Fatal("policy receipt does not record the fixture digest")
	}
	if err := first.Verify(); err != nil {
		t.Fatalf("fixture Verify: %v", err)
	}
	if _, err := transformer.Reverse(ctx, first); !errors.Is(err, ErrIrreversible) {
		t.Fatalf("Reverse error = %v, want ErrIrreversible", err)
	}
}

func TestTodo_SECARCH_009_Golden(t *testing.T) {
	policies := Registry()
	seen := map[GovernmentDataClass]bool{}
	for _, policy := range policies {
		seen[policy.GovernmentClass] = true
		if policy.Controls.Storage == "" || policy.Controls.Key == "" || policy.Controls.Access == "" || policy.Controls.Disclosure == "" || policy.Controls.Personnel == "" || policy.Controls.Retention == "" {
			t.Fatalf("field %q has incomplete class policy: %#v", policy.Field, policy.Controls)
		}
	}
	for _, class := range []GovernmentDataClass{GovernmentClassNone, GovernmentClassFTI, GovernmentClassCJI, GovernmentClassCUI, GovernmentClassACA} {
		if !seen[class] {
			t.Errorf("registry does not exercise government class %s", class)
		}
	}
}

func TestTodo_SECARCH_009_Security(t *testing.T) {
	transformer, ctx, key := testCustody(t)
	fixture, err := transformer.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldBankAccountNumber, Value: []byte("123456789"), Environment: EnvironmentStaging})
	if err != nil {
		t.Fatalf("PrepareCopy: %v", err)
	}
	if len(fixture.Value) != 0 || strings.Contains(string(fixture.Value), "123456789") {
		t.Fatalf("excluded bank value leaked into fixture: %q", fixture.Value)
	}
	if fixture.SourceDigest == "123456789" || fixture.PolicyReceipt.Field != authz.FieldBankAccountNumber {
		t.Fatal("source value or field receipt is unsafe")
	}
	wrongKey := key
	wrongKey.ID = "not-the-production-key"
	if _, err := transformer.PrepareCopy(PrepareCopyRequest{Context: custody.Context{RequestContext: custody.RequestContext{Workload: "fixture-copy", Tenant: "other-tenant", Region: "us-east", Purpose: "lower_environment_copy", Destination: "sandbox"}}, Field: authz.FieldCaseNotes, Value: []byte("sensitive case"), Environment: EnvironmentSandbox}); err == nil {
		t.Fatal("cross-tenant copy was accepted")
	}
	if _, err := New(nil, wrongKey); !errors.Is(err, ErrCustodyUnavailable) {
		t.Fatalf("New without custody = %v, want ErrCustodyUnavailable", err)
	}
}

func TestTodo_SECARCH_009_Integration(t *testing.T) {
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "subject-a", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{string(authz.RoleCompAdmin)}, Purposes: []string{authz.PurposePayrollProcessing},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-a", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "cred-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	decision, err := authz.ResolveFields(principal, authz.PurposePayrollProcessing, []authz.FieldID{authz.FieldBaseSalary, authz.FieldBankAccountNumber}, nil)
	if err != nil {
		t.Fatalf("authz.ResolveFields: %v", err)
	}
	transformer, ctx, _ := testCustody(t)
	fixture, err := transformer.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldBaseSalary, Value: []byte("90000.00"), Environment: EnvironmentSandbox})
	if err != nil {
		t.Fatalf("PrepareCopy: %v", err)
	}
	receipts, err := ResolvePolicyReceipts(decision, EnvironmentSandbox, map[authz.FieldID]string{authz.FieldBaseSalary: fixture.Digest, authz.FieldBankAccountNumber: "sha256:excluded"})
	if err != nil {
		t.Fatalf("ResolvePolicyReceipts: %v", err)
	}
	if len(receipts) != 2 || receipts[0].GovernmentClass == "" || receipts[1].GovernmentClass == "" {
		t.Fatalf("receipts do not carry closed classification: %#v", receipts)
	}
	for _, receipt := range receipts {
		if err := receipt.Verify(); err != nil {
			t.Fatalf("receipt Verify(%q): %v", receipt.Field, err)
		}
	}
}

func TestTodo_SECARCH_009_Mutation(t *testing.T) {
	transformer, ctx, _ := testCustody(t)
	fixture, err := transformer.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldCaseNotes, Value: []byte("case value"), Environment: EnvironmentDevelopment})
	if err != nil {
		t.Fatalf("PrepareCopy: %v", err)
	}
	fixture.Value[0] ^= 1
	if err := fixture.Verify(); err == nil {
		t.Fatal("mutated fixture value verified")
	}
	fixture, err = transformer.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldCaseNotes, Value: []byte("case value"), Environment: EnvironmentDevelopment})
	if err != nil {
		t.Fatal(err)
	}
	fixture.PolicyReceipt.GovernmentClass = GovernmentClassNone
	if err := fixture.Verify(); err == nil {
		t.Fatal("mutated policy classification verified")
	}
}

func TestTodo_SECARCH_020(t *testing.T) {
	classes := []GovernmentDataClass{GovernmentClassNone, GovernmentClassFTI, GovernmentClassCJI, GovernmentClassCUI, GovernmentClassACA}
	policies := make([]ClassPolicy, 0, len(classes))
	for _, class := range classes {
		policy, err := ClassPolicyFor(class)
		if err != nil {
			t.Fatalf("ClassPolicyFor(%s): %v", class, err)
		}
		policies = append(policies, policy)
	}
	for i := 1; i < len(policies); i++ {
		if policies[i] == policies[0] {
			t.Fatalf("government class %s reused ordinary PII behavior", classes[i])
		}
	}
	for _, field := range []authz.FieldID{authz.FieldTaxID, authz.FieldCaseNotes, authz.FieldWorkerNumber, authz.FieldMedicalAccomodation} {
		policy, err := ResolveField(field)
		if err != nil {
			t.Fatal(err)
		}
		if policy.GovernmentClass == GovernmentClassNone || policy.Classification != policy.GovernmentClass {
			t.Errorf("field %q missing government classification: %#v", field, policy)
		}
	}
}

func TestTodo_SECARCH_020_Golden(t *testing.T) {
	for _, class := range []GovernmentDataClass{GovernmentClassFTI, GovernmentClassCJI, GovernmentClassCUI, GovernmentClassACA} {
		policy, err := ClassPolicyFor(class)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(policy.Key), string(class)) || !strings.Contains(string(policy.Storage), string(class)) {
			t.Fatalf("class %s is not bound to distinct key/storage behavior: %#v", class, policy)
		}
	}
}

func TestTodo_SECARCH_020_Security(t *testing.T) {
	if _, err := ClassPolicyFor("SECRET"); !errors.Is(err, ErrInvalidClassification) {
		t.Fatalf("unknown class error = %v, want ErrInvalidClassification", err)
	}
	if _, err := ResolveField(authz.FieldID("account.number.raw")); !errors.Is(err, ErrUnknownField) {
		t.Fatalf("unknown field error = %v, want ErrUnknownField", err)
	}
	var refusalErr *RefusalError
	if _, err := ResolveField(authz.FieldID("account.number.raw")); !errors.As(err, &refusalErr) || refusalErr.Field != "account.number.raw" {
		t.Fatalf("unknown field refusal does not name field: %v", err)
	}
	if strings.Contains(Explain(), "account") || strings.Contains(Explain(), "secret") {
		t.Fatal("Explain contains a raw secret/account disclosure")
	}
}

func TestTodo_SECARCH_020_Integration(t *testing.T) {
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "subject-a", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{string(authz.RoleAuditor)}, Purposes: []string{authz.PurposeAuditReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-a", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "cred-digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := authz.ResolveFields(principal, authz.PurposeAuditReview, []authz.FieldID{authz.FieldTaxID, authz.FieldCaseNotes}, nil)
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := ResolvePolicyReceipts(decision, EnvironmentDevelopment, map[authz.FieldID]string{authz.FieldTaxID: "sha256:fixture-tax", authz.FieldCaseNotes: "sha256:fixture-case"})
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range receipts {
		if receipt.AuthZPolicy != authz.PolicyVersion || receipt.AuthZRule == "" || receipt.GovernmentClass == GovernmentClassNone {
			t.Fatalf("receipt lost authz/classification dimensions: %#v", receipt)
		}
	}
}

func TestTodo_SECARCH_020_Mutation(t *testing.T) {
	receipt, err := NewPolicyReceipt(authz.FieldTaxID, EnvironmentSandbox, "sha256:fixture")
	if err != nil {
		t.Fatal(err)
	}
	receipt.Controls.Key = "CUSTODY_OTHER"
	if err := receipt.Verify(); err == nil {
		t.Fatal("mutated key behavior verified")
	}
	receipt, err = NewPolicyReceipt(authz.FieldTaxID, EnvironmentSandbox, "sha256:fixture")
	if err != nil {
		t.Fatal(err)
	}
	receipt.GovernmentClass = GovernmentClassCUI
	if err := receipt.Verify(); err == nil {
		t.Fatal("mutated government class verified")
	}
}

func testProfile(t *testing.T) ContractProfile {
	t.Helper()
	profile, err := NewContractProfile(ContractProfileSpec{
		EngagementRef: "engagement-a", Clause: "DFARS-252.204-7012", CMMCLevel: CMMCLevel2,
		NISTRevision: NIST800171Rev2, AssessmentMethod: AssessmentC3PAO, POAMRule: POAMProhibited,
		Evidence: []EvidenceRecord{{Ref: "ssp-digest", Digest: "sha256:ssp", Control: "3.1", Status: EvidenceAccepted}},
	})
	if err != nil {
		t.Fatalf("NewContractProfile: %v", err)
	}
	return profile
}

func validClaim(profile ContractProfile) ComplianceClaim {
	return ComplianceClaim{Clause: profile.Clause, CMMCLevel: profile.CMMCLevel, NISTRevision: profile.NISTRevision, AssessmentMethod: profile.AssessmentMethod, POAMRule: profile.POAMRule, Evidence: append([]EvidenceRecord(nil), profile.Evidence...)}
}

func TestTodo_SECARCH_021(t *testing.T) {
	profile := testProfile(t)
	if err := profile.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := profile.ValidateClaim(validClaim(profile)); err != nil {
		t.Fatalf("valid compliance claim refused: %v", err)
	}
	claim := validClaim(profile)
	claim.NISTRevision = NIST800171Rev3
	if err := EvaluateComplianceClaim(profile, claim); !errors.Is(err, ErrClaimRefused) {
		t.Fatalf("inconsistent NIST claim = %v, want ErrClaimRefused", err)
	}
	claim = validClaim(profile)
	claim.Evidence[0].Digest = "sha256:not-recorded"
	if err := profile.ValidateClaim(claim); !errors.Is(err, ErrClaimRefused) {
		t.Fatalf("unrecorded evidence claim = %v, want ErrClaimRefused", err)
	}
}

func TestTodo_SECARCH_021_Golden(t *testing.T) {
	profile := testProfile(t)
	if profile.Clause != "DFARS-252.204-7012" || profile.CMMCLevel != CMMCLevel2 || profile.NISTRevision != NIST800171Rev2 || profile.AssessmentMethod != AssessmentC3PAO || profile.POAMRule != POAMProhibited {
		t.Fatalf("profile contract coordinates drifted: %#v", profile)
	}
	if profile.Digest == "" || !strings.HasPrefix(profile.Digest, "sha256:") {
		t.Fatalf("profile digest = %q", profile.Digest)
	}
}

func TestTodo_SECARCH_021_Security(t *testing.T) {
	profile := testProfile(t)
	claim := validClaim(profile)
	claim.Evidence[0].Status = EvidencePOAM
	if err := profile.ValidateClaim(claim); !errors.Is(err, ErrClaimRefused) {
		t.Fatalf("POA&M evidence under prohibited rule = %v, want ErrClaimRefused", err)
	}
	profile.EngagementRef = "other-engagement"
	if err := profile.Verify(); err == nil {
		t.Fatal("mutated engagement profile verified")
	}
}

func TestTodo_SECARCH_021_Integration(t *testing.T) {
	profile := testProfile(t)
	next, err := profile.AddEvidence(EvidenceRecord{Ref: "assessment-digest", Digest: "sha256:assessment", Control: "3.13", Status: EvidenceAccepted})
	if err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}
	if next.Revision != profile.Revision+1 || next.Digest == profile.Digest || len(next.Evidence) != 2 {
		t.Fatalf("profile revision was not immutable/versioned: old=%#v new=%#v", profile, next)
	}
	if err := next.ValidateClaim(validClaim(next)); err != nil {
		t.Fatalf("claim against revised evidence set refused: %v", err)
	}
}

func TestTodo_SECARCH_021_Conformance(t *testing.T) {
	bad := ContractProfileSpec{EngagementRef: "engagement-a", Clause: "clause", CMMCLevel: CMMCLevel2, NISTRevision: NIST800171Rev3, AssessmentMethod: AssessmentC3PAO, POAMRule: POAMProhibited}
	if _, err := NewContractProfile(bad); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("empty evidence profile = %v, want ErrInvalidProfile", err)
	}
	bad.Evidence = []EvidenceRecord{{Ref: "evidence", Digest: "sha256:e", Control: "3.1", Status: EvidenceAccepted}}
	profile, err := NewContractProfile(bad)
	if err != nil {
		t.Fatal(err)
	}
	claim := validClaim(profile)
	claim.Clause = "wrong-clause"
	if err := profile.ValidateClaim(claim); !errors.Is(err, ErrClaimRefused) {
		t.Fatalf("wrong clause claim = %v, want ErrClaimRefused", err)
	}
}

func TestTodo_SECARCH_021_Mutation(t *testing.T) {
	profile := testProfile(t)
	profile.Evidence[0].Control = "3.2"
	if err := profile.Verify(); err == nil {
		t.Fatal("mutated evidence control verified")
	}
}
