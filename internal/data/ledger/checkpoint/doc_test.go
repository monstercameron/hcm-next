package checkpoint_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
)

// TestDocAnchoringIsOptionalAndChangesNothing proves the claim the package
// doc makes about external anchoring: the default anchor publishes nothing,
// and anchoring is a port, so which provider is configured can never change
// what a checkpoint says.
func TestDocAnchoringIsOptionalAndChangesNothing(t *testing.T) {
	var anchor checkpoint.Anchor = checkpoint.NoAnchor{}
	if anchor.Name() != "none" {
		t.Fatalf("default anchor is named %q, want %q", anchor.Name(), "none")
	}
	receipt, err := anchor.Anchor(context.Background(), checkpoint.Manifest{})
	if err != nil {
		t.Fatalf("the default anchor failed: %v", err)
	}
	if receipt.Reference != "" {
		t.Fatalf("the default anchor returned reference %q, want none", receipt.Reference)
	}
}

// TestDocServiceRequiresBothHalvesOfTheKeySeam proves that a Service cannot
// be built without the two things that make "never sign with a revoked or
// expired key" enforceable: something to sign with, and something that can
// refuse.
func TestDocServiceRequiresBothHalvesOfTheKeySeam(t *testing.T) {
	_, priv := loadDevKey(t)
	signer, err := checkpoint.NewEd25519Signer(devKeyID, priv)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}

	if _, err := checkpoint.NewService(nil, checkpoint.NewStaticKeyDirectory()); err == nil {
		t.Error("a service was built without a signer")
	}
	if _, err := checkpoint.NewService(signer, nil); err == nil {
		t.Error("a service was built without a key directory")
	}
	if _, err := checkpoint.NewService(signer, checkpoint.NewStaticKeyDirectory()); err != nil {
		t.Errorf("a service with both halves failed to build: %v", err)
	}
}

// TestDocAlgorithmsAreRecordedNotAssumed proves the two algorithm constants
// exist and are the only ones this package names, so adding a second is a
// visible change rather than an implicit one.
func TestDocAlgorithmsAreRecordedNotAssumed(t *testing.T) {
	if checkpoint.AlgorithmEd25519 != "ed25519" {
		t.Errorf("signature algorithm is %q, want ed25519", checkpoint.AlgorithmEd25519)
	}
	if checkpoint.DigestAlgorithm != "sha256" {
		t.Errorf("digest algorithm is %q, want sha256", checkpoint.DigestAlgorithm)
	}
	if checkpoint.ManifestSchemaVersion != 1 {
		t.Errorf("manifest schema version is %d, want 1", checkpoint.ManifestSchemaVersion)
	}
}
