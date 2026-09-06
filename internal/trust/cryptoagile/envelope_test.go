package cryptoagile

import (
	"errors"
	"testing"
)

func TestEnvelope_EncodeDecodeRoundTrip(t *testing.T) {
	env := Envelope{SuiteID: "ed25519-v1", Signature: []byte{0x01, 0x02, 0x0a, 0xff}}
	got, err := DecodeEnvelope(env.Encode())
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if got.SuiteID != env.SuiteID || string(got.Signature) != string(env.Signature) {
		t.Fatalf("round trip = %+v, want %+v", got, env)
	}
}

func TestEnvelope_EncodeFixedForm(t *testing.T) {
	env := Envelope{SuiteID: "suite-a", Signature: []byte{0x01, 0x02, 0x0a}}
	want := "cryptoagile.v1:suite-a:01020a"
	if got := env.Encode(); got != want {
		t.Fatalf("Encode() = %q, want %q", got, want)
	}
}

func TestDecodeEnvelope_Refuses(t *testing.T) {
	cases := []string{
		"",
		"suite-a:01020a",            // missing tag
		"cryptoagile.v1:01020a",     // missing suite id segment collapses wrong
		"cryptoagile.v1::01020a",    // empty suite id
		"cryptoagile.v1:suite-a:zz", // non-hex signature
	}
	for _, s := range cases {
		if _, err := DecodeEnvelope(s); !errors.Is(err, ErrEnvelopeFormat) {
			t.Fatalf("DecodeEnvelope(%q) = %v, want ErrEnvelopeFormat", s, err)
		}
	}
}

