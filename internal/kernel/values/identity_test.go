package values

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

const (
	tenantAcme  = TenantId("acme-corp")
	ulidWorker  = "01HZY6R8N2QK4V8B0C3M5T7X9A"
	ulidWorker2 = "01J9Q0M3ZQ8F7B4T2N6VW5XH1K"
	uuidWorker  = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
)

// TestTodo_MODEL_001 is the primary test for MODEL-001: canonical entity and
// reference identifiers.
func TestTodo_MODEL_001(t *testing.T) {
	t.Parallel()

	t.Run("EntityIdCanonicalRoundTrip", func(t *testing.T) {
		id := EntityId{Kind: "worker", Id: ulidWorker}
		if err := id.Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
		want := "eid:v1:worker:" + ulidWorker
		if got := string(id.Canonical()); got != want {
			t.Fatalf("Canonical() = %q, want %q", got, want)
		}
		text, err := id.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText() error = %v", err)
		}
		var back EntityId
		if err := back.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q) error = %v", text, err)
		}
		if back != id {
			t.Fatalf("round trip = %+v, want %+v", back, id)
		}
	})

	t.Run("EntityIdRejectsEmptyAndWrongKind", func(t *testing.T) {
		cases := []struct {
			name string
			id   EntityId
			want error
		}{
			{"empty kind", EntityId{Kind: "", Id: ulidWorker}, ErrEmptyKind},
			{"empty id", EntityId{Kind: "worker", Id: ""}, ErrEmptyID},
			{"uppercase kind", EntityId{Kind: "Worker", Id: ulidWorker}, ErrInvalidKind},
			{"separator in kind", EntityId{Kind: "work:er", Id: ulidWorker}, ErrInvalidKind},
			{"non canonical uuid", EntityId{Kind: "worker", Id: strings.ToUpper(uuidWorker)}, ErrInvalidID},
			{"ulid with excluded letter", EntityId{Kind: "worker", Id: "01HZY6R8N2QK4V8B0C3M5T7X9I"}, ErrInvalidID},
			{"free form id", EntityId{Kind: "worker", Id: "worker-42"}, ErrInvalidID},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.id.Validate()
				if !errors.Is(err, tc.want) {
					t.Fatalf("Validate() = %v, want %v", err, tc.want)
				}
				if got := tc.id.Canonical(); got != nil {
					t.Fatalf("Canonical() = %q, want nil for invalid value", got)
				}
			})
		}
	})

	t.Run("EntityRefCarriesTenant", func(t *testing.T) {
		ref := EntityRef{Tenant: tenantAcme, Kind: "worker", Id: uuidWorker}
		if err := ref.Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
		want := "eref:v1:acme-corp:worker:" + uuidWorker
		if got := string(ref.Canonical()); got != want {
			t.Fatalf("Canonical() = %q, want %q", got, want)
		}
		if got := ref.EntityId(); got != (EntityId{Kind: "worker", Id: uuidWorker}) {
			t.Fatalf("EntityId() = %+v", got)
		}
		var back EntityRef
		if err := back.UnmarshalText(ref.Canonical()); err != nil {
			t.Fatalf("UnmarshalText error = %v", err)
		}
		if back != ref {
			t.Fatalf("round trip = %+v, want %+v", back, ref)
		}
	})

	t.Run("EntityRefRejectsTenantlessAndCrossKindDrift", func(t *testing.T) {
		if err := (EntityRef{Kind: "worker", Id: uuidWorker}).Validate(); !errors.Is(err, ErrTenantRequired) {
			t.Fatalf("tenantless EntityRef error = %v, want ErrTenantRequired", err)
		}
		if err := (EntityRef{Tenant: "Acme Corp", Kind: "worker", Id: uuidWorker}).Validate(); !errors.Is(err, ErrInvalidTenant) {
			t.Fatalf("bad tenant error = %v, want ErrInvalidTenant", err)
		}
		// A reference to one kind never decodes as another kind.
		a := EntityRef{Tenant: tenantAcme, Kind: "worker", Id: uuidWorker}
		b := EntityRef{Tenant: tenantAcme, Kind: "position", Id: uuidWorker}
		if bytes.Equal(a.Canonical(), b.Canonical()) {
			t.Fatal("different kinds produced identical canonical bytes")
		}
	})

	t.Run("ResourceKeyIsTenantScoped", func(t *testing.T) {
		key, err := NewResourceKey(tenantAcme, "pay_group", "north-america", "2026 Q1")
		if err != nil {
			t.Fatalf("NewResourceKey error = %v", err)
		}
		want := "rkey:v1:acme-corp:pay_group:north-america/2026%20Q1"
		if got := string(key.Canonical()); got != want {
			t.Fatalf("Canonical() = %q, want %q", got, want)
		}
		var back ResourceKey
		if err := back.UnmarshalText(key.Canonical()); err != nil {
			t.Fatalf("UnmarshalText error = %v", err)
		}
		if !back.Equal(key) {
			t.Fatalf("round trip = %+v, want %+v", back, key)
		}
	})

	t.Run("ResourceKeyRejectsTenantlessAndEmptySegments", func(t *testing.T) {
		if _, err := NewResourceKey("", "pay_group", "x"); !errors.Is(err, ErrTenantRequired) {
			t.Fatalf("tenantless key error = %v, want ErrTenantRequired", err)
		}
		if _, err := NewResourceKey(tenantAcme, "pay_group"); !errors.Is(err, ErrEmptyResourceKey) {
			t.Fatalf("segmentless key error = %v, want ErrEmptyResourceKey", err)
		}
		if _, err := NewResourceKey(tenantAcme, "pay_group", ""); !errors.Is(err, ErrEmptyResourceKey) {
			t.Fatalf("empty segment error = %v, want ErrEmptyResourceKey", err)
		}
		if _, err := NewResourceKey(tenantAcme, "pay_group", "\xff\xfe"); !errors.Is(err, ErrInvalidUTF8) {
			t.Fatalf("invalid utf8 error = %v, want ErrInvalidUTF8", err)
		}
	})

	t.Run("RevisionTokenDistinguishesUnspecifiedFromExplicit", func(t *testing.T) {
		unspec := UnspecifiedRevision()
		if unspec.IsSpecified() {
			t.Fatal("UnspecifiedRevision().IsSpecified() = true")
		}
		if got, want := string(unspec.Canonical()), "rev:v1:unspecified"; got != want {
			t.Fatalf("Canonical() = %q, want %q", got, want)
		}

		explicit, err := NewSequenceRevision("worker/"+uuidWorker, 7)
		if err != nil {
			t.Fatalf("NewSequenceRevision error = %v", err)
		}
		if !explicit.IsSpecified() {
			t.Fatal("explicit revision reported unspecified")
		}
		if got, want := string(explicit.Canonical()), "rev:v1:worker%2F"+uuidWorker+":s7"; got != want {
			t.Fatalf("Canonical() = %q, want %q", got, want)
		}
		if bytes.Equal(explicit.Canonical(), unspec.Canonical()) {
			t.Fatal("unspecified and explicit revisions share canonical bytes")
		}

		var back RevisionToken
		if err := back.UnmarshalText(explicit.Canonical()); err != nil {
			t.Fatalf("UnmarshalText error = %v", err)
		}
		if !back.Equal(explicit) {
			t.Fatalf("round trip = %+v, want %+v", back, explicit)
		}

		var backUnspec RevisionToken
		if err := backUnspec.UnmarshalText(unspec.Canonical()); err != nil {
			t.Fatalf("UnmarshalText(unspecified) error = %v", err)
		}
		if backUnspec.IsSpecified() {
			t.Fatal("decoded unspecified revision reported specified")
		}
	})

	t.Run("RevisionTokenIsMonotonicWithinStream", func(t *testing.T) {
		stream := "worker/" + uuidWorker
		lo, err := NewSequenceRevision(stream, 7)
		if err != nil {
			t.Fatalf("NewSequenceRevision error = %v", err)
		}
		hi, err := NewSequenceRevision(stream, 8)
		if err != nil {
			t.Fatalf("NewSequenceRevision error = %v", err)
		}
		cmp, err := lo.CompareInStream(hi)
		if err != nil {
			t.Fatalf("CompareInStream error = %v", err)
		}
		if cmp != -1 {
			t.Fatalf("CompareInStream = %d, want -1", cmp)
		}
		other, err := NewSequenceRevision("position/"+uuidWorker, 8)
		if err != nil {
			t.Fatalf("NewSequenceRevision error = %v", err)
		}
		if _, err := lo.CompareInStream(other); !errors.Is(err, ErrRevisionStreamMismatch) {
			t.Fatalf("cross-stream compare error = %v, want ErrRevisionStreamMismatch", err)
		}
	})

	t.Run("RevisionTokenRejectsAmbiguousSelectors", func(t *testing.T) {
		if _, err := NewSequenceRevision("", 1); !errors.Is(err, ErrRevisionStreamRequired) {
			t.Fatalf("streamless revision error = %v, want ErrRevisionStreamRequired", err)
		}
		if _, err := NewOpaqueRevision("worker/"+uuidWorker, nil); !errors.Is(err, ErrAmbiguousRevision) {
			t.Fatalf("empty opaque revision error = %v, want ErrAmbiguousRevision", err)
		}
		var tok RevisionToken
		for _, bad := range []string{
			"rev:v1:worker:s7:s8",
			"rev:v1::s7",
			"rev:v1:worker:s",
			"rev:v1:worker:s-1",
			"rev:v1:worker:x7",
			"rev:v1:unspecified:s7",
			"rev:v2:worker:s7",
		} {
			if err := tok.UnmarshalText([]byte(bad)); err == nil {
				t.Fatalf("UnmarshalText(%q) = nil error, want failure", bad)
			}
		}
	})
}

