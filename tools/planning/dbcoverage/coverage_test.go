package dbcoverage

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent/model"
	"github.com/monstercameron/hcm-next/tools/gen/storagemanifest"
)

func TestCheckRequiresExplicitDisposition(t *testing.T) {
	objects := []Object{
		{Kind: Entity, Name: "Person", Source: "people-workforce.md", Owner: "people", Disposition: Database, StorageRef: "ledger_stream:PERSON"},
		{Kind: Property, Name: "person.name", Source: "people-workforce.md", Owner: "people", Disposition: NonDatabase, StorageRef: "embedded"},
		{Kind: Relationship, Name: "person.worker", Source: "people-workforce.md", Owner: "people"},
	}
	r := Check(objects)
	if r.Total != 3 || r.Verified != 2 || len(r.Gaps) != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
	if r.Gaps[0].Source != "people-workforce.md" || r.Gaps[0].Kind != Relationship {
		t.Fatalf("gap lost source/kind: %+v", r.Gaps[0])
	}
	if r.ByDisposition[Database] != 1 || r.ByDisposition[NonDatabase] != 1 {
		t.Fatalf("disposition counts: %+v", r.ByDisposition)
	}
}

func TestCheckDigestAndOrderingAreStable(t *testing.T) {
	a := []Object{{Kind: Property, Name: "b", Source: "z.md", Owner: "people", Disposition: NonDatabase, StorageRef: "embedded"}, {Kind: Entity, Name: "a", Source: "a.md", Owner: "people", Disposition: Database, StorageRef: "table a"}}
	b := []Object{a[1], a[0]}
	ra, rb := Check(a), Check(b)
	if ra.Digest == "" || ra.Digest != rb.Digest {
		t.Fatalf("digest not stable: %q != %q", ra.Digest, rb.Digest)
	}
	if !ra.FullyVerified() || !rb.FullyVerified() {
		t.Fatalf("reports should verify: %+v %+v", ra, rb)
	}
}

func TestCheckRejectsUnknownKindOwnerAndTarget(t *testing.T) {
	r := Check([]Object{
		{Kind: Kind("AUTHORITY"), Name: "a", Source: "x.md", Owner: "people", Disposition: NonDatabase, StorageRef: "registry"},
		{Kind: Entity, Name: "b", Source: "x.md", Disposition: NonDatabase, StorageRef: "registry"},
		{Kind: Entity, Name: "c", Source: "x.md", Owner: "people", Disposition: NonDatabase},
	})
	if r.FullyVerified() || len(r.Gaps) != 3 {
		t.Fatalf("expected three explicit contract gaps, got %+v", r.Gaps)
	}
}

func TestCheckDigestTieBreakersAreStable(t *testing.T) {
	a := []Object{
		{Kind: Entity, Name: "same", Source: "x.md", Owner: "b", Disposition: Database, StorageRef: "t2"},
		{Kind: Entity, Name: "same", Source: "x.md", Owner: "a", Disposition: Database, StorageRef: "t1"},
	}
	b := []Object{a[1], a[0]}
	if Check(a).Digest != Check(b).Digest {
		t.Fatal("digest changed with input order for otherwise equal sort keys")
	}
}

func TestFromManifestsCoversRegistryObjects(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	storage := storagemanifest.DispositionManifest{Entities: make([]storagemanifest.EntityDisposition, 0, len(reg.Entities()))}
	for _, e := range reg.Entities() {
		storage.Entities = append(storage.Entities, storagemanifest.EntityDisposition{EntityRef: e.Ref.String(), Key: e.Key, Owner: e.OwnerDomain, Disposition: storagemanifest.DispositionTable, Target: "table " + e.Key})
	}
	objects := FromManifests(reg, storage, "registry-and-coverage-contracts.md")
	if len(objects) <= len(reg.Entities()) {
		t.Fatalf("expected properties/lifecycle rows, got %d", len(objects))
	}
	r := Check(objects)
	if !r.FullyVerified() {
		t.Fatalf("adapted registry has gaps: %v", r.Gaps)
	}
	if r.ByKind[Entity] != len(reg.Entities()) || r.ByKind[State] != len(reg.Entities()) {
		t.Fatalf("kind counts: %+v", r.ByKind)
	}
}
