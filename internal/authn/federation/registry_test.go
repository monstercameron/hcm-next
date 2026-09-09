package federation_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/federation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	tenantAcme  = values.TenantId("acme-corp")
	tenantOther = values.TenantId("other-corp")
	issuerAcme  = "https://login.acme.invalid/"
)

func baseProfile() federation.Profile {
	return federation.Profile{
		Tenant:        tenantAcme,
		Issuer:        issuerAcme,
		Protocol:      federation.ProtocolOIDC,
		Version:       1,
		Owner:         "identity-team",
		Scopes:        []string{"openid", "profile"},
		Assurance:     trust.AssuranceSubstantial,
		Audience:      "hcm-next-api",
		Algorithms:    []string{"RS256"},
		IssuedAt:      time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:     time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC),
		MetadataUntil: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		KeyVersion:    1,
	}
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	t.Parallel()
	r := federation.New()
	p := baseProfile()
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := r.Lookup(tenantAcme, issuerAcme)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Owner != p.Owner || got.Version != p.Version {
		t.Fatalf("Lookup = %+v, want owner/version matching %+v", got, p)
	}
}

func TestRegistryLookupUnknownIssuer(t *testing.T) {
	t.Parallel()
	r := federation.New()
	if _, err := r.Lookup(tenantAcme, "https://never-registered.invalid/"); !errors.Is(err, federation.ErrUnknownIssuer) {
		t.Fatalf("Lookup error = %v, want ErrUnknownIssuer", err)
	}
}