func TestEnvelopeSignerVerifier_RoundTrip(t *testing.T) {
	keys := NewFakeKeySource()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	if err := keys.AddEd25519("ed25519-v1", seed); err != nil {
		t.Fatalf("AddEd25519: %v", err)
	}

	registry := NewRegistry()
	activatedAt := day(0)
	if err := registry.Register(AlgorithmSuite{ID: "ed25519-v1", Kind: KindSignature, Status: StatusActive, ActivatedAt: activatedAt}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	signer := NewEnvelopeSigner(keys)
	env, err := signer.Sign("ed25519-v1", []byte("hello world"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	verifier := NewEnvelopeVerifier(registry, keys)
	if err := verifier.Verify([]byte("hello world"), env); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := verifier.Verify([]byte("tampered"), env); err == nil {
		t.Fatalf("Verify(tampered message) = nil, want error")
	}
}

func TestEnvelopeSigner_RefusesEmptySuiteID(t *testing.T) {
	signer := NewEnvelopeSigner(NewFakeKeySource())
	if _, err := signer.Sign("", []byte("m")); !errors.Is(err, ErrEnvelopeFormat) {
		t.Fatalf("Sign(empty suite) = %v, want ErrEnvelopeFormat", err)
	}
}

func TestEnvelopeVerifier_RefusesStrippedSuiteID(t *testing.T) {
	verifier := NewEnvelopeVerifier(NewRegistry(), NewFakeKeySource())
	err := verifier.Verify([]byte("m"), Envelope{SuiteID: "", Signature: []byte{1}})
	if !errors.Is(err, ErrStrippedSuiteID) {
		t.Fatalf("Verify(stripped suite id) = %v, want ErrStrippedSuiteID", err)
	}
}

func TestEnvelopeVerifier_RefusesUnknownSuite(t *testing.T) {
	verifier := NewEnvelopeVerifier(NewRegistry(), NewFakeKeySource())
	err := verifier.Verify([]byte("m"), Envelope{SuiteID: "ghost", Signature: []byte{1}})
	if !errors.Is(err, ErrSuiteNotFound) {
		t.Fatalf("Verify(unknown suite) = %v, want ErrSuiteNotFound", err)
	}
}

func TestEnvelopeVerifier_RefusesRetiredSuite(t *testing.T) {
	keys := NewFakeKeySource()
	seed := make([]byte, 32)
	if err := keys.AddEd25519("old", seed); err != nil {
		t.Fatalf("AddEd25519: %v", err)
	}
	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "old", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(0)}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	signer := NewEnvelopeSigner(keys)
	env, err := signer.Sign("old", []byte("m"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := registry.Transition("old", StatusRetired, day(30)); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	verifier := NewEnvelopeVerifier(registry, keys)
	verifyErr := verifier.Verify([]byte("m"), env)
	var retired *RetiredSuiteError
	if !errors.As(verifyErr, &retired) {
		t.Fatalf("Verify(retired suite) = %v, want *RetiredSuiteError", verifyErr)
	}
	if retired.SuiteID != "old" {
		t.Fatalf("RetiredSuiteError.SuiteID = %q, want %q", retired.SuiteID, "old")
	}
}

func TestEnvelopeVerifier_RefusesSignatureRelabeledToAnotherSuite(t *testing.T) {
	keys := NewFakeKeySource()
	seedA := make([]byte, 32)
	seedB := make([]byte, 32)
	for i := range seedB {
		seedB[i] = 0xff
	}
	if err := keys.AddEd25519("suite-a", seedA); err != nil {
		t.Fatalf("AddEd25519 a: %v", err)
	}
	if err := keys.AddEd25519("suite-b", seedB); err != nil {
		t.Fatalf("AddEd25519 b: %v", err)
	}
	registry := NewRegistry()
	for _, id := range []string{"suite-a", "suite-b"} {
		if err := registry.Register(AlgorithmSuite{ID: id, Kind: KindSignature, Status: StatusActive, ActivatedAt: day(0)}); err != nil {
			t.Fatalf("Register(%s): %v", id, err)
		}
	}

	signer := NewEnvelopeSigner(keys)
	env, err := signer.Sign("suite-a", []byte("m"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Forge: relabel the envelope as suite-b while keeping suite-a's
	// signature bytes.
	forged := Envelope{SuiteID: "suite-b", Signature: env.Signature}

	verifier := NewEnvelopeVerifier(registry, keys)
	if err := verifier.Verify([]byte("m"), forged); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("Verify(relabeled envelope) = %v, want ErrSignatureInvalid", err)
	}
}

func TestEnvelopeVerifier_VerifyAny(t *testing.T) {
	keys := NewFakeKeySource()
	seedA := make([]byte, 32)
	seedB := make([]byte, 32)
	for i := range seedB {
		seedB[i] = 0x11
	}
	if err := keys.AddEd25519("a", seedA); err != nil {
		t.Fatalf("AddEd25519 a: %v", err)
	}
	if err := keys.AddEd25519("b", seedB); err != nil {
		t.Fatalf("AddEd25519 b: %v", err)
	}
	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "a", Kind: KindSignature, Status: StatusRetired, ActivatedAt: day(0), RetiredAt: day(10)}); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := registry.Register(AlgorithmSuite{ID: "b", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(10)}); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	signer := NewEnvelopeSigner(keys)
	envA, err := signer.Sign("a", []byte("m"))
	if err != nil {
		t.Fatalf("Sign a: %v", err)
	}
	envB, err := signer.Sign("b", []byte("m"))
	if err != nil {
		t.Fatalf("Sign b: %v", err)
	}

	verifier := NewEnvelopeVerifier(registry, keys)
	got, err := verifier.VerifyAny([]byte("m"), []Envelope{envA, envB})
	if err != nil {
		t.Fatalf("VerifyAny: %v", err)
	}
	if got.SuiteID != "b" {
		t.Fatalf("VerifyAny picked suite %q, want b (a is retired)", got.SuiteID)
	}

	if _, err := verifier.VerifyAny([]byte("m"), []Envelope{envA}); err == nil {
		t.Fatalf("VerifyAny([retired only]) = nil, want error")
	}
}
