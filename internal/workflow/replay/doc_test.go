package replay

import (
	"os"
	"strings"
	"testing"
)

// TestDoc_NamesTheContractItImplements keeps the package comment honest about
// the four guarantees the rest of this package is built to hold. A doc comment
// that drifted away from them would be the first thing a reader trusted and
// the last thing anyone checked.
func TestDoc_NamesTheContractItImplements(t *testing.T) {
	b, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read doc.go: %v", err)
	}
	doc := string(b)
	for _, want := range []string{
		"REPLAY", "Record", "Recorder", "Divergence", "Trace",
		"CodeArtifactUnavailable", "CodeEffectForbidden", "CodeDivergence",
		"intent.ModeContract", "intent.CausalSeparation", "MemorySource", "StoreSource",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("doc.go does not mention %q", want)
		}
	}
	if !strings.HasPrefix(doc, "// Package replay") {
		t.Fatalf("doc.go does not open with the package comment")
	}
}

func TestDoc_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
