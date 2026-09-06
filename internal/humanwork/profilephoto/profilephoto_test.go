package profilephoto

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadPreservesOriginalAndCreatesSquareProxy(t *testing.T) {
	t.Parallel()

	source := testPNG(t, 320, 240, color.NRGBA{R: 39, G: 121, B: 89, A: 255})
	root := t.TempDir()
	result, err := Upload(context.Background(), FileStore{Root: root}, Request{
		WorkerKey:         "hc-001-test-worker",
		Content:           source,
		DeclaredMediaType: "image/png",
		OriginalName:      "hc-001.png",
		ProxyName:         "person-hc-001-small.jpg",
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if result.OriginalRef != "profile-originals/hc-001.png" {
		t.Fatalf("OriginalRef = %q", result.OriginalRef)
	}
	if result.ProxyRef != "/workspace/assets/person-hc-001-small.jpg" {
		t.Fatalf("ProxyRef = %q", result.ProxyRef)
	}
	if result.SourceWidth != 320 || result.SourceHeight != 240 || result.ProxyWidth != ProxySize || result.ProxyHeight != ProxySize {
		t.Fatalf("unexpected dimensions: %+v", result)
	}
	retained, err := os.ReadFile(filepath.Join(root, "profile-originals", "hc-001.png"))
	if err != nil {
		t.Fatalf("read retained original: %v", err)
	}
	if !bytes.Equal(retained, source) {
		t.Fatal("retained original is not byte-exact")
	}
	proxy, err := os.Open(filepath.Join(root, "person-hc-001-small.jpg"))
	if err != nil {
		t.Fatalf("open proxy: %v", err)
	}
	defer proxy.Close()
	decoded, err := jpeg.Decode(proxy)
	if err != nil {
		t.Fatalf("decode proxy: %v", err)
	}
	if decoded.Bounds().Dx() != ProxySize || decoded.Bounds().Dy() != ProxySize {
		t.Fatalf("proxy dimensions = %v", decoded.Bounds())
	}
	if result.OriginalDigest == result.ProxyDigest {
		t.Fatal("original and proxy digests unexpectedly match")
	}
}

func TestUploadRejectsMismatchedMediaTypeAndUnsafeNames(t *testing.T) {
	t.Parallel()

	source := testPNG(t, 200, 200, color.NRGBA{R: 20, A: 255})
	for name, request := range map[string]Request{
		"mismatched type": {WorkerKey: "worker", Content: source, DeclaredMediaType: "image/jpeg", OriginalName: "worker.png", ProxyName: "worker.jpg"},
		"unsafe original": {WorkerKey: "worker", Content: source, DeclaredMediaType: "image/png", OriginalName: "../worker.png", ProxyName: "worker.jpg"},
		"unsafe proxy":    {WorkerKey: "worker", Content: source, DeclaredMediaType: "image/png", OriginalName: "worker.png", ProxyName: "nested/worker.jpg"},
	} {
		request := request
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Upload(context.Background(), FileStore{Root: t.TempDir()}, request); err == nil {
				t.Fatal("Upload unexpectedly succeeded")
			}
		})
	}
}

func TestFileStoreIsContentIdempotentAndRefusesOverwrite(t *testing.T) {
	t.Parallel()

	store := FileStore{Root: t.TempDir()}
	if _, err := store.PutProxy(context.Background(), "worker.jpg", []byte("same")); err != nil {
		t.Fatalf("first PutProxy: %v", err)
	}
	if _, err := store.PutProxy(context.Background(), "worker.jpg", []byte("same")); err != nil {
		t.Fatalf("idempotent PutProxy: %v", err)
	}
	if _, err := store.PutProxy(context.Background(), "worker.jpg", []byte("different")); err == nil {
		t.Fatal("different content unexpectedly overwrote existing asset")
	}
}

func TestReadAllBoundedRejectsOversize(t *testing.T) {
	t.Parallel()

	if _, err := ReadAllBounded(bytes.NewReader(make([]byte, MaxUploadBytes+1))); err == nil {
		t.Fatal("oversized upload unexpectedly succeeded")
	}
}

func testPNG(t *testing.T, width, height int, fill color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixel := fill
			pixel.G = uint8((int(fill.G) + x + y) % 256)
			img.SetNRGBA(x, y, pixel)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return encoded.Bytes()
}
