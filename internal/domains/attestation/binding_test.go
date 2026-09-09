package attestation

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_ATTEST_002 is the PRIMARY test for binding attestation to exact
// evidence and context. It verifies that a Binding captures exact evidence
// digests and context state, and that Verify detects drift.
func TestTodo_ATTEST_002(t *testing.T) {
	validTenant := values.TenantId("acme")

	// Build a valid statement.
	stmt := AttestationStatement{
		ID:      "stmt-001",
		Version: 1,
		Kind:    StatementKindPositional,
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
				ID:   "assurance-001",
				Kind: "CREDENTIAL",
			},
		},
		TextDigest: "sha256:text123",
		EvidenceRefs: []EvidenceRef{
			{ID: "doc-001", Digest: "sha256:evidence_a"},
			{ID: "doc-002", Digest: "sha256:evidence_b"},
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

	// Build a context digest for the binding.
	contextDigest := ContextDigest{
		SubjectAsOf:      "sha256:subject_state_001",
		Jurisdiction:     "us-ca",
		Locale:           "en-US",
		RenderedTextHash: "sha256:rendered_text_001",
	}

	// Build evidence bindings.
	evidenceBindings := []EvidenceBinding{
		{EvidenceID: "doc-001", EvidenceHash: "sha256:evidence_a"},
		{EvidenceID: "doc-002", EvidenceHash: "sha256:evidence_b"},
	}

	// Create a binding.
	binding := Binding{
		StatementID:      stmt.ID,
		StatementVersion: stmt.Version,
		ContextDigest:    contextDigest,
		EvidenceBindings: evidenceBindings,
		BindingVersion:   1,
		BoundAt:          values.NewInstant(time.Now().UTC()),
		Signer: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "550e8400-e29b-41d4-a716-446655440004",
		},
	}

	// Validate the binding.
	if err := binding.Validate(); err != nil {
		t.Fatalf("valid binding failed validation: %v", err)
	}

	// Test Canonical returns non-nil bytes.
	canonical := binding.Canonical()
	if canonical == nil {
		t.Fatal("Canonical() returned nil")
	}
	if len(canonical) == 0 {
		t.Fatal("Canonical() returned empty bytes")
	}

	// Test Digest returns non-empty string.
	digest := binding.Digest()
	if digest == "" {
		t.Fatal("Digest() returned empty string")
	}

	// Test digest is deterministic.
	digest2 := binding.Digest()
	if digest != digest2 {
		t.Errorf("Digest() not deterministic: %q vs %q", digest, digest2)
	}

	// Test Verify with matching context and evidence returns valid.
	verifyResult := binding.Verify(stmt, contextDigest, stmt.EvidenceRefs)
	if !verifyResult.Valid {
		t.Errorf("Verify failed when context and evidence match: %v", verifyResult)
	}
	if verifyResult.Reason != DriftNone {
		t.Errorf("expected DriftNone, got %v", verifyResult.Reason)
	}

	t.Log("PASS: TestTodo_ATTEST_002")
}

