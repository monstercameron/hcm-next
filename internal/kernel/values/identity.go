package values

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

// CanonicalEncodingVersion is the version tag embedded in every canonical
// identifier text encoding. A change to any encoding rule in this file requires
// a new version; historical text keeps decoding under its own version.
const CanonicalEncodingVersion = 1

const canonicalVersionTag = "v1"

// Canonical text prefixes. Each identifier kind has its own prefix so that a
// value of one kind can never be mistaken for, or forged from, another.
const (
	entityIDPrefix      = "eid:" + canonicalVersionTag + ":"
	entityRefPrefix     = "eref:" + canonicalVersionTag + ":"
	resourceKeyPrefix   = "rkey:" + canonicalVersionTag + ":"
	revisionTokenPrefix = "rev:" + canonicalVersionTag + ":"

	// revisionUnspecifiedTag is the reserved body of an unspecified revision
	// token. It is not a legal stream name.
	revisionUnspecifiedTag = "unspecified"
)

// Identifier validation errors. All are matchable with errors.Is.
var (
	ErrEmptyKind              = errors.New("values: entity kind is empty")
	ErrInvalidKind            = errors.New("values: entity kind is not a canonical lower_snake identifier")
	ErrEmptyID                = errors.New("values: entity id is empty")
	ErrInvalidID              = errors.New("values: entity id is not a canonical UUID or ULID")
	ErrTenantRequired         = errors.New("values: tenant is required")
	ErrInvalidTenant          = errors.New("values: tenant is not a canonical slug")
	ErrEmptyResourceKey       = errors.New("values: resource key needs at least one non-empty segment")
	ErrInvalidUTF8            = errors.New("values: string is not valid UTF-8")
	ErrNotNFC                 = errors.New("values: string is not Unicode NFC normalized")
	ErrCanonicalFormat        = errors.New("values: canonical text is malformed")
	ErrAmbiguousRevision      = errors.New("values: revision selector is ambiguous")
	ErrRevisionStreamRequired = errors.New("values: revision token needs a stream")
	ErrRevisionStreamMismatch = errors.New("values: revision tokens belong to different streams")
	ErrRevisionUnspecified    = errors.New("values: revision token is unspecified")
	ErrRevisionNotOrdered     = errors.New("values: opaque revision tokens are not ordered")
)

// Kind is a stable, registry-owned entity or resource type name. Canonical form
// is lower_snake_case starting with a letter; display labels are never a kind.
type Kind string

// Validate reports whether k is a canonical kind name.
func (k Kind) Validate() error {
	if k == "" {
		return ErrEmptyKind
	}
	if len(k) > 63 {
		return fmt.Errorf("%w: %q is longer than 63 bytes", ErrInvalidKind, string(k))
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_'):
		default:
			return fmt.Errorf("%w: %q", ErrInvalidKind, string(k))
		}
	}
	return nil
}

// String returns the kind name.
func (k Kind) String() string { return string(k) }

// TenantId identifies the tenant partition that scopes an identifier. Canonical
// form is a lowercase slug; cross-tenant identity is never inferred from an
// entity id alone.
type TenantId string

// Validate reports whether t is a canonical tenant slug.
func (t TenantId) Validate() error {
	if t == "" {
		return ErrTenantRequired
	}
	if len(t) < 2 || len(t) > 64 {
		return fmt.Errorf("%w: %q must be 2..64 bytes", ErrInvalidTenant, string(t))
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		alnum := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		switch {
		case alnum:
		case c == '-' && i > 0 && i < len(t)-1 && t[i-1] != '-':
		default:
			return fmt.Errorf("%w: %q", ErrInvalidTenant, string(t))
		}
	}
	return nil
}

// String returns the tenant slug.
func (t TenantId) String() string { return string(t) }

// validateOpaqueID accepts a canonical lowercase UUID or an uppercase Crockford
// base32 ULID. Anything else, including a non-canonical spelling of an otherwise
// valid UUID, is rejected so that one entity never has two canonical encodings.
func validateOpaqueID(id string) error {
	if id == "" {
		return ErrEmptyID
	}
	switch len(id) {
	case 36:
		u, err := uuid.Parse(id)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrInvalidID, id)
		}
		if u.String() != id {
			return fmt.Errorf("%w: %q is not the canonical lowercase spelling", ErrInvalidID, id)
		}
		return nil
	case 26:
		if id[0] > '7' {
			return fmt.Errorf("%w: %q overflows the ULID timestamp range", ErrInvalidID, id)
		}
		for i := 0; i < len(id); i++ {
			if strings.IndexByte(crockfordAlphabet, id[i]) < 0 {
				return fmt.Errorf("%w: %q has a non-Crockford character", ErrInvalidID, id)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: %q is neither a 36-byte UUID nor a 26-byte ULID", ErrInvalidID, id)
	}
}

