package legal

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"testing"
)

type nilSignerPort struct{}

func (*nilSignerPort) Sign([]byte) ([]byte, error) { return nil, nil }

func TestTodo_LEGAL_014_SignerPortExactParity(t *testing.T) {
	key := fixedSigner(t, 0x6a)
	data := []byte("the exact canonical receipt bytes")
	legacyDigest, legacy := key.SignDigest(data)
	port, err := NewPortSigner(key.PublicKey(), SignerFunc(func(digest []byte) ([]byte, error) {
		want := sha256.Sum256(data)
		if string(digest) != string(want[:]) {
			t.Fatalf("port received bytes other than raw sha256 digest")
		}
		return ed25519.Sign(key.priv, digest), nil
	}))
	if err != nil {
		t.Fatalf("NewPortSigner: %v", err)
	}
	portDigest, portSignature, err := port.SignDigestChecked(data)
	if err != nil {
		t.Fatalf("SignDigestChecked: %v", err)
	}
	if portDigest != legacyDigest || string(portSignature.Bytes) != string(legacy.Bytes) || string(portSignature.PublicKey) != string(legacy.PublicKey) {
		t.Fatal("external signer changed the established digest or signature encoding")
	}
	if err := VerifySignature(data, portDigest, portSignature); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
}

func TestTodo_LEGAL_014_SignerPortWrongKeyRejected(t *testing.T) {
	trusted := fixedSigner(t, 0x71)
	wrong := fixedSigner(t, 0x72)
	port, err := NewPortSigner(trusted.PublicKey(), SignerFunc(func(digest []byte) ([]byte, error) {
		return ed25519.Sign(wrong.priv, digest), nil
	}))
	if err != nil {
		t.Fatalf("NewPortSigner: %v", err)
	}
	if _, _, err := port.SignDigestChecked([]byte("receipt")); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("wrong-key signature error = %v, want ErrSignatureInvalid", err)
	}
}

func TestTodo_LEGAL_014_SignerPortFailurePropagates(t *testing.T) {
	want := errors.New("custody unavailable")
	trusted := fixedSigner(t, 0x73)
	port, err := NewPortSigner(trusted.PublicKey(), SignerFunc(func([]byte) ([]byte, error) { return nil, want }))
	if err != nil {
		t.Fatalf("NewPortSigner: %v", err)
	}
	if _, _, err := port.SignDigestChecked([]byte("receipt")); !errors.Is(err, ErrSignerPort) || !errors.Is(err, want) {
		t.Fatalf("port failure = %v, want wrapped ErrSignerPort and custody error", err)
	}
}

func TestTodo_LEGAL_014_SignerPortConfigurationValidated(t *testing.T) {
	if _, err := NewPortSigner([]byte{1}, SignerFunc(func([]byte) ([]byte, error) { return nil, nil })); !errors.Is(err, ErrSignerKey) {
		t.Fatalf("bad public key error = %v", err)
	}
	if _, err := NewPortSigner(make(ed25519.PublicKey, ed25519.PublicKeySize), nil); !errors.Is(err, ErrSignerPort) {
		t.Fatalf("nil port error = %v", err)
	}
	var nilFunc SignerFunc
	if _, err := NewPortSigner(make(ed25519.PublicKey, ed25519.PublicKeySize), nilFunc); !errors.Is(err, ErrSignerPort) {
		t.Fatalf("typed nil port error = %v", err)
	}
	var nilPointer *nilSignerPort
	if _, err := NewPortSigner(make(ed25519.PublicKey, ed25519.PublicKeySize), nilPointer); !errors.Is(err, ErrSignerPort) {
		t.Fatalf("typed nil pointer port error = %v", err)
	}
}

func TestTodo_LEGAL_014_SignerPortInputMutationRejected(t *testing.T) {
	trusted := fixedSigner(t, 0x74)
	port, err := NewPortSigner(trusted.PublicKey(), SignerFunc(func(digest []byte) ([]byte, error) {
		digest[0] ^= 0xff
		return ed25519.Sign(trusted.priv, digest), nil
	}))
	if err != nil {
		t.Fatalf("NewPortSigner: %v", err)
	}
	if _, _, err := port.SignDigestChecked([]byte("receipt")); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("mutated-input signature error = %v, want ErrSignatureInvalid", err)
	}
}

func TestTodo_LEGAL_014_ResolvePortFailureReturnsNoContext(t *testing.T) {
	set, packs := preemptionPacks(preemptionGoldenFixtures[0])
	registry := NewRegistry()
	for _, pack := range packs {
		if err := registry.Register(pack); err != nil {
			t.Fatal(err)
		}
	}
	key := fixedSigner(t, 0x75)
	port, err := NewPortSigner(key.PublicKey(), SignerFunc(func([]byte) ([]byte, error) {
		return nil, errors.New("custody unavailable")
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := Resolve(validInput(t, set.Overlays[0]), registry, port, mustInstant(t, 1_770_100_000))
	if ctx != nil || !errors.Is(err, ErrLegalContextUnknown) || !errors.Is(err, ErrSignerPort) {
		t.Fatalf("Resolve result context=%v error=%v", ctx, err)
	}
}

func TestTodo_LEGAL_014_EvaluateReceiptPortFailureReturnsNoReceipt(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	port, err := NewPortSigner(signer.PublicKey(), SignerFunc(func([]byte) ([]byte, error) {
		return nil, errors.New("custody unavailable")
	}))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, port, at)
	if receipt.Digest != "" || !errors.Is(err, ErrReceiptInvalid) || !errors.Is(err, ErrSignerPort) {
		t.Fatalf("EvaluateReceipt result receipt=%+v error=%v", receipt, err)
	}
}
