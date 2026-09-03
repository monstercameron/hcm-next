package artifacts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/artifacts"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// TestTodo_MODEL_029 proves the immutable content-addressed store's golden
// path: Put computes the sha256 content id itself, is idempotent by that id,
// and bytes retrieve back byte-for-byte with their digest verified; a
// correction is a new Put under new content, never an edit of the old row.
func TestTodo_MODEL_029(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	content := []byte("2026-q1 compensation letter, draft 1")
	wantID := sha256Hex(content)

	rec, created, err := f.put(f.putRequest(content))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if !created {
		t.Fatal("first put of new content reported created=false")
	}
	if rec.ContentID != wantID {
		t.Fatalf("content id %s, want %s", rec.ContentID, wantID)
	}
	if rec.ByteSize != int64(len(content)) {
		t.Fatalf("byte size %d, want %d", rec.ByteSize, len(content))
	}
	if rec.DigestAlgorithm != artifacts.Algorithm {
		t.Fatalf("digest algorithm %s, want %s", rec.DigestAlgorithm, artifacts.Algorithm)
	}

	t.Run("put is idempotent by content id", func(t *testing.T) {
		again, createdAgain, err := f.put(f.putRequest(content))
		if err != nil {
			t.Fatalf("replay put: %v", err)
		}
		if createdAgain {
			t.Fatal("replaying an identical put reported created=true")
		}
		if again != rec {
			t.Fatalf("replayed record %+v, want %+v", again, rec)
		}
	})

	t.Run("bytes retrieve back exactly and verify their own digest", func(t *testing.T) {
		got, gotRec, err := f.retrieve(rec.ContentID, allowAll("hr:reviewer"), fixedNow)
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if string(got) != string(content) {
			t.Fatalf("retrieved bytes %q, want %q", got, content)
		}
		if gotRec.ContentID != rec.ContentID {
			t.Fatalf("retrieved record names %s, want %s", gotRec.ContentID, rec.ContentID)
		}
	})

	t.Run("a correction is a new artifact under new content, never an edit", func(t *testing.T) {
		corrected := []byte("2026-q1 compensation letter, draft 2 (corrected amount)")
		correctedRec := f.mustPut(t, f.putRequest(corrected))
		if correctedRec.ContentID == rec.ContentID {
			t.Fatal("different bytes produced the same content id")
		}

		// The original artifact is untouched: it still retrieves its own
		// original bytes, not the correction's.
		got, _, err := f.retrieve(rec.ContentID, allowAll("hr:reviewer"), fixedNow)
		if err != nil {
			t.Fatalf("retrieve original after correction: %v", err)
		}
		if string(got) != string(content) {
			t.Fatal("the original artifact's bytes changed after a correction was written")
		}
	})
}

// TestTodo_MODEL_029_Property proves Put's identity contract holds across a
// range of content and metadata combinations: the content id is always the
// sha256 of the exact bytes, and identical content under identical identity
// metadata is always idempotent regardless of what that metadata is.
func TestTodo_MODEL_029_Property(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	cases := []struct {
		name    string
		content []byte
		class   model.ClassificationLabel
	}{
		{"empty content", []byte{}, model.ClassPublic},
		{"single byte", []byte{0x00}, model.ClassInternal},
		{"ascii text", []byte("a routine ledger export"), model.ClassPII},
		{"binary-ish bytes", []byte{0xff, 0x00, 0x7f, 0x80, 0x10, 0x00, 0xEE}, model.ClassMedical},
		{"long repeated content", bytesRepeat('a', 5000), model.ClassCompensation},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := f.putRequest(c.content)
			req.Classification = c.class

			first, created, err := f.put(req)
			if err != nil {
				t.Fatalf("put: %v", err)
			}
			if !created {
				t.Fatal("first put of distinct content reported created=false")
			}
			if first.ContentID != sha256Hex(c.content) {
				t.Fatalf("content id %s, want %s", first.ContentID, sha256Hex(c.content))
			}

			second, createdAgain, err := f.put(req)
			if err != nil {
				t.Fatalf("replay put: %v", err)
			}
			if createdAgain {
				t.Fatal("replaying an identical put reported created=true")
			}
			if second != first {
				t.Fatalf("replayed record %+v, want %+v", second, first)
			}
		})
	}
}

// TestTodo_MODEL_029_Golden pins the digest algorithm: a fixed, well-known
// byte sequence must always hash to the same sha256 hex content id, so a
// silent change of algorithm or encoding is caught here rather than by every
// caller's own tests independently.
func TestTodo_MODEL_029_Golden(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	const golden = "hcm-next artifact golden vector v1"
	const want = "1409dbd73a425a1dd797538569c64b742796fdc20f866642d2a0677de23394f2"

	rec := f.mustPut(t, f.putRequest([]byte(golden)))
	if rec.ContentID != want {
		t.Fatalf("golden vector hashed to %s, want %s", rec.ContentID, want)
	}
	if len(rec.ContentID) != 64 {
		t.Fatalf("content id %q is not a 64 character hex digest", rec.ContentID)
	}
	if !artifacts.ValidContentID(rec.ContentID) {
		t.Fatalf("content id %q does not satisfy ValidContentID", rec.ContentID)
	}
}

