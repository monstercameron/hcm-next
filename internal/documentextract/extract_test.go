package documentextract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestTodo_DOC_EXTRACT_001(t *testing.T) {
	b := []byte("Alice\nEngineer\n")
	r := Request{Source: Source{ID: "a1", Digest: sum(b), MediaType: "text/plain", Classification: "CONFIDENTIAL", Taint: "UNTRUSTED", Bytes: b}, ParserVersion: "plain-1", ProfileVersion: "profile-1", Limits: Limits{MaxBytes: 100, MaxFields: 10}}
	got, err := Extract(context.Background(), r, nil, nil)
	if err != nil || got.State != Complete {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	if len(got.Fields) != 2 || got.Fields[0].Span.Start != 0 || got.Fields[1].Span.Start != 6 {
		t.Fatalf("fields=%+v", got.Fields)
	}
	if got.Fields[0].Classification != r.Source.Classification || got.Fields[0].Taint != r.Source.Taint {
		t.Fatalf("lineage lost: %+v", got.Fields[0])
	}
	if err := ValidateLineage(r, got); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_DOC_EXTRACT_001_Golden(t *testing.T) {
	b := []byte("x\ny")
	r := Request{Source: Source{ID: "gold", Digest: sum(b), MediaType: "text/plain", Bytes: b}, ParserVersion: "p1", ProfileVersion: "v1", Limits: Limits{MaxBytes: 10}}
	a, err := Extract(context.Background(), r, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := Extract(context.Background(), r, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.DerivativeDigest != b2.DerivativeDigest || a.DerivativeDigest == "" {
		t.Fatalf("non-deterministic digest %q/%q", a.DerivativeDigest, b2.DerivativeDigest)
	}
}

func TestTodo_DOC_EXTRACT_001_Security(t *testing.T) {
	b := []byte("x")
	r := Request{Source: Source{ID: "bad", Digest: sum([]byte("other")), Bytes: b}, ParserVersion: "p", ProfileVersion: "v"}
	got, err := Extract(context.Background(), r, nil, nil)
	if !errors.Is(err, ErrLineage) || got.State != Failed {
		t.Fatalf("digest mismatch accepted: %+v err=%v", got, err)
	}
	b = []byte("one\ntwo")
	r = Request{Source: Source{ID: "limited", Digest: sum(b), MediaType: "text/plain", Bytes: b}, ParserVersion: "p", ProfileVersion: "v", Limits: Limits{MaxBytes: 2}}
	if _, err := Extract(context.Background(), r, nil, nil); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit err=%v", err)
	}
}

func TestTodo_DOC_EXTRACT_001_Partial(t *testing.T) {
	b := []byte("one\ntwo")
	r := Request{Source: Source{ID: "partial", Digest: sum(b), MediaType: "text/plain", Bytes: b}, ParserVersion: "p", ProfileVersion: "v", Limits: Limits{MaxFields: 1}}
	got, err := Extract(context.Background(), r, nil, nil)
	if err != nil || got.State != Partial || len(got.Fields) != 1 {
		t.Fatalf("partial=%+v err=%v", got, err)
	}
}
