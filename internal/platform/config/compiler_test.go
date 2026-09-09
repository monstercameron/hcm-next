package config_test

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
)

func compilerObject(t *testing.T, kind config.ObjectKind, id, version string, deps []config.Dependency) config.ConfigObject {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	o := config.ConfigObject{Kind: kind, ID: id, Version: version, Owner: "platform", Phase: "published", Scope: "tenant/a", EffectiveFrom: time.Unix(1, 0).UTC(), Content: []byte(id + version), Dependencies: deps}
	signed, err := o.Sign("test", priv)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func compilerRef(t *testing.T, o config.ConfigObject) config.ObjectRef {
	t.Helper()
	d, err := o.DigestValue()
	if err != nil {
		t.Fatal(err)
	}
	return config.ObjectRef{Kind: o.Kind, ID: o.ID, Version: o.Version, Digest: d}
}

func compilerFixture(t *testing.T) (*config.Registry, config.ObjectRef) {
	t.Helper()
	r := config.NewRegistry()
	leaf := compilerObject(t, config.ObjectSchema, "schema", "1.0.0", nil)
	ld, _ := leaf.DigestValue()
	root := compilerObject(t, config.ObjectWorkflow, "workflow", "2.0.0", []config.Dependency{{Kind: config.DependencySchema, Name: "schema", Version: "1.0.0", Digest: ld}})
	if err := r.Register(leaf); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(root); err != nil {
		t.Fatal(err)
	}
	return r, compilerRef(t, root)
}

// TestTodo_CP_002 proves complete, pinned, scope-bound closure compilation.
func TestTodo_CP_002(t *testing.T) {
	r, root := compilerFixture(t)
	b, err := config.Compile(r, config.CompileOptions{BundleID: "b", Roots: []config.ObjectRef{root}, TargetScope: "tenant/a", MinimumRuntimeVersion: "runtime/1", CompatibilityRange: ">=1,<2", Signer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Dependencies) != 2 {
		t.Fatalf("closure size = %d, want 2", len(b.Dependencies))
	}
	if _, err := config.BundleDigest(b); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CP_002_Golden(t *testing.T) {
	r, root := compilerFixture(t)
	opts := config.CompileOptions{BundleID: "b", Roots: []config.ObjectRef{root}, TargetScope: "tenant/a", MinimumRuntimeVersion: "runtime/1", CompatibilityRange: ">=1,<2", Signer: "test"}
	a, err := config.Compile(r, opts)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := config.BundleDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	opts.Roots = []config.ObjectRef{root}
	c, err := config.Compile(r, opts)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := config.BundleDigest(c)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("digest changed across identical builds: %s != %s", d1, d2)
	}
}

func TestTodo_CP_002_Integration(t *testing.T) {
	r, root := compilerFixture(t)
	_, err := config.Compile(r, config.CompileOptions{BundleID: "b", Roots: []config.ObjectRef{root}, TargetScope: "tenant/b", MinimumRuntimeVersion: "runtime/1", Signer: "test"})
	if !errors.Is(err, config.ErrScopeMismatch) {
		t.Fatalf("error = %v, want scope mismatch", err)
	}
}

func TestTodo_CP_002_Conformance(t *testing.T) {
	r, root := compilerFixture(t)
	cases := []struct {
		name   string
		mutate func(*config.CompileOptions)
		want   error
	}{
		{"missing runtime floor", func(o *config.CompileOptions) { o.MinimumRuntimeVersion = "" }, config.ErrMissingRuntimeVersion},
		{"missing scope", func(o *config.CompileOptions) { o.TargetScope = "" }, config.ErrMissingTargetScope},
		{"unresolved root", func(o *config.CompileOptions) { o.Roots[0].Version = "9.9.9" }, config.ErrUnresolvedDependency},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := config.CompileOptions{BundleID: "b", Roots: []config.ObjectRef{root}, TargetScope: "tenant/a", MinimumRuntimeVersion: "runtime/1", Signer: "test"}
			tc.mutate(&o)
			_, err := config.Compile(r, o)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
