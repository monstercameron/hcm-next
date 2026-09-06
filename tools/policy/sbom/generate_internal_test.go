package sbom

import (
	"testing"
	"time"
)

func fixedClock(ts time.Time) func() time.Time {
	return func() time.Time { return ts }
}

// TestBuildDocument_Shape proves buildDocument's pure assembly: components
// come from requires (deduped nothing to dedupe here, sorted by name),
// hashes attach only when go.sum has one, scope reflects the indirect
// flag, and the root component/metadata carry the requested version.
func TestBuildDocument_Shape(t *testing.T) {
	requires := []Require{
		{Path: "example.com/b", Version: "v2.0.0", Indirect: true},
		{Path: "example.com/a", Version: "v1.0.0"},
	}
	hashes := map[string]SumHash{
		"example.com/a@v1.0.0": {Alg: HashAlgSHA256, Content: "deadbeef"},
	}
	opts := Options{RootVersion: "v0.0.0-test", GeneratorVersion: "1.0.0", Now: fixedClock(time.Unix(0, 0).UTC())}

	doc := buildDocument("example.com/root", requires, hashes, nil, opts)

	if doc.Metadata.Component.Name != "example.com/root" || doc.Metadata.Component.Version != "v0.0.0-test" {
		t.Fatalf("root component = %+v", doc.Metadata.Component)
	}
	if doc.Metadata.Timestamp != "1970-01-01T00:00:00Z" {
		t.Errorf("Timestamp = %q", doc.Metadata.Timestamp)
	}
	if len(doc.Components) != 2 {
		t.Fatalf("Components = %d entries, want 2: %+v", len(doc.Components), doc.Components)
	}
	// sorted by name: a before b
	if doc.Components[0].Name != "example.com/a" || doc.Components[1].Name != "example.com/b" {
		t.Fatalf("Components not sorted by name: %+v", doc.Components)
	}
	if len(doc.Components[0].Hashes) != 1 || doc.Components[0].Hashes[0].Content != "deadbeef" {
		t.Errorf("Components[0].Hashes = %+v, want the go.sum hash attached", doc.Components[0].Hashes)
	}
	if len(doc.Components[1].Hashes) != 0 {
		t.Errorf("Components[1].Hashes = %+v, want none (no go.sum entry)", doc.Components[1].Hashes)
	}
	if doc.Components[0].Scope != ScopeRequired {
		t.Errorf("direct require scope = %q, want %q", doc.Components[0].Scope, ScopeRequired)
	}
	if doc.Components[1].Scope != ScopeOptional {
		t.Errorf("indirect require scope = %q, want %q", doc.Components[1].Scope, ScopeOptional)
	}
}

// TestBuildDependencies_FiltersSupersededVersions is the mechanism this
// package exists to get right: `go mod graph` can name a version of a
// module that minimal version selection did not actually pick, and that
// edge must not appear in the emitted graph.
func TestBuildDependencies_FiltersSupersededVersions(t *testing.T) {
	selected := map[string]string{
		"example.com/root": "",
		"example.com/dep":  "v2.0.0", // MVS picked v2.0.0
	}
	edges := []GraphEdge{
		{FromPath: "example.com/root", FromVersion: "", ToPath: "example.com/dep", ToVersion: "v1.0.0"}, // superseded, must be dropped
		{FromPath: "example.com/root", FromVersion: "", ToPath: "example.com/dep", ToVersion: "v2.0.0"}, // selected, must be kept
	}
	rootRef := purl("example.com/root", "v0.0.0")
	deps := buildDependencies("example.com/root", rootRef, selected, edges)

	var rootDep *Dependency
	for i := range deps {
		if deps[i].Ref == rootRef {
			rootDep = &deps[i]
		}
	}
	if rootDep == nil {
		t.Fatalf("no Dependency entry for the root ref among %+v", deps)
	}
	wantEdge := purl("example.com/dep", "v2.0.0")
	badEdge := purl("example.com/dep", "v1.0.0")
	foundGood, foundBad := false, false
	for _, to := range rootDep.DependsOn {
		if to == wantEdge {
			foundGood = true
		}
		if to == badEdge {
			foundBad = true
		}
	}
	if !foundGood {
		t.Errorf("root DependsOn = %v, want to contain the selected version %s", rootDep.DependsOn, wantEdge)
	}
	if foundBad {
		t.Errorf("root DependsOn = %v, must not contain the superseded version %s", rootDep.DependsOn, badEdge)
	}
}

// TestBuildDependencies_EveryComponentGetsAnEntry proves a component with
// no outbound edges still appears in Dependencies (with no DependsOn), so
// every known ref is queryable.
func TestBuildDependencies_EveryComponentGetsAnEntry(t *testing.T) {
	selected := map[string]string{
		"example.com/root":    "",
		"example.com/leafdep": "v1.0.0",
	}
	rootRef := purl("example.com/root", "v0.0.0")
	deps := buildDependencies("example.com/root", rootRef, selected, nil)

	leafRef := purl("example.com/leafdep", "v1.0.0")
	found := false
	for _, d := range deps {
		if d.Ref == leafRef {
			found = true
			if len(d.DependsOn) != 0 {
				t.Errorf("leaf dependency %s has DependsOn = %v, want none", leafRef, d.DependsOn)
			}
		}
	}
	if !found {
		t.Errorf("Dependencies %+v missing an entry for %s", deps, leafRef)
	}
}
