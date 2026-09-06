package issuerregistry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

// FuzzTodo_AUTHN_001 fuzzes [issuerregistry.Publish]'s input surface --
// tenant, issuer URL, audience, algorithm and clock-skew/staleness bounds
// -- against a freshly published [issuerregistry.MemoryStore]. It asserts
// the two properties [Issuer.validate] must hold for *any* input:
//
//  1. Publish never panics.
//  2. Every refusal unwraps to one of this package's declared sentinel
//     errors -- never a bare, unclassified error a caller could not match.
//
// It deliberately does not assert that a specific input is accepted or
// refused (that is issuer_test.go's job with concrete cases); a fuzz target
// proves the validation surface is total and closed, not that any one rule
// fires correctly.
func FuzzTodo_AUTHN_001(f *testing.F) {
	seeds := []struct {
		tenant, issuerURL, audience, algorithm string
		clockSkewSeconds, stalenessSeconds     int64
	}{
		{"acme-corp", "https://login.acme.invalid/", "hcm-next-api", "RS256", 30, 86400},
		{"", "https://login.acme.invalid/", "hcm-next-api", "RS256", 30, 86400},
		{"acme-corp", "", "hcm-next-api", "RS256", 30, 86400},
		{"acme-corp", "not a url", "hcm-next-api", "RS256", 30, 86400},
		{"acme-corp", "https://login.acme.invalid/", "", "RS256", 30, 86400},
		{"acme-corp", "https://login.acme.invalid/", "hcm-next-api", "HS256", 30, 86400},
		{"acme-corp", "https://login.acme.invalid/", "hcm-next-api", "", 30, 86400},
		{"acme-corp", "https://login.acme.invalid/", "hcm-next-api", "RS256", -1, 86400},
		{"acme-corp", "https://login.acme.invalid/", "hcm-next-api", "RS256", 30, 0},
		{"acme-corp", "https://login.acme.invalid/", "hcm-next-api", "RS256", 30, -1},
		{"AB", "https://x/", "a", "ES256", 0, 1},
		{"acme-corp", "https://login.acme.invalid/\x00", "hcm-next-api", "EdDSA", 30, 86400},
	}
	for _, s := range seeds {
		f.Add(s.tenant, s.issuerURL, s.audience, s.algorithm, s.clockSkewSeconds, s.stalenessSeconds)
	}

	knownErrors := []error{
		issuerregistry.ErrInvalidIssuer, issuerregistry.ErrJWKSNotPinned,
		issuerregistry.ErrNoAlgorithms, issuerregistry.ErrAlgorithmNotAllowed,
		issuerregistry.ErrRevisionConflict,
	}

	f.Fuzz(func(t *testing.T, tenant, issuerURL, audience, algorithm string, clockSkewSeconds, stalenessSeconds int64) {
		// Bound the fuzzer's duration inputs so a pathological int64 does
		// not overflow time.Duration multiplication in a way unrelated to
		// what this target is proving.
		if clockSkewSeconds > 1<<20 || clockSkewSeconds < -(1<<20) {
			clockSkewSeconds = clockSkewSeconds % (1 << 20)
		}
		if stalenessSeconds > 1<<20 || stalenessSeconds < -(1<<20) {
			stalenessSeconds = stalenessSeconds % (1 << 20)
		}

		store := issuerregistry.NewMemoryStore()
		issuer := issuerregistry.Issuer{
			Tenant:    values.TenantId(tenant),
			IssuerURL: issuerURL,
			Audience:  audience,
			JWKS: issuerregistry.JWKSSource{
				Kind:       issuerregistry.JWKSSourcePinnedKeys,
				PinnedKeys: []issuerregistry.PinnedKey{testFuzzPinnedKey(t, algorithm)},
			},
			Algorithms:         []trustfederation.Algorithm{trustfederation.Algorithm(algorithm)},
			ClockSkew:          time.Duration(clockSkewSeconds) * time.Second,
			MetadataStaleness:  time.Duration(stalenessSeconds) * time.Second,
			Revision:           1,
			PublisherPrincipal: "fuzz-publisher",
			PublishedAt:        baseTime,
		}

		_, err := issuerregistry.Publish(store, issuer)
		if err == nil {
			return
		}
		for _, known := range knownErrors {
			if errors.Is(err, known) {
				return
			}
		}
		t.Fatalf("Publish(%+v) returned an unclassified error: %v", issuer, err)
	})
}

// testFuzzPinnedKey returns a syntactically valid pinned key whose declared
// algorithm is whatever the fuzz corpus supplied -- letting
// [issuerregistry.Issuer.validate]'s own algorithm-closed-set check (not a
// key-parsing failure) be the thing that refuses an out-of-set algorithm.
func testFuzzPinnedKey(t *testing.T, algorithm string) issuerregistry.PinnedKey {
	t.Helper()
	return issuerregistry.PinnedKey{
		KeyID:        "fuzz-kid",
		Algorithm:    trustfederation.Algorithm(algorithm),
		PublicKeyDER: testRSAPublicKeyDER(t),
	}
}