// crockfordAlphabet is Crockford base32 minus I, L, O and U.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// revisionOpaqueEncoding is strict base64url without padding. Strict mode
// rejects non-zero trailing bits, so one byte string has exactly one spelling.
var revisionOpaqueEncoding = base64.RawURLEncoding.Strict()

// EntityId is a tenant-agnostic entity identity: a registry-owned kind plus an
// opaque UUID or ULID. It is never a bare string; the kind is part of identity.
type EntityId struct {
	Kind Kind
	Id   string
}

// Validate reports whether the identifier is well formed.
func (e EntityId) Validate() error {
	if err := e.Kind.Validate(); err != nil {
		return err
	}
	return validateOpaqueID(e.Id)
}

// Canonical returns the canonical text encoding as bytes, or nil when invalid.
func (e EntityId) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return []byte(entityIDPrefix + string(e.Kind) + ":" + e.Id)
}

// String returns the canonical text form, or an empty string when invalid.
func (e EntityId) String() string { return string(e.Canonical()) }

// MarshalText implements encoding.TextMarshaler.
func (e EntityId) MarshalText() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return e.Canonical(), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. A failed decode leaves the
// receiver zeroed rather than partially populated.
func (e *EntityId) UnmarshalText(text []byte) error {
	*e = EntityId{}
	body, ok := strings.CutPrefix(string(text), entityIDPrefix)
	if !ok {
		return fmt.Errorf("%w: %q is not an entity id", ErrCanonicalFormat, text)
	}
	kind, id, ok := strings.Cut(body, ":")
	if !ok || strings.Contains(id, ":") {
		return fmt.Errorf("%w: entity id needs exactly kind and id", ErrCanonicalFormat)
	}
	candidate := EntityId{Kind: Kind(kind), Id: id}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*e = candidate
	return nil
}

// EntityRef is a tenant-scoped reference to an entity. Every cross-aggregate
// reference uses this type; a bare EntityId never crosses a tenant boundary.
type EntityRef struct {
	Tenant TenantId
	Kind   Kind
	Id     string
}

// Validate reports whether the reference is well formed and tenant-scoped.
func (r EntityRef) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return err
	}
	return r.EntityId().Validate()
}

// EntityId returns the tenant-agnostic identity carried by the reference.
func (r EntityRef) EntityId() EntityId { return EntityId{Kind: r.Kind, Id: r.Id} }

// Canonical returns the canonical text encoding as bytes, or nil when invalid.
func (r EntityRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return []byte(entityRefPrefix + string(r.Tenant) + ":" + string(r.Kind) + ":" + r.Id)
}

// String returns the canonical text form, or an empty string when invalid.
func (r EntityRef) String() string { return string(r.Canonical()) }

// MarshalText implements encoding.TextMarshaler.
func (r EntityRef) MarshalText() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r.Canonical(), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (r *EntityRef) UnmarshalText(text []byte) error {
	*r = EntityRef{}
	body, ok := strings.CutPrefix(string(text), entityRefPrefix)
	if !ok {
		return fmt.Errorf("%w: %q is not an entity ref", ErrCanonicalFormat, text)
	}
	parts := strings.Split(body, ":")
	if len(parts) != 3 {
		return fmt.Errorf("%w: entity ref needs exactly tenant, kind and id", ErrCanonicalFormat)
	}
	candidate := EntityRef{Tenant: TenantId(parts[0]), Kind: Kind(parts[1]), Id: parts[2]}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*r = candidate
	return nil
}

// ResourceKey names a tenant-scoped resource by its natural key rather than by
// an opaque id. Segments are ordered and are escaped on encoding, so a segment
// can never inject an extra path element or a different tenant.
type ResourceKey struct {
	Tenant       TenantId
	ResourceType Kind
	Segments     []string
}

// NewResourceKey builds a resource key, normalizing every segment to Unicode
// NFC. It rejects a tenantless key, an empty segment list and an empty,
// invalid-UTF-8 segment.
func NewResourceKey(tenant TenantId, resourceType Kind, segments ...string) (ResourceKey, error) {
	normalized := make([]string, 0, len(segments))
	for _, s := range segments {
		if !utf8.ValidString(s) {
			return ResourceKey{}, fmt.Errorf("%w: resource key segment", ErrInvalidUTF8)
		}
		normalized = append(normalized, norm.NFC.String(s))
	}
	key := ResourceKey{Tenant: tenant, ResourceType: resourceType, Segments: normalized}
	if err := key.Validate(); err != nil {
		return ResourceKey{}, err
	}
	return key, nil
}

