package provenance_test

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
)

type keySourceCustodyFake struct {
	keys map[string]ed25519.PrivateKey
}

func (f *keySourceCustodyFake) Encrypt(custody.Context, custody.Handle, []byte) (custody.Ciphertext, custody.Receipt, error) {
	return custody.Ciphertext{}, custody.Receipt{}, errors.New("unused")
}

func (f *keySourceCustodyFake) Decrypt(custody.Context, custody.Handle, custody.Ciphertext) ([]byte, custody.Receipt, error) {
	return nil, custody.Receipt{}, errors.New("unused")
}

func (f *keySourceCustodyFake) Sign(ctx custody.Context, handle custody.Handle, message []byte) (custody.Signature, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return custody.Signature{}, custody.Receipt{}, err
	}
	private, ok := f.keys[handle.ID]
	if !ok {
		return custody.Signature{}, custody.Receipt{}, fmt.Errorf("missing handle %s", handle.ID)
	}
	return custody.Signature{Handle: handle, Algorithm: provenance.AlgorithmEd25519, Data: ed25519.Sign(private, message)}, custody.Receipt{ID: "sign", Handle: handle, Operation: custody.Sign, At: time.Now().UTC()}, nil
}

func (f *keySourceCustodyFake) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	return false, custody.Receipt{}, errors.New("unused")
}

func (f *keySourceCustodyFake) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}

func (f *keySourceCustodyFake) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}

func (f *keySourceCustodyFake) Rotate(custody.Context, custody.Handle) (custody.Handle, custody.Receipt, error) {
	return custody.Handle{}, custody.Receipt{}, errors.New("unused")
}

func (f *keySourceCustodyFake) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("unused")
}

var _ custody.Provider = (*keySourceCustodyFake)(nil)

func keySourceContext() custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{
		Workload:    "release-builder",
		Tenant:      "release",
		Region:      "us-east-1",
		Purpose:     "release-signing",
		Destination: "release-admission",
	}}
}

func keySourceHandle(version string) custody.Handle {
	return custody.Handle{ID: "release-key", Kind: custody.Key, Version: version, Tenant: "release", Region: "us-east-1"}
}

