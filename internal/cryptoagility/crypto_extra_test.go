package cryptoagility

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

func TestAlgorithmPolicy_PermitsOnlyDeclaredReadsAndWrites(t *testing.T) {
	cutoff := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	policy := AlgorithmPolicy{Active: AlgorithmEd25519V2, Accepted: map[string]bool{AlgorithmEd25519: true, AlgorithmEd25519V2: true}, Retired: map[string]bool{AlgorithmEd25519: true}, Cutoff: cutoff}
	if !policy.permitsWrite(AlgorithmEd25519V2) || policy.permitsWrite(AlgorithmEd25519) || policy.permitsWrite("") {
		t.Fatal("write policy did not enforce the single active algorithm")
	}
	for _, tc := range []struct {
		name string
		alg  string
		at   time.Time
		want bool
	}{
		{"accepted before cutoff", AlgorithmEd25519, cutoff.Add(-time.Nanosecond), true},
		{"retired at cutoff", AlgorithmEd25519, cutoff, false},
		{"retired after cutoff", AlgorithmEd25519, cutoff.Add(time.Hour), false},
		{"active accepted", AlgorithmEd25519V2, cutoff.Add(time.Hour), true},
		{"unknown", "rsa", cutoff.Add(-time.Hour), false},
		{"empty", "", cutoff.Add(-time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.permitsRead(tc.alg, tc.at); got != tc.want {
				t.Fatalf("permitsRead(%q, %s) = %v, want %v", tc.alg, tc.at, got, tc.want)
			}
		})
	}
	if !(AlgorithmPolicy{Accepted: map[string]bool{AlgorithmEd25519: true}, Retired: map[string]bool{AlgorithmEd25519: true}}).permitsRead(AlgorithmEd25519, cutoff) {
		t.Fatal("retired algorithm without cutoff was not accepted")
	}
}

func TestSignAndVerify_RejectMalformedKeysAndSignatures(t *testing.T) {
	good := fixtureKey("good", 1)
	policy := AlgorithmPolicy{Active: AlgorithmEd25519, Accepted: map[string]bool{AlgorithmEd25519: true}}
	if _, err := Sign([]byte("m"), good, AlgorithmPolicy{Active: AlgorithmEd25519V2}); !errors.Is(err, ErrAlgorithmRejected) {
		t.Fatalf("inactive signing algorithm = %v, want ErrAlgorithmRejected", err)
	}
	badAlgorithm := good
	badAlgorithm.Algorithm = "rsa"
	if _, err := Sign([]byte("m"), badAlgorithm, AlgorithmPolicy{Active: "rsa"}); err == nil {
		t.Fatal("unsupported signing algorithm accepted")
	}
	badPrivate := good
	badPrivate.Private = ed25519.PrivateKey([]byte("short"))
	if _, err := Sign([]byte("m"), badPrivate, policy); err == nil {
		t.Fatal("short private key accepted")
	}
	sig, err := Sign([]byte("m"), good, policy)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]Key{"good": good}
	if err := Verify([]byte("m"), sig, keys, policy, time.Now()); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		sig  Signature
		keys map[string]Key
		want error
	}{
		{"unknown key", Signature{KeyID: "missing", Algorithm: AlgorithmEd25519, Value: sig.Value}, keys, ErrInvalidSignature},
		{"key algorithm mismatch", Signature{KeyID: "good", Algorithm: AlgorithmEd25519V1, Value: sig.Value}, keys, ErrAlgorithmRejected},
		{"bad public key", Signature{KeyID: "bad", Algorithm: AlgorithmEd25519, Value: sig.Value}, map[string]Key{"bad": {ID: "bad", Algorithm: AlgorithmEd25519, Public: ed25519.PublicKey{1}}}, ErrInvalidSignature},
		{"forged bytes", Signature{KeyID: "good", Algorithm: AlgorithmEd25519, Value: append([]byte(nil), sig.Value[:len(sig.Value)-1]...)}, keys, ErrInvalidSignature},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Verify([]byte("m"), tc.sig, tc.keys, policy, time.Now()); !errors.Is(err, tc.want) {
				t.Fatalf("Verify = %v, want %v", err, tc.want)
			}
		})
	}
	retired := policy
	retired.Retired = map[string]bool{AlgorithmEd25519: true}
	retired.Cutoff = time.Now().Add(-time.Second)
	if err := Verify([]byte("m"), sig, keys, retired, time.Now()); !errors.Is(err, ErrAlgorithmRejected) {
		t.Fatalf("retired signature = %v, want ErrAlgorithmRejected", err)
	}
}

