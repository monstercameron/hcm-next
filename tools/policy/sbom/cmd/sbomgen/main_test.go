package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/sbom"
)

// TestRun_WritesFile proves the CLI's file-output path: it generates a
// complete SBOM for the real repository root and writes valid, complete
// CycloneDX JSON to the path -out names.
func TestRun_WritesFile(t *testing.T) {
	root := repopath.RootDir()
	outPath := filepath.Join(t.TempDir(), "sbom.cdx.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-out", outPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr=%s", code, stderr.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading %s: %v", outPath, err)
	}
	var doc sbom.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshaling generated SBOM: %v", err)
	}
	if doc.Metadata.Component.Name != sbom.RootModulePath {
		t.Errorf("root component = %q, want %q", doc.Metadata.Component.Name, sbom.RootModulePath)
	}
	if len(doc.Components) == 0 {
		t.Error("generated SBOM has zero components")
	}
}

// TestRun_WritesStdout proves the default (-out unset) path writes the
// document to stdout instead of a file.
func TestRun_WritesStdout(t *testing.T) {
	root := repopath.RootDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr=%s", code, stderr.String())
	}

	var doc sbom.Document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshaling stdout as SBOM JSON: %v", err)
	}
	if doc.Metadata.Component.Name != sbom.RootModulePath {
		t.Errorf("root component = %q, want %q", doc.Metadata.Component.Name, sbom.RootModulePath)
	}
}

// TestRun_VersionOverride proves -version reaches the root component.
func TestRun_VersionOverride(t *testing.T) {
	root := repopath.RootDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-version", "v9.9.9-test"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr=%s", code, stderr.String())
	}
	var doc sbom.Document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshaling stdout: %v", err)
	}
	if doc.Metadata.Component.Version != "v9.9.9-test" {
		t.Errorf("root component version = %q, want v9.9.9-test", doc.Metadata.Component.Version)
	}
}

// TestRun_BadRoot proves a nonexistent -root fails cleanly (nonzero exit,
// no panic).
func TestRun_BadRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", filepath.Join(t.TempDir(), "does-not-exist")}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("run() = 0 for a nonexistent root, want nonzero")
	}
	if stderr.Len() == 0 {
		t.Error("expected a diagnostic on stderr for a bad root")
	}
}

// TestRun_BadFlag proves flag-parse failures return the flag package's own
// exit convention without panicking.
func TestRun_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-not-a-real-flag"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("run() = %d, want 2 for an unrecognized flag", code)
	}
}

// TestRun_UnwritableOut proves a write failure (path under a nonexistent
// directory) is reported, not panicked.
func TestRun_UnwritableOut(t *testing.T) {
	root := repopath.RootDir()
	badOut := filepath.Join(t.TempDir(), "no-such-dir", "sbom.cdx.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-out", badOut}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("run() = 0 for an unwritable -out path, want nonzero")
	}
}
