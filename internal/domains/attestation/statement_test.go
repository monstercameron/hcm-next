package attestation

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_ATTEST_001 verifies that an AttestationStatement is immutable,
// validatable, and produces stable digests. This is the PRIMARY test.
func TestTodo_ATTEST_001(t *testing.T) {
	validTenant := values.TenantId("acme")
	validSubject := values.EntityRef{
		Tenant: validTenant,
		Kind:   "person",
		Id:     "550e8400-e29b-41d4-a716-446655440000",
	}
	validAttesterPrincipal := values.EntityRef{
		Tenant: validTenant,
		Kind:   "person",
		Id:     "550e8400-e29b-41d4-a716-446655440001",
	}
	validAttesterAssurance := IdentityAssuranceRef{
		ID:   "550e8400-e29b-41d4-a716-446655440002",
		Kind: "CREDENTIAL",
	}
	validAttester := AttesterPrincipal{
		PrincipalRef:         validAttesterPrincipal,
		IdentityAssuranceRef: validAttesterAssurance,
	}
	validJurisdiction := values.EntityRef{
		Tenant: validTenant,
		Kind:   "jurisdiction",
		Id:     "550e8400-e29b-41d4-a716-446655440003",
	}

	now := values.NewInstant(time.Now().UTC())
	later := values.NewInstant(time.Now().UTC().Add(24 * time.Hour))

	validWindow := ValidityWindow{
		StartsAt:  now,
		ExpiresAt: later,
	}

	validEvidence := []EvidenceRef{
		{
			ID:     "550e8400-e29b-41d4-a716-446655440004",
			Digest: "sha256:abcd1234",
		},
	}

	stmt := AttestationStatement{
		ID:              "550e8400-e29b-41d4-a716-446655440005",
		Version:         1,
		Kind:            StatementKindPositional,
		SubjectRef:      validSubject,
		Attester:        validAttester,
		TextDigest:      "sha256:text1234",
		EvidenceRefs:    validEvidence,
		ValidityWindow:  validWindow,
		JurisdictionRef: validJurisdiction,
	}

	// Test validation succeeds for well-formed statement.
	if err := stmt.Validate(); err != nil {
		t.Fatalf("valid statement failed validation: %v", err)
	}

	// Test that Canonical returns non-nil bytes.
	canonical := stmt.Canonical()
	if canonical == nil {
		t.Fatal("Canonical() returned nil")
	}
	if len(canonical) == 0 {
		t.Fatal("Canonical() returned empty bytes")
	}

	// Test that Digest returns non-empty string.
	digest := stmt.Digest()
	if digest == "" {
		t.Fatal("Digest() returned empty string")
	}

	// Test digest is deterministic.
	digest2 := stmt.Digest()
	if digest != digest2 {
		t.Errorf("Digest() not deterministic: %q vs %q", digest, digest2)
	}

	// Test that modifying a field changes the digest.
	stmtModified := stmt
	stmtModified.ID = "550e8400-e29b-41d4-a716-446655440006"
	digestModified := stmtModified.Digest()
	if digest == digestModified {
		t.Error("Modifying ID did not change digest")
	}

	// Test that modifying evidence changes digest.
	stmtModifiedEvidence := stmt
	stmtModifiedEvidence.EvidenceRefs[0].Digest = "sha256:efgh5678"
	digestModifiedEvidence := stmtModifiedEvidence.Digest()
	if digest == digestModifiedEvidence {
		t.Error("Modifying evidence digest did not change statement digest")
	}

	// Test that statement is rejected when version is zero.
	stmtZeroVersion := stmt
	stmtZeroVersion.Version = 0
	if err := stmtZeroVersion.Validate(); err == nil {
		t.Error("statement with version 0 should be rejected")
	}

	t.Log("PASS: TestTodo_ATTEST_001")
}

