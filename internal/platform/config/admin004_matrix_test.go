package config_test

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
)

// TestTodo_ADMIN_004 pins the center's immutable, scoped evidence boundary:
// the only evidence that can be resolved is a signed object with an exact
// digest and target scope.
func TestTodo_ADMIN_004(t *testing.T) {
	r, root := compilerFixture(t)
	b, err := config.Compile(r, config.CompileOptions{
		BundleID: "admin-004", Roots: []config.ObjectRef{root},
		TargetScope: "tenant/a", MinimumRuntimeVersion: "runtime/1",
		CompatibilityRange: ">=1,<2", Signer: "operator-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.TargetScope != "tenant/a" || len(b.Dependencies) != 2 {
		t.Fatalf("compiled evidence lost scope or closure: %#v", b)
	}
	if _, err := config.BundleDigest(b); err != nil {
		t.Fatalf("compiled evidence is not digestible: %v", err)
	}
}

// TestTodo_ADMIN_004_Security proves authorization and scope are fail-closed
// and that detached signatures cannot be accepted under another key.
func TestTodo_ADMIN_004_Security(t *testing.T) {
	r, root := compilerFixture(t)
	_, err := config.Compile(r, config.CompileOptions{
		BundleID: "admin-004", Roots: []config.ObjectRef{root},
		TargetScope: "tenant/other", MinimumRuntimeVersion: "runtime/1",
		CompatibilityRange: ">=1", Signer: "operator-key",
	})
	if !errors.Is(err, config.ErrScopeMismatch) {
		t.Fatalf("cross-tenant compile = %v, want scope denial", err)
	}

	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	bundle := config.Bundle{BundleID: "admin-004", ManifestVersion: 1,
		CompatibilityRange: ">=1", Signer: "operator-key", TargetScope: "tenant/a",
		MinimumRuntimeVersion: "runtime/1"}
	signed, err := config.SignBundle(bundle, "operator-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	otherSeed := make([]byte, ed25519.SeedSize)
	otherSeed[0] = 1
	other := ed25519.NewKeyFromSeed(otherSeed)
	// A different alternate key must not be treated as authorization.
	if status, _ := config.VerifyBundle(signed, other.Public().(ed25519.PublicKey)); status == config.VerifyStatusValid {
		t.Fatal("bundle accepted under an unauthorized signing key")
	}
}

// TestTodo_ADMIN_004_Redaction proves raw credential material and secret
// values cannot enter the inspect/export representation.
func TestTodo_ADMIN_004_Redaction(t *testing.T) {
	_, err := config.BundleDigest(config.Bundle{BundleID: "b", ManifestVersion: 1,
		CompatibilityRange: ">=1", Signer: "s", TargetScope: "tenant/a",
		MinimumRuntimeVersion: "runtime/1", CredentialRefs: []string{"raw-secret"}})
	if !errors.Is(err, config.ErrRawCredential) || strings.Contains(err.Error(), "raw-secret") {
		t.Fatalf("raw credential handling leaked or accepted value: %v", err)
	}
	entry, err := config.NewSecretEntry("provider.token", "fingerprint", true, config.SemanticGeneric, config.Refs{})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Value != "" || entry.SecretFingerprint != "fingerprint" {
		t.Fatalf("secret entry is not opaque: %#v", entry)
	}
}

// TestTodo_ADMIN_004_StaleStatus treats changed signed content as tampered;
// an old digest is never a usable configuration status.
func TestTodo_ADMIN_004_StaleStatus(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	b, err := config.SignBundle(config.Bundle{BundleID: "b", ManifestVersion: 1,
		CompatibilityRange: ">=1", Signer: "s", TargetScope: "tenant/a",
		MinimumRuntimeVersion: "runtime/1"}, "key", priv)
	if err != nil {
		t.Fatal(err)
	}
	b.Bundle.CompatibilityRange = ">=2"
	status, err := config.VerifyBundle(b, priv.Public().(ed25519.PublicKey))
	if status != config.VerifyStatusTampered || !errors.Is(err, config.ErrTamperedManifest) {
		t.Fatalf("stale/tampered bundle status = %s, %v", status, err)
	}
}

// TestTodo_ADMIN_004_SafeCommands pins safe failure for missing required
// compilation controls and unresolved provider/config dependencies.
func TestTodo_ADMIN_004_SafeCommands(t *testing.T) {
	r, root := compilerFixture(t)
	cases := []struct {
		name string
		edit func(*config.CompileOptions)
		want error
	}{
		{"missing scope", func(o *config.CompileOptions) { o.TargetScope = "" }, config.ErrMissingTargetScope},
		{"missing runtime", func(o *config.CompileOptions) { o.MinimumRuntimeVersion = "" }, config.ErrMissingRuntimeVersion},
		{"unavailable dependency", func(o *config.CompileOptions) { o.Roots[0].Version = "9.9.9" }, config.ErrUnresolvedDependency},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := config.CompileOptions{BundleID: "b", Roots: []config.ObjectRef{root}, TargetScope: "tenant/a", MinimumRuntimeVersion: "runtime/1", CompatibilityRange: ">=1", Signer: "s"}
			tc.edit(&opts)
			if _, err := config.Compile(r, opts); !errors.Is(err, tc.want) {
				t.Fatalf("compile error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestTodo_ADMIN_004_Integration pins a complete connector/config closure
// round trip through the immutable registry and its content address.
func TestTodo_ADMIN_004_Integration(t *testing.T) {
	r, root := compilerFixture(t)
	b, err := config.Compile(r, config.CompileOptions{BundleID: "b", Roots: []config.ObjectRef{root}, TargetScope: "tenant/a", MinimumRuntimeVersion: "runtime/1", CompatibilityRange: ">=1", Signer: "s"})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := config.BundleDigest(b)
	if err != nil || len(digest) != 64 {
		t.Fatalf("integration evidence digest = %q, %v", digest, err)
	}
}

// TestTodo_ADMIN_004_Fault ensures unavailable dependencies fail closed with
// no partially compiled bundle.
func TestTodo_ADMIN_004_Fault(t *testing.T) {
	r, root := compilerFixture(t)
	root.Digest = strings.Repeat("0", 64)
	b, err := config.Compile(r, config.CompileOptions{BundleID: "b", Roots: []config.ObjectRef{root}, TargetScope: "tenant/a", MinimumRuntimeVersion: "runtime/1", CompatibilityRange: ">=1", Signer: "s"})
	if b.Dependencies != nil || !errors.Is(err, config.ErrDependencyDigestMismatch) {
		t.Fatalf("fault compile returned bundle=%#v error=%v", b, err)
	}
}
