package attestation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestTodo_PERSIST_ATTESTATION_001_DomainMemoryPort(t *testing.T) {
	store := NewMemoryStore()
	tenant := values.TenantId("acme")
	ref := values.EntityRef{Tenant: tenant, Kind: "person", Id: "550e8400-e29b-41d4-a716-446655440000"}
	stmt := AttestationStatement{ID: "statement-1", Version: 1, Kind: StatementKindConsent, SubjectRef: ref,
		Attester:   AttesterPrincipal{PrincipalRef: ref, IdentityAssuranceRef: IdentityAssuranceRef{ID: "assurance", Kind: "CREDENTIAL"}},
		TextDigest: "sha256:text", EvidenceRefs: []EvidenceRef{{ID: "evidence", Digest: "sha256:evidence"}},
		ValidityWindow: ValidityWindow{StartsAt: values.NewInstant(time.Unix(1, 0)), ExpiresAt: values.NewInstant(time.Unix(2, 0))}, JurisdictionRef: ref}
	if err := store.PutStatement(context.Background(), tenant, stmt); err != nil {
		t.Fatal(err)
	}
	if err := store.PutStatement(context.Background(), tenant, stmt); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("duplicate revision error = %v, want version conflict from stale default CAS", err)
	}
	loaded, err := store.GetStatement(context.Background(), tenant, stmt.ID, stmt.Version)
	if err != nil || loaded.Digest() != stmt.Digest() {
		t.Fatalf("loaded statement = %v, %v", loaded, err)
	}
}
