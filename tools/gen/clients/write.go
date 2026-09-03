package clientsgen

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultOutputDir is where [WriteAll] commits the generated client
// package, relative to the repository root.
const DefaultOutputDir = "internal/transport/clients"

// WriteAll renders the generated client package and writes it under
// filepath.Join(repoRoot, DefaultOutputDir), replacing any file this
// generator owns. It does not remove files it did not just render, so a
// stray hand-added file in that directory is a review question, not a
// silent deletion.
func WriteAll(repoRoot string) error {
	manifestPath := filepath.Join(repoRoot, DefaultManifestPath)
	files, err := Render(manifestPath)
	if err != nil {
		return err
	}
	outDir := filepath.Join(repoRoot, DefaultOutputDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("clientsgen: creating %s: %w", outDir, err)
	}
	for _, name := range sortedFileNames(files) {
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return fmt.Errorf("clientsgen: writing %s: %w", path, err)
		}
	}
	return nil
}
