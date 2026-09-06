package sandbox

import (
	"os"
	"strings"
	"testing"
)

// TestDoc_NamesTheContractItImplements keeps the package comment honest about
// the mechanisms the rest of this package is built around, the same
// discipline internal/workflow/replay's own doc_test.go holds itself to.
func TestDoc_NamesTheContractItImplements(t *testing.T) {
	b, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read doc.go: %v", err)
	}
	doc := string(b)
	for _, want := range []string{
		"SANDBOX-001", "INTENT-023", "ModeContractFor",
		"Fence", "FencedConnector", "FencedEffectError",
		"TenantPrefix", "Reset", "RunPromotionProof",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("doc.go does not mention %q", want)
		}
	}
	if !strings.HasPrefix(doc, "// Package sandbox") {
		t.Fatalf("doc.go does not open with the package comment")
	}
}
