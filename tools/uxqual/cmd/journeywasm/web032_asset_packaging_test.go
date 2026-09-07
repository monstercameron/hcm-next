//go:build !(js && wasm)

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
)

func TestAssetIntegrityPackagingMatchesRoutableCatalog(t *testing.T) {
	dir := t.TempDir()
	shim := []byte("globalThis.Go=class {};")
	if err := os.WriteFile(filepath.Join(dir, wasmExecFile), shim, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "person-priya.png"), []byte("private original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAssetIntegrityManifest(dir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, workspace.AssetIntegrityManifestName))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := workspace.ParseAssetIntegrityManifest(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.VerifyAsset("/workspace/assets/"+wasmExecFile, "identity", shim); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Assets) != 1 {
		t.Fatalf("non-routable source original entered manifest: %+v", manifest.Assets)
	}
	if err := os.WriteFile(filepath.Join(dir, workspace.AssetIntegrityManifestName), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAssetIntegrityManifest(dir); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, workspace.AssetIntegrityManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("asset manifest output is not reproducible")
	}
}

func TestAssetIntegrityPackagingRejectsOrphanedCompression(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, wasmExecFile), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orphan.js.gz"), []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAssetIntegrityManifest(dir); err == nil {
		t.Fatal("orphaned compression was silently ignored")
	}
	if _, err := os.Stat(filepath.Join(dir, workspace.AssetIntegrityManifestName)); !os.IsNotExist(err) {
		t.Fatalf("faulting generation published a manifest: %v", err)
	}
}
