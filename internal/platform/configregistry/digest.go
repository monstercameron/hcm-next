package configregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Canonicalization profile identities. A digest is always prefixed with the
// profile that produced it, so bytes hashed under one profile can never be
// mistaken for another profile's digest. This mirrors
// internal/workflow/version/digest.go exactly: a profile-prefixed sha256 over
// the encoding/json rendering, with struct fields in declaration order and no
// unordered collection anywhere in the digested value.
const (
	bodyDigestProfile   = "hcmnext.platform.configregistry.Body/v1"
	recordDigestProfile = "hcmnext.platform.configregistry.ConfigurationObject/v1"
)

// canonicalDigest hashes a value under a profile.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Every value this package digests is plain data assembled by this
		// package itself, so this is unreachable. If it ever happens,
		// produce bytes that cannot collide with a real digest rather than
		// silently returning an empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// computeBodyDigest is a configuration body's content address, independent
// of every other field a [ConfigurationObject] carries. Two revisions that
// happen to carry byte-identical bodies get the same body digest even if
// their metadata (kind, id, scope, ...) differs — the body digest names the
// content, not the publication.
func computeBodyDigest(body []byte) string {
	return canonicalDigest(bodyDigestProfile, body)
}

// recordIdentity is the subset of ConfigurationObject content this package
// digests: every exported field. The unexported digest itself is excluded by
// construction (recordIdentity has no field for it).
type recordIdentity struct {
	Kind                Kind
	ID                  string
	Revision            uint32
	Body                []byte
	CanonicalBodyDigest string
	SchemaRef           string
	Scope               Scope
	PublisherPrincipal  string
	PublishedAt         time.Time
}

// canonicalPublishedAt normalizes a PublishedAt value to the form this
// package always digests: UTC, truncated to microsecond precision.
//
// PostgreSQL's timestamptz column (migrations/00027_config_object.sql)
// stores an absolute instant at microsecond precision with no location —
// jackc/pgx's own timestamptz decoding hands that instant back as a
// time.Time whose Location need not be the same *Location value the
// in-process caller originally supplied (only the same instant), and Go's
// encoding/json rendering of a time.Time is sensitive to both the location's
// numeric offset and any sub-microsecond nanosecond component. Digesting the
// raw time.Time would therefore make [Rehydrate] report [CodeRecordMutated]
// for a record nothing ever mutated, purely because it round-tripped through
// storage: two time.Time values naming the exact same instant must always
// produce the same digest. Normalizing here, once, in the one place both
// [Publish]'s in-process mint and every [Store] adapter's [Rehydrate] call
// pass through, is what makes that true regardless of which Location or
// sub-microsecond precision a caller's clock happened to produce.
func canonicalPublishedAt(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}

// computeRecordDigest hashes a ConfigurationObject's full identity-bearing
// content.
func computeRecordDigest(o ConfigurationObject) string {
	id := recordIdentity{
		Kind:                o.Kind,
		ID:                  o.ID,
		Revision:            o.Revision,
		Body:                o.Body,
		CanonicalBodyDigest: o.CanonicalBodyDigest,
		SchemaRef:           o.SchemaRef,
		Scope:               o.Scope,
		PublisherPrincipal:  o.PublisherPrincipal,
		PublishedAt:         canonicalPublishedAt(o.PublishedAt),
	}
	return canonicalDigest(recordDigestProfile, id)
}
