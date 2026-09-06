package messagetemplate

import (
	"strings"
	"testing"
)

func TestVersionIsTheEngineContractVersion(t *testing.T) {
	if got := Version(); got != 1 {
		t.Fatalf("Version() = %d, want 1", got)
	}
}

func TestExplainNamesTheBindingButNotTheBody(t *testing.T) {
	r := Rendered{Key: "offer.letter", Version: 3, Purpose: "OFFER", Channel: "EMAIL", Locale: "en-US",
		Classification: "CONFIDENTIAL", Subject: "Your offer", Body: "Dear Priya, secret salary 93000", Digest: "sha256:abc"}
	got := r.Explain()
	for _, want := range []string{"offer.letter@3", "OFFER", "EMAIL", "en-US", "CONFIDENTIAL", "sha256:abc"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "93000") || strings.Contains(got, "Priya") || strings.Contains(got, "Your offer") {
		t.Fatalf("Explain() must not repeat rendered content: %q", got)
	}
}
