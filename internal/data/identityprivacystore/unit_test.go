package identityprivacystore

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/proofing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestIdentityPrivacyStore_HelperValidationAndEncoding(t *testing.T) {
	if _, err := parseTenant(""); err == nil {
		t.Fatal("empty tenant was accepted")
	}
	if _, err := parseTenant(uuid.Nil.String()); err == nil {
		t.Fatal("nil tenant was accepted")
	}
	validTenant := uuid.New()
	if got, err := parseTenant(validTenant.String()); err != nil || got != validTenant {
		t.Fatalf("parseTenant = %v, %v", got, err)
	}
	if _, err := subjectUUID(values.EntityRef{Id: "bad"}); err == nil {
		t.Fatal("malformed subject was accepted")
	}
	worker := values.EntityRef{Tenant: "subject-tenant", Kind: "worker", Id: uuid.New().String()}
	if got, err := subjectUUID(worker); err != nil || got.String() != worker.Id {
		t.Fatalf("subjectUUID = %v, %v", got, err)
	}
	encoded, err := encodeSource(worker, "source://record")
	if err != nil {
		t.Fatal(err)
	}
	tenant, kind, source := decodeSource(encoded, "fallback")
	if tenant != worker.Tenant.String() || kind != worker.Kind.String() || source != "source://record" {
		t.Fatalf("decoded source = %q/%q/%q", tenant, kind, source)
	}
	if tenant, kind, source := decodeSource("not-encoded", "fallback"); tenant != "fallback" || kind != "worker" || source != "not-encoded" {
		t.Fatalf("fallback source = %q/%q/%q", tenant, kind, source)
	}
	if tenant, kind, source := decodeSource("hcmnext.subject.v1:!", "fallback"); tenant != "fallback" || kind != "worker" || source != "hcmnext.subject.v1:!" {
		t.Fatalf("malformed source = %q/%q/%q", tenant, kind, source)
	}
	if nullableString(" ") != nil || nullableString("x") != "x" || nullableOutcome("") != nil || nullableOutcome(proofing.OutcomeVerified) != string(proofing.OutcomeVerified) || nullableRevision(0) != nil || nullableRevision(2) != uint64(2) {
		t.Fatal("nullable helper changed optional values")
	}
	if dateValue(values.LocalDate{}) != "" {
		t.Fatalf("dateValue zero = %v", dateValue(values.LocalDate{}))
	}
	if got := storageDigest("sha256:abc"); got != "abc" || domainDigest("abc") != "sha256:abc" || domainDigest("sha256:abc") != "sha256:abc" {
		t.Fatal("digest conversion was not stable")
	}
	if _, err := localDate(nil); err == nil {
		t.Fatal("nil date was accepted")
	}
	date, err := localDate(func() *time.Time { value := time.Date(2026, 3, 4, 22, 0, 0, 0, time.UTC); return &value }())
	if err != nil || date.String() != "2026-03-04" {
		t.Fatalf("localDate = %v, %v", date, err)
	}
}

func TestIdentityPrivacyStore_CheckCASBranches(t *testing.T) {
	tests := []struct {
		name       string
		expected   uint64
		actual     uint64
		found      bool
		supersedes uint64
		want       proofing.StoreErrorCode
	}{
		{name: "initial accepted", want: ""},
		{name: "initial cannot supersede", supersedes: 1, want: proofing.StoreInvalidCode},
		{name: "initial duplicate", actual: 1, found: true, want: proofing.StoreStaleCASCode},
		{name: "missing expected revision", expected: 1, supersedes: 1, want: proofing.StoreStaleCASCode},
		{name: "wrong expected revision", expected: 2, actual: 1, found: true, supersedes: 2, want: proofing.StoreStaleCASCode},
		{name: "matching successor", expected: 1, actual: 1, found: true, supersedes: 1, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCAS(tt.expected, tt.actual, tt.found, tt.supersedes, "session")
			if tt.want == "" {
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				return
			}
			var storeErr *proofing.StoreError
			if !errors.As(err, &storeErr) {
				t.Fatalf("error = %v, want typed store error", err)
			}
			if storeErr.Code != tt.want {
				t.Fatalf("store error code = %q, want %q", storeErr.Code, tt.want)
			}
		})
	}
}