func keySourceStatement() provenance.Statement {
	return provenance.Statement{
		SchemaVersion: provenance.SchemaVersion,
		PredicateType: provenance.PredicateType,
		GeneratedAt:   "2026-09-05T00:00:00Z",
		Subjects:      []provenance.Subject{{Name: "hcmnext", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		Builder:       provenance.Builder{ID: provenance.BuilderID},
		Source:        provenance.SourceRef{Repository: provenance.RootModulePath, Ref: "refs/heads/main", Commit: "commit"},
		BuildConfig:   provenance.BuildConfig{GoVersion: "go1.26.3", GOOS: "windows", GOARCH: "arm64", ConfigDigest: "config"},
		SBOM:          provenance.SBOMReference{Path: "sbom.json", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}
}

func TestTodo_SECARCH_005(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = 7
	}
	private := ed25519.NewKeyFromSeed(seed)
	fixture, err := provenance.NewFixtureKeySource(private, "test-fixture")
	if err != nil {
		t.Fatalf("NewFixtureKeySource: %v", err)
	}
	signed, err := provenance.SignStatementWithKeySource(fixture, keySourceStatement(), "test-fixture")
	if err != nil {
		t.Fatalf("SignStatementWithKeySource: %v", err)
	}
	if err := provenance.Verify(signed, provenance.VerifyOptions{TrustedPublicKeys: map[string]bool{fixture.PublicKey(): true}}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestTodo_SECARCH_005_Golden(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 8
	private := ed25519.NewKeyFromSeed(seed)
	source, err := provenance.NewFixtureKeySource(private, "golden")
	if err != nil {
		t.Fatalf("NewFixtureKeySource: %v", err)
	}
	signed, err := provenance.SignStatementWithKeySource(source, keySourceStatement(), "golden")
	if err != nil {
		t.Fatalf("SignStatementWithKeySource: %v", err)
	}
	if signed.Signature == nil || signed.Signature.PublicKey != source.PublicKey() || signed.Signature.Value == "" {
		t.Fatalf("signature = %+v, want source public key and value", signed.Signature)
	}
}

func TestTodo_SECARCH_005_Integration(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 9
	private := ed25519.NewKeyFromSeed(seed)
	fake := &keySourceCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key": private}}
	publicKey := private.Public().(ed25519.PublicKey)
	source, err := provenance.NewCustodyKeySource(fake, keySourceContext(), keySourceHandle("v1"), fmt.Sprintf("%x", publicKey))
	if err != nil {
		t.Fatalf("NewCustodyKeySource: %v", err)
	}
	signed, err := provenance.SignStatementWithKeySource(source, keySourceStatement(), "custody:v1")
	if err != nil {
		t.Fatalf("SignStatementWithKeySource: %v", err)
	}
	if err := provenance.Verify(signed, provenance.VerifyOptions{TrustedPublicKeys: map[string]bool{source.PublicKey(): true}}); err != nil {
		t.Fatalf("custody-backed Verify: %v", err)
	}
}

func TestTodo_SECARCH_005_Security(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	private := ed25519.NewKeyFromSeed(seed)
	fake := &keySourceCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key": private}}
	if _, err := provenance.NewCustodyKeySource(fake, keySourceContext(), keySourceHandle("v1"), "not-a-public-key"); err == nil || !containsField(err.Error(), "public_key") {
		t.Fatalf("invalid public_key error = %v, want field name", err)
	}
	source, err := provenance.NewCustodyKeySource(fake, keySourceContext(), keySourceHandle("v1"), fmt.Sprintf("%x", private.Public()))
	if err != nil {
		t.Fatalf("NewCustodyKeySource: %v", err)
	}
	signed, err := provenance.SignStatementWithKeySource(source, keySourceStatement(), "custody")
	if err != nil {
		t.Fatalf("SignStatementWithKeySource: %v", err)
	}
	signed.Subjects[0].SHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if err := provenance.Verify(signed, provenance.VerifyOptions{TrustedPublicKeys: map[string]bool{source.PublicKey(): true}}); err == nil {
		t.Fatal("tampered custody-backed statement was admitted")
	}
}

func TestTodo_SECARCH_005_Mutation(t *testing.T) {
	if _, err := provenance.SignStatementWithKeySource(nil, keySourceStatement(), ""); err == nil {
		t.Fatal("nil key source was accepted")
	}
}

func TestFixtureKeySourceRejectsMalformedInputAndCopiesPrivateKey(t *testing.T) {
	if _, err := provenance.NewFixtureKeySource(nil, "nil"); err == nil {
		t.Fatal("nil fixture private key was accepted")
	}

	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{4}, ed25519.SeedSize))
	wantPublic := fmt.Sprintf("%x", private.Public())
	source, err := provenance.NewFixtureKeySource(private, "  fixture  ")
	if err != nil {
		t.Fatalf("NewFixtureKeySource: %v", err)
	}
	private[0] ^= 0xff
	if source.PublicKey() != wantPublic {
		t.Fatalf("PublicKey changed after caller mutation: got %q, want %q", source.PublicKey(), wantPublic)
	}
	if _, err := source.SignDigest("not-hex"); err == nil {
		t.Fatal("SignDigest accepted a malformed digest")
	}
	if signature, err := source.SignDigest(strings.Repeat("00", 32)); err != nil || len(signature) != ed25519.SignatureSize*2 {
		t.Fatalf("SignDigest(valid digest) = %q, %v", signature, err)
	}

	var nilSource *provenance.FixtureKeySource
	if nilSource.PublicKey() != "" {
		t.Fatal("nil FixtureKeySource returned a public key")
	}
	if _, err := nilSource.SignDigest(strings.Repeat("00", 32)); err == nil {
		t.Fatal("nil FixtureKeySource signed a digest")
	}
}

func TestLoadFixtureKeySourceAndCustodySourceValidation(t *testing.T) {
	loaded, err := provenance.LoadFixtureKeySource(devSigningKeyFixture)
	if err != nil {
		t.Fatalf("LoadFixtureKeySource: %v", err)
	}
	if loaded.PublicKey() == "" {
		t.Fatal("LoadFixtureKeySource returned an empty public key")
	}

	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize))
	public := fmt.Sprintf("%x", private.Public())
	fake := &keySourceCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key": private}}
	cases := []struct {
		name     string
		provider custody.Provider
		context  custody.Context
		handle   custody.Handle
		public   string
		wantText string
	}{
		{"nil provider", nil, keySourceContext(), keySourceHandle("v1"), public, "requires a provider"},
		{"invalid context", fake, custody.Context{}, keySourceHandle("v1"), public, "context"},
		{"invalid handle", fake, keySourceContext(), custody.Handle{}, public, "handle"},
		{"wrong kind", fake, keySourceContext(), func() custody.Handle { h := keySourceHandle("v1"); h.Kind = custody.Secret; return h }(), public, "handle.kind"},
		{"tenant mismatch", fake, keySourceContext(), func() custody.Handle { h := keySourceHandle("v1"); h.Tenant = "other"; return h }(), public, "scope mismatch"},
		{"region mismatch", fake, keySourceContext(), func() custody.Handle { h := keySourceHandle("v1"); h.Region = "eu-west-1"; return h }(), public, "scope mismatch"},
		{"bad public hex", fake, keySourceContext(), keySourceHandle("v1"), "not-hex", "public_key"},
		{"short public key", fake, keySourceContext(), keySourceHandle("v1"), strings.Repeat("00", ed25519.PublicKeySize-1), "public_key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := provenance.NewCustodyKeySource(tc.provider, tc.context, tc.handle, tc.public); err == nil || !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("NewCustodyKeySource error = %v, want %q", err, tc.wantText)
			}
		})
	}

	source, err := provenance.NewCustodyKeySource(fake, keySourceContext(), keySourceHandle("v1"), strings.ToUpper(public))
	if err != nil {
		t.Fatalf("NewCustodyKeySource(uppercase public key): %v", err)
	}
	if source.PublicKey() != public || source.Explain() == "" {
		t.Fatalf("custody source identity = %q, explanation = %q", source.PublicKey(), source.Explain())
	}
	if _, err := source.SignDigest("not-hex"); err == nil {
		t.Fatal("custody source accepted a malformed digest")
	}
	if _, err := source.SignDigest(strings.Repeat("00", 31)); err == nil {
		t.Fatal("custody source accepted a non-sha256 digest")
	}
	missing := &keySourceCustodyFake{}
	missingSource, err := provenance.NewCustodyKeySource(missing, keySourceContext(), keySourceHandle("v1"), public)
	if err != nil {
		t.Fatalf("NewCustodyKeySource(missing key): %v", err)
	}
	if _, err := missingSource.SignDigest(strings.Repeat("00", 32)); err == nil || !strings.Contains(err.Error(), "custody sign") {
		t.Fatalf("missing custody key error = %v, want custody sign error", err)
	}

	var nilSource *provenance.CustodyKeySource
	if nilSource.PublicKey() != "" || nilSource.Explain() == "" {
		t.Fatal("nil CustodyKeySource methods returned invalid values")
	}
	if _, err := nilSource.SignDigest(strings.Repeat("00", 32)); err == nil {
		t.Fatal("nil CustodyKeySource signed a digest")
	}
}