func TestDualSignVerifyDualAndReSign_RequireBothAndPreserveHistory(t *testing.T) {
	old, neu := fixtureKey("old", 1), fixtureKey("new", 2)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := AlgorithmPolicy{Active: AlgorithmEd25519, Accepted: map[string]bool{AlgorithmEd25519: true}, Cutoff: at.Add(time.Hour)}
	evidence, err := DualSign([]byte("payload"), old, neu, policy, at)
	if err != nil || len(evidence.Signatures) != 2 {
		t.Fatalf("DualSign = %+v, %v", evidence, err)
	}
	if _, err := DualSign([]byte("payload"), old, neu, policy, at.Add(time.Hour)); !errors.Is(err, ErrAlgorithmRejected) {
		t.Fatalf("at cutoff DualSign = %v, want ErrAlgorithmRejected", err)
	}
	if _, err := VerifyDual(evidence, map[string]Key{"old": old, "new": neu}, policy, at); err != nil {
		t.Fatalf("VerifyDual: %v", err)
	}
	if _, err := VerifyDual(SignedEvidence{Payload: evidence.Payload, Signatures: evidence.Signatures[:1]}, map[string]Key{"old": old, "new": neu}, policy, at); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("one signature VerifyDual = %v", err)
	}
	tampered := evidence
	tampered.Signatures = make([]Signature, len(evidence.Signatures))
	copy(tampered.Signatures, evidence.Signatures)
	for i := range tampered.Signatures {
		tampered.Signatures[i].Value = append([]byte(nil), evidence.Signatures[i].Value...)
	}
	tampered.Signatures[0].Value[0] ^= 1
	count, err := VerifyDual(tampered, map[string]Key{"old": old, "new": neu}, policy, at)
	if count != 1 || !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("one valid signature VerifyDual = %d, %v", count, err)
	}
	originalPayload := append([]byte(nil), evidence.Payload...)
	updated, receipt, err := ReSign(evidence, map[string]Key{"old": old, "new": neu}, policy, neu, "artifact-1", at)
	if err != nil || len(updated.Signatures) != 3 || receipt.ArtifactID != "artifact-1" || receipt.FromAlgorithm != AlgorithmEd25519 || receipt.ToAlgorithm != AlgorithmEd25519 {
		t.Fatalf("ReSign = %+v, %+v, %v", updated, receipt, err)
	}
	updated.Payload[0] ^= 1
	if string(evidence.Payload) != string(originalPayload) || len(evidence.Signatures) != 2 {
		t.Fatal("ReSign mutated historical evidence")
	}
}

func TestEncryptionAndDigestHelpers_RejectBadInputs(t *testing.T) {
	if _, _, err := encrypt([]byte("short"), []byte("p")); err == nil {
		t.Fatal("invalid AES key accepted")
	}
	key := []byte("01234567890123456789012345678901")
	nonce, ciphertext, err := encrypt(key, []byte("plain"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decrypt(key, Envelope{Nonce: nonce, Ciphertext: ciphertext}); err != nil || string(got) != "plain" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
	for _, env := range []Envelope{{Nonce: nil, Ciphertext: ciphertext}, {Nonce: append([]byte(nil), nonce...), Ciphertext: []byte("forged")}} {
		if _, err := decrypt(key, env); !errors.Is(err, ErrInvalidEnvelope) && err == nil {
			t.Fatalf("decrypt malformed envelope = %v", err)
		}
	}
	if _, _, err := ReEncrypt(Envelope{Nonce: nonce, Ciphertext: ciphertext}, []byte("bad"), key, "new", "a", time.Now()); err == nil {
		t.Fatal("ReEncrypt accepted invalid old key")
	}
	if _, _, err := ReEncrypt(Envelope{Nonce: nonce, Ciphertext: ciphertext}, key, []byte("bad"), "new", "a", time.Now()); err == nil {
		t.Fatal("ReEncrypt accepted invalid new key")
	}
	if !CompareDigest([]byte("same"), []byte("same")) || CompareDigest([]byte("same"), []byte("different")) || CompareDigest([]byte("same"), nil) {
		t.Fatal("CompareDigest returned incorrect equality")
	}
}