func TestRegistryLookupWrongTenant(t *testing.T) {
	t.Parallel()
	r := federation.New()
	if err := r.Register(baseProfile()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := r.Lookup(tenantOther, issuerAcme); !errors.Is(err, federation.ErrWrongTenant) {
		t.Fatalf("Lookup error = %v, want ErrWrongTenant", err)
	}
}

func TestRegistryRegisterRejectsInvalidProfile(t *testing.T) {
	t.Parallel()
	r := federation.New()
	bad := baseProfile()
	bad.Owner = ""
	if err := r.Register(bad); !errors.Is(err, federation.ErrInvalidProfile) {
		t.Fatalf("Register error = %v, want ErrInvalidProfile", err)
	}
}

func TestRegistryRegisterRejectsStaleOrEqualVersion(t *testing.T) {
	t.Parallel()
	r := federation.New()
	p := baseProfile()
	if err := r.Register(p); err != nil {
		t.Fatalf("Register v1: %v", err)
	}
	if err := r.Register(p); !errors.Is(err, federation.ErrVersionConflict) {
		t.Fatalf("re-Register same version error = %v, want ErrVersionConflict", err)
	}
}

func TestRegistryReplaceCompareAndSwap(t *testing.T) {
	t.Parallel()
	r := federation.New()
	p := baseProfile()
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	next := p
	next.Version = 2
	next.KeyVersion = 2
	if err := r.Replace(next, 1); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := r.Lookup(tenantAcme, issuerAcme)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Version != 2 || got.KeyVersion != 2 {
		t.Fatalf("Lookup after Replace = %+v, want version/key version 2", got)
	}
}

func TestRegistryReplaceRejectsWrongWant(t *testing.T) {
	t.Parallel()
	r := federation.New()
	p := baseProfile()
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	next := p
	next.Version = 2
	if err := r.Replace(next, 99); !errors.Is(err, federation.ErrVersionConflict) {
		t.Fatalf("Replace error = %v, want ErrVersionConflict", err)
	}
}

func TestRegistryReplaceUnknownIssuer(t *testing.T) {
	t.Parallel()
	r := federation.New()
	p := baseProfile()
	p.Version = 2
	if err := r.Replace(p, 1); !errors.Is(err, federation.ErrUnknownIssuer) {
		t.Fatalf("Replace error = %v, want ErrUnknownIssuer", err)
	}
}

func TestRegistryValidateAcceptsMatchingAssertion(t *testing.T) {
	t.Parallel()
	r := federation.New()
	if err := r.Register(baseProfile()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a := federation.Assertion{
		Tenant: tenantAcme, Issuer: issuerAcme, Algorithm: "RS256",
		Audience: "hcm-next-api", At: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
	if _, err := r.Validate(a); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRegistryValidateRejectsUnknownAlgorithm(t *testing.T) {
	t.Parallel()
	r := federation.New()
	if err := r.Register(baseProfile()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a := federation.Assertion{Tenant: tenantAcme, Issuer: issuerAcme, Algorithm: "HS256", Audience: "hcm-next-api"}
	if _, err := r.Validate(a); !errors.Is(err, federation.ErrAlgorithm) {
		t.Fatalf("Validate error = %v, want ErrAlgorithm", err)
	}
}

func TestRegistryValidateRejectsWrongAudience(t *testing.T) {
	t.Parallel()
	r := federation.New()
	if err := r.Register(baseProfile()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a := federation.Assertion{Tenant: tenantAcme, Issuer: issuerAcme, Algorithm: "RS256", Audience: "wrong-audience"}
	if _, err := r.Validate(a); !errors.Is(err, federation.ErrAudience) {
		t.Fatalf("Validate error = %v, want ErrAudience", err)
	}
}

func TestRegistryValidateRejectsStaleMetadata(t *testing.T) {
	t.Parallel()
	r := federation.New()
	if err := r.Register(baseProfile()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a := federation.Assertion{
		Tenant: tenantAcme, Issuer: issuerAcme, Algorithm: "RS256", Audience: "hcm-next-api",
		At: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), // past MetadataUntil
	}
	if _, err := r.Validate(a); !errors.Is(err, federation.ErrStaleMetadata) {
		t.Fatalf("Validate error = %v, want ErrStaleMetadata", err)
	}
}

func TestRegistryValidateRejectsKeyRollover(t *testing.T) {
	t.Parallel()
	r := federation.New()
	if err := r.Register(baseProfile()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a := federation.Assertion{
		Tenant: tenantAcme, Issuer: issuerAcme, Algorithm: "RS256", Audience: "hcm-next-api",
		At: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), KeyVersion: 999,
	}
	if _, err := r.Validate(a); !errors.Is(err, federation.ErrKeyRollover) {
		t.Fatalf("Validate error = %v, want ErrKeyRollover", err)
	}
}

func TestRegistryValidateRejectsWrongTenant(t *testing.T) {
	t.Parallel()
	r := federation.New()
	if err := r.Register(baseProfile()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a := federation.Assertion{Tenant: tenantOther, Issuer: issuerAcme, Algorithm: "RS256", Audience: "hcm-next-api"}
	if _, err := r.Validate(a); !errors.Is(err, federation.ErrWrongTenant) {
		t.Fatalf("Validate error = %v, want ErrWrongTenant", err)
	}
}

func TestRegistryValidateInvalidTenant(t *testing.T) {
	t.Parallel()
	r := federation.New()
	a := federation.Assertion{Tenant: "", Issuer: issuerAcme, Algorithm: "RS256", Audience: "hcm-next-api"}
	if _, err := r.Validate(a); !errors.Is(err, federation.ErrInvalidProfile) {
		t.Fatalf("Validate error = %v, want ErrInvalidProfile", err)
	}
}

func TestNewRegistryAlias(t *testing.T) {
	t.Parallel()
	r := federation.NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
}

func TestProfileValidationBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*federation.Profile)
	}{
		{"tenant", func(p *federation.Profile) { p.Tenant = "" }},
		{"issuer", func(p *federation.Profile) { p.Issuer = " " }},
		{"version", func(p *federation.Profile) { p.Version = 0 }},
		{"owner", func(p *federation.Profile) { p.Owner = "\t" }},
		{"protocol", func(p *federation.Profile) { p.Protocol = "saml2" }},
		{"audience", func(p *federation.Profile) { p.Audience = "" }},
		{"algorithms_empty", func(p *federation.Profile) { p.Algorithms = nil }},
		{"algorithm_blank", func(p *federation.Profile) { p.Algorithms = []string{""} }},
		{"algorithm_duplicate", func(p *federation.Profile) { p.Algorithms = []string{"RS256", "RS256"} }},
		{"assurance", func(p *federation.Profile) { p.Assurance = trust.AssuranceUnspecified }},
		{"expiry", func(p *federation.Profile) { p.ExpiresAt = time.Time{} }},
		{"key_version", func(p *federation.Profile) { p.KeyVersion = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := baseProfile()
			tc.mutate(&p)
			if err := federation.New().Register(p); !errors.Is(err, federation.ErrInvalidProfile) {
				t.Fatalf("Register error = %v, want ErrInvalidProfile", err)
			}
		})
	}
}

func TestRegistryValidateVersionAndFreshnessBoundaries(t *testing.T) {
	r := federation.New()
	p := baseProfile()
	if err := r.Register(p); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*federation.Assertion)
		want error
	}{
		{"profile_rollover", func(a *federation.Assertion) { a.ProfileVersion = 2 }, federation.ErrVersionConflict},
		{"key_rollover", func(a *federation.Assertion) { a.KeyVersion = 2 }, federation.ErrKeyRollover},
		{"before_issued", func(a *federation.Assertion) { a.At = p.IssuedAt.Add(-time.Nanosecond) }, federation.ErrStaleMetadata},
		{"at_metadata_expiry", func(a *federation.Assertion) { a.At = p.MetadataUntil }, federation.ErrStaleMetadata},
		{"at_profile_expiry", func(a *federation.Assertion) { a.At = p.ExpiresAt }, federation.ErrStaleMetadata},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := federation.Assertion{Tenant: tenantAcme, Issuer: issuerAcme, Algorithm: "RS256", Audience: p.Audience, At: p.IssuedAt}
			tc.edit(&a)
			if _, err := r.Validate(a); !errors.Is(err, tc.want) {
				t.Fatalf("Validate error = %v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
}

func TestRegistryDefensiveCopiesAndReplaceValidation(t *testing.T) {
	r := federation.New()
	p := baseProfile()
	if err := r.Register(p); err != nil {
		t.Fatal(err)
	}
	got, err := r.Lookup(tenantAcme, issuerAcme)
	if err != nil {
		t.Fatal(err)
	}
	got.Scopes[0], got.Algorithms[0] = "tampered", "HS256"
	again, err := r.Lookup(tenantAcme, issuerAcme)
	if err != nil || again.Scopes[0] != "openid" || again.Algorithms[0] != "RS256" {
		t.Fatalf("registry stored mutable slices: got=%+v err=%v", again, err)
	}
	bad := p
	bad.Version = 2
	bad.Owner = ""
	if err := r.Replace(bad, 1); !errors.Is(err, federation.ErrInvalidProfile) {
		t.Fatalf("invalid Replace error = %v, want ErrInvalidProfile", err)
	}
	if err := r.Replace(p, 99); !errors.Is(err, federation.ErrVersionConflict) {
		t.Fatalf("wrong-version Replace error = %v, want ErrVersionConflict", err)
	}
	if _, err := r.Lookup(tenantAcme, " "); !errors.Is(err, federation.ErrUnknownIssuer) {
		t.Fatalf("blank issuer Lookup error = %v, want ErrUnknownIssuer", err)
	}
}

// TestTodo_AUTHN_001_Race_FederationProfileRegistry exercises this package's
// [federation.Registry] under concurrent Register/Lookup/Validate calls to
// prove the sync.RWMutex it holds actually serializes mutation against
// readers -- the gap tools/policy/racepolicy flags for any package that
// imports "sync" with no test file that would run at all. It is a race
// *coverage* test (this environment does not run go test -race on
// windows/arm64); go.mod's own CI job races it for real on linux/amd64.
func TestTodo_AUTHN_001_Race_FederationProfileRegistry(t *testing.T) {
	r := federation.New()
	if err := r.Register(baseProfile()); err != nil {
		t.Fatalf("seed Register: %v", err)
	}

	var wg sync.WaitGroup
	const writers, readers = 4, 8
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(version uint64) {
			defer wg.Done()
			p := baseProfile()
			p.Version = version
			p.KeyVersion = version
			_ = r.Register(p) // a version conflict from a concurrent writer is expected and fine
		}(uint64(w + 2))
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Lookup(tenantAcme, issuerAcme)
			_, _ = r.Validate(federation.Assertion{
				Tenant: tenantAcme, Issuer: issuerAcme, Algorithm: "RS256", Audience: "hcm-next-api",
				At: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
			})
		}()
	}
	wg.Wait()

	got, err := r.Lookup(tenantAcme, issuerAcme)
	if err != nil {
		t.Fatalf("Lookup after concurrent access: %v", err)
	}
	if got.Version < 1 {
		t.Fatalf("Lookup after concurrent access = %+v, want a registered profile", got)
	}
}
