package issuerregistry_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
)

// TestPackageDocCompiles is a placeholder proving the package (and its
// doc.go package comment) compiles, matching internal/platform/configregistry's
// own doc_test.go convention. The substantive behavior doc.go describes is
// exercised by the rest of this package's test suite.
func TestPackageDocCompiles(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	if store == nil {
		t.Fatal("NewMemoryStore returned nil")
	}
	if _, err := issuerregistry.Lookup(store, "doc-tenant", "https://unknown.invalid/"); !errors.Is(err, issuerregistry.ErrUnknownIssuer) {
		t.Fatalf("package contract lookup error = %v, want ErrUnknownIssuer", err)
	}
}