// Validate reports whether the key is tenant-scoped and canonically encodable.
func (k ResourceKey) Validate() error {
	if err := k.Tenant.Validate(); err != nil {
		return err
	}
	if err := k.ResourceType.Validate(); err != nil {
		return err
	}
	if len(k.Segments) == 0 {
		return ErrEmptyResourceKey
	}
	for i, s := range k.Segments {
		if s == "" {
			return fmt.Errorf("%w: segment %d is empty", ErrEmptyResourceKey, i)
		}
		if !utf8.ValidString(s) {
			return fmt.Errorf("%w: segment %d", ErrInvalidUTF8, i)
		}
		if !norm.NFC.IsNormalString(s) {
			return fmt.Errorf("%w: segment %d", ErrNotNFC, i)
		}
	}
	return nil
}

// Equal reports whether two keys denote the same resource.
func (k ResourceKey) Equal(other ResourceKey) bool {
	return k.Tenant == other.Tenant &&
		k.ResourceType == other.ResourceType &&
		slices.Equal(k.Segments, other.Segments)
}

// Canonical returns the canonical text encoding as bytes, or nil when invalid.
func (k ResourceKey) Canonical() []byte {
	if k.Validate() != nil {
		return nil
	}
	var b strings.Builder
	b.WriteString(resourceKeyPrefix)
	b.WriteString(string(k.Tenant))
	b.WriteByte(':')
	b.WriteString(string(k.ResourceType))
	b.WriteByte(':')
	for i, s := range k.Segments {
		if i > 0 {
			b.WriteByte('/')
		}
		b.WriteString(escapeSegment(s))
	}
	return []byte(b.String())
}

// String returns the canonical text form, or an empty string when invalid.
func (k ResourceKey) String() string { return string(k.Canonical()) }

