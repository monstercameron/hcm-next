package configregistry_test

import "testing"

// TestPackageDocCompiles is a placeholder proving the package (and its doc.go
// package comment) compiles and is importable under its _test external test
// package convention, matching internal/workflow/version/doc_test.go's own
// pattern. The substantive behavior doc.go describes is exercised by the
// other test files in this package.
func TestPackageDocCompiles(t *testing.T) {
	t.Parallel()
}