// TestTodo_ATTEST_002_Golden tests binding digest stability across different
// evidence orderings and rebindings.
func TestTodo_ATTEST_002_Golden(t *testing.T) {
	validTenant := values.TenantId("test-tenant")
	boundTime := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	stmt := AttestationStatement{
		ID:      "stmt-gold-001",
		Version: 1,
		Kind:    StatementKindConsent,
		SubjectRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "550e8400-e29b-41d4-a716-446655440010",
		},
		Attester: AttesterPrincipal{
			PrincipalRef: values.EntityRef{
				Tenant: validTenant,
				Kind:   "person",
				Id:     "550e8400-e29b-41d4-a716-446655440011",
			},
			IdentityAssuranceRef: IdentityAssuranceRef{
				ID:   "assurance-gold-001",
				Kind: "CREDENTIAL",
			},
		},
		TextDigest: "sha256:text_gold",
		EvidenceRefs: []EvidenceRef{
			{ID: "doc-c", Digest: "sha256:evidence_c"},
			{ID: "doc-a", Digest: "sha256:evidence_a"},
			{ID: "doc-b", Digest: "sha256:evidence_b"},
		},
		ValidityWindow: ValidityWindow{
			StartsAt:  boundTime,
			ExpiresAt: values.NewInstant(time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)),
		},
		JurisdictionRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "jurisdiction",
			Id:     "550e8400-e29b-41d4-a716-446655440012",
		},
	}

	contextDigest := ContextDigest{
		SubjectAsOf:      "sha256:subject_state_gold",
		Jurisdiction:     "us-ny",
		Locale:           "en-US",
		RenderedTextHash: "sha256:rendered_text_gold",
	}

	binding1 := Binding{
		StatementID:      stmt.ID,
		StatementVersion: stmt.Version,
		ContextDigest:    contextDigest,
		EvidenceBindings: []EvidenceBinding{
			{EvidenceID: "doc-c", EvidenceHash: "sha256:evidence_c"},
			{EvidenceID: "doc-a", EvidenceHash: "sha256:evidence_a"},
			{EvidenceID: "doc-b", EvidenceHash: "sha256:evidence_b"},
		},
		BindingVersion: 1,
		BoundAt:        boundTime,
		Signer: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "550e8400-e29b-41d4-a716-446655440013",
		},
	}

	digest1 := binding1.Digest()

	// Create binding2 with evidence in different order; should have same digest
	// (canonicalization sorts them).
	binding2 := Binding{
		StatementID:      stmt.ID,
		StatementVersion: stmt.Version,
		ContextDigest:    contextDigest,
		EvidenceBindings: []EvidenceBinding{
			{EvidenceID: "doc-b", EvidenceHash: "sha256:evidence_b"},
			{EvidenceID: "doc-c", EvidenceHash: "sha256:evidence_c"},
			{EvidenceID: "doc-a", EvidenceHash: "sha256:evidence_a"},
		},
		BindingVersion: 1,
		BoundAt:        boundTime,
		Signer: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "550e8400-e29b-41d4-a716-446655440013",
		},
	}

	digest2 := binding2.Digest()
	if digest1 != digest2 {
		t.Errorf("binding digest changed after evidence reordering: %q != %q", digest1, digest2)
	}

	// Test rebinding: new binding version should have different digest.
	binding3 := binding1
	binding3.BindingVersion = 2
	digest3 := binding3.Digest()
	if digest1 == digest3 {
		t.Error("rebinding with new version did not change digest")
	}

	t.Log("PASS: TestTodo_ATTEST_002_Golden")
}

