package cryptoagility

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

func fixtureKey(id string, n byte) Key {
	var seed [ed25519.SeedSize]byte
	for i := range seed {
		seed[i] = n + byte(i)
	}
	p := ed25519.NewKeyFromSeed(seed[:])
	return Key{ID: id, Algorithm: AlgorithmEd25519, Private: p, Public: p.Public().(ed25519.PublicKey)}
}

func TestTodo_CRYPTO_001(t *testing.T) {
	old, neu := fixtureKey("old", 1), fixtureKey("new", 9)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := AlgorithmPolicy{Active: AlgorithmEd25519, Accepted: map[string]bool{AlgorithmEd25519: true}, Cutoff: at.Add(time.Hour)}
	e, err := DualSign([]byte("immutable-ledger-entry"), old, neu, policy, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Signatures) != 2 || e.Signatures[0].Algorithm != AlgorithmEd25519 || e.Signatures[1].Algorithm != AlgorithmEd25519 {
		t.Fatalf("dual signature matrix = %#v", e.Signatures)
	}
	if _, err := VerifyDual(e, map[string]Key{"old": old, "new": neu}, policy, at); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CRYPTO_001_Golden(t *testing.T) {
	old, neu := fixtureKey("old", 1), fixtureKey("new", 9)
	at := time.Unix(100, 0).UTC()
	e, err := DualSign([]byte("fixture"), old, neu, AlgorithmPolicy{Active: AlgorithmEd25519, Accepted: map[string]bool{AlgorithmEd25519: true}, Cutoff: at.Add(time.Second)}, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Payload) != 7 || len(e.Signatures[0].Value) != ed25519.SignatureSize {
		t.Fatal("golden fixture shape changed")
	}
}

func TestTodo_CRYPTO_001_Integration(t *testing.T) {
	old, neu := fixtureKey("old", 1), fixtureKey("new", 9)
	at := time.Unix(100, 0).UTC()
	e, _ := DualSign([]byte("record"), old, neu, AlgorithmPolicy{Active: AlgorithmEd25519, Accepted: map[string]bool{AlgorithmEd25519: true}, Cutoff: at.Add(time.Hour)}, at)
	updated, receipt, err := ReSign(e, map[string]Key{"old": old, "new": neu}, AlgorithmPolicy{Active: AlgorithmEd25519, Accepted: map[string]bool{AlgorithmEd25519: true}, Cutoff: at.Add(time.Hour)}, neu, "ledger-1", at)
	if err != nil || receipt.Kind != "re-sign" || len(updated.Signatures) != 3 {
		t.Fatalf("re-sign: %#v %v", receipt, err)
	}
	keyOld, keyNew := []byte("01234567890123456789012345678901"), []byte("abcdefghijklmnopqrstuvwxyz123456")
	n, c, err := encrypt(keyOld, []byte("document"))
	if err != nil {
		t.Fatal(err)
	}
	env, r, err := ReEncrypt(Envelope{ArtifactID: "doc-1", Algorithm: "aes-gcm-old", Nonce: n, Ciphertext: c}, keyOld, keyNew, "aes-gcm-new", "doc-1", at)
	if err != nil || r.Kind != "re-encrypt" {
		t.Fatalf("re-encrypt: %#v %v", r, err)
	}
	plain, err := decrypt(keyNew, env)
	if err != nil || string(plain) != "document" {
		t.Fatalf("new envelope: %q %v", plain, err)
	}
}

func TestTodo_CRYPTO_001_Mutation(t *testing.T) {
	old, neu := fixtureKey("old", 1), fixtureKey("new", 9)
	at := time.Unix(100, 0).UTC()
	p := AlgorithmPolicy{Active: AlgorithmEd25519, Accepted: map[string]bool{AlgorithmEd25519: true}, Retired: map[string]bool{AlgorithmEd25519: true}, Cutoff: at}
	e, _ := DualSign([]byte("record"), old, neu, p, at.Add(-time.Second))
	e.Payload[0] ^= 1
	if _, err := VerifyDual(e, map[string]Key{"old": old, "new": neu}, p, at.Add(-time.Second)); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tamper err = %v", err)
	}
	if err := Verify([]byte("record"), e.Signatures[0], map[string]Key{"old": old}, p, at); !errors.Is(err, ErrAlgorithmRejected) {
		t.Fatalf("retired err = %v", err)
	}
}
