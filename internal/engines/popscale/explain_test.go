package popscale

import (
	"strings"
	"testing"
)

func TestExplainNamesFieldStateAndVersionWithoutTheErrorToken(t *testing.T) {
	r := rejected("population_ref", "UNRESOLVED")
	got := r.Explain()
	for _, want := range []string{`"population_ref"`, `"UNRESOLVED"`, versionToken()} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, ErrRejected.Error()) {
		t.Fatalf("Explain() must not carry the log-matcher token: %q", got)
	}
}
