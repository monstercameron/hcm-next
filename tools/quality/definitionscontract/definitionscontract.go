// Package definitionscontract validates the repository definitions tree.
//
// Definitions are source-of-truth inputs.  This package is intentionally a
// read-only checker: it only opens files beneath the supplied definitions
// directory and never writes planning, generated, runtime, module, or
// definitions files.  Callers can therefore run it from a pre-commit hook or
// a build without risking a source-of-truth mutation.
package definitionscontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Report is the deterministic result of checking a definitions directory.
// Files are relative to Root, use slash separators, and are sorted.
type Report struct {
	Root  string
	Files []string
}

// Check validates root and returns a deterministic report. It only reads
// regular files with the supported definition extensions (.yaml, .yml,
// .json, and .md). Symbolic links and executable/source files are rejected so
// generated or runtime material cannot silently become definitions.
func Check(root string) (Report, error) {
	if strings.TrimSpace(root) == "" {
		return Report{}, errors.New("definitionscontract: definitions directory is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		return Report{}, fmt.Errorf("definitionscontract: stat definitions directory: %w", err)
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("definitionscontract: %q is not a directory", root)
	}

	root, err = filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("definitionscontract: resolve root: %w", err)
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("definitionscontract: symbolic link is not a definition: %s", rel(root, path))
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("definitionscontract: non-regular file is not a definition: %s", rel(root, path))
		}
		r := rel(root, path)
		if !supported(filepath.Ext(path)) {
			return fmt.Errorf("definitionscontract: unsupported file %q (definitions must be yaml, yml, json, or md)", r)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("definitionscontract: read %s: %w", r, readErr)
		}
		if parseErr := parse(filepath.Ext(path), data); parseErr != nil {
			return fmt.Errorf("definitionscontract: %s: %w", r, parseErr)
		}
		files = append(files, r)
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	sort.Strings(files)
	return Report{Root: root, Files: files}, nil
}

// Validate is the error-only form of Check.
func Validate(root string) error {
	_, err := Check(root)
	return err
}

// CheckDir is an alias for Validate, useful at call sites where the argument
// is visibly a directory rather than a single definition file.
func CheckDir(root string) error { return Validate(root) }

// Files returns the sorted relative definition paths beneath root. It is a
// convenience for tooling that needs the inventory while applying the same
// validation and read-only guarantees as Check.
func Files(root string) ([]string, error) {
	report, err := Check(root)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), report.Files...), nil
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(r)
}

func supported(ext string) bool {
	switch strings.ToLower(ext) {
	case ".yaml", ".yml", ".json", ".md":
		return true
	default:
		return false
	}
}

func parse(ext string, data []byte) error {
	switch strings.ToLower(ext) {
	case ".json":
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
	case ".yaml", ".yml":
		var value any
		if err := yaml.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("invalid YAML: %w", err)
		}
	}
	return nil
}
