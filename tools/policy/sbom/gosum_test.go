package sbom_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/sbom"
)

func TestParseGoSum(t *testing.T) {
	contentDigest := bytes.Repeat([]byte{0x01}, 32)
	goModDigest := bytes.Repeat([]byte{0x02}, 32)
	contentSum := "h1:" + base64.StdEncoding.EncodeToString(contentDigest)
	goModSum := "h1:" + base64.StdEncoding.EncodeToString(goModDigest)

	dir := t.TempDir()
	goSum := "example.com/mod v1.2.3 " + contentSum + "\n" +
		"example.com/mod v1.2.3/go.mod " + goModSum + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte(goSum), 0o644); err != nil {
		t.Fatalf("writing fixture go.sum: %v", err)
	}

	hashes, err := sbom.ParseGoSum(dir)
	if err != nil {
		t.Fatalf("ParseGoSum: %v", err)
	}

	got, ok := hashes["example.com/mod@v1.2.3"]
	if !ok {
		t.Fatalf("ParseGoSum: no hash recorded for example.com/mod@v1.2.3, got %+v", hashes)
	}
	if got.Alg != sbom.HashAlgSHA256 {
		t.Errorf("Alg = %q, want %q", got.Alg, sbom.HashAlgSHA256)
	}
	wantContent := hex.EncodeToString(contentDigest)
	if got.Content != wantContent {
		t.Errorf("Content = %q, want %q (the module hash, not the go.mod hash)", got.Content, wantContent)
	}
	if len(hashes) != 1 {
		t.Errorf("ParseGoSum returned %d entries, want 1 (the /go.mod line must not produce its own entry)", len(hashes))
	}
}

func TestParseGoSum_MissingFile(t *testing.T) {
	dir := t.TempDir()
	hashes, err := sbom.ParseGoSum(dir)
	if err != nil {
		t.Fatalf("ParseGoSum: unexpected error for missing go.sum: %v", err)
	}
	if len(hashes) != 0 {
		t.Errorf("expected empty map for missing go.sum, got %+v", hashes)
	}
}

func TestParseGoSum_MalformedLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte("only.one.field\n"), 0o644); err != nil {
		t.Fatalf("writing fixture go.sum: %v", err)
	}
	if _, err := sbom.ParseGoSum(dir); err == nil {
		t.Fatal("ParseGoSum: expected error for malformed line, got nil")
	}
}

func TestParseGoSumSkipsUnknownSchemesAndRejectsBadBase64(t *testing.T) {
	dir := t.TempDir()
	content := "example.com/mod v1.0.0 sha256:unknown\n"
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing unknown-scheme fixture: %v", err)
	}
	hashes, err := sbom.ParseGoSum(dir)
	if err != nil {
		t.Fatalf("ParseGoSum(unknown scheme): %v", err)
	}
	if len(hashes) != 0 {
		t.Fatalf("unknown scheme produced hashes: %+v", hashes)
	}

	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte("example.com/mod v1.0.0 h1:not-base64!\n"), 0o644); err != nil {
		t.Fatalf("writing invalid-base64 fixture: %v", err)
	}
	if _, err := sbom.ParseGoSum(dir); err == nil {
		t.Fatal("ParseGoSum accepted invalid base64")
	}
}