// TestTodo_MODEL_001_Property asserts canonical-encoding properties: round trip
// stability, injectivity and idempotence across a table of values.
func TestTodo_MODEL_001_Property(t *testing.T) {
	t.Parallel()

	values := make([]interface {
		Canonical() []byte
	}, 0, 16)

	ids := []EntityId{
		{Kind: "worker", Id: ulidWorker},
		{Kind: "worker", Id: ulidWorker2},
		{Kind: "position", Id: ulidWorker},
		{Kind: "pay_group", Id: uuidWorker},
	}
	for _, id := range ids {
		values = append(values, id)
	}
	refs := []EntityRef{
		{Tenant: tenantAcme, Kind: "worker", Id: ulidWorker},
		{Tenant: "globex", Kind: "worker", Id: ulidWorker},
		{Tenant: tenantAcme, Kind: "worker", Id: ulidWorker2},
	}
	for _, ref := range refs {
		values = append(values, ref)
	}
	for _, segs := range [][]string{{"a"}, {"a", "b"}, {"a/b"}, {"a%2Fb"}} {
		key, err := NewResourceKey(tenantAcme, "pay_group", segs...)
		if err != nil {
			t.Fatalf("NewResourceKey(%q) error = %v", segs, err)
		}
		values = append(values, key)
	}
	rev1, err := NewSequenceRevision("s", 1)
	if err != nil {
		t.Fatalf("NewSequenceRevision error = %v", err)
	}
	rev2, err := NewOpaqueRevision("s", []byte{0x01, 0x02})
	if err != nil {
		t.Fatalf("NewOpaqueRevision error = %v", err)
	}
	values = append(values, rev1, rev2, UnspecifiedRevision())

	seen := map[string]int{}
	for i, v := range values {
		canon := v.Canonical()
		if len(canon) == 0 {
			t.Fatalf("value %d produced empty canonical bytes", i)
		}
		// Idempotence: recomputing yields identical bytes.
		if !bytes.Equal(canon, v.Canonical()) {
			t.Fatalf("value %d canonical bytes are not stable", i)
		}
		// Injectivity: distinct values never share canonical bytes.
		if prev, dup := seen[string(canon)]; dup {
			t.Fatalf("values %d and %d share canonical bytes %q", prev, i, canon)
		}
		seen[string(canon)] = i
	}

	// Unicode: NFD input normalizes to the same canonical bytes as NFC input.
	nfc, err := NewResourceKey(tenantAcme, "job_profile", "Müller")
	if err != nil {
		t.Fatalf("NewResourceKey(NFC) error = %v", err)
	}
	nfd, err := NewResourceKey(tenantAcme, "job_profile", "Müller")
	if err != nil {
		t.Fatalf("NewResourceKey(NFD) error = %v", err)
	}
	if !bytes.Equal(nfc.Canonical(), nfd.Canonical()) {
		t.Fatalf("NFC %q and NFD %q disagree", nfc.Canonical(), nfd.Canonical())
	}

	// Round trip: canonical text decodes back to an equal value.
	for _, id := range ids {
		var back EntityId
		if err := back.UnmarshalText(id.Canonical()); err != nil || back != id {
			t.Fatalf("EntityId round trip %+v -> %+v err=%v", id, back, err)
		}
	}
	for _, ref := range refs {
		var back EntityRef
		if err := back.UnmarshalText(ref.Canonical()); err != nil || back != ref {
			t.Fatalf("EntityRef round trip %+v -> %+v err=%v", ref, back, err)
		}
	}
}

