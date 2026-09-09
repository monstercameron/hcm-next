package checkpoint_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
)

var (
	goldenTenant = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	goldenEpoch  = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	hashA        = strings.Repeat("a", 64)
	hashB        = strings.Repeat("b", 64)
	hashC        = strings.Repeat("c", 64)
)

// validManifest returns a manifest that passes Validate, so each test below
// can break exactly one thing about it.
func validManifest(t *testing.T) checkpoint.Manifest {
	t.Helper()
	streams := []checkpoint.StreamHead{
		{StreamKey: "worker:1", Sequence: 7, ChainHash: hashA, ChainAlgorithm: "sha256"},
		{StreamKey: "worker:2", Sequence: 3, ChainHash: hashB, ChainAlgorithm: "sha256"},
	}
	root, err := checkpoint.ComputeRootDigest(streams)
	if err != nil {
		t.Fatalf("root digest: %v", err)
	}
	return checkpoint.Manifest{
		SchemaVersion: checkpoint.ManifestSchemaVersion,
		Tenant:        goldenTenant,
		EpochID:       goldenEpoch,
		EpochNumber:   1,
		Schema:        checkpoint.SchemaRelease{Version: 28, Digest: hashC},
		Streams:       streams,
		RootDigest:    root, RootDigestAlgorithm: checkpoint.DigestAlgorithm,
		CoversFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CoversTo:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		Signature: &checkpoint.Signature{
			Algorithm: checkpoint.AlgorithmEd25519, KeyID: devKeyID, PublicKey: hashA,
		},
	}
}

func TestComputeRootDigest(t *testing.T) {
	a := checkpoint.StreamHead{StreamKey: "worker:1", Sequence: 7, ChainHash: hashA, ChainAlgorithm: "sha256"}
	b := checkpoint.StreamHead{StreamKey: "worker:2", Sequence: 3, ChainHash: hashB, ChainAlgorithm: "sha256"}

	forward, err := checkpoint.ComputeRootDigest([]checkpoint.StreamHead{a, b})
	if err != nil {
		t.Fatalf("root digest: %v", err)
	}
	backward, err := checkpoint.ComputeRootDigest([]checkpoint.StreamHead{b, a})
	if err != nil {
		t.Fatalf("root digest: %v", err)
	}
	if forward != backward {
		t.Fatal("the root digest depends on the order heads were read in")
	}
	if len(forward) != 64 {
		t.Fatalf("root digest %q is not a sha256 hex digest", forward)
	}

	t.Run("every field changes the digest", func(t *testing.T) {
		mutations := map[string]checkpoint.StreamHead{
			"stream key": {StreamKey: "worker:9", Sequence: 7, ChainHash: hashA, ChainAlgorithm: "sha256"},
			"sequence":   {StreamKey: "worker:1", Sequence: 8, ChainHash: hashA, ChainAlgorithm: "sha256"},
			"chain hash": {StreamKey: "worker:1", Sequence: 7, ChainHash: hashB, ChainAlgorithm: "sha256"},
			"algorithm":  {StreamKey: "worker:1", Sequence: 7, ChainHash: hashA, ChainAlgorithm: "sha512"},
		}
		for name, mutated := range mutations {
			got, err := checkpoint.ComputeRootDigest([]checkpoint.StreamHead{mutated, b})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got == forward {
				t.Errorf("changing the %s did not change the root digest", name)
			}
		}
	})

	t.Run("length framing prevents a field boundary from moving", func(t *testing.T) {
		// Without a length prefix on each field, "ab"+"c" and "a"+"bc" would
		// produce the same preimage.
		left, err := checkpoint.ComputeRootDigest([]checkpoint.StreamHead{
			{StreamKey: "ab", Sequence: 1, ChainHash: "c", ChainAlgorithm: "sha256"},
		})
		if err != nil {
			t.Fatalf("root digest: %v", err)
		}
		right, err := checkpoint.ComputeRootDigest([]checkpoint.StreamHead{
			{StreamKey: "a", Sequence: 1, ChainHash: "bc", ChainAlgorithm: "sha256"},
		})
		if err != nil {
			t.Fatalf("root digest: %v", err)
		}
		if left == right {
			t.Fatal("two different head sets produced the same preimage; the fields are not length-framed")
		}
	})

	t.Run("the stream count is bound in", func(t *testing.T) {
		one, err := checkpoint.ComputeRootDigest([]checkpoint.StreamHead{a})
		if err != nil {
			t.Fatalf("root digest: %v", err)
		}
		if one == forward {
			t.Fatal("dropping a stream did not change the root digest")
		}
	})

	t.Run("a duplicated stream is refused", func(t *testing.T) {
		if _, err := checkpoint.ComputeRootDigest([]checkpoint.StreamHead{a, a}); err == nil {
			t.Fatal("a head set containing the same stream twice was folded")
		}
	})
}

