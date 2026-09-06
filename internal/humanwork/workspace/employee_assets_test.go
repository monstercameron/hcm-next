package workspace

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"testing"
)

func TestEmployeePhotosAreExplicitSameOriginAssets(t *testing.T) {
	photos := []struct {
		original string
		proxy    string
	}{
		{assetPersonPriya, assetPersonPriyaSmall},
		{assetPersonJane, assetPersonJaneSmall},
		{assetPersonOmar, assetPersonOmarSmall},
		{assetPersonLena, assetPersonLenaSmall},
		{assetPersonNoor, assetPersonNoorSmall},
	}
	for _, photo := range photos {
		original, ok := employeePhotoOriginal(photo.original)
		if !ok || len(original) == 0 {
			t.Fatalf("employee original %q is not embedded", photo.original)
		}
		proxy, ok := asset(photo.proxy)
		if !ok || len(proxy) == 0 {
			t.Fatalf("employee proxy %q is not embedded", photo.proxy)
		}
		if got := assetContentType(photo.original); got != "image/png" {
			t.Fatalf("employee original %q content type = %q", photo.original, got)
		}
		if got := assetContentType(photo.proxy); got != "image/jpeg" {
			t.Fatalf("employee proxy %q content type = %q", photo.proxy, got)
		}
		if len(proxy) >= len(original)/10 {
			t.Fatalf("employee proxy %q is %d bytes; original %q is %d bytes", photo.proxy, len(proxy), photo.original, len(original))
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
	if _, ok := asset(assetPersonPriya); ok {
		t.Fatal("retained employee original escaped onto the public asset route")
	}
}
