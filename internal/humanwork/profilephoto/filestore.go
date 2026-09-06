package profilephoto

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FileStore is the build-time sink used by the demo asset pipeline. Originals
// are retained below a private subdirectory; only flat proxy names are exposed
// by the workspace asset allowlist.
type FileStore struct {
	// Root is the public proxy root. OriginalRoot may point at a separate,
	// non-embedded store; when empty, tests and small callers retain originals
	// below Root/profile-originals for backward-compatible convenience.
	Root         string
	OriginalRoot string
}

func (s FileStore) PutOriginal(_ context.Context, name string, content []byte) (string, error) {
	if !safeFilename(name) {
		return "", fmt.Errorf("unsafe original filename %q", name)
	}
	originalRoot := s.OriginalRoot
	if originalRoot == "" {
		originalRoot = filepath.Join(s.Root, "profile-originals")
	}
	if err := atomicWrite(filepath.Join(originalRoot, name), content); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("profile-originals", name)), nil
}

func (s FileStore) PutProxy(_ context.Context, name string, content []byte) (string, error) {
	if !safeFilename(name) {
		return "", fmt.Errorf("unsafe proxy filename %q", name)
	}
	if err := atomicWrite(filepath.Join(s.Root, name), content); err != nil {
		return "", err
	}
	return "/workspace/assets/" + name, nil
}

func atomicWrite(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create asset directory: %w", err)
	}
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, content) {
			return nil
		}
		return fmt.Errorf("publish asset: %s already exists with different content", filepath.Base(path))
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect existing asset: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".profile-photo-*")
	if err != nil {
		return fmt.Errorf("create temporary asset: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary asset: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary asset: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary asset: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("publish asset: %w", err)
	}
	return nil
}