// TestTodo_ATTEST_001_Mutation tests that tampering with statement
// fields is detected by Validate.
func TestTodo_ATTEST_001_Mutation(t *testing.T) {
	validTenant := values.TenantId("acme")

	stmtBase := AttestationStatement{
		ID:      "550e8400-e29b-41d4-a716-446655440005",
		Version: 1,
		Kind:    StatementKindConsent,
		SubjectRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "550e8400-e29b-41d4-a716-446655440000",
		},
		Attester: AttesterPrincipal{
			PrincipalRef: values.EntityRef{
				Tenant: validTenant,
				Kind:   "person",
				Id:     "550e8400-e29b-41d4-a716-446655440001",
			},
			IdentityAssuranceRef: IdentityAssuranceRef{
				ID:   "550e8400-e29b-41d4-a716-446655440002",
				Kind: "CREDENTIAL",
			},
		},
		TextDigest: "sha256:text1234",
		EvidenceRefs: []EvidenceRef{
			{ID: "550e8400-e29b-41d4-a716-446655440004", Digest: "sha256:abcd1234"},
		},
		ValidityWindow: ValidityWindow{
			StartsAt:  values.NewInstant(time.Now().UTC()),
			ExpiresAt: values.NewInstant(time.Now().UTC().Add(24 * time.Hour)),
		},
		JurisdictionRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "jurisdiction",
			Id:     "550e8400-e29b-41d4-a716-446655440003",
		},
	}

	tests := []struct {
		name      string
		mutate    func(*AttestationStatement)
		shouldErr bool
		errType   error
	}{
		{
			name: "empty ID",
			mutate: func(s *AttestationStatement) {
				s.ID = ""
			},
			shouldErr: true,
			errType:   ErrStatementIDEmpty,
		},
		{
			name: "zero version",
			mutate: func(s *AttestationStatement) {
				s.Version = 0
			},
			shouldErr: true,
			errType:   ErrVersionZero,
		},
		{
			name: "invalid kind",
			mutate: func(s *AttestationStatement) {
				s.Kind = StatementKind("INVALID")
			},
			shouldErr: true,
			errType:   ErrInvalidStatementKind,
		},
		{
			name: "empty subject",
			mutate: func(s *AttestationStatement) {
				s.SubjectRef = values.EntityRef{}
			},
			shouldErr: true,
		},
		{
			name: "empty attester principal",
			mutate: func(s *AttestationStatement) {
				s.Attester.PrincipalRef = values.EntityRef{}
			},
			shouldErr: true,
		},
		{
			name: "empty identity assurance",
			mutate: func(s *AttestationStatement) {
				s.Attester.IdentityAssuranceRef = IdentityAssuranceRef{}
			},
			shouldErr: true,
		},
		{
			name: "empty text digest",
			mutate: func(s *AttestationStatement) {
				s.TextDigest = ""
			},
			shouldErr: true,
			errType:   ErrTextDigestEmpty,
		},
		{
			name: "empty evidence refs",
			mutate: func(s *AttestationStatement) {
				s.EvidenceRefs = []EvidenceRef{}
			},
			shouldErr: true,
			errType:   ErrEvidenceRefsEmpty,
		},
		{
			name: "invalid evidence ref (empty digest)",
			mutate: func(s *AttestationStatement) {
				s.EvidenceRefs[0].Digest = ""
			},
			shouldErr: true,
		},
		{
			name: "empty jurisdiction",
			mutate: func(s *AttestationStatement) {
				s.JurisdictionRef = values.EntityRef{}
			},
			shouldErr: true,
		},
		{
			name: "self-revocation",
			mutate: func(s *AttestationStatement) {
				s.RevocationLink = &RevocationLink{
					StatementID: s.ID,
					Reason:      "test",
				}
			},
			shouldErr: true,
			errType:   ErrSelfRevocation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt := stmtBase
			tt.mutate(&stmt)
			err := stmt.Validate()
			if !tt.shouldErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.shouldErr && err == nil {
				t.Fatalf("expected error but got none")
			}
		})
	}

	t.Log("PASS: TestTodo_ATTEST_001_Mutation")
}

