package evolution

import "testing"

// TestDoc_Smoke proves the package compiles and imports cleanly under go
// vet/go test, matching the smoke convention used by internal/intent's own
// doc_test.go.
func TestDoc_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
