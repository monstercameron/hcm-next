package deferredschema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePreviewSetRoundTrips(t *testing.T) {
	set, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := WritePreviewSet(dir, set); err != nil {
		t.Fatalf("WritePreviewSet: %v", err)
	}
	roundTripped, err := ReadPreviewSet(dir)
	if err != nil {
		t.Fatalf("ReadPreviewSet: %v", err)
	}
	if roundTripped.Digest() != set.Digest() {
		t.Fatal("WritePreviewSet/ReadPreviewSet did not round-trip byte-identically")
	}
}

func TestWritePreviewSetCreatesMissingDir(t *testing.T) {
	set, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "nested", "preview")
	if err := WritePreviewSet(dir, set); err != nil {
		t.Fatalf("WritePreviewSet: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected dir to be created: %v", err)
	}
}

func TestReadPreviewSetMissingDirErrors(t *testing.T) {
	_, err := ReadPreviewSet(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error reading a missing directory")
	}
}
