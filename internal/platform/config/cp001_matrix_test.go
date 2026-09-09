package config_test

import (
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
)

// TestTodo_CP_001_Golden pins the published object's identity fields and the
// deterministic content address used by the registry.
func TestTodo_CP_001_Golden(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	first, err := registryObject(t, "1.0.0").Sign("key-1", priv)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registryObject(t, "1.0.0").Sign("key-1", priv)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("equivalent objects have different digests: %s vs %s", first.Digest, second.Digest)
	}
	if first.Kind != config.ObjectWorkflow || first.ID != "onboarding" || first.Version != "1.0.0" || first.Owner != "team:people" || first.Phase != "P1" || first.Scope != "tenant:acme" {
		t.Fatalf("published identity changed: %#v", first)
	}
	if err := first.Verify(); err != nil {
		t.Fatalf("golden object does not verify: %v", err)
	}
}

// TestTodo_CP_001_Integration proves that multiple immutable versions can be
// published and resolved through the same registry without replacement.
func TestTodo_CP_001_Integration(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	r := config.NewRegistry()
	for _, version := range []string{"1.0.0", "1.1.0"} {
		o, err := registryObject(t, version).Sign("key-1", priv)
		if err != nil {
			t.Fatalf("sign %s: %v", version, err)
		}
		if err := r.Register(o); err != nil {
			t.Fatalf("register %s: %v", version, err)
		}
	}
	versions := r.Versions(config.ObjectWorkflow, "onboarding")
	if len(versions) != 2 || versions[0].Version != "1.0.0" || versions[1].Version != "1.1.0" {
		t.Fatalf("versions = %#v, want ordered immutable versions", versions)
	}
	for _, version := range []string{"1.0.0", "1.1.0"} {
		if _, ok := r.Lookup(config.ObjectWorkflow, "onboarding", version); !ok {
			t.Fatalf("exact lookup for %s failed", version)
		}
	}
}

// TestTodo_CP_001_Mutation proves that changing any signed identity-bearing
// field is detected rather than being accepted as the originally published
// object.
func TestTodo_CP_001_Mutation(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	base, err := registryObject(t, "1.0.0").Sign("key-1", priv)
	if err != nil {
		t.Fatal(err)
	}
	mutants := []struct {
		name   string
		mutate func(*config.ConfigObject)
	}{
		{"owner", func(o *config.ConfigObject) { o.Owner = "team:other" }},
		{"scope", func(o *config.ConfigObject) { o.Scope = "tenant:other" }},
		{"content", func(o *config.ConfigObject) { o.Content[0] ^= 1 }},
		{"dependency", func(o *config.ConfigObject) { o.Dependencies[0].Version = "1.1.0" }},
	}
	for _, tc := range mutants {
		t.Run(tc.name, func(t *testing.T) {
			mutant := base
			mutant.Content = append([]byte(nil), base.Content...)
			mutant.Dependencies = append([]config.Dependency(nil), base.Dependencies...)
			tc.mutate(&mutant)
			if !errors.Is(mutant.Verify(), config.ErrTamperedManifest) {
				t.Fatalf("mutated %s object was accepted: %v", tc.name, mutant.Verify())
			}
		})
	}
}
