package main

import (
	"path/filepath"
	"testing"
)

// TestTodo_SUPPLY_001_GeneratorDefaults pins the no-argument release
// evidence paths: the generator uses the existing gateevidence development
// key convention and writes the checked-in provenance document under the
// repository root.
func TestTodo_SUPPLY_001_GeneratorDefaults(t *testing.T) {
	if defaultKeyPath != "tools/planning/gateevidence/testdata/dev-signing-key.yaml" {
		t.Fatalf("defaultKeyPath = %q", defaultKeyPath)
	}
	if defaultOutputPath != "definitions/supply-chain/provenance.json" {
		t.Fatalf("defaultOutputPath = %q", defaultOutputPath)
	}
	root := t.TempDir()
	if got, want := rootedPath(root, defaultOutputPath), filepath.Join(root, "definitions", "supply-chain", "provenance.json"); got != want {
		t.Errorf("rootedPath output = %q, want %q", got, want)
	}
	absolute := filepath.Join(root, "absolute.json")
	if got := rootedPath(root, absolute); got != absolute {
		t.Errorf("rootedPath absolute = %q, want %q", got, absolute)
	}
}