// TestTodo_ATTEST_001_Golden verifies digest stability across
// different field orderings (e.g., evidence refs are sorted).
func TestTodo_ATTEST_001_Golden(t *testing.T) {
	validTenant := values.TenantId("test-tenant")
	validSubject := values.EntityRef{
		Tenant: validTenant,
		Kind:   "person",
		Id:     "550e8400-e29b-41d4-a716-446655440007",
	}
	validAttester := AttesterPrincipal{
		PrincipalRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "550e8400-e29b-41d4-a716-446655440008",
		},
		IdentityAssuranceRef: IdentityAssuranceRef{
			ID:   "550e8400-e29b-41d4-a716-446655440009",
			Kind: "CREDENTIAL",
		},
	}
	validJurisdiction := values.EntityRef{
		Tenant: validTenant,
		Kind:   "jurisdiction",
		Id:     "550e8400-e29b-41d4-a716-4466554400a0",
	}

	baseTime := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	endTime := values.NewInstant(time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC))

	stmt := AttestationStatement{
		ID:         "550e8400-e29b-41d4-a716-4466554400a4",
		Version:    1,
		Kind:       StatementKindCertification,
		SubjectRef: validSubject,
		Attester:   validAttester,
		TextDigest: "sha256:text",
		EvidenceRefs: []EvidenceRef{
			{ID: "550e8400-e29b-41d4-a716-4466554400a1", Digest: "sha256:c"},
			{ID: "550e8400-e29b-41d4-a716-4466554400a2", Digest: "sha256:a"},
			{ID: "550e8400-e29b-41d4-a716-4466554400a3", Digest: "sha256:b"},
		},
		ValidityWindow: ValidityWindow{
			StartsAt:  baseTime,
			ExpiresAt: endTime,
		},
		JurisdictionRef: validJurisdiction,
	}

	digest1 := stmt.Digest()

	// Reorder evidence refs and verify digest is the same
	// (canonicalization sorts them).
	stmt.EvidenceRefs = []EvidenceRef{
		{ID: "550e8400-e29b-41d4-a716-4466554400a3", Digest: "sha256:b"},
		{ID: "550e8400-e29b-41d4-a716-4466554400a2", Digest: "sha256:a"},
		{ID: "550e8400-e29b-41d4-a716-4466554400a1", Digest: "sha256:c"},
	}

	digest2 := stmt.Digest()
	if digest1 != digest2 {
		t.Errorf("digest changed after evidence reordering: %q != %q", digest1, digest2)
	}

	t.Log("PASS: TestTodo_ATTEST_001_Golden")
}

// TestTodo_ATTEST_001_Property verifies that content equality
// implies digest equality.
func TestTodo_ATTEST_001_Property(t *testing.T) {
	validTenant := values.TenantId("acme")
	validSubject := values.EntityRef{
		Tenant: validTenant,
		Kind:   "person",
		Id:     "550e8400-e29b-41d4-a716-446655440000",
	}
	validAttester := AttesterPrincipal{
		PrincipalRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "550e8400-e29b-41d4-a716-446655440001",
		},
		IdentityAssuranceRef: IdentityAssuranceRef{
			ID:   "550e8400-e29b-41d4-a716-446655440002",
			Kind: "CREDENTIAL",
		},
	}
	validJurisdiction := values.EntityRef{
		Tenant: validTenant,
		Kind:   "jurisdiction",
		Id:     "550e8400-e29b-41d4-a716-446655440003",
	}

	now := values.NewInstant(time.Now().UTC())
	later := values.NewInstant(time.Now().UTC().Add(24 * time.Hour))

	stmt1 := AttestationStatement{
		ID:         "550e8400-e29b-41d4-a716-4466554400a5",
		Version:    1,
		Kind:       StatementKindPositional,
		SubjectRef: validSubject,
		Attester:   validAttester,
		TextDigest: "sha256:text1",
		EvidenceRefs: []EvidenceRef{
			{ID: "550e8400-e29b-41d4-a716-4466554400a7", Digest: "sha256:a"},
		},
		ValidityWindow: ValidityWindow{
			StartsAt:  now,
			ExpiresAt: later,
		},
		JurisdictionRef: validJurisdiction,
	}

	stmt2 := AttestationStatement{
		ID:         "550e8400-e29b-41d4-a716-4466554400a5",
		Version:    1,
		Kind:       StatementKindPositional,
		SubjectRef: validSubject,
		Attester:   validAttester,
		TextDigest: "sha256:text1",
		EvidenceRefs: []EvidenceRef{
			{ID: "550e8400-e29b-41d4-a716-4466554400a7", Digest: "sha256:a"},
		},
		ValidityWindow: ValidityWindow{
			StartsAt:  now,
			ExpiresAt: later,
		},
		JurisdictionRef: validJurisdiction,
	}

	digest1 := stmt1.Digest()
	digest2 := stmt2.Digest()

	if digest1 != digest2 {
		t.Errorf("identical statements have different digests: %q != %q", digest1, digest2)
	}

	// Now change one field and verify digest differs.
	stmt2.TextDigest = "sha256:text2"
	digest2Changed := stmt2.Digest()
	if digest1 == digest2Changed {
		t.Error("changed text digest but statement digest remained the same")
	}

	t.Log("PASS: TestTodo_ATTEST_001_Property")
}

