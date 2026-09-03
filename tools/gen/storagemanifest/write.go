package storagemanifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// RepoRoot walks up from dir looking for go.mod, the same way
// tools/quality/main.go locates the repository root, so this package's tests
// and any future `go run` entry point behave the same regardless of the
// working directory they are invoked from.
func RepoRoot(dir string) (string, error) {
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("storagemanifest: no go.mod found walking up from %s", dir)
		}
		dir = parent
	}
}

// RenderYAML renders v deterministically: every manifest type in this
// package is built entirely from sorted slices, never a map, so gopkg.in/
// yaml.v3's field-order-preserving encoding is byte-identical for two
// structurally identical values.
func RenderYAML(v any) ([]byte, error) {
	var buf []byte
	enc := yaml.NewEncoder(sliceWriter{&buf})
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf, nil
}

type sliceWriter struct{ buf *[]byte }

func (w sliceWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

// Generated is the full set of DB-002/003/004 manifests built from one
// compiled registry and one scanned migration inventory.
type Generated struct {
	Disposition DispositionManifest
	Properties  PropertyMappingManifest
	Constraints ConstraintManifest
}

// BuildAll compiles the model catalog, scans migrationsDir, and builds all
// three manifests. It is a pure function of the repository's own source: the
// registry catalog is compiled-in Go, and the migration scan reads immutable
// checked-in files, so two calls against the same source tree always agree.
func BuildAll(migrationsDir string) (Generated, error) {
	reg, err := model.Catalog()
	if err != nil {
		return Generated{}, fmt.Errorf("storagemanifest: compile catalog: %w", err)
	}
	inv, err := ScanMigrations(migrationsDir)
	if err != nil {
		return Generated{}, fmt.Errorf("storagemanifest: scan migrations: %w", err)
	}
	disp, err := BuildDispositionManifest(reg, inv)
	if err != nil {
		return Generated{}, fmt.Errorf("storagemanifest: build disposition manifest: %w", err)
	}
	props, err := BuildPropertyMappings(reg)
	if err != nil {
		return Generated{}, fmt.Errorf("storagemanifest: build property mappings: %w", err)
	}
	cons, err := BuildConstraints(reg, inv)
	if err != nil {
		return Generated{}, fmt.Errorf("storagemanifest: build constraints: %w", err)
	}
	return Generated{Disposition: disp, Properties: props, Constraints: cons}, nil
}

// Manifest file names under definitions/model/.
const (
	FileDisposition = "storage-disposition.yaml"
	FileProperties  = "property-sql-mappings.yaml"
	FileConstraints = "relationship-lifecycle-constraints.yaml"
)

// WriteAll renders and writes all three manifests into
// <repoRoot>/definitions/model/. It is the one place this package touches
// disk for output; every other function is pure.
func WriteAll(repoRoot string, gen Generated) error {
	dir := filepath.Join(repoRoot, "definitions", "model")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	writes := []struct {
		name string
		v    any
	}{
		{FileDisposition, gen.Disposition},
		{FileProperties, gen.Properties},
		{FileConstraints, gen.Constraints},
	}
	for _, w := range writes {
		data, err := RenderYAML(w.v)
		if err != nil {
			return fmt.Errorf("storagemanifest: render %s: %w", w.name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, w.name), data, 0o644); err != nil {
			return fmt.Errorf("storagemanifest: write %s: %w", w.name, err)
		}
	}
	return nil
}
