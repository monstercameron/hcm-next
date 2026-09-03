// Package contractarchive stores executable contracts as self-contained,
// content-addressed evidence. It deliberately has no dependency on a live
// registry, compiler, network, or activation runtime.
package contractarchive

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const DigestAlgorithm = "sha256"

var (
	ErrTampered    = errors.New("contract archive is tampered")
	ErrUnsupported = errors.New("contract archive uses an unsupported algorithm or profile")
)

// Artifact is one immutable executable contract and all information needed to
// interpret it after the live publication systems have retired it.
type Artifact struct {
	Kind            string            `json:"kind"`
	ID              string            `json:"id"`
	Version         string            `json:"version"`
	Source          []byte            `json:"source"`
	Descriptor      []byte            `json:"descriptor"`
	CompilerProfile string            `json:"compiler_profile"`
	ToolProfile     string            `json:"tool_profile"`
	Compatibility   map[string]string `json:"compatibility,omitempty"`
}

// Signature retains the public verification material, rather than only a key
// identifier, so key rotation cannot make old evidence unverifiable.
type Signature struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey []byte `json:"public_key"`
	Value     []byte `json:"value"`
}

type TrustRecord struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey []byte `json:"public_key"`
	Status    string `json:"status"`
}

// Package is an immutable archive envelope. Use New to construct one; all
// byte slices returned by Decode and Artifacts are defensive copies.
type Package struct {
	FormatVersion string        `json:"format_version"`
	Profile       string        `json:"profile"`
	Artifacts     []Artifact    `json:"artifacts"`
	TrustHistory  []TrustRecord `json:"trust_history,omitempty"`
	Signatures    []Signature   `json:"signatures,omitempty"`
	Digest        string        `json:"digest"`
}

type canonicalPackage struct {
	FormatVersion string        `json:"format_version"`
	Profile       string        `json:"profile"`
	Artifacts     []Artifact    `json:"artifacts"`
	TrustHistory  []TrustRecord `json:"trust_history,omitempty"`
}

func New(formatVersion, profile string, artifacts []Artifact, trust []TrustRecord) (Package, error) {
	if formatVersion == "" || profile == "" || len(artifacts) == 0 {
		return Package{}, errors.New("format, profile, and artifacts are required")
	}
	a := cloneArtifacts(artifacts)
	sort.Slice(a, func(i, j int) bool {
		return a[i].Kind+"\x00"+a[i].ID+"\x00"+a[i].Version < a[j].Kind+"\x00"+a[j].ID+"\x00"+a[j].Version
	})
	for i := 1; i < len(a); i++ {
		if a[i-1].Kind == a[i].Kind && a[i-1].ID == a[i].ID && a[i-1].Version == a[i].Version {
			return Package{}, errors.New("duplicate artifact identity")
		}
	}
	t := cloneTrust(trust)
	sort.Slice(t, func(i, j int) bool { return t[i].KeyID < t[j].KeyID })
	p := Package{FormatVersion: formatVersion, Profile: profile, Artifacts: a, TrustHistory: t}
	b, err := p.canonicalBytes()
	if err != nil {
		return Package{}, err
	}
	p.Digest = digest(b)
	return p, nil
}

func (p Package) canonicalBytes() ([]byte, error) {
	return json.Marshal(canonicalPackage{p.FormatVersion, p.Profile, p.Artifacts, p.TrustHistory})
}

func (p Package) DigestBytes() []byte   { b, _ := p.canonicalBytes(); return append([]byte(nil), b...) }
func (p Package) ContentDigest() string { b, _ := p.canonicalBytes(); return digest(b) }

// CanonicalBytes returns the exact bytes covered by Digest. Signatures are
// excluded so key rotation adds attestations without changing identity.
func (p Package) CanonicalBytes() []byte { return p.DigestBytes() }

// ArchiveID is the content address of the canonical envelope.
func (p Package) ArchiveID() string { return p.ContentDigest() }

// NewArchivePackage and VerifyOffline are descriptive aliases used by
// publication and evidence consumers.
func NewArchivePackage(formatVersion, profile string, artifacts []Artifact, trust []TrustRecord) (Package, error) {
	return New(formatVersion, profile, artifacts, trust)
}
func (p Package) VerifyOffline() Verification { return p.Verify() }

func (p Package) AddSignature(keyID string, private ed25519.PrivateKey) (Package, error) {
	if len(private) != ed25519.PrivateKeySize {
		return Package{}, errors.New("invalid ed25519 private key")
	}
	if p.ContentDigest() != p.Digest {
		return Package{}, ErrTampered
	}
	s := Signature{KeyID: keyID, Algorithm: "ed25519", PublicKey: append([]byte(nil), private.Public().(ed25519.PublicKey)...)}
	s.Value = ed25519.Sign(private, []byte(p.Digest))
	p.Signatures = append(append([]Signature(nil), p.Signatures...), s)
	return p, nil
}

type Verification struct {
	Digest         string
	SignatureCount int
	Valid          bool
	Err            error
}

func (p Package) Verify() Verification {
	r := Verification{Digest: p.ContentDigest(), SignatureCount: len(p.Signatures)}
	if p.Digest == "" || p.Digest != r.Digest {
		r.Err = ErrTampered
		return r
	}
	if len(p.Signatures) == 0 {
		r.Err = errors.New("archive has no signature")
		return r
	}
	for _, s := range p.Signatures {
		if s.Algorithm != "ed25519" || len(s.PublicKey) != ed25519.PublicKeySize {
			r.Err = ErrUnsupported
			return r
		}
		if !ed25519.Verify(ed25519.PublicKey(s.PublicKey), []byte(p.Digest), s.Value) {
			r.Err = ErrTampered
			return r
		}
	}
	r.Valid = true
	return r
}

func Encode(p Package) ([]byte, error) {
	if p.Verify().Err != nil {
		return nil, p.Verify().Err
	}
	return json.Marshal(p)
}
func Decode(b []byte) (Package, error) {
	var p Package
	if err := json.Unmarshal(b, &p); err != nil {
		return Package{}, fmt.Errorf("decode archive: %w", err)
	}
	p.Artifacts = cloneArtifacts(p.Artifacts)
	p.TrustHistory = cloneTrust(p.TrustHistory)
	return p, nil
}
func digest(b []byte) string {
	s := sha256.Sum256(b)
	return DigestAlgorithm + ":" + hex.EncodeToString(s[:])
}
func cloneArtifacts(in []Artifact) []Artifact {
	out := append([]Artifact(nil), in...)
	for i := range out {
		out[i].Source = append([]byte(nil), in[i].Source...)
		out[i].Descriptor = append([]byte(nil), in[i].Descriptor...)
		if in[i].Compatibility != nil {
			out[i].Compatibility = map[string]string{}
			for k, v := range in[i].Compatibility {
				out[i].Compatibility[k] = v
			}
		}
	}
	return out
}
func cloneTrust(in []TrustRecord) []TrustRecord {
	out := append([]TrustRecord(nil), in...)
	for i := range out {
		out[i].PublicKey = append([]byte(nil), in[i].PublicKey...)
	}
	return out
}
