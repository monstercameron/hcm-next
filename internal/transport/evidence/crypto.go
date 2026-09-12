package evidence

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/cryptoagility"
)

// ContractVersion names the export package's own wire/content contract.
const ContractVersion = "hcmnext.evidence-export-package/v1"

// PackageKey is the AES-256-GCM symmetric key an export package is sealed
// under. Production composition supplies this from a KMS-backed source; the
// tests in this package use a fixed key, exactly as
// internal/data/ledger/checkpoint accepts a [cryptoagility] key from its
// caller rather than owning key custody itself.
type PackageKey [32]byte

// Manifest binds every fact GREEN requires an export to bind: purpose,
// scope, the exact fields disclosed and redacted, watermarks (IssuedAt/
// ExpiresAt/RequestorSubject/IdempotencyKey) and the declared format.
type Manifest struct {
	ContractVersion      string    `json:"contract_version"`
	Tenant               string    `json:"tenant"`
	OrganizationScope    string    `json:"organization_scope,omitempty"`
	IntentRef            string    `json:"intent_ref"`
	RequestorSubject     string    `json:"requestor_subject"`
	IdempotencyKey       string    `json:"idempotency_key"`
	Purpose              string    `json:"purpose"`
	Format               string    `json:"format"`
	AllowedFields        []string  `json:"allowed_fields"`
	RedactedFields       []string  `json:"redacted_fields"`
	RedactionReason      string    `json:"redaction_reason,omitempty"`
	LineageDigest        string    `json:"lineage_digest"`
	ReceiptDigest        string    `json:"receipt_digest"`
	HumanContentDigest   string    `json:"human_content_digest"`
	MachineContentDigest string    `json:"machine_content_digest"`
	IssuedAt             time.Time `json:"issued_at"`
	ExpiresAt            time.Time `json:"expires_at"`
	ManifestDigest       string    `json:"manifest_digest"`
}

// Content is the plaintext package layout sealed inside an [Envelope]: the
// manifest plus the two rendered artifacts (human-safe CSV, exact-value
// machine JSON) internal/operations/export produced.
type Content struct {
	Manifest        Manifest `json:"manifest"`
	HumanArtifact   []byte   `json:"human_artifact"`
	MachineArtifact []byte   `json:"machine_artifact"`
}

// Envelope is the AES-256-GCM sealed form of a marshaled [Content].
type Envelope struct {
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

// Package is the encrypted, signed, offline-verifiable artifact this
// package persists through an [ArtifactSink]. Verify needs nothing but the
// package bytes, the symmetric key and the signer's public key directory:
// no database, no network, no exporter process.
type Package struct {
	Envelope  Envelope                `json:"envelope"`
	Signature cryptoagility.Signature `json:"signature"`
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestManifest(m Manifest) string {
	m.ManifestDigest = ""
	b, _ := json.Marshal(m)
	return digest(b)
}

func sealAESGCM(key PackageKey, plaintext []byte) (Envelope, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return Envelope{}, fmt.Errorf("evidence: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, fmt.Errorf("evidence: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, fmt.Errorf("evidence: nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return Envelope{Nonce: nonce, Ciphertext: ciphertext}, nil
}

func openAESGCM(key PackageKey, env Envelope) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("evidence: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("evidence: gcm: %w", err)
	}
	if len(env.Nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("%w: invalid nonce length", ErrPackageTampered)
	}
	plaintext, err := gcm.Open(nil, env.Nonce, env.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPackageTampered, err)
	}
	return plaintext, nil
}

func signedPayload(env Envelope) []byte {
	payload := make([]byte, 0, len(env.Nonce)+len(env.Ciphertext))
	payload = append(payload, env.Nonce...)
	payload = append(payload, env.Ciphertext...)
	return payload
}

// SealAndSign finalizes content's manifest digest, encrypts the whole
// content under key with AES-256-GCM, and signs the encrypted bytes with
// signer under policy. It returns the finalized content (with
// ManifestDigest populated, exactly what was sealed) alongside the package.
func SealAndSign(content Content, key PackageKey, signer cryptoagility.Key, policy cryptoagility.AlgorithmPolicy) (Content, Package, error) {
	content.Manifest.ManifestDigest = digestManifest(content.Manifest)
	plaintext, err := json.Marshal(content)
	if err != nil {
		return Content{}, Package{}, fmt.Errorf("evidence: marshal content: %w", err)
	}
	env, err := sealAESGCM(key, plaintext)
	if err != nil {
		return Content{}, Package{}, err
	}
	sig, err := cryptoagility.Sign(signedPayload(env), signer, policy)
	if err != nil {
		return Content{}, Package{}, fmt.Errorf("evidence: sign package: %w", err)
	}
	return content, Package{Envelope: env, Signature: sig}, nil
}

// OpenAndVerify is the offline verifier: it checks the signature over the
// encrypted bytes, decrypts (which independently authenticates the
// ciphertext via GCM's own tag), and recomputes the manifest digest. Any one
// of a flipped ciphertext byte, a stripped/zeroed signature, or an edited
// manifest field fails closed with [ErrPackageTampered] (or the more
// specific cryptoagility error the signature layer itself returns).
func OpenAndVerify(pkg Package, key PackageKey, keys map[string]cryptoagility.Key, policy cryptoagility.AlgorithmPolicy, at time.Time) (Content, error) {
	if err := cryptoagility.Verify(signedPayload(pkg.Envelope), pkg.Signature, keys, policy, at); err != nil {
		return Content{}, fmt.Errorf("%w: signature: %v", ErrPackageTampered, err)
	}
	plaintext, err := openAESGCM(key, pkg.Envelope)
	if err != nil {
		return Content{}, err
	}
	var content Content
	if err := json.Unmarshal(plaintext, &content); err != nil {
		return Content{}, fmt.Errorf("%w: undecodable content: %v", ErrPackageTampered, err)
	}
	if content.Manifest.ManifestDigest == "" || content.Manifest.ManifestDigest != digestManifest(content.Manifest) {
		return Content{}, fmt.Errorf("%w: manifest digest mismatch", ErrPackageTampered)
	}
	if digest(content.HumanArtifact) != content.Manifest.HumanContentDigest {
		return Content{}, fmt.Errorf("%w: human artifact digest mismatch", ErrPackageTampered)
	}
	if digest(content.MachineArtifact) != content.Manifest.MachineContentDigest {
		return Content{}, fmt.Errorf("%w: machine artifact digest mismatch", ErrPackageTampered)
	}
	return content, nil
}

// MarshalPackage/UnmarshalPackage are the exact bytes this package persists
// through an ArtifactSink and later re-reads to verify offline.
func MarshalPackage(pkg Package) ([]byte, error) { return json.Marshal(pkg) }

func UnmarshalPackage(b []byte) (Package, error) {
	var pkg Package
	if err := json.Unmarshal(b, &pkg); err != nil {
		return Package{}, fmt.Errorf("%w: undecodable package: %v", ErrPackageTampered, err)
	}
	return pkg, nil
}