// TestTodo_ATTEST_001_Security verifies that invalid statements
// are rejected, including those with missing assurance refs or
// inverted validity windows.
func TestTodo_ATTEST_001_Security(t *testing.T) {
	validTenant := values.TenantId("acme")

	validSubject := values.EntityRef{
		Tenant: validTenant,
		Kind:   "person",
		Id:     "550e8400-e29b-41d4-a716-446655440000",
	}

	validAttester := AttesterPrincipal{
		PrincipalRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "550e8400-e29b-41d4-a716-446655440001",
		},
		IdentityAssuranceRef: IdentityAssuranceRef{
			ID:   "550e8400-e29b-41d4-a716-446655440002",
			Kind: "CREDENTIAL",
		},
	}

	validJurisdiction := values.EntityRef{
		Tenant: validTenant,
		Kind:   "jurisdiction",
		Id:     "550e8400-e29b-41d4-a716-446655440003",
	}

	validNow := values.NewInstant(time.Now().UTC())
	validLater := values.NewInstant(time.Now().UTC().Add(24 * time.Hour))

	tests := []struct {
		name        string
		buildStmt   func() *AttestationStatement
		shouldFail  bool
		description string
	}{
		{
			name: "no identity assurance ref",
			buildStmt: func() *AttestationStatement {
				stmt := &AttestationStatement{
					ID:         "550e8400-e29b-41d4-a716-4466554400a5",
					Version:    1,
					Kind:       StatementKindPositional,
					SubjectRef: validSubject,
					Attester: AttesterPrincipal{
						PrincipalRef: values.EntityRef{
							Tenant: validTenant,
							Kind:   "person",
							Id:     "550e8400-e29b-41d4-a716-446655440001",
						},
						IdentityAssuranceRef: IdentityAssuranceRef{
							ID:   "",
							Kind: "",
						},
					},
					TextDigest: "sha256:text",
					EvidenceRefs: []EvidenceRef{
						{ID: "550e8400-e29b-41d4-a716-446655440004", Digest: "sha256:doc"},
					},
					ValidityWindow: ValidityWindow{
						StartsAt:  validNow,
						ExpiresAt: validLater,
					},
					JurisdictionRef: validJurisdiction,
				}
				return stmt
			},
			shouldFail:  true,
			description: "statement without identity assurance is rejected",
		},
		{
			name: "inverted validity window",
			buildStmt: func() *AttestationStatement {
				stmt := &AttestationStatement{
					ID:         "550e8400-e29b-41d4-a716-4466554400a6",
					Version:    1,
					Kind:       StatementKindPositional,
					SubjectRef: validSubject,
					Attester:   validAttester,
					TextDigest: "sha256:text",
					EvidenceRefs: []EvidenceRef{
						{ID: "550e8400-e29b-41d4-a716-446655440004", Digest: "sha256:doc"},
					},
					ValidityWindow: ValidityWindow{
						StartsAt:  validLater,
						ExpiresAt: validNow,
					},
					JurisdictionRef: validJurisdiction,
				}
				return stmt
			},
			shouldFail:  true,
			description: "statement with inverted validity window is rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt := tt.buildStmt()
			err := stmt.Validate()
			if tt.shouldFail && err == nil {
				t.Fatalf("expected failure: %s", tt.description)
			}
			if !tt.shouldFail && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	t.Log("PASS: TestTodo_ATTEST_001_Security")
}
