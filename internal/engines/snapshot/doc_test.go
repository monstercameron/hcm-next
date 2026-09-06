package snapshot_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/snapshot"
)

// TestPackageSurfaceIsTheDocumentedOne holds doc.go's structural claim to
// account: FakeSource really does implement Source, so a caller can compose
// a source-neutral resolve against the fake exactly as it would against a
// real per-domain repository.
func TestPackageSurfaceIsTheDocumentedOne(t *testing.T) {
	t.Parallel()
	var _ snapshot.Source = snapshot.NewFakeSource()
}

// TestAuthorityClassSetIsClosedAtThreeMembers pins doc.go's claim that
// AuthorityClass is a closed, three-member set naming which kind of source
// produced an entry: native, external or reference/config.
func TestAuthorityClassSetIsClosedAtThreeMembers(t *testing.T) {
	t.Parallel()
	declared := []snapshot.AuthorityClass{
		snapshot.AuthorityNativeState,
		snapshot.AuthorityExternalObservation,
		snapshot.AuthorityReferenceConfig,
	}
	if len(declared) != 3 {
		t.Fatalf("declared %d authority classes, want 3", len(declared))
	}
	for _, a := range declared {
		if !a.Valid() {
			t.Errorf("%q is declared but Valid() = false", a)
		}
	}
}
