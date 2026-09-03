package attest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidBinding identifies a request that cannot be bound to an
// attestation.  A binding is never produced for an incomplete request.
var ErrInvalidBinding = errors.New("attest: invalid evidence binding")

// PresentedArtifact is an immutable identity for an artifact shown to the
// respondent.  Digest is the content digest (not a mutable URL or filename).
type PresentedArtifact struct {
	Ref     string
	Version string
	Kind    string
	Digest  string
}

// Evidence is a proof artifact presented with the response.
type Evidence struct {
	Ref     string
	Version string
	Kind    string
	Digest  string
}

// Context is the material execution context at presentation time. Every
// field is deliberately explicit: changing time, tenant, purpose, policy or
// authorization context creates a new binding.
type Context struct {
	Tenant          string
	Subject         string
	Purpose         string
	EffectiveFrom   time.Time
	EffectiveTo     time.Time
	Timezone        string
	TzdbVersion     string
	CalendarVersion string
	AuthZDigest     string
	PolicyDigest    string
}

// Request is the complete material request shown to an attestor. Slices are
// ordered presentation lists; their order is therefore part of the digest.
type Request struct {
	StatementID      string
	StatementVersion string
	StatementDigest  string
	Facts            []PresentedArtifact
	Attachments      []PresentedArtifact
	Evidence         []Evidence
	Context          Context
}

// Binding is the immutable receipt that ties a response to one exact request.
type Binding struct {
	Digest string
}

// BindingError identifies the first offending field in an invalid request.
type BindingError struct {
	Field  string
	Detail string
}

func (e *BindingError) Error() string {
	return fmt.Sprintf("attest: binding %s: %s", e.Field, e.Detail)
}
func (e *BindingError) Unwrap() error { return ErrInvalidBinding }

func invalidBinding(field, detail string) error {
	return &BindingError{Field: field, Detail: detail}
}

func (r Request) validate() error {
	if strings.TrimSpace(r.StatementID) == "" {
		return invalidBinding("statement_id", "statement id is empty")
	}
	if strings.TrimSpace(r.StatementVersion) == "" {
		return invalidBinding("statement_version", "statement version is empty")
	}
	if strings.TrimSpace(r.StatementDigest) == "" {
		return invalidBinding("statement_digest", "statement digest is empty")
	}
	if len(r.Facts) == 0 {
		return invalidBinding("facts", "no presented facts")
	}
	for i, a := range r.Facts {
		if err := validateArtifact(fmt.Sprintf("facts[%d]", i), a.Ref, a.Version, a.Kind, a.Digest); err != nil {
			return err
		}
	}
	for i, a := range r.Attachments {
		if err := validateArtifact(fmt.Sprintf("attachments[%d]", i), a.Ref, a.Version, a.Kind, a.Digest); err != nil {
			return err
		}
	}
	for i, e := range r.Evidence {
		if err := validateArtifact(fmt.Sprintf("evidence[%d]", i), e.Ref, e.Version, e.Kind, e.Digest); err != nil {
			return err
		}
	}
	c := r.Context
	for _, item := range [][2]string{{"tenant", c.Tenant}, {"subject", c.Subject}, {"purpose", c.Purpose}, {"timezone", c.Timezone}, {"tzdb_version", c.TzdbVersion}, {"calendar_version", c.CalendarVersion}, {"authz_digest", c.AuthZDigest}, {"policy_digest", c.PolicyDigest}} {
		if strings.TrimSpace(item[1]) == "" {
			return invalidBinding("context."+item[0], "context field is empty")
		}
	}
	if c.EffectiveFrom.IsZero() || c.EffectiveTo.IsZero() || !c.EffectiveFrom.Before(c.EffectiveTo) {
		return invalidBinding("context.effective_time", "effective period must be ordered and non-zero")
	}
	return nil
}

func validateArtifact(field, ref, version, kind, digest string) error {
	if strings.TrimSpace(ref) == "" {
		return invalidBinding(field+".ref", "artifact reference is empty")
	}
	if strings.TrimSpace(version) == "" {
		return invalidBinding(field+".version", "artifact version is empty")
	}
	if strings.TrimSpace(kind) == "" {
		return invalidBinding(field+".kind", "artifact kind is empty")
	}
	if strings.TrimSpace(digest) == "" {
		return invalidBinding(field+".digest", "artifact digest is empty")
	}
	return nil
}

func frame(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "%s=%d:", label, len(value))
	b.WriteString(value)
	b.WriteByte(';')
}

func (r Request) canonical() string {
	var b strings.Builder
	frame(&b, "statement_id", r.StatementID)
	frame(&b, "statement_version", r.StatementVersion)
	frame(&b, "statement_digest", r.StatementDigest)
	writeArtifact := func(prefix, ref, version, kind, digest string) {
		frame(&b, prefix+".ref", ref)
		frame(&b, prefix+".version", version)
		frame(&b, prefix+".kind", kind)
		frame(&b, prefix+".digest", digest)
	}
	fmt.Fprintf(&b, "facts.count=%d;", len(r.Facts))
	for i, a := range r.Facts {
		writeArtifact(fmt.Sprintf("facts[%d]", i), a.Ref, a.Version, a.Kind, a.Digest)
	}
	fmt.Fprintf(&b, "attachments.count=%d;", len(r.Attachments))
	for i, a := range r.Attachments {
		writeArtifact(fmt.Sprintf("attachments[%d]", i), a.Ref, a.Version, a.Kind, a.Digest)
	}
	fmt.Fprintf(&b, "evidence.count=%d;", len(r.Evidence))
	for i, e := range r.Evidence {
		writeArtifact(fmt.Sprintf("evidence[%d]", i), e.Ref, e.Version, e.Kind, e.Digest)
	}
	c := r.Context
	for _, x := range [][2]string{{"tenant", c.Tenant}, {"subject", c.Subject}, {"purpose", c.Purpose}, {"timezone", c.Timezone}, {"tzdb_version", c.TzdbVersion}, {"calendar_version", c.CalendarVersion}, {"authz_digest", c.AuthZDigest}, {"policy_digest", c.PolicyDigest}} {
		frame(&b, "context."+x[0], x[1])
	}
	fmt.Fprintf(&b, "context.from=%d;context.to=%d;", c.EffectiveFrom.UTC().UnixNano(), c.EffectiveTo.UTC().UnixNano())
	return b.String()
}

// Digest returns the SHA-256 digest of the complete, framed request.
func (r Request) Digest() (string, error) {
	if err := r.validate(); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(r.canonical()))
	return hex.EncodeToString(sum[:]), nil
}

// Bind validates and returns an immutable digest receipt for the request.
func Bind(r Request) (Binding, error) {
	d, err := r.Digest()
	if err != nil {
		return Binding{}, err
	}
	return Binding{Digest: d}, nil
}

// VerifyBinding checks that the request is complete and byte-for-byte matches the
// previously issued binding. Any statement, artifact, version, or context
// change is rejected.
func VerifyBinding(r Request, b Binding) error {
	if strings.TrimSpace(b.Digest) == "" {
		return invalidBinding("digest", "binding digest is empty")
	}
	d, err := r.Digest()
	if err != nil {
		return err
	}
	if d != b.Digest {
		return invalidBinding("digest", "request does not match binding")
	}
	return nil
}
