package federation

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func joseTestInput(t *testing.T) string {
	t.Helper()
	header, err := json.Marshal(joseHeader{Algorithm: string(AlgRS256), KeyID: "kid"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString([]byte("{\"sub\":\"user\"}"))
}

func TestSplitJWS_RejectsMalformedAndTrailingJSON(t *testing.T) {
	validHeader := base64.RawURLEncoding.EncodeToString([]byte("{\"alg\":\"RS256\",\"kid\":\"kid\"}"))
	validPayload := base64.RawURLEncoding.EncodeToString([]byte("{\"iss\":\"issuer\"}"))
	valid := validHeader + "." + validPayload + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
	h, signingInput, payload, sig, err := splitJWS(valid, 1024)
	if err != nil || h.Algorithm != string(AlgRS256) || h.KeyID != "kid" || signingInput != validHeader+"."+validPayload || string(payload) != "{\"iss\":\"issuer\"}" || string(sig) != "sig" {
		t.Fatalf("splitJWS valid = h=%+v input=%q payload=%q sig=%q err=%v", h, signingInput, payload, sig, err)
	}
	trailingHeader := base64.RawURLEncoding.EncodeToString([]byte("{\"alg\":\"RS256\",\"kid\":\"kid\"} {}")) + "." + validPayload + ".sig"
	trailingPayload := validHeader + "." + base64.RawURLEncoding.EncodeToString([]byte("{\"iss\":\"issuer\"} {}")) + ".sig"
	for _, raw := range []string{
		"", valid[:len(valid)-1], strings.Repeat("a", 20), "a.b", "a.b.c.d", ".b.c", "a..c", "a.b.",
		"%%%." + validPayload + ".sig", validHeader + ".%!.sig", validHeader + "." + validPayload + ".%!",
		trailingHeader, trailingPayload,
	} {
		if _, _, _, _, err := splitJWS(raw, 10); !errors.Is(err, ErrMalformedAssertion) {
			t.Errorf("splitJWS(%q) = %v, want ErrMalformedAssertion", raw, err)
		}
	}
	if _, _, _, _, err := splitJWS(valid, 1024); err != nil {
		t.Fatalf("valid split after malformed cases: %v", err)
	}
}

func TestVerifySignature_AllAlgorithmsAndTypeBoundaries(t *testing.T) {
	input := joseTestInput(t)
	sum := sha256.Sum256([]byte(input))
	rsaKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	rsaSig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	if err := verifySignature(AlgRS256, &rsaKey.PublicKey, input, rsaSig); err != nil {
		t.Fatalf("valid RS256: %v", err)
	}
	if err := verifySignature(AlgRS256, &rsaKey.PublicKey, input, []byte("bad")); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("bad RS256 = %v, want ErrSignatureInvalid", err)
	}
	if err := verifySignature(AlgRS256, &ecdsa.PublicKey{}, input, rsaSig); !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("wrong RS256 key type = %v, want ErrUnsupportedAlgorithm", err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	r, s, err := ecdsa.Sign(rand.Reader, ecKey, sum[:])
	if err != nil {
		t.Fatalf("ecdsa.Sign: %v", err)
	}
	ecSig := make([]byte, 64)
	r.FillBytes(ecSig[:32])
	s.FillBytes(ecSig[32:])
	if err := verifySignature(AlgES256, &ecKey.PublicKey, input, ecSig); err != nil {
		t.Fatalf("valid ES256: %v", err)
	}
	for _, sig := range [][]byte{nil, make([]byte, 63), make([]byte, 65)} {
		if err := verifySignature(AlgES256, &ecKey.PublicKey, input, sig); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("ES256 signature length %d = %v, want ErrSignatureInvalid", len(sig), err)
		}
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey(P384): %v", err)
	}
	if err := verifySignature(AlgES256, &p384.PublicKey, input, ecSig); !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("P384 ES256 = %v, want ErrUnsupportedAlgorithm", err)
	}
	if err := verifySignature(AlgES256, &rsaKey.PublicKey, input, ecSig); !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("RSA ES256 = %v, want ErrUnsupportedAlgorithm", err)
	}

	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	edSig := ed25519.Sign(edPriv, []byte(input))
	if err := verifySignature(AlgEdDSA, edPub, input, edSig); err != nil {
		t.Fatalf("valid EdDSA: %v", err)
	}
	for _, sig := range [][]byte{nil, make([]byte, ed25519.SignatureSize-1), append([]byte(nil), edSig...)} {
		if len(sig) == len(edSig) {
			sig[0] ^= 1
		}
		if err := verifySignature(AlgEdDSA, edPub, input, sig); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("EdDSA signature length/content %d = %v, want ErrSignatureInvalid", len(sig), err)
		}
	}
	if err := verifySignature(AlgEdDSA, &rsaKey.PublicKey, input, edSig); !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("RSA EdDSA = %v, want ErrUnsupportedAlgorithm", err)
	}
	if err := verifySignature(Algorithm("none"), edPub, input, edSig); !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("unknown algorithm = %v, want ErrUnsupportedAlgorithm", err)
	}
}

func TestSigningKeyValidityAndStaticKeySource(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		key  SigningKey
		at   time.Time
		want bool
	}{
		{name: "unbounded", key: SigningKey{}, at: now, want: true},
		{name: "at not before", key: SigningKey{NotBefore: now}, at: now, want: true},
		{name: "before not before", key: SigningKey{NotBefore: now}, at: now.Add(-time.Nanosecond), want: false},
		{name: "before not after", key: SigningKey{NotAfter: now}, at: now.Add(-time.Nanosecond), want: true},
		{name: "at not after", key: SigningKey{NotAfter: now}, at: now, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.validAt(tt.at); got != tt.want {
				t.Fatalf("validAt(%s) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
	source := NewStaticKeySource()
	key := SigningKey{ID: "kid", Algorithm: AlgRS256}
	if got := source.WithKey("issuer", key); got != source {
		t.Fatal("WithKey did not return receiver")
	}
	got, err := source.ResolveKey(context.Background(), "issuer", "kid")
	if err != nil || got != key {
		t.Fatalf("ResolveKey = %+v, err=%v, want %+v", got, err, key)
	}
	for _, lookup := range [][2]string{{"missing", "kid"}, {"issuer", "missing"}} {
		if _, err := source.ResolveKey(context.Background(), lookup[0], KeyID(lookup[1])); !errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("ResolveKey(%v) = %v, want ErrKeyNotFound", lookup, err)
		}
	}
}
