package issuerregistry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

const (
	tenantAcme  = values.TenantId("acme-corp")
	tenantOther = values.TenantId("other-corp")
	issuerAcme  = "https://login.acme.invalid/"
)

var baseTime = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// validPinnedKey returns a syntactically valid pinned key: a real RSA
// public key DER-encoded as SubjectPublicKeyInfo, generated once per test
// binary run via testMustRSAKey (see helpers_test.go).
func validPinnedKey(t *testing.T, keyID string) issuerregistry.PinnedKey {
	t.Helper()
	return issuerregistry.PinnedKey{
		KeyID:        keyID,
		Algorithm:    trustfederation.AlgRS256,
		PublicKeyDER: testRSAPublicKeyDER(t),
		NotBefore:    baseTime.Add(-time.Hour),
		NotAfter:     baseTime.Add(24 * time.Hour),
	}
}

func validIssuer(t *testing.T) issuerregistry.Issuer {
	t.Helper()
	return issuerregistry.Issuer{
		Tenant:    tenantAcme,
		IssuerURL: issuerAcme,
		Audience:  "hcm-next-api",
		JWKS: issuerregistry.JWKSSource{
			Kind:       issuerregistry.JWKSSourcePinnedKeys,
			PinnedKeys: []issuerregistry.PinnedKey{validPinnedKey(t, "kid-1")},
		},
		Algorithms:         []trustfederation.Algorithm{trustfederation.AlgRS256},
		ClockSkew:          30 * time.Second,
		MetadataStaleness:  24 * time.Hour,
		Revision:           1,
		PublisherPrincipal: "alice",
		PublishedAt:        baseTime,
	}
}

func TestIssuerRefValid(t *testing.T) {
	t.Parallel()
	i := validIssuer(t)
	ref := i.Ref()
	if ref.Tenant != i.Tenant || ref.IssuerURL != i.IssuerURL || ref.Revision != i.Revision {
		t.Fatalf("Ref() = %+v, want it to mirror the issuer's own identity", ref)
	}
}

func TestPublishAcceptsValidIssuer(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	if _, err := issuerregistry.Publish(store, validIssuer(t)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func TestPublishRejectsInvalidTenant(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.Tenant = ""
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsBlankIssuerURL(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.IssuerURL = ""
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsMalformedIssuerURL(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.IssuerURL = "not a url"
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsBlankAudience(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.Audience = ""
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsUnpinnedJWKSSourceKindPinnedKeys(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.JWKS = issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys}
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrJWKSNotPinned) {
		t.Fatalf("Publish error = %v, want ErrJWKSNotPinned", err)
	}
}

func TestPublishRejectsUnpinnedJWKSSourceKindPinnedBundle(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.JWKS = issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedBundle} // no BundleRef/Version
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrJWKSNotPinned) {
		t.Fatalf("Publish error = %v, want ErrJWKSNotPinned", err)
	}
}

func TestPublishRejectsUnknownJWKSSourceKind(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.JWKS.Kind = "LIVE_DISCOVERY"
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishAcceptsPinnedBundleSource(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.JWKS = issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedBundle, BundleRef: "acme-ca", BundleVersion: 1}
	if _, err := issuerregistry.Publish(store, i); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func TestPublishRejectsDuplicatePinnedKeyID(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.JWKS.PinnedKeys = append(i.JWKS.PinnedKeys, validPinnedKey(t, "kid-1"))
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsPinnedKeyWithUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.JWKS.PinnedKeys[0].Algorithm = "HS256"
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsPinnedKeyWithNoMaterial(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.JWKS.PinnedKeys[0].PublicKeyDER = nil
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsPinnedKeyWithMalformedMaterial(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.JWKS.PinnedKeys[0].PublicKeyDER = []byte("not a der-encoded key")
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsNoAlgorithms(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.Algorithms = nil
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrNoAlgorithms) {
		t.Fatalf("Publish error = %v, want ErrNoAlgorithms", err)
	}
}

func TestPublishRejectsAlgorithmOutsideClosedSet(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.Algorithms = []trustfederation.Algorithm{"HS256"}
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrAlgorithmNotAllowed) {
		t.Fatalf("Publish error = %v, want ErrAlgorithmNotAllowed", err)
	}
}

func TestPublishRejectsDuplicateAlgorithm(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.Algorithms = []trustfederation.Algorithm{trustfederation.AlgRS256, trustfederation.AlgRS256}
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsNegativeClockSkew(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.ClockSkew = -time.Second
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishAcceptsZeroClockSkew(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.ClockSkew = 0
	if _, err := issuerregistry.Publish(store, i); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func TestPublishRejectsNonPositiveStaleness(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.MetadataStaleness = 0
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsZeroRevision(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.Revision = 0
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsBlankPublisher(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.PublisherPrincipal = ""
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestPublishRejectsZeroPublishedAt(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.PublishedAt = time.Time{}
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestClaimMappingRejectsUnknownTarget(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.ClaimMappings = []issuerregistry.ClaimMapping{{SourceClaim: "cognito:groups", Target: "not_a_field"}}
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestClaimMappingRejectsBlankSource(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.ClaimMappings = []issuerregistry.ClaimMapping{{SourceClaim: "", Target: issuerregistry.PrincipalFieldRoles}}
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestClaimMappingRejectsDuplicateSource(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.ClaimMappings = []issuerregistry.ClaimMapping{
		{SourceClaim: "grp", Target: issuerregistry.PrincipalFieldRoles},
		{SourceClaim: "grp", Target: issuerregistry.PrincipalFieldPurposes},
	}
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestClaimMappingRejectsDuplicateTarget(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.ClaimMappings = []issuerregistry.ClaimMapping{
		{SourceClaim: "grp", Target: issuerregistry.PrincipalFieldRoles},
		{SourceClaim: "groups", Target: issuerregistry.PrincipalFieldRoles},
	}
	if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrInvalidIssuer) {
		t.Fatalf("Publish error = %v, want ErrInvalidIssuer", err)
	}
}

func TestClaimMappingAcceptsDistinctFields(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	i := validIssuer(t)
	i.ClaimMappings = []issuerregistry.ClaimMapping{
		{SourceClaim: "grp", Target: issuerregistry.PrincipalFieldRoles},
		{SourceClaim: "org", Target: issuerregistry.PrincipalFieldOrganizationScopeID},
	}
	if _, err := issuerregistry.Publish(store, i); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}
