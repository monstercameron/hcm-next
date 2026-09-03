package documentextract

import (
	"context"
	"testing"
)

type matrixParser struct{}

func (matrixParser) Extract(_ context.Context, b []byte, _ Limits) ([]Field, error) {
	return []Field{{
		Path: "body.text", Raw: string(b), Normalized: string(b), Kind: Text,
		Span: Span{Page: 1, Start: 0, End: uint32(len(b))}, Confidence: 0.9,
	}}, nil
}

type matrixOCR struct{}

func (matrixOCR) Extract(_ context.Context, b []byte, _ Limits) ([]Field, error) {
	return []Field{{
		Path: "body.ocr", Raw: "ocr:" + string(b), Normalized: string(b), Kind: OCR,
		Span: Span{Page: 1, Start: 0, End: uint32(len(b))}, Confidence: 0.8,
	}}, nil
}

func TestTodo_DOC_EXTRACT_001_Integration(t *testing.T) {
	b := []byte("employee record")
	r := Request{
		Source:        Source{ID: "integration-1", Digest: sum(b), MediaType: "application/pdf", Classification: "RESTRICTED", Taint: "UNTRUSTED", Bytes: b},
		ParserVersion: "parser-2", ProfileVersion: "profile-3", EnableOCR: true,
		Limits:   Limits{MaxBytes: 1024, MaxOutputBytes: 1024, MaxFields: 10, MaxSpanBytes: 1024},
		Metadata: map[string]string{"filename": "record.pdf"},
	}
	got, err := Extract(context.Background(), r, matrixParser{}, matrixOCR{})
	if err != nil || got.State != Complete {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	if len(got.Fields) != 3 || got.Fields[0].Path != "body.ocr" || got.Fields[1].Path != "body.text" || got.Fields[2].Path != "metadata.filename" {
		t.Fatalf("fields=%+v", got.Fields)
	}
	if got.SourceID != r.Source.ID || got.SourceDigest != r.Source.Digest || got.ParserVersion != r.ParserVersion || got.ProfileVersion != r.ProfileVersion {
		t.Fatalf("source lineage=%+v", got)
	}
	for _, field := range got.Fields {
		if field.Classification != r.Source.Classification || field.Taint != r.Source.Taint {
			t.Fatalf("field lineage=%+v", field)
		}
	}
	if err := ValidateLineage(r, got); err != nil {
		t.Fatal(err)
	}
}

func FuzzTodo_DOC_EXTRACT_001(f *testing.F) {
	f.Add([]byte("hello\nworld"))
	f.Add([]byte(""))
	f.Add([]byte{0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) == 0 {
			return
		}
		r := Request{
			Source:        Source{ID: "fuzz", Digest: sum(b), MediaType: "text/plain", Bytes: b},
			ParserVersion: "parser-1", ProfileVersion: "profile-1",
			Limits: Limits{MaxBytes: uint64(len(b)), MaxOutputBytes: uint64(len(b)) + 1024, MaxFields: 10000, MaxSpanBytes: uint32(len(b))},
		}
		got, err := Extract(context.Background(), r, nil, nil)
		if err != nil {
			return
		}
		if got.State != Complete && got.State != Partial && got.State != Failed {
			t.Fatalf("invalid state %q", got.State)
		}
		if err := ValidateLineage(r, got); err != nil {
			t.Fatalf("lineage=%v result=%+v", err, got)
		}
	})
}
