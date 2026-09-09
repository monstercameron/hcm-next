package dbcoverage

import (
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest"
)

// TestTodo_DB_COVERAGE_001 is the primary contract: every supported object
// must carry an owner and an explicit, traceable disposition.
func TestTodo_DB_COVERAGE_001(t *testing.T) {
	r := Check([]Object{
		{Kind: Entity, Name: "Person", Source: "people.md", Owner: "people", Disposition: Database, StorageRef: "table people"},
		{Kind: Property, Name: "Person.name", Source: "people.md", Owner: "people", Disposition: NonDatabase, StorageRef: "embedded:person"},
		{Kind: State, Name: "Person/lifecycle", Source: "people.md", Owner: "Person", Disposition: NonDatabase, StorageRef: "lifecycle:employment"},
	})
	if !r.FullyVerified() || r.Total != 3 || r.Verified != 3 {
		t.Fatalf("complete explicit corpus was not verified: %+v", r)
	}
	if r.ByKind[Entity] != 1 || r.ByKind[Property] != 1 || r.ByKind[State] != 1 {
		t.Fatalf("kind counts lost: %+v", r.ByKind)
	}
}

// TestTodo_DB_COVERAGE_001_Golden checks the report shape and stable digest
// used by CI artifacts, including all disposition and source indexes.
func TestTodo_DB_COVERAGE_001_Golden(t *testing.T) {
	objects := []Object{
		{Kind: Relationship, Name: "Person.manager", Source: "people.md", Owner: "people", Disposition: NonDatabase, StorageRef: "registry"},
		{Kind: Entity, Name: "Person", Source: "people.md", Owner: "people", Disposition: Database, StorageRef: "table people"},
	}
	a, b := Check(objects), Check([]Object{objects[1], objects[0]})
	if a.Digest == "" || a.Digest != b.Digest {
		t.Fatalf("digest is not stable under source ordering: %q != %q", a.Digest, b.Digest)
	}
	if a.ByFile["people.md"] != 2 || a.ByDisposition[Database] != 1 || a.ByDisposition[NonDatabase] != 1 {
		t.Fatalf("report indexes incomplete: %+v", a)
	}
	if len(a.Gaps) != 0 || !a.FullyVerified() {
		t.Fatalf("golden corpus unexpectedly has gaps: %+v", a.Gaps)
	}
}

// TestTodo_DB_COVERAGE_001_Integration adapts the real catalog and proves a
// missing manifest row is reported against its model source, rather than
// being silently inferred from an entity name.
func TestTodo_DB_COVERAGE_001_Integration(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	storage := storagemanifest.DispositionManifest{Entities: make([]storagemanifest.EntityDisposition, 0, len(reg.Entities()))}
	for _, e := range reg.Entities() {
		storage.Entities = append(storage.Entities, storagemanifest.EntityDisposition{
			EntityRef: e.Ref.String(), Key: e.Key, Owner: e.OwnerDomain,
			Disposition: storagemanifest.DispositionTable, Target: "table:" + e.Key,
		})
	}
	complete := Check(FromManifests(reg, storage, "registry-and-coverage-contracts.md"))
	if !complete.FullyVerified() {
		t.Fatalf("catalog with complete manifest has gaps: %v", complete.Gaps)
	}
	storage.Entities = storage.Entities[:len(storage.Entities)-1]
	broken := Check(FromManifests(reg, storage, "registry-and-coverage-contracts.md"))
	if broken.FullyVerified() || len(broken.Gaps) == 0 {
		t.Fatal("missing manifest row must fail integration coverage")
	}
	for _, gap := range broken.Gaps {
		if gap.Source != "registry-and-coverage-contracts.md" {
			t.Fatalf("gap source not preserved: %+v", gap)
		}
	}
}

// FuzzTodo_DB_COVERAGE_001 ensures arbitrary caller input cannot panic and
// only a fully explicit supported object can be counted as verified.
func FuzzTodo_DB_COVERAGE_001(f *testing.F) {
	f.Add("ENTITY", "Person", "people.md", "people", "DATABASE", "table:people")
	f.Add("AUTHORITY", "", "", "", "", "")
	f.Fuzz(func(t *testing.T, kind, name, source, owner, disposition, target string) {
		r := Check([]Object{{Kind: Kind(kind), Name: name, Source: source, Owner: owner, Disposition: Disposition(disposition), StorageRef: target}})
		if r.Total != 1 || r.Verified > 1 || r.Verified == 1 && len(r.Gaps) != 0 {
			t.Fatalf("invalid report for fuzz input: %+v", r)
		}
	})
}

// TestTodo_DB_COVERAGE_001_Race exercises concurrent read-only checks and
// verifies that each invocation produces the same digest and counts.
func TestTodo_DB_COVERAGE_001_Race(t *testing.T) {
	objects := []Object{{Kind: Entity, Name: "Person", Source: "people.md", Owner: "people", Disposition: Database, StorageRef: "table people"}}
	want := Check(objects)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got := Check(objects)
			if got.Digest != want.Digest || got.Verified != want.Verified || len(got.Gaps) != len(want.Gaps) {
				t.Errorf("worker %d produced non-deterministic report: %s", i, fmt.Sprint(got))
			}
		}(i)
	}
	wg.Wait()
}
