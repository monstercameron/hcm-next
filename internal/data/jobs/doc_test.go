package jobs

import "testing"

// TestDoc_Smoke is doc.go's paired test: the file carries only a package
// comment, so this records that the package still compiles and the doc
// comment is not orphaned from a renamed or removed package.
func TestDoc_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}
