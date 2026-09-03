package documentredact

import (
	"context"
	"errors"
	"testing"
)

func request() Request {
	b := []byte("name=Alice; ssn=123-45-6789; status=active")
	return Request{Source: Source{ID: "doc-1", Digest: digest(b), Bytes: b}, Policy: Policy{ID: "dlp-human-v1", Version: "7", Purpose: "case-review", Recipient: "case-team", Regions: []Region{{Start: 11, End: 25}}, Fields: []Field{{Path: "person.name", Region: Region{Start: 5, End: 10}}}}, Surfaces: []Surface{Preview, Thumbnail, OCR, Search, Export}}
}

func TestTodo_DOC_REDACT_001(t *testing.T) {
	r, err := Generate(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(request()); err != nil {
		t.Fatal(err)
	}
	if len(r.Surfaces) != 5 || !r.Proof.Complete || r.Evidence.OutputDigest == "" {
		t.Fatalf("incomplete result: %+v", r)
	}
}

func TestTodo_DOC_REDACT_001_Golden(t *testing.T) {
	a, err := Generate(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if a.OutputDigest != b.OutputDigest || a.Proof.Digest != b.Proof.Digest {
		t.Fatal("same input and policy produced different digests")
	}
	for s, x := range a.Surfaces {
		if x.Digest != a.OutputDigest || string(x.Bytes) != string(a.Surfaces[Preview].Bytes) {
			t.Fatalf("surface %s differs", s)
		}
	}
}

func TestTodo_DOC_REDACT_001_Integration(t *testing.T) {
	r, err := Redact(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []Surface{Preview, Thumbnail, OCR, Search, Export} {
		if _, ok := r.Surfaces[s]; !ok {
			t.Fatalf("missing %s", s)
		}
	}
}

func TestTodo_DOC_REDACT_001_Incomplete(t *testing.T) {
	r, err := Generate(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	r.Surfaces[Preview] = SurfaceResult{Digest: digest([]byte("leak")), Bytes: []byte("name=Alice")}
	if !errors.Is(r.Verify(request()), ErrRedactionIncomplete) {
		t.Fatal("surface mismatch was not rejected")
	}
}

func TestTodo_DOC_REDACT_001_Validation(t *testing.T) {
	r := request()
	r.Source.Digest = "sha256:bad"
	if _, err := Generate(context.Background(), r); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	r = request()
	r.Policy.Regions = []Region{{Start: 99, End: 100}}
	if _, err := Generate(context.Background(), r); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
}
