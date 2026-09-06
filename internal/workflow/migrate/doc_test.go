package migrate

import "testing"

// doc.go carries no executable behavior of its own; this is the same
// no-op smoke test internal/workflow/runtime/doc_test.go pins for its
// package-doc file, present so every hand-written file in this package has a
// companion test.
func TestDoc_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}