type goldenIdentity struct {
	EncodingVersion int `json:"encoding_version"`
	EntityIds       []struct {
		Kind      string `json:"kind"`
		Id        string `json:"id"`
		Canonical string `json:"canonical"`
	} `json:"entity_ids"`
	EntityRefs []struct {
		Tenant    string `json:"tenant"`
		Kind      string `json:"kind"`
		Id        string `json:"id"`
		Canonical string `json:"canonical"`
	} `json:"entity_refs"`
	ResourceKeys []struct {
		Tenant       string   `json:"tenant"`
		ResourceType string   `json:"resource_type"`
		Segments     []string `json:"segments"`
		Canonical    string   `json:"canonical"`
	} `json:"resource_keys"`
	Revisions []struct {
		Stream    string `json:"stream"`
		Sequence  *uint64
		Opaque    string `json:"opaque_hex"`
		Canonical string `json:"canonical"`
	} `json:"revisions"`
	Invalid []string `json:"invalid_canonical_text"`
}

// TestTodo_MODEL_001_Golden pins canonical identifier vectors from testdata.
func TestTodo_MODEL_001_Golden(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/model_001_identifiers.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden goldenIdentity
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if golden.EncodingVersion != CanonicalEncodingVersion {
		t.Fatalf("golden encoding version = %d, want %d", golden.EncodingVersion, CanonicalEncodingVersion)
	}
	for _, tc := range golden.EntityIds {
		id := EntityId{Kind: Kind(tc.Kind), Id: tc.Id}
		if got := string(id.Canonical()); got != tc.Canonical {
			t.Errorf("EntityId(%q,%q) canonical = %q, want %q", tc.Kind, tc.Id, got, tc.Canonical)
		}
	}
	for _, tc := range golden.EntityRefs {
		ref := EntityRef{Tenant: TenantId(tc.Tenant), Kind: Kind(tc.Kind), Id: tc.Id}
		if got := string(ref.Canonical()); got != tc.Canonical {
			t.Errorf("EntityRef canonical = %q, want %q", got, tc.Canonical)
		}
	}
	for _, tc := range golden.ResourceKeys {
		key, err := NewResourceKey(TenantId(tc.Tenant), Kind(tc.ResourceType), tc.Segments...)
		if err != nil {
			t.Errorf("NewResourceKey(%+v) error = %v", tc, err)
			continue
		}
		if got := string(key.Canonical()); got != tc.Canonical {
			t.Errorf("ResourceKey canonical = %q, want %q", got, tc.Canonical)
		}
	}
	for _, bad := range golden.Invalid {
		var (
			id  EntityId
			ref EntityRef
			key ResourceKey
			tok RevisionToken
		)
		switch {
		case id.UnmarshalText([]byte(bad)) == nil,
			ref.UnmarshalText([]byte(bad)) == nil,
			key.UnmarshalText([]byte(bad)) == nil,
			tok.UnmarshalText([]byte(bad)) == nil:
			t.Errorf("invalid golden vector %q decoded successfully", bad)
		}
	}
}