// TestTodo_MODEL_029_Security proves the RED cases: a claimed content id that
// does not match its bytes is rejected before anything is written; a second
// put of the same bytes with different identity metadata is an immutable
// conflict, not a silent overwrite; an artifact missing retention or creator
// metadata cannot be written at all, so it can never be referenced either;
// and one tenant's artifact is invisible under another tenant's id.
func TestTodo_MODEL_029_Security(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	t.Run("a claimed content id that does not match its bytes is refused", func(t *testing.T) {
		req := f.putRequest([]byte("mismatched content"))
		req.ContentID = sha256Hex([]byte("some other content entirely"))
		_, _, err := f.put(req)
		var mismatch artifacts.ErrDigestMismatch
		if !errors.As(err, &mismatch) {
			t.Fatalf("put with a forged content id returned %v, want ErrDigestMismatch", err)
		}
	})

	t.Run("a tenant/classification conflict on the same content id is refused", func(t *testing.T) {
		content := []byte("shared bytes, disputed classification")
		first := f.putRequest(content)
		first.Classification = model.ClassPublic
		f.mustPut(t, first)

		second := f.putRequest(content)
		second.Classification = model.ClassSpecialCategory
		_, _, err := f.put(second)
		var conflict artifacts.ErrImmutableConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("re-putting %x with a different classification returned %v, want ErrImmutableConflict", sha256Hex(content), err)
		}
		if conflict.Field != "classification" {
			t.Fatalf("conflict field %q, want %q", conflict.Field, "classification")
		}
	})

	t.Run("a mutable overwrite of identity metadata is refused", func(t *testing.T) {
		content := []byte("shared bytes, disputed creator")
		first := f.putRequest(content)
		first.CreatorPrincipalRef = "authority:workday"
		f.mustPut(t, first)

		second := f.putRequest(content)
		second.CreatorPrincipalRef = "authority:someone-else"
		_, _, err := f.put(second)
		var conflict artifacts.ErrImmutableConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("re-putting with a different creator returned %v, want ErrImmutableConflict", err)
		}
	})

	t.Run("an artifact missing retention or creator metadata cannot be written, and so can never be referenced", func(t *testing.T) {
		missingRetention := f.putRequest([]byte("no retention class"))
		missingRetention.RetentionClass = ""
		if _, _, err := f.put(missingRetention); err == nil {
			t.Fatal("put with no retention class succeeded")
		}

		missingCreator := f.putRequest([]byte("no creator principal"))
		missingCreator.CreatorPrincipalRef = ""
		if _, _, err := f.put(missingCreator); err == nil {
			t.Fatal("put with no creator principal succeeded")
		}

		// Because such a row can never be written, a reference against its
		// content id -- which was never actually stored -- is refused as
		// not found, not silently accepted (MODEL-029 RED).
		neverStored := sha256Hex([]byte("no retention class"))
		err := f.addReference(neverStored, artifacts.OwnerRef{Kind: artifacts.OwnerReceipt, ID: "receipt-1"})
		var notFound artifacts.ErrNotFound
		if !errors.As(err, &notFound) {
			t.Fatalf("referencing a never-written content id returned %v, want ErrNotFound", err)
		}
	})

	t.Run("an artifact is invisible under another tenant's id", func(t *testing.T) {
		content := []byte("tenant-scoped content")
		rec := f.mustPut(t, f.putRequest(content))

		other := fixture{db: f.db, schema: f.schema, tenant: insertTenant(t, f.db, "other-tenant")}
		if _, err := artifactsRead(other, rec.ContentID); err == nil {
			t.Fatal("another tenant read an artifact it never wrote")
		}
		if _, _, err := other.retrieve(rec.ContentID, allowAll("intruder"), fixedNow); err == nil {
			t.Fatal("another tenant retrieved bytes for an artifact it never wrote")
		}
	})
}

// FuzzTodo_MODEL_029 fuzzes the pure content-id contract with no database
// involved, matching internal/data/pgtest's guidance that a `go test -fuzz`
// worker never gets its own PostgreSQL server: the sha256 digest of any byte
// string is deterministic and is always recognized by [artifacts.ValidContentID].
func FuzzTodo_MODEL_029(f *testing.F) {
	f.Add([]byte("hello, artifact store"))
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, content []byte) {
		id := sha256Hex(content)
		if !artifacts.ValidContentID(id) {
			t.Fatalf("computed content id %q is not recognized as a valid content id", id)
		}
		if again := sha256Hex(content); again != id {
			t.Fatalf("sha256 of the same bytes produced %s then %s", id, again)
		}
	})
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// artifactsRead runs the read-only, no-bytes lookup in its own transaction.
func artifactsRead(f fixture, contentID string) (artifacts.Record, error) {
	var rec artifacts.Record
	err := f.inTx(func(tx dbport.Tx) error {
		var readErr error
		rec, readErr = artifacts.Read(context.Background(), tx, f.schema, f.tenant, contentID)
		return readErr
	})
	return rec, err
}
