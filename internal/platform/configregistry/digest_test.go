package configregistry

import (
	"testing"
	"time"
)

func TestComputeBodyDigestIsDeterministicAndContentSensitive(t *testing.T) {
	t.Parallel()
	a := computeBodyDigest([]byte("hello"))
	b := computeBodyDigest([]byte("hello"))
	if a != b {
		t.Fatalf("computeBodyDigest is not deterministic: %q != %q", a, b)
	}
	if c := computeBodyDigest([]byte("world")); c == a {
		t.Fatal("different bodies produced the same digest")
	}
	if len(a) != 64 {
		t.Fatalf("digest length = %d, want 64 (hex sha256)", len(a))
	}
}

func TestComputeRecordDigestIsSensitiveToEveryField(t *testing.T) {
	t.Parallel()
	base := ConfigurationObject{
		Kind:                KindSchema,
		ID:                  "s1",
		Revision:            1,
		Body:                []byte("body"),
		CanonicalBodyDigest: computeBodyDigest([]byte("body")),
		SchemaRef:           "schema/v1",
		Scope:               Scope{TenantID: "t1", CellID: "c1"},
		PublisherPrincipal:  "pub-1",
		PublishedAt:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	baseDigest := computeRecordDigest(base)

	variants := []func(ConfigurationObject) ConfigurationObject{
		func(o ConfigurationObject) ConfigurationObject { o.Kind = KindRule; return o },
		func(o ConfigurationObject) ConfigurationObject { o.ID = "s2"; return o },
		func(o ConfigurationObject) ConfigurationObject { o.Revision = 2; return o },
		func(o ConfigurationObject) ConfigurationObject { o.SchemaRef = "schema/v2"; return o },
		func(o ConfigurationObject) ConfigurationObject { o.Scope.CellID = "c2"; return o },
		func(o ConfigurationObject) ConfigurationObject { o.PublisherPrincipal = "pub-2"; return o },
		func(o ConfigurationObject) ConfigurationObject {
			o.PublishedAt = o.PublishedAt.Add(time.Hour)
			return o
		},
	}
	for i, mutate := range variants {
		if got := computeRecordDigest(mutate(base)); got == baseDigest {
			t.Errorf("variant %d: digest unchanged after mutating a distinguishing field", i)
		}
	}

	// Status is not a field on ConfigurationObject at all in this package
	// (activation is a wholly separate ActivationRecord), so there is no
	// analogous "excluded field" to check here — every field feeds the
	// digest.
	if got := computeRecordDigest(base); got != baseDigest {
		t.Fatal("computeRecordDigest is not deterministic for an unchanged value")
	}
}

func TestCanonicalDigestProfilesDoNotCollide(t *testing.T) {
	t.Parallel()
	// The same raw bytes hashed under the two distinct profiles this package
	// declares must never collide, or a body digest could be mistaken for a
	// record digest.
	raw := []byte("payload")
	bodyProfileDigest := canonicalDigest(bodyDigestProfile, raw)
	recordProfileDigest := canonicalDigest(recordDigestProfile, raw)
	if bodyProfileDigest == recordProfileDigest {
		t.Fatal("distinct canonicalization profiles produced the same digest for identical bytes")
	}
}
