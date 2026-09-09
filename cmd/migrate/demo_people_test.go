package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

func TestIngestDemoPhotosUsesPrivateOriginalsAndPublicProxies(t *testing.T) {
	t.Parallel()

	sourceDir, assetDir, originalDir := t.TempDir(), t.TempDir(), t.TempDir()
	content := demoPNG(t)
	if err := os.WriteFile(filepath.Join(sourceDir, "hc-001.png"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	employees := []demoworkforce.Employee{
		{Row: workforce.WorkerRow{WorkerKey: "hc-001-test"}, HasProfilePhoto: true, PhotoSourceName: "hc-001.png", PhotoOriginalRef: "profile-originals/hc-001.png", PhotoProxyRef: "/workspace/assets/person-hc-001-small.jpg"},
		{HasProfilePhoto: false},
	}
	if err := ingestDemoPhotos(context.Background(), employees, sourceDir, assetDir, originalDir); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(filepath.Join(originalDir, "hc-001.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retained, content) {
		t.Fatal("ingestion did not preserve the byte-exact original")
	}
	if info, err := os.Stat(filepath.Join(assetDir, "person-hc-001-small.jpg")); err != nil || info.Size() == 0 {
		t.Fatalf("proxy was not written: info=%v err=%v", info, err)
	}
}

func TestIngestDemoPhotosRefusesAMissingSelectedSource(t *testing.T) {
	t.Parallel()

	err := ingestDemoPhotos(context.Background(), []demoworkforce.Employee{{
		HasProfilePhoto: true, PhotoSourceName: "hc-001.png",
	}}, t.TempDir(), t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("missing selected source unexpectedly succeeded")
	}
}

func demoPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 240, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 240; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