// TestTodo_MODEL_001_Fault proves malformed canonical text always fails closed.
func TestTodo_MODEL_001_Fault(t *testing.T) {
	t.Parallel()

	bad := []string{
		"", ":", "eid", "eid:", "eid:v1", "eid:v1:", "eid:v1:worker",
		"eid:v1:worker:", "eid:v2:worker:" + ulidWorker, "eid:v1::" + ulidWorker,
		"eref:v1:acme-corp:worker", "rkey:v1:acme-corp:pay_group",
		"rkey:v1:acme-corp:pay_group:", "rkey:v1::pay_group:a",
		"rkey:v1:acme-corp:pay_group:a//b", "rkey:v1:acme-corp:pay_group:%zz",
		"rev:v1", "rev:v1:", "\x00", "eid:v1:worker:" + ulidWorker + ":extra",
	}
	for _, s := range bad {
		var (
			id  EntityId
			ref EntityRef
			key ResourceKey
			rev RevisionToken
		)
		if err := id.UnmarshalText([]byte(s)); err == nil {
			t.Errorf("EntityId.UnmarshalText(%q) = nil error", s)
		}
		if err := ref.UnmarshalText([]byte(s)); err == nil {
			t.Errorf("EntityRef.UnmarshalText(%q) = nil error", s)
		}
		if err := key.UnmarshalText([]byte(s)); err == nil {
			t.Errorf("ResourceKey.UnmarshalText(%q) = nil error", s)
		}
		if err := rev.UnmarshalText([]byte(s)); err == nil {
			t.Errorf("RevisionToken.UnmarshalText(%q) = nil error", s)
		}
	}
}

