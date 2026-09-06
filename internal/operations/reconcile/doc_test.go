package reconcile

import "testing"

// TestDoc_Smoke proves the package's documentation-only file (doc.go) compiles
// as part of an otherwise normal package, matching the paired-test convention
// this lane's other doc.go files (internal/workflow/timer, internal/workflow/lease)
// already use.
func TestDoc_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}
