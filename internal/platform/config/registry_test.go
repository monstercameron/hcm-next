package config_test

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/config"
)

func registryObject(t *testing.T, version string) config.ConfigObject {
	t.Helper()
	return config.ConfigObject{
		Kind: config.ObjectWorkflow, ID: "onboarding", Version: version,
		Owner: "team:people", Phase: "P1", Scope: "tenant:acme",
		EffectiveFrom: time.Unix(1700000000, 0).UTC(), Content: []byte("workflow:" + version),
		Dependencies: []config.Dependency{{Kind: config.DependencySchema, Name: "worker", Version: "1.0.0", Digest: digestHex(t, 7)}},
	}
}

func TestTodo_CP_001(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	o, err := registryObject(t, "1.0.0").Sign("key-1", priv)
	if err != nil {
		t.Fatal(err)
	}
	r := config.NewRegistry()
	if err := r.Register(o); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(o); !errors.Is(err, config.ErrAlreadyRegistered) {
		t.Fatalf("duplicate register = %v", err)
	}
	got, ok := r.Lookup(config.ObjectWorkflow, "onboarding", "1.0.0")
	if !ok || got.Digest != o.Digest {
		t.Fatalf("lookup = %#v, %v", got, ok)
	}
	got.Content[0] = 'X'
	got.Signature[0] ^= 1
	again, _ := r.Lookup(config.ObjectWorkflow, "onboarding", "1.0.0")
	if string(again.Content) != "workflow:1.0.0" {
		t.Fatal("registry exposed mutable content")
	}
	if len(r.Versions(config.ObjectWorkflow, "onboarding")) != 1 {
		t.Fatal("version listing missing object")
	}
}

func TestRegistryRejectsUnownedAndUnsignedObjects(t *testing.T) {
	o := registryObject(t, "1.0.0")
	o.Owner = ""
	if err := o.Verify(); !errors.Is(err, config.ErrMissingOwner) {
		t.Fatalf("owner error = %v", err)
	}
	o = registryObject(t, "1.0.0")
	if err := config.NewRegistry().Register(o); !errors.Is(err, config.ErrInvalidSignature) {
		t.Fatalf("unsigned error = %v", err)
	}
}
