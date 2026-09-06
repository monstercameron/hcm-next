package sbom_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/sbom"
)

const fixtureGoMod = `module example.com/fixture

go 1.26.3

require (
	example.com/direct v1.2.3
	example.com/directalso v0.1.0
)

require (
	example.com/indirect v1.5.6 // indirect
)
`

func writeFixtureModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing fixture %s: %v", name, err)
		}
	}
	return dir
}

func TestModulePath(t *testing.T) {
	dir := writeFixtureModule(t, map[string]string{"go.mod": fixtureGoMod})
	got, err := sbom.ModulePath(dir)
	if err != nil {
		t.Fatalf("ModulePath: %v", err)
	}
	if got != "example.com/fixture" {
		t.Errorf("ModulePath = %q, want example.com/fixture", got)
	}
}

func TestModulePath_MissingGoMod(t *testing.T) {
	dir := t.TempDir()
	if _, err := sbom.ModulePath(dir); err == nil {
		t.Fatal("ModulePath: expected error for missing go.mod, got nil")
	}
}

func TestModulePath_RejectsGoModWithoutModuleDirective(t *testing.T) {
	dir := writeFixtureModule(t, map[string]string{"go.mod": "go 1.26.3\n"})
	if _, err := sbom.ModulePath(dir); err == nil {
		t.Fatal("ModulePath accepted go.mod without a module directive")
	}
}

func TestParseRequires_MissingGoMod(t *testing.T) {
	if _, err := sbom.ParseRequires(t.TempDir()); err == nil {
		t.Fatal("ParseRequires accepted a directory without go.mod")
	}
}

func TestParseRequires(t *testing.T) {
	dir := writeFixtureModule(t, map[string]string{"go.mod": fixtureGoMod})
	requires, err := sbom.ParseRequires(dir)
	if err != nil {
		t.Fatalf("ParseRequires: %v", err)
	}
	want := map[string]sbom.Require{
		"example.com/direct":     {Path: "example.com/direct", Version: "v1.2.3", Indirect: false},
		"example.com/directalso": {Path: "example.com/directalso", Version: "v0.1.0", Indirect: false},
		"example.com/indirect":   {Path: "example.com/indirect", Version: "v1.5.6", Indirect: true},
	}
	if len(requires) != len(want) {
		t.Fatalf("ParseRequires returned %d entries, want %d: %+v", len(requires), len(want), requires)
	}
	for i, r := range requires {
		exp, ok := want[r.Path]
		if !ok {
			t.Errorf("unexpected require %q", r.Path)
			continue
		}
		if r != exp {
			t.Errorf("require %q = %+v, want %+v", r.Path, r, exp)
		}
		if i > 0 && requires[i-1].Path > r.Path {
			t.Errorf("ParseRequires not sorted by path: %q before %q", requires[i-1].Path, r.Path)
		}
	}
}

func TestParseRequires_MalformedGoMod(t *testing.T) {
	dir := writeFixtureModule(t, map[string]string{"go.mod": "not a go.mod file {{{"})
	if _, err := sbom.ParseRequires(dir); err == nil {
		t.Fatal("ParseRequires: expected error for malformed go.mod, got nil")
	}
}
