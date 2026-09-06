package provenance_test

import "testing"

// TestPackageDocCompiles is a placeholder ensuring doc.go's package
// comment (a non-executable file on its own) sits in an importable
// package. Every hand-written file in this package gets a paired test per
// the repository's per-file testing standard; doc.go's own content is
// prose, so this file just proves the package still imports and builds.
func TestPackageDocCompiles(t *testing.T) {
	// No behavior to exercise; a failed import/build would fail this
	// package's whole test binary before this test body ever ran.
}