// TestTodo_ATTEST_002_Mutation tests that tampering with binding fields
// is detected by Validate or Verify.
func TestTodo_ATTEST_002_Mutation(t *testing.T) {
	validTenant := values.TenantId("acme")
	boundTime := values.NewInstant(time.Now().UTC())

	stmt := AttestationStatement{
		ID:      "stmt-mut-001",
		Version: 1,
		Kind:    StatementKindPositional,
		SubjectRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "person-mut-001",
		},
		Attester: AttesterPrincipal{
			PrincipalRef: values.EntityRef{
				Tenant: validTenant,
				Kind:   "person",
				Id:     "attester-mut-001",
			},
			IdentityAssuranceRef: IdentityAssuranceRef{
				ID:   "assurance-mut-001",
				Kind: "CREDENTIAL",
			},
		},
		TextDigest: "sha256:text_mut",
		EvidenceRefs: []EvidenceRef{
			{ID: "doc-001", Digest: "sha256:evidence_a"},
		},
		ValidityWindow: ValidityWindow{
			StartsAt:  boundTime,
			ExpiresAt: values.NewInstant(boundTime.Time().Add(24 * time.Hour)),
		},
		JurisdictionRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "jurisdiction",
			Id:     "us-tx",
		},
	}

	contextDigest := ContextDigest{
		SubjectAsOf:      "sha256:subject_state_mut",
		Jurisdiction:     "us-tx",
		Locale:           "en-US",
		RenderedTextHash: "sha256:rendered_text_mut",
	}

	baseBinding := Binding{
		StatementID:      stmt.ID,
		StatementVersion: stmt.Version,
		ContextDigest:    contextDigest,
		EvidenceBindings: []EvidenceBinding{
			{EvidenceID: "doc-001", EvidenceHash: "sha256:evidence_a"},
		},
		BindingVersion: 1,
		BoundAt:        boundTime,
		Signer: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "signer-mut-001",
		},
	}

	tests := []struct {
		name      string
		mutate    func(*Binding)
		shouldErr bool
	}{
		{
			name: "empty statement id",
			mutate: func(b *Binding) {
				b.StatementID = ""
			},
			shouldErr: true,
		},
		{
			name: "zero statement version",
			mutate: func(b *Binding) {
				b.StatementVersion = 0
			},
			shouldErr: true,
		},
		{
			name: "zero binding version",
			mutate: func(b *Binding) {
				b.BindingVersion = 0
			},
			shouldErr: true,
		},
		{
			name: "empty evidence bindings",
			mutate: func(b *Binding) {
				b.EvidenceBindings = []EvidenceBinding{}
			},
			shouldErr: true,
		},
		{
			name: "empty signer",
			mutate: func(b *Binding) {
				b.Signer = values.EntityRef{}
			},
			shouldErr: true,
		},
		{
			name: "invalid context (empty subject as-of)",
			mutate: func(b *Binding) {
				b.ContextDigest.SubjectAsOf = ""
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := baseBinding
			tt.mutate(&binding)
			err := binding.Validate()
			if !tt.shouldErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.shouldErr && err == nil {
				t.Fatalf("expected error but got none")
			}
		})
	}

	t.Log("PASS: TestTodo_ATTEST_002_Mutation")
}