func TestManifestCanonicalDigest(t *testing.T) {
	base := validManifest(t)
	want, err := base.CanonicalDigest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	t.Run("the signature value is not covered", func(t *testing.T) {
		// A signature cannot cover its own bytes, or signing would change
		// what was signed.
		signed := base
		sig := *base.Signature
		sig.Value = strings.Repeat("f", 128)
		signed.Signature = &sig
		got, err := signed.CanonicalDigest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if got != want {
			t.Fatal("the signature value changed the digest it is supposed to cover")
		}
	})

	t.Run("the signing key identity is covered", func(t *testing.T) {
		reattributed := base
		sig := *base.Signature
		sig.KeyID = "hcmnext:checkpoint:someone-else"
		reattributed.Signature = &sig
		got, err := reattributed.CanonicalDigest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if got == want {
			t.Fatal("the signing key id is not inside the signed bytes; a signature could be re-attributed")
		}
	})

	t.Run("every bound field changes the digest", func(t *testing.T) {
		mutate := map[string]func(*checkpoint.Manifest){
			"tenant":         func(m *checkpoint.Manifest) { m.Tenant = uuid.New() },
			"epoch id":       func(m *checkpoint.Manifest) { m.EpochID = uuid.New() },
			"epoch number":   func(m *checkpoint.Manifest) { m.EpochNumber = 2 },
			"schema version": func(m *checkpoint.Manifest) { m.Schema.Version = 99 },
			"schema digest":  func(m *checkpoint.Manifest) { m.Schema.Digest = hashB },
			"root digest":    func(m *checkpoint.Manifest) { m.RootDigest = hashB },
			"covers from":    func(m *checkpoint.Manifest) { m.CoversFrom = m.CoversFrom.Add(time.Second) },
			"covers to":      func(m *checkpoint.Manifest) { m.CoversTo = m.CoversTo.Add(time.Second) },
			"created at":     func(m *checkpoint.Manifest) { m.CreatedAt = m.CreatedAt.Add(time.Second) },
			"stream head": func(m *checkpoint.Manifest) {
				heads := append([]checkpoint.StreamHead(nil), m.Streams...)
				heads[0].Sequence = 99
				m.Streams = heads
			},
		}
		for name, apply := range mutate {
			mutated := base
			apply(&mutated)
			got, err := mutated.CanonicalDigest()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got == want {
				t.Errorf("changing the %s did not change the manifest digest", name)
			}
		}
	})
}

