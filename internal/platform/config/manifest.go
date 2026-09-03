package config

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
)

// DependencyKind names what kind of component a Dependency pins.
type DependencyKind uint8

// Declared dependency kinds.
const (
	DependencyUnspecified DependencyKind = iota
	DependencySchema
	DependencyRule
	DependencyWorkflow
	DependencyMapping
)

var dependencyKindWire = map[DependencyKind]string{
	DependencySchema:   "SCHEMA",
	DependencyRule:     "RULE",
	DependencyWorkflow: "WORKFLOW",
	DependencyMapping:  "MAPPING",
}

// String returns the stable wire token.
func (k DependencyKind) String() string {
	if w, ok := dependencyKindWire[k]; ok {
		return w
	}
	return "DEPENDENCY_KIND_UNSPECIFIED"
}

// Valid reports whether k is a declared kind.
func (k DependencyKind) Valid() bool {
	_, ok := dependencyKindWire[k]
	return ok
}

var floatingVersionTokens = map[string]bool{
	"":       true,
	"latest": true,
	"head":   true,
	"main":   true,
	"*":      true,
}

// Dependency is one pinned, content-addressed component a configuration
// bundle depends on. Version must resolve to exactly one immutable
// artifact release, never a moving target: an empty version, a bare "latest"
// or "*" token, or a semver-range operator all fail validation as a
// floating dependency.
type Dependency struct {
	Kind    DependencyKind
	Name    string
	Version string
	// Digest is the lowercase hex sha256 of the dependency content.
	Digest string
}

func (d Dependency) validate() error {
	if !d.Kind.Valid() {
		return newError("Dependency.validate", ErrUnknownDependencyKind, "%d", uint8(d.Kind))
	}
	if d.Name == "" {
		return newError("Dependency.validate", ErrEmptyKey, "%s dependency has no name", d.Kind)
	}
	v := strings.ToLower(strings.TrimSpace(d.Version))
	if floatingVersionTokens[v] {
		return newError("Dependency.validate", ErrMissingVersion, "%s %q has no pinned version", d.Kind, d.Name)
	}
	if strings.ContainsAny(d.Version, "*^~") || strings.HasPrefix(d.Version, ">") || strings.HasPrefix(d.Version, "<") {
		return newError("Dependency.validate", ErrFloatingDependency, "%s %q version %q is a range, not a pin", d.Kind, d.Name, d.Version)
	}
	digest := strings.ToLower(d.Digest)
	if len(digest) != sha256.Size*2 {
		return newError("Dependency.validate", ErrMissingDigest, "%s %q digest has the wrong length", d.Kind, d.Name)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return newError("Dependency.validate", ErrMissingDigest, "%s %q digest is not hex", d.Kind, d.Name)
	}
	return nil
}

// CredentialRefPrefix is the required scheme for every entry in
// Bundle.CredentialRefs. A bundle carries only opaque references to
// credential material held by a separate vault or secret store, never the
// material itself; Build/validate rejects any reference that does not carry
// this prefix.
const CredentialRefPrefix = "credref://"

// Bundle is an immutable, content-addressed configuration package: a set of
// pinned dependencies, the compatibility range it targets, its signer and
// build provenance, and the opaque credential references it needs at
// install time.
type Bundle struct {
	BundleID           string
	ManifestVersion    uint32
	Dependencies       []Dependency
	CompatibilityRange string
	Signer             string
	Provenance         string
	CredentialRefs     []string
}

