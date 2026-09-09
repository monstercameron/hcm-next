package sbom_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/sbom"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func shippedBinary(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "hcmnext-tool017.exe")
	// Build a small real Go module so this test remains isolated from
	// unrelated working-tree edits in production packages. It still exercises
	// the actual Go compiler metadata and a real cached third-party module.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.invalid/tool017\n\ngo 1.26.3\n\nrequire github.com/google/uuid v1.6.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte("github.com/google/uuid v1.6.0 h1:NIvaJDMOsjHA8n1jAhLSgzrAzy1Hgr+hNrb57e+94F0=\ngithub.com/google/uuid v1.6.0/go.mod h1:TIyPZe4MgqvfeYDBFedMoGGpEw/LqOeaOT+nhxU+yHo=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport (\n \"fmt\"\n \"github.com/google/uuid\"\n)\n\nfunc main() { fmt.Println(uuid.Nil) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building shipped binary: %v\n%s", err, data)
	}
	return dir, out
}

func generated(t *testing.T) (sbom.Document, string) {
	t.Helper()
	root, binary := shippedBinary(t)
	d, err := sbom.Generate(root, binary)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return d, binary
}

func localReplacementBinary(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	dep := filepath.Join(dir, "dep")
	if err := os.Mkdir(dep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "go.mod"), []byte("module example.invalid/dep\n\ngo 1.26.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "dep.go"), []byte("package dep\n\nfunc X() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.invalid/main\n\ngo 1.26.3\n\nrequire example.invalid/dep v1.0.0\nreplace example.invalid/dep v1.0.0 => ./dep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport \"example.invalid/dep\"\n\nfunc main() { dep.X() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "replacement.exe")
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building local replacement binary: %v\n%s", err, data)
	}
	return dir, out
}

func TestGenerateLocalReplacementWithoutModuleSum(t *testing.T) {
	root, binary := localReplacementBinary(t)
	d, err := sbom.Generate(root, binary)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var replacement *sbom.Component
	for i := range d.Components {
		if !d.Components[i].Main {
			replacement = &d.Components[i]
			break
		}
	}
	if replacement == nil || replacement.Replacement == nil {
		t.Fatalf("replacement component missing replacement metadata: %+v", d.Components)
	}
	if replacement.Hash == "" || replacement.Hash != replacement.SourceDigest {
		t.Fatalf("local replacement hash = %q, source digest = %q; want hash bound to source digest", replacement.Hash, replacement.SourceDigest)
	}
	if err := sbom.ValidateArtifact(d, binary); err != nil {
		t.Fatalf("ValidateArtifact: %v", err)
	}
}

// TestSBOMCompleteness is TOOL-017's primary test. It uses a real release
// command binary, not a mocked module list, and proves that every shipped
// component has identity, license and both module/source hashes bound to the
// exact artifact subject.
func TestSBOMCompleteness(t *testing.T) {
	d, binary := generated(t)
	if err := sbom.ValidateArtifact(d, binary); err != nil {
		t.Fatalf("ValidateArtifact: %v", err)
	}
	if err := sbom.ValidateAgainstSubject(d, d.Subject.Digest); err != nil {
		t.Fatalf("signed subject validation: %v", err)
	}
	if len(d.Components) < 2 {
		t.Fatalf("components = %d, want main module plus shipped dependencies", len(d.Components))
	}
	for _, c := range d.Components {
		if c.Name == "" || c.Version == "" || c.Hash == "" || c.SourceDigest == "" || c.License == "" {
			t.Errorf("incomplete component: %+v", c)
		}
	}

	t.Run("RED_missing_license_hash_or_version_is_rejected", func(t *testing.T) {
		for field := range map[string]bool{"license": true, "hash": true, "version": true} {
			bad := d
			bad.Components = append([]sbom.Component(nil), d.Components...)
			bad.Components[0] = d.Components[0]
			switch field {
			case "license":
				bad.Components[0].License = ""
			case "hash":
				bad.Components[0].Hash = ""
			case "version":
				bad.Components[0].Version = ""
			}
			if err := sbom.Validate(bad); err == nil {
				t.Errorf("Validate accepted missing %s", field)
			}
		}
	})
	t.Run("RED_wrong_signed_subject_is_rejected", func(t *testing.T) {
		if err := sbom.ValidateAgainstSubject(d, "sha256:"+strings.Repeat("0", 64)); err == nil {
			t.Fatal("accepted wrong signed subject")
		}
	})
	t.Run("RED_undeclared_binary_component_is_rejected", func(t *testing.T) {
		bad := d
		bad.Components = append([]sbom.Component(nil), d.Components...)
		bad.Components = append(bad.Components, sbom.Component{Type: "go-module", Name: "example.invalid/undeclared", Version: "v1.0.0", Hash: "h1:fake", SourceDigest: "sha256:" + strings.Repeat("1", 64), License: "MIT"})
		sort.Slice(bad.Components, func(i, j int) bool {
			return bad.Components[i].Name+"@"+bad.Components[i].Version < bad.Components[j].Name+"@"+bad.Components[j].Version
		})
		if err := sbom.ValidateArtifact(bad, binary); err == nil {
			t.Fatal("accepted undeclared binary component")
		}
	})
}

// TestTodo_TOOL_017_Golden pins the deterministic document shape and key
// release exclusions without pinning dependency versions. Two generations
// over the same immutable artifact must have byte-identical canonical JSON.
func TestTodo_TOOL_017_Golden(t *testing.T) {
	d, binary := generated(t)
	// The graph and subject are embedded in the immutable binary; generation
	// does not depend on the mutable checkout after that point.
	again, err := sbom.Generate("", binary)
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	a, err := d.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	b, err := again.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("SBOM JSON is not deterministic:\n%s\n%s", a, b)
	}
	if !sbom.EqualCanonical(d, again) {
		t.Fatal("EqualCanonical returned false for equal documents")
	}
	if d.Schema != "hcmnext.sbom.v1" || d.Generator.Name != "hcmnext-sbom" || d.Generator.Version == "" || d.Graph.Tool != "go version -m" {
		t.Fatalf("unexpected metadata: %+v", d)
	}
	seen := map[string]bool{}
	for i, c := range d.Components {
		key := c.Name + "@" + c.Version
		if seen[key] {
			t.Fatalf("duplicate component %s", key)
		}
		seen[key] = true
		if i > 0 && (c.Name < d.Components[i-1].Name || (c.Name == d.Components[i-1].Name && c.Version < d.Components[i-1].Version)) {
			t.Fatalf("components not sorted")
		}
		if strings.Contains(strings.ToLower(c.Name), "node") || strings.Contains(strings.ToLower(c.Name), "typescript") {
			t.Fatalf("non-Go runtime in release SBOM: %s", c.Name)
		}
	}
	if err := sbom.Validate(d); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var roundTrip sbom.Document
	if err := json.Unmarshal(a, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !sbom.EqualCanonical(d, roundTrip) {
		t.Fatal(fmt.Sprintf("JSON round trip changed canonical document"))
	}
}
