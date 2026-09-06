package abuse_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/abuse"
)

// TestTodo_ABUSE_001_Conformance is the ABUSE-001 conformance test: every
// signal kind in the closed governed vocabulary has exactly one
// classification, and nothing outside the vocabulary has one.
func TestTodo_ABUSE_001_Conformance(t *testing.T) {
	kinds := abuse.SignalKinds()
	if len(kinds) == 0 {
		t.Fatal("SignalKinds() returned an empty vocabulary")
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		if !k.Valid() {
			t.Errorf("vocabulary member %q reports itself invalid", k)
		}
		class, ok := k.Classification()
		if !ok || class == "" {
			t.Errorf("signal kind %q has no classification", k)
		}
		seen[class] = true
	}
	if len(seen) == 0 {
		t.Fatal("no classifications observed across the vocabulary")
	}

	if abuse.SignalKind("NOT_A_GOVERNED_KIND").Valid() {
		t.Fatal("an ungoverned signal kind reported itself valid")
	}
	if _, ok := abuse.SignalKind("NOT_A_GOVERNED_KIND").Classification(); ok {
		t.Fatal("an ungoverned signal kind produced a classification")
	}
}

func TestSignalKindsIsStableAndDeterministic(t *testing.T) {
	a := abuse.SignalKinds()
	b := abuse.SignalKinds()
	if len(a) != len(b) {
		t.Fatalf("SignalKinds() length changed between calls: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("SignalKinds() order changed between calls at index %d: %q vs %q", i, a[i], b[i])
		}
	}
}