// OrderedDependencies returns Dependencies sorted into the manifest
// canonical dependency-graph order: by kind, then name, then version. The
// order is independent of the slice order Bundle was built with, which is
// what makes the manifest digest a function of content rather than of
// construction order.
func (b Bundle) OrderedDependencies() []Dependency {
	out := append([]Dependency(nil), b.Dependencies...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func redactForError(s string) string {
	if len(s) <= 8 {
		return "[REDACTED]"
	}
	return s[:4] + "...[REDACTED]"
}

func (b Bundle) validate() error {
	if b.BundleID == "" {
		return newError("Bundle.validate", ErrEmptyBundleID, "")
	}
	if b.CompatibilityRange == "" {
		return newError("Bundle.validate", ErrEmptyCompatibilityRange, "%s", b.BundleID)
	}
	if b.Signer == "" {
		return newError("Bundle.validate", ErrEmptySigner, "%s", b.BundleID)
	}
	seen := make(map[string]bool, len(b.Dependencies))
	for _, d := range b.Dependencies {
		if err := d.validate(); err != nil {
			return err
		}
		key := d.Kind.String() + "|" + d.Name
		if seen[key] {
			return newError("Bundle.validate", ErrDuplicateDependency, "%s %q", d.Kind, d.Name)
		}
		seen[key] = true
	}
	for _, c := range b.CredentialRefs {
		if !strings.HasPrefix(c, CredentialRefPrefix) {
			return newError("Bundle.validate", ErrRawCredential, "%q is not an opaque credential reference", redactForError(c))
		}
	}
	return nil
}

func canonicalBundleBytes(b Bundle) []byte {
	out := []byte("hcmnext.config.bundle.v1")
	out = appendStr(out, b.BundleID)
	out = binary.BigEndian.AppendUint32(out, b.ManifestVersion)
	ordered := b.OrderedDependencies()
	out = binary.BigEndian.AppendUint32(out, uint32(len(ordered)))
	for _, d := range ordered {
		out = append(out, byte(d.Kind))
		out = appendStr(out, d.Name)
		out = appendStr(out, d.Version)
		out = appendStr(out, strings.ToLower(d.Digest))
	}
	out = appendStr(out, b.CompatibilityRange)
	out = appendStr(out, b.Signer)
	out = appendStr(out, b.Provenance)
	refs := append([]string(nil), b.CredentialRefs...)
	sort.Strings(refs)
	out = appendStrs(out, refs)
	return out
}

// BundleDigest validates b and returns the immutable, content-addressed hex
// sha256 digest of its canonical encoding. The digest depends only on
// content: dependency order, and any duplicate CredentialRefs ordering, are
// canonicalized away before hashing.
func BundleDigest(b Bundle) (string, error) {
	if err := b.validate(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonicalBundleBytes(b))
	return hex.EncodeToString(sum[:]), nil
}

// VerifyStatus is the outcome of VerifyBundle.
type VerifyStatus uint8

// Declared verify outcomes.
const (
	VerifyStatusUnspecified VerifyStatus = iota
	// VerifyStatusValid reports that the bundle content matches its
	// recorded digest and the signature verifies against the supplied
	// public key.
	VerifyStatusValid
	// VerifyStatusTampered reports that the bundle content no longer
	// matches the digest recorded at signing time.
	VerifyStatusTampered
	// VerifyStatusInvalidSignature reports that the signature does not
	// verify against the recorded digest and the supplied public key.
	VerifyStatusInvalidSignature
	// VerifyStatusInvalidManifest reports that the bundle itself fails
	// validation (floating dependency, missing digest, raw credential,
	// and so on), independent of any signature question.
	VerifyStatusInvalidManifest
)

var verifyStatusWire = map[VerifyStatus]string{
	VerifyStatusValid:            "VALID",
	VerifyStatusTampered:         "TAMPERED",
	VerifyStatusInvalidSignature: "INVALID_SIGNATURE",
	VerifyStatusInvalidManifest:  "INVALID_MANIFEST",
}

// String returns the stable wire token.
func (s VerifyStatus) String() string {
	if w, ok := verifyStatusWire[s]; ok {
		return w
	}
	return "VERIFY_STATUS_UNSPECIFIED"
}

// SignedBundle is a Bundle together with the digest and ed25519 signature
// recorded at signing time. It carries no private key material and no raw
// secret: Bundle itself already excludes both, and this type adds only
// public, shareable evidence.
type SignedBundle struct {
	Bundle      Bundle
	Digest      string
	SignerKeyID string
	Signature   string
}

// SignBundle validates b, computes its digest, and signs that digest with
// priv under keyID.
func SignBundle(b Bundle, keyID string, priv ed25519.PrivateKey) (SignedBundle, error) {
	if keyID == "" {
		return SignedBundle{}, newError("SignBundle", ErrEmptySigner, "")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return SignedBundle{}, newError("SignBundle", ErrInvalidSignature, "private key has the wrong size")
	}
	digestHex, err := BundleDigest(b)
	if err != nil {
		return SignedBundle{}, err
	}
	digestBytes, err := hex.DecodeString(digestHex)
	if err != nil {
		return SignedBundle{}, newError("SignBundle", ErrInvalidSignature, "digest is not hex")
	}
	sig := ed25519.Sign(priv, digestBytes)
	return SignedBundle{
		Bundle:      b,
		Digest:      digestHex,
		SignerKeyID: keyID,
		Signature:   hex.EncodeToString(sig),
	}, nil
}

// VerifyBundle recomputes sb.Bundle canonical digest and checks it against
// sb.Digest, then checks sb.Signature against sb.Digest under pub. A
// mismatch in the first check is VerifyStatusTampered: the bundle content
// changed since it was signed. A mismatch in the second, with matching
// digests, is VerifyStatusInvalidSignature: the wrong key, a corrupted
// signature, or a forged one. A structurally invalid bundle never reaches
// either check and is reported as VerifyStatusInvalidManifest.
func VerifyBundle(sb SignedBundle, pub ed25519.PublicKey) (VerifyStatus, error) {
	if err := sb.Bundle.validate(); err != nil {
		return VerifyStatusInvalidManifest, err
	}
	recomputed, err := BundleDigest(sb.Bundle)
	if err != nil {
		return VerifyStatusInvalidManifest, err
	}
	recordedDigest := strings.ToLower(sb.Digest)
	if recomputed != recordedDigest {
		return VerifyStatusTampered, newError("VerifyBundle", ErrTamperedManifest, "")
	}
	digestBytes, err := hex.DecodeString(recordedDigest)
	if err != nil {
		return VerifyStatusInvalidManifest, newError("VerifyBundle", ErrTamperedManifest, "recorded digest is not hex")
	}
	sigBytes, err := hex.DecodeString(sb.Signature)
	if err != nil {
		return VerifyStatusInvalidSignature, newError("VerifyBundle", ErrInvalidSignature, "signature is not hex")
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, digestBytes, sigBytes) {
		return VerifyStatusInvalidSignature, newError("VerifyBundle", ErrInvalidSignature, "ed25519 verification failed")
	}
	return VerifyStatusValid, nil
}
