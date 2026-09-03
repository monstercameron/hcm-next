// Package contact owns the semantic representation of personal contact
// endpoints. Provider and transport packages may carry raw input, but they do
// not own normalization, verification, or primary-selection meaning.
package contact

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"golang.org/x/text/unicode/norm"
)

type EndpointType string

const (
	EndpointEmail EndpointType = "EMAIL"
	EndpointPhone EndpointType = "PHONE"
)

type VerificationState string

const (
	Unverified VerificationState = "UNVERIFIED"
	Verified   VerificationState = "VERIFIED"
)

var (
	ErrInvalidEndpoint        = errors.New("contact: invalid endpoint")
	ErrAmbiguousNormalization = errors.New("contact: ambiguous endpoint normalization")
	ErrInvalidPurpose         = errors.New("contact: purpose is required")
)

// EndpointRevision is an immutable, normalized revision of one endpoint.
// ValueNormalized is deliberately retained separately from ValueMasked: the
// former is for matching and challenge binding, the latter is safe for UI and
// evidence views. Source is an authority label, not an untrusted provider blob.
type EndpointRevision struct {
	Subject         values.EntityRef
	EndpointID      string
	Type            EndpointType
	Purpose         string
	Priority        int
	Source          string
	ValueNormalized string
	ValueMasked     string
	Verification    VerificationState
}

// Normalized returns the canonical matching value.
func (e EndpointRevision) Normalized() string { return e.ValueNormalized }

// Masked returns the presentation-safe value.
func (e EndpointRevision) Masked() string { return e.ValueMasked }

// IsVerified reports whether this revision has a completed proof.
func (e EndpointRevision) IsVerified() bool { return e.Verification == Verified }

// NormalizeEndpoint validates and canonicalizes an email address or phone
// number. It rejects input where punctuation, whitespace, or Unicode would
// make two materially different values collapse to one value.
func NormalizeEndpoint(typ EndpointType, raw string) (normalized, masked string, err error) {
	s := norm.NFC.String(strings.TrimSpace(raw))
	if s == "" || s != raw && strings.TrimSpace(raw) != raw {
		return "", "", fmt.Errorf("%w: surrounding whitespace", ErrAmbiguousNormalization)
	}
	switch typ {
	case EndpointEmail:
		if strings.Count(s, "@") != 1 || strings.ContainsAny(s, "\r\n\t ") {
			return "", "", fmt.Errorf("%w: email syntax", ErrInvalidEndpoint)
		}
		parts := strings.SplitN(s, "@", 2)
		if parts[0] == "" || parts[1] == "" || strings.Contains(parts[1], "@") || strings.Contains(parts[1], ".") == false {
			return "", "", fmt.Errorf("%w: email syntax", ErrInvalidEndpoint)
		}
		for _, r := range s {
			if unicode.IsSpace(r) || unicode.IsControl(r) {
				return "", "", fmt.Errorf("%w: email contains whitespace/control", ErrInvalidEndpoint)
			}
		}
		normalized = strings.ToLower(s)
		masked = maskEmail(normalized)
	case EndpointPhone:
		if !strings.HasPrefix(s, "+") {
			return "", "", fmt.Errorf("%w: phone must use international form", ErrInvalidEndpoint)
		}
		var b strings.Builder
		for i, r := range s[1:] {
			if r < '0' || r > '9' {
				return "", "", fmt.Errorf("%w: phone contains non-digit at %d", ErrInvalidEndpoint, i)
			}
			b.WriteRune(r)
		}
		if n := b.Len(); n < 7 || n > 15 || b.String()[0] == '0' {
			return "", "", fmt.Errorf("%w: phone digit count", ErrInvalidEndpoint)
		}
		normalized = "+" + b.String()
		masked = "+" + strings.Repeat("*", len(normalized)-4) + normalized[len(normalized)-4:]
	default:
		return "", "", fmt.Errorf("%w: unsupported endpoint type %q", ErrInvalidEndpoint, typ)
	}
	return normalized, masked, nil
}

func maskEmail(s string) string {
	p := strings.LastIndexByte(s, '@')
	local := s[:p]
	if len(local) <= 2 {
		return strings.Repeat("*", len(local)) + s[p:]
	}
	return local[:1] + strings.Repeat("*", len(local)-2) + local[len(local)-1:] + s[p:]
}

func (e EndpointRevision) Validate() error {
	if err := e.Subject.Validate(); err != nil {
		return fmt.Errorf("contact: subject: %w", err)
	}
	if e.EndpointID == "" || strings.TrimSpace(e.EndpointID) != e.EndpointID {
		return fmt.Errorf("%w: endpoint id", ErrInvalidEndpoint)
	}
	if e.Purpose == "" {
		return ErrInvalidPurpose
	}
	if e.Priority < 0 {
		return fmt.Errorf("%w: negative priority", ErrInvalidEndpoint)
	}
	if e.Source == "" {
		return fmt.Errorf("%w: source is required", ErrInvalidEndpoint)
	}
	if e.Verification != Unverified && e.Verification != Verified {
		return fmt.Errorf("%w: verification state", ErrInvalidEndpoint)
	}
	n, m, err := NormalizeEndpoint(e.Type, e.ValueNormalized)
	if err != nil || n != e.ValueNormalized || m != e.ValueMasked {
		return fmt.Errorf("%w: revision is not normalized", ErrAmbiguousNormalization)
	}
	return nil
}

// NewEndpointRevision creates an unverified endpoint revision.
func NewEndpointRevision(subject values.EntityRef, id string, typ EndpointType, raw, purpose string, priority int, source string) (EndpointRevision, error) {
	n, m, err := NormalizeEndpoint(typ, raw)
	if err != nil {
		return EndpointRevision{}, err
	}
	e := EndpointRevision{Subject: subject, EndpointID: id, Type: typ, Purpose: purpose, Priority: priority, Source: source, ValueNormalized: n, ValueMasked: m, Verification: Unverified}
	return e, e.Validate()
}