// TestTodo_MODEL_001_Security proves an identifier cannot be forged by
// separator injection and that a failed decode leaves the receiver unusable.
func TestTodo_MODEL_001_Security(t *testing.T) {
	t.Parallel()

	// Segment separators inside a resource-key segment are escaped, so a
	// caller cannot inject an extra path segment or a different tenant.
	injected, err := NewResourceKey(tenantAcme, "pay_group", "a/../../globex/pay_group/b")
	if err != nil {
		t.Fatalf("NewResourceKey error = %v", err)
	}
	if strings.Contains(string(injected.Canonical()), "globex/pay_group") {
		t.Fatalf("segment injection leaked into canonical bytes: %q", injected.Canonical())
	}
	var back ResourceKey
	if err := back.UnmarshalText(injected.Canonical()); err != nil {
		t.Fatalf("UnmarshalText error = %v", err)
	}
	if len(back.Segments) != 1 {
		t.Fatalf("decoded %d segments, want 1", len(back.Segments))
	}

	// A failed decode must not leave a partially populated, valid-looking value.
	good := EntityRef{Tenant: tenantAcme, Kind: "worker", Id: uuidWorker}
	target := good
	if err := target.UnmarshalText([]byte("eref:v1:acme-corp:worker")); err == nil {
		t.Fatal("truncated EntityRef decoded successfully")
	}
	if err := target.Validate(); err == nil {
		t.Fatalf("receiver still valid after failed decode: %+v", target)
	}

	// Kind confusion: the same opaque id under two kinds is never equal.
	if (EntityId{Kind: "worker", Id: uuidWorker}) == (EntityId{Kind: "position", Id: uuidWorker}) {
		t.Fatal("kind is not part of identity")
	}
}

// FuzzTodo_MODEL_001 checks that identifier decoding never panics and that any
// value that decodes re-encodes to the exact input bytes.
func FuzzTodo_MODEL_001(f *testing.F) {
	seeds := []string{
		"eid:v1:worker:" + ulidWorker,
		"eref:v1:acme-corp:worker:" + uuidWorker,
		"rkey:v1:acme-corp:pay_group:north-america/2026%20Q1",
		"rev:v1:unspecified",
		"rev:v1:worker:s7",
		"rev:v1:worker:oAQI",
		"", ":::", "eid:v1:worker:%",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		b := []byte(s)

		var id EntityId
		if id.UnmarshalText(b) == nil {
			if got := string(id.Canonical()); got != s {
				t.Fatalf("EntityId re-encode = %q, want %q", got, s)
			}
		}
		var ref EntityRef
		if ref.UnmarshalText(b) == nil {
			if got := string(ref.Canonical()); got != s {
				t.Fatalf("EntityRef re-encode = %q, want %q", got, s)
			}
		}
		var key ResourceKey
		if key.UnmarshalText(b) == nil {
			if got := string(key.Canonical()); got != s {
				t.Fatalf("ResourceKey re-encode = %q, want %q", got, s)
			}
		}
		var rev RevisionToken
		if rev.UnmarshalText(b) == nil {
			if got := string(rev.Canonical()); got != s {
				t.Fatalf("RevisionToken re-encode = %q, want %q", got, s)
			}
		}
	})
}
