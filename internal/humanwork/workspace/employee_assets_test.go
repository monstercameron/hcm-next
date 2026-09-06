package workspace

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"testing"
)

func TestEmployeePhotosAreExplicitSameOriginAssets(t *testing.T) {
	legacyPhotos := []struct {
		original string
		proxy    string
	}{
		{assetPersonPriya, assetPersonPriyaSmall},
		{assetPersonJane, assetPersonJaneSmall},
		{assetPersonOmar, assetPersonOmarSmall},
		{assetPersonLena, assetPersonLenaSmall},
		{assetPersonNoor, assetPersonNoorSmall},
	}
	for _, photo := range legacyPhotos {
		original, ok := employeePhotoOriginal(photo.original)
		if !ok || len(original) == 0 {
			t.Fatalf("employee original %q is not embedded", photo.original)
		}
		if got := assetContentType(photo.original); got != "image/png" {
			t.Fatalf("employee original %q content type = %q", photo.original, got)
		}
	}
	photos := append([]struct{ original, proxy string }(nil), legacyPhotos...)
	for index := 1; index <= 59; index++ {
		if index%4 == 0 {
			continue
		}
		photos = append(photos, struct{ original, proxy string }{
			original: fmt.Sprintf("hc-%03d.png", index),
			proxy:    fmt.Sprintf("person-hc-%03d-small.jpg", index),
		})
	}
	for _, photo := range photos {
		proxy, ok := asset(photo.proxy)
		if !ok || len(proxy) == 0 {
			t.Fatalf("employee proxy %q is not embedded", photo.proxy)
		}
		if got := assetContentType(photo.proxy); got != "image/jpeg" {
			t.Fatalf("employee proxy %q content type = %q", photo.proxy, got)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(proxy))
		if err != nil {
			t.Fatalf("decode employee proxy %q: %v", photo.proxy, err)
		}
		if config.Width != 160 || config.Height != 160 {
			t.Fatalf("employee proxy %q is %dx%d, want 160x160", photo.proxy, config.Width, config.Height)
		}
	}
	if _, ok := asset("person-unknown.png"); ok {
		t.Fatal("unknown employee asset escaped the explicit allowlist")
	}
	if _, ok := employeePhotoOriginal("person-unknown.png"); ok {
		t.Fatal("unknown employee original escaped the explicit allowlist")
	}
	if _, ok := employeePhotoOriginal("hc-001.png"); ok {
		t.Fatal("seeded retained original escaped into the embedded workspace")
	}
	if _, ok := asset(assetPersonPriya); ok {
		t.Fatal("retained employee original escaped onto the public asset route")
	}
	if _, ok := asset("person-hc-060-small.jpg"); ok {
		t.Fatal("unpopulated employee proxy escaped the explicit allowlist")
	}
	if _, ok := asset("person-hc-004-small.jpg"); ok {
		t.Fatal("deliberately unpopulated employee proxy escaped the explicit allowlist")
	}
}
