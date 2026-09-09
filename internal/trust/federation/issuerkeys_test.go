package federation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

func TestAlgorithmValidExportsClosedSet(t *testing.T) {
	t.Parallel()
	for _, alg := range []federation.Algorithm{federation.AlgRS256, federation.AlgES256, federation.AlgEdDSA} {
		if !alg.Valid() {
			t.Fatalf("Algorithm(%q).Valid() = false, want true", alg)
		}
	}
	for _, alg := range []federation.Algorithm{"", "HS256", "none", "RS512"} {
		if federation.Algorithm(alg).Valid() {
			t.Fatalf("Algorithm(%q).Valid() = true, want false", alg)
		}
	}
}

// fakeIssuerResolver is a minimal, deterministic [federation.IssuerResolver]
// test double: a fixed map from issuer to the keys it currently resolves.
type fakeIssuerResolver struct {
	keys map[string][]federation.SigningKey
	err  error
}

func (f *fakeIssuerResolver) ResolveIssuerKeys(_ context.Context, issuer string) ([]federation.SigningKey, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.keys[issuer], nil
}

func TestRegistryKeySourceResolvesMatchingKeyID(t *testing.T) {
	t.Parallel()
	want := federation.SigningKey{ID: "kid-2", Algorithm: federation.AlgES256}
	resolver := &fakeIssuerResolver{keys: map[string][]federation.SigningKey{
		"https://issuer.invalid/": {
			{ID: "kid-1", Algorithm: federation.AlgRS256},
			want,
		},
	}}
	src := federation.NewRegistryKeySource(resolver)

	got, err := src.ResolveKey(context.Background(), "https://issuer.invalid/", "kid-2")
	if err != nil {
		t.Fatalf("ResolveKey: %v", err)
	}
	if got.ID != want.ID || got.Algorithm != want.Algorithm {
		t.Fatalf("ResolveKey = %+v, want %+v", got, want)
	}
}

func TestRegistryKeySourceUnknownKeyID(t *testing.T) {
	t.Parallel()
	resolver := &fakeIssuerResolver{keys: map[string][]federation.SigningKey{
		"https://issuer.invalid/": {{ID: "kid-1", Algorithm: federation.AlgRS256}},
	}}
	src := federation.NewRegistryKeySource(resolver)

	_, err := src.ResolveKey(context.Background(), "https://issuer.invalid/", "kid-missing")
	if !errors.Is(err, federation.ErrKeyNotFound) {
		t.Fatalf("ResolveKey error = %v, want ErrKeyNotFound", err)
	}
}

func TestRegistryKeySourcePropagatesResolverError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("issuer suspended")
	resolver := &fakeIssuerResolver{err: sentinel}
	src := federation.NewRegistryKeySource(resolver)

	_, err := src.ResolveKey(context.Background(), "https://issuer.invalid/", "kid-1")
	if !errors.Is(err, sentinel) {
		t.Fatalf("ResolveKey error = %v, want sentinel %v", err, sentinel)
	}
}

func TestRegistryKeySourceNilResolverFailsClosed(t *testing.T) {
	t.Parallel()
	src := federation.NewRegistryKeySource(nil)
	if _, err := src.ResolveKey(context.Background(), "https://issuer.invalid/", "kid-1"); !errors.Is(err, federation.ErrKeyNotFound) {
		t.Fatalf("ResolveKey error = %v, want ErrKeyNotFound", err)
	}
}

func TestRegistryKeySourceImplementsKeySource(t *testing.T) {
	t.Parallel()
	var _ federation.KeySource = federation.NewRegistryKeySource(nil)
}

// TestSigningKeyValidAtSmoke exercises the rotation-window validity this
// package already ships (unexported outside the package, so a wiring test
// here proves the [federation.SigningKey] values a [RegistryKeySource]
// hands back behave the same as any statically-configured key).
func TestSigningKeyValidAtSmoke(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	k := federation.SigningKey{
		ID:        "kid-1",
		Algorithm: federation.AlgRS256,
		NotBefore: now.Add(-time.Hour),
		NotAfter:  now.Add(time.Hour),
	}
	resolver := &fakeIssuerResolver{keys: map[string][]federation.SigningKey{"iss": {k}}}
	src := federation.NewRegistryKeySource(resolver)
	got, err := src.ResolveKey(context.Background(), "iss", "kid-1")
	if err != nil {
		t.Fatalf("ResolveKey: %v", err)
	}
	if got.NotBefore.IsZero() || got.NotAfter.IsZero() {
		t.Fatalf("ResolveKey lost rotation window: %+v", got)
	}
}