// MarshalText implements encoding.TextMarshaler.
func (k ResourceKey) MarshalText() ([]byte, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return k.Canonical(), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (k *ResourceKey) UnmarshalText(text []byte) error {
	*k = ResourceKey{}
	body, ok := strings.CutPrefix(string(text), resourceKeyPrefix)
	if !ok {
		return fmt.Errorf("%w: %q is not a resource key", ErrCanonicalFormat, text)
	}
	parts := strings.Split(body, ":")
	if len(parts) != 3 {
		return fmt.Errorf("%w: resource key needs exactly tenant, type and segments", ErrCanonicalFormat)
	}
	rawSegments := strings.Split(parts[2], "/")
	segments := make([]string, 0, len(rawSegments))
	for _, raw := range rawSegments {
		s, err := unescapeSegment(raw)
		if err != nil {
			return err
		}
		segments = append(segments, s)
	}
	candidate := ResourceKey{Tenant: TenantId(parts[0]), ResourceType: Kind(parts[1]), Segments: segments}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*k = candidate
	return nil
}

// revisionSelector distinguishes the two ways a revision can be named. Exactly
// one is legal for a specified token.
type revisionSelector uint8

const (
	revisionSelectorNone revisionSelector = iota
	revisionSelectorSequence
	revisionSelectorOpaque
)

// RevisionToken names a specific revision of a stream. It is opaque to callers,
// monotonic only within its own stream, and it distinguishes "unspecified" (the
// caller expresses no revision expectation) from an explicit selector.
//
// The zero value is the unspecified token.
type RevisionToken struct {
	specified bool
	selector  revisionSelector
	stream    string
	sequence  uint64
	opaque    string // base64url, no padding
}

// UnspecifiedRevision returns the token that expresses no revision expectation.
// It is distinct from every explicit revision, including sequence zero.
func UnspecifiedRevision() RevisionToken { return RevisionToken{} }

// NewSequenceRevision returns an explicit token naming a monotonic revision
// within stream.
func NewSequenceRevision(stream string, sequence uint64) (RevisionToken, error) {
	if err := validateStream(stream); err != nil {
		return RevisionToken{}, err
	}
	return RevisionToken{
		specified: true,
		selector:  revisionSelectorSequence,
		stream:    norm.NFC.String(stream),
		sequence:  sequence,
	}, nil
}

// NewOpaqueRevision returns an explicit token naming a compare-and-set value
// that the source authority issued. Opaque tokens are comparable for equality
// but never for order.
func NewOpaqueRevision(stream string, cas []byte) (RevisionToken, error) {
	if err := validateStream(stream); err != nil {
		return RevisionToken{}, err
	}
	if len(cas) == 0 {
		return RevisionToken{}, fmt.Errorf("%w: opaque revision has no bytes", ErrAmbiguousRevision)
	}
	return RevisionToken{
		specified: true,
		selector:  revisionSelectorOpaque,
		stream:    norm.NFC.String(stream),
		opaque:    revisionOpaqueEncoding.EncodeToString(cas),
	}, nil
}

func validateStream(stream string) error {
	if stream == "" {
		return ErrRevisionStreamRequired
	}
	if !utf8.ValidString(stream) {
		return fmt.Errorf("%w: revision stream", ErrInvalidUTF8)
	}
	if norm.NFC.String(stream) == revisionUnspecifiedTag {
		return fmt.Errorf("%w: %q is reserved", ErrRevisionStreamRequired, revisionUnspecifiedTag)
	}
	return nil
}

// IsSpecified reports whether the token names a revision.
func (r RevisionToken) IsSpecified() bool { return r.specified }

// Stream returns the stream the revision is monotonic within, or "" when the
// token is unspecified.
func (r RevisionToken) Stream() string { return r.stream }

// Sequence returns the monotonic revision and whether the token carries one.
func (r RevisionToken) Sequence() (uint64, bool) {
	return r.sequence, r.specified && r.selector == revisionSelectorSequence
}

// Opaque returns the compare-and-set bytes and whether the token carries them.
func (r RevisionToken) Opaque() ([]byte, bool) {
	if !r.specified || r.selector != revisionSelectorOpaque {
		return nil, false
	}
	b, err := revisionOpaqueEncoding.DecodeString(r.opaque)
	if err != nil {
		return nil, false
	}
	return b, true
}

// Equal reports whether two tokens name the same revision.
func (r RevisionToken) Equal(other RevisionToken) bool { return r == other }

// CompareInStream orders two sequence tokens from the same stream. Revisions
// are monotonic only within a stream, so comparing across streams, comparing an
// unspecified token, or comparing opaque tokens is an error rather than a
// silently wrong answer.
func (r RevisionToken) CompareInStream(other RevisionToken) (int, error) {
	if !r.specified || !other.specified {
		return 0, ErrRevisionUnspecified
	}
	if r.stream != other.stream {
		return 0, fmt.Errorf("%w: %q vs %q", ErrRevisionStreamMismatch, r.stream, other.stream)
	}
	if r.selector != revisionSelectorSequence || other.selector != revisionSelectorSequence {
		return 0, ErrRevisionNotOrdered
	}
	switch {
	case r.sequence < other.sequence:
		return -1, nil
	case r.sequence > other.sequence:
		return 1, nil
	default:
		return 0, nil
	}
}

// Validate reports whether the token is internally consistent.
func (r RevisionToken) Validate() error {
	if !r.specified {
		if r.selector != revisionSelectorNone || r.stream != "" || r.sequence != 0 || r.opaque != "" {
			return fmt.Errorf("%w: unspecified token carries a selector", ErrAmbiguousRevision)
		}
		return nil
	}
	if err := validateStream(r.stream); err != nil {
		return err
	}
	switch r.selector {
	case revisionSelectorSequence:
		if r.opaque != "" {
			return fmt.Errorf("%w: sequence token also carries opaque bytes", ErrAmbiguousRevision)
		}
		return nil
	case revisionSelectorOpaque:
		if r.sequence != 0 {
			return fmt.Errorf("%w: opaque token also carries a sequence", ErrAmbiguousRevision)
		}
		if r.opaque == "" {
			return fmt.Errorf("%w: opaque token has no bytes", ErrAmbiguousRevision)
		}
		return nil
	default:
		return fmt.Errorf("%w: specified token has no selector", ErrAmbiguousRevision)
	}
}

// Canonical returns the canonical text encoding as bytes, or nil when invalid.
func (r RevisionToken) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	if !r.specified {
		return []byte(revisionTokenPrefix + revisionUnspecifiedTag)
	}
	body := revisionTokenPrefix + escapeSegment(r.stream) + ":"
	if r.selector == revisionSelectorSequence {
		return []byte(body + "s" + strconv.FormatUint(r.sequence, 10))
	}
	return []byte(body + "o" + r.opaque)
}

// String returns the canonical text form.
func (r RevisionToken) String() string { return string(r.Canonical()) }