func TestManifestValidate(t *testing.T) {
	if err := validManifest(t).Validate(); err != nil {
		t.Fatalf("a well-formed manifest was refused: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*checkpoint.Manifest)
		mustSay string
	}{
		{"no tenant", func(m *checkpoint.Manifest) { m.Tenant = uuid.Nil }, "tenant is required"},
		{"no epoch id", func(m *checkpoint.Manifest) { m.EpochID = uuid.Nil }, "epoch id is required"},
		{"epoch number zero", func(m *checkpoint.Manifest) { m.EpochNumber = 0 }, "not positive"},
		{"no schema version", func(m *checkpoint.Manifest) { m.Schema.Version = 0 }, "schema release version"},
		{"no schema digest", func(m *checkpoint.Manifest) { m.Schema.Digest = "" }, "schema release digest"},
		{"no streams", func(m *checkpoint.Manifest) { m.Streams = nil }, "at least one stream"},
		{"a head with no chain hash", func(m *checkpoint.Manifest) {
			heads := append([]checkpoint.StreamHead(nil), m.Streams...)
			heads[0].ChainHash = ""
			m.Streams = heads
		}, "no chain hash"},
		{"a head with no algorithm", func(m *checkpoint.Manifest) {
			heads := append([]checkpoint.StreamHead(nil), m.Streams...)
			heads[0].ChainAlgorithm = ""
			m.Streams = heads
		}, "algorithm its chain hash was produced with"},
		{"a head at sequence zero", func(m *checkpoint.Manifest) {
			heads := append([]checkpoint.StreamHead(nil), m.Streams...)
			heads[0].Sequence = 0
			m.Streams = heads
		}, "holds at least one event"},
		{"a duplicated stream", func(m *checkpoint.Manifest) {
			m.Streams = append(append([]checkpoint.StreamHead(nil), m.Streams...), m.Streams[0])
		}, "included twice"},
		{"no root digest algorithm", func(m *checkpoint.Manifest) { m.RootDigestAlgorithm = "" }, "root digest algorithm"},
		{"no covered window", func(m *checkpoint.Manifest) { m.CoversFrom, m.CoversTo = time.Time{}, time.Time{} }, "covered recorded-time window"},
		{"an inverted window", func(m *checkpoint.Manifest) { m.CoversFrom, m.CoversTo = m.CoversTo, m.CoversFrom }, "empty or inverted"},
		{"no creation time", func(m *checkpoint.Manifest) { m.CreatedAt = time.Time{} }, "creation time"},
		{"no signing key named", func(m *checkpoint.Manifest) { m.Signature = nil }, "signing key must be named"},
		{"no key id", func(m *checkpoint.Manifest) {
			sig := *m.Signature
			sig.KeyID = ""
			m.Signature = &sig
		}, "signing key id"},
		{"no public key", func(m *checkpoint.Manifest) {
			sig := *m.Signature
			sig.PublicKey = ""
			m.Signature = &sig
		}, "signing public key"},
		{"a wrong signature algorithm", func(m *checkpoint.Manifest) {
			sig := *m.Signature
			sig.Algorithm = "rsa"
			m.Signature = &sig
		}, "signature algorithm"},
		{"epoch 1 claiming a predecessor", func(m *checkpoint.Manifest) {
			m.PreviousEpochID = uuid.New()
			m.PreviousManifestDigest = hashA
		}, "opens the chain"},
		{"a later epoch with no predecessor", func(m *checkpoint.Manifest) { m.EpochNumber = 2 }, "does not name the epoch it follows"},
		{"a correction with no reason", func(m *checkpoint.Manifest) { m.CorrectsEpochID = uuid.New() }, "must say why"},
		{"a reason with nothing to correct", func(m *checkpoint.Manifest) { m.CorrectsReason = "because" }, "without an epoch to correct"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest(t)
			tc.mutate(&m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("Validate accepted a manifest with %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.mustSay) {
				t.Fatalf("Validate reported %q, want it to mention %q", err, tc.mustSay)
			}
		})
	}

	t.Run("a root digest that does not reproduce is a distinct failure", func(t *testing.T) {
		m := validManifest(t)
		m.RootDigest = hashB
		err := m.Validate()
		var mismatch checkpoint.ErrRootDigestMismatch
		if !errors.As(err, &mismatch) {
			t.Fatalf("Validate returned %v, want ErrRootDigestMismatch", err)
		}
		if mismatch.Actual != hashB || mismatch.Expected == hashB {
			t.Fatalf("mismatch reports expected %s actual %s, want the recomputed value as expected",
				mismatch.Expected, mismatch.Actual)
		}
	})

	t.Run("every problem is reported at once", func(t *testing.T) {
		m := validManifest(t)
		m.Tenant = uuid.Nil
		m.CreatedAt = time.Time{}
		var invalid checkpoint.ErrManifestInvalid
		if !errors.As(m.Validate(), &invalid) {
			t.Fatal("Validate did not report ErrManifestInvalid")
		}
		if len(invalid.Missing) < 2 {
			t.Fatalf("Validate reported %v, want both problems", invalid.Missing)
		}
	})
}

func TestOrderedStreamsDoesNotMutateTheManifest(t *testing.T) {
	m := validManifest(t)
	m.Streams = []checkpoint.StreamHead{m.Streams[1], m.Streams[0]}
	first := m.Streams[0].StreamKey

	ordered := m.OrderedStreams()
	if ordered[0].StreamKey != "worker:1" {
		t.Fatalf("OrderedStreams returned %q first, want worker:1", ordered[0].StreamKey)
	}
	if m.Streams[0].StreamKey != first {
		t.Fatal("OrderedStreams reordered the manifest's own slice")
	}
}
