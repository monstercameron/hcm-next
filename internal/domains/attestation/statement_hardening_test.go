package attestation

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func statementFixture() AttestationStatement {
	tenant := values.TenantId("tenant-a")
	ref := values.EntityRef{Tenant: tenant, Kind: "person", Id: "00000000-0000-4000-8000-000000000001"}
	return AttestationStatement{ID: "statement-1", Version: 1, Kind: StatementKindConsent, SubjectRef: ref,
		Attester:   AttesterPrincipal{PrincipalRef: ref, IdentityAssuranceRef: IdentityAssuranceRef{ID: "assurance-1", Kind: "CREDENTIAL"}},
		TextDigest: "sha256:text", EvidenceRefs: []EvidenceRef{{ID: "e1", Digest: "sha256:e1"}},
		ValidityWindow:  ValidityWindow{StartsAt: values.NewInstant(time.Unix(1, 0)), ExpiresAt: values.NewInstant(time.Unix(2, 0))},
		JurisdictionRef: values.EntityRef{Tenant: tenant, Kind: "jurisdiction", Id: "00000000-0000-4000-8000-000000000002"}}
}

func TestStatementVocabulary_ValidateAndString(t *testing.T) {
	for _, kind := range []StatementKind{StatementKindPositional, StatementKindConsent, StatementKindAcknowledgment, StatementKindCertification} {
		if !kind.Valid() || kind.String() != string(kind) {
			t.Fatalf("kind %q did not validate/stringify", kind)
		}
	}
	if StatementKindUnspecified.Valid() || StatementKind("OTHER").Valid() {
		t.Fatal("undeclared statement kind accepted")
	}
}

func TestStatementNestedValidation_ReturnsTypedErrors(t *testing.T) {
	if err := (IdentityAssuranceRef{}).Validate(); !errors.Is(err, ErrIdentityAssuranceEmpty) {
		t.Fatalf("empty assurance id = %v, want ErrIdentityAssuranceEmpty", err)
	}
	if err := (IdentityAssuranceRef{ID: "id"}).Validate(); err == nil {
		t.Fatal("empty assurance kind accepted")
	}
	if err := (AttesterPrincipal{}).Validate(); !errors.Is(err, ErrAttesterEmpty) {
		t.Fatalf("empty principal = %v", err)
	}
	if err := (EvidenceRef{}).Validate(); err == nil {
		t.Fatal("empty evidence ref accepted")
	}
	valid := statementFixture()
	if err := valid.Attester.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := valid.ValidityWindow.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*ValidityWindow)
	}{
		{"missing start", func(v *ValidityWindow) { v.StartsAt = values.Instant{} }},
		{"missing end", func(v *ValidityWindow) { v.ExpiresAt = values.Instant{} }},
		{"inverted", func(v *ValidityWindow) { v.StartsAt, v.ExpiresAt = v.ExpiresAt, v.StartsAt }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			window := valid.ValidityWindow
			tc.edit(&window)
			if err := window.Validate(); err == nil {
				t.Fatal("invalid validity window accepted")
			}
		})
	}
}

func TestAttestationStatement_ValidateAllSecurityBranches(t *testing.T) {
	base := statementFixture()
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*AttestationStatement)
		want error
	}{
		{"id", func(s *AttestationStatement) { s.ID = "" }, ErrStatementIDEmpty},
		{"version", func(s *AttestationStatement) { s.Version = 0 }, ErrVersionZero},
		{"unspecified kind", func(s *AttestationStatement) { s.Kind = StatementKindUnspecified }, ErrStatementUnspecified},
		{"unknown kind", func(s *AttestationStatement) { s.Kind = "BOGUS" }, ErrInvalidStatementKind},
		{"subject", func(s *AttestationStatement) { s.SubjectRef = values.EntityRef{} }, ErrSubjectEmpty},
		{"attester", func(s *AttestationStatement) { s.Attester.PrincipalRef = values.EntityRef{} }, ErrAttesterEmpty},
		{"text", func(s *AttestationStatement) { s.TextDigest = "" }, ErrTextDigestEmpty},
		{"evidence list", func(s *AttestationStatement) { s.EvidenceRefs = nil }, ErrEvidenceRefsEmpty},
		{"jurisdiction", func(s *AttestationStatement) { s.JurisdictionRef = values.EntityRef{} }, ErrJurisdictionEmpty},
		{"self revoke", func(s *AttestationStatement) { s.RevocationLink = &RevocationLink{StatementID: s.ID, Reason: "bad"} }, ErrSelfRevocation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			bad.EvidenceRefs = append([]EvidenceRef(nil), base.EvidenceRefs...)
			tc.edit(&bad)
			if err := bad.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
	badEvidence := base
	badEvidence.EvidenceRefs = []EvidenceRef{{ID: "e1"}}
	if err := badEvidence.Validate(); err == nil {
		t.Fatal("malformed nested evidence accepted")
	}
	badWindow := base
	badWindow.ValidityWindow.StartsAt = badWindow.ValidityWindow.ExpiresAt
	if !errors.Is(badWindow.Validate(), ErrValidityWindowInverted) {
		t.Fatal("inverted window did not return sentinel")
	}
	badRevocation := base
	badRevocation.RevocationLink = &RevocationLink{StatementID: "other"}
	if err := badRevocation.Validate(); err == nil {
		t.Fatal("revocation without reason accepted")
	}
	withRevocation := base
	withRevocation.RevocationLink = &RevocationLink{StatementID: "revoker", Reason: "withdrawn"}
	if err := withRevocation.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAttestationStatement_CanonicalSortsWithoutMutation(t *testing.T) {
	base := statementFixture()
	base.EvidenceRefs = []EvidenceRef{{ID: "e2", Digest: "sha256:e2"}, {ID: "e1", Digest: "sha256:e1"}}
	before := append([]EvidenceRef(nil), base.EvidenceRefs...)
	first := base.Digest()
	if first == "" || len(base.Canonical()) == 0 {
		t.Fatal("canonical statement was empty")
	}
	if base.EvidenceRefs[0] != before[0] || base.EvidenceRefs[1] != before[1] {
		t.Fatal("Canonical mutated input evidence order")
	}
	base.EvidenceRefs = []EvidenceRef{{ID: "e1", Digest: "sha256:e1"}, {ID: "e2", Digest: "sha256:e2"}}
	if first != base.Digest() {
		t.Fatal("evidence order changed digest")
	}
}
