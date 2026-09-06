package replay

import (
	"strings"
	"testing"
)

// TestDivergenceFields_AreDistinct pins the classification an investigation
// triages on.
func TestDivergenceFields_AreDistinct(t *testing.T) {
	seen := map[DivergenceField]bool{}
	for _, f := range []DivergenceField{
		FieldFrontier, FieldArtifact, FieldRoute, FieldOpenFrontier,
		FieldTerminal, FieldTraceDigest,
	} {
		if f == "" || seen[f] {
			t.Fatalf("divergence field %q is empty or duplicated", f)
		}
		seen[f] = true
	}
	if len(seen) != 6 {
		t.Fatalf("declared %d divergence fields, want 6", len(seen))
	}
}

// TestDivergence_StringNamesBothSides checks the one-line rendering a report
// prints: it must say which node, which field and what each side held, and it
// must render an empty side visibly rather than as nothing.
func TestDivergence_StringNamesBothSides(t *testing.T) {
	d := Divergence{
		Sequence: 4, NodeID: "raise_threshold", Attempt: 1, Field: FieldRoute,
		Recorded: "EXCEEDS_THRESHOLD", Replayed: "",
		Detail: "the compiled plan cannot route the recorded outcome",
	}
	got := d.String()
	for _, want := range []string{
		"raise_threshold", string(FieldRoute), `"EXCEEDS_THRESHOLD"`, `""`, d.Detail,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("String() = %q, missing %q", got, want)
		}
	}

	// A whole-run divergence names the instance rather than an empty node.
	whole := Divergence{Field: FieldTraceDigest, Recorded: "a", Replayed: "b"}
	if !strings.HasPrefix(whole.String(), "instance ") {
		t.Fatalf("String() = %q, want it to name the instance", whole.String())
	}
	if strings.Contains(whole.String(), "(") {
		t.Fatalf("a divergence with no detail rendered an empty parenthesis: %q", whole.String())
	}
}

func TestQuote(t *testing.T) {
	if got := quote(""); got != `""` {
		t.Fatalf("quote(\"\") = %q", got)
	}
	if got := quote("x"); got != `"x"` {
		t.Fatalf("quote(\"x\") = %q", got)
	}
}
