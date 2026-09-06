package sbom_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/sbom"
)

// TestSBOMCompleteness is TOOL-017's primary test. It generates the real
// SBOM for this repository's own root module, round-trips it through JSON
// exactly as a downstream validator would ("parse the generated
// document"), and checks the three completeness clauses TOOL-017 names:
// every go.mod require appears, the root component is
// github.com/monstercameron/hcm-next, and no component lacks a version.
func TestSBOMCompleteness(t *testing.T) {
	root := repopath.RootDir()

	doc, err := sbom.Generate(root, sbom.Options{})
	if err != nil {
		t.Fatalf("Generate(%s): %v", root, err)
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var parsed sbom.Document
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	t.Run("GREEN: root component is github.com/monstercameron/hcm-next", func(t *testing.T) {
		if parsed.Metadata.Component.Name != sbom.RootModulePath {
			t.Fatalf("root component name = %q, want %q", parsed.Metadata.Component.Name, sbom.RootModulePath)
		}
	})

	t.Run("GREEN: every go.mod require appears as a component at the same version", func(t *testing.T) {
		completeness, err := sbom.ValidateCompleteness(&parsed, root)
		if err != nil {
			t.Fatalf("ValidateCompleteness: %v", err)
		}
		if len(completeness.MissingRequires) != 0 {
			t.Errorf("MissingRequires = %v, want none", completeness.MissingRequires)
		}
	})

	t.Run("GREEN: no component lacks a version", func(t *testing.T) {
		completeness, err := sbom.ValidateCompleteness(&parsed, root)
		if err != nil {
			t.Fatalf("ValidateCompleteness: %v", err)
		}
		if len(completeness.VersionlessRefs) != 0 {
			t.Errorf("VersionlessRefs = %v, want none", completeness.VersionlessRefs)
		}
	})

	t.Run("RED: an artifact missing a required module is rejected", func(t *testing.T) {
		mutated := parsed
		if len(mutated.Components) == 0 {
			t.Fatal("no components to remove from the fixture")
		}
		mutated.Components = mutated.Components[1:] // drop one required module
		completeness, err := sbom.ValidateCompleteness(&mutated, root)
		if err != nil {
			t.Fatalf("ValidateCompleteness: %v", err)
		}
		if completeness.Empty() {
			t.Fatal("expected the mutated document (missing a required module) to fail completeness")
		}
	})

	t.Run("RED: an artifact with a versionless component is rejected", func(t *testing.T) {
		mutated := parsed
		components := make([]sbom.Component, len(parsed.Components))
		copy(components, parsed.Components)
		components[0].Version = ""
		mutated.Components = components
		completeness, err := sbom.ValidateCompleteness(&mutated, root)
		if err != nil {
			t.Fatalf("ValidateCompleteness: %v", err)
		}
		if completeness.Empty() {
			t.Fatal("expected the mutated document (versionless component) to fail completeness")
		}
	})

	if len(parsed.Components) == 0 {
		t.Fatal("Generate produced zero components for the live repository")
	}
}

// TestTodo_TOOL_017_Golden pins the exact JSON shape buildDocument (the
// exec-free core Generate delegates to) emits for a fixed, synthetic input
// set, so an accidental field rename/reorder/tag change is caught without
// depending on this repository's own ever-changing dependency graph.
func TestTodo_TOOL_017_Golden(t *testing.T) {
	requires := []sbom.Require{
		{Path: "example.com/alpha", Version: "v1.2.3"},
		{Path: "example.com/beta", Version: "v0.9.0", Indirect: true},
	}
	hashes := map[string]sbom.SumHash{
		"example.com/alpha@v1.2.3": {Alg: sbom.HashAlgSHA256, Content: "aa"},
	}
	edges := []sbom.GraphEdge{
		{FromPath: "example.com/golden-root", FromVersion: "", ToPath: "example.com/alpha", ToVersion: "v1.2.3"},
		{FromPath: "example.com/alpha", FromVersion: "v1.2.3", ToPath: "example.com/beta", ToVersion: "v0.9.0"},
	}
	opts := sbom.Options{
		RootVersion:      "v1.0.0",
		GeneratorVersion: "golden-test",
		Now:              func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}

	doc := sbom.BuildDocumentForTest("example.com/golden-root", requires, hashes, edges, opts)

	// The serial number is a content hash of the module/version identity
	// (see deterministicSerial's own stability test in
	// generate_internal_test.go); it is not itself part of what this
	// golden test pins, only that it round-trips as a well-formed
	// "urn:uuid:..." string in the expected position.
	if len(doc.SerialNumber) != len("urn:uuid:")+36 {
		t.Fatalf("unexpected serial number shape: %q", doc.SerialNumber)
	}

	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}

	want := `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.5",
  "serialNumber": "` + doc.SerialNumber + `",
  "version": 1,
  "metadata": {
    "timestamp": "2026-01-02T03:04:05Z",
    "tools": [
      {
        "vendor": "github.com/monstercameron/hcm-next",
        "name": "hcm-next-sbomgen",
        "version": "golden-test"
      }
    ],
    "component": {
      "bom-ref": "pkg:golang/example.com/golden-root@v1.0.0",
      "type": "application",
      "name": "example.com/golden-root",
      "version": "v1.0.0",
      "purl": "pkg:golang/example.com/golden-root@v1.0.0"
    }
  },
  "components": [
    {
      "bom-ref": "pkg:golang/example.com/alpha@v1.2.3",
      "type": "library",
      "name": "example.com/alpha",
      "version": "v1.2.3",
      "purl": "pkg:golang/example.com/alpha@v1.2.3",
      "scope": "required",
      "hashes": [
        {
          "alg": "SHA-256",
          "content": "aa"
        }
      ]
    },
    {
      "bom-ref": "pkg:golang/example.com/beta@v0.9.0",
      "type": "library",
      "name": "example.com/beta",
      "version": "v0.9.0",
      "purl": "pkg:golang/example.com/beta@v0.9.0",
      "scope": "optional"
    }
  ],
  "dependencies": [
    {
      "ref": "pkg:golang/example.com/alpha@v1.2.3",
      "dependsOn": [
        "pkg:golang/example.com/beta@v0.9.0"
      ]
    },
    {
      "ref": "pkg:golang/example.com/beta@v0.9.0"
    },
    {
      "ref": "pkg:golang/example.com/golden-root@v1.0.0",
      "dependsOn": [
        "pkg:golang/example.com/alpha@v1.2.3"
      ]
    }
  ]
}`
	if string(got) != want {
		t.Fatalf("golden mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