// TestTodo_ATTEST_002_Security tests that Verify properly detects drift in
// evidence, context, and statement, and names the exact drift reason.
func TestTodo_ATTEST_002_Security(t *testing.T) {
	validTenant := values.TenantId("acme")
	boundTime := values.NewInstant(time.Now().UTC())

	stmt := AttestationStatement{
		ID:      "stmt-sec-001",
		Version: 1,
		Kind:    StatementKindCertification,
		SubjectRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "person-sec-001",
		},
		Attester: AttesterPrincipal{
			PrincipalRef: values.EntityRef{
				Tenant: validTenant,
				Kind:   "person",
				Id:     "attester-sec-001",
			},
			IdentityAssuranceRef: IdentityAssuranceRef{
				ID:   "assurance-sec-001",
				Kind: "CREDENTIAL",
			},
		},
		TextDigest: "sha256:text_sec",
		EvidenceRefs: []EvidenceRef{
			{ID: "doc-001", Digest: "sha256:evidence_a"},
			{ID: "doc-002", Digest: "sha256:evidence_b"},
		},
		ValidityWindow: ValidityWindow{
			StartsAt:  boundTime,
			ExpiresAt: values.NewInstant(boundTime.Time().Add(24 * time.Hour)),
		},
		JurisdictionRef: values.EntityRef{
			Tenant: validTenant,
			Kind:   "jurisdiction",
			Id:     "us-fl",
		},
	}

	contextDigest := ContextDigest{
		SubjectAsOf:      "sha256:subject_state_sec",
		Jurisdiction:     "us-fl",
		Locale:           "en-US",
		RenderedTextHash: "sha256:rendered_text_sec",
	}

	binding := Binding{
		StatementID:      stmt.ID,
		StatementVersion: stmt.Version,
		ContextDigest:    contextDigest,
		EvidenceBindings: []EvidenceBinding{
			{EvidenceID: "doc-001", EvidenceHash: "sha256:evidence_a"},
			{EvidenceID: "doc-002", EvidenceHash: "sha256:evidence_b"},
		},
		BindingVersion: 1,
		BoundAt:        boundTime,
		Signer: values.EntityRef{
			Tenant: validTenant,
			Kind:   "person",
			Id:     "signer-sec-001",
		},
	}

	tests := []struct {
		name                string
		modifyStmt          func(*AttestationStatement)
		modifyContext       func(*ContextDigest)
		modifyEvidenceRefs  func(*[]EvidenceRef)
		expectedValid       bool
		expectedDriftReason VerifyDriftReason
	}{
		{
			name:                "no drift: binding is valid",
			modifyStmt:          nil,
			modifyContext:       nil,
			modifyEvidenceRefs:  nil,
			expectedValid:       true,
			expectedDriftReason: DriftNone,
		},
		{
			name: "statement version changed",
			modifyStmt: func(s *AttestationStatement) {
				s.Version = 2
			},
			expectedValid:       false,
			expectedDriftReason: DriftStatementModified,
		},
		{
			name: "subject as-of changed",
			modifyContext: func(cd *ContextDigest) {
				cd.SubjectAsOf = "sha256:subject_state_changed"
			},
			expectedValid:       false,
			expectedDriftReason: DriftSubjectAsOf,
		},
		{
			name: "jurisdiction changed",
			modifyContext: func(cd *ContextDigest) {
				cd.Jurisdiction = "us-ca"
			},
			expectedValid:       false,
			expectedDriftReason: DriftJurisdiction,
		},
		{
			name: "locale changed",
			modifyContext: func(cd *ContextDigest) {
				cd.Locale = "fr-FR"
			},
			expectedValid:       false,
			expectedDriftReason: DriftLocale,
		},
		{
			name: "rendered text changed",
			modifyContext: func(cd *ContextDigest) {
				cd.RenderedTextHash = "sha256:rendered_text_changed"
			},
			expectedValid:       false,
			expectedDriftReason: DriftRenderedText,
		},
		{
			name: "evidence modified",
			modifyEvidenceRefs: func(refs *[]EvidenceRef) {
				(*refs)[0].Digest = "sha256:evidence_a_modified"
			},
			expectedValid:       false,
			expectedDriftReason: DriftEvidenceModified,
		},
		{
			name: "evidence removed",
			modifyEvidenceRefs: func(refs *[]EvidenceRef) {
				*refs = (*refs)[1:] // Remove first evidence
			},
			expectedValid:       false,
			expectedDriftReason: DriftEvidenceRemoved,
		},
		{
			name: "evidence added",
			modifyEvidenceRefs: func(refs *[]EvidenceRef) {
				*refs = append(*refs, EvidenceRef{ID: "doc-003", Digest: "sha256:evidence_c"})
			},
			expectedValid:       false,
			expectedDriftReason: DriftEvidenceAdded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmtCopy := stmt
			if tt.modifyStmt != nil {
				tt.modifyStmt(&stmtCopy)
			}

			contextCopy := contextDigest
			if tt.modifyContext != nil {
				tt.modifyContext(&contextCopy)
			}

			evidenceRefsCopy := make([]EvidenceRef, len(stmt.EvidenceRefs))
			copy(evidenceRefsCopy, stmt.EvidenceRefs)
			if tt.modifyEvidenceRefs != nil {
				tt.modifyEvidenceRefs(&evidenceRefsCopy)
			}

			result := binding.Verify(stmtCopy, contextCopy, evidenceRefsCopy)
			if result.Valid != tt.expectedValid {
				t.Errorf("expected valid=%v, got %v (detail: %s)", tt.expectedValid, result.Valid, result.Detail)
			}
			if result.Reason != tt.expectedDriftReason {
				t.Errorf("expected drift reason %v, got %v", tt.expectedDriftReason, result.Reason)
			}
		})
	}

	t.Log("PASS: TestTodo_ATTEST_002_Security")
}
