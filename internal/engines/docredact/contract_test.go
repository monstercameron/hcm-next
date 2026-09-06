package docredact

import (
	"strings"
	"testing"
)

func TestVersionAndExplain(t *testing.T) {
	if Version() <= 0 {
		t.Fatalf("Version() = %d, want positive", Version())
	}
	if got := Explain(); !strings.Contains(got, "docredact") || !strings.Contains(got, "TOKENIZE") {
		t.Fatalf("Explain() = %q, want contract description", got)
	}
}

func TestResultExplainOmitsSourceText(t *testing.T) {
	r := Result{ArtifactDigest: "sha256:artifact", PolicyID: "policy", PolicyVersion: "v1", Audience: "reviewer", Redactions: []Redaction{{SpanPath: "page.1.line.1"}}, Digest: "sha256:result"}
	got := r.Explain()
	for _, want := range []string{"sha256:artifact", "policy", "v1", "reviewer", "redactions=1", "sha256:result"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, missing %q", got, want)
		}
	}
}
