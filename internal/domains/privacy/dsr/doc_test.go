package dsr

import "testing"

// TestDoc_Smoke is the package-doc companion test: it exists so
// internal/domains/privacy/dsr/doc.go (a doc-comment-only file) is not an
// untested hand-written file per this repository's per-file testing
// standard. The package's actual behavior is exercised by the PRIV-005
// matrix tests and the other per-file unit tests in this package.
func TestDoc_Smoke(t *testing.T) {
	if len(AllKinds()) == 0 {
		t.Fatal("package dsr declares no request kinds")
	}
}
