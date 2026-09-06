package artifacts

import "testing"

// doc.go carries no executable behavior of its own; this is the same no-op
// smoke test internal/workflow/migrate/doc_test.go pins for its package-doc
// file, present so every hand-written file in this package has a companion
// test.
func TestDoc_Smoke(t *testing.T) {
	t.Parallel()
	if len(Kinds()) == 0 {
		t.Fatal("the package declares no artifact kinds")
	}
}