// MarshalText implements encoding.TextMarshaler.
func (r RevisionToken) MarshalText() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r.Canonical(), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (r *RevisionToken) UnmarshalText(text []byte) error {
	*r = RevisionToken{}
	body, ok := strings.CutPrefix(string(text), revisionTokenPrefix)
	if !ok {
		return fmt.Errorf("%w: %q is not a revision token", ErrCanonicalFormat, text)
	}
	if body == revisionUnspecifiedTag {
		return nil
	}
	rawStream, selector, ok := strings.Cut(body, ":")
	if !ok || strings.Contains(selector, ":") {
		return fmt.Errorf("%w: revision token needs exactly stream and selector", ErrCanonicalFormat)
	}
	stream, err := unescapeSegment(rawStream)
	if err != nil {
		return err
	}
	if len(selector) < 2 {
		return fmt.Errorf("%w: revision selector %q is empty", ErrAmbiguousRevision, selector)
	}
	switch selector[0] {
	case 's':
		digits := selector[1:]
		for i := 0; i < len(digits); i++ {
			if digits[i] < '0' || digits[i] > '9' {
				return fmt.Errorf("%w: revision sequence %q is not a canonical number", ErrCanonicalFormat, digits)
			}
		}
		if len(digits) > 1 && digits[0] == '0' {
			return fmt.Errorf("%w: revision sequence %q has a leading zero", ErrCanonicalFormat, digits)
		}
		seq, err := strconv.ParseUint(digits, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: revision sequence %q: %v", ErrCanonicalFormat, digits, err)
		}
		token, err := NewSequenceRevision(stream, seq)
		if err != nil {
			return err
		}
		*r = token
		return nil
	case 'o':
		encoded := selector[1:]
		// Go's base64 decoder silently skips CR and LF even in strict mode, so
		// the alphabet is checked first: one byte string, one spelling.
		for i := 0; i < len(encoded); i++ {
			c := encoded[i]
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return fmt.Errorf("%w: opaque revision has a non-base64url byte %#x", ErrCanonicalFormat, c)
			}
		}
		cas, err := revisionOpaqueEncoding.DecodeString(encoded)
		if err != nil {
			return fmt.Errorf("%w: opaque revision is not canonical base64url: %v", ErrCanonicalFormat, err)
		}
		token, err := NewOpaqueRevision(stream, cas)
		if err != nil {
			return err
		}
		*r = token
		return nil
	default:
		return fmt.Errorf("%w: unknown revision selector %q", ErrAmbiguousRevision, selector[:1])
	}
}

// unreserved reports whether c may appear literally in an escaped segment. The
// set is RFC 3986 unreserved, which excludes both canonical separators.
func unreserved(c byte) bool {
	return c >= 'A' && c <= 'Z' ||
		c >= 'a' && c <= 'z' ||
		c >= '0' && c <= '9' ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

const upperHex = "0123456789ABCDEF"

// escapeSegment percent-escapes every byte outside the unreserved set using
// uppercase hex, which is the only spelling the decoder accepts.
func escapeSegment(s string) string {
	needs := false
	for i := 0; i < len(s); i++ {
		if !unreserved(s[i]) {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if unreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[c>>4])
		b.WriteByte(upperHex[c&0x0f])
	}
	return b.String()
}

// unescapeSegment reverses escapeSegment and rejects every non-canonical
// spelling: lowercase hex, a truncated escape and a needless escape of an
// unreserved byte all fail, so one segment has exactly one encoding.
func unescapeSegment(s string) (string, error) {
	if s == "" {
		return "", ErrEmptyResourceKey
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '%' {
			if !unreserved(c) {
				return "", fmt.Errorf("%w: byte %#x must be percent-escaped", ErrCanonicalFormat, c)
			}
			b.WriteByte(c)
			i++
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("%w: truncated percent escape", ErrCanonicalFormat)
		}
		hi, err := upperHexValue(s[i+1])
		if err != nil {
			return "", err
		}
		lo, err := upperHexValue(s[i+2])
		if err != nil {
			return "", err
		}
		decoded := hi<<4 | lo
		if unreserved(decoded) {
			return "", fmt.Errorf("%w: needless escape of %q", ErrCanonicalFormat, string(rune(decoded)))
		}
		b.WriteByte(decoded)
		i += 3
	}
	out := b.String()
	if !utf8.ValidString(out) {
		return "", ErrInvalidUTF8
	}
	if !norm.NFC.IsNormalString(out) {
		return "", ErrNotNFC
	}
	return out, nil
}

func upperHexValue(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	default:
		return 0, fmt.Errorf("%w: %q is not canonical uppercase hex", ErrCanonicalFormat, string(rune(c)))
	}
}
