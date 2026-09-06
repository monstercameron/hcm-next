package workforce_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/workforce"
	"github.com/monstercameron/hcm-next/internal/domains/people"
)

// TestPackageSurfaceIsTheDocumentedOne holds doc.go's two structural claims to
// account: the layered reader really is a people.WorkerFacts (so a cell can
// wire it as its single governed worker read), and the created-population
// reader is one too (so the layering is a composition of the same port rather
// than a special case).
func TestPackageSurfaceIsTheDocumentedOne(t *testing.T) {
	t.Parallel()
	var _ people.WorkerFacts = workforce.NewLayeredWorkerFacts(nil, nil, nil)
	var _ people.WorkerFacts = workforce.NewFacts(nil, nil)
}

// TestDeclaredAuthorityIsTheCellsOwnPeopleAuthority pins the source authority
// doc.go promises created workers are disclosed under. It is the corpus's own
// authority verbatim: an explanation must not be able to tell a created worker
// from a corpus worker by its authority, because their authority really is the
// same.
func TestDeclaredAuthorityIsTheCellsOwnPeopleAuthority(t *testing.T) {
	t.Parallel()
	if workforce.SourceSystem != "hcmnext.people" {
		t.Errorf("SourceSystem = %q, want hcmnext.people", workforce.SourceSystem)
	}
	if workforce.AuthorityPolicy != "people.source_authority/2026.1" {
		t.Errorf("AuthorityPolicy = %q, want the People authority-by-field policy", workforce.AuthorityPolicy)
	}
}
