// Package evidence verifies the completeness of document and signature
// evidence packages. It is deliberately independent of a signature provider:
// provider receipts are evidence supplied to this package, not a dependency
// of verification.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Status is the outcome of completeness verification.
type Status string

const (
	Complete Status = "COMPLETE"
	Partial  Status = "PARTIAL"
	Unknown  Status = "UNKNOWN"
	Rejected Status = "REJECTED"
	// Descriptive aliases follow the naming used by other status-bearing
	// packages in the repository.
	StatusComplete Status = Complete
	StatusPartial  Status = Partial
	StatusUnknown  Status = Unknown
	StatusRejected Status = Rejected
)

// EvidenceItem identifies an immutable document, rendition, receipt, or
// signature artifact included in the package. Sequence preserves chronology.
type EvidenceItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Digest   string `json:"digest"`
	Sequence int    `json:"sequence"`
}

// Package is the portable evidence envelope. All policy and interpretation
// references are carried in the envelope so historical verification does not
// depend on a live document or signing service.
type Package struct {
	Issuer            string         `json:"issuer"`
	Freshness         string         `json:"freshness"`
	Signature         string         `json:"signature"`
	Classification    string         `json:"classification"`
	Purpose           string         `json:"purpose"`
	Retention         string         `json:"retention"`
	Hold              string         `json:"hold"`
	Method            string         `json:"method"`
	Schema            string         `json:"schema"`
	Rule              string         `json:"rule"`
	Redaction         string         `json:"redaction"`
	VerifierAuthority string         `json:"verifier_authority"`
	Items             []EvidenceItem `json:"items,omitempty"`
	Digest            string         `json:"digest"`
}

// EvidencePackage is a descriptive alias used by callers that prefer the
// domain name in their APIs.
type EvidencePackage = Package

// Verification contains a stable outcome and exact repair hints. Digest is
// always the digest of the canonical package (excluding Digest itself).
type Verification struct {
	Status  Status   `json:"status"`
	Digest  string   `json:"digest"`
	Missing []string `json:"missing,omitempty"`
	Invalid []string `json:"invalid,omitempty"`
}

var (
	ErrInvalidDigest = errors.New("document evidence: invalid package digest")
	ErrInvalid       = errors.New("document evidence: invalid package")
)

var required = []struct {
	name  string
	value func(Package) string
}{
	{"issuer", func(p Package) string { return p.Issuer }},
	{"freshness", func(p Package) string { return p.Freshness }},
	{"signature", func(p Package) string { return p.Signature }},
	{"classification", func(p Package) string { return p.Classification }},
	{"purpose", func(p Package) string { return p.Purpose }},
	{"retention", func(p Package) string { return p.Retention }},
	{"hold", func(p Package) string { return p.Hold }},
	{"method", func(p Package) string { return p.Method }},
	{"schema", func(p Package) string { return p.Schema }},
	{"rule", func(p Package) string { return p.Rule }},
	{"redaction", func(p Package) string { return p.Redaction }},
	{"verifier_authority", func(p Package) string { return p.VerifierAuthority }},
}

type canonical struct {
	Issuer, Freshness, Signature, Classification, Purpose string
	Retention, Hold, Method, Schema, Rule, Redaction      string
	VerifierAuthority                                     string
	Items                                                 []EvidenceItem `json:"items,omitempty"`
}

func (p Package) canonicalBytes() ([]byte, error) {
	return json.Marshal(canonical{p.Issuer, p.Freshness, p.Signature, p.Classification, p.Purpose, p.Retention, p.Hold, p.Method, p.Schema, p.Rule, p.Redaction, p.VerifierAuthority, p.Items})
}

// DigestBytes returns the canonical bytes covered by Digest.
func (p Package) DigestBytes() []byte { b, _ := p.canonicalBytes(); return b }

// ContentDigest computes the canonical package digest.
func (p Package) ContentDigest() string {
	b, _ := p.canonicalBytes()
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// CanonicalDigest is an explicit spelling for callers exporting verification
// results.
func (p Package) CanonicalDigest() string { return p.ContentDigest() }

// VerifyCompleteness checks metadata and the embedded canonical digest.
func (p Package) VerifyCompleteness() Verification {
	r := Verification{Status: Unknown, Digest: p.ContentDigest()}
	for _, f := range required {
		if strings.TrimSpace(f.value(p)) == "" {
			r.Missing = append(r.Missing, f.name)
		}
	}
	for i, item := range p.Items {
		if strings.TrimSpace(item.ID) == "" {
			r.Invalid = append(r.Invalid, fmt.Sprintf("items[%d].id", i))
		}
		if strings.TrimSpace(item.Kind) == "" {
			r.Invalid = append(r.Invalid, fmt.Sprintf("items[%d].kind", i))
		}
		if strings.TrimSpace(item.Digest) == "" {
			r.Invalid = append(r.Invalid, fmt.Sprintf("items[%d].digest", i))
		}
	}
	if p.Digest != "" && p.Digest != r.Digest {
		r.Invalid = append(r.Invalid, "digest")
	}
	if len(r.Invalid) > 0 {
		r.Status = Rejected
	} else if len(r.Missing) > 0 {
		// An entirely empty envelope contains no assertion from which a
		// reviewer can infer even a partial package; distinguish that case from
		// a package with some usable evidence.
		if len(r.Missing) == len(required) && len(p.Items) == 0 && p.Digest == "" {
			r.Status = Unknown
		} else {
			r.Status = Partial
		}
	} else {
		r.Status = Complete
	}
	return r
}

// Verify is a concise alias for VerifyCompleteness.
func (p Package) Verify() Verification { return p.VerifyCompleteness() }

// VerifyPackage verifies one evidence package without consulting a provider.
func VerifyPackage(p Package) Verification { return p.VerifyCompleteness() }

// New canonicalizes and returns a package with its digest populated.
func New(p Package) (Package, error) {
	if p.VerifyCompleteness().Status == Rejected {
		return Package{}, ErrInvalid
	}
	p.Digest = p.ContentDigest()
	return p, nil
}
