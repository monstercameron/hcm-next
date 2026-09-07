//go:build !js || !wasm

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodo_WEB_031_Conformance(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	approvedAdapter := filepath.Join(root, "tools", "uxqual", "cmd", "journeywasm", "browser_history_wasm.go")
	approvedContract := filepath.Join(root, "tools", "uxqual", "productclient", "browser_state.go")
	seamCount := 0
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(body)
		for _, forbidden := range []string{"localStorage", "indexedDB", "caches.", "cacheStorage"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s directly references forbidden browser storage API %q", filepath.ToSlash(path), forbidden)
			}
		}
		if strings.Contains(text, `Get("sessionStorage")`) {
			if path != approvedAdapter {
				t.Errorf("%s directly acquires sessionStorage outside the approved adapter", filepath.ToSlash(path))
			} else {
				seamCount++
			}
		}
		if strings.Contains(text, "sessionStorage") && path != approvedAdapter && path != approvedContract {
			t.Errorf("%s references sessionStorage outside the approved adapter/contract", filepath.ToSlash(path))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if seamCount != 1 {
		t.Fatalf("approved adapter has %d sessionStorage property seams, want exactly one", seamCount)
	}
}

func repositoryRoot() (string, error) {
	path, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
			return path, nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", os.ErrNotExist
		}
		path = parent
	}
}
