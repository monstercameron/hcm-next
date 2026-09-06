package docextract

import (
	"strings"
	"testing"
)

func TestVersionIsTheEngineContractVersion(t *testing.T) {
	if got := Version(); got != 1 {
		t.Fatalf("Version() = %d, want 1", got)
	}
}

func TestExplainReportsBoundsAndLineageWithoutExtractedText(t *testing.T) {
	r := Result{ArtifactDigest: "sha256:art", State: "COMPLETE", Text: "salary 93000 for Priya",
		Spans: []Span{{Path: "p1", Text: "salary 93000"}, {Path: "p2", Text: "Priya"}}, Digest: "sha256:res",
		PagesExamined: 2, BytesExamined: 128}
	got := r.Explain()
	for _, want := range []string{"sha256:art", "COMPLETE", "2 page(s)", "128 byte(s)", "2 lineage-bearing span(s)", "sha256:res"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "93000") || strings.Contains(got, "Priya") {
		t.Fatalf("Explain() must not repeat extracted text: %q", got)
	}
	r.Error = "limit.bytes"
	if !strings.Contains(r.Explain(), "refusal: limit.bytes") {
		t.Fatalf("Explain() must surface the refusal: %q", r.Explain())
	}
}
