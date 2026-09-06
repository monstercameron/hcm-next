package deferredschema

import (
	"fmt"
	"os"
	"path/filepath"
)

// WritePreviewSet writes every file in p to dir, creating dir if needed. It
// is used once to materialize testdata/preview and by generate_test.go's
// golden test (against a throwaway t.TempDir(), never against the checked-in
// testdata/preview itself) to prove Generate's output round-trips byte for
// byte through the filesystem.
func WritePreviewSet(dir string, p PreviewSet) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("deferredschema: create %s: %w", dir, err)
	}
	for _, f := range p.Files {
		path := filepath.Join(dir, f.Name)
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("deferredschema: write %s: %w", path, err)
		}
	}
	return nil
}

// ReadPreviewSet reads every *.sql and the storage-disposition.deferred.yaml
// file out of dir, in Generate's own sorted-by-name order, so it can be
// compared directly against a fresh Generate() call.
func ReadPreviewSet(dir string) (PreviewSet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return PreviewSet{}, fmt.Errorf("deferredschema: read %s: %w", dir, err)
	}
	var files []PreviewFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return PreviewSet{}, fmt.Errorf("deferredschema: read %s: %w", e.Name(), err)
		}
		files = append(files, PreviewFile{Name: e.Name(), Content: string(data)})
	}
	return PreviewSet{Files: files}, nil
}
