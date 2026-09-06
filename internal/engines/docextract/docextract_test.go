package docextract

import (
	"context"
	"errors"
	"testing"
)

func doc() Document {
	return Document{Pages: []Page{{Number: 1, Text: "Alice\nEngineer"}, {Number: 2, Text: "Active"}}, Metadata: map[string]string{"filename": "alice.txt"}}
}

func limits() Limits { return Limits{MaxBytes: 1024, MaxPages: 2, MaxOutputBytes: 1024, MaxSpans: 20} }

func TestTodo_DOC_EXTRACT_001(t *testing.T) {
	result, err := ExtractDocument(context.Background(), doc(), limits(), nil)
	if err != nil || result.State != Complete {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Text != "Alice\nEngineer\nActive" || result.Digest == "" {
		t.Fatalf("text/digest=%q/%q", result.Text, result.Digest)
	}
	if len(result.Structured.Paragraphs) != 3 {
		t.Fatalf("structured=%+v", result.Structured)
	}
	if err := ValidateLineage(doc(), result); err != nil {
		t.Fatal(err)
	}
	for _, span := range result.Spans {
		if span.Lineage.ArtifactDigest == "" {
			t.Fatalf("span lost artifact lineage: %+v", span)
		}
	}
}

func TestTodo_DOC_EXTRACT_001_Golden(t *testing.T) {
	first, err := ExtractDocument(context.Background(), doc(), limits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExtractDocument(context.Background(), doc(), limits(), nil)
	if err != nil || first.Digest != second.Digest {
		t.Fatalf("digest not stable: %q != %q (err=%v)", first.Digest, second.Digest, err)
	}
	reordered := doc()
	reordered.Metadata = map[string]string{"z": "last", "filename": "alice.txt"}
	third, err := ExtractDocument(context.Background(), reordered, limits(), nil)
	if err != nil || third.Digest == first.Digest {
		t.Fatalf("metadata change did not alter digest: %q %q", first.Digest, third.Digest)
	}
}

func TestTodo_DOC_EXTRACT_001_Integration(t *testing.T) {
	request := Request{Document: Document{Pages: []Page{{Number: 1, Text: ""}, {Number: 2, Text: "plain"}}}, Limits: limits(), OCR: MemoryOCR{Pages: map[uint32]string{1: "scanned"}}, ParserVersion: "plain-v1", ProfileVersion: "profile-v1"}
	result, err := Extract(context.Background(), request)
	if err != nil || result.State != Complete {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	foundOCR := false
	for _, span := range result.Spans {
		if span.Kind == KindOCR && span.Text == "scanned" {
			foundOCR = true
		}
	}
	if !foundOCR {
		t.Fatalf("OCR span missing: %+v", result.Spans)
	}
	if err := ValidateLineage(request.Document, result); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_DOC_EXTRACT_001_Security(t *testing.T) {
	tooManyPages := Document{Pages: []Page{{Number: 1, Text: "one"}, {Number: 2, Text: "two"}}}
	result, err := ExtractDocument(context.Background(), tooManyPages, Limits{MaxBytes: 100, MaxPages: 1}, nil)
	if !errors.Is(err, ErrLimitExceeded) || result.State == Complete {
		t.Fatalf("page limit result=%+v err=%v", result, err)
	}
	tooManyBytes := Document{Bytes: []byte("too large")}
	result, err = ExtractDocument(context.Background(), tooManyBytes, Limits{MaxBytes: 2, MaxPages: 1}, nil)
	if !errors.Is(err, ErrLimitExceeded) || result.State == Complete {
		t.Fatalf("byte limit result=%+v err=%v", result, err)
	}
}

func FuzzTodo_DOC_EXTRACT_001(f *testing.F) {
	f.Add("hello\nworld")
	f.Add("")
	f.Fuzz(func(t *testing.T, text string) {
		if text == "" {
			return
		}
		result, err := ExtractDocument(context.Background(), Document{Pages: []Page{{Number: 1, Text: text}}}, Limits{MaxBytes: uint64(len(text)) + 1, MaxPages: 1}, nil)
		if err != nil {
			return
		}
		if result.State != Complete || result.Digest == "" {
			t.Fatalf("result=%+v", result)
		}
		if err := ValidateLineage(Document{Pages: []Page{{Number: 1, Text: text}}}, result); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMemoryOCRHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (MemoryOCR{Pages: map[uint32]string{1: "x"}}).Recognize(ctx, OCRRequest{Page: Page{Number: 1}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("OCR error=%v", err)
	}
}
