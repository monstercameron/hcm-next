package ledger

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	checkpointadapter "github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
)

// testSigner builds a throwaway signer. The port's own tests generate a key
// rather than reading one: the checked-in fixture belongs to the adapter
// package that needs a reproducible golden signature, and nothing here does.
func testSigner(t *testing.T) (CheckpointSigner, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := checkpointadapter.NewEd25519Signer("hcmnext:checkpoint:port-test", priv)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}
	return signer, signer.PublicKey()
}

// TestCheckpointerServiceSatisfiesThePort proves the adapter this package
// hands out actually implements the port it declares, so a signature drift
// in internal/data/ledger/checkpoint is a build failure here rather than at
// some future call site.
func TestCheckpointerServiceSatisfiesThePort(t *testing.T) {
	signer, pub := testSigner(t)
	dir := checkpointadapter.NewStaticKeyDirectory(CheckpointKeyStatus{
		KeyID: signer.KeyID(), PublicKey: pub,
	})
	checkpointer, err := NewCheckpointer(signer, dir)
	if err != nil {
		t.Fatalf("build checkpointer: %v", err)
	}
	if checkpointer == nil {
		t.Fatal("NewCheckpointer returned nil")
	}
	var _ Checkpointer = checkpointer
}

// TestNewCheckpointerRequiresBothKeySeams proves the port cannot be used to
// bypass the rule the adapter enforces: a checkpointer without a directory
// could never refuse a revoked key.
func TestNewCheckpointerRequiresBothKeySeams(t *testing.T) {
	signer, _ := testSigner(t)
	if _, err := NewCheckpointer(nil, checkpointadapter.NewStaticKeyDirectory()); err == nil {
		t.Error("a checkpointer was built without a signer")
	}
	if _, err := NewCheckpointer(signer, nil); err == nil {
		t.Error("a checkpointer was built without a key directory")
	}
}

// TestVerifyCheckpointIsOffline proves the port's verification entry point
// needs nothing but a manifest and a key directory: no database handle
// appears in its signature, which is what makes an auditor's check possible
// without production credentials.
func TestVerifyCheckpointIsOffline(t *testing.T) {
	signer, pub := testSigner(t)
	dir := checkpointadapter.NewStaticKeyDirectory(CheckpointKeyStatus{
		KeyID: signer.KeyID(), PublicKey: pub,
	})

	streams := []CheckpointStreamHead{
		{StreamKey: "worker:1", Sequence: 3, ChainHash: strings.Repeat("a", 64), ChainAlgorithm: "sha256"},
	}
	root, err := checkpointadapter.ComputeRootDigest(streams)
	if err != nil {
		t.Fatalf("root digest: %v", err)
	}
	manifest := CheckpointManifest{
		SchemaVersion: checkpointadapter.ManifestSchemaVersion,
		Tenant:        uuid.New(),
		EpochID:       uuid.New(),
		EpochNumber:   1,
		Schema:        CheckpointSchemaRelease{Version: 1, Digest: strings.Repeat("c", 64)},
		Streams:       streams,
		RootDigest:    root, RootDigestAlgorithm: checkpointadapter.DigestAlgorithm,
		CoversFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CoversTo:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	signed, err := checkpointadapter.Sign(manifest, signer, dir)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := VerifyCheckpoint(signed, dir); err != nil {
		t.Fatalf("VerifyCheckpoint: %v", err)
	}

	t.Run("an edited manifest is refused", func(t *testing.T) {
		edited := signed
		edited.EpochNumber = 2
		if err := VerifyCheckpoint(edited, dir); err == nil {
			t.Fatal("an edited manifest verified through the port")
		}
	})

	t.Run("no private key material is reachable through the port", func(t *testing.T) {
		// Signer exposes no method that returns private material; the public
		// key it does expose is a valid Ed25519 public key and nothing more.
		raw, err := hex.DecodeString(signer.PublicKey())
		if err != nil || len(raw) != ed25519.PublicKeySize {
			t.Fatalf("PublicKey() = %q, want a %d-byte hex key", signer.PublicKey(), ed25519.PublicKeySize)
		}
	})
}
