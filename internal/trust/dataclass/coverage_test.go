package dataclass

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/authz"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

func TestPolicyVocabularyRegistryAndReceipts(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version() = %d", Version())
	}
	for _, class := range []GovernmentDataClass{GovernmentClassNone, GovernmentClassFTI, GovernmentClassCJI, GovernmentClassCUI, GovernmentClassACA} {
		if !class.Valid() || class.String() != string(class) {
			t.Errorf("class %q validation/string failed", class)
		}
		if _, err := ClassPolicyFor(class); err != nil {
			t.Error(err)
		}
	}
	if GovernmentDataClass("bad").Valid() || HandlingRule("bad").Valid() || !HandlingUnchanged.Valid() {
		t.Fatal("closed vocabulary validation failed")
	}
	if _, err := ClassPolicyFor(GovernmentDataClass("bad")); !errors.Is(err, ErrInvalidClassification) {
		t.Fatalf("invalid class policy = %v", err)
	}
	fields, alias := Registry(), Fields()
	if len(fields) == 0 || len(fields) != len(alias) || fields[0].Field != alias[0].Field {
		t.Fatalf("registry/fields = %d/%d", len(fields), len(alias))
	}
	fields[0].Field = "mutated"
	if Registry()[0].Field == "mutated" {
		t.Fatal("Registry exposed package state")
	}
	if _, err := ResolveFields([]authz.FieldID{authz.FieldBaseSalary, authz.FieldBaseSalary}); !errors.Is(err, ErrDuplicateField) {
		t.Fatalf("duplicate fields = %v", err)
	}
	if _, err := ResolveFields([]authz.FieldID{authz.FieldBaseSalary, "unknown"}); !errors.Is(err, ErrUnknownField) {
		t.Fatalf("unknown field = %v", err)
	}
	if got, err := ResolveFields(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty fields = %#v, %v", got, err)
	}
	for _, tc := range []struct {
		name string
		env  Environment
		fix  string
	}{
		{"invalid environment", Environment("LOCAL"), "sha256:x"},
		{"missing lower fixture", EnvironmentSandbox, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPolicyReceipt(authz.FieldTaxID, tc.env, tc.fix); !errors.Is(err, ErrInvalidReceipt) {
				t.Fatalf("receipt error = %v", err)
			}
		})
	}
	production, err := NewPolicyReceipt(authz.FieldTaxID, EnvironmentProduction, "")
	if err != nil || production.Verify() != nil || production.FixtureDigest != "" {
		t.Fatalf("production receipt = %+v, %v", production, err)
	}
	valid, err := NewPolicyReceipt(authz.FieldTaxID, EnvironmentSandbox, "sha256:fixture")
	if err != nil || valid.Verify() != nil {
		t.Fatalf("valid receipt = %+v, %v", valid, err)
	}
	valid.Digest = ""
	if err := valid.Verify(); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("incomplete receipt = %v", err)
	}
	valid, _ = NewPolicyReceipt(authz.FieldTaxID, EnvironmentSandbox, "sha256:fixture")
	valid.Digest = "sha256:forged"
	if err := valid.Verify(); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("forged receipt = %v", err)
	}
	var refusalErr *RefusalError
	if _, err := ResolveField("missing"); !errors.As(err, &refusalErr) || refusalErr.Unwrap() == nil || refusalErr.Error() == "" || refusalErr.Field != "missing" {
		t.Fatalf("refusal error = %v", err)
	}
}

type coverageDeriver struct {
	output []byte
	err    error
}

func (d coverageDeriver) Derive(custody.Context, custody.Handle, []byte) (custody.DerivedValue, custody.Receipt, error) {
	return custody.DerivedValue{Output: d.output}, custody.Receipt{}, d.err
}

func TestTransformer_ConstructPrepareCopyAndReverse(t *testing.T) {
	_, ctx, key := testCustody(t)
	if _, err := New(nil, key); !errors.Is(err, ErrCustodyUnavailable) {
		t.Fatalf("nil deriver = %v", err)
	}
	if _, err := New(coverageDeriver{output: make([]byte, 32)}, custody.Handle{}); !errors.Is(err, ErrCustodyUnavailable) {
		t.Fatalf("invalid key = %v", err)
	}
	transformer, err := NewTransformer(coverageDeriver{output: make([]byte, 32)}, key)
	if err != nil || transformer == nil {
		t.Fatalf("NewTransformer = %v", err)
	}
	for _, tc := range []struct {
		name string
		mut  func(*PrepareCopyRequest)
	}{
		{"unknown field", func(r *PrepareCopyRequest) { r.Field = "unknown" }},
		{"production target", func(r *PrepareCopyRequest) { r.Environment = EnvironmentProduction }},
		{"invalid environment", func(r *PrepareCopyRequest) { r.Environment = Environment("LOCAL") }},
		{"invalid context", func(r *PrepareCopyRequest) { r.Context = custody.Context{} }},
		{"wrong tenant", func(r *PrepareCopyRequest) { r.Context.Tenant = "other" }},
		{"wrong region", func(r *PrepareCopyRequest) { r.Context.Region = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := PrepareCopyRequest{Context: ctx, Field: authz.FieldBaseSalary, Value: []byte("value"), Environment: EnvironmentSandbox}
			tc.mut(&req)
			if _, err := transformer.PrepareCopy(req); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	if _, err := (*Transformer)(nil).PrepareCopy(PrepareCopyRequest{Field: authz.FieldBaseSalary}); !errors.Is(err, ErrCustodyUnavailable) {
		t.Fatalf("nil transformer = %v", err)
	}
	for _, deriver := range []Deriver{coverageDeriver{err: errors.New("derive failed")}, coverageDeriver{output: []byte("short")}} {
		tm, err := New(deriver, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tm.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldCaseNotes, Value: []byte("case"), Environment: EnvironmentSandbox}); !errors.Is(err, ErrCustodyUnavailable) {
			t.Fatalf("derivation failure = %v", err)
		}
	}
	transformer, ctx, _ = testCustody(t)
	excluded, err := transformer.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldTaxID, Value: []byte("tax"), Environment: EnvironmentSandbox})
	if err != nil || len(excluded.Value) != 0 || excluded.OutputDigest != "" || excluded.Verify() != nil || FixtureDigest(excluded) != excluded.Digest {
		t.Fatalf("excluded fixture = %+v, %v", excluded, err)
	}
	if _, err := transformer.Reverse(ctx, excluded); !errors.Is(err, ErrIrreversible) {
		t.Fatalf("Reverse = %v", err)
	}
	fixture, err := transformer.PrepareCopy(PrepareCopyRequest{Context: ctx, Field: authz.FieldBaseSalary, Value: []byte("salary"), Environment: EnvironmentSandbox})
	if err != nil || fixture.Handling != HandlingDeterministicMask || !strings.HasPrefix(string(fixture.Value), "mask:v1:") {
		t.Fatalf("masked fixture = %+v, %v", fixture, err)
	}
	fixture.SourceDigest = ""
	if err := fixture.Verify(); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("fixture without source digest = %v", err)
	}
}