type malformedCustodyProvider struct {
	keySourceCustodyFake
	signature custody.Signature
	err       error
}

func (f *malformedCustodyProvider) Sign(custody.Context, custody.Handle, []byte) (custody.Signature, custody.Receipt, error) {
	return f.signature, custody.Receipt{}, f.err
}

func TestCustodyKeySourceRejectsMalformedProviderSignatures(t *testing.T) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{6}, ed25519.SeedSize))
	handle := keySourceHandle("v1")
	public := fmt.Sprintf("%x", private.Public())
	cases := []struct {
		name      string
		signature custody.Signature
		wantText  string
	}{
		{"wrong handle", custody.Signature{Handle: keySourceHandle("v2"), Algorithm: provenance.AlgorithmEd25519, Data: make([]byte, ed25519.SignatureSize)}, "does not match"},
		{"wrong algorithm", custody.Signature{Handle: handle, Algorithm: "rsa", Data: make([]byte, ed25519.SignatureSize)}, "algorithm"},
		{"wrong length", custody.Signature{Handle: handle, Algorithm: provenance.AlgorithmEd25519, Data: []byte{1}}, "signature.value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &malformedCustodyProvider{signature: tc.signature}
			source, err := provenance.NewCustodyKeySource(provider, keySourceContext(), handle, public)
			if err != nil {
				t.Fatalf("NewCustodyKeySource: %v", err)
			}
			_, err = source.SignDigest(strings.Repeat("00", 32))
			if err == nil || !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("SignDigest error = %v, want %q", err, tc.wantText)
			}
		})
	}
	providerError := &malformedCustodyProvider{err: errors.New("provider unavailable")}
	source, err := provenance.NewCustodyKeySource(providerError, keySourceContext(), handle, public)
	if err != nil {
		t.Fatalf("NewCustodyKeySource(provider error): %v", err)
	}
	if _, err := source.SignDigest(strings.Repeat("00", 32)); err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("provider error = %v, want wrapped provider error", err)
	}
}

type stubKeySource struct {
	public string
	sig    string
	err    error
}

func (s stubKeySource) PublicKey() string                 { return s.public }
func (s stubKeySource) SignDigest(string) (string, error) { return s.sig, s.err }

func TestSignStatementWithKeySourceRejectsBadSourceOutputs(t *testing.T) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	public := fmt.Sprintf("%x", private.Public())
	cases := []struct {
		name   string
		source stubKeySource
		want   string
	}{
		{"bad public key", stubKeySource{public: "not-hex", sig: strings.Repeat("00", ed25519.SignatureSize)}, "public_key"},
		{"signing error", stubKeySource{public: public, err: errors.New("signing failed")}, "signing failed"},
		{"bad signature", stubKeySource{public: public, sig: "00"}, "signature.value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := provenance.SignStatementWithKeySource(tc.source, keySourceStatement(), "label")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("SignStatementWithKeySource error = %v, want %q", err, tc.want)
			}
		})
	}
}

func containsField(message, field string) bool {
	return len(message) >= len(field) && (message == field || strings.Contains(message, field))
}
