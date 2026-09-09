// Package provenance implements deterministic, offline release provenance.
//
// A Statement binds an artifact subject to the source revision, builder and
// toolchain that produced it, as well as the canonical SBOM document. The
// signed bytes are deliberately a canonical JSON payload; no wall clock or
// machine-local data is introduced by this package.
package provenance

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/sbom"
)

const Schema = "hcmnext.provenance.v1"

var (
	ErrUnsigned         = errors.New("provenance: unsigned statement")
	ErrInvalidSignature = errors.New("provenance: invalid signature")
	ErrPolicyIncomplete = errors.New("provenance: incomplete policy")
	ErrPolicyViolation  = errors.New("provenance: policy violation")
	ErrSubjectMismatch  = errors.New("provenance: subject mismatch")
)

// Statement is the signed release provenance payload. SBOMDigest is the
// digest of sbom.Document.Marshal(), not the artifact digest.
type Statement struct {
	Schema     string `json:"schema"`
	Subject    string `json:"subject"`
	Source     string `json:"source"`
	Build      string `json:"build"`
	Toolchain  string `json:"toolchain"`
	Builder    string `json:"builder"`
	SBOMDigest string `json:"sbom_digest"`
}

// Signed is a Statement and its detached Ed25519 signature.
type Signed struct {
	Statement Statement `json:"statement"`
	KeyID     string    `json:"key_id"`
	Signature string    `json:"signature"`
}

// Policy describes the exact release identities admitted by Verify.
type Policy struct {
	Builder   string
	Source    string
	Toolchain string
	KeyID     string
}

func (s Statement) canonical() ([]byte, error) {
	if s.Schema != Schema || s.Subject == "" || s.Source == "" || s.Build == "" || s.Toolchain == "" || s.Builder == "" || s.SBOMDigest == "" {
		return nil, ErrPolicyIncomplete
	}
	return json.Marshal(s)
}

// Digest returns the lowercase SHA-256 digest of canonical statement bytes.
func (s Statement) Digest() (string, error) {
	b, err := s.canonical()
	if err != nil {
		return "", err
	}
	d := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(d[:]), nil
}

// NewStatement validates and binds an artifact subject to an SBOM.
func NewStatement(subject, source, build, toolchain, builder string, document sbom.Document) (Statement, error) {
	b, err := document.Marshal()
	if err != nil {
		return Statement{}, fmt.Errorf("provenance: marshal SBOM: %w", err)
	}
	if err := sbom.ValidateAgainstSubject(document, subject); err != nil {
		return Statement{}, err
	}
	d := sha256.Sum256(b)
	return Statement{Schema: Schema, Subject: subject, Source: source, Build: build, Toolchain: toolchain, Builder: builder, SBOMDigest: "sha256:" + hex.EncodeToString(d[:])}, nil
}

// Sign validates the statement and returns a deterministic detached signature.
func Sign(s Statement, keyID string, privateKey ed25519.PrivateKey) (Signed, error) {
	b, err := s.canonical()
	if err != nil {
		return Signed{}, err
	}
	if keyID == "" || len(privateKey) != ed25519.PrivateKeySize {
		return Signed{}, ErrPolicyIncomplete
	}
	return Signed{Statement: s, KeyID: keyID, Signature: hex.EncodeToString(ed25519.Sign(privateKey, b))}, nil
}

// Verify checks signature, statement shape, expected subject and admission policy.
func Verify(s Signed, publicKey ed25519.PublicKey, policy Policy, expectedSubject string) error {
	b, err := s.Statement.canonical()
	if err != nil {
		return err
	}
	if s.KeyID == "" || s.Signature == "" {
		return ErrUnsigned
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return ErrInvalidSignature
	}
	sig, err := hex.DecodeString(s.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(publicKey, b, sig) {
		return ErrInvalidSignature
	}
	if expectedSubject == "" || s.Statement.Subject != expectedSubject {
		return ErrSubjectMismatch
	}
	if policy.Builder == "" || policy.Source == "" || policy.Toolchain == "" || policy.KeyID == "" {
		return ErrPolicyIncomplete
	}
	if s.KeyID != policy.KeyID || s.Statement.Builder != policy.Builder || s.Statement.Source != policy.Source || s.Statement.Toolchain != policy.Toolchain {
		return ErrPolicyViolation
	}
	return nil
}

// VerifyWithSBOM performs admission verification and additionally proves that
// the supplied SBOM is the exact document named by the signed statement.
func VerifyWithSBOM(s Signed, publicKey ed25519.PublicKey, policy Policy, document sbom.Document) error {
	if err := Verify(s, publicKey, policy, document.Subject.Digest); err != nil {
		return err
	}
	b, err := document.Marshal()
	if err != nil {
		return fmt.Errorf("provenance: marshal SBOM: %w", err)
	}
	d := sha256.Sum256(b)
	want := "sha256:" + hex.EncodeToString(d[:])
	if s.Statement.SBOMDigest != want {
		return fmt.Errorf("%w: SBOM digest %q does not match signed %q", ErrPolicyViolation, want, s.Statement.SBOMDigest)
	}
	return nil
}