func TestResolvePolicyReceiptsAndComplianceProfiles(t *testing.T) {
	decision := authz.FieldDecision{Purpose: "purpose", PolicyVersion: authz.PolicyVersion, Rulings: map[authz.FieldID]authz.FieldRuling{authz.FieldTaxID: {Effect: authz.EffectDenied, RuleID: "deny-tax"}, authz.FieldBaseSalary: {Effect: authz.EffectAllow, RuleID: "allow-salary"}}}
	receipts, err := ResolvePolicyReceipts(decision, EnvironmentSandbox, map[authz.FieldID]string{authz.FieldTaxID: "tax", authz.FieldBaseSalary: "salary"})
	if err != nil || len(receipts) != 2 || receipts[0].Field >= receipts[1].Field || receipts[0].AuthZRule == "" || receipts[0].Purpose != "purpose" {
		t.Fatalf("receipts = %+v, %v", receipts, err)
	}
	if _, err := ResolvePolicyReceipts(decision, EnvironmentSandbox, nil); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatalf("missing fixture digests = %v", err)
	}
	if got, err := ResolvePolicyReceipts(authz.FieldDecision{}, EnvironmentProduction, nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty decision = %#v, %v", got, err)
	}
	profile := testProfile(t)
	if err := profile.Verify(); err != nil || EvaluateComplianceClaim(profile, validClaim(profile)) != nil {
		t.Fatalf("valid profile/claim = %v", err)
	}
	base := ContractProfileSpec{EngagementRef: "e", Clause: "c", CMMCLevel: CMMCLevel2, NISTRevision: NIST800171Rev2, AssessmentMethod: AssessmentSelf, POAMRule: POAMProhibited, Evidence: profile.Evidence}
	for _, mutate := range []func(*ContractProfileSpec){func(s *ContractProfileSpec) { s.EngagementRef = " " }, func(s *ContractProfileSpec) { s.Clause = " " }, func(s *ContractProfileSpec) { s.CMMCLevel = CMMCLevelUnspecified }, func(s *ContractProfileSpec) { s.NISTRevision = "bad" }, func(s *ContractProfileSpec) { s.AssessmentMethod = "bad" }, func(s *ContractProfileSpec) { s.POAMRule = "bad" }, func(s *ContractProfileSpec) { s.Evidence = nil }} {
		spec := base
		mutate(&spec)
		if _, err := NewContractProfile(spec); !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("invalid profile = %v", err)
		}
	}
	dup := profile.Evidence[0]
	if _, err := NewContractProfile(ContractProfileSpec{EngagementRef: "e", Clause: "c", CMMCLevel: CMMCLevel1, NISTRevision: NIST800171Rev3, AssessmentMethod: AssessmentSelf, POAMRule: POAMAllowed, Evidence: []EvidenceRecord{dup, dup}}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("duplicate evidence = %v", err)
	}
	next, err := profile.AddEvidence(EvidenceRecord{Ref: "new", Digest: "digest", Control: "3.2", Status: EvidencePOAM})
	if err != nil || next.Revision != profile.Revision+1 || len(next.Evidence) != 2 || next.Verify() != nil {
		t.Fatalf("AddEvidence = %+v, %v", next, err)
	}
	if _, err := profile.AddEvidence(dup); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("duplicate AddEvidence = %v", err)
	}
	claim := validClaim(profile)
	claim.Clause = "wrong"
	if err := profile.ValidateClaim(claim); !errors.Is(err, ErrClaimRefused) {
		t.Fatalf("wrong claim = %v", err)
	}
	claim = validClaim(profile)
	claim.Evidence = nil
	if err := profile.ValidateClaim(claim); !errors.Is(err, ErrClaimRefused) {
		t.Fatalf("empty evidence claim = %v", err)
	}
	claim = validClaim(profile)
	claim.Evidence[0].Control = "forged"
	if err := profile.ValidateClaim(claim); !errors.Is(err, ErrClaimRefused) {
		t.Fatal("forged evidence claim accepted")
	}
	claim = validClaim(profile)
	claim.Evidence[0].Status = EvidencePOAM
	if err := profile.ValidateClaim(claim); !errors.Is(err, ErrClaimRefused) {
		t.Fatal("POAM evidence accepted under prohibited profile")
	}
	if !strings.Contains(Explain(), "government data classes") {
		t.Fatal("Explain omitted policy description")
	}
}
