package object

import (
	"errors"
	"testing"
)

func metadataPolicy() MetadataPolicy {
	return MetadataPolicy{MaxBytes: 1024, AllowedMediaTypes: []string{"text/plain", "application/pdf", "image/png"}, Classification: "INTERNAL", RetentionPolicy: "WORKFORCE-7Y", RequireSourceRef: true}
}

func TestTodo_ARTIFACT_005(t *testing.T) {
	got, err := Normalize(IngestRequest{SuggestedFilename: "report.txt", DeclaredMediaType: "text/plain", Content: []byte("hello"), SourceRef: "upload/1"}, metadataPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtifactID != got.Digest || got.Size != 5 || got.MediaType != "text/plain" || got.Classification != "INTERNAL" || got.RetentionPolicy != "WORKFORCE-7Y" {
		t.Fatalf("metadata = %#v", got)
	}
	if got.Filename == "../secret.txt" || got.Filename == "" {
		t.Fatalf("filename was not server-derived: %q", got.Filename)
	}
}

func TestTodo_ARTIFACT_005_Golden(t *testing.T) {
	a, err := Normalize(IngestRequest{SuggestedFilename: "report.txt", Content: []byte("same"), SourceRef: "source/1"}, metadataPolicy())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Normalize(IngestRequest{SuggestedFilename: "other.txt", Content: []byte("same"), SourceRef: "source/1"}, metadataPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("same bytes and lineage should normalize identically: %#v %#v", a, b)
	}
}

func TestTodo_ARTIFACT_005_Security(t *testing.T) {
	cases := []IngestRequest{
		{SuggestedFilename: "C:\\windows\\system32", Content: []byte("x"), SourceRef: "s"},
		{SuggestedFilename: "bad\x00name", Content: []byte("x"), SourceRef: "s"},
		{DeclaredMediaType: "application/pdf", Content: []byte("plain text"), SourceRef: "s"},
		{Content: []byte("x"), SourceRef: "s", UserMetadata: map[string]string{"../x": "y"}},
	}
	for _, in := range cases {
		if _, err := Normalize(in, metadataPolicy()); !errors.Is(err, ErrInvalidMetadata) && !errors.Is(err, ErrMediaTypeMismatch) && !errors.Is(err, ErrMetadataConflict) {
			t.Errorf("input %#v error = %v", in, err)
		}
	}
}

func TestTodo_ARTIFACT_005_Integration(t *testing.T) {
	policy := metadataPolicy()
	policy.MaxBytes = 3
	if _, err := Normalize(IngestRequest{Content: []byte("four"), SourceRef: "s"}, policy); !errors.Is(err, ErrMetadataTooLarge) {
		t.Fatalf("size error = %v", err)
	}
	if _, err := Normalize(IngestRequest{Content: []byte{0x00, 0xff}, SourceRef: "s"}, metadataPolicy()); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("binary allowlist error = %v", err)
	}
}

func TestTodo_ARTIFACT_005_Race(t *testing.T) {
	const workers = 16
	done := make(chan SafeMetadata, workers)
	for i := 0; i < workers; i++ {
		go func() {
			m, err := Normalize(IngestRequest{Content: []byte("parallel"), SourceRef: "s"}, metadataPolicy())
			if err != nil {
				t.Errorf("normalize: %v", err)
				return
			}
			done <- m
		}()
	}
	first := <-done
	for i := 1; i < workers; i++ {
		if got := <-done; got != first {
			t.Fatalf("concurrent result differs: %#v %#v", first, got)
		}
	}
}

func FuzzTodo_ARTIFACT_005(f *testing.F) {
	f.Add([]byte("seed"))
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) == 0 || len(content) > 1024 {
			return
		}
		m, err := Normalize(IngestRequest{Content: content, SourceRef: "fuzz"}, metadataPolicy())
		if err == nil && m.Validate() != nil {
			t.Fatal("successful normalization returned invalid metadata")
		}
	})
}
