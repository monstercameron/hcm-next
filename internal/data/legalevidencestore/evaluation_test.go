package legalevidencestore

import (
	"context"
	"crypto/ed25519"
	"errors"
	"slices"
	"testing"

	legal "github.com/monstercameron/hcm-next/internal/governance/legal"
)

func TestTodo_LEGAL_014_InvalidVerifierRequest(t *testing.T) {
	v := NewVerifier(nil, nil, []byte("not-a-key"))
	_, err := v.VerifyLegalEvidence(context.Background(), EvidenceRequest{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty request error = %v, want ErrInvalid", err)
	}
}

func TestTodo_LEGAL_014_TrustedIssuerAllowlist(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	key := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if got := trustedKey(legal.Signature{PublicKey: key}, [][]byte{[]byte("wrong")}); got != nil {
		t.Fatal("unconfigured issuer key was accepted")
	}
	configured := slices.Clone([]byte(key))
	got := trustedKey(legal.Signature{PublicKey: key}, [][]byte{configured})
	if len(got) != ed25519.PublicKeySize {
		t.Fatalf("trusted key length = %d, want %d", len(got), ed25519.PublicKeySize)
	}
}
